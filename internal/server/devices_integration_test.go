//go:build linux

package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func deviceHTTPCompatItems(t *testing.T, f *serverFixture, headers http.Header, query string) []map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Devices"+query, nil, headers)
	expectStatus(t, response, http.StatusOK)
	deviceHTTPNoStore(t, response)
	object := jsonObject(t, response)
	values, ok := object["Items"].([]any)
	if !ok || len(object) != 2 || object["TotalRecordCount"] != float64(len(values)) {
		t.Fatal("compatibility device list must contain a non-null array and coherent total")
	}
	items := make([]map[string]any, 0, len(values))
	for _, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatal("compatibility device list member must be an object")
		}
		deviceHTTPCompatDTO(t, item)
		items = append(items, item)
	}
	return items
}

func deviceHTTPCompatDTO(t *testing.T, item map[string]any) {
	t.Helper()
	allowed := map[string]bool{"Id": true, "ReportedDeviceId": true, "Name": true, "AppName": true, "AppVersion": true, "LastUserId": true, "LastUserName": true, "DateLastActivity": true, "IpAddress": true}
	for field := range item {
		if !allowed[field] {
			t.Fatalf("compatibility device exposed an undocumented or private field %s", field)
		}
	}
	id := stringValue(t, item, "Id")
	parsed, err := strconv.ParseInt(id, 10, 64)
	if err != nil || parsed < 1 || strconv.FormatInt(parsed, 10) != id {
		t.Fatal("compatibility DeviceInfo.Id must be a canonical decimal string")
	}
	stringValue(t, item, "ReportedDeviceId")
	stringValue(t, item, "Name")
	expectApplicationKeyHTTPUTC(t, stringValue(t, item, "DateLastActivity"))
	for _, field := range []string{"AppName", "AppVersion", "IpAddress"} {
		if _, ok := item[field].(string); !ok {
			t.Fatalf("compatibility device omitted string field %s", field)
		}
	}
}

func deviceHTTPCompatGet(t *testing.T, f *serverFixture, headers http.Header, action, id string) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Devices/"+action+"?Id="+url.QueryEscape(id), nil, headers)
	expectStatus(t, response, http.StatusOK)
	deviceHTTPNoStore(t, response)
	return jsonObject(t, response)
}

func deviceHTTPCompatNoContent(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	expectStatus(t, response, http.StatusNoContent)
	deviceHTTPNoStore(t, response)
	if response.Body.Len() != 0 {
		t.Fatal("compatibility device mutation or absent numeric lookup must have an empty 204 body")
	}
}

func TestHTTPDevicesSixRoutesRequireAdministratorEmbyAuthority(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	deviceHTTPUser(t, a.serverFixture, "Device Viewer")
	viewer := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "compat-viewer", "Viewer Room", "Viewer App", "1.0")
	admin := deviceHTTPLogin(t, a.serverFixture, "Administrator", "administrator-password", "compat-admin", "Admin Room", "Admin App", "1.0")
	device := deviceHTTPFind(t, a.serverFixture, a.cookie, viewer.deviceID)
	id := device["Id"].(string)
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/emby/Devices", nil}, {http.MethodGet, "/emby/Devices/Info?Id=" + id, nil}, {http.MethodGet, "/emby/Devices/Options?Id=" + id, nil},
		{http.MethodPost, "/emby/Devices/Options?Id=" + id, map[string]any{"CustomName": "Denied"}},
		{http.MethodDelete, "/emby/Devices?Id=" + id, nil}, {http.MethodPost, "/emby/Devices/Delete?Id=" + id, nil},
	} {
		expectAPIError(t, a.request(t, route.method, route.path, route.body, nil), http.StatusUnauthorized, "authentication_required", true)
		expectAPIError(t, a.request(t, route.method, route.path, route.body, nil, a.cookie), http.StatusUnauthorized, "authentication_required", true)
		expectEmbyTextError(t, a.request(t, route.method, route.path, route.body, viewer.headers), http.StatusForbidden, "User Device Viewer does not have access to ManageServer feature.")
	}
	if !reflect.DeepEqual(device, deviceHTTPFind(t, a.serverFixture, a.cookie, viewer.deviceID)) {
		t.Fatal("unauthorized compatibility device operations changed the registry")
	}
	items := deviceHTTPCompatItems(t, a.serverFixture, admin.headers, "")
	if len(items) != 2 {
		t.Fatal("ordinary administrator device list omitted authorized ordinary registry entries")
	}
	for _, sortOrder := range []string{"Ascending", "Descending", "ascending", "invalid"} {
		if got := deviceHTTPCompatItems(t, a.serverFixture, admin.headers, "?SortOrder="+sortOrder); !reflect.DeepEqual(items, got) {
			t.Fatal("compatibility SortOrder changed the registry's activity order or membership")
		}
	}
	canonical := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", id)
	reported := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", viewer.deviceID)
	deviceHTTPCompatDTO(t, canonical)
	if !reflect.DeepEqual(canonical, reported) || canonical["ReportedDeviceId"] != viewer.deviceID || canonical["Id"] != id {
		t.Fatal("compatibility reported-ID lookup did not resolve the same registry identity")
	}
	if options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", id); len(options) != 0 {
		t.Fatal("default compatibility device options must be an empty object")
	}
	for _, action := range []string{"Info", "Options"} {
		for _, query := range []string{"", "?Id=unregistered-reported-device"} {
			expectEmbyTextError(t, a.request(t, http.MethodGet, "/emby/Devices/"+action+query, nil, admin.headers), http.StatusNotFound, "Exception of type 'MediaBrowser.Common.Extensions.ResourceNotFoundException' was thrown.")
		}
	}
	deviceHTTPCompatNoContent(t, a.request(t, http.MethodGet, "/emby/Devices/Info?Id=9223372036854775807", nil, admin.headers))
	if unknown := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", "9223372036854775807"); len(unknown) != 0 {
		t.Fatal("unknown positive numeric device options must be an empty object")
	}
	for _, query := range []string{"?Id=" + id + "&id=" + id, "?Id=%ff", "?Id=a%00b", "?Id=" + strings.Repeat("x", 257)} {
		expectAPIError(t, a.request(t, http.MethodGet, "/emby/Devices/Info"+query, nil, admin.headers), http.StatusBadRequest, "invalid_input", true)
	}
	// Invalid URL escaping prevents complete credential-query parsing before
	// the device handler runs; a token header cannot rescue a partial query.
	expectEmbyTextError(t, a.request(t, http.MethodGet, "/emby/Devices/Info?Id=%zz", nil, admin.headers),
		http.StatusUnauthorized, embyInvalidTokenMessage)
}

func TestHTTPDevicesOptionsClearEmptyNullAndMissingImmediatelyRefreshSessions(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	deviceHTTPUser(t, a.serverFixture, "Device Viewer")
	deviceHTTPUser(t, a.serverFixture, "Device Other")
	admin := deviceHTTPLogin(t, a.serverFixture, "Administrator", "administrator-password", "options-admin", "Administrator Room", "Admin App", "1.0")
	first := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "options-shared", "Earlier Report", "Viewer App", "1.0")
	second := deviceHTTPLogin(t, a.serverFixture, "Device Other", "device-viewer-password", first.deviceID, "Current Report", "Other App", "2.0")
	device := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
	id := device["Id"].(string)
	path := "/emby/Devices/Options?Id=" + url.QueryEscape(first.deviceID)
	for _, clear := range []map[string]any{{"CustomName": ""}, {"CustomName": nil}, {}} {
		deviceHTTPCompatNoContent(t, a.request(t, http.MethodPost, path, map[string]any{"CustomName": "Shared Cinema"}, admin.headers))
		if options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", id); len(options) != 1 || options["CustomName"] != "Shared Cinema" {
			t.Fatal("compatibility rename did not persist its only supported option")
		}
		if info := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", id); info["Name"] != "Shared Cinema" {
			t.Fatal("compatibility rename did not refresh DeviceInfo.Name")
		}
		for _, login := range []clientSessionHTTPLogin{first, second} {
			if clientSessionHTTPOne(t, a.serverFixture, login, login.id)["DeviceName"] != "Shared Cinema" {
				t.Fatal("compatibility rename left a grouped session name stale")
			}
		}
		deviceHTTPCompatNoContent(t, a.request(t, http.MethodPost, path, clear, admin.headers))
		if options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", id); len(options) != 0 {
			t.Fatal("empty, null, or missing CustomName did not clear the override")
		}
		if info := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", id); info["Name"] != "Current Report" {
			t.Fatal("clearing compatibility options did not restore the reported device name")
		}
		for _, test := range []struct {
			login clientSessionHTTPLogin
			name  string
		}{{first, "Earlier Report"}, {second, "Current Report"}} {
			if clientSessionHTTPOne(t, a.serverFixture, test.login, test.login.id)["DeviceName"] != test.name {
				t.Fatal("compatibility clear did not immediately restore each session's own reported name")
			}
		}
		if native := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID); native["CustomName"] != nil || native["Name"] != "Current Report" {
			t.Fatal("compatibility and native device options diverged")
		}
	}
	before := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
	for _, body := range []string{"null", "[]", `{"CustomName":1}`, `{"CustomName":true}`, `{"CustomName":"A","CustomName":"B"}`, `{"CustomName":"A","customname":"B"}`, `{"CustomName":"a\u0000b"}`, `{"CustomName":"a\nb"}`, "{\"CustomName\":\"\xff\"}", `{"CustomName":"` + strings.Repeat("x", 257) + `"}`, "{}{}"} {
		expectAPIError(t, adminMetadataHTTPRaw(t, a.serverFixture, http.MethodPost, path, body, http.Header{"X-Emby-Token": {admin.headers.Get("X-Emby-Token")}, "Content-Type": {"application/json"}}, nil), http.StatusBadRequest, "invalid_input", true)
	}
	if !reflect.DeepEqual(before, deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)) {
		t.Fatal("invalid compatibility options partially changed the device")
	}
}

func TestHTTPDevicesDeleteMethodsRevokeGroupedLoginsAndRetainOtherDevices(t *testing.T) {
	for _, method := range []string{http.MethodDelete, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			a := newApplicationKeyHTTPFixture(t)
			deviceHTTPUser(t, a.serverFixture, "Device Viewer")
			deviceHTTPUser(t, a.serverFixture, "Device Other")
			admin := deviceHTTPLogin(t, a.serverFixture, "Administrator", "administrator-password", "delete-control", "Control Device", "Control App", "1.0")
			first := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", "delete-shared", "Viewer Device", "Viewer App", "1.0")
			second := deviceHTTPLogin(t, a.serverFixture, "Device Other", "device-viewer-password", first.deviceID, "Other Device", "Other App", "2.0")
			sharedAdmin := deviceHTTPLogin(t, a.serverFixture, "Administrator", "administrator-password", first.deviceID, "Shared Admin Device", "Shared Admin App", "3.0")
			device := deviceHTTPFind(t, a.serverFixture, a.cookie, first.deviceID)
			id := device["Id"].(string)
			deviceHTTPCompatNoContent(t, a.request(t, http.MethodPost, "/emby/Devices/Options?Id="+id, map[string]any{"CustomName": "Removed Custom Name"}, admin.headers))
			path := "/emby/Devices"
			lookup := id
			if method == http.MethodPost {
				path += "/Delete"
				lookup = first.deviceID
			}
			deviceHTTPCompatNoContent(t, a.request(t, method, path+"?Id="+url.QueryEscape(lookup), nil, admin.headers))
			deviceHTTPCompatNoContent(t, a.request(t, method, path+"?Id="+id, nil, admin.headers))
			deviceHTTPCompatNoContent(t, a.request(t, http.MethodGet, "/emby/Devices/Info?Id="+id, nil, admin.headers))
			if options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", id); len(options) != 0 {
				t.Fatal("deleted numeric device options must remain an empty object")
			}
			for _, login := range []clientSessionHTTPLogin{first, second, sharedAdmin} {
				expectAPIError(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusUnauthorized, "invalid_credentials", true)
			}
			expectStatus(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, admin.headers), http.StatusOK)
			expectStatus(t, a.request(t, http.MethodGet, "/admin/v1/session", nil, nil, a.cookie), http.StatusOK)
			for _, item := range deviceHTTPCompatItems(t, a.serverFixture, admin.headers, "") {
				if item["Id"] == id || item["ReportedDeviceId"] == first.deviceID {
					t.Fatal("removed ordinary device remained in the complete compatibility list")
				}
			}
			replacement := deviceHTTPLogin(t, a.serverFixture, "Device Viewer", "device-viewer-password", first.deviceID, "New Generation", "New App", "4.0")
			current := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Info", first.deviceID)
			if current["Id"] == id || current["Name"] != "New Generation" || replacement.id == first.id {
				t.Fatal("compatibility relogin reused a deleted registry generation or old custom name")
			}
			if options := deviceHTTPCompatGet(t, a.serverFixture, admin.headers, "Options", current["Id"].(string)); len(options) != 0 {
				t.Fatal("new registry generation inherited removed device options")
			}
			for _, login := range []clientSessionHTTPLogin{first, second, sharedAdmin} {
				expectAPIError(t, a.request(t, http.MethodGet, "/emby/Sessions", nil, login.headers), http.StatusUnauthorized, "invalid_credentials", true)
			}
		})
	}
}
