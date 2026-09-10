package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
)

const (
	adminBackupJSONLimit        = 16 * 1024
	adminBackupJSONTimeout      = 15 * time.Second
	adminBackupUploadIdle       = 30 * time.Second
	adminBackupDownloadLifetime = 30 * time.Minute
)

// The manager owns admission, audit records, jobs and their lifetime. The HTTP
// adapter owns only request bodies and the pinned descriptor used by a download.
type adminRecoveryManager interface {
	Status(context.Context, identity.Principal) (recovery.StatusView, error)
	ListBackups(context.Context, identity.Principal, int, int) (recovery.BackupPage, error)
	Backup(context.Context, identity.Principal, string) (recovery.BackupView, error)
	ListOperations(context.Context, identity.Principal, int, int) (recovery.OperationPage, error)
	Operation(context.Context, identity.Principal, string) (recovery.OperationView, error)
	Create(context.Context, identity.Principal, recovery.CreateRequest) (recovery.OperationView, error)
	Import(context.Context, identity.Principal, string, io.ReadCloser) (recovery.OperationView, error)
	Delete(context.Context, identity.Principal, string, recovery.DeleteRequest) (recovery.OperationView, error)
	Cancel(context.Context, identity.Principal, string, string) (recovery.OperationView, error)
	Plan(context.Context, identity.Principal, recovery.PlanRequest) (recovery.OperationView, error)
	Apply(context.Context, identity.Principal, string, recovery.ApplyRequest) (recovery.OperationView, error)
	Rollback(context.Context, identity.Principal, recovery.RollbackRequest) (recovery.OperationView, error)
	Download(context.Context, identity.Principal, string) (*backupstore.Snapshot, error)
	Revalidate(context.Context, identity.Principal) error
}

func WithRecovery(manager *recovery.Manager) Option {
	return func(server *Server) {
		if manager != nil {
			server.recovery = manager
		}
	}
}

func (s *Server) registerAdminBackupRoutes(mux *http.ServeMux) {
	for _, route := range []struct{ pattern, action string }{
		{"GET /admin/v1/backups/status", "status"},
		{"GET /admin/v1/backups", "backups"},
		{"GET /admin/v1/backups/{id}", "backup"},
		{"POST /admin/v1/backups", "create"},
		{"POST /admin/v1/backups/import", "import"},
		{"GET /admin/v1/backups/{id}/file", "file"},
		{"DELETE /admin/v1/backups/{id}", "delete"},
		{"GET /admin/v1/backup-operations", "operations"},
		{"GET /admin/v1/backup-operations/{id}", "operation"},
		{"POST /admin/v1/backup-operations/{id}/cancel", "cancel"},
		{"POST /admin/v1/restores/plans", "plan"},
		{"POST /admin/v1/restores/{id}/apply", "apply"},
		{"POST /admin/v1/restores/rollback", "rollback"},
	} {
		action := route.action
		mux.HandleFunc(route.pattern, adminBackupNoCache(s.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
			s.adminBackupRequest(w, r, action)
		})))
	}
}

func adminBackupNoCache(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		if r.Body != nil && r.Body != http.NoBody {
			// Share one body/deadline owner across authentication's request
			// clones without replacing net/http's original request body.
			body := &adminBackupRequestBody{body: r.Body, controller: http.NewResponseController(w)}
			r = r.WithContext(r.Context())
			r.Body = body
			w = &adminBackupBodyResponse{ResponseWriter: w, body: body}
			defer body.Close()
		}
		next(w, r)
	}
}

type adminBackupBodyResponse struct {
	http.ResponseWriter
	body *adminBackupRequestBody
}

func (w *adminBackupBodyResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *adminBackupBodyResponse) WriteHeader(status int) {
	if status >= 200 && !w.body.complete() {
		// Reject unread bodies without letting net/http drain a stalled upload
		// or reuse the connection whose read deadline must be interrupted.
		w.Header().Set("Connection", "close")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *adminBackupBodyResponse) Write(data []byte) (int, error) {
	if !w.body.complete() {
		w.Header().Set("Connection", "close")
	}
	return w.ResponseWriter.Write(data)
}

func adminBackupHeaders(r *http.Request) bool {
	seen := make(map[string]bool, len(r.Header))
	for key, values := range r.Header {
		key = strings.ToLower(key)
		if seen[key] || len(values) != 1 || key == "content-encoding" {
			return false
		}
		seen[key] = true
	}
	return len(r.Header.Get("Range")) <= 4096
}

func adminBackupHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func adminBackupPagination(r *http.Request) (start, limit int, err error) {
	limit = 25
	if len(r.URL.RawQuery) > 4096 || r.URL.ForceQuery && r.URL.RawQuery == "" {
		return 0, 0, recovery.ErrInvalid
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, 0, recovery.ErrInvalid
	}
	for key, entries := range values {
		if (key != "StartIndex" && key != "Limit") || len(entries) != 1 {
			return 0, 0, recovery.ErrInvalid
		}
		value, parseErr := strconv.ParseInt(entries[0], 10, 32)
		if parseErr != nil || value < 0 || strconv.FormatInt(value, 10) != entries[0] || key == "Limit" && (value < 1 || value > 100) {
			return 0, 0, recovery.ErrInvalid
		}
		if key == "StartIndex" {
			start = int(value)
		} else {
			limit = int(value)
		}
	}
	return start, limit, nil
}

func (s *Server) adminBackupRequest(w http.ResponseWriter, r *http.Request, action string) {
	actor, _ := r.Context().Value(principalKey).(identity.Principal)
	if !adminBackupHeaders(r) {
		s.adminBackupError(w, r, recovery.ErrInvalid)
		return
	}
	if s.recovery == nil {
		s.adminBackupError(w, r, recovery.ErrUnavailable)
		return
	}
	if action != "backups" && action != "operations" && (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		s.adminBackupError(w, r, recovery.ErrInvalid)
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			s.adminBackupError(w, r, recovery.ErrInvalid)
			return
		}
	}
	id := r.PathValue("id")
	if id != "" && !adminBackupHex(id, 32) {
		s.adminBackupError(w, r, recovery.ErrInvalid)
		return
	}
	var result any
	var err error
	status := http.StatusOK
	switch action {
	case "status":
		result, err = s.recovery.Status(r.Context(), actor)
	case "backups", "operations":
		var start, limit int
		start, limit, err = adminBackupPagination(r)
		if err == nil {
			if action == "backups" {
				result, err = s.recovery.ListBackups(r.Context(), actor, start, limit)
			} else {
				result, err = s.recovery.ListOperations(r.Context(), actor, start, limit)
			}
		}
	case "backup":
		var view recovery.BackupView
		view, err = s.recovery.Backup(r.Context(), actor, id)
		result = map[string]any{"Backup": view}
	case "operation":
		var view recovery.OperationView
		view, err = s.recovery.Operation(r.Context(), actor, id)
		result = map[string]any{"Operation": view}
	case "file":
		s.adminBackupDownload(w, r, actor, id)
		return
	case "import":
		s.adminBackupImport(w, r, actor)
		return
	default:
		status = http.StatusAccepted
		var view recovery.OperationView
		view, err = s.adminBackupMutation(w, r, actor, id, action)
		result = map[string]any{"Operation": view}
	}
	if err != nil {
		s.adminBackupError(w, r, err)
		return
	}
	jsonResponse(w, status, result)
}

func adminBackupContentType(r *http.Request, binary bool) bool {
	if len(r.Header.Values("Content-Type")) != 1 {
		return false
	}
	raw := r.Header.Get("Content-Type")
	mediaType, parameters, err := mime.ParseMediaType(raw)
	if err != nil {
		return false
	}
	if binary {
		return mediaType == "application/octet-stream" && len(parameters) == 0
	}
	if mediaType != "application/json" || len(parameters) > 1 || strings.Count(raw, ";") > 1 {
		return false
	}
	if len(parameters) == 0 {
		return !strings.Contains(raw, ";")
	}
	_, parameter, _ := strings.Cut(raw, ";")
	name, _, exists := strings.Cut(parameter, "=")
	return exists && strings.EqualFold(strings.TrimSpace(name), "charset") && strings.EqualFold(parameters["charset"], "utf-8")
}

var errAdminBackupMediaType = errors.New("unsupported backup request media type")
var errAdminBackupPayloadLarge = fmt.Errorf("backup request body exceeds its HTTP limit: %w", recovery.ErrCapacity)

func adminBackupObject(w http.ResponseWriter, r *http.Request, fields []string) (map[string]json.RawMessage, error) {
	if !adminBackupContentType(r, false) {
		return nil, errAdminBackupMediaType
	}
	body, finish, err := newAdminBackupBody(w, r, adminBackupJSONLimit, adminBackupJSONTimeout)
	if err != nil {
		return nil, err
	}
	defer finish()
	data, err := io.ReadAll(body)
	defer clear(data)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) || !adminSettingsUnicode(data) {
		return nil, recovery.ErrInvalid
	}
	// The shared object primitive checks exact field names, duplicates, required
	// fields and trailing input. Its settings/task error text is never returned.
	values, invalid := adminTaskObject(data, fields, fields, "")
	if invalid != nil {
		clearAdminBackupObject(values)
		return nil, recovery.ErrInvalid
	}
	return values, nil
}

func clearAdminBackupObject(values map[string]json.RawMessage) {
	for _, value := range values {
		clear(value)
	}
}

func adminBackupString(raw json.RawMessage) (string, bool) {
	var value string
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func adminBackupRevision(raw json.RawMessage, positive bool) (string, bool) {
	value, ok := adminBackupString(raw)
	parsed, err := strconv.ParseUint(value, 10, 64)
	return value, ok && err == nil && len(value) <= 20 && strconv.FormatUint(parsed, 10) == value && (!positive || parsed > 0)
}

// Decode directly into the caller-owned byte slice so no additional plaintext
// Go string is created for the passphrase. Lossless Unicode was checked before
// this function, and every failure clears the partial decoded secret.
func adminBackupPassphrase(raw json.RawMessage) (phrase []byte, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, false
	}
	phrase = make([]byte, 0, 1024+utf8.UTFMax)
	defer func() {
		if !ok {
			clear(phrase)
			phrase = nil
		}
	}()
	unit := func(data []byte) uint16 {
		var value uint16
		for _, digit := range data {
			value <<= 4
			switch {
			case digit >= '0' && digit <= '9':
				value += uint16(digit - '0')
			case digit >= 'a' && digit <= 'f':
				value += uint16(digit - 'a' + 10)
			default:
				value += uint16(digit - 'A' + 10)
			}
		}
		return value
	}
	for index := 1; index < len(raw)-1; index++ {
		if raw[index] != '\\' {
			phrase = append(phrase, raw[index])
			if len(phrase) > 1024 {
				return phrase, false
			}
			continue
		}
		index++
		switch raw[index] {
		case '"', '\\', '/':
			phrase = append(phrase, raw[index])
		case 'b':
			phrase = append(phrase, '\b')
		case 'f':
			phrase = append(phrase, '\f')
		case 'n':
			phrase = append(phrase, '\n')
		case 'r':
			phrase = append(phrase, '\r')
		case 't':
			phrase = append(phrase, '\t')
		case 'u':
			character := rune(unit(raw[index+1 : index+5]))
			index += 4
			if character >= 0xd800 && character <= 0xdbff {
				character = utf16.DecodeRune(character, rune(unit(raw[index+3:index+7])))
				index += 6
			}
			phrase = utf8.AppendRune(phrase, character)
		}
		if len(phrase) > 1024 {
			return phrase, false
		}
	}
	return phrase, len(phrase) >= recovery.MinPassphraseBytes && len(phrase) <= 1024 && utf8.Valid(phrase)
}

func (s *Server) adminBackupMutation(w http.ResponseWriter, r *http.Request, actor identity.Principal, id, action string) (recovery.OperationView, error) {
	fields := map[string][]string{
		"create": {"RequestId", "Passphrase"}, "delete": {"RequestId", "SHA256"}, "cancel": {"Revision"},
		"plan":  {"RequestId", "BackupId", "SHA256", "Passphrase", "RestoreDefaults", "ReplaceRollback", "GenerationRevision"},
		"apply": {"Revision", "GenerationRevision"}, "rollback": {"RequestId", "GenerationRevision"},
	}[action]
	if fields == nil {
		return recovery.OperationView{}, recovery.ErrInvalid
	}
	values, err := adminBackupObject(w, r, fields)
	if err != nil {
		return recovery.OperationView{}, err
	}
	defer clearAdminBackupObject(values)
	text := make(map[string]string)
	for _, field := range fields {
		if field == "Passphrase" || field == "RestoreDefaults" || field == "ReplaceRollback" {
			continue
		}
		value, ok := adminBackupString(values[field])
		if !ok {
			return recovery.OperationView{}, recovery.ErrInvalid
		}
		switch field {
		case "RequestId", "BackupId":
			ok = adminBackupHex(value, 32)
		case "SHA256":
			// Failed or interrupted objects may have no published digest. The
			// manager still compares this explicit empty value with the object.
			ok = adminBackupHex(value, 64) || action == "delete" && value == ""
		case "Revision", "GenerationRevision":
			_, ok = adminBackupRevision(values[field], field == "Revision")
		}
		if !ok {
			return recovery.OperationView{}, recovery.ErrInvalid
		}
		text[field] = value
	}
	var phrase []byte
	if raw, exists := values["Passphrase"]; exists {
		var ok bool
		phrase, ok = adminBackupPassphrase(raw)
		if !ok {
			return recovery.OperationView{}, recovery.ErrInvalid
		}
		defer clear(phrase)
	}
	switch action {
	case "create":
		return s.recovery.Create(r.Context(), actor, recovery.CreateRequest{RequestId: text["RequestId"], Passphrase: phrase})
	case "delete":
		return s.recovery.Delete(r.Context(), actor, id, recovery.DeleteRequest{RequestId: text["RequestId"], SHA256: text["SHA256"]})
	case "cancel":
		return s.recovery.Cancel(r.Context(), actor, id, text["Revision"])
	case "apply":
		return s.recovery.Apply(r.Context(), actor, id, recovery.ApplyRequest{Revision: text["Revision"], GenerationRevision: text["GenerationRevision"]})
	case "rollback":
		return s.recovery.Rollback(r.Context(), actor, recovery.RollbackRequest{RequestId: text["RequestId"], GenerationRevision: text["GenerationRevision"]})
	case "plan":
		var defaults, replace bool
		if bytes.Equal(bytes.TrimSpace(values["RestoreDefaults"]), []byte("null")) || bytes.Equal(bytes.TrimSpace(values["ReplaceRollback"]), []byte("null")) ||
			json.Unmarshal(values["RestoreDefaults"], &defaults) != nil || json.Unmarshal(values["ReplaceRollback"], &replace) != nil {
			return recovery.OperationView{}, recovery.ErrInvalid
		}
		return s.recovery.Plan(r.Context(), actor, recovery.PlanRequest{RequestId: text["RequestId"], BackupId: text["BackupId"],
			SHA256: text["SHA256"], Passphrase: phrase, RestoreDefaults: defaults, ReplaceRollback: replace, GenerationRevision: text["GenerationRevision"]})
	}
	return recovery.OperationView{}, recovery.ErrInvalid
}

func (s *Server) adminBackupImport(w http.ResponseWriter, r *http.Request, actor identity.Principal) {
	if !adminBackupContentType(r, true) {
		s.adminBackupError(w, r, errAdminBackupMediaType)
		return
	}
	request := r.Header.Get("X-Backup-Request-Id")
	if len(r.Header.Values("X-Backup-Request-Id")) != 1 || !adminBackupHex(request, 32) {
		s.adminBackupError(w, r, recovery.ErrInvalid)
		return
	}
	policy := s.cfg.Recovery.WithDefaults()
	body, finish, err := newAdminBackupBody(w, r, policy.Backups.MaxObjectBytes, policy.OperationTimeout)
	if err != nil {
		s.adminBackupError(w, r, err)
		return
	}
	defer finish()
	view, err := s.recovery.Import(body.ctx, actor, request, body)
	if err != nil && (errors.Is(body.err, errAdminBackupPayloadLarge) || errors.Is(body.err, context.DeadlineExceeded) || errors.Is(body.err, context.Canceled)) {
		err = body.err
	}
	// An idempotent admission may return without consuming another upload.
	// Retire that body before response flushing can try to drain it.
	finish()
	if err != nil {
		s.adminBackupError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]any{"Operation": view})
}

// Read consumes at most maximum+1 bytes, including chunked bodies. Before any
// blocking read it installs an idle deadline; cancellation advances that same
// deadline before Close, which would otherwise wait behind net/http's Read lock.
type adminBackupBody struct {
	ctx       context.Context
	cancel    context.CancelFunc
	body      *adminBackupRequestBody
	remaining int64
	err       error
}

// net/http starts a connection read as soon as the request body reaches EOF.
// Expiring its deadline after EOF cancels the connection context, including a
// still-running native operation and subsequent keep-alive requests. Only an
// incomplete body may use an expired deadline to interrupt its blocking Close.
type adminBackupRequestBody struct {
	body       io.ReadCloser
	controller *http.ResponseController
	mu         sync.Mutex
	closeOnce  sync.Once
	eof        bool
	closed     bool
	closeErr   error
}

func (b *adminBackupRequestBody) complete() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.eof
}

func (b *adminBackupRequestBody) Read(data []byte) (int, error) {
	n, err := b.body.Read(data)
	if errors.Is(err, io.EOF) {
		b.mu.Lock()
		b.eof = true
		if !b.closed {
			_ = b.controller.SetReadDeadline(time.Time{})
		}
		b.mu.Unlock()
	}
	return n, err
}

func (b *adminBackupRequestBody) arm(deadline time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.eof {
		return nil
	}
	if b.closed {
		return io.ErrClosedPipe
	}
	if err := b.controller.SetReadDeadline(deadline); err != nil {
		return recovery.ErrUnavailable
	}
	return nil
}

func (b *adminBackupRequestBody) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closed = true
		if !b.eof {
			_ = b.controller.SetReadDeadline(time.Now())
		}
		b.mu.Unlock()
		b.closeErr = b.body.Close()
		b.mu.Lock()
		_ = b.controller.SetReadDeadline(time.Time{})
		b.mu.Unlock()
	})
	return b.closeErr
}

func newAdminBackupBody(w http.ResponseWriter, r *http.Request, maximum int64, lifetime time.Duration) (*adminBackupBody, func(), error) {
	if maximum < 1 || maximum > 1<<40 || lifetime <= 0 || lifetime > 30*time.Minute {
		return nil, nil, recovery.ErrUnavailable
	}
	if r.ContentLength > maximum {
		return nil, nil, errAdminBackupPayloadLarge
	}
	owner, ok := r.Body.(*adminBackupRequestBody)
	if !ok {
		input := r.Body
		if input == nil {
			input = http.NoBody
		}
		owner = &adminBackupRequestBody{body: input, controller: http.NewResponseController(w)}
	}
	ctx, cancel := context.WithTimeout(r.Context(), lifetime)
	body := &adminBackupBody{ctx: ctx, cancel: cancel, body: owner, remaining: maximum}
	if err := body.arm(); err != nil {
		cancel()
		return nil, nil, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(done); body.interrupt() })
	var finishOnce sync.Once
	finish := func() {
		finishOnce.Do(func() {
			stopped := stop()
			cancel()
			if !stopped {
				<-done
			}
			body.interrupt()
		})
	}
	return body, finish, nil
}

func (b *adminBackupBody) arm() error {
	if err := b.ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(adminBackupUploadIdle)
	if bound, ok := b.ctx.Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	return b.body.arm(deadline)
}

func (b *adminBackupBody) interrupt() {
	_ = b.body.Close()
}

func (b *adminBackupBody) Close() error {
	if !b.body.complete() {
		b.cancel()
	}
	return b.body.Close()
}

func (b *adminBackupBody) Read(data []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if err := b.arm(); err != nil {
		b.err = err
		return 0, err
	}
	if int64(len(data)) > b.remaining+1 {
		data = data[:b.remaining+1]
	}
	n, err := b.body.Read(data)
	if int64(n) > b.remaining {
		b.err = errAdminBackupPayloadLarge
		return 0, b.err
	}
	b.remaining -= int64(n)
	if cause := b.ctx.Err(); cause != nil {
		b.err = cause
		return 0, cause
	}
	if err != nil && !errors.Is(err, io.EOF) {
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			err = context.DeadlineExceeded
		}
		b.err = err
	}
	return n, err
}

func (s *Server) adminBackupError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized), errors.Is(err, identity.ErrInvalidCredentials):
		apiError(w, r, http.StatusUnauthorized, "authentication_required", "Sign in as an administrator.")
	case errors.Is(err, identity.ErrClientSessionForbidden):
		apiError(w, r, http.StatusForbidden, "administrator_required", "Administrator access is required.")
	case errors.Is(err, errAdminBackupMediaType):
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use the documented UTF-8 JSON or binary backup content type.")
	case errors.Is(err, recovery.ErrInvalid), errors.Is(err, backupstore.ErrInvalid):
		apiError(w, r, http.StatusBadRequest, "invalid_input", "Check the backup request fields, headers, and query parameters.")
	case errors.Is(err, recovery.ErrArchive):
		apiError(w, r, http.StatusUnprocessableEntity, "invalid_archive", "The backup archive could not be authenticated or validated.")
	case errors.Is(err, recovery.ErrNotFound), errors.Is(err, backupstore.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The requested backup or recovery operation was not found.")
	case errors.Is(err, recovery.ErrConflict), errors.Is(err, backupstore.ErrConflict):
		apiError(w, r, http.StatusConflict, "conflict", "The backup or recovery state changed. Refresh it before trying again.")
	case errors.Is(err, recovery.ErrBusy), errors.Is(err, backupstore.ErrBusy):
		apiError(w, r, http.StatusConflict, "busy", "Another backup or recovery operation is active.")
	case errors.Is(err, backupstore.ErrNotReady):
		apiError(w, r, http.StatusConflict, "target_not_ready", "The requested backup is not ready for this operation.")
	case errors.Is(err, errAdminBackupPayloadLarge):
		apiError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "The backup request body exceeds the configured byte limit.")
	case errors.Is(err, recovery.ErrCapacity), errors.Is(err, backupstore.ErrQuota):
		apiError(w, r, http.StatusInsufficientStorage, "capacity_exceeded", "The backup request exceeds the configured capacity.")
	case errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusRequestTimeout, "request_timeout", "The backup request exceeded its allowed time.")
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		return
	default:
		// Private manager, SQL, storage and archive errors never enter logs or
		// the response. Availability uses a fixed native projection only.
		apiError(w, r, http.StatusServiceUnavailable, "backup_unavailable", "Backup and recovery are currently unavailable.")
	}
}

func (s *Server) adminBackupDownload(w http.ResponseWriter, r *http.Request, actor identity.Principal, id string) {
	if !s.sameOrigin(w, r) {
		return
	}
	deadline := time.Now().Add(adminBackupDownloadLifetime)
	if !actor.ExpiresAt.IsZero() && actor.ExpiresAt.Before(deadline) {
		deadline = actor.ExpiresAt
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	view, err := s.recovery.Backup(ctx, actor, id)
	if err != nil {
		s.adminBackupError(w, r, err)
		return
	}
	if view.State != "ready" {
		s.adminBackupError(w, r, backupstore.ErrNotReady)
		return
	}
	size, err := strconv.ParseInt(view.SizeBytes, 10, 64)
	if err != nil || size <= 0 || strconv.FormatInt(size, 10) != view.SizeBytes ||
		size > s.cfg.Recovery.WithDefaults().Backups.MaxObjectBytes || view.Id != id || !adminBackupHex(view.SHA256, 64) {
		s.adminBackupError(w, r, recovery.ErrUnavailable)
		return
	}
	snapshot, snapshotErr := s.recovery.Download(ctx, actor, id)
	if snapshot != nil {
		defer snapshot.Close()
	}
	if err := s.recovery.Revalidate(ctx, actor); err != nil {
		s.adminBackupError(w, r, err)
		return
	}
	if snapshotErr != nil {
		s.adminBackupError(w, r, snapshotErr)
		return
	}
	if snapshot == nil || snapshot.Name() != id+".age" || snapshot.Size() != size {
		s.adminBackupError(w, r, recovery.ErrUnavailable)
		return
	}
	err = serveAdminBackupSnapshot(w, r.WithContext(ctx), snapshot, view.SHA256, func(ctx context.Context) error {
		check, done := context.WithTimeout(ctx, 2*time.Second)
		defer done()
		return s.recovery.Revalidate(check, actor)
	})
	if err != nil {
		s.adminBackupError(w, r, err)
	}
}

type adminBackupSnapshot interface {
	io.ReadSeekCloser
	Name() string
	Size() int64
	ModTime() time.Time
}

// ServeContent provides one immutable representation for HEAD, validators and
// byte ranges. The watcher closes both the source and blocked network writes;
// after headers, every failure escapes middleware as ErrAbortHandler.
func serveAdminBackupSnapshot(w http.ResponseWriter, r *http.Request, snapshot adminBackupSnapshot, digest string, recheck func(context.Context) error) (result error) {
	if snapshot == nil || snapshot.Size() <= 0 || snapshot.Size() > 1<<40 || !adminBackupHex(digest, 64) {
		return recovery.ErrUnavailable
	}
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)
	controller := http.NewResponseController(w)
	writer := &adminBackupDownloadWriter{ResponseWriter: w, ctx: ctx, controller: controller, maximum: snapshot.Size() + 1<<20}
	if err := writer.arm(); err != nil {
		return err
	}
	reader := &adminBackupDownloadReader{ReadSeeker: snapshot, ctx: ctx}
	callbackDone := make(chan struct{})
	stopCallback := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		writer.mu.Lock()
		_ = controller.SetWriteDeadline(time.Now())
		writer.mu.Unlock()
		_ = snapshot.Close()
	})
	watchStop, watchDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-watchStop:
				return
			case <-ticker.C:
				if err := recheck(ctx); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	defer func() {
		close(watchStop)
		<-watchDone
		if !stopCallback() {
			<-callbackDone
		}
		closeErr := snapshot.Close()
		_ = controller.SetWriteDeadline(time.Time{})
		if result == nil {
			result = closeErr
		}
		if result == nil {
			result = context.Cause(ctx)
		}
		if result != nil && writer.status != 0 {
			panic(http.ErrAbortHandler)
		}
	}()
	// Conditional 304 and HEAD must pass this fresh check too, before
	// ServeContent can respond without ever invoking the source reader.
	if err := recheck(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": snapshot.Name()}))
	w.Header().Set("ETag", `"`+digest+`"`)
	http.ServeContent(writer, r.WithContext(ctx), snapshot.Name(), snapshot.ModTime(), reader)
	if writer.err != nil {
		return writer.err
	}
	if reader.err != nil {
		return reader.err
	}
	if r.Method != http.MethodHead && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent) {
		length, err := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
		if err != nil || writer.written != length {
			return io.ErrUnexpectedEOF
		}
	}
	if err := writer.arm(); err != nil {
		return err
	}
	return controller.Flush()
}

type adminBackupDownloadWriter struct {
	http.ResponseWriter
	ctx              context.Context
	controller       *http.ResponseController
	mu               sync.Mutex
	maximum, written int64
	status           int
	err              error
}

func (w *adminBackupDownloadWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *adminBackupDownloadWriter) arm() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	if bound, ok := w.ctx.Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	if err := w.controller.SetWriteDeadline(deadline); err != nil {
		return recovery.ErrUnavailable
	}
	return nil
}

func (w *adminBackupDownloadWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *adminBackupDownloadWriter) Write(data []byte) (int, error) {
	if err := w.arm(); err != nil {
		w.err = err
		return 0, err
	}
	if int64(len(data)) > w.maximum-w.written {
		w.err = recovery.ErrUnavailable
		return 0, w.err
	}
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.written += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}

type adminBackupDownloadReader struct {
	io.ReadSeeker
	ctx context.Context
	err error
}

func (r *adminBackupDownloadReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return 0, err
	}
	n, err := r.ReadSeeker.Read(data)
	if err != nil && !errors.Is(err, io.EOF) {
		r.err = err
	}
	if cause := r.ctx.Err(); cause != nil {
		r.err = cause
		return 0, cause
	}
	return n, err
}

func (r *adminBackupDownloadReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		r.err = err
		return 0, err
	}
	position, err := r.ReadSeeker.Seek(offset, whence)
	if err != nil {
		r.err = err
	}
	return position, err
}
