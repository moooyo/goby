package providers

import (
	"errors"
	"net/http"
	"testing"
)

func TestOpaqueMetadataIDIsNotAnOnlineLookupAddress(t *testing.T) {
	const id = "https://example.invalid/local-identifier/7"
	client := openSubtitlesContractClient()
	client.config.TMDBToken = "fixture-token"
	client.config.MusicBrainzUserAgent = "Offline test client"
	client.http = &http.Client{Transport: providerRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("opaque provider ID reached the HTTP transport")
		return nil, ErrUnavailable
	})}

	ctx := t.Context()
	if _, err := client.Lookup(ctx, Selection{Provider: "tmdb", Type: "Movie", ID: id}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("TMDB lookup error = %v, want invalid input", err)
	}
	if _, err := client.Images(ctx, Selection{Provider: "tmdb", Type: "Series", ID: id}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("TMDB images error = %v, want invalid input", err)
	}
	if _, err := client.TVEpisodeGroups(ctx, id); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("TMDB episode groups error = %v, want invalid input", err)
	}
	if _, err := client.Lookup(ctx, Selection{Provider: "musicbrainz", Type: "MusicAlbum", ID: id}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("MusicBrainz lookup error = %v, want invalid input", err)
	}
	query := SubtitleQuery{Query: Query{Type: "Movie", Name: "Fixture", ProviderIDs: map[string]string{"Tmdb": id}}}
	if _, err := client.SearchSubtitles(ctx, query); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("subtitle search error = %v, want invalid input", err)
	}
}

func TestProviderAdapterNamesDoNotFoldUnicodeLookalikes(t *testing.T) {
	if got := MetadataID("musicbrainz", "Audio", map[string]string{"MuſicBrainzRecording": "other", "MusicBrainz": "known"}); got != "known" {
		t.Fatalf("lookalike overrode known MusicBrainz ID: %q", got)
	}
	client := openSubtitlesContractClient()
	params, err := client.openSubtitlesSearchParams(SubtitleQuery{Query: Query{Type: "Movie", Name: "Fixture", ProviderIDs: map[string]string{"İmdb": "tt123"}}})
	if err != nil || params.Get("imdb_id") != "" {
		t.Fatalf("custom provider was recognized as IMDb: params=%v, error=%v", params, err)
	}
}
