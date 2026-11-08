package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/updates"
)

var errUpdateAuthorizationUnavailable = errors.New("update authority could not be confirmed")

func (s *Server) registerCoreUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/server-updates", observabilityNoCache(s.requireAdmin(s.adminServerUpdates)))
	mux.HandleFunc("POST /admin/v1/server-updates/check", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		s.requireAdmin(s.checkAdminServerUpdates)(w, r)
	})
	mux.HandleFunc("GET /emby/Packages/Updates", observabilityNoCache(s.requireEmby(s.embySystemUpdates)))
}

func (s *Server) updatePolicySummary() map[string]any {
	configured := false
	if s.updates != nil {
		configured = s.updates.Status().ConfiguredSource
	}
	return map[string]any{
		"CanSelfUpdate": false, "Delivery": "docker", "ConfiguredSource": configured, "CurrentVersion": s.version,
	}
}

// projectVerifiedUpdateInfo adds only a successfully compared process-local
// observation. Unknown, unconfigured, failed, and unchecked states omit it.
func (s *Server) projectVerifiedUpdateInfo(dto map[string]any) {
	if s.updates == nil {
		return
	}
	if available, known := s.updates.HasUpdateAvailable(); known {
		dto["HasUpdateAvailable"] = available
	}
}

func (s *Server) updateAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	err := identity.CheckAdministratorRead(bounded, s.db, actor, audience)
	if errors.Is(err, identity.ErrAdministratorReadUnavailable) {
		return errUpdateAuthorizationUnavailable
	}
	return err
}

func (s *Server) updateServiceAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.updates == nil {
		apiError(w, r, http.StatusServiceUnavailable, "update_service_unavailable", "Update information is not available. Reload before checking again.")
		return false
	}
	return true
}

func updateReadBody(w http.ResponseWriter, r *http.Request) bool {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This operation does not accept a request body.")
		return false
	}
	return true
}

func (s *Server) adminServerUpdates(w http.ResponseWriter, r *http.Request) {
	if !adminSettingsNoQuery(w, r) || !updateReadBody(w, r) || !s.updateServiceAvailable(w, r) {
		return
	}
	result := s.updates.Status()
	actor := observabilityPrincipal(r)
	if err := s.updateAdministrator(r.Context(), actor, identity.AdministratorNative); err != nil {
		s.coreUpdateError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) checkAdminServerUpdates(w http.ResponseWriter, r *http.Request) {
	if _, ok := adminSettingsBody(w, r, []string{}, []string{}); !ok || !s.updateServiceAvailable(w, r) {
		return
	}
	actor := observabilityPrincipal(r)
	result, err := s.updates.Check(r.Context(), func(ctx context.Context) error {
		return s.updateAdministrator(ctx, actor, identity.AdministratorNative)
	})
	// An observed failure is a complete native status, not proof that the
	// running version is current. Transport and authority errors stay errors.
	if err != nil && !errors.Is(err, updates.ErrUnconfigured) && !errors.Is(err, updates.ErrCheckFailed) && !errors.Is(err, updates.ErrVersionUnknown) {
		s.coreUpdateError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) embySystemUpdates(w http.ResponseWriter, r *http.Request) {
	if !updateReadBody(w, r) {
		return
	}
	query, err := embyBusinessQuery(r)
	if err != nil || len(r.URL.RawQuery) > 4096 || len(query) != 1 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply PackageType=System exactly once.")
		return
	}
	packageType := ""
	for name, values := range query {
		if !strings.EqualFold(name, "PackageType") || len(values) != 1 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply PackageType=System exactly once.")
			return
		}
		packageType = values[0]
	}
	actor := observabilityPrincipal(r)
	if err := s.updateAdministrator(r.Context(), actor, identity.AdministratorEmby); err != nil {
		s.coreUpdateError(w, r, err)
		return
	}
	if packageType == "UserInstalled" {
		apiError(w, r, http.StatusNotImplemented, "plugin_updates_unsupported", "Plugin package updates are not implemented.")
		return
	}
	if packageType != "System" {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply PackageType=System exactly once.")
		return
	}
	if !s.updateServiceAvailable(w, r) {
		return
	}
	result, err := s.updates.Check(r.Context(), func(ctx context.Context) error {
		return s.updateAdministrator(ctx, actor, identity.AdministratorEmby)
	})
	if err != nil {
		s.coreUpdateError(w, r, err)
		return
	}
	packages := []map[string]any{}
	if result.State == updates.Available && result.Release != nil {
		release := result.Release
		packages = append(packages, map[string]any{
			"name": "Goby Server", "versionStr": release.Version, "classification": release.Classification,
			"description": release.Description, "sourceUrl": release.SourceUrl, "checksum": release.SHA256,
			"targetFilename": release.TargetFilename, "infoUrl": release.InfoUrl, "timestamp": release.PublishedAt,
		})
	}
	jsonResponse(w, http.StatusOK, packages)
}

func (s *Server) coreUpdateError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
	case errors.Is(err, updates.ErrUnconfigured):
		apiError(w, r, http.StatusServiceUnavailable, "update_source_unconfigured", "This deployment has not configured a Goby release source.")
	case errors.Is(err, updates.ErrCheckFailed):
		apiError(w, r, http.StatusServiceUnavailable, "update_check_failed", "The Goby release source could not be checked. The current version has not been confirmed.")
	case errors.Is(err, updates.ErrVersionUnknown):
		apiError(w, r, http.StatusConflict, "current_version_unknown", "The running Goby product version cannot be compared with the release source.")
	case errors.Is(err, updates.ErrBusy):
		apiError(w, r, http.StatusConflict, "update_check_busy", "An update check is already running. Reload its status before trying again.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "update_authorization_unavailable", "Update information could not be authorized. Reload before checking again.")
	}
}
