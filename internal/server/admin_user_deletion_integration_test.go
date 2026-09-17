package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPDeleteManagedUserRequiresNativeAuthorityCSRFAndStrictBody(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	id := managedHTTPCreate(t, f, cookie, csrf, "Delete HTTP Member", false)
	path := "/admin/v1/users/" + id
	body := map[string]any{"Revision": "1"}
	expectAPIError(t, f.request(t, http.MethodDelete, path, body, nil), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodDelete, path, body, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	expectAPIError(t, f.request(t, http.MethodDelete, path, body, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://foreign.example.test"}}, cookie), http.StatusForbidden, "origin_denied", false)
	login := f.embyLogin(t, "Delete HTTP Member", "managed-user-password")
	token := stringValue(t, login, "AccessToken")
	expectAPIError(t, f.request(t, http.MethodDelete, path, body, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized, "authentication_required", false)
	expectAPIError(t, f.request(t, http.MethodDelete, path, body, http.Header{"X-CSRF-Token": {csrf}}, &http.Cookie{Name: sessionCookie, Value: token}), http.StatusUnauthorized, "invalid_credentials", false)
	for _, malformed := range []string{`{}`, `{"Revision":1}`, `{"Revision":null}`, `{"Revision":"1","Revision":"1"}`, `{"Revision":"1","Id":"other"}`, `{"Revision":"1"}{}`} {
		request := httptest.NewRequest(http.MethodDelete, path, strings.NewReader(malformed))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		expectAPIError(t, response, http.StatusBadRequest, "invalid_input", false)
	}
	if user := managedHTTPDetail(t, f, cookie, id); user["Revision"] != "1" {
		t.Fatal("rejected delete changed the target revision")
	}
}

func TestHTTPDeleteManagedUserRevisionAuditAndSelfCookieContract(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	otherID := managedHTTPCreate(t, f, cookie, csrf, "Deletion Survivor", true)
	memberID := managedHTTPCreate(t, f, cookie, csrf, "Deleted Member", false)
	headers := http.Header{"X-CSRF-Token": {csrf}}
	path := "/admin/v1/users/" + memberID
	expectAPIError(t, f.request(t, http.MethodDelete, path, map[string]any{"Revision": "2"}, headers, cookie), http.StatusConflict, "revision_conflict", false)
	response := f.request(t, http.MethodDelete, path, map[string]any{"Revision": "1"}, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	if body := jsonObject(t, response); len(body) != 1 || body["CurrentSessionRevoked"] != false || len(response.Result().Cookies()) != 0 {
		t.Fatal("other-user deletion returned an editable user or cleared the actor cookie")
	}
	expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil, cookie), http.StatusNotFound, "not_found", false)
	expectAPIError(t, f.request(t, http.MethodDelete, path, map[string]any{"Revision": "1"}, headers, cookie), http.StatusNotFound, "not_found", false)
	response = f.request(t, http.MethodDelete, "/admin/v1/users/"+adminID, map[string]any{"Revision": "1"}, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	if len(jsonObject(t, response)) != 1 {
		t.Fatal("self deletion exposed internal session cleanup handles")
	}
	expectManagedCookieCleared(t, response)
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/session", nil, nil, cookie), http.StatusUnauthorized, "invalid_credentials", false)
	survivorCookie, survivorCSRF := managedHTTPLogin(t, f, "Deletion Survivor", "managed-user-password")
	activityResponse := f.request(t, http.MethodGet, "/admin/v1/activity?Action=user.deleted&Limit=10", nil, nil, survivorCookie)
	expectStatus(t, activityResponse, http.StatusOK)
	page := jsonObject(t, activityResponse)
	items := page["Items"].([]any)
	if page["TotalRecordCount"] != float64(2) || len(items) != 2 {
		t.Fatal("user.deleted filtering did not return the two real deletions")
	}
	for _, raw := range items {
		entry := raw.(map[string]any)
		if entry["Action"] != "user.deleted" || entry["Name"] != "User deleted" || entry["Overview"] != "A user account and its associated user state were deleted." || entry["Revision"] != "1" {
			t.Fatal("deleted-user public activity metadata is incomplete")
		}
		actor := objectValue(t, entry, "Actor")
		if actor["Id"] != adminID || actor["Name"] != nil {
			t.Fatal("deleted actor was removed from audit history or given a fabricated name")
		}
	}
	expectAPIError(t, f.request(t, http.MethodDelete, "/admin/v1/users/"+otherID, map[string]any{"Revision": "1"}, http.Header{"X-CSRF-Token": {survivorCSRF}}, survivorCookie), http.StatusConflict, "last_administrator", false)
}
