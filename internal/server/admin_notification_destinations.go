package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/notifications"
)

func (s *Server) registerAdminNotificationDestinationRoutes(mux *http.ServeMux) {
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		mux.HandleFunc(method+" /admin/v1/notification-destinations", s.requireAdmin(s.adminNotificationDestinations))
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		mux.HandleFunc(method+" /admin/v1/notification-destinations/{id}", s.requireAdmin(s.adminNotificationDestination))
	}
	mux.HandleFunc("POST /admin/v1/notification-destinations/{id}/test", s.requireAdmin(s.testAdminNotificationDestination))
}

func (s *Server) adminDestinationError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, notifications.ErrDestinationInactive) {
		apiError(w, r, http.StatusConflict, "notification_destination_inactive", "Save this destination again under an active administrator account before testing.")
		return
	}
	if errors.Is(err, notifications.ErrDestinationNotFound) {
		apiError(w, r, http.StatusNotFound, "notification_destination_not_found", "The notification destination no longer exists.")
		return
	}
	s.notificationError(w, r, err)
}

func (s *Server) adminDestinationRequest(w http.ResponseWriter, r *http.Request) bool {
	if s.notificationStore == nil {
		s.notificationError(w, r, notifications.ErrUnavailable)
		return false
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.notificationError(w, r, notifications.ErrInvalid)
		return false
	}
	return true
}

func readAdminDestinationInput(w http.ResponseWriter, r *http.Request, input *notifications.AdminDestinationInput) bool {
	fields := []string{"Revision", "Name", "Transport", "Enabled", "EventIds", "AllowedNetworks", "Endpoint", "SMTP", "Secret"}
	return notificationBody(w, r, fields, fields[:len(fields)-1], input)
}

func (s *Server) adminNotificationDestinations(w http.ResponseWriter, r *http.Request) {
	if !s.adminDestinationRequest(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if r.Method == http.MethodGet {
		result, err := s.notificationStore.AdminDestinations(r.Context(), actor)
		if err != nil {
			s.adminDestinationError(w, r, err)
			return
		}
		notificationResponse(w, result)
		return
	}
	var input notifications.AdminDestinationInput
	if !readAdminDestinationInput(w, r, &input) {
		return
	}
	result, err := s.notificationStore.PutAdminDestination(r.Context(), actor, "", input)
	if err != nil {
		s.adminDestinationError(w, r, err)
		return
	}
	notificationResponse(w, result)
}

func (s *Server) adminNotificationDestination(w http.ResponseWriter, r *http.Request) {
	if !s.adminDestinationRequest(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	id := r.PathValue("id")
	if r.Method == http.MethodDelete {
		var input struct{ Revision string }
		if !notificationBody(w, r, []string{"Revision"}, []string{"Revision"}, &input) {
			return
		}
		if err := s.notificationStore.DeleteAdminDestination(r.Context(), actor, id, input.Revision); err != nil {
			s.adminDestinationError(w, r, err)
			return
		}
		if err := s.notificationRuntime.FenceAdminDestination(r.Context(), id, 0); err != nil {
			s.notificationError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var input notifications.AdminDestinationInput
	if !readAdminDestinationInput(w, r, &input) {
		return
	}
	result, err := s.notificationStore.PutAdminDestination(r.Context(), actor, id, input)
	if err == nil {
		current, _ := strconv.ParseInt(result.Revision, 10, 64)
		err = s.notificationRuntime.FenceAdminDestination(r.Context(), id, current)
	}
	if err != nil {
		s.adminDestinationError(w, r, err)
		return
	}
	notificationResponse(w, result)
}

func (s *Server) testAdminNotificationDestination(w http.ResponseWriter, r *http.Request) {
	if !s.adminDestinationRequest(w, r) {
		return
	}
	var input struct{ Revision string }
	if !notificationBody(w, r, []string{"Revision"}, []string{"Revision"}, &input) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	id, err := s.notificationStore.TestAdminDestination(r.Context(), actor, r.PathValue("id"), input.Revision)
	if err != nil {
		s.adminDestinationError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusAccepted, map[string]string{"Id": id, "State": "pending"})
}
