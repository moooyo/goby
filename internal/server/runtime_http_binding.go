package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
)

// HTTPBindingStartup is the immutable setting captured before reserving this
// generation's listener. Later settings writes do not change this reservation.
type HTTPBindingStartup struct {
	Binding  settings.NetworkValues
	Revision int64
}

func (binding HTTPBindingStartup) Address() string {
	return net.JoinHostPort(binding.Binding.BindHost, strconv.Itoa(binding.Binding.HttpPort))
}

// HTTPBindingActive contains only an observed socket and its startup choice.
// It is runtime state: it is neither a writable setting nor backup content.
type HTTPBindingActive struct {
	Configured settings.NetworkValues
	BoundHost  string
	HttpPort   int
	Revision   string
}

type HTTPBindingState struct {
	Active          *HTTPBindingActive
	RestartRequired bool
	ReconnectURL    string
}

type runtimeHTTPBinding struct {
	mu        sync.Mutex
	frozen    bool
	published bool
	retired   bool
	startup   HTTPBindingStartup
	active    *HTTPBindingActive
}

// StartupHTTPBinding is called only after Server.New has loaded settings, and
// before ingress exists. The first successful call freezes the exact choice.
func (s *Server) StartupHTTPBinding() (HTTPBindingStartup, error) {
	s.httpBinding.mu.Lock()
	defer s.httpBinding.mu.Unlock()
	if s.httpBinding.retired {
		return HTTPBindingStartup{}, errors.New("HTTP generation binding has been retired")
	}
	if s.httpBinding.frozen {
		return s.httpBinding.startup, nil
	}
	var selected HTTPBindingStartup
	if s.settings != nil {
		snapshot := s.settings.Snapshot()
		selected = HTTPBindingStartup{Binding: snapshot.Runtime.DesiredNetwork, Revision: snapshot.Revision}
	} else {
		// Direct handler fixtures can omit the settings store. Production owns
		// one before it calls this method and never substitutes deployment state.
		defaults, err := config.HTTPBindingDefaults(s.cfg.ListenAddress)
		if err != nil {
			return HTTPBindingStartup{}, err
		}
		selected.Binding = settings.NetworkValues{BindHost: defaults.BindHost, HttpPort: defaults.HttpPort}
	}
	parsed, err := config.HTTPBindingDefaults(selected.Address())
	if err != nil || parsed.BindHost != selected.Binding.BindHost || parsed.HttpPort != selected.Binding.HttpPort || selected.Revision < 0 {
		return HTTPBindingStartup{}, errors.New("HTTP startup binding is invalid")
	}
	s.httpBinding.startup, s.httpBinding.frozen = selected, true
	return selected, nil
}

// PublishHTTPBinding follows a successful net.Listen and precedes Serve. It
// rejects an unrelated reservation, a modified startup receipt and republication.
func (s *Server) PublishHTTPBinding(address net.Addr, startup HTTPBindingStartup) error {
	endpoint, ok := httpTCPAddress(address, true)
	if !ok {
		return errors.New("HTTP listener did not provide a TCP reservation")
	}
	s.httpBinding.mu.Lock()
	defer s.httpBinding.mu.Unlock()
	if !s.httpBinding.frozen || s.httpBinding.published || s.httpBinding.retired || s.httpBinding.startup != startup ||
		startup.Binding.HttpPort != 0 && int(endpoint.Port()) != startup.Binding.HttpPort {
		return errors.New("HTTP listener does not match its startup binding")
	}
	if host, err := netip.ParseAddr(startup.Binding.BindHost); err == nil && !host.Unmap().IsUnspecified() && host.Unmap() != endpoint.Addr() {
		return errors.New("HTTP listener does not match its configured address")
	}
	s.httpBinding.active = &HTTPBindingActive{Configured: startup.Binding, BoundHost: endpoint.Addr().String(),
		HttpPort: int(endpoint.Port()), Revision: strconv.FormatInt(startup.Revision, 10)}
	s.httpBinding.published = true
	return nil
}

// WithdrawHTTPBinding is called after ingress and its reservation are closed.
// The startup receipt remains frozen: a retired generation cannot be reused.
func (s *Server) WithdrawHTTPBinding() {
	s.httpBinding.mu.Lock()
	defer s.httpBinding.mu.Unlock()
	s.httpBinding.active = nil
	s.httpBinding.retired = true
}

// ManagedHTTPBindingState never reads a new settings snapshot: a caller projects
// the exact committed desired values returned by its own read or mutation.
func (s *Server) ManagedHTTPBindingState(desired settings.NetworkValues) HTTPBindingState {
	s.httpBinding.mu.Lock()
	defer s.httpBinding.mu.Unlock()
	var state HTTPBindingState
	if s.httpBinding.active != nil {
		active := *s.httpBinding.active
		state.Active = &active
		state.RestartRequired = active.Configured != desired
	}
	// Neither a wildcard nor an inherited hostname proves a browser host. Do
	// not use Host, proxy headers, DNS resolution or the deployment PublicURL
	// to guess where the browser should reconnect after a bind change.
	host, err := netip.ParseAddr(desired.BindHost)
	host = host.Unmap()
	port := desired.HttpPort
	if port == 0 && state.Active != nil && !state.RestartRequired {
		port = state.Active.HttpPort
	}
	if err == nil && host.Zone() == "" && !host.IsUnspecified() && port > 0 && port <= 65535 {
		state.ReconnectURL = "http://" + net.JoinHostPort(host.String(), strconv.Itoa(port))
	}
	return state
}

type httpConnectionEndpointKey struct{}

type httpConnectionEndpoint struct {
	owner   *runtimeHTTPBinding
	address netip.AddrPort
}

// HTTPConnectionContext is installed as the generation's http.Server.ConnContext.
// Only the accepted TCP connection supplies this value; request headers cannot.
func (s *Server) HTTPConnectionContext(ctx context.Context, connection net.Conn) context.Context {
	if connection == nil {
		return ctx
	}
	endpoint, ok := httpTCPAddress(connection.LocalAddr(), false)
	if !ok {
		return ctx
	}
	s.httpBinding.mu.Lock()
	active := s.httpBinding.active
	accepted := active != nil && active.HttpPort == int(endpoint.Port())
	if accepted {
		bound, err := netip.ParseAddr(active.BoundHost)
		accepted = err == nil && (bound.IsUnspecified() || bound.Unmap() == endpoint.Addr())
	}
	s.httpBinding.mu.Unlock()
	if !accepted {
		return ctx
	}
	return context.WithValue(ctx, httpConnectionEndpointKey{}, httpConnectionEndpoint{owner: &s.httpBinding, address: endpoint})
}

func httpTCPAddress(address net.Addr, allowUnspecified bool) (netip.AddrPort, bool) {
	tcp, ok := address.(*net.TCPAddr)
	if !ok || tcp == nil || tcp.Zone != "" || tcp.Port < 1 || tcp.Port > 65535 {
		return netip.AddrPort{}, false
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	ip = ip.Unmap()
	if !ok || !allowUnspecified && ip.IsUnspecified() {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip, uint16(tcp.Port)), true
}

func (s *Server) directHTTPOriginAllowed(r *http.Request, origin string) bool {
	observed, ok := r.Context().Value(httpConnectionEndpointKey{}).(httpConnectionEndpoint)
	if !ok || observed.owner != &s.httpBinding {
		return false
	}
	s.httpBinding.mu.Lock()
	active := s.httpBinding.active
	current := active != nil && active.HttpPort == int(observed.address.Port())
	s.httpBinding.mu.Unlock()
	if !current {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Opaque != "" || u.User != nil ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return false
	}
	host, portText := u.Hostname(), u.Port()
	port := 80
	if portText != "" {
		port, err = strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 || portText != strconv.Itoa(port) {
			return false
		}
	}
	if port != int(observed.address.Port()) {
		return false
	}
	var serializedHost string
	if strings.EqualFold(host, "localhost") {
		if !observed.address.Addr().IsLoopback() {
			return false
		}
		serializedHost = host
	} else {
		ip, err := netip.ParseAddr(host)
		if err != nil || ip.Zone() != "" || ip.Unmap().IsUnspecified() || ip.Unmap() != observed.address.Addr() {
			return false
		}
		serializedHost = host
		if ip.Is6() {
			serializedHost = "[" + host + "]"
		}
	}
	if portText != "" {
		serializedHost += ":" + portText
	}
	return origin == "http://"+serializedHost
}
