//go:build linux

package transcode

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

type liveJournalPipe struct {
	read, write *os.File
	records     chan liveJournalRecord
}
type liveSubtitlePipe struct {
	read, write *os.File
	slot        int
	completed   int64
}

type liveObserver struct {
	ctx          context.Context
	cancel       func()
	plan         Plan
	live         liveRuntime
	directory    *os.File
	journals     []*liveJournalPipe
	subtitles    []*liveSubtitlePipe
	mu           sync.Mutex
	err          error
	clocks       [MaxHLSRenditions]HLSMuxClock
	known        [MaxHLSRenditions]bool
	clockChanged chan struct{}
	initHashes   [MaxHLSRenditions][32]byte
	readers      sync.WaitGroup
	done         chan struct{}
	watchStop    chan struct{}
	watchDone    chan struct{}
	stopContext  func() bool
	closeOnce    sync.Once
	report       func(Progress)
}

func newLiveObserver(ctx context.Context, directory string, p Plan, live liveRuntime, cancel func(), report func(Progress)) (*liveObserver, error) {
	if live.publish == nil || live.maxBytes < 1 || p.SourceMode != "stream" {
		return nil, ErrInvalidOptions
	}
	live.maxBytes = min(live.maxBytes, MaxLiveScratchBytes)
	if live.timeout <= 0 {
		live.timeout = 30 * time.Second
	}
	fd, err := syscall.Open(directory, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrInvalidDirectory
	}
	o := &liveObserver{ctx: ctx, cancel: cancel, plan: p, live: live, directory: os.NewFile(uintptr(fd), directory), clockChanged: make(chan struct{}), done: make(chan struct{}), watchStop: make(chan struct{}), watchDone: make(chan struct{}), report: report}
	for index := 0; index < liveRenditionCount(p); index++ {
		read, write, err := os.Pipe()
		if err != nil {
			o.close()
			return nil, err
		}
		// A small kernel FIFO bounds records that can accumulate while the
		// synchronous publication callback is blocked. No growing list is kept.
		_, _, _ = syscall.Syscall(syscall.SYS_FCNTL, write.Fd(), syscall.F_SETPIPE_SZ, liveJournalPipeBytes)
		size, _, errno := syscall.Syscall(syscall.SYS_FCNTL, write.Fd(), syscall.F_GETPIPE_SZ, 0)
		if errno != 0 || size > 64<<10 {
			_ = read.Close()
			_ = write.Close()
			o.close()
			return nil, ErrStart
		}
		o.journals = append(o.journals, &liveJournalPipe{read: read, write: write, records: make(chan liveJournalRecord, 1)})
	}
	tracks := PlanHLSSubtitles(p)
	for slot := 0; slot < tracks.Count; slot++ {
		if liveSubtitleFD(p, slot) < 0 {
			continue
		}
		if live.caption == nil {
			o.close()
			return nil, ErrInvalidOptions
		}
		read, write, err := os.Pipe()
		if err != nil {
			o.close()
			return nil, err
		}
		o.subtitles = append(o.subtitles, &liveSubtitlePipe{read: read, write: write, slot: slot})
	}
	o.stopContext = context.AfterFunc(ctx, o.closePipes)
	return o, nil
}

func (o *liveObserver) fail(err error) {
	if err == nil {
		return
	}
	o.mu.Lock()
	if o.err == nil {
		o.err = err
	}
	o.mu.Unlock()
	o.cancel()
}

func (o *liveObserver) setClock(clock HLSMuxClock) {
	if clock.Rendition < 0 || clock.Rendition >= len(o.journals) {
		o.fail(ErrInvalidTimeline)
		return
	}
	if _, err := clock.ticks(false); err != nil {
		o.fail(err)
		return
	}
	o.mu.Lock()
	if !o.known[clock.Rendition] {
		o.clocks[clock.Rendition], o.known[clock.Rendition] = clock, true
		close(o.clockChanged)
		o.clockChanged = make(chan struct{})
	}
	o.mu.Unlock()
}

func (o *liveObserver) firstClock(rendition int) (int64, error) {
	for {
		o.mu.Lock()
		known, clock, changed := o.known[rendition], o.clocks[rendition], o.clockChanged
		o.mu.Unlock()
		if known {
			return clock.ticks(false)
		}
		select {
		case <-o.ctx.Done():
			return 0, o.ctx.Err()
		case <-changed:
		}
	}
}

func (o *liveObserver) start() {
	for index, pipe := range o.journals {
		_ = pipe.write.Close()
		o.readers.Add(1)
		go func(index int, pipe *liveJournalPipe) {
			defer o.readers.Done()
			defer close(pipe.records)
			defer pipe.read.Close()
			reader := bufio.NewReaderSize(pipe.read, 512)
			for sequence := int64(0); ; sequence++ {
				line, err := reader.ReadSlice('\n')
				if errors.Is(err, io.EOF) && len(line) == 0 {
					return
				}
				if err != nil || len(line) > liveJournalLineBytes {
					o.fail(ErrInvalidTimeline)
					return
				}
				record, err := parseLiveJournalRecord(string(line), o.plan, index, sequence)
				if err != nil {
					o.fail(err)
					return
				}
				select {
				case pipe.records <- record:
				case <-o.ctx.Done():
					return
				}
			}
		}(index, pipe)
	}
	for _, pipe := range o.subtitles {
		_ = pipe.write.Close()
		o.readers.Add(1)
		go func(pipe *liveSubtitlePipe) {
			defer o.readers.Done()
			defer pipe.read.Close()
			// These records close a companion containing copied reference and
			// original subtitle packets. The callback extracts and commits that
			// complete source prefix before advancing its subtitle watermark.
			reader := bufio.NewReaderSize(pipe.read, 512)
			for sequence := int64(0); ; sequence++ {
				line, err := reader.ReadSlice('\n')
				if errors.Is(err, io.EOF) && len(line) == 0 {
					return
				}
				if err != nil || len(line) > liveJournalLineBytes {
					o.fail(ErrInvalidTimeline)
					return
				}
				name, err := LiveCaptionName(o.plan, pipe.slot, sequence)
				if err != nil {
					o.fail(err)
					return
				}
				record, err := parseLiveJournalNamedRecord(string(line), name, sequence)
				if err != nil {
					o.fail(err)
					return
				}
				if err := o.publishCaption(pipe.slot, record); err != nil {
					o.fail(err)
					return
				}
				pipe.completed = sequence + 1
			}
		}(pipe)
	}
	go func() {
		defer close(o.done)
		if err := o.publish(); err != nil {
			o.fail(err)
		}
	}()
	go o.watch()
}

func (o *liveObserver) publishCaption(slot int, record liveJournalRecord) error {
	file, err := cacheOpenRegular(o.directory, record.name, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	defer syscall.Unlinkat(int(o.directory.Fd()), record.name)
	before, err := file.Stat()
	if err != nil || before.Size() <= 0 || before.Size() > min(MaxLiveSegmentBytes, o.live.maxBytes) {
		return ErrQuota
	}
	container, err := LiveCaptionContainer(o.plan, slot)
	if err != nil {
		return err
	}
	track, valid := HLSSubtitleTrackAt(o.plan, slot)
	if !valid {
		return ErrInvalidPlan
	}
	if record.end <= record.start || record.start < 0 {
		return ErrInvalidTimeline
	}
	segment := LiveCaptionSegment{Slot: slot, Sequence: record.sequence, StartTicks: record.start, EndTicks: record.end, File: file,
		SubtitleStreamIndex: 1, Codec: track.Codec, Container: container}
	ctx, cancel := context.WithTimeout(o.ctx, o.live.timeout)
	defer cancel()
	if err := o.live.caption(ctx, o.live.spec, o.live.jobID, segment); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !transcodeSourceUnchanged(file, before) {
		return ErrInvalidInput
	}
	return nil
}

func (o *liveObserver) publish() error {
	var previousEnd int64
	for sequence := int64(0); ; sequence++ {
		var records [MaxHLSRenditions]liveJournalRecord
		closed := 0
		for index, pipe := range o.journals {
			select {
			case record, ok := <-pipe.records:
				if !ok {
					closed++
					continue
				}
				if record.sequence != sequence {
					return ErrInvalidTimeline
				}
				records[index] = record
			case <-o.ctx.Done():
				return o.ctx.Err()
			}
		}
		if closed == len(o.journals) {
			if sequence == 0 {
				return ErrOutputUnavailable
			}
			return nil
		}
		if closed != 0 {
			return ErrInvalidTimeline
		}
		bundle := LiveSegment{Sequence: sequence, RenditionCount: len(o.journals), Discontinuity: sequence == 0}
		for index := range o.journals {
			if sequence == 0 {
				clock, err := o.firstClock(index)
				if err != nil {
					return err
				}
				if clock < 0 {
					return ErrInvalidTimeline
				}
				records[index].start = clock
			}
			if records[index].end <= records[index].start || records[index].end-records[index].start > liveMaxSegmentTicks {
				return ErrInvalidTimeline
			}
			if index > 0 && (!liveTicksClose(records[index].start, records[0].start) || !liveTicksClose(records[index].end, records[0].end)) {
				return ErrInvalidTimeline
			}
		}
		bundle.StartTicks, bundle.DurationTicks = records[0].start, records[0].end-records[0].start
		if sequence > 0 {
			if bundle.StartTicks < previousEnd-LiveAlignmentTicks {
				return ErrInvalidTimeline
			}
			bundle.Discontinuity = !liveTicksClose(bundle.StartTicks, previousEnd)
		}
		if err := o.publishBundle(bundle, records); err != nil {
			return err
		}
		previousEnd = records[0].end
	}
}

func (o *liveObserver) publishBundle(bundle LiveSegment, records [MaxHLSRenditions]liveJournalRecord) (result error) {
	var opened [MaxHLSRenditions]liveOpenedSegment
	defer func() {
		for index := range o.journals {
			if err := opened[index].closeAndRemove(o.directory); err != nil && result == nil {
				result = err
			}
		}
	}()
	var bytes int64
	for index := range o.journals {
		var err error
		opened[index], err = openLiveSegment(o.directory, o.plan, index, records[index], min(MaxLiveSegmentBytes, o.live.maxBytes), bundle.Sequence == 0, o.initHashes[index])
		if err != nil {
			return err
		}
		bundle.Renditions[index] = opened[index].render
		bundle.Renditions[index].PreMuxClockTicks = records[index].start
		bundle.Renditions[index].DurationTicks = records[index].end - records[index].start
		bytes += opened[index].render.Size
		if bytes > o.live.maxBytes {
			return ErrQuota
		}
	}
	ctx, cancel := context.WithTimeout(o.ctx, o.live.timeout)
	defer cancel()
	if err := o.live.publish(ctx, o.live.spec, o.live.jobID, bundle); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for index := range o.journals {
		o.initHashes[index] = opened[index].initHash
	}
	if o.report != nil {
		o.report(Progress{Ready: true, OutputTicks: bundle.StartTicks + bundle.DurationTicks})
	}
	return nil
}

func (o *liveObserver) watch() {
	defer close(o.watchDone)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-o.watchStop:
			return
		case <-o.ctx.Done():
			return
		case <-ticker.C:
			if err := o.scratchBudget(); err != nil {
				o.fail(err)
				return
			}
		}
	}
}

func (o *liveObserver) scratchBudget() error {
	names, err := cacheDirectoryNames(o.directory, liveScratchFileLimit)
	if err != nil {
		return err
	}
	var bytes int64
	for _, name := range names {
		if !validLiveScratchName(name, o.plan) {
			return ErrCacheUnsafe
		}
		file, err := cacheOpenRegular(o.directory, name, syscall.O_RDONLY, 0)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		info, err := file.Stat()
		_ = file.Close()
		if err != nil {
			return err
		}
		if info.Size() < 0 || info.Size() > o.live.maxBytes-bytes {
			return ErrQuota
		}
		bytes += info.Size()
	}
	return nil
}

func validLiveScratchName(name string, p Plan) bool {
	if isLiveCaptionScratchName(name) {
		return true
	}
	for index := 0; index < liveRenditionCount(p); index++ {
		prefix := hlsPrefix(index, p.HLS.RenditionCount)
		if p.HLS.SegmentType == "fmp4" && name == prefix+"init.mp4" {
			return true
		}
		number, _, _, valid := generatedHLSSegment(strings.TrimSuffix(name, ".tmp"))
		if valid && (name == liveSegmentName(p, index, number, true) || name == liveSegmentName(p, index, number, false)) {
			return true
		}
	}
	return false
}

func (o *liveObserver) closePipes() {
	for _, pipe := range o.journals {
		_ = pipe.write.Close()
		_ = pipe.read.Close()
	}
	for _, pipe := range o.subtitles {
		_ = pipe.write.Close()
		_ = pipe.read.Close()
	}
}
func (o *liveObserver) close() {
	o.closeOnce.Do(func() {
		if o.stopContext != nil {
			o.stopContext()
		}
		o.closePipes()
		if o.directory != nil {
			_ = o.directory.Close()
		}
	})
}
func (o *liveObserver) finish(success bool) error {
	<-o.done
	o.readers.Wait()
	close(o.watchStop)
	<-o.watchDone
	o.mu.Lock()
	err := o.err
	o.mu.Unlock()
	if err != nil {
		return err
	}
	if success && o.ctx.Err() == nil {
		for _, pipe := range o.subtitles {
			ctx, cancel := context.WithTimeout(o.ctx, o.live.timeout)
			err := o.live.caption(ctx, o.live.spec, o.live.jobID, LiveCaptionSegment{Slot: pipe.slot, Sequence: pipe.completed, Complete: true})
			if err == nil {
				err = ctx.Err()
			}
			cancel()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
