package transcode

import (
	"fmt"
	"strconv"
)

// VideoEncodingSupported reports codecs with complete output argument paths.
// It does not assert that an encoder is installed or usable on a given GPU.
func VideoEncodingSupported(codec string) bool {
	return codec == "h264" || codec == "hevc" || codec == "av1"
}

// VideoEncoder returns the selected FFmpeg implementation, or an empty string
// for a combination outside the supported implementation matrix.
func VideoEncoder(codec, backend string) string {
	if backend == "" {
		backend = "software"
	}
	if !VideoEncodingSupported(codec) {
		return ""
	}
	if backend == "software" {
		switch codec {
		case "h264":
			return "libx264"
		case "hevc":
			return "libx265"
		case "av1":
			return "libaom-av1"
		}
	}
	if backend == "vaapi" || codec == "h264" && (backend == "qsv" || backend == "nvenc") {
		return codec + "_" + backend
	}
	return ""
}

// VideoOutputCodec returns the actual coded video in the output. Historical
// copy plans predate the explicit source codec and retain their H.264 contract.
func VideoOutputCodec(p Plan) string {
	if p.VideoCodec != "copy" {
		return p.VideoCodec
	}
	if p.VideoCopyCodec == "" {
		return "h264"
	}
	return p.VideoCopyCodec
}

// VideoOutputBitDepth resolves the output format default. Copy plans deliberately
// do not describe a pixel conversion and must leave VideoBitDepth unspecified.
func VideoOutputBitDepth(p Plan) int {
	if p.VideoBitDepth == 0 {
		return 8
	}
	return p.VideoBitDepth
}

// VideoOutputProfile resolves the profile represented by the output pixels.
func VideoOutputProfile(p Plan) string {
	if p.VideoProfile != "" {
		return p.VideoProfile
	}
	switch p.VideoCodec {
	case "h264":
		return "high"
	case "hevc":
		if VideoOutputBitDepth(p) == 10 {
			return "main10"
		}
		return "main"
	case "av1":
		return "main"
	}
	return ""
}

func validateVideoEncoding(p Plan) error {
	invalid := func(field string) error { return fmt.Errorf("%w: video %s", ErrInvalidPlan, field) }
	if p.VideoCodec == "copy" {
		if p.VideoCopyCodec != "" && !VideoEncodingSupported(p.VideoCopyCodec) {
			return invalid("copy codec")
		}
	} else if p.VideoCopyCodec != "" {
		return invalid("unused copy codec")
	}
	if !VideoEncodingSupported(p.VideoCodec) {
		if p.VideoProfile != "" || p.VideoBitDepth != 0 {
			return invalid("encoding options without encoder")
		}
		return nil
	}
	depth := VideoOutputBitDepth(p)
	if depth != 8 && depth != 10 || p.VideoCodec == "h264" && depth != 8 {
		return invalid("bit depth")
	}
	profile := VideoOutputProfile(p)
	switch p.VideoCodec {
	case "h264":
		if profile != "baseline" && profile != "main" && profile != "high" {
			return invalid("H.264 profile")
		}
	case "hevc":
		if depth == 8 && profile != "main" || depth == 10 && profile != "main10" {
			return invalid("HEVC profile and bit depth")
		}
	case "av1":
		if profile != "main" {
			return invalid("AV1 profile")
		}
	}
	_, encode := hardwareSelection(p.Hardware)
	if VideoEncoder(p.VideoCodec, encode) == "" {
		return invalid("encoder backend")
	}
	return nil
}

func videoSoftwarePixelFormat(p Plan) string {
	if VideoOutputBitDepth(p) == 10 {
		return "yuv420p10le"
	}
	return "yuv420p"
}

// VideoMP4Tag returns a sample entry consistent with parameter-set placement.
// Copied and VAAPI-encoded HEVC can carry authoritative in-band parameter sets.
// hvc1 would make the MP4 muxer discard those sets, so these paths use hev1.
func VideoMP4Tag(p Plan) string {
	switch VideoOutputCodec(p) {
	case "hevc":
		_, encode := hardwareSelection(p.Hardware)
		if p.VideoCodec == "copy" || encode == "vaapi" {
			return "hev1"
		}
		return "hvc1"
	case "av1":
		return "av01"
	default:
		return "avc1"
	}
}

func appendVideoEncoderOptions(args []string, p Plan, encode string, threads int) []string {
	args = append(args, "-profile:v", VideoOutputProfile(p))
	if encode == "software" {
		args = append(args, "-pix_fmt", videoSoftwarePixelFormat(p))
		switch p.VideoCodec {
		case "h264":
			return append(args, "-preset", "veryfast", "-sc_threshold", "0", "-flags", "+cgop")
		case "hevc":
			// x265 creates its own pools unless explicitly constrained. Closed
			// GOPs and forced IDRs make each advertised HLS cut independently
			// decodable. hvc1 stores parameter sets in hvcC, never in samples.
			repeatHeaders := "1"
			if p.Container == "mp4" {
				repeatHeaders = "0"
			}
			return append(args, "-preset", "veryfast", "-forced-idr", "1", "-flags", "+cgop",
				"-x265-params", "pools=none:frame-threads="+strconv.Itoa(min(threads, 16))+":wpp=0:open-gop=0:scenecut=0:repeat-headers="+repeatHeaders)
		case "av1":
			// libaom honors arbitrary forced frame timestamps. libsvtav1 does
			// not, so selecting it here would silently break HLS segment cuts.
			return append(args, "-usage", "realtime", "-cpu-used", "8", "-row-mt", "1", "-lag-in-frames", "0")
		}
	}
	switch encode {
	case "vaapi":
		args = append(args, "-flags", "+cgop", "-idr_interval", "0")
		if p.VideoCodec == "h264" && VideoOutputProfile(p) == "baseline" {
			args = append(args, "-coder", "cavlc")
		}
		return args
	case "qsv":
		return append(args, "-idr_interval", "0", "-forced_idr", "1")
	case "nvenc":
		return append(args, "-forced-idr", "1")
	}
	return args
}
