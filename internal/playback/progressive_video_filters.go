package playback

import (
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// videoProcessingPlan derives a closed processing graph from probed source
// facts. Unknown HDR colorimetry cannot be repaired by guessing source tags.
func videoProcessingPlan(source media.Stream) (transcode.VideoFilters, *Reason) {
	var filters transcode.VideoFilters
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
	if !conversionHDR(&source) {
		return filters, nil
	}
	unsupported := func() (transcode.VideoFilters, *Reason) {
		return transcode.VideoFilters{}, conversionReason("conversion_hdr_colorimetry_unverified", "VideoRange", "HDR conversion requires PQ or HLG with known BT.2020 primaries, matrix, and range.")
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
	return filters, nil
}

// videoProcessingOutput describes actual software filter output. Source HDR
// tags must not leak into an SDR projection or remain in the encoded bitstream.
func videoProcessingOutput(video *media.Stream, filters transcode.VideoFilters) {
	video.IsInterlaced, video.InterlaceKnown, video.FieldOrder = false, true, "progressive"
	if filters.ToneMap != "" {
		video.VideoRange, video.VideoRangeKnown = "SDR", true
		video.ColorRange, video.ColorSpace, video.ColorTransfer, video.ColorPrimaries = "tv", "bt709", "bt709", "bt709"
	}
}
