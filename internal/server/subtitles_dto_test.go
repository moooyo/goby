package server

import (
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestExternalSubtitleDisplayTitlesPreserveSourceFacts(t *testing.T) {
	for _, test := range []struct {
		name, language, title, codec, displayTitle, displayLanguage string
		forced, sdh                                                 bool
	}{
		{name: "stored language title", language: "en", title: "en", codec: "srt", displayTitle: "English (SRT)", displayLanguage: "English"},
		{name: "empty title", language: "en", codec: "srt", displayTitle: "English (SRT)", displayLanguage: "English"},
		{name: "three letter language", language: "eng", title: "eng", codec: "vtt", displayTitle: "English (VTT)", displayLanguage: "English"},
		{name: "same language native vtt", language: "en", title: "en", codec: "vtt", displayTitle: "English (VTT)", displayLanguage: "English"},
		{name: "reference forced vtt", language: "en", title: "en", codec: "vtt", forced: true, displayTitle: "English (Forced VTT)", displayLanguage: "English"},
		{name: "goby sdh display policy", language: "en", title: "en", codec: "srt", sdh: true, displayTitle: "English (SDH SRT)", displayLanguage: "English"},
		{name: "unknown language", language: "zxx-Qaaa", title: "zxx-Qaaa", codec: "srt", displayTitle: "zxx-Qaaa (SRT)", displayLanguage: "zxx-Qaaa"},
		{name: "missing language", codec: "srt", displayTitle: "(SRT)"},
		{name: "blank language and title", language: " ", title: "\t", codec: "vtt", displayTitle: "(VTT)"},
		{name: "title does not imply language", title: "English commentary", codec: "srt", displayTitle: "English commentary (SRT)"},
		{name: "custom title and flags", language: "en", title: "Director's notes \u2014 \u96ea", codec: "srt", forced: true, sdh: true,
			displayTitle: "Director's notes \u2014 \u96ea (Forced SDH SRT)", displayLanguage: "English"},
		{name: "custom spacing", language: "en", title: "  Alternate dialogue  ", codec: "vtt",
			displayTitle: "  Alternate dialogue   (VTT)", displayLanguage: "English"},
		{name: "missing codec", language: "en", title: "en", displayTitle: "English", displayLanguage: "English"},
		{name: "unknown codec", language: "en", title: "en", codec: "unrecognized", displayTitle: "English (UNRECOGNIZED)", displayLanguage: "English"},
		{name: "missing all facts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := library.Subtitle{Index: 7, Language: test.language, Title: test.title, Codec: test.codec,
				IsDefault: true, IsForced: test.forced, IsHearingImpaired: test.sdh, Filename: "movie.subtitle"}
			embedded := media.Stream{Index: 2, CodecType: "subtitle", Codec: "subrip", Language: "fr", Title: "Embedded custom", IsForced: true}
			item := library.Item{ID: "owned-item", Path: "/media/movie.mp4", Media: &media.Info{Streams: []media.Stream{embedded}},
				Subtitles: []library.Subtitle{source}}
			streams := itemMediaStreamsDTO(item)
			if len(streams) != 2 {
				t.Fatal("the external title projection changed stream membership")
			}
			if streams[0]["DisplayTitle"] != "Embedded custom" {
				t.Error("external title formatting changed an embedded stream")
			}
			got := streams[1]
			if got["DisplayTitle"] != test.displayTitle {
				t.Errorf("DisplayTitle = %q, want %q", got["DisplayTitle"], test.displayTitle)
			}
			if test.displayLanguage == "" {
				if _, exists := got["DisplayLanguage"]; exists {
					t.Error("a missing language acquired an invented display name")
				}
			} else if got["DisplayLanguage"] != test.displayLanguage {
				t.Errorf("DisplayLanguage = %q, want %q", got["DisplayLanguage"], test.displayLanguage)
			}
			for field, want := range map[string]any{"Index": source.Index, "Type": "Subtitle", "Codec": source.Codec,
				"Language": source.Language, "Title": source.Title, "IsDefault": true, "IsForced": test.forced,
				"IsHearingImpaired": test.sdh, "IsExternal": true, "IsTextSubtitleStream": true} {
				if got[field] != want {
					t.Errorf("display projection changed source field %s", field)
				}
			}
			if !reflect.DeepEqual(item.Media.Streams, []media.Stream{embedded}) || !reflect.DeepEqual(item.Subtitles, []library.Subtitle{source}) {
				t.Error("display projection mutated indexed source facts")
			}
		})
	}
}
