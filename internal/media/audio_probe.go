package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxAudioProbeOutput int64 = 512 * 1024 * 1024

// Frames expose decoder-applied priming, edit and discard semantics; packet
// positions link those effective samples to the immutable copy boundaries.
// Output is decoded one record at a time and never accumulated in memory.
func runAudioTimingProbe(ctx context.Context, timeout time.Duration, executable string, file *os.File, info Info) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	processContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(processContext, executable,
		"-v", "error", "-threads", "1", "-select_streams", "a", "-show_packets", "-show_frames",
		"-show_entries", "packet=stream_index,pts,duration,pos:packet_side_data=side_data_type,skip_samples,discard_padding:"+
			"frame=stream_index,pts,best_effort_timestamp,nb_samples,pkt_pos,sample_rate",
		"-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-i", "/proc/self/fd/3")
	command.ExtraFiles = []*os.File{file}
	command.Env = mediaProbeEnvironment()
	command.WaitDelay = time.Second
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Info{}, fmt.Errorf("open audio scan output: %w", err)
	}
	retired, err := startMediaProcess(command)
	if err != nil {
		return Info{}, fmt.Errorf("start audio scan: %w", err)
	}
	bounded := &io.LimitedReader{R: stdout, N: maxAudioProbeOutput + 1}
	accurate, parseErr := parseAudioTiming(bounded, info)
	interrupted := processContext.Err()
	if parseErr != nil {
		cancel()
	}
	waitErr := errors.Join(<-retired, command.Wait())
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	if bounded.N <= 0 || stderr.exceeded {
		return Info{}, &audioTimingUnproven{Reason: "scan_output_limit"}
	}
	if interrupted != nil {
		return Info{}, interrupted
	}
	if parseErr == nil && processContext.Err() != nil {
		return Info{}, processContext.Err()
	}
	// A nonzero decoder exit is an actual failure; cancellation caused by an
	// already classified unproven scan must not hide that useful classification.
	var exitErr *exec.ExitError
	if waitErr != nil && (parseErr == nil || errors.As(waitErr, &exitErr) && exitErr.ExitCode() >= 0) {
		detail := strings.TrimSpace(stderr.buffer.String())
		if len(detail) > maxErrorDetail {
			detail = detail[:maxErrorDetail]
		}
		return Info{}, fmt.Errorf("execute audio scan: %w: %s", waitErr, detail)
	}
	if parseErr != nil {
		return Info{}, parseErr
	}
	if stderr.buffer.Len() != 0 {
		return Info{}, &audioTimingUnproven{Reason: "decoder_error"}
	}
	return accurate, nil
}
