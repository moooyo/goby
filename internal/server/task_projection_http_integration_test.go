//go:build linux

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

func TestHTTPTaskSummariesAndTargetsKeepTheirProjectionBoundaries(t *testing.T) {
	f, cookie, _, taskID := adminTaskHTTPFixtureForKey(t, library.TaskIntroAnalysisKey)
	login := loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "task-projection-controller")
	runID := strings.Repeat("c", 32)
	// The database validates selection shape, while complete Run decoding also
	// validates its element types. Display reads do not consume this selection.
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO task_runs(id,task_id,state,source,task_key,task_emby_key,task_name,
		actor_kind,actor_user_id,actor_session_id,analysis_input,analysis_config_fingerprint,
		created_at,started_at,finished_at,error_code,error_message)
		SELECT $1,id,'failed','manual',key,emby_key,name,'admin','fixture-actor','fixture-session',
		'{"ItemIds":[7]}'::jsonb,repeat('b',64),clock_timestamp()-interval '2 seconds',
		clock_timestamp()-interval '1 second',clock_timestamp(),'fixture_failure','Retained task failure'
		FROM task_definitions WHERE id=$2`, runID, taskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.taskStore.GetRun(f.ctx, runID); err == nil {
		t.Fatal("complete execution read accepted a malformed selection")
	}
	base := "/admin/v1/tasks/" + taskID
	for _, path := range []string{"/admin/v1/tasks", base, base + "/runs?Limit=1", "/admin/v1/task-runs/" + runID} {
		response := f.request(t, http.MethodGet, path, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		adminTaskHTTPNoPrivateFields(t, jsonObject(t, response))
		if strings.Contains(response.Body.String(), "AnalysisInput") || strings.Contains(response.Body.String(), "analysis_input") {
			t.Fatal("display response exposed an execution selection")
		}
	}
	detail := jsonObject(t, f.request(t, http.MethodGet, base, nil, nil, cookie))
	last := objectValue(t, objectValue(t, detail, "Task"), "LastRun")
	if last["Id"] != runID || last["State"] != "failed" || last["ErrorMessage"] != "Retained task failure" || last["ScheduledFor"] != nil {
		t.Fatal("task summary lost its terminal state, error, or nullable time")
	}
	page := jsonObject(t, f.request(t, http.MethodGet, base+"/runs?StartIndex=20&Limit=1", nil, nil, cookie))
	if page["TotalRecordCount"] != float64(1) || len(page["Items"].([]any)) != 0 {
		t.Fatal("empty task summary page lost its total")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/ScheduledTasks", nil, login.headers), http.StatusOK)
	compat := jsonObject(t, f.request(t, http.MethodGet, "/emby/ScheduledTasks/"+taskID, nil, login.headers))
	result := objectValue(t, compat, "LastExecutionResult")
	if compat["State"] != "Idle" || result["Id"] != taskID || result["Key"] != tasks.CompatibilityKey(library.TaskIntroAnalysisKey) ||
		result["Status"] != "Failed" || result["ErrorMessage"] != "Retained task failure" {
		t.Fatal("compatibility task summary lost definition identity or the last failure")
	}
	// A malformed selected timestamp still fails display decoding. A target
	// lookup omits the nested Run and reaches the unchanged body validation.
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_runs SET created_at='infinity' WHERE id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, base, nil, nil, cookie), http.StatusInternalServerError)
	triggerPath := "/emby/ScheduledTasks/" + taskID + "/Triggers"
	expectStatus(t, f.request(t, http.MethodPost, triggerPath, nil, login.headers), http.StatusUnsupportedMediaType)
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET emby_key='UnpublishedTask' WHERE id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, triggerPath},
		{http.MethodPost, "/emby/ScheduledTasks/Running/" + taskID},
		{http.MethodDelete, "/emby/ScheduledTasks/Running/" + taskID},
	} {
		expectStatus(t, f.request(t, request.method, request.path, nil, login.headers), http.StatusNotFound)
	}
}
