package playback

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestSubtitleExtractionPolicyPreservesSidecarAndEmbedDelivery(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "enabled", true: "disabled"}[disabled], func(t *testing.T) {
			source := externalSubtitleTestSource("srt")
			source.DisableSubtitleExtraction = disabled
			original := source
			original.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
			request := externalSubtitleTestRequest(SubtitleProfile{Format: "srt,vtt,ass", Method: SubtitleDeliveryMethodExternal})

			request.SubtitleStreamIndex = profileTestPtr(12)
			decision := profileTestDecision(t, source, request, !disabled)
			if disabled {
				if decision.SubtitleMethod != "" || decision.SubtitleFormat != "" {
					t.Fatalf("disabled embedded extraction still advertised external delivery: %+v", decision)
				}
			} else if decision.SubtitleMethod != SubtitleDeliveryMethodExternal || decision.SubtitleFormat != "srt" {
				t.Fatalf("default extraction lost the embedded external representation: %+v", decision)
			}

			request.SubtitleStreamIndex = profileTestPtr(externalSubtitleTestIndex)
			assertExternalSubtitleDecision(t, profileTestDecision(t, source, request, true), "srt")
			want := map[int]string{externalSubtitleTestIndex: "srt"}
			if !disabled {
				want[12] = "srt"
			}
			if got := ExternalSubtitleCandidateFormats(source, request.DeviceProfile); !reflect.DeepEqual(got, want) {
				t.Fatalf("external candidates = %v, want %v", got, want)
			}

			request.SubtitleStreamIndex = profileTestPtr(12)
			request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{{Format: "subrip", Method: SubtitleDeliveryMethodEmbed}}
			decision = profileTestDecision(t, source, request, true)
			if decision.SubtitleMethod != SubtitleDeliveryMethodEmbed || decision.SubtitleFormat != "" {
				t.Fatalf("extraction policy changed original-file embedding: %+v", decision)
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("subtitle policy changed caller-owned source facts")
			}
		})
	}
}

func TestHLSSubtitleExtractionPolicyOnlyFiltersNewEmbeddedRenditions(t *testing.T) {
	source, request := hlsSubtitleFixture()
	profile := request.DeviceProfile.TranscodingProfiles[0]
	template := transcode.Plan{Container: "ts", VideoStreamIndex: 2, AudioStreamIndex: 5}
	bound := template
	if reason := configureHLSSubtitleTracks(&bound, source, request, profile, nil); reason != nil {
		t.Fatalf("default rendition binding failed: %+v", reason)
	}
	if tracks := transcode.PlanHLSSubtitles(bound); tracks.Count != 3 {
		t.Fatalf("default rendition set lost eligible tracks: %+v", tracks)
	}
	previous := bound
	source.DisableSubtitleExtraction = true
	filtered := template
	if reason := configureHLSSubtitleTracks(&filtered, source, request, profile, nil); reason != nil {
		t.Fatalf("sidecar rendition binding failed with extraction disabled: %+v", reason)
	}
	if tracks := transcode.PlanHLSSubtitles(filtered); tracks.Count != 1 || tracks.Tracks[0].StreamIndex != 18 || tracks.Tracks[0].ExternalTag == "" {
		t.Fatalf("disabled extraction did not retain only the sidecar rendition: %+v", tracks)
	}
	rejected := template
	if reason := configureHLSSubtitleTracks(&rejected, source, request, profile, &source.Info.Streams[3]); reason == nil {
		t.Fatal("a disabled embedded rendition remained selectable in a new plan")
	}
	if rejected != template {
		t.Fatal("a declined embedded selection published a partial rendition set")
	}

	selected := 12
	if _, err := HLSSubtitleViewFor(bound, &selected, 0); err != nil {
		t.Fatalf("a retained plan lost its already-bound embedded selection: %v", err)
	}
	if metadata, ok := HLSSubtitleMetadata(source, bound, 0); !ok || metadata.Index != selected {
		t.Fatal("a later extraction policy changed the identity of an already-bound track")
	}
	if bound != previous {
		t.Fatal("new rendition policy changed an immutable retained plan")
	}
}

func TestHLSSubtitleExtractionLimitsFilterManifestDefaults(t *testing.T) {
	source, request := hlsSubtitleFixture()
	profile := request.DeviceProfile.TranscodingProfiles[0]
	profile.ManifestSubtitles = "vtt"
	request.DeviceProfile.SubtitleProfiles = nil
	plan := transcode.Plan{Container: "ts", VideoStreamIndex: 2, AudioStreamIndex: 5}
	if reason := configureHLSSubtitle(&plan, source, request, profile, nil, true, ConversionLimits{DisableSubtitleExtraction: true}); reason != nil {
		t.Fatalf("manifest defaults could not retain a sidecar rendition: %+v", reason)
	}
	if tracks := transcode.PlanHLSSubtitles(plan); tracks.Count != 1 || tracks.Tracks[0].StreamIndex != 18 {
		t.Fatalf("manifest defaults bypassed disabled embedded extraction: %+v", tracks)
	}
}

func TestHLSSubtitleExtractionDisabledKeepsExplicitBurnDelivery(t *testing.T) {
	source, request := hlsSubtitleFixture()
	source.DisableSubtitleExtraction = true
	request.DeviceProfile.SubtitleProfiles = []SubtitleProfile{
		{Format: "vtt", Method: SubtitleDeliveryMethodHls, Container: "ts", Protocol: "hls"},
		{Format: "subrip", Method: SubtitleDeliveryMethodEncode, Container: "ts", Protocol: "hls"},
	}
	profile := request.DeviceProfile.TranscodingProfiles[0]
	plan := transcode.Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 2, AudioStreamIndex: 5}
	if reason := configureHLSSubtitle(&plan, source, request, profile, &source.Info.Streams[3], false,
		ConversionLimits{DisableSubtitleExtraction: true, DisableAttachmentExtraction: true}); reason != nil {
		t.Fatalf("disabled delivery extraction blocked explicit burn-in: %+v", reason)
	}
	if plan.Subtitle.Mode != "burn" || plan.Subtitle.StreamIndex != 12 || transcode.HasHLSSubtitles(plan) {
		t.Fatalf("explicit encoded delivery did not retain its burn-in plan: %+v", plan)
	}
}

func TestSubtitleAttachmentExtractionPolicyPreservesASSBurnIn(t *testing.T) {
	for _, test := range []struct {
		name      string
		fonts     int
		policies  []ConversionLimits
		wantFonts string
		decline   bool
	}{
		{name: "legacy font extraction", fonts: 2, wantFonts: "30,31"},
		{name: "enabled font extraction", fonts: 2, policies: []ConversionLimits{{DisableSubtitleExtraction: true}}, wantFonts: "30,31"},
		{name: "disabled font extraction", fonts: 2, policies: []ConversionLimits{{DisableSubtitleExtraction: true, DisableAttachmentExtraction: true}}},
		{name: "legacy excessive font extraction", fonts: 17, decline: true},
		{name: "enabled excessive font extraction", fonts: 17, policies: []ConversionLimits{{DisableSubtitleExtraction: true}}, decline: true},
		{name: "disabled excessive font extraction", fonts: 17, policies: []ConversionLimits{{DisableSubtitleExtraction: true, DisableAttachmentExtraction: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := conversionTestSource()
			source.DisableSubtitleExtraction = true
			selected := media.Stream{Index: 21, Codec: "ass", CodecType: "subtitle", IsTextSubtitleStream: true}
			source.Info.Streams = append(source.Info.Streams, selected)
			for index := test.fonts - 1; index >= 0; index-- {
				source.Info.Streams = append(source.Info.Streams, media.Stream{Index: 30 + index, Codec: "ttf", CodecType: "attachment"})
			}
			original := source
			original.Info.Streams = append([]media.Stream(nil), source.Info.Streams...)
			plan := transcode.Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 2, AudioStreamIndex: 5}
			reason := configureEncodedSubtitle(&plan, source, &selected, test.policies...)
			if test.decline {
				if reason == nil {
					t.Fatal("enabled extraction accepted more font attachments than its bounded budget")
				}
			} else {
				if reason != nil {
					t.Fatalf("ASS burn-in was declined: %+v", reason)
				}
				if plan.Subtitle.Mode != "burn" || plan.Subtitle.Codec != "ass" || plan.Subtitle.StreamIndex != selected.Index ||
					plan.Subtitle.SubtitleOrdinal != 1 || plan.Subtitle.ExternalTag != "" || plan.Subtitle.FontStreams != test.wantFonts {
					t.Fatalf("subtitle or attachment policy changed the selected ASS burn-in: %+v", plan.Subtitle)
				}
			}
			if !reflect.DeepEqual(source, original) {
				t.Fatal("burn-in policy changed caller-owned subtitle or attachment facts")
			}
		})
	}
}
