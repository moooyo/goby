package notifications

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/netip"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/literaldial"
)

const adminSMTPResponseLimit = 256 * 1024

var errAdminSMTPResponseLimit = errors.New("SMTP response limit reached")

// adminSMTPReadConn bounds replies, including a hostile unterminated greeting.
// The allowance also includes TLS records and a normal certificate chain.
type adminSMTPReadConn struct {
	net.Conn
	remaining int
}

func (c *adminSMTPReadConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, errAdminSMTPResponseLimit
	}
	if len(p) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Read(p)
	c.remaining -= n
	return n, err
}

func validAdminSMTPAddress(value string) bool {
	if !validAdminMailAddress(value) {
		return false
	}
	for _, char := range value {
		if char < 0x21 || char > 0x7e {
			return false
		}
	}
	return true
}

func validAdminSMTPTransport(configuration AdminSMTPConfig, password string, message AdminMessage) bool {
	if !validAdminSMTPHost(configuration.Host) || configuration.Port < 1 || configuration.Port > 65535 ||
		configuration.Security != "tls" && configuration.Security != "starttls" || !validAdminSMTPAddress(configuration.From) ||
		len(configuration.To) == 0 || len(configuration.To) > 16 ||
		!validAdminText(configuration.Username, 256) || !validAdminText(password, 1024) ||
		(configuration.Username == "") != (password == "") || !validAdminText(message.Title, 512) ||
		len(message.Summary) > 16*1024 || !utf8.ValidString(message.Summary) || strings.ContainsRune(message.Summary, 0) {
		return false
	}
	seen := make(map[string]bool, len(configuration.To))
	for _, address := range configuration.To {
		if !validAdminSMTPAddress(address) || seen[strings.ToLower(address)] {
			return false
		}
		seen[strings.ToLower(address)] = true
	}
	return true
}

func adminSMTPMessage(configuration AdminSMTPConfig, message AdminMessage) ([]byte, error) {
	title := message.Title
	if title == "" {
		title = "Goby notification"
	}
	subject := mime.QEncoding.Encode("UTF-8", title)
	subject = strings.ReplaceAll(subject, "?= =?", "?=\r\n =?")
	occurred := message.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	id := sha256.Sum256([]byte(message.EventID))
	var body bytes.Buffer
	for _, header := range []string{
		"Date: " + occurred.UTC().Format(time.RFC1123Z),
		"From: " + configuration.From,
		"To: " + strings.Join(configuration.To, ",\r\n "),
		"Subject: " + subject,
		"Message-ID: <" + hex.EncodeToString(id[:]) + "@goby.local>",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
	} {
		body.WriteString(header)
		body.WriteString("\r\n")
	}
	body.WriteString("\r\n")
	encoded := quotedprintable.NewWriter(&body)
	if _, err := io.WriteString(encoded, message.Summary+"\n"); err != nil {
		return nil, err
	}
	if err := encoded.Close(); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

func adminSMTPContextOutcome(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	return "retry"
}

func adminSMTPFailure(ctx context.Context, err error, rejected string) string {
	if ctx.Err() != nil {
		return adminSMTPContextOutcome(ctx)
	}
	if errors.Is(err, errAdminSMTPResponseLimit) {
		return "request_rejected"
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		if reply.Code >= 400 && reply.Code < 500 {
			return "retry"
		}
		return rejected
	}
	if rejected == "tls_failed" {
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return "retry"
		}
		return "tls_failed"
	}
	return "retry"
}

func adminSMTPAuthorize(ctx context.Context, authorize func(context.Context) error) string {
	if ctx.Err() != nil {
		return adminSMTPContextOutcome(ctx)
	}
	if authorize == nil {
		return ""
	}
	if err := authorize(ctx); err != nil {
		if ctx.Err() != nil {
			return adminSMTPContextOutcome(ctx)
		}
		if safeCode(err) == "authority_revoked" || errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalid) {
			return "authority_revoked"
		}
		return "retry"
	}
	return ""
}

// adminSMTPLoginAuth supports servers that advertise LOGIN without PLAIN. It
// never sends credentials before the connection has verified TLS and its host.
type adminSMTPLoginAuth struct {
	host, username, password string
	step                     int
}

func (a *adminSMTPLoginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS || server.Name != a.host {
		return "", nil, ErrInvalid
	}
	a.step = 0
	return "LOGIN", nil, nil
}

func (a *adminSMTPLoginAuth) Next(_ []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch a.step {
	case 0:
		a.step++
		return []byte(a.username), nil
	case 1:
		a.step++
		return []byte(a.password), nil
	default:
		return nil, ErrInvalid
	}
}

func sendAdminSMTP(ctx context.Context, configuration AdminSMTPConfig, networks []string, password string, message AdminMessage, options RuntimeOptions, authorize func(context.Context) error) string {
	dialer := &net.Dialer{}
	return sendAdminSMTPWithNetwork(ctx, configuration, networks, password, message, options, authorize, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

func sendAdminSMTPWithNetwork(ctx context.Context, configuration AdminSMTPConfig, networks []string, password string, message AdminMessage, options RuntimeOptions, authorize func(context.Context) error, lookup func(context.Context, string, string) ([]netip.Addr, error), dial literaldial.DialFunc) string {
	if !validAdminSMTPTransport(configuration, password, message) || lookup == nil || dial == nil {
		return "endpoint_invalid"
	}
	raw, err := adminSMTPMessage(configuration, message)
	if err != nil {
		return "request_rejected"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if code := adminSMTPAuthorize(ctx, authorize); code != "" {
		return code
	}
	dialContext, stopDial := context.WithTimeout(ctx, 3*time.Second)
	addresses, err := lookup(dialContext, "ip", configuration.Host)
	if err != nil || len(addresses) == 0 || len(addresses) > 32 {
		stopDial()
		return adminSMTPContextOutcome(ctx)
	}
	for _, address := range addresses {
		if !address.IsValid() || address.Zone() != "" || !allowedAddress(address, networks) {
			stopDial()
			return "endpoint_invalid"
		}
	}
	connection, err := literaldial.DialContext(dialContext, "tcp", addresses, strconv.Itoa(configuration.Port), dial)
	stopDial()
	if err != nil {
		return adminSMTPContextOutcome(ctx)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if connection.SetDeadline(deadline) != nil {
			_ = connection.Close()
			return "retry"
		}
	}
	watcherStop, watcherDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-watcherStop:
		}
	}()
	defer func() {
		close(watcherStop)
		_ = connection.Close()
		<-watcherDone
	}()
	bounded := &adminSMTPReadConn{Conn: connection, remaining: adminSMTPResponseLimit}
	var wire net.Conn = bounded
	tlsConfiguration := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.RootCAs, ServerName: configuration.Host}
	if configuration.Security == "tls" {
		secured := tls.Client(bounded, tlsConfiguration)
		handshake, stop := context.WithTimeout(ctx, 3*time.Second)
		err = secured.HandshakeContext(handshake)
		stop()
		if err != nil {
			return adminSMTPFailure(ctx, err, "tls_failed")
		}
		wire = secured
	}
	client, err := smtp.NewClient(wire, configuration.Host)
	if err != nil {
		return adminSMTPFailure(ctx, err, "request_rejected")
	}
	if configuration.Security == "starttls" {
		if supported, _ := client.Extension("STARTTLS"); !supported {
			return "tls_failed"
		}
		deadline, _ := ctx.Deadline()
		handshakeDeadline := time.Now().Add(3 * time.Second)
		if deadline.Before(handshakeDeadline) {
			handshakeDeadline = deadline
		}
		if err = connection.SetDeadline(handshakeDeadline); err != nil {
			return "retry"
		}
		if err = client.StartTLS(tlsConfiguration); err != nil {
			return adminSMTPFailure(ctx, err, "tls_failed")
		}
		if err = connection.SetDeadline(deadline); err != nil {
			return "retry"
		}
	}
	if code := adminSMTPAuthorize(ctx, authorize); code != "" {
		return code
	}
	if configuration.Username != "" {
		supported, mechanisms := client.Extension("AUTH")
		if !supported {
			return "authentication_failed"
		}
		var authentication smtp.Auth
		for _, mechanism := range strings.Fields(mechanisms) {
			if strings.EqualFold(mechanism, "PLAIN") {
				authentication = smtp.PlainAuth("", configuration.Username, password, configuration.Host)
				break
			}
			if strings.EqualFold(mechanism, "LOGIN") {
				authentication = &adminSMTPLoginAuth{host: configuration.Host, username: configuration.Username, password: password}
			}
		}
		if authentication == nil {
			return "authentication_failed"
		}
		if err = client.Auth(authentication); err != nil {
			return adminSMTPFailure(ctx, err, "authentication_failed")
		}
	}
	if code := adminSMTPAuthorize(ctx, authorize); code != "" {
		return code
	}
	if err = client.Mail(configuration.From); err != nil {
		return adminSMTPFailure(ctx, err, "request_rejected")
	}
	for _, recipient := range configuration.To {
		if code := adminSMTPAuthorize(ctx, authorize); code != "" {
			return code
		}
		if err = client.Rcpt(recipient); err != nil {
			return adminSMTPFailure(ctx, err, "recipient_rejected")
		}
	}
	if code := adminSMTPAuthorize(ctx, authorize); code != "" {
		return code
	}
	data, err := client.Data()
	if err != nil {
		return adminSMTPFailure(ctx, err, "request_rejected")
	}
	if _, err = data.Write(raw); err != nil {
		// Without the terminating DATA dot the server cannot accept the message.
		return adminSMTPFailure(ctx, err, "request_rejected")
	}
	if err = data.Close(); err != nil {
		var reply *textproto.Error
		if errors.As(err, &reply) {
			return adminSMTPFailure(ctx, err, "request_rejected")
		}
		// The server may have accepted DATA before its acknowledgement was lost.
		// This outcome is terminal: automatically replaying could duplicate mail.
		return "delivery_unconfirmed"
	}
	// The DATA acknowledgement confirms acceptance even if QUIT subsequently fails.
	_ = client.Quit()
	return "delivered"
}
