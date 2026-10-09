package notifications

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type notificationDialConnection struct {
	net.Conn
	closed atomic.Int32
}

func (connection *notificationDialConnection) Close() error {
	connection.closed.Add(1)
	return nil
}

func TestWebhookDialValidatesWholeDNSAnswer(t *testing.T) {
	for _, test := range []struct {
		name      string
		addresses []netip.Addr
		networks  []string
		wantError error
	}{
		{name: "mixed private", addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("10.0.0.1")}, wantError: ErrInvalid},
		{name: "mapped loopback", addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("::ffff:127.0.0.1")}, wantError: ErrInvalid},
		{name: "link local despite override", addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("169.254.169.254")}, networks: []string{"0.0.0.0/0"}, wantError: ErrInvalid},
		{name: "empty answer", wantError: ErrUnavailable},
		{name: "answer count bound", addresses: make([]netip.Addr, 33), wantError: ErrUnavailable},
		{name: "explicit private override", addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}, networks: []string{"127.0.0.1/32"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			winner := &notificationDialConnection{}
			var attempts atomic.Int32
			transport, closeTransport, err := webhookTransportWithNetwork(target{endpoint: "https://receiver.example:8443/events", networks: test.networks}, RuntimeOptions{}, context.Background(), nil, func(_ context.Context, network, host string) ([]netip.Addr, error) {
				if network != "ip" || host != "receiver.example" {
					t.Errorf("lookup = %s %s", network, host)
				}
				return test.addresses, nil
			}, func(_ context.Context, network, address string) (net.Conn, error) {
				attempts.Add(1)
				if network != "tcp" || address != "127.0.0.1:8443" {
					t.Errorf("unexpected dial = %s %s", network, address)
				}
				return winner, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			defer closeTransport()
			connection, err := transport.DialContext(context.Background(), "tcp", "receiver.example:8443")
			if test.wantError != nil {
				if !errors.Is(err, test.wantError) || connection != nil || attempts.Load() != 0 {
					t.Fatalf("connection = %v, error = %v, attempts = %d", connection, err, attempts.Load())
				}
			} else {
				if err != nil || connection != winner || attempts.Load() != 1 {
					t.Fatalf("connection = %v, error = %v, attempts = %d", connection, err, attempts.Load())
				}
				_ = connection.Close()
			}
		})
	}
}

func TestWebhookDialRejectsChangedDestinationBeforeLookup(t *testing.T) {
	transport, closeTransport, err := webhookTransportWithNetwork(target{endpoint: "https://receiver.example/events"}, RuntimeOptions{}, context.Background(), nil, func(context.Context, string, string) ([]netip.Addr, error) {
		t.Error("changed destination reached DNS")
		return nil, errors.New("unexpected lookup")
	}, func(context.Context, string, string) (net.Conn, error) {
		t.Error("changed destination reached dialer")
		return nil, errors.New("unexpected dial")
	})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTransport()
	for _, address := range []string{"other.example:443", "receiver.example:444", "receiver.example"} {
		if connection, err := transport.DialContext(context.Background(), "tcp", address); connection != nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("address = %s, connection = %v, error = %v", address, connection, err)
		}
	}
}

func TestWebhookDialSharesDeadlineWithDNS(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var attempts atomic.Int32
		transport, closeTransport, err := webhookTransportWithNetwork(target{endpoint: "https://receiver.example/events"}, RuntimeOptions{}, context.Background(), nil, func(ctx context.Context, _, _ string) ([]netip.Addr, error) {
			if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(start.Add(3*time.Second)) {
				t.Errorf("DNS deadline = %v, present = %v", deadline, ok)
			}
			time.Sleep(2600 * time.Millisecond)
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("8.8.4.4")}, nil
		}, func(ctx context.Context, _, _ string) (net.Conn, error) {
			attempts.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		defer closeTransport()
		connection, err := transport.DialContext(context.Background(), "tcp", "receiver.example:443")
		if connection != nil || !errors.Is(err, ErrUnavailable) || attempts.Load() != 2 || time.Since(start) != 3*time.Second {
			t.Fatalf("connection = %v, error = %v, attempts = %d, elapsed = %v", connection, err, attempts.Load(), time.Since(start))
		}
	})
}

func TestWebhookTransportCloseJoinsDialAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var attempts, canceled, active, authorized atomic.Int32
		late := make(chan *notificationDialConnection, 2)
		transport, closeTransport, err := webhookTransportWithNetwork(target{endpoint: "https://receiver.example/events"}, RuntimeOptions{}, context.Background(), func(context.Context) error {
			authorized.Add(1)
			return nil
		}, func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("8.8.8.8")}, nil
		}, func(ctx context.Context, _, _ string) (net.Conn, error) {
			attempts.Add(1)
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			canceled.Add(1)
			<-release
			connection := &notificationDialConnection{}
			late <- connection
			return connection, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		dialed := make(chan error, 1)
		go func() {
			connection, err := transport.DialTLSContext(context.Background(), "tcp", "receiver.example:443")
			if connection != nil {
				t.Error("canceled attempt returned a TLS connection")
				_ = connection.Close()
			}
			dialed <- err
		}()
		synctest.Wait()
		time.Sleep(250 * time.Millisecond)
		synctest.Wait()
		if attempts.Load() != 2 {
			t.Fatalf("attempts = %d", attempts.Load())
		}
		closed := make(chan struct{})
		go func() {
			closeTransport()
			close(closed)
		}()
		synctest.Wait()
		if canceled.Load() != 2 || active.Load() != 2 {
			t.Fatalf("canceled = %d, active = %d", canceled.Load(), active.Load())
		}
		select {
		case <-closed:
			t.Fatal("transport closed before dial attempts retired")
		default:
		}
		close(release)
		if err := <-dialed; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("dial error = %v", err)
		}
		<-closed
		if active.Load() != 0 || authorized.Load() != 0 {
			t.Fatalf("active = %d, authorized = %d", active.Load(), authorized.Load())
		}
		for range 2 {
			if connection := <-late; connection.closed.Load() != 1 {
				t.Fatalf("late connection closes = %d", connection.closed.Load())
			}
		}
		if connection, err := transport.DialTLSContext(context.Background(), "tcp", "receiver.example:443"); connection != nil || !errors.Is(err, context.Canceled) || attempts.Load() != 2 {
			t.Fatalf("closed transport dial = %v, %v, attempts = %d", connection, err, attempts.Load())
		}
		closeTransport()
	})
}
