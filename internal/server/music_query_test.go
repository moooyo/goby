package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestMusicQueryDecodesOnlyDeclaredMusicFilters(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/emby/Items?ArtistIds=12,13&AlbumArtistIds=12&AlbumIds=album-one,album-two&ExcludeItemIds=track-one", nil)
	response := httptest.NewRecorder()
	var query library.Query
	if !readMusicFilters(response, request, &query) {
		t.Fatal("declared music filters were rejected")
	}
	if !reflect.DeepEqual(query.ArtistIds, []int64{12, 13}) || !reflect.DeepEqual(query.AlbumArtistIds, []int64{12}) ||
		!reflect.DeepEqual(query.AlbumIds, []string{"album-one", "album-two"}) || !reflect.DeepEqual(query.ExcludeItemIds, []string{"track-one"}) || len(query.Ids) != 0 {
		t.Fatal("music fields were confused with another catalog identifier purpose")
	}
}

func TestMusicQueryRejectsMalformedAndOversizedIdentifierLists(t *testing.T) {
	for _, values := range []url.Values{
		{"ArtistIds": {"0"}}, {"AlbumArtistIds": {"-1"}}, {"ArtistIds": {"12,,13"}},
		{"AlbumArtistIds": {"12,not-an-id"}}, {"AlbumIds": {"album,"}},
		{"ExcludeItemIds": {"line\nbreak"}}, {"AlbumIds": {strings.Repeat("x", 257)}},
		{"AlbumIds": {strings.Repeat("album,", 1024) + "album"}},
		{"ArtistIds": {strings.Repeat("12,", 1024) + "12"}},
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/Items?"+values.Encode(), nil)
		response := httptest.NewRecorder()
		var query library.Query
		if readMusicFilters(response, request, &query) || response.Code != http.StatusBadRequest {
			t.Fatal("invalid music filter was silently dropped or broadened into an unfiltered query")
		}
	}
}
