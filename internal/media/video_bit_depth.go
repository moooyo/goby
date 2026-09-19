package media

import "strings"

// EffectiveVideoBitDepth preserves a reported sample depth and supplements a
// missing HEVC/AV1 report only from an explicitly known decoded pixel format.
// It never mutates Stream: BitDepth remains the raw probe/cache fact. A positive
// result is not evidence of HDR, a codec profile, or hardware support. Callers
// making compatibility decisions must also reject VideoBitDepthConflict.
func EffectiveVideoBitDepth(stream Stream) int {
	if stream.BitDepth != 0 {
		return stream.BitDepth
	}
	return knownModernVideoPixelDepth(stream)
}

// VideoBitDepthConflict reports contradictory sample and decoded-format facts.
// The reported value remains available for faithful metadata projection, but
// copying or processing must not silently treat this contradiction as proof.
func VideoBitDepthConflict(stream Stream) bool {
	depth := knownModernVideoPixelDepth(stream)
	return stream.BitDepth > 0 && depth > 0 && stream.BitDepth != depth
}

func knownModernVideoPixelDepth(stream Stream) int {
	if !strings.EqualFold(stream.CodecType, "video") || stream.IsAttachedPicture ||
		!strings.EqualFold(stream.Codec, "hevc") && !strings.EqualFold(stream.Codec, "av1") {
		return 0
	}
	// Match complete FFmpeg format names. A prefix, suffix, profile name, or
	// hardware surface token does not establish decoded component precision.
	switch strings.ToLower(stream.PixelFormat) {
	case "yuv420p", "nv12":
		return 8
	case "yuv420p10le", "yuv420p10be", "p010le", "p010be":
		return 10
	default:
		return 0
	}
}
