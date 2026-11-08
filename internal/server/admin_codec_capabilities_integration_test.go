package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestHTTPCodecInventoryRequiresNativeAuthorityAndExplicitCSRFRefresh(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	runtime := hardwareEncodingTestRuntime(t)
	probes := 0
	runtime.capabilityProbe = func(context.Context, string) (media.Capabilities, error) {
		probes++
		return media.Capabilities{Version: "fixture-version", HardwareVerified: true, HardwareAccelerators: []string{"vaapi"},
			Decoders: []string{"h264"}, Encoders: []string{"libx264", "h264_vaapi"}}, nil
	}
	f.app.hardwareEncoding = runtime
	path := "/admin/v1/hardware/codecs"
	denied := f.request(t, http.MethodGet, path, nil, nil)
	expectStatus(t, denied, http.StatusUnauthorized)
	if denied.Header().Get("Cache-Control") != "no-store" || denied.Header().Get("Pragma") != "no-cache" {
		t.Fatal("unauthorized codec observations were cacheable")
	}
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {stringValue(t, emby, "AccessToken")}}), http.StatusUnauthorized)
	read := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, read, http.StatusOK)
	if jsonObject(t, read)["CompiledState"] != "unknown" || probes != 0 {
		t.Fatal("GET codec inventory started a process instead of returning unknown interfaces")
	}
	expectStatus(t, f.request(t, http.MethodPost, path+"/refresh", map[string]any{}, nil, cookie), http.StatusForbidden)
	headers := http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}
	expectStatus(t, f.request(t, http.MethodPost, path+"/refresh", map[string]any{"FFmpegPath": "untrusted"}, headers, cookie), http.StatusBadRequest)
	if probes != 0 {
		t.Fatal("rejected refresh reached the encoding tool")
	}
	refreshed := f.request(t, http.MethodPost, path+"/refresh", map[string]any{}, headers, cookie)
	expectStatus(t, refreshed, http.StatusOK)
	body := jsonObject(t, refreshed)
	if body["CompiledState"] != "current" || body["FFmpegVersion"] != "fixture-version" || probes != 1 {
		t.Fatalf("explicit capability refresh was not retained: %#v probes=%d", body, probes)
	}
	for _, marker := range []string{cookie.Value, "untrusted", f.cfg.FFmpegPath + "/", "HardwareVerified"} {
		if marker != "" && strings.Contains(refreshed.Body.String(), marker) {
			t.Fatalf("codec observation leaked a private field or promoted compiled evidence: %q", marker)
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, path+"?probe=true", nil, nil, cookie), http.StatusBadRequest)
}
