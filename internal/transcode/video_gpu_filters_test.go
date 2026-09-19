package transcode

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestVulkanHDRPipelineBindsOneDeviceAndTransfersActualPixels(t *testing.T) {
	p := videoFilterHDRPlan()
	p.VideoFilters.Backend, p.VideoFilters.SourceBitDepth = "vulkan", 10
	p.Hardware = Hardware{Decode: "vaapi", Encode: "vaapi", Device: "/dev/dri/renderD129"}
	args, err := BuildArgs(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-init_hw_device", "drm=gobydrm:/dev/dri/renderD129"}, {"-init_hw_device", "vaapi=goby@gobydrm"}, {"-init_hw_device", "vulkan=gobyvk@gobydrm"}, {"-filter_hw_device", "gobyvk"}, {"-hwaccel_device", "goby"}, {"-c:v", "h264_vaapi"}} {
		if !hasArgumentPair(args, pair[0], pair[1]) {
			t.Fatalf("processing selected an unrelated or implicit adapter: %v", args)
		}
	}
	filter := args[slices.Index(args, "-vf")+1]
	ordered := []string{"hwdownload,format=p010le", "setfield=mode=bff", "libplacebo=", "deinterlace=yadif:send_fields=0", "color_trc=bt709", "DOVI_METADATA", "scale=w=160:h=90", "format=nv12,hwupload=derive_device=vaapi"}
	previous := -1
	for _, stage := range ordered {
		index := strings.Index(filter, stage)
		if index <= previous {
			t.Fatalf("stage %q did not execute in the required order: %s", stage, filter)
		}
		previous = index
	}
}

func TestVAAPIDeinterlacingPreservesParityAndFrameCadence(t *testing.T) {
	p := commandPlan()
	p.VideoFilters = VideoFilters{Backend: "vaapi", Deinterlace: "tff", SourceBitDepth: 10}
	p.Hardware = Hardware{Decode: "software", Encode: "vaapi"}
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	filter := args[slices.Index(args, "-vf")+1]
	if !strings.Contains(filter, "format=p010le,hwupload=extra_hw_frames=64,setfield=mode=tff,deinterlace_vaapi=mode=default:rate=frame:auto=0,hwdownload,format=p010le") || strings.Contains(filter, "bwdif") || slices.Contains(args, "-color_trc") {
		t.Fatalf("VAAPI deinterlace changed its backend, cadence, or color contract: %s", filter)
	}
}

func TestDolbyVisionRequiresRPUDecodePathAndRealTenBitHDROutput(t *testing.T) {
	p := commandPlan()
	p.VideoCodec, p.VideoBitDepth = "hevc", 10
	p.VideoFilters = VideoFilters{Backend: "vulkan", ToneMap: "dolbyvision", DVProfile: 7, SourceBitDepth: 10, OutputRange: "hdr10", SourceTransfer: "smpte2084", SourcePrimaries: "bt2020", SourceMatrix: "bt2020nc", SourceRange: "tv"}
	p.Hardware = Hardware{Decode: "software", Encode: "vaapi"}
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	filter := args[slices.Index(args, "-vf")+1]
	if !strings.Contains(filter, "apply_dolbyvision=1") || !strings.Contains(filter, "color_trc=smpte2084") || !strings.Contains(filter, "format=p010le") || slices.Contains(args, "-hwaccel") || !hasArgumentPair(args, "-color_trc", "smpte2084") {
		t.Fatalf("Dolby Vision output lacks actual RPU processing or HDR pixels: %v", args)
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.Hardware.Decode = "vaapi" },
		func(p *Plan) { p.VideoBitDepth = 8 },
		func(p *Plan) { p.VideoFilters.Backend = "" },
		func(p *Plan) { p.VideoFilters.DVProfile = 4 },
		func(p *Plan) { p.VideoFilters.SourceBitDepth = 8 },
		func(p *Plan) { p.VideoFilters.OutputRange = "hdr10,format=rgba" },
	} {
		changed := p
		mutate(&changed)
		if err := ValidatePlan(changed); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("unproven Dolby Vision pipeline accepted: %+v, %v", changed, err)
		}
	}
}

func TestGPUSubtitlesCompositeAfterSourceColorTransform(t *testing.T) {
	for _, progressive := range []bool{false, true} {
		for _, codec := range []string{"ass", "hdmv_pgs_subtitle"} {
			p := videoFilterHDRPlan()
			p.VideoFilters.Backend, p.VideoFilters.SourceBitDepth = "vulkan", 10
			p.Hardware = Hardware{Decode: "vaapi", Encode: "vaapi"}
			p.Subtitle = SubtitlePlan{Mode: "burn", Codec: codec, StreamIndex: 2, OffsetTicks: ticksPerSecond}
			p.StartTicks = 2 * ticksPerSecond
			if progressive {
				p.OutputMode, p.Container, p.SegmentSeconds, p.SourceFormatStartKnown = "progressive", "mp4", 0, true
			}
			args, err := BuildArgs(p, 1)
			if err != nil {
				t.Fatal(err)
			}
			position := slices.Index(args, "-filter_complex")
			if position < 0 || slices.Contains(args, "-vf") {
				t.Fatalf("GPU subtitle composition was not mapped exactly once: %v", args)
			}
			graph := args[position+1]
			if strings.Index(graph, "libplacebo=inputs=2") <= strings.Index(graph, "color_trc=bt709") || !strings.Contains(graph, "hwupload=derive_device=vaapi") || !hasArgumentPair(args, "-map", "[goby_video]") {
				t.Fatalf("subtitle composition lost GPU execution or entered source tone mapping: %s", graph)
			}
			if codec == "ass" && (!strings.Contains(graph, "colorchannelmixer=rr=0:gg=0:bb=0:aa=0") || !strings.Contains(graph, "filename='subtitle.ass':alpha=1")) {
				t.Fatalf("text captions lack a CPU-rasterized transparent plane: %s", graph)
			}
		}
	}
}

func TestProgressiveSubtitleClockDoesNotDoubleApplySeek(t *testing.T) {
	p := commandPlan()
	p.OutputMode, p.Container, p.SegmentSeconds, p.SourceFormatStartKnown = "progressive", "mp4", 0, true
	p.StartTicks = 2 * ticksPerSecond
	p.Subtitle = SubtitlePlan{Mode: "burn", Codec: "ass", StreamIndex: 2, OffsetTicks: ticksPerSecond}
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	filter := args[slices.Index(args, "-vf")+1]
	if !strings.Contains(filter, "setpts=PTS+(-1.0000000)/TB") || strings.Contains(filter, "setpts=PTS+(1.0000000)/TB") {
		t.Fatalf("progressive source timestamps received the HLS input seek shift: %s", filter)
	}
}
