//go:build linux

package server

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type adminRefreshMediaHTTPProber struct{ *scheduledTaskHTTPProber }

func (adminRefreshMediaHTTPProber) CacheVersion() int { return media.CurrentProbeVersion }

func (prober adminRefreshMediaHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := prober.scheduledTaskHTTPProber.ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info.ProbeVersion = media.CurrentProbeVersion
	info.FileChangeTimeNs = media.FileChangeTime(stat)
	return info, nil
}

func adminRefreshMediaHTTPFixture(t *testing.T) (*scheduledTaskHTTPFixture, string, *scheduledTaskHTTPProber) {
	t.Helper()
	f, root := newLibraryServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	prober := &scheduledTaskHTTPProber{}
	prober.block().open()
	catalog, err := library.New(f.pool, adminRefreshMediaHTTPProber{prober}, []string{root})
	if err != nil {
		t.Fatalf("create versioned refresh task catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	t.Cleanup(prober.releaseAll)
	f.handler = f.app.Handler()
	return scheduledTaskHTTPAuthenticate(t, f), root, prober
}

func adminRefreshMediaHTTPPreservedState(t *testing.T, f *serverFixture) string {
	t.Helper()
	var state string
	// Existing scans advance library and directory observation timestamps.
	// Keep every other directory field and all metadata/user state exact.
	err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'libraries', (SELECT jsonb_agg(to_jsonb(l) - 'last_scan_at' ORDER BY id) FROM libraries l),
		'roots', (SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
		'directories', (SELECT jsonb_agg(to_jsonb(i) - 'updated_at' ORDER BY id) FROM items i WHERE is_folder),
		'items', (SELECT jsonb_agg(jsonb_build_object('id',id,'library',library_id,'root',root_id,
			'parent',parent_id,'path',path,'type',type,'name',name,'sort',sort_name,'overview',overview,
			'local',local_metadata,'localHash',local_metadata_hash,'localPath',local_metadata_path) ORDER BY id)
			FROM items WHERE NOT is_folder),
		'metadata', (SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
		'userdata', (SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id) FROM user_item_data u)
	)::text`).Scan(&state)
	if err != nil {
		t.Fatalf("read refresh task preservation snapshot: %v", err)
	}
	return state
}

func adminRefreshMediaHTTPStart(t *testing.T, f *scheduledTaskHTTPFixture, taskID, requestID string) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/tasks/"+taskID+"/runs", map[string]any{"RequestId": requestID},
		http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	result := jsonObject(t, response)
	run := objectValue(t, result, "Run")
	if run["TaskId"] != taskID || run["TaskName"] != "Refresh media details" || run["RequestId"] != requestID {
		t.Fatal("native refresh admission lost its definition or request identity")
	}
	adminTaskHTTPNoPrivateFields(t, result)
	return result
}

func TestHTTPAdminRefreshMediaTaskForcesOnlyMediaProbesAndCancelsOwnedWork(t *testing.T) {
	f, root, prober := adminRefreshMediaHTTPFixture(t)
	collections := []library.Library{scheduledTaskHTTPCreateLibrary(t, f, root, 0), scheduledTaskHTTPCreateLibrary(t, f, root, 1)}
	refresh, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryRefreshMediaKey)
	if err != nil || refresh.ID == f.taskID || refresh.EmbyKey != tasks.CompatibilityKey(tasks.LibraryRefreshMediaKey) || refresh.Name != "Refresh media details" {
		t.Fatalf("native refresh definition is missing or has a compatibility alias: %v", err)
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/emby/Library/Refresh", nil, f.admin))
	initial := scheduledTaskHTTPHistory(t, f, 1)[0]
	scheduledTaskHTTPWaitRun(t, f, initial.ID, tasks.RunCompleted)
	if prober.calls.Load() != 2 {
		t.Fatal("ordinary fixture scan did not probe each registered library once")
	}
	viewer, err := f.users.CreateUser(f.ctx, "Refresh preserved viewer", "refresh-preserved-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, collection := range collections {
		var itemID string
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie'", collection.ID).Scan(&itemID); err != nil {
			t.Fatalf("read registered refresh media item: %v", err)
		}
		metadataPath := "/admin/v1/items/" + itemID + "/metadata"
		detail := f.request(t, http.MethodGet, metadataPath, nil, nil, f.cookie)
		expectStatus(t, detail, http.StatusOK)
		updated := f.request(t, http.MethodPut, metadataPath, map[string]any{
			"Revision": stringValue(t, jsonObject(t, detail), "Revision"), "Overrides": map[string]any{"Name": "Preserved " + collection.Name}, "LockedFields": []string{},
		}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
		expectStatus(t, updated, http.StatusOK)
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data
			(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at)
			VALUES ($1,$2,420000000,3,true,false,'2026-01-02T03:04:05Z')`, viewer.ID, itemID); err != nil {
			t.Fatalf("seed owned refresh user state: %v", err)
		}
	}
	preserved := adminRefreshMediaHTTPPreservedState(t, f.serverFixture)
	// The compatibility refresh still means a normal scan, including a cache hit.
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/Library/Refresh", nil, f.admin))
	cached := scheduledTaskHTTPHistory(t, f, 2)[0]
	scheduledTaskHTTPWaitRun(t, f, cached.ID, tasks.RunCompleted)
	if prober.calls.Load() != 2 {
		t.Fatal("Library/Refresh changed from an ordinary cached scan into forced probing")
	}
	first := adminRefreshMediaHTTPStart(t, f, refresh.ID, "refresh-complete")
	firstID := stringValue(t, objectValue(t, first, "Run"), "Id")
	finished := scheduledTaskHTTPWaitRun(t, f, firstID, tasks.RunCompleted)
	if first["Admitted"] != true || finished.TaskKey != tasks.LibraryRefreshMediaKey || finished.TaskEmbyKey != tasks.CompatibilityKey(tasks.LibraryRefreshMediaKey) || finished.TotalChildren != 2 || finished.CompletedChildren != 2 || prober.calls.Load() != 4 {
		t.Fatal("native refresh did not force probing across both registered libraries")
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/Library/Refresh", nil, f.admin))
	afterRefresh := scheduledTaskHTTPHistory(t, f, 3)[0]
	scheduledTaskHTTPWaitRun(t, f, afterRefresh.ID, tasks.RunCompleted)
	if prober.calls.Load() != 4 {
		t.Fatal("a completed native refresh changed later Library/Refresh cache behavior")
	}
	for _, run := range []struct {
		id    string
		force bool
	}{{initial.ID, false}, {cached.ID, false}, {firstID, true}, {afterRefresh.ID, false}} {
		for _, child := range scheduledTaskHTTPChildren(t, f, run.id, 2) {
			if child.ScanJobID == nil {
				t.Fatal("task child omitted its actual scan job")
			}
			job, err := f.app.library.GetJob(f.ctx, *child.ScanJobID)
			if err != nil || job.ForceProbe != run.force || job.TaskChildID != child.ID || job.LibraryID != child.LibraryID {
				t.Fatalf("task mode did not bind to its owned scan job: %v", err)
			}
		}
	}
	if after := adminRefreshMediaHTTPPreservedState(t, f.serverFixture); after != preserved {
		t.Fatal("media refresh changed directories, administrator metadata, or user data")
	}
	gate := prober.block()
	second := adminRefreshMediaHTTPStart(t, f, refresh.ID, "refresh-cancel")
	secondID := stringValue(t, objectValue(t, second, "Run"), "Id")
	scheduledTaskHTTPWait(t, f, "both forced refresh probes", func() bool { return gate.entered.Load() == 2 })
	repeated := adminRefreshMediaHTTPStart(t, f, refresh.ID, "refresh-cancel")
	if repeated["Admitted"] != false || objectValue(t, repeated, "Run")["Id"] != secondID {
		t.Fatal("an active native refresh request retry created another execution")
	}
	// The compatibility projection shares this running native execution.
	projection := f.request(t, http.MethodGet, "/ScheduledTasks/"+refresh.ID, nil, f.admin)
	expectStatus(t, projection, http.StatusOK)
	if info := jsonObject(t, projection); info["Key"] != refresh.EmbyKey || info["State"] != "Running" {
		t.Fatal("compatibility refresh projection lost the native execution")
	}
	items := responseArray(t, f.request(t, http.MethodGet, "/emby/ScheduledTasks", nil, f.admin))
	assertPublishedTaskCollection(t, f, items)
	current, err := f.app.taskStore.GetRun(f.ctx, secondID)
	if err != nil || current.State != tasks.RunRunning || gate.cancelled.Load() != 0 {
		t.Fatal("an Emby request changed the running native refresh")
	}
	definition, err := f.app.taskStore.Get(f.ctx, refresh.ID)
	if err != nil || definition.Revision != refresh.Revision || len(definition.Triggers) != 0 {
		t.Fatal("an Emby request changed the native refresh schedule")
	}
	response := f.request(t, http.MethodPost, "/admin/v1/task-runs/"+secondID+"/cancel", map[string]any{}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	scheduledTaskHTTPWait(t, f, "both owned refresh cancellations", func() bool { return gate.cancelled.Load() == 2 })
	gate.open()
	stopped := scheduledTaskHTTPWaitRun(t, f, secondID, tasks.RunCancelled)
	if stopped.CancelledChildren != 2 || stopped.TerminalChildren != 2 || prober.calls.Load() != 6 {
		t.Fatal("native refresh cancellation did not retain both owned child outcomes")
	}
	if after := adminRefreshMediaHTTPPreservedState(t, f.serverFixture); after != preserved {
		t.Fatal("cancelled refresh changed directory, metadata, or user state")
	}
	scheduledTaskHTTPHistory(t, f, 3)
}

func TestHTTPAdminRefreshMediaTaskRejectsNonNativeCredentialsWithoutChangingTasks(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	refresh, err := a.app.taskStore.GetByKey(a.ctx, tasks.LibraryRefreshMediaKey)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := a.users.CreateUser(a.ctx, "Refresh task viewer", "refresh-task-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	admin := loginClientSessionHTTP(t, a.serverFixture, "Administrator", "administrator-password", "refresh-task-admin-client")
	ordinary := loginClientSessionHTTP(t, a.serverFixture, viewer.Name, "refresh-task-viewer-password", "refresh-task-viewer-client")
	key := a.create(t, "Refresh task native boundary")
	tokens := []string{admin.headers.Get("X-Emby-Token"), ordinary.headers.Get("X-Emby-Token"), key.token}
	before, err := a.app.taskStore.Get(a.ctx, refresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	base := "/admin/v1/tasks/" + refresh.ID
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/admin/v1/tasks", nil},
		{http.MethodGet, base, nil},
		{http.MethodGet, base + "/runs", nil},
		{http.MethodPost, base + "/runs", map[string]any{"RequestId": "denied-refresh"}},
		{http.MethodPost, base + "/triggers/preview", map[string]any{"ScheduleTimezone": "UTC", "Triggers": []any{}}},
		{http.MethodPut, base + "/triggers", map[string]any{"Revision": "1", "ScheduleTimezone": "UTC", "Triggers": []any{}}},
		{http.MethodGet, "/admin/v1/task-runs/" + strings.Repeat("f", 32), nil},
		{http.MethodPost, "/admin/v1/task-runs/" + strings.Repeat("f", 32) + "/cancel", map[string]any{}},
	} {
		expectStatus(t, a.request(t, route.method, route.path, route.body, nil), http.StatusUnauthorized)
		for _, token := range tokens {
			for _, carrier := range []string{"header", "query", "cookie"} {
				path, headers := route.path, make(http.Header)
				var cookies []*http.Cookie
				switch carrier {
				case "header":
					headers.Set("X-Emby-Token", token)
				case "query":
					path += "?api_key=" + url.QueryEscape(token)
				case "cookie":
					cookies = append(cookies, &http.Cookie{Name: sessionCookie, Value: token})
					headers.Set("X-CSRF-Token", csrfToken(token))
				}
				response := a.request(t, route.method, path, route.body, headers, cookies...)
				expectStatus(t, response, http.StatusUnauthorized)
				if token == "" || strings.Contains(response.Body.String(), token) {
					t.Fatal("native task rejection lacked a real credential or exposed it")
				}
			}
		}
	}
	after, err := a.app.taskStore.Get(a.ctx, refresh.ID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal("non-native credentials changed the refresh definition")
	}
	var runs, scans int
	if err := a.pool.QueryRow(a.ctx, "SELECT (SELECT count(*) FROM task_runs),(SELECT count(*) FROM scan_jobs)").Scan(&runs, &scans); err != nil || runs != 0 || scans != 0 {
		t.Fatal("non-native credentials admitted task or scan work")
	}
}
