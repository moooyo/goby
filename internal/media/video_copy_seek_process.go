package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const maxVideoCopySeekProofBytes = 64 * 1024

// VerifyVideoCopySeekCandidate proves the actual copied packet boundary on the
// borrowed source and the actual executable immediately before production.
// Unlike decoded seek verification, an unsuccessful result cannot authorize a
// linear-copy fallback: the caller must fail or create a separate encoding plan.
func VerifyVideoCopySeekCandidate(ctx context.Context, executable string, file *os.File, encoded string, threads int) (verification VideoSeekVerification, resultErr error) {
	if err := ctx.Err(); err != nil {
		return verification, err
	}
	if file == nil {
		return verification, fmt.Errorf("video copy seek source is nil")
	}
	before, err := file.Stat()
	if err != nil {
		return verification, err
	}
	if !before.Mode().IsRegular() {
		return verification, fmt.Errorf("video copy seek source is not regular")
	}
	defer func() {
		if err := videoSeekCheckSource(file, before); err != nil {
			verification, resultErr = VideoSeekVerification{}, err
		}
		if err := ctx.Err(); err != nil {
			verification, resultErr = VideoSeekVerification{}, err
		}
	}()
	candidate, err := ValidateVideoCopySeekCandidate(encoded)
	if err != nil || runtime.GOOS != "linux" {
		return verification, nil
	}
	args, err := BuildVideoCopySeekCommandArgs(encoded, threads)
	if err != nil {
		return verification, nil
	}
	identity, err := VideoSeekSourceIdentity(before)
	if err != nil || identity != candidate.Index.SourceIdentity {
		return verification, nil
	}
	proofContext, cancel := context.WithTimeout(ctx, videoSeekProofTimeout)
	defer cancel()
	resolved, toolIdentity, err := VideoSeekToolIdentity(proofContext, executable)
	if err != nil || toolIdentity != candidate.Index.ToolIdentity {
		return verification, nil
	}
	if err := runVideoCopySeekProof(proofContext, resolved, file, args, candidate); err != nil {
		return verification, nil
	}
	if err := videoSeekCheckSource(file, before); err != nil {
		return verification, err
	}
	afterPath, afterTool, err := VideoSeekToolIdentity(proofContext, executable)
	if err != nil || afterPath != resolved || afterTool != toolIdentity {
		return verification, nil
	}
	return VideoSeekVerification{Verified: true, InputSeekTicks: candidate.RequestedStartTicks}, nil
}

func runVideoCopySeekProof(ctx context.Context, executable string, file *os.File, args []string, candidate VideoCopySeekCandidate) error {
	processContext, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &limitedOutput{limit: maxVideoCopySeekProofBytes, cancel: cancel}
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command := exec.CommandContext(processContext, executable, args...)
	command.ExtraFiles = []*os.File{file}
	command.Env = mediaProbeEnvironment()
	command.Stdout, command.Stderr = stdout, stderr
	command.WaitDelay = time.Second
	retired, err := startMediaProcess(command)
	if err != nil {
		return err
	}
	waitErr := errors.Join(<-retired, command.Wait())
	if err := ctx.Err(); err != nil {
		return err
	}
	if stdout.exceeded || stderr.exceeded {
		return ErrOutputLimit
	}
	if waitErr != nil {
		return fmt.Errorf("execute video copy seek proof: %w", waitErr)
	}
	for _, severity := range []string{"[error]", "[fatal]", "[panic]"} {
		if strings.Contains(stderr.buffer.String(), severity) {
			return fmt.Errorf("video copy seek reported an error")
		}
	}
	return parseVideoCopySeekProof(&stdout.buffer, candidate)
}

func parseVideoCopySeekProof(input io.Reader, candidate VideoCopySeekCandidate) error {
	if input == nil {
		return fmt.Errorf("video copy seek proof is nil")
	}
	if err := validateVideoCopySeekCandidate(candidate); err != nil {
		return err
	}
	streams := make([]videoSeekHashStream, 2)
	for number := range streams {
		streams[number].seen = make(map[string]bool)
	}
	headers := make(map[string]bool)
	bounded := &io.LimitedReader{R: input, N: maxVideoCopySeekProofBytes + 1}
	reader := bufio.NewReaderSize(bounded, maxVideoSeekLine)
	started := false
	records := [2]*videoSeekHashRecord{}
	for {
		raw, err := reader.ReadSlice('\n')
		if err == io.EOF && len(raw) == 0 {
			break
		}
		if err != nil || len(raw) > maxVideoSeekLine || bounded.N <= 0 {
			return fmt.Errorf("video copy seek proof is truncated or exceeds its byte budget")
		}
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if started {
				return fmt.Errorf("video copy seek proof changed its headers")
			}
			if err := parseVideoSeekHashHeader(line, streams, headers); err != nil {
				return err
			}
			continue
		}
		if !started {
			if !headers["format"] || !headers["version"] || !headers["hash"] {
				return fmt.Errorf("video copy seek proof has incomplete headers")
			}
			for _, stream := range streams {
				if len(stream.seen) != 4 || stream.timeBase == nil || stream.media != "video" || stream.codec != "h264" ||
					stream.width != candidate.Index.Width || stream.height != candidate.Index.Height ||
					stream.timeBase.Cmp(videoSeekTimeBase(candidate.Index.TimeBaseNumerator, candidate.Index.TimeBaseDenominator)) != 0 {
					return fmt.Errorf("video copy seek packet metadata differs from the source")
				}
			}
			started = true
		}
		number, record, err := parseVideoSeekHashRecord(line)
		if err != nil || number >= len(records) || records[number] != nil {
			return fmt.Errorf("video copy seek proof has unexpected packet records")
		}
		// The production output subtracts the requested source position. Zero
		// native timestamps prove that neither decode nor presentation pre-roll
		// survives that exact argument sequence; no rounded tick comparison is
		// used here. The scoped source index has strictly increasing native DTS,
		// so the two branches cannot match different packets at this boundary.
		duration, durationErr := strconv.ParseInt(strings.TrimSpace(strings.Split(line, ",")[3]), 10, 64)
		if durationErr != nil || duration <= 0 || record.pts != 0 || record.dts != 0 || !videoSeekSupportedPacketSideData(record.sideData) {
			return fmt.Errorf("video copy seek first packet does not start at the requested position")
		}
		records[number] = &record
	}
	if !started || records[0] == nil || records[1] == nil || records[1].hash != candidate.Index.Entries[0].CodedSHA256 {
		return fmt.Errorf("video copy seek first packet has no matching IDR evidence")
	}
	return nil
}
