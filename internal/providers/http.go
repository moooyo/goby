package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var networkSlots = make(chan struct{}, 4)
var providerHTTPTransport = newProviderHTTPTransport()

// Hosts are fixed by adapters, never by administrator input. DNS addresses are
// checked at each connection and the checked literal address is dialed directly.
func allowedHost(host string) bool {
	host = strings.ToLower(host)
	return host == "api.themoviedb.org" || host == "image.tmdb.org" || host == "musicbrainz.org" ||
		host == "opensubtitles.com" || host == "api.opensubtitles.com" || host == "vip-api.opensubtitles.com" ||
		strings.HasSuffix(host, ".opensubtitles.com")
}

func safeURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" ||
		(parsed.Port() != "" && parsed.Port() != "443") || !allowedHost(parsed.Hostname()) {
		return nil, ErrInvalidInput
	}
	return parsed, nil
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2001::/32", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(address) {
			return false
		}
	}
	return true
}

func newProviderHTTPTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		MaxConnsPerHost: 4, MaxIdleConns: 8, IdleConnTimeout: 30 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil || port != "443" || !allowedHost(host) {
				return nil, ErrUnavailable
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(addresses) == 0 {
				return nil, ErrUnavailable
			}
			for _, candidate := range addresses {
				if !publicAddress(candidate) {
					return nil, ErrUnavailable
				}
			}
			for _, candidate := range addresses {
				connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
				if err == nil {
					return connection, nil
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
			return nil, ErrUnavailable
		},
	}
	return transport
}

func newHTTPClient() *http.Client {
	return &http.Client{Transport: providerHTTPTransport, Timeout: 30 * time.Second, CheckRedirect: func(request *http.Request, previous []*http.Request) error {
		if len(previous) >= 3 {
			return ErrUnavailable
		}
		if _, err := safeURL(request.URL.String()); err != nil {
			return err
		}
		// API credentials never move to another hostname, even within a provider.
		if len(previous) == 0 || request.URL.Hostname() != previous[0].URL.Hostname() {
			return ErrUnavailable
		}
		return nil
	}}
}

func (client *Client) requestBytes(ctx context.Context, method, rawURL string, headers http.Header, body any, limit int64) ([]byte, string, error) {
	if _, err := safeURL(rawURL); err != nil {
		return nil, "", err
	}
	if limit <= 0 || limit > 20<<20 {
		return nil, "", ErrInvalidInput
	}
	select {
	case networkSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	defer func() { <-networkSlots }()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil || len(data) > 64<<10 {
			return nil, "", ErrInvalidInput
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, rawURL, bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrInvalidInput
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusNotFound:
		return nil, "", ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, "", ErrNotConfigured
	case http.StatusTooManyRequests, http.StatusPaymentRequired, http.StatusNotAcceptable:
		return nil, "", ErrQuota
	default:
		return nil, "", ErrUnavailable
	}
	if response.ContentLength > limit {
		return nil, "", ErrUnavailable
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit || len(data) == 0 {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", ErrUnavailable
	}
	return data, response.Header.Get("Content-Type"), nil
}

func (client *Client) requestJSON(ctx context.Context, method, rawURL string, headers http.Header, body, target any) error {
	data, _, err := client.requestBytes(ctx, method, rawURL, headers, body, 4<<20)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return ErrUnavailable
	}
	return nil
}
