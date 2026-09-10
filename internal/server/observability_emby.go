package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

const embyObservabilityDateFormat = "2006-01-02T15:04:05.0000000Z"

func (s *Server) registerEmbyObservabilityRoutes(mux *http.ServeMux) {
	for _, route := range []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /emby/System/ActivityLog/Entries", s.embyActivity},
		{"GET /emby/System/Logs/Query", s.embyDiagnosticLogs},
		{"GET /emby/System/Logs/{name}/Lines", s.embyDiagnosticLines},
		{"GET /emby/System/Logs/{name}", s.embyDiagnosticDownload},
	} {
		authenticated := s.requireEmby(route.handler)
		mux.HandleFunc(route.pattern, observabilityNoCache(func(w http.ResponseWriter, r *http.Request) {
			// These are GET adapters, not an implicit promise of HEAD support.
			// The captured log HEAD is 404 for both administrators and anonymous
			// callers. Its unavailable body is replaced with a fixed safe message.
			if r.Method == http.MethodHead {
				embyTextError(w, r, http.StatusNotFound, "The requested operation was not found.")
				return
			}
			authenticated(w, r)
		}))
	}
}

type embyObservabilityPageOptions struct {
	StartIndex   int
	Limit        int
	LimitPresent bool
}

func embyObservabilityQuery(r *http.Request, allowed ...string) (url.Values, error) {
	// The established Emby authentication query transport is not a paging or
	// file-selection parameter. It remains single-valued and byte bounded.
	return observabilityQuery(r, append(allowed, "api_key")...)
}

func embyObservabilityPage(values url.Values, defaultLimit, maximum int, nonpositiveEmpty bool) (embyObservabilityPageOptions, error) {
	page := embyObservabilityPageOptions{Limit: defaultLimit}
	for _, field := range []string{"StartIndex", "Limit"} {
		entries, present := values[field]
		if !present {
			continue
		}
		value, err := strconv.ParseInt(entries[0], 10, 32)
		if err != nil || strconv.FormatInt(value, 10) != entries[0] || field == "Limit" && value > int64(maximum) || value < 0 && !nonpositiveEmpty {
			return embyObservabilityPageOptions{}, activity.ErrInvalidInput
		}
		if field == "StartIndex" {
			page.StartIndex = max(0, int(value))
		} else {
			page.Limit, page.LimitPresent = max(0, int(value)), true
		}
	}
	return page, nil
}

type embyObservabilityResult[T any] struct {
	Items            []T   `json:"Items"`
	TotalRecordCount int64 `json:"TotalRecordCount"`
}

type embyActivityEntry struct {
	ID       int64             `json:"Id"`
	Name     string            `json:"Name"`
	Overview string            `json:"Overview"`
	Type     string            `json:"Type"`
	Date     string            `json:"Date"`
	UserID   string            `json:"UserId,omitempty"`
	Severity activity.Severity `json:"Severity"`
}

func embyActivityDTO(page activity.Page, options embyObservabilityPageOptions) (embyObservabilityResult[embyActivityEntry], error) {
	result := embyObservabilityResult[embyActivityEntry]{Items: []embyActivityEntry{}}
	if page.TotalRecordCount < 0 {
		return result, activity.ErrUnavailable
	}
	// These count rules describe the captured 4.9.5.0 cases: an omitted Limit
	// and an offset outside the match set report zero; an explicit zero Limit
	// still reports the matching total when the requested offset exists.
	if options.LimitPresent && int64(options.StartIndex) < page.TotalRecordCount {
		result.TotalRecordCount = page.TotalRecordCount
	}
	if options.Limit == 0 {
		return result, nil
	}
	for _, entry := range page.Items {
		if entry.ID < 1 {
			return embyObservabilityResult[embyActivityEntry]{}, activity.ErrUnavailable
		}
		item := embyActivityEntry{ID: entry.ID, Name: entry.Name, Overview: entry.Overview, Type: string(entry.Action),
			Date: entry.Date.UTC().Format(embyObservabilityDateFormat), Severity: entry.Severity}
		switch entry.Action {
		case activity.ActionSessionLogin:
			item.Type = "user.authenticated"
		case activity.ActionUserPasswordReset:
			item.Type = "user.passwordchanged"
		}
		// The reference's UserId is an internal numeric identity, not its public
		// Users DTO GUID. Goby uses its actual associated user ID, without making
		// up an integer identity for an application key or a system event.
		if entry.Resource.Kind == activity.ResourceUser {
			item.UserID = entry.Resource.ID
		} else if entry.Actor.Kind == activity.ActorUser {
			item.UserID = entry.Actor.ID
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func (s *Server) embyActivity(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	var result embyObservabilityResult[embyActivityEntry]
	err := s.library.WithOwnedTx(r.Context(), func(tx library.OwnedTx) error {
		adapter := observabilityOwnedAuthorization{tx: tx}
		if err := identity.CheckAdministrator(context.Background(), adapter, principal, identity.AdministratorEmby, true); err != nil {
			return err
		}
		values, err := embyObservabilityQuery(r, "StartIndex", "Limit", "MinDate")
		if err != nil {
			return err
		}
		options, err := embyObservabilityPage(values, activity.MaxPageLimit, activity.MaxPageLimit, true)
		if err != nil {
			return err
		}
		query := activity.QueryOptions{StartIndex: options.StartIndex, Limit: max(1, options.Limit)}
		if entries, present := values["MinDate"]; present {
			date, err := time.Parse(time.RFC3339Nano, entries[0])
			if err != nil {
				return activity.ErrInvalidInput
			}
			query.MinDate = &date
		}
		page, err := activity.QueryOwned(func(statement string, args ...any) activity.Row { return tx.QueryRow(statement, args...) }, query)
		if err != nil {
			if errors.Is(err, activity.ErrInvalidInput) {
				return err
			}
			return errors.Join(activity.ErrUnavailable, err)
		}
		result, err = embyActivityDTO(page, options)
		if err != nil {
			return err
		}
		return identity.CheckAdministrator(context.Background(), adapter, principal, identity.AdministratorEmby, false)
	})
	if err != nil {
		s.embyObservabilityError(w, r, err, false)
		return
	}
	embyObservabilityJSON(w, r, result)
}

type embyDiagnosticFile struct {
	DateCreated  string `json:"DateCreated"`
	DateModified string `json:"DateModified"`
	Size         int64  `json:"Size"`
	Name         string `json:"Name"`
}

func (s *Server) embyDiagnosticLogs(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorEmby); err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	values, err := embyObservabilityQuery(r, "StartIndex", "Limit")
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	options, err := embyObservabilityPage(values, 200, 200, true)
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	if s.diagnostics == nil {
		s.embyObservabilityError(w, r, diagnostics.ErrUnavailable, true)
		return
	}
	page, readErr := s.diagnostics.List(r.Context(), diagnostics.ListOptions{StartIndex: options.StartIndex, Limit: max(1, options.Limit)})
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorEmby); err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	if readErr != nil {
		s.embyObservabilityError(w, r, readErr, true)
		return
	}
	result := embyObservabilityResult[embyDiagnosticFile]{Items: []embyDiagnosticFile{}, TotalRecordCount: int64(page.TotalRecordCount)}
	if options.Limit > 0 {
		for _, file := range page.Items {
			result.Items = append(result.Items, embyDiagnosticFile{DateCreated: file.DateCreated.UTC().Format(embyObservabilityDateFormat),
				DateModified: file.DateModified.UTC().Format(embyObservabilityDateFormat), Size: file.Size, Name: file.Name})
		}
	}
	embyObservabilityJSON(w, r, result)
}

func (s *Server) embyDiagnosticLines(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorEmby); err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	name, err := diagnosticRequestName(r)
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	values, err := embyObservabilityQuery(r, "StartIndex", "Limit")
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	options, err := embyObservabilityPage(values, 0, 500, false)
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	if s.diagnostics == nil {
		s.embyObservabilityError(w, r, diagnostics.ErrUnavailable, true)
		return
	}
	page, readErr := s.diagnostics.Lines(r.Context(), name, diagnostics.LinesOptions{StartIndex: options.StartIndex, Limit: max(1, options.Limit)})
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorEmby); err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	if readErr != nil {
		s.embyObservabilityError(w, r, readErr, true)
		return
	}
	result := embyObservabilityResult[string]{Items: []string{}, TotalRecordCount: int64(page.TotalRecordCount)}
	if options.Limit > 0 {
		result.Items = page.Items
	}
	embyObservabilityJSON(w, r, result)
}

func (s *Server) embyDiagnosticDownload(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorEmby); err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	name, err := diagnosticRequestName(r)
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	values, err := embyObservabilityQuery(r, "Sanitize")
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	if entries, present := values["Sanitize"]; present && entries[0] != "true" && entries[0] != "false" {
		s.embyObservabilityError(w, r, diagnostics.ErrInvalid, true)
		return
	}
	if len(r.Header.Values("Range")) > 1 || len(r.Header.Get("Range")) > 4096 {
		s.embyObservabilityError(w, r, diagnostics.ErrInvalid, true)
		return
	}
	if s.diagnostics == nil {
		s.embyObservabilityError(w, r, diagnostics.ErrUnavailable, true)
		return
	}
	deadline := time.Now().Add(diagnosticDownloadLifetime)
	if !principal.ExpiresAt.IsZero() && principal.ExpiresAt.Before(deadline) {
		deadline = principal.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	snapshot, readErr := s.diagnostics.Snapshot(ctx, name)
	if snapshot != nil {
		defer snapshot.Close()
	}
	if err := s.checkObservabilityAdministrator(ctx, principal, identity.AdministratorEmby); err != nil {
		s.embyObservabilityError(w, r, err, true)
		return
	}
	if readErr != nil {
		s.embyObservabilityError(w, r, readErr, true)
		return
	}
	// Sanitize is a compatibility input, never permission to retrieve secrets.
	// Every mode serves the same already sanitized registered JSONL snapshot.
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	err = serveDiagnosticSnapshot(w, r.WithContext(ctx), snapshot, func(ctx context.Context) error {
		return s.checkObservabilityAdministrator(ctx, principal, identity.AdministratorEmby)
	})
	if err != nil {
		s.embyObservabilityError(w, r, err, true)
	}
}

func embyObservabilityJSON(w http.ResponseWriter, r *http.Request, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		embyTextError(w, r, http.StatusInternalServerError, "The activity or diagnostic response could not be represented.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) embyObservabilityError(w http.ResponseWriter, r *http.Request, err error, diagnostic bool) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		actor := observabilityPrincipal(r)
		embyTextError(w, r, http.StatusForbidden, fmt.Sprintf("User %s does not have access to ManageServer feature.", actor.User.Name))
	case errors.Is(err, activity.ErrInvalidInput), errors.Is(err, diagnostics.ErrInvalid):
		embyTextError(w, r, http.StatusBadRequest, "Supply valid activity or diagnostic query fields.")
	case errors.Is(err, diagnostics.ErrNotFound):
		embyTextError(w, r, http.StatusNotFound, "The requested diagnostic file was not found.")
	case errors.Is(err, activity.ErrUnavailable), errors.Is(err, diagnostics.ErrUnavailable), errors.Is(err, diagnostics.ErrBusy), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		message := "Activity history is currently unavailable."
		if diagnostic {
			message = "Diagnostic logs are currently unavailable."
		}
		embyTextError(w, r, http.StatusServiceUnavailable, message)
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		embyTextError(w, r, http.StatusInternalServerError, "The activity or diagnostic request could not be completed.")
	}
}
