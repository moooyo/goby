//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
)

// The external launcher owns a private mount namespace and an empty, bounded
// tmpfs. It must trace real writes with strace -f -yy -s 0 and keep that trace
// outside the tmpfs. This test never mounts storage or fills the host filesystem.
func TestHTTPPhase3PreviewENOSPCRecovery(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_ENOSPC") != "1" {
		t.Skip("GOBY_PHASE3_ENOSPC=1 is required for the isolated real ENOSPC scenario")
	}
	mount, blockSize := phase3ENOSPCMount(t)
	tracePath := os.Getenv("GOBY_PHASE3_ENOSPC_TRACE")
	traceInfo, err := os.Stat(tracePath)
	if err != nil || !traceInfo.Mode().IsRegular() || !filepath.IsAbs(tracePath) ||
		tracePath == mount || strings.HasPrefix(tracePath, mount+string(os.PathSeparator)) {
		t.Fatal("an existing syscall trace outside the isolated tmpfs is required")
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE", "GOTMPDIR"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required for real preview recovery", name)
		}
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	version := string(hlsHTTPMediaCommand(t, ffmpeg, "-version"))
	if !strings.HasPrefix(version, "ffmpeg version 9.") && !strings.HasPrefix(version, "ffmpeg version n9.") {
		t.Fatal("the ENOSPC scenario requires the real FFmpeg 9 toolchain")
	}
	f, accounts := newClientSessionHTTPAccounts(t, 10*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	if base == mount || strings.HasPrefix(base, mount+string(os.PathSeparator)) {
		t.Fatal("media and ordinary temporary files must remain outside the fault filesystem")
	}
	mediaRoot := filepath.Join(base, "media")
	if err := os.Mkdir(mediaRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(mediaRoot, "Preview ENOSPC.mp4")
	hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=12:duration=6",
		"-vf", "noise=alls=60:allf=t", "-an", "-c:v", "libx264", "-threads:v", "1",
		"-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "24", "-bf", "0", "-t", "6", mediaPath)
	cacheRoot := filepath.Join(mount, "cache")
	f.cfg.MediaRoots, f.cfg.FFmpegPath, f.cfg.FFprobePath = []string{mediaRoot}, ffmpeg, ffprobe
	f.cfg.MediaAnalysis = config.MediaAnalysisConfig{Enabled: true, CacheDirectory: cacheRoot,
		CacheMaxBytes: 64 << 20, CacheMaxEntries: 8, MaxEntryBytes: 8 << 20, MaxFileBytes: 1 << 20}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "phase3-preview-enospc")
	if err != nil {
		t.Fatalf("start the production preview server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close preview ENOSPC workers: %v", err)
		}
		t.Logf("preview_enospc_cleanup cache_preserved=true state=%+v", app.mediaAnalysis.cache.Stats())
	})
	f.app, f.handler = app, app.Handler()
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe, server: httptest.NewServer(f.handler)}
	t.Cleanup(h.server.Close)
	admin := http.Header{"Cookie": {accounts.cookie.String()}, "X-CSRF-Token": {csrfToken(accounts.cookie.Value)}}
	requestJSON := func(method, path string, body any, status int) map[string]any {
		t.Helper()
		response := h.request(t, method, path, body, admin)
		if response.status != status {
			t.Fatalf("preview ENOSPC HTTP status=%d expected=%d path=%s", response.status, status, path)
		}
		var value map[string]any
		if err := json.Unmarshal(response.body, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	created := requestJSON(http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Preview ENOSPC", "CollectionType": "movies", "Paths": []string{mediaRoot}, "Scan": true,
	}, http.StatusCreated)
	jobID := stringValue(t, objectValue(t, created, "Job"), "Id")
	for {
		job, err := app.library.GetJob(f.ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "Completed" {
			if job.Error != "" || job.Scanned != 1 || job.Added != 1 {
				t.Fatalf("real source scan did not complete exactly: %+v", job)
			}
			break
		}
		if job.Status != "Queued" && job.Status != "Running" {
			t.Fatalf("real source scan failed: %+v", job)
		}
		phase3ENOSPCPause(t, f.ctx)
	}
	var itemID string
	if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE path=$1 AND type='Movie'", mediaPath).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	response := h.request(t, http.MethodPost, "/emby/Users/"+accounts.admin.userID+"/Items/"+itemID+"/UserData",
		map[string]any{"IsFavorite": true, "Played": false, "PlayCount": 3, "PlaybackPositionTicks": 10_000_000}, accounts.admin.headers)
	expectHLSHTTPStatus(t, response, http.StatusOK)
	overview := requestJSON(http.MethodGet, "/admin/v1/media-analysis", nil, http.StatusOK)
	if objectValue(t, overview, "Runtime")["PreviewAvailable"] != true {
		t.Fatal("real preview dependencies are unavailable")
	}
	configuration := objectValue(t, overview, "Configuration")
	profile := objectValue(t, configuration, "Profile")
	profile["PreviewIntervalSeconds"] = 2
	requestJSON(http.MethodPut, "/admin/v1/media-analysis/configuration", map[string]any{
		"Revision": configuration["Revision"], "Profile": profile,
	}, http.StatusOK)
	runPreview := func(requestID, expected string) {
		t.Helper()
		admitted := requestJSON(http.MethodPost, "/admin/v1/media-analysis/runs", map[string]any{
			"Kind": "previews", "RequestId": requestID, "LibraryIds": []string{}, "ItemIds": []string{itemID}, "Force": true,
		}, http.StatusAccepted)
		runID := stringValue(t, admitted, "RunId")
		for {
			value := requestJSON(http.MethodGet, "/admin/v1/task-runs/"+runID+"?StartIndex=0&Limit=25", nil, http.StatusOK)
			run := objectValue(t, value, "Run")
			state := stringValue(t, run, "State")
			if state == "completed" || state == "failed" || state == "cancelled" || state == "interrupted" {
				children := objectValue(t, value, "Children")
				items, ok := children["Items"].([]any)
				if state != expected || !ok || len(items) != 1 || children["TotalRecordCount"] != float64(1) {
					t.Fatalf("preview terminal result differs: expected=%s run=%v children=%v", expected, run, children)
				}
				child := items[0].(map[string]any)
				wantScanned, wantUpdated, code := float64(1), float64(1), ""
				if expected == "failed" {
					wantScanned, wantUpdated, code = 0, 0, "executor_failed"
					if run["ErrorCode"] != "child_failed" {
						t.Fatalf("failed preview did not preserve its child failure: %v", run)
					}
				}
				if child["State"] != expected || child["ErrorCode"] != code || child["Scanned"] != wantScanned || child["Updated"] != wantUpdated {
					t.Fatalf("preview child did not preserve exact work outcome: %v", child)
				}
				t.Logf("preview_enospc_run request=%s state=%s child_error=%s scanned=%g updated=%g", requestID, state, code, wantScanned, wantUpdated)
				return
			}
			phase3ENOSPCPause(t, f.ctx)
		}
	}
	readBIF := func() ([]byte, int) {
		t.Helper()
		response := h.request(t, http.MethodGet, "/emby/Videos/"+itemID+"/index.bif?Width=240", nil, accounts.admin.headers)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		archive, err := bif.Open(bytes.NewReader(response.body), int64(len(response.body)), bif.DefaultLimits())
		if err != nil || archive.Len() != 3 {
			t.Fatalf("real six-second preview did not produce exactly three HTTP BIF frames: %v", err)
		}
		firstBytes := 0
		for index := 0; index < archive.Len(); index++ {
			frame, err := archive.JPEG(f.ctx, index)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := jpeg.Decode(bytes.NewReader(frame))
			if err != nil || decoded.Bounds().Dx() != 240 || decoded.Bounds().Dy() <= 0 {
				t.Fatalf("preview BIF contains an invalid real JPEG: %v", err)
			}
			if index == 0 {
				firstBytes = len(frame)
			}
		}
		return response.body, firstBytes
	}
	snapshot := func(table, order string) string {
		t.Helper()
		var value string
		if err := f.pool.QueryRow(f.ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY "+order+"),'[]'::jsonb)::text FROM "+table+" r").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	waitIdle := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
		defer cancel()
		for {
			stats := app.mediaAnalysis.cache.Stats()
			app.mediaAnalysis.mu.Lock()
			active := len(app.mediaAnalysis.activeOperations)
			app.mediaAnalysis.mu.Unlock()
			if stats.BuildingEntries == 0 && stats.PendingPublications == 0 && stats.ReservedBytes == 0 &&
				stats.Readers == 0 && active == 0 && len(app.mediaAnalysis.previewSlots) == 0 {
				return
			}
			if ctx.Err() != nil {
				t.Fatalf("preview work retained resources after its terminal result: cache=%+v operations=%d", stats, active)
			}
			phase3ENOSPCPause(t, ctx)
		}
	}
	runPreview("enospc-baseline", "completed")
	baselineBIF, firstJPEGBytes := readBIF()
	waitIdle()
	if int64(firstJPEGBytes) <= blockSize {
		t.Fatalf("the real first JPEG must exceed one filesystem block: jpeg=%d block=%d", firstJPEGBytes, blockSize)
	}
	baselineItems, baselineUserData := snapshot("items", "r.id"), snapshot("user_item_data", "r.user_id,r.item_id")
	baselinePreviews := snapshot("analysis_previews", "r.item_id,r.width")
	baselineCache := app.mediaAnalysis.cache.Stats()
	if baselineCache.ReadyEntries != 1 || baselineCache.BuildingEntries != 0 || baselineCache.ReservedBytes != 0 {
		t.Fatalf("baseline preview did not close its owned generation: %+v", baselineCache)
	}
	removeFiller := phase3ENOSPCFill(t, mount, blockSize)
	runPreview("enospc-pressure", "failed")
	phase3ENOSPCAssertPayloadTrace(t, tracePath, cacheRoot)
	if snapshot("items", "r.id") != baselineItems || snapshot("user_item_data", "r.user_id,r.item_id") != baselineUserData ||
		snapshot("analysis_previews", "r.item_id,r.width") != baselinePreviews {
		t.Fatal("failed preview changed catalog, user state, or a published preview row")
	}
	retainedBIF, _ := readBIF()
	if !bytes.Equal(retainedBIF, baselineBIF) {
		t.Fatal("ENOSPC replaced or truncated the previously published HTTP BIF")
	}
	waitIdle()
	afterFailure := app.mediaAnalysis.cache.Stats()
	if afterFailure != baselineCache || !app.mediaAnalysis.Available(library.TaskPreviewGenerationKey) {
		t.Fatalf("payload ENOSPC did not retire its reservation or withdrew healthy preview execution: %+v", afterFailure)
	}
	removeFiller()
	runPreview("enospc-recovery", "completed")
	readBIF()
	if snapshot("items", "r.id") != baselineItems || snapshot("user_item_data", "r.user_id,r.item_id") != baselineUserData ||
		snapshot("analysis_previews", "r.item_id,r.width") == baselinePreviews {
		t.Fatal("recovery changed durable media state or failed to publish a fresh forced preview")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = app.Close(ctx)
	cancel()
	if err != nil {
		t.Fatalf("close production preview resources: %v", err)
	}
	stats := app.mediaAnalysis.cache.Stats()
	app.mediaAnalysis.mu.Lock()
	active := len(app.mediaAnalysis.activeOperations)
	app.mediaAnalysis.mu.Unlock()
	if stats.BuildingEntries != 0 || stats.PendingPublications != 0 || stats.ReservedBytes != 0 || stats.Readers != 0 ||
		active != 0 || len(app.mediaAnalysis.previewSlots) != 0 || f.pool.Stat().AcquiredConns() != 0 {
		t.Fatalf("closed preview scenario retained resources: cache=%+v operations=%d", stats, active)
	}
	for _, entry := range phase3ENOSPCReadDir(t, cacheRoot) {
		if strings.HasPrefix(entry.Name(), "tmp-") || strings.HasPrefix(entry.Name(), "trash-") {
			t.Fatal("the failed or recovered preview retained an unfinished cache directory")
		}
	}
	t.Log("preview_enospc_passed=true kernel_payload_enospc=true old_preview_preserved=true forced_retry_completed=true http_bif_decoded=true resources_closed=true")
}

func phase3ENOSPCPause(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func phase3ENOSPCReadDir(t *testing.T, path string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func phase3ENOSPCMount(t *testing.T) (string, int64) {
	t.Helper()
	mount := os.Getenv("GOBY_PHASE3_ENOSPC_MOUNT")
	canonical, err := filepath.EvalSymlinks(mount)
	if err != nil || !filepath.IsAbs(mount) || canonical != mount || filepath.Clean(mount) != mount ||
		mount == "/" || strings.ContainsAny(mount, " \t\r\n\\") {
		t.Fatal("an exact canonical owned tmpfs mountpoint is required")
	}
	host := os.Getenv("GOBY_PHASE3_ENOSPC_HOST_MOUNT_NAMESPACE")
	pidOne, err := os.Readlink("/proc/1/ns/mnt")
	self, selfErr := os.Readlink("/proc/self/ns/mnt")
	if err != nil || selfErr != nil || host == "" || host != pidOne || self == host {
		t.Fatal("the ENOSPC test must run in an independent mount namespace")
	}
	info, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(string(info), "\n") {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			continue
		}
		fields := strings.Fields(parts[0])
		if len(fields) < 6 {
			t.Fatal("malformed mount namespace inventory")
		}
		for _, optional := range fields[6:] {
			if strings.HasPrefix(optional, "shared:") || strings.HasPrefix(optional, "master:") || strings.HasPrefix(optional, "propagate_from:") {
				t.Fatal("the fault namespace has propagating mounts")
			}
		}
		filesystem := strings.Fields(parts[1])
		if fields[4] == mount && len(filesystem) != 0 {
			found = filesystem[0] == "tmpfs" && fields[3] == "/"
		}
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(mount, &fs); err != nil || !found || fs.Type != unix.TMPFS_MAGIC || fs.Bsize <= 0 ||
		fs.Blocks < uint64((4<<20)/fs.Bsize) || fs.Blocks > uint64((32<<20)/fs.Bsize) || len(phase3ENOSPCReadDir(t, mount)) != 0 {
		t.Fatal("the empty independent tmpfs must be between 4 and 32 MiB")
	}
	t.Logf("preview_enospc_mount verified_private_tmpfs=true bytes=%d block_bytes=%d", fs.Blocks*uint64(fs.Bsize), fs.Bsize)
	return mount, fs.Bsize
}

func phase3ENOSPCFill(t *testing.T, mount string, blockSize int64) func() {
	t.Helper()
	path := filepath.Join(mount, "filler")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	remove := func() {
		t.Helper()
		if !removed {
			if err := os.Remove(path); err != nil {
				t.Fatalf("remove only the owned fault filler: %v", err)
			}
			removed = true
		}
	}
	t.Cleanup(func() { _ = file.Close(); remove() })
	buffer := make([]byte, 256<<10)
	var written int64
	for {
		n, writeErr := file.Write(buffer)
		written += int64(n)
		if errors.Is(writeErr, syscall.ENOSPC) {
			break
		}
		if writeErr != nil || n == 0 || written > 32<<20 {
			t.Fatalf("bounded tmpfs pressure did not produce real ENOSPC: written=%d error=%v", written, writeErr)
		}
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(mount, &fs); err != nil || fs.Bavail != 0 || written <= 2*blockSize || written%blockSize != 0 {
		t.Fatal("the filler did not exhaust actual page-aligned filesystem capacity")
	}
	// Leave room for the genuine owner marker, but not for the first JPEG.
	if err := file.Truncate(written - 2*blockSize); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Statfs(mount, &fs); err != nil || int64(fs.Bavail)*fs.Bsize != 2*blockSize {
		t.Fatal("the fault fixture did not leave exactly two data pages")
	}
	t.Logf("preview_enospc_pressure filler_errno=ENOSPC filler_bytes=%d remaining_bytes=%d", written-2*blockSize, 2*blockSize)
	return remove
}

func phase3ENOSPCAssertPayloadTrace(t *testing.T, tracePath, cacheRoot string) {
	t.Helper()
	file, err := os.Open(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, (16<<20)+1))
	if err != nil || len(raw) > 16<<20 {
		t.Fatal("the external syscall witness is unavailable or oversized")
	}
	payloadFailures, ownerFailures := 0, 0
	pending := make(map[string]string)
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid := fields[0]
		if pid == "[pid" && len(fields) > 1 {
			pid = strings.TrimSuffix(fields[1], "]")
		}
		if strings.Contains(line, "<unfinished ...>") {
			pending[pid] = line
			continue
		}
		if strings.Contains(line, "resumed>") {
			line = pending[pid] + line
			delete(pending, pid)
		}
		if !strings.Contains(line, cacheRoot+"/tmp-") || !strings.Contains(line, "= -1 ENOSPC") {
			continue
		}
		write := strings.Contains(line, "write(") || strings.Contains(line, "pwrite64(") ||
			strings.Contains(line, "writev(") || strings.Contains(line, "pwritev(")
		if write && strings.Contains(line, "/frame-000000.jpg") {
			payloadFailures++
		}
		if strings.Contains(line, "/.owner.json") {
			ownerFailures++
		}
	}
	if payloadFailures == 0 || ownerFailures != 0 {
		t.Fatalf("fault pressure must reach a real first-frame write, not owner initialization: payload_enospc=%d owner_enospc=%d", payloadFailures, ownerFailures)
	}
	t.Logf("preview_enospc_syscall_witness actual_frame_write_enospc=%d owner_write_enospc=%d", payloadFailures, ownerFailures)
}
