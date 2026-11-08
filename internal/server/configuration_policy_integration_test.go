//go:build linux

package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/settings"
)

func configurationPolicyHTTPRows(t *testing.T, f *serverFixture) string {
	t.Helper()
	var result string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'managed',(SELECT to_jsonb(m) FROM managed_settings m WHERE id=1),
		'policy',(SELECT to_jsonb(p) FROM server_policy_settings p WHERE id=1))::text`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHTTPConfigurationPolicyProjectionAndMixedWrites(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	const peer = "127.0.0.1:12345"
	initial := configurationHTTPObject(t, serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodGet,
		"/System/Configuration", nil, nil, "", f.admin.headers))
	if initial["EnableRemoteAccess"] != true || initial["UICulture"] != "zh-CN" || initial["LocalNetworkSubnets"] == nil {
		t.Fatal("core configuration did not expose its backed default policy")
	}
	for index, path := range []string{"/System/Configuration", "/EMBY/system/configuration/pArTiAl"} {
		body := map[string]any{"ServerName": "Core configuration policy", "HttpServerPortNumber": 9096 + index,
			"PreferredMetadataLanguage": "fr", "MetadataCountryCode": "FR", "EnableRemoteAccess": false,
			"LocalNetworkSubnets": []string{}, "UICulture": "en-US"}
		response := serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodPost, path, body, nil, "", f.admin.headers)
		configurationHTTPStatus(t, response, http.StatusNoContent)
		value := configurationHTTPObject(t, serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodGet,
			"/emby/System/Configuration", nil, nil, "", f.admin.headers))
		if value["ServerName"] != body["ServerName"] || value["HttpServerPortNumber"] != float64(9096+index) ||
			value["PreferredMetadataLanguage"] != "fr" || value["MetadataCountryCode"] != "FR" ||
			value["EnableRemoteAccess"] != false || value["UICulture"] != "en-US" ||
			!reflect.DeepEqual(value["LocalNetworkSubnets"], []any{}) {
			t.Fatal("core configuration did not persist and project its mixed settings")
		}
	}
	viewer := serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodGet, "/System/Configuration", nil, nil, "", f.viewer.headers)
	configurationHTTPStatus(t, viewer, http.StatusOK)
	if viewer.Body.String() != "{}" || viewer.Header().Get("Content-Length") != "2" {
		t.Fatal("policy additions exposed administrator data to a viewer")
	}
	rowsBefore := configurationPolicyHTTPRows(t, f.serverFixture)
	before, policyBefore := f.app.settings.Snapshot(), f.app.serverPolicy.Snapshot()
	for _, policyFields := range []map[string]any{
		{"LocalNetworkSubnets": []string{"192.0.2.1/24"}},
		{"LocalNetworkSubnets": []string{"192.0.2.0/24", "192.0.2.0/24"}},
		{"UICulture": "fr-FR"},
		{"EnableRemoteAccess": true, "ConfirmDisconnect": true},
	} {
		policyFields["ServerName"] = "Invalid mixed write must roll back"
		policyFields["HttpServerPortNumber"] = 9100
		response := serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodPost, "/emby/System/Configuration/Partial",
			policyFields, nil, "", f.admin.headers)
		configurationHTTPStatus(t, response, http.StatusBadRequest)
		if configurationPolicyHTTPRows(t, f.serverFixture) != rowsBefore || !reflect.DeepEqual(f.app.settings.Snapshot(), before) ||
			!reflect.DeepEqual(f.app.serverPolicy.Snapshot(), policyBefore) {
			t.Fatal("invalid mixed configuration changed one of its rows or published caches")
		}
	}
}

func TestHTTPConfigurationPolicySelfDisconnectCompletesAuthorizedResponse(t *testing.T) {
	for _, path := range []string{"/System/Configuration", "/EMBY/system/configuration/pArTiAl"} {
		t.Run(path, func(t *testing.T) {
			f := newConfigurationHTTPFixture(t)
			const address, peer = "203.0.113.17", "203.0.113.17:12345"
			probe := httptest.NewRequest(http.MethodPost, path, nil).WithContext(f.ctx)
			probe.RemoteAddr = peer
			admitted, finish, admittedAllowed := f.app.admitServerPolicy(httptest.NewRecorder(), compatibilityNamespace(probe))
			if !admittedAllowed {
				t.Fatal("the initial core policy mutation was denied")
			}
			defer finish()
			namedProbe := httptest.NewRequest(http.MethodPost, "/emby/System/Configuration/encoding", nil).WithContext(f.ctx)
			namedProbe.RemoteAddr = peer
			namedAdmitted, finishNamed, namedAllowed := f.app.admitServerPolicy(httptest.NewRecorder(), namedProbe)
			if !namedAllowed {
				t.Fatal("the initial named configuration operation was denied")
			}
			defer finishNamed()
			operation, release, allowed := f.app.serverPolicy.Watch(f.ctx, address)
			if !allowed {
				t.Fatal("the initial remote operation was denied")
			}
			defer release()
			key := f.create(t, "Core policy self-disconnect key")
			headers := http.Header{"X-Emby-Token": {key.token}}
			response := serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodPost, path,
				map[string]any{"ServerName": "Committed before remote revocation", "EnableRemoteAccess": false,
					"LocalNetworkSubnets": []string{}, "UICulture": "en-US"}, nil, "", headers)
			configurationHTTPStatus(t, response, http.StatusNoContent)
			if admitted.Context().Err() != nil || !errors.Is(context.Cause(namedAdmitted.Context()), settings.ErrServerPolicyDenied) {
				t.Fatal("policy admission canceled the bounded core response or exempted a named operation")
			}
			if !errors.Is(context.Cause(operation), settings.ErrServerPolicyDenied) ||
				f.app.settings.Snapshot().Effective.ServerName != "Committed before remote revocation" ||
				f.app.serverPolicy.Snapshot().Values.EnableRemoteAccess {
				t.Fatal("self-disconnecting core write failed to publish managed state before revocation")
			}
			expectStatus(t, serverPolicyHTTPRequest(t, f.serverFixture, peer, http.MethodGet,
				"/emby/System/Configuration", nil, nil, "", headers), http.StatusForbidden)
			local := configurationHTTPObject(t, serverPolicyHTTPRequest(t, f.serverFixture, "127.0.0.1:12345", http.MethodGet,
				"/emby/System/Configuration", nil, nil, "", headers))
			if local["EnableRemoteAccess"] != false || local["UICulture"] != "en-US" {
				t.Fatal("the committed core policy was not readable from an allowed address")
			}
		})
	}
}

type configurationPausedBody struct {
	reader  io.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (body *configurationPausedBody) Read(data []byte) (int, error) {
	body.once.Do(func() {
		close(body.entered)
		<-body.release
	})
	return body.reader.Read(data)
}

func TestHTTPConfigurationPolicyRechecksLatestBoundaryAfterBodyWait(t *testing.T) {
	for _, includePolicy := range []bool{false, true} {
		name := "managed-only"
		body := `{"ServerName":"Admitted before the policy changed"}`
		if includePolicy {
			name = "attempted-reopen"
			body = `{"ServerName":"Admitted before the policy changed","EnableRemoteAccess":true}`
		}
		t.Run(name, func(t *testing.T) {
			f := newConfigurationHTTPFixture(t)
			before := f.app.settings.Snapshot()
			rowsBefore := adminSettingsHTTPRow(t, f.serverFixture)
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			request := httptest.NewRequest(http.MethodPost, "/emby/System/Configuration/Partial", nil).WithContext(f.ctx)
			request.RemoteAddr = "203.0.113.17:12345"
			request.Header.Set("Content-Type", "application/json")
			request.Body = io.NopCloser(&configurationPausedBody{reader: strings.NewReader(body), entered: entered, release: release})
			for header, values := range f.admin.headers {
				for _, value := range values {
					request.Header.Add(header, value)
				}
			}
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				f.handler.ServeHTTP(response, request)
			}()
			select {
			case <-entered:
			case <-f.ctx.Done():
				t.Fatal("the authorized core configuration request did not reach its body read")
			}
			native, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
			if err != nil {
				t.Fatal(err)
			}
			policy := f.app.serverPolicy.Snapshot()
			policy.Values.EnableRemoteAccess = false
			if _, err := f.app.serverPolicy.Update(f.ctx, settings.Actor{Principal: native, Audience: identity.AdministratorNative},
				settings.ServerPolicyUpdate{Revision: policy.Revision, Values: policy.Values, ClientAddress: "127.0.0.1"}); err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case <-done:
			case <-f.ctx.Done():
				t.Fatal("the core configuration request did not finish after its body resumed")
			}
			configurationHTTPStatus(t, response, http.StatusForbidden)
			if !reflect.DeepEqual(f.app.settings.Snapshot(), before) || adminSettingsHTTPRow(t, f.serverFixture) != rowsBefore ||
				f.app.serverPolicy.Snapshot().Values.EnableRemoteAccess {
				t.Fatal("a previously admitted but now denied peer changed settings or reopened remote access")
			}
		})
	}
}
