package updates

import (
	"context"
	"crypto/tls"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/literaldial"
)

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

func newFeedTransport(host string) *http.Transport {
	dialer := &net.Dialer{KeepAlive: 30 * time.Second}
	return newFeedTransportWithNetwork(host, net.DefaultResolver.LookupNetIP, dialer.DialContext)
}

func newFeedTransportWithNetwork(host string, lookup func(context.Context, string, string) ([]netip.Addr, error), dial literaldial.DialFunc) *http.Transport {
	return &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DisableCompression: true, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second,
		MaxConnsPerHost: 1, MaxIdleConns: 1, IdleConnTimeout: 30 * time.Second, MaxResponseHeaderBytes: 16 << 10,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			requestedHost, port, err := net.SplitHostPort(address)
			if err != nil || port != "443" || !strings.EqualFold(requestedHost, host) {
				return nil, ErrCheckFailed
			}
			addresses, err := lookup(ctx, "ip", requestedHost)
			if err != nil || len(addresses) == 0 || len(addresses) > 16 {
				return nil, ErrCheckFailed
			}
			for _, address := range addresses {
				if !publicAddress(address) {
					return nil, ErrCheckFailed
				}
			}
			connection, err := literaldial.DialContext(ctx, network, addresses, port, dial)
			if err != nil {
				return nil, ErrCheckFailed
			}
			return connection, nil
		},
	}
}

func (service *Service) fetch(ctx context.Context) (Release, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.feedURL, nil)
	if err != nil {
		return Release{}, ErrCheckFailed
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Goby-Update-Check/1")
	response, err := service.client.Do(request)
	if err != nil {
		return Release{}, ErrCheckFailed
	}
	defer response.Body.Close()
	mediaType, parameters, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || response.ContentLength > MaxFeedBytes ||
		err != nil || len(response.Header.Values("Content-Type")) != 1 || mediaType != "application/json" || len(parameters) > 1 ||
		len(parameters) == 1 && !strings.EqualFold(parameters["charset"], "utf-8") ||
		response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return Release{}, ErrCheckFailed
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxFeedBytes+1))
	if err != nil || len(data) > MaxFeedBytes {
		return Release{}, ErrCheckFailed
	}
	release, err := parseFeed(data)
	if err != nil {
		return Release{}, ErrCheckFailed
	}
	return release, nil
}
