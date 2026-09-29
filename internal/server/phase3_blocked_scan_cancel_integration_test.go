//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/moooyo/goby/internal/config"
)

// The launcher owns an ext4 loop/dm-linear mount and a private mount namespace.
// Its fixed control program accepts only remount, suspend, and resume. The
// external watchdog must resume that exact device before any forced cleanup.
func TestHTTPPhase3BlockedScanCancellationRecovery(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_BLOCKED_SCAN_CANCEL") != "1" {
		t.Skip("GOBY_PHASE3_BLOCKED_SCAN_CANCEL=1 is required for actual blocked metadata")
	}
	mount, control, tracePath := os.Getenv("GOBY_PHASE3_BLOCKED_MOUNT"), os.Getenv("GOBY_PHASE3_BLOCKED_CONTROL"), os.Getenv("GOBY_PHASE3_BLOCKED_TRACE")
	canonical, err := filepath.EvalSymlinks(mount)
	var fs unix.Statfs_t
	host, _ := os.Readlink("/proc/1/ns/mnt")
	self, _ := os.Readlink("/proc/self/ns/mnt")
	if err != nil || !filepath.IsAbs(mount) || mount == "/" || canonical != mount || filepath.Clean(mount) != mount ||
		host == "" || host != os.Getenv("GOBY_PHASE3_BLOCKED_HOST_MOUNT_NAMESPACE") || self == host ||
		unix.Statfs(mount, &fs) != nil || fs.Type != unix.EXT4_SUPER_MAGIC {
		t.Fatal("an owned ext4 mount in an independent mount namespace is required")
	}
	for _, path := range []string{control, tracePath} {
		info, err := os.Stat(path)
		if err != nil || !filepath.IsAbs(path) || !info.Mode().IsRegular() || path == mount || strings.HasPrefix(path, mount+"/") {
			t.Fatal("fixed control and existing metadata trace must be outside the fault mount")
		}
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE", "GOTMPDIR"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required for blocked metadata recovery", name)
		}
	}
	f, accounts := newClientSessionHTTPAccounts(t, 10*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-blocked-scan-cancel-")
	if err != nil {
		t.Fatal(err)
	}
	if base == mount || strings.HasPrefix(base, mount+"/") {
		t.Fatal("control artifacts and healthy media must remain outside the fault mount")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("blocked_scan_cancel_artifacts_retained=%s", base)
		} else if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	faultRoot, healthyRoot := filepath.Join(mount, "deep", "library"), filepath.Join(base, "healthy")
	for _, path := range []string{faultRoot, healthyRoot, filepath.Join(base, "evidence")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	seed := filepath.Join(base, "seed.mp4")
	hlsHTTPMediaCommand(t, os.Getenv("GOBY_FFMPEG"), "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=blue:size=160x90:rate=12:duration=5", "-an", "-c:v", "libx264",
		"-threads:v", "1", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-t", "5", seed)
	mediaBytes, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(faultRoot, "Faulty.mp4"), filepath.Join(healthyRoot, "Healthy.mp4")} {
		if err := os.WriteFile(path, mediaBytes, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg.MediaRoots = []string{filepath.Dir(mount), healthyRoot}
	f.cfg.ListenAddress, f.cfg.CookieSecure = "127.0.0.1:0", false
	f.cfg.FFmpegPath, f.cfg.FFprobePath = os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	f.cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: filepath.Join(base, "evidence"), MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	readyPath, configPath := filepath.Join(base, "ready.json"), filepath.Join(base, "child.json")
	encoded, err := json.Marshal(phase3RecoveryProcessConfig{Server: f.cfg, Schema: schema, Ready: readyPath})
	if err != nil || os.WriteFile(configPath, encoded, 0o600) != nil {
		t.Fatal("write the private blocked-scan child configuration")
	}
	var current *phase3RecoveryChild
	stopChild := func() {
		t.Helper()
		if current == nil || current.joined {
			return
		}
		if err := current.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		select {
		case err := <-current.done:
			current.joined = true
			if err != nil {
				t.Errorf("blocked-scan server did not close normally: %v", err)
			}
		case <-time.After(35 * time.Second):
			_ = current.command.Process.Kill()
			select {
			case <-current.done:
				current.joined = true
			case <-time.After(5 * time.Second):
			}
			t.Error("blocked-scan server required forced cleanup after device recovery")
		}
	}
	t.Cleanup(stopChild)
	suspended := false
	device := func(action string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if action == "suspend" {
			suspended = true
		}
		output, err := exec.CommandContext(ctx, control, action).CombinedOutput()
		if err != nil {
			return fmt.Errorf("owned device %s failed: %w; %s", action, err, output)
		}
		if action == "resume" {
			suspended = false
		}
		t.Logf("blocked_scan_cancel_control action=%s completed=true", action)
		return nil
	}
	// Registered after child cleanup: recovery always precedes SIGTERM and Wait.
	t.Cleanup(func() {
		if suspended {
			if err := device("resume"); err != nil {
				t.Error(err)
			}
		}
	})
	generation := 0
	startChild := func() {
		t.Helper()
		generation++
		_ = os.Remove(readyPath)
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(base, fmt.Sprintf("app-%d.log", generation))
		log, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(executable, "-test.run=^TestHTTPPhase3RecoveryProcessHelper$", "-test.timeout=9m", "-test.v")
		command.Env = append(os.Environ(), "GOBY_PHASE3_RECOVERY_HELPER_CONFIG="+configPath)
		command.Stdout, command.Stderr = log, log
		if err := command.Start(); err != nil {
			_ = log.Close()
			t.Fatal(err)
		}
		_ = log.Close()
		current = &phase3RecoveryChild{command: command, done: make(chan error, 1)}
		child := current
		go func() { child.done <- command.Wait() }()
		deadline := time.After(60 * time.Second)
		for {
			var ready struct {
				URL string
				PID int
			}
			data, err := os.ReadFile(readyPath)
			if err == nil && json.Unmarshal(data, &ready) == nil && ready.PID == command.Process.Pid && ready.URL != "" {
				current.url = ready.URL
				t.Logf("blocked_scan_cancel_app generation=%d pid=%d ready=true", generation, ready.PID)
				return
			}
			select {
			case err := <-child.done:
				child.joined = true
				t.Fatalf("blocked-scan child startup failed: %v; inspect %s", err, logPath)
			case <-deadline:
				t.Fatalf("blocked-scan child startup timed out; inspect %s", logPath)
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	client := &http.Client{Timeout: 15 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	admin := http.Header{"Cookie": {accounts.cookie.String()}, "X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	request := func(method, path string, body any, headers http.Header) *httptest.ResponseRecorder {
		t.Helper()
		var input io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			input = bytes.NewReader(data)
		}
		req, err := http.NewRequestWithContext(f.ctx, method, current.url+path, input)
		if err != nil {
			t.Fatal(err)
		}
		req.Header = headers.Clone()
		req.Header.Set("Origin", current.url)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatalf("blocked-scan HTTP transport failed (%T)", err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		if err != nil || len(data) >= 8<<20 {
			t.Fatal("read the bounded blocked-scan HTTP response")
		}
		result := httptest.NewRecorder()
		for name, values := range response.Header {
			result.Header()[name] = append([]string(nil), values...)
		}
		result.WriteHeader(response.StatusCode)
		_, _ = result.Write(data)
		return result
	}
	startChild()
	libraries, roots, items := make([]string, 2), make([]string, 2), make([]string, 2)
	for index, path := range []string{faultRoot, healthyRoot} {
		created := request(http.MethodPost, "/admin/v1/libraries", map[string]any{
			"Name": fmt.Sprintf("Blocked metadata %d", index), "CollectionType": "movies", "Paths": []string{path}, "Scan": true,
		}, admin)
		expectStatus(t, created, http.StatusCreated)
		value := jsonObject(t, created)
		libraries[index] = stringValue(t, objectValue(t, value, "Library"), "Id")
		jobID := stringValue(t, objectValue(t, value, "Job"), "Id")
		for {
			var status, message string
			var scanned int
			if err := f.pool.QueryRow(f.ctx, "SELECT status,error,scanned FROM scan_jobs WHERE id=$1", jobID).Scan(&status, &message, &scanned); err != nil {
				t.Fatal(err)
			}
			if status == "Completed" {
				if message != "" || scanned != 1 {
					t.Fatal("baseline scan did not complete one real media item")
				}
				break
			}
			if status != "Queued" && status != "Running" {
				t.Fatalf("baseline scan failed: %s %s", status, message)
			}
			phase3ENOSPCPause(t, f.ctx)
		}
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM library_roots WHERE library_id=$1 AND storage_binding IS NOT NULL", libraries[index]).Scan(&roots[index]); err != nil {
			t.Fatalf("the real filesystem did not establish a persisted root binding: %v", err)
		}
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie'", libraries[index]).Scan(&items[index]); err != nil {
			t.Fatal(err)
		}
	}
	bindingPath := func(index int) string {
		return "/admin/v1/libraries/" + libraries[index] + "/roots/" + roots[index] + "/binding"
	}
	baselineBinding := objectValue(t, jsonObject(t, request(http.MethodGet, bindingPath(0), nil, admin)), "Binding")
	fingerprint := stringValue(t, baselineBinding, "ApprovedFingerprint")
	if baselineBinding["Status"] != "verified" || baselineBinding["ObservedFingerprint"] != fingerprint {
		t.Fatal("baseline storage binding was not genuinely verified")
	}
	userDataURL := "/emby/Users/" + accounts.admin.userID + "/Items/" + items[0] + "/UserData"
	expectStatus(t, request(http.MethodPost, userDataURL, map[string]any{
		"IsFavorite": true, "Played": false, "PlayCount": 7, "PlaybackPositionTicks": 10_000_000,
	}, accounts.admin.headers), http.StatusOK)
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
			'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
			'userdata',(SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id) FROM user_item_data u),
			'roots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	baseline := snapshot()
	type jobState struct {
		status, message         string
		cancelRequested         bool
		finished                *time.Time
		scanned, added, updated int64
	}
	readJob := func(id string) jobState {
		t.Helper()
		var job jobState
		if err := f.pool.QueryRow(f.ctx, `SELECT status,error,cancel_requested,finished_at,scanned,added,updated
			FROM scan_jobs WHERE id=$1`, id).Scan(&job.status, &job.message, &job.cancelRequested, &job.finished,
			&job.scanned, &job.added, &job.updated); err != nil {
			t.Fatal(err)
		}
		return job
	}
	waitJob := func(id, expected string) jobState {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			job := readJob(id)
			if job.status == expected {
				return job
			}
			if job.status != "Queued" && job.status != "Running" {
				t.Fatalf("scan reached an unexpected terminal state: expected=%s actual=%+v", expected, job)
			}
			if time.Now().After(deadline) {
				t.Fatalf("scan did not reach %s within the fixture hang guard", expected)
			}
			phase3ENOSPCPause(t, f.ctx)
		}
	}
	startScan := func(index int) string {
		t.Helper()
		response := request(http.MethodPost, "/admin/v1/libraries/"+libraries[index]+"/scan", map[string]any{"ForceProbe": false}, admin)
		expectStatus(t, response, http.StatusAccepted)
		return stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	}
	evidence := func() string {
		t.Helper()
		response := request(http.MethodGet, "/admin/v1/runtime/resources", nil, admin)
		expectStatus(t, response, http.StatusOK)
		value := objectValue(t, jsonObject(t, response), "ScanEvidence")
		fields := []string{"ActivePasses", "RetiringPasses", "CleanupFailures", "ReservedBytes", "ReservedFileDescriptors"}
		values := make([]any, 0, len(fields))
		for _, field := range fields {
			number, ok := value[field].(float64)
			if !ok {
				t.Fatalf("scan evidence omitted numeric field %s", field)
			}
			values = append(values, number)
		}
		return fmt.Sprint(values)
	}
	stopChild()
	if !current.joined || t.Failed() {
		t.Fatal("all original app descriptors must close before the cold remount")
	}
	if err := device("remount"); err != nil {
		t.Fatal(err)
	}
	startChild()
	baselineEvidence := evidence()
	if baselineEvidence != "[0 0 0 0 0]" {
		t.Fatalf("the restarted fixture retained old scan evidence: %s", baselineEvidence)
	}
	if err := device("suspend"); err != nil {
		t.Fatal(err)
	}
	traceInfo, err := os.Stat(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	traceOffset := traceInfo.Size()
	blockedJob := startScan(0)
	waitJob(blockedJob, "Running")
	witness := phase3BlockedScanWaitKernel(t, f.ctx, tracePath, traceOffset, mount, current.command.Process.Pid)
	kernel, blocked := phase3BlockedScanKernel(current.command.Process.Pid, witness.tid)
	if !blocked {
		t.Fatal("the scan syscall returned before the cancellation request")
	}
	beforeCancel := readJob(blockedJob)
	if beforeCancel.status != "Running" || beforeCancel.cancelRequested || beforeCancel.finished != nil {
		t.Fatalf("the fault did not retain a genuinely running scan: %+v", beforeCancel)
	}
	resources := request(http.MethodGet, "/admin/v1/runtime/resources", nil, admin)
	expectStatus(t, resources, http.StatusOK)
	heldEvidence := objectValue(t, jsonObject(t, resources), "ScanEvidence")
	heldBytes, bytesOK := heldEvidence["ReservedBytes"].(float64)
	heldFDs, fdsOK := heldEvidence["ReservedFileDescriptors"].(float64)
	if heldEvidence["Enabled"] != true || heldEvidence["ActivePasses"] != float64(1) || !bytesOK || !fdsOK || heldBytes <= 0 || heldFDs <= 0 {
		t.Fatal("the blocked scan did not own one enabled evidence pass with reserved bytes and descriptors")
	}
	t.Logf("blocked_scan_cancel_evidence phase=before_cancel active_passes=1 reserved_bytes=%.0f reserved_fds=%.0f", heldBytes, heldFDs)
	cancelled := request(http.MethodPost, "/admin/v1/jobs/"+blockedJob+"/cancel", nil, admin)
	expectStatus(t, cancelled, http.StatusAccepted)
	if objectValue(t, jsonObject(t, cancelled), "Job")["Status"] != "running" {
		t.Fatal("HTTP cancellation claimed a terminal scan before the blocked syscall returned")
	}
	assertStillBlocked := func(label string) {
		t.Helper()
		for observation := 0; observation < 2; observation++ {
			job := readJob(blockedJob)
			currentKernel, blocked := phase3BlockedScanKernel(current.command.Process.Pid, witness.tid)
			if job.status != "Running" || !job.cancelRequested || job.finished != nil || !blocked || currentKernel != kernel {
				t.Fatalf("cancelled scan lost its live worker before resume at %s: job=%+v kernel_blocked=%t", label, job, blocked)
			}
			if observation == 0 {
				time.Sleep(200 * time.Millisecond)
			}
		}
		t.Logf("blocked_scan_cancel_retained phase=%s status=Running cancel_requested=true finished=false tid=%d", label, witness.tid)
	}
	assertStillBlocked("after_cancel_202")
	resources = request(http.MethodGet, "/admin/v1/runtime/resources", nil, admin)
	expectStatus(t, resources, http.StatusOK)
	retainedEvidence := objectValue(t, jsonObject(t, resources), "ScanEvidence")
	if retainedEvidence["Enabled"] != true || retainedEvidence["ActivePasses"] != float64(1) ||
		retainedEvidence["ReservedBytes"] != heldBytes || retainedEvidence["ReservedFileDescriptors"] != heldFDs {
		t.Fatal("cancellation released the blocked scan's evidence reservation before its worker returned")
	}
	t.Logf("blocked_scan_cancel_evidence phase=after_cancel active_passes=1 reserved_bytes=%.0f reserved_fds=%.0f", heldBytes, heldFDs)
	healthyJob := waitJob(startScan(1), "Completed")
	if healthyJob.message != "" || healthyJob.finished == nil || healthyJob.scanned != 1 || healthyJob.added != 0 || healthyJob.updated != 0 || healthyJob.cancelRequested {
		t.Fatalf("the independent healthy scan was affected by the blocked worker: %+v", healthyJob)
	}
	healthy := request(http.MethodGet, "/emby/Items?Ids="+items[1], nil, accounts.admin.headers)
	healthyItems, total := responseItems(t, healthy)
	if total != 1 || len(healthyItems) != 1 || healthyItems[0]["Id"] != items[1] {
		t.Fatal("healthy catalog queries changed while the other scan remained blocked")
	}
	healthy = request(http.MethodGet, "/emby/Videos/"+items[1]+"/stream.mp4?Static=true", nil, accounts.admin.headers)
	expectStatus(t, healthy, http.StatusOK)
	if !bytes.Equal(healthy.Body.Bytes(), mediaBytes) || snapshot() != baseline {
		t.Fatal("blocked scan cancellation affected healthy media or durable catalog/user state")
	}
	assertStillBlocked("after_healthy_scan_query_and_media")
	resumeTrace, err := os.Stat(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := device("resume"); err != nil {
		t.Fatal(err)
	}
	terminal := waitJob(blockedJob, "Cancelled")
	if !terminal.cancelRequested || terminal.finished == nil || terminal.scanned != 0 || terminal.added != 0 || terminal.updated != 0 {
		t.Fatalf("resumed cancellation lost its original pre-walk outcome: %+v", terminal)
	}
	phase3BlockedMetadataReturned(t, tracePath, resumeTrace.Size(), witness)
	deadline := time.Now().Add(10 * time.Second)
	for evidence() != baselineEvidence {
		if time.Now().After(deadline) {
			t.Fatal("cancelled scan did not retire its evidence reservations and cleanup state")
		}
		phase3ENOSPCPause(t, f.ctx)
	}
	if snapshot() != baseline {
		t.Fatal("cancelled scan changed the original catalog, root approval, or user data")
	}
	retried := waitJob(startScan(0), "Completed")
	if retried.message != "" || retried.finished == nil || retried.cancelRequested || retried.scanned != 1 || retried.added != 0 || retried.updated != 0 {
		t.Fatalf("the resumed original root did not support a fresh exact scan: %+v", retried)
	}
	if snapshot() != baseline || evidence() != baselineEvidence {
		t.Fatal("the successful retry changed retained state or leaked scan evidence")
	}
	stopChild()
	if t.Failed() {
		t.Fatal("the recovered scan process did not close normally")
	}
	t.Log("blocked_scan_cancel_passed=true actual_kernel_wait=true cancel_http_202=true cancel_persisted_before_worker_exit=true healthy_scan_query_and_media=true same_syscall_resumed=true cancelled_after_resume=true evidence_retired=true durable_state_preserved=true fresh_scan_completed=true apps_closed=true")
}

func phase3BlockedScanKernel(pid, tid int) (string, bool) {
	root := fmt.Sprintf("/proc/%d/task/%d", pid, tid)
	stat, err := os.ReadFile(root + "/stat")
	if err != nil {
		return "", false
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(string(stat[end+1:]))
	call, err := os.ReadFile(root + "/syscall")
	if err != nil || len(fields) < 20 || fields[0] != "D" || strings.TrimSpace(string(call)) == "running" {
		return "", false
	}
	return fields[19] + ":" + string(call), true
}

func phase3BlockedScanWaitKernel(t *testing.T, ctx context.Context, tracePath string, offset int64, mount string, pid int) phase3BlockedMetadataCall {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pending := make(map[int]string)
		for _, line := range strings.Split(phase3BlockedMetadataTrace(t, tracePath, offset), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			tid, err := strconv.Atoi(fields[0])
			if err != nil {
				continue
			}
			if strings.Contains(line, "resumed>") {
				delete(pending, tid)
			}
			if strings.Contains(line, "<unfinished ...>") && strings.Contains(line, mount) {
				for _, name := range []string{"openat", "openat2", "newfstatat", "statx", "getdents64"} {
					if strings.Contains(line, name+"(") {
						pending[tid] = name
					}
				}
			}
		}
		for tid, name := range pending {
			first, blocked := phase3BlockedScanKernel(pid, tid)
			if !blocked {
				continue
			}
			time.Sleep(200 * time.Millisecond)
			if second, blocked := phase3BlockedScanKernel(pid, tid); blocked && first == second {
				t.Logf("blocked_scan_cancel_kernel app_pid=%d tid=%d syscall=%s state=D stable_observations=2", pid, tid, name)
				return phase3BlockedMetadataCall{tid: tid, name: name}
			}
		}
		phase3ENOSPCPause(t, ctx)
	}
	t.Fatal("the scan did not enter an observable cold metadata kernel wait")
	return phase3BlockedMetadataCall{}
}
