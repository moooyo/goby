package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/settings"
)

func (s *Server) registerConfigurationRoutes(mux *http.ServeMux) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		authenticated := s.requireEmby(handler)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authenticated(w, r)
		}
	}
	mux.HandleFunc("GET /emby/System/Configuration", wrap(s.embyConfiguration))
	mux.HandleFunc("GET /emby/System/Configuration/{key}", wrap(s.embyNamedConfiguration))
	mux.HandleFunc("POST /emby/System/Configuration", wrap(s.updateEmbyConfiguration))
	mux.HandleFunc("POST /emby/System/Configuration/Partial", wrap(s.patchEmbyConfiguration))
	mux.HandleFunc("POST /emby/System/Configuration/{key}", wrap(s.updateEmbyNamedConfiguration))
}

func configurationActor(r *http.Request) settings.Actor {
	return settings.Actor{Principal: r.Context().Value(principalKey).(identity.Principal), Audience: identity.AdministratorEmby}
}

// The configuration boundary checks current authority before interpreting a
// named section or mutation body. The store repeats mutation authorization in
// the transaction that changes and publishes configuration.
func (s *Server) configurationAccess(w http.ResponseWriter, r *http.Request, administrator bool, feature string) (settings.Configuration, bool) {
	if s.settings == nil {
		s.configurationError(w, r, settings.ErrUnavailable, feature)
		return settings.Configuration{}, false
	}
	view, err := s.settings.GetConfiguration(r.Context(), configurationActor(r))
	if err != nil {
		s.configurationError(w, r, err, feature)
		return settings.Configuration{}, false
	}
	if administrator && !view.CanManage {
		s.configurationError(w, r, identity.ErrClientSessionForbidden, feature)
		return settings.Configuration{}, false
	}
	return view, true
}

func (s *Server) embyConfiguration(w http.ResponseWriter, r *http.Request) {
	view, ok := s.configurationAccess(w, r, false, "ReadServerConfiguration")
	if !ok || !configurationQuery(w, r) {
		return
	}
	if !view.CanManage {
		// An ordinary viewer receives the observed two-byte object, not a
		// redacted administrator object or the encoder's trailing newline.
		configurationJSON(w, r, map[string]any{})
		return
	}
	configurationJSON(w, r, serverConfigurationDTO(view))
}

func (s *Server) embyNamedConfiguration(w http.ResponseWriter, r *http.Request) {
	view, ok := s.configurationAccess(w, r, true, "ReadServerConfiguration")
	if !ok || !configurationQuery(w, r) || !configurationSection(w, r) {
		return
	}
	configurationJSON(w, r, encodingConfigurationDTO(view.Snapshot))
}

func (s *Server) updateEmbyConfiguration(w http.ResponseWriter, r *http.Request) {
	s.writeEmbyConfiguration(w, r, settings.ConfigurationFull)
}

func (s *Server) patchEmbyConfiguration(w http.ResponseWriter, r *http.Request) {
	s.writeEmbyConfiguration(w, r, settings.ConfigurationPartial)
}

func (s *Server) updateEmbyNamedConfiguration(w http.ResponseWriter, r *http.Request) {
	s.writeEmbyConfiguration(w, r, settings.ConfigurationEncoding)
}

func (s *Server) writeEmbyConfiguration(w http.ResponseWriter, r *http.Request, section settings.ConfigurationSection) {
	if _, ok := s.configurationAccess(w, r, true, "ManageServer"); !ok {
		return
	}
	if !configurationQuery(w, r) || section == settings.ConfigurationEncoding && !configurationSection(w, r) {
		return
	}
	mutation, ok := decodeConfiguration(w, r, section)
	if !ok {
		return
	}
	if _, err := s.settings.ApplyConfiguration(r.Context(), configurationActor(r), mutation); err != nil {
		s.configurationError(w, r, err, "ManageServer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Named configuration is a closed capability registry, never a filesystem
// path or a bag of values that can be saved without a corresponding consumer.
func configurationSection(w http.ResponseWriter, r *http.Request) bool {
	switch strings.ToLower(r.PathValue("key")) {
	case "encoding":
		return true
	case "devices", "dlna":
		embyTextError(w, r, http.StatusNotImplemented, "This configuration section is not implemented.")
	default:
		embyTextError(w, r, http.StatusNotFound, "Configuration not found.")
	}
	return false
}

func configurationJSON(w http.ResponseWriter, r *http.Request, value map[string]any) {
	// The closed DTO projections contain only strings, booleans and integers.
	data, err := json.Marshal(value)
	if err != nil {
		embyTextError(w, r, http.StatusInternalServerError, "Configuration could not be represented.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func (s *Server) configurationError(w http.ResponseWriter, r *http.Request, err error, feature string) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		actor, _ := r.Context().Value(principalKey).(identity.Principal)
		embyTextError(w, r, http.StatusForbidden, fmt.Sprintf("User %s does not have access to %s feature.", actor.User.Name, feature))
	case errors.Is(err, settings.ErrInvalidInput):
		configurationInputError(w, r)
	case errors.Is(err, settings.ErrStoredSettings), errors.Is(err, settings.ErrUnavailable), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		embyTextError(w, r, http.StatusServiceUnavailable, "Server configuration is currently unavailable.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		s.log.Error("compatibility configuration operation failed", "request_id", r.Context().Value(requestIDKey))
		embyTextError(w, r, http.StatusInternalServerError, "The configuration operation could not be completed.")
	}
}
