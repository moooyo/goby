package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

var legacyManagedPolicyFields = []string{
	"EnableAllFolders", "EnabledFolders", "EnableMediaPlayback", "EnablePlaybackRemuxing",
	"EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding",
}

var managedPolicyFields = []string{
	"EnableAllFolders", "EnabledFolders", "EnableMediaPlayback", "EnablePlaybackRemuxing",
	"EnableAudioPlaybackTranscoding", "EnableVideoPlaybackTranscoding", "IsHidden", "IsHiddenRemotely",
	"IsHiddenFromUnusedDevices", "MaxParentalRating", "AllowTagOrRating", "BlockedTags",
	"IsTagBlockingModeInclusive", "IncludeTags", "EnableUserPreferenceAccess", "AccessSchedules",
	"BlockUnratedItems", "EnableRemoteControlOfOtherUsers", "EnableSharedDeviceControl", "EnableRemoteAccess",
	"AutoRemoteQuality", "EnableContentDeletion", "RestrictedFeatures", "EnableContentDeletionFromFolders",
	"EnableContentDownloading", "EnableSubtitleDownloading", "EnableSubtitleManagement",
	"RemoteClientBitrateLimit", "ExcludedSubFolders", "SimultaneousStreamLimit", "EnabledDevices", "EnableAllDevices",
}

// Only supported configuration facts cross the native boundary. Unknown stored
// JSON and computed account facts never become editable policy fields.
func nativeManagedPolicy(policy identity.ManagedPolicy) map[string]any {
	return map[string]any{
		"EnableAllFolders": policy.EnableAllFolders, "EnabledFolders": append([]string{}, policy.EnabledFolders...),
		"EnableMediaPlayback": policy.EnableMediaPlayback, "EnablePlaybackRemuxing": policy.EnablePlaybackRemuxing,
		"EnableAudioPlaybackTranscoding": policy.EnableAudioPlaybackTranscoding, "EnableVideoPlaybackTranscoding": policy.EnableVideoPlaybackTranscoding,
		"IsHidden": policy.IsHidden, "IsHiddenRemotely": policy.IsHiddenRemotely, "IsHiddenFromUnusedDevices": policy.IsHiddenFromUnusedDevices,
		"MaxParentalRating": policy.MaxParentalRating, "AllowTagOrRating": policy.AllowTagOrRating,
		"BlockedTags": append([]string{}, policy.BlockedTags...), "IsTagBlockingModeInclusive": policy.IsTagBlockingModeInclusive,
		"IncludeTags": append([]string{}, policy.IncludeTags...), "EnableUserPreferenceAccess": policy.EnableUserPreferenceAccess,
		"AccessSchedules": append([]identity.AccessSchedule{}, policy.AccessSchedules...), "BlockUnratedItems": append([]string{}, policy.BlockUnratedItems...),
		"EnableRemoteControlOfOtherUsers": policy.EnableRemoteControlOfOtherUsers, "EnableSharedDeviceControl": policy.EnableSharedDeviceControl,
		"EnableRemoteAccess": policy.EnableRemoteAccess, "AutoRemoteQuality": policy.AutoRemoteQuality,
		"EnableContentDeletion": policy.EnableContentDeletion, "RestrictedFeatures": append([]string{}, policy.RestrictedFeatures...),
		"EnableContentDeletionFromFolders": append([]string{}, policy.EnableContentDeletionFromFolders...),
		"EnableContentDownloading":         policy.EnableContentDownloading, "EnableSubtitleDownloading": policy.EnableSubtitleDownloading,
		"EnableSubtitleManagement": policy.EnableSubtitleManagement, "RemoteClientBitrateLimit": policy.RemoteClientBitrateLimit,
		"ExcludedSubFolders": append([]string{}, policy.ExcludedSubFolders...), "SimultaneousStreamLimit": policy.SimultaneousStreamLimit,
		"EnabledDevices": append([]string{}, policy.EnabledDevices...), "EnableAllDevices": policy.EnableAllDevices,
	}
}

func mergeManagedPolicy(base identity.ManagedPolicy, patch map[string]json.RawMessage) (identity.ManagedPolicy, error) {
	values := nativeManagedPolicy(base)
	invalid := make(map[string]string)
	for name, value := range patch {
		field := "Policy." + name
		switch values[name].(type) {
		case bool:
			var flag bool
			managedUserValue(value, field, &flag, invalid)
		case int:
			var number int
			managedUserValue(value, field, &number, invalid)
		case []string:
			var entries []json.RawMessage
			managedUserValue(value, field, &entries, invalid)
			for _, entry := range entries {
				var text string
				managedUserValue(entry, field, &text, invalid)
			}
		case []identity.AccessSchedule:
			var schedules []identity.AccessSchedule
			managedUserValue(value, field, &schedules, invalid)
		case *int:
			var rating *int
			if json.Unmarshal(value, &rating) != nil {
				invalid[field] = "Supply a nonnegative integer or null."
			}
		default:
			invalid[field] = "The policy field is not supported."
		}
		values[name] = value
	}
	if len(invalid) != 0 {
		return identity.ManagedPolicy{}, &identity.ManagedUserValidationError{Fields: invalid}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return identity.ManagedPolicy{}, identity.ErrInvalidInput
	}
	return identity.ParseManagedPolicy(data)
}

func (s *Server) registerUserManagementRoutes(mux *http.ServeMux) {
	s.registerUserPreferenceRoutes(mux)
	wrap := func(next http.HandlerFunc) http.HandlerFunc {
		return s.requireEmby(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			next(w, r)
		})
	}
	mux.HandleFunc("POST /emby/Users/New", wrap(s.createEmbyUser))
	mux.HandleFunc("POST /emby/Users/{Id}", wrap(s.updateEmbyUser))
	mux.HandleFunc("DELETE /emby/Users/{Id}", wrap(s.deleteEmbyUser))
	mux.HandleFunc("POST /emby/Users/{Id}/Delete", wrap(s.deleteEmbyUser))
	mux.HandleFunc("POST /emby/Users/{Id}/Password", wrap(s.updateEmbyUserPassword))
	mux.HandleFunc("POST /emby/Users/{Id}/Policy", wrap(s.updateEmbyUserPolicy))
}

func embyManagedActor(w http.ResponseWriter, r *http.Request) (identity.Principal, bool) {
	actor, ok := r.Context().Value(principalKey).(identity.Principal)
	if !ok || actor.IsApplicationKey() || !actor.CanManageServer() {
		apiError(w, r, http.StatusForbidden, "administrator_required", "An administrator user session is required.")
		return identity.Principal{}, false
	}
	return actor, true
}

func embyManagedUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	copy := r.Clone(r.Context())
	copy.SetPathValue("id", r.PathValue("Id"))
	return managedUserID(w, copy)
}

// Compatibility fields are case-insensitive, but aliases and duplicate keys
// are rejected before choosing a value. Native callers keep exact casing.
func userManagementObject(data []byte, fields []string, fold bool, path string) (map[string]json.RawMessage, map[string]string) {
	if path == "" {
		path = "Body"
	}
	fail := func(message string) (map[string]json.RawMessage, map[string]string) {
		return nil, map[string]string{path: message}
	}
	if !utf8.Valid(data) {
		return fail("Supply a UTF-8 JSON object.")
	}
	allowed := make(map[string]string, len(fields))
	for _, field := range fields {
		key := field
		if fold {
			key = strings.ToLower(key)
		}
		allowed[key] = field
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return fail("Supply a JSON object.")
	}
	values := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return fail("Supply a valid JSON object.")
		}
		if fold {
			key = strings.ToLower(key)
		}
		field, found := allowed[key]
		if !found {
			return fail("The object contains an unsupported field.")
		}
		if _, found := values[field]; found {
			return fail("Supply each field exactly once.")
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return fail("Supply a valid JSON object.")
		}
		values[field] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return fail("Supply a valid JSON object.")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return fail("Supply exactly one JSON object.")
	}
	return values, nil
}

func embyUserManagementBody(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, bool) {
	query, err := embyBusinessQuery(r)
	if err != nil || len(query) != 0 {
		managedUserInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return nil, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || (mediaType != "application/json" && mediaType != "text/plain") {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json or text/plain with a JSON body for this request.")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		managedUserInputError(w, r, map[string]string{"Body": "Supply a JSON object no larger than 1 MiB."})
		return nil, false
	}
	values, invalid := userManagementObject(data, fields, true, "")
	if invalid != nil {
		managedUserInputError(w, r, invalid)
		return nil, false
	}
	return values, true
}

func embyManagedBodyID(values map[string]json.RawMessage, id string, invalid map[string]string) {
	if raw, found := values["Id"]; found {
		var bodyID string
		managedUserValue(raw, "Id", &bodyID, invalid)
		if bodyID != "" && bodyID != id {
			invalid["Id"] = "The body user identifier must match the request path."
		}
	}
}

func (s *Server) createEmbyUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := embyManagedActor(w, r)
	if !ok {
		return
	}
	values, ok := embyUserManagementBody(w, r, []string{"Name", "CopyFromUserId", "UserCopyOptions"})
	if !ok {
		return
	}
	invalid := make(map[string]string)
	var name, copyFrom string
	var options []string
	managedUserValue(values["Name"], "Name", &name, invalid)
	if raw, found := values["CopyFromUserId"]; found {
		managedUserValue(raw, "CopyFromUserId", &copyFrom, invalid)
	}
	if raw, found := values["UserCopyOptions"]; found {
		managedUserValue(raw, "UserCopyOptions", &options, invalid)
	}
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return
	}
	user, err := s.identity.CreateManagedUserCopy(r.Context(), actor, name, copyFrom, options)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.userDTO(user))
}

func managedUpdateFrom(current identity.ManagedUser) identity.ManagedUserUpdate {
	return identity.ManagedUserUpdate{Revision: current.Revision, Name: current.User.Name,
		IsAdministrator: current.User.IsAdministrator, IsDisabled: current.User.IsDisabled, Policy: current.Policy}
}

func (s *Server) updateEmbyUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := embyManagedActor(w, r)
	if !ok {
		return
	}
	id, ok := embyManagedUserID(w, r)
	if !ok {
		return
	}
	values, ok := embyUserManagementBody(w, r, []string{
		"Name", "Id", "ServerId", "ServerName", "Prefix", "ConnectUserName", "DateCreated", "ConnectLinkType",
		"PrimaryImageTag", "HasPassword", "HasConfiguredPassword", "EnableAutoLogin", "LastLoginDate",
		"LastActivityDate", "Configuration", "Policy", "PrimaryImageAspectRatio", "UserItemShareLevel",
	})
	if !ok {
		return
	}
	invalid := make(map[string]string)
	embyManagedBodyID(values, id, invalid)
	var name string
	managedUserValue(values["Name"], "Name", &name, invalid)
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return
	}
	current, err := s.identity.GetManagedUser(r.Context(), id)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	// UserDto is a read projection. Account policy, passwords and preferences
	// have separate write operations and are not sourced from this envelope.
	input := managedUpdateFrom(current)
	input.Name = name
	result, err := s.identity.UpdateManagedUser(r.Context(), actor, id, input)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	s.finishEmbyUserMutation(w, r, actor, "update_user", result)
}

func (s *Server) deleteEmbyUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := embyManagedActor(w, r)
	if !ok {
		return
	}
	id, ok := embyManagedUserID(w, r)
	if !ok {
		return
	}
	query, err := embyBusinessQuery(r)
	if err != nil || len(query) != 0 {
		managedUserInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil || len(bytes.TrimSpace(data)) != 0 {
		managedUserInputError(w, r, map[string]string{"Body": "This operation does not accept a request body."})
		return
	}
	current, err := s.identity.GetManagedUser(r.Context(), id)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	result, err := s.identity.DeleteManagedUser(r.Context(), actor, id, current.Revision)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	if result.CollectionsChanged {
		s.catalogNotifier.Enqueue(library.CatalogNotification{Resync: true})
	}
	s.mediaDiagnostics.cancelActor(id, "")
	for _, sessionID := range result.RevokedSessionIDs {
		if s.eventHub != nil {
			s.eventHub.DisconnectCredential(sessionID)
		}
		s.cancelPlaybackCredential(sessionID)
	}
	s.log.Info("administrator user mutation", "actor_id", actor.User.ID, "user_id", id, "action", "delete_user")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) updateEmbyUserPassword(w http.ResponseWriter, r *http.Request) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	id, ok := embyManagedUserID(w, r)
	if !ok {
		return
	}
	if actor.IsApplicationKey() || (id != actor.User.ID && !actor.CanManageServer()) {
		apiError(w, r, http.StatusForbidden, "access_denied", "The requested user is not accessible.")
		return
	}
	values, ok := embyUserManagementBody(w, r, []string{"Id", "NewPw", "ResetPassword", "CurrentPw", "Pw"})
	if !ok {
		return
	}
	invalid := make(map[string]string)
	embyManagedBodyID(values, id, invalid)
	var password, currentPassword string
	var reset bool
	if raw, found := values["ResetPassword"]; found {
		managedUserValue(raw, "ResetPassword", &reset, invalid)
	}
	if raw, found := values["NewPw"]; found {
		managedUserValue(raw, "NewPw", &password, invalid)
	} else if !reset {
		invalid["NewPw"] = "Supply the new password."
	}
	if reset && password != "" {
		invalid["NewPw"] = "Do not combine a new password with ResetPassword."
	}
	for _, name := range []string{"CurrentPw", "Pw"} {
		if raw, found := values[name]; found {
			var supplied string
			managedUserValue(raw, name, &supplied, invalid)
			if _, found := values["CurrentPw"]; name == "Pw" && found {
				invalid["CurrentPw"] = "Supply the current password once."
			}
			currentPassword = supplied
		}
	}
	if !actor.CanManageServer() {
		if reset {
			apiError(w, r, http.StatusForbidden, "administrator_required", "An administrator user session is required to reset a password.")
			return
		}
		if _, current := values["CurrentPw"]; !current {
			if _, legacy := values["Pw"]; !legacy {
				invalid["CurrentPw"] = "Supply the current password."
			}
		}
	}
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return
	}
	var result identity.ManagedUserMutation
	var err error
	if actor.CanManageServer() {
		var current identity.ManagedUser
		current, err = s.identity.GetManagedUser(r.Context(), id)
		if err == nil {
			result, err = s.identity.ResetManagedUserPassword(r.Context(), actor, id, current.Revision, password)
		}
	} else {
		if !s.allowLogin(w, r) {
			return
		}
		result, err = s.identity.ChangeUserPassword(r.Context(), actor, id, currentPassword, password)
	}
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	s.finishEmbyUserMutation(w, r, actor, "reset_user_password", result)
}

// Deferred services expose inert capability values and reject attempts to
// enable them. Read-only counters and provider identifiers are never persisted.
var deferredEmbyPolicy = map[string]any{
	"EnableLiveTvManagement": false, "EnableLiveTvAccess": false, "EnableSyncTranscoding": false,
	"EnableMediaConversion": false, "EnableAllChannels": false, "EnabledChannels": []string{},
	"EnablePublicSharing": false, "AllowCameraUpload": false, "AllowSharingPersonalItems": false,
	"AuthenticationProviderId": "",
}

func readOnlyEmbyPolicy(user identity.User) map[string]any {
	result := map[string]any{"LockedOutDate": int64(0), "InvalidLoginAttemptCount": int64(0)}
	var values map[string]json.RawMessage
	if json.Unmarshal(user.Policy, &values) != nil {
		return result
	}
	for name := range result {
		if raw, found := values[name]; found {
			var value int64
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && name == "LockedOutDate" {
				result[name] = nil
			} else if json.Unmarshal(raw, &value) == nil && value >= 0 && (name == "LockedOutDate" || value <= 2147483647) {
				result[name] = value
			} else {
				delete(result, name)
			}
		}
	}
	return result
}

func (s *Server) updateEmbyUserPolicy(w http.ResponseWriter, r *http.Request) {
	actor, ok := embyManagedActor(w, r)
	if !ok {
		return
	}
	id, ok := embyManagedUserID(w, r)
	if !ok {
		return
	}
	fields := append([]string{}, managedPolicyFields...)
	fields = append(fields, "IsAdministrator", "IsDisabled", "LockedOutDate", "InvalidLoginAttemptCount")
	for name := range deferredEmbyPolicy {
		fields = append(fields, name)
	}
	values, ok := embyUserManagementBody(w, r, fields)
	if !ok {
		return
	}
	current, err := s.identity.GetManagedUser(r.Context(), id)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	input := managedUpdateFrom(current)
	invalid := make(map[string]string)
	readOnly := readOnlyEmbyPolicy(current.User)
	for _, name := range []string{"LockedOutDate", "InvalidLoginAttemptCount"} {
		if raw, found := values[name]; found {
			var actual *int64
			expected, known := readOnly[name]
			if json.Unmarshal(raw, &actual) != nil || !known {
				invalid[name] = "This policy state is read-only."
			} else {
				canonical, _ := json.Marshal(actual)
				stored, _ := json.Marshal(expected)
				if !bytes.Equal(canonical, stored) {
					invalid[name] = "This policy state is read-only."
				}
			}
			delete(values, name)
		}
	}
	for _, flag := range []struct {
		name string
		dst  *bool
	}{{"IsAdministrator", &input.IsAdministrator}, {"IsDisabled", &input.IsDisabled}} {
		if raw, found := values[flag.name]; found {
			managedUserValue(raw, flag.name, flag.dst, invalid)
			delete(values, flag.name)
		}
	}
	for name, inert := range deferredEmbyPolicy {
		if raw, found := values[name]; found {
			var actual any
			if json.Unmarshal(raw, &actual) != nil {
				invalid[name] = "Supply the inert value for this unsupported capability."
			} else {
				canonical, _ := json.Marshal(actual)
				expected, _ := json.Marshal(inert)
				if !bytes.Equal(canonical, expected) {
					invalid[name] = "This capability is not supported."
				}
			}
			delete(values, name)
		}
	}
	if len(invalid) != 0 {
		managedUserInputError(w, r, invalid)
		return
	}
	input.Policy, err = mergeManagedPolicy(current.Policy, values)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	result, err := s.identity.UpdateManagedUser(r.Context(), actor, id, input)
	if err != nil {
		s.managedUserError(w, r, err)
		return
	}
	s.finishEmbyUserMutation(w, r, actor, "update_user", result)
}

func (s *Server) finishEmbyUserMutation(w http.ResponseWriter, r *http.Request, actor identity.Principal, action string, result identity.ManagedUserMutation) {
	s.retireManagedUserSessions(result.RevokedSessionIDs)
	if action == "reset_user_password" || !result.User.User.IsAdministrator || result.User.User.IsDisabled {
		s.mediaDiagnostics.cancelActor(result.User.User.ID, "")
	}
	if result.CurrentSessionRevoked {
		if s.eventHub != nil {
			s.eventHub.DisconnectCredential(actor.SessionID)
		}
		s.cancelPlaybackCredential(actor.SessionID)
	}
	s.log.Info("user mutation", "actor_id", actor.User.ID, "user_id", result.User.User.ID, "action", action)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) retireManagedUserSessions(sessionIDs []string) {
	for _, sessionID := range sessionIDs {
		if s.eventHub != nil {
			s.eventHub.DisconnectCredential(sessionID)
		}
		s.cancelPlaybackCredential(sessionID)
	}
}

func publicPolicyVisible(user identity.User, remote bool, deviceID string, usedDevice bool) bool {
	return identity.PublicAvatarVisible(user, remote, deviceID, usedDevice)
}

func (s *Server) publicUserVisible(r *http.Request, user identity.User, remote bool, deviceID string) (bool, error) {
	policy, err := identity.ParseRuntimePolicy(user.Policy)
	if err != nil {
		return false, nil
	}
	used := false
	if policy.IsHiddenFromUnusedDevices && deviceID != "" {
		used, err = s.identity.UserHasUsedDevice(r.Context(), user.ID, deviceID)
		if err != nil {
			return false, err
		}
	}
	return publicPolicyVisible(user, remote, deviceID, used), nil
}
