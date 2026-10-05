package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/creditsskipper"
)

const creditsVisualMaximumRecords = 250000

type CreditsVisualRequest struct {
	VideoStreamIndex int
	// -1 means the indexed source contains no local audio stream.
	AudioStreamIndex int
	IsMovie          bool
	// Nil uses the source's probed chapters. A non-nil slice supplies the
	// caller's effective, source-bound chapter metadata in source seconds.
	Chapters      []creditsskipper.Chapter
	AudioSegments []creditsskipper.Segment
}

type CreditsVisualEvidence struct {
	Result           creditsskipper.Result
	AlgorithmProfile string
	SourceIdentity   string
	FFmpegSHA256     string
	VisualsAvailable bool
}

type CreditsVisualCapabilities struct {
	Available        bool
	VisualsAvailable bool
	Profile          string
	FFmpegSHA256     string
	Reason           string
}

func CreditsVisualAlgorithmProfile(capabilities CreditsVisualCapabilities) (string, error) {
	if !capabilities.Available || !analysisValidSHA256(capabilities.FFmpegSHA256) {
		return "", ErrAnalysisUnavailable
	}
	return "credits-visual-v1;upstream=" + creditsskipper.UpstreamCommit + ";pass=" + creditsskipper.Version +
		";ffmpeg=" + capabilities.FFmpegSHA256 + ";entropy=" + strconv.FormatBool(capabilities.VisualsAvailable) +
		";keyframes=independent-graphs;timeline=upstream;defaults=v1", nil
}

// CreditsVisualAvailability admits the main software FFmpeg independently from
// the optional intro-specific Chromaprint binary. Entropy/saturation are an
// optional upstream branch; required black-frame and adjustment tools are not.
func (e AnalysisExtractor) CreditsVisualAvailability(ctx context.Context) (result CreditsVisualCapabilities, resultErr error) {
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tool, err := analysisOpenToolExpected(bounded, e.FFmpegPath, e.ExpectedFFmpegSHA256)
	if err != nil {
		result.Reason = "ffmpeg_unavailable"
		return result, bounded.Err()
	}
	defer tool.file.Close()
	result, err = inspectCreditsVisualTool(bounded, tool)
	if err != nil {
		if bounded.Err() != nil {
			return result, errors.Join(bounded.Err(), err)
		}
		result.Available = false
		if result.Reason == "" {
			result.Reason = "credits_visual_profile_unavailable"
		}
		return result, nil
	}
	return result, nil
}

func inspectCreditsVisualTool(ctx context.Context, tool *analysisTool) (result CreditsVisualCapabilities, resultErr error) {
	result.FFmpegSHA256 = tool.sha
	if err := analysisValidateFFmpeg(ctx, tool); err != nil {
		return result, err
	}
	var inventory []byte
	sink := &introSkipperStderr{limit: 64 << 10}
	err := runAnalysisProcess(ctx, "/proc/self/fd/3", nil, nil, []string{"-hide_banner", "-filters"}, 5*time.Second, 256<<10, sink, func(reader io.Reader) error { var err error; inventory, err = io.ReadAll(reader); return err }, tool.file)
	if err != nil {
		return result, err
	}
	has := func(name string) bool {
		for _, line := range strings.Split(string(inventory), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == name {
				return true
			}
		}
		return false
	}
	for _, filter := range []string{"blackframe", "blackdetect", "silencedetect", "showinfo"} {
		if !has(filter) {
			result.Reason = "credits_visual_filters_unavailable"
			return result, ErrAnalysisUnavailable
		}
	}
	result.VisualsAvailable = has("format") && has("entropy") && has("signalstats") && has("metadata")
	if err := tool.check(); err != nil {
		return result, err
	}
	result.Available = true
	result.Profile, resultErr = CreditsVisualAlgorithmProfile(result)
	return result, resultErr
}

// ExtractCreditsVisual executes the complete default pure CreditsPass with
// authorized FFmpeg probes. One admission covers every probe, final source
// check, and actual process join; probes never reacquire the same analysis slot.
func (e AnalysisExtractor) ExtractCreditsVisual(ctx context.Context, input *os.File, info Info, request CreditsVisualRequest) (result CreditsVisualEvidence, resultErr error) {
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
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
	video, err := analysisSelectedStream(info, request.VideoStreamIndex, "video")
	if err != nil {
		return result, err
	}
	if video.Width < 1 || video.Height < 1 || video.Width > 16384 || video.Height > 16384 || int64(video.Width)*int64(video.Height) > 32<<20 {
		return result, ErrAnalysisBudget
	}
	if request.AudioStreamIndex >= 0 {
		if _, err := analysisSelectedStream(info, request.AudioStreamIndex, "audio"); err != nil {
			return result, err
		}
	} else {
		for _, stream := range info.Streams {
			if stream.CodecType == "audio" && !stream.IsExternal && !stream.IsAttachedPicture {
				return result, fmt.Errorf("%w: credits audio selection is missing", ErrAnalysisUnproven)
			}
		}
	}
	if len(request.Chapters) > 4096 || len(info.Chapters) > 4096 || len(request.AudioSegments) > 64 {
		return result, ErrAnalysisBudget
	}
	finalContext := ctx
	var release func()
	defer func() {
		if release != nil {
			defer release()
		}
		if err := errors.Join(videoSeekCheckSource(input, before), finalContext.Err()); err != nil {
			result, resultErr = CreditsVisualEvidence{}, err
		}
	}()
	bounded, operationRelease, err := analysisAcquire(ctx, limits.Timeout)
	if err != nil {
		return result, err
	}
	finalContext, release = bounded, operationRelease
	tool, err := analysisOpenToolExpected(bounded, e.FFmpegPath, e.ExpectedFFmpegSHA256)
	if err != nil {
		return result, err
	}
	defer tool.file.Close()
	defer func() {
		if err := tool.check(); err != nil {
			result, resultErr = CreditsVisualEvidence{}, errors.Join(resultErr, err)
		}
	}()
	capabilities, err := inspectCreditsVisualTool(bounded, tool)
	if err != nil {
		return result, err
	}
	duration := float64(info.DurationTicks) / float64(TicksPerSecond)
	chapters := append([]creditsskipper.Chapter(nil), request.Chapters...)
	if request.Chapters == nil {
		for _, chapter := range info.Chapters {
			chapters = append(chapters, creditsskipper.Chapter{Name: chapter.Title, StartSeconds: float64(chapter.StartTicks) / float64(TicksPerSecond)})
		}
	}
	probe := &creditsVisualProbe{source: input, tool: tool, before: before, duration: duration, videoIndex: request.VideoStreamIndex, audioIndex: request.AudioStreamIndex, visuals: capabilities.VisualsAvailable, timeout: limits.Timeout, stderrLimit: min(limits.MaxStderrBytes, 64<<20)}
	pass, err := creditsskipper.Detect(bounded, creditsskipper.Request{DurationSeconds: duration, IsMovie: request.IsMovie, Chapters: chapters, AudioSegments: append([]creditsskipper.Segment(nil), request.AudioSegments...)}, probe)
	if err != nil {
		return result, err
	}
	identity, err := VideoSeekSourceIdentity(before)
	if err != nil {
		return result, err
	}
	return CreditsVisualEvidence{Result: pass, AlgorithmProfile: capabilities.Profile, SourceIdentity: identity, FFmpegSHA256: tool.sha, VisualsAvailable: capabilities.VisualsAvailable}, nil
}

type creditsVisualProbe struct {
	source                 *os.File
	tool                   *analysisTool
	before                 os.FileInfo
	duration               float64
	videoIndex, audioIndex int
	visuals                bool
	timeout                time.Duration
	stderrLimit            int64
	operations             int
}

var _ creditsskipper.Probe = (*creditsVisualProbe)(nil)

func (probe *creditsVisualProbe) validateRange(ctx context.Context, window creditsskipper.Range) error {
	if ctx == nil {
		return ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !window.Valid() || window.Start < 0 || window.End > probe.duration || window.Duration() > 900 {
		return ErrAnalysisUnproven
	}
	if probe.operations >= 1024 {
		return ErrAnalysisBudget
	}
	probe.operations++
	return errors.Join(videoSeekCheckSource(probe.source, probe.before), probe.tool.check())
}

func (probe *creditsVisualProbe) run(ctx context.Context, args []string) (string, error) {
	sink := &creditsVisualLog{limit: probe.stderrLimit}
	err := runAnalysisStream(ctx, "/proc/self/fd/4", probe.source, args, probe.timeout, 1, sink, func(reader io.Reader) error {
		count, err := io.Copy(io.Discard, reader)
		if err == nil && count != 0 {
			return fmt.Errorf("%w: unexpected credits null output", ErrAnalysisUnproven)
		}
		return err
	}, probe.tool.file)
	if err != nil {
		return "", err
	}
	if err := errors.Join(ctx.Err(), videoSeekCheckSource(probe.source, probe.before), probe.tool.check()); err != nil {
		return "", err
	}
	return sink.result()
}

func creditsVisualNumber(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func creditsVisualPrefix() []string {
	return []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "info", "-xerror", "-max_alloc", "268435456", "-threads", "1", "-filter_threads", "1", "-filter_complex_threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", probeFormats}
}

func creditsVisualOutput(index int, filter string) []string {
	return []string{"-map", "0:" + strconv.Itoa(index), "-an", "-dn", "-sn", "-vf", filter, "-threads:v", "1", "-f", "null", "pipe:1"}
}

func creditsKeyframeArgs(index int, window creditsskipper.Range, threshold int, visuals bool) []string {
	args := append(creditsVisualPrefix(), "-skip_frame", "nokey", "-ss", creditsVisualNumber(window.Start), "-i", "/proc/self/fd/3")
	args = append(args, creditsVisualOutput(index, "blackframe=amount=0:threshold="+strconv.Itoa(threshold))...)
	if visuals {
		args = append(args, creditsVisualOutput(index, "format=yuv420p,entropy,signalstats,metadata=print")...)
	}
	return args
}

func creditsBlackIntervalArgs(index int, window creditsskipper.Range, threshold, minimum int) []string {
	args := append(creditsVisualPrefix(), "-ss", creditsVisualNumber(window.Start), "-skip_frame", "noref", "-i", "/proc/self/fd/3", "-to", creditsVisualNumber(window.Duration()))
	pixel := creditsVisualFourDecimals(min(1, max(0, float64(threshold-16)/219)))
	picture := creditsVisualFourDecimals(min(1, max(0, float64(minimum)/100)))
	return append(args, creditsVisualOutput(index, "blackdetect=d=0.1:pix_th="+pixel+":pic_th="+picture)...)
}

func creditsVisualFourDecimals(value float64) string {
	// Invariant 0.#### formatting uses midpoint-to-even rounding and removes
	// insignificant zeros; inputs here are bounded nonnegative proportions.
	text := strconv.FormatFloat(math.RoundToEven(value*10000)/10000, 'f', 4, 64)
	return strings.TrimRight(strings.TrimRight(text, "0"), ".")
}

func creditsBoundaryArgs(index int, window creditsskipper.Range, threshold int) []string {
	args := append(creditsVisualPrefix(), "-ss", creditsVisualNumber(window.Start), "-i", "/proc/self/fd/3", "-to", creditsVisualNumber(window.Duration()))
	return append(args, creditsVisualOutput(index, "blackframe=amount=50:threshold="+strconv.Itoa(threshold))...)
}

func creditsSilenceArgs(index int, window creditsskipper.Range) []string {
	args := append(creditsVisualPrefix(), "-vn", "-sn", "-dn", "-ss", creditsVisualNumber(window.Start), "-i", "/proc/self/fd/3", "-to", creditsVisualNumber(window.Duration()))
	return append(args, "-map", "0:"+strconv.Itoa(index), "-af", "silencedetect=noise=-50dB:duration=0.1", "-threads:a", "1", "-f", "null", "pipe:1")
}

func creditsBoundaryKeyframeArgs(index int, window creditsskipper.Range) []string {
	args := append(creditsVisualPrefix(), "-skip_frame", "nokey", "-ss", creditsVisualNumber(window.Start), "-i", "/proc/self/fd/3", "-to", creditsVisualNumber(window.Duration()))
	return append(args, creditsVisualOutput(index, "showinfo")...)
}

func (probe *creditsVisualProbe) ScanKeyframes(ctx context.Context, window creditsskipper.Range, threshold int) (creditsskipper.KeyframeEvidence, error) {
	if err := probe.validateRange(ctx, window); err != nil {
		return creditsskipper.KeyframeEvidence{}, err
	}
	if threshold < 0 || threshold > 255 {
		return creditsskipper.KeyframeEvidence{}, ErrAnalysisUnproven
	}
	raw, err := probe.run(ctx, creditsKeyframeArgs(probe.videoIndex, window, threshold, probe.visuals))
	if err != nil {
		return creditsskipper.KeyframeEvidence{}, err
	}
	frames, err := parseCreditsBlackFrames(raw)
	if err != nil {
		return creditsskipper.KeyframeEvidence{}, err
	}
	var visuals []creditsskipper.KeyframeVisual
	if probe.visuals {
		visuals, err = parseCreditsKeyframeVisuals(raw, window.Duration())
		if err != nil {
			return creditsskipper.KeyframeEvidence{}, err
		}
	}
	return creditsskipper.KeyframeEvidence{BlackFrames: frames, Visuals: visuals}, nil
}

func (probe *creditsVisualProbe) ScanBlackIntervals(ctx context.Context, window creditsskipper.Range, threshold, minimum int) ([]creditsskipper.Range, error) {
	if err := probe.validateRange(ctx, window); err != nil {
		return nil, err
	}
	if threshold < 0 || threshold > 255 || minimum < 0 || minimum > 100 {
		return nil, ErrAnalysisUnproven
	}
	raw, err := probe.run(ctx, creditsBlackIntervalArgs(probe.videoIndex, window, threshold, minimum))
	if err != nil {
		return nil, err
	}
	return parseCreditsBlackIntervals(raw)
}

func (probe *creditsVisualProbe) ScanBoundary(ctx context.Context, window creditsskipper.Range, threshold, minimum int) ([]creditsskipper.BlackFrame, error) {
	if err := probe.validateRange(ctx, window); err != nil {
		return nil, err
	}
	if threshold < 0 || threshold > 255 || minimum < 0 || minimum > 100 {
		return nil, ErrAnalysisUnproven
	}
	raw, err := probe.run(ctx, creditsBoundaryArgs(probe.videoIndex, window, threshold))
	if err != nil {
		return nil, err
	}
	all, err := parseCreditsBlackFrames(raw)
	if err != nil {
		return nil, err
	}
	frames := make([]creditsskipper.BlackFrame, 0, len(all))
	for _, frame := range all {
		if frame.Percentage >= minimum {
			frames = append(frames, frame)
		}
	}
	return frames, nil
}

func (probe *creditsVisualProbe) ScanSilence(ctx context.Context, window creditsskipper.Range) ([]creditsskipper.Range, error) {
	if err := probe.validateRange(ctx, window); err != nil {
		return nil, err
	}
	if probe.audioIndex < 0 {
		return nil, nil
	}
	raw, err := probe.run(ctx, creditsSilenceArgs(probe.audioIndex, window))
	if err != nil {
		return nil, err
	}
	return parseCreditsSilence(raw, window.Start)
}

func (probe *creditsVisualProbe) ScanKeyframesAtBoundary(ctx context.Context, window creditsskipper.Range) ([]float64, error) {
	if err := probe.validateRange(ctx, window); err != nil {
		return nil, err
	}
	raw, err := probe.run(ctx, creditsBoundaryKeyframeArgs(probe.videoIndex, window))
	if err != nil {
		return nil, err
	}
	return parseCreditsBoundaryKeyframes(raw, window.Start)
}
