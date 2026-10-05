package transcode

import (
	"errors"
	"strings"
	"testing"
)

func externalBitmapTestPlan() Plan {
	plan := commandPlan()
	plan.StartTicks = 2 * ticksPerSecond
	plan.Subtitle = SubtitlePlan{Mode: "burn", Codec: "dvd_subtitle", StreamIndex: 1000000,
		ExternalTag: strings.Repeat("a", 64), ExternalStreamIndex: 1, OffsetTicks: ticksPerSecond / 2,
		ExternalCanvasWidth: 640, ExternalCanvasHeight: 384}
	return plan
}

func TestExternalBitmapPlanSeparatesCatalogIdentityFromDemuxOrdinal(t *testing.T) {
	plan := externalBitmapTestPlan()
	if err := ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Plan){
		"unbound ordinal":            func(p *Plan) { p.Subtitle.ExternalTag = "" },
		"negative ordinal":           func(p *Plan) { p.Subtitle.ExternalStreamIndex = -1 },
		"excessive ordinal":          func(p *Plan) { p.Subtitle.ExternalStreamIndex = 32 },
		"SUP ordinal":                func(p *Plan) { p.Subtitle.Codec = "hdmv_pgs_subtitle" },
		"text ordinal":               func(p *Plan) { p.Subtitle.Codec = "ass" },
		"unsupported external codec": func(p *Plan) { p.Subtitle.Codec = "dvb_subtitle" },
		"external manifest":          func(p *Plan) { p.Subtitle.Mode = "hls" },
		"stream source":              func(p *Plan) { p.SourceMode, p.DurationTicks = "stream", 0 },
		"missing canvas":             func(p *Plan) { p.Subtitle.ExternalCanvasWidth = 0 },
		"excessive canvas":           func(p *Plan) { p.Subtitle.ExternalCanvasWidth = 16385 },
		"excessive raster":           func(p *Plan) { p.Subtitle.ExternalCanvasWidth, p.Subtitle.ExternalCanvasHeight = 16384, 16384 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := plan
			mutate(&changed)
			if err := ValidatePlan(changed); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unbound bitmap plan was accepted: %+v, %v", changed, err)
			}
		})
	}
}

func TestExternalBitmapInputPreservesSidecarClockAndFixedNames(t *testing.T) {
	for _, mode := range []string{"hls", "ladder", "progressive"} {
		for _, codec := range []string{"dvd_subtitle", "hdmv_pgs_subtitle"} {
			t.Run(mode+"/"+codec, func(t *testing.T) {
				plan := externalBitmapTestPlan()
				plan.Subtitle.Codec = codec
				ordinal, filename, formats := "1", "subtitle.idx", "vobsub,mpeg"
				if codec == "hdmv_pgs_subtitle" {
					plan.Subtitle.ExternalStreamIndex = 0
					ordinal, filename, formats = "0", "subtitle.sup", "sup"
				}
				offset := "-2.0000000"
				if mode == "progressive" {
					plan.OutputMode, plan.Container, plan.SegmentSeconds = "progressive", "mp4", 0
					plan.SourceFormatStartKnown, plan.SourceFormatStartTicks = true, 5*ticksPerSecond
					offset = "0.0000000"
				} else if mode == "ladder" {
					plan.Container, plan.HLS.SegmentType = "mp4", "fmp4"
					plan.HLS.RenditionCount = 2
					plan.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 90, VideoBitrate: 256000}
					plan.HLS.Renditions[1] = HLSRendition{Width: 80, Height: 44, VideoBitrate: 128000}
				}
				args, err := BuildArgs(plan, 1)
				if err != nil {
					t.Fatal(err)
				}
				joined := strings.Join(args, " ")
				if !strings.Contains(joined, "[1:"+ordinal+"]settb=AVTB,format=rgba,scale=w=640:h=384:flags=bilinear,setsar=1,setpts=PTS+(0.5000000)/TB") ||
					!strings.Contains(joined, "-seek_timestamp 1 -itsoffset "+offset) ||
					!hasArgumentPair(args, "-format_whitelist", formats) || !hasArgumentPair(args, "-protocol_whitelist", "file") ||
					!hasArgumentPair(args, "-i", filename) || strings.Contains(joined, "[1:1000000]") ||
					strings.Count(joined, "-i /proc/self/fd/3") != 1 {
					t.Fatalf("external input lost its identity or authored clock: %v", args)
				}
				if codec == "dvd_subtitle" && !hasArgumentPair(args, "-sub_name", "subtitle.sub") {
					t.Fatal("VobSub companion escaped the fixed private asset")
				}
				if mode == "progressive" {
					args = buildProgressiveVideoArgsWithSeek(plan, 1, ticksPerSecond)
					if !hasArgumentPair(args, "-map", "2:1") || strings.Count(strings.Join(args, " "), "-i /proc/self/fd/3") != 2 {
						t.Fatalf("external bitmap displaced the independent seek audio input: %v", args)
					}
				}
			})
		}
	}
}

func TestExternalBitmapGPUCanvasUsesTheSelectedSourceRaster(t *testing.T) {
	plan := externalBitmapTestPlan()
	plan.VideoFilters.Backend = "vulkan"
	plan.Hardware = Hardware{Decode: "vaapi", Encode: "vaapi"}
	args, err := BuildArgs(plan, 1)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "[1:1]settb=AVTB,format=rgba,scale=w=640:h=384:flags=bilinear,setsar=1,") ||
		!strings.Contains(joined, "[goby_subtitle_clock][goby_bitmap]overlay=") ||
		!strings.Contains(joined, "[goby_canvas][goby_subtitle]libplacebo=inputs=2:") {
		t.Fatalf("GPU subtitle geometry differs from the source canvas: %v", args)
	}
}
