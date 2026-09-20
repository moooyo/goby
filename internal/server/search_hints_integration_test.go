//go:build linux

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func searchHintsResponse(t *testing.T, response *httptest.ResponseRecorder) ([]map[string]any, int) {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	object := jsonObject(t, response)
	if _, exists := object["Items"]; exists {
		t.Fatal("search result was incorrectly wrapped as an item query")
	}
	values, ok := object["SearchHints"].([]any)
	if !ok {
		t.Fatalf("search result has no array: %#v", object)
	}
	total, ok := object["TotalRecordCount"].(float64)
	if !ok {
		t.Fatalf("search result has no count: %#v", object)
	}
	hints := make([]map[string]any, len(values))
	for index, value := range values {
		hint, ok := value.(map[string]any)
		if !ok {
			t.Fatal("search result has a non-object hint")
		}
		hints[index] = hint
	}
	return hints, int(total)
}

func TestHTTPSearchHintsTypedNavigationImagesAndCurrentAuthority(t *testing.T) {
	a := newApplicationKeyCatalogFixture(t)
	f := a.f
	search := "/sEaRcH/hInTs?SearchTerm=Private&IncludeMedia=false"
	hints, total := searchHintsResponse(t, f.request(t, http.MethodGet, search, nil, a.headers))
	if total != 3 || len(hints) != 3 {
		t.Fatalf("search must combine genre, studio and person: count=%d, hints=%#v", total, hints)
	}
	var genre map[string]any
	for _, hint := range hints {
		reference := objectValue(t, hint, "GobyReference")
		if reference["Kind"] != "Entity" || reference["Id"] != hint["Id"] || hint["ItemId"] != hint["Id"] {
			t.Fatalf("search changed entity identities: %#v", hint)
		}
		navigation := stringValue(t, hint, "GobyNavigationUrl")
		if !strings.HasPrefix(navigation, "/emby/Search/Entities/") || strings.Contains(navigation, a.key.Token) {
			t.Fatalf("search emitted an ambiguous or credential-bearing URL: %s", navigation)
		}
		detail := f.request(t, http.MethodGet, navigation, nil, a.headers)
		expectStatus(t, detail, http.StatusOK)
		if jsonObject(t, detail)["Id"] != hint["Id"] || jsonObject(t, detail)["Name"] != hint["Name"] {
			t.Fatal("typed entity navigation reached another owner")
		}
		expectStatus(t, f.request(t, http.MethodGet, navigation, nil, a.viewerHeaders), http.StatusNotFound)
		if hint["Type"] == "Genre" {
			genre = hint
		}
	}
	if genre == nil {
		t.Fatal("genre hint was omitted")
	}
	image := stringValue(t, genre, "PrimaryImageUrl")
	expectStatus(t, f.request(t, http.MethodGet, image, nil, a.headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, image, nil, a.viewerHeaders), http.StatusNotFound)
	page, count := searchHintsResponse(t, f.request(t, http.MethodGet, search+"&StartIndex=1&Limit=1", nil, a.headers))
	if count != total || len(page) != 1 || page[0]["Id"] != hints[1]["Id"] {
		t.Fatal("search adapter changed unified count or stable page order")
	}
	empty, count := searchHintsResponse(t, f.request(t, http.MethodGet, search+"&Limit=0", nil, a.headers))
	if count != total || len(empty) != 0 {
		t.Fatal("search count-only page changed its population")
	}
	hidden, count := searchHintsResponse(t, f.request(t, http.MethodGet, search, nil, a.viewerHeaders, a.adminCookie))
	if count != 0 || len(hidden) != 0 {
		t.Fatal("viewer search borrowed an administrator cookie or exposed restricted entities")
	}
	hidden, count = searchHintsResponse(t, f.request(t, http.MethodGet, search+"&UserId="+url.QueryEscape(a.viewerID), nil, a.headers))
	if count != 0 || len(hidden) != 0 {
		t.Fatal("application search ignored its explicit target library scope")
	}
	movies, count := searchHintsResponse(t, f.request(t, http.MethodGet, "/emby/Search/Hints?SearchTerm=Hidden%20Movie", nil, a.headers))
	if count != 1 || len(movies) != 1 || movies[0]["ItemId"] != a.movieID ||
		objectValue(t, movies[0], "GobyReference")["Kind"] != "Item" ||
		movies[0]["PrimaryImageTag"] != imageSourceTag(a.poster) {
		t.Fatalf("opaque physical search projection changed: %#v", movies)
	}
	navigation := stringValue(t, movies[0], "GobyNavigationUrl")
	image = stringValue(t, movies[0], "PrimaryImageUrl")
	expectStatus(t, f.request(t, http.MethodGet, navigation, nil, a.headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, image, nil, a.headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, navigation, nil, a.viewerHeaders), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, image, nil, a.viewerHeaders), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", a.key.CredentialID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{search, search + "&Limit=0", "/emby/Search/Hints?SearchTerm=", navigation, image} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, a.headers), http.StatusUnauthorized)
	}
}
