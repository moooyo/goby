package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

type serverFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	users   *identity.Store
	cfg     config.Config
	log     *slog.Logger
	handler http.Handler
	app     *Server
}

// Every API integration test uses a newly created, uniquely named schema.
// Cleanup only removes that schema after CREATE SCHEMA has succeeded.
func newServerFixture(t *testing.T) *serverFixture {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create integration database connection")
	}
	t.Cleanup(adminPool.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := "goby_server_test_" + hex.EncodeToString(suffix[:])
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create isolated server schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("remove owned server schema: %v", err)
		}
	})
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse integration database configuration")
	}
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	poolConfig.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("create isolated server database pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate isolated server schema: %v", err)
	}
	webDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDirectory, "index.html"), []byte("<!doctype html><title>Dashboard fixture</title>"), 0o600); err != nil {
		t.Fatalf("write dashboard fixture: %v", err)
	}
	fixture := &serverFixture{
		ctx: ctx, pool: pool, users: identity.New(pool), log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg: config.Config{
			DatabaseURL: databaseURL, PublicURL: "https://goby.example.test", ServerName: "Integration Server",
			SetupToken: "integration-setup-token-with-32-bytes", CookieSecure: true, WebDirectory: webDirectory,
		},
	}
	api, err := New(ctx, fixture.cfg, pool, fixture.users, fixture.log, "integration-version")
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	fixture.handler = api.Handler()
	fixture.app = api
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := api.Close(closeCtx); err != nil {
			t.Errorf("close server workers: %v", err)
		}
	})
	return fixture
}

func (f *serverFixture) request(t *testing.T, method, target string, body any, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var content io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		content = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, content).WithContext(f.ctx)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func expectStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("HTTP status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
}

func jsonObject(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("response is not JSON: content type = %q, body = %s", response.Header().Get("Content-Type"), response.Body.String())
	}
	var object map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil || object == nil {
		t.Fatalf("decode response object: %v; body = %s", err, response.Body.String())
	}
	return object
}

func objectValue(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	if !ok {
		t.Fatalf("JSON field %s must be an object, got %#v", key, object[key])
	}
	return value
}

func stringValue(t *testing.T, object map[string]any, key string) string {
	t.Helper()
	value, ok := object[key].(string)
	if !ok || value == "" {
		t.Fatalf("JSON field %s must be a nonempty string, got %#v", key, object[key])
	}
	return value
}

func expectAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code string, emby bool) {
	t.Helper()
	if emby && status == http.StatusUnauthorized && (code == "authentication_required" || code == "invalid_credentials") {
		expectEmbyTextError(t, response, status, "Access token is invalid or expired.")
		return
	}
	expectStatus(t, response, status)
	object := jsonObject(t, response)
	field, codeField := "Error", "Code"
	if emby {
		field, codeField = "ResponseStatus", "ErrorCode"
	}
	if got := stringValue(t, objectValue(t, object, field), codeField); got != code {
		t.Errorf("API error code = %q, want %q", got, code)
	}
}

func expectEmbyTextError(t *testing.T, response *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	expectStatus(t, response, status)
	if got := response.Header().Get("Content-Type"); got != "text/plain" {
		t.Errorf("Emby authentication content type = %q, want text/plain", got)
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Errorf("Emby authentication content length = %q, want %d", got, len(body))
	}
	if got := response.Body.String(); got != body {
		t.Errorf("Emby authentication body = %q, want %q", got, body)
	}
}

func (f *serverFixture) bootstrap(t *testing.T) string {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/bootstrap", map[string]any{
		"SetupToken": f.cfg.SetupToken, "Name": "Administrator", "Password": "administrator-password",
	}, http.Header{"Origin": {f.cfg.PublicURL}})
	expectStatus(t, response, http.StatusCreated)
	return stringValue(t, objectValue(t, jsonObject(t, response), "User"), "Id")
}

func (f *serverFixture) adminLogin(t *testing.T) (*http.Cookie, string) {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{
		"Name": "Administrator", "Password": "administrator-password",
	}, http.Header{"Origin": {f.cfg.PublicURL}})
	expectStatus(t, response, http.StatusOK)
	object := jsonObject(t, response)
	csrf := stringValue(t, object, "CSRFToken")
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name != "goby_session" {
			continue
		}
		if cookie.Value == "" || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/admin" || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge <= 0 {
			t.Fatalf("administrator cookie has incorrect security or lifetime attributes: %+v", cookie)
		}
		if strings.Contains(response.Body.String(), cookie.Value) {
			t.Error("administrator response body must not expose the session token")
		}
		return cookie, csrf
	}
	t.Fatal("administrator login did not set a session cookie")
	return nil, ""
}

func (f *serverFixture) embyLogin(t *testing.T, name, password string) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
		"Username": name, "Pw": password,
	}, http.Header{"Authorization": {`Emby Client="Integration, Client", DeviceId="integration-device", Device="Linux", Version="1.0"`}})
	expectStatus(t, response, http.StatusOK)
	return jsonObject(t, response)
}

func TestHTTPBootstrapRequiresSetupTokenAndOnlyRunsOnce(t *testing.T) {
	f := newServerFixture(t)
	status := f.request(t, http.MethodGet, "/admin/v1/bootstrap", nil, nil)
	expectStatus(t, status, http.StatusOK)
	if jsonObject(t, status)["Initialized"] != false {
		t.Fatal("new server reports completed setup")
	}
	wrong := f.request(t, http.MethodPost, "/admin/v1/bootstrap", map[string]any{
		"SetupToken": "incorrect-setup-token", "Name": "Unwanted", "Password": "password",
	}, nil)
	expectAPIError(t, wrong, http.StatusForbidden, "setup_token_invalid", false)
	if initialized, err := f.users.Initialized(f.ctx); err != nil || initialized {
		t.Fatalf("incorrect setup token changed initialization state: %v, %v", initialized, err)
	}
	adminID := f.bootstrap(t)
	admin, err := f.users.GetUser(f.ctx, adminID)
	if err != nil || !admin.IsAdministrator || admin.Name != "Administrator" {
		t.Fatalf("HTTP bootstrap did not persist the administrator: %+v, %v", admin, err)
	}
	repeated := f.request(t, http.MethodPost, "/admin/v1/bootstrap", map[string]any{
		"SetupToken": f.cfg.SetupToken, "Name": "Second Admin", "Password": "another-password",
	}, nil)
	expectAPIError(t, repeated, http.StatusConflict, "already_initialized", false)
	users, err := f.users.ListUsers(f.ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("repeated setup changed users: count = %d, error = %v", len(users), err)
	}
	status = f.request(t, http.MethodGet, "/admin/v1/bootstrap", nil, nil)
	if jsonObject(t, status)["Initialized"] != true {
		t.Fatal("completed setup is not reported")
	}
}

func TestHTTPAdministratorCookieCSRFAndLogout(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	session := f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, cookie)
	expectStatus(t, session, http.StatusOK)
	if stringValue(t, jsonObject(t, session), "CSRFToken") != csrf {
		t.Error("session endpoint returned a different CSRF token")
	}
	newUser := map[string]any{"Name": "Viewer", "Password": "viewer-password", "IsAdministrator": false}
	for _, headers := range []http.Header{nil, {"X-CSRF-Token": {"incorrect-token"}}} {
		response := f.request(t, http.MethodPost, "/admin/v1/users", newUser, headers, cookie)
		expectAPIError(t, response, http.StatusForbidden, "csrf_invalid", false)
	}
	for _, headers := range []http.Header{
		{"Origin": {"https://other.example.test"}, "X-CSRF-Token": {csrf}},
		{"Sec-Fetch-Site": {"cross-site"}, "X-CSRF-Token": {csrf}},
	} {
		response := f.request(t, http.MethodPost, "/admin/v1/users", newUser, headers, cookie)
		expectAPIError(t, response, http.StatusForbidden, "origin_denied", false)
	}
	created := f.request(t, http.MethodPost, "/admin/v1/users", newUser, http.Header{
		"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf},
	}, cookie)
	expectStatus(t, created, http.StatusCreated)
	user := objectValue(t, jsonObject(t, created), "User")
	if user["Name"] != "Viewer" || user["IsAdministrator"] != false || user["HasPassword"] != true {
		t.Fatalf("created user has unexpected account metadata: %#v", user)
	}
	if _, err := f.users.GetUser(f.ctx, stringValue(t, user, "Id")); err != nil {
		t.Errorf("created user is not persisted: %v", err)
	}
	viewerLogin := f.request(t, http.MethodPost, "/admin/v1/session", map[string]any{
		"Name": "Viewer", "Password": "viewer-password",
	}, nil)
	expectAPIError(t, viewerLogin, http.StatusUnauthorized, "invalid_credentials", false)
	deniedLogout := f.request(t, http.MethodDelete, "/admin/v1/session", nil, nil, cookie)
	expectAPIError(t, deniedLogout, http.StatusForbidden, "csrf_invalid", false)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, cookie), http.StatusOK)
	logout := f.request(t, http.MethodDelete, "/admin/v1/session", nil, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, logout, http.StatusNoContent)
	deletedCookie := false
	for _, cleared := range logout.Result().Cookies() {
		if cleared.Name == cookie.Name && cleared.Path == cookie.Path && cleared.Value == "" && cleared.MaxAge < 0 {
			deletedCookie = true
		}
	}
	if !deletedCookie {
		t.Error("logout did not clear the administrator cookie")
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, cookie), http.StatusUnauthorized, "invalid_credentials", false)
	if _, err := f.users.Resolve(f.ctx, cookie.Value, "admin"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Errorf("logged-out administrator token remains active: %v", err)
	}
}

func TestHTTPEmbyAuthenticationShapeTokenSourcesAndRevocation(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	token := stringValue(t, login, "AccessToken")
	serverID := stringValue(t, login, "ServerId")
	user := objectValue(t, login, "User")
	session := objectValue(t, login, "SessionInfo")
	userID := stringValue(t, user, "Id")
	if user["Name"] != "Administrator" || user["ServerId"] != serverID || user["HasPassword"] != true || user["HasConfiguredPassword"] != true {
		t.Fatalf("Emby user object has unexpected fields: %#v", user)
	}
	if objectValue(t, user, "Policy")["IsAdministrator"] != true {
		t.Error("Emby administrator policy is missing")
	}
	objectValue(t, user, "Configuration")
	if session["UserId"] != userID || session["UserName"] != "Administrator" || session["ServerId"] != serverID || session["Client"] != "Integration, Client" || session["DeviceId"] != "integration-device" || session["DeviceName"] != "Linux" || session["ApplicationVersion"] != "1.0" {
		t.Fatalf("Emby session metadata does not match the client: %#v", session)
	}
	stringValue(t, session, "Id")
	for _, forbidden := range []string{"accessToken", "access_token", "user", "sessionInfo", "serverId"} {
		if _, exists := login[forbidden]; exists {
			t.Errorf("Emby response contains non-PascalCase field %q", forbidden)
		}
	}
	for name, headers := range map[string]http.Header{
		"token header":           {"X-Emby-Token": {token}},
		"authorization":          {"Authorization": {`Emby Token="` + token + `"`}},
		"emby authorization":     {"X-Emby-Authorization": {`Emby Token="` + token + `"`}},
		"matching token sources": {"Authorization": {`Emby Token="` + token + `"`}, "X-Emby-Token": {token}},
	} {
		t.Run(name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, "/emby/Users/"+userID, nil, headers)
			expectStatus(t, response, http.StatusOK)
			if jsonObject(t, response)["Id"] != userID {
				t.Error("token resolved a different user")
			}
		})
	}
	query := f.request(t, http.MethodGet, "/emby/Users/"+userID+"?api_key="+url.QueryEscape(token), nil, nil)
	expectStatus(t, query, http.StatusOK)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, nil), http.StatusUnauthorized, "authentication_required", true)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {strings.Repeat("A", 43)}}), http.StatusUnauthorized, "invalid_credentials", true)
	conflict := f.request(t, http.MethodGet, "/emby/System/Info?api_key=conflicting-token", nil, http.Header{"X-Emby-Token": {token}})
	expectAPIError(t, conflict, http.StatusUnauthorized, "authentication_required", true)
	wrongPassword := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
		"Username": "Administrator", "Pw": "wrong-password",
	}, http.Header{"Authorization": {`Emby Client="Integration", DeviceId="device"`}})
	expectEmbyTextError(t, wrongPassword, http.StatusUnauthorized, "Invalid username or password. Please try again.")
	logout := f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, http.Header{"X-Emby-Token": {token}})
	expectStatus(t, logout, http.StatusOK)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+userID, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized, "invalid_credentials", true)
}

func TestHTTPUserScopeAndSessionKindsCannotBeEscalated(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	viewer, err := f.users.CreateUser(f.ctx, "Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create scoped user: %v", err)
	}
	viewerLogin := f.embyLogin(t, viewer.Name, "viewer-password")
	viewerToken := stringValue(t, viewerLogin, "AccessToken")
	viewerHeaders := http.Header{"Authorization": {`Emby Token="` + viewerToken + `", UserId="` + adminID + `", IsAdministrator="true"`}}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID, nil, viewerHeaders), http.StatusOK)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+adminID, nil, viewerHeaders), http.StatusForbidden, "access_denied", true)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/Query", nil, viewerHeaders), http.StatusForbidden, "administrator_required", true)
	metadataOnly := http.Header{"Authorization": {`Emby Client="Dashboard", DeviceId="device", UserId="` + adminID + `", IsAdministrator="true"`}}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/Query", nil, metadataOnly), http.StatusUnauthorized, "authentication_required", true)
	adminCookie, _ := f.adminLogin(t)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {adminCookie.Value}}), http.StatusUnauthorized, "invalid_credentials", true)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/System/Info", nil, nil, adminCookie), http.StatusUnauthorized, "authentication_required", true)
	adminEmby := f.embyLogin(t, "Administrator", "administrator-password")
	adminEmbyToken := stringValue(t, adminEmby, "AccessToken")
	embyAsCookie := &http.Cookie{Name: "goby_session", Value: adminEmbyToken}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, embyAsCookie), http.StatusUnauthorized, "invalid_credentials", false)
	adminQuery := f.request(t, http.MethodGet, "/emby/Users/Query?StartIndex=0&Limit=1", nil, http.Header{"X-Emby-Token": {adminEmbyToken}})
	expectStatus(t, adminQuery, http.StatusOK)
	queryObject := jsonObject(t, adminQuery)
	items, ok := queryObject["Items"].([]any)
	if !ok || len(items) != 1 || queryObject["TotalRecordCount"] != float64(2) {
		t.Fatalf("administrator user query has incorrect pagination: %#v", queryObject)
	}
}

func TestHTTPPublicServerIDPersistsAndUnknownAdminAPIReturnsJSON(t *testing.T) {
	f := newServerFixture(t)
	before := f.request(t, http.MethodGet, "/emby/System/Info/Public", nil, nil)
	expectStatus(t, before, http.StatusOK)
	public := jsonObject(t, before)
	serverID := stringValue(t, public, "Id")
	if public["ServerName"] != f.cfg.ServerName || public["ProductName"] != "Goby" || public["LocalAddress"] != f.cfg.PublicURL || public["StartupWizardCompleted"] != false {
		t.Fatalf("public server metadata is incorrect: %#v", public)
	}
	f.bootstrap(t)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatalf("close original server before recreation: %v", err)
	}
	recreated, err := New(f.ctx, f.cfg, f.pool, identity.New(f.pool), f.log, "integration-version")
	if err != nil {
		t.Fatalf("recreate API server: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := recreated.Close(closeCtx); err != nil {
			t.Errorf("close recreated server: %v", err)
		}
	})
	f.handler = recreated.Handler()
	after := f.request(t, http.MethodGet, "/emby/System/Info/Public", nil, nil)
	expectStatus(t, after, http.StatusOK)
	public = jsonObject(t, after)
	if public["Id"] != serverID || public["StartupWizardCompleted"] != true {
		t.Fatalf("server recreation changed public identity or lost setup: %#v", public)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		response := f.request(t, method, "/admin/v1/unknown-operation", nil, nil)
		expectAPIError(t, response, http.StatusNotFound, "not_found", false)
		if strings.Contains(response.Body.String(), "Dashboard fixture") {
			t.Error("unknown administrator API was handled by the dashboard SPA")
		}
	}
}
