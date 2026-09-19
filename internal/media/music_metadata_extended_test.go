package media

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestMusicMetadataExplicitCreditsNumberingAndProviderIdentity(t *testing.T) {
	value, err := parseMusicMetadata(json.RawMessage(`{"artist":"Earth, Wind & Fire; Guest / Duo",
		"artists":"[\"Solo A\",\"Solo B\"]","albumartist":"Ensemble","albumartists":"Ensemble;Guest",
		"composer":"Bach & Handel; Studio","composers":"Composer A;Composer B","genres":"Classical;Live",
		"tracknumber":"03/12","disc":"2/3","year":"2024","date":"2024-02-29",
		"musicbrainz_recordingid":"ABCDEF01-2345-6789-ABCD-0123456789AB"}`))
	if err != nil {
		t.Fatal(err)
	}
	if value.Artist != "Earth, Wind & Fire; Guest / Duo" || value.AlbumArtist != "Ensemble" ||
		!reflect.DeepEqual(value.Artists, []string{"Solo A", "Solo B"}) || !reflect.DeepEqual(value.AlbumArtists, []string{"Ensemble", "Guest"}) ||
		!reflect.DeepEqual(value.Composers, []string{"Composer A", "Composer B"}) || !reflect.DeepEqual(value.Genres, []string{"Classical", "Live"}) ||
		value.TrackNumber != 3 || value.TrackTotal != 12 || value.DiscNumber != 2 || value.DiscTotal != 3 || value.Date != "2024-02-29" ||
		value.ProviderIDs["MusicBrainzRecording"] != "abcdef01-2345-6789-abcd-0123456789ab" {
		t.Fatalf("explicit music facts changed: %+v", value)
	}
	scalar, err := parseMusicMetadata(json.RawMessage(`{"artist":"A & B; C / D","composer":"A & B; C / D","genre":"Jazz / Rock"}`))
	if err != nil || len(scalar.Artists) != 0 || !reflect.DeepEqual(scalar.Composers, []string{"A & B; C / D"}) ||
		!reflect.DeepEqual(scalar.Genres, []string{"Jazz / Rock"}) {
		t.Fatal("scalar credit punctuation manufactured relationships", err)
	}
}

func TestMusicMetadataExtendedFieldsRejectConflictsAndMalformedValues(t *testing.T) {
	for _, input := range []string{
		`{"album_artist":"A","albumartist":"B"}`, `{"track":"3/12","tracknumber":"4/12"}`,
		`{"artists":"A;;B"}`, `{"artists":"[null]"}`, `{"artists":"[\"\\ud800\"]"}`,
		`{"composers":["A"]}`, `{"track":"0"}`, `{"track":"4/3"}`, `{"disc":"-1"}`,
		`{"track":"1.0"}`, `{"disc":"2147483648"}`, `{"date":"2023-02-29"}`,
		`{"year":"2023","date":"2024-01-01"}`, `{"musicbrainz_trackid":"not-a-uuid"}`,
	} {
		if _, err := parseMusicMetadata(json.RawMessage(input)); !errors.Is(err, ErrInvalidMusicMetadata) {
			t.Fatalf("invalid music tag accepted: %s %v", input, err)
		}
	}
}

func TestMusicMetadataVersionTwoCacheIsNotRelabeledByDecoding(t *testing.T) {
	var old Info
	data := []byte(`{"ProbeVersion":7,"EmbeddedMusic":{"Version":2,"Title":"Old","Artist":"A & B","AlbumArtist":"Ensemble"}}`)
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	if old.EmbeddedMusic.Version != 2 || len(old.EmbeddedMusic.Artists) != 0 || old.ProbeVersion != 7 {
		t.Fatal("cache decoding invented new facts")
	}
	if err := ValidateMusicMetadata(*old.EmbeddedMusic); !errors.Is(err, ErrInvalidMusicMetadata) {
		t.Fatal("version two facts bypassed refresh")
	}
}
