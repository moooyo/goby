//go:build linux

package server

import (
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/tasks"
)

func assertPublishedTaskCollection(t *testing.T, f *scheduledTaskHTTPFixture, items []map[string]any) {
	t.Helper()
	if len(items) != 7 {
		t.Fatalf("expected seven implemented task capabilities, got %d", len(items))
	}
	seen := make(map[string]bool)
	for _, item := range items {
		id, ok := item["Id"].(string)
		if !ok || seen[id] {
			t.Fatal("task collection duplicated or omitted a stable identity")
		}
		seen[id] = true
		definition, err := f.app.taskStore.Get(f.ctx, id)
		if err != nil || item["Key"] != tasks.CompatibilityKey(definition.Key) || item["Key"] == "" {
			t.Fatal("unimplemented or incorrectly identified task was published")
		}
		if id == f.taskID {
			scheduledTaskHTTPAssertInfo(t, item, id)
		}
	}
	if !seen[f.taskID] {
		t.Fatal("scan capability disappeared")
	}
}

func TestManagementCompatibilityCacheTaskAndProviderDisabledAdmission(t *testing.T) {
	f := scheduledTaskHTTPAuthenticate(t, newServerFixture(t))
	for _, key := range []string{tasks.LibraryRefreshMediaKey, tasks.CacheMaintainKey} {
		definition, err := f.app.taskStore.GetByKey(f.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/emby/ScheduledTasks/Running/"+definition.ID, nil, f.admin))
		scheduledTaskHTTPWait(t, f, "compatible executor completion", func() bool {
			current, err := f.app.taskStore.Get(f.ctx, definition.ID)
			return err == nil && current.LastRun != nil && current.LastRun.State == tasks.RunCompleted
		})
	}
	for _, key := range []string{tasks.MetadataRefreshKey, tasks.SubtitleDownloadKey} {
		definition, err := f.app.taskStore.GetByKey(f.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		expectStatus(t, f.request(t, http.MethodPost, "/emby/ScheduledTasks/Running/"+definition.ID, nil, f.admin), http.StatusServiceUnavailable)
		page, err := f.app.taskStore.ListRuns(f.ctx, definition.ID, tasks.Page{})
		if err != nil || page.TotalRecordCount != 0 {
			t.Fatal("disabled provider was represented as completed work")
		}
	}
}

func TestNamedTaskConfigurationControlsActualCacheMaintenance(t *testing.T) {
	f := scheduledTaskHTTPAuthenticate(t, newServerFixture(t))
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES ('configured-cache-library','Configured cache','movies');
        INSERT INTO items(id,library_id,name,sort_name,type) VALUES ('configured-cache-item','configured-cache-library','Cache item','cache item','Movie');
        INSERT INTO item_provider_images(item_id,image_type,image_index,provider,provider_id,image_id,content,mime_type,width,height,source_hash,fetched_at)
        SELECT 'configured-cache-item','Backdrop',n,'tmdb','fixture','fixture-'||n,decode('00','hex'),'image/png',1,1,repeat('a',64),
            clock_timestamp()-make_interval(days=>n) FROM generate_series(0,2) n`); err != nil {
		t.Fatal(err)
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/System/Configuration/tasks", map[string]any{"MaxConcurrent": 1, "CacheRetentionDays": 1, "CacheMaxEntries": 1}, f.admin))
	configuration := configurationHTTPObject(t, f.request(t, http.MethodGet, "/System/Configuration/tasks", nil, f.admin))
	if configuration["MaxConcurrent"] != float64(1) || configuration["CacheMaxEntries"] != float64(1) {
		t.Fatal("named task configuration did not publish effective values")
	}
	definition, err := f.app.taskStore.GetByKey(f.ctx, tasks.CacheMaintainKey)
	if err != nil {
		t.Fatal(err)
	}
	var beforeSignals int64
	if err := f.pool.QueryRow(f.ctx, `SELECT sequence FROM task_system_events WHERE name='LibraryChanged'`).Scan(&beforeSignals); err != nil {
		t.Fatal(err)
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/ScheduledTasks/Running/"+definition.ID, nil, f.admin))
	scheduledTaskHTTPWait(t, f, "configured cache pruning", func() bool {
		value, err := f.app.taskStore.Get(f.ctx, definition.ID)
		return err == nil && value.LastRun != nil && value.LastRun.State == tasks.RunCompleted
	})
	var count, index int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*),min(image_index) FROM item_provider_images WHERE item_id='configured-cache-item'`).Scan(&count, &index); err != nil || count != 1 || index != 0 {
		t.Fatalf("cache executor ignored the newly configured limit: %v", err)
	}
	var signals int64
	if err := f.pool.QueryRow(f.ctx, `SELECT sequence FROM task_system_events WHERE name='LibraryChanged'`).Scan(&signals); err != nil || signals != beforeSignals {
		t.Fatal("task cache output recursively signalled library work")
	}
	scheduledTaskHTTPNoContent(t, f.request(t, http.MethodPost, "/System/Configuration/tasks", map[string]any{}, f.admin))
	if options := f.app.settings.Snapshot().Management.Tasks; options.MaxConcurrent != 2 || options.CacheRetentionDays != 30 || options.CacheMaxEntries != 10000 {
		t.Fatal("empty named task object did not restore shared defaults")
	}
}
