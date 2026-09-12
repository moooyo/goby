//go:build linux

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

type rootBindingHTTPAuthRoute struct {
	name, method, pattern, path string
	handler                     http.HandlerFunc
	body                        any
}

func rootBindingHTTPAuthRoutes(f *rootBindingHTTPFixture) []rootBindingHTTPAuthRoute {
	return []rootBindingHTTPAuthRoute{
		{name: "list", method: http.MethodGet, pattern: "GET /admin/v1/libraries/{id}/roots", path: f.rootListPath(), handler: f.app.listRegisteredRoots},
		{name: "detail", method: http.MethodGet, pattern: "GET /admin/v1/libraries/{id}/roots/{rootId}/binding", path: f.bindingPath(), handler: f.app.getRootBinding},
		{name: "update", method: http.MethodPut, pattern: "PUT /admin/v1/libraries/{id}/roots/{rootId}/binding", path: f.bindingPath(), handler: f.app.updateRootBinding, body: map[string]any{
			"Revision": "1", "ObservedFingerprint": strings.Repeat("a", 64), "AcknowledgeMissingRemoval": true,
		}},
	}
}

func expectRootBindingHTTPAuthDenied(t *testing.T, f *rootBindingHTTPFixture, response *httptest.ResponseRecorder, before string, status int, code string) {
	t.Helper()
	expectAPIError(t, response, status, code, false)
	for _, path := range []string{f.root, f.registered} {
		if path != "" && strings.Contains(response.Body.String(), path) {
			t.Error("denied root binding request exposed a configured storage path")
		}
	}
	object := jsonObject(t, response)
	for _, field := range []string{"Items", "Binding", "Path", "AllowedPath", "RelativePath", "Approved", "Observed"} {
		if _, exposed := object[field]; exposed {
			t.Errorf("denied root binding request exposed field %s", field)
		}
	}
	if f.state(t) != before {
		t.Fatal("denied root binding request changed catalog rows, user item data, or catalog audit history")
	}
}

func TestHTTPRootBindingsRequireNativeAdministratorCredentials(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	managedHTTPCreate(t, f.serverFixture, f.cookie, f.csrf, "Root Binding Viewer", false)
	embyToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	viewerToken := stringValue(t, f.embyLogin(t, "Root Binding Viewer", "managed-user-password"), "AccessToken")
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "root-binding-master.key")))
	f.app.identity = f.users
	keyResponse := f.request(t, http.MethodPost, "/admin/v1/api-keys", map[string]any{"AppName": "Root Binding Credential Isolation"}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, keyResponse, http.StatusCreated)
	keyToken := stringValue(t, jsonObject(t, keyResponse), "AccessToken")
	keyClient := identity.Client{Name: "Root Binding Fixture", DeviceID: "root-binding-key-client", Device: "Fixture", Version: "1"}
	keyActor, err := f.users.ResolveEmbyForClient(f.ctx, keyToken, keyClient)
	if err != nil || !keyActor.IsApplicationKey() {
		t.Fatalf("application credential fixture did not resolve: %v", err)
	}
	before := f.state(t)
	for _, route := range rootBindingHTTPAuthRoutes(f) {
		for _, credential := range []struct {
			name, suffix, code string
			headers            http.Header
			cookie             *http.Cookie
		}{
			{name: "missing", code: "authentication_required"},
			{name: "emby_header", headers: http.Header{"X-Emby-Token": {embyToken}}, code: "authentication_required"},
			{name: "emby_authorization_header", headers: http.Header{"Authorization": {`Emby Token="` + embyToken + `"`}}, code: "authentication_required"},
			{name: "emby_query", suffix: "?api_key=" + url.QueryEscape(embyToken), code: "authentication_required"},
			{name: "emby_cookie", cookie: &http.Cookie{Name: sessionCookie, Value: embyToken}, code: "invalid_credentials"},
			{name: "application_key_header", headers: http.Header{"X-Emby-Token": {keyToken}}, code: "authentication_required"},
			{name: "application_key_cookie", cookie: &http.Cookie{Name: sessionCookie, Value: keyToken}, code: "invalid_credentials"},
			{name: "ordinary_user_header", headers: http.Header{"X-Emby-Token": {viewerToken}}, code: "authentication_required"},
			{name: "ordinary_user_cookie", cookie: &http.Cookie{Name: sessionCookie, Value: viewerToken}, code: "invalid_credentials"},
			{name: "native_token_header", headers: http.Header{"X-Emby-Token": {f.cookie.Value}}, code: "authentication_required"},
		} {
			t.Run(route.name+"/"+credential.name, func(t *testing.T) {
				headers := credential.headers.Clone()
				if headers == nil {
					headers = make(http.Header)
				}
				var cookies []*http.Cookie
				if credential.cookie != nil {
					cookies = append(cookies, credential.cookie)
					headers.Set("X-CSRF-Token", csrfToken(credential.cookie.Value))
				}
				response := f.request(t, route.method, route.path+credential.suffix, route.body, headers, cookies...)
				expectRootBindingHTTPAuthDenied(t, f, response, before, http.StatusUnauthorized, credential.code)
			})
		}
	}
	for _, test := range []struct {
		name, code string
		headers    http.Header
	}{
		{name: "missing_csrf", code: "csrf_invalid"},
		{name: "wrong_csrf", headers: http.Header{"X-CSRF-Token": {"incorrect-csrf-token"}}, code: "csrf_invalid"},
		{name: "foreign_origin", headers: http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://foreign.example.test"}}, code: "origin_denied"},
		{name: "cross_site", headers: http.Header{"X-CSRF-Token": {f.csrf}, "Sec-Fetch-Site": {"cross-site"}}, code: "origin_denied"},
	} {
		t.Run("update/"+test.name, func(t *testing.T) {
			route := rootBindingHTTPAuthRoutes(f)[2]
			response := f.request(t, route.method, route.path, route.body, test.headers, f.cookie)
			expectRootBindingHTTPAuthDenied(t, f, response, before, http.StatusForbidden, test.code)
		})
	}

	// Exercise the store's native audience check even if a weaker middleware
	// supplies a valid administrator or application-key compatibility principal.
	for _, route := range rootBindingHTTPAuthRoutes(f) {
		for _, credential := range []struct{ name, token string }{{"emby", embyToken}, {"application_key", keyToken}} {
			t.Run(route.name+"/store_rejects_"+credential.name, func(t *testing.T) {
				mux := http.NewServeMux()
				entered := false
				mux.HandleFunc(route.pattern, f.app.requireEmby(func(w http.ResponseWriter, r *http.Request) {
					entered = true
					route.handler(w, r)
				}))
				f.handler = f.app.middleware(mux)
				headers := http.Header{
					"X-Emby-Token": {credential.token}, "X-Emby-Client": {keyClient.Name},
					"X-Emby-Device-Id": {keyClient.DeviceID}, "X-Emby-Device-Name": {keyClient.Device}, "X-Emby-Client-Version": {keyClient.Version},
				}
				response := f.request(t, route.method, route.path, route.body, headers)
				if !entered {
					t.Fatal("valid compatibility credential did not reach the root binding handler")
				}
				expectRootBindingHTTPAuthDenied(t, f, response, before, http.StatusUnauthorized, "unauthorized")
			})
		}
	}
}

func rootBindingHTTPInvalidateAuthority(t *testing.T, f *rootBindingHTTPFixture, actor identity.Principal, authority, ordinaryID string) {
	t.Helper()
	var createdAt time.Time
	if err := f.pool.QueryRow(f.ctx, "SELECT created_at FROM sessions WHERE id = $1", actor.SessionID).Scan(&createdAt); err != nil {
		t.Fatalf("read native session fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator = true, is_disabled = false WHERE id = $1", actor.User.ID); err != nil {
			t.Errorf("restore administrator fixture: %v", err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET user_id = $2, created_at = $3, expires_at = $4,
			revoked_at = NULL WHERE id = $1`, actor.SessionID, actor.User.ID, createdAt, actor.ExpiresAt); err != nil {
			t.Errorf("restore native session fixture: %v", err)
		}
	})
	query, args := "", []any{actor.SessionID}
	switch authority {
	case "ordinary_user":
		query = "UPDATE sessions SET user_id = $2 WHERE id = $1"
		args = append(args, ordinaryID)
	case "revoked":
		query = "UPDATE sessions SET revoked_at = now() WHERE id = $1"
	case "expired":
		query = "UPDATE sessions SET created_at = now() - interval '1 day', expires_at = now() - interval '1 second' WHERE id = $1"
	case "demoted":
		query, args = "UPDATE users SET is_administrator = false WHERE id = $1", []any{actor.User.ID}
	case "disabled":
		query, args = "UPDATE users SET is_disabled = true WHERE id = $1", []any{actor.User.ID}
	default:
		t.Fatalf("unknown native authority fixture %q", authority)
	}
	result, err := f.pool.Exec(f.ctx, query, args...)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("invalidate native authority fixture %s: %v", authority, err)
	}
}

func TestHTTPRootBindingsRejectInvalidNativeSessions(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	ordinaryID := managedHTTPCreate(t, f.serverFixture, f.cookie, f.csrf, "Root Binding Ordinary Owner", false)
	actor, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatalf("resolve native administrator fixture: %v", err)
	}
	before := f.state(t)
	login := f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{
		"Name": "Root Binding Ordinary Owner", "Password": "managed-user-password",
	}, nil)
	expectRootBindingHTTPAuthDenied(t, f, login, before, http.StatusUnauthorized, "invalid_credentials")
	for _, authority := range []string{"ordinary_user", "demoted", "disabled", "revoked", "expired"} {
		t.Run(authority, func(t *testing.T) {
			// Ordinary users cannot obtain native sessions through login. Give
			// a real issued session an ordinary owner to test persisted authority.
			rootBindingHTTPInvalidateAuthority(t, f, actor, authority, ordinaryID)
			for _, route := range rootBindingHTTPAuthRoutes(f) {
				t.Run(route.name, func(t *testing.T) {
					response := f.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
					expectRootBindingHTTPAuthDenied(t, f, response, before, http.StatusUnauthorized, "invalid_credentials")
				})
			}
		})
	}
}

func TestHTTPRootBindingsRevalidateNativeAuthorityAfterMiddleware(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	actor, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatalf("resolve native administrator fixture: %v", err)
	}
	before := f.state(t)
	for _, route := range rootBindingHTTPAuthRoutes(f) {
		for _, authority := range []string{"revoked", "expired", "demoted", "disabled"} {
			t.Run(route.name+"/"+authority, func(t *testing.T) {
				mux := http.NewServeMux()
				entered := false
				mux.HandleFunc(route.pattern, f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
					resolved, ok := r.Context().Value(principalKey).(identity.Principal)
					if !ok || resolved.Kind != "admin" || resolved.SessionID != actor.SessionID || !resolved.User.IsAdministrator || resolved.User.IsDisabled {
						t.Fatal("middleware did not supply the live native administrator fixture")
					}
					entered = true
					rootBindingHTTPInvalidateAuthority(t, f, resolved, authority, "")
					// Preserve the already authenticated context so the handler
					// must detect the revoked authority inside its transaction.
					route.handler(w, r)
				}))
				f.handler = f.app.middleware(mux)
				response := f.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
				if !entered {
					t.Fatal("request did not pass native administrator middleware before authority changed")
				}
				expectRootBindingHTTPAuthDenied(t, f, response, before, http.StatusUnauthorized, "unauthorized")
			})
		}
	}
}
