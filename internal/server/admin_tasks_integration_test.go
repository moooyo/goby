//go:build linux

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func adminTaskHTTPFixture(t *testing.T) (*serverFixture, *http.Cookie, string, string) {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	response := f.request(t, http.MethodGet, "/admin/v1/tasks", nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	items, ok := page["Items"].([]any)
	if !ok || len(items) != 1 || page["TotalRecordCount"] != float64(1) {
		t.Fatal("task startup did not register exactly one executable library task")
	}
	definition, ok := items[0].(map[string]any)
	if !ok || definition["Key"] != "library.scan" || definition["Enabled"] != true || definition["Revision"] != "1" {
		t.Fatal("native task definition lost its stable public metadata")
	}
	id := stringValue(t, definition, "Id")
	if len(id) != 32 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("native task identity or caching contract is invalid")
	}
	return f, cookie, csrf, id
}

func adminTaskHTTPRaw(f *serverFixture, cookie *http.Cookie, csrf, method, target, body, contentType string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(f.ctx)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func adminTaskHTTPNoPrivateFields(t *testing.T, value any) {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		for name, child := range value {
			switch strings.ToLower(name) {
			case "actoruserid", "actorsessionid", "actorkind", "taskkey", "taskembykey", "triggerid", "triggerrevision", "request_fingerprint":
				t.Errorf("native task response exposed internal field %s", name)
			}
			if strings.Contains(name, "_") {
				t.Errorf("native task response has non-PascalCase field %s", name)
			}
			adminTaskHTTPNoPrivateFields(t, child)
		}
	case []any:
		for _, child := range value {
			adminTaskHTTPNoPrivateFields(t, child)
		}
	}
}

func TestHTTPAdminTasksAuthenticateValidateAndRetainDurableRequestReceipts(t *testing.T) {
	f, cookie, csrf, taskID := adminTaskHTTPFixture(t)
	base := "/admin/v1/tasks/" + taskID
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/tasks", nil, nil), http.StatusUnauthorized)
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	embyHeaders := http.Header{"X-Emby-Token": {stringValue(t, emby, "AccessToken")}}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/tasks", nil, embyHeaders), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPost, base+"/runs", map[string]any{}, nil, cookie), http.StatusForbidden)
	for _, target := range []string{"/admin/v1/tasks/not-an-id", "/admin/v1/tasks/" + strings.ToUpper(taskID), base + "?Unexpected=1",
		base + "/runs?Limit=0", base + "/runs?Limit=201", base + "/runs?StartIndex=-1", base + "/runs?StartIndex=2147483648", base + "/runs?Limit=1&Limit=2"} {
		// IDs containing digits alone are unchanged by case conversion.
		if target == base {
			continue
		}
		expectStatus(t, f.request(t, http.MethodGet, target, nil, nil, cookie), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/tasks/"+strings.Repeat("0", 32), nil, nil, cookie), http.StatusNotFound)
	for _, body := range []string{`{"RequestId":"one","RequestId":"two"}`, `{"RequestId":null}`, `{"RequestId":9}`, `{"requestId":"one"}`, `{"ForceProbe":true}`} {
		expectStatus(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPost, base+"/runs", body, "application/json"), http.StatusBadRequest)
	}
	expectStatus(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPost, base+"/runs?RequestId=override", `{}`, "application/json"), http.StatusBadRequest)
	expectStatus(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPost, base+"/runs", `{}`, "text/plain"), http.StatusUnsupportedMediaType)
	headers := http.Header{"X-CSRF-Token": {csrf}}
	first := f.request(t, http.MethodPost, base+"/runs", map[string]any{"RequestId": "native-retry"}, headers, cookie)
	expectStatus(t, first, http.StatusAccepted)
	started := jsonObject(t, first)
	run := objectValue(t, started, "Run")
	runID := stringValue(t, run, "Id")
	if started["Admitted"] != true || run["RequestId"] != "native-retry" || run["TaskId"] != taskID || run["TotalChildren"] != float64(0) {
		t.Fatal("native task admission did not preserve the empty library snapshot and request ID")
	}
	adminTaskHTTPNoPrivateFields(t, started)
	repeated := f.request(t, http.MethodPost, base+"/runs", map[string]any{"RequestId": "native-retry"}, headers, cookie)
	expectStatus(t, repeated, http.StatusAccepted)
	receipt := jsonObject(t, repeated)
	if receipt["Admitted"] != false || objectValue(t, receipt, "Run")["Id"] != runID {
		t.Fatal("an uncertain native HTTP retry created a second task execution")
	}
	detail := f.request(t, http.MethodGet, "/admin/v1/task-runs/"+runID+"?Limit=1&StartIndex=0", nil, nil, cookie)
	expectStatus(t, detail, http.StatusOK)
	object := jsonObject(t, detail)
	children := objectValue(t, object, "Children")
	if children["TotalRecordCount"] != float64(0) || len(children["Items"].([]any)) != 0 || children["Limit"] != float64(1) {
		t.Fatal("native run detail did not return an empty bounded child page")
	}
	adminTaskHTTPNoPrivateFields(t, object)
	list := f.request(t, http.MethodGet, base+"/runs?StartIndex=0&Limit=1", nil, nil, cookie)
	expectStatus(t, list, http.StatusOK)
	page := jsonObject(t, list)
	if page["TotalRecordCount"] != float64(1) || len(page["Items"].([]any)) != 1 {
		t.Fatal("native run history duplicated an idempotent admission")
	}
	cancel := f.request(t, http.MethodPost, "/admin/v1/task-runs/"+runID+"/cancel", map[string]any{}, headers, cookie)
	expectStatus(t, cancel, http.StatusAccepted)
	if objectValue(t, jsonObject(t, cancel), "Run")["Id"] != runID {
		t.Fatal("terminal run cancellation changed its identity")
	}
}

func TestHTTPAdminTaskSchedulesUseExactTicksAndRevisionConflicts(t *testing.T) {
	f, cookie, csrf, taskID := adminTaskHTTPFixture(t)
	base := "/admin/v1/tasks/" + taskID + "/triggers"
	headers := http.Header{"X-CSRF-Token": {csrf}}
	triggers := []map[string]any{{"Kind": "interval", "IntervalTicks": "100000000", "MaxRuntimeTicks": "0"},
		{"Kind": "daily", "TimeOfDayTicks": "90000000000"}, {"Kind": "startup"}}
	previewBody := map[string]any{"ScheduleTimezone": "Asia/Shanghai", "Triggers": triggers}
	preview := f.request(t, http.MethodPost, base+"/preview", previewBody, headers, cookie)
	expectStatus(t, preview, http.StatusOK)
	previewObject := jsonObject(t, preview)
	if _, err := time.Parse(time.RFC3339Nano, stringValue(t, previewObject, "ServerTime")); err != nil {
		t.Fatal("schedule preview omitted its database clock")
	}
	items, ok := previewObject["Items"].([]any)
	if !ok || len(items) != 3 {
		t.Fatal("schedule preview omitted a rule")
	}
	for index, raw := range items {
		item := raw.(map[string]any)
		occurrences := item["Occurrences"].([]any)
		if item["Index"] != float64(index) || index < 2 && len(occurrences) == 0 || index == 2 && (len(occurrences) != 0 || item["Event"] == nil) {
			t.Fatal("schedule preview lost timed occurrences or startup event semantics")
		}
	}
	definition := f.request(t, http.MethodGet, "/admin/v1/tasks/"+taskID, nil, nil, cookie)
	expectStatus(t, definition, http.StatusOK)
	if objectValue(t, jsonObject(t, definition), "Task")["Revision"] != "1" {
		t.Fatal("read-only schedule preview changed the task revision")
	}
	body := map[string]any{"Revision": "1", "ScheduleTimezone": "Asia/Shanghai", "Triggers": triggers}
	replaced := f.request(t, http.MethodPut, base, body, headers, cookie)
	expectStatus(t, replaced, http.StatusOK)
	updated := objectValue(t, jsonObject(t, replaced), "Task")
	if updated["Revision"] != "2" || updated["ScheduleTimezone"] != "Asia/Shanghai" || len(updated["Triggers"].([]any)) != 3 || updated["NextRunAt"] == nil {
		t.Fatal("schedule replacement did not commit one revision with all trigger rows")
	}
	adminTaskHTTPNoPrivateFields(t, updated)
	expectStatus(t, f.request(t, http.MethodPut, base, body, headers, cookie), http.StatusConflict)
	bad := `{"Revision":"2","ScheduleTimezone":"UTC","Triggers":[{"Kind":"interval","IntervalTicks":"10000000","IntervalTicks":"20000000"}]}`
	expectStatus(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPut, base, bad, "application/json"), http.StatusBadRequest)
	cleared := f.request(t, http.MethodPut, base, map[string]any{"Revision": "2", "ScheduleTimezone": "UTC", "Triggers": []any{}}, headers, cookie)
	expectStatus(t, cleared, http.StatusOK)
	if objectValue(t, jsonObject(t, cleared), "Task")["NextRunAt"] != nil {
		t.Fatal("clearing task triggers retained a next occurrence")
	}
}

func TestHTTPAdminTaskShutdownFencesAdmissionBeforeCatalogClose(t *testing.T) {
	f, cookie, csrf, taskID := adminTaskHTTPFixture(t)
	f.app.taskManager.BeginClose()
	if f.app.taskManager.Available() || !f.app.library.Available() {
		t.Fatal("task shutdown did not fence admission before releasing the catalog")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/readyz", nil, nil), http.StatusServiceUnavailable)
	expectStatus(t, f.request(t, http.MethodGet, "/healthz", nil, nil), http.StatusOK)
	response := f.request(t, http.MethodPost, "/admin/v1/tasks/"+taskID+"/runs", map[string]any{}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusServiceUnavailable)
	var count int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_runs").Scan(&count); err != nil || count != 0 {
		t.Fatal("shutdown admitted a task run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := f.app.Close(ctx); err != nil {
		t.Fatalf("close server task lifecycle: %T", err)
	}
	if f.app.library.Available() {
		t.Fatal("server shutdown returned before catalog ownership was released")
	}
}

func TestHTTPAdminTaskResponsesDoNotExposePersistedActorFields(t *testing.T) {
	f, cookie, csrf, taskID := adminTaskHTTPFixture(t)
	response := f.request(t, http.MethodPost, "/admin/v1/tasks/"+taskID+"/runs", map[string]any{}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, response, http.StatusAccepted)
	var object map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &object); err != nil {
		t.Fatal(err)
	}
	runID := stringValue(t, objectValue(t, object, "Run"), "Id")
	var persistedActor string
	if err := f.pool.QueryRow(f.ctx, "SELECT actor_session_id FROM task_runs WHERE id = $1", runID).Scan(&persistedActor); err != nil || persistedActor == "" {
		t.Fatal("task fixture did not retain internal actor provenance")
	}
	if strings.Contains(response.Body.String(), persistedActor) {
		t.Fatal("native admission exposed its actor credential ID")
	}
	detail := f.request(t, http.MethodGet, "/admin/v1/task-runs/"+runID, nil, nil, cookie)
	expectStatus(t, detail, http.StatusOK)
	if strings.Contains(detail.Body.String(), persistedActor) {
		t.Fatal("native run detail exposed persisted credential provenance")
	}
	adminTaskHTTPNoPrivateFields(t, jsonObject(t, detail))
}
