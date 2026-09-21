//go:build linux

package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func analysisHTTPObject(t *testing.T, response *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	expectStatus(t, response, status)
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("analysis management response was cacheable")
	}
	return jsonObject(t, response)
}

func TestHTTPAdminMediaAnalysisConfigurationAuthenticationStrictCASAndSafeOverview(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	base := "/admin/v1/media-analysis"
	expectStatus(t, f.request(t, http.MethodGet, base, nil, nil), http.StatusUnauthorized)
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	expectStatus(t, f.request(t, http.MethodGet, base, nil, http.Header{"X-Emby-Token": {stringValue(t, emby, "AccessToken")}}), http.StatusUnauthorized)
	initial := analysisHTTPObject(t, f.request(t, http.MethodGet, base, nil, nil, cookie), http.StatusOK)
	if len(initial) != 2 {
		t.Fatal("overview exposed an undocumented root field")
	}
	configuration, runtime := objectValue(t, initial, "Configuration"), objectValue(t, initial, "Runtime")
	if len(configuration) != 4 || configuration["Revision"] != "1" || len(runtime) != 5 {
		t.Fatal("overview omitted its closed configuration/runtime shape")
	}
	if _, ok := runtime["Reasons"].([]any); !ok {
		t.Fatal("runtime reasons were not an array")
	}
	for _, secret := range []string{"CacheRoot", "FFmpegPath", "FFprobePath", "FingerprintPath", "SessionId", "ActorId", "ExecutorToken"} {
		if _, exists := runtime[secret]; exists {
			t.Fatal("overview exposed runtime authority or storage paths")
		}
	}
	body := map[string]any{"Revision": "1", "Profile": configuration["Profile"]}
	expectStatus(t, f.request(t, http.MethodPut, base+"/configuration", body, nil, cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPut, base+"/configuration", body, http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://outside.example"}}, cookie), http.StatusForbidden)
	headers := http.Header{"X-CSRF-Token": {csrf}}
	unchanged := analysisHTTPObject(t, f.request(t, http.MethodPut, base+"/configuration", body, headers, cookie), http.StatusOK)
	if !reflect.DeepEqual(unchanged, configuration) {
		t.Fatal("complete no-op configuration changed the revision or timestamp")
	}
	for _, raw := range []string{
		strings.Replace(analysisConfigurationBodyForTest, `"Revision":"1"`, `"Revision":"1","Revision":"2"`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"AutoPublishIntros":true`, `"AutoPublishIntros":null`, 1),
		strings.Replace(analysisConfigurationBodyForTest, `"PreviewQuality":80`, `"PreviewQuality":80,"previewQuality":81`, 1),
	} {
		expectStatus(t, adminTaskHTTPRaw(f, cookie, csrf, http.MethodPut, base+"/configuration", raw, "application/json"), http.StatusBadRequest)
	}
	quality := objectValue(t, configuration, "Profile")
	quality["PreviewQuality"] = float64(81)
	body["Profile"] = quality
	saved := analysisHTTPObject(t, f.request(t, http.MethodPut, base+"/configuration", body, headers, cookie), http.StatusOK)
	if saved["Revision"] != "2" || objectValue(t, saved, "Profile")["PreviewQuality"] != float64(81) {
		t.Fatal("configuration update did not return the committed profile")
	}
	expectStatus(t, f.request(t, http.MethodPut, base+"/configuration", body, headers, cookie), http.StatusConflict)
	read := analysisHTTPObject(t, f.request(t, http.MethodGet, base, nil, nil, cookie), http.StatusOK)
	if !reflect.DeepEqual(objectValue(t, read, "Configuration"), saved) {
		t.Fatal("overview returned a stale configuration")
	}
	page := analysisHTTPObject(t, f.request(t, http.MethodGet, base+"/items?StartIndex=0&Limit=25", nil, nil, cookie), http.StatusOK)
	if page["TotalRecordCount"] != float64(0) || page["StartIndex"] != float64(0) || page["Limit"] != float64(25) || !reflect.DeepEqual(page["Items"], []any{}) {
		t.Fatal("empty inventory lost its bounded page contract")
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"/items/unknown", nil, nil, cookie), http.StatusNotFound)
}

type analysisRevokeBody struct {
	reader io.Reader
	revoke func()
}

func (body *analysisRevokeBody) Read(value []byte) (int, error) {
	if body.revoke != nil {
		revoke := body.revoke
		body.revoke = nil
		revoke()
	}
	return body.reader.Read(value)
}

func TestHTTPAdminMediaAnalysisRevalidatesCredentialAfterBodyRead(t *testing.T) {
	f := newServerFixture(t)
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	body := &analysisRevokeBody{reader: strings.NewReader(strings.Replace(analysisConfigurationBodyForTest, `"PreviewQuality":80`, `"PreviewQuality":81`, 1)), revoke: func() {
		if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE user_id=$1 AND kind='admin'`, userID); err != nil {
			t.Fatal(err)
		}
	}}
	r := httptest.NewRequest(http.MethodPut, "/admin/v1/media-analysis/configuration", body).WithContext(f.ctx)
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	expectStatus(t, w, http.StatusUnauthorized)
	var revision, quality int
	if err := f.pool.QueryRow(f.ctx, `SELECT revision,preview_quality FROM analysis_settings WHERE id=1`).Scan(&revision, &quality); err != nil || revision != 1 || quality != 80 {
		t.Fatal("revoked credential changed analysis configuration")
	}
}

type analysisHTTPNoWorkExecutor struct{}

func (analysisHTTPNoWorkExecutor) Available() bool { return true }
func (analysisHTTPNoWorkExecutor) Execute(context.Context, tasks.Work, func(tasks.Progress) error) error {
	return nil
}

func TestHTTPAdminMediaAnalysisRunsRetainTypedRequestReceiptAndActualNativeActor(t *testing.T) {
	f := newServerFixture(t)
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	// Replace only the task execution boundary with a no-source executor. Its
	// admission still writes the real library profile/source snapshot contract.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskIntroAnalysisKey, Name: "Analysis HTTP receipt fixture", Executor: analysisHTTPNoWorkExecutor{},
		AnalysisAdmission: func(tx library.OwnedTx, request tasks.AnalysisAdmissionRequest) (tasks.AnalysisAdmissionBinding, error) {
			binding, err := library.PrepareAnalysis(tx, request.TaskKey, request.Selection, library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, UnavailableReason: "not_configured"})
			return tasks.AnalysisAdmissionBinding{ConfigurationFingerprint: binding.ConfigurationFingerprint, Bind: binding.Bind, SnapshotChildren: binding.SnapshotChildren}, err
		}})
	if err != nil {
		t.Fatal(err)
	}
	f.app.taskStore, err = tasks.New(f.pool, f.app.library, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.taskStore.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.app.taskManager, err = tasks.NewManager(f.app.taskStore, f.app.library, tasks.ManagerOptions{Logger: f.log})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"Kind": "intro", "RequestId": "analysis-http-retry", "LibraryIds": []string{}, "ItemIds": []string{}, "Force": false}
	headers := http.Header{"X-CSRF-Token": {csrf}}
	first := analysisHTTPObject(t, f.request(t, http.MethodPost, "/admin/v1/media-analysis/runs", body, headers, cookie), http.StatusAccepted)
	if len(first) != 3 || first["Admitted"] != true {
		t.Fatal("run receipt changed its closed public shape")
	}
	repeated := analysisHTTPObject(t, f.request(t, http.MethodPost, "/admin/v1/media-analysis/runs", body, headers, cookie), http.StatusAccepted)
	if repeated["RunId"] != first["RunId"] || repeated["TaskId"] != first["TaskId"] || repeated["Admitted"] != false {
		t.Fatal("HTTP retry replaced its durable admitted run")
	}
	var source, actorKind, actorID string
	var input []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT source,actor_kind,actor_user_id,analysis_input FROM task_runs WHERE id=$1`, first["RunId"]).Scan(&source, &actorKind, &actorID, &input); err != nil || source != "manual" || actorKind != "admin" || actorID != userID || string(input) != "{}" {
		t.Fatal("HTTP admission lost its typed scope or original native actor")
	}
	body["Force"] = true
	expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/media-analysis/runs", body, headers, cookie), http.StatusConflict, "request_conflict", false)
}

type analysisHTTPProber struct{}

func (analysisHTTPProber) CacheVersion() int { return media.CurrentProbeVersion }
func (analysisHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (apiMediaProber{}).ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	info.ProbeVersion, info.FileChangeTimeNs = media.CurrentProbeVersion, media.FileChangeTime(stat)
	return info, nil
}

func TestHTTPAdminMediaAnalysisIndexedInventoryAndDecisionUseOneCurrentItemScope(t *testing.T) {
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	catalog, err := library.New(f.pool, analysisHTTPProber{}, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	firstPath := writeAPIMediaFile(t, root, "selected/Alpha.mp4")
	writeAPIMediaFile(t, root, "selected/Beta.mp4")
	writeAPIMediaFile(t, root, "outside/Outside.mp4")
	selected := createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "selected"), "movies")
	createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "outside"), "movies")
	var itemID string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1`, firstPath).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	base := "/admin/v1/media-analysis"
	page := analysisHTTPObject(t, f.request(t, http.MethodGet, base+"/items?LibraryId="+selected+"&StartIndex=0&Limit=1", nil, nil, cookie), http.StatusOK)
	items, ok := page["Items"].([]any)
	if !ok || len(items) != 1 || page["TotalRecordCount"] != float64(2) || page["Limit"] != float64(1) {
		t.Fatal("HTTP count included a different library or ignored the page limit")
	}
	item := items[0].(map[string]any)
	if item["Id"] != itemID || item["LibraryId"] != selected || item["MediaSourceId"] != media.SourceID(itemID) || !reflect.DeepEqual(item["Previews"], []any{}) {
		t.Fatal("HTTP inventory lost actual indexed membership")
	}
	assertNoItemPaths(t, item, root)
	detail := analysisHTTPObject(t, f.request(t, http.MethodGet, base+"/items/"+itemID, nil, nil, cookie), http.StatusOK)
	if detail["Id"] != itemID || detail["SourceRevision"] != item["SourceRevision"] || objectValue(t, detail, "Detection")["SourceRevision"] != detail["SourceRevision"] {
		t.Fatal("detail mixed different source revisions")
	}
	detection := objectValue(t, detail, "Detection")
	body := map[string]any{"Revision": detection["Revision"], "SourceRevision": detail["SourceRevision"], "ManualRevision": detection["ManualRevision"], "Action": "reject"}
	headers := http.Header{"X-CSRF-Token": {csrf}}
	decision := analysisHTTPObject(t, f.request(t, http.MethodPost, base+"/items/"+itemID+"/decision", body, headers, cookie), http.StatusOK)
	if decision["ItemId"] != itemID || decision["Revision"] != "1" || decision["Suppressed"] != true || decision["Candidate"] != nil {
		t.Fatal("initial HTTP decision did not create a real source-bound tombstone")
	}
	expectAPIError(t, f.request(t, http.MethodPost, base+"/items/"+itemID+"/decision", body, headers, cookie), http.StatusConflict, "analysis_conflict", false)
	body["Revision"], body["Action"] = decision["Revision"], "reset"
	reset := analysisHTTPObject(t, f.request(t, http.MethodPost, base+"/items/"+itemID+"/decision", body, headers, cookie), http.StatusOK)
	if reset["Revision"] != "2" || reset["Suppressed"] != false || reset["Effective"] != nil {
		t.Fatal("HTTP reset elevated an absent candidate or lost CAS")
	}
	var userData, sessions, jobs int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM user_item_data),(SELECT count(*) FROM play_sessions),(SELECT count(*) FROM encoding_jobs)`).Scan(&userData, &sessions, &jobs); err != nil || userData != 0 || sessions != 0 || jobs != 0 {
		t.Fatal("analysis management changed playback state")
	}
}
