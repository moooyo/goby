//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type scheduledTaskHTTPFixture struct {
	*serverFixture
	cookie *http.Cookie
	csrf   string
	admin  http.Header
	taskID string
}

func scheduledTaskHTTPAuthenticate(t *testing.T, f *serverFixture) *scheduledTaskHTTPFixture {
	t.Helper()
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	admin := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "scheduled-task-administrator")
	definition, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatalf("read library task definition: %v", err)
	}
	fixture := &scheduledTaskHTTPFixture{serverFixture: f, cookie: cookie, csrf: csrf, admin: admin.headers, taskID: definition.ID}
	scheduledTaskHTTPWait(t, fixture, "task manager readiness", func() bool { return f.app.taskManager.Available() })
	return fixture
}

func scheduledTaskHTTPWait(t *testing.T, f *scheduledTaskHTTPFixture, description string, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s: %v", description, ctx.Err())
		case <-ticker.C:
		}
	}
}

func scheduledTaskHTTPNoContent(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	expectStatus(t, response, http.StatusNoContent)
	if response.Body.Len() != 0 {
		t.Fatalf("task mutation returned a response body: %q", response.Body.String())
	}
}

func scheduledTaskHTTPInfo(t *testing.T, f *scheduledTaskHTTPFixture) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/ScheduledTasks/"+f.taskID, nil, f.admin)
	expectStatus(t, response, http.StatusOK)
	info := jsonObject(t, response)
	scheduledTaskHTTPAssertInfo(t, info, f.taskID)
	return info
}

func scheduledTaskHTTPAssertInfo(t *testing.T, info map[string]any, taskID string) {
	t.Helper()
	required := []string{"Id", "Name", "Key", "Description", "Category", "IsHidden", "State", "Triggers"}
	allowed := map[string]bool{"LastExecutionResult": true, "CurrentProgressPercentage": true}
	for _, name := range required {
		allowed[name] = true
		if _, present := info[name]; !present {
			t.Errorf("TaskInfo omitted %s", name)
		}
	}
	for name := range info {
		if !allowed[name] {
			t.Errorf("TaskInfo exposed a native or unexpected field: %s", name)
		}
	}
	if info["Id"] != taskID || info["Key"] != "RefreshLibrary" || info["Name"] != "Scan media library" || info["Category"] != "Library" || info["IsHidden"] != false {
		t.Fatalf("TaskInfo lost its library definition identity: %#v", info)
	}
	stringValue(t, info, "Description")
	if _, ok := info["Triggers"].([]any); !ok {
		t.Fatal("TaskInfo Triggers must be a non-null array")
	}
	progress, hasProgress := info["CurrentProgressPercentage"]
	if info["State"] == "Idle" {
		if hasProgress {
			t.Error("idle TaskInfo included CurrentProgressPercentage")
		}
	} else {
		value, ok := progress.(float64)
		if !ok || value < 0 || value > 100 {
			t.Errorf("active TaskInfo has invalid progress: %#v", progress)
		}
	}
	if _, present := info["LastExecutionResult"]; present {
		result := objectValue(t, info, "LastExecutionResult")
		fields := map[string]bool{"StartTimeUtc": true, "EndTimeUtc": true, "Status": true, "Name": true, "Id": true, "Key": true}
		if result["Status"] == "Failed" || result["Status"] == "Aborted" {
			fields["ErrorMessage"] = true
		}
		for name := range result {
			if !fields[name] {
				t.Errorf("LastExecutionResult exposed an unexpected field: %s", name)
			}
		}
		if result["Id"] != taskID || result["Key"] != info["Key"] || result["Name"] != info["Name"] {
			t.Fatal("LastExecutionResult must identify its task definition")
		}
		for _, name := range []string{"StartTimeUtc", "EndTimeUtc"} {
			value := stringValue(t, result, name)
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil || !strings.HasSuffix(value, "Z") {
				t.Errorf("LastExecutionResult %s is not an RFC3339 UTC timestamp: %q", name, value)
			}
		}
	}
}

func scheduledTaskHTTPDefinition(t *testing.T, f *scheduledTaskHTTPFixture) tasks.Definition {
	t.Helper()
	definition, err := f.app.taskStore.Get(f.ctx, f.taskID)
	if err != nil {
		t.Fatalf("read durable task definition: %v", err)
	}
	return definition
}

func scheduledTaskHTTPHistory(t *testing.T, f *scheduledTaskHTTPFixture, count int64) []tasks.Run {
	t.Helper()
	page, err := f.app.taskStore.ListRuns(f.ctx, f.taskID, tasks.Page{Limit: tasks.MaxPageLimit})
	if err != nil {
		t.Fatalf("read durable task history: %v", err)
	}
	if page.TotalRecordCount != count || int64(len(page.Items)) != count {
		t.Fatalf("durable task history contains %d runs, want %d", page.TotalRecordCount, count)
	}
	return page.Items
}

func scheduledTaskHTTPChildren(t *testing.T, f *scheduledTaskHTTPFixture, runID string, count int) []tasks.Child {
	t.Helper()
	page, err := f.app.taskStore.ListChildren(f.ctx, runID, tasks.Page{Limit: tasks.MaxPageLimit})
	if err != nil {
		t.Fatalf("read durable task children: %v", err)
	}
	if page.TotalRecordCount != int64(count) || len(page.Items) != count {
		t.Fatalf("durable task snapshot contains %d children, want %d", page.TotalRecordCount, count)
	}
	return page.Items
}

func scheduledTaskHTTPWaitRun(t *testing.T, f *scheduledTaskHTTPFixture, runID string, state tasks.RunState) tasks.Run {
	t.Helper()
	var run tasks.Run
	scheduledTaskHTTPWait(t, f, "task run "+string(state), func() bool {
		var err error
		run, err = f.app.taskStore.GetRun(f.ctx, runID)
		if err != nil {
			t.Fatalf("read task run: %v", err)
		}
		if run.State == state {
			return true
		}
		if !run.State.Active() {
			t.Fatalf("task run ended as %s, want %s", run.State, state)
		}
		return false
	})
	return run
}

func TestHTTPScheduledTasksRequireManagementTokensAndExposeOnlyTaskInfo(t *testing.T) {
	f := scheduledTaskHTTPAuthenticate(t, newServerFixture(t))
	viewer, err := f.users.CreateUser(f.ctx, "Scheduled Task Viewer", "scheduled-task-viewer-password", false)
	if err != nil {
		t.Fatalf("create task viewer: %v", err)
	}
	viewerLogin := loginClientSessionHTTP(t, f.serverFixture, viewer.Name, "scheduled-task-viewer-password", "scheduled-task-viewer")
	base := "/emby/ScheduledTasks/" + f.taskID
	routes := []struct {
		name, method, target string
		body                 any
	}{
		{"list", http.MethodGet, "/emby/ScheduledTasks", nil},
		{"detail", http.MethodGet, base, nil},
		{"triggers", http.MethodPost, base + "/Triggers", []any{}},
		{"start", http.MethodPost, "/emby/ScheduledTasks/Running/" + f.taskID, nil},
		{"stop", http.MethodDelete, "/emby/ScheduledTasks/Running/" + f.taskID, nil},
		{"stop alias", http.MethodPost, "/emby/ScheduledTasks/Running/" + f.taskID + "/Delete", nil},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			expectEmbyTextError(t, f.request(t, route.method, route.target, route.body, nil), http.StatusUnauthorized, "Access token is invalid or expired.")
			expectEmbyTextError(t, f.request(t, route.method, route.target, route.body, viewerLogin.headers), http.StatusForbidden, "User "+viewer.Name+" does not have access to ManageServer feature.")
			expectEmbyTextError(t, f.request(t, route.method, route.target, route.body, nil, f.cookie), http.StatusUnauthorized, "Access token is invalid or expired.")
			expectEmbyTextError(t, f.request(t, route.method, route.target, route.body, http.Header{"X-Emby-Token": {f.cookie.Value}}), http.StatusUnauthorized, "Access token is invalid or expired.")
		})
	}
	scheduledTaskHTTPHistory(t, f, 0)
	if definition := scheduledTaskHTTPDefinition(t, f); definition.Revision != 1 || len(definition.Triggers) != 0 {
		t.Fatal("denied requests changed the task schedule")
	}
	for _, test := range []struct {
		target string
		count  int
	}{
		{"/emby/ScheduledTasks", 1},
		{"/ScheduledTasks", 1},
		{"/EmBy/sChEdUlEdTaSkS?iShIdDeN=false&iSeNaBlEd=true", 1},
		{"/ScheduledTasks?IsHidden=true", 0},
		{"/ScheduledTasks?IsEnabled=false", 0},
		{"/ScheduledTasks?IsHidden=false&IsEnabled=false", 0},
	} {
		response := f.request(t, http.MethodGet, test.target, nil, f.admin)
		items := responseArray(t, response)
		if len(items) != test.count {
			t.Fatalf("task list %s contains %d definitions, want %d", test.target, len(items), test.count)
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
			t.Error("task list is cacheable")
		}
		for _, info := range items {
			scheduledTaskHTTPAssertInfo(t, info, f.taskID)
		}
	}
	for _, target := range []string{"/sChEdUlEdTaSkS/" + f.taskID, "/EMBY/SCHEDULEDTASKS/" + f.taskID} {
		response := f.request(t, http.MethodGet, target, nil, f.admin)
		expectStatus(t, response, http.StatusOK)
		scheduledTaskHTTPAssertInfo(t, jsonObject(t, response), f.taskID)
	}
	for _, query := range []string{"?IsHidden=True", "?IsEnabled=1"} {
		expectAPIError(t, f.request(t, http.MethodGet, "/ScheduledTasks"+query, nil, f.admin), http.StatusBadRequest, "invalid_input", true)
	}
	for _, route := range routes[2:] {
		target := strings.Replace(route.target, f.taskID, "goby-missing-task", 1)
		expectEmbyTextError(t, f.request(t, route.method, target, route.body, f.admin), http.StatusNotFound, "Task not found")
	}
	expectEmbyTextError(t, f.request(t, http.MethodGet, "/ScheduledTasks/goby-missing-task", nil, f.admin), http.StatusNotFound, "Task not found")
	for _, route := range routes[4:] {
		expectEmbyTextError(t, f.request(t, route.method, route.target, nil, f.admin), http.StatusInternalServerError, "Cannot cancel a Task unless it is in the Running state.")
	}
	scheduledTaskHTTPHistory(t, f, 0)
}

func scheduledTaskHTTPNativeTriggers(t *testing.T, f *scheduledTaskHTTPFixture, revision int64, timezone string, triggers []map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPut, "/admin/v1/tasks/"+f.taskID+"/triggers", map[string]any{
		"Revision": strconv.FormatInt(revision, 10), "ScheduleTimezone": timezone, "Triggers": triggers,
	}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
}

func scheduledTaskHTTPAssertJSON(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("encode observed JSON: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("encode expected JSON: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("JSON = %s, want %s", gotJSON, wantJSON)
	}
}

func TestHTTPScheduledTaskTriggersPreserveDuplicatesAndNativeTimezoneAtomically(t *testing.T) {
	f := scheduledTaskHTTPAuthenticate(t, newServerFixture(t))
	native := scheduledTaskHTTPNativeTriggers(t, f, 1, "America/New_York", []map[string]any{})
	expectStatus(t, native, http.StatusOK)
	before := scheduledTaskHTTPDefinition(t, f)
	triggers := []map[string]any{
		{"Type": "IntervalTrigger", "IntervalTicks": int64(432000000000)},
		{"Type": "IntervalTrigger", "IntervalTicks": int64(432000000000)},
		{"Type": "DailyTrigger", "TimeOfDayTicks": int64(72000000000), "MaxRuntimeTicks": int64(144000000000)},
		{"Type": "StartupTrigger"},
	}
	headers := f.admin.Clone()
	// Client hints cannot take ownership of the native schedule timezone.
	headers.Set("X-Emby-TimeZone", "Asia/Shanghai")
	path := "/sChEdUlEdTaSkS/" + f.taskID + "/tRiGgErS"
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, path, triggers, headers))
	scheduledTaskHTTPAssertJSON(t, scheduledTaskHTTPInfo(t, f)["Triggers"], triggers)
	saved := scheduledTaskHTTPDefinition(t, f)
	if saved.Revision != before.Revision+1 || saved.ScheduleTimezone != "America/New_York" || len(saved.Triggers) != 4 {
		t.Fatal("compatibility replacement lost the native revision or timezone")
	}
	if saved.Triggers[0].ID == saved.Triggers[1].ID || saved.Triggers[2].Timezone == nil || *saved.Triggers[2].Timezone != "America/New_York" {
		t.Fatal("duplicate intervals were deduplicated or daily time used a client timezone")
	}
	for index, trigger := range saved.Triggers {
		if trigger.Position != index || trigger.ScheduleRevision != saved.Revision || trigger.TaskID != f.taskID {
			t.Fatal("native trigger storage lost order or definition ownership")
		}
	}
	for _, test := range []struct {
		name string
		body []map[string]any
	}{
		{"unknown", []map[string]any{{"Type": "GobyOwnedUnknownTrigger"}}},
		{"mixed", []map[string]any{{"Type": "IntervalTrigger", "IntervalTicks": int64(432000000000)}, {"Type": "GobyOwnedUnknownTrigger"}}},
		// Goby has no SystemEvent executor; this is an explicit compatibility gap.
		{"unsupported system event", []map[string]any{{"Type": "SystemEventTrigger", "SystemEvent": "DisplayConfigurationChange"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			expectAPIError(t, f.request(t, http.MethodPost, path, test.body, f.admin), http.StatusBadRequest, "unsupported_trigger", true)
			after := scheduledTaskHTTPDefinition(t, f)
			if after.Revision != saved.Revision || after.ScheduleTimezone != saved.ScheduleTimezone || len(after.Triggers) != len(saved.Triggers) {
				t.Fatal("rejected trigger replacement changed the native schedule")
			}
			for index, trigger := range after.Triggers {
				if trigger.ID != saved.Triggers[index].ID || trigger.RetiredAt != nil {
					t.Fatal("rejected trigger replacement partially replaced stored rules")
				}
			}
			scheduledTaskHTTPAssertJSON(t, scheduledTaskHTTPInfo(t, f)["Triggers"], triggers)
		})
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, path, []any{}, f.admin))
	scheduledTaskHTTPAssertJSON(t, scheduledTaskHTTPInfo(t, f)["Triggers"], []any{})
	if cleared := scheduledTaskHTTPDefinition(t, f); cleared.ScheduleTimezone != saved.ScheduleTimezone || len(cleared.Triggers) != 0 {
		t.Fatal("clearing compatibility rules changed the native timezone")
	}
}

type scheduledTaskHTTPProbeGate struct {
	release   chan struct{}
	once      sync.Once
	entered   atomic.Int64
	cancelled atomic.Int64
}

func (gate *scheduledTaskHTTPProbeGate) open() { gate.once.Do(func() { close(gate.release) }) }

type scheduledTaskHTTPProber struct {
	mu    sync.Mutex
	gate  *scheduledTaskHTTPProbeGate
	gates []*scheduledTaskHTTPProbeGate
	calls atomic.Int64
}

func (prober *scheduledTaskHTTPProber) block() *scheduledTaskHTTPProbeGate {
	prober.mu.Lock()
	defer prober.mu.Unlock()
	gate := &scheduledTaskHTTPProbeGate{release: make(chan struct{})}
	prober.gate = gate
	prober.gates = append(prober.gates, gate)
	return gate
}

func (prober *scheduledTaskHTTPProber) releaseAll() {
	prober.mu.Lock()
	defer prober.mu.Unlock()
	for _, gate := range prober.gates {
		gate.open()
	}
}

func (prober *scheduledTaskHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	prober.mu.Lock()
	gate := prober.gate
	prober.mu.Unlock()
	prober.calls.Add(1)
	gate.entered.Add(1)
	select {
	case <-gate.release:
	case <-ctx.Done():
		gate.cancelled.Add(1)
		// Cancellation remains observable until the test releases worker cleanup.
		<-gate.release
		return media.Info{}, ctx.Err()
	}
	return (apiMediaProber{}).ProbeFile(ctx, file)
}

func scheduledTaskHTTPScanFixture(t *testing.T) (*scheduledTaskHTTPFixture, string, *scheduledTaskHTTPProber, *scheduledTaskHTTPProbeGate) {
	t.Helper()
	f, root := newLibraryServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	prober := &scheduledTaskHTTPProber{}
	gate := prober.block()
	catalog, err := library.New(f.pool, prober, []string{root})
	if err != nil {
		t.Fatalf("create gated task catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	// Cleanup must release probes before the manager waits for owned workers.
	t.Cleanup(prober.releaseAll)
	f.handler = f.app.Handler()
	return scheduledTaskHTTPAuthenticate(t, f), root, prober, gate
}

func scheduledTaskHTTPCreateLibrary(t *testing.T, f *scheduledTaskHTTPFixture, root string, index int) library.Library {
	t.Helper()
	path := writeAPIMediaFile(t, root, fmt.Sprintf("task-library-%03d/Movie.mp4", index))
	collection, err := f.app.library.CreateLibrary(f.ctx, fmt.Sprintf("Task library %03d", index), "movies", []string{filepath.Dir(path)})
	if err != nil {
		t.Fatalf("create task library: %v", err)
	}
	return collection
}

func TestHTTPScheduledTaskRunsCoalesceAndBothStopRoutesCancelOwnedScans(t *testing.T) {
	f, root, prober, gate := scheduledTaskHTTPScanFixture(t)
	startPath := "/emby/ScheduledTasks/Running/" + f.taskID
	// An empty-library run supplies a real prior result before a blocked run.
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, startPath, nil, f.admin))
	initial := scheduledTaskHTTPHistory(t, f, 1)[0]
	scheduledTaskHTTPWaitRun(t, f, initial.ID, tasks.RunCompleted)
	priorResult := objectValue(t, scheduledTaskHTTPInfo(t, f), "LastExecutionResult")
	if priorResult["Status"] != "Completed" || len(priorResult) != 6 {
		t.Fatal("empty-library completion did not produce the six-field compatibility result")
	}
	collection := scheduledTaskHTTPCreateLibrary(t, f, root, 0)
	for index, stop := range []struct{ method, path string }{
		{http.MethodDelete, "/emby/ScheduledTasks/Running/" + f.taskID},
		{http.MethodPost, "/sChEdUlEdTaSkS/rUnNiNg/" + f.taskID + "/dElEtE"},
	} {
		if index > 0 {
			gate = prober.block()
			startPath = "/ScheduledTasks/running/" + f.taskID
		}
		scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, startPath, nil, f.admin))
		run := scheduledTaskHTTPHistory(t, f, int64(index+2))[0]
		if run.ID == f.taskID || run.Source != "compatibility" || run.TotalChildren != 1 {
			t.Fatal("task admission lost its execution identity, source, or library snapshot")
		}
		scheduledTaskHTTPWait(t, f, "owned scan entering the probe gate", func() bool { return gate.entered.Load() == 1 })
		running := scheduledTaskHTTPInfo(t, f)
		if running["State"] != "Running" || running["CurrentProgressPercentage"] != float64(0) || !reflect.DeepEqual(running["LastExecutionResult"], priorResult) {
			t.Fatal("running TaskInfo lost zero progress or the prior terminal result")
		}
		definition := scheduledTaskHTTPDefinition(t, f)
		if definition.CurrentRun == nil || definition.CurrentRun.ID != run.ID || definition.CurrentRun.State != tasks.RunRunning {
			t.Fatal("stop precondition did not reach a real running execution")
		}
		scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, startPath, nil, f.admin))
		if repeated := scheduledTaskHTTPHistory(t, f, int64(index+2))[0]; repeated.ID != run.ID {
			t.Fatal("duplicate compatibility start did not coalesce into the active run")
		}
		children := scheduledTaskHTTPChildren(t, f, run.ID, 1)
		if children[0].LibraryID != collection.ID || children[0].ScanJobID == nil {
			t.Fatal("running task did not own its library scan")
		}
		scheduledTaskHTTPNoContent(t, f.request(t, stop.method, stop.path, nil, f.admin))
		scheduledTaskHTTPWait(t, f, "owned worker cancellation", func() bool { return gate.cancelled.Load() == 1 })
		if cancelling := scheduledTaskHTTPInfo(t, f); cancelling["State"] != "Cancelling" || !reflect.DeepEqual(cancelling["LastExecutionResult"], priorResult) {
			t.Fatal("task cancellation reported completion before its worker exited")
		}
		expectEmbyTextError(t, f.request(t, stop.method, stop.path, nil, f.admin), http.StatusInternalServerError, "Cannot cancel a Task unless it is in the Running state.")
		gate.open()
		finished := scheduledTaskHTTPWaitRun(t, f, run.ID, tasks.RunCancelled)
		if finished.TerminalChildren != 1 || finished.CancelledChildren != 1 {
			t.Fatal("cancelled task lost its owned child outcome")
		}
		idle := scheduledTaskHTTPInfo(t, f)
		priorResult = objectValue(t, idle, "LastExecutionResult")
		if idle["State"] != "Idle" || priorResult["Status"] != "Cancelled" || priorResult["Id"] != f.taskID || len(priorResult) != 6 {
			t.Fatal("cancelled result did not retain the task definition identity and fixture shape")
		}
		child := scheduledTaskHTTPChildren(t, f, run.ID, 1)[0]
		if child.State != tasks.ChildCancelled || child.ScanJobID == nil {
			t.Fatal("cancelled task lost its scan association")
		}
		job, err := f.app.library.GetJob(f.ctx, *child.ScanJobID)
		if err != nil || job.Status != "Cancelled" || job.TaskChildID != child.ID || job.LibraryID != collection.ID || job.ForceProbe {
			t.Fatal("task stop did not cancel the associated ordinary scan")
		}
		expectEmbyTextError(t, f.request(t, stop.method, stop.path, nil, f.admin), http.StatusInternalServerError, "Cannot cancel a Task unless it is in the Running state.")
	}
	scheduledTaskHTTPHistory(t, f, 3)
}

func TestHTTPLibraryRefreshDurablyQueuesEveryLibraryBeyondScannerCapacity(t *testing.T) {
	f, root, prober, gate := scheduledTaskHTTPScanFixture(t)
	// Two blocked workers plus 128 queue slots must leave one durable waiter.
	const libraryCount = 131
	libraries := make(map[string]bool, libraryCount)
	for index := 0; index < libraryCount; index++ {
		collection := scheduledTaskHTTPCreateLibrary(t, f, root, index)
		libraries[collection.ID] = true
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/emby/Library/Refresh", nil, f.admin))
	run := scheduledTaskHTTPHistory(t, f, 1)[0]
	if run.TotalChildren != libraryCount || run.Source != "compatibility" {
		t.Fatal("Library/Refresh returned before committing its complete library snapshot")
	}
	// Retain bounded state before fixture cleanup if capacity draining fails.
	// The existing completion deadline and successful path remain unchanged.
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var diagnostic string
		err := f.pool.QueryRow(ctx, `SELECT jsonb_build_object(
			'State', r.state, 'Total', r.total_children, 'Terminal', r.terminal_children,
			'Completed', r.completed_children, 'Error', r.error_message,
			'ChildStates', (SELECT jsonb_object_agg(state, total) FROM (
				SELECT state, count(*) total FROM task_run_children WHERE run_id=r.id GROUP BY state) counts),
			'Active', (SELECT COALESCE(jsonb_agg(to_jsonb(active)), '[]'::jsonb) FROM (
				SELECT c.id, c.state, c.scan_job_id, c.error_code, c.error_message,
					s.status scan_status, s.scanned, s.added, s.updated, s.error scan_error
				FROM task_run_children c LEFT JOIN scan_jobs s ON s.id=c.scan_job_id
				WHERE c.run_id=r.id AND c.state IN ('waiting','queued','running') ORDER BY c.id LIMIT 8) active)
			)::text FROM task_runs r WHERE r.id=$1`, run.ID).Scan(&diagnostic)
		if err != nil {
			t.Logf("capacity failure state could not be read: %v", err)
			return
		}
		t.Logf("capacity failure state: %s", diagnostic)
	})
	for _, child := range scheduledTaskHTTPChildren(t, f, run.ID, libraryCount) {
		if !libraries[child.LibraryID] || child.RunID != run.ID {
			t.Fatal("Library/Refresh snapshot does not cover the registered libraries")
		}
	}
	scheduledTaskHTTPWait(t, f, "full scanner queue with a durable waiting child", func() bool {
		var running, queued, waiting, linked int
		err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER (WHERE state = 'running'),
			count(*) FILTER (WHERE state = 'queued'), count(*) FILTER (WHERE state = 'waiting'),
			count(*) FILTER (WHERE scan_job_id IS NOT NULL)
			FROM task_run_children WHERE run_id = $1`, run.ID).Scan(&running, &queued, &waiting, &linked)
		if err != nil {
			t.Fatalf("read scanner capacity snapshot: %v", err)
		}
		return gate.entered.Load() == 2 && running == 2 && queued == 128 && waiting == 1 && linked == 130
	})
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/Library/Refresh", nil, f.admin))
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/ScheduledTasks/Running/"+f.taskID, nil, f.admin))
	if repeated := scheduledTaskHTTPHistory(t, f, 1)[0]; repeated.ID != run.ID {
		t.Fatal("refresh aliases and task start did not share one active execution")
	}
	gate.open()
	finished := scheduledTaskHTTPWaitRun(t, f, run.ID, tasks.RunCompleted)
	if finished.TotalChildren != libraryCount || finished.CompletedChildren != libraryCount || finished.TerminalChildren != libraryCount || finished.Added != libraryCount {
		t.Fatal("scanner capacity retries dropped a library or its scan result")
	}
	seenLibraries, seenJobs := make(map[string]bool), make(map[string]bool)
	for _, child := range scheduledTaskHTTPChildren(t, f, run.ID, libraryCount) {
		if child.State != tasks.ChildCompleted || child.ScanJobID == nil || !libraries[child.LibraryID] || seenLibraries[child.LibraryID] || seenJobs[*child.ScanJobID] {
			t.Fatal("completed refresh lost a library or reused a child scan")
		}
		seenLibraries[child.LibraryID], seenJobs[*child.ScanJobID] = true, true
		job, err := f.app.library.GetJob(f.ctx, *child.ScanJobID)
		if err != nil || job.Status != "Completed" || job.TaskChildID != child.ID || job.LibraryID != child.LibraryID || job.ForceProbe || job.Scanned != 1 || job.Added != 1 {
			t.Fatal("completed task child is not linked to its ordinary library scan")
		}
	}
	if prober.calls.Load() != libraryCount {
		t.Fatalf("refresh probed %d files, want %d", prober.calls.Load(), libraryCount)
	}
	scheduledTaskHTTPHistory(t, f, 1)
	info := scheduledTaskHTTPInfo(t, f)
	if info["State"] != "Idle" || objectValue(t, info, "LastExecutionResult")["Status"] != "Completed" {
		t.Fatal("completed Library/Refresh is missing from compatibility task history")
	}
}

type scheduledTaskHTTPGatedBody struct {
	reader  io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (body *scheduledTaskHTTPGatedBody) Read(buffer []byte) (int, error) {
	body.once.Do(func() { close(body.entered) })
	<-body.release
	return body.reader.Read(buffer)
}

func (*scheduledTaskHTTPGatedBody) Close() error { return nil }

func TestHTTPScheduledTaskTriggerRevisionConflictPreservesConcurrentNativeEdit(t *testing.T) {
	f := scheduledTaskHTTPAuthenticate(t, newServerFixture(t))
	body := &scheduledTaskHTTPGatedBody{reader: strings.NewReader(`[]`), entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(body.release) }) }
	defer release()
	request := httptest.NewRequest(http.MethodPost, "/emby/ScheduledTasks/"+f.taskID+"/Triggers", body).WithContext(f.ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Emby-Token", f.admin.Get("X-Emby-Token"))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.handler.ServeHTTP(response, request)
	}()
	// Retire the request before fixture cleanup can close the owned catalog.
	t.Cleanup(func() {
		release()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("gated compatibility request did not finish during cleanup")
		}
	})
	select {
	case <-body.entered:
	case <-done:
		t.Fatalf("compatibility request returned before reading its body: status %d", response.Code)
	case <-time.After(10 * time.Second):
		t.Fatal("compatibility request did not reach its body gate")
	}
	nativeRules := []map[string]any{{"Kind": "daily", "TimeOfDayTicks": "72000000000"}}
	native := scheduledTaskHTTPNativeTriggers(t, f, 1, "Asia/Shanghai", nativeRules)
	expectStatus(t, native, http.StatusOK)
	saved := scheduledTaskHTTPDefinition(t, f)
	if saved.Revision != 2 || saved.ScheduleTimezone != "Asia/Shanghai" || len(saved.Triggers) != 1 {
		t.Fatal("concurrent native edit did not establish its revision and timezone")
	}
	release()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("compatibility trigger replacement did not finish after release")
	}
	expectAPIError(t, response, http.StatusConflict, "revision_conflict", true)
	after := scheduledTaskHTTPDefinition(t, f)
	if after.Revision != saved.Revision || after.ScheduleTimezone != saved.ScheduleTimezone || len(after.Triggers) != 1 || after.Triggers[0].ID != saved.Triggers[0].ID {
		t.Fatal("compatibility revision conflict overwrote the concurrent native edit")
	}
	scheduledTaskHTTPAssertJSON(t, scheduledTaskHTTPInfo(t, f)["Triggers"], []map[string]any{{"Type": "DailyTrigger", "TimeOfDayTicks": int64(72000000000)}})
}

func TestHTTPScheduledTasksProjectApplicationKeyAuthorization(t *testing.T) {
	// Application-key task administration is a Goby policy. The fresh Emby
	// reference fixtures exercised user tokens and did not sample this principal.
	a := newApplicationKeyHTTPFixture(t)
	key := a.create(t, "Goby scheduled task administration")
	definition, err := a.app.taskStore.GetByKey(a.ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatalf("read application-key task definition: %v", err)
	}
	headers := http.Header{"X-Emby-Token": {key.token}}
	f := &scheduledTaskHTTPFixture{serverFixture: a.serverFixture, cookie: a.cookie, csrf: a.csrf, admin: headers, taskID: definition.ID}
	scheduledTaskHTTPWait(t, f, "application-key task manager readiness", func() bool { return f.app.taskManager.Available() })
	queryPath := "/ScheduledTasks?IsHidden=false&api_key=" + url.QueryEscape(key.token)
	items := responseArray(t, f.request(t, http.MethodGet, queryPath, nil, nil))
	if len(items) != 1 {
		t.Fatal("application-key query transport did not expose the executable task")
	}
	scheduledTaskHTTPAssertInfo(t, items[0], f.taskID)
	triggerPath := "/emby/ScheduledTasks/" + f.taskID + "/Triggers"
	startPath := "/emby/ScheduledTasks/Running/" + f.taskID
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, triggerPath, []any{}, headers))
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, startPath, nil, headers))
	run := scheduledTaskHTTPHistory(t, f, 1)[0]
	scheduledTaskHTTPWaitRun(t, f, run.ID, tasks.RunCompleted)
	saved := scheduledTaskHTTPDefinition(t, f)
	expectStatus(t, a.native(t, http.MethodPost, "/admin/v1/api-keys/"+key.id+"/revoke", map[string]any{}), http.StatusOK)
	expectEmbyTextError(t, f.request(t, http.MethodGet, queryPath, nil, nil), http.StatusUnauthorized, "Access token is invalid or expired.")
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, triggerPath, []map[string]any{{"Type": "IntervalTrigger", "IntervalTicks": int64(432000000000)}}},
		{http.MethodPost, startPath, nil},
		{http.MethodDelete, startPath, nil},
		{http.MethodPost, startPath + "/Delete", nil},
	} {
		expectEmbyTextError(t, f.request(t, route.method, route.path, route.body, headers), http.StatusUnauthorized, "Access token is invalid or expired.")
	}
	after := scheduledTaskHTTPDefinition(t, f)
	if after.Revision != saved.Revision || !reflect.DeepEqual(after.Triggers, saved.Triggers) || after.ScheduleTimezone != saved.ScheduleTimezone {
		t.Fatal("a revoked application key changed the task schedule")
	}
	if retained := scheduledTaskHTTPHistory(t, f, 1)[0]; retained.ID != run.ID || retained.State != tasks.RunCompleted {
		t.Fatal("a revoked application key admitted or changed a task execution")
	}
}
