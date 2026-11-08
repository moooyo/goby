package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
)

type adminBackupPolicyManager interface {
	BackupPolicy(context.Context, identity.Principal) (recovery.BackupPolicyView, error)
	UpdateBackupPolicy(context.Context, identity.Principal, recovery.BackupPolicyRequest) (recovery.BackupPolicyView, error)
}

type adminBackupPolicySettingsManager interface {
	UpdateBackupPolicySettings(context.Context, identity.Principal, recovery.BackupPolicySettingsRequest) (recovery.BackupPolicyView, error)
}

func (s *Server) registerAdminBackupPolicyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/backups/policy", adminBackupNoCache(s.requireAdmin(s.adminBackupPolicy)))
	mux.HandleFunc("PUT /admin/v1/backups/policy", adminBackupNoCache(s.requireAdmin(s.updateAdminBackupPolicy)))
	mux.HandleFunc("PATCH /admin/v1/backups/policy", adminBackupNoCache(s.requireAdmin(s.patchAdminBackupPolicy)))
}

func (s *Server) adminBackupPolicy(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.recovery.(adminBackupPolicyManager)
	if !ok || !adminBackupHeaders(r) || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		if !ok {
			s.adminBackupError(w, r, recovery.ErrUnavailable)
		} else {
			s.adminBackupError(w, r, recovery.ErrInvalid)
		}
		return
	}
	actor, _ := r.Context().Value(principalKey).(identity.Principal)
	view, err := manager.BackupPolicy(r.Context(), actor)
	if err != nil {
		s.adminBackupError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, view)
}

func readAdminBackupPolicy(w http.ResponseWriter, r *http.Request) (recovery.BackupPolicyRequest, error) {
	return readAdminBackupPolicyInput(w, r, false)
}

func readAdminBackupPolicyInput(w http.ResponseWriter, r *http.Request, settingsOnly bool) (recovery.BackupPolicyRequest, error) {
	var request recovery.BackupPolicyRequest
	if !adminBackupContentType(r, false) {
		return request, errAdminBackupMediaType
	}
	body, finish, err := newAdminBackupBody(w, r, adminBackupJSONLimit, adminBackupJSONTimeout)
	if err != nil {
		return request, err
	}
	defer finish()
	data, err := io.ReadAll(body)
	defer clear(data)
	if err != nil {
		return request, err
	}
	if !utf8.Valid(data) || !adminSettingsUnicode(data) {
		return request, recovery.ErrInvalid
	}
	required := []string{"Revision", "Enabled", "Schedule", "KeepCount", "IncludeImages"}
	if settingsOnly {
		required = []string{"Revision", "KeepCount", "IncludeImages"}
	}
	allowed := append(append([]string{}, required...), "Passphrase")
	values, invalid := adminTaskObject(data, allowed, required, "")
	if invalid != nil {
		clearAdminBackupObject(values)
		return request, recovery.ErrInvalid
	}
	defer clearAdminBackupObject(values)
	var ok bool
	if request.Revision, ok = adminBackupRevision(values["Revision"], true); !ok {
		return request, recovery.ErrInvalid
	}
	booleans := map[string]*bool{"IncludeImages": &request.IncludeImages}
	if !settingsOnly {
		booleans["Enabled"] = &request.Enabled
	}
	for name, target := range booleans {
		if bytes.Equal(bytes.TrimSpace(values[name]), []byte("null")) || json.Unmarshal(values[name], target) != nil {
			return request, recovery.ErrInvalid
		}
	}
	if bytes.Equal(bytes.TrimSpace(values["KeepCount"]), []byte("null")) || json.Unmarshal(values["KeepCount"], &request.KeepCount) != nil {
		return request, recovery.ErrInvalid
	}
	if !settingsOnly {
		schedule, invalid := adminTaskObject(values["Schedule"], []string{"Kind", "DayOfWeek", "TimeOfDay", "Timezone"}, []string{"Kind", "DayOfWeek", "TimeOfDay", "Timezone"}, "Schedule")
		if invalid != nil {
			clearAdminBackupObject(schedule)
			return request, recovery.ErrInvalid
		}
		defer clearAdminBackupObject(schedule)
		for name, target := range map[string]*string{"Kind": &request.Schedule.Kind, "TimeOfDay": &request.Schedule.TimeOfDay, "Timezone": &request.Schedule.Timezone} {
			if *target, ok = adminBackupString(schedule[name]); !ok {
				return request, recovery.ErrInvalid
			}
		}
		if !bytes.Equal(bytes.TrimSpace(schedule["DayOfWeek"]), []byte("null")) {
			var day int
			if json.Unmarshal(schedule["DayOfWeek"], &day) != nil {
				return request, recovery.ErrInvalid
			}
			request.Schedule.DayOfWeek = &day
		}
	}
	if raw, exists := values["Passphrase"]; exists {
		var passphrase []byte
		if bytes.Equal(bytes.TrimSpace(raw), []byte(`""`)) {
			passphrase = []byte{}
		} else {
			passphrase, ok = adminBackupPassphrase(raw)
			if !ok {
				return request, &recovery.BackupPolicyValidationError{Fields: map[string]string{"Passphrase": "Use an encryption passphrase of 12 to 1,024 UTF-8 bytes."}}
			}
		}
		request.Passphrase = &passphrase
	}
	return request, nil
}

func (s *Server) updateAdminBackupPolicy(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.recovery.(adminBackupPolicyManager)
	if !ok {
		s.adminBackupError(w, r, recovery.ErrUnavailable)
		return
	}
	if !adminBackupHeaders(r) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.adminBackupError(w, r, recovery.ErrInvalid)
		return
	}
	request, err := readAdminBackupPolicy(w, r)
	if request.Passphrase != nil {
		defer clear(*request.Passphrase)
	}
	var view recovery.BackupPolicyView
	if err == nil {
		actor, _ := r.Context().Value(principalKey).(identity.Principal)
		view, err = manager.UpdateBackupPolicy(r.Context(), actor, request)
	}
	if err != nil {
		s.adminBackupPolicyError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, view)
}

func (s *Server) patchAdminBackupPolicy(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.recovery.(adminBackupPolicySettingsManager)
	if !ok {
		s.adminBackupError(w, r, recovery.ErrUnavailable)
		return
	}
	if !adminBackupHeaders(r) || r.URL.RawQuery != "" || r.URL.ForceQuery {
		s.adminBackupError(w, r, recovery.ErrInvalid)
		return
	}
	input, err := readAdminBackupPolicyInput(w, r, true)
	if input.Passphrase != nil {
		defer clear(*input.Passphrase)
	}
	var view recovery.BackupPolicyView
	if err == nil {
		actor, _ := r.Context().Value(principalKey).(identity.Principal)
		view, err = manager.UpdateBackupPolicySettings(r.Context(), actor, recovery.BackupPolicySettingsRequest{
			Revision: input.Revision, KeepCount: input.KeepCount, IncludeImages: input.IncludeImages, Passphrase: input.Passphrase,
		})
	}
	if err != nil {
		s.adminBackupPolicyError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, view)
}

func (s *Server) adminBackupPolicyError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *recovery.BackupPolicyValidationError
	if errors.As(err, &validation) {
		requestID, _ := r.Context().Value(requestIDKey).(string)
		jsonResponse(w, http.StatusBadRequest, map[string]any{"Error": map[string]any{"Code": "invalid_input", "Message": "Check the automatic backup settings.", "Fields": validation.Fields}, "RequestId": requestID})
	} else {
		s.adminBackupError(w, r, err)
	}
}
