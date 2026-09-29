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
func TestHTTPPhase3BlockedMetadataRecovery(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_BLOCKED_METADATA") != "1" {
		t.Skip("GOBY_PHASE3_BLOCKED_METADATA=1 is required for actual blocked metadata")
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
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-blocked-metadata-")
	if err != nil {
		t.Fatal(err)
	}
	if base == mount || strings.HasPrefix(base, mount+"/") {
		t.Fatal("control artifacts and healthy media must remain outside the fault mount")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("blocked_metadata_artifacts_retained=%s", base)
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
		t.Fatal("write the private blocked-metadata child configuration")
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
				t.Errorf("blocked-metadata server did not close normally: %v", err)
			}
		case <-time.After(35 * time.Second):
			_ = current.command.Process.Kill()
			select {
			case <-current.done:
				current.joined = true
			case <-time.After(5 * time.Second):
			}
			t.Error("blocked-metadata server required forced cleanup after device recovery")
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
		t.Logf("blocked_metadata_control action=%s completed=true", action)
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
				t.Logf("blocked_metadata_app generation=%d pid=%d ready=true", generation, ready.PID)
				return
			}
			select {
			case err := <-child.done:
				child.joined = true
				t.Fatalf("blocked-metadata child startup failed: %v; inspect %s", err, logPath)
			case <-deadline:
				t.Fatalf("blocked-metadata child startup timed out; inspect %s", logPath)
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
			t.Fatalf("blocked-metadata HTTP transport failed (%T)", err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
		if err != nil || len(data) >= 8<<20 {
			t.Fatal("read the bounded blocked-metadata HTTP response")
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
	stopChild()
	if !current.joined || t.Failed() {
		t.Fatal("all original app descriptors must close before remount")
	}
	if err := device("remount"); err != nil {
		t.Fatal(err)
	}
	startChild()
	observations := func() float64 {
		t.Helper()
		response := request(http.MethodGet, "/admin/v1/runtime/resources", nil, admin)
		expectStatus(t, response, http.StatusOK)
		return objectValue(t, jsonObject(t, response), "StorageObservations")["Active"].(float64)
	}
	if observations() != 0 {
		t.Fatal("the restarted server already has a filesystem observation")
	}
	if err := device("suspend"); err != nil {
		t.Fatal(err)
	}
	traceInfo, err := os.Stat(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	traceOffset := traceInfo.Size()
	started := time.Now()
	response := request(http.MethodGet, bindingPath(0), nil, admin)
	expectAPIError(t, response, http.StatusServiceUnavailable, "library_unavailable", false)
	if time.Since(started) < 4*time.Second || observations() != 1 {
		t.Fatal("caller timeout did not retain exactly one actual filesystem observation")
	}
	witness := phase3BlockedMetadataWitness(t, tracePath, traceOffset, mount, current.command.Process.Pid)
	healthy := request(http.MethodGet, "/emby/Items?Ids="+items[1], nil, accounts.admin.headers)
	healthyItems, total := responseItems(t, healthy)
	if total != 1 || len(healthyItems) != 1 || healthyItems[0]["Id"] != items[1] {
		t.Fatal("healthy catalog queries were affected by the blocked root")
	}
	healthy = request(http.MethodGet, "/emby/Videos/"+items[1]+"/stream.mp4?Static=true", nil, accounts.admin.headers)
	expectStatus(t, healthy, http.StatusOK)
	if !bytes.Equal(healthy.Body.Bytes(), mediaBytes) {
		t.Fatal("healthy original delivery changed while metadata was blocked")
	}
	if observations() != 1 || snapshot() != baseline {
		t.Fatal("blocked metadata changed durable state or released its worker early")
	}
	resumeTraceInfo, err := os.Stat(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := device("resume"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for observations() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("resumed metadata worker did not return its observation slot")
		}
		phase3ENOSPCPause(t, f.ctx)
	}
	phase3BlockedMetadataReturned(t, tracePath, resumeTraceInfo.Size(), witness)
	recovered := objectValue(t, jsonObject(t, request(http.MethodGet, bindingPath(0), nil, admin)), "Binding")
	if recovered["Status"] != "verified" || recovered["ApprovedFingerprint"] != fingerprint || recovered["ObservedFingerprint"] != fingerprint || snapshot() != baseline {
		t.Fatal("original storage recovery changed its binding, catalog, or user state")
	}
	stopChild()
	if t.Failed() {
		t.Fatal("the recovered server did not close normally")
	}
	t.Log("blocked_metadata_passed=true actual_kernel_wait=true caller_timeout_worker_retained=true healthy_query_and_media=true same_syscall_resumed=true observation_slots_released=true durable_state_preserved=true binding_retry_verified=true apps_closed=true")
}

type phase3BlockedMetadataCall struct {
	tid  int
	name string
}

func phase3BlockedMetadataTrace(t *testing.T, path string, offset int64) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil || len(raw) > 16<<20 {
		t.Fatal("bounded external metadata trace is unavailable")
	}
	return string(raw)
}

func phase3BlockedMetadataWitness(t *testing.T, path string, offset int64, mount string, pid int) phase3BlockedMetadataCall {
	t.Helper()
	pending := make(map[int]string)
	for _, line := range strings.Split(phase3BlockedMetadataTrace(t, path, offset), "\n") {
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
	state := func(tid int) (string, bool) {
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
	for tid, name := range pending {
		first, blocked := state(tid)
		if !blocked {
			continue
		}
		time.Sleep(200 * time.Millisecond)
		second, stillBlocked := state(tid)
		if stillBlocked && first == second {
			t.Logf("blocked_metadata_kernel app_pid=%d tid=%d syscall=%s state=D stable_after_caller_return=true", pid, tid, name)
			return phase3BlockedMetadataCall{tid: tid, name: name}
		}
	}
	t.Fatal("no live application metadata syscall remained blocked in the kernel after caller timeout")
	return phase3BlockedMetadataCall{}
}

func phase3BlockedMetadataReturned(t *testing.T, path string, offset int64, call phase3BlockedMetadataCall) {
	t.Helper()
	for _, line := range strings.Split(phase3BlockedMetadataTrace(t, path, offset), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != strconv.Itoa(call.tid) || !strings.Contains(line, "<... "+call.name+" resumed>") {
			continue
		}
		at := strings.LastIndex(line, "= ")
		if at < 0 {
			t.Fatal("the blocked metadata syscall resumed without an inspectable result")
		}
		result := strings.Fields(line[at+2:])
		if len(result) == 0 {
			t.Fatal("the blocked metadata syscall resumed without a result value")
		}
		number := strings.SplitN(result[0], "<", 2)[0]
		if value, err := strconv.Atoi(number); err == nil && value >= 0 {
			t.Logf("blocked_metadata_kernel_return tid=%d syscall=%s completed_after_resume=true", call.tid, call.name)
			return
		}
		t.Fatal("the originally blocked metadata syscall's first resumed result was not successful")
	}
	t.Fatal("the originally blocked metadata syscall has no successful resumed kernel result")
}
