//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/moooyo/goby/internal/config"
)

// The launcher owns the private ext4/dm-linear mount, fixed suspend/resume
// control, syscall trace, and independent recovery watchdog. Only this test's
// media file receives cache advice; no global cache or existing device changes.
func TestHTTPPhase3BlockedReadRecovery(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_BLOCKED_READ") != "1" {
		t.Skip("GOBY_PHASE3_BLOCKED_READ=1 is required for actual blocked media reads")
	}
	mount, control, tracePath := os.Getenv("GOBY_PHASE3_BLOCKED_MOUNT"), os.Getenv("GOBY_PHASE3_BLOCKED_CONTROL"), os.Getenv("GOBY_PHASE3_BLOCKED_TRACE")
	canonical, err := filepath.EvalSymlinks(mount)
	host, _ := os.Readlink("/proc/1/ns/mnt")
	self, _ := os.Readlink("/proc/self/ns/mnt")
	var fs unix.Statfs_t
	if err != nil || !filepath.IsAbs(mount) || canonical != mount || mount == "/" ||
		host == "" || host != os.Getenv("GOBY_PHASE3_BLOCKED_HOST_MOUNT_NAMESPACE") || self == host ||
		unix.Statfs(mount, &fs) != nil || fs.Type != unix.EXT4_SUPER_MAGIC {
		t.Fatal("an owned ext4 fault mount in a private mount namespace is required")
	}
	for _, path := range []string{control, tracePath} {
		info, err := os.Stat(path)
		if err != nil || !filepath.IsAbs(path) || !info.Mode().IsRegular() || path == mount || strings.HasPrefix(path, mount+"/") {
			t.Fatal("fixed control and existing syscall trace must be outside the fault mount")
		}
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE", "GOTMPDIR"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required for blocked-read recovery", name)
		}
	}
	f, accounts := newClientSessionHTTPAccounts(t, 10*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-blocked-read-")
	if err != nil {
		t.Fatal(err)
	}
	if base == mount || strings.HasPrefix(base, mount+"/") {
		t.Fatal("healthy media and control artifacts must remain outside the fault filesystem")
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("blocked_read_artifacts_retained=%s", base)
		} else if err := os.RemoveAll(base); err != nil {
			t.Error(err)
		}
	})
	faultRoot, healthyRoot := filepath.Join(mount, "media"), filepath.Join(base, "healthy")
	for _, path := range []string{faultRoot, healthyRoot, filepath.Join(base, "evidence")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	seed := filepath.Join(base, "seed.mp4")
	hlsHTTPMediaCommand(t, os.Getenv("GOBY_FFMPEG"), "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=10", "-vf", "noise=alls=60:allf=t",
		"-an", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-g", "48", "-bf", "0", "-movflags", "+faststart", "-t", "10", seed)
	mediaBytes, err := os.ReadFile(seed)
	if err != nil || len(mediaBytes) < 256<<10 {
		t.Fatal("the real MP4 must contain at least 256 KiB for a cold interior range")
	}
	faultFile := filepath.Join(faultRoot, "Faulty.mp4")
	for _, path := range []string{faultFile, filepath.Join(healthyRoot, "Healthy.mp4")} {
		if err := os.WriteFile(path, mediaBytes, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg.MediaRoots = []string{filepath.Dir(mount), healthyRoot}
	f.cfg.FFmpegPath, f.cfg.FFprobePath = os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	f.cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: filepath.Join(base, "evidence"), MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "phase3-blocked-read")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close recovered read workers: %v", err)
		}
	})
	f.app, f.handler = app, app.Handler()
	var track atomic.Bool
	var activeHandler atomic.Int32
	faultURLPath := ""
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: f.cfg.FFmpegPath, ffprobe: f.cfg.FFprobePath}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if track.Load() && r.Method == http.MethodGet && r.URL.Path == faultURLPath {
			activeHandler.Add(1)
			defer activeHandler.Add(-1)
		}
		f.handler.ServeHTTP(w, r)
	}))
	t.Cleanup(h.server.Close)
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
		t.Logf("blocked_read_control action=%s completed=true", action)
		return nil
	}
	// This cleanup precedes listener and app closure even after an assertion fails.
	t.Cleanup(func() {
		if suspended {
			if err := device("resume"); err != nil {
				t.Error(err)
			}
		}
	})
	admin := http.Header{"Cookie": {accounts.cookie.String()}, "X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	requestJSON := func(method, path string, body any, headers http.Header, status int) map[string]any {
		t.Helper()
		response := h.request(t, method, path, body, headers)
		expectHLSHTTPStatus(t, response, status)
		var value map[string]any
		if err := json.Unmarshal(response.body, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	items := make([]string, 2)
	for index, path := range []string{faultRoot, healthyRoot} {
		created := requestJSON(http.MethodPost, "/admin/v1/libraries", map[string]any{
			"Name": fmt.Sprintf("Blocked read %d", index), "CollectionType": "movies", "Paths": []string{path}, "Scan": true,
		}, admin, http.StatusCreated)
		libraryID := stringValue(t, objectValue(t, created, "Library"), "Id")
		jobID := stringValue(t, objectValue(t, created, "Job"), "Id")
		for {
			job, err := app.library.GetJob(f.ctx, jobID)
			if err != nil {
				t.Fatal(err)
			}
			if job.Status == "Completed" {
				if job.Error != "" || job.Scanned != 1 || job.Added != 1 {
					t.Fatal("baseline scan did not publish exactly one real source")
				}
				break
			}
			if job.Status != "Queued" && job.Status != "Running" {
				t.Fatalf("baseline source scan failed: %+v", job)
			}
			phase3ENOSPCPause(t, f.ctx)
		}
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie'", libraryID).Scan(&items[index]); err != nil {
			t.Fatal(err)
		}
	}
	faultURLPath = "/emby/Videos/" + items[0] + "/stream.mp4"
	faultURL := faultURLPath + "?Static=true"
	requestJSON(http.MethodPost, "/emby/Users/"+accounts.admin.userID+"/Items/"+items[0]+"/UserData",
		map[string]any{"IsFavorite": true, "Played": false, "PlayCount": 7, "PlaybackPositionTicks": 10_000_000}, accounts.admin.headers, http.StatusOK)
	snapshot := func() string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
			'items',(SELECT jsonb_agg(to_jsonb(i) ORDER BY id) FROM items i),
			'userdata',(SELECT jsonb_agg(to_jsonb(u) ORDER BY user_id,item_id) FROM user_item_data u))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	baseline := snapshot()
	originals := func() map[string]any {
		t.Helper()
		return objectValue(t, requestJSON(http.MethodGet, "/admin/v1/runtime/resources", nil, admin, http.StatusOK), "OriginalStreams")
	}
	head := h.request(t, http.MethodHead, faultURL, nil, accounts.admin.headers)
	expectHLSHTTPStatus(t, head, http.StatusOK)
	if len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(mediaBytes)) {
		t.Fatal("metadata warmup HEAD did not identify the complete original source")
	}
	pageSize := os.Getpagesize()
	start := int64((len(mediaBytes) / 2 / pageSize) * pageSize)
	length := 4 * pageSize
	phase3BlockedReadColdRange(t, faultFile, start, length)
	if originals()["ActiveCount"] != float64(0) {
		t.Fatal("metadata warmup retained an original stream")
	}
	if err := device("suspend"); err != nil {
		t.Fatal(err)
	}
	traceInfo, err := os.Stat(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	traceOffset := traceInfo.Size()
	track.Store(true)
	readCtx, cancelRead := context.WithCancel(f.ctx)
	t.Cleanup(cancelRead)
	rangeHeader := fmt.Sprintf("bytes=%d-%d", start, start+int64(length)-1)
	readRequest, err := http.NewRequestWithContext(readCtx, http.MethodGet, h.server.URL+faultURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	readRequest.Header = accounts.admin.headers.Clone()
	readRequest.Header.Set("Range", rangeHeader)
	returned := make(chan error, 1)
	go func() {
		response, err := h.server.Client().Do(readRequest)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if err == nil {
				err = closeErr
			}
		}
		returned <- err
	}()
	witness := phase3BlockedReadWait(t, f.ctx, tracePath, traceOffset, faultFile)
	resources := originals()
	current := resources["Current"].([]any)
	if resources["ActiveCount"] != float64(1) || len(current) != 1 || activeHandler.Load() != 1 {
		t.Fatal("the kernel read is not owned by one active original HTTP handler")
	}
	lease := current[0].(map[string]any)
	leaseID := stringValue(t, lease, "LeaseId")
	if lease["ItemId"] != items[0] || lease["Active"] != true {
		t.Fatal("the retained original lease belongs to another source")
	}
	cancelRead()
	select {
	case err := <-returned:
		if err == nil {
			t.Fatal("the cancelled cold read unexpectedly completed successfully")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client cancellation did not return while its server read was blocked")
	}
	phase3BlockedReadStillBlocked(t, witness)
	resources = originals()
	if resources["ActiveCount"] != float64(1) || activeHandler.Load() != 1 {
		t.Fatal("client cancellation falsely retired the blocked server handler or original lease")
	}
	healthy := requestJSON(http.MethodGet, "/emby/Items?Ids="+items[1], nil, accounts.admin.headers, http.StatusOK)
	if healthy["TotalRecordCount"] != float64(1) || len(healthy["Items"].([]any)) != 1 || healthy["Items"].([]any)[0].(map[string]any)["Id"] != items[1] {
		t.Fatal("healthy catalog reads changed while payload I/O was blocked")
	}
	healthyMedia := h.request(t, http.MethodGet, "/emby/Videos/"+items[1]+"/stream.mp4?Static=true", nil, accounts.admin.headers)
	expectHLSHTTPStatus(t, healthyMedia, http.StatusOK)
	if !bytes.Equal(healthyMedia.body, mediaBytes) || snapshot() != baseline {
		t.Fatal("blocked payload affected healthy delivery or durable media state")
	}
	phase3BlockedReadStillBlocked(t, witness)
	resumeTrace, err := os.Stat(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := device("resume"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for activeHandler.Load() != 0 || originals()["ActiveCount"] != float64(0) {
		if time.Now().After(deadline) {
			t.Fatal("resumed source read did not release its handler and original lease")
		}
		phase3ENOSPCPause(t, f.ctx)
	}
	phase3BlockedReadReturned(t, tracePath, resumeTrace.Size(), witness)
	completed := false
	for _, raw := range originals()["Completed"].([]any) {
		entry := raw.(map[string]any)
		completed = completed || entry["LeaseId"] == leaseID && entry["ItemId"] == items[0] && entry["Active"] == false
	}
	if !completed {
		t.Fatal("the original lease did not move to completed history after the read returned")
	}
	for _, entry := range phase3ENOSPCReadDir(t, "/proc/self/fd") {
		if target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name())); err == nil && target == faultFile {
			t.Fatal("a resumed original source descriptor remains open")
		}
	}
	track.Store(false)
	headers := accounts.admin.headers.Clone()
	headers.Set("Range", rangeHeader)
	retry := h.request(t, http.MethodGet, faultURL, nil, headers)
	expectHLSHTTPStatus(t, retry, http.StatusPartialContent)
	if retry.header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, start+int64(length)-1, len(mediaBytes)) ||
		!bytes.Equal(retry.body, mediaBytes[int(start):int(start)+length]) || snapshot() != baseline {
		t.Fatal("recovered range changed its exact bytes or durable catalog/user state")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = app.Close(ctx)
	cancel()
	if err != nil {
		t.Fatalf("close the recovered original runtime: %v", err)
	}
	t.Log("blocked_read_passed=true target_pages_cold=true actual_kernel_payload_wait=true cancelled_client_server_lease_retained=true healthy_query_and_media=true same_syscall_resumed=true handler_and_fd_closed=true exact_range_retry=true durable_state_preserved=true")
}

func phase3BlockedReadColdRange(t *testing.T, path string, offset int64, length int) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Fadvise(int(file.Fd()), offset, int64(length), unix.FADV_DONTNEED); err != nil {
		t.Fatalf("discard only the owned range's clean pages: %v", err)
	}
	mapping, err := unix.Mmap(int(file.Fd()), offset, length, unix.PROT_NONE, unix.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Munmap(mapping)
	resident := make([]byte, (length+os.Getpagesize()-1)/os.Getpagesize())
	_, _, errno := unix.Syscall(unix.SYS_MINCORE, uintptr(unsafe.Pointer(&mapping[0])), uintptr(len(mapping)), uintptr(unsafe.Pointer(&resident[0])))
	if errno != 0 {
		t.Fatalf("inspect the owned payload range's residency: %v", errno)
	}
	for _, page := range resident {
		if page&1 != 0 {
			t.Fatal("target payload pages remain resident after scoped cache advice")
		}
	}
	t.Logf("blocked_read_cold_range offset=%d bytes=%d pages=%d resident_pages=0 global_cache_drop=false", offset, length, len(resident))
}

type phase3BlockedReadCall struct {
	tid          int
	name, kernel string
}

func phase3BlockedReadKernel(tid int) (string, bool) {
	root := fmt.Sprintf("/proc/%d/task/%d", os.Getpid(), tid)
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

func phase3BlockedReadStillBlocked(t *testing.T, call phase3BlockedReadCall) {
	t.Helper()
	first, blocked := phase3BlockedReadKernel(call.tid)
	if !blocked || first != call.kernel {
		t.Fatal("the identified source syscall stopped being blocked before device resume")
	}
	time.Sleep(200 * time.Millisecond)
	if second, blocked := phase3BlockedReadKernel(call.tid); !blocked || second != first {
		t.Fatal("the source read did not remain in the same kernel wait")
	}
}

func phase3BlockedReadWait(t *testing.T, ctx context.Context, tracePath string, offset int64, source string) phase3BlockedReadCall {
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
			if strings.Contains(line, "<unfinished ...>") && strings.Contains(line, "<"+source+">") {
				for _, name := range []string{"read", "pread64", "readv", "sendfile", "splice"} {
					if strings.Contains(line, " "+name+"(") {
						pending[tid] = name
					}
				}
			}
		}
		for tid, name := range pending {
			if kernel, blocked := phase3BlockedReadKernel(tid); blocked {
				call := phase3BlockedReadCall{tid: tid, name: name, kernel: kernel}
				phase3BlockedReadStillBlocked(t, call)
				t.Logf("blocked_read_kernel pid=%d tid=%d syscall=%s state=D source_fd_verified=true", os.Getpid(), tid, name)
				return call
			}
		}
		phase3ENOSPCPause(t, ctx)
	}
	t.Fatal("the cold HTTP range did not enter an observable source-file kernel read")
	return phase3BlockedReadCall{}
}

func phase3BlockedReadReturned(t *testing.T, path string, offset int64, call phase3BlockedReadCall) {
	t.Helper()
	for _, line := range strings.Split(phase3BlockedMetadataTrace(t, path, offset), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != strconv.Itoa(call.tid) || !strings.Contains(line, "<... "+call.name+" resumed>") {
			continue
		}
		at := strings.LastIndex(line, "= ")
		if at < 0 {
			t.Fatal("the source syscall resumed without an inspectable result")
		}
		result := strings.Fields(line[at+2:])
		if len(result) == 0 {
			t.Fatal("the source syscall resumed without a result value")
		}
		value, err := strconv.Atoi(result[0])
		if err != nil || value < 0 {
			t.Fatal("the original source read's first resumed kernel result was not successful")
		}
		t.Logf("blocked_read_kernel_return tid=%d syscall=%s bytes=%d completed_after_resume=true", call.tid, call.name, value)
		return
	}
	t.Fatal("the original source read has no resumed kernel result after device recovery")
}
