//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

type continuationHTTPIdentityTrace struct {
	mu              sync.Mutex
	historyQueries  int
	historyUsers    []string
	profileReads    int
	preferenceReads int
}

func (trace *continuationHTTPIdentityTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if strings.Contains(data.SQL, "SELECT DISTINCT user_id FROM sessions") {
		trace.historyQueries++
		if ids, ok := data.Args[0].([]string); ok {
			trace.historyUsers = append(trace.historyUsers, ids...)
		}
	}
	if strings.Contains(data.SQL, "SELECT profile_pin_ciphertext,configuration FROM users") {
		trace.profileReads++
	}
	if strings.Contains(data.SQL, "SELECT configuration,configuration_revision FROM users") {
		trace.preferenceReads++
	}
	return ctx
}

func (*continuationHTTPIdentityTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *continuationHTTPIdentityTrace) reset() {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	trace.historyQueries, trace.profileReads, trace.preferenceReads = 0, 0, 0
	trace.historyUsers = nil
}

func traceContinuationHTTPIdentity(t *testing.T, f *serverFixture, masterPath string) *continuationHTTPIdentityTrace {
	t.Helper()
	trace := new(continuationHTTPIdentityTrace)
	configuration := f.pool.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(f.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.app.identity = identity.New(pool)
	if masterPath != "" {
		f.app.identity = identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(masterPath))
	}
	return trace
}

func TestHTTPPublicUsersBatchHistoricalDeviceVisibility(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	for _, fixture := range []struct {
		id, policy, kind, device string
		disabled, administrator  bool
	}{
		{id: "history-known", policy: `{"IsHiddenFromUnusedDevices":true}`, kind: "emby", device: "known-device"},
		{id: "history-unused", policy: `{"IsHiddenFromUnusedDevices":true}`},
		{id: "history-admin-kind", policy: `{"IsHiddenFromUnusedDevices":true}`, kind: "admin", device: "known-device"},
		{id: "history-other-device", policy: `{"IsHiddenFromUnusedDevices":true}`, kind: "emby", device: "other-device"},
		{id: "history-hidden", policy: `{"IsHiddenFromUnusedDevices":true,"IsHidden":true}`, kind: "emby", device: "known-device"},
		{id: "history-disabled", policy: `{"IsHiddenFromUnusedDevices":true}`, kind: "emby", device: "known-device", disabled: true},
		{id: "history-administrator", policy: `{"IsHiddenFromUnusedDevices":true}`, kind: "emby", device: "known-device", administrator: true},
		{id: "history-remote-hidden", policy: `{"IsHiddenFromUnusedDevices":true,"IsHiddenRemotely":true}`, kind: "emby", device: "known-device"},
		{id: "history-local-only", policy: `{"IsHiddenFromUnusedDevices":true,"EnableRemoteAccess":false}`, kind: "emby", device: "known-device"},
		{id: "history-device-allowed", policy: `{"IsHiddenFromUnusedDevices":true,"EnableAllDevices":false,"EnabledDevices":["known-device"]}`, kind: "emby", device: "known-device"},
		{id: "history-device-denied", policy: `{"IsHiddenFromUnusedDevices":true,"EnableAllDevices":false,"EnabledDevices":["other-device"]}`, kind: "emby", device: "known-device"},
		{id: "history-malformed", policy: `{"IsHiddenFromUnusedDevices":true,"EnableRemoteAccess":"invalid"}`, kind: "emby", device: "known-device"},
		{id: "open-user", policy: `{}`},
	} {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO users(id,name,normalized_name,password_hash,policy,is_disabled,is_administrator)
			VALUES($1,$1,$1,'fixture',$2::jsonb,$3,$4)`, fixture.id, fixture.policy, fixture.disabled, fixture.administrator); err != nil {
			t.Fatal(err)
		}
		if fixture.kind != "" {
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,device_id,created_at,expires_at,revoked_at)
				VALUES($1,$1,decode(md5($1)||md5($1),'hex'),$2,$3,clock_timestamp()-interval '2 days',
				clock_timestamp()-interval '1 day',clock_timestamp())`, fixture.id, fixture.kind, fixture.device); err != nil {
				t.Fatal(err)
			}
		}
	}
	users, err := f.users.ListUsers(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	trace := traceContinuationHTTPIdentity(t, f, "")
	for _, scenario := range []struct {
		name, device string
		remote       bool
		queries      int
	}{
		{name: "local known", device: "known-device", queries: 1},
		{name: "remote known", device: "known-device", remote: true, queries: 1},
		{name: "another device", device: "other-device", queries: 1},
		{name: "missing device"},
		{name: "invalid device", device: " invalid-device "},
		{name: "oversized device", device: strings.Repeat("x", 257)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			want := []string{}
			for _, user := range users {
				used, err := f.users.UserHasUsedDevice(f.ctx, user.ID, scenario.device)
				if err != nil {
					t.Fatal(err)
				}
				if identity.PublicAvatarVisible(user, scenario.remote, scenario.device, used) {
					want = append(want, user.ID)
				}
			}
			trace.reset()
			query := url.Values{"X-Emby-Device-Id": {scenario.device}}
			request := httptest.NewRequest(http.MethodGet, "/emby/Users/Public?"+query.Encode(), nil).WithContext(f.ctx)
			request.RemoteAddr = "192.168.1.20:4321"
			if scenario.remote {
				request.RemoteAddr = "192.0.2.20:4321"
			}
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, request)
			items := clientSessionHTTPArray(t, response)
			got := make([]string, 0, len(items))
			for _, item := range items {
				got = append(got, stringValue(t, item, "Id"))
			}
			if !reflect.DeepEqual(got, want) || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("public account visibility/order changed: got %v, want %v", got, want)
			}
			trace.mu.Lock()
			defer trace.mu.Unlock()
			if trace.historyQueries != scenario.queries {
				t.Fatalf("public picker issued %d historical queries, want %d", trace.historyQueries, scenario.queries)
			}
			for _, id := range trace.historyUsers {
				if id == "history-hidden" || id == "history-disabled" || id == "history-administrator" || id == "history-malformed" || id == "open-user" ||
					scenario.remote && (id == "history-remote-hidden" || id == "history-local-only") {
					t.Fatalf("irrelevant account %s entered the historical-device batch", id)
				}
			}
		})
	}
}

func TestHTTPOwnConfigurationCombinesReadWithoutChangingWireOrProfileGates(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	masterPath := filepath.Join(t.TempDir(), "profile-master.key")
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(masterPath))
	f.app.identity = f.users
	path := "/emby/Users/" + accounts.viewer.userID + "/Configuration"
	expectStatus(t, f.request(t, http.MethodPost, path+"/Partial", map[string]any{"ProfilePin": "4826", "SubtitleMode": "Always"}, accounts.viewer.headers), http.StatusOK)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET configuration=configuration ||
		'{"AudioLanguagePreference":"","SubtitleLanguagePreference":"","UnknownPrivateField":"not-on-the-wire"}'::jsonb WHERE id=$1`, accounts.viewer.userID); err != nil {
		t.Fatal(err)
	}
	trace := traceContinuationHTTPIdentity(t, f, masterPath)
	response := f.request(t, http.MethodGet, path, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	configuration := jsonObject(t, response)
	if configuration["ProfilePin"] != "4826" || configuration["SubtitleMode"] != "Always" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("combined Configuration response lost its private owner projection")
	}
	for _, field := range []string{"AudioLanguagePreference", "SubtitleLanguagePreference", "UnknownPrivateField"} {
		if _, exists := configuration[field]; exists {
			t.Fatalf("owner configuration changed omitted-field behavior for %s", field)
		}
	}
	trace.mu.Lock()
	profileReads, preferenceReads := trace.profileReads, trace.preferenceReads
	trace.mu.Unlock()
	if profileReads != 1 || preferenceReads != 0 {
		t.Fatalf("owner Configuration read projections = %d private + %d ordinary, want 1 + 0", profileReads, preferenceReads)
	}
	trace.reset()
	response = f.request(t, http.MethodGet, path, nil, accounts.admin.headers)
	expectStatus(t, response, http.StatusOK)
	configuration = jsonObject(t, response)
	if _, exists := configuration["ProfilePin"]; exists || configuration["AudioLanguagePreference"] != "" || configuration["SubtitleLanguagePreference"] != "" {
		t.Fatal("another-user administrator response changed its public preference shape")
	}
	trace.mu.Lock()
	profileReads, preferenceReads = trace.profileReads, trace.preferenceReads
	trace.mu.Unlock()
	if profileReads != 0 || preferenceReads != 1 {
		t.Fatal("another-user administrator read accessed the private profile projection")
	}
	for _, policy := range []string{`{"EnableUserPreferenceAccess":false}`, `{"RestrictedFeatures":["goby_preferences"]}`} {
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", accounts.viewer.userID, policy); err != nil {
			t.Fatal(err)
		}
		expectStatus(t, f.request(t, http.MethodGet, path, nil, accounts.viewer.headers), http.StatusForbidden)
		response := f.request(t, http.MethodGet, "/emby/Users/Me", nil, accounts.viewer.headers)
		expectStatus(t, response, http.StatusOK)
		if objectValue(t, jsonObject(t, response), "Configuration")["ProfilePin"] != "4826" {
			t.Fatal("the endpoint preference gate changed Users/Me's existing profile gate")
		}
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy='{}',profile_pin_ciphertext=NULL WHERE id=$1", accounts.viewer.userID); err != nil {
		t.Fatal(err)
	}
	response = f.request(t, http.MethodGet, path, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if _, exists := jsonObject(t, response)["ProfilePin"]; exists {
		t.Fatal("an absent PIN became an empty private field")
	}
}
