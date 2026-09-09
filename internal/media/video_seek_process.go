package media

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	videoSeekAnalysisTimeout = 2 * time.Minute
	videoSeekProofTimeout    = 5 * time.Second
	maxVideoSeekToolBytes    = 256 * 1024 * 1024
	maxVideoSeekProofPoints  = 4
)

// VideoSeekToolIdentity binds a resolved executable to its bytes, filesystem
// identity, version output, and loader search path. The caller must recheck it
// after analysis or verification before relying on any produced evidence.
func VideoSeekToolIdentity(ctx context.Context, executable string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(executable) == "" || strings.ContainsRune(executable, '\x00') {
		return "", "", fmt.Errorf("video seek executable is unavailable")
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return "", "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", "", err
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", "", err
	}
	file, err := openLocalMedia(resolved)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		return "", "", err
	}
	stamp, err := VideoSeekSourceIdentity(before)
	if err != nil || before.Size() <= 0 || before.Size() > maxVideoSeekToolBytes {
		return "", "", fmt.Errorf("video seek executable identity is unavailable")
	}
	hash := sha256.New()
	buffer := make([]byte, 64*1024)
	remaining := int64(maxVideoSeekToolBytes + 1)
	for {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		n, readErr := file.Read(buffer)
		remaining -= int64(n)
		if remaining <= 0 {
			return "", "", fmt.Errorf("video seek executable exceeds its byte budget")
		}
		_, _ = hash.Write(buffer[:n])
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", "", readErr
		}
	}
	// An inherited descriptor also selects the sanitized media environment.
	version, err := runLimitedFiles(ctx, videoSeekProofTimeout, 64*1024, resolved, []*os.File{file}, "-version")
	if err != nil {
		return "", "", err
	}
	after, err := file.Stat()
	if err != nil {
		return "", "", err
	}
	afterStamp, err := VideoSeekSourceIdentity(after)
	pathStat, pathErr := os.Stat(resolved)
	if err != nil || afterStamp != stamp || pathErr != nil || !os.SameFile(before, pathStat) {
		return "", "", fmt.Errorf("video seek executable changed during identification")
	}
	_, _ = fmt.Fprintf(hash, "\x00video-seek-tool-v1\x00%s\x00%s\x00%s\x00", resolved, stamp, os.Getenv("LD_LIBRARY_PATH"))
	_, _ = hash.Write(version)
	return resolved, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// AnalyzeVideoSeekIndexes performs optional library analysis. Unsupported or
// budget-exhausted evidence leaves a playable source without indexes. Caller
// cancellation and mutation of the borrowed source remain hard failures.
func AnalyzeVideoSeekIndexes(ctx context.Context, executable string, file *os.File, info Info) (indexes []VideoSeekIndex, resultErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if file == nil {
		return nil, fmt.Errorf("video seek source is nil")
	}
	before, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("video seek source is not regular")
	}
	defer func() {
		if err := videoSeekCheckSource(file, before); err != nil {
			indexes, resultErr = nil, err
		}
		if err := ctx.Err(); err != nil {
			indexes, resultErr = nil, err
		}
	}()
	if runtime.GOOS != "linux" || !info.FormatStartKnown || info.DurationTicks <= 0 || info.DurationTicks > MaxVideoSeekDurationTicks || strings.TrimSpace(executable) == "" {
		return nil, nil
	}
	streams := make([]Stream, 0)
	for _, stream := range info.Streams {
		_, supported := videoSeekFrameBytes(stream.Width, stream.Height, stream.PixelFormat)
		if stream.CodecType == "video" && stream.Codec == "h264" && !stream.IsAttachedPicture &&
			supported && stream.BitDepth == videoSeekPixelDepth(stream.PixelFormat) {
			streams = append(streams, stream)
		}
	}
	if len(streams) == 0 {
		return nil, nil
	}
	sort.SliceStable(streams, func(i, j int) bool { return streams[i].IsDefault && !streams[j].IsDefault })
	analysisContext, cancel := context.WithTimeout(ctx, videoSeekAnalysisTimeout)
	defer cancel()
	sourceIdentity, err := VideoSeekSourceIdentity(before)
	if err != nil {
		return nil, nil
	}
	resolved, toolIdentity, err := VideoSeekToolIdentity(analysisContext, executable)
	if err != nil {
		return nil, nil
	}
	remainingEntries := MaxVideoSeekEntries
	for _, stream := range streams {
		if analysisContext.Err() != nil || remainingEntries <= 0 {
			break
		}
		if len(stream.TimeBase) == 0 || len(stream.TimeBase) > 64 || len(stream.PixelFormat) > 64 {
			continue
		}
		timeBase, err := parseVideoSeekTimeBase(stream.TimeBase)
		if err != nil {
			continue
		}
		base := VideoSeekIndex{Version: VideoSeekIndexVersion, StreamIndex: stream.Index,
			FormatStartTicks: info.FormatStartTicks, DurationTicks: info.DurationTicks,
			TimeBaseNumerator: timeBase.Num().Int64(), TimeBaseDenominator: timeBase.Denom().Int64(),
			SourceIdentity: sourceIdentity, ToolIdentity: toolIdentity, Width: stream.Width, Height: stream.Height, PixelFormat: stream.PixelFormat}
		args, err := BuildVideoSeekCommandArgs(stream.Index, nil, 1)
		if err != nil {
			continue
		}
		index, err := runVideoSeekFrameHash(analysisContext, resolved, file, args, base, remainingEntries, maxVideoSeekScanBytes)
		if err != nil {
			continue
		}
		proposed := append(indexes, index)
		data, err := json.Marshal(proposed)
		if err != nil || len(data) > MaxVideoSeekIndexBytes {
			continue
		}
		indexes = proposed
		remainingEntries -= len(index.Entries)
	}
	if len(indexes) > 0 {
		afterPath, afterTool, err := VideoSeekToolIdentity(analysisContext, executable)
		if err != nil || afterPath != resolved || afterTool != toolIdentity {
			return nil, nil
		}
	}
	return indexes, nil
}

func videoSeekCheckSource(file *os.File, before os.FileInfo) error {
	after, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat video seek source after analysis: %w", err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || FileChangeTime(before) != FileChangeTime(after) {
		return fmt.Errorf("media file changed during video seek analysis")
	}
	return nil
}

func runVideoSeekFrameHash(ctx context.Context, executable string, file *os.File, args []string, base VideoSeekIndex, maxEntries, maxOutput int) (VideoSeekIndex, error) {
	processContext, cancel := context.WithCancel(ctx)
	defer cancel()
	command := exec.CommandContext(processContext, executable, args...)
	command.ExtraFiles = []*os.File{file}
	command.Env = mediaProbeEnvironment()
	command.WaitDelay = time.Second
	stderr := &limitedOutput{limit: maxProcessStderr, cancel: cancel}
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return VideoSeekIndex{}, err
	}
	retired, err := startMediaProcess(command)
	if err != nil {
		return VideoSeekIndex{}, err
	}
	bounded := &io.LimitedReader{R: stdout, N: int64(maxOutput) + 1}
	index, parseErr := ParseVideoSeekFrameHash(bounded, base, maxEntries)
	if bounded.N <= 0 {
		parseErr = ErrOutputLimit
	}
	if parseErr != nil {
		cancel()
	}
	waitErr := errors.Join(<-retired, command.Wait())
	if err := ctx.Err(); err != nil {
		return VideoSeekIndex{}, err
	}
	if stderr.exceeded {
		return VideoSeekIndex{}, ErrOutputLimit
	}
	if parseErr != nil {
		return VideoSeekIndex{}, parseErr
	}
	if waitErr != nil {
		return VideoSeekIndex{}, fmt.Errorf("execute video seek analysis: %w", waitErr)
	}
	for _, severity := range []string{"[error]", "[fatal]", "[panic]"} {
		if strings.Contains(stderr.buffer.String(), severity) {
			return VideoSeekIndex{}, fmt.Errorf("video seek decoder reported an error")
		}
	}
	return index, nil
}

// VideoSeekVerification authorizes only the exact input argument that was
// preflighted. Unsupported evidence returns Verified=false for linear fallback.
type VideoSeekVerification struct {
	Verified       bool
	InputSeekTicks int64
}

// VerifyVideoSeekCandidate repeats the bounded restart proof on every run.
func VerifyVideoSeekCandidate(ctx context.Context, executable string, file *os.File, encoded string, decoderThreads int) (verification VideoSeekVerification, resultErr error) {
	if err := ctx.Err(); err != nil {
		return verification, err
	}
	if file == nil {
		return verification, fmt.Errorf("video seek source is nil")
	}
	before, err := file.Stat()
	if err != nil {
		return verification, err
	}
	if !before.Mode().IsRegular() {
		return verification, fmt.Errorf("video seek source is not regular")
	}
	defer func() {
		if err := videoSeekCheckSource(file, before); err != nil {
			verification, resultErr = VideoSeekVerification{}, err
		}
		if err := ctx.Err(); err != nil {
			verification, resultErr = VideoSeekVerification{}, err
		}
	}()
	candidate, err := ValidateVideoSeekCandidate(encoded)
	if err != nil || runtime.GOOS != "linux" || decoderThreads < 1 || decoderThreads > MaxVideoSeekDecoderThreads {
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
	for _, inputTicks := range videoSeekCandidateAttempts(candidate) {
		if proofContext.Err() != nil {
			break
		}
		absolute := new(big.Int).Add(big.NewInt(candidate.Index.FormatStartTicks), big.NewInt(inputTicks))
		seconds := new(big.Rat).SetFrac(absolute, big.NewInt(TicksPerSecond)).FloatString(7)
		args, err := BuildVideoSeekCommandArgs(candidate.Index.StreamIndex, &seconds, decoderThreads)
		if err != nil {
			continue
		}
		actual, err := runVideoSeekFrameHash(proofContext, resolved, file, args, candidate.Index, 2, 64*1024)
		if sourceErr := videoSeekCheckSource(file, before); sourceErr != nil {
			return verification, sourceErr
		}
		if err != nil || len(actual.Entries) != 1 {
			continue
		}
		point := actual.Entries[0]
		matched := false
		for _, expected := range candidate.Index.Entries {
			if point == expected {
				matched = true
				break
			}
		}
		if !matched || VideoSeekPointTime(actual, point).Cmp(VideoSeekRequestedTime(actual, candidate.RequestedStartTicks)) > 0 {
			continue
		}
		afterPath, afterTool, err := VideoSeekToolIdentity(proofContext, executable)
		if err != nil || afterPath != resolved || afterTool != toolIdentity {
			return verification, nil
		}
		return VideoSeekVerification{Verified: true, InputSeekTicks: inputTicks}, nil
	}
	return verification, nil
}

// Both PTS and DTS are only input proposals derived from actual indexed IDRs.
// Every distinct proposal still needs its own matching decoded restart proof.
func videoSeekCandidateAttempts(candidate VideoSeekCandidate) []int64 {
	var attempts []int64
	seen := make(map[int64]bool)
	entries := candidate.Index.Entries
	for position, points := len(entries)-1, 0; position >= 0 && points < maxVideoSeekProofPoints; position, points = position-1, points+1 {
		for _, timestamp := range []int64{entries[position].PTS, entries[position].DTS} {
			ticks, err := videoSeekInputTicks(candidate.Index, VideoSeekPoint{PTS: timestamp})
			if err != nil || ticks > candidate.RequestedStartTicks || seen[ticks] {
				continue
			}
			seen[ticks] = true
			attempts = append(attempts, ticks)
		}
	}
	return attempts
}
