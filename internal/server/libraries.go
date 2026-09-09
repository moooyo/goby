package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
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
	case errors.Is(err, library.ErrNotFound):
		return 404, "not_found", "The library resource was not found."
	case errors.Is(err, library.ErrForbidden):
		return 403, "access_denied", "The resource or directory is not permitted."
	case errors.Is(err, library.ErrInvalidInput):
		return 400, "invalid_input", "Check the library name, media type, configured directories, and query parameters."
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
	item, err := s.library.CreateLibrary(r.Context(), body.Name, body.CollectionType, body.Paths)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	response := map[string]any{"Library": libraryDTO(item)}
	if body.Scan {
		job, err := s.library.StartScan(r.Context(), item.ID)
		if err != nil {
			_, code, message := libraryErrorInfo(err)
			response["ScanError"] = map[string]string{"Code": code, "Message": message}
		} else {
			response["Job"] = jobDTO(job)
		}
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	s.log.Info("library created", "actor_id", principal.User.ID, "library_id", item.ID)
	jsonResponse(w, 201, response)
}

func (s *Server) deleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.library.DeleteLibrary(r.Context(), id); err != nil {
		s.libraryError(w, r, err)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	s.log.Info("library removed", "actor_id", principal.User.ID, "library_id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) scanLibrary(w http.ResponseWriter, r *http.Request) {
	options, ok := decodeScanOptions(w, r)
	if !ok {
		return
	}
	job, err := s.library.StartScanWithOptions(r.Context(), r.PathValue("id"), options)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
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
	if err := s.library.CancelJob(r.Context(), id); err != nil {
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
	if !principal.User.IsAdministrator {
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
	item, err := s.library.CreateLibrary(r.Context(), body.Name, body.CollectionType, body.Paths)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	if body.RefreshLibrary {
		if _, err := s.library.StartScan(r.Context(), item.ID); err != nil {
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
	if err := s.library.DeleteLibrary(r.Context(), body.Id); err != nil {
		s.libraryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) embyRefreshLibraries(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	libraries, err := s.library.ListLibraries(r.Context())
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	for _, item := range libraries {
		if _, err := s.library.StartScan(r.Context(), item.ID); err != nil && !errors.Is(err, library.ErrBusy) {
			s.libraryError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
