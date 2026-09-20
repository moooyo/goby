package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const mediaEditMaxPacketJSONBytes int64 = 32 << 30

func mediaEditLimitedTool(executable string, maxFileBytes int64) (string, []string, error) {
	limiter, err := mediaEditResourceLimiter()
	if err != nil {
		return "", nil, err
	}
	limit := strconv.FormatInt(maxFileBytes, 10)
	arguments := []string{"--fsize=" + limit + ":" + limit, "--as=4294967296:4294967296", "--nofile=64:64", "--", executable}
	return limiter, arguments, nil
}

func runMediaEditRemux(ctx context.Context, executable string, input, candidate *os.File, maxBytes int64, args []string) error {
	limiter, arguments, err := mediaEditLimitedTool(executable, maxBytes)
	if err != nil {
		return err
	}
	arguments = append(arguments, args...)
	output, err := runLimitedFilesOutput(ctx, MaxSubtitleRemovalTimeout, 4096, limiter, []*os.File{input, candidate}, arguments...)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrOutputLimit) || mediaEditProcessHitFileLimit(err) {
			return fmt.Errorf("%w: %w", ErrSubtitleRemovalBudget, err)
		}
		if info, statErr := candidate.Stat(); statErr == nil && info.Size() >= maxBytes {
			return fmt.Errorf("%w: candidate reached its hard byte limit", ErrSubtitleRemovalBudget)
		}
		return fmt.Errorf("subtitle removal remux: %w", err)
	}
	if len(output.stderr) != 0 || len(output.stdout) != 0 {
		return fmt.Errorf("%w: remux did not finish with clean diagnostics", ErrSubtitleRemovalUnsupported)
	}
	return nil
}

func probeMediaEditPackets(ctx context.Context, executable string, file *os.File, timeBases map[int]*big.Rat) (map[int]mediaEditPacketDigest, error) {
	processContext, cancel := context.WithCancel(ctx)
	defer cancel()
	limiter, args, err := mediaEditLimitedTool(executable, 0)
	if err != nil {
		return nil, err
	}
	args = append(args,
		"-v", "error", "-max_alloc", "268435456", "-show_packets", "-show_data_hash", "sha256",
		"-show_entries", "packet=stream_index,pts,dts,duration,size,flags,data_hash:packet_side_data",
		"-of", "json", "-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2", "-i", "/proc/self/fd/3")
	command := exec.CommandContext(processContext, limiter, args...)
	command.ExtraFiles = []*os.File{file}
	command.Env = mediaProbeEnvironment()
	command.WaitDelay = time.Second
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	retired, err := startMediaProcess(command)
	if err != nil {
		_ = stdout.Close()
		return nil, err
	}
	bounded := &io.LimitedReader{R: stdout, N: mediaEditMaxPacketJSONBytes + 1}
	packets, parseErr := parseMediaEditPackets(bounded, timeBases, mediaEditMaxPackets)
	if bounded.N <= 0 {
		parseErr = ErrSubtitleRemovalBudget
	}
	if parseErr != nil {
		cancel()
	}
	waitErr := errors.Join(<-retired, command.Wait())
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stderr.exceeded {
		return nil, ErrSubtitleRemovalBudget
	}
	if parseErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrSubtitleRemovalUnsupported, parseErr)
	}
	if waitErr != nil {
		return nil, fmt.Errorf("subtitle removal packet probe failed: %w", waitErr)
	}
	if len(stderr.buffer.Bytes()) != 0 {
		return nil, fmt.Errorf("%w: packet probe did not finish with clean diagnostics", ErrSubtitleRemovalUnsupported)
	}
	return packets, nil
}
