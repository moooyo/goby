package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func (s *Server) registerSubtitleTimelineRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/subtitle-timelines", s.requireAdmin(s.adminSubtitleTimelineItems))
	mux.HandleFunc("GET /admin/v1/items/{id}/subtitle-timelines", s.requireAdmin(s.adminSubtitleTimelineItem))
	mux.HandleFunc("GET /emby/Items/{Id}/SubtitleTimelines", s.requireEmby(s.embySubtitleTimelines))
	mux.HandleFunc("GET /emby/Items/{Id}/SubtitleTimelines/{StreamIndex}", s.requireEmby(s.embySubtitleTimelineTrack))
}

func (s *Server) subtitleTimelineError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrSubtitleTimelineConflict) || errors.Is(err, library.ErrSubtitleTimelineStorageConflict) {
		apiError(w, r, http.StatusConflict, "subtitle_timeline_conflict", "The subtitle timeline state changed or its existing files need explicit review.")
		return
	}
	if errors.Is(err, library.ErrSubtitleTimelineStale) {
		apiError(w, r, http.StatusNotFound, "subtitle_timeline_stale", "The preserved subtitle timeline belongs to an earlier source. Explicit regeneration is required.")
		return
	}
	s.libraryError(w, r, err)
}

func (s *Server) adminSubtitleTimelineItems(w http.ResponseWriter, r *http.Request) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	start, limit := 0, 50
	valid := err == nil && (!r.URL.ForceQuery || r.URL.RawQuery != "")
	for name, values := range query {
		if len(values) != 1 || values[0] == "" {
			valid = false
			break
		}
		switch name {
		case "LibraryId", "SearchTerm", "State":
		case "StartIndex", "Limit":
			value, err := strconv.Atoi(values[0])
			if err != nil || value < 0 || strconv.Itoa(value) != values[0] {
				valid = false
				break
			}
			if name == "Limit" {
				limit = value
			} else {
				start = value
			}
		default:
			valid = false
		}
	}
	if !valid || limit < 1 || limit > 200 || start > 1000000 {
		adminTaskInputError(w, r, map[string]string{"Query": "Supply supported subtitle timeline filters and pagination."})
		return
	}
	page, err := s.library.GetSubtitleTimelineItems(r.Context(), adminMediaAnalysisActor(r), query.Get("LibraryId"), query.Get("SearchTerm"), query.Get("State"), start, limit)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, page)
}

func (s *Server) adminSubtitleTimelineItem(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMediaAnalysisNoQuery(w, r) {
		return
	}
	actor := adminMediaAnalysisActor(r)
	detail, err := s.library.GetSubtitleTimelineItem(r.Context(), actor, id)
	if err != nil {
		s.mediaAnalysisError(w, r, err)
		return
	}
	artifact, err := s.library.GetSubtitleTimelineFor(r.Context(), library.Subject{UserID: actor.User.ID}, id)
	if err != nil && !errors.Is(err, library.ErrNotFound) {
		s.subtitleTimelineError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, struct {
		library.SubtitleTimelineDetail
		Artifact library.SubtitleTimelineArtifact `json:"Artifact"`
	}{detail, artifact})
}

func (s *Server) startAdminSubtitleTimelineRun(w http.ResponseWriter, r *http.Request, input adminMediaAnalysisRunInput) {
	if s.taskStore == nil || s.taskManager == nil || !(subtitleTimelineTaskExecutor{s}).Available() {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.GetByKey(r.Context(), library.TaskSubtitleTimelineGenerationKey)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	if !definition.Enabled {
		s.taskError(w, r, tasks.ErrDisabled)
		return
	}
	queued, err := s.library.QueueSubtitleTimelines(r.Context(), adminMediaAnalysisActor(r), input.Selection, input.RequestID)
	if err != nil {
		s.subtitleTimelineError(w, r, err)
		return
	}
	result, err := s.taskManager.Start(r.Context(), adminTaskActor(r), tasks.StartRequest{TaskID: definition.ID, RequestID: input.RequestID})
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"RunId": result.Run.ID, "TaskId": result.Run.TaskID, "Admitted": result.Admitted, "Queued": queued.Queued})
}

func subtitleTimelineQuery(w http.ResponseWriter, r *http.Request, track bool) (url.Values, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	valid := err == nil && (!r.URL.ForceQuery || r.URL.RawQuery != "")
	for name, values := range query {
		if len(values) != 1 || values[0] == "" {
			valid = false
			break
		}
		if name == "UserId" || name == "api_key" || track && name == "tag" {
			continue
		}
		valid = false
	}
	if track && (query.Get("tag") == "" || len(query.Get("tag")) > 256) {
		valid = false
	}
	if !valid {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply documented subtitle timeline query parameters once.")
	}
	return query, valid
}

type subtitleTimelineStreamDTO struct {
	StreamIndex   int    `json:"StreamIndex"`
	Codec         string `json:"Codec"`
	IntervalCount int    `json:"IntervalCount"`
	URL           string `json:"Url"`
}

type subtitleTimelineDescriptor struct {
	Available     bool                        `json:"Available"`
	Stale         bool                        `json:"Stale,omitempty"`
	MediaSourceID string                      `json:"MediaSourceId,omitempty"`
	SourceVersion string                      `json:"SourceVersion,omitempty"`
	DurationTicks int64                       `json:"DurationTicks,omitempty"`
	Streams       []subtitleTimelineStreamDTO `json:"Streams,omitempty"`
}

type subtitleTimelineTrackDTO struct {
	MediaSourceID string                           `json:"MediaSourceId"`
	SourceVersion string                           `json:"SourceVersion"`
	StreamIndex   int                              `json:"StreamIndex"`
	DurationTicks int64                            `json:"DurationTicks"`
	Intervals     []media.SubtitleTimelineInterval `json:"Intervals"`
}

func unavailableSubtitleTimeline(err error) bool {
	return errors.Is(err, library.ErrNotFound) || errors.Is(err, library.ErrUnavailable) ||
		errors.Is(err, library.ErrSubtitleTimelineStale) || errors.Is(err, library.ErrSubtitleTimelineStorageConflict)
}

func (s *Server) embySubtitleTimelines(w http.ResponseWriter, r *http.Request) {
	if _, ok := subtitleTimelineQuery(w, r, false); !ok {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	subject, id := requestLibrarySubject(r, userID), r.PathValue("Id")
	item, err := s.library.GetItemFor(r.Context(), subject, id)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	result := subtitleTimelineDescriptor{}
	if item.IsFolder || item.Media == nil || item.Type != "Movie" && item.Type != "Episode" {
		jsonResponse(w, http.StatusOK, result)
		return
	}
	artifact, err := s.library.GetSubtitleTimelineFor(r.Context(), subject, id)
	if err != nil && !unavailableSubtitleTimeline(err) {
		s.subtitleTimelineError(w, r, err)
		return
	}
	if _, accessErr := s.library.GetItemFor(r.Context(), subject, id); accessErr != nil {
		s.libraryError(w, r, accessErr)
		return
	}
	if err == nil && artifact.Available && !artifact.Stale {
		result.Available, result.MediaSourceID, result.SourceVersion, result.DurationTicks = true, media.SourceID(id), artifact.SourceStamp, artifact.DurationTicks
		for _, track := range artifact.Tracks {
			query := url.Values{"tag": {artifact.ETag}}
			if r.URL.Query().Has("UserId") {
				query.Set("UserId", userID)
			}
			result.Streams = append(result.Streams, subtitleTimelineStreamDTO{track.StreamIndex, track.Codec, track.IntervalCount,
				"/emby/Items/" + url.PathEscape(id) + "/SubtitleTimelines/" + strconv.Itoa(track.StreamIndex) + "?" + query.Encode()})
		}
	} else {
		result.Stale = artifact.Stale || errors.Is(err, library.ErrSubtitleTimelineStale)
	}
	jsonResponse(w, http.StatusOK, result)
}

func (s *Server) embySubtitleTimelineTrack(w http.ResponseWriter, r *http.Request) {
	query, ok := subtitleTimelineQuery(w, r, true)
	if !ok {
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	index, err := strconv.Atoi(r.PathValue("StreamIndex"))
	if err != nil || index < 0 || index > 2147483647 || strconv.Itoa(index) != r.PathValue("StreamIndex") {
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply a canonical subtitle stream index.")
		return
	}
	subject, id := requestLibrarySubject(r, userID), r.PathValue("Id")
	file, artifact, err := s.library.OpenSubtitleTimelineFor(r.Context(), subject, id)
	if err != nil {
		if unavailableSubtitleTimeline(err) {
			apiError(w, r, http.StatusNotFound, "subtitle_timeline_unavailable", "No current subtitle timeline is available.")
		} else {
			s.subtitleTimelineError(w, r, err)
		}
		return
	}
	defer file.Close()
	if artifact.ETag != query.Get("tag") {
		apiError(w, r, http.StatusNotFound, "subtitle_timeline_changed", "Reload the current subtitle timeline descriptor.")
		return
	}
	encoded, err := io.ReadAll(io.LimitReader(file, media.MaxSubtitleTimelineBytes+1))
	if err != nil || int64(len(encoded)) > media.MaxSubtitleTimelineBytes {
		apiError(w, r, http.StatusNotFound, "subtitle_timeline_unavailable", "No current subtitle timeline is available.")
		return
	}
	data, err := media.ParseSubtitleTimelines(encoded)
	if err != nil {
		apiError(w, r, http.StatusNotFound, "subtitle_timeline_unavailable", "No current subtitle timeline is available.")
		return
	}
	for _, track := range data.Tracks {
		if track.StreamIndex != index {
			continue
		}
		payload, err := json.Marshal(subtitleTimelineTrackDTO{media.SourceID(id), artifact.SourceStamp, index, data.DurationTicks, track.Intervals})
		if err != nil || len(payload) > 1<<20 {
			apiError(w, r, http.StatusInternalServerError, "subtitle_timeline_invalid", "The subtitle timeline exceeds its response budget.")
			return
		}
		// Reopen current authority and the source-bound manifest before exposing data.
		current, err := s.library.GetSubtitleTimelineFor(r.Context(), subject, id)
		if err != nil {
			if unavailableSubtitleTimeline(err) {
				apiError(w, r, http.StatusNotFound, "subtitle_timeline_unavailable", "No current subtitle timeline is available.")
			} else {
				s.subtitleTimelineError(w, r, err)
			}
			return
		}
		if !current.Available || current.Stale || current.ETag != artifact.ETag || current.SourceStamp != artifact.SourceStamp {
			apiError(w, r, http.StatusNotFound, "subtitle_timeline_changed", "Reload the current subtitle timeline descriptor.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("ETag", `"`+strings.Trim(artifact.ETag, `"`)+"-"+strconv.Itoa(index)+`"`)
		http.ServeContent(w, r, "subtitle-timeline.json", artifact.ModifiedAt, bytes.NewReader(payload))
		return
	}
	apiError(w, r, http.StatusNotFound, "not_found", "The subtitle timeline track was not found.")
}
