package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// This opt-in experiment isolates planner statistics from parent-scope query
// behavior. A passing diagnostic is not catalog or capacity acceptance.
func TestHTTPPhase3ParentScopeDiagnostic(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_PARENT_DIAGNOSTIC") != "1" {
		t.Skip("GOBY_PHASE3_PARENT_DIAGNOSTIC=1 is required for the parent-scope diagnostic")
	}
	f, root := catalogCapacityFixture(t, 10*time.Minute)
	f.bootstrap(t)
	const leavesPerLibrary = 50_000
	movies := leavesPerLibrary - catalogCapacityEpisodes - catalogCapacityAudio
	libraries := make([]string, 0, 2)
	for _, name := range []string{"A", "B"} {
		path := filepath.Join(root, name)
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		collection, err := f.app.library.CreateLibrary(f.ctx, "Parent diagnostic "+name, "mixed", []string{path})
		if err != nil {
			t.Fatalf("create parent diagnostic library: %v", err)
		}
		catalogCapacitySeed(t, f, collection.ID, path, movies)
		libraries = append(libraries, collection.ID)
	}
	viewer, err := f.users.CreateUser(f.ctx, "Parent Diagnostic Viewer", "parent-diagnostic-password", false)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": libraries,
		"EnableMediaPlayback": true, "EnablePlaybackRemuxing": true,
		"EnableVideoPlaybackTranscoding": true, "EnableAudioPlaybackTranscoding": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$2::jsonb WHERE id=$1", viewer.ID, policy); err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, viewer.Name, "parent-diagnostic-password")
	token := stringValue(t, login, "AccessToken")
	query := func(parent string, offset, limit int) string {
		values := url.Values{
			"Recursive": {"true"}, "IncludeItemTypes": {"Movie,Episode,Audio"},
			"SortBy": {"SortName"}, "SortOrder": {"Ascending"},
			"StartIndex": {strconv.Itoa(offset)}, "Limit": {strconv.Itoa(limit)},
		}
		if parent != "" {
			values.Set("ParentId", parent)
		}
		return "/emby/Items?" + values.Encode()
	}
	parent := libraries[0]
	globalCount := query("", 0, 0)
	parentCount := query(parent, 0, 0)
	firstPage := query(parent, 0, catalogCapacityPage)
	deepOffset := leavesPerLibrary - catalogCapacityPage
	deepPage := query(parent, deepOffset, catalogCapacityPage)
	request := func(stage, target string, wantTotal, offset, limit int) bool {
		t.Helper()
		ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		started := time.Now()
		r := httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx)
		r.Header.Set("X-Emby-Token", token)
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, r)
		elapsed := time.Since(started)
		contextErr := ctx.Err()
		cancel()
		var payload struct {
			TotalRecordCount int
			Items            []struct{ Id, Name, Type string }
		}
		decodeErr := json.Unmarshal(response.Body.Bytes(), &payload)
		failure := ""
		switch {
		case contextErr != nil:
			failure = contextErr.Error()
		case response.Code != http.StatusOK:
			failure = "unexpected_http_status"
		case decodeErr != nil:
			failure = "invalid_response_json"
		case payload.TotalRecordCount != wantTotal || len(payload.Items) != limit:
			failure = "incorrect_total_or_page_length"
		default:
			for index, item := range payload.Items {
				number := offset + index + 1
				kind := "Movie"
				if number > movies {
					kind = "Episode"
				}
				if number > movies+catalogCapacityEpisodes {
					kind = "Audio"
				}
				if item.Id != catalogCapacityLeafID(parent, number) || item.Name != fmt.Sprintf("Item %05d", number) || item.Type != kind {
					failure = "incorrect_page_identity_or_order"
					break
				}
			}
		}
		record, _ := json.Marshal(map[string]any{
			"stage": stage, "status": response.Code, "elapsed_ms": elapsed.Milliseconds(),
			"total": payload.TotalRecordCount, "items": len(payload.Items),
			"expected_total": wantTotal, "offset": offset, "limit": limit,
			"valid": failure == "", "failure": failure,
		})
		t.Logf("phase3_parent_scope_request=%s", record)
		return failure == ""
	}
	statistics := func(stage string) {
		t.Helper()
		var estimated float64
		var analyzed, autoanalyzed string
		if err := f.pool.QueryRow(f.ctx, `SELECT c.reltuples::double precision,
			COALESCE(s.last_analyze::text,''),COALESCE(s.last_autoanalyze::text,'')
			FROM pg_class c JOIN pg_stat_all_tables s ON s.relid=c.oid
			WHERE c.oid='items'::regclass`).Scan(&estimated, &analyzed, &autoanalyzed); err != nil {
			t.Fatalf("read parent diagnostic planner statistics: %v", err)
		}
		record, _ := json.Marshal(map[string]any{"stage": stage, "reltuples": estimated,
			"last_analyze": analyzed, "last_autoanalyze": autoanalyzed})
		t.Logf("phase3_parent_scope_statistics=%s", record)
	}
	statistics("before_analyze")
	request("before_global_count", globalCount, 2*leavesPerLibrary, 0, 0)
	if request("before_parent_count", parentCount, leavesPerLibrary, 0, 0) {
		request("before_parent_first", firstPage, leavesPerLibrary, 0, catalogCapacityPage)
		request("before_parent_deep", deepPage, leavesPerLibrary, deepOffset, catalogCapacityPage)
	} else {
		t.Log("phase3_parent_scope_before_pages_skipped=true reason=parent_count_failed")
	}
	analyzeCtx, cancelAnalyze := context.WithTimeout(f.ctx, 30*time.Second)
	_, err = f.pool.Exec(analyzeCtx, "ANALYZE items")
	cancelAnalyze()
	if err != nil {
		t.Fatalf("analyze only the owned items table: %v", err)
	}
	statistics("after_analyze")
	for _, step := range []struct {
		stage, target string
		offset, limit int
	}{
		{"after_parent_count", parentCount, 0, 0},
		{"after_parent_first", firstPage, 0, catalogCapacityPage},
		{"after_parent_deep", deepPage, deepOffset, catalogCapacityPage},
	} {
		if !request(step.stage, step.target, leavesPerLibrary, step.offset, step.limit) {
			t.Fatalf("parent-scope diagnostic still failed after ANALYZE at %s", step.stage)
		}
	}
	t.Log("phase3_parent_scope_diagnostic_complete=true acceptance_claim=false synthetic_catalog_only=true")
}
