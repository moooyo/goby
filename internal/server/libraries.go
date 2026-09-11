package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

func (s *Server) registerLibraryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/libraries", s.requireAdmin(s.listLibraries))
	mux.HandleFunc("POST /admin/v1/libraries", s.requireAdmin(s.createLibrary))
	mux.HandleFunc("DELETE /admin/v1/libraries/{id}", s.requireAdmin(s.deleteLibrary))
	mux.HandleFunc("POST /admin/v1/libraries/{id}/scan", s.requireAdmin(s.scanLibrary))
	mux.HandleFunc("GET /admin/v1/jobs", s.requireAdmin(s.listJobs))
	mux.HandleFunc("POST /admin/v1/jobs/{id}/cancel", s.requireAdmin(s.cancelJob))
	mux.HandleFunc("GET /admin/v1/storage/roots", s.requireAdmin(s.storageRoots))
	mux.HandleFunc("GET /emby/Users/{UserId}/Views", s.requireEmby(s.embyViews))
	mux.HandleFunc("GET /emby/Users/{UserId}/Items/Root", s.requireEmby(s.embyRoot))
	mux.HandleFunc("GET /emby/Users/{UserId}/Items", s.requireEmby(s.embyItems))
	mux.HandleFunc("GET /emby/Items", s.requireEmby(s.embyItems))
	mux.HandleFunc("GET /emby/Items/{Id}/Similar", s.requireEmby(s.embySimilar))
	mux.HandleFunc("GET /emby/Items/{Id}/ThemeMedia", s.requireEmby(s.embyThemeMedia))
	mux.HandleFunc("GET /emby/Users/{UserId}/Items/Latest", s.requireEmby(s.embyLatest))
	mux.HandleFunc("GET /emby/Users/{UserId}/Items/{Id}", s.requireEmby(s.embyItem))
	mux.HandleFunc("GET /emby/Shows/{Id}/Seasons", s.requireEmby(s.embySeasons))
	mux.HandleFunc("GET /emby/Shows/{Id}/Episodes", s.requireEmby(s.embyEpisodes))
	mux.HandleFunc("GET /emby/Library/VirtualFolders/Query", s.requireEmby(s.embyLibraryList))
	mux.HandleFunc("POST /emby/Library/VirtualFolders", s.requireEmby(s.embyCreateLibrary))
	mux.HandleFunc("POST /emby/Library/VirtualFolders/Delete", s.requireEmby(s.embyDeleteLibrary))
	mux.HandleFunc("POST /emby/Library/Refresh", s.requireEmby(s.embyRefreshLibraries))
}

func libraryDTO(item library.Library) map[string]any {
	return map[string]any{"Id": item.ID, "Name": item.Name, "CollectionType": item.CollectionType, "Paths": item.Paths, "CreatedAt": item.CreatedAt, "LastScanAt": item.LastScanAt}
}

func jobDTO(job library.Job) map[string]any {
	status := strings.ToLower(job.Status)
	if status == "queued" {
		status = "pending"
	}
	return map[string]any{"Id": job.ID, "LibraryId": job.LibraryID, "ForceProbe": job.ForceProbe, "Status": status, "Error": job.Error, "Scanned": job.Scanned, "Added": job.Added, "Updated": job.Updated, "CreatedAt": job.CreatedAt, "StartedAt": job.StartedAt, "FinishedAt": job.FinishedAt}
}

func libraryErrorInfo(err error) (int, string, string) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		return 401, "unauthorized", "A valid login is required."
	case errors.Is(err, identity.ErrClientSessionForbidden):
		return 403, "administrator_required", "Administrator access is required."
	case errors.Is(err, library.ErrNotFound):
		return 404, "not_found", "The library resource was not found."
	case errors.Is(err, library.ErrForbidden):
		return 403, "access_denied", "The resource or directory is not permitted."
	case errors.Is(err, library.ErrInvalidInput):
		return 400, "invalid_input", "Check the library name, media type, configured directories, and query parameters."
	case errors.Is(err, library.ErrUnsupportedFilter):
		return http.StatusNotImplemented, "unsupported_filter", "List membership cannot be evaluated for this result set."
	case errors.Is(err, library.ErrBusy):
		return 409, "scan_busy", "A scan is already active or the scan queue is full."
	case errors.Is(err, library.ErrUnavailable):
		return 503, "library_unavailable", "The configured media directory or scanner is unavailable."
	default:
		return 500, "internal_error", "The library operation could not be completed."
	}
}

func (s *Server) libraryError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := libraryErrorInfo(err)
	if status == 500 {
		s.log.Error("library operation failed", "request_id", r.Context().Value(requestIDKey))
	}
	apiError(w, r, status, code, message)
}

func (s *Server) listLibraries(w http.ResponseWriter, r *http.Request) {
	libraries, err := s.library.ListLibraries(r.Context())
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(libraries))
	for _, item := range libraries {
		items = append(items, libraryDTO(item))
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) createLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, CollectionType string
		Paths                []string
		Scan                 bool
	}
	if !decodeBody(w, r, &body) {
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	item, err := s.library.CreateLibraryAsAdministrator(r.Context(), principal, identity.AdministratorNative, body.Name, body.CollectionType, body.Paths)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	response := map[string]any{"Library": libraryDTO(item)}
	if body.Scan {
		job, err := s.library.StartScanAsAdministrator(r.Context(), principal, identity.AdministratorNative, item.ID, library.ScanOptions{})
		if err != nil {
			_, code, message := libraryErrorInfo(err)
			response["ScanError"] = map[string]string{"Code": code, "Message": message}
		} else {
			response["Job"] = jobDTO(job)
		}
	}
	s.log.Info("library created", "actor_id", principal.User.ID, "library_id", item.ID)
	jsonResponse(w, 201, response)
}

func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	principal := r.Context().Value(principalKey).(identity.Principal)
	if err := s.library.DeleteLibraryAsAdministrator(r.Context(), principal, identity.AdministratorNative, id); err != nil {
		s.libraryError(w, r, err)
		return
	}
	s.log.Info("library removed", "actor_id", principal.User.ID, "library_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) {
	options, ok := decodeScanOptions(w, r)
	if !ok {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	job, err := s.library.StartScanAsAdministrator(r.Context(), actor, identity.AdministratorNative, r.PathValue("id"), options)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	s.log.Info("library scan requested", "actor_id", actor.User.ID, "library_id", job.LibraryID,
		"job_id", job.ID, "force_probe", job.ForceProbe)
	jsonResponse(w, http.StatusAccepted, map[string]any{"Job": jobDTO(job)})
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.library.ListJobs(r.Context())
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, jobDTO(job))
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor := r.Context().Value(principalKey).(identity.Principal)
	if err := s.library.CancelJobAsAdministrator(r.Context(), actor, identity.AdministratorNative, id); err != nil {
		s.libraryError(w, r, err)
		return
	}
	job, err := s.library.GetJob(r.Context(), id)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"Job": jobDTO(job)})
}

func (s *Server) storageRoots(w http.ResponseWriter, r *http.Request) {
	items := make([]map[string]any, 0, len(s.cfg.MediaRoots))
	for _, root := range s.cfg.MediaRoots {
		available := false
		if anchor, err := os.OpenRoot(root); err == nil {
			if directory, openErr := anchor.Open("."); openErr == nil {
				_, readErr := directory.ReadDir(1)
				available = readErr == nil || errors.Is(readErr, io.EOF)
				directory.Close()
			}
			anchor.Close()
		}
		items = append(items, map[string]any{"Path": root, "Available": available})
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "Configured": len(items) > 0})
}

func (s *Server) embyAdministrator(w http.ResponseWriter, r *http.Request) bool {
	principal := r.Context().Value(principalKey).(identity.Principal)
	if !principal.CanManageServer() {
		apiError(w, r, 403, "administrator_required", "Administrator access is required.")
		return false
	}
	return true
}

func (s *Server) embyLibraryList(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	libraries, err := s.library.ListLibraries(r.Context())
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	items := make([]map[string]any, 0, len(libraries))
	for _, item := range libraries {
		items = append(items, map[string]any{"Name": item.Name, "ItemId": item.ID, "CollectionType": item.CollectionType, "Locations": item.Paths})
	}
	jsonResponse(w, 200, map[string]any{"Items": items, "TotalRecordCount": len(items)})
}

func (s *Server) embyCreateLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct {
		Name, CollectionType string
		Paths                []string
		RefreshLibrary       bool
	}
	if !decodeBody(w, r, &body) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	item, err := s.library.CreateLibraryAsAdministrator(r.Context(), actor, identity.AdministratorEmby, body.Name, body.CollectionType, body.Paths)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	if body.RefreshLibrary {
		if _, err := s.library.StartScanAsAdministrator(r.Context(), actor, identity.AdministratorEmby, item.ID, library.ScanOptions{}); err != nil {
			s.libraryError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) embyDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct {
		Id             string
		RefreshLibrary bool
	}
	if !decodeBody(w, r, &body) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if err := s.library.DeleteLibraryAsAdministrator(r.Context(), actor, identity.AdministratorEmby, body.Id); err != nil {
		s.libraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) embyRefreshLibraries(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	if s.taskStore == nil {
		s.scheduledTaskError(w, r, tasks.ErrUnavailable)
		return
	}
	definition, err := s.taskStore.GetByKey(r.Context(), tasks.LibraryScanKey)
	if err != nil {
		s.scheduledTaskError(w, r, err)
		return
	}
	// The durable task owns a snapshot of every library. Bounded scanner
	// admission can defer children without dropping later libraries as busy.
	if _, err := s.taskManager.Start(r.Context(), embyTaskActor(r), tasks.StartRequest{TaskID: definition.ID}); err != nil {
		s.scheduledTaskError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
