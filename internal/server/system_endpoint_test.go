package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/moooyo/goby/internal/config"
)

func TestSystemEndpointClassifiesResolvedConnectionWithoutGrantingTrust(t *testing.T) {
	for _, tc := range []struct {
		name, peer, forwarded, local string
		trusted                      []string
		want                         systemEndpointInfo
	}{
		{"loopback", "127.0.0.1:1234", "", "", nil, systemEndpointInfo{true, true}},
		{"ipv6 loopback", "[::1]:1234", "", "", nil, systemEndpointInfo{true, true}},
		{"mapped loopback", "[::ffff:127.0.0.1]:1234", "", "", nil, systemEndpointInfo{true, true}},
		{"private peer", "10.8.0.2:1234", "", "", nil, systemEndpointInfo{false, true}},
		{"ula peer", "[fd00::2]:1234", "", "", nil, systemEndpointInfo{false, true}},
		{"link local", "169.254.10.2:1234", "", "", nil, systemEndpointInfo{false, true}},
		{"public peer", "203.0.113.8:1234", "", "", nil, systemEndpointInfo{}},
		{"same server address", "203.0.113.8:1234", "", "203.0.113.8:8096", nil, systemEndpointInfo{true, true}},
		{"wildcard listener is not a local identity", "203.0.113.8:1234", "", "0.0.0.0:8096", nil, systemEndpointInfo{}},
		{"untrusted forwarding cannot claim loopback", "203.0.113.8:1234", "127.0.0.1", "", nil, systemEndpointInfo{}},
		{"trusted proxy preserves external client", "127.0.0.1:1234", "203.0.113.8", "127.0.0.1:8096", []string{"127.0.0.1/32"}, systemEndpointInfo{}},
		{"trusted proxy preserves private client", "127.0.0.1:1234", "192.168.2.20", "127.0.0.1:8096", []string{"127.0.0.1/32"}, systemEndpointInfo{false, true}},
		{"invalid peer", "invalid", "127.0.0.1", "", nil, systemEndpointInfo{}},
		{"unspecified peer", "0.0.0.0:1234", "", "", nil, systemEndpointInfo{}},
		{"multicast peer", "[ff02::1]:1234", "", "", nil, systemEndpointInfo{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{}
			for _, prefix := range tc.trusted {
				cfg.TrustedProxies = append(cfg.TrustedProxies, netip.MustParsePrefix(prefix))
			}
			s := Server{cfg: cfg}
			r := httptest.NewRequest(http.MethodGet, "/emby/System/Endpoint?IsLocal=true&IsInNetwork=true", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			r.Header.Set("Forwarded", "for=127.0.0.1")
			r.Header.Set("X-Real-IP", "127.0.0.1")
			if tc.local != "" {
				address, err := net.ResolveTCPAddr("tcp", tc.local)
				if err != nil {
					t.Fatal(err)
				}
				r = r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, address))
			}
			if got := s.endpointInfo(r); got != tc.want {
				t.Fatalf("endpoint classification = %#v, want %#v", got, tc.want)
			}
		})
	}
}
