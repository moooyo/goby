package transcode

import (
	"errors"
	"strings"
	"testing"
)

func hlsSubtitlePlanFixture() Plan {
	p := commandPlan()
	p.HLS.Subtitles.Count = 2
	p.HLS.Subtitles.Tracks[0] = HLSSubtitleTrack{StreamIndex: 2, Codec: "subrip"}
	p.HLS.Subtitles.Tracks[1] = HLSSubtitleTrack{StreamIndex: 7, Codec: "ass"}
	return p
}

func TestHLSSubtitlePlanKeepsComparableBoundedTrackIdentities(t *testing.T) {
	p := hlsSubtitlePlanFixture()
	if err := ValidatePlan(p); err != nil {
		t.Fatal(err)
	}
	jobs := map[Plan]int{p: 1}
	if jobs[p] != 1 || !GeneratedHLS(p) || !HasHLSSubtitles(p) || PlanHLSSubtitles(p) != p.HLS.Subtitles {
		t.Fatal("the complete subtitle track set lost immutable producer identity")
	}
	for slot := 0; slot < 2; slot++ {
		track, ok := HLSSubtitleTrackAt(p, slot)
		if !ok || track != p.HLS.Subtitles.Tracks[slot] {
			t.Fatal("a valid track slot did not preserve its bound identity")
		}
	}
	for _, slot := range []int{-1, 2, MaxHLSSubtitleTracks} {
		if _, ok := HLSSubtitleTrackAt(p, slot); ok {
			t.Fatal("an unbound track slot was exposed")
		}
	}
	p.HLS.Subtitles.Tracks[1] = HLSSubtitleTrack{StreamIndex: 1<<31 - 1, Codec: "vtt", ExternalTag: strings.Repeat("a", 64)}
	if err := ValidatePlan(p); err != nil {
		t.Fatalf("an indexed external subtitle lost its catalog identity: %v", err)
	}
}

func TestHLSSubtitlePlanAcceptsLegacySingleTrackWithoutMutation(t *testing.T) {
	p := commandPlan()
	p.Subtitle = SubtitlePlan{Mode: "hls", Codec: "subrip", StreamIndex: 3, SubtitleOrdinal: 1, OffsetTicks: ticksPerSecond}
	original := p
	tracks := PlanHLSSubtitles(p)
	if err := ValidatePlan(p); err != nil || !HasHLSSubtitles(p) || tracks.Count != 1 || tracks.Tracks[0] != (HLSSubtitleTrack{StreamIndex: 3, Codec: "subrip"}) || p != original {
		t.Fatalf("legacy single-track identity or offset changed: %+v, %v", tracks, err)
	}
}

func TestHLSSubtitlePlanRejectsAliasesUnboundAndMixedDelivery(t *testing.T) {
	for name, change := range map[string]func(*Plan){
		"negative count":         func(p *Plan) { p.HLS.Subtitles.Count = -1 },
		"excess count":           func(p *Plan) { p.HLS.Subtitles.Count = MaxHLSSubtitleTracks + 1 },
		"unused slot":            func(p *Plan) { p.HLS.Subtitles.Tracks[2] = p.HLS.Subtitles.Tracks[0] },
		"hidden tracks":          func(p *Plan) { p.HLS.Subtitles.Count = 0 },
		"duplicate index":        func(p *Plan) { p.HLS.Subtitles.Tracks[1].StreamIndex = 2 },
		"reversed index":         func(p *Plan) { p.HLS.Subtitles.Tracks[0].StreamIndex = 8 },
		"audio index":            func(p *Plan) { p.HLS.Subtitles.Tracks[0].StreamIndex = p.AudioStreamIndex },
		"negative index":         func(p *Plan) { p.HLS.Subtitles.Tracks[0].StreamIndex = -1 },
		"unbound external index": func(p *Plan) { p.HLS.Subtitles.Tracks[1].StreamIndex = 4096 },
		"external digest":        func(p *Plan) { p.HLS.Subtitles.Tracks[1].ExternalTag = "not-a-digest" },
		"noncanonical digest":    func(p *Plan) { p.HLS.Subtitles.Tracks[1].ExternalTag = strings.Repeat("A", 64) },
		"bitmap":                 func(p *Plan) { p.HLS.Subtitles.Tracks[0].Codec = "hdmv_pgs_subtitle" },
		"noncanonical codec":     func(p *Plan) { p.HLS.Subtitles.Tracks[0].Codec = "ASS" },
		"single and set":         func(p *Plan) { p.Subtitle = SubtitlePlan{Mode: "hls", StreamIndex: 3, Codec: "subrip"} },
		"burn and set":           func(p *Plan) { p.Subtitle = SubtitlePlan{Mode: "burn", StreamIndex: 3, Codec: "subrip"} },
	} {
		t.Run(name, func(t *testing.T) {
			p := hlsSubtitlePlanFixture()
			change(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("unsafe track set accepted: %+v, %v", p.HLS.Subtitles, err)
			}
		})
	}
}
