//go:build linux

package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestManagedProviderHTTPPreservesCookieCSRFRevisionAndSecretBoundaries(t *testing.T) {
	f := newServerFixture(t)
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "provider-http-master.key")))
	f.app.identity = f.users
	if err := f.app.initializeProviderConfiguration(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	path := "/admin/v1/providers/configuration"
	expectAPIError(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized, "authentication_required", false)
	response := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("provider configuration was cacheable")
	}
	initial := jsonObject(t, response)
	if stringValue(t, initial, "Revision") != "1" {
		t.Fatal("provider configuration did not expose a string revision")
	}
	secret := "native-provider-credential-sentinel"
	input := map[string]any{"Revision": "1", "Fields": map[string]any{"Token": map[string]any{"Mode": "replace", "Value": secret}}}
	updatePath := "/admin/v1/providers/tmdb/configuration"
	expectAPIError(t, f.request(t, http.MethodPut, updatePath, input, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
	expectAPIError(t, f.request(t, http.MethodPut, updatePath, input, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://untrusted.invalid"}}, cookie), http.StatusForbidden, "origin_denied", false)
	headers := http.Header{"X-CSRF-Token": {csrf}}
	response = f.request(t, http.MethodPut, updatePath, input, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	if strings.Contains(response.Body.String(), secret) {
		t.Fatal("provider mutation returned its credential")
	}
	if stringValue(t, jsonObject(t, response), "Revision") != "2" {
		t.Fatal("provider mutation lost its exact successor revision")
	}
	expectAPIError(t, f.request(t, http.MethodPut, updatePath, input, headers, cookie), http.StatusConflict, "revision_conflict", false)
	response = f.request(t, http.MethodPost, "/admin/v1/providers/tmdb/test", map[string]any{"Revision": "2"}, headers, cookie)
	expectStatus(t, response, http.StatusOK)
	if stringValue(t, jsonObject(t, response), "Status") != "disabled" {
		t.Fatal("deployment-disabled provider attempted a connectivity check")
	}
	expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/providers/tmdb/test", map[string]any{"Revision": "1"}, headers, cookie), http.StatusConflict, "revision_conflict", false)
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id IN (SELECT id FROM sessions WHERE kind='admin')`); err != nil {
		t.Fatal(err)
	}
	response = f.request(t, http.MethodGet, path, nil, nil, cookie)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("revoked administrator read provider configuration")
	}
}
