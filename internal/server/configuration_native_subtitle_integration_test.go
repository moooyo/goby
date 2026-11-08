//go:build linux

package server

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestHTTPNativeSubtitleASSOnlyWritePreservesDownloadPolicyAndOlderWriters(t *testing.T) {
	fixture := newConfigurationHTTPFixture(t)
	downloads := map[string]any{"DownloadLanguages": []string{"fr", "zh-CN"}, "DownloadMovieSubtitles": false, "DownloadEpisodeSubtitles": true}
	configurationHTTPStatus(t, fixture.request(t, http.MethodPost, "/System/Configuration/subtitles", downloads, fixture.admin.headers), http.StatusNoContent)
	before := fixture.app.settings.Snapshot()
	configurationHTTPStatus(t, fixture.request(t, http.MethodPost, "/System/Configuration/subtitles", map[string]any{"AllowAssToText": false}, fixture.admin.headers), http.StatusNoContent)
	selected := fixture.app.settings.Snapshot()
	if selected.Revision != before.Revision+1 || selected.Runtime.SubtitleOptions.AllowAssToText || selected.Runtime.Overrides.SubtitleOptions == nil ||
		!reflect.DeepEqual(selected.Management.Subtitles, before.Management.Subtitles) {
		t.Fatal("an ASS-only update lost its override or replaced independent subtitle downloads")
	}
	object := configurationHTTPObject(t, fixture.request(t, http.MethodGet, "/System/Configuration/subtitles", nil, fixture.admin.headers))
	if object["AllowAssToText"] != false || !reflect.DeepEqual(object["DownloadLanguages"], []any{"fr", "zh-CN"}) {
		t.Fatal("the fixed subtitle readback lost a saved conversion choice or download languages")
	}
	configurationHTTPStatus(t, fixture.request(t, http.MethodPost, "/System/Configuration/subtitles", map[string]any{"AllowAssToText": true}, fixture.viewer.headers), http.StatusForbidden)
	for _, body := range []map[string]any{{"AllowAssToText": nil}, {"AllowAssToText": "false"}, {"AllowAssToText": false, "Style": "private-marker"}} {
		configurationHTTPStatus(t, fixture.request(t, http.MethodPost, "/System/Configuration/subtitles", body, fixture.admin.headers), http.StatusBadRequest)
	}
	if !reflect.DeepEqual(fixture.app.settings.Snapshot(), selected) {
		t.Fatal("rejected subtitle configuration changed the published output policy")
	}
	configurationHTTPStatus(t, fixture.request(t, http.MethodPost, "/System/Configuration/subtitles", map[string]any{}, fixture.admin.headers), http.StatusNoContent)
	resetDownloads := fixture.app.settings.Snapshot()
	if !reflect.DeepEqual(resetDownloads.Management.Subtitles, settings.DefaultManagement().Subtitles) || resetDownloads.Runtime.SubtitleOptions.AllowAssToText ||
		!reflect.DeepEqual(resetDownloads.Runtime.Overrides.SubtitleOptions, selected.Runtime.Overrides.SubtitleOptions) {
		t.Fatal("an older named subtitle reset cleared a new output-policy override")
	}
	configurationHTTPStatus(t, fixture.request(t, http.MethodPost, "/System/Configuration/subtitles", map[string]any{"AllowAssToText": true}, fixture.admin.headers), http.StatusNoContent)
	if !fixture.app.settings.Snapshot().Runtime.SubtitleOptions.AllowAssToText {
		t.Fatal("the fixed native ASS control failed to restore text negotiation")
	}
}
