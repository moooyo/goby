package notifications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"time"
)

func Signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
func allowedAddress(address netip.Addr, networks []string) bool {
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range networks {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(address) {
			return true
		}
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2001::/32", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(address) {
			return false
		}
	}
	return true
}
func webhookTransport(t target, options RuntimeOptions, lifetime context.Context, authorize func(context.Context) error) (*http.Transport, func(), error) {
	u, err := url.Parse(t.endpoint)
	if err != nil {
		return nil, nil, ErrInvalid
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	networkContext, cancelNetwork := context.WithCancel(lifetime)
	var ownership sync.Mutex
	var workers sync.WaitGroup
	closing := false
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: options.RootCAs}, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16 * 1024, DisableCompression: true, DisableKeepAlives: true,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			h, p, e := net.SplitHostPort(address)
			if e != nil || h != host || p != port {
				return nil, ErrInvalid
			}
			addresses, e := net.DefaultResolver.LookupNetIP(ctx, "ip", h)
			if e != nil || len(addresses) == 0 || len(addresses) > 32 {
				return nil, ErrUnavailable
			}
			for _, candidate := range addresses {
				if !allowedAddress(candidate, t.networks) {
					return nil, ErrInvalid
				}
			}
			for _, candidate := range addresses {
				connection, e := dialer.DialContext(ctx, "tcp", net.JoinHostPort(candidate.String(), p))
				if e == nil {
					return connection, nil
				}
				if ctx.Err() != nil {
					break
				}
			}
			return nil, ErrUnavailable
		}}
	// Recheck authority after DNS, connect and TLS have completed, before the
	// HTTP transport can write the signed body. No database lock spans the wait.
	transport.DialTLSContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		ownership.Lock()
		if closing {
			ownership.Unlock()
			return nil, context.Canceled
		}
		workers.Add(1)
		ownership.Unlock()
		defer workers.Done()
		ctx = networkContext
		plain, err := transport.DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		configuration := transport.TLSClientConfig.Clone()
		configuration.ServerName = host
		secured := tls.Client(plain, configuration)
		handshake, stop := context.WithTimeout(ctx, 3*time.Second)
		err = secured.HandshakeContext(handshake)
		stop()
		if err != nil {
			secured.Close()
			return nil, err
		}
		if authorize != nil {
			if err = authorize(ctx); err != nil {
				secured.Close()
				return nil, err
			}
		}
		return secured, nil
	}
	closeTransport := func() {
		cancelNetwork()
		ownership.Lock()
		closing = true
		ownership.Unlock()
		transport.CloseIdleConnections()
		workers.Wait()
	}
	return transport, closeTransport, nil
}
func postWebhook(ctx context.Context, t target, credential, token, eventID string, raw []byte, options RuntimeOptions, authorize func(context.Context) error) (string, time.Duration) {
	transport, closeTransport, err := webhookTransport(t, options, ctx, authorize)
	if err != nil {
		return "endpoint_invalid", 0
	}
	defer closeTransport()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(raw))
	if err != nil {
		return "endpoint_invalid", 0
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Goby-Notification-Version", "1")
	request.Header.Set("X-Goby-Target-Token", token)
	request.Header.Set("X-Goby-Timestamp", timestamp)
	request.Header.Set("X-Goby-Signature", Signature(credential, timestamp, raw))
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "retry", 0
		}
		if ctx.Err() != nil {
			return "cancelled", 0
		}
		if code := safeCode(err); code == "authority_revoked" || code == "source_unavailable" {
			return code, 0
		}
		if errors.Is(err, errSourceVisibilityChanged) {
			return "source_changed", 0
		}
		return "retry", 0
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "retry", 0
		}
		if ctx.Err() != nil {
			return "cancelled", 0
		}
		return "retry", 0
	}
	if len(body) > 4096 {
		return "response_invalid", 0
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		var ack struct {
			EventId  string
			Accepted bool
		}
		if json.Unmarshal(body, &ack) != nil || ack.EventId != eventID || !ack.Accepted {
			return "ack_invalid", 0
		}
		return "delivered", 0
	}
	switch response.StatusCode {
	case 401, 403, 404, 410:
		return "target_invalid", 0
	case 408, 429:
		return "retry", retryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	if response.StatusCode >= 500 {
		return "retry", 0
	}
	return "request_rejected", 0
}
func retryAfter(raw string, now time.Time) time.Duration {
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err == nil && seconds >= 0 && seconds <= 3600 {
		return time.Duration(seconds) * time.Second
	}
	date, err := http.ParseTime(raw)
	if err == nil {
		delay := date.Sub(now)
		if delay >= 0 && delay <= time.Hour {
			return delay
		}
	}
	return 0
}
