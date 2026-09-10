//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func deviceHTTPLogin(t *testing.T, f *serverFixture, name, password, reportedID, reportedName, appName, version string) clientSessionHTTPLogin {
	t.Helper()
	response := f.request(t, http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{"Username": name, "Pw": password}, http.Header{
		"X-Emby-Client": {appName}, "X-Emby-Device-Id": {reportedID}, "X-Emby-Device-Name": {reportedName}, "X-Emby-Client-Version": {version},
	})
	expectStatus(t, response, http.StatusOK)
	object := jsonObject(t, response)
	session := objectValue(t, object, "SessionInfo")
	return clientSessionHTTPLogin{id: stringValue(t, session, "Id"), userID: stringValue(t, session, "UserId"), deviceID: reportedID, headers: http.Header{"X-Emby-Token": {stringValue(t, object, "AccessToken")}}}
}

func deviceHTTPUser(t *testing.T, f *serverFixture, name string) string {
	t.Helper()
	user, err := f.users.CreateUser(f.ctx, name, "device-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func deviceHTTPNoStore(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("device management response must not be cached")
	}
}

func deviceHTTPNativeList(t *testing.T, f *serverFixture, cookie *http.Cookie, query string) ([]map[string]any, map[string]any) {
	t.Helper()
	response := f.request(t, http.MethodGet, "/admin/v1/devices"+query, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	deviceHTTPNoStore(t, response)
	page := jsonObject(t, response)
	values, ok := page["Items"].([]any)
	if !ok || len(page) != 4 {
		t.Fatal("native devices must have the exact paged envelope with non-null Items")
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatal("native device list member must be an object")
		}
		deviceHTTPNativeDTO(t, item)
		items = append(items, item)
	}
	return items, page
}

func deviceHTTPNativeDTO(t *testing.T, item map[string]any) {
	t.Helper()
	fields := []string{"Id", "Revision", "ReportedDeviceId", "Name", "ReportedName", "CustomName", "AppName", "AppVersion", "LastUserId", "LastUserName", "CreatedAt", "LastSeenAt", "IpAddress", "ActiveLoginCount"}
	if len(item) != len(fields) {
		t.Fatal("native device changed its fourteen-field safe DTO")
	}
	for _, field := range fields {
		if _, present := item[field]; !present {
			t.Fatalf("native device omitted %s", field)
		}
	}
	for _, field := range []string{"Id", "Revision"} {
		value := stringValue(t, item, field)
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != value {
			t.Fatalf("native device %s must preserve canonical decimal precision", field)
		}
	}
	for _, field := range []string{"ReportedDeviceId", "Name"} {
		stringValue(t, item, field)
	}
	for _, field := range []string{"ReportedName", "AppName", "AppVersion", "IpAddress"} {
		if _, ok := item[field].(string); !ok {
			t.Fatalf("native device %s must be a string", field)
		}
	}
	for _, field := range []string{"CustomName", "LastUserId", "LastUserName"} {
		if item[field] != nil {
			if _, ok := item[field].(string); !ok {
				t.Fatalf("native device %s must be a string or explicit null", field)
			}
		}
	}
	for _, field := range []string{"CreatedAt", "LastSeenAt"} {
		expectApplicationKeyHTTPUTC(t, stringValue(t, item, field))
	}
	count, ok := item["ActiveLoginCount"].(float64)
	if !ok || count < 0 || count > 9007199254740991 || count != float64(int64(count)) {
		t.Fatal("native device login count is not a nonnegative safe integer")
	}
}

func deviceHTTPFind(t *testing.T, f *serverFixture, cookie *http.Cookie, reportedID string) map[string]any {
	t.Helper()
	items, _ := deviceHTTPNativeList(t, f, cookie, "?Limit=200")
	var found map[string]any
	for _, item := range items {
		if item["ReportedDeviceId"] == reportedID {
			if found != nil {
				t.Fatal("one reported device ID registered duplicate live generations")
			}
			found = item
		}
	}
	if found == nil {
		t.Fatal("native list omitted the expected registered device")
	}
	return found
}

func deviceHTTPOptions(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf string, device map[string]any, name string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPost, "/admin/v1/devices/"+stringValue(t, device, "Id")+"/options", map[string]any{"Revision": device["Revision"], "CustomName": name}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
}

func deviceHTTPDelete(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf string, device map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPost, "/admin/v1/devices/"+stringValue(t, device, "Id")+"/delete", map[string]any{"Revision": device["Revision"]}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
}

func TestHTTPAdminDevicesCookieCSRFAndStrictMutationBoundaries(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	deviceHTTPUser(t, a.serverFixture, "Device Viewer")
	viewer := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "device-boundary", "Boundary Device", "Boundary App", "1.0")
	admin := deviceHTTPLogin(t, a.serverFixture, "Administrator", "administrator-password", "device-admin", "Admin Device", "Admin App", "1.0")
	key := a.create(t, "Device Boundary Key")
	device := deviceHTTPFind(t, a.serverFixture, a.cookie, viewer.deviceID)
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/admin/v1/devices", nil},
		{http.MethodPost, "/admin/v1/devices/" + device["Id"].(string) + "/options", map[string]any{"Revision": device["Revision"], "CustomName": "Denied"}},
		{http.MethodPost, "/admin/v1/devices/" + device["Id"].(string) + "/delete", map[string]any{"Revision": device["Revision"]}},
	} {
		expectAPIError(t, a.request(t, route.method, route.path, route.body, nil), http.StatusUnauthorized, "authentication_required", false)
		for _, login := range []clientSessionHTTPLogin{admin, viewer, {headers: http.Header{"X-Emby-Token": {key.token}}}} {
			expectAPIError(t, a.request(t, route.method, route.path, route.body, login.headers), http.StatusUnauthorized, "authentication_required", false)
			expectAPIError(t, a.request(t, route.method, route.path, route.body, nil, &http.Cookie{Name: sessionCookie, Value: login.headers.Get("X-Emby-Token")}), http.StatusUnauthorized, "invalid_credentials", false)
		}
		if route.method == http.MethodPost {
			expectAPIError(t, a.request(t, route.method, route.path, route.body, nil, a.cookie), http.StatusForbidden, "csrf_invalid", false)
			expectAPIError(t, a.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {"wrong"}}, a.cookie), http.StatusForbidden, "csrf_invalid", false)
			expectAPIError(t, a.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {a.csrf}, "Origin": {"https://foreign.example.test"}}, a.cookie), http.StatusForbidden, "origin_denied", false)
		}
	}
	for _, test := range []struct{ query, field string }{
		{"SearchTerm=a&SearchTerm=a", "SearchTerm"}, {"SearchTerm=%ff", "SearchTerm"}, {"SearchTerm=a%00b", "SearchTerm"}, {"SearchTerm=a%0Ab", "SearchTerm"},
		{"SearchTerm=" + strings.Repeat("a", 257), "SearchTerm"}, {"SearchTerm=%zz", "Query"}, {"SortOrder=Ascending", "Query"}, {"api_key=private-marker", "Query"}, {"Limit=201", "Limit"}, {"Limit=01", "Limit"}, {"StartIndex=2147483648", "StartIndex"},
	} {
		expectManagedFieldError(t, a.request(t, http.MethodGet, "/admin/v1/devices?"+test.query, nil, nil, a.cookie), test.field)
	}
	for _, action := range []string{"options", "delete"} {
		path := "/admin/v1/devices/" + device["Id"].(string) + "/" + action
		for _, body := range []string{"", "null", "[]", "{}{}", `{"Revision":"1","Revision":"2"}`, `{"Revision":"1","\u0052evision":"2"}`, `{"Revision":null}`, `{"Revision":"1","Token":"private-marker"}`, "{\"Revision\":\"1\",\"\xff\":0}", strings.Repeat(" ", 4097)} {
			expectAPIError(t, adminMetadataHTTPRaw(t, a.serverFixture, http.MethodPost, path, body, http.Header{"X-CSRF-Token": {a.csrf}, "Content-Type": {"application/json"}}, a.cookie), http.StatusBadRequest, "invalid_input", false)
		}
		body := map[string]any{"Revision": device["Revision"]}
		if action == "options" {
			body["CustomName"] = "Room"
		}
		expectManagedFieldError(t, a.request(t, http.MethodPost, path+"?Id=2", body, http.Header{"X-CSRF-Token": {a.csrf}}, a.cookie), "Query")
		expectAPIError(t, adminMetadataHTTPRaw(t, a.serverFixture, http.MethodPost, path, `{}`, http.Header{"X-CSRF-Token": {a.csrf}, "Content-Type": {"text/plain"}}, a.cookie), http.StatusUnsupportedMediaType, "unsupported_media_type", false)
		for _, id := range []string{"0", "01", "reported-device", "9223372036854775808"} {
			expectManagedFieldError(t, a.request(t, http.MethodPost, "/admin/v1/devices/"+id+"/"+action, body, http.Header{"X-CSRF-Token": {a.csrf}}, a.cookie), "Id")
		}
		for _, id := range []string{"1", "9223372036854775807"} {
			expectAPIError(t, a.request(t, http.MethodPost, "/admin/v1/devices/"+id+"/"+action, body, http.Header{"X-CSRF-Token": {a.csrf}}, a.cookie), http.StatusNotFound, "not_found", false)
		}
	}
	if !reflect.DeepEqual(device, deviceHTTPFind(t, a.serverFixture, a.cookie, viewer.deviceID)) {
		t.Fatal("rejected device requests changed registry state")
	}
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, viewer.headers), http.StatusOK)
}

func TestHTTPAdminDevicesGroupingSearchSnapshotPagingAndSafeDTO(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	items, page := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "")
	if len(items) != 0 || page["TotalRecordCount"] != float64(0) || page["StartIndex"] != float64(0) || page["Limit"] != float64(50) {
		t.Fatal("native dashboard login registered an ordinary Emby device or changed defaults")
	}
	key := a.create(t, "Excluded Application Device")
	expectStatus(t, a.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {key.token}}), http.StatusOK)
	items, _ = deviceHTTPNativeList(t, a.serverFixture, a.cookie, "")
	if len(items) != 0 {
		t.Fatal("application credential registered a native ordinary device")
	}
	deviceHTTPUser(t, a.serverFixture, "Device First User")
	lastUserID := deviceHTTPUser(t, a.serverFixture, "Device Latest User")
	first := deviceHTTPLogin(t, a.serverFixture, "Device First User", "device-viewer-password", "shared%_reported", "Original Room", "Initial App", "1.0")
	second := deviceHTTPLogin(t, a.serverFixture, "Device Latest User", "device-viewer-password", first.deviceID, "Latest Room", "Latest Device App", "2.0")
	device := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
	if device["ActiveLoginCount"] != float64(2) || device["LastUserId"] != lastUserID || device["LastUserName"] != "Device Latest User" || device["ReportedName"] != "Latest Room" || device["Name"] != "Latest Room" || device["AppName"] != "Latest Device App" || device["AppVersion"] != "2.0" || device["CustomName"] != nil || device["IpAddress"] != "192.0.2.1" {
		t.Fatal("shared reported device did not retain latest registration facts across users and applications")
	}
	// Place registry IDs on opposite sides of a decimal-width boundary. The
	// API must sort numeric identities, rather than their JSON string spelling.
	if _, err := a.pool.Exec(a.ctx, "SELECT setval(pg_get_serial_sequence('devices', 'id'), 9, false)"); err != nil {
		t.Fatal(err)
	}
	ninth := deviceHTTPLogin(t, a.serverFixture, "Device First User", "device-viewer-password", "device-nine", "Ninth Room", "Distinct App", "1.0")
	tenth := deviceHTTPLogin(t, a.serverFixture, "Device First User", "device-viewer-password", "device-ten", "", "Distinct App", "1.0")
	if _, err := a.pool.Exec(a.ctx, "UPDATE devices SET created_at = now() - interval '2 hours', last_seen_at = now() - interval '1 hour'"); err != nil {
		t.Fatal(err)
	}
	items, page = deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?Limit=200")
	if len(items) != 3 || page["TotalRecordCount"] != float64(3) || items[0]["ReportedDeviceId"] != tenth.deviceID || items[1]["ReportedDeviceId"] != ninth.deviceID || items[2]["ReportedDeviceId"] != first.deviceID {
		t.Fatal("registry pagination did not use one ordinary-device set ordered by activity and numeric ID")
	}
	firstPage, firstEnvelope := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?StartIndex=0&Limit=1")
	secondPage, secondEnvelope := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?StartIndex=1&Limit=1")
	if len(firstPage) != 1 || len(secondPage) != 1 || firstPage[0]["Id"] != items[0]["Id"] || secondPage[0]["Id"] != items[1]["Id"] || firstEnvelope["TotalRecordCount"] != float64(3) || secondEnvelope["TotalRecordCount"] != float64(3) || secondEnvelope["StartIndex"] != float64(1) || secondEnvelope["Limit"] != float64(1) {
		t.Fatal("paged device result changed its coherent filtered total or offset")
	}
	empty, envelope := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?StartIndex=2147483647&Limit=1")
	if len(empty) != 0 || envelope["TotalRecordCount"] != float64(3) {
		t.Fatal("empty device page lost its full filtered total")
	}
	for _, term := range []string{"%_", "latest room", "LATEST DEVICE APP", "Device Latest User", "shared%_reported"} {
		found, filtered := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?SearchTerm="+url.QueryEscape(term))
		if len(found) != 1 || found[0]["Id"] != device["Id"] || filtered["TotalRecordCount"] != float64(1) {
			t.Fatal("device search failed literal case-insensitive matching across public device facts")
		}
	}
	none, nonePage := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?SearchTerm=unmatched-device")
	if len(none) != 0 || nonePage["TotalRecordCount"] != float64(0) {
		t.Fatal("unmatched device search returned nonempty or null Items")
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{key.token, first.headers.Get("X-Emby-Token"), second.headers.Get("X-Emby-Token"), a.cookie.Value, "TokenHash", "token_hash", "secret_ciphertext", "Capabilities", "Policy"} {
		if strings.Contains(string(encoded), private) {
			t.Fatal("native device projection exposed credentials or unrelated identity state")
		}
	}
	if _, err := a.pool.Exec(a.ctx, "UPDATE sessions SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE id = $1", second.id); err != nil {
		t.Fatal(err)
	}
	if deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)["ActiveLoginCount"] != float64(1) {
		t.Fatal("device active-login count included expired credentials")
	}
	if err := a.users.Revoke(a.ctx, first.headers.Get("X-Emby-Token")); err != nil {
		t.Fatal(err)
	}
	if deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)["ActiveLoginCount"] != float64(0) {
		t.Fatal("device history disappeared or its active-login count included revoked credentials")
	}
}

func TestHTTPAdminDevicesRenameRevisionDeleteGenerationsAndCredentialIsolation(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	deviceHTTPUser(t, a.serverFixture, "Device Viewer")
	deviceHTTPUser(t, a.serverFixture, "Device Other")
	first := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "goby-dashboard", "First Report", "Viewer App", "1.0")
	second := deviceHTTPLogin(t, a.serverFixture, "Device Other", "device-viewer-password", first.deviceID, "Latest Report", "Other App", "2.0")
	peer := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "unrelated-generation", "Peer Room", "Peer App", "1.0")
	key := a.create(t, "Preserved Device Application")
	keyClient := identity.Client{Name: "Independent Application Context", DeviceID: first.deviceID, Device: "Application Context Name", Version: "3.0"}
	keyHeaders := http.Header{"X-Emby-Token": {key.token}, "Authorization": {`Emby Client="` + keyClient.Name + `", DeviceId="` + keyClient.DeviceID + `", Device="` + keyClient.Device + `", Version="` + keyClient.Version + `"`}}
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, keyHeaders), http.StatusOK)
	keyPrincipal, err := a.users.ResolveEmbyForClient(a.ctx, key.token, keyClient)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore := applicationContextSessionDTO(t, a.serverFixture, keyHeaders, keyPrincipal.ClientSessionID)
	before := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
	stateBefore := adminSessionHTTPSnapshot(t, a.serverFixture)
	updatedResponse := deviceHTTPOptions(t, a.serverFixture, a.cookie, a.csrf, before, "  Family Room  ")
	expectStatus(t, updatedResponse, http.StatusOK)
	deviceHTTPNoStore(t, updatedResponse)
	updated := jsonObject(t, updatedResponse)
	deviceHTTPNativeDTO(t, updated)
	oldRevision, _ := strconv.ParseInt(before["Revision"].(string), 10, 64)
	if updated["Revision"] != strconv.FormatInt(oldRevision+1, 10) || updated["Name"] != "Family Room" || updated["CustomName"] != "Family Room" || updated["ReportedName"] != "Latest Report" || updated["Id"] != before["Id"] {
		t.Fatal("native device rename did not retain reported identity and advance only its management revision")
	}
	if found, page := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?SearchTerm=family+room"); len(found) != 1 || found[0]["Id"] != updated["Id"] || page["TotalRecordCount"] != float64(1) {
		t.Fatal("native search did not include the effective custom device name")
	}
	unchanged := deviceHTTPOptions(t, a.serverFixture, a.cookie, a.csrf, updated, "Family Room")
	expectStatus(t, unchanged, http.StatusOK)
	if !reflect.DeepEqual(updated, jsonObject(t, unchanged)) {
		t.Fatal("unchanged custom device name advanced revision or changed activity")
	}
	expectAPIError(t, deviceHTTPOptions(t, a.serverFixture, a.cookie, a.csrf, before, "Stale Rename"), http.StatusConflict, "revision_conflict", false)
	expectAPIError(t, deviceHTTPDelete(t, a.serverFixture, a.cookie, a.csrf, before), http.StatusConflict, "revision_conflict", false)
	third := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", first.deviceID, "Newest Report", "Latest App", "4.0")
	if current := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID); current["Revision"] != updated["Revision"] || current["ReportedName"] != "Newest Report" || current["Name"] != "Family Room" {
		t.Fatal("ordinary login replaced an administrator override or advanced management revision")
	}
	if found, _ := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "?SearchTerm=newest+report"); len(found) != 1 || found[0]["Id"] != updated["Id"] {
		t.Fatal("native search lost the reported name underneath an active custom override")
	}
	for _, login := range []clientSessionHTTPLogin{first, second, third} {
		if clientSessionHTTPOne(t, a.serverFixture, login, login.id)["DeviceName"] != "Family Room" {
			t.Fatal("renaming a grouped device did not immediately refresh each ordinary session name")
		}
	}
	clearedResponse := deviceHTTPOptions(t, a.serverFixture, a.cookie, a.csrf, updated, "")
	expectStatus(t, clearedResponse, http.StatusOK)
	cleared := jsonObject(t, clearedResponse)
	if cleared["CustomName"] != nil || cleared["Name"] != "Newest Report" {
		t.Fatal("clearing native custom name did not restore the current reported name")
	}
	for _, test := range []struct {
		login clientSessionHTTPLogin
		name  string
	}{{first, "First Report"}, {second, "Latest Report"}, {third, "Newest Report"}} {
		if clientSessionHTTPOne(t, a.serverFixture, test.login, test.login.id)["DeviceName"] != test.name {
			t.Fatal("clearing a name did not immediately restore each ordinary session's own reported name")
		}
	}
	active := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
	if active["Id"] != before["Id"] || active["Revision"] != cleared["Revision"] || active["ActiveLoginCount"] != float64(3) || active["Name"] != "Newest Report" {
		t.Fatal("ordinary device activity advanced management revision or replaced its live generation")
	}
	var historicalBefore int
	if err := a.pool.QueryRow(a.ctx, "SELECT count(*) FROM sessions WHERE id = ANY($1::text[])", []string{first.id, second.id, third.id}).Scan(&historicalBefore); err != nil {
		t.Fatal(err)
	}
	deletedResponse := deviceHTTPDelete(t, a.serverFixture, a.cookie, a.csrf, active)
	expectStatus(t, deletedResponse, http.StatusOK)
	deviceHTTPNoStore(t, deletedResponse)
	deleted := jsonObject(t, deletedResponse)
	if len(deleted) != 3 || deleted["Id"] != active["Id"] || deleted["RevokedLoginCount"] != float64(3) || len(deletedResponse.Result().Cookies()) != 0 {
		t.Fatal("native device removal changed its response or retired the native administrator cookie")
	}
	expectApplicationKeyHTTPUTC(t, stringValue(t, deleted, "DeletedAt"))
	again := deviceHTTPDelete(t, a.serverFixture, a.cookie, a.csrf, before)
	expectStatus(t, again, http.StatusOK)
	if repeated := jsonObject(t, again); repeated["DeletedAt"] != deleted["DeletedAt"] || repeated["RevokedLoginCount"] != float64(0) {
		t.Fatal("repeated device deletion changed its original timestamp or revoked credentials twice")
	}
	expectAPIError(t, deviceHTTPOptions(t, a.serverFixture, a.cookie, a.csrf, active, "After Removal"), http.StatusNotFound, "not_found", false)
	for _, login := range []clientSessionHTTPLogin{first, second, third} {
		expectAPIError(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusUnauthorized, "invalid_credentials", true)
	}
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, peer.headers), http.StatusOK)
	expectStatus(t, a.request(t, http.MethodGet, "/admin/v1/session", nil, nil, a.cookie), http.StatusOK)
	keyAfter := applicationContextSessionDTO(t, a.serverFixture, keyHeaders, keyPrincipal.ClientSessionID)
	if keyAfter["Id"] != keyBefore["Id"] || keyAfter["DeviceName"] != keyBefore["DeviceName"] {
		t.Fatal("ordinary device rename/removal changed an application client context with the same reported identifier")
	}
	var historicalAfter, revokedCount int
	if err := a.pool.QueryRow(a.ctx, "SELECT count(*), count(*) FILTER (WHERE revoked_at IS NOT NULL) FROM sessions WHERE id = ANY($1::text[])", []string{first.id, second.id, third.id}).Scan(&historicalAfter, &revokedCount); err != nil || historicalAfter != historicalBefore || revokedCount != 3 {
		t.Fatal("device deletion removed credential history or left grouped logins active")
	}
	if adminSessionHTTPSnapshot(t, a.serverFixture) != stateBefore {
		t.Fatal("device management changed personal state, metadata, account policy, or user management revisions")
	}
	replacement := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", first.deviceID, "Returned Room", "Returned App", "5.0")
	live := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
	if live["Id"] == active["Id"] || live["Revision"] != "1" || live["CustomName"] != nil || live["Name"] != "Returned Room" || live["ActiveLoginCount"] != float64(1) || replacement.id == first.id {
		t.Fatal("relogin resurrected the removed device generation, name override, or credential")
	}
	for _, login := range []clientSessionHTTPLogin{first, second, third} {
		expectAPIError(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusUnauthorized, "invalid_credentials", true)
	}
}

func TestHTTPAdminDeviceDeleteRetiresGroupedSocketsAndHLSWithoutAffectingSiblingDevice(t *testing.T) {
	h := newHLSHTTPFixture(t)
	csrf := adminSessionHTTPCSRF(t, h.f, h.accounts.cookie)
	related := deviceHTTPLogin(t, h.f, "Session Other", "session-viewer-password", h.accounts.viewer.deviceID, "Shared HLS Device", "Related App", "1.0")
	target := h.graph(t, h.accounts.viewer, 0)
	peer := h.graph(t, h.accounts.second, 0)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, target.children[0], nil, nil), http.StatusOK)
	peerMedia := h.request(t, http.MethodGet, peer.children[0], nil, nil)
	expectHLSHTTPStatus(t, peerMedia, http.StatusOK)
	h.f.app.sockets.revalidateEvery, h.f.app.sockets.pingEvery = time.Hour, time.Hour
	targetSocket := websocketHTTPDial(t, h.server, "/emby/socket", h.accounts.viewer.headers, http.StatusSwitchingProtocols)
	relatedSocket := websocketHTTPDial(t, h.server, "/emby/socket", related.headers, http.StatusSwitchingProtocols)
	peerSocket := websocketHTTPDial(t, h.server, "/emby/socket", h.accounts.second.headers, http.StatusSwitchingProtocols)
	for _, id := range []string{h.accounts.viewer.id, related.id, h.accounts.second.id} {
		websocketHTTPWaitCount(t, h.f, id, 1)
	}
	device := deviceHTTPFind(t, h.f, h.accounts.cookie, h.accounts.viewer.deviceID)
	readPlaybackHistory := func() string {
		var history string
		if err := h.f.pool.QueryRow(h.f.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY id), '[]'::jsonb)::text
			FROM play_sessions p WHERE id = ANY($1::text[])`, []string{target.playID, peer.playID}).Scan(&history); err != nil {
			t.Fatal(err)
		}
		return history
	}
	playbackBefore := readPlaybackHistory()
	stateBefore := adminSessionHTTPSnapshot(t, h.f)
	response := deviceHTTPDelete(t, h.f, h.accounts.cookie, csrf, device)
	expectStatus(t, response, http.StatusOK)
	if jsonObject(t, response)["RevokedLoginCount"] != float64(2) {
		t.Fatal("device deletion did not commit revocation of every grouped ordinary login")
	}
	if adminSessionHTTPHasHLS(h.f, target.hlsID) || !adminSessionHTTPHasHLS(h.f, peer.hlsID) {
		t.Fatal("device deletion did not synchronously retire exactly its affected HLS owners")
	}
	if readPlaybackHistory() != playbackBefore || adminSessionHTTPSnapshot(t, h.f) != stateBefore {
		t.Fatal("device removal changed retained playback, personal data, catalog metadata, or account revision state")
	}
	h.retired(t, target)
	targetSocket.closed(t)
	relatedSocket.closed(t)
	peerSocket.ping(t)
	for _, id := range []string{h.accounts.viewer.id, related.id} {
		websocketHTTPWaitCount(t, h.f, id, 0)
	}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, target.children[0], nil, nil), http.StatusUnauthorized)
	remaining := h.request(t, http.MethodGet, peer.children[0], nil, nil)
	expectHLSHTTPStatus(t, remaining, http.StatusOK)
	if !bytes.Equal(peerMedia.body, remaining.body) {
		t.Fatal("device removal interrupted a sibling device's independent cached conversion")
	}
}

func TestHTTPAdminDevicesRevalidateRevokedActorAfterMiddleware(t *testing.T) {
	for _, action := range []string{"list", "options", "delete"} {
		t.Run(action, func(t *testing.T) {
			a := newApplicationKeyHTTPFixture(t)
			deviceHTTPUser(t, a.serverFixture, "Device Viewer")
			viewer := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "stale-actor-device", "Unchanged Room", "Unchanged App", "1.0")
			device := deviceHTTPFind(t, a.serverFixture, a.cookie, viewer.deviceID)
			original := a.handler
			method, pattern, path := http.MethodGet, "GET /admin/v1/devices", "/admin/v1/devices"
			handler := a.app.adminDevices
			var body any
			if action != "list" {
				method, pattern, path = http.MethodPost, "POST /admin/v1/devices/{id}/"+action, "/admin/v1/devices/"+device["Id"].(string)+"/"+action
				body = map[string]any{"Revision": device["Revision"]}
				if action == "options" {
					handler = a.app.updateAdminDeviceOptions
					body.(map[string]any)["CustomName"] = "Unauthorized Rename"
				} else {
					handler = a.app.deleteAdminDevice
				}
			}
			mux := http.NewServeMux()
			mux.HandleFunc(pattern, a.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
				if err := a.users.Revoke(a.ctx, a.cookie.Value); err != nil {
					t.Fatal(err)
				}
				handler(w, r)
			}))
			a.handler = a.app.middleware(mux)
			response := a.request(t, method, path, body, http.Header{"X-CSRF-Token": {a.csrf}}, a.cookie)
			expectAPIError(t, response, http.StatusUnauthorized, "invalid_credentials", false)
			if strings.Contains(response.Body.String(), viewer.deviceID) {
				t.Fatal("revoked administrator received protected device details")
			}
			a.handler = original
			fresh, _ := a.adminLogin(t)
			if !reflect.DeepEqual(device, deviceHTTPFind(t, a.serverFixture, fresh, viewer.deviceID)) {
				t.Fatal("stale administrator renamed or removed a device")
			}
			expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, viewer.headers), http.StatusOK)
		})
	}
}
