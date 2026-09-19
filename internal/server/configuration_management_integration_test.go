//go:build linux

package server

import (
	"net/http"
	"reflect"
	"testing"
)

func TestHTTPNamedManagementConfigurationAuthorityPersistenceAndReset(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	for _, section := range []string{"subtitles", "tasks"} {
		expectStatus(t, f.request(t, http.MethodGet, "/System/Configuration/"+section, nil, f.viewer.headers), http.StatusForbidden)
		expectStatus(t, f.request(t, http.MethodPost, "/System/Configuration/"+section, map[string]any{}, f.viewer.headers), http.StatusForbidden)
	}
	body := map[string]any{"DownloadLanguages": []string{"fr", "zh-CN"}, "DownloadMovieSubtitles": false, "DownloadEpisodeSubtitles": true}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/subtitles", body, f.admin.headers), http.StatusNoContent)
	actual := configurationHTTPObject(t, f.request(t, http.MethodGet, "/System/Configuration/subtitles", nil, f.admin.headers))
	if actual["DownloadMovieSubtitles"] != false || !reflect.DeepEqual(actual["DownloadLanguages"], []any{"fr", "zh-CN"}) {
		t.Fatal("named subtitle configuration did not round trip")
	}
	before := f.app.settings.Snapshot()
	for _, invalid := range []map[string]any{{"DownloadLanguages": nil}, {"DownloadLanguages": []string{"en", "en"}}, {"MaxConcurrent": 1}} {
		configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/subtitles", invalid, f.admin.headers), http.StatusBadRequest)
		if !reflect.DeepEqual(f.app.settings.Snapshot(), before) {
			t.Fatal("rejected named write changed published state")
		}
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", map[string]any{"EnableInternetProviders": true}, f.admin.headers), http.StatusNoContent)
	if !f.app.settings.Snapshot().Management.Metadata.EnableInternetProviders {
		t.Fatal("compatibility internet-provider switch has no runtime state")
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/subtitles", map[string]any{}, f.admin.headers), http.StatusNoContent)
	current := f.app.settings.Snapshot().Management
	if !reflect.DeepEqual(current.Subtitles.DownloadLanguages, []string{"en"}) || !current.Metadata.EnableInternetProviders {
		t.Fatal("named reset changed another section")
	}
}
