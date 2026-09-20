//go:build linux

package server

import (
	"net"
	"net/http"
	"testing"
)

// This is a projection test with a synthetic reservation receipt. The separate
// HTTP binding journey owns evidence from real listeners and authenticated I/O.
func TestHTTPSystemInfoSeparatesObservedPortDesiredPortAndPublicIdentity(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	admin := jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, f.admin.headers))
	if _, exists := admin["HttpServerPortNumber"]; exists {
		t.Fatal("unobserved listener was projected as an active port")
	}
	for _, name := range []string{"CanSelfRestart", "CanSelfUpdate", "SupportsHttps"} {
		if admin[name] != false {
			t.Fatalf("unsupported process capability %s was advertised", name)
		}
	}
	if admin["SupportsLocalPortConfiguration"] != true {
		t.Fatal("managed port configuration capability was omitted")
	}
	startup, err := f.app.StartupHTTPBinding()
	if err != nil {
		t.Fatal(err)
	}
	port := startup.Binding.HttpPort
	if port == 0 {
		port = 18096
	}
	address := net.ParseIP(startup.Binding.BindHost)
	if address == nil || address.IsUnspecified() {
		address = net.ParseIP("127.0.0.1")
	}
	if err := f.app.PublishHTTPBinding(&net.TCPAddr{IP: address, Port: port}, startup); err != nil {
		t.Fatal(err)
	}
	admin = jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, f.admin.headers))
	if admin["HttpServerPortNumber"] != float64(port) || admin["HasPendingRestart"] != false {
		t.Fatal("observed listener state differs from its successful reservation")
	}
	next := port + 1
	if next > 65535 {
		next = port - 1
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial",
		map[string]any{"HttpServerPortNumber": next}, f.admin.headers), http.StatusNoContent)
	configured := configurationHTTPObject(t, f.request(t, http.MethodGet, "/System/Configuration", nil, f.admin.headers))
	admin = jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, f.admin.headers))
	if configured["HttpServerPortNumber"] != float64(next) || admin["HttpServerPortNumber"] != float64(port) || admin["HasPendingRestart"] != true {
		t.Fatal("saved binding was confused with the running listener")
	}
	if admin["LocalAddress"] != f.cfg.PublicURL {
		t.Fatal("listener override rewrote the advertised deployment origin")
	}
	viewer := jsonObject(t, f.request(t, http.MethodGet, "/System/Info", nil, f.viewer.headers))
	public := jsonObject(t, f.request(t, http.MethodGet, "/System/Info/Public", nil, nil))
	if len(public) != 7 {
		t.Fatal("public discovery exposed management capability or binding details")
	}
	for _, name := range []string{"HttpServerPortNumber", "HasPendingRestart"} {
		if _, exists := viewer[name]; exists {
			t.Fatalf("viewer received private management field %s", name)
		}
		if _, exists := public[name]; exists {
			t.Fatalf("public discovery received private management field %s", name)
		}
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial",
		map[string]any{"HttpServerPortNumber": port}, f.viewer.headers), http.StatusForbidden)
}
