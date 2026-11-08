package notifications

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

type adminSMTPFixtureOptions struct {
	security        string
	advertiseTLS    bool
	mechanisms      string
	rejectAuth      bool
	rejectRecipient bool
	dropAfterData   bool
}

type adminSMTPFixture struct {
	connection    net.Conn
	listener      net.Listener
	roots         *x509.CertPool
	done          chan struct{}
	errors        chan error
	authAttempts  atomic.Int32
	authenticated atomic.Bool
	rcptAccepted  atomic.Bool
	commands      []string
	message       []byte
	insecureAuth  bool
	dialed        atomic.Bool
}

func adminSMTPTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "smtp.example.test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"smtp.example.test"},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key, Leaf: leaf}, roots
}

func newAdminSMTPFixture(t *testing.T, options adminSMTPFixtureOptions) *adminSMTPFixture {
	t.Helper()
	certificate, roots := adminSMTPTestCertificate(t)
	// TLS can reject a certificate while the peer is still flushing its flight.
	// A zero-buffer net.Pipe then deadlocks the peer's flight and the fatal alert;
	// loopback TCP preserves the production transport's duplex buffering.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &adminSMTPFixture{listener: listener, roots: roots, done: make(chan struct{}), errors: make(chan error, 1)}
	go func() {
		defer close(f.done)
		server, err := listener.Accept()
		if err != nil {
			return
		}
		defer server.Close()
		_ = server.SetDeadline(time.Now().Add(5 * time.Second))
		var wire net.Conn = server
		secure := false
		secureConnection := func() bool {
			connection := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
			if err := connection.Handshake(); err != nil {
				return false
			}
			wire, secure = connection, true
			return true
		}
		if options.security == "tls" && !secureConnection() {
			return
		}
		reader, writer := bufio.NewReader(wire), bufio.NewWriter(wire)
		respond := func(value string) bool {
			if _, err := writer.WriteString(value); err != nil {
				return false
			}
			return writer.Flush() == nil
		}
		if !respond("220 smtp.example.test ready\r\n") {
			return
		}
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			command := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
			if !strings.HasPrefix(command, "AUTH ") {
				f.commands = append(f.commands, command)
			}
			switch {
			case strings.HasPrefix(command, "EHLO "), strings.HasPrefix(command, "HELO "):
				response := "250-smtp.example.test\r\n"
				if !secure && options.advertiseTLS {
					response += "250-STARTTLS\r\n"
				}
				if options.mechanisms != "" {
					response += "250-AUTH " + options.mechanisms + "\r\n"
				}
				if !respond(response + "250 SIZE 65536\r\n") {
					return
				}
			case command == "STARTTLS":
				if !respond("220 Ready for TLS\r\n") || !secureConnection() {
					return
				}
				reader, writer = bufio.NewReader(wire), bufio.NewWriter(wire)
			case strings.HasPrefix(command, "AUTH PLAIN"):
				f.authAttempts.Add(1)
				f.insecureAuth = !secure
				if options.rejectAuth {
					if !respond("535 Authentication rejected\r\n") {
						return
					}
				} else {
					f.authenticated.Store(true)
					if !respond("235 Authenticated\r\n") {
						return
					}
				}
			case command == "AUTH LOGIN":
				f.authAttempts.Add(1)
				f.insecureAuth = !secure
				if !respond("334 VXNlcm5hbWU6\r\n") {
					return
				}
				if _, err := reader.ReadString('\n'); err != nil || !respond("334 UGFzc3dvcmQ6\r\n") {
					return
				}
				if _, err := reader.ReadString('\n'); err != nil {
					return
				}
				f.authenticated.Store(true)
				if !respond("235 Authenticated\r\n") {
					return
				}
			case strings.HasPrefix(command, "MAIL FROM:"):
				if !respond("250 Sender accepted\r\n") {
					return
				}
			case strings.HasPrefix(command, "RCPT TO:"):
				if options.rejectRecipient {
					if !respond("550 Recipient rejected\r\n") {
						return
					}
				} else {
					f.rcptAccepted.Store(true)
					if !respond("250 Recipient accepted\r\n") {
						return
					}
				}
			case command == "DATA":
				if !respond("354 Send content\r\n") {
					return
				}
				var content bytes.Buffer
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					if line == ".\r\n" {
						break
					}
					content.WriteString(strings.TrimPrefix(line, "."))
				}
				f.message = content.Bytes()
				if options.dropAfterData || !respond("250 Message accepted\r\n") {
					return
				}
			case command == "QUIT":
				respond("221 Closing connection\r\n")
				return
			case command == "*":
				if !respond("501 Authentication cancelled\r\n") {
					return
				}
			default:
				f.errors <- errors.New("unexpected SMTP command")
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		if f.connection != nil {
			_ = f.connection.Close()
		}
		select {
		case <-f.done:
		case <-time.After(3 * time.Second):
			t.Error("SMTP fixture did not retire")
		}
		select {
		case err := <-f.errors:
			t.Error(err)
		default:
		}
	})
	return f
}

func (f *adminSMTPFixture) lookup(_ context.Context, network, host string) ([]netip.Addr, error) {
	if network != "ip" || host != "smtp.example.test" {
		return nil, errors.New("unexpected DNS query")
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func (f *adminSMTPFixture) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address != "127.0.0.1:587" || f.dialed.Swap(true) {
		return nil, errors.New("unexpected literal dial")
	}
	// The injected dial still verifies the authorized literal destination before
	// mapping the fixed test port to this fixture's task-owned loopback listener.
	dialer := &net.Dialer{}
	connection, err := dialer.DialContext(ctx, network, f.listener.Addr().String())
	if err == nil {
		f.connection = connection
	}
	return connection, err
}

func (f *adminSMTPFixture) wait(t *testing.T) {
	t.Helper()
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP fixture did not close")
	}
}

func adminSMTPTestInput(security string) (AdminSMTPConfig, AdminMessage) {
	return AdminSMTPConfig{
			Host: "smtp.example.test", Port: 587, Security: security, Username: "notification-user",
			From: "goby@example.test", To: []string{"owner@example.test", "operator@example.test"},
		}, AdminMessage{
			EventID: "test-event", Kind: "Test", Title: "Goby \u901a\u77e5",
			Summary: "Committed event\nSecond line", OccurredAt: time.Date(2026, 11, 6, 1, 2, 3, 0, time.UTC),
		}
}

func TestAdminSMTPDeliversOnlyAfterVerifiedTLS(t *testing.T) {
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: security, advertiseTLS: true, mechanisms: "PLAIN LOGIN"})
			configuration, message := adminSMTPTestInput(security)
			var checks int
			code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
				RuntimeOptions{RootCAs: f.roots}, func(context.Context) error { checks++; return nil }, f.lookup, f.dial)
			f.wait(t)
			if code != "delivered" || !f.authenticated.Load() || f.insecureAuth || checks < 5 {
				t.Fatalf("SMTP security or authority boundary failed: outcome=%s checks=%d", code, checks)
			}
			mail, err := mail.ReadMessage(bytes.NewReader(f.message))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(mail.Header.Get("Subject"))
			if err != nil || subject != message.Title || mail.Header.Get("From") != configuration.From {
				t.Fatal("message headers did not preserve the safe envelope")
			}
			body, err := io.ReadAll(quotedprintable.NewReader(mail.Body))
			if err != nil || !strings.Contains(string(body), "Committed event") || bytes.Contains(f.message, []byte("smtp-password-sentinel")) {
				t.Fatal("message content was invalid or exposed a transport credential")
			}
		})
	}
}

func TestAdminSMTPRejectsUnsafeInputBeforeDNS(t *testing.T) {
	tests := []struct {
		name   string
		change func(*AdminSMTPConfig, *AdminMessage, *string)
	}{
		{"sender header injection", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) { c.From += "\r\nBcc: other@example.test" }},
		{"recipient header injection", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) {
			c.To = []string{"owner@example.test\nBcc: other@example.test"}
		}},
		{"display name envelope", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) { c.From = "Sender <goby@example.test>" }},
		{"subject header injection", func(_ *AdminSMTPConfig, m *AdminMessage, _ *string) { m.Title = "Notice\r\nBcc: other@example.test" }},
		{"URL host", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) { c.Host = "https://smtp.example.test" }},
		{"host userinfo", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) { c.Host = "user@smtp.example.test" }},
		{"host zone", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) { c.Host = "fe80::1%eth0" }},
		{"plaintext", func(c *AdminSMTPConfig, _ *AdminMessage, _ *string) { c.Security = "none" }},
		{"password control", func(_ *AdminSMTPConfig, _ *AdminMessage, p *string) { *p = "unsafe\r\ncredential" }},
		{"missing secret", func(_ *AdminSMTPConfig, _ *AdminMessage, p *string) { *p = "" }},
		{"body NUL", func(_ *AdminSMTPConfig, m *AdminMessage, _ *string) { m.Summary = "content\x00tail" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration, message := adminSMTPTestInput("starttls")
			password := "smtp-password-sentinel"
			test.change(&configuration, &message, &password)
			var lookups, dials int
			code := sendAdminSMTPWithNetwork(context.Background(), configuration, nil, password, message, RuntimeOptions{}, nil,
				func(context.Context, string, string) ([]netip.Addr, error) { lookups++; return nil, nil },
				func(context.Context, string, string) (net.Conn, error) { dials++; return nil, nil })
			if code != "endpoint_invalid" || lookups != 0 || dials != 0 {
				t.Fatalf("unsafe input reached the network: outcome=%s lookups=%d dials=%d", code, lookups, dials)
			}
		})
	}
}

func TestAdminSMTPRejectsEveryDNSAnswerBeforeDial(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("10.0.0.1")},
		{netip.Addr{}},
		{netip.MustParseAddr("fe80::1%eth0")},
	} {
		configuration, message := adminSMTPTestInput("tls")
		var dials int
		code := sendAdminSMTPWithNetwork(context.Background(), configuration, nil, "smtp-password-sentinel", message, RuntimeOptions{}, nil,
			func(context.Context, string, string) ([]netip.Addr, error) { return addresses, nil },
			func(context.Context, string, string) (net.Conn, error) { dials++; return nil, nil })
		if code != "endpoint_invalid" || dials != 0 {
			t.Fatalf("an unauthorized DNS answer was dialed: outcome=%s dials=%d", code, dials)
		}
	}
}

func TestAdminSMTPWillNotDowngradeSTARTTLS(t *testing.T) {
	f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: "starttls", mechanisms: "PLAIN"})
	configuration, message := adminSMTPTestInput("starttls")
	code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
		RuntimeOptions{RootCAs: f.roots}, nil, f.lookup, f.dial)
	f.wait(t)
	if code != "tls_failed" || f.authenticated.Load() || f.insecureAuth || len(f.message) != 0 {
		t.Fatalf("STARTTLS was downgraded: outcome=%s", code)
	}
}

func TestAdminSMTPRejectsUntrustedTLS(t *testing.T) {
	for _, security := range []string{"tls", "starttls"} {
		t.Run(security, func(t *testing.T) {
			f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: security, advertiseTLS: true, mechanisms: "PLAIN"})
			configuration, message := adminSMTPTestInput(security)
			code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
				RuntimeOptions{RootCAs: x509.NewCertPool()}, nil, f.lookup, f.dial)
			f.wait(t)
			if code != "tls_failed" || f.authAttempts.Load() != 0 || f.authenticated.Load() || f.insecureAuth || len(f.message) != 0 {
				t.Fatalf("untrusted TLS transmitted credentials or mail: outcome=%s", code)
			}
			for _, command := range f.commands {
				if strings.HasPrefix(command, "MAIL ") || command == "DATA" {
					t.Fatal("an untrusted TLS peer received a mail transaction")
				}
			}
		})
	}
}

func TestAdminSMTPRechecksAuthorityAfterAuthentication(t *testing.T) {
	f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: "tls", mechanisms: "PLAIN"})
	configuration, message := adminSMTPTestInput("tls")
	code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
		RuntimeOptions{RootCAs: f.roots}, func(context.Context) error {
			if f.authenticated.Load() {
				return identity.ErrUnauthorized
			}
			return nil
		}, f.lookup, f.dial)
	f.wait(t)
	if code != "authority_revoked" || !f.authenticated.Load() || f.rcptAccepted.Load() || len(f.message) != 0 {
		t.Fatalf("revoked authority transmitted mail: outcome=%s", code)
	}
	for _, command := range f.commands {
		if strings.HasPrefix(command, "MAIL ") {
			t.Fatal("MAIL followed revoked authority")
		}
	}
}

func TestAdminSMTPRechecksAuthorityBeforeDATA(t *testing.T) {
	f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: "tls", mechanisms: "PLAIN"})
	configuration, message := adminSMTPTestInput("tls")
	configuration.To = configuration.To[:1]
	code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
		RuntimeOptions{RootCAs: f.roots}, func(context.Context) error {
			if f.rcptAccepted.Load() {
				return identity.ErrUnauthorized
			}
			return nil
		}, f.lookup, f.dial)
	f.wait(t)
	if code != "authority_revoked" || !f.rcptAccepted.Load() || len(f.message) != 0 {
		t.Fatalf("revoked authority transmitted DATA: outcome=%s", code)
	}
	for _, command := range f.commands {
		if command == "DATA" {
			t.Fatal("DATA followed revoked authority")
		}
	}
}

func TestAdminSMTPDoesNotReplayUnconfirmedDATA(t *testing.T) {
	f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: "tls", mechanisms: "PLAIN", dropAfterData: true})
	configuration, message := adminSMTPTestInput("tls")
	code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
		RuntimeOptions{RootCAs: f.roots}, nil, f.lookup, f.dial)
	f.wait(t)
	if code != "delivery_unconfirmed" || len(f.message) == 0 {
		t.Fatalf("lost DATA acceptance was incorrectly classified: outcome=%s", code)
	}
}

func TestAdminSMTPCancellationRetiresConnection(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	connected, peerDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(peerDone)
		var data [1]byte
		_, _ = server.Read(data[:])
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configuration, message := adminSMTPTestInput("starttls")
	result := make(chan string, 1)
	go func() {
		result <- sendAdminSMTPWithNetwork(ctx, configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message, RuntimeOptions{}, nil,
			func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			},
			func(context.Context, string, string) (net.Conn, error) { close(connected); return client, nil })
	}()
	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP sender did not connect")
	}
	cancel()
	select {
	case code := <-result:
		if code != "cancelled" {
			t.Fatalf("cancelled SMTP sender returned %s", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP sender did not retire after cancellation")
	}
	select {
	case <-peerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP sender returned without closing its connection")
	}
}

func TestAdminSMTPSupportsLOGINInsideTLS(t *testing.T) {
	f := newAdminSMTPFixture(t, adminSMTPFixtureOptions{security: "tls", mechanisms: "LOGIN"})
	configuration, message := adminSMTPTestInput("tls")
	code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
		RuntimeOptions{RootCAs: f.roots}, nil, f.lookup, f.dial)
	f.wait(t)
	if code != "delivered" || !f.authenticated.Load() || f.insecureAuth {
		t.Fatalf("LOGIN inside verified TLS failed: outcome=%s", code)
	}
}

func TestAdminSMTPSafeRejectionOutcomes(t *testing.T) {
	for _, test := range []struct {
		name    string
		options adminSMTPFixtureOptions
		want    string
	}{
		{"authentication", adminSMTPFixtureOptions{security: "tls", mechanisms: "PLAIN", rejectAuth: true}, "authentication_failed"},
		{"recipient", adminSMTPFixtureOptions{security: "tls", mechanisms: "PLAIN", rejectRecipient: true}, "recipient_rejected"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAdminSMTPFixture(t, test.options)
			configuration, message := adminSMTPTestInput("tls")
			code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message,
				RuntimeOptions{RootCAs: f.roots}, nil, f.lookup, f.dial)
			f.wait(t)
			if code != test.want || len(f.message) != 0 {
				t.Fatalf("rejected SMTP request had an unsafe result: outcome=%s", code)
			}
		})
	}
}

func TestAdminSMTPBoundsUnterminatedGreeting(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = server.Write(bytes.Repeat([]byte("A"), adminSMTPResponseLimit+1))
	}()
	configuration, message := adminSMTPTestInput("starttls")
	code := sendAdminSMTPWithNetwork(context.Background(), configuration, []string{"127.0.0.1/32"}, "smtp-password-sentinel", message, RuntimeOptions{}, nil,
		func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		func(context.Context, string, string) (net.Conn, error) { return client, nil })
	if code != "request_rejected" {
		t.Fatalf("oversized SMTP reply was not rejected: outcome=%s", code)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP sender did not retire the oversized reply")
	}
}
