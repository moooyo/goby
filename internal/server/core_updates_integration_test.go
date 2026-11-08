//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/updates"
)

type coreUpdateFixtureTransport func(*http.Request) (*http.Response, error)

func (transport coreUpdateFixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func coreUpdateFeed(t *testing.T, version string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "product": "goby", "release": map[string]any{
			"version": version, "classification": "Beta", "publishedAt": "2026-11-06T08:00:00Z", "description": "A publisher-selected Goby Docker image.",
			"infoUrl": "https://releases.example.com/goby/details", "sourceUrl": "https://releases.example.com/goby-image.tar",
			"sha256": strings.Repeat("a", 64), "targetFilename": "goby-linux-amd64-image.tar",
		},
	})
	if err != nil {
		t.Fatal("encode Goby feed fixture")
	}
	return data
}

func coreUpdateService(t *testing.T, currentVersion string, handler http.HandlerFunc) *updates.Service {
	t.Helper()
	fixture := httptest.NewTLSServer(handler)
	t.Cleanup(fixture.Close)
	endpoint, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal("parse update TLS fixture")
	}
	transport := coreUpdateFixtureTransport(func(request *http.Request) (*http.Response, error) {
		copy := request.Clone(request.Context())
		address := *request.URL
		address.Scheme, address.Host = endpoint.Scheme, endpoint.Host
		copy.URL, copy.Host = &address, endpoint.Host
		return fixture.Client().Transport.RoundTrip(copy)
	})
	service, err := updates.New(updates.Options{CurrentVersion: currentVersion, FeedURL: "https://updates.example.com/release.json", Transport: transport})
	if err != nil {
		t.Fatal("create explicitly injected update fixture transport")
	}
	t.Cleanup(service.Close)
	return service
}

func TestCoreUpdateHTTPRequiresAdministratorCSRFAndHonestSourceState(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := "/admin/v1/server-updates"
	expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, path+"/check", map[string]any{}, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	headers := http.Header{"X-CSRF-Token": {csrf}}
	expectAPIError(t, f.request(t, http.MethodPost, path+"/check", map[string]any{}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://untrusted.invalid"}}, cookie), http.StatusForbidden, "origin_denied", false)
	expectAPIError(t, f.request(t, http.MethodGet, path+"?Source=https://untrusted.invalid", nil, nil, cookie), http.StatusBadRequest, "invalid_input", false)
	expectAPIError(t, f.request(t, http.MethodPost, path+"/check", map[string]any{"Source": "https://untrusted.invalid"}, headers, cookie), http.StatusBadRequest, "invalid_input", false)
	response := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	initial := jsonObject(t, response)
	if initial["State"] != "unconfigured" || initial["ConfiguredSource"] != false || initial["CheckedAt"] != nil || initial["Release"] != nil || initial["CanSelfUpdate"] != false || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unconfigured deployment fabricated an update check")
	}
	response = f.request(t, http.MethodPost, path+"/check", map[string]any{}, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	if jsonObject(t, response)["State"] != "unconfigured" {
		t.Fatal("explicit unconfigured check claimed a completed comparison")
	}
	login := f.embyLogin(t, "Administrator", "administrator-password")
	embyHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Packages/Updates?PackageType=System", nil, embyHeaders), http.StatusServiceUnavailable, "update_source_unconfigured", true)
	expectAPIError(t, f.request(t, http.MethodGet, "/Packages/Updates?PackageType=UserInstalled", nil, embyHeaders), http.StatusNotImplemented, "plugin_updates_unsupported", true)
	for _, query := range []string{"", "?PackageType=System&PackageType=System", "?PackageType=System&Source=x", "?PackageType=Unknown"} {
		expectAPIError(t, f.request(t, http.MethodGet, "/emby/Packages/Updates"+query, nil, embyHeaders), http.StatusBadRequest, "invalid_input", true)
	}
	if _, err := f.users.CreateUser(f.ctx, "Update viewer", "update-viewer-password", false); err != nil {
		t.Fatal("create update viewer")
	}
	viewer := f.embyLogin(t, "Update viewer", "update-viewer-password")
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Packages/Updates?PackageType=System", nil, http.Header{"X-Emby-Token": {stringValue(t, viewer, "AccessToken")}}), http.StatusForbidden, "administrator_required", true)
}

func TestCoreUpdateHTTPMapsRealProductResultsAndClearsFailedAssertions(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	embyHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	var calls atomic.Int32
	var failed atomic.Bool
	feed := coreUpdateFeed(t, "0.2.0-beta.1")
	handler := func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/release.json" || r.URL.RawQuery != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-Emby-Token") != "" {
			t.Error("release observation forwarded credentials or fetched an artifact")
		}
		w.Header().Set("Content-Type", "application/json")
		if failed.Load() {
			w.WriteHeader(http.StatusBadGateway)
		}
		_, _ = w.Write(feed)
	}
	f.app.version = "0.1.0"
	f.app.updates = coreUpdateService(t, "0.1.0", handler)
	response := f.request(t, http.MethodGet, "/admin/v1/server-updates", nil, nil, cookie)
	if jsonObject(t, response)["State"] != "not_checked" || calls.Load() != 0 {
		t.Fatal("native status read performed an implicit release request")
	}
	response = f.request(t, http.MethodGet, "/pAcKaGeS/uPdAtEs?PackageType=System", nil, embyHeaders)
	expectStatus(t, response, http.StatusOK)
	var packages []map[string]any
	if json.Unmarshal(response.Body.Bytes(), &packages) != nil || len(packages) != 1 || packages[0]["versionStr"] != "0.2.0-beta.1" || packages[0]["classification"] != "Beta" || packages[0]["name"] != "Goby Server" || packages[0]["checksum"] != strings.Repeat("a", 64) {
		t.Fatal("confirmed Goby release did not match PackageVersionInfo")
	}
	info := jsonObject(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, embyHeaders))
	if info["HasUpdateAvailable"] != true || info["CanSelfUpdate"] != false || info["Version"] != embyAPIVersion || info["GobyVersion"] != "0.1.0" {
		t.Fatal("update comparison confused product and compatibility versions")
	}
	failed.Store(true)
	response = f.request(t, http.MethodPost, "/admin/v1/server-updates/check", map[string]any{}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusOK)
	view := jsonObject(t, response)
	if view["State"] != "failed" || view["CheckedAt"] == nil || view["Release"] != nil || view["ErrorCode"] != "update_check_failed" {
		t.Fatal("failed check was not explicitly represented")
	}
	info = jsonObject(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, embyHeaders))
	if _, known := info["HasUpdateAvailable"]; known {
		t.Fatal("failed check retained an old successful update assertion")
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Packages/Updates?PackageType=System", nil, embyHeaders), http.StatusServiceUnavailable, "update_check_failed", true)
	failed.Store(false)
	f.app.updates = coreUpdateService(t, "0.2.0-beta.1", handler)
	response = f.request(t, http.MethodGet, "/emby/Packages/Updates?PackageType=System", nil, embyHeaders)
	expectStatus(t, response, http.StatusOK)
	if strings.TrimSpace(response.Body.String()) != "[]" {
		t.Fatal("successful equal-version comparison did not return an empty update list")
	}
	info = jsonObject(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, embyHeaders))
	if info["HasUpdateAvailable"] != false {
		t.Fatal("confirmed current product version omitted its successful result")
	}
	f.app.updates = coreUpdateService(t, "(devel)", handler)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Packages/Updates?PackageType=System", nil, embyHeaders), http.StatusConflict, "current_version_unknown", true)
	if f.app.updates.Status().State != updates.VersionUnknown {
		t.Fatal("incomparable build was treated as a release comparison")
	}
}

func TestCoreUpdateHTTPDiscardsResponseAfterCredentialRevocation(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	actor, err := f.users.ResolveWithPeer(f.ctx, cookie.Value, "admin", "192.0.2.1")
	if err != nil {
		t.Fatal("resolve fixture administrator")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	feed := coreUpdateFeed(t, "0.2.0")
	f.app.updates = coreUpdateService(t, "0.1.0", func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(feed)
	})
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- f.request(t, http.MethodPost, "/admin/v1/server-updates/check", map[string]any{}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	}()
	select {
	case <-entered:
	case <-f.ctx.Done():
		t.Fatal("update request did not reach the TLS fixture")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
		t.Fatal("revoke checking administrator")
	}
	unblock.Do(func() { close(release) })
	select {
	case response := <-done:
		expectAPIError(t, response, http.StatusUnauthorized, "invalid_credentials", false)
	case <-f.ctx.Done():
		t.Fatal("revoked update request did not complete")
	}
	if view := f.app.updates.Status(); view.State != updates.NotChecked || view.CheckedAt != nil || view.Release != nil {
		t.Fatal("revoked administrator published an observed release")
	}
}
