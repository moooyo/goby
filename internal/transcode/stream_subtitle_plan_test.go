package transcode

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestStreamingSubtitlePlanSeparatesTextRenditionsFromBitmapBurn(t *testing.T) {
	p := commandPlan()
	p.SourceMode, p.DurationTicks = "stream", 0
	p.HLS.Subtitles.Count = 1
	p.HLS.Subtitles.Tracks[0] = HLSSubtitleTrack{StreamIndex: 3, Codec: "webvtt"}
	if err := ValidatePlan(p); err != nil {
		t.Fatalf("bound stream text rendition rejected: %v", err)
	}
	p.HLS.Subtitles.Tracks[0] = HLSSubtitleTrack{StreamIndex: 1 << 30, Codec: "webvtt", ExternalTag: strings.Repeat("a", 64)}
	if err := ValidatePlan(p); err != nil {
		t.Fatalf("authorized dynamic definition identity rejected: %v", err)
	}
	p.HLS.Subtitles = HLSSubtitlePlan{}
	p.Subtitle = SubtitlePlan{Mode: "burn", Codec: "hdmv_pgs_subtitle", StreamIndex: 3}
	if err := ValidatePlan(p); err != nil {
		t.Fatalf("stream bitmap burn plan rejected: %v", err)
	}
	for name, mutate := range map[string]func(*Plan){
		"text burn":               func(p *Plan) { p.Subtitle.Codec = "ass" },
		"external burn":           func(p *Plan) { p.Subtitle.ExternalTag = strings.Repeat("a", 64) },
		"font extraction":         func(p *Plan) { p.Subtitle.FontStreams = "4" },
		"source seeking":          func(p *Plan) { p.StartTicks = 1 },
		"finite duration":         func(p *Plan) { p.DurationTicks = ticksPerSecond },
		"copied burn":             func(p *Plan) { p.VideoCodec = "copy" },
		"legacy finite rendition": func(p *Plan) { p.Subtitle.Mode, p.Subtitle.Codec = "hls", "webvtt" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := p
			mutate(&changed)
			if err := ValidatePlan(changed); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unbounded input accepted an incompatible subtitle path: %+v, %v", changed, err)
			}
		})
	}
}

func TestStreamingPlanAcceptsOnlyBoundedProbedOrigins(t *testing.T) {
	p := commandPlan()
	p.SourceMode, p.DurationTicks = "stream", 0
	const maximumOrigin = math.MaxInt64 - ticksPerSecond
	for _, start := range []int64{-maximumOrigin, 0, maximumOrigin} {
		p.SourceFormatStartKnown, p.SourceFormatStartTicks = true, start
		if err := ValidatePlan(p); err != nil {
			t.Fatalf("a bounded stream origin was rejected: %d, %v", start, err)
		}
	}
	for _, mutate := range []func(*Plan){
		func(p *Plan) { p.SourceFormatStartKnown, p.SourceFormatStartTicks = false, 1 },
		func(p *Plan) { p.SourceFormatStartTicks = maximumOrigin + 1 },
		func(p *Plan) { p.SourceFormatStartTicks = -maximumOrigin - 1 },
		func(p *Plan) { p.CopyTimestamps = true },
	} {
		changed := p
		mutate(&changed)
		if err := ValidatePlan(changed); !errors.Is(err, ErrInvalidPlan) {
			t.Fatal("a stream accepted an unbound input clock or client timestamp mode")
		}
	}
}
