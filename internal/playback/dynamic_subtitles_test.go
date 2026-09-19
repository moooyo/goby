package playback

import (
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func dynamicSubtitleFixture() (Source, Request) {
	source, request := hlsSubtitleFixture()
	source.Info.DurationTicks, source.Info.Size, source.Info.FileChangeTimeNs = 0, 0, 0
	for index := range source.Info.Streams {
		source.Info.Streams[index].IsExternal, source.Info.Streams[index].SubtitleTag = false, ""
	}
	request.LiveStreamID = "live_owned"
	return source, request
}

func TestDynamicSubtitleSelectionOffAndDelayShareNonseekableProducer(t *testing.T) {
	source, request := dynamicSubtitleFixture()
	var first transcode.Plan
	for position, selected := range []int{12, 18, 21, -1} {
		request.SubtitleStreamIndex = profileTestPtr(selected)
		decision, err := PlanDynamicConversion(source, request, conversionTestLimits())
		if err != nil || decision.Plan == nil {
			t.Fatalf("dynamic caption selection failed: %+v, %v", decision, err)
		}
		if position == 0 {
			first = *decision.Plan
		}
		if *decision.Plan != first || first.SourceMode != "stream" || first.StartTicks != 0 || first.DurationTicks != 0 || first.Subtitle != (transcode.SubtitlePlan{}) ||
			decision.SubtitleView.SelectedStreamIndex != selected || !decision.SubtitleView.SelectionSet || decision.Output.DefaultSubtitleStreamIndex == nil ||
			*decision.Output.DefaultSubtitleStreamIndex != selected || transcode.PlanHLSSubtitles(first).Count != 3 {
			t.Fatal("dynamic subtitle selection changed the media input or lost its view")
		}
		if selected == -1 && (decision.Output.SubtitleMethod != "" || decision.Output.SubtitleFormat != "") ||
			selected >= 0 && (decision.Output.SubtitleMethod != SubtitleDeliveryMethodHls || decision.Output.SubtitleFormat != "vtt") {
			t.Fatal("dynamic subtitle projection did not describe the active view")
		}
		view, err := HLSSubtitleViewFor(*decision.Plan, &selected, 2*media.TicksPerSecond)
		if err != nil || view.OffsetTicks != 2*media.TicksPerSecond || *decision.Plan != first || request.LiveStreamID != "live_owned" || *request.SubtitleStreamIndex != selected {
			t.Fatal("dynamic caption delay mutated the producer or caller-owned lease selection")
		}
	}
	request.StartTimeTicks = profileTestPtr(int64(1))
	if _, err := PlanDynamicConversion(source, request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("caption availability turned a nonseekable input into a seekable source")
	}
}

func TestDynamicSubtitleDefinitionIdentityIsBoundWithoutNetworkAccess(t *testing.T) {
	source, request := dynamicSubtitleFixture()
	external := &source.Info.Streams[5]
	external.IsExternal, external.Index, external.Codec = true, 1<<30, "webvtt"
	external.SubtitleTag = strings.Repeat("b", 64)
	request.SubtitleStreamIndex = profileTestPtr(external.Index)
	decision, err := PlanDynamicConversion(source, request, conversionTestLimits())
	if err != nil || decision.Plan == nil {
		t.Fatalf("authorized dynamic subtitle metadata could not be planned: %+v, %v", decision, err)
	}
	tracks := transcode.PlanHLSSubtitles(*decision.Plan)
	track := tracks.Tracks[tracks.Count-1]
	if track.StreamIndex != external.Index || track.ExternalTag != external.SubtitleTag || track.Codec != "webvtt" {
		t.Fatal("the external dynamic definition was not bound to its authorized lease metadata")
	}
	external.SubtitleTag = strings.Repeat("c", 64)
	if _, ok := HLSSubtitleMetadata(source, *decision.Plan, tracks.Count-1); ok {
		t.Fatal("a replaced dynamic definition matched the previous immutable track set")
	}
	external.SubtitleTag = ""
	if rejected, err := PlanDynamicConversion(source, request, conversionTestLimits()); err != nil || rejected.Plan != nil {
		t.Fatal("an unbound dynamic external subtitle was advertised")
	}
}

func TestDynamicBitmapBurnRequiresEncodingAndNeverFiniteTextExtraction(t *testing.T) {
	source, request := dynamicSubtitleFixture()
	source.Info.Streams[3].Codec, source.Info.Streams[3].IsTextSubtitleStream = "hdmv_pgs_subtitle", false
	request.SubtitleStreamIndex = profileTestPtr(12)
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: SubtitleDeliveryMethodEncode, Format: "hdmv_pgs_subtitle", Container: "ts", Protocol: "hls"}}
	decision, err := PlanDynamicConversion(source, request, conversionTestLimits())
	if err != nil || decision.Plan == nil {
		t.Fatalf("dynamic bitmap burn could not be planned: %+v, %v", decision, err)
	}
	plan := *decision.Plan
	if plan.VideoCodec != "h264" || plan.SourceMode != "stream" || plan.StartTicks != 0 || plan.DurationTicks != 0 ||
		plan.Subtitle.Mode != "burn" || plan.Subtitle.StreamIndex != 12 || plan.Subtitle.ExternalTag != "" || plan.Subtitle.FontStreams != "" ||
		transcode.HasHLSSubtitles(plan) || decision.Output.SubtitleMethod != SubtitleDeliveryMethodEncode {
		t.Fatal("streaming bitmap composition lost its execution or selection contract")
	}
	limits := conversionTestLimits()
	limits.AllowVideoTranscode = false
	if rejected, err := PlanDynamicConversion(source, request, limits); err != nil || rejected.Plan != nil {
		t.Fatal("dynamic bitmap burn bypassed video encoding permission")
	}
	source.Info.Streams[3].Codec, source.Info.Streams[3].IsTextSubtitleStream = "ass", true
	request.DeviceProfile.SubtitleProfiles[0].Format = "ass"
	if rejected, err := PlanDynamicConversion(source, request, conversionTestLimits()); err != nil || rejected.Plan != nil {
		t.Fatal("dynamic text burn claimed a finite ASS extraction pass over an unbounded source")
	}
	request.DeviceProfile.SubtitleProfiles[0] = SubtitleProfile{Method: SubtitleDeliveryMethodExternal, Format: "ass", Container: "ts", Protocol: "http"}
	if rejected, err := PlanDynamicConversion(source, request, conversionTestLimits()); err != nil || rejected.Plan != nil {
		t.Fatal("dynamic text claimed the finite external subtitle delivery path")
	}
}
