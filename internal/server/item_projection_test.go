package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestItemProjectionNormalizesOnlyDeclaredOptions(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/emby/Items?fields=Overview,Path&fields=Genres&excludefields=Path&enableimages=false&enableuserdata=true&imagetypelimit=3&enableimagetypes=Primary&UserId=CaseSensitiveID&api_key=CaseSensitiveToken", nil)
	originalURL := r.URL
	original := originalURL.RawQuery
	if !normalizeItemProjectionQuery(httptest.NewRecorder(), r) {
		t.Fatal("the ordinary client's projection casing was rejected")
	}
	want := url.Values{
		"Fields": {"Overview,Path,Genres"}, "ExcludeFields": {"Path"},
		"EnableImages": {"false"}, "EnableUserData": {"true"}, "ImageTypeLimit": {"3"}, "EnableImageTypes": {"Primary"},
		"UserId": {"CaseSensitiveID"}, "api_key": {"CaseSensitiveToken"},
	}
	if !reflect.DeepEqual(r.URL.Query(), want) || originalURL.RawQuery != original {
		t.Fatal("normalization changed unrelated values or the shared URL")
	}
	if !normalizeItemProjectionQuery(httptest.NewRecorder(), r) || !reflect.DeepEqual(r.URL.Query(), want) {
		t.Fatal("projection normalization is not idempotent")
	}
}

func TestItemProjectionRejectsAmbiguousOrUnboundedInputs(t *testing.T) {
	for _, query := range []string{
		"Fields=Overview&fields=Overview", "ExcludeFields=Path&excludefields=Path",
		"EnableImages=true&enableimages=true", "EnableUserData=true&EnableUserData=false",
		"EnableImages=", "ImageTypeLimit=-1", "ImageTypeLimit=2147483648",
		"Fields=%00", "Fields=%ff", "Fields=Path%0a", "Fields=Source.Path", "Fields=%2a",
		"X-Emby-Language=%00", "X-Emby-Language=en-US&x-emby-language=en-US",
		"Fields=" + strings.Repeat("x", 65), "Fields=" + strings.Repeat("Overview,", maxItemProjectionFields+1),
		"Fields=" + strings.Repeat(" ", maxItemProjectionBytes+1),
	} {
		r := httptest.NewRequest(http.MethodGet, "/emby/Items?"+strings.ReplaceAll(query, " ", "%20"), nil)
		w := httptest.NewRecorder()
		if normalizeItemProjectionQuery(w, r) || w.Code != http.StatusBadRequest {
			t.Fatalf("invalid projection was accepted: %q", query)
		}
	}
}

func TestItemProjectionExclusionWinsAndCoversNestedSourceFacts(t *testing.T) {
	source := map[string]any{"Id": "source-id", "Path": "/private/movie.mkv", "MediaStreams": []map[string]any{{"DeliveryUrl": "/subtitles?api_key=credential"}}, "Chapters": []map[string]any{{"StartPositionTicks": 0}}}
	item := map[string]any{
		"Id": "item-id", "Name": "Movie", "Type": "Movie", "ServerId": "server-id", "IsFolder": false,
		"Path": "/private/movie.mkv", "MediaStreams": source["MediaStreams"], "Chapters": source["Chapters"],
		"MediaSources": []map[string]any{source}, "Overview": "retained", "PrimaryImageAspectRatio": 1.5,
	}
	r := httptest.NewRequest(http.MethodGet, "/emby/Items/item-id?Fields=MediaStreams,Chapters,Path&ExcludeFields=mediastreams,Chapters,Path,PrimaryImageAspectRatio,Id,Name,UnsupportedField", nil)
	applyItemFieldExclusions(item, r)
	for _, name := range []string{"MediaStreams", "Chapters", "Path"} {
		if _, present := item[name]; present {
			t.Fatalf("excluded top-level %s remains", name)
		}
		if _, present := source[name]; present {
			t.Fatalf("excluded nested %s remains", name)
		}
	}
	if _, present := item["PrimaryImageAspectRatio"]; present || item["Overview"] != "retained" || item["Id"] != "item-id" || item["Name"] != "Movie" || source["Id"] != "source-id" {
		t.Fatal("exclusion did not preserve identity and unrelated fields")
	}
	r.URL.RawQuery = "ExcludeFields=MediaSources"
	applyItemFieldExclusions(item, r)
	if _, present := item["MediaSources"]; present {
		t.Fatal("the excluded media source collection remains")
	}
}
