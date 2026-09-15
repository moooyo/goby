package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

const liveTVIdentifierBytes = 256

type liveTVProgramsQuery struct {
	UserID           string
	LibrarySeriesID  string
	HasAired         *bool
	SortBy           string
	ImageTypeLimit   *int32
	EnableImageTypes []string
	EnableUserData   *bool
	Fields           []string
	Limit            *int32
}

// This endpoint only needs catalog authorization, not local episode queries.
type liveTVProgramCatalog interface {
	GetItemFor(context.Context, library.Subject, string) (library.Item, error)
	ListUserLibrariesFor(context.Context, library.Subject) ([]library.Library, error)
}

func parseLiveTVProgramsQuery(r *http.Request) (liveTVProgramsQuery, error) {
	var query liveTVProgramsQuery
	values, err := embyBusinessQuery(r)
	if err != nil {
		return query, err
	}
	languageSeen := false
	for name, entries := range values {
		if len(entries) != 1 {
			return query, identity.ErrInvalidInput
		}
		value := entries[0]
		if strings.EqualFold(name, "X-Emby-Language") {
			// This observed locale hint is local to Programs. Do not broaden the
			// shared transport allowlist or turn a hint into user authority.
			if languageSeen || !liveTVScalar(value, true) {
				return query, identity.ErrInvalidInput
			}
			languageSeen = true
			continue
		}
		switch name {
		case "UserId", "LibrarySeriesId":
			if !liveTVScalar(value, true) {
				return query, identity.ErrInvalidInput
			}
			if name == "UserId" {
				query.UserID = value
			} else {
				query.LibrarySeriesID = value
			}
		case "HasAired", "EnableUserData":
			if value != "true" && value != "false" {
				return query, identity.ErrInvalidInput
			}
			parsed := value == "true"
			if name == "HasAired" {
				query.HasAired = &parsed
			} else {
				query.EnableUserData = &parsed
			}
		case "Limit", "ImageTypeLimit":
			parsed, err := strconv.ParseInt(value, 10, 32)
			if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != value {
				return query, identity.ErrInvalidInput
			}
			number := int32(parsed)
			if name == "Limit" {
				query.Limit = &number
			} else {
				query.ImageTypeLimit = &number
			}
		case "SortBy":
			if value != "StartDate" {
				return query, identity.ErrInvalidInput
			}
			query.SortBy = value
		case "EnableImageTypes":
			query.EnableImageTypes, err = liveTVCSV(value, "Primary", "Thumb", "Backdrop")
			if err != nil {
				return query, err
			}
		case "Fields":
			query.Fields, err = liveTVCSV(value, "PrimaryImageAspectRatio", "ChannelInfo")
			if err != nil {
				return query, err
			}
		default:
			return query, identity.ErrInvalidInput
		}
	}
	return query, nil
}

func liveTVScalar(value string, allowEmpty bool) bool {
	if len(value) > liveTVIdentifierBytes || !utf8.ValidString(value) || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	return value == "" && allowEmpty || strings.TrimSpace(value) != ""
}

func liveTVCSV(value string, allowed ...string) ([]string, error) {
	if !liveTVScalar(value, false) {
		return nil, identity.ErrInvalidInput
	}
	entries := strings.Split(value, ",")
	seen := make(map[string]bool, len(entries))
	for i, entry := range entries {
		entry = strings.TrimSpace(entry)
		known := false
		for _, candidate := range allowed {
			if entry == candidate {
				known = true
				break
			}
		}
		if !known || seen[entry] {
			return nil, identity.ErrInvalidInput
		}
		seen[entry] = true
		entries[i] = entry
	}
	return entries, nil
}

func (s *Server) embyLiveTVPrograms(w http.ResponseWriter, r *http.Request) {
	s.serveLiveTVPrograms(w, r, s.library)
}

func (s *Server) serveLiveTVPrograms(w http.ResponseWriter, r *http.Request, catalog liveTVProgramCatalog) {
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Body != nil && r.Body != http.NoBody {
		// The existing bounded request-body owner retires unread bodies.
		apiError(w, r, 400, "invalid_input", "Live TV program queries must not contain a request body.")
		return
	}
	query, err := parseLiveTVProgramsQuery(r)
	if err != nil {
		apiError(w, r, 400, "invalid_input", "Check the Live TV program query parameters.")
		return
	}
	userID, ok := s.itemUser(w, r)
	if !ok {
		return
	}
	subject := requestLibrarySubject(r, userID)
	if query.LibrarySeriesID != "" {
		series, err := catalog.GetItemFor(r.Context(), subject, query.LibrarySeriesID)
		if err != nil {
			s.libraryError(w, r, err)
			return
		}
		if series.Type != "Series" {
			apiError(w, r, 400, "invalid_input", "The item is not a television series.")
			return
		}
	} else if _, err := catalog.ListUserLibrariesFor(r.Context(), subject); err != nil {
		s.libraryError(w, r, err)
		return
	}
	// Goby has no EPG source. Local library entries establish visibility only;
	// they do not supply broadcast programs. Even Limit=0 requires this check.
	jsonResponse(w, 200, struct {
		Items            []any
		TotalRecordCount int
	}{Items: []any{}, TotalRecordCount: 0})
}
