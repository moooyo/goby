package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/notifications"
)

func (s *Server) initializeNotifications() {
	s.notificationStore = notifications.NewStore(s.db, s.identity, s.library)
	s.notificationRuntime = notifications.NewRuntime(s.notificationStore, s.notificationOptions)
}

func WithNotificationTrustRoots(roots *x509.CertPool) Option {
	return func(s *Server) {
		if roots != nil {
			s.notificationOptions.RootCAs = roots.Clone()
		}
	}
}
func (s *Server) registerNotificationRoutes(mux *http.ServeMux) {
	for _, method := range []string{"GET", "PUT"} {
		mux.HandleFunc(method+" /admin/v1/notifications", s.requireAdmin(s.adminNotifications))
	}
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		mux.HandleFunc(method+" /emby/Sessions/Notifications", s.requireEmby(s.personalNotifications))
	}
	mux.HandleFunc("POST /emby/Sessions/Notifications/Test", s.requireEmby(s.testPersonalNotification))
}
func notificationBody(w http.ResponseWriter, r *http.Request, allowed, required []string, out any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		apiError(w, r, 415, "notification_invalid", "Use an application/json notification request.")
		return false
	}
	if r.Body == nil {
		apiError(w, r, 400, "notification_invalid", "Supply a notification request.")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 16385))
	if err != nil || len(raw) > 16384 || !utf8.Valid(raw) || !adminSettingsUnicode(raw) {
		apiError(w, r, 400, "notification_invalid", "Use a bounded lossless notification request.")
		return false
	}
	values, invalid := adminTaskObject(raw, allowed, required, "")
	if len(invalid) != 0 {
		apiError(w, r, 400, "notification_invalid", "Check the notification request fields.")
		return false
	}
	for _, value := range values {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			apiError(w, r, 400, "notification_invalid", "Notification fields cannot be null.")
			return false
		}
	}
	if json.Unmarshal(raw, out) != nil {
		apiError(w, r, 400, "notification_invalid", "Check the notification request field types.")
		return false
	}
	return true
}
func (s *Server) notificationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, notifications.ErrInvalid):
		apiError(w, r, 400, "notification_invalid", "Check the notification transport, fields, and limits.")
	case errors.Is(err, notifications.ErrConflict):
		apiError(w, r, 409, "notification_conflict", "Reload notification settings before changing them.")
	case errors.Is(err, notifications.ErrLimit):
		apiError(w, r, 429, "notification_capacity", "Notification capacity is currently full.")
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, 403, "notification_registration_unsupported", "Personal notifications require an ordinary Emby login session.")
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	default:
		apiError(w, r, 503, "notification_unavailable", "The notification service is unavailable.")
	}
}
func notificationResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	jsonResponse(w, 200, value)
}
func (s *Server) adminNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notificationStore == nil {
		s.notificationError(w, r, notifications.ErrUnavailable)
		return
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.notificationError(w, r, notifications.ErrInvalid)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if r.Method == http.MethodGet {
		value, err := s.notificationStore.Config(r.Context(), actor)
		if err != nil {
			s.notificationError(w, r, err)
			return
		}
		notificationResponse(w, value)
		return
	}
	var input notifications.ConfigUpdate
	if !notificationBody(w, r, []string{"Revision", "Enabled", "Endpoint", "AllowedNetworks", "ReceiverCredential"}, []string{"Revision", "Enabled", "Endpoint", "AllowedNetworks"}, &input) {
		return
	}
	result, err := s.notificationStore.UpdateConfig(r.Context(), actor, input)
	if err == nil {
		rev, _ := strconv.ParseInt(result.Revision, 10, 64)
		err = s.notificationRuntime.FenceConfig(r.Context(), rev)
	}
	if err != nil {
		s.notificationError(w, r, err)
		return
	}
	notificationResponse(w, result)
}
func (s *Server) personalNotifications(w http.ResponseWriter, r *http.Request) {
	if s.notificationStore == nil {
		s.notificationError(w, r, notifications.ErrUnavailable)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if r.Method == http.MethodGet {
		result, err := s.notificationStore.Registration(r.Context(), actor)
		if err != nil {
			s.notificationError(w, r, err)
			return
		}
		notificationResponse(w, result)
		return
	}
	var result notifications.Registration
	var err error
	if r.Method == http.MethodDelete {
		values, e := streamValues(r)
		if e != nil {
			s.notificationError(w, r, notifications.ErrInvalid)
			return
		}
		result, err = s.notificationStore.DeleteRegistration(r.Context(), actor, values["revision"])
	} else {
		var input notifications.RegistrationUpdate
		if !notificationBody(w, r, []string{"Revision", "Transport", "TargetToken", "EventIds"}, []string{"Revision", "Transport", "EventIds"}, &input) {
			return
		}
		result, err = s.notificationStore.PutRegistration(r.Context(), actor, input)
	}
	if err == nil {
		rev, _ := strconv.ParseInt(result.Revision, 10, 64)
		err = s.notificationRuntime.FenceRegistration(r.Context(), result.Id, rev)
	}
	if err != nil {
		s.notificationError(w, r, err)
		return
	}
	notificationResponse(w, result)
}
func (s *Server) testPersonalNotification(w http.ResponseWriter, r *http.Request) {
	if s.notificationStore == nil {
		s.notificationError(w, r, notifications.ErrUnavailable)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if err := s.notificationStore.EnqueueTest(r.Context(), actor); err != nil {
		s.notificationError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
}
func (s *Server) retireNotificationSession(ctx context.Context, id string) error {
	if s.notificationRuntime == nil {
		return nil
	}
	return s.notificationRuntime.FenceSession(ctx, id)
}
