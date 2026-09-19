package playback

import (
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// videoProcessingPlan derives a closed processing graph from probed source
// facts. Unknown HDR colorimetry cannot be repaired by guessing source tags.
func videoProcessingPlan(source media.Stream) (transcode.VideoFilters, *Reason) {
	return videoProcessingPlanForRange(source, "")
}

func videoProcessingPlanForRange(source media.Stream, outputRange string) (transcode.VideoFilters, *Reason) {
	var filters transcode.VideoFilters
	if media.VideoBitDepthConflict(source) {
		return filters, conversionReason("conversion_video_bit_depth_inconsistent", "VideoBitDepth", "Reported sample depth and decoded pixel format must agree before video processing.")
	}
	outputRange = strings.ToLower(outputRange)
	if outputRange != "" && outputRange != "sdr" && outputRange != "hdr10" {
		return filters, conversionReason("conversion_video_range_unsupported", "VideoRange", "The requested output range must be SDR or HDR10.")
	}
	if source.IsInterlaced || !source.InterlaceKnown {
		filters.Deinterlace = "auto"
		if source.IsInterlaced {
			switch strings.ToLower(source.FieldOrder) {
			case "tt", "bt":
				filters.Deinterlace = "tff"
			case "bb", "tb":
				filters.Deinterlace = "bff"
			}
		}
	}
	if media.EffectiveVideoBitDepth(source) == 10 || source.BitDepth == 0 && (source.PixelFormat == "yuv420p10le" || source.PixelFormat == "p010le") {
		filters.SourceBitDepth = 10
	}
	if !conversionHDR(&source) {
		if outputRange == "hdr10" {
			return transcode.VideoFilters{}, conversionReason("conversion_video_range_unsupported", "VideoRange", "SDR sources do not establish an HDR10 signal for conversion.")
		}
		return filters, nil
	}
	unsupported := func() (transcode.VideoFilters, *Reason) {
		return transcode.VideoFilters{}, conversionReason("conversion_hdr_colorimetry_unverified", "VideoRange", "HDR conversion requires PQ or HLG with known BT.2020 primaries, matrix, and range.")
	}
	if source.DolbyVision != nil || source.VideoRangeKnown && strings.EqualFold(source.VideoRange, "DOVI") {
		dv := source.DolbyVision
		if dv == nil || !strings.EqualFold(source.Codec, "hevc") || dv.Profile != 5 && dv.Profile != 7 && dv.Profile != 8 || !dv.RPUPresent || !dv.BLPresent ||
			!dv.RPUVerified || dv.RPUProfile != dv.Profile || dv.RPUResidualMixed || dv.RPUFrameCount <= 0 ||
			dv.Profile == 7 && dv.ResidualDisabled || dv.Profile != 7 && (!dv.ResidualDisabled || dv.ELPresent) ||
			dv.Profile == 5 && dv.CompatibilityID != 0 || dv.Profile == 7 && dv.CompatibilityID != 6 && dv.CompatibilityID != 1 || dv.Profile == 8 && dv.CompatibilityID != 1 && dv.CompatibilityID != 2 && dv.CompatibilityID != 4 ||
			media.EffectiveVideoBitDepth(source) != 10 {
			return transcode.VideoFilters{}, conversionReason("conversion_dolby_vision_unsupported", "VideoRange", "Dolby Vision conversion requires verified RPU metadata on every frame of HEVC profile 5, 7, or 8; the strict renderer accepts profile 7 zero-residual MEL and rejects FEL reconstruction.")
		}
		filters.Backend, filters.ToneMap, filters.DVProfile, filters.SourceBitDepth = "vulkan", "dolbyvision", dv.Profile, 10
		// Dolby Vision RPU reshaping supplies its own signal interpretation;
		// the reshaped image is BT.2020 PQ, including profile 5 IPT sources.
		filters.SourceTransfer, filters.SourcePrimaries, filters.SourceMatrix, filters.SourceRange = "smpte2084", "bt2020", "bt2020nc", "tv"
		filters.OutputRange = outputRange
		return filters, nil
	}
	transfer := strings.ToLower(source.ColorTransfer)
	switch transfer {
	case "smpte2084":
		filters.ToneMap = "hdr10"
	case "arib-std-b67":
		filters.ToneMap = "hlg"
	default:
		return unsupported()
	}
	// A Dolby Vision or contradictory declaration needs additional metadata
	// and processing. A PQ base layer alone is not proof of the declared image.
	if source.VideoRangeKnown {
		rangeName := strings.ToLower(source.VideoRange)
		if rangeName != "hdr10" && rangeName != "hdr10+" && rangeName != "hlg" && rangeName != "hdr" ||
			rangeName == "hlg" && filters.ToneMap != "hlg" || (rangeName == "hdr10" || rangeName == "hdr10+") && filters.ToneMap != "hdr10" {
			return unsupported()
		}
	}
	filters.SourceTransfer = transfer
	filters.SourcePrimaries = strings.ToLower(source.ColorPrimaries)
	filters.SourceMatrix = strings.ToLower(source.ColorSpace)
	filters.SourceRange = strings.ToLower(source.ColorRange)
	if filters.SourcePrimaries != "bt2020" || filters.SourceMatrix != "bt2020nc" && filters.SourceMatrix != "bt2020c" ||
		filters.SourceRange != "tv" && filters.SourceRange != "pc" {
		return unsupported()
	}
	filters.OutputRange = outputRange
	if outputRange == "hdr10" {
		filters.Backend = "vulkan"
	}
	return filters, nil
}

// selectVideoProcessingHardware chooses only implemented processing/codec
// combinations. A configured VAAPI adapter enables Vulkan color/composition
// work and VAAPI codecs, with explicit CPU transfers between those stages.
func selectVideoProcessingHardware(plan *transcode.Plan) {
	f := &plan.VideoFilters
	decode, encode := plan.Hardware.Decode, plan.Hardware.Encode
	amd := (decode == "" || decode == "software" || decode == "vaapi") && (encode == "" || encode == "software" || encode == "vaapi") && (decode == "vaapi" || encode == "vaapi" || plan.Hardware.Device != "")
	if f.ToneMap == "dolbyvision" {
		f.Backend = "vulkan"
		if amd {
			plan.Hardware.Decode = "software"
			if plan.Hardware.Device == "" {
				plan.Hardware.Device = "/dev/dri/renderD128"
			}
		} else {
			plan.Hardware = transcode.Hardware{}
		}
		return
	}
	if amd {
		if f.ToneMap != "" || plan.Subtitle.Mode == "burn" {
			f.Backend = "vulkan"
		} else if f.Deinterlace != "" {
			// The AMD VPP interface can enumerate deinterlacers that fail at
			// picture submission. The verified AMD path uses Vulkan YADIF.
			f.Backend = "vulkan"
		} else if f.SourceBitDepth != 0 {
			f.Backend = "vulkan"
		}
		return
	}
	if *f != (transcode.VideoFilters{}) || plan.Subtitle.Mode == "burn" {
		plan.Hardware = transcode.Hardware{}
	}
}

// videoProcessingOutput describes actual processed output. Source HDR
// tags must not leak into an SDR projection or remain in the encoded bitstream.
func videoProcessingOutput(video *media.Stream, filters transcode.VideoFilters) {
	video.IsInterlaced, video.InterlaceKnown, video.FieldOrder = false, true, "progressive"
	if filters.ToneMap != "" {
		if filters.OutputRange == "hdr10" {
			video.VideoRange, video.VideoRangeKnown = "HDR10", true
			video.ColorRange, video.ColorSpace, video.ColorTransfer, video.ColorPrimaries = "tv", "bt2020nc", "smpte2084", "bt2020"
		} else {
			video.VideoRange, video.VideoRangeKnown = "SDR", true
			video.ColorRange, video.ColorSpace, video.ColorTransfer, video.ColorPrimaries = "tv", "bt709", "bt709", "bt709"
		}
		video.DolbyVision = nil
	}
}
