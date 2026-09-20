package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// applyItemCapabilities belongs before final ExcludeFields projection. It uses
// the real actor rather than a UserId selected for another account's state.
// No filesystem observation or operation admission occurs while listing items.
func (s *Server) applyItemCapabilities(w http.ResponseWriter, r *http.Request, items []map[string]any, detail bool) bool {
	fields := queryValues(r.URL.Query()["Fields"])
	wantDelete, wantDownload := detail || hasField(fields, "CanDelete"), detail || hasField(fields, "CanDownload")
	if !wantDelete && !wantDownload || len(items) == 0 {
		return true
	}
	ids := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		id, _ := item["Id"].(string)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	actor, ok := r.Context().Value(principalKey).(identity.Principal)
	if !ok {
		s.libraryError(w, r, library.ErrForbidden)
		return false
	}
	capabilities := make(map[string]library.ItemCapabilities, len(ids))
	for start := 0; start < len(ids); start += 1000 {
		batch, err := s.library.ItemCapabilitiesFor(r.Context(), actor, ids[start:min(start+1000, len(ids))])
		if err != nil {
			s.libraryError(w, r, err)
			return false
		}
		for id, capability := range batch {
			capabilities[id] = capability
		}
	}
	for _, item := range items {
		id, _ := item["Id"].(string)
		capability := capabilities[id]
		if wantDelete {
			item["CanDelete"] = capability.CanDelete
		}
		if wantDownload {
			item["CanDownload"] = capability.CanDownload
		}
	}
	return true
}
