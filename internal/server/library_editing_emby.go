package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerEmbyLibraryEditingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /emby/Library/VirtualFolders/Name", s.requireEmby(s.embyRenameLibrary))
	mux.HandleFunc("POST /emby/Library/VirtualFolders/Paths", s.requireEmby(s.embyAddLibraryPath))
	mux.HandleFunc("POST /emby/Library/VirtualFolders/Paths/Delete", s.requireEmby(s.embyRemoveLibraryPath))
	mux.HandleFunc("POST /emby/Library/VirtualFolders/LibraryOptions", s.requireEmby(s.embyUpdateLibraryOptions))
	mux.HandleFunc("GET /emby/Environment/DefaultDirectoryBrowser", s.requireEmby(s.embyDefaultDirectoryBrowser))
	mux.HandleFunc("GET /emby/Environment/DirectoryContents", s.requireEmby(s.embyDirectoryContents))
	mux.HandleFunc("GET /emby/Environment/ParentPath", s.requireEmby(s.embyDirectoryParent))
	mux.HandleFunc("POST /emby/Environment/ValidatePath", s.requireEmby(s.embyValidateDirectory))
}

// DisabledLocalMetadataReaders is the supported standard LibraryOptions field.
// The native API separately exposes the local artwork importer switch.
type embyLibraryOptionsUpdate struct {
	DisabledLocalMetadataReaders *[]string
}

func (value *embyLibraryOptionsUpdate) native() (*library.LibraryOptionsUpdate, error) {
	if value == nil || value.DisabledLocalMetadataReaders == nil {
		return nil, library.ErrInvalidInput
	}
	readers := *value.DisabledLocalMetadataReaders
	if len(readers) > 1 || len(readers) == 1 && !strings.EqualFold(readers[0], "Nfo") {
		return nil, library.ErrInvalidInput
	}
	enabled := len(readers) == 0
	return &library.LibraryOptionsUpdate{EnableLocalMetadata: &enabled}, nil
}

func embyEditableLibraryOptions(value library.Library) map[string]any {
	disabled := []string{}
	if !library.EffectiveLibraryOptions(value).EnableLocalMetadata {
		disabled = append(disabled, "Nfo")
	}
	return map[string]any{"DisabledLocalMetadataReaders": disabled}
}

func (s *Server) embyLibraryEditing(w http.ResponseWriter, r *http.Request, id string) (library.LibraryEditing, bool) {
	if !s.embyAdministrator(w, r) {
		return library.LibraryEditing{}, false
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.library.GetLibraryEditingAsAdministrator(r.Context(), actor, identity.AdministratorEmby, id)
	if err != nil {
		s.libraryError(w, r, err)
		return library.LibraryEditing{}, false
	}
	return value, true
}

func (s *Server) applyEmbyLibraryEdit(w http.ResponseWriter, r *http.Request, id string, update library.LibraryUpdate, refresh bool) {
	actor := r.Context().Value(principalKey).(identity.Principal)
	value, err := s.library.UpdateLibraryAsAdministrator(r.Context(), actor, identity.AdministratorEmby, id, update)
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	if refresh {
		if _, err := s.library.StartScanAsAdministrator(r.Context(), actor, identity.AdministratorEmby, id, library.ScanOptions{}); err != nil {
			// The edit has committed. Preserve the empty success contract instead
			// of inviting a duplicate mutation when optional scan admission fails.
			s.log.Warn("edited library refresh unavailable", "library_id", id)
		}
	}
	w.Header().Set("ETag", `"`+value.Library.Revision+`"`)
	w.WriteHeader(http.StatusNoContent)
}

func embyLibraryEditRevision(r *http.Request, previous library.Library) string {
	// Standard clients omit a revision. Their mutation still uses an atomic
	// snapshot CAS and never retries a conflict over newer administrator edits.
	if value := r.Header.Get("If-Match"); value != "" {
		return strings.Trim(value, `"`)
	}
	return previous.Revision
}

func (s *Server) embyRenameLibrary(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct{ Id, NewName string }
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	value, ok := s.embyLibraryEditing(w, r, body.Id)
	if !ok {
		return
	}
	s.applyEmbyLibraryEdit(w, r, body.Id, library.LibraryUpdate{Revision: embyLibraryEditRevision(r, value.Library), Name: &body.NewName}, false)
}

func (s *Server) embyAddLibraryPath(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct {
		Id, Path       string
		PathInfo       *struct{ Path, NetworkPath, Username, Password string }
		RefreshLibrary bool
	}
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	if body.PathInfo != nil {
		if body.PathInfo.NetworkPath != "" || body.PathInfo.Username != "" || body.PathInfo.Password != "" || body.Path != "" && body.Path != body.PathInfo.Path {
			s.libraryError(w, r, library.ErrInvalidInput)
			return
		}
		body.Path = body.PathInfo.Path
	}
	value, ok := s.embyLibraryEditing(w, r, body.Id)
	if !ok {
		return
	}
	paths := append(append([]string(nil), value.Library.Paths...), body.Path)
	s.applyEmbyLibraryEdit(w, r, body.Id, library.LibraryUpdate{Revision: embyLibraryEditRevision(r, value.Library), Paths: &paths}, body.RefreshLibrary)
}

func (s *Server) embyRemoveLibraryPath(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct {
		Id, Path       string
		RefreshLibrary bool
	}
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	value, ok := s.embyLibraryEditing(w, r, body.Id)
	if !ok {
		return
	}
	paths := make([]string, 0, len(value.Library.Paths))
	found := false
	for _, path := range value.Library.Paths {
		if path == body.Path {
			found = true
		} else {
			paths = append(paths, path)
		}
	}
	if !found {
		s.libraryError(w, r, library.ErrNotFound)
		return
	}
	// The explicit compatibility remove endpoint supplies the same deletion
	// acknowledgment as the native editor. It never deletes filesystem content.
	s.applyEmbyLibraryEdit(w, r, body.Id, library.LibraryUpdate{Revision: embyLibraryEditRevision(r, value.Library), Paths: &paths, AcknowledgePathRemoval: true}, body.RefreshLibrary)
}

func (s *Server) embyUpdateLibraryOptions(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct {
		Id             string
		LibraryOptions *embyLibraryOptionsUpdate
	}
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	options, err := body.LibraryOptions.native()
	if err != nil {
		s.libraryError(w, r, err)
		return
	}
	value, ok := s.embyLibraryEditing(w, r, body.Id)
	if !ok {
		return
	}
	s.applyEmbyLibraryEdit(w, r, body.Id, library.LibraryUpdate{Revision: embyLibraryEditRevision(r, value.Library), LibraryOptions: options}, false)
}

func (s *Server) embyDefaultDirectoryBrowser(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if _, err := s.library.BrowseServerDirectories(r.Context(), actor, identity.AdministratorEmby, "", 0, 1); err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"Path": ""})
}

func (s *Server) embyDirectoryContents(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	query := r.URL.Query()
	includeDirectories := true
	for _, name := range []string{"IncludeFiles", "IncludeDirectories"} {
		if values, exists := query[name]; exists {
			if len(values) != 1 {
				s.libraryError(w, r, library.ErrInvalidInput)
				return
			}
			value, err := strconv.ParseBool(values[0])
			if err != nil || name == "IncludeFiles" && value {
				s.libraryError(w, r, library.ErrInvalidInput)
				return
			}
			if name == "IncludeDirectories" {
				includeDirectories = value
			}
		}
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	// Compatibility has no page metadata. Refuse a truncated answer rather than
	// presenting the first page as a complete directory listing.
	page, err := s.library.BrowseServerDirectories(r.Context(), actor, identity.AdministratorEmby, query.Get("Path"), 0, 200)
	if err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	if page.TotalRecordCount > 200 {
		s.libraryError(w, r, library.ErrDirectoryLimit)
		return
	}
	items := make([]map[string]string, 0, len(page.Items))
	if includeDirectories {
		for _, item := range page.Items {
			items = append(items, map[string]string{"Name": item.Name, "Path": item.Path, "Type": "Directory"})
		}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (s *Server) embyDirectoryParent(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	parent, err := s.library.ServerDirectoryParent(r.Context(), actor, identity.AdministratorEmby, r.URL.Query().Get("Path"))
	if err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, parent)
}

func (s *Server) embyValidateDirectory(w http.ResponseWriter, r *http.Request) {
	if !s.embyAdministrator(w, r) {
		return
	}
	var body struct {
		ValidateWriteable, IsFile bool
		Username, Password        string
	}
	if !decodeLibraryEditingBody(w, r, &body) {
		return
	}
	if body.ValidateWriteable || body.IsFile || body.Username != "" || body.Password != "" {
		s.libraryError(w, r, library.ErrInvalidInput)
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if _, err := s.library.ValidateServerDirectory(r.Context(), actor, identity.AdministratorEmby, r.URL.Query().Get("Path")); err != nil {
		s.serverDirectoryError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
