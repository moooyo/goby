package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/moooyo/goby/internal/settings"
)

type adminRestartController struct {
	request func()
	pending atomic.Bool
}

// WithAdministratorRestart advertises restart only when the process owner can
// perform it. The HTTP handler never exits the process or assumes a supervisor.
func WithAdministratorRestart(request func()) Option {
	return func(server *Server) {
		if request != nil {
			server.restartController = &adminRestartController{request: request}
		}
	}
}

func (s *Server) registerAdminServerPolicyRoutes(mux *http.ServeMux) {
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		authorized := s.requireAdmin(next)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authorized(w, r)
		}
	}
	mux.HandleFunc("GET /admin/v1/server-policy", wrap(s.adminServerPolicy))
	mux.HandleFunc("POST /admin/v1/server-policy/preview", wrap(s.previewAdminServerPolicy))
	mux.HandleFunc("PUT /admin/v1/server-policy", wrap(s.updateAdminServerPolicy))
	mux.HandleFunc("POST /admin/v1/server/restart", wrap(s.adminRestart))
}

func (s *Server) serverUICulture() string {
	if s.serverPolicy == nil {
		return settings.DefaultServerPolicy().UICulture
	}
	return s.serverPolicy.Snapshot().Values.UICulture
}

func (s *Server) serverPolicyClientDTO(snapshot settings.ServerPolicySnapshot, r *http.Request) map[string]any {
	address := s.policyClientAddress(r)
	var exposed any
	if address != "" {
		exposed = address
	}
	return map[string]any{"Address": exposed, "IsLocal": snapshot.IsLocal(address)}
}

func (s *Server) adminServerPolicyDTO(snapshot settings.ServerPolicySnapshot, r *http.Request) map[string]any {
	restartAvailable := s.restartController != nil && s.restartController.request != nil
	restartPending := restartAvailable && s.restartController.pending.Load()
	return map[string]any{
		"Revision":            strconv.FormatInt(snapshot.Revision, 10),
		"EnableRemoteAccess":  snapshot.Values.EnableRemoteAccess,
		"LocalNetworkSubnets": append([]string{}, snapshot.Values.LocalNetworkSubnets...),
		"UICulture":           snapshot.Values.UICulture, "UpdatedAt": snapshot.UpdatedAt,
		"Client":  s.serverPolicyClientDTO(snapshot, r),
		"Restart": map[string]bool{"Available": restartAvailable, "InProgress": restartPending},
		"Updates": s.updatePolicySummary(),
	}
}

func (s *Server) adminServerPolicy(w http.ResponseWriter, r *http.Request) {
	if !adminSettingsNoQuery(w, r) {
		return
	}
	if s.serverPolicy == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	snapshot, err := s.serverPolicy.Get(r.Context(), adminSettingsActor(r))
	if err != nil {
		s.serverPolicyError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.adminServerPolicyDTO(snapshot, r))
}

func decodeAdminServerPolicy(w http.ResponseWriter, r *http.Request, mutation bool) (settings.ServerPolicyUpdate, bool) {
	fields := []string{"Revision", "EnableRemoteAccess", "LocalNetworkSubnets", "UICulture"}
	if mutation {
		fields = append(fields, "ConfirmDisconnect")
	}
	values, ok := adminSettingsBody(w, r, fields, fields)
	if !ok {
		return settings.ServerPolicyUpdate{}, false
	}
	invalid := map[string]string{}
	request := settings.ServerPolicyUpdate{Revision: adminSettingsRevision(values["Revision"], invalid)}
	var enabled *bool
	if json.Unmarshal(values["EnableRemoteAccess"], &enabled) != nil || enabled == nil {
		invalid["EnableRemoteAccess"] = "Supply a boolean."
	} else {
		request.Values.EnableRemoteAccess = *enabled
	}
	if json.Unmarshal(values["LocalNetworkSubnets"], &request.Values.LocalNetworkSubnets) != nil {
		invalid["LocalNetworkSubnets"] = "Supply an array of canonical network CIDRs."
	}
	if json.Unmarshal(values["UICulture"], &request.Values.UICulture) != nil {
		invalid["UICulture"] = "Select zh-CN or en-US."
	}
	if mutation {
		var confirmed *bool
		if json.Unmarshal(values["ConfirmDisconnect"], &confirmed) != nil || confirmed == nil {
			invalid["ConfirmDisconnect"] = "Supply a boolean."
		} else {
			request.ConfirmDisconnect = *confirmed
		}
	}
	if err := settings.ValidateServerPolicy(request.Values); err != nil {
		var validation *settings.ValidationError
		if errors.As(err, &validation) {
			for field, message := range validation.Fields {
				invalid[field] = message
			}
		}
	}
	if len(invalid) != 0 {
		adminSettingsInputError(w, r, invalid)
		return settings.ServerPolicyUpdate{}, false
	}
	return request, true
}

func (s *Server) previewAdminServerPolicy(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAdminServerPolicy(w, r, false)
	if !ok {
		return
	}
	if s.serverPolicy == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	current, err := s.serverPolicy.Get(r.Context(), adminSettingsActor(r))
	if err != nil {
		s.serverPolicyError(w, r, err)
		return
	}
	if current.Revision != request.Revision {
		s.settingsError(w, r, settings.ErrRevisionConflict)
		return
	}
	address := s.policyClientAddress(r)
	local, allowed, err := settings.PreviewServerPolicy(request.Values, address)
	if err != nil {
		s.serverPolicyError(w, r, err)
		return
	}
	var exposed any
	if address != "" {
		exposed = address
	}
	jsonResponse(w, http.StatusOK, map[string]any{
		"Revision": strconv.FormatInt(current.Revision, 10), "WouldDisconnect": !allowed,
		"Client": map[string]any{"Address": exposed, "IsLocal": local},
	})
}

func (s *Server) updateAdminServerPolicy(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeAdminServerPolicy(w, r, true)
	if !ok {
		return
	}
	if s.serverPolicy == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	request.ClientAddress = s.policyClientAddress(r)
	committed, err := s.serverPolicy.Update(r.Context(), adminSettingsActor(r), request)
	if err != nil {
		s.serverPolicyError(w, r, err)
		return
	}
	result := s.adminServerPolicyDTO(committed, r)
	result["DisconnectExpected"] = !committed.Allows(request.ClientAddress)
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) serverPolicyError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, settings.ErrServerPolicyDenied) {
		apiError(w, r, http.StatusForbidden, "remote_access_disabled", "Remote access is disabled for this client address.")
		return
	}
	var invalid *settings.ValidationError
	if errors.As(err, &invalid) {
		adminSettingsInputError(w, r, invalid.Fields)
		return
	}
	s.settingsError(w, r, err)
}

func (s *Server) adminRestart(w http.ResponseWriter, r *http.Request) {
	values, ok := adminSettingsBody(w, r, []string{"Revision", "Confirm"}, []string{"Revision", "Confirm"})
	if !ok {
		return
	}
	invalid := map[string]string{}
	revision := adminSettingsRevision(values["Revision"], invalid)
	var confirm bool
	if json.Unmarshal(values["Confirm"], &confirm) != nil || !confirm {
		invalid["Confirm"] = "Confirm the restart."
	}
	if len(invalid) != 0 {
		adminSettingsInputError(w, r, invalid)
		return
	}
	if s.serverPolicy == nil {
		s.settingsError(w, r, settings.ErrUnavailable)
		return
	}
	snapshot, err := s.serverPolicy.Get(r.Context(), adminSettingsActor(r))
	if err != nil {
		s.serverPolicyError(w, r, err)
		return
	}
	if snapshot.Revision != revision {
		s.settingsError(w, r, settings.ErrRevisionConflict)
		return
	}
	controller := s.restartController
	if controller == nil || controller.request == nil {
		apiError(w, r, http.StatusConflict, "restart_unavailable", "This deployment has not enabled a managed restart.")
		return
	}
	if !controller.pending.CompareAndSwap(false, true) {
		apiError(w, r, http.StatusConflict, "restart_pending", "A restart has already been requested. Reconnect after it completes.")
		return
	}
	// An accepted request is not proof that the replacement process is ready.
	// Allow the acknowledgement to leave the handler before its owner drains it.
	jsonResponse(w, http.StatusAccepted, map[string]bool{"Accepted": true, "Restarting": true})
	_ = http.NewResponseController(w).Flush()
	time.AfterFunc(250*time.Millisecond, controller.request)
}

// admitServerPolicy runs before CORS, authentication and WebSocket dispatch.
// Health checks remain usable by the deployment without exposing media or
// account data. All application endpoints, including ping and static assets,
// share the access boundary.
func (s *Server) admitServerPolicy(w http.ResponseWriter, r *http.Request) (*http.Request, func(), bool) {
	if s.serverPolicy == nil || r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
		return r, func() {}, true
	}
	address := s.policyClientAddress(r)
	boundedPolicyMutation := r.Method == http.MethodPut && r.URL.Path == "/admin/v1/server-policy" ||
		r.Method == http.MethodPost && (r.URL.Path == "/emby/System/Configuration" || r.URL.Path == "/emby/System/Configuration/Partial")
	if boundedPolicyMutation {
		// These bounded configuration operations may deliver a confirmed result after
		// committing a self-disconnecting policy. Core configuration writes
		// recheck the current ACL after locking their policy row in the same
		// transaction. Namespace aliases are canonicalized before admission.
		if s.serverPolicy.Snapshot().Allows(address) {
			return r, func() {}, true
		}
		apiError(w, r, http.StatusForbidden, "remote_access_disabled", "Remote access is disabled for this client address.")
		return r, func() {}, false
	}
	ctx, release, allowed := s.serverPolicy.Watch(r.Context(), address)
	if !allowed {
		apiError(w, r, http.StatusForbidden, "remote_access_disabled", "Remote access is disabled for this client address.")
		return r, func() {}, false
	}
	interrupted := make(chan struct{})
	stopInterrupt := context.AfterFunc(ctx, func() {
		defer close(interrupted)
		if errors.Is(context.Cause(ctx), settings.ErrServerPolicyDenied) {
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Now())
			_ = controller.SetWriteDeadline(time.Now())
		}
	})
	finish := func() {
		if !stopInterrupt() {
			<-interrupted
		}
		release()
	}
	return r.WithContext(ctx), finish, true
}
