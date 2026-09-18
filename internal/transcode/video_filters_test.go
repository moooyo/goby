package transcode

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func videoFilterHDRPlan() Plan {
	p := commandPlan()
	p.VideoFilters = VideoFilters{Deinterlace: "bff", ToneMap: "hdr10", SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
	return p
}

func TestVideoFiltersEncodeActualLinearLightTransformAndOutputTags(t *testing.T) {
	for _, progressive := range []bool{false, true} {
		p := videoFilterHDRPlan()
		if progressive {
			p.OutputMode, p.Container, p.SegmentSeconds = "progressive", "mp4", 0
			p.SourceFormatStartKnown = true
		}
		args, err := BuildArgs(p, 1)
		if err != nil {
			t.Fatal(err)
		}
		position := slices.Index(args, "-vf")
		if position < 0 || position+1 >= len(args) {
			t.Fatalf("pixel transformations were not emitted: %v", args)
		}
		filter := args[position+1]
		previous := -1
		for _, stage := range []string{"bwdif=mode=send_frame:parity=bff:deint=all", "transferin=smpte2084", "transfer=linear", "format=gbrpf32le", "zscale=primaries=709", "tonemap=tonemap=hable", "transfer=709:matrix=709:range=limited", "MASTERING_DISPLAY_METADATA", "CONTENT_LIGHT_LEVEL", "DYNAMIC_HDR_PLUS", "setparams=range=limited", "scale=w=160:h=90"} {
			index := strings.Index(filter, stage)
			if index <= previous {
				t.Fatalf("missing or reordered transform %q: %s", stage, filter)
			}
			previous = index
		}
		for _, pair := range [][2]string{{"-color_primaries", "bt709"}, {"-color_trc", "bt709"}, {"-colorspace", "bt709"}, {"-color_range", "tv"}, {"-field_order", "progressive"}, {"-c:v", "libx264"}} {
			if !hasArgumentPair(args, pair[0], pair[1]) {
				t.Fatalf("output metadata does not describe transformed pixels: %v", args)
			}
		}
		if slices.Contains(args, "-hwaccel") || slices.Contains(args, "-init_hw_device") {
			t.Fatal("software HDR transform unexpectedly selected a hardware device")
		}
	}
}

func TestVideoFiltersRejectUnprovenOrInjectedExecutionPaths(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*Plan)
	}{
		{"copy", func(p *Plan) { p.VideoCodec = "copy" }},
		{"audio only", func(p *Plan) { p.VideoStreamIndex, p.VideoCodec = -1, "" }},
		{"hardware decode", func(p *Plan) { p.Hardware.Decode = "vaapi" }},
		{"hardware encode", func(p *Plan) { p.Hardware.Encode = "nvenc" }},
		{"software device", func(p *Plan) { p.Hardware.Device = "0" }},
		{"filter expression", func(p *Plan) { p.VideoFilters.Deinterlace = "tff,scale=2:2" }},
		{"transfer mismatch", func(p *Plan) { p.VideoFilters.ToneMap = "hlg" }},
		{"source transfer", func(p *Plan) { p.VideoFilters.SourceTransfer = "bt709" }},
		{"unknown primaries", func(p *Plan) { p.VideoFilters.SourcePrimaries = "" }},
		{"unknown matrix", func(p *Plan) { p.VideoFilters.SourceMatrix = "" }},
		{"unknown range", func(p *Plan) { p.VideoFilters.SourceRange = "" }},
		{"unused metadata", func(p *Plan) { p.VideoFilters.ToneMap = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := videoFilterHDRPlan()
			test.mutate(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unverified transform accepted: %+v, %v", p, err)
			}
		})
	}
}

func TestVideoFiltersHLGFullRangeAndDeinterlaceCadence(t *testing.T) {
	p := videoFilterHDRPlan()
	p.VideoFilters.ToneMap, p.VideoFilters.SourceTransfer = "hlg", "arib-std-b67"
	p.VideoFilters.SourceMatrix, p.VideoFilters.SourceRange = "bt2020c", "pc"
	p.VideoFilters.Deinterlace = "tff"
	if err := ValidatePlan(p); err != nil {
		t.Fatal(err)
	}
	filter := softwareVideoFilter(p)
	if !strings.Contains(filter, "transferin=arib-std-b67:primariesin=2020:matrixin=2020_cl:rangein=full") || !strings.Contains(filter, "mode=send_frame:parity=tff") {
		t.Fatalf("the source transfer, range, or field order was replaced: %s", filter)
	}
	p.VideoFilters = VideoFilters{Deinterlace: "auto"}
	if got := softwareVideoFilter(p); got != "bwdif=mode=send_frame:parity=auto:deint=all,scale=w=160:h=90,format=yuv420p" {
		t.Fatalf("ordinary deinterlacing changed colorimetry or cadence: %s", got)
	}
	args := AppendVideoColorArgs(nil, p)
	if slices.Contains(args, "-color_trc") {
		t.Fatal("deinterlacing alone fabricated SDR color metadata")
	}
}

func TestVideoFiltersConvertSourcePixelsBeforeSubtitleCompositing(t *testing.T) {
	for _, codec := range []string{"subrip", "hdmv_pgs_subtitle"} {
		t.Run(codec, func(t *testing.T) {
			p := videoFilterHDRPlan()
			p.Subtitle = SubtitlePlan{Mode: "burn", Codec: codec, StreamIndex: 2}
			args, err := BuildArgs(p, 1)
			if err != nil {
				t.Fatal(err)
			}
			option, compositor := "-vf", "subtitles="
			if codec == "hdmv_pgs_subtitle" {
				option, compositor = "-filter_complex", "overlay="
			}
			position := slices.Index(args, option)
			if position < 0 || position+1 >= len(args) {
				t.Fatalf("missing subtitle composition graph: %v", args)
			}
			graph := args[position+1]
			deinterlace, toneMap, subtitle := strings.Index(graph, "bwdif="), strings.Index(graph, "tonemap="), strings.Index(graph, compositor)
			if deinterlace < 0 || toneMap <= deinterlace || subtitle <= toneMap {
				t.Fatalf("subtitle pixels entered the HDR or deinterlace source transform: %s", graph)
			}
		})
	}
}
