package playback

import (
	"errors"

	"github.com/moooyo/goby/internal/transcode"
)

// captureConversionExecution runs after the selected filter and encoder graph
// is known. Rejection never projects unconverted HDR as an SDR output.
func captureConversionExecution(plan *transcode.Plan, limits ConversionLimits) *Reason {
	if limits.AdmittedVideoEncoding == (AdmittedVideoEncoding{}) && !transcode.ConfiguredVideoProfileAllowed(limits.CodecProfileLimits, *plan) {
		return conversionReason("conversion_profile_limit", "CodecProfileLimits", "The selected encoded video profile exceeds the configured server limit.")
	}
	if limits.DisableHevcEncoding && plan.VideoCodec == "hevc" {
		return conversionReason("conversion_hevc_encoding_disabled", "EnableHevcEncoding", "HEVC encoding is disabled for new conversion admissions.")
	}
	// Availability belongs to the selected execution profile, before a missing
	// device path can make graph selection look like an ordinary CPU profile.
	// This also covers software codecs paired with a selected Vulkan device.
	// Copy and audio candidates do not depend on that video execution profile.
	finalizeVideoProcessingHardware(plan)
	if limits.HardwareUnavailable && transcode.VideoEncodingSupported(plan.VideoCodec) &&
		(!limits.DisableHardwareEncoding || conversionHardwareRequired(*plan)) {
		return conversionReason("conversion_hardware_unavailable", "HardwareEncoding", "The selected video execution profile has an unavailable authorized device.")
	}
	if reason := configuredVideoEncodingReason(*plan, limits.CodecConfigurations); reason != nil {
		return reason
	}
	// Legacy planner callers did not supply an execution context. Preserve
	// that absence until the manager captures its explicit admission default;
	// choosing two here could override such a caller's configured thread count.
	if limits.Execution == (transcode.ExecutionOptions{}) {
		return nil
	}
	captured, err := transcode.CaptureExecution(*plan, limits.Execution)
	if errors.Is(err, transcode.ErrToneMappingDisabled) {
		return conversionReason("conversion_tone_mapping_disabled", "VideoRange", "The tone mapping backend required by this output is disabled.")
	}
	if err != nil {
		return conversionReason("conversion_execution_invalid", "TranscodingProfiles", "The execution settings are outside the supported closed contract.")
	}
	*plan = captured
	return nil
}

func conversionHardwareRequired(plan transcode.Plan) bool {
	return plan.Hardware.Decode != "" && plan.Hardware.Decode != "software" ||
		plan.Hardware.Encode != "" && plan.Hardware.Encode != "software" ||
		plan.VideoFilters.Backend == "vaapi" || plan.VideoFilters.Backend == "vulkan"
}
