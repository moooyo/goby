package server

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/moooyo/goby/internal/config"
)

func TestClientAddressTrustBoundary(t *testing.T) {
	cases := []struct {
		name, peer, forwarded, want string
		trusted                     []string
	}{
		{"direct", "198.51.100.10:4321", "", "198.51.100.10", nil},
		{"untrusted spoof", "198.51.100.10:4321", "192.0.2.99", "198.51.100.10", nil},
		{"loopback proxy", "127.0.0.1:4321", "198.51.100.10", "198.51.100.10", []string{"127.0.0.1/32"}},
		{"left spoof ignored", "127.0.0.1:4321", "192.0.2.99, 198.51.100.10", "198.51.100.10", []string{"127.0.0.1/32"}},
		{"two trusted proxies", "127.0.0.1:4321", "198.51.100.10, 10.0.0.2", "198.51.100.10", []string{"127.0.0.1/32", "10.0.0.0/24"}},
		{"malformed immediate forwarded value", "127.0.0.1:4321", "bad-ip", "127.0.0.1", []string{"127.0.0.1/32"}},
		{"IPv6", "[::1]:4321", "2001:db8::10", "2001:db8::10", []string{"::1/128"}},
		{"mapped IPv4", "[::ffff:127.0.0.1]:4321", "198.51.100.10", "198.51.100.10", []string{"127.0.0.1/32"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{}
			for _, cidr := range tc.trusted {
				cfg.TrustedProxies = append(cfg.TrustedProxies, netip.MustParsePrefix(cidr))
			}
			s := Server{cfg: cfg}
			r := httptest.NewRequest("POST", "/admin/v1/session", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			if got := s.clientAddress(r); got != tc.want {
				t.Fatalf("clientAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTrustedProxyClientsHaveIndependentLoginLimits(t *testing.T) {
	s := Server{cfg: config.Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}}, limiter: newLoginLimiter()}
	request := httptest.NewRequest("POST", "/admin/v1/session", nil)
	request.RemoteAddr = "127.0.0.1:9876"
	request.Header.Set("X-Forwarded-For", "198.51.100.1")
	for i := 0; i < 10; i++ {
		if !s.allowLogin(httptest.NewRecorder(), request) {
			t.Fatal("allowed attempts exhausted early")
		}
	}
	if s.allowLogin(httptest.NewRecorder(), request) {
		t.Fatal("abusive client was not limited")
	}
	request.Header.Set("X-Forwarded-For", "198.51.100.2")
	if !s.allowLogin(httptest.NewRecorder(), request) {
		t.Fatal("independent client was blocked by the first client's quota")
	}
}
