package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestThemeQueryPreservesIndependentObservedDefaults(t *testing.T) {
	for _, entry := range []struct {
		query                  string
		inherit, songs, videos bool
	}{
		{"", false, true, true},
		{"InheritFromParent=true", true, true, true},
		{"EnableThemeSongs=false", false, false, true},
		{"EnableThemeVideos=false", false, true, false},
		{"InheritFromParent=true&EnableThemeSongs=false&EnableThemeVideos=false", true, false, false},
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/Items/seed/ThemeMedia?"+entry.query, nil)
		actual, ok := readThemeQuery(httptest.NewRecorder(), request)
		if !ok || actual.InheritFromParent != entry.inherit || actual.EnableThemeSongs != entry.songs || actual.EnableThemeVideos != entry.videos {
			t.Fatal("theme controls replaced an independent default or explicit disable")
		}
		if actual.Subject != (library.Subject{}) {
			t.Fatal("query parameters supplied theme authority before authentication")
		}
	}
}

func TestThemeQueryRejectsMalformedAndRepeatedControls(t *testing.T) {
	for _, name := range []string{"InheritFromParent", "EnableThemeSongs", "EnableThemeVideos"} {
		for _, raw := range [][]string{{""}, {"perhaps"}, {"true", "false"}, {"false", "false"}} {
			request := httptest.NewRequest(http.MethodGet, "/emby/Items/seed/ThemeMedia?"+url.Values{name: raw}.Encode(), nil)
			response := httptest.NewRecorder()
			if _, ok := readThemeQuery(response, request); ok || response.Code != http.StatusBadRequest {
				t.Fatal("an ambiguous theme control was silently accepted")
			}
		}
	}
}

func TestThemeNamespaceAndExtraTypesPreserveResourceIdentity(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/iTeMs/MixedCase-ID/tHeMeMeDiA?UserId=viewer", nil)
	canonical := compatibilityNamespace(request)
	if canonical.URL.Path != "/emby/Items/MixedCase-ID/ThemeMedia" || canonical.URL.RawQuery != request.URL.RawQuery || request.URL.Path != "/iTeMs/MixedCase-ID/tHeMeMeDiA" {
		t.Fatal("theme route canonicalization changed an opaque resource or query")
	}
	server := &Server{}
	for _, entry := range []struct{ kind, itemType, extra string }{
		{"song", "Audio", "ThemeSong"}, {"video", "Video", "ThemeVideo"},
		{"", "Audio", ""}, {"video", "Movie", ""}, {"song", "Video", ""},
	} {
		dto := server.itemDTO(library.Item{ID: "resource", ParentID: "owner", Type: entry.itemType, ThemeKind: entry.kind}, nil, false)
		extra, present := dto["ExtraType"]
		if entry.extra == "" && present || entry.extra != "" && extra != entry.extra || dto["ParentId"] != "owner" || dto["Id"] != "resource" {
			t.Fatal("theme projection fabricated a type or replaced the resource/owner identity")
		}
	}
}
