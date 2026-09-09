package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
)

func (s *Server) registerAdminUserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/users/{id}", s.requireAdmin(s.managedUser))
	mux.HandleFunc("PUT /admin/v1/users/{id}", s.requireAdmin(s.updateManagedUser))
	mux.HandleFunc("POST /admin/v1/users/{id}/password", s.requireAdmin(s.resetManagedUserPassword))
}

func nativeManagedUser(managed identity.ManagedUser) map[string]any {
	user := nativeUser(managed.User)
	user["Revision"] = strconv.FormatInt(managed.Revision, 10)
	user["Policy"] = map[string]any{
		"EnableAllFolders":               managed.Policy.EnableAllFolders,
		"EnabledFolders":                 append([]string{}, managed.Policy.EnabledFolders...),
		"EnableMediaPlayback":            managed.Policy.EnableMediaPlayback,
		"EnablePlaybackRemuxing":         managed.Policy.EnablePlaybackRemuxing,
		"EnableAudioPlaybackTranscoding": managed.Policy.EnableAudioPlaybackTranscoding,
		"EnableVideoPlaybackTranscoding": managed.Policy.EnableVideoPlaybackTranscoding,
	}
	return user
}

func (s *Server) managedUser(w http.ResponseWriter, r *http.Request) {
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	user, err := s.identity.GetManagedUser(r.Context(), id)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"User": nativeManagedUser(user)})
}

func (s *Server) updateManagedUser(w http.ResponseWriter, r *http.Request) {
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	input, ok := decodeManagedUserUpdate(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.UpdateManagedUser(r.Context(), actor, id, input)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	s.managedUserMutation(w, r, actor, "update_user", result)
}

func (s *Server) resetManagedUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := managedUserID(w, r)
	if !ok {
		return
	}
	revision, password, ok := decodeManagedUserPassword(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	result, err := s.identity.ResetManagedUserPassword(r.Context(), actor, id, revision, password)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	s.managedUserMutation(w, r, actor, "reset_user_password", result)
}

func (s *Server) managedUserMutation(w http.ResponseWriter, r *http.Request, actor identity.Principal, action string, result identity.ManagedUserMutation) {
	if result.CurrentSessionRevoked {
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/admin", HttpOnly: true, Secure: s.cfg.CookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
	s.log.Info("administrator user mutation", "actor_id", actor.User.ID, "user_id", result.User.User.ID, "action", action)
	jsonResponse(w, http.StatusOK, map[string]any{"User": nativeManagedUser(result.User), "CurrentSessionRevoked": result.CurrentSessionRevoked})
}

func managedUserInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the highlighted user fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func managedUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		managedUserInputError(w, r, map[string]string{"Id": "Supply a user identifier of 1 to 256 UTF-8 bytes without surrounding whitespace or control characters."})
		return "", false
	}
	return id, true
}

func (s *Server) managedUserError(w http.ResponseWriter, r *http.Request, err error) {
	var validation *identity.ManagedUserValidationError
	switch {
	case errors.Is(err, identity.ErrRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "This user changed after it was loaded. Reload the user before saving again.")
	case errors.Is(err, identity.ErrLastAdministrator):
		apiError(w, r, http.StatusConflict, "last_administrator", "Keep at least one enabled administrator.")
	case errors.As(err, &validation):
		managedUserInputError(w, r, validation.Fields)
	case errors.Is(err, identity.ErrInvalidInput):
		managedUserInputError(w, r, map[string]string{"User": "The user request is invalid."})
	default:
		s.identityError(w, r, err)
	}
}

// The managed-user write contract is complete and case-sensitive. Decode each
// object explicitly so missing, null, duplicate, and unknown fields cannot be
// silently converted to zero values or last-key-wins account mutations.
func managedUserObject(data []byte, fields []string, path string) (map[string]json.RawMessage, map[string]string) {
	fieldPath := func(name string) string {
		if path == "" {
			return name
		}
		return path + "." + name
	}
	objectPath := path
	if objectPath == "" {
		objectPath = "Body"
	}
	invalid := func(message string) (map[string]json.RawMessage, map[string]string) {
		return nil, map[string]string{objectPath: message}
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return invalid("Supply a JSON object.")
	}
	values := make(map[string]json.RawMessage, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return invalid("Supply a valid JSON object.")
		}
		name, ok := token.(string)
		if !ok || !allowed[name] {
			return invalid("The object contains an unsupported field.")
		}
		if _, exists := values[name]; exists {
			return nil, map[string]string{fieldPath(name): "Supply each field exactly once."}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return invalid("Supply a valid JSON object.")
		}
		values[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return invalid("Supply a valid JSON object.")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return invalid("Supply exactly one JSON object.")
	}
	missing := make(map[string]string)
	for _, field := range fields {
		if _, exists := values[field]; !exists {
			missing[fieldPath(field)] = "This field is required."
		}
	}
	if len(missing) != 0 {
		return nil, missing
	}
	return values, nil
}

func managedUserBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		managedUserInputError(w, r, map[string]string{"Body": "Supply a JSON object no larger than 1 MiB."})
		return nil, false
	}
	if !utf8.Valid(data) {
		managedUserInputError(w, r, map[string]string{"Body": "Supply a UTF-8 JSON object."})
		return nil, false
	}
	values, invalid := managedUserObject(data, fields, "")
	if invalid != nil {
		managedUserInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func managedUserValue[T any](raw json.RawMessage, field string, dst *T, invalid map[string]string) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, dst) != nil {
		invalid[field] = "Supply a value of the required type."
	}
}

func managedUserRevision(raw json.RawMessage, invalid map[string]string) int64 {
	var value string
	managedUserValue(raw, "Revision", &value, invalid)
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != value {
		invalid["Revision"] = "Supply the current positive decimal revision string."
	}
	return revision
}

func decodeManagedUserUpdate(w http.ResponseWriter, r *http.Request) (identity.ManagedUserUpdate, bool) {
	var input identity.ManagedUserUpdate
	values, ok := managedUserBody(w, r, []string{"Revision", "Name", "IsAdministrator", "IsDisabled", "Policy"})
	if !ok {
		return input, false
	}
	invalid := make(map[string]string)
	input.Revision = managedUserRevision(values["Revision"], invalid)
	managedUserValue(values["Name"], "Name", &input.Name, invalid)
	managedUserValue(values["IsAdministrator"], "IsAdministrator", &input.IsAdministrator, invalid)
	managedUserValue(values["IsDisabled"], "IsDisabled", &input.IsDisabled, invalid)
	policy, policyErrors := managedUserObject(values["Policy"], []string{
		"EnableAllFolders", "EnabledFolders", "EnableMediaPlayback", "EnablePlaybackRemuxing", "EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding",
	}, "Policy")
	for field, message := range policyErrors {
		invalid[field] = message
	}
	if policyErrors == nil {
		managedUserValue(policy["EnableAllFolders"], "Policy.EnableAllFolders", &input.Policy.EnableAllFolders, invalid)
		managedUserValue(policy["EnableMediaPlayback"], "Policy.EnableMediaPlayback", &input.Policy.EnableMediaPlayback, invalid)
		managedUserValue(policy["EnablePlaybackRemuxing"], "Policy.EnablePlaybackRemuxing", &input.Policy.EnablePlaybackRemuxing, invalid)
		managedUserValue(policy["EnableAudioPlaybackTranscoding"], "Policy.EnableAudioPlaybackTranscoding", &input.Policy.EnableAudioPlaybackTranscoding, invalid)
		managedUserValue(policy["EnableVideoPlaybackTranscoding"], "Policy.EnableVideoPlaybackTranscoding", &input.Policy.EnableVideoPlaybackTranscoding, invalid)
		var folders []json.RawMessage
		managedUserValue(policy["EnabledFolders"], "Policy.EnabledFolders", &folders, invalid)
		input.Policy.EnabledFolders = make([]string, len(folders))
		for index, folder := range folders {
			managedUserValue(folder, "Policy.EnabledFolders", &input.Policy.EnabledFolders[index], invalid)
		}
	}
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return identity.ManagedUserUpdate{}, false
	}
	return input, true
}

func decodeManagedUserPassword(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	values, ok := managedUserBody(w, r, []string{"Revision", "Password"})
	if !ok {
		return 0, "", false
	}
	invalid := make(map[string]string)
	revision := managedUserRevision(values["Revision"], invalid)
	var password string
	managedUserValue(values["Password"], "Password", &password, invalid)
	if password == "" {
		invalid["Password"] = "Enter a nonempty password."
	}
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return 0, "", false
	}
	return revision, password, true
}
