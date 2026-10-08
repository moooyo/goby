package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

// ErrIntroSkipperFingerprintUnavailable is a completed muxer invocation with no
// fingerprint words. Like the upstream per-episode FingerprintException, this
// source can be excluded from matching without publishing a successful cache.
var ErrIntroSkipperFingerprintUnavailable = errors.New("intro skipper produced no fingerprint words")

// IntroSkipperAnalysisRequest selects an original audio stream and a complete
// upstream configuration. Its fingerprint window uses the upstream clock.
type IntroSkipperAnalysisRequest struct {
	AudioStreamIndex int
	Options          introskipper.Options
}

// IntroSkipperFeatures keeps the complete raw sequence. Its array indexes use
// Intro Skipper's clock, not the source-PTS bins of the legacy audiovisual path.
type IntroSkipperFeatures struct {
	RawFingerprint        []uint32
	FingerprintEndSeconds float64
	AlgorithmProfile      string
	SourceIdentity        string
	FFmpegSHA256          string
}

// IntroSkipperAlgorithmProfile binds extraction semantics and the admitted
// binary without requiring the legacy helper, video filters, or geometry probe.
// The work configuration separately binds the window and matcher options.
func IntroSkipperAlgorithmProfile(available AnalysisAvailability) (string, error) {
	if !available.IntroSkipperAvailable || !analysisValidSHA256(available.IntroFFmpegSHA256) {
		return "", ErrAnalysisUnavailable
	}
	return "intro-skipper-fp-v1;upstream=" + introskipper.UpstreamCommit +
		";ffmpeg=" + available.IntroFFmpegSHA256 +
		";format=chromaprint-raw-u32le;channels=2;clock=upstream;selection=language-channels-index-v1", nil
}

func (e AnalysisExtractor) introSkipperTool() (string, string) {
	path, expected := e.IntroFFmpegPath, e.ExpectedIntroFFmpegSHA256
	if path == "" {
		path = e.FFmpegPath
		if expected == "" {
			expected = e.ExpectedFFmpegSHA256
		}
	}
	return path, expected
}

func (e AnalysisExtractor) introSkipperAvailability(ctx context.Context, result *AnalysisAvailability) error {
	path, expected := e.introSkipperTool()
	tool, err := analysisOpenToolExpected(ctx, path, expected)
	if err != nil {
		result.IntroSkipperReason = "intro_ffmpeg_unavailable"
		return ctx.Err()
	}
	defer tool.file.Close()
	if err := analysisValidateIntroSkipperFFmpeg(ctx, tool); err != nil {
		result.IntroSkipperReason = "chromaprint_muxer_unavailable"
		return ctx.Err()
	}
	result.IntroSkipperAvailable = true
	result.IntroFFmpegPath, result.IntroFFmpegSHA256 = tool.path, tool.sha
	return nil
}

func analysisValidateIntroSkipperFFmpeg(ctx context.Context, tool *analysisTool) error {
	query := func(args ...string) (string, error) {
		var output []byte
		sink := &introSkipperStderr{limit: 64 << 10}
		err := runAnalysisProcess(ctx, "/proc/self/fd/3", nil, nil, args, 5*time.Second, 128<<10, sink,
			func(reader io.Reader) error { var err error; output, err = io.ReadAll(reader); return err }, tool.file)
		return string(output), err
	}
	version, err := query("-version")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !strings.HasPrefix(version, "ffmpeg version ") {
		return fmt.Errorf("%w: intro FFmpeg version query failed", ErrAnalysisUnavailable)
	}
	muxer, err := query("-hide_banner", "-h", "muxer=chromaprint")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !introSkipperMuxerSupported(muxer) {
		return fmt.Errorf("%w: raw Chromaprint muxer is required", ErrAnalysisUnavailable)
	}
	encoders, err := query("-hide_banner", "-encoders")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || !introSkipperEncoderSupported(encoders) {
		return fmt.Errorf("%w: Chromaprint requires the pcm_s16le encoder", ErrAnalysisUnavailable)
	}
	return tool.check()
}

func introSkipperEncoderSupported(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && len(fields[0]) == 6 && fields[0][0] == 'A' && fields[1] == "pcm_s16le" {
			return true
		}
	}
	return false
}

func introSkipperMuxerSupported(output string) bool {
	if !strings.Contains(output, "Muxer chromaprint [Chromaprint]") ||
		!strings.Contains(output, "Default audio codec: pcm_s16le.") {
		return false
	}
	var algorithm, silence, raw bool
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "-algorithm":
			algorithm = strings.HasSuffix(strings.TrimSpace(line), "(default 1)")
		case "-silence_threshold":
			silence = strings.HasSuffix(strings.TrimSpace(line), "(default -1)")
		case "raw":
			raw = strings.Contains(line, "binary raw fingerprint")
		}
	}
	return algorithm && silence && raw
}

// SelectIntroSkipperAudioStream follows the upstream language fallback and
// channel-count policy. A default-track flag has no priority in that policy.
func SelectIntroSkipperAudioStream(info Info, preferredLanguage string, preferMostChannels bool) (int, bool) {
	preferredLanguage = strings.TrimSpace(preferredLanguage)
	streams := make([]Stream, 0, len(info.Streams))
	seen := make(map[int]bool, len(info.Streams))
	for _, stream := range info.Streams {
		if stream.IsExternal {
			continue
		}
		if stream.Index < 0 || stream.Index > 4095 || seen[stream.Index] {
			return 0, false
		}
		seen[stream.Index] = true
		if stream.CodecType == "audio" && !stream.IsAttachedPicture {
			streams = append(streams, stream)
		}
	}
	if len(streams) == 0 {
		return 0, false
	}
	candidates := streams
	if preferredLanguage != "" {
		matching := make([]Stream, 0, len(streams))
		for _, stream := range streams {
			if strings.EqualFold(strings.TrimSpace(stream.Language), preferredLanguage) {
				matching = append(matching, stream)
			}
		}
		if len(matching) != 0 {
			candidates = matching
		}
	}
	selected := candidates[0]
	for _, stream := range candidates[1:] {
		if preferMostChannels && stream.Channels > selected.Channels ||
			(!preferMostChannels || stream.Channels == selected.Channels) && stream.Index < selected.Index {
			selected = stream
		}
	}
	return selected.Index, true
}

func introSkipperSource(input *os.File, info Info) (os.FileInfo, error) {
	if input == nil || info.DurationTicks <= 0 || info.DurationTicks > MaxAnalysisDurationTicks {
		return nil, ErrAnalysisUnproven
	}
	before, err := input.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxSubtitleRemovalInputBytes {
		return nil, ErrAnalysisUnproven
	}
	if info.Size > 0 && info.Size != before.Size() || info.FileChangeTimeNs != 0 && info.FileChangeTimeNs != FileChangeTime(before) {
		return nil, fmt.Errorf("%w: indexed intro source identity changed", ErrAnalysisUnproven)
	}
	return before, nil
}

func introSkipperArgs(index int, end float64) []string {
	return []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-xerror", "-max_alloc", "268435456",
		"-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-ss", "0", "-i", "/proc/self/fd/3", "-to", strconv.FormatFloat(end, 'f', -1, 64), "-map", "0:" + strconv.Itoa(index),
		"-vn", "-sn", "-dn", "-ac", "2", "-threads:a", "1", "-f", "chromaprint", "-fp_format", "raw", "pipe:1"}
}

// ExtractIntroSkipper executes the upstream stereo Chromaprint muxer recipe
// through an authorized descriptor. It never trims raw words or adds source-PTS
// compensation. Source checks and process cancellation remain Goby boundaries.
func (e AnalysisExtractor) ExtractIntroSkipper(ctx context.Context, input *os.File, info Info, request IntroSkipperAnalysisRequest) (result IntroSkipperFeatures, resultErr error) {
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := introskipper.ValidateOptions(request.Options); err != nil {
		return result, err
	}
	limits, err := e.analysisLimits()
	if err != nil {
		return result, err
	}
	before, err := introSkipperSource(input, info)
	if err != nil {
		return result, err
	}
	finalContext := ctx
	var release func()
	defer func() {
		finalizeIntroSkipperAnalysis(finalContext, func() error { return videoSeekCheckSource(input, before) }, release, &result, &resultErr)
	}()
	if _, err := analysisSelectedStream(info, request.AudioStreamIndex, "audio"); err != nil {
		return result, err
	}
	bounded, operationRelease, err := analysisAcquire(ctx, limits.Timeout)
	if err != nil {
		return result, err
	}
	finalContext, release = bounded, operationRelease
	path, expected := e.introSkipperTool()
	tool, err := analysisOpenToolExpected(bounded, path, expected)
	if err != nil {
		return result, err
	}
	defer tool.file.Close()
	if err := analysisValidateIntroSkipperFFmpeg(bounded, tool); err != nil {
		return result, err
	}
	end := introskipper.FingerprintEndSeconds(info.DurationTicks, request.Options)
	var output []byte
	sink := &introSkipperStderr{limit: min(limits.MaxStderrBytes, 1<<20)}
	err = runAnalysisStream(bounded, "/proc/self/fd/4", input, introSkipperArgs(request.AudioStreamIndex, end), limits.Timeout,
		int64(introskipper.MaxFingerprintPoints*4), sink,
		func(reader io.Reader) error { var err error; output, err = io.ReadAll(reader); return err }, tool.file)
	if err != nil {
		return result, err
	}
	if err := tool.check(); err != nil {
		return result, err
	}
	raw, err := parseIntroSkipperFingerprint(output)
	if err != nil {
		return result, err
	}
	profile, err := IntroSkipperAlgorithmProfile(AnalysisAvailability{IntroSkipperAvailable: true, IntroFFmpegSHA256: tool.sha})
	if err != nil {
		return result, err
	}
	identity, err := VideoSeekSourceIdentity(before)
	if err != nil {
		return result, err
	}
	if err := bounded.Err(); err != nil {
		return result, err
	}
	return IntroSkipperFeatures{RawFingerprint: raw, FingerprintEndSeconds: end, AlgorithmProfile: profile, SourceIdentity: identity, FFmpegSHA256: tool.sha}, nil
}

// The admission slot belongs to the entire operation, including final source
// I/O. A source or deadline failure supersedes the empty-fingerprint sentinel:
// callers must never classify an incomplete operation as an ordinary no-result.
func finalizeIntroSkipperAnalysis(ctx context.Context, checkSource func() error, release func(), result *IntroSkipperFeatures, resultErr *error) {
	if release != nil {
		defer release()
	}
	if err := errors.Join(checkSource(), ctx.Err()); err != nil {
		*result, *resultErr = IntroSkipperFeatures{}, err
	}
}

func parseIntroSkipperFingerprint(data []byte) ([]uint32, error) {
	if len(data) > introskipper.MaxFingerprintPoints*4 {
		return nil, ErrAnalysisBudget
	}
	if len(data) == 0 {
		return nil, ErrIntroSkipperFingerprintUnavailable
	}
	if len(data)%4 != 0 {
		return nil, fmt.Errorf("%w: malformed raw Chromaprint sequence", ErrAnalysisUnproven)
	}
	raw := make([]uint32, len(data)/4)
	for index := range raw {
		raw[index] = binary.LittleEndian.Uint32(data[index*4 : index*4+4])
	}
	return raw, nil
}

// Diagnostics do not participate in the native fingerprint clock. Retain only
// the byte count while the common runner checks the real child exit status.
type introSkipperStderr struct {
	mu     sync.Mutex
	limit  int64
	bytes  int64
	closed bool
}

func (sink *introSkipperStderr) Write(data []byte) (int, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.closed {
		return 0, io.ErrClosedPipe
	}
	if int64(len(data)) > sink.limit-sink.bytes {
		return 0, ErrAnalysisBudget
	}
	sink.bytes += int64(len(data))
	return len(data), nil
}

func (sink *introSkipperStderr) Close(error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.closed = true
}
