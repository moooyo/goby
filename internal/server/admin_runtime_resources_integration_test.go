package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPAdminRuntimeResourcesNativeAuthorityAndSafeBoundedProjection(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	path := "/admin/v1/runtime/resources"
	denied := f.request(t, http.MethodGet, path, nil, nil)
	expectStatus(t, denied, http.StatusUnauthorized)
	if denied.Header().Get("Cache-Control") != "no-store" || denied.Header().Get("Pragma") != "no-cache" {
		t.Fatal("unauthorized resource observations were cacheable")
	}
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	token := stringValue(t, emby, "AccessToken")
	for _, asCookie := range []bool{false, true} {
		headers := http.Header{"X-Emby-Token": {token}}
		var cookies []*http.Cookie
		if asCookie {
			headers = nil
			cookies = []*http.Cookie{{Name: sessionCookie, Value: token}}
		}
		expectStatus(t, f.request(t, http.MethodGet, path, nil, headers, cookies...), http.StatusUnauthorized)
	}
	actor := identity.Principal{User: identity.User{ID: "private-owner-marker"}, SessionID: "private-session-marker", Kind: "emby"}
	_, leave, err := f.app.originals.enterSource(actor, "item-runtime", "mediasource_item-runtime")
	if err != nil {
		t.Fatal(err)
	}
	defer leave()
	response := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("authorized resource observations were cacheable")
	}
	body := jsonObject(t, response)
	if len(body) != 3 || len(objectValue(t, body, "StorageObservations")) != 2 || len(objectValue(t, body, "OriginalStreams")) != 11 {
		t.Fatal("runtime endpoint exposed an undocumented root or original-stream field")
	}
	originals := objectValue(t, body, "OriginalStreams")
	current, ok := originals["Current"].([]any)
	if !ok || len(current) != 1 || len(current[0].(map[string]any)) != 8 {
		t.Fatal("active source lease shape is not closed")
	}
	lease := current[0].(map[string]any)
	if lease["ItemId"] != "item-runtime" || lease["MediaSourceId"] != "mediasource_item-runtime" || lease["Active"] != true || lease["CompletedUnixNano"] != "0" {
		t.Fatal("endpoint did not project the actual registered lease")
	}
	for _, secret := range []string{"private-owner-marker", "private-session-marker", cookie.Value, token, f.cfg.DatabaseURL,
		"Directory", "URL", "Session", "Token", "Path", "Password"} {
		if secret != "" && strings.Contains(response.Body.String(), secret) {
			t.Fatal("runtime observation exposed request, credential or filesystem metadata")
		}
	}
	bad := f.request(t, http.MethodGet, path+"?private-query-marker=1", nil, nil, cookie)
	expectStatus(t, bad, http.StatusBadRequest)
	if strings.Contains(bad.Body.String(), "private-query-marker") {
		t.Fatal("resource endpoint reflected a rejected query")
	}
	leave()
	final := objectValue(t, jsonObject(t, f.request(t, http.MethodGet, path, nil, nil, cookie)), "OriginalStreams")
	completed := final["Completed"].([]any)
	if len(completed) != 1 || completed[0].(map[string]any)["LeaseId"] != lease["LeaseId"] || completed[0].(map[string]any)["Active"] != false {
		t.Fatal("completed projection lost the exact active lease identity")
	}
}
