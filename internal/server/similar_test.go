package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestSimilarQueryPreservesZeroLimitAndDistinctExclusions(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/emby/Items/seed/Similar?Limit=0&StartIndex=3&ExcludeArtistIds=12,13&ExcludeItemIds=other&EnableTotalRecordCount=false&ArtistType=AlbumArtist", nil)
	response := httptest.NewRecorder()
	query, ok := readSimilarQuery(response, request, "viewer")
	if !ok || query.UserID != "viewer" || query.Limit != 0 || query.StartIndex != 3 || !query.Recursive || query.ExplicitSort ||
		!reflect.DeepEqual(query.ExcludeArtistIds, []int64{12, 13}) || !reflect.DeepEqual(query.ExcludeItemIds, []string{"other"}) {
		t.Fatal("similarity controls lost their independent identities, zero limit, or recursive default")
	}
	request = httptest.NewRequest(http.MethodGet, "/emby/Items/seed/Similar?SortBy=SortName&SortOrder=Descending&Recursive=false", nil)
	query, ok = readSimilarQuery(httptest.NewRecorder(), request, "viewer")
	if !ok || !query.ExplicitSort || query.SortBy != "SortName" || query.SortOrder != "Descending" || query.Recursive {
		t.Fatal("explicit sorting or parent traversal was replaced by a similarity default")
	}
}

func TestSimilarQueryRejectsAmbiguousAndMalformedControls(t *testing.T) {
	for _, values := range []url.Values{
		{"ExcludeArtistIds": {"0"}}, {"ExcludeArtistIds": {"-1"}}, {"ExcludeArtistIds": {"12,,13"}},
		{"ExcludeArtistIds": {"9223372036854775808"}}, {"ExcludeArtistIds": {strings.Repeat("12,", 1024) + "12"}},
		{"EnableTotalRecordCount": {"invalid"}}, {"EnableTotalRecordCount": {"true", "false"}},
		{"ArtistType": {"Person"}}, {"ArtistType": {"Artist", "AlbumArtist"}},
		{"Limit": {"-1"}}, {"StartIndex": {"-1"}},
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/Items/seed/Similar?"+values.Encode(), nil)
		response := httptest.NewRecorder()
		if _, ok := readSimilarQuery(response, request, "viewer"); ok || response.Code != http.StatusBadRequest {
			t.Fatal("malformed similarity controls were silently broadened into another query")
		}
	}
}

func TestSimilarNamespaceKeepsOpaqueSeedSpelling(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/iTeMs/MixedCase-ID/sImIlAr?UserId=viewer", nil)
	canonical := compatibilityNamespace(request)
	if canonical.URL.Path != "/emby/Items/MixedCase-ID/Similar" || canonical.URL.RawQuery != request.URL.RawQuery ||
		request.URL.Path != "/iTeMs/MixedCase-ID/sImIlAr" {
		t.Fatal("similar route canonicalization changed the opaque seed, query, or original request")
	}
}
