package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
)

func httpBindingTestServer(t *testing.T, address string, observed *net.TCPAddr) (*Server, HTTPBindingStartup) {
	t.Helper()
	s := &Server{cfg: config.Config{ListenAddress: address, PublicURL: "https://media.example.test"}}
	startup, err := s.StartupHTTPBinding()
	if err != nil {
		t.Fatal("freeze HTTP startup binding")
	}
	if observed != nil {
		if err := s.PublishHTTPBinding(observed, startup); err != nil {
			t.Fatal("publish observed TCP binding")
		}
	}
	return s, startup
}

func TestManagedHTTPBindingPendingRevertAndRestartUseConfiguredChoice(t *testing.T) {
	s, startup := httpBindingTestServer(t, "127.0.0.1:0", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123})
	initial := s.ManagedHTTPBindingState(startup.Binding)
	if initial.Active == nil || initial.Active.HttpPort != 32123 || initial.Active.Configured.HttpPort != 0 || initial.RestartRequired || initial.ReconnectURL != "http://127.0.0.1:32123" {
		t.Fatal("ephemeral deployment binding was confused with a pending managed port change")
	}
	desired := settings.NetworkValues{BindHost: "127.0.0.1", HttpPort: 32124}
	pending := s.ManagedHTTPBindingState(desired)
	if !pending.RestartRequired || !reflect.DeepEqual(initial.Active, pending.Active) || pending.ReconnectURL != "http://127.0.0.1:32124" {
		t.Fatal("a desired change rewrote the running observation or omitted pending restart")
	}
	if reverted := s.ManagedHTTPBindingState(startup.Binding); !reflect.DeepEqual(initial, reverted) {
		t.Fatal("reverting to the configured running binding did not clear pending restart")
	}
	restarted, nextStartup := httpBindingTestServer(t, "127.0.0.1:32124", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32124})
	if next := restarted.ManagedHTTPBindingState(nextStartup.Binding); next.RestartRequired || next.Active == nil || next.Active.HttpPort != 32124 {
		t.Fatal("the next generation did not report its successfully reserved desired port")
	}
	pending.Active.HttpPort = 1
	if current := s.ManagedHTTPBindingState(desired); current.Active == nil || current.Active.HttpPort != 32123 {
		t.Fatal("a caller mutated the active binding through its detached projection")
	}
}

func TestHTTPBindingStartupFreezesReceiptAndWithdrawPreventsReuse(t *testing.T) {
	s, startup := httpBindingTestServer(t, "127.0.0.1:32123", nil)
	s.cfg.ListenAddress = "127.0.0.1:32124"
	if next, err := s.StartupHTTPBinding(); err != nil || next != startup || next.Address() != "127.0.0.1:32123" {
		t.Fatal("startup binding followed a later mutable deployment value")
	}
	observed := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}
	if err := s.PublishHTTPBinding(observed, startup); err != nil {
		t.Fatal("publish the frozen reservation")
	}
	if err := s.PublishHTTPBinding(observed, startup); err == nil {
		t.Fatal("one generation published multiple reservations")
	}
	s.WithdrawHTTPBinding()
	if state := s.ManagedHTTPBindingState(startup.Binding); state.Active != nil || state.RestartRequired {
		t.Fatal("a closed reservation remains active")
	}
	if err := s.PublishHTTPBinding(observed, startup); err == nil {
		t.Fatal("a retired generation was reused")
	}
	if _, err := s.StartupHTTPBinding(); err == nil {
		t.Fatal("a retired generation still supplied a reusable startup receipt")
	}
	unpublished, otherStartup := httpBindingTestServer(t, "127.0.0.1:32123", nil)
	unpublished.WithdrawHTTPBinding()
	if err := unpublished.PublishHTTPBinding(observed, otherStartup); err == nil {
		t.Fatal("an unpublished retired generation was reused")
	}
}

func TestHTTPBindingPublicationRejectsUnrelatedReservations(t *testing.T) {
	for _, test := range []struct {
		name   string
		addr   net.Addr
		change func(*HTTPBindingStartup)
	}{
		{name: "nil"},
		{name: "unix", addr: &net.UnixAddr{Name: "unused", Net: "unix"}},
		{name: "wrong-port", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32124}},
		{name: "wrong-host", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2"), Port: 32123}},
		{name: "zero-port", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}},
		{name: "zone", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123, Zone: "eth0"}},
		{name: "changed-revision", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}, change: func(value *HTTPBindingStartup) { value.Revision++ }},
		{name: "changed-choice", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}, change: func(value *HTTPBindingStartup) { value.Binding.HttpPort++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, startup := httpBindingTestServer(t, "127.0.0.1:32123", nil)
			if test.change != nil {
				test.change(&startup)
			}
			if err := s.PublishHTTPBinding(test.addr, startup); err == nil || s.ManagedHTTPBindingState(startup.Binding).Active != nil {
				t.Fatal("an invalid listener publication produced active state")
			}
		})
	}
	s := &Server{}
	if err := s.PublishHTTPBinding(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}, HTTPBindingStartup{}); err == nil {
		t.Fatal("publication without a frozen startup receipt was accepted")
	}
}

func TestHTTPBindingReconnectNeverInventsWildcardOrHostnameBrowserURLs(t *testing.T) {
	s := &Server{}
	for _, host := range []string{"", "0.0.0.0", "::", "::ffff:0.0.0.0", "media.example.test", "localhost", "fe80::1%eth0"} {
		if state := s.ManagedHTTPBindingState(settings.NetworkValues{BindHost: host, HttpPort: 8096}); state.ReconnectURL != "" || state.Active != nil {
			t.Fatal("an unproved browser host or active binding was projected")
		}
	}
	if got := s.ManagedHTTPBindingState(settings.NetworkValues{BindHost: "2001:db8::1", HttpPort: 8096}).ReconnectURL; got != "http://[2001:db8::1]:8096" {
		t.Fatal("the explicit IPv6 reconnect address is not bracketed")
	}
	if got := s.ManagedHTTPBindingState(settings.NetworkValues{BindHost: "::ffff:127.0.0.1", HttpPort: 8096}).ReconnectURL; got != "http://127.0.0.1:8096" {
		t.Fatal("the mapped IPv4 reconnect address did not use its canonical browser host")
	}
}

type httpBindingTestConnection struct{ local net.Addr }

func (c httpBindingTestConnection) LocalAddr() net.Addr              { return c.local }
func (c httpBindingTestConnection) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c httpBindingTestConnection) Read([]byte) (int, error)         { return 0, net.ErrClosed }
func (c httpBindingTestConnection) Write([]byte) (int, error)        { return 0, net.ErrClosed }
func (c httpBindingTestConnection) Close() error                     { return nil }
func (c httpBindingTestConnection) SetDeadline(time.Time) error      { return nil }
func (c httpBindingTestConnection) SetReadDeadline(time.Time) error  { return nil }
func (c httpBindingTestConnection) SetWriteDeadline(time.Time) error { return nil }

func TestHTTPDirectOriginUsesAcceptedSocketAndStrictOriginSyntax(t *testing.T) {
	for _, test := range []struct {
		name, binding, local, origin string
		port                         int
		allowed                      bool
	}{
		{"ipv4", ":32123", "192.0.2.10", "http://192.0.2.10:32123", 32123, true},
		{"ipv6", "[::]:32123", "2001:db8::1", "http://[2001:db8::1]:32123", 32123, true},
		{"mapped-ipv4", ":32123", "127.0.0.1", "http://[::ffff:7f00:1]:32123", 32123, true},
		{"localhost-ipv4", ":32123", "127.0.0.1", "http://localhost:32123", 32123, true},
		{"localhost-ipv6", "[::]:32123", "::1", "http://LOCALHOST:32123", 32123, true},
		{"default-http-port", ":80", "127.0.0.1", "http://127.0.0.1", 80, true},
		{"wrong-address", ":32123", "192.0.2.10", "http://192.0.2.11:32123", 32123, false},
		{"wrong-port", ":32123", "192.0.2.10", "http://192.0.2.10:32124", 32123, false},
		{"hostname", ":32123", "192.0.2.10", "http://media.example.test:32123", 32123, false},
		{"localhost-nonloopback", ":32123", "192.0.2.10", "http://localhost:32123", 32123, false},
		{"wildcard", ":32123", "192.0.2.10", "http://0.0.0.0:32123", 32123, false},
		{"https-is-not-http", ":32123", "127.0.0.1", "https://127.0.0.1:32123", 32123, false},
		{"credentials", ":32123", "127.0.0.1", "http://user@127.0.0.1:32123", 32123, false},
		{"path", ":32123", "127.0.0.1", "http://127.0.0.1:32123/", 32123, false},
		{"empty-query", ":32123", "127.0.0.1", "http://127.0.0.1:32123?", 32123, false},
		{"empty-fragment", ":32123", "127.0.0.1", "http://127.0.0.1:32123#", 32123, false},
		{"empty-port", ":80", "127.0.0.1", "http://127.0.0.1:", 80, false},
		{"noncanonical-port", ":32123", "127.0.0.1", "http://127.0.0.1:032123", 32123, false},
		{"ipv6-zone", "[::]:32123", "::1", "http://[::1%25lo]:32123", 32123, false},
		{"null", ":32123", "127.0.0.1", "null", 32123, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			boundIP := net.IPv4zero
			if test.binding[0] == '[' {
				boundIP = net.IPv6zero
			}
			s, _ := httpBindingTestServer(t, test.binding, &net.TCPAddr{IP: boundIP, Port: test.port})
			ctx := s.HTTPConnectionContext(context.Background(), httpBindingTestConnection{local: &net.TCPAddr{IP: net.ParseIP(test.local), Port: test.port}})
			r := httptest.NewRequest(http.MethodPost, "/admin/v1/session", nil).WithContext(ctx)
			r.Header.Set("Origin", test.origin)
			w := httptest.NewRecorder()
			if got := s.sameOrigin(w, r); got != test.allowed {
				t.Fatal("direct HTTP origin admission disagrees with the accepted socket")
			}
			if !test.allowed && w.Code != http.StatusForbidden {
				t.Fatal("invalid origin lost the stable forbidden response")
			}
		})
	}
}

func TestHTTPDirectOriginCannotBeGrantedByHeadersOrForeignContexts(t *testing.T) {
	s, startup := httpBindingTestServer(t, "127.0.0.1:32123", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123})
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:32123/admin/v1/session", nil)
	r.Host = "127.0.0.1:32123"
	r.Header.Set("Origin", "http://127.0.0.1:32123")
	r.Header.Set("Forwarded", "host=127.0.0.1:32123;proto=http")
	r.Header.Set("X-Forwarded-Host", "127.0.0.1:32123")
	r.Header.Set("X-Forwarded-Port", "32123")
	r.Header.Set("X-Forwarded-Proto", "http")
	r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}))
	if s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("request headers or an unowned standard context value granted direct origin authority")
	}
	other, _ := httpBindingTestServer(t, startup.Address(), &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123})
	ctx := other.HTTPConnectionContext(r.Context(), httpBindingTestConnection{local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}})
	if s.sameOrigin(httptest.NewRecorder(), r.WithContext(ctx)) {
		t.Fatal("another application generation granted direct origin authority")
	}
	ctx = s.HTTPConnectionContext(r.Context(), httpBindingTestConnection{local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123}})
	r = r.WithContext(ctx)
	if !s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("the owned accepted connection did not grant direct origin authority")
	}
	s.WithdrawHTTPBinding()
	if s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("a withdrawn generation retained direct origin authority")
	}
}

func TestHTTPOriginKeepsDeploymentAuthorityAndFetchMetadataChecks(t *testing.T) {
	s, _ := httpBindingTestServer(t, "127.0.0.1:32123", nil)
	r := httptest.NewRequest(http.MethodPost, "/admin/v1/session", nil)
	r.Header.Set("Origin", s.cfg.PublicURL)
	if !s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("the configured reverse-proxy origin lost authority")
	}
	r.Header.Add("Origin", s.cfg.PublicURL)
	if s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("multiple origin fields were accepted")
	}
	r.Header.Set("Origin", s.cfg.PublicURL)
	r.Header["Sec-Fetch-Site"] = []string{"same-origin", "cross-site"}
	if s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("a later cross-site fetch metadata field was ignored")
	}
	r.Header.Del("Sec-Fetch-Site")
	r.Header.Del("Origin")
	if !s.sameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("an established nonbrowser request without Origin changed behavior")
	}
}

func TestHTTPConnectionContextRejectsUnobservedOrNonTCPAddresses(t *testing.T) {
	s, _ := httpBindingTestServer(t, "127.0.0.1:32123", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32123})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, connection := range []net.Conn{nil,
		httpBindingTestConnection{local: &net.UnixAddr{Name: "unused", Net: "unix"}},
		httpBindingTestConnection{local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 32124}},
		httpBindingTestConnection{local: &net.TCPAddr{IP: net.ParseIP("127.0.0.2"), Port: 32123}},
		httpBindingTestConnection{local: &net.TCPAddr{IP: net.IPv4zero, Port: 32123}},
	} {
		got := s.HTTPConnectionContext(ctx, connection)
		if got.Value(httpConnectionEndpointKey{}) != nil || !errors.Is(got.Err(), context.Canceled) {
			t.Fatal("invalid connection supplied origin authority or lost its lifetime context")
		}
	}
}
