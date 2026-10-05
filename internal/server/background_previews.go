package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

func (s *Server) registerBackgroundPreviewRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/background-previews/configuration", s.requireAdmin(s.adminBackgroundPreviewConfiguration))
	mux.HandleFunc("PUT /admin/v1/background-previews/configuration", s.requireAdmin(s.updateAdminBackgroundPreviewConfiguration))
	mux.HandleFunc("GET /admin/v1/background-previews/items", s.requireAdmin(s.adminBackgroundPreviewItems))
	mux.HandleFunc("GET /admin/v1/items/{id}/background-preview", s.requireAdmin(s.adminBackgroundPreviewItem))
	mux.HandleFunc("PUT /admin/v1/items/{id}/background-preview", s.requireAdmin(s.updateAdminBackgroundPreviewItem))
	mux.HandleFunc("GET /emby/Items/{Id}/BackgroundPreview", s.requireEmby(s.embyBackgroundPreview))
	mux.HandleFunc("GET /emby/Items/{Id}/BackgroundPreview/stream.mp4", s.requireEmby(s.embyBackgroundPreviewStream))
}

func (s *Server) backgroundPreviewError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrBackgroundPreviewConflict) || errors.Is(err, library.ErrBackgroundClipConflict) {
		apiError(w, r, http.StatusConflict, "background_preview_conflict", "The background preview state changed or its existing files need explicit review. Reload before editing.")
		return
	}
	s.mediaAnalysisError(w, r, err)
}

func (s *Server) adminBackgroundPreviewConfiguration(w http.ResponseWriter, r *http.Request) {
	if !adminMediaAnalysisNoQuery(w, r) {
		return
	}
	configuration, err := s.library.GetBackgroundPreviewConfiguration(r.Context(), adminMediaAnalysisActor(r))
	if err != nil {
		s.backgroundPreviewError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, configuration)
}

func (s *Server) updateAdminBackgroundPreviewConfiguration(w http.ResponseWriter, r *http.Request) {
	values, ok := adminTaskBody(w, r, []string{"Revision", "Profile"}, []string{"Revision", "Profile"})
	if !ok {
		return
	}
	fields := []string{"DurationSeconds", "MaxWidth", "VideoBitrate", "MaxItemRuntimeSeconds"}
	if _, invalid := adminTaskObject(values["Profile"], fields, fields, "Profile"); invalid != nil {
		adminTaskInputError(w, r, invalid)
		return
	}
	var input library.BackgroundPreviewConfigurationUpdate
	if json.Unmarshal(values["Revision"], &input.Revision) != nil || json.Unmarshal(values["Profile"], &input.Profile) != nil {
		adminTaskInputError(w, r, map[string]string{"Body": "Supply the current revision and complete integer profile."})
		return
	}
	configuration, err := s.library.UpdateBackgroundPreviewConfiguration(r.Context(), adminMediaAnalysisActor(r), input)
	if err != nil {
		s.backgroundPreviewError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, configuration)
}

type adminBackgroundPreviewDetail struct {
	library.BackgroundPreviewDetail
	Artifact library.BackgroundClipArtifact `json:"Artifact"`
}

func (s *Server) backgroundPreviewDetail(r *http.Request, detail library.BackgroundPreviewDetail) adminBackgroundPreviewDetail {
	artifact, _ := s.library.GetBackgroundPreviewFor(r.Context(), library.Subject{UserID: adminMediaAnalysisActor(r).User.ID}, detail.ItemID)
	return adminBackgroundPreviewDetail{detail, artifact}
}

func (s *Server) adminBackgroundPreviewItem(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMediaAnalysisNoQuery(w, r) {
		return
	}
	detail, err := s.library.GetBackgroundPreviewAsAdministrator(r.Context(), adminMediaAnalysisActor(r), id)
	if err != nil {
		s.backgroundPreviewError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.backgroundPreviewDetail(r, detail))
}

func (s *Server) updateAdminBackgroundPreviewItem(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	fields := []string{"Revision", "SourceRevision", "StartTicks"}
	values, ok := adminTaskBody(w, r, fields, fields)
	if !ok {
		return
	}
	var input library.BackgroundPreviewEdit
	if json.Unmarshal(values["Revision"], &input.Revision) != nil || json.Unmarshal(values["SourceRevision"], &input.SourceRevision) != nil || json.Unmarshal(values["StartTicks"], &input.StartTicks) != nil {
		adminTaskInputError(w, r, map[string]string{"Body": "Supply exact revisions and an integer start or null."})
		return
	}
	detail, err := s.library.UpdateBackgroundPreviewAsAdministrator(r.Context(), adminMediaAnalysisActor(r), id, input)
	if err != nil {
		s.backgroundPreviewError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, s.backgroundPreviewDetail(r, detail))
}

func (s *Server) adminBackgroundPreviewItems(w http.ResponseWriter, r *http.Request) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	start, limit := 0, 50
	valid := err == nil && len(r.URL.RawQuery) <= 4096
	for name, entries := range values {
		if len(entries) != 1 {
			valid = false
			break
		}
		switch name {
		case "LibraryId", "SearchTerm", "State":
		case "StartIndex", "Limit":
			number, parseErr := strconv.Atoi(entries[0])
			if parseErr != nil || strconv.Itoa(number) != entries[0] || number < 0 {
				valid = false
				break
			}
			if name == "Limit" {
				limit = number
			} else {
				start = number
			}
		default:
			valid = false
		}
	}
	if !valid || limit < 1 || limit > 200 {
		adminTaskInputError(w, r, map[string]string{"Query": "Supply supported item filters and pagination."})
		return
	}
	page, err := s.library.ListBackgroundPreviewItems(r.Context(), adminMediaAnalysisActor(r), values.Get("LibraryId"), values.Get("SearchTerm"), values.Get("State"), start, limit)
	if err != nil {
		s.backgroundPreviewError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, page)
}

func (s *Server) startAdminBackgroundPreviewRun(w http.ResponseWriter, r *http.Request, input adminMediaAnalysisRunInput) {
	if s.taskStore == nil || s.taskManager == nil || !(backgroundPreviewTaskExecutor{s}).Available() {
		s.taskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.GetByKey(r.Context(), library.TaskBackgroundPreviewGenerationKey)
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	if !definition.Enabled {
		s.taskError(w, r, tasks.ErrDisabled)
		return
	}
	queued, err := s.library.QueueBackgroundPreviews(r.Context(), adminMediaAnalysisActor(r), input.Selection, input.RequestID)
	if err != nil {
		s.backgroundPreviewError(w, r, err)
		return
	}
	result, err := s.taskManager.Start(r.Context(), adminTaskActor(r), tasks.StartRequest{TaskID: definition.ID, RequestID: input.RequestID})
	if err != nil {
		s.taskError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"RunId": result.Run.ID, "TaskId": result.Run.TaskID, "Admitted": result.Admitted, "Queued": queued.Queued})
}

func (s *Server) embyBackgroundPreview(w http.ResponseWriter, r *http.Request) {
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
	if item.IsFolder || item.Media == nil || item.Type != "Movie" && item.Type != "Episode" {
		jsonResponse(w, http.StatusOK, library.BackgroundClipArtifact{})
		return
	}
	artifact, err := s.library.GetBackgroundPreviewFor(r.Context(), subject, id)
	if errors.Is(err, library.ErrNotFound) {
		// Absence is a normal fallback only while the owner remains visible.
		if _, err := s.library.GetItemFor(r.Context(), subject, id); err != nil {
			s.libraryError(w, r, err)
			return
		}
		jsonResponse(w, http.StatusOK, library.BackgroundClipArtifact{})
		return
	}
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, struct {
		library.BackgroundClipArtifact
		StreamURL string `json:"StreamUrl"`
	}{artifact, "/emby/Items/" + url.PathEscape(id) + "/BackgroundPreview/stream.mp4?tag=" + url.QueryEscape(artifact.ETag)})
}

func (s *Server) embyBackgroundPreviewStream(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	file, artifact, err := s.library.OpenBackgroundPreviewFor(r.Context(), requestLibrarySubject(r, userID), r.PathValue("Id"))
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	defer file.Close()
	if tags, present := r.URL.Query()["tag"]; present && (len(tags) != 1 || tags[0] != artifact.ETag) {
		apiError(w, r, http.StatusNotFound, "background_preview_changed", "The background clip changed. Reload its current descriptor.")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", artifact.ETag)
	http.ServeContent(w, r, "background.mp4", artifact.ModifiedAt, file)
}
