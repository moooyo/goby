package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

const sessionCookie = "goby_session"

func csrfToken(token string) string {
	sum := sha256.Sum256([]byte("goby:admin:csrf:" + token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		apiError(w, r, 403, "origin_denied", "The request origin is not allowed.")
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || origin != s.cfg.PublicURL || u.User != nil {
			apiError(w, r, 403, "origin_denied", "The request origin is not allowed.")
			return false
		}
	}
	return true
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			apiError(w, r, 401, "authentication_required", "Sign in as an administrator.")
			return
		}
		principal, err := s.identity.Resolve(r.Context(), cookie.Value, "admin")
		if err != nil {
			s.identityError(w, r, err)
			return
		}
		if !principal.User.IsAdministrator {
			apiError(w, r, 403, "administrator_required", "Administrator access is required.")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if !s.sameOrigin(w, r) {
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(csrfToken(cookie.Value))) != 1 {
				apiError(w, r, 403, "csrf_invalid", "Refresh the session and try again.")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal)))
	}
}

func (s *Server) requireEmby(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _, err := parseEmbyCredentials(r)
		if err != nil || token == "" {
			apiError(w, r, 401, "authentication_required", "An Emby access token is required.")
			return
		}
		principal, err := s.identity.Resolve(r.Context(), token, "emby")
		if err != nil {
			s.identityError(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal)))
	}
}

func parseEmbyCredentials(r *http.Request) (string, identity.Client, error) {
	values := map[string]string{}
	for _, header := range []string{"Authorization", "X-Emby-Authorization"} {
		for _, value := range r.Header.Values(header) {
			if value == "" {
				continue
			}
			prefix, rest, found := strings.Cut(value, " ")
			if !found || !strings.EqualFold(prefix, "Emby") {
				return "", identity.Client{}, fmt.Errorf("unsupported authorization scheme")
			}
			for rest != "" {
				rest = strings.TrimSpace(rest)
				key, tail, ok := strings.Cut(rest, "=")
				if !ok {
					return "", identity.Client{}, fmt.Errorf("invalid authorization attributes")
				}
				key = strings.ToLower(strings.TrimSpace(key))
				tail = strings.TrimSpace(tail)
				var attribute string
				if strings.HasPrefix(tail, "\"") {
					end := strings.Index(tail[1:], "\"")
					if end < 0 {
						return "", identity.Client{}, fmt.Errorf("unterminated authorization attribute")
					}
					attribute = tail[1 : 1+end]
					rest = strings.TrimSpace(tail[end+2:])
					if rest != "" {
						if rest[0] != ',' {
							return "", identity.Client{}, fmt.Errorf("invalid authorization delimiter")
						}
						rest = rest[1:]
					}
				} else {
					attribute, rest, _ = strings.Cut(tail, ",")
					attribute = strings.TrimSpace(attribute)
				}
				if prior, exists := values[key]; exists && prior != attribute {
					return "", identity.Client{}, fmt.Errorf("conflicting authorization attributes")
				}
				values[key] = attribute
			}
		}
	}
	token := values["token"]
	candidates := append(r.Header.Values("X-Emby-Token"), r.URL.Query()["api_key"]...)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if token != "" && token != candidate {
			return "", identity.Client{}, fmt.Errorf("conflicting access tokens")
		}
		token = candidate
	}
	return token, identity.Client{Name: values["client"], DeviceID: values["deviceid"], Device: values["device"], Version: values["version"]}, nil
}

type loginWindow struct {
	count   int
	expires time.Time
}
type loginLimiter struct {
	mu      sync.Mutex
	windows map[string]loginWindow
}

func newLoginLimiter() *loginLimiter { return &loginLimiter{windows: map[string]loginWindow{}} }
func (l *loginLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.windows) > 10000 {
		for k, v := range l.windows {
			if now.After(v.expires) {
				delete(l.windows, k)
			}
		}
	}
	window, found := l.windows[key]
	if !found && len(l.windows) >= 20000 {
		return false
	}
	if now.After(window.expires) {
		window = loginWindow{expires: now.Add(time.Minute)}
	}
	window.count++
	l.windows[key] = window
	return window.count <= 10
}
func (s *Server) allowLogin(w http.ResponseWriter, r *http.Request) bool {
	host := s.clientAddress(r)
	if !s.limiter.allow(host) {
		w.Header().Set("Retry-After", "60")
		apiError(w, r, 429, "rate_limited", "Too many attempts. Try again in a minute.")
		return false
	}
	return true
}

// clientAddress walks a trusted proxy chain from the immediate peer toward the
// client. Headers from untrusted peers never influence authentication limits.
func (s *Server) clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	trusted := func(address netip.Addr) bool {
		for _, prefix := range s.cfg.TrustedProxies {
			if prefix.Contains(address) {
				return true
			}
		}
		return false
	}
	if !trusted(peer) {
		return peer.String()
	}
	forwarded := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	if len(forwarded) > 32 {
		return peer.String()
	}
	current := peer
	for i := len(forwarded) - 1; i >= 0 && trusted(current); i-- {
		address, parseErr := netip.ParseAddr(strings.TrimSpace(forwarded[i]))
		if parseErr != nil {
			return peer.String()
		}
		current = address.Unmap()
	}
	return current.String()
}
