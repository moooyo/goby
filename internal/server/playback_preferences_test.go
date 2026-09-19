package server

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

func preferenceIndex(value int) *int { return &value }

func TestPlaybackPreferencesRespectExplicitSelectionsOffAndCurrentTrackFacts(t *testing.T) {
	info := media.Info{ProbeVersion: media.CurrentProbeVersion, Streams: []media.Stream{
		{Index: 2, CodecType: "audio", Language: "deu", IsDefault: true},
		{Index: 5, CodecType: "audio", Language: "eng"},
		{Index: 7, CodecType: "subtitle", Language: "eng", IsDefault: true},
		{Index: 8, CodecType: "subtitle", Language: "eng", IsForced: true},
		{Index: 9, CodecType: "subtitle", Language: "eng", IsHearingImpaired: true},
	}}
	configuration := identity.DefaultUserConfiguration()
	configuration.AudioLanguagePreference, configuration.SubtitleLanguagePreference = "en", "eng"
	configuration.PlayDefaultAudioTrack = false
	configuration.SubtitleMode = "HearingImpaired"
	request := playback.Request{}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{}, &request)
	if request.AudioStreamIndex == nil || *request.AudioStreamIndex != 5 || request.SubtitleStreamIndex == nil || *request.SubtitleStreamIndex != 9 {
		t.Fatal("preferred language and measured hearing-impaired track were not selected")
	}
	explicit := playback.Request{AudioStreamIndex: preferenceIndex(2), SubtitleStreamIndex: preferenceIndex(-1)}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{AudioStreamIndex: preferenceIndex(5), SubtitleStreamIndex: preferenceIndex(9)}, &explicit)
	if *explicit.AudioStreamIndex != 2 || *explicit.SubtitleStreamIndex != -1 {
		t.Fatal("stored defaults replaced explicit playback selections")
	}
	configuration.SubtitleMode = "Always"
	off := playback.Request{}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{SubtitleStreamIndex: preferenceIndex(-1)}, &off)
	if off.SubtitleStreamIndex == nil || *off.SubtitleStreamIndex != -1 {
		t.Fatal("remembered off was confused with an unset selection")
	}
	stale := playback.Request{}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{AudioStreamIndex: preferenceIndex(7), SubtitleStreamIndex: preferenceIndex(5)}, &stale)
	if stale.AudioStreamIndex == nil || *stale.AudioStreamIndex != 5 || stale.SubtitleStreamIndex == nil || *stale.SubtitleStreamIndex != 7 {
		t.Fatal("remembered indexes were not checked against current stream kinds")
	}
	configuration.SubtitleMode = "None"
	disabled := playback.Request{}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{SubtitleStreamIndex: preferenceIndex(7)}, &disabled)
	if disabled.SubtitleStreamIndex == nil || *disabled.SubtitleStreamIndex != -1 {
		t.Fatal("a remembered subtitle defeated the current disabled mode")
	}
}

func TestHearingImpairedPreferenceTreatsOlderProbeAbsenceAsUnknown(t *testing.T) {
	configuration := identity.DefaultUserConfiguration()
	configuration.SubtitleMode = "HearingImpaired"
	info := media.Info{ProbeVersion: 7, Streams: []media.Stream{{Index: 1, CodecType: "audio"}, {Index: 2, CodecType: "subtitle", IsDefault: true}}}
	request := playback.Request{}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{}, &request)
	if request.SubtitleStreamIndex != nil {
		t.Fatal("old probe absence was mistaken for a measured lack of hearing-impaired captions")
	}
	info.ProbeVersion = media.CurrentProbeVersion
	info.Streams[1].IsHearingImpaired = true
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{}, &request)
	if request.SubtitleStreamIndex == nil || *request.SubtitleStreamIndex != 2 {
		t.Fatal("refreshed hearing-impaired facts were ignored")
	}
	info.ProbeVersion = 7
	info.Streams[1].IsHearingImpaired = false
	info.Streams = append(info.Streams, media.Stream{Index: 10, CodecType: "subtitle", IsExternal: true, IsHearingImpaired: true})
	request = playback.Request{}
	applyPreferredStreams(configuration, info, library.PlaybackPreferences{}, &request)
	if request.SubtitleStreamIndex == nil || *request.SubtitleStreamIndex != 10 {
		t.Fatal("known sidecar hearing-impaired facts were discarded with older primary metadata")
	}
}

func TestPreferenceRevisionUsesCanonicalInt64Strings(t *testing.T) {
	for _, raw := range []string{`1`, `1.0`, `null`, `"01"`, `"-1"`, `" 1 "`, `"9223372036854775808"`, `"0"`} {
		if _, ok := preferenceRevision(json.RawMessage(raw), false); ok {
			t.Fatalf("invalid native revision %s was accepted", raw)
		}
	}
	if value, ok := preferenceRevision(json.RawMessage(`"9223372036854775807"`), false); !ok || value != 9223372036854775807 {
		t.Fatal("maximum int64 revision lost precision")
	}
	if value, ok := preferenceRevision(json.RawMessage(`"0"`), true); !ok || value != 0 {
		t.Fatal("unsaved display scope revision was rejected")
	}
}

func TestDynamicPreferencesUseOnlyTheCurrentGenerationFactsAndExplicitChoices(t *testing.T) {
	principal := identity.Principal{Kind: "emby", User: identity.User{Configuration: json.RawMessage(`{"PlayDefaultAudioTrack":false,"AudioLanguagePreference":"fra","SubtitleLanguagePreference":"fra","SubtitleMode":"Always"}`)}}
	first := media.Info{ProbeVersion: media.CurrentProbeVersion, Streams: []media.Stream{
		{Index: 1, CodecType: "audio", Language: "eng", IsDefault: true}, {Index: 3, CodecType: "audio", Language: "fra"},
		{Index: 5, CodecType: "subtitle", Language: "fra"},
	}}
	request := playback.Request{}
	ApplyDynamicUserPreferences(principal, first, &request)
	if request.AudioStreamIndex == nil || *request.AudioStreamIndex != 3 || request.SubtitleStreamIndex == nil || *request.SubtitleStreamIndex != 5 {
		t.Fatal("live language preferences did not consume actual source facts")
	}
	second := media.Info{ProbeVersion: media.CurrentProbeVersion, Streams: []media.Stream{
		{Index: 3, CodecType: "audio", Language: "eng", IsDefault: true}, {Index: 8, CodecType: "audio", Language: "fra"},
		{Index: 5, CodecType: "video"}, {Index: 12, CodecType: "subtitle", Language: "fra"},
	}}
	request = playback.Request{}
	ApplyDynamicUserPreferences(principal, second, &request)
	if request.AudioStreamIndex == nil || *request.AudioStreamIndex != 8 || request.SubtitleStreamIndex == nil || *request.SubtitleStreamIndex != 12 {
		t.Fatal("a live generation inherited indexes from the previous track layout")
	}
	request = playback.Request{AudioStreamIndex: preferenceIndex(3), SubtitleStreamIndex: preferenceIndex(-1)}
	ApplyDynamicUserPreferences(principal, second, &request)
	if *request.AudioStreamIndex != 3 || *request.SubtitleStreamIndex != -1 {
		t.Fatal("dynamic defaults replaced explicit selections")
	}
}

func TestPlaybackSourceProjectsTheSameIntroMarkersAsTheItem(t *testing.T) {
	item := library.Item{ID: "source-bound-item", Type: "Episode", Path: "/media/episode.mp4", CanPlay: true,
		Media: &media.Info{ProbeVersion: media.CurrentProbeVersion, FileChangeTimeNs: 1, DurationTicks: 600 * media.TicksPerSecond,
			Chapters: []media.Chapter{{StartTicks: 0, EndTicks: 100 * media.TicksPerSecond, Title: "Chapter one"}}},
		Intro: &library.IntroInterval{StartTicks: 10 * media.TicksPerSecond, EndTicks: 45 * media.TicksPerSecond, Provenance: "Manual"}}
	dto := originalSourceDTO(item)
	chapters, ok := dto["Chapters"].([]map[string]any)
	if !ok || !reflect.DeepEqual(chapters, itemChaptersDTO(item)) {
		t.Fatal("the selected media source overrode the item's source-bound intro markers")
	}
	markers := make(map[string]int64)
	for _, chapter := range chapters {
		if kind, _ := chapter["MarkerType"].(string); kind == "IntroStart" || kind == "IntroEnd" {
			markers[kind], _ = chapter["StartPositionTicks"].(int64)
		}
	}
	if len(markers) != 2 || markers["IntroStart"] != item.Intro.StartTicks || markers["IntroEnd"] != item.Intro.EndTicks {
		t.Fatal("playback source omitted or changed the intro interval")
	}
	if _, exists := dto["StartTimeTicks"]; exists {
		t.Fatal("marker projection added a second server-owned seek")
	}
	item.Intro = nil
	for _, chapter := range originalSourceDTO(item)["Chapters"].([]map[string]any) {
		if chapter["MarkerType"] == "IntroStart" || chapter["MarkerType"] == "IntroEnd" {
			t.Fatal("ordinary chapter presence invented an intro interval")
		}
	}
}
