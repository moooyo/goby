//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
)

// Run as an unprivileged user with writable TMPDIR/GOTMPDIR, reachable test
// PostgreSQL, and executable real FFmpeg/ffprobe. Only directories created by
// this test are chmod-ed; the actual EACCES observation cannot be skipped.
func TestHTTPPhase3NonrootPermissionRecovery(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_PERMISSION_RECOVERY") != "1" {
		t.Skip("set GOBY_PHASE3_PERMISSION_RECOVERY=1 for real nonroot permission recovery")
	}
	if os.Geteuid() == 0 {
		t.Fatal("permission recovery must run with a nonzero effective UID")
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE", "GOTMPDIR"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required for the nonroot permission fixture", name)
		}
	}
	f, accounts := newClientSessionHTTPAccounts(t, 10*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-phase3-permission-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove owned permission fixture: %v", err)
		}
	})
	mediaRoot, evidenceRoot := filepath.Join(base, "media"), filepath.Join(base, "evidence")
	faultyRoot, healthyRoot := filepath.Join(mediaRoot, "faulty"), filepath.Join(mediaRoot, "healthy")
	for _, path := range []string{faultyRoot, healthyRoot, evidenceRoot} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	seed := filepath.Join(base, "seed.mp4")
	hlsHTTPMediaCommand(t, os.Getenv("GOBY_FFMPEG"), "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=blue:size=160x90:rate=24:duration=5",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=5",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "24", "-keyint_min", "24",
		"-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p", "-c:a", "aac", "-threads:a", "1", "-t", "5", seed)
	mediaBytes, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"FaultyKept": filepath.Join(faultyRoot, "FaultyKept.mp4"), "FaultyLost": filepath.Join(faultyRoot, "FaultyLost.mp4"),
		"HealthyOne": filepath.Join(healthyRoot, "HealthyOne.mp4"), "HealthyTwo": filepath.Join(healthyRoot, "HealthyTwo.mp4"),
	}
	for _, path := range files {
		if err := os.WriteFile(path, mediaBytes, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg.MediaRoots = []string{mediaRoot}
	f.cfg.FFmpegPath, f.cfg.FFprobePath = os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	f.cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: evidenceRoot, MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "phase3-nonroot-permission-recovery")
	if err != nil {
		t.Fatalf("start real nonroot server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close permission fixture server: %v", err)
		}
	})
	f.app, f.handler = app, app.Handler()
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: f.cfg.FFmpegPath, ffprobe: f.cfg.FFprobePath}
	h.server = httptest.NewServer(f.handler)
	t.Cleanup(h.server.Close)
	adminHeaders := http.Header{"Cookie": {accounts.cookie.String()}, "X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	requestJSON := func(method, path string, body any, headers http.Header, status int) map[string]any {
		t.Helper()
		response := h.request(t, method, path, body, headers)
		if response.status != status {
			t.Fatalf("permission HTTP %s %s status=%d expected=%d body=%s", method, path, response.status, status, response.body)
		}
		var result map[string]any
		if err := json.Unmarshal(response.body, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	created := requestJSON(http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Nonroot permission recovery", "CollectionType": "movies", "Paths": []string{faultyRoot, healthyRoot}, "Scan": false,
	}, adminHeaders, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, created, "Library"), "Id")
	rootList := "/admin/v1/libraries/" + libraryID + "/roots"
	roots := requestJSON(http.MethodGet, rootList, nil, adminHeaders, http.StatusOK)
	if roots["TotalRecordCount"] != float64(2) || len(roots["Items"].([]any)) != 2 {
		t.Fatal("permission fixture did not register exactly two roots")
	}
	bindings := make(map[string]map[string]any, 2)
	for _, raw := range roots["Items"].([]any) {
		root := raw.(map[string]any)
		id := stringValue(t, root, "Id")
		binding := objectValue(t, requestJSON(http.MethodGet, rootList+"/"+id+"/binding", nil, adminHeaders, http.StatusOK), "Binding")
		if binding["Status"] != "verified" {
			t.Fatalf("nonroot storage does not provide verified bindings: %#v", binding)
		}
		bindings[id] = binding
	}
	runScan := func(label, expectedStatus string, expectedScanned int) {
		t.Helper()
		started := time.Now()
		admitted := requestJSON(http.MethodPost, "/admin/v1/libraries/"+libraryID+"/scan", map[string]any{"ForceProbe": false}, adminHeaders, http.StatusAccepted)
		jobID := stringValue(t, objectValue(t, admitted, "Job"), "Id")
		timer, ticker := time.NewTimer(2*time.Minute), time.NewTicker(25*time.Millisecond)
		defer timer.Stop()
		defer ticker.Stop()
		for {
			jobs := requestJSON(http.MethodGet, "/admin/v1/jobs", nil, adminHeaders, http.StatusOK)
			for _, raw := range jobs["Items"].([]any) {
				job := raw.(map[string]any)
				if job["Id"] != jobID || job["Status"] == "pending" || job["Status"] == "running" {
					continue
				}
				if job["Status"] != expectedStatus || expectedScanned >= 0 && job["Scanned"] != float64(expectedScanned) ||
					expectedStatus == "completed" && job["Error"] != "" || expectedStatus == "failed" && job["Error"] == "" {
					t.Fatalf("permission scan phase=%s had an unexpected result: %#v", label, job)
				}
				t.Logf("permission phase=%s status=%v scanned=%v added=%v updated=%v elapsed=%s", label, job["Status"], job["Scanned"], job["Added"], job["Updated"], time.Since(started))
				return
			}
			select {
			case <-ticker.C:
			case <-timer.C:
				t.Fatalf("permission scan phase=%s exceeded its hang guard", label)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
		}
	}
	catalog := func(want int) map[string]string {
		t.Helper()
		page := requestJSON(http.MethodGet, "/emby/Items?ParentId="+libraryID+"&Recursive=true&IncludeItemTypes=Movie&Limit=100", nil, accounts.admin.headers, http.StatusOK)
		items := page["Items"].([]any)
		if page["TotalRecordCount"] != float64(want) || len(items) != want {
			t.Fatalf("permission catalog population=%v/%d expected=%d", page["TotalRecordCount"], len(items), want)
		}
		result := make(map[string]string, want)
		for _, raw := range items {
			item := raw.(map[string]any)
			result[stringValue(t, item, "Name")] = stringValue(t, item, "Id")
		}
		if len(result) != want {
			t.Fatal("permission catalog contains duplicate movie names")
		}
		return result
	}
	runScan("baseline", "completed", len(files))
	baseline := catalog(len(files))
	for name := range files {
		if baseline[name] == "" {
			t.Fatalf("real movie %s is missing from the baseline", name)
		}
	}
	userDataURL := "/emby/Users/" + accounts.admin.userID + "/Items/" + baseline["FaultyKept"] + "/UserData"
	requestJSON(http.MethodPost, userDataURL, map[string]any{
		"PlaybackPositionTicks": int64(10_000_000), "PlayCount": 7, "Played": false, "IsFavorite": true,
		"LastPlayedDate": "2026-09-01T03:04:05Z",
	}, accounts.admin.headers, http.StatusOK)
	savedUserData := requestJSON(http.MethodGet, userDataURL, nil, accounts.admin.headers, http.StatusOK)
	if savedUserData["PlaybackPositionTicks"] != float64(10_000_000) || savedUserData["PlayCount"] != float64(7) || savedUserData["IsFavorite"] != true {
		t.Fatal("permission fixture did not persist its nondefault UserData")
	}
	settings := requestJSON(http.MethodGet, "/admin/v1/settings", nil, adminHeaders, http.StatusOK)
	savedSettings := requestJSON(http.MethodPut, "/admin/v1/settings", adminSettingsHTTPUpdate(stringValue(t, settings, "Revision"), "Permission recovery settings", nil), adminHeaders, http.StatusOK)
	assertSavedState := func() {
		t.Helper()
		if current := requestJSON(http.MethodGet, userDataURL, nil, accounts.admin.headers, http.StatusOK); !reflect.DeepEqual(current, savedUserData) {
			t.Fatal("permission failure or recovery changed confirmed UserData")
		}
		current := requestJSON(http.MethodGet, "/admin/v1/settings", nil, adminHeaders, http.StatusOK)
		if current["Revision"] != savedSettings["Revision"] || !reflect.DeepEqual(current["Overrides"], savedSettings["Overrides"]) ||
			objectValue(t, current, "Effective")["ServerName"] != "Permission recovery settings" {
			t.Fatal("permission failure or recovery changed confirmed settings")
		}
	}
	assertHealthy := func() {
		t.Helper()
		for _, name := range []string{"HealthyOne", "HealthyTwo"} {
			page := requestJSON(http.MethodGet, "/emby/Items?Ids="+baseline[name], nil, accounts.admin.headers, http.StatusOK)
			items := page["Items"].([]any)
			if page["TotalRecordCount"] != float64(1) || len(items) != 1 || items[0].(map[string]any)["Id"] != baseline[name] {
				t.Fatal("inaccessible root disrupted a healthy-root catalog query")
			}
		}
		response := h.request(t, http.MethodGet, "/emby/Videos/"+baseline["HealthyOne"]+"/stream.mp4?Static=true", nil, accounts.admin.headers)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.body, mediaBytes) {
			t.Fatal("healthy-root HTTP playback did not return the actual unchanged MP4")
		}
	}
	info, err := os.Stat(faultyRoot)
	if err != nil {
		t.Fatal(err)
	}
	originalMode := info.Mode().Perm()
	t.Cleanup(func() {
		if err := os.Chmod(faultyRoot, originalMode); err != nil {
			t.Errorf("restore this test's permission fault before cleanup: %v", err)
		}
	})
	// Create a real missing file before denying access. The scanner must retain
	// its old row until access to every registered root is restored.
	if err := os.Remove(files["FaultyLost"]); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(faultyRoot, 0); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{faultyRoot, files["FaultyKept"]} {
		opened, err := os.Open(path)
		if opened != nil {
			_ = opened.Close()
		}
		if !errors.Is(err, syscall.EACCES) {
			t.Fatalf("nonroot fixture did not produce actual EACCES: uid=%d error=%v", os.Geteuid(), err)
		}
	}
	t.Logf("permission fault confirmed euid=%d directory_mode=000 open_directory=EACCES open_media=EACCES", os.Geteuid())
	assertHealthy()
	runScan("access-denied", "failed", -1)
	if current := catalog(len(files)); !reflect.DeepEqual(current, baseline) {
		t.Fatal("inaccessible storage was treated as missing media and changed catalog identities")
	}
	var retained bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id=$1)`, baseline["FaultyLost"]).Scan(&retained); err != nil || !retained {
		t.Fatalf("known missing record was deleted without readable storage: %v", err)
	}
	assertHealthy()
	assertSavedState()
	if err := os.Chmod(faultyRoot, originalMode); err != nil {
		t.Fatal(err)
	}
	readback, err := os.ReadFile(files["FaultyKept"])
	if err != nil || !bytes.Equal(readback, mediaBytes) {
		t.Fatalf("restored permissions did not restore actual file access: %v", err)
	}
	runScan("permissions-restored", "completed", len(files)-1)
	expected := make(map[string]string, len(files)-1)
	for name, id := range baseline {
		if name != "FaultyLost" {
			expected[name] = id
		}
	}
	if current := catalog(len(expected)); !reflect.DeepEqual(current, expected) {
		t.Fatal("permission recovery removed the wrong record or changed surviving identities")
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id=$1)`, baseline["FaultyLost"]).Scan(&retained); err != nil || retained {
		t.Fatalf("restored storage did not reconcile the real missing file: %v", err)
	}
	for id, before := range bindings {
		after := objectValue(t, requestJSON(http.MethodGet, rootList+"/"+id+"/binding", nil, adminHeaders, http.StatusOK), "Binding")
		if after["Status"] != "verified" || after["Revision"] != before["Revision"] || after["ApprovedFingerprint"] != before["ApprovedFingerprint"] {
			t.Fatal("permission recovery required or manufactured a storage rebind")
		}
	}
	assertHealthy()
	assertSavedState()
	runScan("settled", "completed", len(expected))
	if current := catalog(len(expected)); !reflect.DeepEqual(current, expected) {
		t.Fatal("unchanged rescan changed the recovered catalog")
	}
	assertSavedState()
	t.Logf("permission recovery accepted euid=%d actual_EACCES=true retained_during_denial=4 recovered_media=3 healthy_playback=true userdata=true settings=true rebind_required=false", os.Geteuid())
}
