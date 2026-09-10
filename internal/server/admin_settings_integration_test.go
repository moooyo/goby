//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
)

func adminSettingsHTTPFixture(t *testing.T) (*serverFixture, *http.Cookie, string, string) {
	t.Helper()
	f := newServerFixture(t)
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	if f.app.settings == nil {
		t.Fatal("server startup did not initialize its settings store")
	}
	return f, cookie, csrf, userID
}

func adminSettingsHTTPObject(t *testing.T, response *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if response.Code != status {
		t.Fatalf("settings response status = %d, want %d", response.Code, status)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("settings responses must not be cached")
	}
	var value map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil || value == nil {
		t.Fatal("settings response did not contain a JSON object")
	}
	return value
}

func adminSettingsHTTPRow(t *testing.T, f *serverFixture) string {
	t.Helper()
	var value string
	if err := f.pool.QueryRow(f.ctx, "SELECT to_jsonb(s)::text FROM managed_settings s WHERE id = 1").Scan(&value); err != nil {
		t.Fatal("read the owned settings singleton")
	}
	return value
}

func adminSettingsHTTPUpdate(revision string, name any, bitrate any) map[string]any {
	return map[string]any{"Revision": revision, "Overrides": map[string]any{
		"ServerName": name, "MaxBitrate": bitrate, "MaxWidth": nil, "MaxHeight": nil, "MaxAudioChannels": nil,
	}}
}

func adminSettingsHTTPWrite(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, method, path, body, http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie)
}

func adminSettingsHTTPAssertSnapshot(t *testing.T, value map[string]any, revision string) {
	t.Helper()
	fields := []string{"Revision", "Defaults", "Overrides", "Effective", "Sources", "UpdatedAt", "Deployment", "ServerNameMode", "Encoding"}
	if len(value) != len(fields) || value["Revision"] != revision {
		t.Fatal("settings response changed its exact top-level contract or revision")
	}
	for _, field := range fields {
		if _, present := value[field]; !present {
			t.Fatalf("settings response omitted %s", field)
		}
	}
	for _, section := range []string{"Defaults", "Overrides", "Effective", "Sources"} {
		object, ok := value[section].(map[string]any)
		if !ok || len(object) != 5 {
			t.Fatalf("settings section %s did not contain exactly five values", section)
		}
		for _, field := range []string{"ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"} {
			if _, present := object[field]; !present {
				t.Fatalf("settings section %s omitted %s", section, field)
			}
		}
	}
	switch value["ServerNameMode"] {
	case "deployment", "custom", "empty", "unset":
	default:
		t.Fatal("settings response did not identify its exact server-name mode")
	}
	encoding := objectValue(t, value, "Encoding")
	width, ok := encoding["TranscodingMaxWidth"].(float64)
	if len(encoding) != 1 || !ok || width < 0 || width > 8192 || width != float64(int(width)) {
		t.Fatal("settings encoding projection did not contain its exact bounded integer width")
	}
	deployment := objectValue(t, value, "Deployment")
	if host, ok := deployment["HostName"].(string); !ok || host == "" || len(deployment) != 8 {
		t.Fatal("settings deployment omitted its read-only startup hostname")
	}
	stamp, ok := value["UpdatedAt"].(string)
	if _, err := time.Parse(time.RFC3339Nano, stamp); !ok || err != nil || !strings.HasSuffix(stamp, "Z") {
		t.Fatal("settings update time is not explicit UTC")
	}
}

func TestHTTPAdminSettingsDefaultsExplicitOverridesNoopAndSelectiveReset(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	initial := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, initial, "1")
	defaults := objectValue(t, initial, "Defaults")
	if initial["ServerNameMode"] != "deployment" || objectValue(t, initial, "Encoding")["TranscodingMaxWidth"] != float64(0) ||
		!reflect.DeepEqual(defaults, initial["Effective"]) {
		t.Fatal("initial effective settings did not use deployment defaults")
	}
	for field, value := range objectValue(t, initial, "Overrides") {
		if value != nil || objectValue(t, initial, "Sources")[field] != "deployment" {
			t.Fatal("initial settings synthesized a persisted override")
		}
	}
	explicit := map[string]any{"Revision": "1", "Overrides": defaults}
	saved := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", explicit), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, saved, "2")
	if saved["ServerNameMode"] != "custom" || !reflect.DeepEqual(saved["Defaults"], defaults) || !reflect.DeepEqual(saved["Overrides"], defaults) || !reflect.DeepEqual(saved["Effective"], defaults) {
		t.Fatal("an explicit deployment-default value lost its persisted override identity")
	}
	for _, source := range objectValue(t, saved, "Sources") {
		if source != "database" {
			t.Fatal("explicit default-valued overrides did not report database provenance")
		}
	}
	explicit["Revision"] = "2"
	beforeNoop := adminSettingsHTTPRow(t, f)
	noop := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", explicit), http.StatusOK)
	if !reflect.DeepEqual(saved, noop) || adminSettingsHTTPRow(t, f) != beforeNoop {
		t.Fatal("an identical complete override update changed its revision, timestamp, or persisted row")
	}
	changed := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate("2", "Updated native server", 12_345_678)), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, changed, "3")
	if objectValue(t, changed, "Effective")["ServerName"] != "Updated native server" || objectValue(t, changed, "Effective")["MaxBitrate"] != float64(12_345_678) ||
		objectValue(t, changed, "Overrides")["MaxWidth"] != nil || objectValue(t, changed, "Sources")["MaxWidth"] != "deployment" ||
		f.app.settings.Snapshot().Revision != 3 || f.app.settings.Snapshot().Effective.ServerName != "Updated native server" {
		t.Fatal("PUT returned a stale request-entry snapshot instead of its committed effective settings")
	}
	if got := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK); !reflect.DeepEqual(got, changed) {
		t.Fatal("the next GET did not observe the committed PUT snapshot")
	}
	reset := map[string]any{"Revision": "3", "Fields": []string{"ServerName"}}
	partial := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, partial, "4")
	if partial["ServerNameMode"] != "deployment" || objectValue(t, partial, "Effective")["ServerName"] != defaults["ServerName"] || objectValue(t, partial, "Sources")["ServerName"] != "deployment" ||
		objectValue(t, partial, "Overrides")["MaxBitrate"] != float64(12_345_678) || objectValue(t, partial, "Sources")["MaxBitrate"] != "database" {
		t.Fatal("selective reset cleared an unrelated override or did not restore the original default")
	}
	reset["Revision"] = "4"
	beforeNoop = adminSettingsHTTPRow(t, f)
	if noChange := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK); !reflect.DeepEqual(noChange, partial) || adminSettingsHTTPRow(t, f) != beforeNoop {
		t.Fatal("resetting an already-default field changed persistent settings")
	}
	reset["Fields"] = []string{"ServerName", "MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"}
	restored := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, restored, "5")
	if !reflect.DeepEqual(restored["Defaults"], initial["Defaults"]) || !reflect.DeepEqual(restored["Effective"], initial["Effective"]) ||
		!reflect.DeepEqual(restored["Overrides"], initial["Overrides"]) || !reflect.DeepEqual(restored["Sources"], initial["Sources"]) {
		t.Fatal("complete reset did not retain defaults and restore their original provenance")
	}
}

func TestHTTPAdminSettingsFourNameModesAndIndependentEncodingRoundTrip(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	initial := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, initial, "1")
	host := objectValue(t, initial, "Deployment")["HostName"]
	defaultName := objectValue(t, initial, "Defaults")["ServerName"]
	if host != f.app.settings.Snapshot().HostName {
		t.Fatal("native deployment did not expose the store's frozen startup hostname")
	}
	assertState := func(value map[string]any, revision, mode string, rawName, effectiveName any, width int) {
		t.Helper()
		adminSettingsHTTPAssertSnapshot(t, value, revision)
		source := "database"
		if mode == "deployment" {
			source = "deployment"
		}
		published := f.app.settings.Snapshot()
		var publishedRawName any
		if published.Overrides.ServerName != nil {
			publishedRawName = *published.Overrides.ServerName
		}
		if value["ServerNameMode"] != mode || !reflect.DeepEqual(objectValue(t, value, "Overrides")["ServerName"], rawName) ||
			objectValue(t, value, "Effective")["ServerName"] != effectiveName || objectValue(t, value, "Sources")["ServerName"] != source ||
			objectValue(t, value, "Encoding")["TranscodingMaxWidth"] != float64(width) ||
			!reflect.DeepEqual(value["Defaults"], initial["Defaults"]) || !reflect.DeepEqual(value["Deployment"], initial["Deployment"]) ||
			string(published.ServerNameMode) != mode || !reflect.DeepEqual(publishedRawName, rawName) || published.Effective.ServerName != effectiveName ||
			published.HostName != host || published.Encoding.TranscodingMaxWidth != width {
			t.Fatal("the committed native name mode or independent encoding setting lost its exact persisted meaning")
		}
		for _, field := range []string{"MaxBitrate", "MaxWidth", "MaxHeight", "MaxAudioChannels"} {
			if objectValue(t, value, "Overrides")[field] != nil || objectValue(t, value, "Sources")[field] != "deployment" ||
				objectValue(t, value, "Effective")[field] != objectValue(t, initial, "Effective")[field] {
				t.Fatal("independent encoding writes or resets changed a native output ceiling")
			}
		}
		readback := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
		if !reflect.DeepEqual(readback, value) {
			t.Fatal("native settings readback differs from the committed extended PUT response")
		}
	}
	write := func(body map[string]any) map[string]any {
		return adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", body), http.StatusOK)
	}
	legacy := adminSettingsHTTPUpdate("1", nil, nil)
	legacy["Encoding"] = map[string]any{"TranscodingMaxWidth": 960}
	assertState(write(legacy), "2", "deployment", nil, defaultName, 960)
	empty := adminSettingsHTTPUpdate("2", "", nil)
	empty["ServerNameMode"] = "empty"
	savedEmpty := write(empty)
	assertState(savedEmpty, "3", "empty", "", host, 960)
	empty["Revision"] = "3"
	beforeNoop := adminSettingsHTTPRow(t, f)
	if noop := write(empty); !reflect.DeepEqual(noop, savedEmpty) || adminSettingsHTTPRow(t, f) != beforeNoop {
		t.Fatal("an identical empty-mode update changed its revision, encoding, or stored row")
	}
	unset := adminSettingsHTTPUpdate("3", nil, nil)
	unset["ServerNameMode"] = "unset"
	assertState(write(unset), "4", "unset", nil, host, 960)
	const customName = "  Native \ufffd name  "
	custom := adminSettingsHTTPUpdate("4", customName, nil)
	custom["ServerNameMode"] = "custom"
	custom["Encoding"] = map[string]any{"TranscodingMaxWidth": 512}
	assertState(write(custom), "5", "custom", customName, customName, 512)
	// Older clients omit both extension fields. Their complete name override
	// still derives deployment/custom while the independent encoding survives.
	assertState(write(adminSettingsHTTPUpdate("5", nil, nil)), "6", "deployment", nil, defaultName, 512)
	assertState(write(adminSettingsHTTPUpdate("6", "Legacy custom", nil)), "7", "custom", "Legacy custom", "Legacy custom", 512)
	reset := map[string]any{"Revision": "7", "Fields": []string{"ServerName"}}
	nameReset := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK)
	assertState(nameReset, "8", "deployment", nil, defaultName, 512)
	reset = map[string]any{"Revision": "8", "Fields": []string{"TranscodingMaxWidth"}}
	widthReset := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK)
	assertState(widthReset, "9", "deployment", nil, defaultName, 0)
	reset["Revision"] = "9"
	beforeNoop = adminSettingsHTTPRow(t, f)
	if noop := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", reset), http.StatusOK); !reflect.DeepEqual(noop, widthReset) || adminSettingsHTTPRow(t, f) != beforeNoop {
		t.Fatal("resetting the already-zero independent width changed another setting")
	}
}

func TestHTTPAdminSettingsModeMismatchAndReadOnlyHostRejectWholeBatch(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	seed := adminSettingsHTTPUpdate("1", "Existing custom name", 12_345_678)
	seed["ServerNameMode"] = "custom"
	seed["Encoding"] = map[string]any{"TranscodingMaxWidth": 960}
	adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", seed), http.StatusOK)
	before, published := adminSettingsHTTPRow(t, f), f.app.settings.Snapshot()
	for _, test := range []struct {
		mode string
		name any
	}{
		{"deployment", "private-marker"}, {"deployment", ""},
		{"custom", nil}, {"custom", ""}, {"custom", " \t\n "}, {"custom", "private-marker\x00"},
		{"empty", nil}, {"empty", "private-marker"}, {"unset", ""}, {"unset", "private-marker"},
	} {
		body := adminSettingsHTTPUpdate("2", test.name, 1_234_567)
		body["ServerNameMode"] = test.mode
		body["Encoding"] = map[string]any{"TranscodingMaxWidth": 320}
		response := adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", body)
		value := adminSettingsHTTPObject(t, response, http.StatusBadRequest)
		if objectValue(t, value, "Error")["Code"] != "invalid_input" || strings.Contains(response.Body.String(), "private-marker") ||
			adminSettingsHTTPRow(t, f) != before || !reflect.DeepEqual(f.app.settings.Snapshot(), published) {
			t.Fatal("a mode/name mismatch partially saved numeric or encoding changes, published state, or reflected rejected input")
		}
	}
	for _, body := range []map[string]any{
		adminSettingsHTTPUpdate("2", "", nil),
		{"Revision": "2", "Overrides": seed["Overrides"], "HostName": "private-marker"},
		{"Revision": "2", "Overrides": seed["Overrides"], "Deployment": map[string]any{"HostName": "private-marker"}},
		{"Revision": "2", "Overrides": seed["Overrides"], "Encoding": map[string]any{"TranscodingMaxWidth": 320, "HostName": "private-marker"}},
		{"Revision": "2", "Overrides": seed["Overrides"], "ServerNameMode": "private-marker", "Encoding": map[string]any{"TranscodingMaxWidth": 320}},
	} {
		response := adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", body)
		adminSettingsHTTPObject(t, response, http.StatusBadRequest)
		if strings.Contains(response.Body.String(), "private-marker") || adminSettingsHTTPRow(t, f) != before || !reflect.DeepEqual(f.app.settings.Snapshot(), published) {
			t.Fatal("legacy empty names or read-only host writes changed the complete persisted and published settings")
		}
	}
	response := adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", map[string]any{"Revision": "2", "Fields": []string{"HostName"}})
	adminSettingsHTTPObject(t, response, http.StatusBadRequest)
	if adminSettingsHTTPRow(t, f) != before || !reflect.DeepEqual(f.app.settings.Snapshot(), published) {
		t.Fatal("a read-only hostname reset changed settings")
	}
}

func TestHTTPAdminSettingsStrictWireAndDomainValidationPreserveStoredState(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	before := adminSettingsHTTPRow(t, f)
	beforeRevision := f.app.settings.Snapshot().Revision
	for _, test := range []struct {
		method, path, body, contentType string
		status                          int
	}{
		{http.MethodGet, "/admin/v1/settings?private-marker=query", "", "", http.StatusBadRequest},
		{http.MethodGet, "/admin/v1/settings?", "", "", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings?", adminSettingsNullUpdateForTest, "application/json", http.StatusBadRequest},
		{http.MethodPost, "/admin/v1/settings/reset?", adminSettingsResetForTest, "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", adminSettingsNullUpdateForTest, "text/plain", http.StatusUnsupportedMediaType},
		{http.MethodPut, "/admin/v1/settings", adminSettingsNullUpdateForTest, "application/json; private-marker*=invalid", http.StatusUnsupportedMediaType},
		{http.MethodPut, "/admin/v1/settings", adminSettingsUpdateBodyForTest("MaxWidth", "8193"), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", adminSettingsUpdateBodyForTest("MaxAudioChannels", "9"), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", adminSettingsUpdateBodyForTest("MaxBitrate", "1e6"), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", adminSettingsUpdateBodyForTest("ServerName", `"`+strings.Repeat("private-marker", 11)+`"`), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", adminSettingsUpdateBodyForTest("ServerName", `"private-marker\u0000name"`), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", strings.Replace(adminSettingsNullUpdateForTest, `"Revision":"1"`, `"Revision":"1","\u0052evision":"1"`, 1), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", strings.Replace(adminSettingsNullUpdateForTest, `"Overrides":{`, `"Deployment":{"SetupToken":"private-marker"},"Overrides":{`, 1), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", adminSettingsUpdateBodyForTest("ServerName", `"\ud800private-marker"`), "application/json", http.StatusBadRequest},
		{http.MethodPut, "/admin/v1/settings", strings.Repeat(" ", maxAdminSettingsBodyBytes) + adminSettingsNullUpdateForTest, "application/json", http.StatusBadRequest},
		{http.MethodPost, "/admin/v1/settings/reset", `{"Revision":"1","Fields":["ServerName","ServerName"]}`, "application/json", http.StatusBadRequest},
		{http.MethodPost, "/admin/v1/settings/reset", `{"Revision":"1","Fields":["private-marker"]}`, "application/json", http.StatusBadRequest},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)).WithContext(f.ctx)
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", csrf)
		if test.contentType != "" {
			request.Header.Set("Content-Type", test.contentType)
		}
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		value := adminSettingsHTTPObject(t, response, test.status)
		if objectValue(t, value, "Error")["Code"] != map[int]string{400: "invalid_input", 415: "unsupported_media_type"}[test.status] ||
			strings.Contains(response.Body.String(), "private-marker") || adminSettingsHTTPRow(t, f) != before || f.app.settings.Snapshot().Revision != beforeRevision {
			t.Fatal("invalid HTTP settings input changed state, reflected rejected data, or lost its stable error code")
		}
	}
}

func TestHTTPAdminSettingsNativeAuthorityCSRFAndDeploymentSecretBoundary(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	viewer, err := a.users.CreateUser(a.ctx, "Settings Viewer", "settings-viewer-password", false)
	if err != nil {
		t.Fatal("create the owned non-administrator fixture")
	}
	var tokens []string
	for _, user := range []struct{ name, password, device string }{
		{"Administrator", "administrator-password", "settings-emby-admin"},
		{viewer.Name, "settings-viewer-password", "settings-emby-viewer"},
	} {
		response := a.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{"Username": user.name, "Pw": user.password},
			http.Header{"X-Emby-Client": {"Settings authority fixture"}, "X-Emby-Device-Id": {user.device}})
		if response.Code != http.StatusOK {
			t.Fatal("issue an owned ordinary fixture credential")
		}
		tokens = append(tokens, stringValue(t, jsonObject(t, response), "AccessToken"))
	}
	tokens = append(tokens, a.create(t, "Settings native-boundary key").token)
	before := adminSettingsHTTPRow(t, a.serverFixture)
	deniedUpdate := adminSettingsHTTPUpdate("1", "Denied", nil)
	deniedUpdate["ServerNameMode"] = "custom"
	deniedUpdate["Encoding"] = map[string]any{"TranscodingMaxWidth": 320}
	for _, operation := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/admin/v1/settings", nil},
		{http.MethodPut, "/admin/v1/settings", deniedUpdate},
		{http.MethodPost, "/admin/v1/settings/reset", map[string]any{"Revision": "1", "Fields": []string{"ServerName", "TranscodingMaxWidth"}}},
	} {
		adminSettingsHTTPObject(t, a.request(t, operation.method, operation.path, operation.body, nil), http.StatusUnauthorized)
		for _, token := range tokens {
			for _, asCookie := range []bool{false, true} {
				headers := http.Header{"X-Emby-Token": {token}}
				var cookies []*http.Cookie
				if asCookie {
					headers = nil
					cookies = []*http.Cookie{{Name: "goby_session", Value: token}}
				}
				response := a.request(t, operation.method, operation.path, operation.body, headers, cookies...)
				adminSettingsHTTPObject(t, response, http.StatusUnauthorized)
				if strings.Contains(response.Body.String(), token) {
					t.Fatal("native settings authentication reflected a bearer credential")
				}
			}
		}
		if operation.method != http.MethodGet {
			for _, headers := range []http.Header{nil, {"X-CSRF-Token": {"private-marker"}},
				{"X-CSRF-Token": {a.csrf}, "Origin": {"https://foreign.example.test"}}} {
				response := a.request(t, operation.method, operation.path, operation.body, headers, a.cookie)
				adminSettingsHTTPObject(t, response, http.StatusForbidden)
				if strings.Contains(response.Body.String(), "private-marker") {
					t.Fatal("CSRF rejection echoed rejected input")
				}
			}
		}
	}
	if adminSettingsHTTPRow(t, a.serverFixture) != before {
		t.Fatal("denied settings requests changed the singleton")
	}
	a.app.cfg.DatabaseURL = "private-marker-database"
	a.app.cfg.SetupToken = "private-marker-setup"
	a.app.cfg.APIKeyMasterKeyFile = "private-marker-master-path"
	a.app.cfg.WebDirectory = "private-marker-web-path"
	a.app.cfg.MediaRoots = []string{"private-marker-media-root"}
	a.app.cfg.Transcoding.CacheDirectory = "private-marker-cache-path"
	a.app.cfg.Transcoding.Hardware.Device = "private-marker-hardware-path"
	response := a.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, a.cookie)
	value := adminSettingsHTTPObject(t, response, http.StatusOK)
	deployment := objectValue(t, value, "Deployment")
	if len(deployment) != 8 || deployment["HostName"] != a.app.settings.Snapshot().HostName || strings.Contains(response.Body.String(), "private-marker") {
		t.Fatal("the startup-only deployment projection exposed configuration secrets or paths")
	}
	for _, field := range []string{"TranscodingEnabled", "HardwareDecoder", "HardwareEncoder", "Threads", "MaxJobs", "MaxUserJobs", "MaxSessionJobs", "HostName"} {
		if _, present := deployment[field]; !present {
			t.Fatalf("deployment projection omitted safe field %s", field)
		}
	}
}

func TestHTTPAdminSettingsConcurrentRevisionConflictReturnsCommittedWinner(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for _, proposal := range []struct {
		name  string
		width int
	}{{"First proposed name", 640}, {"Second proposed name", 960}} {
		update := adminSettingsHTTPUpdate("1", proposal.name, 8_000_000)
		update["ServerNameMode"] = "custom"
		update["Encoding"] = map[string]any{"TranscodingMaxWidth": proposal.width}
		body, err := json.Marshal(update)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", bytes.NewReader(body)).WithContext(f.ctx)
		request.AddCookie(cookie)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		go func() {
			<-start
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, request)
			responses <- response
		}()
	}
	close(start)
	var winner map[string]any
	conflicts := 0
	for range 2 {
		select {
		case response := <-responses:
			switch response.Code {
			case http.StatusOK:
				if winner != nil {
					t.Fatal("two writers committed the same starting revision")
				}
				winner = adminSettingsHTTPObject(t, response, http.StatusOK)
				adminSettingsHTTPAssertSnapshot(t, winner, "2")
			case http.StatusConflict:
				value := adminSettingsHTTPObject(t, response, http.StatusConflict)
				if objectValue(t, value, "Error")["Code"] != "revision_conflict" {
					t.Fatal("concurrent update returned the wrong conflict code")
				}
				conflicts++
			default:
				t.Fatalf("concurrent settings update returned status %d", response.Code)
			}
		case <-f.ctx.Done():
			t.Fatal("concurrent settings requests did not finish")
		}
	}
	if winner == nil || conflicts != 1 || f.app.settings.Snapshot().Revision != 2 || winner["ServerNameMode"] != "custom" ||
		objectValue(t, winner, "Encoding")["TranscodingMaxWidth"] != float64(f.app.settings.Snapshot().Encoding.TranscodingMaxWidth) {
		t.Fatal("concurrent updates did not retain one committed winner")
	}
	current := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, cookie), http.StatusOK)
	if !reflect.DeepEqual(current, winner) {
		t.Fatal("the winning PUT response did not describe the committed published state")
	}
	before := adminSettingsHTTPRow(t, f)
	stale := adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPost, "/admin/v1/settings/reset", map[string]any{"Revision": "1", "Fields": []string{"ServerName", "TranscodingMaxWidth"}})
	adminSettingsHTTPObject(t, stale, http.StatusConflict)
	if adminSettingsHTTPRow(t, f) != before {
		t.Fatal("a stale selective reset changed the winner")
	}
}

func TestHTTPAdminSettingsRequestSnapshotRemainsConsistentAcrossConcurrentCommit(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	type observation struct {
		before, after requestSettingsSnapshot
		planning      config.TranscodingConfig
	}
	entered := make(chan requestSettingsSnapshot, 1)
	release := make(chan struct{})
	finished := make(chan observation, 1)
	var releaseOnce sync.Once
	resume := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(resume)
	handler := f.app.withSettingsSnapshot(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		before := f.app.requestSettings(r)
		entered <- before
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		finished <- observation{before: before, after: f.app.requestSettings(r), planning: f.app.requestPlanningConfig(r)}
		w.WriteHeader(http.StatusNoContent)
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/request-settings-observer", nil).WithContext(f.ctx))
	var old requestSettingsSnapshot
	select {
	case old = <-entered:
	case <-f.ctx.Done():
		t.Fatal("the paused request did not capture its entry snapshot")
	}
	if old.Revision != 1 || old.Effective != f.app.settings.Snapshot().Effective || old.TranscodingMaxWidth != 0 {
		t.Fatal("the paused request did not begin with real committed defaults")
	}
	extraWidth := max(1, old.Effective.MaxWidth/2)
	update := adminSettingsHTTPUpdate("1", "A later committed server", 7_654_321)
	update["ServerNameMode"] = "custom"
	update["Encoding"] = map[string]any{"TranscodingMaxWidth": extraWidth}
	updated := adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
		update), http.StatusOK)
	adminSettingsHTTPAssertSnapshot(t, updated, "2")
	if objectValue(t, updated, "Effective")["ServerName"] != "A later committed server" ||
		objectValue(t, updated, "Effective")["MaxBitrate"] != float64(7_654_321) || updated["ServerNameMode"] != "custom" ||
		objectValue(t, updated, "Encoding")["TranscodingMaxWidth"] != float64(extraWidth) {
		t.Fatal("the authorized PUT reused its own older request-entry snapshot")
	}
	resume()
	select {
	case retained := <-finished:
		wanted := f.app.cfg.Transcoding
		wanted.MaxBitrate, wanted.MaxWidth = old.Effective.MaxBitrate, old.Effective.MaxWidth
		wanted.MaxHeight, wanted.MaxAudioChannels = old.Effective.MaxHeight, old.Effective.MaxAudioChannels
		if old.TranscodingMaxWidth > 0 {
			wanted.MaxWidth = min(wanted.MaxWidth, old.TranscodingMaxWidth)
		}
		if retained.before != old || retained.after != old || !reflect.DeepEqual(retained.planning, wanted) {
			t.Fatal("the paused request mixed old and newly committed settings or planning limits")
		}
	case <-f.ctx.Done():
		t.Fatal("the old request did not finish after its barrier was released")
	}
	var fresh requestSettingsSnapshot
	var planning config.TranscodingConfig
	f.app.withSettingsSnapshot(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fresh, planning = f.app.requestSettings(r), f.app.requestPlanningConfig(r)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/request-settings-observer", nil).WithContext(f.ctx))
	if fresh.Revision != 2 || fresh.Effective.ServerName != "A later committed server" || fresh.Effective.MaxBitrate != 7_654_321 ||
		fresh.TranscodingMaxWidth != extraWidth || planning.MaxBitrate != fresh.Effective.MaxBitrate || planning.MaxWidth != min(fresh.Effective.MaxWidth, extraWidth) ||
		planning.MaxHeight != fresh.Effective.MaxHeight || planning.MaxAudioChannels != fresh.Effective.MaxAudioChannels {
		t.Fatal("a new request did not capture one consistent newly committed revision")
	}
}

func TestHTTPAdminSettingsExpiredActorWaitingForBusinessRowCannotCommit(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve the owned administrator")
	}
	before, published := adminSettingsHTTPRow(t, f), f.app.settings.Snapshot()
	blocker, err := f.pool.BeginTx(f.ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal("start the owned business-row barrier")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = blocker.Rollback(ctx)
	})
	if _, err := blocker.Exec(f.ctx, "SELECT id FROM managed_settings WHERE id = 1 FOR UPDATE"); err != nil {
		t.Fatal("lock the owned settings business row")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, actor.SessionID); err != nil {
		t.Fatal("prepare an owned administrator expiry boundary")
	}
	update := adminSettingsHTTPUpdate("1", "Must not commit", nil)
	update["ServerNameMode"] = "custom"
	update["Encoding"] = map[string]any{"TranscodingMaxWidth": 320}
	encoded, err := json.Marshal(update)
	if err != nil {
		t.Fatal("encode the owned extended settings update")
	}
	request := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", bytes.NewReader(encoded)).WithContext(f.ctx)
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		result <- response
	}()
	waitCtx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := f.pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity activity
			JOIN pg_locks locks ON locks.pid = activity.pid WHERE locks.relation = 'managed_settings'::regclass
			AND activity.wait_event_type = 'Lock' AND activity.query LIKE 'SELECT revision, server_name,%')`).Scan(&waiting); err != nil {
			t.Fatal("observe the owned settings writer's business-row wait")
		}
		if waiting {
			break
		}
		select {
		case <-result:
			t.Fatal("the writer did not wait on the owned business row before expiry")
		case <-waitCtx.Done():
			t.Fatal("the settings writer never reached the business-row barrier")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		var expired bool
		if err := f.pool.QueryRow(waitCtx, "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id = $1", actor.SessionID).Scan(&expired); err != nil {
			t.Fatal("observe the owned administrator's natural database expiry")
		}
		if expired {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("the prepared administrator expiry was not observed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := blocker.Rollback(f.ctx); err != nil {
		t.Fatal("release the owned business-row barrier")
	}
	select {
	case response := <-result:
		adminSettingsHTTPObject(t, response, http.StatusUnauthorized)
	case <-waitCtx.Done():
		t.Fatal("the expired writer did not finish after the barrier was released")
	}
	if adminSettingsHTTPRow(t, f) != before || !reflect.DeepEqual(f.app.settings.Snapshot(), published) {
		t.Fatal("an administrator that expired during a business-row wait changed persisted or published settings")
	}
	newCookie, _ := f.adminLogin(t)
	adminSettingsHTTPAssertSnapshot(t, adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, newCookie), http.StatusOK), "1")
}

func TestHTTPAdminSettingsTransactionAudienceAndStoredFailureDoNotLeak(t *testing.T) {
	f, cookie, csrf, adminID := adminSettingsHTTPFixture(t)
	issued, err := f.users.Authenticate(f.ctx, "Administrator", "administrator-password", identity.Client{Name: "Settings Emby boundary", DeviceID: "settings-emby-boundary"}, "emby")
	if err != nil {
		t.Fatal("issue the owned Emby administrator")
	}
	emby, err := f.users.ResolveEmby(f.ctx, issued.Token)
	if err != nil {
		t.Fatal("resolve the owned Emby administrator")
	}
	before := adminSettingsHTTPRow(t, f)
	request := httptest.NewRequest(http.MethodPut, "/admin/v1/settings", strings.NewReader(adminSettingsNullUpdateForTest))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(f.ctx, principalKey, emby))
	response := httptest.NewRecorder()
	f.app.updateAdminSettings(response, request)
	if response.Code != http.StatusUnauthorized || adminSettingsHTTPRow(t, f) != before {
		t.Fatal("the domain transaction accepted an Emby actor for a native settings mutation")
	}
	native, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve the native administrator before role change")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = false WHERE id = $1", adminID); err != nil {
		t.Fatal("demote the owned actor for a stale-principal check")
	}
	request = httptest.NewRequest(http.MethodPost, "/admin/v1/settings/reset", strings.NewReader(adminSettingsResetForTest))
	request.Header.Set("Content-Type", "application/json")
	request = request.WithContext(context.WithValue(f.ctx, principalKey, native))
	response = httptest.NewRecorder()
	f.app.resetAdminSettings(response, request)
	if response.Code != http.StatusUnauthorized || adminSettingsHTTPRow(t, f) != before {
		t.Fatal("a stale native role claim authorized an otherwise no-op reset")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = true WHERE id = $1", adminID); err != nil {
		t.Fatal("restore the isolated actor for the stored-state failure check")
	}
	const secret = "private-marker-unsupported-direct-sql"
	if _, err := f.pool.Exec(f.ctx, "UPDATE managed_settings SET server_name = $1, server_name_mode = 'custom' WHERE id = 1", secret); err != nil {
		t.Fatal("prepare an owned unsupported stored-state change")
	}
	response = adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings", adminSettingsHTTPUpdate("1", nil, nil))
	value := adminSettingsHTTPObject(t, response, http.StatusServiceUnavailable)
	if objectValue(t, value, "Error")["Code"] != "settings_unavailable" || strings.Contains(response.Body.String(), secret) ||
		f.app.settings.Snapshot().Effective.ServerName == secret {
		t.Fatal("stored-state failure exposed unpublished configuration or replaced the committed runtime snapshot")
	}
}
