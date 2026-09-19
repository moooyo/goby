package library

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

func TestMusicMetadataAcceptedFieldsPreserveLocalPrecedenceAndLegacySourceHash(t *testing.T) {
	legacy := []byte(`{"Version":1,"Name":"Legacy","Artists":["A & B; C"],"AlbumArtists":[]}`)
	parsed, accepted, err := decodeAcceptedMusicSource(legacy)
	if err != nil || !accepted {
		t.Fatal(err)
	}
	encoded, err := encodeAcceptedMusicSource(parsed)
	if err != nil {
		t.Fatal(err)
	}
	before, err := acceptedMusicSourceHash(legacy)
	after, afterErr := acceptedMusicSourceHash(encoded)
	if err != nil || afterErr != nil || before != after {
		t.Fatal("optional music fields rotated a legacy accepted source identity")
	}
	facts := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Title: "Tagged", Album: "Album", Artist: "A & B; C",
		Artists: []string{"Artist One", "Artist Two"}, AlbumArtists: []string{"Ensemble"}, Composers: []string{"Composer One", "Composer Two"},
		Genres: []string{"Classical"}, TrackNumber: 3, DiscNumber: 2, Year: 2024, Date: "2024-02-29",
		ProviderIDs: map[string]string{"MusicBrainzRecording": "abcdef01-2345-6789-abcd-0123456789ab"}}
	source, valid := musicSourceFromProbe(&media.Info{EmbeddedMusic: &facts})
	if !valid || !reflect.DeepEqual(source.Artists, facts.Artists) || source.IndexNumber == nil || *source.IndexNumber != 3 {
		t.Fatal("current tags lost explicit credits or numbering")
	}
	raw, err := encodeAcceptedMusicSource(source)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := mergeAcceptedMusicSource([]byte(`{"Name":"NFO title","Genres":["NFO genre"],"IndexNumber":9}`), raw)
	if err != nil {
		t.Fatal(err)
	}
	var got metadata.Metadata
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "NFO title" || !reflect.DeepEqual(got.Genres, []string{"NFO genre"}) || *got.IndexNumber != 9 || *got.ParentIndexNumber != 2 ||
		len(got.People) != 2 || got.People[0].Type != "Composer" || got.People[1].Name != "Composer Two" ||
		got.ProviderIDs["MusicBrainzRecording"] != facts.ProviderIDs["MusicBrainzRecording"] {
		t.Fatalf("NFO priority or indexed music facts changed: %+v", got)
	}
	// A partial date may establish a year but never invent a release day.
	facts.Date = "2024-02"
	source, valid = musicSourceFromProbe(&media.Info{EmbeddedMusic: &facts})
	if !valid || source.PremiereDate != nil || source.ProductionYear == nil || *source.ProductionYear != 2024 {
		t.Fatal("partial date invented a day")
	}
}

func TestMusicMetadataEditableFieldsLocksAndCreditArrays(t *testing.T) {
	for _, kind := range []string{"Audio", "MusicAlbum", "MusicArtist"} {
		fields := editableMetadataFields(kind)
		for _, field := range []string{"Album", "Artists", "AlbumArtists"} {
			if !slices.Contains(fields, field) {
				t.Fatalf("%s omitted music field %s", kind, field)
			}
		}
		if slices.Contains(fields, "IndexNumber") != (kind == "Audio") || slices.Contains(fields, "ParentIndexNumber") != (kind == "Audio") {
			t.Fatal("music numbering changed structural folder controls")
		}
	}
	if slices.Contains(editableMetadataFields("Movie"), "Artists") || slices.Contains(editableMetadataFields("Episode"), "ParentIndexNumber") {
		t.Fatal("music controls leaked to unrelated types")
	}
	manual, err := normalizeMetadataValue("Artists", json.RawMessage(`[" A & B; C / D ","Solo","Solo"]`))
	var credits []string
	if err != nil || json.Unmarshal(manual, &credits) != nil || !reflect.DeepEqual(credits, []string{"A & B; C / D", "Solo"}) {
		t.Fatalf("explicit artists were split or duplicated: %s %v", manual, err)
	}
	locked, err := normalizeMetadataLockValue("Artists", json.RawMessage(`["  Exact accepted artist  "]`))
	if err != nil || string(locked) != `["  Exact accepted artist  "]` {
		t.Fatal("locking rewrote accepted music spelling", err)
	}
	for _, raw := range []string{`null`, `"A;B"`, `[""]`, `["A\nB"]`} {
		if _, err := normalizeMetadataValue("Artists", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid manual music credits accepted", raw)
		}
	}
}

func TestMusicAlbumExtendedFactsRequireReleaseConsensus(t *testing.T) {
	var album albumMusicMetadata
	first := media.MusicMetadata{Version: media.CurrentMusicMetadataVersion, Year: 2024, Date: "2024-02-29", Genres: []string{"Classical"},
		Composers: []string{"One"}, ProviderIDs: map[string]string{"MusicBrainzReleaseGroup": "abcdef01-2345-6789-abcd-0123456789ab"}}
	second := first
	second.Year, second.Date, second.ProviderIDs = 2023, "2023-01-01", nil
	second.Genres, second.Composers = []string{"Live", "Classical"}, []string{"Two", "One"}
	if !album.add(first) || !album.add(second) {
		t.Fatal("bounded album aggregate rejected")
	}
	var source musicMetadataSource
	album.apply(&source)
	if source.ProductionYear != nil || source.PremiereDate != nil || len(source.ProviderIDs) != 0 ||
		!reflect.DeepEqual(source.Genres, []string{"Classical", "Live"}) || !reflect.DeepEqual(source.Composers, []string{"One", "Two"}) {
		t.Fatalf("album aggregate invented release agreement: %+v", source)
	}
}

func TestAcceptedTrackMusicRejectsNullExtendedCacheFacts(t *testing.T) {
	for _, field := range []string{"Artists", "AlbumArtists", "Composers", "Genres", "TrackNumber", "DiscNumber", "Year", "Date", "ProviderIDs"} {
		raw, err := json.Marshal(map[string]any{"Version": media.CurrentMusicMetadataVersion, field: nil})
		if err != nil {
			t.Fatal(err)
		}
		if _, accepted := acceptedTrackMusic(raw); accepted {
			t.Fatal("null cache field was relabeled as observed absence", field)
		}
	}
}
