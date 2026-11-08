//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func expectCoreRestartStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	expectStatus(t, response, status)
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("restart response can be cached")
	}
	if status == http.StatusOK && (response.Body.Len() != 0 || !response.Flushed) {
		t.Fatal("core restart did not flush an empty acknowledgement")
	}
}

func waitCoreRestartOwner(t *testing.T, requested <-chan struct{}, calls *atomic.Int32) {
	t.Helper()
	select {
	case <-requested:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted restart did not reach the process owner")
	}
	select {
	case <-requested:
		t.Fatal("duplicate restart reached the process owner")
	case <-time.After(300 * time.Millisecond):
	}
	if calls.Load() != 1 {
		t.Fatal("shared restart admission did not notify its owner exactly once")
	}
}

func TestHTTPCoreRestartAuthorizesBeforeInputsAndCapability(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	for _, test := range []struct {
		name    string
		headers http.Header
		cookie  *http.Cookie
		status  int
	}{
		{name: "anonymous", status: http.StatusUnauthorized},
		{name: "invalid_token", headers: http.Header{"X-Emby-Token": {"invalid-restart-token"}}, status: http.StatusUnauthorized},
		{name: "viewer", headers: f.viewer.headers, status: http.StatusForbidden},
		{name: "native_cookie", cookie: f.cookie, status: http.StatusUnauthorized},
		{name: "native_token", headers: http.Header{"X-Emby-Token": {f.cookie.Value}}, status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if test.cookie != nil {
				cookies = append(cookies, test.cookie)
			}
			response := configurationHTTPRaw(f.serverFixture, http.MethodPost, "/EMBY/system/rEsTaRt?private-marker=1", "private-marker", "text/plain", test.headers, cookies...)
			expectCoreRestartStatus(t, response, test.status)
			if strings.Contains(response.Body.String(), "private-marker") {
				t.Fatal("restart authorization disclosed rejected request inputs")
			}
			if test.status == http.StatusForbidden && response.Body.String() != "User Configuration Viewer does not have access to RestartServer feature." {
				t.Fatal("restart used the wrong administrator feature boundary")
			}
		})
	}
	expectCoreRestartStatus(t, f.request(t, http.MethodPost, "/System/Restart", nil, f.admin.headers), http.StatusConflict)
	var calls atomic.Int32
	WithAdministratorRestart(func() { calls.Add(1) })(f.app)
	for _, test := range []struct {
		name, target, body string
	}{
		{name: "business_query", target: "/System/Restart?Force=true"},
		{name: "nonempty_body", target: "/System/Restart", body: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			expectCoreRestartStatus(t, configurationHTTPRaw(f.serverFixture, http.MethodPost, test.target, test.body, "application/json", f.admin.headers), http.StatusBadRequest)
		})
	}
	request := httptest.NewRequest(http.MethodPost, "/emby/System/Restart", http.NoBody).WithContext(f.ctx)
	request.ContentLength = -1
	request.TransferEncoding = []string{"chunked"}
	for name, values := range f.admin.headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	expectCoreRestartStatus(t, response, http.StatusBadRequest)
	if f.app.restartController.pending.Load() || calls.Load() != 0 {
		t.Fatal("rejected restart occupied admission or notified its owner")
	}
}

func TestHTTPCoreRestartAliasesAndSharedNativeAdmission(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	key := f.create(t, "Core restart owner")
	keyHeaders := http.Header{"X-Emby-Token": {key.token}}
	for _, test := range []struct {
		name, target string
		headers      http.Header
	}{
		{name: "administrator", target: "/emby/System/Restart", headers: f.admin.headers},
		{name: "application_key", target: "/System/Restart", headers: keyHeaders},
		{name: "credential_query_alias", target: "/EMBY/sYsTeM/rEsTaRt?api_key=" + url.QueryEscape(key.token)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			requested := make(chan struct{}, 4)
			WithAdministratorRestart(func() {
				calls.Add(1)
				select {
				case requested <- struct{}{}:
				default:
				}
			})(f.app)
			expectCoreRestartStatus(t, f.request(t, http.MethodPost, test.target, nil, test.headers), http.StatusOK)
			if !f.app.restartController.pending.Load() {
				t.Fatal("accepted restart did not retain its admission latch")
			}
			expectCoreRestartStatus(t, f.request(t, http.MethodPost, "/System/Restart", nil, keyHeaders), http.StatusConflict)
			body := map[string]any{"Revision": strconv.FormatInt(f.app.serverPolicy.Snapshot().Revision, 10), "Confirm": true}
			expectCoreRestartStatus(t, f.native(t, http.MethodPost, "/admin/v1/server/restart", body), http.StatusConflict)
			waitCoreRestartOwner(t, requested, &calls)
		})
	}
	t.Run("native_then_compatibility", func(t *testing.T) {
		var calls atomic.Int32
		requested := make(chan struct{}, 4)
		WithAdministratorRestart(func() {
			calls.Add(1)
			select {
			case requested <- struct{}{}:
			default:
			}
		})(f.app)
		body := map[string]any{"Revision": strconv.FormatInt(f.app.serverPolicy.Snapshot().Revision, 10), "Confirm": true}
		expectCoreRestartStatus(t, f.native(t, http.MethodPost, "/admin/v1/server/restart", body), http.StatusAccepted)
		expectCoreRestartStatus(t, f.request(t, http.MethodPost, "/emby/System/Restart", nil, keyHeaders), http.StatusConflict)
		waitCoreRestartOwner(t, requested, &calls)
	})
	t.Run("concurrent_administrator_and_key", func(t *testing.T) {
		var calls atomic.Int32
		requested := make(chan struct{}, 4)
		WithAdministratorRestart(func() {
			calls.Add(1)
			select {
			case requested <- struct{}{}:
			default:
			}
		})(f.app)
		start := make(chan struct{})
		responses := make(chan *httptest.ResponseRecorder, 2)
		for _, headers := range []http.Header{f.admin.headers, keyHeaders} {
			go func(headers http.Header) {
				<-start
				responses <- configurationHTTPRaw(f.serverFixture, http.MethodPost, "/System/Restart", "", "", headers)
			}(headers)
		}
		close(start)
		accepted, conflicts := 0, 0
		for range 2 {
			select {
			case response := <-responses:
				switch response.Code {
				case http.StatusOK:
					accepted++
				case http.StatusConflict:
					conflicts++
				default:
					t.Fatalf("concurrent restart returned HTTP %d", response.Code)
				}
				expectCoreRestartStatus(t, response, response.Code)
			case <-f.ctx.Done():
				t.Fatal("concurrent restart admission did not finish")
			}
		}
		if accepted != 1 || conflicts != 1 {
			t.Fatal("competing restart credentials did not share one admission latch")
		}
		waitCoreRestartOwner(t, requested, &calls)
	})
}

func TestHTTPCoreRestartRevalidatesStaleAdministratorAndKeyAuthority(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	admin, err := f.users.ResolveEmby(f.ctx, f.admin.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal("resolve the administrator before authority changes")
	}
	native, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve the native audience fixture")
	}
	key, other := f.create(t, "Stale restart owner"), f.create(t, "Independent restart owner")
	keyActor, err := f.users.ResolveEmby(f.ctx, key.token)
	if err != nil {
		t.Fatal("resolve the key before revocation")
	}
	otherActor, err := f.users.ResolveEmby(f.ctx, other.token)
	if err != nil {
		t.Fatal("resolve the independent key client")
	}
	if !keyActor.IsApplicationKey() || keyActor.User.ID != "" {
		t.Fatal("restart application credential unexpectedly acquired a user")
	}
	var calls atomic.Int32
	WithAdministratorRestart(func() { calls.Add(1) })(f.app)
	invoke := func(ctx context.Context, actor identity.Principal) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/emby/System/Restart", http.NoBody)
		request = request.WithContext(context.WithValue(ctx, principalKey, actor))
		response := httptest.NewRecorder()
		f.app.embyRestart(response, request)
		return response
	}
	expectStatus(t, invoke(f.ctx, native), http.StatusUnauthorized)
	for _, field := range []string{"client", "parent", "key"} {
		t.Run("foreign_"+field, func(t *testing.T) {
			actor := keyActor
			switch field {
			case "client":
				actor.ClientSessionID = otherActor.ClientSessionID
			case "parent":
				actor.SessionID = otherActor.SessionID
			case "key":
				actor.ApplicationKeyID = otherActor.ApplicationKeyID
			}
			expectStatus(t, invoke(f.ctx, actor), http.StatusUnauthorized)
		})
	}
	for _, test := range []struct {
		name, statement, id string
		status              int
	}{
		{name: "demoted", statement: "UPDATE users SET is_administrator=false WHERE id=$1", id: admin.User.ID, status: http.StatusForbidden},
		{name: "disabled", statement: "UPDATE users SET is_disabled=true WHERE id=$1", id: admin.User.ID, status: http.StatusUnauthorized},
		{name: "revoked_login", statement: "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", id: admin.SessionID, status: http.StatusUnauthorized},
		{name: "expired_login", statement: "UPDATE sessions SET expires_at=clock_timestamp()-interval '1 minute' WHERE id=$1", id: admin.SessionID, status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator=true,is_disabled=false WHERE id=$1", admin.User.ID); err != nil {
				t.Fatal("restore the owned administrator for its next authority boundary")
			}
			if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=NULL,created_at=clock_timestamp()-interval '1 hour',expires_at=clock_timestamp()+interval '1 day' WHERE id=$1", admin.SessionID); err != nil {
				t.Fatal("restore the owned administrator login lifetime")
			}
			if _, err := f.pool.Exec(f.ctx, test.statement, test.id); err != nil {
				t.Fatal("change the committed administrator authority")
			}
			expectStatus(t, invoke(f.ctx, admin), test.status)
		})
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", keyActor.SessionID); err != nil {
		t.Fatal("revoke the owned application credential after its snapshot")
	}
	expectStatus(t, invoke(f.ctx, keyActor), http.StatusUnauthorized)
	expectCoreRestartStatus(t, f.request(t, http.MethodPost, "/emby/System/Restart", nil, http.Header{"X-Emby-Token": {key.token}}), http.StatusUnauthorized)
	canceled, cancel := context.WithCancel(f.ctx)
	cancel()
	invoke(canceled, otherActor)
	if f.app.restartController.pending.Load() || calls.Load() != 0 {
		t.Fatal("stale or canceled authority occupied restart admission")
	}
}

func TestHTTPCoreRestartCapabilityReflectsOwnerWithoutGrantingAuthority(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	key := f.create(t, "Restart capability reader")
	for _, available := range []bool{false, true} {
		if available {
			WithAdministratorRestart(func() {})(f.app)
		}
		for _, headers := range []http.Header{f.admin.headers, f.viewer.headers, {"X-Emby-Token": {key.token}}} {
			value := jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, headers))
			if value["CanSelfRestart"] != available {
				t.Fatal("system restart capability differs from its configured process owner")
			}
		}
		public := jsonObject(t, f.request(t, http.MethodGet, "/System/Info/Public", nil, nil))
		if _, exists := public["CanSelfRestart"]; exists {
			t.Fatal("public discovery exposed a management capability")
		}
	}
	f.app.restartController.pending.Store(true)
	value := jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, f.admin.headers))
	if value["CanSelfRestart"] != true {
		t.Fatal("pending admission concealed the deployment's restart capability")
	}
	expectCoreRestartStatus(t, f.request(t, http.MethodPost, "/System/Restart", nil, f.viewer.headers), http.StatusForbidden)
	f.app.restartController = &adminRestartController{}
	value = jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, f.admin.headers))
	if value["CanSelfRestart"] != false {
		t.Fatal("a controller without a process callback advertised restart")
	}
}
