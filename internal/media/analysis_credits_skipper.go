package media

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"

	"github.com/moooyo/goby/internal/introskipper"
)

// CreditsSkipperFeatures preserves the complete raw sequence and both exact
// upstream binary64 window boundaries. The matcher adds FingerprintStartSeconds
// to its relative candidates; extraction never trims words or changes clocks.
type CreditsSkipperFeatures struct {
	RawFingerprint          []uint32
	FingerprintStartSeconds float64
	FingerprintEndSeconds   float64
	AlgorithmProfile        string
	SourceIdentity          string
	FFmpegSHA256            string
}

// CreditsSkipperAlgorithmProfile uses the existing admitted stereo Chromaprint
// tool. Credits extraction has no video, geometry, GPU, or derivative-cache
// dependency. Its identity cannot be confused with the intro raw profile.
func CreditsSkipperAlgorithmProfile(available AnalysisAvailability) (string, error) {
	if !available.IntroSkipperAvailable || !analysisValidSHA256(available.IntroFFmpegSHA256) {
		return "", ErrAnalysisUnavailable
	}
	return "credits-skipper-fp-v1;detector=" + introskipper.CreditsVersion + ";upstream=" + introskipper.UpstreamCommit +
		";ffmpeg=" + available.IntroFFmpegSHA256 + ";format=chromaprint-raw-u32le;channels=2;clock=upstream;window=tail-450-seconds;selection=language-channels-index-v1", nil
}

// The pinned upstream FFmpegService.FingerprintAsync places -ss before the
// input, then sets output -to to end-start. Without -copyts, output timestamps
// are relative to the seek. Supplying the absolute source end would decode the
// wrong interval. Keep binary64 subtraction and shortest exact decimal values.
func creditsSkipperArgs(index int, start, end float64) []string {
	return []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-xerror", "-max_alloc", "268435456",
		"-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats,
		"-ss", strconv.FormatFloat(start, 'f', -1, 64), "-i", "/proc/self/fd/3", "-to", strconv.FormatFloat(end-start, 'f', -1, 64), "-map", "0:" + strconv.Itoa(index),
		"-vn", "-sn", "-dn", "-ac", "2", "-threads:a", "1", "-f", "chromaprint", "-fp_format", "raw", "pipe:1"}
}

// ExtractCreditsSkipper follows the pinned ending-credit raw fingerprint
// recipe over the last 450 seconds (or the complete shorter source). It borrows
// an authorized descriptor and shares the existing bounded process/source
// lifetime. The caller selects audio with SelectIntroSkipperAudioStream and
// owns cohort matching, publication, and any persistence policy.
func (e AnalysisExtractor) ExtractCreditsSkipper(ctx context.Context, input *os.File, info Info, request IntroSkipperAnalysisRequest) (result CreditsSkipperFeatures, resultErr error) {
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
		finalizeCreditsSkipperAnalysis(finalContext, func() error { return videoSeekCheckSource(input, before) }, release, &result, &resultErr)
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
	start := introskipper.CreditsFingerprintStartSeconds(info.DurationTicks)
	end := float64(info.DurationTicks) / float64(TicksPerSecond)
	var output []byte
	sink := &introSkipperStderr{limit: min(limits.MaxStderrBytes, 1<<20)}
	err = runAnalysisStream(bounded, "/proc/self/fd/4", input, creditsSkipperArgs(request.AudioStreamIndex, start, end), limits.Timeout,
		int64(introskipper.MaxFingerprintPoints*4), sink, func(reader io.Reader) error { var err error; output, err = io.ReadAll(reader); return err }, tool.file)
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
	profile, err := CreditsSkipperAlgorithmProfile(AnalysisAvailability{IntroSkipperAvailable: true, IntroFFmpegSHA256: tool.sha})
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
	return CreditsSkipperFeatures{RawFingerprint: raw, FingerprintStartSeconds: start, FingerprintEndSeconds: end, AlgorithmProfile: profile, SourceIdentity: identity, FFmpegSHA256: tool.sha}, nil
}

// A failed final source check or expired operation invalidates both successful
// and empty results before the shared analysis slot is released. In particular,
// ErrIntroSkipperFingerprintUnavailable retains its existing completed-empty
// meaning and must never hide an interrupted credits extraction.
func finalizeCreditsSkipperAnalysis(ctx context.Context, checkSource func() error, release func(), result *CreditsSkipperFeatures, resultErr *error) {
	if release != nil {
		defer release()
	}
	if err := errors.Join(checkSource(), ctx.Err()); err != nil {
		*result, *resultErr = CreditsSkipperFeatures{}, err
	}
}
