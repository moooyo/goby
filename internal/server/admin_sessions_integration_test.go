//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func adminSessionHTTPCSRF(t *testing.T, f *serverFixture, cookie *http.Cookie) string {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	return stringValue(t, jsonObject(t, response), "CSRFToken")
}

func adminSessionHTTPList(t *testing.T, f *serverFixture, cookie *http.Cookie, query string) ([]map[string]any, map[string]any) {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/sessions"+query, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	if len(page) != 4 {
		t.Fatal("managed session list changed its four-field envelope")
	}
	values, ok := page["Items"].([]any)
	if !ok {
		t.Fatal("managed session list must contain a non-null Items array")
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok || len(item) != 16 {
			t.Fatal("managed session item changed its sixteen-field whitelist")
		}
		items = append(items, item)
	}
	return items, page
}

func adminSessionHTTPRevoke(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf, id string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPost, "/admin/v1/sessions/"+url.PathEscape(id)+"/revoke", map[string]any{}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
}

func adminSessionHTTPCurrentID(t *testing.T, f *serverFixture, cookie *http.Cookie) string {
	t.Helper()
	items, _ := adminSessionHTTPList(t, f, cookie, "?Kind=admin")
	current := ""
	for _, item := range items {
		if item["IsCurrent"] == true {
			if current != "" {
				t.Fatal("session list marked more than one current login")
			}
			current = stringValue(t, item, "Id")
		}
	}
	if current == "" {
		t.Fatal("session list omitted the current administrator login")
	}
	return current
}

func TestHTTPAdminSessionsCookieCSRFAndEmbyIsolation(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	csrf := adminSessionHTTPCSRF(t, f, accounts.cookie)
	path := "/admin/v1/sessions/" + accounts.viewer.id + "/revoke"
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/admin/v1/sessions", nil}, {http.MethodPost, path, map[string]any{}},
	} {
		expectAPIError(t, f.request(t, route.method, route.path, route.body, nil), http.StatusUnauthorized, "authentication_required", false)
		for _, login := range []clientSessionHTTPLogin{accounts.admin, accounts.viewer} {
			expectAPIError(t, f.request(t, route.method, route.path, route.body, login.headers), http.StatusUnauthorized, "authentication_required", false)
			expectAPIError(t, f.request(t, route.method, route.path, route.body, nil, &http.Cookie{Name: sessionCookie, Value: login.headers.Get("X-Emby-Token")}), http.StatusUnauthorized, "invalid_credentials", false)
		}
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/sessions?api_key="+accounts.admin.headers.Get("X-Emby-Token"), nil, nil), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{}, nil, accounts.cookie), http.StatusForbidden, "csrf_invalid", false)
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{}, http.Header{"X-CSRF-Token": {"wrong-csrf"}}, accounts.cookie), http.StatusForbidden, "csrf_invalid", false)
	expectAPIError(t, f.request(t, http.MethodPost, path, map[string]any{}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://foreign.example.test"}}, accounts.cookie), http.StatusForbidden, "origin_denied", false)
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Sessions", nil, nil, accounts.cookie), http.StatusUnauthorized, "authentication_required", true)
	items, page := adminSessionHTTPList(t, f, accounts.cookie, "")
	if len(items) != 5 || page["TotalRecordCount"] != float64(5) || page["StartIndex"] != float64(0) || page["Limit"] != float64(50) {
		t.Fatal("default session list lost the active login set or pagination defaults")
	}
	for _, route := range []string{"/admin/v1/capabilities", "/admin/v1/overview"} {
		response := f.request(t, http.MethodGet, route, nil, nil, accounts.cookie)
		expectStatus(t, response, http.StatusOK)
		if objectValue(t, jsonObject(t, response), "Features")["SessionManagement"] != true {
			t.Fatal("administrator feature discovery omitted session management")
		}
	}
	// Rejected writes must leave the exact target credential usable.
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID, nil, accounts.viewer.headers), http.StatusOK)
}

func TestHTTPAdminSessionsStatusesFiltersPaginationAndPrivateProjection(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	currentID := adminSessionHTTPCurrentID(t, f, accounts.cookie)
	expired := loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", "expired-device")
	revoked := loginClientSessionHTTP(t, f, "Session Other", "session-viewer-password", "revoked-device")
	demoted, err := f.users.CreateUser(f.ctx, "Former Administrator", "former-admin-password", true)
	if err != nil {
		t.Fatal(err)
	}
	demotedCookie, _ := managedHTTPLogin(t, f, demoted.Name, "former-admin-password")
	demotedID := adminSessionHTTPCurrentID(t, f, demotedCookie)
	demotedEmby := loginClientSessionHTTP(t, f, demoted.Name, "former-admin-password", "former-admin-emby")
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{"UPDATE sessions SET created_at = now() - interval '60 days', last_seen_at = now() - interval '59 days', client_capabilities = '{\"PushToken\":\"private-capability-marker\"}'::jsonb", nil},
		{"UPDATE sessions SET expires_at = now() - interval '1 day' WHERE id = $1", []any{expired.id}},
		{"UPDATE users SET is_disabled = true WHERE id = $1", []any{accounts.other.userID}},
		{"UPDATE sessions SET expires_at = now() - interval '1 day' WHERE user_id = $1", []any{accounts.other.userID}},
		{"UPDATE sessions SET revoked_at = now() - interval '2 days' WHERE id = $1", []any{revoked.id}},
		{"UPDATE users SET is_administrator = false WHERE id = $1", []any{demoted.ID}},
		{"UPDATE sessions SET client_name = 'Caf\u00e9 %_ Player' WHERE id = $1", []any{accounts.viewer.id}},
	} {
		if _, err := f.pool.Exec(f.ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	statuses := map[string]string{currentID: "active", accounts.admin.id: "active", accounts.viewer.id: "active", accounts.second.id: "active", demotedEmby.id: "active",
		expired.id: "expired", revoked.id: "revoked", accounts.other.id: "disabled", demotedID: "disabled"}
	all, page := adminSessionHTTPList(t, f, accounts.cookie, "?Status=all&Limit=200")
	if page["TotalRecordCount"] != float64(len(statuses)) || len(all) != len(statuses) {
		t.Fatal("all-status list omitted historical authentication sessions")
	}
	var sortedIDs []string
	for id := range statuses {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(sortedIDs)))
	for index, item := range all {
		id := stringValue(t, item, "Id")
		if id != sortedIDs[index] || item["Status"] != statuses[id] || item["IsCurrent"] != (id == currentID) {
			t.Fatal("session history lost status precedence, current identity, or deterministic creation/ID ordering")
		}
		if id == demotedID && (item["UserIsDisabled"] != false || item["UserIsAdministrator"] != false) {
			t.Fatal("demoted native login hid the reason for its disabled status")
		}
		if id == revoked.id && item["UserIsDisabled"] != true {
			t.Fatal("revoked status did not preserve current disabled account facts")
		}
		for _, field := range []string{"CreatedAt", "LastSeenAt", "ExpiresAt", "RevokedAt"} {
			if field == "RevokedAt" && id != revoked.id {
				if item[field] != nil {
					t.Fatal("unrevoked history must contain an explicit null timestamp")
				}
				continue
			}
			stamp := stringValue(t, item, field)
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil || !strings.HasSuffix(stamp, "Z") {
				t.Fatal("session timestamp was not UTC RFC3339")
			}
		}
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{accounts.cookie.Value, accounts.admin.headers.Get("X-Emby-Token"), accounts.viewer.headers.Get("X-Emby-Token"), "private-capability-marker", "TokenHash", "token_hash", "Capabilities", "Policy", "Password"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("administrator session projection exposed credentials or private account/client state")
		}
	}
	for _, status := range []string{"active", "revoked", "disabled", "expired"} {
		items, page := adminSessionHTTPList(t, f, accounts.cookie, "?Status="+status)
		var want []string
		for id, state := range statuses {
			if state == status {
				want = append(want, id)
			}
		}
		assertClientSessionHTTPIDs(t, items, want...)
		if page["TotalRecordCount"] != float64(len(want)) {
			t.Fatal("status-filtered count differs from its unpaginated result")
		}
	}
	for _, test := range []struct {
		query string
		ids   []string
	}{
		{"?Status=all&UserId=" + accounts.viewer.userID, []string{accounts.viewer.id, accounts.second.id, expired.id}},
		{"?Status=all&Kind=admin", []string{currentID, demotedID}},
		{"?DeviceId=" + accounts.second.deviceID, []string{accounts.second.id}},
		{"?UserId=" + accounts.viewer.userID + "&Kind=emby&DeviceId=" + accounts.second.deviceID, []string{accounts.second.id}},
		{"?Status=all&SearchTerm=%25_", []string{accounts.viewer.id}},
		{"?SearchTerm=" + url.QueryEscape("caf\u00e9"), []string{accounts.viewer.id}},
		{"?SearchTerm=" + url.QueryEscape("Former Administrator"), []string{demotedEmby.id}},
		{"?DeviceId=missing", nil}, {"?UserId=missing", nil}, {"?SearchTerm=missing", nil},
	} {
		items, page := adminSessionHTTPList(t, f, accounts.cookie, test.query)
		assertClientSessionHTTPIDs(t, items, test.ids...)
		if page["TotalRecordCount"] != float64(len(test.ids)) {
			t.Fatal("combined session filter count is incorrect")
		}
	}
	first, _ := adminSessionHTTPList(t, f, accounts.cookie, "?Status=all&StartIndex=0&Limit=2")
	if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET last_seen_at = now() WHERE id = $1", sortedIDs[len(sortedIDs)-1]); err != nil {
		t.Fatal(err)
	}
	second, secondPage := adminSessionHTTPList(t, f, accounts.cookie, "?Status=all&StartIndex=2&Limit=2")
	if first[0]["Id"] != sortedIDs[0] || first[1]["Id"] != sortedIDs[1] || second[0]["Id"] != sortedIDs[2] || second[1]["Id"] != sortedIDs[3] || secondPage["StartIndex"] != float64(2) || secondPage["Limit"] != float64(2) || secondPage["TotalRecordCount"] != float64(len(statuses)) {
		t.Fatal("activity updates moved a session across the deterministic page boundary")
	}
	empty, emptyPage := adminSessionHTTPList(t, f, accounts.cookie, "?Status=all&StartIndex=2147483647&Limit=1")
	if len(empty) != 0 || emptyPage["TotalRecordCount"] != float64(len(statuses)) {
		t.Fatal("out-of-range page discarded the filtered total or returned null Items")
	}
}

func TestHTTPAdminSessionsStrictValidationAndIdempotentSingleRevocation(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	csrf := adminSessionHTTPCSRF(t, f, accounts.cookie)
	before := managedHTTPDetail(t, f, accounts.cookie, accounts.viewer.userID)
	for _, test := range []struct{ query, field string }{
		{"?Kind=Admin", "Kind"}, {"?Status=online", "Status"}, {"?Status=ACTIVE", "Status"},
		{"?UserId=%20user", "UserId"}, {"?UserId=a%00b", "UserId"}, {"?DeviceId=a%00b", "DeviceId"}, {"?SearchTerm=a%00b", "SearchTerm"},
		{"?UserId=" + strings.Repeat("a", 257), "UserId"}, {"?DeviceId=" + strings.Repeat("a", 257), "DeviceId"}, {"?SearchTerm=" + strings.Repeat("a", 257), "SearchTerm"},
		{"?Limit=01", "Limit"}, {"?Status=active&Status=active", "Status"}, {"?SearchTerm=%ff", "SearchTerm"}, {"?api_key=private-marker", "Query"},
	} {
		expectManagedFieldError(t, f.request(t, http.MethodGet, "/admin/v1/sessions"+test.query, nil, nil, accounts.cookie), test.field)
	}
	path := "/admin/v1/sessions/" + accounts.viewer.id + "/revoke"
	for _, body := range []string{"", "null", "[]", "{} {}", "{\"Id\":null}", "{\"Id\":1,\"\\u0049d\":2}", "{\"\xff\":0}", strings.Repeat(" ", 4095) + "{}"} {
		expectManagedFieldError(t, adminMetadataHTTPRaw(t, f, http.MethodPost, path, body, http.Header{"Content-Type": {"application/json"}, "X-CSRF-Token": {csrf}}, accounts.cookie), "Body")
	}
	expectManagedFieldError(t, f.request(t, http.MethodPost, path+"?UserId="+accounts.other.userID, map[string]any{}, http.Header{"X-CSRF-Token": {csrf}}, accounts.cookie), "Query")
	for _, id := range []string{" leading", "trailing ", "a\x00b", "a\xffb", strings.Repeat("a", 257)} {
		expectManagedFieldError(t, adminSessionHTTPRevoke(t, f, accounts.cookie, csrf, id), "Id")
	}
	for _, id := range []string{"missing", "opaque:session id", strings.Repeat("\u00e9", 128)} {
		expectAPIError(t, adminSessionHTTPRevoke(t, f, accounts.cookie, csrf, id), http.StatusNotFound, "not_found", false)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID, nil, accounts.viewer.headers), http.StatusOK)
	// Whitespace counts toward the 4 KiB bound but a complete empty object at
	// the exact limit is valid, including an explicit UTF-8 media parameter.
	response := adminMetadataHTTPRaw(t, f, http.MethodPost, path, strings.Repeat(" ", 4094)+"{}", http.Header{"Content-Type": {"application/json; charset=utf-8"}, "X-CSRF-Token": {csrf}}, accounts.cookie)
	expectStatus(t, response, http.StatusOK)
	result := jsonObject(t, response)
	if len(result) != 5 || result["SessionId"] != accounts.viewer.id || result["UserId"] != accounts.viewer.userID || result["Kind"] != "emby" || result["CurrentSessionRevoked"] != false || len(response.Result().Cookies()) != 0 {
		t.Fatal("single-session revocation returned an invalid result or cleared the unrelated administrator cookie")
	}
	stamp := stringValue(t, result, "RevokedAt")
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil || !strings.HasSuffix(stamp, "Z") {
		t.Fatal("revocation timestamp is not UTC RFC3339")
	}
	again := adminSessionHTTPRevoke(t, f, accounts.cookie, csrf, accounts.viewer.id)
	expectStatus(t, again, http.StatusOK)
	if !reflect.DeepEqual(result, jsonObject(t, again)) {
		t.Fatal("repeated session revocation changed its persisted timestamp or outcome")
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+accounts.viewer.userID, nil, accounts.viewer.headers), http.StatusUnauthorized, "invalid_credentials", true)
	for _, peer := range []clientSessionHTTPLogin{accounts.second, accounts.other, accounts.admin} {
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+peer.userID, nil, peer.headers), http.StatusOK)
	}
	if !reflect.DeepEqual(before, managedHTTPDetail(t, f, accounts.cookie, accounts.viewer.userID)) {
		t.Fatal("session revocation changed account facts, policy, or management revision")
	}
	replacement := loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", accounts.viewer.deviceID)
	if replacement.id == accounts.viewer.id {
		t.Fatal("a revoked device login was resurrected instead of creating a fresh session")
	}
	history, _ := adminSessionHTTPList(t, f, accounts.cookie, "?Status=revoked&UserId="+accounts.viewer.userID)
	assertClientSessionHTTPIDs(t, history, accounts.viewer.id)
}

func TestHTTPAdminSessionsSelfRevocationClearsOnlyCurrentCookie(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	peer, peerCSRF := f.adminLogin(t)
	currentID := adminSessionHTTPCurrentID(t, f, cookie)
	peerID := adminSessionHTTPCurrentID(t, f, peer)
	response := adminSessionHTTPRevoke(t, f, cookie, csrf, currentID)
	expectStatus(t, response, http.StatusOK)
	expectManagedCookieCleared(t, response)
	for _, path := range []string{"/admin/v1/session", "/admin/v1/sessions"} {
		expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil, cookie), http.StatusUnauthorized, "invalid_credentials", false)
	}
	if adminSessionHTTPCurrentID(t, f, peer) != peerID {
		t.Fatal("self-revocation invalidated a second login of the same administrator")
	}
	again := adminSessionHTTPRevoke(t, f, peer, peerCSRF, currentID)
	expectStatus(t, again, http.StatusOK)
	if jsonObject(t, again)["CurrentSessionRevoked"] != false || len(again.Result().Cookies()) != 0 || jsonObject(t, again)["RevokedAt"] != jsonObject(t, response)["RevokedAt"] {
		t.Fatal("a peer administrator's idempotent retry cleared its own cookie or changed history")
	}
}

func TestHTTPAdminSessionsRevalidateStaleActorsAfterMiddleware(t *testing.T) {
	for _, operation := range []string{"list", "revoke", "missing"} {
		for _, authority := range []string{"revoked", "expired", "demoted", "disabled", "emby-kind"} {
			t.Run(operation+"/"+authority, func(t *testing.T) {
				f, accounts := newClientSessionHTTPAccounts(t)
				csrf := adminSessionHTTPCSRF(t, f, accounts.cookie)
				actorID := adminSessionHTTPCurrentID(t, f, accounts.cookie)
				handler, method, pattern, path := f.app.adminSessions, http.MethodGet, "GET /admin/v1/sessions", "/admin/v1/sessions"
				var body any
				if operation != "list" {
					handler, method, pattern, path = f.app.revokeAdminSession, http.MethodPost, "POST /admin/v1/sessions/{id}/revoke", "/admin/v1/sessions/"+accounts.viewer.id+"/revoke"
					body = map[string]any{}
					if operation == "missing" {
						path = "/admin/v1/sessions/missing-target/revoke"
					}
				}
				mux := http.NewServeMux()
				if authority == "emby-kind" {
					mux.HandleFunc(pattern, f.app.requireEmby(handler))
				} else {
					mux.HandleFunc(pattern, f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
						query, id := "UPDATE sessions SET revoked_at = now() WHERE id = $1", actorID
						switch authority {
						case "expired":
							query = "UPDATE sessions SET created_at = now() - interval '1 day', expires_at = now() - interval '1 second' WHERE id = $1"
						case "demoted":
							query, id = "UPDATE users SET is_administrator = false WHERE id = $1", accounts.admin.userID
						case "disabled":
							query, id = "UPDATE users SET is_disabled = true WHERE id = $1", accounts.admin.userID
						}
						if _, err := f.pool.Exec(f.ctx, query, id); err != nil {
							t.Fatal(err)
						}
						handler(w, r)
					}))
				}
				f.handler = f.app.middleware(mux)
				headers := http.Header{"X-CSRF-Token": {csrf}}
				if authority == "emby-kind" {
					headers = accounts.admin.headers
				}
				response := f.request(t, method, path, body, headers, accounts.cookie)
				expectAPIError(t, response, http.StatusUnauthorized, "invalid_credentials", false)
				if strings.Contains(response.Body.String(), accounts.viewer.id) || strings.Contains(response.Body.String(), "Session Viewer") {
					t.Fatal("stale actor received another session's private details")
				}
				var targetUnrevoked bool
				if err := f.pool.QueryRow(f.ctx, "SELECT revoked_at IS NULL FROM sessions WHERE id = $1", accounts.viewer.id).Scan(&targetUnrevoked); err != nil || !targetUnrevoked {
					t.Fatal("stale or wrong-kind actor revoked another authentication session")
				}
			})
		}
	}
}

func adminSessionHTTPSend(ctx context.Context, client *http.Client, baseURL string, cookie *http.Cookie, csrf, id string) (hlsHTTPResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/admin/v1/sessions/"+url.PathEscape(id)+"/revoke", strings.NewReader("{}"))
	if err != nil {
		return hlsHTTPResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	response, err := client.Do(request)
	if err != nil {
		return hlsHTTPResponse{}, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	return hlsHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: data}, err
}

func adminSessionHTTPSnapshot(t *testing.T, f *serverFixture) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'users', (SELECT COALESCE(jsonb_agg(jsonb_build_object('id', id, 'name', name,
			'revision', management_revision, 'policy', policy, 'admin', is_administrator,
			'disabled', is_disabled) ORDER BY id), '[]'::jsonb) FROM users),
		'metadata', (SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY item_id), '[]'::jsonb) FROM item_metadata_state m),
		'userdata', (SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id), '[]'::jsonb) FROM user_item_data d)
	)::text`).Scan(&state); err != nil {
		t.Fatalf("read session side-effect snapshot: %v", err)
	}
	return state
}

func adminSessionHTTPHasHLS(f *serverFixture, id string) bool {
	f.app.hls.mu.Lock()
	defer f.app.hls.mu.Unlock()
	_, present := f.app.hls.sessions[id]
	return present
}

func TestHTTPAdminSessionRevocationCommitsBeforeDisconnectingExactSocketAndHLSOwners(t *testing.T) {
	h := newHLSHTTPFixture(t)
	csrf := adminSessionHTTPCSRF(t, h.f, h.accounts.cookie)
	// Give all protected state real values so an unrelated reset is observable.
	if _, err := h.f.pool.Exec(h.f.ctx, `INSERT INTO user_item_data
		(user_id, item_id, playback_position_ticks, play_count, is_favorite, played)
		VALUES ($1, $2, 1234567, 3, true, false)`, h.accounts.viewer.userID, h.item.ID); err != nil {
		t.Fatal(err)
	}
	metadata := h.f.request(t, http.MethodGet, "/admin/v1/items/"+h.item.ID+"/metadata", nil, nil, h.accounts.cookie)
	expectStatus(t, metadata, http.StatusOK)
	edit := h.f.request(t, http.MethodPut, "/admin/v1/items/"+h.item.ID+"/metadata", map[string]any{
		"Revision": jsonObject(t, metadata)["Revision"], "Overrides": map[string]any{"Overview": "Preserved session-management metadata"}, "LockedFields": []string{"Overview"},
	}, http.Header{"X-CSRF-Token": {csrf}}, h.accounts.cookie)
	expectStatus(t, edit, http.StatusOK)
	target := h.graph(t, h.accounts.viewer, 0)
	peer := h.graph(t, h.accounts.second, 0)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, target.children[0], nil, nil), http.StatusOK)
	peerBytes := h.request(t, http.MethodGet, peer.children[0], nil, nil)
	expectHLSHTTPStatus(t, peerBytes, http.StatusOK)
	// An hour-long revalidation interval prevents periodic socket maintenance
	// from standing in for the handler's post-commit DisconnectSession call.
	h.f.app.sockets.revalidateEvery, h.f.app.sockets.pingEvery = time.Hour, time.Hour
	firstSocket := websocketHTTPDial(t, h.server, "/emby/socket", h.accounts.viewer.headers, http.StatusSwitchingProtocols)
	secondSocket := websocketHTTPDial(t, h.server, "/emby/socket", h.accounts.viewer.headers, http.StatusSwitchingProtocols)
	peerSocket := websocketHTTPDial(t, h.server, "/emby/socket", h.accounts.second.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, h.f, h.accounts.viewer.id, 2)
	websocketHTTPWaitCount(t, h.f, h.accounts.second.id, 1)
	before := adminSessionHTTPSnapshot(t, h.f)
	blocker, err := h.f.pool.Begin(h.f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	var lockedID string
	if err := blocker.QueryRow(h.f.ctx, "SELECT id FROM sessions WHERE id = $1 FOR UPDATE", h.accounts.viewer.id).Scan(&lockedID); err != nil {
		t.Fatal(err)
	}
	type pendingResult struct {
		response hlsHTTPResponse
		err      error
	}
	done := make(chan pendingResult, 1)
	requestCtx, cancelRequest := context.WithTimeout(h.f.ctx, 12*time.Second)
	defer cancelRequest()
	go func() {
		response, err := adminSessionHTTPSend(requestCtx, h.server.Client(), h.server.URL, h.accounts.cookie, csrf, h.accounts.viewer.id)
		done <- pendingResult{response, err}
	}()
	waitCtx, cancelWait := context.WithTimeout(h.f.ctx, 3*time.Second)
	defer cancelWait()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var waiting bool
		if err := h.f.pool.QueryRow(waitCtx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
			WHERE $1::integer = ANY(pg_blocking_pids(pid)) AND wait_event_type = 'Lock'
			AND position('SELECT id, user_id, kind FROM sessions' in query) > 0)`, blocker.Conn().PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatalf("observe pending session revocation lock: %v", err)
		}
		if waiting {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("session revocation returned before its database lock was released: status %d, error type %T", result.response.status, result.err)
		case <-waitCtx.Done():
			t.Fatal("session revocation did not reach the owned authentication row lock")
		case <-tick.C:
		}
	}
	var unrevoked bool
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT revoked_at IS NULL FROM sessions WHERE id = $1", h.accounts.viewer.id).Scan(&unrevoked); err != nil || !unrevoked {
		t.Fatal("blocked session revocation became visible before commit")
	}
	if !adminSessionHTTPHasHLS(h.f, target.hlsID) || !adminSessionHTTPHasHLS(h.f, peer.hlsID) {
		t.Fatal("pending revocation prematurely retired a conversion")
	}
	firstSocket.ping(t)
	secondSocket.ping(t)
	peerSocket.ping(t)
	if err := blocker.Commit(h.f.ctx); err != nil {
		t.Fatal(err)
	}
	var result pendingResult
	select {
	case result = <-done:
	case <-requestCtx.Done():
		t.Fatal("native session revocation did not finish after release of its database lock")
	}
	if result.err != nil {
		t.Fatalf("perform native session revocation (%T)", result.err)
	}
	expectHLSHTTPStatus(t, result.response, http.StatusOK)
	var outcome struct {
		SessionID             string `json:"SessionId"`
		RevokedAt             time.Time
		CurrentSessionRevoked bool
	}
	if err := json.Unmarshal(result.response.body, &outcome); err != nil || outcome.SessionID != h.accounts.viewer.id || outcome.CurrentSessionRevoked {
		t.Fatal("native runtime revocation returned the wrong session identity")
	}
	var stored time.Time
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT revoked_at FROM sessions WHERE id = $1", h.accounts.viewer.id).Scan(&stored); err != nil || !stored.Equal(outcome.RevokedAt) {
		t.Fatal("successful runtime revocation did not durably commit its returned timestamp")
	}
	if adminSessionHTTPHasHLS(h.f, target.hlsID) || !adminSessionHTTPHasHLS(h.f, peer.hlsID) {
		t.Fatal("successful revocation did not synchronously retire exactly its HLS login owner")
	}
	h.retired(t, target)
	firstSocket.closed(t)
	secondSocket.closed(t)
	websocketHTTPWaitCount(t, h.f, h.accounts.viewer.id, 0)
	peerSocket.ping(t)
	websocketHTTPWaitCount(t, h.f, h.accounts.second.id, 1)
	websocketHTTPDial(t, h.server, "/emby/socket", h.accounts.viewer.headers, http.StatusUnauthorized)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, target.children[0], nil, http.Header{"If-None-Match": {peerBytes.header.Get("ETag")}}), http.StatusUnauthorized)
	stillPlaying := h.request(t, http.MethodGet, peer.children[0], nil, nil)
	expectHLSHTTPStatus(t, stillPlaying, http.StatusOK)
	if !bytes.Equal(peerBytes.body, stillPlaying.body) {
		t.Fatal("single-session revocation changed a sibling login's cached media")
	}
	if adminSessionHTTPSnapshot(t, h.f) != before {
		t.Fatal("session revocation changed user data, metadata, account policy, or management revisions")
	}
}

func TestHTTPAdminSessionRevocationInterruptsBlockedOriginalAndPreservesSiblingLogin(t *testing.T) {
	f := newStreamHTTPFixture(t)
	path := originalRevocationSparseSource(t, f)
	csrf := adminSessionHTTPCSRF(t, f.f, f.cookie)
	principal, err := f.f.users.Resolve(f.f.ctx, f.token, "emby")
	if err != nil {
		t.Fatal(err)
	}
	peer := loginClientSessionHTTP(t, f.f, "Stream Viewer", "stream-viewer-password", "original-sibling-device")
	before := adminSessionHTTPSnapshot(t, f.f)
	targetStream := originalRevocationOpen(t, f, path, f.token)
	peerStream := originalRevocationOpen(t, f, path, peer.headers.Get("X-Emby-Token"))
	if len(f.f.app.streamSlots) != 2 {
		t.Fatal("original session fixtures were not simultaneously blocked on writes")
	}
	ctx, cancel := context.WithTimeout(f.f.ctx, 10*time.Second)
	defer cancel()
	result, err := adminSessionHTTPSend(ctx, f.client, f.server.URL, f.cookie, csrf, principal.SessionID)
	if err != nil {
		t.Fatalf("revoke blocked original login (%T)", err)
	}
	expectHLSHTTPStatus(t, result, http.StatusOK)
	// Neither large response body is consumed until the existing five-second
	// authorization watcher must interrupt only the revoked login's writer.
	originalRevocationWaitSlots(t, f, 1)
	originalRevocationAssertAborted(t, targetStream)
	originalRevocationAssertPeerActive(t, f, peerStream)
	expectStreamStatus(t, f.request(t, http.MethodHead, path, f.token, nil, nil), http.StatusUnauthorized)
	originalRevocationAssertRestored(t, f, path, peer.headers.Get("X-Emby-Token"))
	if adminSessionHTTPSnapshot(t, f.f) != before {
		t.Fatal("revoking an original-stream login changed unrelated persistent user or catalog state")
	}
	peerStream.close()
	originalRevocationWaitSlots(t, f, 0)
}
