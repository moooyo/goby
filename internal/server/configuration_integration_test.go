//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/settings"
)

type configurationHTTPFixture struct {
	*applicationKeyHTTPFixture
	admin, viewer clientSessionHTTPLogin
}

func newConfigurationHTTPFixture(t *testing.T) *configurationHTTPFixture {
	t.Helper()
	a := newApplicationKeyHTTPFixture(t)
	if _, err := a.users.CreateUser(a.ctx, "Configuration Viewer", "configuration-viewer-password", false); err != nil {
		t.Fatal("create the configuration viewer")
	}
	return &configurationHTTPFixture{applicationKeyHTTPFixture: a,
		admin:  loginClientSessionHTTP(t, a.serverFixture, "Administrator", "administrator-password", "configuration-admin"),
		viewer: loginClientSessionHTTP(t, a.serverFixture, "Configuration Viewer", "configuration-viewer-password", "configuration-viewer")}
}

func configurationHTTPRaw(f *serverFixture, method, target, body, contentType string, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(f.ctx)
	// Real HTTP parsing canonicalizes header names; cloning a literal map
	// would leave acronym keys such as X-CSRF-Token invisible to Header.Get.
	for name, values := range headers {
		for _, value := range values {
			r.Header.Add(name, value)
		}
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func configurationHTTPStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("configuration HTTP status = %d, want %d", response.Code, status)
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("configuration response can be cached")
	}
	if status == http.StatusNoContent && response.Body.Len() != 0 {
		t.Fatal("configuration mutation returned a nonempty success body")
	}
}

func configurationHTTPObject(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	configurationHTTPStatus(t, response, http.StatusOK)
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Content-Length") != strconv.Itoa(response.Body.Len()) {
		t.Fatal("configuration JSON representation has inconsistent headers")
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result == nil {
		t.Fatal("configuration response was not a JSON object")
	}
	return result
}

func configurationHTTPUnchanged(t *testing.T, f *serverFixture, row string, snapshot settings.Snapshot) {
	t.Helper()
	// Authentication may refresh its own activity metadata. Configuration
	// rollback covers the singleton and published runtime snapshot precisely.
	if adminSettingsHTTPRow(t, f) != row || !reflect.DeepEqual(f.app.settings.Snapshot(), snapshot) {
		t.Fatal("rejected configuration request changed persisted or published settings")
	}
}

func TestHTTPConfigurationAuthorityAliasesAndExactViewerProjection(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	key := f.create(t, "Configuration authority key")
	keyHeaders := http.Header{"X-Emby-Token": {key.token}}
	for _, path := range []string{"/emby/System/Configuration", "/System/Configuration", "/EMBY/sYsTeM/cOnFiGuRaTiOn"} {
		configurationHTTPStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
		viewer := f.request(t, http.MethodGet, path, nil, f.viewer.headers)
		configurationHTTPStatus(t, viewer, http.StatusOK)
		if viewer.Body.String() != "{}" || viewer.Header().Get("Content-Length") != "2" || viewer.Header().Get("Content-Type") != "application/json" {
			t.Fatal("viewer total configuration was not the exact two-byte empty object")
		}
		head := f.request(t, http.MethodHead, path, nil, f.viewer.headers)
		configurationHTTPStatus(t, head, http.StatusOK)
		if head.Body.Len() != 0 || head.Header().Get("Content-Length") != "2" {
			t.Fatal("viewer HEAD did not retain empty-object representation headers")
		}
		for _, headers := range []http.Header{f.admin.headers, keyHeaders} {
			response := f.request(t, http.MethodGet, path, nil, headers)
			value := configurationHTTPObject(t, response)
			if value["IsStartupWizardCompleted"] != true || value["ServerName"] == nil {
				t.Fatal("authorized configuration read omitted backed server fields")
			}
			head := f.request(t, http.MethodHead, path, nil, headers)
			configurationHTTPStatus(t, head, http.StatusOK)
			if head.Body.Len() != 0 || head.Header().Get("Content-Length") != response.Header().Get("Content-Length") {
				t.Fatal("administrator/key HEAD did not preserve configuration representation headers")
			}
		}
	}
	for _, section := range []string{"encoding", "ENCODING", "devices", "dlna", "private-marker"} {
		path := "/emby/System/Configuration/" + section
		configurationHTTPStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
		denied := f.request(t, http.MethodGet, path, nil, f.viewer.headers)
		configurationHTTPStatus(t, denied, http.StatusForbidden)
		if denied.Body.String() != "User Configuration Viewer does not have access to ReadServerConfiguration feature." || denied.Header().Get("Content-Type") != "text/plain" {
			t.Fatal("named configuration used the wrong read permission boundary")
		}
		want := http.StatusNotFound
		if strings.EqualFold(section, "encoding") {
			want = http.StatusOK
		} else if section == "devices" || section == "dlna" {
			want = http.StatusNotImplemented
		}
		response := f.request(t, http.MethodGet, path, nil, f.admin.headers)
		configurationHTTPStatus(t, response, want)
		if want == http.StatusOK {
			head := f.request(t, http.MethodHead, path, nil, f.admin.headers)
			configurationHTTPStatus(t, head, http.StatusOK)
			if head.Body.Len() != 0 || head.Header().Get("Content-Length") != response.Header().Get("Content-Length") {
				t.Fatal("named configuration HEAD returned media or mismatched headers")
			}
		}
		if strings.Contains(response.Body.String(), "private-marker") {
			t.Fatal("unknown configuration name was reflected")
		}
	}
	row, published := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	for _, path := range []string{"/emby/System/Configuration", "/System/Configuration/Partial", "/EMBY/system/configuration/pArTiAl", "/emby/System/Configuration/private-marker"} {
		for _, headers := range []http.Header{nil, {"X-Emby-Token": {"invalid-token"}}} {
			response := configurationHTTPRaw(f.serverFixture, http.MethodPost, path, "invalid private-marker", "text/plain", headers)
			configurationHTTPStatus(t, response, http.StatusUnauthorized)
			if response.Body.String() != embyInvalidTokenMessage {
				t.Fatal("authentication did not precede configuration parsing")
			}
		}
		denied := configurationHTTPRaw(f.serverFixture, http.MethodPost, path, "invalid private-marker", "text/plain", f.viewer.headers)
		configurationHTTPStatus(t, denied, http.StatusForbidden)
		if denied.Body.String() != "User Configuration Viewer does not have access to ManageServer feature." {
			t.Fatal("configuration write used the wrong administrator feature")
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		configurationHTTPStatus(t, f.request(t, method, "/emby/System/Configuration", map[string]any{}, nil, f.cookie), http.StatusUnauthorized)
		configurationHTTPStatus(t, f.request(t, method, "/emby/System/Configuration", map[string]any{}, http.Header{"X-Emby-Token": {f.cookie.Value}}), http.StatusUnauthorized)
	}
	configurationHTTPUnchanged(t, f.serverFixture, row, published)
	query := "/System/Configuration/encoding?api_key=" + url.QueryEscape(key.token)
	configurationHTTPObject(t, f.request(t, http.MethodGet, query, nil, nil))
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/SYSTEM/configuration/pArTiAl", map[string]any{"sErVeRnAmE": "Partial alias name"}, f.admin.headers), http.StatusNoContent)
	if f.app.settings.Snapshot().Effective.ServerName != "Partial alias name" {
		t.Fatal("Partial route was dispatched as an unknown named configuration")
	}
}

func TestHTTPConfigurationRevalidatesStaleRolesKeysAndNativeAudience(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	admin, err := f.users.ResolveEmby(f.ctx, f.admin.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal("resolve the administrator before its role changes")
	}
	native, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve the native audience fixture")
	}
	key := f.create(t, "Stale configuration actor")
	keyActor, err := f.users.ResolveEmby(f.ctx, key.token)
	if err != nil {
		t.Fatal("resolve the key before revocation")
	}
	otherKey := f.create(t, "Foreign configuration client")
	otherActor, err := f.users.ResolveEmby(f.ctx, otherKey.token)
	if err != nil {
		t.Fatal("resolve an unrelated key client")
	}
	row, published := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	invoke := func(actor identity.Principal, handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"ServerName":"Must not commit"}`))
		r = r.WithContext(context.WithValue(f.ctx, principalKey, actor))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	if response := invoke(native, f.app.patchEmbyConfiguration, "/emby/System/Configuration/Partial"); response.Code != http.StatusUnauthorized {
		t.Fatal("configuration domain accepted a native-cookie principal in the Emby audience")
	}
	foreignClient := keyActor
	foreignClient.ClientSessionID = otherActor.ClientSessionID
	if response := invoke(foreignClient, f.app.patchEmbyConfiguration, "/emby/System/Configuration/Partial"); response.Code != http.StatusUnauthorized {
		t.Fatal("a key borrowed another credential's client context for configuration authority")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = false WHERE id = $1", f.adminID); err != nil {
		t.Fatal("demote the owned configuration actor")
	}
	if response := invoke(admin, f.app.patchEmbyConfiguration, "/emby/System/Configuration/Partial"); response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "ManageServer feature.") {
		t.Fatal("stale administrator metadata authorized a configuration mutation")
	}
	if response := invoke(admin, f.app.embyConfiguration, "/emby/System/Configuration"); response.Code != http.StatusOK || response.Body.String() != "{}" {
		t.Fatal("a demoted live login retained the full configuration view")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = true WHERE id = $1", f.adminID); err != nil {
		t.Fatal("restore the owned administrator for key cleanup")
	}
	expectStatus(t, f.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/revoke", map[string]any{}), http.StatusOK)
	if response := invoke(keyActor, f.app.patchEmbyConfiguration, "/emby/System/Configuration/Partial"); response.Code != http.StatusUnauthorized {
		t.Fatal("stale application-key metadata authorized configuration after revocation")
	}
	configurationHTTPUnchanged(t, f.serverFixture, row, published)
}

func TestHTTPConfigurationNativeAndCompatibilityConcurrencyPreservesSections(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	native := adminSettingsHTTPUpdate("1", "Native concurrent name", 8_000_000)
	native["Overrides"].(map[string]any)["MaxWidth"] = 160
	encoded, err := json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan struct {
		native   bool
		response *httptest.ResponseRecorder
	}, 2)
	for _, request := range []struct {
		native                   bool
		method, path, body, mime string
		headers                  http.Header
		cookies                  []*http.Cookie
	}{
		{true, http.MethodPut, "/admin/v1/settings", string(encoded), "application/json", http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {f.cfg.PublicURL}}, []*http.Cookie{f.cookie}},
		{false, http.MethodPost, "/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":96}`, "application/octet-stream", f.admin.headers, nil},
	} {
		go func() {
			<-start
			response := configurationHTTPRaw(f.serverFixture, request.method, request.path, request.body, request.mime, request.headers, request.cookies...)
			results <- struct {
				native   bool
				response *httptest.ResponseRecorder
			}{request.native, response}
		}()
	}
	close(start)
	nativeConflict := false
	for range 2 {
		select {
		case result := <-results:
			if result.native {
				if result.response.Code == http.StatusConflict {
					nativeConflict = true
				} else {
					adminSettingsHTTPObject(t, result.response, http.StatusOK)
				}
			} else {
				configurationHTTPStatus(t, result.response, http.StatusNoContent)
			}
		case <-f.ctx.Done():
			t.Fatal("concurrent configuration requests did not finish")
		}
	}
	if nativeConflict {
		current := adminSettingsHTTPObject(t, f.request(t, http.MethodGet, "/admin/v1/settings", nil, nil, f.cookie), http.StatusOK)
		native["Revision"] = current["Revision"]
		adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings", native), http.StatusOK)
	}
	snapshot := f.app.settings.Snapshot()
	if snapshot.Effective.ServerName != "Native concurrent name" || snapshot.Effective.MaxWidth != 160 || snapshot.Effective.MaxBitrate != 8_000_000 || snapshot.Encoding.TranscodingMaxWidth != 96 {
		t.Fatal("concurrent native and named writes overwrote an unrelated section")
	}
	row := adminSettingsHTTPRow(t, f.serverFixture)
	native["Revision"] = "1"
	stale := adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings", native)
	value := adminSettingsHTTPObject(t, stale, http.StatusConflict)
	if objectValue(t, value, "Error")["Code"] != "revision_conflict" {
		t.Fatal("native CAS lost its conflict contract after a compatibility write")
	}
	configurationHTTPUnchanged(t, f.serverFixture, row, snapshot)
}

func TestHTTPConfigurationExpiredWriterWaitingForSettingsCannotCommit(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	row, published := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	blocker, err := f.pool.BeginTx(f.ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal("create the owned configuration row barrier")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = blocker.Rollback(ctx)
	})
	if _, err := blocker.Exec(f.ctx, "SELECT id FROM managed_settings WHERE id = 1 FOR UPDATE"); err != nil {
		t.Fatal("lock the configuration singleton")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour', expires_at = clock_timestamp() + interval '3 seconds' WHERE id = $1`, f.admin.id); err != nil {
		t.Fatal("prepare the administrator's natural expiry boundary")
	}
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- configurationHTTPRaw(f.serverFixture, http.MethodPost, "/emby/System/Configuration/Partial", `{"ServerName":"Expired write"}`, "application/json", f.admin.headers)
	}()
	waitCtx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := f.pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity activity JOIN pg_locks locks ON locks.pid = activity.pid
			WHERE locks.relation = 'managed_settings'::regclass AND activity.wait_event_type = 'Lock' AND activity.query LIKE 'SELECT revision, server_name,%')`).Scan(&waiting); err != nil {
			t.Fatal("observe the configuration writer at its owned business-row barrier")
		}
		if waiting {
			break
		}
		select {
		case <-result:
			t.Fatal("configuration write finished before reaching its row barrier")
		case <-waitCtx.Done():
			t.Fatal("configuration writer did not reach its row barrier")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		var expired bool
		if err := f.pool.QueryRow(waitCtx, "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id = $1", f.admin.id).Scan(&expired); err != nil {
			t.Fatal("observe database-clock configuration actor expiry")
		}
		if expired {
			break
		}
		select {
		case <-waitCtx.Done():
			t.Fatal("configuration actor did not reach the prepared expiry")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := blocker.Rollback(f.ctx); err != nil {
		t.Fatal("release the configuration business-row barrier")
	}
	select {
	case response := <-result:
		configurationHTTPStatus(t, response, http.StatusUnauthorized)
	case <-waitCtx.Done():
		t.Fatal("expired configuration writer did not return")
	}
	configurationHTTPUnchanged(t, f.serverFixture, row, published)
}

func TestHTTPConfigurationFullPartialEncodingAndReadonlyState(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	seed := adminSettingsHTTPUpdate("1", nil, 8_000_000)
	seed["Overrides"].(map[string]any)["MaxWidth"] = 160
	adminSettingsHTTPObject(t, adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings", seed), http.StatusOK)
	getTotal := func() map[string]any {
		return configurationHTTPObject(t, f.request(t, http.MethodGet, "/emby/System/Configuration", nil, f.admin.headers))
	}
	getEncoding := func() map[string]any {
		return configurationHTTPObject(t, f.request(t, http.MethodGet, "/emby/System/Configuration/encoding", nil, f.admin.headers))
	}
	baseline := getTotal()
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration", baseline, f.admin.headers), http.StatusNoContent)
	if !reflect.DeepEqual(getTotal(), baseline) || f.app.settings.Snapshot().ServerNameMode != settings.ServerNameCustom {
		t.Fatal("GET clone POST lost its values or configured-name identity")
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": "First partial name"}, f.admin.headers), http.StatusNoContent)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{}, f.admin.headers), http.StatusNoContent)
	if getTotal()["ServerName"] != "First partial name" {
		t.Fatal("missing Partial name did not preserve the current name")
	}
	for _, contentType := range []string{"application/json", "application/octet-stream"} {
		configurationHTTPStatus(t, configurationHTTPRaw(f.serverFixture, http.MethodPost, "/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":96}`, contentType, f.admin.headers), http.StatusNoContent)
		if getEncoding()["TranscodingMaxWidth"] != float64(96) || getTotal()["ServerName"] != "First partial name" || f.app.settings.Snapshot().Effective.MaxWidth != 160 {
			t.Fatal("named encoding changed native width or server configuration")
		}
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/encoding", getEncoding(), f.admin.headers), http.StatusNoContent)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration", map[string]any{}, f.admin.headers), http.StatusNoContent)
	if _, present := getTotal()["ServerName"]; present || f.app.settings.Snapshot().ServerNameMode != settings.ServerNameUnset || getEncoding()["TranscodingMaxWidth"] != float64(96) {
		t.Fatal("empty full configuration did not reset only its server-name section")
	}
	configurationHTTPStatus(t, configurationHTTPRaw(f.serverFixture, http.MethodPost, "/emby/System/Configuration/encoding", `{}`, "application/octet-stream", f.admin.headers), http.StatusNoContent)
	if getEncoding()["TranscodingMaxWidth"] != float64(0) || f.app.settings.Snapshot().Effective.MaxWidth != 160 || f.app.settings.Snapshot().ServerNameMode != settings.ServerNameUnset {
		t.Fatal("empty encoding configuration reset an unrelated setting")
	}
	for _, name := range []any{"", nil, "Restored custom name"} {
		configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": name, "IsStartupWizardCompleted": true}, f.admin.headers), http.StatusNoContent)
		total := getTotal()
		configured, present := total["ServerName"]
		if name == nil && present || name != nil && (!present || configured != name) {
			t.Fatal("configuration lost empty/null/custom name distinctions")
		}
		public := f.request(t, http.MethodGet, "/emby/System/Info/Public", nil, nil)
		expectStatus(t, public, http.StatusOK)
		want := name
		if name == nil || name == "" {
			want = f.app.settings.Snapshot().HostName
		}
		if jsonObject(t, public)["ServerName"] != want {
			t.Fatal("public system name did not use the configured name or frozen host fallback")
		}
	}
	row, snapshot := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": "Must roll back", "IsStartupWizardCompleted": false}, f.admin.headers), http.StatusBadRequest)
	configurationHTTPUnchanged(t, f.serverFixture, row, snapshot)
	if snapshot.Effective.MaxBitrate != 8_000_000 || snapshot.Effective.MaxWidth != 160 {
		t.Fatal("configuration writes lost unrelated native overrides")
	}
}

func TestHTTPConfigurationInvalidRequestsRollbackWithoutEchoingInput(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	row, published := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	for _, test := range []struct {
		path, body, mime string
		status           int
	}{
		{"/emby/System/Configuration/Partial", `{"ServerName":"Must not commit","ImageExtractionTimeoutMs":"private-marker"}`, "application/json", 400},
		{"/emby/System/Configuration", `{"ServerName":"Must not commit","SortRemoveWords":["private-marker"]}`, "application/json", 400},
		{"/emby/System/Configuration/Partial", `{"ServerName":"first","SERVERNAME":"private-marker"}`, "application/json", 400},
		{"/emby/System/Configuration/Partial", `{"ServerName":"\ud800private-marker"}`, "application/json", 400},
		{"/emby/System/Configuration/Partial", `{"ServerName":"` + strings.Repeat("x", 129) + `"}`, "application/json", 400},
		{"/emby/System/Configuration/Partial", strings.Repeat(" ", maxConfigurationBodyBytes) + `{"ServerName":"private-marker"}`, "application/json", 400},
		{"/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":null}`, "application/json", 400},
		{"/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":1e3}`, "application/json", 400},
		{"/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":-1}`, "application/json", 400},
		{"/emby/System/Configuration/encoding", `{}`, "application/json; charset=latin1", 415},
		{"/emby/System/Configuration", `{}`, "application/octet-stream", 415},
		{"/emby/System/Configuration/Partial?ServerName=private-marker", `{}`, "application/json", 400},
		{"/emby/System/Configuration/private-marker", `{}`, "application/json", 404},
		{"/emby/System/Configuration/devices", `{}`, "application/json", 501},
		{"/emby/System/Configuration/dlna", `{}`, "application/json", 501},
	} {
		response := configurationHTTPRaw(f.serverFixture, http.MethodPost, test.path, test.body, test.mime, f.admin.headers)
		configurationHTTPStatus(t, response, test.status)
		if strings.Contains(response.Body.String(), "private-marker") || strings.Contains(f.logs.String(), "private-marker") {
			t.Fatal("configuration diagnostics exposed rejected input")
		}
		configurationHTTPUnchanged(t, f.serverFixture, row, published)
	}
}

func TestHTTPConfigurationApplicationKeysMutateAndRetainOldNameSnapshots(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	old := f.create(t, "Configuration writer")
	headers := http.Header{"X-Emby-Token": {old.token}}
	for _, path := range []string{"/emby/System/Configuration", "/emby/System/Configuration/encoding"} {
		configurationHTTPObject(t, f.request(t, http.MethodGet, path, nil, headers))
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration", map[string]any{"ServerName": "Key full name", "IsStartupWizardCompleted": true}, headers), http.StatusNoContent)
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": "Key partial name"}, headers), http.StatusNoContent)
	configurationHTTPStatus(t, configurationHTTPRaw(f.serverFixture, http.MethodPost, "/emby/System/Configuration/encoding", `{"TranscodingMaxWidth":128}`, "application/octet-stream", headers), http.StatusNoContent)
	if f.app.settings.Snapshot().Effective.ServerName != "Key partial name" || f.app.settings.Snapshot().Encoding.TranscodingMaxWidth != 128 {
		t.Fatal("application-key writes did not change both supported configuration sections")
	}
	readDefaultName := func(key applicationKeyHTTPSecret) string {
		t.Helper()
		var credentialName, clientName string
		if err := f.pool.QueryRow(f.ctx, `SELECT a.device_name, c.device_name FROM application_keys k JOIN sessions a ON a.id = k.credential_id
			JOIN application_key_clients c ON c.credential_id = a.id AND c.client_name = a.client_name AND c.device_id = a.device_id WHERE k.id = $1`, key.numericID(t)).Scan(&credentialName, &clientName); err != nil || credentialName != clientName {
			t.Fatal("key/default-client name snapshot was inconsistent")
		}
		return clientName
	}
	oldName := readDefaultName(old)
	for _, name := range []any{"", nil} {
		configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": name}, f.admin.headers), http.StatusNoContent)
		created := f.create(t, "Host fallback key")
		if readDefaultName(created) != f.app.settings.Snapshot().HostName || readDefaultName(old) != oldName {
			t.Fatal("empty/unset naming did not snapshot the host for new keys while preserving an old key")
		}
	}
	row, published := adminSettingsHTTPRow(t, f.serverFixture), f.app.settings.Snapshot()
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial", map[string]any{"ServerName": "Reject atomically", "IsStartupWizardCompleted": false}, headers), http.StatusBadRequest)
	configurationHTTPUnchanged(t, f.serverFixture, row, published)
	revoked := f.native(t, http.MethodPost, "/admin/v1/api-keys/"+old.id+"/revoke", map[string]any{})
	expectStatus(t, revoked, http.StatusOK)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		configurationHTTPStatus(t, f.request(t, method, "/emby/System/Configuration", map[string]any{"ServerName": "Denied"}, headers), http.StatusUnauthorized)
	}
	configurationHTTPUnchanged(t, f.serverFixture, row, published)
}
