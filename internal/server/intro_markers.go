package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerIntroMarkerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/items/{id}/intro", s.requireAdmin(s.adminItemIntro))
	mux.HandleFunc("PUT /admin/v1/items/{id}/intro", s.requireAdmin(s.updateAdminItemIntro))
	mux.HandleFunc("DELETE /admin/v1/items/{id}/intro", s.requireAdmin(s.resetAdminItemIntro))
}

func (s *Server) adminItemIntro(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.GetItemIntro(r.Context(), actor, id)
	if err != nil {
		s.introError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, detail)
}

func (s *Server) updateAdminItemIntro(w http.ResponseWriter, r *http.Request) {
	s.mutateAdminItemIntro(w, r, false)
}

func (s *Server) resetAdminItemIntro(w http.ResponseWriter, r *http.Request) {
	s.mutateAdminItemIntro(w, r, true)
}

func (s *Server) mutateAdminItemIntro(w http.ResponseWriter, r *http.Request, reset bool) {
	id, ok := adminMetadataID(w, r)
	if !ok || !adminMetadataNoQuery(w, r) {
		return
	}
	var raw json.RawMessage
	if !decodeBody(w, r, &raw) {
		return
	}
	edit, err := decodeIntroEdit(raw, reset)
	if err != nil {
		apiError(w, r, 400, "invalid_input", "Supply exact revision, source revision and one valid intro interval.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	detail, err := s.library.UpdateItemIntro(r.Context(), actor, id, edit, reset)
	if err != nil {
		s.introError(w, r, err)
		return
	}
	s.log.Info("administrator intro mutation", "actor_id", actor.User.ID, "item_id", id, "revision", detail.Revision)
	jsonResponse(w, http.StatusOK, detail)
}

// Reject absent scalars, arrays and duplicate/unknown fields. Tick zero is a
// valid explicit start and must not also mean that StartTicks was omitted.
func decodeIntroEdit(raw []byte, reset bool) (library.IntroEdit, error) {
	var edit library.IntroEdit
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
		required["StartTicks"], required["EndTicks"], required["Provenance"] = &edit.StartTicks, &edit.EndTicks, &edit.Provenance
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

func (s *Server) introError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, library.ErrIntroRevisionConflict) {
		apiError(w, r, http.StatusConflict, "intro_revision_conflict", "The intro or media source changed. Reload the current values before editing.")
		return
	}
	s.libraryError(w, r, err)
}

// Emby represents an interval with two ChapterInfo marker positions. The
// client owns any resulting seek; these source facts do not depend on its mode.
func itemChaptersDTO(item library.Item) []map[string]any {
	chapters := make([]map[string]any, 0)
	if item.Media == nil {
		return chapters
	}
	for _, chapter := range item.Media.Chapters {
		chapters = append(chapters, map[string]any{"StartPositionTicks": chapter.StartTicks, "Name": chapter.Title, "MarkerType": "Chapter"})
	}
	if item.Intro != nil {
		chapters = append(chapters,
			map[string]any{"StartPositionTicks": item.Intro.StartTicks, "Name": "Intro Start", "MarkerType": "IntroStart"},
			map[string]any{"StartPositionTicks": item.Intro.EndTicks, "Name": "Intro End", "MarkerType": "IntroEnd"})
	}
	sort.SliceStable(chapters, func(i, j int) bool {
		return chapters[i]["StartPositionTicks"].(int64) < chapters[j]["StartPositionTicks"].(int64)
	})
	for index := range chapters {
		chapters[index]["ChapterIndex"] = index
	}
	return chapters
}
