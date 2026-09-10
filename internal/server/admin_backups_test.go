package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
)

const backupTestID = "0123456789abcdef0123456789abcdef"
const backupTestSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type backupTestCall struct {
	action  string
	actor   identity.Principal
	id      string
	request any
}

type backupTestManager struct {
	mu         sync.Mutex
	calls      []backupTestCall
	run        func(backupTestCall) error
	backup     recovery.BackupView
	open       func(context.Context, string) (*backupstore.Snapshot, error)
	readImport func(context.Context, io.ReadCloser) error
	plan       func(context.Context, identity.Principal, recovery.PlanRequest) (recovery.OperationView, error)
}

func (m *backupTestManager) record(action string, actor identity.Principal, id string, request any) error {
	call := backupTestCall{action, actor, id, request}
	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.mu.Unlock()
	if m.run != nil {
		return m.run(call)
	}
	return nil
}

func (m *backupTestManager) recorded() []backupTestCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]backupTestCall(nil), m.calls...)
}

func backupTestOperation() recovery.OperationView {
	return recovery.OperationView{Id: backupTestID, RequestId: backupTestID, Revision: "1", Kind: "create", State: "pending", Phase: "admission",
		CreatedAt: time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC), GenerationRevision: "0"}
}

func (m *backupTestManager) Status(_ context.Context, a identity.Principal) (recovery.StatusView, error) {
	return recovery.StatusView{GenerationRevision: "0"}, m.record("status", a, "", nil)
}
func (m *backupTestManager) ListBackups(_ context.Context, a identity.Principal, start, limit int) (recovery.BackupPage, error) {
	return recovery.BackupPage{Items: []recovery.BackupView{}, StartIndex: start, Limit: limit}, m.record("backups", a, "", [2]int{start, limit})
}
func (m *backupTestManager) Backup(_ context.Context, a identity.Principal, id string) (recovery.BackupView, error) {
	return m.backup, m.record("backup", a, id, nil)
}
func (m *backupTestManager) ListOperations(_ context.Context, a identity.Principal, start, limit int) (recovery.OperationPage, error) {
	return recovery.OperationPage{Items: []recovery.OperationView{}, StartIndex: start, Limit: limit}, m.record("operations", a, "", [2]int{start, limit})
}
func (m *backupTestManager) Operation(_ context.Context, a identity.Principal, id string) (recovery.OperationView, error) {
	return backupTestOperation(), m.record("operation", a, id, nil)
}
func (m *backupTestManager) Create(_ context.Context, a identity.Principal, r recovery.CreateRequest) (recovery.OperationView, error) {
	return backupTestOperation(), m.record("create", a, "", r)
}
func (m *backupTestManager) Import(ctx context.Context, a identity.Principal, id string, input io.ReadCloser) (recovery.OperationView, error) {
	if err := m.record("import", a, id, nil); err != nil {
		return recovery.OperationView{}, err
	}
	if m.readImport != nil {
		return backupTestOperation(), m.readImport(ctx, input)
	}
	_, err := io.Copy(io.Discard, input)
	return backupTestOperation(), err
}
func (m *backupTestManager) Delete(_ context.Context, a identity.Principal, id string, r recovery.DeleteRequest) (recovery.OperationView, error) {
	return backupTestOperation(), m.record("delete", a, id, r)
}
func (m *backupTestManager) Cancel(_ context.Context, a identity.Principal, id, revision string) (recovery.OperationView, error) {
	return backupTestOperation(), m.record("cancel", a, id, revision)
}
func (m *backupTestManager) Plan(ctx context.Context, a identity.Principal, r recovery.PlanRequest) (recovery.OperationView, error) {
	if err := m.record("plan", a, "", r); err != nil {
		return recovery.OperationView{}, err
	}
	if m.plan != nil {
		return m.plan(ctx, a, r)
	}
	return backupTestOperation(), nil
}
func (m *backupTestManager) Apply(_ context.Context, a identity.Principal, id string, r recovery.ApplyRequest) (recovery.OperationView, error) {
	return backupTestOperation(), m.record("apply", a, id, r)
}
func (m *backupTestManager) Rollback(_ context.Context, a identity.Principal, r recovery.RollbackRequest) (recovery.OperationView, error) {
	return backupTestOperation(), m.record("rollback", a, "", r)
}
func (m *backupTestManager) Download(ctx context.Context, a identity.Principal, id string) (*backupstore.Snapshot, error) {
	if err := m.record("download", a, id, nil); err != nil {
		return nil, err
	}
	if m.open == nil {
		return nil, recovery.ErrUnavailable
	}
	return m.open(ctx, id)
}
func (m *backupTestManager) Revalidate(_ context.Context, a identity.Principal) error {
	return m.record("revalidate", a, "", nil)
}

type backupTestResponse struct{ *httptest.ResponseRecorder }

func (w backupTestResponse) SetReadDeadline(time.Time) error  { return nil }
func (w backupTestResponse) SetWriteDeadline(time.Time) error { return nil }

func backupTestRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/admin/v1/backups", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r.WithContext(context.WithValue(context.WithValue(r.Context(), requestIDKey, "backup-test-request"), principalKey,
		identity.Principal{Kind: "admin", SessionID: "test-session", User: identity.User{ID: "test-admin", IsAdministrator: true}}))
}

func backupTestError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("backup status = %d, want %d", response.Code, status)
	}
	var result struct {
		Error     struct{ Code, Message string }
		RequestId string
	}
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.Error.Code != code || result.Error.Message == "" || result.RequestId == "" {
		t.Fatal("backup error omitted its fixed native envelope")
	}
	for _, forbidden := range []string{"private-secret", "/private/", "postgres://", "settings request", "task identifiers"} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatal("backup error exposed private data or unrelated request wording")
		}
	}
}

func TestAdminBackupJSONRejectsLossyUnknownDuplicateAndUnboundedInput(t *testing.T) {
	valid := `{"RequestId":"` + backupTestID + `","Passphrase":"twelve-byte-passphrase"}`
	for _, test := range []struct {
		name, body, contentType string
		code                    int
		errorCode               string
	}{
		{"unknown", strings.TrimSuffix(valid, "}") + `,"PrivatePath":"/private/secret"}`, "application/json", 400, "invalid_input"},
		{"duplicate", strings.TrimSuffix(valid, "}") + `,"RequestId":"` + backupTestID + `"}`, "application/json", 400, "invalid_input"},
		{"escaped duplicate", strings.TrimSuffix(valid, "}") + `,"\u0052equestId":"` + backupTestID + `"}`, "application/json", 400, "invalid_input"},
		{"missing", `{"RequestId":"` + backupTestID + `"}`, "application/json", 400, "invalid_input"},
		{"null", `{"RequestId":"` + backupTestID + `","Passphrase":null}`, "application/json", 400, "invalid_input"},
		{"array", `[]`, "application/json", 400, "invalid_input"},
		{"trailing", valid + ` {}`, "application/json", 400, "invalid_input"},
		{"utf8", strings.Replace(valid, "twelve", "\xff", 1), "application/json", 400, "invalid_input"},
		{"surrogate", strings.Replace(valid, "twelve", `\ud800`, 1), "application/json", 400, "invalid_input"},
		{"wrong case", strings.Replace(valid, "RequestId", "requestId", 1), "application/json", 400, "invalid_input"},
		{"uppercase id", strings.Replace(valid, backupTestID, strings.ToUpper(backupTestID), 1), "application/json", 400, "invalid_input"},
		{"short secret", `{"RequestId":"` + backupTestID + `","Passphrase":"short"}`, "application/json", 400, "invalid_input"},
		{"long secret", `{"RequestId":"` + backupTestID + `","Passphrase":"` + strings.Repeat("x", 1025) + `"}`, "application/json", 400, "invalid_input"},
		{"body bound", valid + strings.Repeat(" ", adminBackupJSONLimit), "application/json", 413, "payload_too_large"},
		{"media type", valid, "text/plain", 415, "unsupported_media_type"},
		{"charset", valid, "application/json; charset=latin1", 415, "unsupported_media_type"},
		{"extended charset", valid, "application/json; charset*=utf-8''utf-8", 415, "unsupported_media_type"},
	} {
		t.Run(test.name, func(t *testing.T) {
			manager := &backupTestManager{}
			app := &Server{recovery: manager}
			r := backupTestRequest(test.body)
			r.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			app.adminBackupRequest(backupTestResponse{response}, r, "create")
			backupTestError(t, response, test.code, test.errorCode)
			if len(manager.recorded()) != 0 {
				t.Fatal("invalid JSON reached recovery admission")
			}
		})
	}
}

func TestAdminBackupPassphrasesPreserveUTF8BytesAndClearCallerOwnership(t *testing.T) {
	for _, encoded := range []string{`"  twelve-byte-secret  "`, `"twelve\n\t\u0000\ud83d\ude80"`, `"` + strings.Repeat("x", 1024) + `"`} {
		var expected string
		if json.Unmarshal([]byte(encoded), &expected) != nil {
			t.Fatal("invalid test passphrase")
		}
		var retained []byte
		manager := &backupTestManager{run: func(call backupTestCall) error {
			request := call.request.(recovery.CreateRequest)
			if string(request.Passphrase) != expected {
				t.Fatal("passphrase bytes were normalized or lost")
			}
			retained = request.Passphrase
			return nil
		}}
		response := httptest.NewRecorder()
		(&Server{recovery: manager}).adminBackupRequest(backupTestResponse{response}, backupTestRequest(`{"RequestId":"`+backupTestID+`","Passphrase":`+encoded+`}`), "create")
		if response.Code != 202 || len(retained) != len(expected) || !bytes.Equal(retained, make([]byte, len(retained))) {
			t.Fatal("accepted passphrase bytes were not cleared after manager admission")
		}
		if strings.Contains(response.Body.String(), "Passphrase") || strings.Contains(response.Body.String(), expected) {
			t.Fatal("secret appeared in the operation response")
		}
	}
}

func TestAdminBackupPaginationAndMutationRevisionBounds(t *testing.T) {
	for _, query := range []string{"?Limit=0", "?Limit=101", "?Limit=01", "?StartIndex=-1", "?StartIndex=+1", "?StartIndex=2147483648", "?Limit=25&Limit=25", "?limit=25", "?Unknown=x", "?Limit=%ff", "?"} {
		if _, _, err := adminBackupPagination(httptest.NewRequest(http.MethodGet, "/admin/v1/backups"+query, nil)); !errors.Is(err, recovery.ErrInvalid) {
			t.Fatal("an invalid backup page query was accepted")
		}
	}
	start, limit, err := adminBackupPagination(httptest.NewRequest(http.MethodGet, "/admin/v1/backups", nil))
	if err != nil || start != 0 || limit != 25 {
		t.Fatal("backup page defaults changed")
	}
	for _, value := range []string{`null`, `0`, `""`, `"01"`, `"+1"`, `"-1"`, `"18446744073709551616"`} {
		if _, ok := adminBackupRevision(json.RawMessage(value), false); ok {
			t.Fatal("an invalid revision was accepted")
		}
	}
	if _, ok := adminBackupRevision(json.RawMessage(`"0"`), true); ok {
		t.Fatal("operation revision zero was accepted")
	}
	if value, ok := adminBackupRevision(json.RawMessage(`"18446744073709551615"`), false); !ok || value != "18446744073709551615" {
		t.Fatal("a uint64 revision lost precision")
	}
}

func TestAdminBackupErrorsUseSafeStatusCodesWithoutPrivateDetails(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{recovery.ErrInvalid, 400, "invalid_input"}, {recovery.ErrArchive, 422, "invalid_archive"},
		{recovery.ErrNotFound, 404, "not_found"}, {recovery.ErrConflict, 409, "conflict"}, {recovery.ErrBusy, 409, "busy"},
		{backupstore.ErrNotReady, 409, "target_not_ready"}, {recovery.ErrCapacity, 507, "capacity_exceeded"},
		{backupstore.ErrQuota, 507, "capacity_exceeded"}, {errAdminBackupPayloadLarge, 413, "payload_too_large"},
		{context.DeadlineExceeded, 408, "request_timeout"}, {identity.ErrUnauthorized, 401, "authentication_required"},
		{identity.ErrClientSessionForbidden, 403, "administrator_required"}, {recovery.ErrUnavailable, 503, "backup_unavailable"},
		{errors.New("postgres://private-secret@host/private/data"), 503, "backup_unavailable"},
	} {
		response := httptest.NewRecorder()
		(&Server{}).adminBackupError(response, backupTestRequest(""), fmt.Errorf("/private/private-secret: %w", test.err))
		backupTestError(t, response, test.status, test.code)
	}
}

type backupTestLockedBody struct {
	mu                     sync.Mutex
	entered                chan struct{}
	unblocked              chan struct{}
	enterOnce, releaseOnce sync.Once
	closed                 atomic.Bool
}

func newBackupTestLockedBody() *backupTestLockedBody {
	return &backupTestLockedBody{entered: make(chan struct{}), unblocked: make(chan struct{})}
}
func (b *backupTestLockedBody) release() { b.releaseOnce.Do(func() { close(b.unblocked) }) }
func (b *backupTestLockedBody) Read([]byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enterOnce.Do(func() { close(b.entered) })
	<-b.unblocked
	return 0, os.ErrDeadlineExceeded
}
func (b *backupTestLockedBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed.Store(true)
	return nil
}

type backupTestBodyDeadline struct {
	*httptest.ResponseRecorder
	body  *backupTestLockedBody
	mu    sync.Mutex
	timer *time.Timer
	last  time.Time
}

func (w *backupTestBodyDeadline) SetReadDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.last = deadline
	if !deadline.IsZero() {
		w.timer = time.AfterFunc(max(time.Duration(0), time.Until(deadline)), w.body.release)
	}
	return nil
}

func TestAdminBackupUploadCancellationAndIdleDeadlineCloseBeforeDraining(t *testing.T) {
	for _, cancelRequest := range []bool{true, false} {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			underlying := newBackupTestLockedBody()
			writer := &backupTestBodyDeadline{ResponseRecorder: httptest.NewRecorder(), body: underlying}
			r := httptest.NewRequest(http.MethodPost, "/admin/v1/backups/import", nil).WithContext(ctx)
			r.Body, r.ContentLength = underlying, -1
			body, finish, err := newAdminBackupBody(writer, r, 1024, 2*time.Minute)
			if err != nil {
				t.Fatal("initialize bounded backup body")
			}
			finished := make(chan error, 1)
			go func() { _, err := io.Copy(io.Discard, body); finish(); finished <- err }()
			<-underlying.entered
			if cancelRequest {
				cancel()
			} else {
				time.Sleep(adminBackupUploadIdle)
			}
			want := context.Canceled
			if !cancelRequest {
				want = context.DeadlineExceeded
			}
			if err := <-finished; !errors.Is(err, want) {
				t.Fatal("stalled upload did not retain its cancellation or timeout cause")
			}
			finish()
			writer.mu.Lock()
			reset := writer.last.IsZero()
			writer.mu.Unlock()
			if !underlying.closed.Load() || !reset {
				t.Fatal("upload cleanup retained its body or poisoned the next request deadline")
			}
		})
	}
}

type backupTestCompletionResponse struct {
	*httptest.ResponseRecorder
	mu      sync.Mutex
	expired int
	last    time.Time
}

func (w *backupTestCompletionResponse) SetReadDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.last = deadline
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		w.expired++
	}
	return nil
}

func TestAdminBackupCompletedBodyNeverExpiresConnectionReadDeadline(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("outer_cleanup_%t", wrapped), func(t *testing.T) {
			response := &backupTestCompletionResponse{ResponseRecorder: httptest.NewRecorder()}
			request := backupTestRequest(`{"completed":true}`)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, finish, err := newAdminBackupBody(w, r, 1024, time.Minute)
				if err != nil {
					t.Fatal("initialize completed request body")
				}
				defer finish()
				data, err := io.ReadAll(body)
				if err != nil || string(data) != `{"completed":true}` {
					t.Fatal("read complete request body")
				}
				finish()
				if err := body.Close(); err != nil || r.Context().Err() != nil {
					t.Fatal("normal body cleanup invalidated the native request")
				}
				w.WriteHeader(http.StatusAccepted)
			})
			if wrapped {
				handler = adminBackupNoCache(handler)
			}
			handler.ServeHTTP(response, request)
			response.mu.Lock()
			expired, reset := response.expired, response.last.IsZero()
			response.mu.Unlock()
			if expired != 0 || !reset || response.Header().Get("Connection") == "close" {
				t.Fatalf("completed cleanup expired a connection deadline: count=%d, reset=%t", expired, reset)
			}
		})
	}
}

func TestAdminBackupImportReadsAtMostOneByteBeyondItsLimit(t *testing.T) {
	manager := &backupTestManager{}
	app := &Server{recovery: manager, cfg: config.Config{Recovery: config.RecoveryConfig{Backups: backupstore.Config{MaxObjectBytes: 32}}}}
	r := backupTestRequest(strings.Repeat("x", 4096))
	r.ContentLength = -1
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Backup-Request-Id", backupTestID)
	response := httptest.NewRecorder()
	app.adminBackupRequest(backupTestResponse{response}, r, "import")
	backupTestError(t, response, 413, "payload_too_large")
	remaining, err := io.ReadAll(r.Body)
	if err != nil || len(remaining) != 4096-33 {
		t.Fatal("the HTTP import reader consumed beyond maximum plus one byte")
	}
}

type backupTestSnapshot struct {
	*bytes.Reader
	closed atomic.Bool
	fail   bool
}

func (s *backupTestSnapshot) Name() string       { return backupTestID + ".age" }
func (s *backupTestSnapshot) Size() int64        { return s.Reader.Size() }
func (s *backupTestSnapshot) ModTime() time.Time { return time.Time{} }
func (s *backupTestSnapshot) Close() error       { s.closed.Store(true); return nil }
func (s *backupTestSnapshot) Read(data []byte) (int, error) {
	if s.fail {
		n, _ := s.Reader.Read(data[:min(3, len(data))])
		return n, io.ErrUnexpectedEOF
	}
	return s.Reader.Read(data)
}

func TestAdminBackupDownloadRevalidatesBeforeConditionalResponses(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		snapshot := &backupTestSnapshot{Reader: bytes.NewReader([]byte("immutable encrypted backup"))}
		r := httptest.NewRequest(method, "/admin/v1/backups/"+backupTestID+"/file", nil)
		r.Header.Set("If-None-Match", `"`+backupTestSHA+`"`)
		response := backupTestResponse{httptest.NewRecorder()}
		err := serveAdminBackupSnapshot(response, r, snapshot, backupTestSHA, func(context.Context) error { return identity.ErrUnauthorized })
		if !errors.Is(err, identity.ErrUnauthorized) || response.Code == 304 || response.Body.Len() != 0 || !snapshot.closed.Load() {
			t.Fatal("a conditional backup response bypassed fresh authority or retained its descriptor")
		}
	}
}

func TestAdminBackupPartialDownloadAbortsThroughMiddleware(t *testing.T) {
	snapshot := &backupTestSnapshot{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 128)), fail: true}
	app := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	response := backupTestResponse{httptest.NewRecorder()}
	handler := app.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = serveAdminBackupSnapshot(w, r, snapshot, backupTestSHA, func(context.Context) error { return nil })
	}))
	var escaped any
	func() {
		defer func() { escaped = recover() }()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/v1/backups/"+backupTestID+"/file", nil))
	}()
	if escaped != http.ErrAbortHandler || response.Body.Len() != 3 || strings.Contains(response.Body.String(), "Error") || !snapshot.closed.Load() {
		t.Fatal("a truncated backup response was reported as a successful stream or swallowed by middleware")
	}
}

type backupTestBlockedDownload struct {
	*httptest.ResponseRecorder
	entered, released      chan struct{}
	enterOnce, releaseOnce sync.Once
	mu                     sync.Mutex
	last                   time.Time
}

func (w *backupTestBlockedDownload) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	w.last = deadline
	w.mu.Unlock()
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		w.releaseOnce.Do(func() { close(w.released) })
	}
	return nil
}

func (w *backupTestBlockedDownload) Write(data []byte) (int, error) {
	n, _ := w.ResponseRecorder.Write(data[:min(8, len(data))])
	w.enterOnce.Do(func() { close(w.entered) })
	<-w.released
	return n, os.ErrDeadlineExceeded
}

func TestAdminBackupDownloadRevocationInterruptsBlockedWritesAndJoinsDeadlineOwners(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		snapshot := &backupTestSnapshot{Reader: bytes.NewReader(bytes.Repeat([]byte("x"), 128))}
		response := &backupTestBlockedDownload{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), released: make(chan struct{})}
		var denied atomic.Bool
		var checks atomic.Int32
		finished := make(chan any, 1)
		go func() {
			defer func() { finished <- recover() }()
			_ = serveAdminBackupSnapshot(response, httptest.NewRequest(http.MethodGet, "/backup/file", nil), snapshot, backupTestSHA,
				func(context.Context) error {
					checks.Add(1)
					if denied.Load() {
						return identity.ErrUnauthorized
					}
					return nil
				})
		}()
		<-response.entered
		denied.Store(true)
		time.Sleep(time.Second)
		if escaped := <-finished; escaped != http.ErrAbortHandler {
			t.Fatal("revoked backup stream did not abort its blocked response")
		}
		response.mu.Lock()
		reset := response.last.IsZero()
		response.mu.Unlock()
		if checks.Load() < 2 || !snapshot.closed.Load() || !reset || response.Body.Len() != 8 {
			t.Fatal("backup revocation did not close its snapshot and join the response deadline owners")
		}
	})
}
