package playback

import (
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestAssToTextPolicyPreservesStyledDeliveryAndEmbedding(t *testing.T) {
	for _, codec := range []string{"ass", "ssa"} {
		for _, external := range []bool{false, true} {
			name := codec + "/embedded"
			if external {
				name = codec + "/sidecar"
			}
			t.Run(name, func(t *testing.T) {
				source := externalSubtitleTestSource(codec)
				source.Info.Streams[len(source.Info.Streams)-1].IsExternal = external
				source.DisableAssToText = true
				original := source
				original.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
				for _, format := range []string{"vtt", "webvtt", "srt", "subrip"} {
					request := externalSubtitleTestRequest(SubtitleProfile{Format: format, Method: SubtitleDeliveryMethodExternal})
					request.EnableTranscoding = profileTestPtr(false)
					decision := profileTestDecision(t, source, request, false)
					if decision.OriginalCompatible || decision.SubtitleMethod != "" || decision.SubtitleFormat != "" {
						t.Fatalf("disabled styled-to-text conversion remained available as %s: %+v", format, decision)
					}
				}
				for _, format := range []string{"ass", "ssa"} {
					request := externalSubtitleTestRequest(SubtitleProfile{Format: format, Method: SubtitleDeliveryMethodExternal})
					assertExternalSubtitleDecision(t, profileTestDecision(t, source, request, true), format)
				}
				if !external {
					request := externalSubtitleTestRequest(SubtitleProfile{Format: codec, Method: SubtitleDeliveryMethodEmbed})
					decision := profileTestDecision(t, source, request, true)
					if decision.SubtitleMethod != SubtitleDeliveryMethodEmbed || decision.SubtitleFormat != "" {
						t.Fatal("styled-to-text policy changed original-file subtitle embedding")
					}
				}
				if !reflect.DeepEqual(source, original) {
					t.Fatal("subtitle delivery policy changed caller-owned media facts")
				}
			})
		}
	}
	for _, test := range []struct{ codec, format string }{{"srt", "vtt"}, {"webvtt", "srt"}} {
		source := externalSubtitleTestSource(test.codec)
		source.DisableAssToText = true
		request := externalSubtitleTestRequest(SubtitleProfile{Format: test.format, Method: SubtitleDeliveryMethodExternal})
		assertExternalSubtitleDecision(t, profileTestDecision(t, source, request, true), test.format)
	}
}

func TestAssToTextPolicyCandidateFormatsPreserveOffAndNativePreference(t *testing.T) {
	source := externalSubtitleTestSource("ass")
	source.Info.Streams = append(source.Info.Streams, media.Stream{Index: 42, Codec: "ssa", CodecType: "subtitle", Language: "eng", IsExternal: true, IsTextSubtitleStream: true})
	source.DisableAssToText = true
	request := externalSubtitleTestRequest(SubtitleProfile{Format: "vtt,srt,ass,ssa", Method: SubtitleDeliveryMethodExternal})
	request.SubtitleStreamIndex = profileTestPtr(-1)
	before := profileTestDecision(t, source, request, true)
	want := map[int]string{12: "srt", externalSubtitleTestIndex: "ass", 42: "ssa"}
	if got := ExternalSubtitleCandidateFormats(source, request.DeviceProfile); !reflect.DeepEqual(got, want) {
		t.Fatalf("native candidate formats = %v, want %v", got, want)
	}
	after := profileTestDecision(t, source, request, true)
	if !reflect.DeepEqual(before, after) || after.DefaultSubtitleStreamIndex == nil || *after.DefaultSubtitleStreamIndex != -1 || after.SubtitleMethod != "" {
		t.Fatal("candidate projection changed subtitle Off or original playback")
	}
	request.DeviceProfile.SubtitleProfiles[0].Format = "vtt,srt"
	if got := ExternalSubtitleCandidateFormats(source, request.DeviceProfile); !reflect.DeepEqual(got, map[int]string{12: "srt"}) {
		t.Fatalf("plain-only client advertised styled track conversion: %v", got)
	}
	legacy := source
	legacy.DisableAssToText = false
	if got := ExternalSubtitleCandidateFormats(legacy, request.DeviceProfile); !reflect.DeepEqual(got, map[int]string{12: "srt", externalSubtitleTestIndex: "vtt", 42: "vtt"}) {
		t.Fatalf("legacy zero policy lost existing styled-to-text conversion: %v", got)
	}
}

func TestAssToTextPolicyFiltersNewHLSRenditionsAndPreservesBoundTracks(t *testing.T) {
	for _, manifest := range []bool{false, true} {
		name := "subtitle profile"
		if manifest {
			name = "manifest default"
		}
		t.Run(name, func(t *testing.T) {
			source, request := hlsSubtitleFixture()
			source.Info.Streams = append(source.Info.Streams, media.Stream{Index: 19, Codec: "ssa", CodecType: "subtitle", IsTextSubtitleStream: true,
				IsExternal: true, SubtitleTag: strings.Repeat("b", 64)})
			if manifest {
				request.DeviceProfile.TranscodingProfiles[0].ManifestSubtitles = "vtt"
				request.DeviceProfile.SubtitleProfiles = nil
			}
			request.SubtitleStreamIndex = profileTestPtr(-1)
			admitted := *conversionTestPlan(t, source, request, conversionTestLimits()).Plan
			if tracks := transcode.PlanHLSSubtitles(admitted); tracks.Count != 4 {
				t.Fatalf("legacy admission did not bind all text tracks: %+v", tracks)
			}
			limits := conversionTestLimits()
			limits.DisableAssToText = true
			filtered := conversionTestPlan(t, source, request, limits)
			if tracks := transcode.PlanHLSSubtitles(*filtered.Plan); tracks.Count != 2 || tracks.Tracks[0].StreamIndex != 12 || tracks.Tracks[1].StreamIndex != 18 {
				t.Fatalf("new admission did not retain only plain-text renditions: %+v", tracks)
			}
			for _, selected := range []int{19, 21} {
				request.SubtitleStreamIndex = profileTestPtr(selected)
				conversionTestDeclined(t, source, request, limits)
				if _, err := HLSSubtitleViewFor(admitted, &selected, media.TicksPerSecond); err != nil {
					t.Fatalf("a later policy change invalidated a bound styled track: %v", err)
				}
			}
			source.DisableAssToText = true
			for slot, index := range []int{12, 18, 19, 21} {
				if metadata, ok := HLSSubtitleMetadata(source, admitted, slot); !ok || metadata.Index != index {
					t.Fatal("a new delivery policy changed historical bound-track identity")
				}
			}
		})
	}
}

func TestAssToTextPolicyKeepsStyledBurnFallbackWithManifestSubtitles(t *testing.T) {
	for _, codec := range []string{"ass", "ssa"} {
		for _, external := range []bool{false, true} {
			name := codec + "/embedded"
			if external {
				name = codec + "/sidecar"
			}
			t.Run(name, func(t *testing.T) {
				source, request := hlsSubtitleFixture()
				selected := &source.Info.Streams[4]
				selected.Codec, selected.IsExternal = codec, external
				if external {
					selected.SubtitleTag = strings.Repeat("c", 64)
				}
				source.Info.Streams = append(source.Info.Streams, media.Stream{Index: 30, Codec: "ttf", CodecType: "attachment"},
					media.Stream{Index: 31, Codec: "ttf", CodecType: "attachment"})
				request.SubtitleStreamIndex = profileTestPtr(21)
				request.DeviceProfile.TranscodingProfiles[0].ManifestSubtitles = "vtt"
				request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{
					{Method: SubtitleDeliveryMethodHls, Format: "vtt", Container: "ts", Protocol: "hls"},
					{Method: SubtitleDeliveryMethodEncode, Format: codec, Container: "ts", Protocol: "hls"},
				}
				limits := conversionTestLimits()
				limits.DisableAssToText = true
				decision := conversionTestPlan(t, source, request, limits)
				if decision.Plan.Subtitle.Mode != "burn" || decision.Plan.Subtitle.Codec != codec || decision.Plan.Subtitle.StreamIndex != 21 ||
					decision.Plan.Subtitle.FontStreams != "30,31" || transcode.HasHLSSubtitles(*decision.Plan) ||
					decision.Output.SubtitleMethod != SubtitleDeliveryMethodEncode || decision.Plan.VideoCodec == "copy" {
					t.Fatalf("styled burn fallback lost its source, fonts, or encoded delivery: %+v", decision.Plan)
				}
				if external && decision.Plan.Subtitle.ExternalTag != strings.Repeat("c", 64) {
					t.Fatal("styled burn fallback lost its sidecar identity")
				}
			})
		}
	}
}

func TestAssToTextPolicyFiltersNewDynamicRenditions(t *testing.T) {
	source, request := dynamicSubtitleFixture()
	request.SubtitleStreamIndex = profileTestPtr(-1)
	limits := conversionTestLimits()
	limits.DisableAssToText = true
	decision, err := PlanDynamicConversion(source, request, limits)
	if err != nil || decision.Plan == nil {
		t.Fatalf("plain dynamic renditions were unavailable: %+v, %v", decision, err)
	}
	if tracks := transcode.PlanHLSSubtitles(*decision.Plan); tracks.Count != 2 || tracks.Tracks[0].StreamIndex != 12 || tracks.Tracks[1].StreamIndex != 18 || decision.Plan.SourceMode != "stream" {
		t.Fatalf("dynamic admission exposed a styled-to-text rendition: %+v", tracks)
	}
	request.SubtitleStreamIndex = profileTestPtr(21)
	if rejected, err := PlanDynamicConversion(source, request, limits); err != nil || rejected.Plan != nil || len(rejected.Reasons) == 0 {
		t.Fatal("a new dynamic admission selected disabled styled-to-text delivery")
	}
}
