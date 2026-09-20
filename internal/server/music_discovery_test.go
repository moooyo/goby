package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

func TestMusicDiscoveryQueryRetainsTypedEntitySeedsAndExplicitZero(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/emby/Artists/InstantMix?Id=42&Limit=0&StartIndex=3&ArtistType=Artist,AlbumArtist&ExcludeArtistIds=51&IsPlayed=true", nil)
	seed, query, ok := readInstantMixQuery(httptest.NewRecorder(), request, "viewer", "Artist")
	if !ok || seed.Kind != "Artist" || seed.ID != "42" || seed.Name != "" || query.Limit != 0 || query.StartIndex != 3 || !query.Recursive || query.ExplicitSort || query.IsPlayed == nil || !*query.IsPlayed || !reflect.DeepEqual(query.ExcludeArtistIds, []int64{51}) {
		t.Fatalf("typed mix controls changed: %#v %#v", seed, query)
	}
	request = httptest.NewRequest(http.MethodGet, "/emby/MusicGenres/Rock%20%2F%20Pop/InstantMix?SortBy=Name&SortOrder=Descending", nil)
	request.SetPathValue("Name", "Rock / Pop")
	seed, query, ok = readInstantMixQuery(httptest.NewRecorder(), request, "viewer", "GenreName")
	if !ok || seed.Name != "Rock / Pop" || seed.ID != "" || !query.ExplicitSort || query.SortOrder != "Descending" {
		t.Fatal("named genre or explicit music ordering changed")
	}
}

func TestMusicDiscoveryQueryRejectsMissingAmbiguousAndWrongEntitySeeds(t *testing.T) {
	for _, values := range []url.Values{
		{}, {"Id": {""}}, {"Id": {"1", "2"}}, {"Id": {"0"}}, {"Id": {"opaque"}},
		{"Id": {"9223372036854775808"}}, {"Id": {"1"}, "ArtistType": {"Composer"}},
		{"Id": {"1"}, "ArtistType": {"Artist", "AlbumArtist"}}, {"Id": {"1"}, "ExcludeArtistIds": {"-2"}},
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/MusicGenres/InstantMix?"+values.Encode(), nil)
		response := httptest.NewRecorder()
		if _, _, ok := readInstantMixQuery(response, request, "viewer", "Genre"); ok || response.Code != http.StatusBadRequest {
			t.Fatalf("invalid mix seed controls accepted: %#v", values)
		}
	}
}

func TestMusicArtistRoleSelectionAcceptsTheRecordedCombinedConsumer(t *testing.T) {
	for _, test := range []struct{ value, family string }{
		{"Artist", "artists"}, {"AlbumArtist", "albumartists"}, {"Artist,AlbumArtist", "allartists"}, {"albumartist, artist", "allartists"},
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/Artists/Prefixes?ArtistType="+url.QueryEscape(test.value), nil)
		if family, ok := musicEntityFamily(request, "artists"); !ok || family != test.family {
			t.Fatalf("artist role %q = %q, %v", test.value, family, ok)
		}
	}
	for _, value := range []string{"", "Artist,", "Artist,Artist", "Artist,AlbumArtist,Artist", "Composer"} {
		if _, _, ok := musicArtistRoles(value); ok {
			t.Fatalf("ambiguous artist role accepted: %q", value)
		}
	}
}
