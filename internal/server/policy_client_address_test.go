package server

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/moooyo/goby/internal/config"
)

func TestPolicyClientAddressPreservesProxyBoundary(t *testing.T) {
	s := &Server{cfg: config.Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8")}}}
	for _, test := range []struct{ name, peer, forwarded, want string }{
		{"direct-local", "192.168.1.7:1234", "", "192.168.1.7"},
		{"direct-remote-ignores-forgery", "198.51.100.7:1234", "127.0.0.1", "198.51.100.7"},
		{"trusted-proxy", "127.0.0.1:1234", "198.51.100.7", "198.51.100.7"},
		{"trusted-chain", "127.0.0.1:1234", "198.51.100.7, 10.0.0.1", "198.51.100.7"},
		{"untrusted-chain-boundary", "127.0.0.1:1234", "192.168.1.7, 198.51.100.7", "198.51.100.7"},
		{"missing-forwarded-is-unknown", "127.0.0.1:1234", "", ""},
		{"invalid-forwarded-is-unknown", "127.0.0.1:1234", "not-an-address", ""},
		{"invalid-peer-is-unknown", "not-an-address", "127.0.0.1", ""},
		{"mapped-ipv4", "[::ffff:198.51.100.7]:1234", "", "198.51.100.7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.peer
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			if got := s.policyClientAddress(request); got != test.want {
				t.Fatalf("client address = %q, want %q", got, test.want)
			}
		})
	}
}
