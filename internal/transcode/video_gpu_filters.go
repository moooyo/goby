package transcode

import (
	"strconv"
	"strings"
)

func videoSoftwareFormat(p Plan) string {
	if VideoOutputBitDepth(p) == 10 {
		return "yuv420p10le"
	}
	return "yuv420p"
}

func videoHardwareFormat(p Plan) string {
	if VideoOutputBitDepth(p) == 10 {
		return "p010le"
	}
	return "nv12"
}

func sourceHardwareFormat(p Plan) string {
	if p.VideoFilters.SourceBitDepth == 10 {
		return "p010le"
	}
	return "nv12"
}

func videoDimensions(p Plan) (string, string) {
	if p.Width > 0 {
		return strconv.Itoa(p.Width), strconv.Itoa(p.Height)
	}
	return "trunc(iw/2)*2", "trunc(ih/2)*2"
}

func videoUploadFilter(p Plan) string {
	if p.VideoFilters.Backend == "vulkan" {
		// The Vulkan filter device descends from the same DRM device as VAAPI.
		// FFmpeg derives the upload device through that ancestry, avoiding an
		// implicit default adapter or unsupported direct frame interop.
		return "hwupload=derive_device=vaapi:extra_hw_frames=64"
	}
	return "hwupload=extra_hw_frames=64"
}

func videoOutputFilter(p Plan, encode string, resize bool) string {
	var filters []string
	if resize {
		width, height := videoDimensions(p)
		filters = append(filters, "scale=w="+width+":h="+height)
	}
	if encode == "software" {
		filters = append(filters, "format="+videoSoftwareFormat(p))
	} else {
		filters = append(filters, "format="+videoHardwareFormat(p), videoUploadFilter(p))
	}
	return strings.Join(filters, ",")
}

// videoCanvasFilter returns source-sized software frames after the actual pixel
// transforms. CPU subtitle rasterization and GPU composition share this canvas;
// an explicit final upload supplies VAAPI encoding without claiming zero copy.
func videoCanvasFilter(p Plan, decode string) string {
	f := p.VideoFilters
	var filters []string
	if f.Backend == "vaapi" {
		if decode == "software" {
			filters = append(filters, "format="+sourceHardwareFormat(p), videoUploadFilter(p))
		}
		if f.Deinterlace == "tff" || f.Deinterlace == "bff" {
			filters = append(filters, "setfield=mode="+f.Deinterlace)
		}
		filters = append(filters, "deinterlace_vaapi=mode=default:rate=frame:auto=0", "hwdownload", "format="+sourceHardwareFormat(p))
		return strings.Join(filters, ",")
	}
	if decode != "software" {
		filters = append(filters, "hwdownload", "format="+sourceHardwareFormat(p))
	}
	if f.Backend != "vulkan" {
		if processing := softwareVideoProcessingFilter(p); processing != "" {
			filters = append(filters, processing)
		}
		return strings.Join(filters, ",")
	}
	if f.ToneMap == "" && f.Deinterlace == "" {
		return strings.Join(filters, ",")
	}
	if f.Deinterlace == "tff" || f.Deinterlace == "bff" {
		filters = append(filters, "setfield=mode="+f.Deinterlace)
	}
	if f.ToneMap != "" && f.ToneMap != "dolbyvision" {
		filters = append(filters, "setparams=range="+f.SourceRange+":color_primaries="+f.SourcePrimaries+":color_trc="+f.SourceTransfer+":colorspace="+f.SourceMatrix)
	}
	options := "libplacebo=format=" + videoSoftwareFormat(p)
	if f.Deinterlace != "" {
		// Preserve input cadence; setfield provides explicit display parity.
		options += ":deinterlace=yadif:send_fields=0"
	}
	if f.ToneMap != "" {
		options += libplaceboOutputColor(p)
		if f.ToneMap == "dolbyvision" {
			// The pinned FFmpeg patch rejects missing RPU/FEL data and safely
			// normalizes only a verified zero-residual MEL metadata copy.
			options += ":apply_dolbyvision=1:strict_dolbyvision=1:strict_dolbyvision_profile=" + strconv.Itoa(f.DVProfile)
		} else {
			options += ":apply_dolbyvision=0"
		}
		options += ":tonemapping=bt.2390:peak_detect=0"
	}
	filters = append(filters, options, "format="+videoSoftwareFormat(p))
	if f.ToneMap != "" {
		// Do not pass source mastering metadata or dynamic RPU/HDR10+ records
		// into a new encoding after their pixel interpretation has changed.
		filters = append(filters, "sidedata=mode=delete:type=MASTERING_DISPLAY_METADATA", "sidedata=mode=delete:type=CONTENT_LIGHT_LEVEL", "sidedata=mode=delete:type=DYNAMIC_HDR_PLUS", "sidedata=mode=delete:type=DOVI_RPU_BUFFER", "sidedata=mode=delete:type=DOVI_METADATA")
	}
	return strings.Join(filters, ",")
}

func libplaceboOutputColor(p Plan) string {
	if p.VideoFilters.OutputRange == "hdr10" {
		return ":colorspace=bt2020nc:color_primaries=bt2020:color_trc=smpte2084:range=tv"
	}
	return ":colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv"
}

func gpuSubtitleComposition(p Plan) bool {
	return p.Subtitle.Mode == "burn" && p.VideoFilters.Backend == "vulkan"
}
