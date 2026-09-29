package providers

import "testing"

func TestMetadataIDUsesMusicBrainzEntityBeforeLegacyFallback(t *testing.T) {
	ids := map[string]string{
		"MusicBrainz":             "generic",
		"MusicBrainzReleaseGroup": "album",
		"MusicBrainzArtist":       "artist",
		"MusicBrainzRecording":    "recording",
		"MusicBrainzRelease":      "release",
	}
	for _, test := range []struct{ itemType, want string }{
		{"MusicAlbum", "album"}, {"MusicArtist", "artist"}, {"Audio", "recording"}, {"Movie", ""},
	} {
		t.Run(test.itemType, func(t *testing.T) {
			if got := MetadataID("musicbrainz", test.itemType, ids); got != test.want {
				t.Fatalf("selected %q, want %q", got, test.want)
			}
		})
	}
	if got := MetadataID("musicbrainz", "Audio", map[string]string{"musicbrainzrecording": " typed ", "MusicBrainz": "generic"}); got != "typed" {
		t.Fatalf("imported typed identifier lost precedence: %q", got)
	}
	if got := MetadataID("musicbrainz", "MusicAlbum", map[string]string{"MusicBrainzReleaseGroup": " ", "musicbrainz": "legacy"}); got != "legacy" {
		t.Fatalf("missing typed identifier did not use the legacy fallback: %q", got)
	}
	if got := MetadataID("musicbrainz", "MusicAlbum", map[string]string{"MusicBrainzRecording": "track", "MusicBrainzRelease": "edition"}); got != "" {
		t.Fatalf("an unrelated MusicBrainz entity was selected: %q", got)
	}
	if got := MetadataID("tmdb", "Movie", map[string]string{"TMDB": "42", "MusicBrainz": "other"}); got != "42" {
		t.Fatalf("TMDB lookup changed: %q", got)
	}
}
