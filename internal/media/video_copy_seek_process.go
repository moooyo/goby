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
	if candidate.Audio != nil {
		audioArgs, err := BuildVideoCopySeekAudioCommandArgs(encoded, threads)
		if err != nil {
			return verification, nil
		}
		if err := runVideoCopySeekHashProof(proofContext, resolved, file, audioArgs, func(input io.Reader) error {
			return parseVideoCopySeekAudioProof(input, candidate)
		}); err != nil {
			return verification, nil
		}
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
	return runVideoCopySeekHashProof(ctx, executable, file, args, func(input io.Reader) error {
		return parseVideoCopySeekProof(input, candidate)
	})
}

func runVideoCopySeekHashProof(ctx context.Context, executable string, file *os.File, args []string, parse func(io.Reader) error) error {
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
	return parse(&stdout.buffer)
}

func parseVideoCopySeekProof(input io.Reader, candidate VideoCopySeekCandidate) error {
	if input == nil {
		return fmt.Errorf("video copy seek proof is nil")
	}
	if err := validateVideoCopySeekCandidate(candidate); err != nil {
		return err
	}
	videoStreams := 2
	if VideoSeekCodec(candidate.Index) == "hevc" {
		videoStreams = 3
	}
	streamCount := videoStreams
	streams := make([]videoSeekHashStream, streamCount)
	for number := range streams {
		streams[number].seen = make(map[string]bool)
	}
	headers := make(map[string]bool)
	bounded := &io.LimitedReader{R: input, N: maxVideoCopySeekProofBytes + 1}
	reader := bufio.NewReaderSize(bounded, maxVideoSeekLine)
	started := false
	records := make([]*videoSeekHashRecord, streamCount)
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
			for number, stream := range streams {
				codec := VideoSeekCodec(candidate.Index)
				if codec == "av1" && number == 1 || codec == "hevc" && number == 2 {
					codec = "rawvideo"
				}
				if len(stream.seen) != 4 || stream.timeBase == nil || stream.media != "video" || stream.codec != codec ||
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
		// Output-side trimming removes the requested source position; an
		// explicit source-clock contract restores that offset at the muxer.
		// Exact native timestamps prove that neither decode nor presentation
		// preroll survives either sequence. The scoped source index has strictly
		// increasing DTS, so branches cannot match different boundary packets.
		duration, durationErr := strconv.ParseInt(strings.TrimSpace(strings.Split(line, ",")[3]), 10, 64)
		expectedTimestamp, timestampErr := videoCopySeekOutputTimestamp(candidate, streams[number].timeBase)
		if durationErr != nil || duration <= 0 || timestampErr != nil || record.pts != expectedTimestamp || record.dts != expectedTimestamp || !videoSeekSupportedPacketSideData(record.sideData) {
			return fmt.Errorf("video copy seek first packet does not start at the requested position")
		}
		records[number] = &record
	}
	for _, record := range records {
		if record == nil {
			return fmt.Errorf("video copy seek proof has incomplete packet evidence")
		}
	}
	point := candidate.Index.Entries[0]
	if !started || point.PacketSHA256 != "" && records[0].hash != point.PacketSHA256 {
		return fmt.Errorf("video copy seek first packet differs from the indexed packet")
	}
	if VideoSeekCodec(candidate.Index) == "av1" {
		if records[1].hash != point.DecodedSHA256 || records[1].size != candidate.Index.DecodedFrameBytes {
			return fmt.Errorf("video copy seek AV1 decoder did not restart at the indexed picture")
		}
	} else if records[1].hash != point.CodedSHA256 {
		return fmt.Errorf("video copy seek first packet has no matching IDR evidence")
	}
	if VideoSeekCodec(candidate.Index) == "hevc" && (records[2].hash != point.DecodedSHA256 || records[2].size != candidate.Index.DecodedFrameBytes) {
		return fmt.Errorf("video copy seek HEVC decoder did not restart at the indexed picture")
	}
	return nil
}
