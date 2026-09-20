package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/moooyo/goby/internal/introdetect"
)

const (
	MaxAnalysisDurationTicks int64 = 12 * 60 * 60 * TicksPerSecond
	MaxIntroAnalysisTicks    int64 = 600 * TicksPerSecond
	maxAnalysisPCMBytes      int64 = 13_230_000
)

var (
	ErrAnalysisUnavailable = errors.New("media analysis dependency is unavailable")
	ErrAnalysisUnproven    = errors.New("media analysis cannot prove its source timeline")
	ErrAnalysisBudget      = errors.New("media analysis exceeded its resource budget")
	analysisSlots          = make(chan struct{}, 1)
)

// AnalysisExtractor consumes only descriptors opened by an authorized caller.
// Its results are unpublished evidence. The caller owns source/authority CAS,
// cache publication, retention and application of any detected boundaries.
type AnalysisExtractor struct {
	FFmpegPath                string
	FFprobePath               string
	FingerprintPath           string
	ExpectedFFmpegSHA256      string
	ExpectedFFprobeSHA256     string
	ExpectedFingerprintSHA256 string
	Limits                    AnalysisLimits
}

type AnalysisLimits struct {
	Timeout          time.Duration
	MaxPCMBytes      int64
	MaxAudioFrames   int
	MaxVisualSamples int
	MaxPreviewFrames int
	MaxSourceFrames  int
	MaxSourcePixels  int64
	MaxFramePixels   int64
	MaxJPEGBytes     int64
	MaxOutputBytes   int64
	MaxRawBytes      int64
	MaxStderrBytes   int64
}

func DefaultAnalysisLimits() AnalysisLimits {
	return AnalysisLimits{Timeout: 10 * time.Minute, MaxPCMBytes: maxAnalysisPCMBytes, MaxAudioFrames: 65_536,
		MaxVisualSamples: 2400, MaxPreviewFrames: 8192, MaxSourceFrames: 2_000_000, MaxSourcePixels: 16 << 20,
		MaxFramePixels: 1 << 20, MaxJPEGBytes: 2 << 20, MaxOutputBytes: 512 << 20, MaxRawBytes: 4 << 30, MaxStderrBytes: 512 << 20}
}

func (e AnalysisExtractor) analysisLimits() (AnalysisLimits, error) {
	value, defaults := e.Limits, DefaultAnalysisLimits()
	if value.Timeout == 0 {
		value.Timeout = defaults.Timeout
	}
	if value.MaxPCMBytes == 0 {
		value.MaxPCMBytes = defaults.MaxPCMBytes
	}
	if value.MaxAudioFrames == 0 {
		value.MaxAudioFrames = defaults.MaxAudioFrames
	}
	if value.MaxVisualSamples == 0 {
		value.MaxVisualSamples = defaults.MaxVisualSamples
	}
	if value.MaxPreviewFrames == 0 {
		value.MaxPreviewFrames = defaults.MaxPreviewFrames
	}
	if value.MaxSourceFrames == 0 {
		value.MaxSourceFrames = defaults.MaxSourceFrames
	}
	if value.MaxSourcePixels == 0 {
		value.MaxSourcePixels = defaults.MaxSourcePixels
	}
	if value.MaxFramePixels == 0 {
		value.MaxFramePixels = defaults.MaxFramePixels
	}
	if value.MaxJPEGBytes == 0 {
		value.MaxJPEGBytes = defaults.MaxJPEGBytes
	}
	if value.MaxOutputBytes == 0 {
		value.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if value.MaxRawBytes == 0 {
		value.MaxRawBytes = defaults.MaxRawBytes
	}
	if value.MaxStderrBytes == 0 {
		value.MaxStderrBytes = defaults.MaxStderrBytes
	}
	if value.Timeout < time.Second || value.Timeout > 2*time.Hour ||
		value.MaxPCMBytes < 2 || value.MaxPCMBytes > maxAnalysisPCMBytes || value.MaxAudioFrames < 1 || value.MaxAudioFrames > 131_072 ||
		value.MaxVisualSamples < 1 || value.MaxVisualSamples > 10_000 || value.MaxPreviewFrames < 1 || value.MaxPreviewFrames > 8192 ||
		value.MaxSourceFrames < 1 || value.MaxSourceFrames > 4_000_000 || value.MaxSourcePixels < 1 || value.MaxSourcePixels > 32<<20 ||
		value.MaxFramePixels < 1 || value.MaxFramePixels > 4<<20 || value.MaxJPEGBytes < 1 || value.MaxJPEGBytes > 8<<20 ||
		value.MaxOutputBytes < 1 || value.MaxOutputBytes > 2<<30 || value.MaxRawBytes < 1 || value.MaxRawBytes > 8<<30 ||
		value.MaxStderrBytes < 1 || value.MaxStderrBytes > 512<<20 {
		return AnalysisLimits{}, ErrAnalysisBudget
	}
	return value, nil
}

func analysisAcquire(ctx context.Context, timeout time.Duration) (context.Context, func(), error) {
	if ctx == nil || timeout <= 0 || timeout > 2*time.Hour {
		return nil, nil, ErrAnalysisBudget
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	select {
	case analysisSlots <- struct{}{}:
		return bounded, func() { cancel(); <-analysisSlots }, nil
	case <-bounded.Done():
		cancel()
		return nil, nil, bounded.Err()
	}
}

func analysisCheckSource(file *os.File, info Info) (os.FileInfo, error) {
	if runtime.GOOS != "linux" || file == nil || !info.FormatStartKnown || info.DurationTicks <= 0 || info.DurationTicks > MaxAnalysisDurationTicks ||
		info.FormatStartTicks < -MaxAnalysisDurationTicks || info.FormatStartTicks > MaxAnalysisDurationTicks {
		return nil, ErrAnalysisUnproven
	}
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > MaxSubtitleRemovalInputBytes {
		return nil, ErrAnalysisUnproven
	}
	if info.Size > 0 && info.Size != before.Size() || info.FileChangeTimeNs != 0 && info.FileChangeTimeNs != FileChangeTime(before) {
		return nil, fmt.Errorf("%w: indexed source identity changed", ErrAnalysisUnproven)
	}
	return before, nil
}

func analysisSelectedStream(info Info, index int, kind string) (Stream, error) {
	var selected *Stream
	for _, stream := range info.Streams {
		if stream.Index != index {
			continue
		}
		if selected != nil || stream.Index < 0 || stream.Index > 4095 || stream.CodecType != kind || stream.IsExternal || stream.IsAttachedPicture {
			return Stream{}, fmt.Errorf("%w: selected %s stream is ambiguous or unsupported", ErrAnalysisUnproven, kind)
		}
		copyStream := stream
		selected = &copyStream
	}
	if selected != nil {
		return *selected, nil
	}
	return Stream{}, fmt.Errorf("%w: selected %s stream is absent", ErrAnalysisUnproven, kind)
}

type IntroAnalysisRequest struct {
	AudioStreamIndex    int
	VideoStreamIndex    int
	VisualIntervalTicks int64
}

type IntroFeatures struct {
	Audio                         []introdetect.AudioSample
	Visual                        []introdetect.VisualSample
	AudioBoundaryUncertaintyTicks int64
	AudioMetadata                 AudioFingerprintMetadata
	AlgorithmProfile              string
	SourceIdentity                string
	WindowTicks                   int64
	ToolFacts                     AnalysisToolFacts
}

type AnalysisToolFacts struct {
	FFmpegSHA256      string
	FFprobeSHA256     string
	FingerprintSHA256 string
}

// ExtractIntro shares one deadline across serial audio and visual decoding.
// An absent audio/video dependency returns an explicit failure, not a fabricated
// empty successful fingerprint. The cohort layer can classify such abstentions.
func (e AnalysisExtractor) ExtractIntro(ctx context.Context, input *os.File, info Info, request IntroAnalysisRequest) (result IntroFeatures, resultErr error) {
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
	before, err := analysisCheckSource(input, info)
	if err != nil {
		return result, err
	}
	defer func() {
		if err := videoSeekCheckSource(input, before); err != nil {
			result, resultErr = IntroFeatures{}, err
		}
	}()
	bounded, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()
	availability, err := e.Availability(bounded)
	if err != nil {
		return result, err
	}
	profile, err := IntroAlgorithmProfile(availability, request.VisualIntervalTicks)
	if err != nil {
		return result, err
	}
	// Freeze the observed admission bytes even for a standalone caller that
	// supplied no expected hashes. The serial audio and visual phases must not
	// silently use different executable builds after a pathname replacement.
	admitted, err := analysisAdmittedExtractor(e, availability)
	if err != nil {
		return result, err
	}
	audio, err := admitted.ExtractAudio(bounded, input, info, request.AudioStreamIndex)
	if err != nil {
		return result, err
	}
	visual, err := admitted.ExtractVisual(bounded, input, info, request.VideoStreamIndex, VisualAnalysisOptions{IntervalTicks: request.VisualIntervalTicks})
	if err != nil {
		return result, err
	}
	identity, err := VideoSeekSourceIdentity(before)
	if err != nil {
		return result, err
	}
	if err := bounded.Err(); err != nil {
		return IntroFeatures{}, err
	}
	return IntroFeatures{Audio: audio.Samples, Visual: visual, AudioBoundaryUncertaintyTicks: audio.BoundaryUncertaintyTicks,
		AudioMetadata: audio.Metadata, AlgorithmProfile: profile, SourceIdentity: identity, WindowTicks: min(info.DurationTicks, MaxIntroAnalysisTicks),
		ToolFacts: AnalysisToolFacts{FFmpegSHA256: availability.FFmpegSHA256, FFprobeSHA256: availability.FFprobeSHA256, FingerprintSHA256: availability.FingerprintSHA256}}, nil
}

func analysisAdmittedExtractor(extractor AnalysisExtractor, availability AnalysisAvailability) (AnalysisExtractor, error) {
	for _, pair := range [][2]string{
		{extractor.ExpectedFFmpegSHA256, availability.FFmpegSHA256},
		{extractor.ExpectedFFprobeSHA256, availability.FFprobeSHA256},
		{extractor.ExpectedFingerprintSHA256, availability.FingerprintSHA256},
	} {
		if !analysisValidSHA256(pair[1]) || pair[0] != "" && pair[0] != pair[1] {
			return AnalysisExtractor{}, ErrAnalysisUnavailable
		}
	}
	extractor.ExpectedFFmpegSHA256 = availability.FFmpegSHA256
	extractor.ExpectedFFprobeSHA256 = availability.FFprobeSHA256
	extractor.ExpectedFingerprintSHA256 = availability.FingerprintSHA256
	return extractor, nil
}
