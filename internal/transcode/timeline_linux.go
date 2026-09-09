//go:build linux

package transcode

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxTimelineProbeLine  = 4096
	maxTimelineProbeBytes = 256 * 1024 * 1024
	maxTimelinePackets    = 8_000_000
	timelineProbeTimeout  = 2 * time.Minute
)

// Keyframes scans packet headers from a borrowed regular file. It never loads
// compressed media into memory, closes the caller's file, or changes its offset.
// Results are relative to format.start_time, preserving an initial audio lead-in.
// A non-key first video packet, unknown origin, negative normalized timestamps,
// nonmonotonic keyframe timestamps, and missing PTS are unsupported. Packet key
// flags identify demuxer seek points; they do not certify a closed GOP by parsing
// the codec bitstream. Ordinary B-frame presentation reordering remains valid.
// The format and selected stream must agree with the caller's probed duration.
func Keyframes(ctx context.Context, ffprobe string, input *os.File, streamIndex int, durationTicks int64) ([]int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if streamIndex < 0 || streamIndex > maxStreamIndex || durationTicks <= 0 || durationTicks > maxDurationTicks {
		return nil, ErrInvalidTimeline
	}
	if input == nil {
		return nil, ErrInvalidInput
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() {
		return nil, ErrInvalidInput
	}
	beforeStat, ok := before.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, ErrInvalidInput
	}
	if ffprobe == "" || strings.ContainsAny(ffprobe, "\x00\r\n") {
		return nil, ErrStart
	}
	executable, err := exec.LookPath(ffprobe)
	if err != nil || !filepath.IsAbs(executable) {
		return nil, ErrStart
	}
	processCtx, cancel := context.WithTimeout(ctx, timelineProbeTimeout)
	defer cancel()
	output := &keyframeWriter{streamIndex: streamIndex, duration: durationTicks, cancel: cancel}
	cmd := exec.CommandContext(processCtx, executable,
		"-v", "error", "-threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", inputFormats,
		"-select_streams", strconv.Itoa(streamIndex),
		"-show_entries", "format=start_time,duration:stream=index,codec_type,start_time,time_base:packet=stream_index,pts_time,flags:packet_side_data=",
		"-of", "compact=p=1:nk=0", "-i", "/proc/self/fd/3")
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{input}
	cmd.Stdout = output
	cmd.Stderr = &stderrTail{}
	cmd.Env = processEnvironment()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	var groupMu sync.Mutex
	retired := false
	cmd.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, ErrStart
	}
	// Retire the process group while WNOWAIT still pins the leader's PID.
	waitErr := waitWithoutReaping(cmd.Process.Pid)
	groupMu.Lock()
	if !errors.Is(waitErr, syscall.ECHILD) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	retired = true
	groupMu.Unlock()
	runErr := cmd.Wait()
	output.finish()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if output.err != nil {
		return nil, output.err
	}
	if err := processCtx.Err(); err != nil {
		return nil, err
	}
	if waitErr != nil || runErr != nil {
		return nil, ErrTimelineProbe
	}
	after, err := input.Stat()
	if err != nil {
		return nil, ErrInvalidInput
	}
	afterStat, ok := after.Sys().(*syscall.Stat_t)
	if !ok || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || beforeStat.Ctim != afterStat.Ctim {
		return nil, ErrInvalidInput
	}
	return output.keyframes()
}

// keyframeWriter retains one bounded line and keyframe timestamps only. Non-key
// packets update extrema, then are discarded. FFprobe emits packets before the
// stream/format sections, so normalization is deferred until the process exits.
type keyframeWriter struct {
	streamIndex              int
	duration                 int64
	cancel                   context.CancelFunc
	line                     [maxTimelineProbeLine]byte
	length, bytes, packets   int
	keys                     []int64
	firstPTS, minPTS, maxPTS int64
	formatStart              int64
	streamStart              int64
	hasFormat, hasStream     bool
	err                      error
}

func (w *keyframeWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if len(p) > maxTimelineProbeBytes-w.bytes {
		return 0, w.fail(ErrTimelineLimit)
	}
	w.bytes += len(p)
	for index, char := range p {
		if char == '\n' {
			if err := w.consumeLine(); err != nil {
				return index + 1, w.fail(err)
			}
			w.length = 0
			continue
		}
		if w.length == len(w.line) {
			return index, w.fail(ErrTimelineLimit)
		}
		w.line[w.length] = char
		w.length++
	}
	return len(p), nil
}

func (w *keyframeWriter) fail(err error) error {
	w.err = err
	if w.cancel != nil {
		w.cancel()
	}
	return err
}

func (w *keyframeWriter) finish() {
	if w.err == nil && w.length > 0 {
		if err := w.consumeLine(); err != nil {
			w.fail(err)
		}
	}
	w.length = 0
}

func (w *keyframeWriter) consumeLine() error {
	line := strings.TrimSuffix(string(w.line[:w.length]), "\r")
	if line == "" {
		return nil
	}
	// MPEG-TS also emits the selected stream nested in a program. The complete
	// stream section follows it; ignore the duplicate program projection.
	if strings.HasPrefix(line, "program|") {
		return nil
	}
	kind, rest, ok := strings.Cut(line, "|")
	if !ok {
		return ErrUnsupportedTimeline
	}
	fields := make(map[string]string, 5)
	for rest != "" {
		var field string
		field, rest, _ = strings.Cut(rest, "|")
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" {
			return ErrUnsupportedTimeline
		}
		if _, exists := fields[key]; exists {
			return ErrUnsupportedTimeline
		}
		fields[key] = value
	}
	switch kind {
	case "packet":
		index, err := strconv.Atoi(fields["stream_index"])
		pts, valid := timelineTimestamp(fields["pts_time"])
		flags := fields["flags"]
		if err != nil || index != w.streamIndex || !valid || flags == "" || len(flags) > 8 || strings.ContainsAny(flags, "DC") {
			return ErrUnsupportedTimeline
		}
		for _, flag := range flags {
			if flag != '_' && flag != 'K' {
				return ErrUnsupportedTimeline
			}
		}
		key := strings.ContainsRune(flags, 'K')
		if w.packets == 0 {
			if !key {
				return ErrUnsupportedTimeline
			}
			w.firstPTS, w.minPTS, w.maxPTS = pts, pts, pts
		} else {
			w.minPTS = min(w.minPTS, pts)
			w.maxPTS = max(w.maxPTS, pts)
		}
		w.packets++
		if w.packets > maxTimelinePackets {
			return ErrTimelineLimit
		}
		if key {
			if len(w.keys) > 0 && pts <= w.keys[len(w.keys)-1] {
				return ErrUnsupportedTimeline
			}
			if len(w.keys) == maxTimelineKeys {
				return ErrTimelineLimit
			}
			w.keys = append(w.keys, pts)
		}
	case "stream":
		index, err := strconv.Atoi(fields["index"])
		start, valid := timelineTimestamp(fields["start_time"])
		if w.hasStream || err != nil || index != w.streamIndex || fields["codec_type"] != "video" || !valid || !positiveTimeBase(fields["time_base"]) {
			return ErrUnsupportedTimeline
		}
		w.streamStart, w.hasStream = start, true
	case "format":
		start, validStart := timelineTimestamp(fields["start_time"])
		duration, validDuration := timelineTimestamp(fields["duration"])
		if w.hasFormat || !validStart || !validDuration || duration <= 0 || duration != w.duration || start > math.MaxInt64-duration {
			return ErrUnsupportedTimeline
		}
		w.formatStart, w.hasFormat = start, true
	default:
		return ErrUnsupportedTimeline
	}
	return nil
}

func (w *keyframeWriter) keyframes() ([]int64, error) {
	if w.err != nil {
		return nil, w.err
	}
	if !w.hasStream || !w.hasFormat || w.packets == 0 || len(w.keys) == 0 || w.firstPTS != w.streamStart || w.minPTS < w.firstPTS || w.minPTS < w.formatStart || w.maxPTS >= w.formatStart+w.duration {
		return nil, ErrUnsupportedTimeline
	}
	for index := range w.keys {
		w.keys[index] -= w.formatStart
	}
	return w.keys, nil
}

func positiveTimeBase(value string) bool {
	numerator, denominator, ok := strings.Cut(value, "/")
	n, errN := strconv.ParseInt(numerator, 10, 64)
	d, errD := strconv.ParseInt(denominator, 10, 64)
	return ok && errN == nil && errD == nil && n > 0 && d > 0
}

// FFprobe's decimal timestamps are parsed as integers, without float rounding
// or accepting an unknown origin as zero. At most seven decimal places fit the
// source tick domain exactly; FFprobe currently emits six places.
func timelineTimestamp(value string) (int64, bool) {
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = strings.TrimPrefix(value, "-")
	}
	whole, fraction, hasDot := strings.Cut(value, ".")
	if whole == "" || (hasDot && fraction == "") || len(fraction) > 7 || len(whole) > 12 {
		return 0, false
	}
	for _, part := range []string{whole, fraction} {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, false
			}
		}
	}
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}
	fraction += strings.Repeat("0", 7-len(fraction))
	part, _ := strconv.ParseInt(fraction, 10, 64)
	if seconds > (math.MaxInt64-part)/ticksPerSecond {
		return 0, false
	}
	ticks := seconds*ticksPerSecond + part
	if negative {
		ticks = -ticks
	}
	return ticks, true
}
