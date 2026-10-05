package media

import (
	"fmt"
	"strconv"
	"strings"
)

// DolbyVisionConversionSupported checks scanned configuration and per-access-
// unit RPU evidence. The strict renderer must still validate every decoded frame
// and reject nonzero profile 7 residuals before publishing transformed pixels.
func DolbyVisionConversionSupported(source Stream) bool {
	dv := source.DolbyVision
	return dv != nil && strings.EqualFold(source.Codec, "hevc") &&
		(dv.Profile == 5 || dv.Profile == 7 || dv.Profile == 8) && dv.RPUPresent && dv.BLPresent &&
		dv.RPUVerified && dv.RPUProfile == dv.Profile && !dv.RPUResidualMixed && dv.RPUFrameCount > 0 &&
		(dv.Profile == 7 && !dv.ResidualDisabled || dv.Profile != 7 && dv.ResidualDisabled && !dv.ELPresent) &&
		(dv.Profile == 5 && dv.CompatibilityID == 0 || dv.Profile == 7 && (dv.CompatibilityID == 6 || dv.CompatibilityID == 1) ||
			dv.Profile == 8 && (dv.CompatibilityID == 1 || dv.CompatibilityID == 2 || dv.CompatibilityID == 4)) &&
		!VideoBitDepthConflict(source) && EffectiveVideoBitDepth(source) == 10
}

// DolbyVisionRenderOptions describes only closed, server-selected transforms.
// Decoding must remain software HEVC to retain raw and parsed per-frame RPU.
type DolbyVisionRenderOptions struct {
	Profile        int
	OutputBitDepth int
	HDR10          bool
	Deinterlace    bool
}

// StrictDolbyVisionFilter is shared by playback and finite background generation.
// It requires Goby's private FFmpeg contract; an upstream permissive renderer
// cannot silently substitute for missing RPU or unsupported FEL reconstruction.
func StrictDolbyVisionFilter(options DolbyVisionRenderOptions) (string, error) {
	if options.Profile != 5 && options.Profile != 7 && options.Profile != 8 ||
		options.OutputBitDepth != 8 && options.OutputBitDepth != 10 || options.HDR10 && options.OutputBitDepth != 10 {
		return "", fmt.Errorf("invalid strict Dolby Vision render options")
	}
	pixel := "yuv420p"
	if options.OutputBitDepth == 10 {
		pixel = "yuv420p10le"
	}
	filter := "libplacebo=format=" + pixel
	if options.Deinterlace {
		filter += ":deinterlace=yadif:send_fields=0"
	}
	if options.HDR10 {
		filter += ":colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084:range=tv"
	} else {
		filter += ":colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv"
	}
	return filter + ":apply_dolbyvision=1:strict_dolbyvision=1:strict_dolbyvision_profile=" + strconv.Itoa(options.Profile) +
		":tonemapping=bt.2390:peak_detect=0", nil
}
