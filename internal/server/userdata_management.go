package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerExtendedUserStateRoutes(mux *http.ServeMux) {
	base := "/emby/Users/{UserId}/Items/{Id}"
	mux.HandleFunc("GET "+base+"/UserData", s.requireEmby(s.getUserData))
	mux.HandleFunc("POST "+base+"/UserData", s.requireEmby(s.updateUserData))
	mux.HandleFunc("POST "+base+"/HideFromResume", s.requireEmby(s.hideUserResume))
	mux.HandleFunc("POST "+base+"/Rating", s.requireEmby(s.updateUserRating))
	mux.HandleFunc("DELETE "+base+"/Rating", s.requireEmby(s.deleteUserRating))
	mux.HandleFunc("POST "+base+"/Rating/Delete", s.requireEmby(s.deleteUserRating))
	playing := "/emby/Users/{UserId}/PlayingItems/{Id}"
	mux.HandleFunc("POST "+playing, s.requireEmby(s.legacyPlaybackReport("Started")))
	mux.HandleFunc("POST "+playing+"/Progress", s.requireEmby(s.legacyPlaybackReport("Progress")))
	mux.HandleFunc("DELETE "+playing, s.requireEmby(s.legacyPlaybackReport("Stopped")))
	mux.HandleFunc("POST "+playing+"/Delete", s.requireEmby(s.legacyPlaybackReport("Stopped")))
}

func (s *Server) getUserData(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	if _, ok := userStateQuery(w, r, "userid"); !ok {
		return
	}
	var data library.UserData
	var err error
	if id, entity := positiveEntityID(r.PathValue("Id")); entity {
		data, err = s.library.GetEntityUserDataFor(r.Context(), requestLibrarySubject(r, userID), id)
	} else {
		data, err = s.library.GetUserDataFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"))
	}
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, userDataDTO(data, true))
}

func userStateQuery(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]string, bool) {
	raw, err := embyBusinessQuery(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "The user state query is invalid.")
		return nil, false
	}
	values := make(map[string]string)
	for name, entries := range raw {
		key := strings.ToLower(name)
		valid := false
		for _, candidate := range allowed {
			valid = valid || key == candidate
		}
		if _, duplicate := values[key]; !valid || duplicate || len(entries) != 1 {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply each supported user state parameter once.")
			return nil, false
		}
		values[key] = entries[0]
	}
	if target := r.PathValue("UserId"); target != "" && values["userid"] != "" && values["userid"] != target {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "User identifiers must agree.")
		return nil, false
	}
	return values, true
}

func userStateEmptyBody(w http.ResponseWriter, r *http.Request) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil || len(bytes.TrimSpace(data)) != 0 {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "This user state operation does not accept a body.")
		return false
	}
	return true
}

func decodeUserDataPatch(w http.ResponseWriter, r *http.Request, itemID, serverID string) (library.UserDataPatch, bool) {
	values, ok := preferenceBody(w, r, []string{"PlaybackPositionTicks", "PlayCount", "IsFavorite", "Played", "LastPlayedDate", "Rating", "Likes", "HideFromResume", "ItemId", "Key", "ServerId", "PlayedPercentage", "UnplayedItemCount"})
	if !ok {
		return library.UserDataPatch{}, false
	}
	patch := library.UserDataPatch{}
	invalid := false
	for name, raw := range values {
		null := bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
		switch name {
		case "PlaybackPositionTicks":
			var value int64
			if null || json.Unmarshal(raw, &value) != nil {
				invalid = true
			} else {
				patch.PlaybackPositionTicks = &value
			}
		case "PlayCount":
			var value int
			if null || json.Unmarshal(raw, &value) != nil {
				invalid = true
			} else {
				patch.PlayCount = &value
			}
		case "IsFavorite", "Played", "HideFromResume", "Likes":
			if name == "Likes" && null {
				patch.ClearLikes = true
				continue
			}
			var value bool
			if null || json.Unmarshal(raw, &value) != nil {
				invalid = true
				continue
			}
			switch name {
			case "IsFavorite":
				patch.IsFavorite = &value
			case "Played":
				patch.Played = &value
			case "HideFromResume":
				patch.HideFromResume = &value
			case "Likes":
				patch.Likes = &value
			}
		case "Rating":
			if null {
				patch.ClearRating = true
				continue
			}
			var value float64
			if json.Unmarshal(raw, &value) != nil {
				invalid = true
			} else {
				patch.Rating = &value
			}
		case "LastPlayedDate":
			if null {
				patch.ClearLastPlayedDate = true
				continue
			}
			var value time.Time
			if json.Unmarshal(raw, &value) != nil {
				invalid = true
			} else {
				patch.LastPlayedDate = &value
			}
		case "ItemId", "ServerId", "Key":
			var value string
			if null || json.Unmarshal(raw, &value) != nil || len(value) > 256 ||
				name == "ItemId" && value != "" && value != itemID || name == "ServerId" && value != "" && value != serverID {
				invalid = true
			}
		case "PlayedPercentage", "UnplayedItemCount":
			// These fields are derived read projections, not counters that clients
			// can use to replace aggregate or authoritative media-duration facts.
			invalid = true
		}
	}
	if invalid || patch.Validate() != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check writable user data fields, ranges and matching identifiers.")
		return library.UserDataPatch{}, false
	}
	return patch, true
}

func (s *Server) applyUserDataPatch(w http.ResponseWriter, r *http.Request, userID string, patch library.UserDataPatch) {
	var data library.UserData
	var err error
	if id, entity := positiveEntityID(r.PathValue("Id")); entity {
		data, err = s.library.UpdateEntityUserDataFor(r.Context(), requestLibrarySubject(r, userID), id, patch)
	} else {
		data, err = s.library.UpdateUserDataFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"), patch)
	}
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	s.notifier.Enqueue(userID, data.ItemID, patch.Played != nil)
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, userDataDTO(data, true))
}

func (s *Server) updateUserData(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	if _, ok := userStateQuery(w, r, "userid"); !ok {
		return
	}
	patch, ok := decodeUserDataPatch(w, r, r.PathValue("Id"), s.serverID)
	if ok {
		s.applyUserDataPatch(w, r, userID, patch)
	}
}

func (s *Server) hideUserResume(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	values, ok := userStateQuery(w, r, "userid", "hide")
	if !ok || !userStateEmptyBody(w, r) {
		return
	}
	hide, err := strconv.ParseBool(values["hide"])
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Hide must be an explicit boolean.")
		return
	}
	s.applyUserDataPatch(w, r, userID, library.UserDataPatch{HideFromResume: &hide})
}

func (s *Server) updateUserRating(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	values, ok := userStateQuery(w, r, "userid", "likes")
	if !ok || !userStateEmptyBody(w, r) {
		return
	}
	likes, err := strconv.ParseBool(values["likes"])
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Likes must be an explicit boolean.")
		return
	}
	rating := float64(0)
	if likes {
		rating = 10
	}
	s.applyUserDataPatch(w, r, userID, library.UserDataPatch{Likes: &likes, Rating: &rating})
}

func (s *Server) deleteUserRating(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	if _, ok := userStateQuery(w, r, "userid"); !ok || !userStateEmptyBody(w, r) {
		return
	}
	s.applyUserDataPatch(w, r, userID, library.UserDataPatch{ClearRating: true, ClearLikes: true})
}

// Legacy report routes translate their explicit query/body values once, then
// use the current playback handler, including its ownership, idempotency,
// heartbeat, notifications and resource-stop behavior. No second state machine
// or cross-user administrator playback identity is introduced.
type legacyPlaybackResponse struct{ http.ResponseWriter }

func (writer legacyPlaybackResponse) WriteHeader(status int) {
	if status == http.StatusNoContent {
		status = http.StatusOK
	}
	writer.ResponseWriter.WriteHeader(status)
}

func (s *Server) legacyPlaybackReport(event string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := r.Context().Value(principalKey).(identity.Principal)
		if actor.IsApplicationKey() || r.PathValue("UserId") != actor.User.ID {
			apiError(w, r, http.StatusForbidden, "access_denied", "Legacy playback reports belong to the authenticated user.")
			return
		}
		values, ok := userStateQuery(w, r, "userid", "mediasourceid", "positionticks", "ispaused", "ismuted", "canseek", "audiostreamindex", "subtitlestreamindex", "volumelevel", "playmethod", "livestreamid", "playsessionid", "nextmediatype", "repeatmode", "subtitleoffset", "playbackrate")
		if !ok {
			return
		}
		if userID := values["userid"]; userID != "" && userID != actor.User.ID {
			apiError(w, r, http.StatusForbidden, "access_denied", "Playback user identifiers must agree.")
			return
		}
		body := make(map[string]json.RawMessage)
		if r.Body != nil {
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
			if err != nil {
				apiError(w, r, http.StatusBadRequest, "invalid_input", "The playback report exceeds its limit.")
				return
			}
			if len(bytes.TrimSpace(raw)) != 0 {
				var invalid map[string]string
				body, invalid = userManagementObject(raw, []string{"ItemId", "MediaSourceId", "SessionId", "PlaySessionId", "PositionTicks", "IsPaused", "IsMuted", "CanSeek", "AudioStreamIndex", "SubtitleStreamIndex", "VolumeLevel", "PlayMethod", "RepeatMode", "SubtitleOffset", "PlaybackRate", "Shuffle"}, true, "Body")
				if len(invalid) != 0 {
					managedUserInputError(w, r, invalid)
					return
				}
			}
		}
		if raw := body["ItemId"]; raw != nil {
			var id string
			if json.Unmarshal(raw, &id) != nil || id != r.PathValue("Id") {
				apiError(w, r, http.StatusBadRequest, "invalid_input", "Playback item identifiers must agree.")
				return
			}
		}
		body["ItemId"], _ = json.Marshal(r.PathValue("Id"))
		for _, field := range []struct{ query, name, kind string }{
			{"mediasourceid", "MediaSourceId", "string"}, {"playsessionid", "PlaySessionId", "string"},
			{"positionticks", "PositionTicks", "integer"}, {"audiostreamindex", "AudioStreamIndex", "integer"}, {"subtitlestreamindex", "SubtitleStreamIndex", "integer"},
			{"volumelevel", "VolumeLevel", "integer"}, {"subtitleoffset", "SubtitleOffset", "integer"}, {"playbackrate", "PlaybackRate", "number"},
			{"ispaused", "IsPaused", "boolean"}, {"ismuted", "IsMuted", "boolean"}, {"canseek", "CanSeek", "boolean"},
			{"playmethod", "PlayMethod", "string"}, {"repeatmode", "RepeatMode", "string"},
		} {
			value, present := values[field.query]
			if !present {
				continue
			}
			var encoded []byte
			var err error
			switch field.kind {
			case "string":
				encoded, err = json.Marshal(value)
			case "integer":
				var number int64
				number, err = strconv.ParseInt(value, 10, 64)
				if err == nil {
					encoded, err = json.Marshal(number)
				}
			case "number":
				var number float64
				number, err = strconv.ParseFloat(value, 64)
				if err == nil {
					encoded, err = json.Marshal(number)
				}
			case "boolean":
				var flag bool
				flag, err = strconv.ParseBool(value)
				if err == nil {
					encoded, err = json.Marshal(flag)
				}
			}
			if err != nil {
				apiError(w, r, http.StatusBadRequest, "invalid_input", "A legacy playback parameter is invalid.")
				return
			}
			if old, exists := body[field.name]; exists {
				var oldValue, newValue any
				decoder := json.NewDecoder(bytes.NewReader(old))
				decoder.UseNumber()
				if decoder.Decode(&oldValue) != nil {
					apiError(w, r, http.StatusBadRequest, "invalid_input", "A legacy playback body value is invalid.")
					return
				}
				decoder = json.NewDecoder(bytes.NewReader(encoded))
				decoder.UseNumber()
				_ = decoder.Decode(&newValue)
				left, _ := json.Marshal(oldValue)
				right, _ := json.Marshal(newValue)
				if !bytes.Equal(left, right) {
					apiError(w, r, http.StatusBadRequest, "invalid_input", "Playback query and body values must agree.")
					return
				}
			}
			body[field.name] = encoded
		}
		encoded, err := json.Marshal(body)
		if err != nil {
			apiError(w, r, http.StatusBadRequest, "invalid_input", "The playback report is invalid.")
			return
		}
		copy := r.Clone(r.Context())
		copy.Body = io.NopCloser(bytes.NewReader(encoded))
		copy.ContentLength = int64(len(encoded))
		copy.Header = r.Header.Clone()
		copy.Header.Set("Content-Type", "application/json")
		s.playbackReport(event)(legacyPlaybackResponse{w}, copy)
	}
}
