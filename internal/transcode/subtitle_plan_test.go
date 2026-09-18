package transcode

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestSubtitleBurnPlanDoesNotAcceptPathsOrCopyPipeline(t *testing.T) {
	plan := Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: 1,
		Subtitle: SubtitlePlan{Mode: "burn", Codec: "ass", StreamIndex: 2, FontStreams: "3,7"}}
	if err := ValidateSubtitlePlan(plan); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Plan){
		func(p *Plan) { p.VideoCodec = "copy" },
		func(p *Plan) { p.Hardware.Encode = "nvenc" },
		func(p *Plan) { p.Subtitle.Mode = "../burn" },
		func(p *Plan) { p.Subtitle.FontStreams = "3,3" },
		func(p *Plan) { p.Subtitle.FontStreams = "7,3" },
		func(p *Plan) { p.Subtitle.FontStreams = "../font.ttf" },
		func(p *Plan) { p.Subtitle.StreamIndex = p.AudioStreamIndex },
		func(p *Plan) { p.Subtitle.OffsetTicks = 24*60*60*ticksPerSecond + 1 },
		func(p *Plan) { p.SourceMode = "stream" },
	} {
		changed := plan
		change(&changed)
		if err := ValidateSubtitlePlan(changed); err == nil {
			t.Errorf("invalid subtitle plan was accepted: %+v", changed)
		}
	}
}

func TestExternalSubtitleAssetsBindScopeAndPlanWithoutPaths(t *testing.T) {
	plan := Plan{VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: 1,
		Subtitle: SubtitlePlan{Mode: "burn", Codec: "ass", StreamIndex: 7, ExternalTag: strings.Repeat("a", 64)}}
	spec := Spec{Scope: Scope{UserID: "reader", AuthSessionID: "auth", ItemID: "movie", SourceID: "source", PlaySessionID: "play"}, SourceStamp: "source-stamp", Plan: plan}
	contents := []byte("[Script Info]\nScriptType: v4.00+\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	loaded := false
	ctx := withSubtitleSource(context.Background(), spec, func(ctx context.Context, got Spec) ([]byte, error) {
		if got != spec {
			t.Fatal("subtitle loading lost the authorized source or session identity")
		}
		loaded = true
		return contents, nil
	})
	directory := t.TempDir()
	if err := PrepareSubtitleAssets(ctx, "unused", directory, nil, plan); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "subtitle.ass"))
	if err != nil || string(data) != string(contents) || !loaded {
		t.Fatalf("private styled subtitle asset was not prepared: %q, %v", data, err)
	}
	changed := plan
	changed.Subtitle.ExternalTag = strings.Repeat("b", 64)
	if err := PrepareSubtitleAssets(ctx, "unused", t.TempDir(), nil, changed); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("changed subtitle fingerprint reused an old loader: %v", err)
	}
	if err := PrepareSubtitleAssets(context.Background(), "unused", t.TempDir(), nil, plan); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("external subtitle burned without an authorized loader: %v", err)
	}
	if err := PrepareSubtitleAssets(ctx, "unused", directory, nil, plan); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing subtitle asset was overwritten: %v", err)
	}
}

func TestExternalSubtitleAssetsRejectOversizeAndCancelledLoads(t *testing.T) {
	plan := Plan{VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: 1,
		Subtitle: SubtitlePlan{Mode: "burn", Codec: "srt", StreamIndex: 7, ExternalTag: strings.Repeat("a", 64)}}
	spec := Spec{Plan: plan}
	for _, oversized := range []bool{false, true} {
		parent, cancel := context.WithCancel(context.Background())
		ctx := withSubtitleSource(parent, spec, func(context.Context, Spec) ([]byte, error) {
			if oversized {
				return make([]byte, media.MaxSubtitleExtractionBytes+1), nil
			}
			cancel()
			return []byte("[Script Info]"), nil
		})
		directory := t.TempDir()
		err := PrepareSubtitleAssets(ctx, "unused", directory, nil, plan)
		cancel()
		if err == nil {
			t.Fatal("cancelled or oversized subtitle was prepared")
		}
		entries, readErr := os.ReadDir(directory)
		if readErr != nil || len(entries) != 0 {
			t.Fatalf("rejected subtitle left a cache asset: %v", readErr)
		}
	}
}

func TestSubtitleBurnFiltersPreserveTimelineAndBitmapCanvas(t *testing.T) {
	plan := Plan{VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: 1, StartTicks: 10 * ticksPerSecond,
		Subtitle: SubtitlePlan{Mode: "burn", Codec: "ass", StreamIndex: 2, OffsetTicks: 2 * ticksPerSecond}}
	filter, err := TextSubtitleFilter(plan)
	if err != nil || !strings.Contains(filter, "setpts=PTS+(8.0000000)/TB") || !strings.HasSuffix(filter, "setpts=PTS-(8.0000000)/TB") || !strings.Contains(filter, "filename='subtitle.ass':fontsdir='.'") {
		t.Fatalf("text subtitle timeline filter = %q, %v", filter, err)
	}
	plan.Subtitle.Codec = "hdmv_pgs_subtitle"
	plan.Width, plan.Height = 640, 360
	graph, label, err := BitmapSubtitleGraph(plan, "scale=640:360,format=yuv420p")
	if err != nil || label != "[goby_video]" || !strings.Contains(graph, "[0:2]setpts=PTS+(2.0000000)/TB") || !strings.Contains(graph, "overlay=eof_action=pass:shortest=0:repeatlast=0,scale=640:360") {
		t.Fatalf("bitmap subtitle graph = %q, %q, %v", graph, label, err)
	}
	plan.Subtitle = SubtitlePlan{}
	if filter, err := TextSubtitleFilter(plan); err != nil || filter != "" {
		t.Fatalf("disabled subtitle retained filter %q: %v", filter, err)
	}
}
