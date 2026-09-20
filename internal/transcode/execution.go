package transcode

import (
	"errors"
	"fmt"
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
}

// ExecutionOptions contains only comparable, resolved execution choices. A
// settings snapshot retains both codec groups. CaptureExecution removes choices
// that cannot affect the selected output before they enter a job/cache identity.
type ExecutionOptions struct {
	Threads             int        `json:"Threads"`
	H264                CPUQuality `json:"H264"`
	HEVC                CPUQuality `json:"HEVC"`
	SoftwareToneMapping bool       `json:"SoftwareToneMapping"`
	VulkanToneMapping   bool       `json:"VulkanToneMapping"`
}

const ExecutionVersion = 1

// DefaultExecutionOptions preserves the historical command policy. A zero
// deployment thread value selects two; other values are validated separately.
func DefaultExecutionOptions(threads int) ExecutionOptions {
	if threads == 0 {
		threads = 2
	}
	return ExecutionOptions{Threads: threads,
		H264:                CPUQuality{Preset: "veryfast", RateControl: "bitrate", CRF: 23},
		HEVC:                CPUQuality{Preset: "veryfast", RateControl: "bitrate", CRF: 28},
		SoftwareToneMapping: true, VulkanToneMapping: true}
}

func ValidateCPUQuality(codec string, value CPUQuality) error {
	if codec != "h264" && codec != "hevc" {
		return fmt.Errorf("%w: codec", ErrInvalidExecution)
	}
	if value.Preset != "veryfast" && value.Preset != "fast" && value.Preset != "medium" && value.Preset != "slow" {
		return fmt.Errorf("%w: %s preset", ErrInvalidExecution, codec)
	}
	if value.RateControl != "bitrate" && value.RateControl != "capped_crf" {
		return fmt.Errorf("%w: %s rate control", ErrInvalidExecution, codec)
	}
	if value.CRF < 18 || value.CRF > 35 {
		return fmt.Errorf("%w: %s CRF", ErrInvalidExecution, codec)
	}
	return nil
}

func ValidateExecutionOptions(value ExecutionOptions) error {
	if value.Threads < 1 || value.Threads > maxThreads {
		return fmt.Errorf("%w: threads", ErrInvalidExecution)
	}
	if err := ValidateCPUQuality("h264", value.H264); err != nil {
		return err
	}
	return ValidateCPUQuality("hevc", value.HEVC)
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
	canonical := DefaultExecutionOptions(options.Threads)
	_, encode := hardwareSelection(plan.Hardware)
	if encode == "software" {
		switch plan.VideoCodec {
		case "h264":
			canonical.H264 = options.H264
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
	plan.ExecutionVersion, plan.Execution = ExecutionVersion, canonical
	return plan, nil
}

// validateExecutionPlan deliberately admits an entirely absent legacy context.
// It never fills historical records: their execution thread count is unknown.
// Managers capture a legacy caller's default only before creating a new job.
func validateExecutionPlan(plan Plan) error {
	if plan.ExecutionVersion == 0 {
		if plan.Execution != (ExecutionOptions{}) {
			return fmt.Errorf("%w: unversioned execution options", ErrInvalidPlan)
		}
		return nil
	}
	if plan.ExecutionVersion != ExecutionVersion {
		return fmt.Errorf("%w: execution version", ErrInvalidPlan)
	}
	canonical, err := CaptureExecution(plan, plan.Execution)
	if err != nil || canonical.Execution != plan.Execution {
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
	if plan.ExecutionVersion == ExecutionVersion {
		return plan.Execution.Threads, nil
	}
	if legacyThreads < 1 || legacyThreads > maxThreads {
		return 0, ErrInvalidThreads
	}
	return legacyThreads, nil
}
