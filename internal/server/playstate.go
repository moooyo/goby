package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func userDataDTO(data library.UserData, includeItemID bool) map[string]any {
	result := map[string]any{"PlaybackPositionTicks": data.PlaybackPositionTicks, "PlayCount": data.PlayCount,
		"IsFavorite": data.IsFavorite, "Played": data.Played}
	if includeItemID {
		result["ItemId"] = data.ItemID
	}
	if data.LastPlayedDate != nil {
		result["LastPlayedDate"] = data.LastPlayedDate.UTC()
	}
	if data.UnplayedItemCount != nil {
		result["UnplayedItemCount"] = *data.UnplayedItemCount
	}
	return result
}

func (s *Server) playbackReport(event string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			library.PlayerStateUpdate
			PlaySessionID string `json:"PlaySessionId"`
			ItemID        string `json:"ItemId"`
			MediaSourceID string `json:"MediaSourceId"`
			SessionID     string `json:"SessionId"`
			PositionTicks *int64
			IsPaused      bool
		}
		if !decodeBody(w, r, &body) {
			return
		}
		principal := r.Context().Value(principalKey).(identity.Principal)
		principal, err := s.bindKeyPlaybackContext(r, principal, body.PlaySessionID)
		if err != nil {
			s.identityError(w, r, err)
			return
		}
		if body.SessionID != "" && body.SessionID != clientSessionID(principal) {
			apiError(w, r, http.StatusForbidden, "session_mismatch", "Playback reports must belong to the authenticated session.")
			return
		}
		play, data, err := s.library.ReportPlayback(r.Context(), playbackOwner(principal), library.PlaybackReport{
			PlaySessionID: body.PlaySessionID, ItemID: body.ItemID, MediaSourceID: body.MediaSourceID,
			Event: event, PositionTicks: body.PositionTicks, IsPaused: body.IsPaused, PlayerState: &body.PlayerStateUpdate})
		if err != nil {
			s.playbackError(w, r, err)
			return
		}
		if principal.User.ID != "" {
			s.notifier.Enqueue(principal.User.ID, data.ItemID, false)
		}
		if event == "Stopped" {
			s.hls.cancelMatching(principal.SessionID, play.ID)
		} else {
			s.hls.touchMatching(principal.SessionID, play.ID)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) playbackPing(w http.ResponseWriter, r *http.Request) {
	values, err := streamValues(r)
	if err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_playback_request", "The playback ping query is invalid.")
		return
	}
	if values["playsessionid"] == "" {
		embyTextError(w, r, http.StatusBadRequest, "Value cannot be null. (Parameter 'key')")
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	principal, err = s.bindKeyPlaybackContext(r, principal, values["playsessionid"])
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	play, _, err := s.library.ReportPlayback(r.Context(), playbackOwner(principal), library.PlaybackReport{Event: "Ping", PlaySessionID: values["playsessionid"], ItemID: values["itemid"], MediaSourceID: values["mediasourceid"]})
	if err != nil && !errors.Is(err, library.ErrNotFound) {
		s.playbackError(w, r, err)
		return
	}
	if err == nil {
		s.hls.touchMatching(principal.SessionID, play.ID)
	}
	// Unknown or foreign play-session keys are inert, matching the captured
	// reference while disclosing no ownership information or changing state.
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setUserFlag(favorite, value bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, ok := s.itemUser(w, r)
		if !ok {
			return
		}
		var data library.UserData
		var err error
		if favorite {
			data, err = s.library.SetFavoriteFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"), value)
		} else {
			var datePlayed *time.Time
			if raw := r.URL.Query().Get("DatePlayed"); raw != "" {
				parsed, parseErr := time.Parse(time.RFC3339Nano, raw)
				if parseErr != nil {
					apiError(w, r, http.StatusBadRequest, "invalid_input", "DatePlayed must be an RFC3339 timestamp.")
					return
				}
				datePlayed = &parsed
			}
			data, err = s.library.SetPlayedFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"), value, datePlayed)
		}
		if err != nil {
			s.playbackError(w, r, err)
			return
		}
		s.notifier.Enqueue(userID, data.ItemID, !favorite)
		jsonResponse(w, http.StatusOK, userDataDTO(data, false))
	}
}

func (s *Server) resumeItems(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	query, ok := readItemQuery(w, r, userID)
	if !ok {
		return
	}
	attachApplicationCredentialID(r, &query)
	zeroLimit := query.Limit == 0
	if zeroLimit {
		query.Limit = 1
	}
	result, err := s.library.QueryResume(r.Context(), query)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	if !zeroLimit {
		for _, item := range result.Items {
			dto := s.itemDTOForRequest(r, item, queryValues(r.URL.Query()["Fields"]), false)
			applyItemSwitches(dto, r)
			items = append(items, dto)
		}
	}
	if !s.applyIndexedImages(w, r, userID, items, false) {
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"Items": items, "TotalRecordCount": result.TotalRecordCount})
}
