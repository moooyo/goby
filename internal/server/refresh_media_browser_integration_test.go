//go:build linux && goby_embed_admin && goby_browser_integration

package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	iofs "io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
	adminassets "github.com/moooyo/goby/web/admin"
	"golang.org/x/sys/unix"
)

const refreshBrowserTitle = "fresh native refresh task completes, cancels active library work, and runs a removable interval"

type refreshBrowserLibrary struct {
	ID        string `json:"Id"`
	Name      string `json:"Name"`
	Path      string `json:"Path"`
	FileCount int    `json:"FileCount"`
}

type refreshBrowserManifest struct {
	Marker        string
	RunID         string `json:"RunId"`
	Origin        string
	Administrator struct {
		ID   string `json:"Id"`
		Name string
	}
	TaskID                string `json:"TaskId"`
	TaskKey               string
	ScanTaskID            string `json:"ScanTaskId"`
	Libraries             []refreshBrowserLibrary
	PreservedItemIDs      []string `json:"PreservedItemIds"`
	ResultPath            string
	CancellationReadyPath string
}

type refreshBrowserResult struct {
	Marker           string
	RunID            string `json:"RunId"`
	Complete         bool
	TaskID           string   `json:"TaskId"`
	ScanTaskID       string   `json:"ScanTaskId"`
	ManualRunID      string   `json:"ManualRunId"`
	CancelledRunID   string   `json:"CancelledRunId"`
	IntervalRunIDs   []string `json:"IntervalRunIds"`
	Libraries        []refreshBrowserLibrary
	FinalRevision    string
	FinalTriggers    []json.RawMessage
	FinalTimezone    string
	BrowserSessionID string `json:"BrowserSessionId"`
	BrowserCookie    string
	BrowserCSRF      string
	Checks           map[string]bool
	Observations     struct{ ActiveStopDialogObserved bool }
}

// The third probe of each path is an owned cancellation gate, not a simulated
// kernel or ffprobe hang. Every other invocation uses the real media prober.
type refreshBrowserProber struct {
	media.Prober
	mu               sync.Mutex
	calls            map[string]int
	realCalls        int
	cancelled        int
	entered          int
	readyPath, runID string
	cleanup          chan struct{}
	cleanupOnce      sync.Once
	gateError        error
}

func (p *refreshBrowserProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", file.Fd()))
	if err != nil {
		return media.Info{}, err
	}
	p.mu.Lock()
	count, owned := p.calls[path]
	if !owned {
		p.mu.Unlock()
		return media.Info{}, errors.New("unexpected browser fixture probe path")
	}
	count++
	p.calls[path] = count
	if count == 3 {
		p.entered++
		if p.entered == 2 {
			p.gateError = refreshBrowserWriteJSON(p.readyPath, map[string]any{"RunId": p.runID, "Ready": true})
		}
		err = p.gateError
		p.mu.Unlock()
		if err != nil {
			return media.Info{}, err
		}
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.cancelled++
			p.mu.Unlock()
			return media.Info{}, ctx.Err()
		case <-p.cleanup:
			return media.Info{}, context.Canceled
		case <-timer.C:
			return media.Info{}, errors.New("browser cancellation gate deadline")
		}
	}
	p.realCalls++
	p.mu.Unlock()
	return p.Prober.ProbeFile(ctx, file)
}

func (p *refreshBrowserProber) release() { p.cleanupOnce.Do(func() { close(p.cleanup) }) }

func (p *refreshBrowserProber) counts() (map[string]int, int, int, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	copy := make(map[string]int, len(p.calls))
	for path, count := range p.calls {
		copy[path] = count
	}
	return copy, p.realCalls, p.entered, p.cancelled, p.gateError
}

func refreshBrowserWriteJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func refreshBrowserPath(t *testing.T, key string, directory bool) string {
	t.Helper()
	path := os.Getenv(key)
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		t.Fatalf("%s must name an existing absolute path", key)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		t.Fatalf("%s must be canonical", key)
	}
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() != directory || (!directory && !info.Mode().IsRegular()) {
		t.Fatalf("%s has the wrong file type", key)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || st.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("%s must be root-owned and not group/world writable", key)
	}
	return path
}

type refreshBrowserLimitedOutput struct {
	file      *os.File
	remaining int64
}

func (w *refreshBrowserLimitedOutput) Write(raw []byte) (int, error) {
	if int64(len(raw)) > w.remaining {
		return 0, errors.New("browser output limit")
	}
	n, err := w.file.Write(raw)
	w.remaining -= int64(n)
	return n, err
}

type refreshBrowserProcess struct {
	PID   int
	Start uint64
}

func refreshBrowserProcessAt(pid int) (refreshBrowserProcess, bool) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return refreshBrowserProcess{}, false
	}
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return refreshBrowserProcess{}, false
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) < 20 || fields[0] == "Z" || fields[0] == "X" {
		return refreshBrowserProcess{}, false
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	return refreshBrowserProcess{PID: pid, Start: start}, err == nil
}

// Playwright may place Chromium in its own process group. Track descendants
// of this exact Node invocation as well as its original process group.
func refreshBrowserCollectProcesses(owned map[int]refreshBrowserProcess) error {
	pending := make([]int, 0, len(owned))
	for pid := range owned {
		pending = append(pending, pid)
	}
	seen := make(map[int]bool)
	for len(pending) > 0 {
		pid := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		if current, alive := refreshBrowserProcessAt(pid); !alive || current != owned[pid] {
			continue
		}
		paths, err := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", pid))
		if err != nil {
			return err
		}
		for _, path := range paths {
			raw, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || len(raw) > 65536 {
				return errors.New("browser child inventory unavailable")
			}
			for _, text := range strings.Fields(string(raw)) {
				child, err := strconv.Atoi(text)
				if err != nil || child <= 1 {
					return errors.New("browser child identity invalid")
				}
				if observed, alive := refreshBrowserProcessAt(child); alive {
					if previous, exists := owned[child]; exists && previous != observed {
						return errors.New("browser child pid reused")
					}
					owned[child] = observed
					pending = append(pending, child)
					if len(owned) > 256 {
						return errors.New("browser child inventory exceeded")
					}
				}
			}
		}
	}
	return nil
}

func refreshBrowserSignal(process refreshBrowserProcess, sig unix.Signal) error {
	current, alive := refreshBrowserProcessAt(process.PID)
	if !alive {
		return nil
	}
	if current != process {
		return errors.New("owned browser pid changed before signal")
	}
	fd, err := unix.PidfdOpen(process.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	current, alive = refreshBrowserProcessAt(process.PID)
	if !alive {
		return nil
	}
	if current != process {
		return errors.New("owned browser pid changed after pidfd open")
	}
	if err := unix.PidfdSendSignal(fd, sig, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return err
	}
	return nil
}

func refreshBrowserCommand(ctx context.Context, executable string, arguments, environment []string, work, output string) (map[string]any, error) {
	stdout, err := os.OpenFile(filepath.Join(output, "stdout.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(output, "stderr.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	defer stderr.Close()
	cmd := exec.CommandContext(ctx, executable, arguments...)
	cmd.Dir, cmd.Env = work, environment
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout = &refreshBrowserLimitedOutput{stdout, 8 << 20}
	cmd.Stderr = &refreshBrowserLimitedOutput{stderr, 8 << 20}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	started := time.Now()
	if err := cmd.Start(); err != nil {
		return map[string]any{"Started": false}, err
	}
	root, alive := refreshBrowserProcessAt(cmd.Process.Pid)
	owned := map[int]refreshBrowserProcess{cmd.Process.Pid: root}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr, inventoryErr error
	if !alive {
		inventoryErr = errors.New("browser launcher exited before identity observation")
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	waiting := true
	for waiting {
		if err := refreshBrowserCollectProcesses(owned); err != nil && inventoryErr == nil {
			inventoryErr = err
		}
		select {
		case waitErr = <-done:
			waiting = false
		case <-ticker.C:
		}
	}
	// Natural Playwright teardown should already have closed every descendant.
	// A bounded failure cleanup only signals identities observed below Node.
	forced := 0
	for _, phase := range []struct {
		signal  unix.Signal
		timeout time.Duration
	}{{unix.SIGTERM, 5 * time.Second}, {unix.SIGKILL, 5 * time.Second}} {
		until := time.Now().Add(phase.timeout)
		for {
			if err := refreshBrowserCollectProcesses(owned); err != nil && inventoryErr == nil {
				inventoryErr = err
			}
			remaining := 0
			for _, process := range owned {
				if current, live := refreshBrowserProcessAt(process.PID); live && current == process {
					remaining++
					forced++
					if err := refreshBrowserSignal(process, phase.signal); err != nil && inventoryErr == nil {
						inventoryErr = err
					}
				}
			}
			if remaining == 0 || time.Now().After(until) {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	closed := true
	for _, process := range owned {
		if current, live := refreshBrowserProcessAt(process.PID); live && current == process {
			closed = false
		}
	}
	groupClosed := errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH)
	stdout.Sync()
	stderr.Sync()
	result := map[string]any{"Started": true, "PID": cmd.Process.Pid, "ProcessGroup": cmd.Process.Pid,
		"ExitCode": cmd.ProcessState.ExitCode(), "ElapsedMilliseconds": time.Since(started).Milliseconds(),
		"ObservedProcessIdentities": len(owned), "ObservedDescendantsClosed": closed, "ProcessGroupClosed": groupClosed,
		"FinalClosureRequiresDedicatedWorkerCgroupEmpty": true,
		"FailureCleanupSignals":                          forced, "OutputFiles": []string{"stdout.log", "stderr.log"}}
	if !closed || !groupClosed {
		return result, errors.New("owned browser processes remain")
	}
	if inventoryErr != nil {
		return result, inventoryErr
	}
	return result, waitErr
}

func refreshBrowserSnapshot(t *testing.T, f *serverFixture) string {
	t.Helper()
	var value string
	err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'libraries',(SELECT jsonb_agg(to_jsonb(l)-'last_scan_at' ORDER BY id) FROM libraries l),
		'roots',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM library_roots r),
		'directories',(SELECT jsonb_agg(to_jsonb(i)-'updated_at' ORDER BY id) FROM items i WHERE is_folder),
		'items',(SELECT jsonb_agg(jsonb_build_object('id',id,'library',library_id,'root',root_id,'parent',parent_id,
			'path',path,'type',type,'name',name,'sort',sort_name,'overview',overview,'local',local_metadata,
			'localHash',local_metadata_hash,'localPath',local_metadata_path) ORDER BY id) FROM items WHERE NOT is_folder),
		'metadata',(SELECT jsonb_agg(to_jsonb(m) ORDER BY item_id) FROM item_metadata_state m),
		'userdata',(SELECT jsonb_agg(to_jsonb(u)||jsonb_build_object('xmin',u.xmin::text) ORDER BY user_id,item_id) FROM user_item_data u)
	)::text`).Scan(&value)
	if err != nil {
		t.Fatal("read owned browser preservation snapshot")
	}
	return value
}

func refreshBrowserEntitySnapshot(t *testing.T, f *serverFixture) string {
	t.Helper()
	var value string
	if f.pool.QueryRow(f.ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY item_id,entity_id,position),'[]'::jsonb)::text FROM item_entities e").Scan(&value) != nil {
		t.Fatal("read owned genre relationship snapshot")
	}
	return value
}

func refreshBrowserWaitJob(t *testing.T, f *serverFixture, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 30*time.Second)
	defer cancel()
	for {
		job, err := f.app.library.GetJob(ctx, id)
		if err != nil {
			t.Fatal("read browser seed scan")
		}
		if job.Status == "Completed" {
			if job.Error != "" || job.ForceProbe || job.Scanned != 1 {
				t.Fatal("browser seed scan facts differ")
			}
			return
		}
		if job.Status != "Queued" && job.Status != "Running" {
			t.Fatal("browser seed scan did not complete")
		}
		select {
		case <-ctx.Done():
			t.Fatal("browser seed scan deadline")
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func refreshBrowserMediaFacts(path string) (map[string]any, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<10 {
		return nil, errors.New("owned media file metadata")
	}
	value, ok := info.Sys().(*syscall.Stat_t)
	if !ok || value.Nlink != 1 {
		return nil, errors.New("owned media file links")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	return map[string]any{"Path": path, "Device": uint64(value.Dev), "Inode": value.Ino, "UID": value.Uid, "GID": value.Gid,
		"Mode": uint32(info.Mode().Perm()), "Bytes": info.Size(), "ModifiedNs": info.ModTime().UnixNano(), "ChangedNs": value.Ctim.Nano(), "SHA256": hex.EncodeToString(hash[:])}, nil
}

func refreshBrowserAssetInventory(assets iofs.FS) ([]map[string]any, error) {
	rows := []map[string]any{}
	hasEntry := false
	err := iofs.WalkDir(assets, ".", func(path string, entry iofs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("administrator asset %q must be a regular file", path)
		}
		raw, err := iofs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		if path == "index.html" {
			if len(raw) == 0 {
				return errors.New("administrator HTML entry must not be empty")
			}
			hasEntry = true
		}
		checksum := sha256.Sum256(raw)
		rows = append(rows, map[string]any{"Path": path, "Bytes": len(raw), "SHA256": hex.EncodeToString(checksum[:])})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !hasEntry {
		return nil, errors.New("administrator bundle must contain index.html")
	}
	return rows, nil
}

func TestNativeRefreshMediaAdministratorBrowser(t *testing.T) {
	// Explicit build tags are admission: unavailable required inputs fail rather
	// than appearing as a successful ordinary-suite skip.
	if os.Geteuid() != 0 || os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("root and an owned integration database are required")
	}
	ffmpeg := refreshBrowserPath(t, "GOBY_FFMPEG", false)
	ffprobe := refreshBrowserPath(t, "GOBY_FFPROBE", false)
	node := refreshBrowserPath(t, "GOBY_TEST_BROWSER_NODE", false)
	cli := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_CLI", false)
	work := refreshBrowserPath(t, "GOBY_TEST_PLAYWRIGHT_WORK", true)
	artifacts := refreshBrowserPath(t, "GOBY_TEST_BROWSER_ARTIFACTS_DIR", true)
	browserCache := refreshBrowserPath(t, "PLAYWRIGHT_BROWSERS_PATH", true)
	if info, _ := os.Stat(artifacts); info.Mode().Perm() != 0o700 {
		t.Fatal("browser artifact parent must be private")
	}
	runID := os.Getenv("GOBY_REFRESH_TASK_RUN_ID")
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("a valid explicit browser run ID is required")
	}
	for _, name := range []string{"playwright.config.ts", "e2e/scheduled-tasks.spec.ts"} {
		path := filepath.Join(work, name)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("the owned Playwright work copy is incomplete")
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal("read browser driver working directory")
	}
	sourceRoot := ""
	for directory := cwd; directory != filepath.Dir(directory); directory = filepath.Dir(directory) {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			sourceRoot = directory
			relative, err := filepath.Rel(directory, work)
			if err != nil || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
				t.Fatal("Playwright work must be a writable copy outside frozen Go source")
			}
			break
		}
	}
	if sourceRoot == "" {
		t.Fatal("locate frozen browser source root")
	}
	output, err := os.MkdirTemp(artifacts, "refresh-media-browser-")
	if err != nil {
		t.Fatal("create private browser artifact directory")
	}
	if err := os.Chmod(output, 0o700); err != nil {
		t.Fatal("protect browser artifact directory")
	}
	driver := map[string]any{"Marker": "goby-refresh-task-browser-driver-v1", "RunId": runID, "Complete": false,
		"ArtifactDirectory": output, "GateMeaning": "owned context-cancellation gate; not a kernel or ffprobe hang", "SchemaDisposalIsNotLogout": true}
	t.Cleanup(func() {
		driver["GoTestFailed"] = t.Failed()
		if t.Failed() {
			driver["Complete"] = false
		}
		if err := refreshBrowserWriteJSON(filepath.Join(output, "driver-result.json"), driver); err != nil {
			t.Error("preserve private browser driver result")
		}
		if !t.Failed() && driver["Complete"] == true {
			t.Log("refresh_media_browser_passed=true cleanup_completed=true owned_sessions_revoked=true")
		}
	})
	f := newServerFixtureWithTimeout(t, 5*time.Minute)
	root := t.TempDir()
	readyPath := filepath.Join(output, "cancellation-ready.json")
	prober := &refreshBrowserProber{Prober: media.Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: true, Timeout: 15 * time.Second}, calls: make(map[string]int), cleanup: make(chan struct{}), readyPath: readyPath, runID: runID}
	paths := []string{filepath.Join(root, "library-a", "Movie.mp4"), filepath.Join(root, "library-b", "Movie.mp4")}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal("create owned media directory")
		}
		prober.calls[path] = 0
	}
	generateCtx, generateCancel := context.WithTimeout(f.ctx, 15*time.Second)
	generate := exec.CommandContext(generateCtx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=64x48:r=1:d=1", "-an", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-movflags", "+faststart", "-y", paths[0])
	generate.WaitDelay = 2 * time.Second
	generateErr := generate.Run()
	generateCancel()
	if generateErr != nil {
		t.Fatal("generate the owned tiny MP4")
	}
	data, err := os.ReadFile(paths[0])
	if err != nil || len(data) == 0 || len(data) > 128<<10 {
		t.Fatal("tiny MP4 exceeds its admitted bound")
	}
	if err := os.WriteFile(filepath.Join(output, "media-template.mp4"), data, 0o600); err != nil {
		t.Fatal("preserve the private real media template")
	}
	if err := os.WriteFile(paths[1], data, 0o600); err != nil {
		t.Fatal("create the independent second media inode")
	}
	a, errA := os.Stat(paths[0])
	b, errB := os.Stat(paths[1])
	if errA != nil || errB != nil || os.SameFile(a, b) {
		t.Fatal("browser media copies do not have distinct readable inodes")
	}
	allFiles := append([]string{}, paths...)
	for index, path := range paths {
		nfo := filepath.Join(filepath.Dir(path), "Movie.nfo")
		content := fmt.Sprintf("<movie><title>Automatic refresh item %d</title><plot>Preserved source overview.</plot><genre>Source refresh genre</genre></movie>\n", index+1)
		if err := os.WriteFile(nfo, []byte(content), 0o600); err != nil {
			t.Fatal("write owned real NFO source")
		}
		allFiles = append(allFiles, nfo)
	}
	mediaDigest := sha256.Sum256(data)
	driver["MediaFiles"] = 2
	driver["MediaBytesEach"] = len(data)
	driver["MediaSha256"] = hex.EncodeToString(mediaDigest[:])
	mediaBefore := make(map[string]map[string]any)
	for _, path := range allFiles {
		facts, err := refreshBrowserMediaFacts(path)
		if err != nil {
			t.Fatal("capture owned media baseline")
		}
		mediaBefore[path] = facts
	}
	driver["MediaBefore"] = mediaBefore
	driver["NFOFiles"] = 2
	f.app.notifier.Close()
	f.app.catalogNotifier.Close()
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, prober, []string{root})
	if err != nil {
		t.Fatal("create real browser media catalog")
	}
	f.app.cfg.MediaRoots = []string{root}
	f.cfg.MediaRoots = []string{root}
	f.app.cfg.FFmpegPath, f.cfg.FFmpegPath = ffmpeg, ffmpeg
	f.app.cfg.FFprobePath, f.cfg.FFprobePath = ffprobe, ffprobe
	installFixtureCatalog(t, f, catalog)
	f.app.notifier = newUserDataNotifier(catalog, f.app.eventHub)
	f.app.catalogNotifier = newLibraryNotifier(catalog, f.app.eventHub)
	t.Cleanup(func() { f.app.notifier.Close(); f.app.catalogNotifier.Close() })
	t.Cleanup(prober.release)
	assets, err := adminassets.Files()
	if err != nil {
		t.Fatal("open the real embedded administrator bundle")
	}
	assetRows, err := refreshBrowserAssetInventory(assets)
	if err != nil {
		t.Fatalf("inventory the embedded administrator bundle: %v", err)
	}
	bundlePath := filepath.Join(sourceRoot, "web", "admin", "dist")
	if resolved, err := filepath.EvalSymlinks(bundlePath); err != nil || resolved != bundlePath {
		t.Fatal("the frozen administrator bundle must have a canonical source path")
	}
	sourceAssetRows, err := refreshBrowserAssetInventory(os.DirFS(bundlePath))
	if err != nil {
		t.Fatalf("inventory the frozen administrator bundle: %v", err)
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "embedded-assets.json"), assetRows); err != nil {
		t.Fatal("preserve embedded asset inventory")
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "source-assets.json"), sourceAssetRows); err != nil {
		t.Fatal("preserve frozen source asset inventory")
	}
	driver["EmbeddedAssetCount"] = len(assetRows)
	driver["SourceAssetCount"] = len(sourceAssetRows)
	// The runner binds the frozen source archive; compare its complete bundle,
	// including each file's bytes, instead of one historical Vite chunk count.
	if !reflect.DeepEqual(assetRows, sourceAssetRows) {
		t.Fatal("the embedded administrator bundle must match every frozen source asset")
	}
	driver["EmbeddedAssetsMatchFrozenSource"] = true
	WithDashboardAssets(assets)(f.app)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve owned browser HTTP listener")
	}
	actual := httptest.NewUnstartedServer(nil)
	actual.Listener.Close()
	actual.Listener = listener
	origin := "http://" + listener.Addr().String()
	f.cfg.PublicURL, f.cfg.CookieSecure = origin, false
	f.app.cfg.PublicURL, f.app.cfg.CookieSecure = origin, false
	f.handler = f.app.Handler()
	actual.Config.Handler = f.handler
	actual.Start()
	t.Cleanup(actual.Close)
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal("generate private browser administrator password")
	}
	password, username := hex.EncodeToString(secret[:]), "Refresh browser "+runID
	response := f.request(t, http.MethodPost, "/admin/v1/bootstrap", map[string]any{"SetupToken": f.cfg.SetupToken, "Name": username, "Password": password}, http.Header{"Origin": {origin}})
	if response.Code != http.StatusCreated {
		t.Fatal("bootstrap owned browser administrator")
	}
	var bootstrap struct {
		User struct {
			ID string `json:"Id"`
		}
	}
	if json.Unmarshal(response.Body.Bytes(), &bootstrap) != nil || bootstrap.User.ID == "" {
		t.Fatal("read private browser administrator identity")
	}
	setupCredential, err := f.users.Authenticate(f.ctx, username, password, identity.Client{Name: "Refresh browser fixture setup", DeviceID: "refresh-setup-" + runID}, "admin")
	if err != nil {
		t.Fatal("authenticate owned metadata fixture setup")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := f.users.Revoke(ctx, setupCredential.Token); err != nil {
			t.Error("revoke owned metadata setup credential")
		}
	})
	setupActor, err := f.users.Resolve(f.ctx, setupCredential.Token, "admin")
	if err != nil {
		t.Fatal("resolve owned metadata fixture setup")
	}
	refresh, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryRefreshMediaKey)
	if err != nil {
		t.Fatal("read refresh task definition")
	}
	normal, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryScanKey)
	if err != nil || len(refresh.Triggers) != 0 || len(normal.Triggers) != 0 {
		t.Fatal("browser task baseline has schedules")
	}
	var initialRuns int
	if f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_runs").Scan(&initialRuns) != nil || initialRuns != 0 {
		t.Fatal("browser task baseline has runs")
	}
	fixture := refreshBrowserManifest{Marker: "goby-refresh-task-browser-fixtures-v1", RunID: runID, Origin: origin, TaskID: refresh.ID, TaskKey: tasks.LibraryRefreshMediaKey, ScanTaskID: normal.ID, ResultPath: filepath.Join(output, "browser-result.json"), CancellationReadyPath: readyPath}
	fixture.Administrator.ID, fixture.Administrator.Name = bootstrap.User.ID, username
	for index, path := range paths {
		collection, err := catalog.CreateLibrary(f.ctx, fmt.Sprintf("Refresh browser %s %d", runID, index+1), "movies", []string{filepath.Dir(path)})
		if err != nil {
			t.Fatal("create browser fixture library")
		}
		job, err := catalog.StartScan(f.ctx, collection.ID)
		if err != nil {
			t.Fatal("start cold browser fixture scan")
		}
		refreshBrowserWaitJob(t, f, job.ID)
		var id string
		if f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Movie' AND path=$2", collection.ID, path).Scan(&id) != nil {
			t.Fatal("read real media item identity")
		}
		detail, err := catalog.GetItemMetadata(f.ctx, setupActor, id)
		if err != nil || !reflect.DeepEqual(detail.Automatic.Genres, []string{"Source refresh genre"}) {
			t.Fatal("real NFO genre was not discovered")
		}
		nameJSON, _ := json.Marshal(fmt.Sprintf("Manual refresh item %d", index+1))
		genreJSON, _ := json.Marshal([]string{"Manual refresh genre"})
		if _, err := catalog.UpdateItemMetadata(f.ctx, setupActor, id, library.MetadataEdit{Revision: detail.Revision,
			Overrides: map[string]json.RawMessage{"Name": nameJSON, "Genres": genreJSON}, LockedFields: []string{"Overview"}}); err != nil {
			t.Fatal("store nonempty owned metadata overrides and lock")
		}
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,playback_position_ticks,play_count,is_favorite,played,last_played_at) VALUES($1,$2,420000000,3,true,false,'2026-01-02T03:04:05Z')`, bootstrap.User.ID, id); err != nil {
			t.Fatal("seed owned persistent user data")
		}
		fixture.Libraries = append(fixture.Libraries, refreshBrowserLibrary{collection.ID, collection.Name, filepath.Dir(path), 1})
		fixture.PreservedItemIDs = append(fixture.PreservedItemIDs, id)
	}
	if err := f.users.Revoke(f.ctx, setupCredential.Token); err != nil {
		t.Fatal("retire fixture setup authentication before the browser")
	}
	if _, err := f.users.Resolve(f.ctx, setupCredential.Token, "admin"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("fixture setup authentication remains usable")
	}
	driver["MetadataSetupCredentialRevoked"] = true
	expectedEntities := refreshBrowserEntitySnapshot(t, f)
	if err := refreshBrowserWriteJSON(filepath.Join(output, "genre-index-expected.json"), json.RawMessage(expectedEntities)); err != nil {
		t.Fatal("save expected owned genre relationships")
	}
	removed, err := f.pool.Exec(f.ctx, "DELETE FROM item_entities i USING catalog_entities e WHERE i.entity_id=e.id AND e.kind='Genre' AND i.item_id=$1", fixture.PreservedItemIDs[0])
	if err != nil || removed.RowsAffected() != 1 {
		t.Fatal("remove exactly one owned genre relationship")
	}
	missingEntities := refreshBrowserEntitySnapshot(t, f)
	if missingEntities == expectedEntities {
		t.Fatal("owned genre relationship was not removed")
	}
	cached, err := catalog.StartScan(f.ctx, fixture.Libraries[0].ID)
	if err != nil {
		t.Fatal("start owned ordinary cache-hit scan")
	}
	refreshBrowserWaitJob(t, f, cached.ID)
	coldCounts, coldRealCalls, _, _, _ := prober.counts()
	if coldRealCalls != 2 || coldCounts[paths[0]] != 1 || coldCounts[paths[1]] != 1 || refreshBrowserEntitySnapshot(t, f) != missingEntities {
		t.Fatal("ordinary cached scan unexpectedly probed or repaired the removed genre relationship")
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "genre-index-missing-after-normal-scan.json"), json.RawMessage(missingEntities)); err != nil {
		t.Fatal("save actual ordinary-scan index gap")
	}
	driver["OrdinaryCachedScanRetainedIndexGap"] = true
	baseline := refreshBrowserSnapshot(t, f)
	if err := refreshBrowserWriteJSON(filepath.Join(output, "preservation-before.json"), json.RawMessage(baseline)); err != nil {
		t.Fatal("save private baseline")
	}
	var browserResult refreshBrowserResult
	browserAuthBound := false
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		prober.release()
		if _, err := f.pool.Exec(ctx, "UPDATE task_triggers SET retired_at=COALESCE(retired_at,clock_timestamp()),updated_at=clock_timestamp() WHERE task_id=$1 AND retired_at IS NULL", refresh.ID); err != nil {
			t.Error("retire owned browser triggers")
		}
		if err := f.app.taskManager.Close(ctx); err != nil {
			t.Error("close owned browser task manager")
		}
		if browserAuthBound {
			request := httptest.NewRequest(http.MethodDelete, "/admin/v1/session", nil).WithContext(ctx)
			request.Header.Set("Origin", origin)
			request.Header.Set("Cookie", browserResult.BrowserCookie)
			request.Header.Set("X-CSRF-Token", browserResult.BrowserCSRF)
			logout := httptest.NewRecorder()
			f.handler.ServeHTTP(logout, request)
			proof := httptest.NewRequest(http.MethodGet, "/admin/v1/session", nil).WithContext(ctx)
			proof.Header.Set("Cookie", browserResult.BrowserCookie)
			denied := httptest.NewRecorder()
			f.handler.ServeHTTP(denied, proof)
			driver["BrowserLogoutStatus"], driver["SameCookieStatus"] = logout.Code, denied.Code
			if logout.Code != http.StatusNoContent || denied.Code != http.StatusUnauthorized {
				t.Error("owned browser logout and rejection are incomplete")
			}
		}
		// The failure fallback is confined to this freshly created schema. It is
		// recorded separately from the real token logout above and from schema drop.
		result, err := f.pool.Exec(ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE revoked_at IS NULL")
		if err != nil {
			t.Error("revoke residual owned schema sessions")
		} else {
			driver["EmergencySessionRevocations"] = result.RowsAffected()
			if browserResult.Complete && result.RowsAffected() != 0 {
				t.Error("a successful browser scenario needed unexpected emergency session revocation")
			}
		}
		var active int
		sessionErr := f.pool.QueryRow(ctx, "SELECT count(*) FROM sessions WHERE revoked_at IS NULL").Scan(&active)
		if sessionErr != nil || active != 0 {
			t.Error("owned browser sessions remain or could not be observed")
		}
		driver["OwnedSessionsRevoked"] = sessionErr == nil && active == 0
		var sessionState string
		if err := f.pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY id),'[]'::jsonb)::text FROM sessions s").Scan(&sessionState); err != nil {
			t.Error("capture private session cleanup state")
		} else if err := refreshBrowserWriteJSON(filepath.Join(output, "sessions-after-cleanup.json"), json.RawMessage(sessionState)); err != nil {
			t.Error("preserve private session cleanup state")
		}
	})
	manifestPath := filepath.Join(output, "fixture.json")
	if err := refreshBrowserWriteJSON(manifestPath, fixture); err != nil {
		t.Fatal("save private browser fixture manifest")
	}
	for _, name := range []string{"home", "tmp", "cache"} {
		if err := os.Mkdir(filepath.Join(output, name), 0o700); err != nil {
			t.Fatal("create private browser runtime directory")
		}
	}
	// Browser code receives only its own administrator credential and runtime
	// paths. Database URLs, backup passphrases, and the Go worker environment are
	// intentionally absent from the child environment.
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "CI=1",
		"HOME=" + filepath.Join(output, "home"), "TMPDIR=" + filepath.Join(output, "tmp"), "XDG_CACHE_HOME=" + filepath.Join(output, "cache"),
		"PLAYWRIGHT_BROWSERS_PATH=" + browserCache, "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1",
		"GOBY_SMOKE_NAME=" + username, "GOBY_SMOKE_PASSWORD=" + password, "GOBY_SMOKE_BASE_URL=" + origin, "GOBY_REFRESH_TASK_RUN_ID=" + runID,
		"GOBY_SMOKE_REFRESH_TASK_FIXTURE_MANIFEST=" + manifestPath, "GOBY_SMOKE_REFRESH_TASK_DISPOSABLE_DATABASE=1", "GOBY_SMOKE_REFRESH_TASK_DEDICATED_ADMIN=1"}
	ctx, cancel := context.WithTimeout(f.ctx, 210*time.Second)
	grepPattern := "(?:^| )" + regexp.QuoteMeta(refreshBrowserTitle) + "$"
	command, commandErr := refreshBrowserCommand(ctx, node, []string{cli, "test", "e2e/scheduled-tasks.spec.ts", "--project=chromium", "--grep", grepPattern, "--workers=1", "--retries=0", "--output", filepath.Join(output, "playwright")}, environment, work, output)
	if command != nil {
		command["RequestedTitle"] = refreshBrowserTitle
		command["RequestedGrepPattern"] = grepPattern
	}
	cancel()
	driver["BrowserCommand"] = command
	resultInfo, metadataErr := os.Lstat(fixture.ResultPath)
	if metadataErr == nil {
		value, ok := resultInfo.Sys().(*syscall.Stat_t)
		if !ok || !resultInfo.Mode().IsRegular() || resultInfo.Size() > 1<<20 || resultInfo.Mode().Perm() != 0o600 || value.Uid != 0 || value.Nlink != 1 {
			metadataErr = errors.New("private browser result metadata")
		}
	}
	var raw []byte
	readErr := metadataErr
	if readErr == nil {
		raw, readErr = os.ReadFile(fixture.ResultPath)
	}
	if readErr == nil && len(raw) <= 1<<20 {
		readErr = json.Unmarshal(raw, &browserResult)
	}
	if readErr == nil && strings.HasPrefix(browserResult.BrowserCookie, "goby_session=") && browserResult.BrowserSessionID != "" {
		tokenHash := sha256.Sum256([]byte(strings.TrimPrefix(browserResult.BrowserCookie, "goby_session=")))
		var matched int
		if f.pool.QueryRow(f.ctx, "SELECT count(*) FROM sessions WHERE id=$1 AND user_id=$2 AND kind='admin' AND token_hash=$3 AND revoked_at IS NULL", browserResult.BrowserSessionID, bootstrap.User.ID, tokenHash[:]).Scan(&matched) == nil && matched == 1 {
			browserAuthBound = true
		}
	}
	if commandErr != nil || readErr != nil || len(raw) > 1<<20 {
		t.Fatal("administrator browser did not produce a completed private result; inspect retained artifacts")
	}
	checks := []string{"ManualRefreshAllLibrariesForceProbe", "ActiveRefreshCancelledThroughRunDialog", "RefreshIntervalFiredAndRemoved", "CatalogAndMetadataPreserved", "OrdinaryScanTaskUnchanged", "NativeTaskWritesOnly", "NoPageErrors", "NoRemainingTriggersOrActiveRuns"}
	if browserResult.Marker != "goby-refresh-task-browser-result-v1" || browserResult.RunID != runID || !browserResult.Complete || browserResult.TaskID != refresh.ID || browserResult.ScanTaskID != normal.ID || !reflect.DeepEqual(browserResult.Libraries, fixture.Libraries) || !browserResult.Observations.ActiveStopDialogObserved || len(browserResult.Checks) != len(checks) || !browserAuthBound || browserResult.BrowserCSRF == "" {
		t.Fatal("browser result does not bind this fixture and complete scenario")
	}
	for _, check := range checks {
		if !browserResult.Checks[check] {
			t.Fatal("browser scenario check failed; inspect the private result")
		}
	}
	if len(browserResult.IntervalRunIDs) != 1 || browserResult.ManualRunID == browserResult.CancelledRunID {
		t.Fatal("browser execution inventory differs")
	}
	runIDs := append([]string{browserResult.ManualRunID, browserResult.CancelledRunID}, browserResult.IntervalRunIDs...)
	for index, id := range runIDs {
		run, err := f.app.taskStore.GetRun(f.ctx, id)
		want := tasks.RunCompleted
		if index == 1 {
			want = tasks.RunCancelled
		}
		if err != nil || run.TaskKey != tasks.LibraryRefreshMediaKey || run.TaskID != refresh.ID || run.State != want || run.TotalChildren != 2 || run.TerminalChildren != 2 {
			t.Fatal("browser run lost its real owned child outcomes")
		}
		if index < 2 {
			if run.Source != "manual" || run.RequestID == nil || *run.RequestID == "" {
				t.Fatal("manual browser request identity missing")
			}
		} else if run.Source != "schedule" || run.TriggerID == nil {
			t.Fatal("browser interval did not execute a scheduled run")
		}
		if index < 2 {
			var mappings int
			if f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_run_requests WHERE run_id=$1 AND task_id=$2 AND request_id=$3", id, refresh.ID, *run.RequestID).Scan(&mappings) != nil || mappings != 1 {
				t.Fatal("manual RequestId mapping is not unique and bound to its run")
			}
		} else {
			var retired int
			if run.TriggerRevision == nil || run.ScheduledFor == nil || f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_triggers WHERE id=$1 AND task_id=$2 AND retired_at IS NOT NULL", *run.TriggerID, refresh.ID).Scan(&retired) != nil || retired != 1 {
				t.Fatal("scheduled run lost its retired trigger provenance")
			}
		}
		var linked, forced int
		jobStatus := "Completed"
		if index == 1 {
			jobStatus = "Cancelled"
		}
		if f.pool.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER(WHERE j.force_probe AND j.task_child_id=c.id AND j.library_id=c.library_id AND j.status=$2 AND c.state=$3) FROM task_run_children c JOIN scan_jobs j ON j.id=c.scan_job_id WHERE c.run_id=$1`, id, jobStatus, string(want)).Scan(&linked, &forced) != nil || linked != 2 || forced != 2 {
			t.Fatal("browser refresh child did not bind an actual terminal ForceProbe scan")
		}
	}
	var totalRuns, pending, activeTriggers, normalRuns, requestMappings int
	if f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_runs WHERE state IN('pending','running','stopping')),(SELECT count(*) FROM task_triggers WHERE retired_at IS NULL),(SELECT count(*) FROM task_runs WHERE task_id=$1),(SELECT count(*) FROM task_run_requests WHERE task_id=$2)`, normal.ID, refresh.ID).Scan(&totalRuns, &pending, &activeTriggers, &normalRuns, &requestMappings) != nil || totalRuns != len(runIDs) || pending != 0 || activeTriggers != 0 || normalRuns != 0 || requestMappings != 2 {
		t.Fatal("browser left extra execution, request mapping, or schedule responsibility")
	}
	currentNormal, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryScanKey)
	if err != nil || !reflect.DeepEqual(currentNormal, normal) {
		t.Fatal("native refresh changed the ordinary scan definition")
	}
	currentRefresh, err := f.app.taskStore.GetByKey(f.ctx, tasks.LibraryRefreshMediaKey)
	if err != nil || len(currentRefresh.Triggers) != 0 || currentRefresh.ScheduleTimezone != "UTC" || browserResult.FinalTimezone != "UTC" || len(browserResult.FinalTriggers) != 0 || browserResult.FinalRevision != strconv.FormatInt(currentRefresh.Revision, 10) {
		t.Fatal("browser final schedule revision differs from stored retirement")
	}
	var taskState string
	if f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'definitions',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM task_definitions d),
		'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM task_runs r),
		'children',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM task_run_children c),
		'triggers',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM task_triggers t),
		'requests',(SELECT jsonb_agg(to_jsonb(q) ORDER BY task_id,request_id) FROM task_run_requests q),
		'scanJobs',(SELECT jsonb_agg(to_jsonb(j) ORDER BY id) FROM scan_jobs j))::text`).Scan(&taskState) != nil {
		t.Fatal("capture private task and scan outcomes")
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "task-state-after-workflow.json"), json.RawMessage(taskState)); err != nil {
		t.Fatal("preserve private task and scan outcomes")
	}
	counts, realCalls, entered, cancelled, gateErr := prober.counts()
	if gateErr != nil || entered != 2 || cancelled != 2 || realCalls != 2+2*(len(runIDs)-1) {
		t.Fatal("browser scenario did not consume two real cold/refresh probes and both cancellation gates")
	}
	for _, path := range paths {
		if counts[path] != len(runIDs)+1 {
			t.Fatal("a media path was omitted or probed by an undeclared execution")
		}
	}
	after := refreshBrowserSnapshot(t, f)
	if err := refreshBrowserWriteJSON(filepath.Join(output, "preservation-after.json"), json.RawMessage(after)); err != nil {
		t.Fatal("save final preservation snapshot")
	}
	if after != baseline {
		t.Fatal("refresh changed catalog identity, metadata, or complete UserData rows/xmin")
	}
	finalEntities := refreshBrowserEntitySnapshot(t, f)
	if finalEntities != expectedEntities {
		t.Fatal("browser workflow did not restore the exact missing genre relationship")
	}
	if err := refreshBrowserWriteJSON(filepath.Join(output, "genre-index-after-workflow.json"), json.RawMessage(finalEntities)); err != nil {
		t.Fatal("save final restored genre relationships")
	}
	for _, path := range allFiles {
		facts, err := refreshBrowserMediaFacts(path)
		if err != nil || !reflect.DeepEqual(facts, mediaBefore[path]) {
			t.Fatal("browser refresh changed an original media or NFO file")
		}
	}
	driver["Complete"] = true
	driver["PreservationScope"] = "after cold seed to final completed/cancelled/scheduled state"
	driver["MissingGenreRelationshipRestoredAtWorkflowEnd"] = true
	driver["NonemptyMetadataOverridesAndLockPreserved"] = true
	driver["RealProbeCalls"] = realCalls
	driver["CancelledProbeGates"] = cancelled
	driver["TaskRuns"] = len(runIDs)
	driver["RequestMappings"] = requestMappings
	driver["RequestIdReplayPerformed"] = false
	t.Logf("refresh_media_browser_scenario_verified=true libraries=2 files=2 real_probes=%d cancellation_gates=2 task_runs=%d userdata_xmin_preserved=true", realCalls, len(runIDs))
}
