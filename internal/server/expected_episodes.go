package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func (s *Server) registerExpectedEpisodeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/v1/series/{id}/episode-roster", s.requireAdmin(s.adminEpisodeRoster))
	mux.HandleFunc("PUT /admin/v1/series/{id}/episode-roster", s.requireAdmin(s.replaceAdminEpisodeRoster))
	mux.HandleFunc("DELETE /admin/v1/series/{id}/episode-roster", s.requireAdmin(s.withdrawAdminEpisodeRoster))
}

func episodeRosterInputError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	requestID, _ := r.Context().Value(requestIDKey).(string)
	jsonResponse(w, http.StatusBadRequest, map[string]any{
		"Error":     map[string]any{"Code": "invalid_input", "Message": "Check the episode roster fields.", "Fields": fields},
		"RequestId": requestID,
	})
}

func episodeRosterNoQuery(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		episodeRosterInputError(w, r, map[string]string{"Query": "This operation does not accept query parameters."})
		return false
	}
	return true
}

func episodeRosterResponse(w http.ResponseWriter, detail library.EpisodeRosterDetail) {
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, http.StatusOK, detail)
}

func (s *Server) adminEpisodeRoster(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok || !episodeRosterNoQuery(w, r) {
		return
	}
	detail, err := s.library.GetEpisodeRoster(r.Context(), r.Context().Value(principalKey).(identity.Principal), id)
	if err != nil {
		s.episodeRosterError(w, r, err)
		return
	}
	episodeRosterResponse(w, detail)
}

func (s *Server) replaceAdminEpisodeRoster(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	edit, ok := decodeEpisodeRosterEdit(w, r, false)
	if !ok {
		return
	}
	detail, err := s.library.ReplaceEpisodeRoster(r.Context(), r.Context().Value(principalKey).(identity.Principal), id, edit)
	if err != nil {
		s.episodeRosterError(w, r, err)
		return
	}
	episodeRosterResponse(w, detail)
}

func (s *Server) withdrawAdminEpisodeRoster(w http.ResponseWriter, r *http.Request) {
	id, ok := adminMetadataID(w, r)
	if !ok {
		return
	}
	edit, ok := decodeEpisodeRosterEdit(w, r, true)
	if !ok {
		return
	}
	detail, err := s.library.WithdrawEpisodeRoster(r.Context(), r.Context().Value(principalKey).(identity.Principal), id, edit.Revision)
	if err != nil {
		s.episodeRosterError(w, r, err)
		return
	}
	episodeRosterResponse(w, detail)
}

func (s *Server) episodeRosterError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, library.ErrRevisionConflict):
		apiError(w, r, http.StatusConflict, "revision_conflict", "The episode roster changed. Reload its revision before saving.")
	case errors.Is(err, library.ErrEpisodeRosterSourceRevision):
		apiError(w, r, http.StatusConflict, "source_revision_conflict", "This source revision already identifies different content. Supply a new source revision.")
	case errors.Is(err, library.ErrInvalidInput):
		episodeRosterInputError(w, r, map[string]string{"Body": "Supply a bounded, valid episode roster."})
	default:
		s.libraryError(w, r, err)
	}
}

func decodeEpisodeRosterEdit(w http.ResponseWriter, r *http.Request, withdraw bool) (library.EpisodeRosterEdit, bool) {
	var edit library.EpisodeRosterEdit
	if !episodeRosterNoQuery(w, r) {
		return edit, false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || len(r.Header.Values("Content-Type")) != 1 || mediaType != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json for this request.")
		return edit, false
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, library.MaxEpisodeRosterBytes))
	if err != nil || !utf8.Valid(raw) || !adminSettingsUnicode(raw) {
		episodeRosterInputError(w, r, map[string]string{"Body": "Supply lossless UTF-8 JSON no larger than 512 KiB."})
		return edit, false
	}
	fields := []string{"Revision"}
	if !withdraw {
		fields = append(fields, "Source", "Entries")
	}
	values, invalid := adminTaskObject(raw, fields, fields, "")
	if len(invalid) != 0 {
		episodeRosterInputError(w, r, invalid)
		return edit, false
	}
	invalid = map[string]string{}
	edit.Revision = adminMediaOperationString(values["Revision"], "Revision", 20, false, invalid)
	revision, err := strconv.ParseInt(edit.Revision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != edit.Revision {
		invalid["Revision"] = "Supply a canonical nonnegative decimal revision string."
	}
	if !withdraw {
		sourceFields := []string{"Key", "Label", "Revision"}
		source, sourceInvalid := adminTaskObject(values["Source"], sourceFields, sourceFields, "Source")
		for field, message := range sourceInvalid {
			invalid[field] = message
		}
		if len(sourceInvalid) == 0 {
			edit.Source.Key = adminMediaOperationString(source["Key"], "Source.Key", 128, false, invalid)
			edit.Source.Label = adminMediaOperationString(source["Label"], "Source.Label", 256, true, invalid)
			edit.Source.Revision = adminMediaOperationString(source["Revision"], "Source.Revision", 128, false, invalid)
		}
		var entries []json.RawMessage
		if json.Unmarshal(values["Entries"], &entries) != nil || entries == nil || len(entries) > library.MaxEpisodeRosterEntries {
			invalid["Entries"] = "Supply an array containing at most 2000 explicit episode records."
		} else {
			edit.Entries = make([]library.EpisodeRosterEntryInput, 0, len(entries))
			keys, numbers := map[string]bool{}, map[[2]int]bool{}
			for index, entryRaw := range entries {
				prefix := fmt.Sprintf("Entries.%d", index)
				entryFields := []string{"Key", "SeasonNumber", "EpisodeNumber", "Name", "PremiereDate"}
				entryValues, entryInvalid := adminTaskObject(entryRaw, entryFields, entryFields[:4], prefix)
				for field, message := range entryInvalid {
					invalid[field] = message
				}
				if len(entryInvalid) != 0 {
					continue
				}
				entry := library.EpisodeRosterEntryInput{
					Key:  adminMediaOperationString(entryValues["Key"], prefix+".Key", 128, false, invalid),
					Name: adminMediaOperationString(entryValues["Name"], prefix+".Name", 512, true, invalid),
				}
				for _, number := range []struct {
					name   string
					target *int
				}{{"SeasonNumber", &entry.SeasonNumber}, {"EpisodeNumber", &entry.EpisodeNumber}} {
					value := entryValues[number.name]
					if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, number.target) != nil || *number.target < 0 || *number.target > 9999 {
						invalid[prefix+"."+number.name] = "Supply an integer from 0 through 9999."
					}
				}
				if value, exists := entryValues["PremiereDate"]; exists {
					entry.PremiereDate = adminMediaOperationString(value, prefix+".PremiereDate", 10, false, invalid)
					date, err := time.Parse(time.DateOnly, entry.PremiereDate)
					if err != nil || date.Year() < 1 || date.Format(time.DateOnly) != entry.PremiereDate {
						invalid[prefix+".PremiereDate"] = "Supply YYYY-MM-DD, or omit the field when the date is unknown."
					}
				}
				pair := [2]int{entry.SeasonNumber, entry.EpisodeNumber}
				if keys[entry.Key] {
					invalid[prefix+".Key"] = "Each episode key must be unique within this roster."
				}
				if numbers[pair] {
					invalid[prefix+".EpisodeNumber"] = "Each season and episode number pair must be unique within this roster."
				}
				keys[entry.Key], numbers[pair] = true, true
				edit.Entries = append(edit.Entries, entry)
			}
		}
	}
	if len(invalid) != 0 {
		episodeRosterInputError(w, r, invalid)
		return library.EpisodeRosterEdit{}, false
	}
	return edit, true
}
