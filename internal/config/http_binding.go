package config

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// HTTPBinding describes a deployment default, not an active listener. Unlike
// administrator overrides, deployment defaults may retain a hostname or port 0.
type HTTPBinding struct {
	BindHost string
	HttpPort int
}

// HTTPBindingDefaults preserves deployment IP/hostname and ephemeral bindings.
// An empty address is the existing ephemeral wildcard form.
// Service names, zones and ambiguous nonnumeric ports are not managed defaults.
func HTTPBindingDefaults(address string) (HTTPBinding, error) {
	if address == "" {
		return HTTPBinding{}, nil
	}
	host, portText, err := net.SplitHostPort(address)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || port < 0 || port > 65535 || address != net.JoinHostPort(host, portText) ||
		portText != strconv.Itoa(port) || len(host) > 253 || strings.TrimSpace(host) != host ||
		strings.ContainsAny(host, "\\/%?#@,[]:\x00\t\r\n ") && !validDeploymentHTTPIP(host) {
		return HTTPBinding{}, errors.New("GOBY_LISTEN must contain a bounded host and numeric TCP port")
	}
	if parsed, err := netip.ParseAddr(host); err == nil {
		if parsed.Zone() != "" {
			return HTTPBinding{}, errors.New("GOBY_LISTEN must not contain an IPv6 zone")
		}
		host = parsed.String()
	} else if host != "" {
		for _, char := range host {
			if char != '.' && char != '-' && char != '_' &&
				(char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
				return HTTPBinding{}, errors.New("GOBY_LISTEN must contain an IP literal or deployment hostname")
			}
		}
	}
	return HTTPBinding{BindHost: host, HttpPort: port}, nil
}

func validDeploymentHTTPIP(host string) bool {
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Zone() == ""
}
