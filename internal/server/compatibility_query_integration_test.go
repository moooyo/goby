//go:build linux

package server

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPCompatibilityQueryCarriersPreserveAuthorityAcrossManagementEndpoints(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	key := f.create(t, "Query transport key")
	carriers := func(token string) url.Values {
		return url.Values{
			"X-Emby-Token": {token}, "API_KEY": {token},
			"X-Emby-Client": {"Query Browser"}, "X-Emby-Client-Version": {"1.0"},
			"X-Emby-Device-Id": {"query-management-device"}, "X-Emby-Device-Name": {"Linux"},
			"X-Emby-Language": {"zh-CN"},
		}
	}
	for _, path := range []string{
		"/emby/System/Configuration", "/emby/Devices", "/emby/Auth/Keys",
		"/emby/ScheduledTasks", "/emby/System/ActivityLog/Entries",
	} {
		for _, actor := range []struct {
			name, token string
			status      int
		}{
			{"administrator", f.admin.headers["X-Emby-Token"][0], http.StatusOK},
			{"application_key", key.token, http.StatusOK},
			{"viewer", f.viewer.headers["X-Emby-Token"][0], http.StatusForbidden},
		} {
			t.Run(path+"/"+actor.name, func(t *testing.T) {
				status := actor.status
				if actor.name == "viewer" && path == "/emby/System/Configuration" {
					status = http.StatusOK
				}
				baseline := f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {actor.token}})
				expectStatus(t, baseline, status)
				query := carriers(actor.token)
				response := f.request(t, http.MethodGet, path+"?"+query.Encode(), nil, nil)
				expectStatus(t, response, status)
				if actor.name == "viewer" && path == "/emby/System/Configuration" && response.Body.String() != "{}" {
					t.Fatal("query credentials broadened the viewer configuration projection")
				}
				query.Set("api_key", "conflicting-token")
				response = f.request(t, http.MethodGet, path+"?"+query.Encode(), nil, nil)
				expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
			})
		}
		query := carriers("")
		expectEmbyTextError(t, f.request(t, http.MethodGet, path+"?"+query.Encode(), nil, nil), http.StatusUnauthorized, embyInvalidTokenMessage)
		query = carriers(key.token)
		query.Set("X-Emby-IsAdministrator", "true")
		expectStatus(t, f.request(t, http.MethodGet, path+"?"+query.Encode(), nil, nil), http.StatusBadRequest)
	}
	query := carriers(key.token)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/api-keys?"+query.Encode(), nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/System/Configuration/Partial?"+query.Encode(), map[string]any{}, nil), http.StatusNoContent)
	query.Set("App", "Query-created key")
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Auth/Keys?"+query.Encode(), nil, nil), http.StatusNoContent)
	query.Del("App")
	query.Set("UndeclaredBusinessField", "ignored")
	expectStatus(t, f.request(t, http.MethodDelete, "/emby/Auth/Keys/"+key.token+"?"+query.Encode(), nil, nil), http.StatusBadRequest)
	if _, err := f.users.ResolveEmby(f.ctx, key.token); err != nil {
		t.Fatalf("rejected query revoked an application credential: %v", err)
	}
	query.Del("UndeclaredBusinessField")
	expectStatus(t, f.request(t, http.MethodDelete, "/emby/Auth/Keys/"+key.token+"?"+query.Encode(), nil, nil), http.StatusNoContent)
	if _, err := f.users.ResolveEmby(f.ctx, key.token); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("accepted query did not revoke its exact application credential: %v", err)
	}
}
