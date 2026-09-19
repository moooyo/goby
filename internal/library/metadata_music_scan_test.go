package library

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestAcceptedTrackMusicSeparatesProbeVersionFromStoredSourceVersion(t *testing.T) {
	facts := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Exact title",
		Album: "Exact album", Artist: "Track A / B", AlbumArtist: "Album A; B"}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal("encode accepted current probe facts")
	}
	decoded, accepted := acceptedTrackMusic(raw)
	if !accepted || !reflect.DeepEqual(decoded, facts) {
		t.Fatal("accepted probe facts lost the distinct scalar artist roles")
	}
	source, accepted := musicSourceFromProbe(&media.Info{ProbeVersion: 6, EmbeddedMusic: &facts})
	if !accepted || !reflect.DeepEqual(source, musicMetadataSource{Version: 1, Name: facts.Title, Album: facts.Album,
		Artists: []string{facts.Artist}, AlbumArtists: []string{facts.AlbumArtist}}) {
		t.Fatal("probe refresh version leaked into the stable source format or inferred scalar separators")
	}
	encoded, err := encodeAcceptedMusicSource(source)
	if err != nil {
		t.Fatalf("encode unchanged source format after a probe upgrade: %v", err)
	}
	if retained, extracted, err := decodeAcceptedMusicSource(encoded); err != nil || !extracted || !reflect.DeepEqual(retained, source) {
		t.Fatal("the existing source format stopped being readable after a music probe upgrade")
	}
	legacy := []byte(`{"Version":1,"Name":"Retained v1 source","Album":"Retained album","Artists":["A"],"AlbumArtists":["B"]}`)
	if retained, extracted, err := decodeAcceptedMusicSource(legacy); err != nil || !extracted || retained.Version != 1 ||
		len(retained.AlbumArtists) != 1 || retained.AlbumArtists[0] != "B" {
		t.Fatal("a persisted version-one source became unsupported after the separate probe upgrade")
	}
	if _, accepted := acceptedTrackMusic([]byte(`{"Version":1,"Title":"Old probe","Album":"Old album","Artist":"A"}`)); accepted {
		t.Fatal("an old probe without album_artist extraction became a complete current album member")
	}
}

func TestAcceptedTrackMusicRejectsUnsupportedOrUnboundedAlbumArtistFacts(t *testing.T) {
	for _, test := range []struct{ name, raw string }{
		{"missing version", `{"AlbumArtist":"A"}`},
		{"unknown version", `{"Version":` + strconv.Itoa(media.CurrentMusicMetadataVersion+1) + `,"AlbumArtist":"A"}`},
		{"legacy plural cache", `{"Version":2,"AlbumArtists":["A"]}`},
		{"format tag spelling", `{"Version":2,"album_artist":"A"}`},
		{"wrong case", `{"Version":2,"Albumartist":"A"}`},
		{"duplicate", `{"Version":2,"AlbumArtist":"A","AlbumArtist":"B"}`},
		{"null", `{"Version":2,"AlbumArtist":null}`},
		{"number", `{"Version":2,"AlbumArtist":1}`},
		{"boolean", `{"Version":2,"AlbumArtist":true}`},
		{"array", `{"Version":2,"AlbumArtist":["A"]}`},
		{"object", `{"Version":2,"AlbumArtist":{"Name":"A"}}`},
		{"control", `{"Version":2,"AlbumArtist":"A\u0085B"}`},
		{"whitespace control", `{"Version":2,"AlbumArtist":"\t"}`},
		{"oversized", `{"Version":2,"AlbumArtist":"` + strings.Repeat("a", 1025) + `"}`},
		{"invalid utf8", "{\"Version\":2,\"AlbumArtist\":\"" + string([]byte{0xff}) + "\"}"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := test.raw
			// Malformed current facts must fail for their content, not merely
			// because the older fixture version now requires a refresh.
			if test.name != "legacy plural cache" {
				raw = strings.Replace(raw, `"Version":2`, `"Version":`+strconv.Itoa(media.CurrentMusicMetadataVersion), 1)
			}
			if _, accepted := acceptedTrackMusic([]byte(raw)); accepted {
				t.Fatal("invalid persisted probe facts were accepted as a complete member")
			}
		})
	}
	current, accepted := acceptedTrackMusic([]byte(`{"Version":3,"AlbumArtist":"A","Artists":["One","Two"]}`))
	if !accepted || current.Version != 3 || !reflect.DeepEqual(current.Artists, []string{"One", "Two"}) {
		t.Fatal("supported version-three explicit credits were rejected")
	}
	for _, value := range []string{strings.Repeat("a", 1025), "\n", string([]byte{0xff})} {
		facts := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, AlbumArtist: value}
		if _, accepted := musicSourceFromProbe(&media.Info{EmbeddedMusic: &facts}); accepted {
			t.Fatal("a malformed typed album artist became an accepted source")
		}
	}
	text := strings.Repeat("\u00e9", 512)
	facts := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: text, Album: text, Artist: text, AlbumArtist: text}
	raw, err := json.Marshal(facts)
	if err != nil {
		t.Fatal("encode the exact four-field UTF-8 byte boundary")
	}
	if decoded, accepted := acceptedTrackMusic(raw); !accepted || !reflect.DeepEqual(decoded, facts) {
		t.Fatal("the exact 4096-byte accepted probe facts were rejected or truncated")
	}
	for _, artist := range []string{"", "  "} {
		facts := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Artist: "Track artist", AlbumArtist: artist}
		source, accepted := musicSourceFromProbe(&media.Info{EmbeddedMusic: &facts})
		if !accepted || source.AlbumArtists == nil || len(source.AlbumArtists) != 0 {
			t.Fatal("a missing or blank album_artist invented an explicit track relationship")
		}
	}
}
