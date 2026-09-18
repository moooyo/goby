package transcode

import (
	"fmt"
	"strconv"
	"strings"
)

// VideoFilters describes server-selected pixel transforms. Every value belongs
// to a closed enum; source tags never become arbitrary FFmpeg expressions.
// HDR transforms operate in linear floating-point light and produce limited
// range BT.709 SDR. They currently require software decoding and encoding.
type VideoFilters struct {
	Deinterlace     string `json:"Deinterlace,omitempty"`
	ToneMap         string `json:"ToneMap,omitempty"`
	SourceTransfer  string `json:"SourceTransfer,omitempty"`
	SourcePrimaries string `json:"SourcePrimaries,omitempty"`
	SourceMatrix    string `json:"SourceMatrix,omitempty"`
	SourceRange     string `json:"SourceRange,omitempty"`
}

// ValidateVideoFilters validates both the operation and its execution path.
// Compiled hardware filters do not prove that a device can execute this graph.
func ValidateVideoFilters(p Plan) error {
	f := p.VideoFilters
	invalid := func(field string) error { return fmt.Errorf("%w: video filters %s", ErrInvalidPlan, field) }
	if f == (VideoFilters{}) {
		return nil
	}
	decode, encode := hardwareSelection(p.Hardware)
	if p.VideoStreamIndex < 0 || p.VideoCodec != "h264" || decode != "software" || encode != "software" || p.Hardware.Device != "" {
		return invalid("require software video encoding")
	}
	switch f.Deinterlace {
	case "", "auto", "tff", "bff":
	default:
		return invalid("field parity")
	}
	if f.Deinterlace != "" && p.VideoSeekCandidate != "" {
		// A decoder restart proof does not cover the preceding frames required
		// by the temporal deinterlacer. Retain linear decode for this graph.
		return invalid("deinterlace restart history")
	}
	if f.ToneMap == "" {
		if f.SourceTransfer != "" || f.SourcePrimaries != "" || f.SourceMatrix != "" || f.SourceRange != "" {
			return invalid("unused source color metadata")
		}
		return nil
	}
	if f.ToneMap != "hdr10" && f.ToneMap != "hlg" || f.ToneMap == "hdr10" && f.SourceTransfer != "smpte2084" ||
		f.ToneMap == "hlg" && f.SourceTransfer != "arib-std-b67" {
		return invalid("HDR transfer")
	}
	if f.SourcePrimaries != "bt2020" || f.SourceMatrix != "bt2020nc" && f.SourceMatrix != "bt2020c" ||
		f.SourceRange != "tv" && f.SourceRange != "pc" {
		return invalid("source color metadata")
	}
	return nil
}

// VideoFilterRequirements names compiled filters needed by a selected software
// path. A capability response may report these interfaces, but cannot claim GPU
// execution or deployment verification from the filter names alone.
func VideoFilterRequirements(f VideoFilters) []string {
	result := []string{"scale", "format"}
	if f.Deinterlace != "" {
		result = append(result, "bwdif")
	}
	if f.ToneMap != "" {
		result = append(result, "zscale", "tonemap", "setparams", "sidedata")
	}
	return result
}

// softwareVideoProcessingFilter preserves the source canvas. Bitmap subtitle
// compositing can follow these pixel transforms before a shared final resize.
func softwareVideoProcessingFilter(p Plan) string {
	var filters []string
	f := p.VideoFilters
	if f.Deinterlace != "" {
		// One progressive output per input frame preserves the source cadence.
		// Explicit parity uses display order, not the order fields were coded.
		filters = append(filters, "bwdif=mode=send_frame:parity="+f.Deinterlace+":deint=all")
	}
	if f.ToneMap != "" {
		matrix, sourceRange := "2020_ncl", "limited"
		if f.SourceMatrix == "bt2020c" {
			matrix = "2020_cl"
		}
		if f.SourceRange == "pc" {
			sourceRange = "full"
		}
		// zscale first decodes the source transfer in its declared gamut. The
		// floating RGB conversion and gamut transform precede tone mapping;
		// merely relabeling PQ/HLG pixels as BT.709 would not create SDR.
		filters = append(filters,
			"zscale=transferin="+f.SourceTransfer+":primariesin=2020:matrixin="+matrix+":rangein="+sourceRange+":transfer=linear:npl=100",
			"format=gbrpf32le", "zscale=primaries=709", "tonemap=tonemap=hable:desat=2.0",
			"zscale=primaries=709:transfer=709:matrix=709:range=limited:dither=error_diffusion",
			"format=yuv420p",
			"sidedata=mode=delete:type=MASTERING_DISPLAY_METADATA",
			"sidedata=mode=delete:type=CONTENT_LIGHT_LEVEL",
			"sidedata=mode=delete:type=DYNAMIC_HDR_PLUS",
			"setparams=range=limited:color_primaries=bt709:color_trc=bt709:colorspace=bt709")
	}
	return strings.Join(filters, ",")
}

func softwareVideoFilter(p Plan) string {
	filters := []string{}
	if processing := softwareVideoProcessingFilter(p); processing != "" {
		filters = append(filters, processing)
	}
	width, height := "trunc(iw/2)*2", "trunc(ih/2)*2"
	if p.Width > 0 {
		width, height = strconv.Itoa(p.Width), strconv.Itoa(p.Height)
	}
	filters = append(filters, "scale=w="+width+":h="+height, "format=yuv420p")
	return strings.Join(filters, ",")
}

// AppendVideoColorArgs publishes only metadata guaranteed by pixel processing.
// These arguments complement the filter graph; they never replace tone mapping.
func AppendVideoColorArgs(args []string, p Plan) []string {
	if p.VideoFilters.Deinterlace != "" {
		args = append(args, "-field_order", "progressive")
	}
	if p.VideoFilters.ToneMap != "" {
		args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv")
	}
	return args
}
