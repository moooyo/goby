//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHTTPClientCapabilitiesReferencePlainTextJSON(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t)
	send := func(path string, body any, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal("encode reference capability request")
		}
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded)).WithContext(f.ctx)
		request.Header = headers.Clone()
		for _, cookie := range cookies {
			request.AddCookie(cookie)
		}
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		return response
	}
	for _, contentType := range []string{"text/plain", "text/plain; charset=UTF-8"} {
		headers := accounts.viewer.headers.Clone()
		headers.Set("Content-Type", contentType)
		body := map[string]any{"PlayableMediaTypes": []string{"Video", "Audio"}, "SupportsMediaControl": true}
		response := send("/emby/Sessions/Capabilities/Full", body, headers)
		expectStatus(t, response, http.StatusNoContent)
		before := clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)
		if before["SupportsMediaControl"] != true {
			t.Fatal("the reference client's plain-text JSON capabilities were not persisted")
		}
		response = send("/emby/Sessions/Capabilities/Full",
			json.RawMessage(`{"SupportsMediaControl":true,"SupportsMediaControl":false}`), headers)
		expectStatus(t, response, http.StatusBadRequest)
		if !reflect.DeepEqual(before, clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)) {
			t.Fatal("invalid plain-text JSON replaced the previous capability snapshot")
		}
		ambiguous := headers.Clone()
		ambiguous.Add("Content-Type", "application/octet-stream")
		response = send("/emby/Sessions/Capabilities/Full", body, ambiguous)
		expectStatus(t, response, http.StatusUnsupportedMediaType)
		if !reflect.DeepEqual(before, clientSessionHTTPStoredCapabilities(t, f, accounts.viewer.id)) {
			t.Fatal("ambiguous content types replaced the previous capability snapshot")
		}
		response = send("/emby/Sessions/Capabilities/Full", body,
			http.Header{"Content-Type": {contentType}}, accounts.cookie)
		expectStatus(t, response, http.StatusUnauthorized)
		response = send("/admin/v1/session", map[string]string{
			"Name": "Administrator", "Password": "administrator-password",
		}, http.Header{"Content-Type": {contentType}, "Origin": {f.cfg.PublicURL}})
		expectStatus(t, response, http.StatusUnsupportedMediaType)
	}
	if len(clientSessionHTTPStoredCapabilities(t, f, accounts.other.id)) != 0 {
		t.Fatal("the reference content type changed another user's capabilities")
	}
}
