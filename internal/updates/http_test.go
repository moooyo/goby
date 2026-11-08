package updates

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestProductionFeedDialRejectsPrivateMixedAndForeignDestinations(t *testing.T) {
	for _, test := range []struct {
		name, target string
		answers      []netip.Addr
		wantDial     bool
	}{
		{"public", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("93.184.216.34")}, true},
		{"private", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("10.0.0.1")}, false},
		{"mixed", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}, false},
		{"loopback-ipv6", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("::1")}, false},
		{"mapped-private", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("::ffff:192.168.1.1")}, false},
		{"metadata", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("169.254.169.254")}, false},
		{"reserved", "updates.example.com:443", []netip.Addr{netip.MustParseAddr("198.18.0.1")}, false},
		{"foreign-host", "different.example.com:443", []netip.Addr{netip.MustParseAddr("93.184.216.34")}, false},
		{"foreign-port", "updates.example.com:8443", []netip.Addr{netip.MustParseAddr("93.184.216.34")}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dialed := false
			transport := newFeedTransportWithNetwork("updates.example.com", func(ctx context.Context, network, host string) ([]netip.Addr, error) {
				if network != "ip" || host != "updates.example.com" {
					t.Error("resolver was given an unpinned destination")
				}
				return test.answers, nil
			}, func(ctx context.Context, network, address string) (net.Conn, error) {
				dialed = true
				if address != "93.184.216.34:443" {
					t.Error("dialer resolved an unchecked hostname or received the wrong literal")
				}
				return nil, errors.New("fixture dial failure")
			})
			if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify {
				t.Fatal("production transport delegates its security policy to a proxy or bypasses TLS")
			}
			connection, err := transport.DialContext(context.Background(), "tcp", test.target)
			if connection != nil || err == nil || dialed != test.wantDial {
				t.Fatal("production egress boundary did not match the authorized literal set")
			}
		})
	}
}
