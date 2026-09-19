package playback

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsSubtitleFixture() (Source, Request) {
	source, request := conversionTestSource(), conversionTestRequest()
	source.Info.Streams[3].Language, source.Info.Streams[3].Title = "eng", "English"
	source.Info.Streams = append(source.Info.Streams,
		media.Stream{Index: 21, Codec: "ass", CodecType: "subtitle", Language: "jpn", Title: "Japanese", IsTextSubtitleStream: true},
		media.Stream{Index: 18, Codec: "vtt", CodecType: "subtitle", Language: "fra", Title: "French", IsTextSubtitleStream: true,
			IsExternal: true, SubtitleTag: strings.Repeat("a", 64)})
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: SubtitleDeliveryMethodHls, Format: "vtt", Container: "ts", Protocol: "hls"}}
	return source, request
}

func TestHLSSubtitleViewsShareOneImmutableMediaPlan(t *testing.T) {
	source, request := hlsSubtitleFixture()
	request.DeviceProfile.SubtitleProfiles = append([]SubtitleProfile{
		{Method: SubtitleDeliveryMethodExternal, Format: "vtt,srt,ass", Container: "ts", Protocol: "http"},
		{Method: SubtitleDeliveryMethodEncode, Format: "vtt,subrip,ass", Container: "ts", Protocol: "hls"},
	}, request.DeviceProfile.SubtitleProfiles...)
	original := source
	original.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
	var first transcode.Plan
	for number, selected := range []int{12, 18, 21, -1} {
		request.SubtitleStreamIndex = profileTestPtr(selected)
		decision := conversionTestPlan(t, source, request, conversionTestLimits())
		if number == 0 {
			first = *decision.Plan
		}
		if *decision.Plan != first || decision.Plan.Subtitle != (transcode.SubtitlePlan{}) || decision.SubtitleView.SelectedStreamIndex != selected || !decision.SubtitleView.SelectionSet ||
			decision.Output.DefaultSubtitleStreamIndex == nil || *decision.Output.DefaultSubtitleStreamIndex != selected {
			t.Fatalf("track selection changed the producer or lost the view: selected=%d plan=%+v view=%+v", selected, decision.Plan, decision.SubtitleView)
		}
		if selected == -1 {
			if decision.Output.SubtitleMethod != "" || decision.Output.SubtitleFormat != "" {
				t.Fatal("off still claimed an active subtitle delivery method")
			}
		} else if decision.Output.SubtitleMethod != SubtitleDeliveryMethodHls || decision.Output.SubtitleFormat != "vtt" {
			t.Fatal("the selected track lost its HLS WebVTT delivery projection")
		}
		for _, offset := range []int64{-2 * media.TicksPerSecond, 0, 3 * media.TicksPerSecond} {
			view, err := HLSSubtitleViewFor(*decision.Plan, &selected, offset)
			if err != nil || view.OffsetTicks != offset || *decision.Plan != first {
				t.Fatal("caption delay changed the immutable media producer")
			}
		}
	}
	tracks := transcode.PlanHLSSubtitles(first)
	if tracks.Count != 3 || tracks.Tracks[0].StreamIndex != 12 || tracks.Tracks[1].StreamIndex != 18 || tracks.Tracks[2].StreamIndex != 21 ||
		tracks.Tracks[1].ExternalTag != strings.Repeat("a", 64) || !reflect.DeepEqual(source, original) {
		t.Fatal("the bound set is unstable or mutated its catalog source")
	}
	request.SubtitleStreamIndex = nil
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if *decision.Plan != first || decision.SubtitleView.SelectionSet || decision.SubtitleView.SelectedStreamIndex != -1 || *decision.Output.DefaultSubtitleStreamIndex != -1 {
		t.Fatal("an unspecified selection enabled a caption or changed track availability")
	}
}

func TestHLSSubtitleTrackSetRespectsClientDeliveryAndLimits(t *testing.T) {
	source, request := hlsSubtitleFixture()
	request.DeviceProfile.SubtitleProfiles[0].Language = "eng,fra"
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if tracks := transcode.PlanHLSSubtitles(*decision.Plan); tracks.Count != 2 || tracks.Tracks[1].StreamIndex != 18 {
		t.Fatal("the track set advertised a language outside the declared profile")
	}
	request.DeviceProfile.SubtitleProfiles[0].Protocol = "http"
	if decision := conversionTestPlan(t, source, request, conversionTestLimits()); transcode.HasHLSSubtitles(*decision.Plan) {
		t.Fatal("an HTTP-only declaration became an HLS subtitle capability")
	}
	request.DeviceProfile.TranscodingProfiles[0].ManifestSubtitles = "vtt"
	request.DeviceProfile.SubtitleProfiles = nil
	request.DeviceProfile.TranscodingProfiles[0].MaxManifestSubtitles = profileTestPtr(2)
	decision = conversionTestPlan(t, source, request, conversionTestLimits())
	if tracks := transcode.PlanHLSSubtitles(*decision.Plan); tracks.Count != 2 || tracks.Tracks[0].StreamIndex != 12 || tracks.Tracks[1].StreamIndex != 18 {
		t.Fatal("the explicit manifest limit did not produce a stable bounded prefix")
	}
	request.SubtitleStreamIndex = profileTestPtr(21)
	conversionTestDeclined(t, source, request, conversionTestLimits())
	request.SubtitleStreamIndex = profileTestPtr(-1)
	request.DeviceProfile.TranscodingProfiles[0].MaxManifestSubtitles = profileTestPtr(0)
	if decision := conversionTestPlan(t, source, request, conversionTestLimits()); transcode.HasHLSSubtitles(*decision.Plan) {
		t.Fatal("a zero manifest limit still advertised tracks")
	}
	request.DeviceProfile.TranscodingProfiles[0].MaxManifestSubtitles = profileTestPtr(-1)
	if _, err := PlanConversion(source, request, conversionTestLimits()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("a negative manifest limit was not rejected")
	}
}

func TestHLSSubtitleTrackSetCapsInventoryAndExcludesUnknownTracks(t *testing.T) {
	source, request := hlsSubtitleFixture()
	request.DeviceProfile.TranscodingProfiles[0].MaxManifestSubtitles = profileTestPtr(100)
	for index := 30; index < 45; index++ {
		source.Info.Streams = append(source.Info.Streams, media.Stream{Index: index, Codec: "subrip", CodecType: "subtitle", IsTextSubtitleStream: true})
	}
	source.Info.Streams = append(source.Info.Streams,
		media.Stream{Index: 8, Codec: "hdmv_pgs_subtitle", CodecType: "subtitle"},
		media.Stream{Index: 10, Codec: "unknown_text", CodecType: "subtitle", IsTextSubtitleStream: true},
		media.Stream{Index: 11, Codec: "vtt", CodecType: "subtitle", IsTextSubtitleStream: true, IsExternal: true})
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	tracks := transcode.PlanHLSSubtitles(*decision.Plan)
	if tracks.Count != transcode.MaxHLSSubtitleTracks || tracks.Tracks[0].StreamIndex != 12 || tracks.Tracks[7].StreamIndex != 34 {
		t.Fatalf("the inventory exceeded its bound or admitted an unsupported identity: %+v", tracks)
	}
}

func TestHLSSubtitleMetadataRequiresCurrentBoundIdentity(t *testing.T) {
	source, request := hlsSubtitleFixture()
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	metadata, ok := HLSSubtitleMetadata(source, *decision.Plan, 1)
	if !ok || metadata.Index != 18 || metadata.Language != "fra" || metadata.Title != "French" || !metadata.IsExternal {
		t.Fatal("current track metadata was not associated with the bound source")
	}
	for _, mutate := range []func(*media.Stream){
		func(stream *media.Stream) { stream.SubtitleTag = strings.Repeat("b", 64) },
		func(stream *media.Stream) { stream.Codec = "ass" },
		func(stream *media.Stream) { stream.IsExternal = false },
	} {
		changed := source
		changed.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
		mutate(&changed.Info.Streams[5])
		if _, ok := HLSSubtitleMetadata(changed, *decision.Plan, 1); ok {
			t.Fatal("a stale external identity was reused for a bound track")
		}
	}
	for _, slot := range []int{-1, 3, transcode.MaxHLSSubtitleTracks} {
		if _, ok := HLSSubtitleMetadata(source, *decision.Plan, slot); ok {
			t.Fatal("metadata was returned for an unbound slot")
		}
	}
}

func TestHLSSubtitleViewRejectsForeignSelectionAndUnboundedDelay(t *testing.T) {
	source, request := hlsSubtitleFixture()
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	for _, index := range []int{-2, 5, 99} {
		if _, err := HLSSubtitleViewFor(*decision.Plan, &index, 0); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("an invalid view escaped the bound track set")
		}
	}
	for _, delay := range []int64{-24*60*60*media.TicksPerSecond - 1, 24*60*60*media.TicksPerSecond + 1} {
		if _, err := HLSSubtitleViewFor(*decision.Plan, nil, delay); !errors.Is(err, ErrInvalidRequest) {
			t.Fatal("an unbounded caption delay was accepted")
		}
	}
}

func TestHLSSubtitleBurnNeverAdvertisesAnOffSwitchForBurnedPixels(t *testing.T) {
	source, request := hlsSubtitleFixture()
	request.SubtitleStreamIndex = profileTestPtr(12)
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Method: SubtitleDeliveryMethodEncode, Format: "subrip", Container: "ts", Protocol: "hls"}}
	decision := conversionTestPlan(t, source, request, conversionTestLimits())
	if decision.Plan.Subtitle.Mode != "burn" || transcode.HasHLSSubtitles(*decision.Plan) || decision.Output.SubtitleMethod != SubtitleDeliveryMethodEncode {
		t.Fatal("burn-in was combined with an independently switchable subtitle group")
	}
}
