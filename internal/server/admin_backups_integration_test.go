//go:build linux

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/backupstore"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
)

type adminBackupHTTPFixture struct {
	f       *serverFixture
	cookie  *http.Cookie
	csrf    string
	manager *backupTestManager
}

func newAdminBackupHTTPFixture(t *testing.T) *adminBackupHTTPFixture {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	manager := &backupTestManager{}
	f.app.recovery = manager
	return &adminBackupHTTPFixture{f, cookie, csrf, manager}
}

func (f *adminBackupHTTPFixture) exchange(t *testing.T, method, target, body string, headers http.Header, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(f.f.ctx)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	replaced := make(map[string]bool, len(headers))
	for key, values := range headers {
		key = http.CanonicalHeaderKey(key)
		if !replaced[key] {
			r.Header.Del(key)
			replaced[key] = true
		}
		// Preserve every supplied value, including differently cased duplicate
		// names, while matching net/http's normal wire header canonicalization.
		r.Header[key] = append(r.Header[key], values...)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.f.handler.ServeHTTP(backupTestResponse{response}, r)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native backup responses may not be cached")
	}
	return response
}

func (f *adminBackupHTTPFixture) mutationHeaders() http.Header {
	return http.Header{"Origin": {f.f.cfg.PublicURL}, "X-CSRF-Token": {f.csrf}}
}

func TestHTTPAdminBackupRoutesRequireNativeCookieAndMutationCSRF(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	emby := f.f.embyLogin(t, "Administrator", "administrator-password")
	embyToken := stringValue(t, emby, "AccessToken")
	f.f.users = identity.NewWithApplicationKeyVault(f.f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "master.key")))
	f.f.app.identity = f.f.users
	actor, err := f.f.users.ResolveEmby(f.f.ctx, embyToken)
	if err != nil {
		t.Fatal("resolve application-key test administrator")
	}
	key, err := f.f.users.CreateApplicationKey(f.f.ctx, actor, "Backup route isolation", "127.0.0.1", identity.Client{DeviceID: f.f.app.serverID})
	if err != nil {
		t.Fatal("issue the isolated application credential")
	}
	routes := []struct{ method, path string }{
		{"GET", "/admin/v1/backups/status"}, {"GET", "/admin/v1/backups"}, {"GET", "/admin/v1/backups/" + backupTestID},
		{"POST", "/admin/v1/backups"}, {"POST", "/admin/v1/backups/import"}, {"GET", "/admin/v1/backups/" + backupTestID + "/file"},
		{"HEAD", "/admin/v1/backups/" + backupTestID + "/file"}, {"DELETE", "/admin/v1/backups/" + backupTestID},
		{"GET", "/admin/v1/backup-operations"}, {"GET", "/admin/v1/backup-operations/" + backupTestID},
		{"POST", "/admin/v1/backup-operations/" + backupTestID + "/cancel"}, {"POST", "/admin/v1/restores/plans"},
		{"POST", "/admin/v1/restores/" + backupTestID + "/apply"}, {"POST", "/admin/v1/restores/rollback"},
	}
	for _, route := range routes {
		for _, token := range []string{"", embyToken, key.Token} {
			response := f.exchange(t, route.method, route.path, "", http.Header{"X-Emby-Token": {token}}, nil)
			backupTestError(t, response, 401, "authentication_required")
		}
		if route.method == "POST" || route.method == "DELETE" {
			response := f.exchange(t, route.method, route.path, "{}", nil, f.cookie)
			backupTestError(t, response, 403, "csrf_invalid")
			headers := f.mutationHeaders()
			headers.Set("Origin", "https://foreign.example.test")
			response = f.exchange(t, route.method, route.path, "{}", headers, f.cookie)
			backupTestError(t, response, 403, "origin_denied")
		}
	}
	for _, token := range []string{embyToken, key.Token} {
		response := f.exchange(t, "GET", "/admin/v1/backups/status", "", nil, &http.Cookie{Name: sessionCookie, Value: token})
		if response.Code != 401 {
			t.Fatal("an Emby or application credential authorized a native cookie route")
		}
	}
	if len(f.manager.recorded()) != 0 {
		t.Fatal("a denied backup request reached the manager")
	}
	response := f.exchange(t, "GET", "/emby/Backups", "", http.Header{"X-Emby-Token": {embyToken}}, nil)
	if response.Code != 404 {
		t.Fatal("native recovery exposed an undocumented Emby alias")
	}
	if err := f.f.users.Revoke(f.f.ctx, f.cookie.Value); err != nil {
		t.Fatal("revoke native backup administrator")
	}
	response = f.exchange(t, "GET", "/admin/v1/backups/status", "", nil, f.cookie)
	if response.Code != 401 || len(f.manager.recorded()) != 0 {
		t.Fatal("a revoked administrator reached backup storage")
	}
}

func TestHTTPAdminBackupNilManagerStillRegistersEveryOperation(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	f.f.app.recovery = nil
	for _, route := range []struct{ method, path string }{
		{"GET", "/admin/v1/backups/status"}, {"GET", "/admin/v1/backups"}, {"GET", "/admin/v1/backups/" + backupTestID},
		{"POST", "/admin/v1/backups"}, {"POST", "/admin/v1/backups/import"}, {"HEAD", "/admin/v1/backups/" + backupTestID + "/file"},
		{"DELETE", "/admin/v1/backups/" + backupTestID}, {"GET", "/admin/v1/backup-operations"},
		{"GET", "/admin/v1/backup-operations/" + backupTestID}, {"POST", "/admin/v1/backup-operations/" + backupTestID + "/cancel"},
		{"POST", "/admin/v1/restores/plans"}, {"POST", "/admin/v1/restores/" + backupTestID + "/apply"}, {"POST", "/admin/v1/restores/rollback"},
	} {
		response := f.exchange(t, route.method, route.path, "", f.mutationHeaders(), f.cookie)
		backupTestError(t, response, 503, "backup_unavailable")
	}
}

func TestHTTPAdminBackupDispatchesOnlyCompleteTypedRequests(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	create := `{"RequestId":"` + backupTestID + `","Passphrase":"exact passphrase bytes"}`
	plan := `{"RequestId":"` + backupTestID + `","BackupId":"` + backupTestID + `","SHA256":"` + backupTestSHA + `","Passphrase":"exact passphrase bytes","RestoreDefaults":false,"ReplaceRollback":true,"GenerationRevision":"0"}`
	for _, test := range []struct{ method, path, body, action string }{
		{"POST", "/admin/v1/backups", create, "create"},
		{"DELETE", "/admin/v1/backups/" + backupTestID, `{"RequestId":"` + backupTestID + `","SHA256":"` + backupTestSHA + `"}`, "delete"},
		{"DELETE", "/admin/v1/backups/" + backupTestID, `{"RequestId":"` + backupTestID + `","SHA256":""}`, "delete"},
		{"POST", "/admin/v1/backup-operations/" + backupTestID + "/cancel", `{"Revision":"9007199254740993"}`, "cancel"},
		{"POST", "/admin/v1/restores/plans", plan, "plan"},
		{"POST", "/admin/v1/restores/" + backupTestID + "/apply", `{"Revision":"9007199254740993","GenerationRevision":"0"}`, "apply"},
		{"POST", "/admin/v1/restores/rollback", `{"RequestId":"` + backupTestID + `","GenerationRevision":"0"}`, "rollback"},
	} {
		before := len(f.manager.recorded())
		response := f.exchange(t, test.method, test.path, test.body, f.mutationHeaders(), f.cookie)
		if response.Code != 202 {
			t.Fatalf("%s admission status = %d", test.action, response.Code)
		}
		calls := f.manager.recorded()
		if len(calls) != before+1 || calls[before].action != test.action || calls[before].actor.Kind != "admin" || calls[before].actor.SessionID == "" {
			t.Fatal("native admission lost its typed action or administrator identity")
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(response.Body.Bytes(), &object) != nil || len(object) != 1 || object["Operation"] == nil || bytes.Contains(response.Body.Bytes(), []byte("Passphrase")) {
			t.Fatal("admission did not return only its safe operation projection")
		}
		var operation map[string]any
		if json.Unmarshal(object["Operation"], &operation) != nil || operation["Source"] != nil || operation["Revision"] != "1" {
			t.Fatal("operation decimal revision or nullable source changed")
		}
		if request, ok := calls[before].request.(recovery.PlanRequest); ok && (request.RestoreDefaults || !request.ReplaceRollback || request.GenerationRevision != "0" || !bytes.Equal(request.Passphrase, make([]byte, len(request.Passphrase)))) {
			t.Fatal("plan consent, generation, or secret ownership changed")
		}
	}
	response := f.exchange(t, "GET", "/admin/v1/backups?StartIndex=2&Limit=100", "", nil, f.cookie)
	if response.Code != 200 || !bytes.Contains(response.Body.Bytes(), []byte(`"Items":[]`)) {
		t.Fatal("backup pagination did not retain its bounded empty array")
	}
	calls := f.manager.recorded()
	if calls[len(calls)-1].request != ([2]int{2, 100}) {
		t.Fatal("pagination did not reach the manager exactly")
	}
	for _, bad := range []string{
		strings.Replace(plan, `"SHA256":"`+backupTestSHA+`"`, `"SHA256":""`, 1),
		strings.Replace(plan, `"RestoreDefaults":false`, `"RestoreDefaults":null`, 1),
		strings.Replace(plan, `"ReplaceRollback":true`, `"ReplaceRollback":"true"`, 1),
		strings.Replace(plan, `"GenerationRevision":"0"`, `"GenerationRevision":0`, 1),
	} {
		before := len(f.manager.recorded())
		response := f.exchange(t, "POST", "/admin/v1/restores/plans", bad, f.mutationHeaders(), f.cookie)
		backupTestError(t, response, 400, "invalid_input")
		if len(f.manager.recorded()) != before {
			t.Fatal("an incomplete restore consent reached admission")
		}
	}
}

func TestHTTPAdminBackupRejectsUnknownQueriesAndDuplicateOrEncodedHeaders(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	for _, query := range []string{"?", "?RequestId=x", "?Limit=25", "?api_key=private-secret", "?x=%ff"} {
		response := f.exchange(t, "GET", "/admin/v1/backups/status"+query, "", nil, f.cookie)
		backupTestError(t, response, 400, "invalid_input")
	}
	for _, headers := range []http.Header{
		{"Content-Encoding": {"identity"}}, {"Content-Encoding": {"gzip"}}, {"Content-Type": {"application/json", "application/json"}},
		{"X-CSRF-Token": {f.csrf, f.csrf}}, {"x-csrf-token": {f.csrf}}, {"Range": {"bytes=0-1", "bytes=0-1"}}, {"X-Unused": {"a", "b"}},
	} {
		base := f.mutationHeaders()
		for name, values := range headers {
			base[name] = values
		}
		response := f.exchange(t, "POST", "/admin/v1/backups", `{"RequestId":"`+backupTestID+`","Passphrase":"a sufficient secret"}`, base, f.cookie)
		backupTestError(t, response, 400, "invalid_input")
	}
	for _, requestID := range []string{"", strings.ToUpper(backupTestID), backupTestID + ", " + backupTestID} {
		headers := f.mutationHeaders()
		headers.Set("Content-Type", "application/octet-stream")
		headers.Set("X-Backup-Request-Id", requestID)
		response := f.exchange(t, "POST", "/admin/v1/backups/import", "age-encryption.org/v1\n", headers, f.cookie)
		backupTestError(t, response, 400, "invalid_input")
	}
	if len(f.manager.recorded()) != 0 {
		t.Fatal("a malformed backup selector or header reached storage")
	}
}

func adminBackupHTTPObject(t *testing.T, f *adminBackupHTTPFixture) (*backupstore.Store, []byte) {
	t.Helper()
	store, err := backupstore.Open(backupstore.Config{Directory: filepath.Join(t.TempDir(), "backups"), MaxObjectBytes: 4096, MaxTotalBytes: 16384, MaxObjects: 4, MinFreeBytes: 1})
	if err != nil {
		t.Fatal("open isolated backup object storage")
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error("close isolated backup object storage")
		}
	})
	content := []byte("age-encryption.org/v1\ntransport fixture with immutable bytes\n")
	writer, err := store.Begin(f.f.ctx, backupstore.BeginOptions{Kind: backupstore.KindImported, CreatorID: "test-actor", SessionID: "test-session"})
	if err != nil {
		t.Fatal("begin isolated backup object")
	}
	defer writer.Close()
	if _, err := writer.Write(content); err != nil {
		t.Fatal("write isolated transport bytes")
	}
	proof, err := writer.Prepare(f.f.ctx)
	if err != nil {
		t.Fatal("prepare isolated backup object")
	}
	metadata, err := writer.Publish(f.f.ctx, proof, nil)
	if err != nil {
		t.Fatal("publish isolated backup object")
	}
	f.manager.backup = recovery.BackupView{Id: metadata.ID, Kind: "imported", State: "ready", CreatedAt: metadata.CreatedAt, UpdatedAt: metadata.UpdatedAt,
		SizeBytes: strconv.FormatInt(metadata.Size, 10), SHA256: metadata.Digest}
	f.manager.open = store.Snapshot
	return store, content
}

func TestHTTPAdminBackupDownloadHEADRangesAndValidatorsPinOneObject(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	store, content := adminBackupHTTPObject(t, f)
	path := "/admin/v1/backups/" + f.manager.backup.Id + "/file"
	etag := `"` + f.manager.backup.SHA256 + `"`
	for _, test := range []struct {
		method  string
		headers http.Header
		status  int
		body    []byte
	}{
		{"GET", nil, 200, content}, {"HEAD", nil, 200, nil},
		{"GET", http.Header{"Range": {"bytes=3-11"}}, 206, content[3:12]},
		{"GET", http.Header{"Range": {"bytes=-4"}}, 206, content[len(content)-4:]},
		{"GET", http.Header{"If-None-Match": {etag}}, 304, nil},
		{"HEAD", http.Header{"If-None-Match": {etag}}, 304, nil},
		{"GET", http.Header{"Range": {"bytes=99999-"}}, 416, nil},
	} {
		response := f.exchange(t, test.method, path, "", test.headers, f.cookie)
		if response.Code != test.status {
			t.Fatalf("backup download status = %d, want %d", response.Code, test.status)
		}
		if test.status != 416 && !bytes.Equal(response.Body.Bytes(), test.body) {
			t.Fatal("backup range or HEAD changed immutable attachment bytes")
		}
		if test.status == 200 || test.status == 206 {
			if response.Header().Get("ETag") != etag || response.Header().Get("Content-Type") != "application/octet-stream" ||
				response.Header().Get("Content-Disposition") != "attachment; filename="+f.manager.backup.Id+".age" || response.Header().Get("Accept-Ranges") != "bytes" {
				t.Fatal("backup attachment lost its safe filename, strong digest or range contract")
			}
			wantLength := len(test.body)
			if test.method == "HEAD" {
				wantLength = len(content)
			}
			if response.Header().Get("Content-Length") != strconv.Itoa(wantLength) {
				t.Fatal("backup response omitted its exact nonzero length")
			}
		}
		if store.Status().Readers != 0 {
			t.Fatal("backup HTTP response retained a pinned descriptor")
		}
	}
}

func TestHTTPAdminBackupDownloadRechecksRevocationAfterSnapshotBefore304(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	store, _ := adminBackupHTTPObject(t, f)
	var snapshots atomic.Int32
	f.manager.open = func(ctx context.Context, id string) (*backupstore.Snapshot, error) {
		snapshot, err := store.Snapshot(ctx, id)
		if err != nil {
			return nil, err
		}
		snapshots.Add(1)
		if err := f.f.users.Revoke(ctx, f.cookie.Value); err != nil {
			snapshot.Close()
			return nil, err
		}
		return snapshot, nil
	}
	f.manager.run = func(call backupTestCall) error {
		if call.action == "revalidate" {
			_, err := f.f.users.Resolve(f.f.ctx, f.cookie.Value, "admin")
			return err
		}
		return nil
	}
	response := f.exchange(t, "GET", "/admin/v1/backups/"+f.manager.backup.Id+"/file", "", http.Header{"If-None-Match": {`"` + f.manager.backup.SHA256 + `"`}}, f.cookie)
	backupTestError(t, response, 401, "authentication_required")
	if snapshots.Load() != 1 || store.Status().Readers != 0 || response.Header().Get("ETag") != "" {
		t.Fatal("revoked conditional download retained its snapshot or disclosed a validator")
	}
}

func TestHTTPAdminBackupDisconnectedImportDrainsItsBodyOwner(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	entered, finished := make(chan struct{}), make(chan error, 1)
	f.manager.readImport = func(ctx context.Context, input io.ReadCloser) error {
		close(entered)
		_, err := io.Copy(io.Discard, input)
		closeErr := input.Close()
		if err == nil {
			err = closeErr
		}
		finished <- err
		return err
	}
	server := httptest.NewServer(f.f.handler)
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("parse isolated HTTP listener")
	}
	connection, err := net.DialTimeout("tcp", address.Host, 2*time.Second)
	if err != nil {
		t.Fatal("connect to isolated import endpoint")
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal("bound isolated upload socket")
	}
	request := "POST /admin/v1/backups/import HTTP/1.1\r\nHost: " + address.Host + "\r\nOrigin: " + f.f.cfg.PublicURL +
		"\r\nCookie: " + f.cookie.Name + "=" + f.cookie.Value + "\r\nX-CSRF-Token: " + f.csrf + "\r\nX-Backup-Request-Id: " + backupTestID +
		"\r\nContent-Type: application/octet-stream\r\nContent-Length: 1024\r\n\r\nage-encryption.org/v1\n"
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal("send bounded incomplete import")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("import did not enter its bounded body reader")
	}
	connection.Close()
	select {
	case err := <-finished:
		if err == nil || !(errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			t.Fatal("disconnected upload did not retain a transport failure")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnected import retained a blocked request body")
	}
}

type backupTestConnectionKey struct{}

// Keep an expired application deadline installed until net/http observes it.
// This makes the old EOF cleanup race deterministic while leaving Go's own
// coordinated abortPendingRead (which uses an ancient deadline) untouched.
type backupTestDeadlineConnection struct {
	net.Conn
	mu      sync.Mutex
	request context.Context
	expired *atomic.Int64
}

func (c *backupTestDeadlineConnection) SetReadDeadline(deadline time.Time) error {
	err := c.Conn.SetReadDeadline(deadline)
	now := time.Now()
	if err != nil || deadline.IsZero() || deadline.After(now) || deadline.Before(now.Add(-time.Minute)) {
		return err
	}
	c.expired.Add(1)
	c.mu.Lock()
	ctx := c.request
	c.mu.Unlock()
	if ctx != nil {
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

type backupTestDeadlineListener struct {
	net.Listener
	accepted atomic.Int64
	expired  atomic.Int64
}

func (l *backupTestDeadlineListener) Accept() (net.Conn, error) {
	connection, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.accepted.Add(1)
	return &backupTestDeadlineConnection{Conn: connection, expired: &l.expired}, nil
}

func TestHTTPAdminBackupJSONCompletionPreservesRequestAndKeepAlive(t *testing.T) {
	for _, framing := range []string{"content_length", "chunked"} {
		t.Run(framing, func(t *testing.T) {
			f := newAdminBackupHTTPFixture(t)
			entered := make(chan context.Context, 1)
			released := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(released) }) }
			f.manager.plan = func(ctx context.Context, actor identity.Principal, _ recovery.PlanRequest) (recovery.OperationView, error) {
				entered <- ctx
				select {
				case <-released:
				case <-ctx.Done():
					return recovery.OperationView{}, ctx.Err()
				}
				// Admission must retain a usable context after JSON has been fully
				// consumed and closed, including fresh database authorization.
				fresh, err := f.f.users.Resolve(ctx, f.cookie.Value, "admin")
				if err != nil {
					return recovery.OperationView{}, err
				}
				if fresh.SessionID != actor.SessionID {
					return recovery.OperationView{}, identity.ErrUnauthorized
				}
				return backupTestOperation(), nil
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection := r.Context().Value(backupTestConnectionKey{}).(*backupTestDeadlineConnection)
				connection.mu.Lock()
				connection.request = r.Context()
				connection.mu.Unlock()
				f.f.handler.ServeHTTP(w, r)
			})
			server := httptest.NewUnstartedServer(handler)
			listener := &backupTestDeadlineListener{Listener: server.Listener}
			server.Listener = listener
			server.Config.ConnContext = func(ctx context.Context, connection net.Conn) context.Context {
				return context.WithValue(ctx, backupTestConnectionKey{}, connection)
			}
			server.Start()
			t.Cleanup(server.Close)
			transport := &http.Transport{MaxConnsPerHost: 1, MaxIdleConnsPerHost: 1}
			t.Cleanup(transport.CloseIdleConnections)
			t.Cleanup(release)
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			payload := `{"RequestId":"` + backupTestID + `","BackupId":"` + backupTestID + `","SHA256":"` + backupTestSHA + `","Passphrase":"exact passphrase bytes","RestoreDefaults":false,"ReplaceRollback":false,"GenerationRevision":"0"}`
			request, err := http.NewRequest(http.MethodPost, server.URL+"/admin/v1/restores/plans", strings.NewReader(payload))
			if err != nil {
				t.Fatal("create isolated plan request")
			}
			if framing == "chunked" {
				request.ContentLength = -1
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", f.f.cfg.PublicURL)
			request.Header.Set("X-CSRF-Token", f.csrf)
			request.AddCookie(f.cookie)
			type responseResult struct {
				status int
				err    error
			}
			finished := make(chan responseResult, 1)
			go func() {
				response, err := client.Do(request)
				if err != nil {
					finished <- responseResult{err: err}
					return
				}
				_, err = io.Copy(io.Discard, response.Body)
				closeErr := response.Body.Close()
				finished <- responseResult{response.StatusCode, errors.Join(err, closeErr)}
			}()
			select {
			case ctx := <-entered:
				if ctx.Err() != nil {
					t.Fatal("completed JSON canceled the delayed plan request context")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("authenticated plan did not reach delayed admission")
			}
			release()
			select {
			case result := <-finished:
				if result.err != nil || result.status != http.StatusAccepted {
					t.Fatalf("delayed plan response failed: status=%d, transport_error=%t", result.status, result.err != nil)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("completed plan response did not finish")
			}
			next, err := http.NewRequest(http.MethodGet, server.URL+"/admin/v1/session", nil)
			if err != nil {
				t.Fatal("create follow-up native identity request")
			}
			next.AddCookie(f.cookie)
			response, err := client.Do(next)
			if err != nil {
				t.Fatal("follow-up native identity transport failed")
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if response.StatusCode != http.StatusOK || readErr != nil || closeErr != nil {
				t.Fatalf("follow-up native identity failed: status=%d", response.StatusCode)
			}
			if listener.accepted.Load() != 1 || listener.expired.Load() != 0 {
				t.Fatalf("completed backup cleanup poisoned keep-alive: connections=%d, expired_deadlines=%d", listener.accepted.Load(), listener.expired.Load())
			}
		})
	}
}

func TestHTTPAdminBackupUnauthorizedUnreadBodyDoesNotBlockResponse(t *testing.T) {
	f := newAdminBackupHTTPFixture(t)
	server := httptest.NewServer(f.f.handler)
	t.Cleanup(server.Close)
	address, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("parse isolated backup listener")
	}
	connection, err := net.DialTimeout("tcp", address.Host, 2*time.Second)
	if err != nil {
		t.Fatal("connect unauthenticated backup client")
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal("bound unauthenticated backup client")
	}
	// The client advertises a body but withholds every byte. Authentication
	// rejection must return without waiting for that body or reusing its socket.
	request := "POST /admin/v1/backups/import HTTP/1.1\r\nHost: " + address.Host +
		"\r\nContent-Type: application/octet-stream\r\nX-Backup-Request-Id: " + backupTestID +
		"\r\nContent-Length: 1024\r\n\r\n"
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal("send unauthenticated upload headers")
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatal("unread unauthenticated upload blocked its rejection response")
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || !response.Close || readErr != nil || closeErr != nil {
		t.Fatalf("unread rejection did not safely close the connection: status=%d, close=%t", response.StatusCode, response.Close)
	}
	if len(f.manager.recorded()) != 0 {
		t.Fatal("unauthenticated unread upload reached recovery admission")
	}
}
