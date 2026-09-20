//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
)

// These tests open real listeners and perform real native authentication. The
// explicit remote acceptance gate keeps them out of ordinary local unit runs.
func requireHTTPBindingIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("GOBY_TEST_HTTP_BINDING") != "1" {
		t.Skip("GOBY_TEST_HTTP_BINDING=1 is required for remote HTTP binding acceptance")
	}
}

type httpBindingIntegrationApplication struct {
	app      *Server
	server   *http.Server
	listener net.Listener
	startup  HTTPBindingStartup
	done     chan error
	close    sync.Once
	closeErr error
}

func (running *httpBindingIntegrationApplication) stop() error {
	running.close.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if running.server != nil {
			running.closeErr = errors.Join(running.closeErr, running.server.Shutdown(ctx), running.server.Close())
		}
		if running.listener != nil {
			if err := running.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				running.closeErr = errors.Join(running.closeErr, err)
			}
		}
		if running.done != nil {
			select {
			case err := <-running.done:
				if !errors.Is(err, http.ErrServerClosed) {
					running.closeErr = errors.Join(running.closeErr, err)
				}
			case <-ctx.Done():
				running.closeErr = errors.Join(running.closeErr, ctx.Err())
			}
		}
		running.app.WithdrawHTTPBinding()
		running.closeErr = errors.Join(running.closeErr, running.app.Close(ctx))
	})
	return running.closeErr
}

func startHTTPBindingIntegration(t *testing.T, f *serverFixture, cfg config.Config) (*httpBindingIntegrationApplication, error) {
	t.Helper()
	app, err := New(f.ctx, cfg, f.pool, f.users, f.log, "http-binding-integration")
	if err != nil {
		t.Fatal("initialize the owned HTTP binding application")
	}
	running := &httpBindingIntegrationApplication{app: app}
	t.Cleanup(func() {
		if err := running.stop(); err != nil {
			t.Error("close and join the owned HTTP binding application")
		}
	})
	running.startup, err = app.StartupHTTPBinding()
	if err != nil {
		t.Fatal("freeze the owned HTTP startup binding")
	}
	listen := net.ListenConfig{}
	running.listener, err = listen.Listen(f.ctx, "tcp", running.startup.Address())
	if err != nil {
		return running, err
	}
	if err := app.PublishHTTPBinding(running.listener.Addr(), running.startup); err != nil {
		t.Fatal("publish the owned HTTP reservation")
	}
	running.server = &http.Server{Addr: running.startup.Address(), Handler: app.Handler(),
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context { return f.ctx }, ConnContext: app.HTTPConnectionContext}
	running.done = make(chan error, 1)
	go func() { running.done <- running.server.Serve(running.listener) }()
	return running, nil
}

func newHTTPBindingIntegrationClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal("create the native authentication cookie jar")
	}
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Jar: jar, Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func httpBindingIntegrationRequest(t *testing.T, client *http.Client, base, method, path string, body any, headers http.Header, status int) map[string]any {
	t.Helper()
	var content io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal("encode the native HTTP binding request")
		}
		content = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, base+path, content)
	if err != nil {
		t.Fatal("create the native HTTP binding request")
	}
	request.Header.Set("Origin", base)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, values := range headers {
		request.Header[name] = append([]string(nil), values...)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("complete the native request over the owned TCP listener")
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("native TCP response status = %d, want %d", response.StatusCode, status)
	}
	var object map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&object); err != nil || object == nil {
		t.Fatal("decode the bounded native TCP response")
	}
	return object
}

func httpBindingIntegrationLogin(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	response := httpBindingIntegrationRequest(t, client, base, http.MethodPost, "/admin/v1/session",
		map[string]any{"Name": "Administrator", "Password": "administrator-password"}, nil, http.StatusOK)
	return stringValue(t, response, "CSRFToken")
}

func httpBindingIntegrationNetwork(t *testing.T, object map[string]any) map[string]any {
	t.Helper()
	return objectValue(t, objectValue(t, object, "Runtime"), "Network")
}

func TestHTTPBindingActualRestartAuthenticatesAndPreservesPendingChoice(t *testing.T) {
	requireHTTPBindingIntegration(t)
	f := newServerFixtureWithTimeout(t, 90*time.Second)
	f.bootstrap(t)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("retire the fixture's unserved application before its real listener journey")
	}
	cfg := f.cfg
	cfg.ListenAddress, cfg.CookieSecure = "127.0.0.1:0", false
	first, err := startHTTPBindingIntegration(t, f, cfg)
	if err != nil {
		t.Fatal("reserve the initial ephemeral loopback listener")
	}
	client := newHTTPBindingIntegrationClient(t)
	base := "http://" + first.listener.Addr().String()
	csrf := httpBindingIntegrationLogin(t, client, base)
	current := httpBindingIntegrationRequest(t, client, base, http.MethodGet, "/admin/v1/settings", nil, nil, http.StatusOK)
	initial := httpBindingIntegrationNetwork(t, current)
	if initial["RestartRequired"] != false || objectValue(t, initial, "Active")["HttpPort"] != float64(first.listener.Addr().(*net.TCPAddr).Port) {
		t.Fatal("the initial native response did not report its actual reservation")
	}
	guard, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve the independent occupied target endpoint")
	}
	t.Cleanup(func() { _ = guard.Close() })
	nextPort := guard.Addr().(*net.TCPAddr).Port
	desired := settings.NetworkValues{BindHost: "127.0.0.1", HttpPort: nextPort}
	update := func(network any) map[string]any {
		body := adminSettingsHTTPUpdate(stringValue(t, current, "Revision"), nil, nil)
		body["Runtime"] = map[string]any{"Network": network}
		current = httpBindingIntegrationRequest(t, client, base, http.MethodPut, "/admin/v1/settings", body,
			http.Header{"X-CSRF-Token": {csrf}}, http.StatusOK)
		return httpBindingIntegrationNetwork(t, current)
	}
	pending := update(desired)
	if pending["RestartRequired"] != true || objectValue(t, pending, "Desired")["HttpPort"] != float64(nextPort) ||
		objectValue(t, pending, "Active")["HttpPort"] != objectValue(t, initial, "Active")["HttpPort"] {
		t.Fatal("saving an occupied future endpoint changed the live listener or omitted pending state")
	}
	if reverted := update(nil); reverted["RestartRequired"] != false {
		t.Fatal("resetting the network override did not revert to the configured active binding")
	}
	if restored := update(desired); restored["RestartRequired"] != true {
		t.Fatal("resaving the desired endpoint did not restore pending restart")
	}
	_ = httpBindingIntegrationRequest(t, client, base, http.MethodGet, "/admin/v1/session", nil, nil, http.StatusOK)
	if err := first.stop(); err != nil {
		t.Fatal("close and join the first listener and application")
	}
	failed, err := startHTTPBindingIntegration(t, f, cfg)
	if err == nil || failed.listener != nil || failed.startup.Binding != desired || failed.app.ManagedHTTPBindingState(desired).Active != nil {
		t.Fatal("occupied desired startup silently fell back or published a nonexistent listener")
	}
	if err := failed.stop(); err != nil {
		t.Fatal("close and join the failed startup application")
	}
	if err := guard.Close(); err != nil {
		t.Fatal("release the independent target reservation")
	}
	restarted, err := startHTTPBindingIntegration(t, f, cfg)
	if err != nil || restarted.startup.Binding != desired || restarted.listener.Addr().(*net.TCPAddr).Port != nextPort {
		t.Fatal("restarted application did not reserve the exact persisted desired endpoint")
	}
	newBase := "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(nextPort))
	csrf = httpBindingIntegrationLogin(t, client, newBase)
	current = httpBindingIntegrationRequest(t, client, newBase, http.MethodGet, "/admin/v1/settings", nil, nil, http.StatusOK)
	active := httpBindingIntegrationNetwork(t, current)
	if active["RestartRequired"] != false || objectValue(t, active, "Active")["HttpPort"] != float64(nextPort) || restarted.app.cfg.PublicURL != f.cfg.PublicURL {
		t.Fatal("restart did not clear pending state or modified deployment origin authority")
	}
	body := adminSettingsHTTPUpdate(stringValue(t, current, "Revision"), "Restarted HTTP binding", nil)
	current = httpBindingIntegrationRequest(t, client, newBase, http.MethodPut, "/admin/v1/settings", body,
		http.Header{"X-CSRF-Token": {csrf}}, http.StatusOK)
	if objectValue(t, current, "Effective")["ServerName"] != "Restarted HTTP binding" || restarted.app.settings.Snapshot().Runtime.DesiredNetwork != desired {
		t.Fatal("native CSRF write at the new endpoint failed to retain the network choice")
	}
	denied := httpBindingIntegrationRequest(t, client, newBase, http.MethodPost, "/admin/v1/session",
		map[string]any{"Name": "Administrator", "Password": "administrator-password"},
		http.Header{"Origin": {"http://forged.invalid:" + strconv.Itoa(nextPort)}, "X-Forwarded-Host": {"127.0.0.1:" + strconv.Itoa(nextPort)}, "X-Forwarded-Proto": {"http"}}, http.StatusForbidden)
	if objectValue(t, denied, "Error")["Code"] != "origin_denied" {
		t.Fatal("forged forwarded headers changed origin admission")
	}
	denied = httpBindingIntegrationRequest(t, client, newBase, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate(stringValue(t, current, "Revision"), "Unaccepted", nil), nil, http.StatusForbidden)
	if objectValue(t, denied, "Error")["Code"] != "csrf_invalid" {
		t.Fatal("direct socket origin bypassed native CSRF validation")
	}
	denied = httpBindingIntegrationRequest(t, client, newBase, http.MethodPost, "/admin/v1/session",
		map[string]any{"Name": "Administrator", "Password": "administrator-password"},
		http.Header{"Sec-Fetch-Site": {"cross-site"}}, http.StatusForbidden)
	if objectValue(t, denied, "Error")["Code"] != "origin_denied" {
		t.Fatal("direct socket origin bypassed fetch metadata validation")
	}
	if err := restarted.stop(); err != nil {
		t.Fatal("close and join the restarted application")
	}
	for _, address := range []string{first.listener.Addr().String(), restarted.listener.Addr().String()} {
		probe, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			_ = probe.Close()
			t.Fatal("a retired listener remained reachable after joined cleanup")
		}
	}
}

func TestHTTPBindingActualIPv6AuthenticatesAtObservedEndpoint(t *testing.T) {
	requireHTTPBindingIntegration(t)
	f := newServerFixture(t)
	f.bootstrap(t)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal("retire the unserved IPv6 fixture application")
	}
	cfg := f.cfg
	cfg.ListenAddress, cfg.CookieSecure = "[::1]:0", false
	running, err := startHTTPBindingIntegration(t, f, cfg)
	if err != nil {
		t.Fatal("reserve the required remote IPv6 loopback listener")
	}
	client := newHTTPBindingIntegrationClient(t)
	base := "http://" + running.listener.Addr().String()
	csrf := httpBindingIntegrationLogin(t, client, base)
	current := httpBindingIntegrationRequest(t, client, base, http.MethodGet, "/admin/v1/settings", nil, nil, http.StatusOK)
	_ = httpBindingIntegrationRequest(t, client, base, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate(stringValue(t, current, "Revision"), "IPv6 binding", nil),
		http.Header{"X-CSRF-Token": {csrf}}, http.StatusOK)
	state := running.app.ManagedHTTPBindingState(running.startup.Binding)
	if state.Active == nil || state.Active.BoundHost != "::1" || state.ReconnectURL != base || state.RestartRequired {
		t.Fatal("IPv6 active state or reconnect URL did not preserve the observed endpoint")
	}
	if err := running.stop(); err != nil {
		t.Fatal("close and join the IPv6 application")
	}
}
