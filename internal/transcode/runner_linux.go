//go:build linux

package transcode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/moooyo/goby/internal/media"
)

const (
	maxProgressLine = 4096
	maxStderrTail   = 64 * 1024
	terminateGrace  = 2 * time.Second
)

// Run borrows a regular source file or an authorized stream pipe. Regular
// sources are opened in the child through procfs; stream pipes use pipe:3.
// Reopening the inherited descriptor preserves the source inode while giving
// FFmpeg an independent file offset. The caller owns the file and must keep it
// open until Run returns. The output directory must be empty and privately
// owned by the job manager. Calls to onProgress from process-output readers are
// serialized and must return promptly without blocking network or disk work.
func Run(ctx context.Context, executable, directory string, input *os.File, plan Plan, threads int, onProgress func(Progress)) (RunResult, error) {
	result := RunResult{ExitCode: -1}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	args, err := BuildArgs(plan, threads)
	if err != nil {
		return result, err
	}
	if input == nil {
		return result, ErrInvalidInput
	}
	info, err := validateSourceInput(input, plan)
	if err != nil {
		return result, ErrInvalidInput
	}
	var liveConfig liveRuntime
	if plan.SourceMode == "stream" {
		var available bool
		liveConfig, available = liveRuntimeFromContext(ctx)
		if !available {
			return result, ErrInvalidOptions
		}
		if liveConfig.inputs.Media != input || validateStreamInputs(liveConfig.inputs, plan) != nil {
			return result, ErrInvalidInput
		}
	}
	if !emptyOutputDirectory(directory) {
		return result, ErrInvalidDirectory
	}
	if executable == "" || strings.ContainsAny(executable, "\x00\r\n") {
		return result, ErrStart
	}
	if plan.SourceMode != "stream" {
		if err := PrepareSubtitleAssets(ctx, executable, directory, input, plan); err != nil {
			return result, err
		}
	}
	resolved, err := exec.LookPath(executable)
	if err != nil || !filepath.IsAbs(resolved) {
		return result, ErrStart
	}
	if plan.VideoCopySeekCandidate != "" {
		verification, err := media.VerifyVideoCopySeekCandidate(ctx, resolved, input, plan.VideoCopySeekCandidate, threads)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		// A copy plan cannot fall back to an unverified packet boundary. The
		// planner may choose encoding before a job exists; a failed fresh proof
		// must fail this immutable copy job before it publishes any bytes.
		if err != nil || !verification.Verified || !transcodeSourceUnchanged(input, info) {
			return result, ErrInvalidInput
		}
		args = buildProgressiveVideoArgsWithSeek(plan, threads, verification.InputSeekTicks)
	}
	if progressiveVideoSeekPreflightEnabled(plan) {
		verification, err := media.VerifyVideoSeekCandidate(ctx, resolved, input, plan.VideoSeekCandidate, threads)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil || !transcodeSourceUnchanged(input, info) {
			return result, ErrInvalidInput
		}
		if verification.Verified {
			args = buildProgressiveVideoArgsWithSeek(plan, threads, verification.InputSeekTicks)
		}
	}
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	report := onProgress
	var reportMu sync.Mutex
	var live *liveObserver
	if needsHLSClock(plan) {
		report = func(update Progress) {
			reportMu.Lock()
			defer reportMu.Unlock()
			if live != nil && update.HLSClock != nil {
				live.setClock(*update.HLSClock)
			}
			if onProgress != nil {
				onProgress(update)
			}
		}
	}
	progress := &progressWriter{callback: report, cancel: cancel, liveTimeline: plan.SourceMode == "stream"}
	stderr := &stderrTail{cancel: cancel}
	var progressive *progressiveObserver
	var hlsClock *hlsClockObserver
	if needsHLSClock(plan) {
		hlsClock, err = newHLSClockObserver(plan, report, cancel)
		if err != nil {
			return result, ErrStart
		}
		defer hlsClock.close()
	}
	if plan.OutputMode == "progressive" {
		progressive, err = newProgressiveObserver(directory, input, plan, onProgress, cancel)
		if err != nil {
			if errors.Is(err, ErrInvalidPlan) || errors.Is(err, ErrInvalidInput) {
				return result, err
			}
			return result, ErrInvalidDirectory
		}
		defer progressive.file.Close()
		progress.callback = progressive.report
	}
	if plan.SourceMode == "stream" {
		live, err = newLiveObserver(processCtx, directory, plan, liveConfig, cancel, report)
		if err != nil {
			return result, err
		}
		defer live.close()
	}
	cmd := exec.CommandContext(processCtx, resolved, args...)
	cmd.Dir = directory
	cmd.ExtraFiles = []*os.File{input}
	if progressive != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, progressive.file)
	}
	if hlsClock != nil {
		for _, pipe := range hlsClock.pipes {
			cmd.ExtraFiles = append(cmd.ExtraFiles, pipe.write)
		}
	}
	if live != nil {
		for _, pipe := range live.journals {
			cmd.ExtraFiles = append(cmd.ExtraFiles, pipe.write)
		}
		if hasBitmapSubtitleInput(plan) {
			cmd.ExtraFiles = append(cmd.ExtraFiles, liveConfig.inputs.Bitmap)
		}
		for _, pipe := range live.subtitles {
			cmd.ExtraFiles = append(cmd.ExtraFiles, pipe.write)
		}
	}
	cmd.Stdout, cmd.Stderr = progress, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = terminateGrace + time.Second
	cmd.Env = processEnvironment()
	var groupMu sync.Mutex
	var timer *time.Timer
	retired := false
	cmd.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		timer = time.AfterFunc(terminateGrace, func() {
			groupMu.Lock()
			defer groupMu.Unlock()
			if !retired {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		})
		return err
	}
	if err := cmd.Start(); err != nil {
		return result, ErrStart
	}
	if progressive != nil {
		progressive.start()
	}
	if hlsClock != nil {
		hlsClock.start()
	}
	if live != nil {
		live.start()
	}
	var publish func(bool) error
	var publicationErr error
	var publishStop, publishDone chan struct{}
	if plan.SegmentMode == "vod" {
		publisher := &vodPublisher{directory: directory, plan: plan}
		publish = publisher.publish
	} else if plan.HLS.SegmentType == "packed" {
		publisher := &packedHLSPublisher{directory: directory, plan: plan}
		publish = publisher.publish
	}
	if publish != nil {
		publishStop, publishDone = make(chan struct{}), make(chan struct{})
		go func() {
			defer close(publishDone)
			ticker := time.NewTicker(25 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-publishStop:
					return
				case <-ticker.C:
					if err := publish(false); err != nil {
						publicationErr = err
						cancel()
						return
					}
				}
			}
		}()
	}
	// Keep the exited leader waitable: its PID also identifies our process
	// group and must not be recycled until every group signal has been sent.
	// exec's context watcher can signal/close pipes, but only Cmd.Wait reaps.
	waitErr := waitWithoutReaping(cmd.Process.Pid)
	groupMu.Lock()
	if timer != nil {
		timer.Stop()
	}
	// WNOWAIT pins the leader's identity through group retirement. ECHILD
	// indicates that some external reaper violated our exclusive ownership;
	// in that case its old numeric PGID is no longer safe to signal.
	if !errors.Is(waitErr, syscall.ECHILD) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	retired = true
	groupMu.Unlock()
	err = cmd.Wait()
	var hlsClockErr error
	if hlsClock != nil {
		hlsClockErr = hlsClock.finish()
	}
	var liveErr error
	if live != nil {
		if err != nil || waitErr != nil || stderr.failed || hlsClockErr != nil {
			cancel()
		}
		liveErr = live.finish(err == nil && waitErr == nil && !stderr.failed && hlsClockErr == nil && ctx.Err() == nil)
	}
	unchanged := plan.SourceMode == "stream" || transcodeSourceUnchanged(input, info)
	var progressiveErr error
	if progressive != nil {
		progressiveErr = progressive.finish(err == nil && waitErr == nil && ctx.Err() == nil && !stderr.failed && unchanged)
	}
	if publish != nil {
		close(publishStop)
		<-publishDone
		if publicationErr == nil && err == nil && waitErr == nil && ctx.Err() == nil && !stderr.failed && unchanged {
			publicationErr = publish(true)
		}
	}
	result.StderrTail = stderr.String()
	if liveErr != nil {
		result.StderrTail += "\nlive publication: " + liveErr.Error()
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	progress.finish()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if !unchanged {
		return result, ErrInvalidInput
	}
	if progress.err != nil {
		return result, ErrProgress
	}
	if waitErr != nil || err != nil || stderr.failed || progressiveErr != nil || publicationErr != nil || hlsClockErr != nil || liveErr != nil {
		return result, ErrProcess
	}
	return result, nil
}

func transcodeSourceUnchanged(input *os.File, before os.FileInfo) bool {
	after, err := input.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return false
	}
	a, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	b, ok := after.Sys().(*syscall.Stat_t)
	return ok && a.Ctim == b.Ctim
}

func waitWithoutReaping(pid int) error {
	// Linux siginfo_t is 128 bytes on every supported Linux architecture.
	// We only need the syscall's completion and intentionally do not decode
	// its architecture-dependent child-status layout. uint64 keeps alignment.
	var info [16]uint64
	const pPID = 1
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, pPID, uintptr(pid), uintptr(unsafe.Pointer(&info[0])),
			syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func emptyOutputDirectory(directory string) bool {
	if !filepath.IsAbs(directory) || strings.ContainsAny(directory, "\x00\r\n") {
		return false
	}
	before, err := os.Lstat(directory)
	if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return false
	}
	dir, err := os.Open(directory)
	if err != nil {
		return false
	}
	defer dir.Close()
	after, err := dir.Stat()
	if err != nil || !os.SameFile(before, after) {
		return false
	}
	_, err = dir.Readdirnames(1)
	return errors.Is(err, io.EOF)
}

func processEnvironment() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		// Only locale, loader, temporary-directory and headless hardware
		// runtime settings reach a parser handling untrusted media. In
		// particular, server credentials and arbitrary report paths do not.
		switch name {
		case "PATH", "LANG", "LANGUAGE", "TZ", "TMPDIR", "TMP", "TEMP", "LD_LIBRARY_PATH",
			"LC_ALL", "LC_ADDRESS", "LC_COLLATE", "LC_CTYPE", "LC_IDENTIFICATION", "LC_MEASUREMENT",
			"LC_MESSAGES", "LC_MONETARY", "LC_NAME", "LC_NUMERIC", "LC_PAPER", "LC_TELEPHONE", "LC_TIME",
			"LIBVA_DRIVER_NAME", "LIBVA_DRIVERS_PATH", "LIBVA_MESSAGING_LEVEL",
			"ONEVPL_SEARCH_PATH", "MFX_HOME", "MFX_LOAD_PLUGINS",
			"CUDA_VISIBLE_DEVICES", "CUDA_DEVICE_ORDER", "CUDA_CACHE_DISABLE", "CUDA_CACHE_MAXSIZE", "CUDA_CACHE_PATH",
			"NVIDIA_VISIBLE_DEVICES", "NVIDIA_DRIVER_CAPABILITIES", "XDG_RUNTIME_DIR", "XDG_CACHE_HOME":
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, "AV_LOG_FORCE_NOCOLOR=1")
}

// progressWriter bounds each line, not the lifetime byte count. A long movie
// may emit progress for many hours without accumulating memory or being killed
// merely because its regular progress updates exceeded a total output budget.
type progressWriter struct {
	line     [maxProgressLine]byte
	length   int
	current  Progress
	callback func(Progress)
	cancel   context.CancelFunc
	err      error
	// Live clocks accumulate for the source lifetime, independently of the
	// finite-media duration limit. Tick conversion must still fit in int64.
	liveTimeline bool
}

func (w *progressWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	for i, char := range p {
		if char == '\n' {
			if !w.consumeLine() {
				w.fail()
				return i + 1, w.err
			}
			w.length = 0
			continue
		}
		if w.length == len(w.line) {
			w.fail()
			return i, w.err
		}
		w.line[w.length] = char
		w.length++
	}
	return len(p), nil
}

func (w *progressWriter) fail() {
	w.err = ErrProgress
	if w.cancel != nil {
		w.cancel()
	}
}

func (w *progressWriter) finish() {
	if w.err == nil && w.length > 0 && !w.consumeLine() {
		w.fail()
	}
	w.length = 0
}

func (w *progressWriter) consumeLine() bool {
	line := strings.TrimSuffix(string(w.line[:w.length]), "\r")
	if line == "" {
		return true
	}
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	switch key {
	case "out_time_us":
		if value == "N/A" {
			return true
		}
		microseconds, err := strconv.ParseInt(value, 10, 64)
		limit := int64(maxDurationTicks/10 + 10_000_000)
		if w.liveTimeline {
			limit = math.MaxInt64 / 10
		}
		if err != nil || microseconds > limit {
			return false
		}
		if microseconds < 0 {
			microseconds = 0
		}
		w.current.OutputTicks = microseconds * 10
	case "total_size":
		if value == "N/A" {
			return true
		}
		bytes, err := strconv.ParseInt(value, 10, 64)
		if err != nil || bytes < 0 {
			return false
		}
		w.current.Bytes = bytes
	case "progress":
		if value != "continue" && value != "end" {
			return false
		}
		w.current.Ended = value == "end"
		if w.callback != nil {
			w.callback(w.current)
		}
	}
	return true
}

type stderrTail struct {
	buffer []byte
	failed bool
	cancel context.CancelFunc
}

func (w *stderrTail) Write(p []byte) (int, error) {
	n := len(p)
	// FFmpeg's segment muxer can overwrite a trailer I/O error with a later
	// successful list operation. Keep a durable error signal independently
	// of its eventual exit code and of the bounded diagnostic tail.
	prefix := w.buffer
	if len(prefix) > 128 {
		prefix = prefix[len(prefix)-128:]
	}
	scan := append(append([]byte(nil), prefix...), p...)
	if bytes.Contains(scan, []byte("[fatal]")) ||
		bytes.Contains(scan, []byte("No space left on device")) || bytes.Contains(scan, []byte("Input/output error")) ||
		bytes.Contains(scan, []byte("Error writing trailer")) || bytes.Contains(scan, []byte("Error muxing")) ||
		bytes.Contains(scan, []byte("Error writing packet")) || bytes.Contains(scan, []byte("Error closing file")) {
		w.failed = true
		if w.cancel != nil {
			w.cancel()
		}
	}
	if n >= maxStderrTail {
		w.buffer = append(w.buffer[:0], p[n-maxStderrTail:]...)
		return n, nil
	}
	if len(w.buffer)+n > maxStderrTail {
		keep := maxStderrTail - n
		copy(w.buffer, w.buffer[len(w.buffer)-keep:])
		w.buffer = w.buffer[:keep]
	}
	w.buffer = append(w.buffer, p...)
	return n, nil
}

func (w *stderrTail) String() string { return string(w.buffer) }

// vodPublisher exposes only completed local segments. FFmpeg atomically
// publishes its private list just before closing the corresponding segment;
// seeing the next file proves that the previous descriptor has been closed.
// The final segment is eligible only after the process has exited and drained.
type vodPublisher struct {
	directory string
	plan      Plan
	published int
	lastList  string
	err       error
}

func (p *vodPublisher) publish(finished bool) error {
	dir, err := os.Open(p.directory)
	if err != nil {
		return err
	}
	defer dir.Close()
	file, err := cacheOpenRegular(dir, "segment-list.m3u8", syscall.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) && !finished {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxPlaylistBytes+1))
	_ = file.Close()
	if err != nil {
		return err
	}
	if len(data) > MaxPlaylistBytes {
		return ErrInvalidPlaylist
	}
	if !finished && !bytes.Contains(data, []byte("#EXTINF:")) {
		return nil
	}
	list, err := parsePrivateSegmentList(data, p.plan.SegmentStartNumber)
	if err != nil {
		return err
	}
	cuts, _ := planSegmentTimes(p.plan)
	expected := len(cuts) + 1
	if len(list.Segments) > expected || finished && (!list.Ended || len(list.Segments) != expected) {
		return ErrInvalidPlaylist
	}
	ready := 0
	for i, segment := range list.Segments {
		if i < p.published {
			ready++
			continue
		}
		if !finished {
			next := fmt.Sprintf("segment-%06d.ts.tmp", segment.Number+1)
			nextFile, nextErr := cacheOpenRegular(dir, next, syscall.O_RDONLY, 0)
			if errors.Is(nextErr, os.ErrNotExist) {
				break
			}
			if nextErr != nil {
				return nextErr
			}
			_ = nextFile.Close()
		}
		if err := validateTransportSegment(dir, segment.Name+".tmp"); err != nil {
			return err
		}
		if err := syscall.Renameat(int(dir.Fd()), segment.Name+".tmp", int(dir.Fd()), segment.Name); err != nil {
			return err
		}
		p.published++
		ready++
	}
	if ready == 0 {
		return nil
	}
	var output strings.Builder
	fmt.Fprintf(&output, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-MEDIA-SEQUENCE:%d\n#EXT-X-TARGETDURATION:%d\n", p.plan.SegmentStartNumber, list.TargetDuration)
	complete := finished && ready == expected && list.Ended
	if complete {
		output.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n")
	} else {
		output.WriteString("#EXT-X-PLAYLIST-TYPE:EVENT\n")
	}
	for i, segment := range list.Segments[:ready] {
		if i > 0 {
			output.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&output, "#EXTINF:%s,\n%s\n", tickSeconds(segment.DurationTicks), segment.Name)
	}
	if complete {
		output.WriteString("#EXT-X-ENDLIST\n")
	}
	if output.Len() > MaxPlaylistBytes {
		return ErrInvalidPlaylist
	}
	if output.String() == p.lastList {
		return nil
	}
	publication, err := cacheOpenRegular(dir, "main.m3u8.publish.tmp", syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := io.WriteString(publication, output.String())
	if writeErr == nil {
		writeErr = publication.Sync()
	}
	closeErr := publication.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if err := syscall.Renameat(int(dir.Fd()), "main.m3u8.publish.tmp", int(dir.Fd()), "main.m3u8"); err != nil {
		return err
	}
	p.lastList = output.String()
	return nil
}

func parsePrivateSegmentList(data []byte, sequence int) (MediaPlaylist, error) {
	if len(data) > MaxPlaylistBytes {
		return MediaPlaylist{}, ErrInvalidPlaylist
	}
	var normalized strings.Builder
	seenCache := false
	for _, line := range strings.Split(string(data), "\n") {
		if line == "#EXT-X-ALLOW-CACHE:YES" {
			if seenCache {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
			seenCache = true
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:") {
			value := strings.TrimPrefix(line, "#EXT-X-MEDIA-SEQUENCE:")
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || strconv.FormatInt(number, 10) != value {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
			line = "#EXT-X-MEDIA-SEQUENCE:" + strconv.Itoa(sequence)
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			if !strings.HasSuffix(line, ".ts.tmp") {
				return MediaPlaylist{}, ErrInvalidPlaylist
			}
			line = strings.TrimSuffix(line, ".tmp")
		}
		normalized.WriteString(line)
		normalized.WriteByte('\n')
	}
	return ParseMediaPlaylist([]byte(normalized.String()))
}

func validateTransportSegment(dir *os.File, name string) error {
	file, err := cacheOpenRegular(dir, name, syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() < 188 || info.Size()%188 != 0 {
		return ErrProcess
	}
	var packet [188]byte
	for _, offset := range []int64{0, info.Size() - 188} {
		if _, err := file.ReadAt(packet[:], offset); err != nil {
			return err
		}
		if packet[0] != 0x47 || packet[1]&0x80 != 0 || packet[3]&0x30 == 0 {
			return ErrProcess
		}
	}
	return nil
}
