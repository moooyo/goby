package transcode

import (
	"errors"
	"fmt"
	"math"
)

var (
	ErrInvalidExecution    = errors.New("invalid transcode execution options")
	ErrToneMappingDisabled = errors.New("selected tone mapping backend is disabled")
)

// CPUQuality is a closed output-codec policy. CRF remains a stored preference
// in bitrate mode; only capped_crf activates it. It is never an FFmpeg fragment.
type CPUQuality struct {
	Preset      string `json:"Preset"`
	RateControl string `json:"RateControl"`
	CRF         int    `json:"CRF"`
	// The presence bit preserves the historical deployment thread limit when
	// an older stored codec group has no per-codec setting. An explicitly set
	// zero delegates thread selection to the encoder.
	ThreadCount    int    `json:"ThreadCount,omitempty"`
	ThreadCountSet bool   `json:"ThreadCountSet,omitempty"`
	X264Options    string `json:"X264Options,omitempty"`
}

// ExecutionOptions contains only comparable, resolved execution choices. A
// settings snapshot retains both codec groups. CaptureExecution removes choices
// that cannot affect the selected output before they enter a job/cache identity.
type ExecutionOptions struct {
	Threads                int                    `json:"Threads"`
	H264                   CPUQuality             `json:"H264"`
	HEVC                   CPUQuality             `json:"HEVC"`
	SoftwareToneMapping    bool                   `json:"SoftwareToneMapping"`
	VulkanToneMapping      bool                   `json:"VulkanToneMapping"`
	DownMixAudioBoost      float64                `json:"DownMixAudioBoost,omitempty"`
	SoftwareToneMapOptions SoftwareToneMapOptions `json:"SoftwareToneMapOptions,omitzero"`
}

const (
	LegacyExecutionVersion = 1
	ExecutionVersion       = 2
)

// DefaultExecutionOptions preserves the historical command policy. A zero
// deployment thread value selects two; other values are validated separately.
func DefaultExecutionOptions(threads int) ExecutionOptions {
	if threads == 0 {
		threads = 2
	}
	return ExecutionOptions{Threads: threads,
		H264:                CPUQuality{Preset: "veryfast", RateControl: "bitrate", CRF: 23},
		HEVC:                CPUQuality{Preset: "veryfast", RateControl: "bitrate", CRF: 28},
		SoftwareToneMapping: true, VulkanToneMapping: true, DownMixAudioBoost: 1,
		SoftwareToneMapOptions: DefaultSoftwareToneMapOptions()}
}

func ValidateCPUQuality(codec string, value CPUQuality) error {
	if codec != "h264" && codec != "hevc" {
		return fmt.Errorf("%w: codec", ErrInvalidExecution)
	}
	if !validCPUPreset(codec, value.Preset) {
		return fmt.Errorf("%w: %s preset", ErrInvalidExecution, codec)
	}
	if value.RateControl != "bitrate" && value.RateControl != "capped_crf" {
		return fmt.Errorf("%w: %s rate control", ErrInvalidExecution, codec)
	}
	if value.CRF < 1 || value.CRF > 51 {
		return fmt.Errorf("%w: %s CRF", ErrInvalidExecution, codec)
	}
	if value.ThreadCount < 0 || value.ThreadCount > 16 || !value.ThreadCountSet && value.ThreadCount != 0 {
		return fmt.Errorf("%w: %s thread count", ErrInvalidExecution, codec)
	}
	if codec == "hevc" && value.X264Options != "" {
		return fmt.Errorf("%w: unused x264 options", ErrInvalidExecution)
	}
	if _, err := CanonicalX264Options(value.X264Options); err != nil {
		return err
	}
	return nil
}

func validCPUPreset(codec, preset string) bool {
	switch preset {
	case "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow":
		return true
	case "ultrafast", "placebo":
		return codec == "hevc"
	}
	return false
}

func ValidateExecutionOptions(value ExecutionOptions) error {
	if value.Threads < 1 || value.Threads > maxThreads {
		return fmt.Errorf("%w: threads", ErrInvalidExecution)
	}
	if err := ValidateCPUQuality("h264", value.H264); err != nil {
		return err
	}
	if err := ValidateCPUQuality("hevc", value.HEVC); err != nil {
		return err
	}
	// Zero is the canonical inactive value, not a managed volume preference.
	if math.IsNaN(value.DownMixAudioBoost) || math.IsInf(value.DownMixAudioBoost, 0) ||
		value.DownMixAudioBoost != 0 && (value.DownMixAudioBoost < .1 || value.DownMixAudioBoost > 10) {
		return fmt.Errorf("%w: downmix boost", ErrInvalidExecution)
	}
	if value.SoftwareToneMapOptions != (SoftwareToneMapOptions{}) {
		if err := ValidateSoftwareToneMapOptions(value.SoftwareToneMapOptions); err != nil {
			return err
		}
	}
	return nil
}

// CaptureExecution binds one admission to resolved settings. Only the actual
// codec/backend and processing graph are relevant: an inactive preference must
// not split an otherwise identical output. A hardware fallback must call this
// again with its original complete settings snapshot after selecting software.
func CaptureExecution(plan Plan, options ExecutionOptions) (Plan, error) {
	if err := ValidateExecutionOptions(options); err != nil {
		return plan, err
	}
	if plan.VideoFilters.ToneMap != "" {
		if plan.VideoFilters.Backend == "" && !options.SoftwareToneMapping ||
			plan.VideoFilters.Backend == "vulkan" && !options.VulkanToneMapping {
			return plan, ErrToneMappingDisabled
		}
	}
	canonical := legacyExecutionDefaults(options.Threads)
	if plan.VideoFilters.ToneMap != "" && plan.VideoFilters.Backend == "" {
		canonical.SoftwareToneMapOptions = options.SoftwareToneMapOptions
		if canonical.SoftwareToneMapOptions == (SoftwareToneMapOptions{}) {
			canonical.SoftwareToneMapOptions = DefaultSoftwareToneMapOptions()
		}
		// The actual output dimensions already record the size restriction.
		canonical.SoftwareToneMapOptions.ToneMapSizeLimit = "None"
	}
	_, encode := hardwareSelection(plan.Hardware)
	if encode == "software" {
		switch plan.VideoCodec {
		case "h264":
			canonical.H264 = options.H264
			canonical.H264.X264Options, _ = CanonicalX264Options(options.H264.X264Options)
			if canonical.H264.RateControl == "bitrate" {
				canonical.H264.CRF = 23
			}
		case "hevc":
			canonical.HEVC = options.HEVC
			if canonical.HEVC.RateControl == "bitrate" {
				canonical.HEVC.CRF = 28
			}
		}
	}
	// Identical effective thread counts share output identity even when one
	// administrator chose the same value explicitly in the codec editor.
	for _, quality := range []*CPUQuality{&canonical.H264, &canonical.HEVC} {
		if quality.ThreadCountSet && quality.ThreadCount == canonical.Threads {
			quality.ThreadCount, quality.ThreadCountSet = 0, false
		}
	}
	if appliesDownMixBoost(plan, options.DownMixAudioBoost) {
		if plan.SourceAudioChannels > 64 {
			return plan, fmt.Errorf("%w: source audio channels", ErrInvalidPlan)
		}
		canonical.DownMixAudioBoost = options.DownMixAudioBoost
	} else {
		plan.SourceAudioChannels = 0
	}
	plan.ExecutionVersion, plan.Execution = ExecutionVersion, canonical
	return plan, nil
}

func legacyExecutionDefaults(threads int) ExecutionOptions {
	value := DefaultExecutionOptions(threads)
	value.DownMixAudioBoost = 0
	value.SoftwareToneMapOptions = SoftwareToneMapOptions{}
	return value
}

func appliesDownMixBoost(plan Plan, boost float64) bool {
	channels, _ := progressiveAudioDimensions(plan)
	return boost != 0 && boost != 1 && plan.AudioStreamIndex >= 0 && plan.AudioCodec != "" &&
		plan.AudioCodec != "copy" && channels == 2 && plan.SourceAudioChannels > 2
}

func validateLegacyExecution(plan Plan) error {
	options := plan.Execution
	if plan.SourceAudioChannels != 0 || options.DownMixAudioBoost != 0 || options.SoftwareToneMapOptions != (SoftwareToneMapOptions{}) || options.Threads < 1 || options.Threads > maxThreads {
		return fmt.Errorf("%w: legacy execution fields", ErrInvalidPlan)
	}
	for _, quality := range []CPUQuality{options.H264, options.HEVC} {
		if quality.Preset != "veryfast" && quality.Preset != "fast" && quality.Preset != "medium" && quality.Preset != "slow" ||
			quality.RateControl != "bitrate" && quality.RateControl != "capped_crf" || quality.CRF < 18 || quality.CRF > 35 ||
			quality.ThreadCount != 0 || quality.ThreadCountSet || quality.X264Options != "" {
			return fmt.Errorf("%w: legacy CPU options", ErrInvalidPlan)
		}
	}
	canonical, err := CaptureExecution(plan, options)
	canonical.Execution.SoftwareToneMapOptions = SoftwareToneMapOptions{}
	if err != nil || canonical.Execution != options {
		return fmt.Errorf("%w: noncanonical legacy execution options", ErrInvalidPlan)
	}
	return nil
}

// validateExecutionPlan deliberately admits an entirely absent legacy context.
// It never fills historical records: their execution thread count is unknown.
// Managers capture a legacy caller's default only before creating a new job.
func validateExecutionPlan(plan Plan) error {
	if plan.ExecutionVersion == 0 {
		if plan.Execution != (ExecutionOptions{}) || plan.SourceAudioChannels != 0 {
			return fmt.Errorf("%w: unversioned execution options", ErrInvalidPlan)
		}
		return nil
	}
	if plan.ExecutionVersion == LegacyExecutionVersion {
		return validateLegacyExecution(plan)
	}
	if plan.ExecutionVersion != ExecutionVersion {
		return fmt.Errorf("%w: execution version", ErrInvalidPlan)
	}
	canonical, err := CaptureExecution(plan, plan.Execution)
	if err != nil || canonical.Execution != plan.Execution || canonical.SourceAudioChannels != plan.SourceAudioChannels {
		return fmt.Errorf("%w: noncanonical execution options", ErrInvalidPlan)
	}
	return nil
}

// ExecutionThreads returns the immutable admission value for current plans.
// The explicit argument is used only by legacy direct runner callers, never to
// reinterpret an already captured plan or reconstruct an old durable record.
func ExecutionThreads(plan Plan, legacyThreads int) (int, error) {
	if err := validateExecutionPlan(plan); err != nil {
		return 0, err
	}
	if plan.ExecutionVersion == LegacyExecutionVersion || plan.ExecutionVersion == ExecutionVersion {
		return plan.Execution.Threads, nil
	}
	if legacyThreads < 1 || legacyThreads > maxThreads {
		return 0, ErrInvalidThreads
	}
	return legacyThreads, nil
}
