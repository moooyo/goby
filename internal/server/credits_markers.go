package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerCreditsMarkerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/items/{id}/credits", s.requireAdmin(s.adminItemCredits))
	mux.HandleFunc("PUT /admin/v1/items/{id}/credits", s.requireAdmin(s.updateAdminItemCredits))
	mux.HandleFunc("DELETE /admin/v1/items/{id}/credits", s.requireAdmin(s.resetAdminItemCredits))
}

func (s *Server) adminItemCredits(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.GetItemCredits(r.Context(), actor, id)
	if err != nil {
		s.creditsError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, detail)
}

func (s *Server) updateAdminItemCredits(w http.ResponseWriter, r *http.Request) {
	s.mutateAdminItemCredits(w, r, false)
}

func (s *Server) resetAdminItemCredits(w http.ResponseWriter, r *http.Request) {
	s.mutateAdminItemCredits(w, r, true)
}

func (s *Server) mutateAdminItemCredits(w http.ResponseWriter, r *http.Request, reset bool) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	var raw json.RawMessage
	if !decodeBody(w, r, &raw) {
		return
	}
	edit, err := decodeCreditsEdit(raw, reset)
	if err != nil {
		apiError(w, r, 400, "invalid_input", "Supply exact revision, source revision and one valid credits start.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.UpdateItemCredits(r.Context(), actor, id, edit, reset)
	if err != nil {
		s.creditsError(w, r, err)
		return
	}
	s.log.Info("administrator credits mutation", "actor_id", actor.User.ID, "item_id", id, "revision", detail.Revision)
	jsonResponse(w, http.StatusOK, detail)
}

// Reject absent scalars, arrays and duplicate/unknown fields. Tick zero is a
// valid explicit start and must not also mean that StartTicks was omitted.
func decodeCreditsEdit(raw []byte, reset bool) (library.CreditsEdit, error) {
	var edit library.CreditsEdit
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return edit, library.ErrInvalidInput
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return edit, library.ErrInvalidInput
		}
		name, ok := token.(string)
		if !ok || fields[name] != nil {
			return edit, library.ErrInvalidInput
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return edit, library.ErrInvalidInput
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return edit, library.ErrInvalidInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return edit, library.ErrInvalidInput
	}
	required := map[string]any{"Revision": &edit.Revision, "SourceRevision": &edit.SourceRevision}
	if !reset {
		required["StartTicks"], required["Provenance"] = &edit.StartTicks, &edit.Provenance
	}
	if len(fields) != len(required) {
		return edit, library.ErrInvalidInput
	}
	for name, target := range required {
		if len(fields[name]) == 0 || json.Unmarshal(fields[name], target) != nil {
			return edit, library.ErrInvalidInput
		}
	}
	return edit, nil
}

func (s *Server) creditsError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrCreditsRevisionConflict) {
		apiError(w, r, http.StatusConflict, "credits_revision_conflict", "The credits or media source changed. Reload the current values before editing.")
		return
	}
	s.libraryError(w, r, err)
}
