package media

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"
)

// ProbeStreamPrefix discovers decoder and container facts from a bounded
// private snapshot of an authorized stream. A prefix cannot prove complete
// duration, packet/sample coverage, total size, or a safe seek index. Unlike
// ProbeFile, this entry point never scans a truncated presentation for those
// facts or treats missing tail packets as corruption of the complete source.
func (p Prober) ProbeStreamPrefix(ctx context.Context, file *os.File) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if runtime.GOOS != "linux" || file == nil {
		return Info{}, fmt.Errorf("stream prefix probing requires a Linux regular file")
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 1<<20 {
		return Info{}, fmt.Errorf("invalid bounded stream prefix")
	}
	timeout := p.Timeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	work, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	executable := p.FFprobePath
	if executable == "" {
		executable = "ffprobe"
	}
	output, err := runLimitedFiles(work, timeout, maxProbeOutput, executable, []*os.File{file},
		"-v", "error", "-show_format", "-show_streams", "-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3")
	if err != nil {
		return Info{}, fmt.Errorf("probe stream prefix: %w", err)
	}
	info, err := parseProbe(output)
	if err != nil {
		return Info{}, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || FileChangeTime(before) != FileChangeTime(after) {
		return Info{}, fmt.Errorf("stream prefix changed during probe")
	}
	info.DurationTicks, info.Size, info.FileChangeTimeNs = 0, 0, 0
	info.AudioDurationExact, info.AudioDurationReason = false, "streaming_input"
	info.Chapters, info.VideoSeekIndexes, info.EmbeddedMusic = nil, nil, nil
	for index := range info.Streams {
		info.Streams[index].AudioTiming = nil
	}
	return info, nil
}
