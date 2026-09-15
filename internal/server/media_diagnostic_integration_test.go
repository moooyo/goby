//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

const mediaDiagnosticHTTPBase = "/admin/v1/media-diagnostics"

// This owner exercises HTTP orchestration only. Its synthetic stage facts are
// not FFmpeg, decoded-content, hardware-profile, or resource-isolation evidence.
// Native authentication and the runtime's database authorization remain real.
type mediaDiagnosticHTTPTestOwner struct {
	ctx     context.Context
	cancel  context.CancelFunc
	entered chan struct{}
	proceed chan struct{}
	failure error
	closed  atomic.Bool
	closes  atomic.Int32
}

func (o *mediaDiagnosticHTTPTestOwner) Run(_ media.DiagnosticSelection, authorize func(context.Context) error, emit func(media.DiagnosticReport)) (media.DiagnosticReport, error) {
	report := media.DiagnosticReport{Version: 1, State: "running", SessionClosureRequired: true,
		Stages: []media.DiagnosticStage{
			{ID: "synthetic-completed-step", State: "passed"},
			{ID: "synthetic-pending-step", State: "not_run"},
		}}
	if err := authorize(o.ctx); err != nil {
		return report, err
	}
	emit(report)
	close(o.entered)
	select {
	case <-o.ctx.Done():
		report.State, report.Code = "cancelled", "diagnostic_cancelled"
		return report, o.ctx.Err()
	case <-o.proceed:
	}
	if err := authorize(o.ctx); err != nil {
		report.State, report.Code = "cancelled", "diagnostic_authority_lost"
		return report, err
	}
	if o.failure != nil {
		report.State = "incomplete"
		return report, o.failure
	}
	report.State = "stages_complete"
	// Detach the final stage slice from the already published partial report.
	report.Stages = append([]media.DiagnosticStage(nil), report.Stages...)
	report.Stages[1].State = "passed"
	return report, nil
}

func (o *mediaDiagnosticHTTPTestOwner) Cancel() { o.cancel() }

func (o *mediaDiagnosticHTTPTestOwner) Close() error {
	o.cancel()
	o.closes.Add(1)
	o.closed.Store(true)
	return nil
}

type mediaDiagnosticHTTPTestControl struct {
	owners             chan *mediaDiagnosticHTTPTestOwner
	current            atomic.Pointer[mediaDiagnosticHTTPTestOwner]
	opens              atomic.Int32
	reserves           atomic.Int32
	releases           atomic.Int32
	releaseBeforeClose atomic.Bool
	failure            error
}

// Reuse the fixture's initialized runtime and real checkMediaDiagnosticActor.
// Only external execution and conversion-slot acquisition are substituted.
func mediaDiagnosticHTTPControl(t *testing.T, f *serverFixture) *mediaDiagnosticHTTPTestControl {
	t.Helper()
	if f.app.mediaDiagnostics == nil {
		t.Fatal("server did not initialize its diagnostic runtime")
	}
	control := &mediaDiagnosticHTTPTestControl{owners: make(chan *mediaDiagnosticHTTPTestOwner, 8)}
	runtime := f.app.mediaDiagnostics
	runtime.enabled = true
	runtime.reserve = func() (func(), error) {
		control.reserves.Add(1)
		var once sync.Once
		return func() {
			once.Do(func() {
				if owner := control.current.Load(); owner != nil && !owner.closed.Load() {
					control.releaseBeforeClose.Store(true)
				}
				control.releases.Add(1)
			})
		}, nil
	}
	runtime.open = func(ctx context.Context, _ media.DiagnosticExecutionOptions) (mediaDiagnosticOwner, error) {
		ownerCtx, cancel := context.WithCancel(ctx)
		owner := &mediaDiagnosticHTTPTestOwner{ctx: ownerCtx, cancel: cancel,
			entered: make(chan struct{}), proceed: make(chan struct{}), failure: control.failure}
		control.opens.Add(1)
		control.current.Store(owner)
		select {
		case control.owners <- owner:
		default:
		}
		return owner, nil
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close controlled diagnostic workers: %v", err)
		}
	})
	return control
}

func mediaDiagnosticHTTPFixture(t *testing.T) (*serverFixture, *http.Cookie, string, *mediaDiagnosticHTTPTestControl) {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	return f, cookie, csrf, mediaDiagnosticHTTPControl(t, f)
}

func mediaDiagnosticHTTPStartBody(t *testing.T, f *serverFixture, cookie *http.Cookie) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, mediaDiagnosticHTTPBase, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("diagnostic start authority was cacheable")
	}
	status := jsonObject(t, response)
	return map[string]any{"InstanceId": stringValue(t, status, "InstanceId"),
		"RequestId": strings.Repeat("a", 32), "StartToken": stringValue(t, status, "StartToken"), "Mode": "software"}
}

func mediaDiagnosticHTTPPath(body map[string]any) string {
	return mediaDiagnosticHTTPBase + "/runs/" + body["InstanceId"].(string) + "/" + body["RequestId"].(string)
}

func mediaDiagnosticHTTPHeaders(f *serverFixture, csrf string) http.Header {
	return http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}
}

func mediaDiagnosticHTTPRun(t *testing.T, response *httptest.ResponseRecorder, status int, body map[string]any) map[string]any {
	t.Helper()
	expectStatus(t, response, status)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("diagnostic run response was cacheable")
	}
	run := objectValue(t, jsonObject(t, response), "Run")
	if run["InstanceId"] != body["InstanceId"] || run["Id"] != body["RequestId"] || run["Mode"] != body["Mode"] {
		t.Fatal("diagnostic response changed the admitted instance or request identity")
	}
	return run
}

func mediaDiagnosticHTTPWaitOwner(t *testing.T, control *mediaDiagnosticHTTPTestControl) *mediaDiagnosticHTTPTestOwner {
	t.Helper()
	var owner *mediaDiagnosticHTTPTestOwner
	select {
	case owner = <-control.owners:
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic did not create its controlled owner")
	}
	select {
	case <-owner.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic owner did not pass real native authorization")
	}
	return owner
}

func mediaDiagnosticHTTPWaitClosed(t *testing.T, f *serverFixture, control *mediaDiagnosticHTTPTestControl, owner *mediaDiagnosticHTTPTestOwner) {
	t.Helper()
	done := make(chan struct{})
	go func() { f.app.mediaDiagnostics.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("diagnostic did not finish its controlled owner closure")
	}
	if control.opens.Load() != 1 || control.reserves.Load() != 1 || control.releases.Load() != 1 ||
		control.releaseBeforeClose.Load() || owner.closes.Load() != 1 || !owner.closed.Load() {
		t.Fatal("diagnostic duplicated execution or released capacity before owner closure")
	}
}

func mediaDiagnosticHTTPPrivate(t *testing.T, response *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(response.Body.String(), secret) {
			t.Fatal("diagnostic response exposed a credential or private execution error")
		}
	}
}

func TestHTTPMediaDiagnosticsRequireNativeCookieCSRFAndOrigin(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	control := mediaDiagnosticHTTPControl(t, a.serverFixture)
	body := mediaDiagnosticHTTPStartBody(t, a.serverFixture, a.cookie)
	key := a.create(t, "Diagnostic credential boundary")
	emby := stringValue(t, a.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, mediaDiagnosticHTTPBase, nil},
		{http.MethodPost, mediaDiagnosticHTTPBase + "/runs", body},
		{http.MethodGet, mediaDiagnosticHTTPPath(body), nil},
		{http.MethodPost, mediaDiagnosticHTTPPath(body) + "/cancel", map[string]any{}},
	} {
		expectAPIError(t, a.request(t, route.method, route.path, route.body, nil), http.StatusUnauthorized, "authentication_required", false)
		for _, token := range []string{emby, key.token} {
			response := a.request(t, route.method, route.path, route.body, http.Header{"X-Emby-Token": {token}})
			expectAPIError(t, response, http.StatusUnauthorized, "authentication_required", false)
			mediaDiagnosticHTTPPrivate(t, response, token)
			response = a.request(t, route.method, route.path, route.body, nil, &http.Cookie{Name: sessionCookie, Value: token})
			expectAPIError(t, response, http.StatusUnauthorized, "invalid_credentials", false)
			mediaDiagnosticHTTPPrivate(t, response, token)
		}
		if route.method == http.MethodPost {
			expectAPIError(t, a.request(t, route.method, route.path, route.body, nil, a.cookie), http.StatusForbidden, "csrf_invalid", false)
			expectAPIError(t, a.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {"incorrect"}}, a.cookie), http.StatusForbidden, "csrf_invalid", false)
			expectAPIError(t, a.request(t, route.method, route.path, route.body, http.Header{"X-CSRF-Token": {a.csrf}, "Origin": {"https://foreign.example.test"}}, a.cookie), http.StatusForbidden, "origin_denied", false)
		}
	}
	if control.reserves.Load() != 0 || control.opens.Load() != 0 {
		t.Fatal("rejected HTTP authority reached diagnostic execution")
	}
}

func TestHTTPMediaDiagnosticsRejectQueriesAndAmbiguousBodiesBeforeAdmission(t *testing.T) {
	f, cookie, csrf, control := mediaDiagnosticHTTPFixture(t)
	body := mediaDiagnosticHTTPStartBody(t, f, cookie)
	headers := mediaDiagnosticHTTPHeaders(f, csrf)
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, mediaDiagnosticHTTPBase, nil},
		{http.MethodPost, mediaDiagnosticHTTPBase + "/runs", body},
		{http.MethodGet, mediaDiagnosticHTTPPath(body), nil},
		{http.MethodPost, mediaDiagnosticHTTPPath(body) + "/cancel", map[string]any{}},
	} {
		for _, suffix := range []string{"?", "?Mode=software"} {
			expectAPIError(t, f.request(t, route.method, route.path+suffix, route.body, headers, cookie), http.StatusBadRequest, "invalid_input", false)
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(encoded)
	rawHeaders := headers.Clone()
	rawHeaders.Set("Content-Type", "application/json")
	invalidBodies := []string{"", "null", "[]", "{}", valid + "{}", "\xff",
		strings.TrimSuffix(valid, "}") + `,"Mode":"software"}`,
		strings.TrimSuffix(valid, "}") + `,"Path":"/private/diagnostic-source"}`,
		strings.Replace(valid, `"Mode":"software"`, `"Mode":null`, 1),
		strings.Replace(valid, `"Mode":"software"`, `"Mode":7`, 1),
		strings.Replace(valid, `"Mode":"software"`, `"Mode":"hardware"`, 1),
		strings.Replace(valid, `"InstanceId":`, `"instanceId":`, 1),
		strings.Replace(valid, strings.Repeat("a", 32), strings.Repeat("A", 32), 1),
		strings.Repeat(" ", 4096) + valid,
	}
	for _, raw := range invalidBodies {
		response := adminMetadataHTTPRaw(t, f, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", raw, rawHeaders, cookie)
		expectAPIError(t, response, http.StatusBadRequest, "invalid_input", false)
		mediaDiagnosticHTTPPrivate(t, response, "/private/diagnostic-source", cookie.Value)
	}
	for _, values := range [][]string{nil, {"text/plain"}, {"application/json", "application/json"}} {
		malformed := headers.Clone()
		malformed["Content-Type"] = values
		expectAPIError(t, adminMetadataHTTPRaw(t, f, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", valid, malformed, cookie), http.StatusUnsupportedMediaType, "unsupported_media_type", false)
	}
	for _, raw := range []string{"", "null", "[]", `{"Mode":"software"}`, `{"x":1,"x":1}`, "{}{}"} {
		expectAPIError(t, adminMetadataHTTPRaw(t, f, http.MethodPost, mediaDiagnosticHTTPPath(body)+"/cancel", raw, rawHeaders, cookie), http.StatusBadRequest, "invalid_input", false)
	}
	if control.reserves.Load() != 0 || control.opens.Load() != 0 {
		t.Fatal("invalid diagnostic input consumed a conversion slot or created an owner")
	}
}

func TestHTTPMediaDiagnosticDuplicatePostAndInstanceBoundLookup(t *testing.T) {
	f, cookie, csrf, control := mediaDiagnosticHTTPFixture(t)
	body := mediaDiagnosticHTTPStartBody(t, f, cookie)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			request := httptest.NewRequest(http.MethodPost, mediaDiagnosticHTTPBase+"/runs", strings.NewReader(string(encoded))).WithContext(f.ctx)
			request.Header = mediaDiagnosticHTTPHeaders(f, csrf)
			request.Header.Set("Content-Type", "application/json")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, request)
			responses <- response
		}()
	}
	close(start)
	accepted, replayed := 0, 0
	for range 2 {
		select {
		case response := <-responses:
			switch response.Code {
			case http.StatusAccepted:
				accepted++
			case http.StatusOK:
				replayed++
			default:
				t.Fatalf("concurrent identical start returned %d", response.Code)
			}
			mediaDiagnosticHTTPRun(t, response, response.Code, body)
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent diagnostic starts did not return")
		}
	}
	if accepted != 1 || replayed != 1 {
		t.Fatal("identical POST did not admit exactly once")
	}
	owner := mediaDiagnosticHTTPWaitOwner(t, control)
	mediaDiagnosticHTTPRun(t, f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, cookie), http.StatusOK, body)
	conflict := map[string]any{}
	for key, value := range body {
		conflict[key] = value
	}
	conflict["Mode"] = "configured"
	expectAPIError(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", conflict, mediaDiagnosticHTTPHeaders(f, csrf), cookie), http.StatusConflict, "request_conflict", false)
	instance := body["InstanceId"].(string)
	other := "0" + instance[1:]
	if instance[0] == '0' {
		other = "1" + instance[1:]
	}
	expectAPIError(t, f.request(t, http.MethodGet, mediaDiagnosticHTTPBase+"/runs/"+other+"/"+body["RequestId"].(string), nil, nil, cookie), http.StatusConflict, "instance_changed", false)
	expectAPIError(t, f.request(t, http.MethodGet, mediaDiagnosticHTTPBase+"/runs/"+instance+"/"+strings.Repeat("b", 32), nil, nil, cookie), http.StatusNotFound, "diagnostic_run_not_found", false)
	close(owner.proceed)
	mediaDiagnosticHTTPWaitClosed(t, f, control, owner)
	final := mediaDiagnosticHTTPRun(t, f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, cookie), http.StatusOK, body)
	if final["State"] != "passed" || objectValue(t, final, "Report")["SessionClosureRequired"] != false {
		t.Fatal("completed fake owner did not finalize its HTTP result after closure")
	}
	again := mediaDiagnosticHTTPRun(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body, mediaDiagnosticHTTPHeaders(f, csrf), cookie), http.StatusOK, body)
	if again["FinishedAt"] != final["FinishedAt"] || control.opens.Load() != 1 || control.reserves.Load() != 1 {
		t.Fatal("a completed request identifier launched new diagnostic work")
	}
}

func TestHTTPMediaDiagnosticCancellationPreservesPartialFactsWithoutReplay(t *testing.T) {
	f, cookie, csrf, control := mediaDiagnosticHTTPFixture(t)
	body := mediaDiagnosticHTTPStartBody(t, f, cookie)
	headers := mediaDiagnosticHTTPHeaders(f, csrf)
	mediaDiagnosticHTTPRun(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body, headers, cookie), http.StatusAccepted, body)
	owner := mediaDiagnosticHTTPWaitOwner(t, control)
	mediaDiagnosticHTTPRun(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPPath(body)+"/cancel", map[string]any{}, headers, cookie), http.StatusOK, body)
	mediaDiagnosticHTTPWaitClosed(t, f, control, owner)
	response := f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, cookie)
	run := mediaDiagnosticHTTPRun(t, response, http.StatusOK, body)
	if run["State"] != "cancelled" || run["Code"] != "diagnostic_cancelled" {
		t.Fatal("cancellation did not publish its safe terminal state")
	}
	report := objectValue(t, run, "Report")
	stages := report["Stages"].([]any)
	if report["SessionClosureRequired"] != false || len(stages) != 2 || stages[0].(map[string]any)["State"] != "passed" || stages[1].(map[string]any)["State"] != "not_run" {
		t.Fatal("cancellation erased completed facts or presented unattempted work as executed")
	}
	mediaDiagnosticHTTPPrivate(t, response, cookie.Value, csrf, body["StartToken"].(string))
	for _, replay := range []*httptest.ResponseRecorder{
		f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body, headers, cookie),
		f.request(t, http.MethodPost, mediaDiagnosticHTTPPath(body)+"/cancel", map[string]any{}, headers, cookie),
	} {
		again := mediaDiagnosticHTTPRun(t, replay, http.StatusOK, body)
		if again["State"] != "cancelled" || again["FinishedAt"] != run["FinishedAt"] {
			t.Fatal("repeated cancellation or start changed the retained outcome")
		}
	}
	if control.opens.Load() != 1 || control.reserves.Load() != 1 {
		t.Fatal("cancelled request was executed again")
	}
}

func TestHTTPMediaDiagnosticRealAuthorityLossStopsOwnerAndRetainsSafeResult(t *testing.T) {
	for _, loss := range []string{"expiry", "demotion"} {
		t.Run(loss, func(t *testing.T) {
			f, peer, peerCSRF, control := mediaDiagnosticHTTPFixture(t)
			operatorID := managedHTTPCreate(t, f, peer, peerCSRF, "Diagnostic operator", true)
			cookie, csrf := managedHTTPLogin(t, f, "Diagnostic operator", "managed-user-password")
			actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
			if err != nil {
				t.Fatal(err)
			}
			body := mediaDiagnosticHTTPStartBody(t, f, cookie)
			mediaDiagnosticHTTPRun(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body, mediaDiagnosticHTTPHeaders(f, csrf), cookie), http.StatusAccepted, body)
			owner := mediaDiagnosticHTTPWaitOwner(t, control)
			if loss == "expiry" {
				// Preserve the stored expires_at > created_at constraint while
				// making this one authenticated credential expire in database time.
				changed, err := f.pool.Exec(f.ctx, `WITH instant AS MATERIALIZED (SELECT clock_timestamp() AS now)
					UPDATE sessions SET created_at=LEAST(created_at,instant.now-interval '2 seconds'),
					expires_at=instant.now-interval '1 second' FROM instant WHERE id=$1 AND user_id=$2`, actor.SessionID, operatorID)
				if err != nil || changed.RowsAffected() != 1 {
					t.Fatal("expire only the diagnostic actor's real credential")
				}
			} else {
				update := managedHTTPUpdateBody(managedHTTPDetail(t, f, peer, operatorID))
				update["IsAdministrator"] = false
				expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/users/"+operatorID, update, mediaDiagnosticHTTPHeaders(f, peerCSRF), peer), http.StatusOK)
			}
			// The owner's next checkpoint invokes the unchanged real database
			// authorization function; no authority result is supplied by a fake.
			close(owner.proceed)
			mediaDiagnosticHTTPWaitClosed(t, f, control, owner)
			response := f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, peer)
			run := mediaDiagnosticHTTPRun(t, response, http.StatusOK, body)
			if run["State"] != "cancelled" || run["Code"] != "diagnostic_authority_lost" || objectValue(t, run, "Report")["SessionClosureRequired"] != false {
				t.Fatal("lost administrator authority was presented as a successful diagnostic")
			}
			mediaDiagnosticHTTPPrivate(t, response, cookie.Value, csrf, body["StartToken"].(string))
			expectAPIError(t, f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, cookie), http.StatusUnauthorized, "invalid_credentials", false)
			expectAPIError(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body, mediaDiagnosticHTTPHeaders(f, csrf), cookie), http.StatusUnauthorized, "invalid_credentials", false)
			if control.opens.Load() != 1 || control.reserves.Load() != 1 {
				t.Fatal("an invalidated actor replayed its old start request")
			}
		})
	}
}

func TestHTTPMediaDiagnosticOwnerErrorRemainsPrivate(t *testing.T) {
	f, cookie, csrf, control := mediaDiagnosticHTTPFixture(t)
	const privateFailure = "private-ffmpeg-stderr /private/diagnostic-input token=fixture-secret"
	control.failure = errors.New(privateFailure)
	body := mediaDiagnosticHTTPStartBody(t, f, cookie)
	mediaDiagnosticHTTPRun(t, f.request(t, http.MethodPost, mediaDiagnosticHTTPBase+"/runs", body, mediaDiagnosticHTTPHeaders(f, csrf), cookie), http.StatusAccepted, body)
	owner := mediaDiagnosticHTTPWaitOwner(t, control)
	close(owner.proceed)
	mediaDiagnosticHTTPWaitClosed(t, f, control, owner)
	response := f.request(t, http.MethodGet, mediaDiagnosticHTTPPath(body), nil, nil, cookie)
	run := mediaDiagnosticHTTPRun(t, response, http.StatusOK, body)
	if run["State"] != "failed" || run["Code"] != "diagnostic_execution_failed" {
		t.Fatal("private execution failure lost its safe public classification")
	}
	mediaDiagnosticHTTPPrivate(t, response, privateFailure, "fixture-secret", cookie.Value)
}
