//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/recovery"
	"github.com/moooyo/goby/internal/tasks"
)

type adminTaskBackupResultRead struct {
	actor identity.Principal
	ids   []string
}

type adminTaskBackupResultTestManager struct {
	*backupTestManager
	readMu sync.Mutex
	reads  []adminTaskBackupResultRead
	read   func(context.Context, identity.Principal, []string) (map[string]recovery.TaskBackupResultView, error)
}

func (m *adminTaskBackupResultTestManager) TaskBackupResults(ctx context.Context, actor identity.Principal, ids []string) (map[string]recovery.TaskBackupResultView, error) {
	m.readMu.Lock()
	m.reads = append(m.reads, adminTaskBackupResultRead{actor: actor, ids: append([]string(nil), ids...)})
	read := m.read
	m.readMu.Unlock()
	return read(ctx, actor, ids)
}

func (m *adminTaskBackupResultTestManager) recordedReads() []adminTaskBackupResultRead {
	m.readMu.Lock()
	defer m.readMu.Unlock()
	return append([]adminTaskBackupResultRead(nil), m.reads...)
}

func adminTaskBackupResultFixture(t *testing.T) (*serverFixture, *http.Cookie, *adminTaskBackupResultTestManager, string, []string) {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	// Keep the inserted display snapshots stable. These tests do not dispatch
	// backup work or replace the real catalog's transaction owner.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal("stop task dispatch before seeding display snapshots")
	}
	taskID := strings.Repeat("a", 32)
	ids := []string{strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("3", 32)}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO task_definitions(id,key,name,category)
		VALUES($1,$2,'Automatic backup result fixture','Backup')`, taskID, recovery.BackupTaskKey); err != nil {
		t.Fatal("insert automatic backup definition")
	}
	created := time.Date(2026, 11, 7, 7, 0, 0, 0, time.UTC)
	for index, id := range ids {
		state := "completed"
		at := created.Add(time.Duration(index) * time.Minute)
		started, finished := &at, new(time.Time)
		*finished = at.Add(time.Second)
		total, completed, unavailable := 0, 0, 0
		if index == 1 {
			total, completed, unavailable = 2, 1, 1
		}
		if index == 2 {
			state, started, finished = "pending", nil, nil
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO task_runs
			(id,task_id,state,source,task_key,task_name,created_at,started_at,finished_at,
			total_children,terminal_children,completed_children,unavailable_children)
			VALUES($1,$2,$3,'manual',$4,'Automatic backup result fixture',$5,$6,$7,$8,$8,$9,$10)`,
			id, taskID, state, recovery.BackupTaskKey, at, started, finished, total, completed, unavailable); err != nil {
			t.Fatal("insert exact backup run display snapshot")
		}
	}
	for ordinal, state := range []string{"completed", "unavailable"} {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO task_run_children
			(id,run_id,library_id,library_name,ordinal,state,created_at,finished_at)
			VALUES($1,$2,$3,$3,$4,$5,$6,$6)`, strings.Repeat(string(rune('b'+ordinal)), 32), ids[1],
			"backup-work-"+state, ordinal, state, created); err != nil {
			t.Fatal("insert recorded backup work item")
		}
	}
	verified := false
	results := map[string]recovery.TaskBackupResultView{
		ids[0]: {Status: "not_recorded"},
		ids[1]: {Status: "available", BackupId: strings.Repeat("d", 32), State: "ready", SizeBytes: "9007199254740993", SHA256: backupTestSHA, Verified: &verified},
		ids[2]: {Status: "available", BackupId: strings.Repeat("e", 32), State: "writing", SizeBytes: "0", Verified: &verified},
	}
	manager := &adminTaskBackupResultTestManager{backupTestManager: &backupTestManager{}}
	manager.read = func(_ context.Context, _ identity.Principal, exactIDs []string) (map[string]recovery.TaskBackupResultView, error) {
		selected := make(map[string]recovery.TaskBackupResultView, len(exactIDs))
		for _, id := range exactIDs {
			if result, found := results[id]; found {
				selected[id] = result
			}
		}
		return selected, nil
	}
	f.app.recovery = manager
	return f, cookie, manager, taskID, ids
}

func adminTaskBackupResultItems(t *testing.T, object map[string]any) []map[string]any {
	t.Helper()
	values, ok := object["Items"].([]any)
	if !ok {
		t.Fatal("native task page did not contain an item array")
	}
	items := make([]map[string]any, len(values))
	for index, value := range values {
		item, ok := value.(map[string]any)
		if !ok {
			t.Fatal("native task page item was not an object")
		}
		items[index] = item
	}
	return items
}

func assertAdminTaskBackupResultRead(t *testing.T, manager *adminTaskBackupResultTestManager, expected []string) {
	t.Helper()
	reads := manager.recordedReads()
	if len(reads) == 0 {
		t.Fatal("native task request omitted its optional result read")
	}
	read := reads[len(reads)-1]
	if !reflect.DeepEqual(read.ids, expected) {
		t.Fatalf("result lookup used IDs %v, want exact IDs %v", read.ids, expected)
	}
	if read.actor.Kind != "admin" || read.actor.User.ID == "" || read.actor.SessionID == "" ||
		!read.actor.User.IsAdministrator || read.actor.ApplicationKeyID != 0 || read.actor.ClientSessionID != "" {
		t.Fatal("result lookup did not receive the current native administrator principal")
	}
}

func TestHTTPAdminTaskBackupResultsProjectExactNativeRuns(t *testing.T) {
	f, cookie, manager, taskID, ids := adminTaskBackupResultFixture(t)
	base := "/admin/v1/tasks/" + taskID
	response := f.request(t, http.MethodGet, base+"/runs?StartIndex=1&Limit=1", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	items := adminTaskBackupResultItems(t, page)
	if len(items) != 1 || items[0]["Id"] != ids[1] || page["StartIndex"] != float64(1) ||
		page["Limit"] != float64(1) || page["TotalRecordCount"] != float64(3) {
		t.Fatal("backup projection changed the exact bounded run page")
	}
	assertAdminTaskBackupResultRead(t, manager, []string{ids[1]})
	result := objectValue(t, items[0], "BackupResult")
	if result["Status"] != "available" || result["BackupId"] != strings.Repeat("d", 32) || result["State"] != "ready" ||
		result["SizeBytes"] != "9007199254740993" || result["SHA256"] != backupTestSHA || result["Verified"] != false || len(result) != 6 {
		t.Fatalf("result projection lost exact decimal size, digest, or false verification: %#v", result)
	}
	if items[0]["State"] != "completed" || items[0]["Source"] != "manual" || items[0]["CompletedChildren"] != float64(1) || items[0]["UnavailableChildren"] != float64(1) {
		t.Fatal("backup metadata replaced the task's recorded state or source")
	}
	adminTaskHTTPNoPrivateFields(t, page)

	response = f.request(t, http.MethodGet, "/admin/v1/task-runs/"+ids[1]+"?StartIndex=1&Limit=1", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	detail := jsonObject(t, response)
	run := objectValue(t, detail, "Run")
	if !reflect.DeepEqual(objectValue(t, run, "BackupResult"), result) || run["Id"] != ids[1] {
		t.Fatal("run detail projected a different backup result")
	}
	children := objectValue(t, detail, "Children")
	childItems := adminTaskBackupResultItems(t, children)
	if children["StartIndex"] != float64(1) || children["Limit"] != float64(1) || children["TotalRecordCount"] != float64(2) ||
		len(childItems) != 1 || childItems[0]["State"] != "unavailable" {
		t.Fatal("result projection changed the independent work-item page")
	}
	assertAdminTaskBackupResultRead(t, manager, []string{ids[1]})

	for _, target := range []string{base, "/admin/v1/tasks"} {
		response = f.request(t, http.MethodGet, target, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		object := jsonObject(t, response)
		var definition map[string]any
		if target == base {
			definition = objectValue(t, object, "Task")
		} else {
			for _, item := range adminTaskBackupResultItems(t, object) {
				if item["Id"] == taskID {
					definition = item
				}
			}
			if definition == nil {
				t.Fatal("task list omitted the automatic backup definition")
			}
		}
		current, last := objectValue(t, definition, "CurrentRun"), objectValue(t, definition, "LastRun")
		if current["Id"] != ids[2] || objectValue(t, current, "BackupResult")["State"] != "writing" ||
			last["Id"] != ids[1] || !reflect.DeepEqual(objectValue(t, last, "BackupResult"), result) {
			t.Fatal("current or last task snapshot dropped its exact backup projection")
		}
		assertAdminTaskBackupResultRead(t, manager, []string{ids[2], ids[1]})
	}

	response = f.request(t, http.MethodGet, base+"/runs?StartIndex=2&Limit=1", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	unassociated := adminTaskBackupResultItems(t, jsonObject(t, response))[0]
	if projection := objectValue(t, unassociated, "BackupResult"); projection["Status"] != "not_recorded" || len(projection) != 1 {
		t.Fatal("an unassociated run borrowed another backup's metadata")
	}
	assertAdminTaskBackupResultRead(t, manager, []string{ids[0]})

	scan, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatal("read ordinary scan task")
	}
	scanID := strings.Repeat("4", 32)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,task_key,task_name,finished_at)
		VALUES($1,$2,'completed','manual',$3,'Ordinary scan',clock_timestamp())`, scanID, scan.ID, tasks.LibraryScanKey); err != nil {
		t.Fatal("insert ordinary scan history")
	}
	readsBefore := len(manager.recordedReads())
	for _, target := range []string{"/admin/v1/tasks/" + scan.ID + "/runs", "/admin/v1/task-runs/" + scanID} {
		response = f.request(t, http.MethodGet, target, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		if strings.Contains(response.Body.String(), "BackupResult") || len(manager.recordedReads()) != readsBefore {
			t.Fatal("an ordinary task was associated with an automatic backup")
		}
	}

	// Older recovery adapters and fixtures need no new method or response field.
	f.app.recovery = &backupTestManager{}
	for _, target := range []string{base + "/runs", "/admin/v1/task-runs/" + ids[1], base, "/admin/v1/tasks"} {
		response = f.request(t, http.MethodGet, target, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		if strings.Contains(response.Body.String(), "BackupResult") {
			t.Fatal("an older recovery adapter gained a fabricated result projection")
		}
	}
}

func TestHTTPAdminTaskBackupResultsPreserveTaskStateOnProjectionFailure(t *testing.T) {
	f, cookie, manager, taskID, ids := adminTaskBackupResultFixture(t)
	manager.read = func(context.Context, identity.Principal, []string) (map[string]recovery.TaskBackupResultView, error) {
		return nil, errors.New("private backup storage failure")
	}
	for _, target := range []string{"/admin/v1/tasks/" + taskID + "/runs?StartIndex=1&Limit=1", "/admin/v1/task-runs/" + ids[1]} {
		response := f.request(t, http.MethodGet, target, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		object := jsonObject(t, response)
		var run map[string]any
		if value, found := object["Run"].(map[string]any); found {
			run = value
		} else {
			run = adminTaskBackupResultItems(t, object)[0]
		}
		projection := objectValue(t, run, "BackupResult")
		if run["State"] != "completed" || run["UnavailableChildren"] != float64(1) ||
			projection["Status"] != "unavailable" || len(projection) != 1 || strings.Contains(response.Body.String(), "private backup storage failure") {
			t.Fatal("storage failure replaced recorded task state or exposed private error details")
		}
	}
	for _, status := range []string{"deleted", "deleting", "missing", "mismatch"} {
		manager.read = func(context.Context, identity.Principal, []string) (map[string]recovery.TaskBackupResultView, error) {
			return map[string]recovery.TaskBackupResultView{ids[1]: {Status: status}}, nil
		}
		response := f.request(t, http.MethodGet, "/admin/v1/task-runs/"+ids[1], nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		projection := objectValue(t, objectValue(t, jsonObject(t, response), "Run"), "BackupResult")
		if projection["Status"] != status || len(projection) != 1 {
			t.Fatal("unavailable metadata gained a fabricated size or verification value")
		}
	}

	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	manager.read = func(context.Context, identity.Principal, []string) (map[string]recovery.TaskBackupResultView, error) {
		cancel()
		return nil, context.Canceled
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/task-runs/"+ids[1], nil).WithContext(ctx)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	if !errors.Is(ctx.Err(), context.Canceled) || response.Body.Len() != 0 {
		t.Fatal("request cancellation exposed previously read task or backup data")
	}
}

func TestHTTPAdminTaskBackupResultsNeverDowngradeAuthorityErrors(t *testing.T) {
	f, cookie, manager, taskID, ids := adminTaskBackupResultFixture(t)
	for _, scenario := range []struct {
		name string
		err  error
		code int
	}{
		{"revoked", identity.ErrUnauthorized, http.StatusUnauthorized},
		{"invalid", identity.ErrInvalidCredentials, http.StatusUnauthorized},
		{"audience", identity.ErrClientSessionForbidden, http.StatusForbidden},
		{"deadline", context.DeadlineExceeded, http.StatusServiceUnavailable},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			manager.read = func(context.Context, identity.Principal, []string) (map[string]recovery.TaskBackupResultView, error) {
				return nil, scenario.err
			}
			for _, target := range []string{"/admin/v1/tasks/" + taskID + "/runs?StartIndex=1&Limit=1", "/admin/v1/task-runs/" + ids[1], "/admin/v1/tasks/" + taskID, "/admin/v1/tasks"} {
				response := f.request(t, http.MethodGet, target, nil, nil, cookie)
				expectStatus(t, response, scenario.code)
				object := jsonObject(t, response)
				if _, found := object["Error"].(map[string]any); !found || object["Run"] != nil || object["Items"] != nil || object["Task"] != nil || object["Children"] != nil {
					t.Fatal("an authority or deadline error was downgraded to a successful task projection")
				}
			}
		})
	}
}
