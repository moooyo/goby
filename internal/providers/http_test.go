package providers

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestProviderDialRejectsWholeDNSAnswer(t *testing.T) {
	for _, forbidden := range []string{"10.0.0.1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2001:db8::1"} {
		t.Run(forbidden, func(t *testing.T) {
			transport := newProviderHTTPTransportWithNetwork(func(_ context.Context, network, host string) ([]netip.Addr, error) {
				if network != "ip" || host != "api.themoviedb.org" {
					t.Errorf("lookup = %s %s", network, host)
				}
				return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr(forbidden)}, nil
			}, func(context.Context, string, string) (net.Conn, error) {
				t.Error("mixed public/private answer started a connection")
				return nil, errors.New("unexpected dial")
			})
			connection, err := transport.DialContext(context.Background(), "tcp", "api.themoviedb.org:443")
			if connection != nil || !errors.Is(err, ErrUnavailable) {
				t.Fatalf("connection = %v, error = %v", connection, err)
			}
		})
	}
}

func TestProviderDialRejectsHostBeforeLookup(t *testing.T) {
	transport := newProviderHTTPTransportWithNetwork(func(context.Context, string, string) ([]netip.Addr, error) {
		t.Error("unapproved host reached DNS")
		return nil, errors.New("unexpected lookup")
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Error("unapproved host reached dialer")
		return nil, errors.New("unexpected dial")
	})
	for _, address := range []string{"api.themoviedb.org.evil.example:443", "api.themoviedb.org:444", "1.1.1.1:443", "api.themoviedb.org"} {
		if connection, err := transport.DialContext(context.Background(), "tcp", address); connection != nil || !errors.Is(err, ErrUnavailable) {
			t.Fatalf("address = %s, connection = %v, error = %v", address, connection, err)
		}
	}
}

func TestProviderDialUsesValidatedLiteralFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		winner, peer := net.Pipe()
		defer winner.Close()
		defer peer.Close()
		start := time.Now()
		var attempts atomic.Int32
		transport := newProviderHTTPTransportWithNetwork(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("2606:4700:4700::1111")}, nil
		}, func(ctx context.Context, network, address string) (net.Conn, error) {
			attempts.Add(1)
			if network != "tcp" {
				t.Errorf("network = %q", network)
			}
			if address == "1.1.1.1:443" {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			if address != "[2606:4700:4700::1111]:443" {
				t.Errorf("unexpected dial = %q", address)
			}
			return winner, nil
		})
		connection, err := transport.DialContext(context.Background(), "tcp", "api.themoviedb.org:443")
		if err != nil || connection != winner || attempts.Load() != 2 || time.Since(start) != 250*time.Millisecond {
			t.Fatalf("connection = %v, error = %v, attempts = %d, elapsed = %v", connection, err, attempts.Load(), time.Since(start))
		}
	})
}

func TestProviderDialSharesDeadlineWithDNS(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var attempts atomic.Int32
		transport := newProviderHTTPTransportWithNetwork(func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
			if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(start.Add(10*time.Second)) {
				t.Errorf("DNS deadline = %v, present = %v", deadline, ok)
			}
			time.Sleep(9600 * time.Millisecond)
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("8.8.4.4")}, nil
		}, func(ctx context.Context, _, _ string) (net.Conn, error) {
			attempts.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		connection, err := transport.DialContext(context.Background(), "tcp", "api.themoviedb.org:443")
		if connection != nil || !errors.Is(err, context.DeadlineExceeded) || attempts.Load() != 2 || time.Since(start) != 10*time.Second {
			t.Fatalf("connection = %v, error = %v, attempts = %d, elapsed = %v", connection, err, attempts.Load(), time.Since(start))
		}
	})
}

func TestProviderRedirectRetainsOriginalHostAndBound(t *testing.T) {
	client := newHTTPClient()
	original, err := http.NewRequest(http.MethodPost, "https://api.opensubtitles.com/api/v1/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://vip-api.opensubtitles.com/api/v1/download", "http://api.opensubtitles.com/api/v1/download", "https://api.opensubtitles.com:444/api/v1/download"} {
		redirect, err := http.NewRequest(http.MethodPost, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CheckRedirect(redirect, []*http.Request{original}); err == nil {
			t.Errorf("accepted redirect %q", raw)
		}
	}
	if err := client.CheckRedirect(original, []*http.Request{original}); err != nil {
		t.Fatalf("same-host redirect rejected: %v", err)
	}
	if err := client.CheckRedirect(original, []*http.Request{original, original, original}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("redirect bound = %v", err)
	}
}
