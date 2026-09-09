package library

import (
	"fmt"
	"reflect"
	"testing"
)

func TestSubtitleDirectoryIndexUsesCompleteMediaBasenames(t *testing.T) {
	names := []string{"Feature.mkv", "Feature.en.mkv", "Feature extended.mkv", "Feature.srt",
		"Feature.EN.SRT", "Feature.en.fr.forced.VTT", "Feature.zh-CN.default.forced.sdh.VTT",
		"Feature extended.srt", "Feature2.srt", "Feature.unknown.srt", "Feature.en.mkv.srt",
		"Feature.en.fr.commentary.vtt", "../Feature.fr.srt", `nested\Feature.fr.srt`,
		"Feature.fr.forced.forced.srt", "Feature.fr.srt.tmp", "Feature.ass"}
	index := newSubtitleDirectoryIndex(names, nil)
	tracks, overflow := index.candidates("Feature.mkv")
	if overflow || !reflect.DeepEqual(subtitleCandidateFilenames(tracks), []string{"Feature.srt", "Feature.zh-CN.default.forced.sdh.VTT"}) {
		t.Fatalf("base movie received another movie's subtitle or an unsupported name: %+v, overflow=%t", tracks, overflow)
	}
	flags := tracks[1].info
	if flags.Language != "zh-cn" || !flags.IsDefault || !flags.IsForced || !flags.IsHearingImpaired || flags.Codec != "vtt" || flags.MIMEType != "text/vtt" {
		t.Fatalf("subtitle language and flags were not preserved: %+v", flags)
	}
	tracks, overflow = index.candidates("Feature.en.mkv")
	if overflow || !reflect.DeepEqual(subtitleCandidateFilenames(tracks), []string{"Feature.EN.SRT", "Feature.en.fr.forced.VTT", "Feature.en.mkv.srt"}) {
		t.Fatalf("specific media basename did not own its matching sidecars: %+v", tracks)
	}
	if tracks[0].info.Language != "" || tracks[1].info.Language != "fr" || !tracks[1].info.IsForced {
		t.Fatalf("specific basename was incorrectly parsed as a language: %+v", tracks)
	}
	tracks, _ = index.candidates("Feature extended.mkv")
	if !reflect.DeepEqual(subtitleCandidateFilenames(tracks), []string{"Feature extended.srt"}) {
		t.Fatalf("prefix collision changed extended movie tracks: %+v", tracks)
	}
}

func TestSubtitleDirectoryIndexBoundsEachGroupAndOwnsItsNames(t *testing.T) {
	names := []string{"Feature.mkv", "Other.mkv", "Other.srt"}
	for number := 0; number < 100; number++ {
		names = append(names, fmt.Sprintf("Feature.en-%02d.srt", number))
	}
	index := newSubtitleDirectoryIndex(names, nil)
	names[2] = "changed.srt"
	tracks, overflow := index.candidates("Feature.mkv")
	if !overflow || len(tracks) != maxActiveSubtitles {
		t.Fatalf("subtitle candidate group was not bounded: count=%d overflow=%t", len(tracks), overflow)
	}
	other, overflow := index.candidates("Other.mkv")
	if overflow || !reflect.DeepEqual(subtitleCandidateFilenames(other), []string{"Other.srt"}) {
		t.Fatalf("one overflow group or caller mutation affected another group: %+v", other)
	}
}

func TestSubtitleDirectoryIndexHandlesUnicodeBasenames(t *testing.T) {
	index := newSubtitleDirectoryIndex([]string{"Kelvin.mkv", "Kelvin.en.srt", "電影.mkv", "電影.zh-Hant.srt"}, nil)
	for _, name := range []string{"Kelvin.mkv", "電影.mkv"} {
		tracks, overflow := index.candidates(name)
		if len(tracks) != 1 || overflow {
			t.Fatalf("Unicode basename %q lost its track: %+v", name, tracks)
		}
	}
}

func TestSubtitleNameMetadataAcceptsOptionalLanguageAndFlagsInAnyOrder(t *testing.T) {
	for _, test := range []struct {
		suffix                           string
		language                         string
		isDefault, forced, sdh, accepted bool
	}{
		{suffix: ".forced", forced: true, accepted: true},
		{suffix: ".default", isDefault: true, accepted: true},
		{suffix: ".sdh", sdh: true, accepted: true},
		{suffix: ".FORCED.default.SDH", isDefault: true, forced: true, sdh: true, accepted: true},
		{suffix: ".forced.ZH-Hant.sdh.default", language: "zh-hant", isDefault: true, forced: true, sdh: true, accepted: true},
		{suffix: ".default.sdh.forced.en", language: "en", isDefault: true, forced: true, sdh: true, accepted: true},
		{suffix: ".en.fr"},
		{suffix: ".en.EN"},
		{suffix: ".forced.FORCED"},
		{suffix: ".sdh.en.SDH"},
		{suffix: ".default.default"},
		{suffix: ".forced.en.sdh.default.fr"},
		{suffix: ".forced..en"},
	} {
		t.Run(test.suffix, func(t *testing.T) {
			track, accepted := subtitleNameMetadata("Feature"+test.suffix+".srt", test.suffix, "srt")
			if accepted != test.accepted || (accepted && (track.Language != test.language || track.Title != test.language ||
				track.IsDefault != test.isDefault || track.IsForced != test.forced || track.IsHearingImpaired != test.sdh)) {
				t.Fatalf("subtitle suffix metadata = %+v, accepted=%t; want %+v", track, accepted, test)
			}
		})
	}
}

func subtitleCandidateFilenames(candidates []subtitleCandidate) []string {
	names := make([]string, len(candidates))
	for index, candidate := range candidates {
		names[index] = candidate.filename
	}
	return names
}
