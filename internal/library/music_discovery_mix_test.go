package library

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMusicMixSeedNamespacesAndCanonicalEntityIDs(t *testing.T) {
	for _, seed := range []MusicMixSeed{
		{Kind: "Item", ID: "Opaque-123"}, {Kind: "Item", ID: "0007"}, {Kind: "Song", ID: "7"},
		{Kind: "Album", ID: "Album-A"}, {Kind: "Playlist", ID: "Playlist-A"},
		{Kind: "Artist", ID: "7"}, {Kind: "Genre", ID: "9223372036854775807"}, {Kind: "GenreName", Name: "Rock / Pop"},
	} {
		if got, err := normalizeMusicMixSeed(seed); err != nil || got != seed {
			t.Fatalf("valid typed seed changed: %#v, %v", got, err)
		}
	}
	for _, seed := range []MusicMixSeed{
		{}, {Kind: "Entity", ID: "7"}, {Kind: "Artist", ID: "0007"}, {Kind: "Artist", ID: "+7"},
		{Kind: "Genre", ID: "0"}, {Kind: "Genre", ID: "9223372036854775808"}, {Kind: "Song", ID: " a"},
		{Kind: "Item", ID: "a\x00b"}, {Kind: "Item", ID: strings.Repeat("a", 257)},
		{Kind: "GenreName", Name: " "}, {Kind: "GenreName", Name: "Rock", ID: "1"}, {Kind: "Artist", ID: "1", Name: "Name"},
	} {
		if _, err := normalizeMusicMixSeed(seed); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("ambiguous or malformed seed accepted: %#v", seed)
		}
	}
}

func TestMusicPrefixesKeepSearchableUnicodeInitialsAndStringCounts(t *testing.T) {
	cases := map[string]string{"alice": "A", "ALPHA": "A", "\u5468\u6770\u4f26": "\u5468", "\u00e9lan": "\u00c9", "7 notes": "7", "!Mix": "!", "\U0001f3b5 song": "\U0001f3b5", "": "", "\x00bad": "", "\xff": ""}
	counts := map[string]int64{}
	for name, want := range cases {
		if got := musicNameInitial(name); got != want {
			t.Fatalf("initial %q = %q, want %q", name, got, want)
		} else if got != "" {
			counts[got]++
		}
	}
	want := []NamePrefix{{"!", "1"}, {"7", "1"}, {"A", "2"}, {"\u00c9", "1"}, {"\u5468", "1"}, {"\U0001f3b5", "1"}}
	if got := orderedMusicPrefixes(counts); !reflect.DeepEqual(got, want) {
		t.Fatalf("prefix order/count = %#v", got)
	}
}
