package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerClientSessionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /emby/Sessions", s.requireEmby(s.clientSessions))
	mux.HandleFunc("POST /emby/Sessions/Capabilities", s.requireEmby(s.clientCapabilities(false)))
	mux.HandleFunc("POST /emby/Sessions/Capabilities/Full", s.requireEmby(s.clientCapabilities(true)))
	mux.HandleFunc("GET /emby/Shows/NextUp", s.requireEmby(s.nextUpItems))
}

func clientSessionTarget(values map[string]string) (string, error) {
	id, alternate := values["id"], values["sessionid"]
	if id != "" && alternate != "" && id != alternate {
		return "", identity.ErrInvalidInput
	}
	if id == "" {
		id = alternate
	}
	return id, nil
}

func (s *Server) clientCapabilities(full bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		values, err := streamValues(r)
		if err != nil {
			s.clientSessionError(w, r, identity.ErrInvalidInput)
			return
		}
		var encoded json.RawMessage
		if full {
			if !decodeBody(w, r, &encoded) {
				return
			}
		} else {
			fields := map[string]any{}
			for _, field := range []struct{ key, name string }{
				{"playablemediatypes", "PlayableMediaTypes"}, {"supportedcommands", "SupportedCommands"},
			} {
				if raw, supplied := values[field.key]; supplied {
					entries := []string{}
					for _, entry := range strings.Split(raw, ",") {
						if entry = strings.TrimSpace(entry); entry != "" {
							entries = append(entries, entry)
						}
					}
					fields[field.name] = entries
				}
			}
			for _, field := range []struct{ key, name string }{
				{"supportsmediacontrol", "SupportsMediaControl"}, {"supportssync", "SupportsSync"},
			} {
				if raw, supplied := values[field.key]; supplied {
					value, err := strconv.ParseBool(raw)
					if err != nil {
						s.clientSessionError(w, r, identity.ErrInvalidInput)
						return
					}
					fields[field.name] = value
				}
			}
			encoded, _ = json.Marshal(fields)
		}
		capabilities, err := identity.ParseClientCapabilities(encoded)
		if err != nil {
			s.clientSessionError(w, r, err)
			return
		}
		principal := r.Context().Value(principalKey).(identity.Principal)
		// The reference binds capability reports to the current authenticated
		// client, even when a stale or unknown Id hint is supplied. A hint must
		// never grant permission to overwrite another authentication session.
		if err := s.identity.UpdateClientCapabilities(r.Context(), principal, principal.SessionID, capabilities); err != nil {
			s.clientSessionError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func idlePlayerStateDTO() map[string]any {
	return map[string]any{"CanSeek": false, "IsPaused": false, "IsMuted": false,
		"RepeatMode": "RepeatNone", "SleepTimerMode": "None", "SubtitleOffset": 0,
		"Shuffle": false, "PlaybackRate": 1}
}

func (s *Server) clientSessionDTO(session identity.ClientSession) map[string]any {
	result := map[string]any{
		"Id": session.SessionID, "UserId": session.UserID, "UserName": session.UserName,
		"Client": session.Client.Name, "DeviceId": session.Client.DeviceID,
		"DeviceName": session.Client.Device, "ApplicationVersion": session.Client.Version,
		"ServerId": s.serverID, "LastActivityDate": session.LastSeenAt.UTC(),
		"PlayableMediaTypes":    append([]string{}, session.Capabilities.PlayableMediaTypes...),
		"SupportedCommands":     append([]string{}, session.Capabilities.SupportedCommands...),
		"SupportsRemoteControl": false, "AdditionalUsers": []any{},
		"PlayState": idlePlayerStateDTO(), "PlaylistIndex": 0, "PlaylistLength": 0,
	}
	// An arbitrary client IconUrl is stored as a declaration but is not exposed
	// as an automatically loaded remote image in other users' session views.
	return result
}

func (s *Server) clientSessions(w http.ResponseWriter, r *http.Request) {
	values, err := streamValues(r)
	if err != nil {
		s.clientSessionError(w, r, identity.ErrInvalidInput)
		return
	}
	target, err := clientSessionTarget(values)
	if err != nil {
		s.clientSessionError(w, r, err)
		return
	}
	filter := identity.ClientSessionFilter{SessionID: target, DeviceID: values["deviceid"]}
	if raw, supplied := values["activewithinseconds"]; supplied {
		seconds, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			s.clientSessionError(w, r, identity.ErrInvalidInput)
			return
		}
		value := int(seconds)
		filter.ActiveWithinSeconds = &value
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	sessions, err := s.identity.ListClientSessions(r.Context(), principal, filter)
	if err != nil {
		s.clientSessionError(w, r, err)
		return
	}
	if userID := values["controllablebyuserid"]; userID != "" {
		if userID != principal.User.ID && !principal.User.IsAdministrator {
			s.clientSessionError(w, r, identity.ErrClientSessionForbidden)
			return
		}
		// No remote command transport has been implemented in this increment.
		jsonResponse(w, http.StatusOK, []any{})
		return
	}
	authIDs := make([]string, 0, len(sessions))
	for _, session := range sessions {
		authIDs = append(authIDs, session.SessionID)
	}
	playing, err := s.library.ListNowPlayingSessionsForAuth(r.Context(), principal.User.ID, principal.User.IsAdministrator, authIDs)
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	bySession := map[string]library.PlaySession{}
	ids, seen := []string{}, map[string]bool{}
	for _, play := range playing {
		if play.State != "Playing" && play.State != "Paused" {
			continue
		}
		if _, exists := bySession[play.AuthSessionID]; exists {
			continue
		}
		bySession[play.AuthSessionID] = play
		if !seen[play.ItemID] {
			ids, seen[play.ItemID] = append(ids, play.ItemID), true
		}
	}
	items, itemDTOs := map[string]map[string]any{}, []map[string]any{}
	if len(ids) > 0 {
		result, err := s.library.QueryItems(r.Context(), library.Query{UserID: principal.User.ID, Ids: ids, Recursive: true, Limit: len(ids)})
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		for _, item := range result.Items {
			item.UserData = nil
			dto := s.itemDTO(item, nil, false)
			items[item.ID], itemDTOs = dto, append(itemDTOs, dto)
		}
		if !s.applyIndexedImages(w, r, principal.User.ID, itemDTOs, false) {
			return
		}
	}
	result := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		dto := s.clientSessionDTO(session)
		if play, exists := bySession[session.SessionID]; exists && items[play.ItemID] != nil {
			dto["NowPlayingItem"] = items[play.ItemID]
			state := idlePlayerStateDTO()
			// Only validated, persisted client hints are projected. Position and
			// pause/source identity remain authoritative state-machine fields.
			encoded, _ := json.Marshal(play.PlayerState)
			_ = json.Unmarshal(encoded, &state)
			state["PositionTicks"], state["IsPaused"] = play.PositionTicks, play.State == "Paused"
			state["MediaSourceId"] = play.MediaSourceID
			dto["PlayState"] = state
		}
		result = append(result, dto)
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) clientSessionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_session_request", "Check the session identifiers, capability fields, and limits.")
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "session_access_denied", "The requested session is not controlled by this account.")
	default:
		s.identityError(w, r, err)
	}
}
