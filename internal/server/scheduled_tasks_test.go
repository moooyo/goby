package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/tasks"
)

func TestScheduledTaskRouteTableBuildsWithoutDatabase(t *testing.T) {
	app := &Server{}
	// Construct the complete production route table before acquiring any pool
	// or fixture cleanup resource. Pattern conflicts must fail directly here.
	if handler := app.Handler(); handler == nil {
		t.Fatal("server constructed a nil HTTP handler")
	}
	mux := http.NewServeMux()
	app.registerScheduledTaskRoutes(mux)
	const id = "0123456789abcdef0123456789abcdef"
	for _, test := range []struct{ method, path, pattern string }{
		{http.MethodGet, "/sChEdUlEdTaSkS", "GET /emby/ScheduledTasks"},
		{http.MethodGet, "/EMBY/SCHEDULEDTASKS/" + id, "GET /emby/ScheduledTasks/{id}"},
		{http.MethodPost, "/scheduledtasks/" + id + "/triggers", "POST /emby/ScheduledTasks/{path...}"},
		{http.MethodPost, "/EmBy/scheduledtasks/running/" + id, "POST /emby/ScheduledTasks/Running/{id}"},
		{http.MethodDelete, "/ScheduledTasks/Running/" + id, "DELETE /emby/ScheduledTasks/Running/{id}"},
		{http.MethodPost, "/ScheduledTasks/rUnNiNg/" + id + "/dElEtE", "POST /emby/ScheduledTasks/Running/{id}/Delete"},
		{http.MethodPost, "/ScheduledTasks/Running/Triggers", "POST /emby/ScheduledTasks/Running/{id}"},
	} {
		request := compatibilityNamespace(httptest.NewRequest(test.method, test.path, nil))
		_, pattern := mux.Handler(request)
		if pattern != test.pattern {
			t.Errorf("%s %s selected %q, want %q", test.method, test.path, pattern, test.pattern)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("valid scheduled-task alias did not reach authentication: %s %s returned %d", test.method, test.path, response.Code)
		}
		if strings.HasSuffix(strings.ToLower(test.path), "/triggers") && !strings.Contains(test.path, "/Running/") && request.PathValue("id") != id {
			t.Error("trigger dispatcher did not retain the original task identifier")
		}
	}
}

func TestScheduledTaskPostDispatcherRejectsMalformedTailsWithoutDatabase(t *testing.T) {
	app := &Server{}
	mux := http.NewServeMux()
	app.registerScheduledTaskRoutes(mux)
	for _, path := range []string{
		"/ScheduledTasks/task", "/ScheduledTasks/task/Unknown", "/ScheduledTasks/task/Triggers/extra",
		"/ScheduledTasks/task%2Fother/Triggers", "/ScheduledTasks/task%5Cother/Triggers",
		"/ScheduledTasks/task%00/Triggers", "/ScheduledTasks/task%0A/Triggers",
		"/ScheduledTasks/task/%2FTriggers", "/ScheduledTasks/task/%54riggers%00",
	} {
		request := compatibilityNamespace(httptest.NewRequest(http.MethodPost, path, nil))
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound || response.Body.String() != "Task not found" {
			t.Errorf("invalid trigger tail reached authentication or business handling: %s returned %d", path, response.Code)
		}
	}
}

func waitScheduledRefreshOwnedScanJob(t *testing.T, f *serverFixture, libraryID string, priorRunIDs []string) string {
	t.Helper()
	var count int
	var runID, childID string
	var matching bool
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*), COALESCE(min(r.id), ''), COALESCE(min(c.id), ''),
		COALESCE(bool_and(r.source = 'compatibility' AND r.total_children = 1 AND c.library_id = $2), false)
		FROM task_runs r JOIN task_definitions d ON d.id = r.task_id
		JOIN task_run_children c ON c.run_id = r.id
		WHERE d.key = 'library.scan' AND NOT (r.id = ANY($1::text[]))`, priorRunIDs, libraryID).
		Scan(&count, &runID, &childID, &matching); err != nil || count != 1 || runID == "" || childID == "" || !matching {
		t.Fatalf("refresh 204 did not commit exactly its library snapshot child: count=%d matching=%v error=%v", count, matching, err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var jobID string
		if err := f.pool.QueryRow(ctx, `SELECT COALESCE((SELECT j.id FROM task_run_children c
			JOIN scan_jobs j ON j.id = c.scan_job_id AND j.task_child_id = c.id
			WHERE c.id = $1 AND c.run_id = $2 AND c.library_id = $3 AND j.library_id = $3 AND NOT j.force_probe), '')`,
			childID, runID, libraryID).Scan(&jobID); err != nil {
			t.Fatalf("read the acknowledged refresh child's owned scan job: %v", err)
		}
		if jobID != "" {
			return jobID
		}
		select {
		case <-ctx.Done():
			t.Fatal("acknowledged refresh child did not receive a cache-aware owned scan job")
		case <-ticker.C:
		}
	}
}

func scheduledTaskFixtureBody(t *testing.T, name string, request bool) json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "compatibility", "fixtures", "reference", "emby-4.9.5.0",
		"scheduled-tasks-fresh-m5f-"+name+".json"))
	if err != nil {
		t.Fatalf("read task contract fixture: %v", err)
	}
	var record struct {
		Request  struct{ Body json.RawMessage }
		Response struct{ Body json.RawMessage }
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode task contract fixture: %v", err)
	}
	if request {
		return record.Request.Body
	}
	return record.Response.Body
}

func TestEmbyScheduledTaskTriggersPreserveObservedArrayShapes(t *testing.T) {
	for _, name := range []string{"trigger-clear", "trigger-original", "trigger-duplicate-interval", "trigger-daily", "trigger-startup"} {
		t.Run(name, func(t *testing.T) {
			body := scheduledTaskFixtureBody(t, name, true)
			rules, err := parseEmbyTaskTriggers(body, "UTC")
			if err != nil {
				t.Fatalf("reject observed supported trigger shape: %v", err)
			}
			result := make([]map[string]any, 0, len(rules))
			for _, rule := range rules {
				if rule.AnchorAt != nil || rule.Timezone != "" {
					t.Fatal("compatibility parser supplied client-owned anchors or per-rule zones")
				}
				result = append(result, embyTaskTriggerDTO(tasks.Trigger{Kind: string(rule.Kind), IntervalTicks: rule.IntervalTicks,
					TimeOfDayTicks: rule.TimeOfDayTicks, DayOfWeek: rule.DayOfWeek, MaxRuntimeTicks: rule.MaxRuntimeTicks}))
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if json.Unmarshal(body, &want) != nil || json.Unmarshal(encoded, &got) != nil || !reflect.DeepEqual(want, got) {
				t.Fatal("trigger DTO changed the observed array, duplicate rules, numeric ticks, or optional fields")
			}
		})
	}
	for _, name := range []string{"trigger-unknown", "trigger-mixed", "trigger-system-event"} {
		// The reference accepts SystemEventTrigger. Goby deliberately rejects
		// that unsupported executor; unknown/mixed rejection is observed.
		t.Run(name, func(t *testing.T) {
			rules, err := parseEmbyTaskTriggers(scheduledTaskFixtureBody(t, name, true), "UTC")
			if !errors.Is(err, errUnsupportedTaskTrigger) || rules != nil {
				t.Fatal("unsupported trigger input retained a valid prefix or changed its rejection")
			}
		})
	}
}

func TestEmbyScheduledTaskTriggerTickAndCalendarBoundaries(t *testing.T) {
	for _, ticks := range []int64{tasks.ScheduleMinIntervalTicks, 9007199254740993, tasks.ScheduleMaxDurationTicks} {
		body := `[{"Type":"IntervalTrigger","IntervalTicks":` + strconv.FormatInt(ticks, 10) + `}]`
		rules, err := parseEmbyTaskTriggers([]byte(body), "UTC")
		if err != nil || len(rules) != 1 || rules[0].IntervalTicks == nil || *rules[0].IntervalTicks != ticks {
			t.Fatalf("checked integer ticks lost precision or rejected a supported bound: %v", err)
		}
	}
	for _, value := range []string{`"10000000"`, "1.0", "1e7", "null", "true", "-1", "9999999",
		"92233720368547759", "9223372036854775808"} {
		if _, err := parseEmbyTaskTriggers([]byte(`[{"Type":"IntervalTrigger","IntervalTicks":`+value+`}]`), "UTC"); err == nil {
			t.Errorf("accepted invalid or overflowing interval tick representation %s", value)
		}
	}
	for _, body := range []string{
		`[{"Type":"DailyTrigger","TimeOfDayTicks":864000000000}]`,
		`[{"Type":"DailyTrigger","TimeOfDayTicks":-1}]`,
		`[{"Type":"DailyTrigger","TimeOfDayTicks":0,"MaxRuntimeTicks":9999999}]`,
		`[{"Type":"StartupTrigger","IntervalTicks":10000000}]`,
		`[{"Type":"IntervalTrigger"}]`, `[{"Type":"DailyTrigger"}]`,
		`[{"Type":"WeeklyTrigger","TimeOfDayTicks":0}]`,
		`[{"Type":"DailyTrigger","TimeOfDayTicks":0,"DayOfWeek":"Monday"}]`,
		`[{"Type":"DailyTrigger","TimeOfDayTicks":0,"Timezone":"Asia/Shanghai"}]`,
	} {
		if _, err := parseEmbyTaskTriggers([]byte(body), "UTC"); err == nil {
			t.Errorf("accepted an inapplicable or out-of-range trigger: %s", body)
		}
	}
	// Weekly naming derives from the SDK model, and numeric days are a Goby
	// tolerance. Neither is a fresh-reference weekly write observation.
	for day, name := range embyTaskWeekdays {
		for _, input := range []string{strconv.Quote(name), strconv.Itoa(day)} {
			body := `[{"Type":"WeeklyTrigger","TimeOfDayTicks":863999999999,"DayOfWeek":` + input + `,"MaxRuntimeTicks":0}]`
			rules, err := parseEmbyTaskTriggers([]byte(body), "Asia/Shanghai")
			if err != nil || len(rules) != 1 || rules[0].DayOfWeek == nil || *rules[0].DayOfWeek != day || rules[0].Timezone != "" {
				t.Fatal("weekly input or definition-owned timezone mapping failed")
			}
		}
	}
	for _, value := range []string{`"monday"`, `"Funday"`, "-1", "7", "1.0", "null"} {
		if _, err := embyTaskWeekday(json.RawMessage(value)); err == nil {
			t.Errorf("accepted an invalid weekday %s", value)
		}
	}
	if _, err := parseEmbyTaskTriggers([]byte(`[{"Type":"DailyTrigger","TimeOfDayTicks":0}]`), "Local"); err == nil {
		t.Fatal("compatibility calendar guessed an implicit machine timezone")
	}
}

func TestEmbyScheduledTaskTriggerBodyAndQueryValidation(t *testing.T) {
	for _, body := range []string{"", "null", "{}", "[null]", "[] []", `[{"Type":"StartupTrigger","type":"DailyTrigger"}]`,
		`[{"Type":"StartupTrigger","Ignored":1}]`, `[{"Type":3}]`, "[\xff]",
		"[" + strings.Repeat(`{"Type":"StartupTrigger"},`, tasks.MaxTriggers) + `{"Type":"StartupTrigger"}]`,
		strings.Repeat(" ", maxAdminTaskBodyBytes) + "[]"} {
		if _, err := parseEmbyTaskTriggers([]byte(body), "UTC"); err == nil {
			t.Errorf("accepted malformed, duplicate, oversized, or unsupported body %q", body[:min(len(body), 140)])
		}
	}
	request := httptest.NewRequest("GET", "/emby/ScheduledTasks?ishidden=false&IsEnabled=true&api_key=synthetic", nil)
	options, err := parseEmbyTaskQuery(request, true)
	if err != nil || options.IsHidden == nil || *options.IsHidden || options.IsEnabled == nil || !*options.IsEnabled {
		t.Fatal("canonical boolean task filters or credential query transport failed")
	}
	for _, query := range []string{"?IsEnabled=1", "?IsHidden=True", "?IsHidden=false&ishidden=true", "?Limit=1", "?IsEnabled=%ff", "?"} {
		if _, err := parseEmbyTaskQuery(httptest.NewRequest("GET", "/emby/ScheduledTasks"+query, nil), true); err == nil {
			t.Errorf("accepted an invalid task filter %s", query)
		}
	}
	if _, err := parseEmbyTaskQuery(request, false); err == nil {
		t.Fatal("task mutation accepted list filters")
	}
	if _, err := parseEmbyTaskQuery(httptest.NewRequest("POST", "/emby/ScheduledTasks/Running/id?api_key=synthetic", nil), false); err != nil {
		t.Fatal("task mutation rejected existing query bearer authentication")
	}
}

func TestEmbyScheduledTaskDTOUsesDefinitionIdentityAndRetainsTerminalResult(t *testing.T) {
	zone := time.FixedZone("synthetic-offset", 8*3600)
	started := time.Date(2026, 9, 10, 8, 1, 2, 0, zone)
	finished := started.Add(time.Second)
	definition := tasks.Definition{ID: "local-task-definition", Key: tasks.LibraryScanKey, EmbyKey: tasks.LibraryScanEmbyKey,
		Name: "Scan media library", Description: "Scan all registered media libraries.", Category: "Library", Enabled: true,
		Revision: 37, ScheduleTimezone: "Asia/Shanghai"}
	empty := embyTaskDTO(definition)
	if empty["State"] != "Idle" || empty["LastExecutionResult"] != nil || empty["CurrentProgressPercentage"] != nil || len(empty["Triggers"].([]map[string]any)) != 0 {
		t.Fatal("idle unexecuted task invented progress, a prior result, or a default trigger")
	}
	definition.LastRun = &tasks.Run{ID: "private-execution-id", TaskID: definition.ID, ActorUserID: "private-actor-user",
		ActorSessionID: "private-actor-credential", ActorKind: "emby", State: tasks.RunCompleted, CreatedAt: started,
		StartedAt: &started, FinishedAt: &finished}
	definition.CurrentRun = &tasks.Run{ID: "private-current-run", State: tasks.RunRunning, TotalChildren: 4, TerminalChildren: 1}
	current := embyTaskDTO(definition)
	last := current["LastExecutionResult"].(map[string]any)
	if current["State"] != "Running" || current["CurrentProgressPercentage"] != float64(25) ||
		last["Id"] != definition.ID || last["Status"] != "Completed" || last["Key"] != tasks.LibraryScanEmbyKey ||
		last["StartTimeUtc"].(time.Time).Location() != time.UTC || last["EndTimeUtc"].(time.Time).Location() != time.UTC {
		t.Fatal("running projection lost prior result, definition identity, UTC, or bounded library progress")
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private-", "Actor", "RunId", "ScheduleTimezone", "Revision", "library.scan", "Enabled"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("compatibility task exposed an internal field or identifier %s", forbidden)
		}
	}
	definition.CurrentRun.State = tasks.RunStopping
	if embyTaskDTO(definition)["State"] != "Cancelling" {
		t.Fatal("native stopping run did not use the declared compatibility state")
	}
	definition.CurrentRun = nil
	for state, want := range map[tasks.RunState]string{tasks.RunCompleted: "Completed", tasks.RunCancelled: "Cancelled", tasks.RunFailed: "Failed", tasks.RunInterrupted: "Aborted"} {
		definition.LastRun.State = state
		value := embyTaskDTO(definition)
		if value["State"] != "Idle" || value["CurrentProgressPercentage"] != nil || value["LastExecutionResult"].(map[string]any)["Status"] != want {
			t.Errorf("terminal state %s did not map to %s while omitting idle progress", state, want)
		}
	}
	var observed map[string]any
	if json.Unmarshal(scheduledTaskFixtureBody(t, "batch-a-stop-result-0", false), &observed) != nil {
		t.Fatal("decode observed terminal task fixture")
	}
	if observed["State"] != "Idle" || observed["CurrentProgressPercentage"] != nil || observed["LastExecutionResult"].(map[string]any)["Id"] != observed["Id"] {
		t.Fatal("reference premise changed for result identity or idle progress")
	}
}
