package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/providerconfig"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/settings"
)

func (s *Server) initializeProviderConfiguration(ctx context.Context) error {
	store, err := providerconfig.New(ctx, s.db, s.identity, s.cfg.OnlineProviders)
	if err != nil {
		return err
	}
	s.providerConfiguration = store
	return nil
}

func (s *Server) registerProviderConfigurationRoutes(mux *http.ServeMux) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		authorized := s.requireAdmin(handler)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authorized(w, r)
		}
	}
	mux.HandleFunc("GET /admin/v1/providers/configuration", wrap(s.adminProviderConfiguration))
	mux.HandleFunc("PUT /admin/v1/providers/{providerId}/configuration", wrap(s.updateAdminProviderConfiguration))
	mux.HandleFunc("POST /admin/v1/providers/{providerId}/test", wrap(s.testAdminProviderConnection))
}

func (s *Server) providerConfigurationAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.providerConfiguration == nil {
		apiError(w, r, http.StatusServiceUnavailable, "provider_configuration_unavailable", "Provider configuration is not available. Reload before editing.")
		return false
	}
	return true
}

func (s *Server) providerConfigurationPolicy(value providerconfig.View) providerconfig.View {
	value.Enabled = value.DeploymentEnabled && s.settings != nil && s.settings.InternetProvidersEnabled()
	return value
}

func (s *Server) adminProviderConfiguration(w http.ResponseWriter, r *http.Request) {
	if !adminMetadataNoQuery(w, r) || !s.providerConfigurationAvailable(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.providerConfiguration.Get(r.Context(), actor)
	if err != nil {
		s.providerConfigurationError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.providerConfigurationPolicy(value))
}

func (s *Server) updateAdminProviderConfiguration(w http.ResponseWriter, r *http.Request) {
	if !s.providerConfigurationAvailable(w, r) {
		return
	}
	var input providerconfig.Update
	if !providerBody(w, r, &input) {
		return
	}
	providerID := r.PathValue("providerId")
	if !providerconfig.ValidProvider(providerID) {
		apiError(w, r, http.StatusNotFound, "provider_not_found", "The selected provider does not exist.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.providerConfiguration.Update(r.Context(), actor, providerID, input)
	if err != nil {
		s.providerConfigurationError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.providerConfigurationPolicy(value))
}

func (s *Server) testAdminProviderConnection(w http.ResponseWriter, r *http.Request) {
	if !s.providerConfigurationAvailable(w, r) {
		return
	}
	var input struct {
		Revision string `json:"Revision"`
	}
	if !providerBody(w, r, &input) {
		return
	}
	providerID := r.PathValue("providerId")
	if !providerconfig.ValidProvider(providerID) {
		apiError(w, r, http.StatusNotFound, "provider_not_found", "The selected provider does not exist.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	configuration, err := s.providerConfiguration.Capture(r.Context(), actor, input.Revision)
	if err != nil {
		s.providerConfigurationError(w, r, err)
		return
	}
	metadata := settings.MetadataOptions{PreferredMetadataLanguage: "en", MetadataCountryCode: "US"}
	if s.settings != nil {
		metadata = s.settings.MetadataValues()
	}
	configuration.Enabled = configuration.Enabled && s.cfg.OnlineProviders.Enabled
	result, err := providers.New(providerConfigForMetadata(configuration, metadata)).TestConnection(r.Context(), providerID)
	if err != nil {
		s.providerError(w, r, err)
		return
	}
	// Discard a result when the actor or tested saved configuration changed
	// during the network request. No authority locks are held across provider I/O.
	if _, err = s.providerConfiguration.Capture(r.Context(), actor, input.Revision); err != nil {
		s.providerConfigurationError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) providerConfigurationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, providerconfig.ErrInvalid):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the provider fields, paired username and password, and current revision.")
	case errors.Is(err, providerconfig.ErrConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "Provider configuration changed. Reload and review before trying again.")
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrClientSessionForbidden):
		s.identityError(w, r, err)
	default:
		apiError(w, r, http.StatusServiceUnavailable, "provider_configuration_unavailable", "Provider configuration could not be confirmed. Reload and review before trying again.")
	}
}
