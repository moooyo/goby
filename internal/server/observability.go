package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

const observabilityMaxJSONInteger int64 = 1<<53 - 1

func (s *Server) registerObservabilityRoutes(mux *http.ServeMux) {
	for _, route := range []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /admin/v1/activity", s.adminActivity},
		{"GET /admin/v1/logs", s.adminDiagnosticLogs},
		{"GET /admin/v1/logs/{name}/lines", s.adminDiagnosticLines},
		{"GET /admin/v1/logs/{name}/download", s.adminDiagnosticDownload},
	} {
		mux.HandleFunc(route.pattern, observabilityNoCache(s.requireAdmin(route.handler)))
	}
	s.registerEmbyObservabilityRoutes(mux)
}

func observabilityNoCache(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			// These GET/HEAD routes never consume a request body. Prevent HTTP/1
			// response flushing or post-handler cleanup from waiting for a client
			// that advertises a body and then never sends its remaining bytes.
			controller := http.NewResponseController(w)
			w.Header().Set("Connection", "close")
			_ = controller.SetReadDeadline(time.Now())
			defer func() {
				if r.Body != nil {
					_ = r.Body.Close()
				}
				_ = controller.SetReadDeadline(time.Time{})
			}()
		}
		next(w, r)
	}
}

// The activity query and its count share the transaction owned by the catalog.
// This adapter exposes only the authorization helper's query capability.
type observabilityOwnedAuthorization struct{ tx library.OwnedTx }

func (a observabilityOwnedAuthorization) QueryRow(_ context.Context, statement string, args ...any) pgx.Row {
	return a.tx.QueryRow(statement, args...)
}

func observabilityPrincipal(r *http.Request) identity.Principal {
	principal, _ := r.Context().Value(principalKey).(identity.Principal)
	return principal
}

// Filesystem work must never retain the catalog owner or actor row locks.
// Dedicated short transactions also let download revalidation honor cancellation
// while a catalog writer is holding the owner's independent transaction.
func (s *Server) checkObservabilityAdministrator(ctx context.Context, principal identity.Principal, audience identity.AdministratorAudience) error {
	if s.db == nil {
		return diagnostics.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return diagnostics.ErrUnavailable
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer done()
		_ = tx.Rollback(cleanup)
	}()
	if err := identity.CheckAdministrator(ctx, tx, principal, audience, true); err != nil {
		return err
	}
	if err := identity.CheckAdministrator(ctx, tx, principal, audience, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return diagnostics.ErrUnavailable
	}
	return nil
}

func observabilityQuery(r *http.Request, allowed ...string) (url.Values, error) {
	if len(r.URL.RawQuery) > 4096 || !utf8.ValidString(r.URL.RawQuery) {
		return nil, activity.ErrInvalidInput
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, activity.ErrInvalidInput
	}
	for name, entries := range values {
		found := false
		for _, field := range allowed {
			if name == field {
				found = true
				break
			}
		}
		if !found || len(entries) != 1 || !utf8.ValidString(entries[0]) || strings.IndexFunc(entries[0], unicode.IsControl) >= 0 {
			return nil, activity.ErrInvalidInput
		}
	}
	return values, nil
}

func observabilityPage(values url.Values, defaultLimit, maximum int) (int, int, error) {
	start, limit := 0, defaultLimit
	for _, field := range []string{"StartIndex", "Limit"} {
		if entries, present := values[field]; present {
			value, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil || value < 0 || strconv.FormatInt(value, 10) != entries[0] || field == "Limit" && (value < 1 || value > int64(maximum)) {
				return 0, 0, activity.ErrInvalidInput
			}
			if field == "StartIndex" {
				start = int(value)
			} else {
				limit = int(value)
			}
		}
	}
	return start, limit, nil
}

func activityQueryOptions(r *http.Request) (activity.QueryOptions, error) {
	values, err := observabilityQuery(r, "StartIndex", "Limit", "MinDate", "Severity", "Action", "ActorId")
	if err != nil {
		return activity.QueryOptions{}, err
	}
	start, limit, err := observabilityPage(values, activity.DefaultPageLimit, activity.MaxPageLimit)
	if err != nil {
		return activity.QueryOptions{}, err
	}
	options := activity.QueryOptions{StartIndex: start, Limit: limit, Severity: activity.Severity(values.Get("Severity")),
		Action: activity.Action(values.Get("Action")), ActorID: values.Get("ActorId")}
	for _, field := range []string{"Severity", "Action", "ActorId"} {
		if entries, present := values[field]; present && entries[0] == "" {
			return activity.QueryOptions{}, activity.ErrInvalidInput
		}
	}
	if entries, present := values["MinDate"]; present {
		date, err := time.Parse(time.RFC3339Nano, entries[0])
		if err != nil {
			return activity.QueryOptions{}, activity.ErrInvalidInput
		}
		options.MinDate = &date
	}
	return options, nil
}

type nativeActivityActor struct {
	Kind activity.ActorKind `json:"Kind"`
	ID   *string            `json:"Id"`
	Name *string            `json:"Name"`
}

type nativeActivityResource struct {
	Kind activity.ResourceKind `json:"Kind"`
	ID   string                `json:"Id"`
}

type nativeActivityEntry struct {
	ID                     string                 `json:"Id"`
	Date                   time.Time              `json:"Date"`
	Action                 activity.Action        `json:"Action"`
	Severity               activity.Severity      `json:"Severity"`
	Source                 activity.Source        `json:"Source"`
	Actor                  nativeActivityActor    `json:"Actor"`
	Resource               nativeActivityResource `json:"Resource"`
	Revision               *string                `json:"Revision"`
	PreviousRevision       *string                `json:"PreviousRevision,omitempty"`
	ObservationFingerprint string                 `json:"ObservationFingerprint,omitempty"`
	Count                  string                 `json:"Count"`
	State                  *activity.State        `json:"State"`
	ChangedFields          []activity.Field       `json:"ChangedFields"`
	Name                   string                 `json:"Name"`
	Overview               string                 `json:"Overview"`
}

type nativeActivityPage struct {
	Items            []nativeActivityEntry `json:"Items"`
	TotalRecordCount int64                 `json:"TotalRecordCount"`
	StartIndex       int                   `json:"StartIndex"`
	Limit            int                   `json:"Limit"`
	RetentionDays    int                   `json:"RetentionDays"`
}

func nativeActivityDTO(page activity.Page, retention int) (nativeActivityPage, error) {
	if page.TotalRecordCount < 0 || page.TotalRecordCount > observabilityMaxJSONInteger {
		return nativeActivityPage{}, activity.ErrUnavailable
	}
	if retention == 0 {
		retention = int(activity.DefaultRetention / (24 * time.Hour))
	}
	result := nativeActivityPage{Items: make([]nativeActivityEntry, 0, len(page.Items)), TotalRecordCount: page.TotalRecordCount,
		StartIndex: page.StartIndex, Limit: page.Limit, RetentionDays: retention}
	for _, entry := range page.Items {
		if entry.ID < 1 || entry.Count < 0 || entry.Revision < 0 || entry.PreviousRevision < 0 || !nativeActivityRootBindingFactsValid(entry) {
			return nativeActivityPage{}, activity.ErrUnavailable
		}
		item := nativeActivityEntry{ID: strconv.FormatInt(entry.ID, 10), Date: entry.Date.UTC(), Action: entry.Action, Severity: entry.Severity,
			Source: entry.Source, Actor: nativeActivityActor{Kind: entry.Actor.Kind}, Resource: nativeActivityResource{Kind: entry.Resource.Kind, ID: entry.Resource.ID},
			Count: strconv.FormatInt(entry.Count, 10), ChangedFields: append([]activity.Field{}, entry.ChangedFields...), Name: entry.Name, Overview: entry.Overview,
			ObservationFingerprint: entry.ObservationFingerprint}
		if entry.Actor.ID != "" {
			value := entry.Actor.ID
			item.Actor.ID = &value
		}
		if entry.Actor.Kind == activity.ActorUser && entry.ActorName != "" {
			value := entry.ActorName
			item.Actor.Name = &value
		}
		if entry.Revision > 0 {
			value := strconv.FormatInt(entry.Revision, 10)
			item.Revision = &value
		}
		if entry.PreviousRevision > 0 {
			value := strconv.FormatInt(entry.PreviousRevision, 10)
			item.PreviousRevision = &value
		}
		if entry.State != "" {
			value := entry.State
			item.State = &value
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

func nativeActivityRootBindingFactsValid(entry activity.Entry) bool {
	if entry.Action != activity.ActionLibraryRootBindingUpdated {
		return entry.PreviousRevision == 0 && entry.ObservationFingerprint == ""
	}
	if entry.Source != activity.SourceNative || entry.Actor.Kind != activity.ActorUser || entry.Resource.Kind != activity.ResourceLibraryRoot ||
		entry.PreviousRevision < 1 || entry.PreviousRevision == 1<<63-1 || entry.Revision != entry.PreviousRevision+1 ||
		entry.Count != 0 || entry.State != "" || len(entry.ChangedFields) != 0 || len(entry.ObservationFingerprint) != 64 {
		return false
	}
	return strings.IndexFunc(entry.ObservationFingerprint, func(character rune) bool {
		return !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f')
	}) == -1
}

func (s *Server) adminActivity(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	var result nativeActivityPage
	err := s.library.WithOwnedTx(r.Context(), func(tx library.OwnedTx) error {
		adapter := observabilityOwnedAuthorization{tx: tx}
		if err := identity.CheckAdministrator(context.Background(), adapter, principal, identity.AdministratorNative, true); err != nil {
			return err
		}
		options, err := activityQueryOptions(r)
		if err != nil {
			return err
		}
		page, err := activity.QueryOwned(func(statement string, args ...any) activity.Row { return tx.QueryRow(statement, args...) }, options)
		if err != nil {
			if errors.Is(err, activity.ErrInvalidInput) {
				return err
			}
			return errors.Join(activity.ErrUnavailable, err)
		}
		result, err = nativeActivityDTO(page, s.cfg.ActivityRetentionDays)
		if err != nil {
			return err
		}
		return identity.CheckAdministrator(context.Background(), adapter, principal, identity.AdministratorNative, false)
	})
	if err != nil {
		s.observabilityError(w, r, err, false)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}

type nativeDiagnosticFile struct {
	Name         string    `json:"Name"`
	DateCreated  time.Time `json:"DateCreated"`
	DateModified time.Time `json:"DateModified"`
	Size         string    `json:"Size"`
}

type nativeDiagnosticStatus struct {
	Healthy       bool   `json:"Healthy"`
	Degraded      bool   `json:"Degraded"`
	Closed        bool   `json:"Closed"`
	MaxFileBytes  string `json:"MaxFileBytes"`
	MaxFiles      int    `json:"MaxFiles"`
	RetentionDays int    `json:"RetentionDays"`
	MinFreeBytes  string `json:"MinFreeBytes"`
	Format        string `json:"Format"`
}

type nativeDiagnosticPage struct {
	Items            []nativeDiagnosticFile `json:"Items"`
	TotalRecordCount int                    `json:"TotalRecordCount"`
	StartIndex       int                    `json:"StartIndex"`
	Limit            int                    `json:"Limit"`
	Status           nativeDiagnosticStatus `json:"Status"`
}

func (s *Server) adminDiagnosticLogs(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorNative); err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	values, err := observabilityQuery(r, "StartIndex", "Limit")
	if err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	start, limit, err := observabilityPage(values, 50, 200)
	if err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if s.diagnostics == nil {
		s.observabilityError(w, r, diagnostics.ErrUnavailable, true)
		return
	}
	page, readErr := s.diagnostics.List(r.Context(), diagnostics.ListOptions{StartIndex: start, Limit: limit})
	status := s.diagnostics.Status()
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorNative); err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if readErr != nil {
		s.observabilityError(w, r, readErr, true)
		return
	}
	result := nativeDiagnosticPage{Items: make([]nativeDiagnosticFile, 0, len(page.Items)), TotalRecordCount: page.TotalRecordCount, StartIndex: start, Limit: limit,
		Status: nativeDiagnosticStatus{Healthy: status.Healthy, Degraded: status.Degraded, Closed: status.Closed, MaxFileBytes: strconv.FormatInt(status.MaxFileBytes, 10),
			MaxFiles: status.MaxFiles, RetentionDays: status.RetentionDays, MinFreeBytes: strconv.FormatInt(status.MinFreeBytes, 10), Format: status.Format}}
	for _, file := range page.Items {
		result.Items = append(result.Items, nativeDiagnosticFile{Name: file.Name, DateCreated: file.DateCreated.UTC(), DateModified: file.DateModified.UTC(), Size: strconv.FormatInt(file.Size, 10)})
	}
	jsonResponse(w, http.StatusOK, result)
}

func diagnosticRequestName(r *http.Request) (string, error) {
	name := r.PathValue("name")
	if name == "" || len(name) > 160 || name == "." || name == ".." || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "/\\\x00") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", diagnostics.ErrInvalid
	}
	return name, nil
}

func (s *Server) adminDiagnosticLines(w http.ResponseWriter, r *http.Request) {
	principal := observabilityPrincipal(r)
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorNative); err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	name, err := diagnosticRequestName(r)
	if err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	values, err := observabilityQuery(r, "StartIndex", "Limit")
	if err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	start, limit, err := observabilityPage(values, 200, 500)
	if err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if s.diagnostics == nil {
		s.observabilityError(w, r, diagnostics.ErrUnavailable, true)
		return
	}
	page, readErr := s.diagnostics.Lines(r.Context(), name, diagnostics.LinesOptions{StartIndex: start, Limit: limit})
	if err := s.checkObservabilityAdministrator(r.Context(), principal, identity.AdministratorNative); err != nil {
		s.observabilityError(w, r, err, true)
		return
	}
	if readErr != nil {
		s.observabilityError(w, r, readErr, true)
		return
	}
	jsonResponse(w, http.StatusOK, struct {
		Items            []string `json:"Items"`
		StartIndex       int      `json:"StartIndex"`
		NextIndex        int      `json:"NextIndex"`
		TotalRecordCount int      `json:"TotalRecordCount"`
		SnapshotSize     string   `json:"SnapshotSize"`
	}{page.Items, page.StartIndex, page.NextIndex, page.TotalRecordCount, strconv.FormatInt(page.SnapshotSize, 10)})
}

func (s *Server) observabilityError(w http.ResponseWriter, r *http.Request, err error, diagnostic bool) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		s.identityError(w, r, err)
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
	case errors.Is(err, activity.ErrInvalidInput), errors.Is(err, diagnostics.ErrInvalid):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Supply valid activity or diagnostic query fields.")
	case errors.Is(err, diagnostics.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The requested diagnostic file was not found.")
	case errors.Is(err, activity.ErrUnavailable), errors.Is(err, diagnostics.ErrUnavailable), errors.Is(err, diagnostics.ErrBusy), errors.Is(err, library.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		if diagnostic {
			apiError(w, r, http.StatusServiceUnavailable, "diagnostics_unavailable", "Diagnostic logs are currently unavailable.")
		} else {
			apiError(w, r, http.StatusServiceUnavailable, "activity_unavailable", "Activity history is currently unavailable.")
		}
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		apiError(w, r, http.StatusInternalServerError, "internal_error", "The activity or diagnostic request could not be completed.")
	}
}
