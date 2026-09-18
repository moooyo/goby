package transcode

import (
	"slices"
	"strings"
	"testing"
)

func adaptiveHLSPlan() Plan {
	p := commandPlan()
	p.Container, p.HLS.SegmentType = "mp4", "fmp4"
	p.HLS.RenditionCount = 2
	p.HLS.Renditions[0] = HLSRendition{Width: 160, Height: 90, VideoBitrate: 256000}
	p.HLS.Renditions[1] = HLSRendition{Width: 80, Height: 44, VideoBitrate: 96000}
	return p
}

func TestAdaptiveHLSCreatesSeparateAlignedEncoderOutputs(t *testing.T) {
	args, err := BuildArgs(adaptiveHLSPlan(), 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-hls_fmp4_init_filename", "v0-init.mp4"}, {"-hls_fmp4_init_filename", "v1-init.mp4"},
		{"-hls_segment_filename", "v0-segment-%06d.m4s"}, {"-hls_segment_filename", "v1-segment-%06d.m4s"},
		{"-vf", "scale=w=160:h=90,format=yuv420p"}, {"-vf", "scale=w=80:h=44,format=yuv420p"}, {"-b:v", "256000"}, {"-b:v", "96000"}} {
		if !hasArgumentPair(args, pair[0], pair[1]) {
			t.Errorf("missing independent rendition argument %v", pair)
		}
	}
	if !slices.Contains(args, "v0.m3u8") || !slices.Contains(args, "v1.m3u8") || slices.Contains(args, "main.m3u8") ||
		strings.Count(strings.Join(args, " "), "expr:gte(t,n_forced*3)") != 2 {
		t.Fatalf("unaligned or aliased ladder: %v", args)
	}
}

func TestHLSRejectsCopiedAdaptiveAndUnboundRenditions(t *testing.T) {
	for _, mutate := range []func(*Plan){func(p *Plan) { p.VideoCodec = "copy" }, func(p *Plan) { p.HLS.RenditionCount = 1 },
		func(p *Plan) { p.HLS.Renditions[1] = p.HLS.Renditions[0] }, func(p *Plan) { p.HLS.Renditions[3] = p.HLS.Renditions[1] },
		func(p *Plan) { p.SegmentMode = "vod" }, func(p *Plan) { p.AudioCodec = "mp3" }} {
		p := adaptiveHLSPlan()
		mutate(&p)
		if ValidatePlan(p) == nil {
			t.Fatalf("accepted invalid ladder: %+v", p)
		}
	}
}

func TestGeneratedHLSPackedAudioAndLivePipeCommands(t *testing.T) {
	p := Plan{Container: "aac", HLS: HLSPlan{SegmentType: "packed"}, VideoStreamIndex: -1, AudioStreamIndex: 0, AudioCodec: "aac", DurationTicks: 20 * ticksPerSecond, SegmentSeconds: 3}
	args, err := BuildArgs(p, 1)
	if err != nil || !hasArgumentPair(args, "-segment_format", "adts") || args[len(args)-1] != "segment-%06d.aac.tmp" {
		t.Fatalf("packed command = %v, %v", args, err)
	}
	p.HLS.SegmentType, p.Container, p.SourceMode, p.DurationTicks = "fmp4", "mp4", "stream", 0
	args, err = BuildArgs(p, 1)
	if err != nil || !hasArgumentPair(args, "-i", "pipe:3") || slices.Contains(args, "-t") || slices.Contains(args, "-ss") {
		t.Fatalf("stream command = %v, %v", args, err)
	}
	p.StartTicks = 1
	if ValidatePlan(p) == nil {
		t.Fatal("stream input accepted seek")
	}
}
