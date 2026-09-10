//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func applicationKeyDeviceHTTPAdmin(t *testing.T, a *applicationKeyHTTPFixture) clientSessionHTTPLogin {
	t.Helper()
	return deviceHTTPLogin(t, a.serverFixture, "Administrator", "administrator-password", "key-device-admin-control", "Administrator Control", "Device Manager", "1.0")
}

func applicationKeyDeviceHTTPGroup(t *testing.T, a *applicationKeyHTTPFixture, headers http.Header, keys ...applicationKeyHTTPSecret) (string, string) {
	t.Helper()
	response := a.request(t, http.MethodGet, "/emby/Auth/Keys", nil, headers)
	expectStatus(t, response, http.StatusOK)
	deviceHTTPNoStore(t, response)
	object := jsonObject(t, response)
	items, ok := object["Items"].([]any)
	if !ok || len(items) != len(keys) || object["TotalRecordCount"] != float64(len(keys)) {
		t.Fatal("active compatibility key list did not retain the expected credential set")
	}
	want := make(map[string]bool, len(keys))
	for _, key := range keys {
		want[key.token] = true
	}
	var groupID, reportedID string
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatal("compatibility key list member must be an object")
		}
		token := stringValue(t, item, "AccessToken")
		if !want[token] {
			t.Fatal("active key list returned an unrelated or duplicate bearer credential")
		}
		delete(want, token)
		numeric, ok := item["DeviceId"].(float64)
		if !ok || numeric < 1 || numeric > 9007199254740991 || numeric != float64(int64(numeric)) {
			t.Fatal("Auth/Keys.DeviceId must remain a positive numeric registry identity")
		}
		id := strconv.FormatInt(int64(numeric), 10)
		reported := stringValue(t, item, "ReportedDeviceId")
		if groupID != "" && (groupID != id || reportedID != reported) {
			t.Fatal("same-generation application keys did not share one numeric and reported server-device identity")
		}
		groupID, reportedID = id, reported
	}
	if len(want) != 0 {
		t.Fatal("compatibility key list omitted an expected issued credential")
	}
	return groupID, reportedID
}

func applicationKeyDeviceHTTPContext(t *testing.T, a *applicationKeyHTTPFixture, key applicationKeyHTTPSecret, suffix string) applicationSessionContext {
	t.Helper()
	principal, err := a.users.ResolveEmby(a.ctx, key.token)
	if err != nil || !principal.IsApplicationKey() {
		t.Fatal("issued application key did not retain its parent credential")
	}
	return newApplicationSessionContext(t, a.serverFixture, applicationMediaKey{
		key: identity.ApplicationKey{Token: key.token, CredentialID: principal.SessionID}, principal: principal,
		headers: http.Header{"X-Emby-Token": {key.token}},
	}, suffix)
}

func applicationKeyDeviceHTTPDenied(t *testing.T, a *applicationKeyHTTPFixture, headers ...http.Header) {
	t.Helper()
	for _, header := range headers {
		expectAPIError(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, header), http.StatusUnauthorized, "invalid_credentials", true)
	}
}

func applicationKeyDeviceHTTPHistory(t *testing.T, a *applicationKeyHTTPFixture, credentialIDs []string) string {
	t.Helper()
	var history string
	if err := a.pool.QueryRow(a.ctx, `SELECT jsonb_build_object(
		'credentials', (SELECT jsonb_agg(to_jsonb(s) - 'revoked_at' ORDER BY id) FROM sessions s WHERE id = ANY($1::text[])),
		'keys', (SELECT jsonb_agg(to_jsonb(k) ORDER BY id) FROM application_keys k WHERE credential_id = ANY($1::text[])),
		'clients', (SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM application_key_clients c WHERE credential_id = ANY($1::text[]))
	)::text`, credentialIDs).Scan(&history); err != nil {
		t.Fatalf("read retained application device history: %v", err)
	}
	return history
}

func TestHTTPApplicationKeyDevicesShareHiddenIdentityAndKeepMetadataContextsSeparate(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	admin := applicationKeyDeviceHTTPAdmin(t, a)
	first, second := a.create(t, "Shared Device Alpha"), a.create(t, "Shared Device Beta")
	id, reported := applicationKeyDeviceHTTPGroup(t, a, admin.headers, first, second)
	if id != "1" || reported != a.app.serverID {
		t.Fatal("first application-key device generation did not retain the reserved server identity")
	}
	numeric := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", id)
	alias := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", reported)
	deviceHTTPCompatDTO(t, numeric)
	if !reflect.DeepEqual(numeric, alias) || numeric["Id"] != id || numeric["ReportedDeviceId"] != reported {
		t.Fatal("shared server device numeric and reported lookups resolved different identities")
	}
	for _, field := range []string{"LastUserId", "LastUserName"} {
		if _, present := numeric[field]; present {
			t.Fatal("userless server device fabricated a last user")
		}
	}
	alpha := applicationKeyDeviceHTTPContext(t, a, first, "device-boundary-alpha")
	beta := applicationKeyDeviceHTTPContext(t, a, second, "device-boundary-beta")
	if alpha.principal.SessionID == beta.principal.SessionID || alpha.principal.Client.DeviceID == reported || beta.principal.Client.DeviceID == reported {
		t.Fatal("application metadata contexts collapsed into the shared server-device identity")
	}
	native, _ := deviceHTTPNativeList(t, a.serverFixture, a.cookie, "")
	compat := deviceHTTPCompatItems(t, a.serverFixture, admin.headers, "")
	if len(native) != 1 || len(compat) != 1 || native[0]["ReportedDeviceId"] != admin.deviceID || compat[0]["ReportedDeviceId"] != admin.deviceID {
		t.Fatal("ordinary device lists exposed the hidden server device or registered application metadata contexts")
	}
	for _, action := range []string{"options", "delete"} {
		body := map[string]any{"Revision": "1"}
		if action == "options" {
			body["CustomName"] = "Forbidden Native Rename"
		}
		expectAPIError(t, a.native(t, http.MethodPost, "/admin/v1/devices/"+id+"/"+action, body), http.StatusNotFound, "not_found", false)
	}
	applicationKeyDeviceHTTPGroup(t, a, admin.headers, first, second)
	for _, context := range []applicationSessionContext{alpha, beta} {
		applicationContextSessionDTO(t, a.serverFixture, context.headers, context.principal.ClientSessionID)
	}
	encoded, err := json.Marshal(numeric)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{first.token, second.token, alpha.principal.SessionID, beta.principal.SessionID} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("server DeviceInfo exposed bearer or parent credential material")
		}
	}
}

func TestHTTPApplicationKeyDeviceOptionsPersistClearAndRejectOrdinaryViewers(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	admin := applicationKeyDeviceHTTPAdmin(t, a)
	deviceHTTPUser(t, a.serverFixture, "Server Device Viewer")
	viewer := deviceHTTPLogin(t, a.serverFixture, "Server Device Viewer", "device-viewer-password", "key-device-viewer-control", "Viewer Control", "Viewer App", "1.0")
	first, second := a.create(t, "Options Alpha"), a.create(t, "Options Beta")
	id, reported := applicationKeyDeviceHTTPGroup(t, a, admin.headers, first, second)
	initial := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", id)
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/emby/Devices/Info?Id=" + id, nil}, {http.MethodGet, "/emby/Devices/Options?Id=" + id, nil},
		{http.MethodPost, "/emby/Devices/Options?Id=" + id, map[string]any{"CustomName": "Denied"}},
		{http.MethodDelete, "/emby/Devices?Id=" + id, nil}, {http.MethodPost, "/emby/Devices/Delete?Id=" + id, nil},
	} {
		expectEmbyTextError(t, a.request(t, route.method, route.path, route.body, viewer.headers), http.StatusForbidden, "User Server Device Viewer does not have access to ManageServer feature.")
	}
	for _, clear := range []map[string]any{{"CustomName": ""}, {"CustomName": nil}, {}} {
		deviceHTTPCompatNoContent(t, a.request(t, http.MethodPost, "/emby/Devices/Options?Id="+url.QueryEscape(reported), map[string]any{"CustomName": "  Shared Server Label  "}, admin.headers))
		// A fresh identity store must read the committed option, independently
		// of the handler instance which accepted the administrator's mutation.
		a.users = identity.NewWithApplicationKeyVault(a.pool, identity.NewApplicationKeyVault(a.master))
		a.app.identity = a.users
		options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", id)
		if len(options) != 1 || options["CustomName"] != "Shared Server Label" || deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", reported)["Name"] != "Shared Server Label" {
			t.Fatal("shared server-device options did not persist across identity store recreation")
		}
		deviceHTTPCompatNoContent(t, a.request(t, http.MethodPost, "/emby/Devices/Options?Id="+id, clear, admin.headers))
		if options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", reported); len(options) != 0 {
			t.Fatal("empty, null, or missing server CustomName did not clear its persisted option")
		}
		if info := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", id); info["Name"] != initial["Name"] || info["Id"] != id {
			t.Fatal("clearing the shared server label replaced its generation or failed to restore reported metadata")
		}
	}
	applicationKeyDeviceHTTPGroup(t, a, admin.headers, first, second)
	for _, key := range []applicationKeyHTTPSecret{first, second} {
		expectStatus(t, a.request(t, http.MethodGet, "/emby/System/Info", nil, http.Header{"X-Emby-Token": {key.token}}), http.StatusOK)
	}
	if deviceHTTPFind(t, a.serverFixture, a.cookie, viewer.deviceID)["Name"] != "Viewer Control" {
		t.Fatal("shared server-device options changed an ordinary device")
	}
}

func TestHTTPApplicationKeyDeviceDeleteRevokesAllContextsRetainsHistoryAndProtectsNewGeneration(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	admin := applicationKeyDeviceHTTPAdmin(t, a)
	deviceHTTPUser(t, a.serverFixture, "Server Device Viewer")
	viewer := deviceHTTPLogin(t, a.serverFixture, "Server Device Viewer", "device-viewer-password", "key-device-unrelated-viewer", "Unrelated Viewer", "Viewer App", "1.0")
	first, second := a.create(t, "Retired Alpha"), a.create(t, "Retired Beta")
	oldID, reported := applicationKeyDeviceHTTPGroup(t, a, admin.headers, first, second)
	alpha := applicationKeyDeviceHTTPContext(t, a, first, "group-delete-alpha")
	beta := applicationKeyDeviceHTTPContext(t, a, first, "group-delete-beta")
	sibling := applicationKeyDeviceHTTPContext(t, a, second, "group-delete-sibling")
	firstPrincipal, err := a.users.ResolveEmby(a.ctx, first.token)
	if err != nil {
		t.Fatal(err)
	}
	secondPrincipal, err := a.users.ResolveEmby(a.ctx, second.token)
	if err != nil {
		t.Fatal(err)
	}
	contexts := []applicationSessionContext{
		{principal: firstPrincipal, headers: http.Header{"X-Emby-Token": {first.token}}},
		{principal: secondPrincipal, headers: http.Header{"X-Emby-Token": {second.token}}}, alpha, beta, sibling,
	}
	server := websocketHTTPServer(t, a.serverFixture, time.Hour)
	var sockets []*websocketHTTPClient
	for _, context := range contexts {
		sockets = append(sockets, websocketHTTPDial(t, server, "/emby/socket", context.headers, http.StatusSwitchingProtocols))
		websocketHTTPWaitCount(t, a.serverFixture, context.principal.ClientSessionID, 1)
	}
	viewerSocket := websocketHTTPDial(t, server, "/emby/socket", viewer.headers, http.StatusSwitchingProtocols)
	websocketHTTPWaitCount(t, a.serverFixture, viewer.id, 1)
	credentials := []string{firstPrincipal.SessionID, secondPrincipal.SessionID}
	history := applicationKeyDeviceHTTPHistory(t, a, credentials)
	ordinaryState := adminSessionHTTPSnapshot(t, a.serverFixture)
	deviceHTTPCompatNoContent(t, a.request(t, http.MethodDelete, "/emby/Devices?Id="+oldID, nil, admin.headers))
	var committedCount int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*) FROM application_keys k JOIN sessions s ON s.id = k.credential_id
		JOIN application_key_devices d ON d.id = k.reported_device_numeric_id
		WHERE d.id::text = $1 AND d.deleted_at IS NOT NULL AND s.revoked_at = d.deleted_at`, oldID).Scan(&committedCount); err != nil || committedCount != 2 {
		t.Fatal("shared device deletion did not atomically commit both parent credential revocations")
	}
	if applicationKeyDeviceHTTPHistory(t, a, credentials) != history || adminSessionHTTPSnapshot(t, a.serverFixture) != ordinaryState {
		t.Fatal("shared device deletion erased credential/client history or changed personal and catalog state")
	}
	for index, socket := range sockets {
		socket.closed(t)
		websocketHTTPWaitCount(t, a.serverFixture, contexts[index].principal.ClientSessionID, 0)
	}
	viewerSocket.ping(t)
	var oldHeaders []http.Header
	for _, context := range contexts {
		oldHeaders = append(oldHeaders, context.headers)
	}
	applicationKeyDeviceHTTPDenied(t, a, oldHeaders...)
	applicationKeyDeviceHTTPGroup(t, a, admin.headers)
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, viewer.headers), http.StatusOK)
	expectStatus(t, a.request(t, http.MethodGet, "/admin/v1/session", nil, nil, a.cookie), http.StatusOK)
	archived := applicationKeyHTTPItems(t, a.native(t, http.MethodGet, "/admin/v1/api-keys?IncludeRevoked=true", nil), 2, 2)
	for _, raw := range archived {
		if key := raw.(map[string]any); key["Status"] != "revoked" || key["RevokedAt"] == nil {
			t.Fatal("native key history did not retain revoked group credentials")
		}
	}
	replacement := a.create(t, "Replacement Generation")
	newID, newReported := applicationKeyDeviceHTTPGroup(t, a, admin.headers, replacement)
	if newID == oldID || newReported != reported {
		t.Fatal("new key creation reused a removed server-device generation or changed its reported identity")
	}
	current := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", newID)
	if alias := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", reported); alias["Id"] != newID || current["Id"] != newID {
		t.Fatal("recreated server device aliases did not select only the new generation")
	}
	for _, action := range []string{"options", "delete"} {
		body := map[string]any{"Revision": "1"}
		if action == "options" {
			body["CustomName"] = "Forbidden New Generation Rename"
		}
		expectAPIError(t, a.native(t, http.MethodPost, "/admin/v1/devices/"+newID+"/"+action, body), http.StatusNotFound, "not_found", false)
	}
	deviceHTTPCompatNoContent(t, a.request(t, http.MethodGet, "/emby/Devices/Info?Id="+oldID, nil, admin.headers))
	newContext := applicationKeyDeviceHTTPContext(t, a, replacement, "replacement-context")
	for _, route := range []struct{ method, path string }{{http.MethodDelete, "/emby/Devices"}, {http.MethodPost, "/emby/Devices/Delete"}} {
		deviceHTTPCompatNoContent(t, a.request(t, route.method, route.path+"?Id="+oldID, nil, admin.headers))
		applicationKeyDeviceHTTPGroup(t, a, admin.headers, replacement)
		applicationContextSessionDTO(t, a.serverFixture, newContext.headers, newContext.principal.ClientSessionID)
	}
	applicationKeyDeviceHTTPDenied(t, a, oldHeaders...)
	viewerSocket.ping(t)
}

func TestHTTPApplicationKeyCanDeleteItsOwnSharedDeviceAndThenLosesAllAuthority(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	admin := applicationKeyDeviceHTTPAdmin(t, a)
	deviceHTTPUser(t, a.serverFixture, "Self Delete Viewer")
	viewer := deviceHTTPLogin(t, a.serverFixture, "Self Delete Viewer", "device-viewer-password", "self-delete-control", "Viewer Control", "Viewer App", "1.0")
	first, second := a.create(t, "Self Delete Alpha"), a.create(t, "Self Delete Beta")
	id, reported := applicationKeyDeviceHTTPGroup(t, a, admin.headers, first, second)
	actor := applicationKeyDeviceHTTPContext(t, a, first, "self-delete-actor")
	other := applicationKeyDeviceHTTPContext(t, a, second, "self-delete-peer")
	deviceHTTPCompatNoContent(t, a.request(t, http.MethodDelete, "/emby/Devices?Id="+url.QueryEscape(reported), nil, actor.headers))
	applicationKeyDeviceHTTPDenied(t, a, http.Header{"X-Emby-Token": {first.token}}, http.Header{"X-Emby-Token": {second.token}}, actor.headers, other.headers)
	expectAPIError(t, a.request(t, http.MethodDelete, "/emby/Devices?Id="+id, nil, actor.headers), http.StatusUnauthorized, "invalid_credentials", true)
	applicationKeyDeviceHTTPGroup(t, a, admin.headers)
	var retained, revoked int
	if err := a.pool.QueryRow(a.ctx, `SELECT count(*),count(*) FILTER (WHERE s.revoked_at IS NOT NULL)
		FROM application_keys k JOIN sessions s ON s.id = k.credential_id
		WHERE k.reported_device_numeric_id::text = $1`, id).Scan(&retained, &revoked); err != nil || retained != 2 || revoked != 2 {
		t.Fatal("self-directed shared device deletion rolled back or removed historical parent credentials")
	}
	deviceHTTPCompatNoContent(t, a.request(t, http.MethodGet, "/emby/Devices/Info?Id="+id, nil, admin.headers))
	for _, login := range []clientSessionHTTPLogin{admin, viewer} {
		expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusOK)
	}
	expectStatus(t, a.request(t, http.MethodGet, "/admin/v1/session", nil, nil, a.cookie), http.StatusOK)
}
