package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	dynamicSubtitleTrackBytes = 1 << 20
	dynamicSubtitleClockLimit = 4096
	dynamicSubtitleEpochLimit = 64
	dynamicSubtitleWait       = 10 * time.Second
	dynamicSubtitleAnchorWait = 30 * time.Second
)

type dynamicSegmentClock struct {
	Generation                            uint64
	StartTicks, EndTicks, ClockDeltaTicks int64
	artifactID                            string
}

type dynamicSubtitleEpoch struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mediaDelta int64
	anchor     subtitle.LiveClockAnchor
	failures   map[int]error
	intervals  map[int]dynamicSubtitleInterval
	captions   map[int]dynamicCaptionProgress
	watermarks map[int]int64
}

type dynamicSubtitleInterval struct {
	start, end int64
}

type dynamicCaptionProgress struct {
	sequence, start, end int64
}

type dynamicSubtitleTrack struct {
	journal          *subtitle.LiveJournal
	prunedGeneration uint64
	prunedStart      int64
}

// Each fixed track receives at most one MiB across all epochs. Independent
// journals keep a sparse or stalled track from sealing another track's input.
type dynamicSubtitleRuntime struct {
	mu      sync.Mutex
	plan    transcode.HLSSubtitlePlan
	tracks  map[int]*dynamicSubtitleTrack
	epochs  map[uint64]*dynamicSubtitleEpoch
	media   map[uint64]dynamicSegmentClock
	current uint64
	changed chan struct{}
}

func newDynamicSubtitleRuntime(plan transcode.Plan) (*dynamicSubtitleRuntime, error) {
	if err := transcode.ValidateHLSSubtitlePlan(plan); err != nil {
		return nil, err
	}
	tracks := transcode.PlanHLSSubtitles(plan)
	if tracks.Count < 1 || tracks.Count > transcode.MaxHLSSubtitleTracks {
		return nil, subtitle.ErrLiveTrack
	}
	runtime := &dynamicSubtitleRuntime{plan: tracks, tracks: make(map[int]*dynamicSubtitleTrack),
		epochs: make(map[uint64]*dynamicSubtitleEpoch), media: make(map[uint64]dynamicSegmentClock), changed: make(chan struct{})}
	for _, track := range tracks.Tracks[:tracks.Count] {
		journal, err := subtitle.NewLiveJournal(subtitle.LiveJournalOptions{MaxBytes: dynamicSubtitleTrackBytes,
			MaxCues: subtitle.MaxCueCount / transcode.MaxHLSSubtitleTracks, MaxTracks: 1, MaxEpochs: dynamicSubtitleEpochLimit})
		if err != nil {
			return nil, err
		}
		runtime.tracks[track.StreamIndex] = &dynamicSubtitleTrack{journal: journal}
	}
	return runtime, nil
}

func (runtime *dynamicSubtitleRuntime) signalLocked() {
	close(runtime.changed)
	runtime.changed = make(chan struct{})
}

func (runtime *dynamicSubtitleRuntime) beginEpoch(ctx context.Context, generation uint64, mediaDelta int64) (context.Context, bool, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if epoch := runtime.epochs[generation]; epoch != nil {
		if generation != runtime.current || epoch.mediaDelta != mediaDelta || epoch.ctx.Err() != nil {
			return nil, false, subtitle.ErrLiveConflict
		}
		return epoch.ctx, false, nil
	}
	if generation == 0 || generation <= runtime.current {
		return nil, false, subtitle.ErrLiveEpoch
	}
	if len(runtime.epochs) >= dynamicSubtitleEpochLimit {
		return nil, false, subtitle.ErrLimitExceeded
	}
	for index, track := range runtime.tracks {
		if err := track.journal.BeginEpoch(generation, []int{index}); err != nil {
			return nil, false, err
		}
	}
	if previous := runtime.epochs[runtime.current]; previous != nil {
		previous.cancel()
	}
	work, cancel := context.WithCancel(ctx)
	runtime.epochs[generation] = &dynamicSubtitleEpoch{ctx: work, cancel: cancel, mediaDelta: mediaDelta,
		failures: make(map[int]error), intervals: make(map[int]dynamicSubtitleInterval), captions: make(map[int]dynamicCaptionProgress), watermarks: make(map[int]int64)}
	runtime.current = generation
	runtime.signalLocked()
	return work, true, nil
}

func (runtime *dynamicSubtitleRuntime) CancelEpoch(generation uint64) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if epoch := runtime.epochs[generation]; epoch != nil {
		epoch.cancel()
		for index := range runtime.tracks {
			epoch.failures[index] = context.Canceled
		}
		runtime.signalLocked()
	}
}

func (runtime *dynamicSubtitleRuntime) StopEpoch(generation uint64) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if epoch := runtime.epochs[generation]; epoch != nil {
		epoch.cancel()
		runtime.signalLocked()
	}
}

func (runtime *dynamicSubtitleRuntime) BindSourceAnchor(generation uint64, anchor subtitle.LiveClockAnchor) error {
	if !anchor.Known || anchor.Generation != generation || anchor.MPEGTS < 0 || anchor.SourceTicks < 0 ||
		anchor.MaxDistance90k <= 0 || anchor.MaxDistance90k >= 1<<32 {
		return subtitle.ErrLiveTimestampMap
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	epoch := runtime.epochs[generation]
	if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
		return subtitle.ErrLiveEpoch
	}
	if epoch.anchor.Known {
		if epoch.anchor == anchor {
			return nil
		}
		if anchor.SourceTicks < epoch.anchor.SourceTicks && anchor.MPEGTS > epoch.anchor.MPEGTS ||
			anchor.SourceTicks > epoch.anchor.SourceTicks && anchor.MPEGTS < epoch.anchor.MPEGTS {
			return subtitle.ErrLiveConflict
		}
		ticks := anchor.SourceTicks - epoch.anchor.SourceTicks
		fraction := ticks % media.TicksPerSecond * 90_000
		if fraction >= 0 {
			fraction += media.TicksPerSecond / 2
		} else {
			fraction -= media.TicksPerSecond / 2
		}
		expected := ticks/media.TicksPerSecond*90_000 + fraction/media.TicksPerSecond
		difference := anchor.MPEGTS - epoch.anchor.MPEGTS - expected
		if difference < -1 || difference > 1 {
			return subtitle.ErrLiveConflict
		}
		if anchor.SourceTicks < epoch.anchor.SourceTicks {
			return nil
		}
	}
	epoch.anchor = anchor
	runtime.signalLocked()
	return nil
}

// PublishMedia records observed AV coordinates only. A closed AV segment does
// not prove that an independent subtitle pipe has supplied all of its cues.
func (runtime *dynamicSubtitleRuntime) PublishMedia(generation, sequence uint64, start, end, delta int64) error {
	if sequence > math.MaxInt32 || end <= start || end < 0 || start < 0 && end > math.MaxInt64+start || delta < -subtitle.MaxOffsetTicks || delta > subtitle.MaxOffsetTicks {
		return subtitle.ErrInvalidRange
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.epochs[generation] == nil || generation != runtime.current {
		return subtitle.ErrLiveEpoch
	}
	clock := dynamicSegmentClock{Generation: generation, StartTicks: start, EndTicks: end, ClockDeltaTicks: delta}
	if old, exists := runtime.media[sequence]; exists {
		clock.artifactID = old.artifactID
		if old != clock {
			return subtitle.ErrLiveConflict
		}
		return nil
	}
	if len(runtime.media) >= dynamicSubtitleClockLimit {
		return subtitle.ErrLimitExceeded
	}
	runtime.media[sequence] = clock
	runtime.signalLocked()
	return nil
}

func (runtime *dynamicSubtitleRuntime) Append(generation uint64, streamIndex int, document subtitle.Document) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	epoch, track := runtime.epochs[generation], runtime.tracks[streamIndex]
	if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
		return subtitle.ErrLiveEpoch
	}
	if track == nil {
		return subtitle.ErrLiveTrack
	}
	if err := track.journal.Append(generation, streamIndex, document); err != nil {
		return err
	}
	runtime.signalLocked()
	return nil
}

// CompleteTrack requires an explicit ordered-input completeness barrier. Neither
// the newest cue's end nor media publication nor elapsed time is such a barrier.
func (runtime *dynamicSubtitleRuntime) CompleteTrack(generation uint64, streamIndex int, watermark int64) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	epoch, track := runtime.epochs[generation], runtime.tracks[streamIndex]
	if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
		return subtitle.ErrLiveEpoch
	}
	if track == nil {
		return subtitle.ErrLiveTrack
	}
	if err := runtime.advanceLocked(epoch, track, generation, streamIndex, watermark); err != nil {
		return err
	}
	runtime.signalLocked()
	return nil
}

func (runtime *dynamicSubtitleRuntime) advanceLocked(epoch *dynamicSubtitleEpoch, track *dynamicSubtitleTrack, generation uint64, index int, watermark int64) error {
	if err := track.journal.Advance(generation, watermark); err != nil {
		return err
	}
	epoch.watermarks[index] = watermark
	return nil
}

func (runtime *dynamicSubtitleRuntime) failTrack(generation uint64, index int, err error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if epoch := runtime.epochs[generation]; epoch != nil && generation == runtime.current {
		epoch.failures[index] = err
		runtime.signalLocked()
	}
}

func (runtime *dynamicSubtitleRuntime) render(streamIndex int, sequence uint64, offset int64) (subtitle.Result, dynamicSegmentClock, error) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	clock, err := runtime.readyClockLocked(streamIndex, sequence, offset)
	if err != nil {
		return subtitle.Result{}, clock, err
	}
	result, err := runtime.tracks[streamIndex].journal.Window(clock.Generation, streamIndex, max(0, clock.StartTicks), clock.EndTicks, offset, clock.ClockDeltaTicks)
	return result, clock, err
}

// Playlist readiness reads the same proven watermarks without rendering every
// possible VTT body. A long crossing cue must not allocate its full text once
// per retained media segment merely to advertise a bounded playlist.
func (runtime *dynamicSubtitleRuntime) readyClockLocked(streamIndex int, sequence uint64, offset int64) (dynamicSegmentClock, error) {
	clock, exists := runtime.media[sequence]
	if !exists {
		return clock, subtitle.ErrLiveWindowExpired
	}
	track := runtime.tracks[streamIndex]
	if track == nil {
		return clock, subtitle.ErrLiveTrack
	}
	if offset < -subtitle.MaxOffsetTicks || offset > subtitle.MaxOffsetTicks {
		return clock, subtitle.ErrInvalidRange
	}
	start := max(0, clock.StartTicks)
	if clock.EndTicks <= start {
		return clock, subtitle.ErrInvalidRange
	}
	sourceStart, err := dynamicSubtitleAddTicks(start, -offset)
	if err != nil {
		return clock, err
	}
	sourceEnd, err := dynamicSubtitleAddTicks(clock.EndTicks, -offset)
	if err != nil {
		return clock, err
	}
	sourceStart, sourceEnd = max(0, sourceStart), max(0, sourceEnd)
	if clock.Generation < track.prunedGeneration || clock.Generation == track.prunedGeneration && (start < track.prunedStart || sourceStart < track.prunedStart) {
		return clock, subtitle.ErrLiveWindowExpired
	}
	epoch := runtime.epochs[clock.Generation]
	if epoch == nil {
		return clock, subtitle.ErrLiveEpoch
	}
	if interval, known := epoch.intervals[streamIndex]; known && sourceStart < interval.start {
		return clock, subtitle.ErrLiveWindowExpired
	}
	watermark, known := epoch.watermarks[streamIndex]
	if !known || max(clock.EndTicks, sourceEnd) > watermark {
		if epoch.failures[streamIndex] != nil {
			return clock, errDynamicSubtitleSource
		}
		return clock, subtitle.ErrLiveNotReady
	}
	return clock, nil
}

// retain binds opaque media IDs and only discards old coordinates when Store
// confirms that their advertised grace resource is no longer readable.
func (runtime *dynamicSubtitleRuntime) retain(snapshot timeshift.WindowSnapshot, readable func(string) bool) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	current := make(map[uint64]bool, len(snapshot.Segments))
	for _, segment := range snapshot.Segments {
		clock, exists := runtime.media[segment.Sequence]
		if !exists || clock.Generation != segment.Generation || clock.EndTicks-clock.StartTicks != segment.DurationTicks || len(segment.Artifacts) == 0 {
			return subtitle.ErrLiveNotReady
		}
		if clock.artifactID != "" && clock.artifactID != segment.Artifacts[0].ID {
			return subtitle.ErrLiveConflict
		}
		clock.artifactID = segment.Artifacts[0].ID
		runtime.media[segment.Sequence] = clock
		current[segment.Sequence] = true
	}
	for sequence, clock := range runtime.media {
		if current[sequence] {
			continue
		}
		// A concurrently published successor is not covered by this snapshot.
		if len(snapshot.Segments) > 0 && sequence > snapshot.Segments[len(snapshot.Segments)-1].Sequence ||
			len(snapshot.Segments) == 0 && sequence >= snapshot.NextSequence {
			continue
		}
		if clock.artifactID != "" && readable(clock.artifactID) {
			continue
		}
		delete(runtime.media, sequence)
	}
	minimum, earliest := runtime.current, int64(0)
	found := false
	for _, clock := range runtime.media {
		if !found || clock.Generation < minimum || clock.Generation == minimum && clock.StartTicks < earliest {
			minimum, earliest, found = clock.Generation, max(0, clock.StartTicks), true
		}
	}
	if minimum == 0 {
		return nil
	}
	for _, track := range runtime.tracks {
		if minimum > track.prunedGeneration {
			if err := track.journal.Prune(minimum, 0); err != nil {
				return err
			}
			track.prunedGeneration, track.prunedStart = minimum, 0
		}
		if earliest > track.prunedStart {
			if err := track.journal.Prune(minimum, earliest); err == nil {
				track.prunedStart = earliest
			} else if !errors.Is(err, subtitle.ErrLiveNotReady) {
				return err
			}
		}
	}
	for generation, epoch := range runtime.epochs {
		if generation < minimum {
			epoch.cancel()
			delete(runtime.epochs, generation)
		}
	}
	return nil
}

// The caller holds session.mu. Each reader belongs to the session/epoch, never
// the negotiating request. New epochs cancel old readers but retain their cues.
func (s *Server) beginDynamicSubtitlesLocked(session *dynamicStreamSession, lease dynamicsource.Lease, plan transcode.Plan) error {
	if !transcode.HasHLSSubtitles(plan) {
		return nil
	}
	if session.closed || session.ctx.Err() != nil {
		return dynamicsource.ErrClosed
	}
	if plan.SourceFormatStartKnown != lease.Info.FormatStartKnown ||
		plan.SourceFormatStartKnown && plan.SourceFormatStartTicks != lease.Info.FormatStartTicks {
		return subtitle.ErrLiveTimestampMap
	}
	if session.subtitles == nil {
		var err error
		session.subtitles, err = newDynamicSubtitleRuntime(plan)
		if err != nil {
			return err
		}
	} else if session.subtitles.plan != transcode.PlanHLSSubtitles(plan) {
		return subtitle.ErrLiveConflict
	}
	// Match the producer's explicit origin/headroom transform. An unknown
	// format origin means preserved raw copyts plus the same one-second bias.
	if plan.SourceFormatStartKnown && plan.SourceFormatStartTicks < media.TicksPerSecond-math.MaxInt64 {
		return subtitle.ErrLiveTimestampMap
	}
	delta := transcode.LiveSourceClockBiasTicks(plan)
	work, created, err := session.subtitles.beginEpoch(session.ctx, lease.Generation, delta)
	if err != nil || !created {
		return err
	}
	if plan.SourceFormatStartKnown {
		anchor, err := dynamicSourceClockAnchor(lease.Generation, media.TicksPerSecond, plan)
		if err != nil {
			session.subtitles.CancelEpoch(lease.Generation)
			return err
		}
		if err := session.subtitles.BindSourceAnchor(lease.Generation, anchor); err != nil {
			session.subtitles.CancelEpoch(lease.Generation)
			return err
		}
	}
	for _, track := range session.subtitles.plan.Tracks[:session.subtitles.plan.Count] {
		if track.ExternalTag == "" {
			continue
		}
		if s.dynamicSources == nil || s.dynamicStreams == nil {
			return dynamicsource.ErrUnavailable
		}
		s.dynamicStreams.workers.Add(1)
		go func(track transcode.HLSSubtitleTrack) {
			defer s.dynamicStreams.workers.Done()
			err := s.receiveExternalDynamicSubtitle(work, session, lease.Generation, track)
			if err != nil && work.Err() == nil {
				session.subtitles.failTrack(lease.Generation, track.StreamIndex, err)
			}
		}(track)
	}
	return nil
}

func (s *Server) appendDynamicSubtitle(ctx context.Context, spec transcode.Spec, jobID string, slot int, document subtitle.Document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session, generation, err := s.dynamicSessionForSpec(spec, jobID)
	if err != nil {
		return err
	}
	track, ok := transcode.HLSSubtitleTrackAt(spec.Plan, slot)
	if !ok || track.ExternalTag != "" {
		return subtitle.ErrLiveTrack
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.generation != generation || session.jobID != jobID || session.subtitles == nil {
		return dynamicsource.ErrStaleGeneration
	}
	if err := session.subtitles.Append(generation, track.StreamIndex, document); err != nil {
		return err
	}
	signalDynamicSessionLocked(session)
	return nil
}

func (s *Server) completeDynamicSubtitleTrack(ctx context.Context, spec transcode.Spec, jobID string, slot int, watermark int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session, generation, err := s.dynamicSessionForSpec(spec, jobID)
	if err != nil {
		return err
	}
	track, ok := transcode.HLSSubtitleTrackAt(spec.Plan, slot)
	if !ok || track.ExternalTag != "" {
		return subtitle.ErrLiveTrack
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.generation != generation || session.jobID != jobID || session.subtitles == nil {
		return dynamicsource.ErrStaleGeneration
	}
	if err := session.subtitles.CompleteTrack(generation, track.StreamIndex, watermark); err != nil {
		return err
	}
	signalDynamicSessionLocked(session)
	return nil
}

func (s *Server) receiveDynamicSubtitles(ctx context.Context, spec transcode.Spec, jobID string, slot int, reader io.Reader) error {
	return subtitle.ReadLiveWebVTT(reader, subtitle.LiveParserOptions{}, func(batch subtitle.LiveBatch) error {
		if batch.Header.TimestampMap != nil {
			return subtitle.ErrLiveTimestampMap
		}
		// The owned FFmpeg text output already uses the same explicitly selected
		// copyts/itsoffset transform as its AV packets. EOF does not seal a range.
		document, err := subtitle.MapLiveDocument(batch.Document, 0)
		if err != nil {
			return err
		}
		return s.appendDynamicSubtitle(ctx, spec, jobID, slot, document)
	})
}

// The engine keeps this finite companion file stable until the callback
// returns. Its closed mux interval, after extraction has drained every packet,
// is the explicit completeness barrier for embedded text, including empty spans.
func (s *Server) receiveDynamicCaption(ctx context.Context, spec transcode.Spec, jobID string, segment transcode.LiveCaptionSegment) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if segment.Sequence < 0 {
		return subtitle.ErrInvalidRange
	}
	session, generation, err := s.dynamicSessionForSpec(spec, jobID)
	if err != nil {
		return err
	}
	track, ok := transcode.HLSSubtitleTrackAt(spec.Plan, segment.Slot)
	if !ok || track.ExternalTag != "" || media.SubtitleExtractFormat(track.Codec) == "" {
		return subtitle.ErrLiveTrack
	}
	if segment.Complete {
		if segment.File != nil {
			return subtitle.ErrLiveConflict
		}
		session.mu.Lock()
		principal := session.principal
		session.mu.Unlock()
		principal, err = s.freshDynamicPrincipal(ctx, principal)
		if err != nil {
			return err
		}
		lease, err := s.dynamicSources.Info(ctx, dynamicSourceOwner(principal), session.key.liveID)
		if err != nil {
			return err
		}
		if lease.Generation != generation || lease.Stamp != spec.SourceStamp {
			return dynamicsource.ErrStaleGeneration
		}
		session.mu.Lock()
		defer session.mu.Unlock()
		if session.closed || session.generation != generation || session.jobID != jobID || session.subtitles == nil {
			return dynamicsource.ErrStaleGeneration
		}
		if err := session.subtitles.completeCaptionSource(generation, track.StreamIndex, segment.Sequence); err != nil {
			return err
		}
		signalDynamicSessionLocked(session)
		return nil
	}
	if segment.StartTicks < 0 || segment.EndTicks <= segment.StartTicks || media.SubtitleExtractFormat(track.Codec) != media.SubtitleExtractFormat(segment.Codec) {
		return subtitle.ErrInvalidRange
	}
	data, err := transcode.ExtractLiveCaption(ctx, s.cfg.FFmpegPath, segment)
	if err != nil {
		return err
	}
	document, err := subtitle.Parse(data, subtitle.FormatWebVTT)
	if err != nil {
		return err
	}
	if err := s.appendDynamicSubtitle(ctx, spec, jobID, segment.Slot, document); err != nil {
		return err
	}
	session, generation, err = s.dynamicSessionForSpec(spec, jobID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.generation != generation || session.jobID != jobID || session.subtitles == nil {
		return dynamicsource.ErrStaleGeneration
	}
	if err := session.subtitles.completeCaption(generation, track.StreamIndex, segment.Sequence, segment.StartTicks, segment.EndTicks); err != nil {
		return err
	}
	signalDynamicSessionLocked(session)
	return nil
}

// Complete is emitted only after successful producer exit and every preceding
// closed-file callback. The exact per-track count prevents a terminal marker
// from skipping missing extraction work or sealing an abandoned generation.
func (runtime *dynamicSubtitleRuntime) completeCaptionSource(generation uint64, index int, count int64) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	epoch, track := runtime.epochs[generation], runtime.tracks[index]
	if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
		return subtitle.ErrLiveEpoch
	}
	if track == nil {
		return subtitle.ErrLiveTrack
	}
	expected := int64(0)
	if previous, exists := epoch.captions[index]; exists {
		if previous.sequence == math.MaxInt64 {
			return subtitle.ErrLiveConflict
		}
		expected = previous.sequence + 1
	}
	if count != expected {
		return subtitle.ErrLiveConflict
	}
	if err := runtime.advanceLocked(epoch, track, generation, index, math.MaxInt64); err != nil {
		return err
	}
	runtime.signalLocked()
	return nil
}

func (runtime *dynamicSubtitleRuntime) completeCaption(generation uint64, index int, sequence, start, end int64) error {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	epoch, track := runtime.epochs[generation], runtime.tracks[index]
	if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
		return subtitle.ErrLiveEpoch
	}
	if track == nil {
		return subtitle.ErrLiveTrack
	}
	next := dynamicCaptionProgress{sequence: sequence, start: start, end: end}
	previous, exists := epoch.captions[index]
	if exists && previous == next {
		return nil
	}
	if start < 0 || end <= start || !exists && sequence != 0 || exists && (previous.sequence == math.MaxInt64 || sequence != previous.sequence+1 || end < previous.end) {
		return subtitle.ErrLiveConflict
	}
	if err := runtime.advanceLocked(epoch, track, generation, index, end); err != nil {
		return err
	}
	epoch.captions[index] = next
	runtime.signalLocked()
	return nil
}

func (s *Server) receiveExternalDynamicSubtitle(ctx context.Context, session *dynamicStreamSession, generation uint64, track transcode.HLSSubtitleTrack) error {
	session.mu.Lock()
	principal, runtime := session.principal, session.subtitles
	current := !session.closed && session.generation == generation
	session.mu.Unlock()
	if !current {
		return dynamicsource.ErrStaleGeneration
	}
	definition, err := s.dynamicSources.Subtitle(ctx, dynamicSourceOwner(principal), session.key.liveID, generation, track.StreamIndex, track.ExternalTag)
	if err != nil {
		return err
	}
	return readDynamicSubtitleSource(ctx, definition, func(update dynamicSubtitleSourceUpdate) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		delta := int64(0)
		if update.Batch.Document.Format != "" || update.IntervalLocalStartTicks != nil || update.WatermarkTicks != nil {
			delta, err = runtime.externalDelta(ctx, generation, update)
			if err != nil {
				return err
			}
		}
		if update.Batch.Document.Format != "" {
			document, err := subtitle.MapLiveDocument(update.Batch.Document, delta)
			if err != nil {
				return err
			}
			if err := runtime.Append(generation, track.StreamIndex, document); err != nil {
				return err
			}
		}
		if update.IntervalLocalStartTicks != nil {
			start, err := dynamicSubtitleAddTicks(*update.IntervalLocalStartTicks, delta)
			if err != nil {
				return err
			}
			end, err := dynamicSubtitleAddTicks(start, update.IntervalDurationTicks)
			if err != nil {
				return err
			}
			if err := runtime.completeInterval(generation, track.StreamIndex, start, end, update.Discontinuity); err != nil {
				return err
			}
		}
		if update.WatermarkTicks != nil {
			watermark, err := dynamicSubtitleAddTicks(*update.WatermarkTicks, delta)
			if err != nil {
				return err
			}
			if watermark >= 0 {
				if err := runtime.CompleteTrack(generation, track.StreamIndex, watermark); err != nil {
					return err
				}
			}
		}
		// ENDLIST can complete the future of an explicitly anchored HLS suffix;
		// its interval floor still prevents inventing an unavailable prefix.
		if update.SourceComplete && definition.Mode == "webvtt-hls" {
			runtime.mu.Lock()
			epoch := runtime.epochs[generation]
			anchored := false
			if epoch != nil {
				_, anchored = epoch.intervals[track.StreamIndex]
			}
			runtime.mu.Unlock()
			if !anchored {
				return subtitle.ErrLiveNotReady
			}
		}
		if update.SourceComplete && (definition.Mode == "document" || definition.Mode == "webvtt-hls") {
			return runtime.CompleteTrack(generation, track.StreamIndex, math.MaxInt64)
		}
		return nil
	})
}

func (runtime *dynamicSubtitleRuntime) externalDelta(ctx context.Context, generation uint64, update dynamicSubtitleSourceUpdate) (int64, error) {
	timer := time.NewTimer(dynamicSubtitleAnchorWait)
	defer timer.Stop()
	for {
		runtime.mu.Lock()
		epoch, changed := runtime.epochs[generation], runtime.changed
		if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
			runtime.mu.Unlock()
			return 0, subtitle.ErrLiveEpoch
		}
		anchor, delta := epoch.anchor, epoch.mediaDelta
		runtime.mu.Unlock()
		switch update.Clock {
		case "media":
			if update.Batch.Header.TimestampMap != nil {
				return 0, subtitle.ErrLiveTimestampMap
			}
		case "mpegts":
			if update.Batch.Header.TimestampMap == nil {
				return 0, subtitle.ErrLiveTimestampMap
			}
			if !anchor.Known {
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-timer.C:
					return 0, subtitle.ErrLiveNotReady
				case <-changed:
					continue
				}
			}
			var err error
			delta, err = subtitle.ResolveLiveTimestampMap(generation, *update.Batch.Header.TimestampMap, anchor)
			if err != nil {
				return 0, err
			}
		default:
			return 0, subtitle.ErrLiveTimestampMap
		}
		return dynamicSubtitleAddTicks(delta, update.OffsetTicks)
	}
}

func dynamicSubtitleAddTicks(value, delta int64) (int64, error) {
	if delta > 0 && value > math.MaxInt64-delta || delta < 0 && value < math.MinInt64-delta {
		return 0, subtitle.ErrInvalidRange
	}
	return value + delta, nil
}

func (runtime *dynamicSubtitleRuntime) completeInterval(generation uint64, index int, start, end int64, discontinuity bool) error {
	if end <= start || start < 0 && end > math.MaxInt64+start {
		return subtitle.ErrInvalidRange
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	epoch, track := runtime.epochs[generation], runtime.tracks[index]
	if epoch == nil || generation != runtime.current || epoch.ctx.Err() != nil {
		return subtitle.ErrLiveEpoch
	}
	if track == nil {
		return subtitle.ErrLiveTrack
	}
	interval, exists := epoch.intervals[index]
	if exists {
		near := start >= interval.end && uint64(start)-uint64(interval.end) <= 112 || start < interval.end && uint64(interval.end)-uint64(start) <= 112
		if discontinuity || !near || end <= interval.end {
			return subtitle.ErrLiveConflict
		}
	} else {
		interval.start = max(0, start)
	}
	if end >= 0 {
		if err := runtime.advanceLocked(epoch, track, generation, index, end); err != nil {
			return err
		}
	}
	interval.end = end
	epoch.intervals[index] = interval
	runtime.signalLocked()
	return nil
}

// This helper deliberately does not acquire session.mu, so the serialized
// publication callback can call it after recording the newest media clock.
func (s *Server) pruneDynamicSubtitles(ctx context.Context, session *dynamicStreamSession, snapshot timeshift.WindowSnapshot) error {
	if session.subtitles == nil {
		return nil
	}
	return session.subtitles.retain(snapshot, func(id string) bool {
		retained, err := s.dynamicStreams.store.RetainsArtifact(ctx, timeshiftScope(session.scope), session.windowID, id)
		// Cancellation/storage failure is not proof that an advertised resource
		// expired. Preserve its bounded metadata until a conclusive lookup.
		return retained || err != nil && !errors.Is(err, timeshift.ErrNotFound) && !errors.Is(err, timeshift.ErrClosed)
	})
}

func dynamicRenderedSubtitlePlaylist(snapshot timeshift.WindowSnapshot, resource func(uint64) string, view dynamicPlaybackView) ([]byte, error) {
	if snapshot.TargetDurationTicks <= 0 || snapshot.TargetDurationTicks%media.TicksPerSecond != 0 {
		return nil, timeshift.ErrInvalid
	}
	target := snapshot.TargetDurationTicks / media.TicksPerSecond
	if len(snapshot.Segments) == 0 {
		if !snapshot.Ended {
			return nil, timeshift.ErrNotBuffered
		}
		if view.StartTicks != nil || view.Live {
			return nil, timeshift.ErrWindowExpired
		}
		return []byte(fmt.Sprintf("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n#EXT-X-ENDLIST\n", target, snapshot.NextSequence, snapshot.DiscontinuitySequence)), nil
	}
	start, err := dynamicViewStart(snapshot, view)
	if err != nil {
		return nil, err
	}
	for _, segment := range snapshot.Segments {
		if segment.DurationTicks <= 0 || segment.DurationTicks > math.MaxInt64-media.TicksPerSecond || segment.Sequence > math.MaxInt32 {
			return nil, timeshift.ErrInvalid
		}
		if (segment.DurationTicks+media.TicksPerSecond/2)/media.TicksPerSecond > target {
			return nil, timeshift.ErrInvalid
		}
	}
	var output strings.Builder
	fmt.Fprintf(&output, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-DISCONTINUITY-SEQUENCE:%d\n", target, snapshot.Segments[0].Sequence, snapshot.DiscontinuitySequence)
	if start != nil {
		fmt.Fprintf(&output, "#EXT-X-START:TIME-OFFSET=%s,PRECISE=NO\n", dynamicTicksSeconds(*start))
	}
	for index, segment := range snapshot.Segments {
		if index > 0 && segment.Discontinuity {
			output.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		address := resource(segment.Sequence)
		if !validHLSManifestURL(address) {
			return nil, errInvalidHLSManifest
		}
		fmt.Fprintf(&output, "#EXTINF:%s,\n%s\n", dynamicTicksSeconds(segment.DurationTicks), address)
		if output.Len() > maxHLSManifestBytes {
			return nil, errHLSManifestLimit
		}
	}
	if snapshot.Ended {
		output.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(output.String()), nil
}

// A delayed track advertises only a contiguous, complete portion of the actual
// media snapshot. Waiting for every moving live-tail segment would prevent a
// negative subtitle offset from ever producing a usable playlist.
func (runtime *dynamicSubtitleRuntime) readyWindow(snapshot timeshift.WindowSnapshot, index int, offset int64) (timeshift.WindowSnapshot, error) {
	if len(snapshot.Segments) == 0 {
		if snapshot.Ended {
			return snapshot, nil
		}
		return timeshift.WindowSnapshot{}, subtitle.ErrLiveNotReady
	}
	first, end := -1, 0
	expired := false
	for position, segment := range snapshot.Segments {
		runtime.mu.Lock()
		clock, err := runtime.readyClockLocked(index, segment.Sequence, offset)
		runtime.mu.Unlock()
		if errors.Is(err, subtitle.ErrLiveWindowExpired) && first < 0 {
			expired = true
			continue
		}
		if errors.Is(err, subtitle.ErrLiveNotReady) || errors.Is(err, subtitle.ErrLiveEpoch) || errors.Is(err, subtitle.ErrLiveWindowExpired) || errors.Is(err, errDynamicSubtitleSource) {
			if first < 0 {
				return timeshift.WindowSnapshot{}, err
			}
			break
		}
		if err != nil {
			return timeshift.WindowSnapshot{}, err
		}
		if clock.Generation != segment.Generation || clock.EndTicks-clock.StartTicks != segment.DurationTicks {
			return timeshift.WindowSnapshot{}, subtitle.ErrLiveConflict
		}
		if first < 0 {
			first = position
		}
		end = position + 1
	}
	if first < 0 {
		if expired {
			return timeshift.WindowSnapshot{}, subtitle.ErrLiveWindowExpired
		}
		return timeshift.WindowSnapshot{}, subtitle.ErrLiveNotReady
	}
	result := snapshot
	result.Segments = snapshot.Segments[first:end]
	result.Ended = snapshot.Ended && end == len(snapshot.Segments)
	leading, trailing := result.Segments[0], result.Segments[len(result.Segments)-1]
	result.EarliestTicks, result.LiveStartTicks = leading.StartTicks, leading.StartTicks
	if trailing.StartTicks > math.MaxInt64-trailing.DurationTicks {
		return timeshift.WindowSnapshot{}, timeshift.ErrInvalid
	}
	result.LiveEdgeTicks = trailing.StartTicks + trailing.DurationTicks
	result.NextSequence = trailing.Sequence + 1
	result.DiscontinuitySequence = leading.DiscontinuitySequence
	if result.TargetDurationTicks <= 0 || result.TargetDurationTicks > math.MaxInt64/3 {
		return timeshift.WindowSnapshot{}, timeshift.ErrInvalid
	}
	if !result.Ended && result.LiveEdgeTicks-result.EarliestTicks < 3*result.TargetDurationTicks {
		return timeshift.WindowSnapshot{}, subtitle.ErrLiveNotReady
	}
	for position := len(result.Segments) - 1; position >= 0; position-- {
		if result.LiveEdgeTicks-result.Segments[position].StartTicks >= 3*result.TargetDurationTicks {
			result.LiveStartTicks = result.Segments[position].StartTicks
			break
		}
	}
	return result, nil
}

func (s *Server) serveDynamicSubtitle(w http.ResponseWriter, r *http.Request, session *dynamicStreamSession, snapshot timeshift.WindowSnapshot, slot int, sequence int64, playlist bool, token string, view dynamicPlaybackView) bool {
	track, ok := transcode.HLSSubtitleTrackAt(session.key.plan, slot)
	session.mu.Lock()
	runtime := session.subtitles
	session.mu.Unlock()
	if !ok || runtime == nil || !playlist && sequence < 0 {
		s.dynamicSubtitleError(w, r, subtitle.ErrLiveTrack)
		return false
	}
	select {
	case s.subtitleSlots <- struct{}{}:
		defer func() { <-s.subtitleSlots }()
	default:
		s.dynamicSubtitleError(w, r, subtitle.ErrLimitExceeded)
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), dynamicSubtitleWait)
	defer cancel()
	values, err := hlsValues(r)
	if err != nil {
		s.dynamicSubtitleError(w, r, err)
		return false
	}
	for {
		runtime.mu.Lock()
		changed := runtime.changed
		runtime.mu.Unlock()
		session.mu.Lock()
		mediaChanged, closed := session.changed, session.closed
		session.mu.Unlock()
		if closed {
			s.dynamicSubtitleError(w, r, timeshift.ErrClosed)
			return false
		}
		var body []byte
		var clock dynamicSegmentClock
		if playlist {
			var ready timeshift.WindowSnapshot
			ready, err = runtime.readyWindow(snapshot, track.StreamIndex, view.Subtitles.OffsetTicks)
			if err == nil {
				body, err = dynamicRenderedSubtitlePlaylist(ready, func(number uint64) string {
					return dynamicArtifactURLView(session, hlsSubtitleSegmentName(slot, int64(number)), token, view, false)
				}, view)
			}
		} else {
			var rendered subtitle.Result
			rendered, clock, err = runtime.render(track.StreamIndex, uint64(sequence), view.Subtitles.OffsetTicks)
			body = rendered.Data
		}
		if errors.Is(err, subtitle.ErrLiveNotReady) || errors.Is(err, subtitle.ErrLiveEpoch) || errors.Is(err, timeshift.ErrNotBuffered) {
			if r.Method == http.MethodHead {
				s.dynamicSubtitleError(w, r, subtitle.ErrLiveNotReady)
				return false
			}
			select {
			case <-ctx.Done():
				s.dynamicSubtitleError(w, r, subtitle.ErrLiveNotReady)
				return false
			case <-session.ctx.Done():
				s.dynamicSubtitleError(w, r, timeshift.ErrClosed)
				return false
			case <-changed:
			case <-mediaChanged:
			}
			if playlist {
				snapshot, err = s.dynamicStreams.store.Snapshot(ctx, timeshiftScope(session.scope), session.windowID)
				if err != nil {
					s.dynamicWindowError(w, r, err)
					return false
				}
			}
			err = nil
			continue
		}
		if err != nil {
			s.dynamicSubtitleError(w, r, err)
			return false
		}
		if _, _, err = s.findDynamicSession(r.WithContext(ctx), values); err != nil {
			s.dynamicWindowError(w, r, err)
			return false
		}
		if playlist {
			err = s.dynamicStreams.store.Advertise(ctx, timeshiftScope(session.scope), session.windowID, snapshot.Revision)
			if errors.Is(err, timeshift.ErrSnapshotChanged) {
				snapshot, err = s.dynamicStreams.store.Snapshot(ctx, timeshiftScope(session.scope), session.windowID)
				if err != nil {
					s.dynamicWindowError(w, r, err)
					return false
				}
				continue
			}
			if err == nil {
				err = s.pruneDynamicSubtitles(ctx, session, snapshot)
			}
			if err != nil {
				s.dynamicWindowError(w, r, err)
				return false
			}
			if _, _, err = s.findDynamicSession(r.WithContext(ctx), values); err != nil {
				s.dynamicWindowError(w, r, err)
				return false
			}
			writeDynamicManifest(w, r, body)
			return true
		}
		if clock.artifactID == "" {
			if err = s.pruneDynamicSubtitles(ctx, session, snapshot); err != nil {
				s.dynamicSubtitleError(w, r, err)
				return false
			}
			_, clock, err = runtime.render(track.StreamIndex, uint64(sequence), view.Subtitles.OffsetTicks)
			if err != nil {
				s.dynamicSubtitleError(w, r, err)
				return false
			}
		}
		if clock.artifactID == "" {
			s.dynamicSubtitleError(w, r, subtitle.ErrLiveWindowExpired)
			return false
		}
		if _, _, err = s.dynamicStreams.store.ResolveArtifact(ctx, timeshiftScope(session.scope), session.windowID, clock.artifactID); err != nil {
			s.dynamicWindowError(w, r, err)
			return false
		}
		if _, _, err = s.findDynamicSession(r.WithContext(ctx), values); err != nil {
			s.dynamicWindowError(w, r, err)
			return false
		}
		if ctx.Err() != nil {
			s.dynamicSubtitleError(w, r, subtitle.ErrLiveNotReady)
			return false
		}
		writer, err := newIdleResponseWriter(w, ctx, mediaWriteIdle)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		defer writer.finish()
		digest := sha256.Sum256(body)
		w.Header().Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
		w.Header().Set("Content-Type", "text/vtt")
		w.Header().Set("Cache-Control", "private, no-cache, no-transform")
		http.ServeContent(writer, r.WithContext(ctx), "subtitle.vtt", time.Time{}, bytes.NewReader(body))
		return writer.err == nil && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent || writer.status == http.StatusNotModified)
	}
}

func (s *Server) dynamicSubtitleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, subtitle.ErrLiveNotReady), errors.Is(err, subtitle.ErrLiveEpoch):
		w.Header().Set("Retry-After", "1")
		apiError(w, r, http.StatusServiceUnavailable, "subtitles_not_ready", "The requested subtitle interval is not complete yet.")
	case errors.Is(err, subtitle.ErrLiveWindowExpired):
		apiError(w, r, http.StatusGone, "subtitle_window_expired", "The requested subtitle interval is outside the retained window.")
	case errors.Is(err, subtitle.ErrLimitExceeded):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "subtitle_limit", "The bounded subtitle service is at capacity.")
	case errors.Is(err, subtitle.ErrLiveTrack):
		apiError(w, r, http.StatusNotFound, "not_found", "The subtitle track was not found.")
	case errors.Is(err, subtitle.ErrInvalidRange):
		apiError(w, r, http.StatusBadRequest, "invalid_subtitle_range", "Supply a supported subtitle interval and offset.")
	case errors.Is(err, timeshift.ErrClosed), errors.Is(err, timeshift.ErrNotFound), errors.Is(err, timeshift.ErrWindowExpired), errors.Is(err, timeshift.ErrNotBuffered), errors.Is(err, timeshift.ErrInvalid):
		s.dynamicWindowError(w, r, err)
	default:
		apiError(w, r, http.StatusServiceUnavailable, "subtitles_unavailable", "The subtitle source or its verified clock is unavailable.")
	}
}
