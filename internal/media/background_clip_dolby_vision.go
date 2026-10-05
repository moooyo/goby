package media

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrBackgroundClipDolbyVisionUnavailable = errors.New("background Dolby Vision processing is unavailable")

// BackgroundClipDolbyVisionOptions is runtime policy, not a persisted media
// profile. Nil disables this path without changing ordinary SDR generation.
// Device must be an explicitly admitted DRM render node. New compatibility
// profiles have independent gates for real-source and deployment acceptance.
type BackgroundClipDolbyVisionOptions struct {
	Device         string
	AllowProfile5  bool
	AllowProfile84 bool
	AllowProfile82 bool
}

func backgroundClipDolbyVisionFilter(stream Stream, options *BackgroundClipDolbyVisionOptions) (string, error) {
	if !DolbyVisionConversionSupported(stream) {
		return "", fmt.Errorf("%w: unverified Dolby Vision source", ErrBackgroundClipUnsupported)
	}
	dv := stream.DolbyVision
	if options == nil || !backgroundClipVulkanDeviceValid(options.Device) {
		return "", ErrBackgroundClipDolbyVisionUnavailable
	}
	allowed := false
	switch dv.Profile {
	case 5:
		allowed = dv.CompatibilityID == 0 && options.AllowProfile5
	case 7:
		// The strict renderer still verifies every frame's zero-residual MEL
		// identity. Neither these options nor the scan admit FEL reconstruction.
		allowed = dv.ELPresent && (dv.CompatibilityID == 1 || dv.CompatibilityID == 6)
	case 8:
		switch dv.CompatibilityID {
		case 1:
			allowed = true
		case 4:
			allowed = options.AllowProfile84
		case 2:
			allowed = options.AllowProfile82
		}
	}
	if !allowed {
		return "", fmt.Errorf("%w: background Dolby Vision compatibility profile is not enabled", ErrBackgroundClipUnsupported)
	}
	// Consume native RPU curves and matrices before geometry or cadence changes.
	// P5 IPTPQc2, P8.4 HLG, and P8.2 SDR base pixels must not be relabeled as PQ.
	filter, err := StrictDolbyVisionFilter(DolbyVisionRenderOptions{Profile: dv.Profile, OutputBitDepth: 8, Deinterlace: stream.IsInterlaced})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrBackgroundClipUnsupported, err)
	}
	return filter + ",format=yuv420p", nil
}

func backgroundClipVulkanDeviceValid(device string) bool {
	const prefix = "/dev/dri/renderD"
	if !strings.HasPrefix(device, prefix) {
		return false
	}
	number, err := strconv.Atoi(strings.TrimPrefix(device, prefix))
	return err == nil && number >= 128 && number <= 255 && device == prefix+strconv.Itoa(number)
}

func backgroundClipVulkanDeviceArgs(device string) []string {
	return []string{"-init_hw_device", "drm=gobydrm:" + device,
		"-init_hw_device", "vulkan=gobyvk@gobydrm", "-filter_hw_device", "gobyvk"}
}
