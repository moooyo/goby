//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
)

type phase3RecoveryProcessConfig struct {
	Server config.Config
	Schema string
	Ready  string
}

// This helper is a real separate server process. The parent supplies its own
// schema and deployment directories, and sends SIGKILL for the crash case.
// It deliberately does not model cmd/goby's supervisor or generation lifecycle.
func TestHTTPPhase3RecoveryProcessHelper(t *testing.T) {
	path := os.Getenv("GOBY_PHASE3_RECOVERY_HELPER_CONFIG")
	if path == "" {
		t.Skip("private child-process configuration is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings phase3RecoveryProcessConfig
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(settings.Server.DatabaseURL)
	if err != nil {
		t.Fatal("parse the private recovery database configuration")
	}
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = make(map[string]string)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = settings.Schema
	poolConfig.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("open the private recovery database")
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate recovery schema: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	app, err := New(ctx, settings.Server, pool, identity.New(pool), logger, "phase3-process-recovery")
	if err != nil {
		t.Fatalf("start recovery server: %v", err)
	}
	startup, err := app.StartupHTTPBinding()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", startup.Address())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := app.PublishHTTPBinding(listener.Addr(), startup); err != nil {
		t.Fatal(err)
	}
	origin := "http://" + listener.Addr().String()
	app.cfg.PublicURL = origin
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx }, ConnContext: app.HTTPConnectionContext}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ready, _ := json.Marshal(map[string]any{"URL": origin, "PID": os.Getpid()})
	if err := os.WriteFile(settings.Ready, ready, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case err := <-done:
		t.Fatalf("recovery listener exited unexpectedly: %v", err)
	}
	cleanup, finish := context.WithTimeout(context.Background(), 25*time.Second)
	defer finish()
	app.WithdrawHTTPBinding()
	if err := server.Shutdown(cleanup); err != nil {
		_ = server.Close()
		t.Errorf("close recovery listener: %v", err)
	}
	if err := app.Close(cleanup); err != nil {
		t.Errorf("close recovery server: %v", err)
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		t.Errorf("join recovery listener: %v", err)
	}
}

type phase3RecoveryChild struct {
	command *exec.Cmd
	done    chan error
	url     string
	joined  bool
}

func phase3RecoveryProcessIdentity(pid int) (string, bool) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", false
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return "", false
	}
	return fields[19], true
}

// Inspect all Go threads: an exec child need not belong to the leader thread.
// Start-time identities prevent cleanup from signaling a recycled numeric PID.
func phase3RecoveryDescendants(pid int) map[int]string {
	result := make(map[int]string)
	var visit func(int)
	visit = func(parent int) {
		paths, _ := filepath.Glob(fmt.Sprintf("/proc/%d/task/*/children", parent))
		for _, path := range paths {
			data, _ := os.ReadFile(path)
			for _, field := range strings.Fields(string(data)) {
				child, err := strconv.Atoi(field)
				if err != nil || child == pid || result[child] != "" {
					continue
				}
				if identity, alive := phase3RecoveryProcessIdentity(child); alive {
					result[child] = identity
					visit(child)
				}
			}
		}
	}
	visit(pid)
	return result
}

func phase3RecoveryCheckOrphans(t *testing.T, children map[int]string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(children) != 0 && time.Now().Before(deadline) {
		for pid, expected := range children {
			if current, alive := phase3RecoveryProcessIdentity(pid); !alive || current != expected {
				delete(children, pid)
			}
		}
		if len(children) != 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	cleaned := 0
	for pid, expected := range children {
		if current, alive := phase3RecoveryProcessIdentity(pid); alive && current == expected {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				t.Errorf("clean this recovery process's orphan %d: %v", pid, err)
			}
			cleaned++
		}
	}
	t.Logf("recovery orphan_cleanup_count=%d", cleaned)
	if cleaned != 0 {
		t.Errorf("the crashed application left %d live media children; fixture cleanup is not product recovery", cleaned)
	}
}

// The caller provisions one private PostgreSQL cluster and a control executable
// accepting only stop/start. Stop must be immediate; start must wait for ready.
// This test never controls the shared GOBY_TEST_DATABASE_URL server or deletes
// the PostgreSQL cluster. Its data/media/cache survive every child restart.
func TestHTTPPhase3ProcessAndPostgresRestartRecovery(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_PROCESS_RECOVERY") != "1" {
		t.Skip("set GOBY_PHASE3_PROCESS_RECOVERY=1 for real process/database recovery")
	}
	databaseURL, pgControl := os.Getenv("GOBY_PHASE3_RECOVERY_DATABASE_URL"), os.Getenv("GOBY_PHASE3_RECOVERY_PG_CONTROL")
	if databaseURL == "" || pgControl == "" || os.Getenv("GOBY_FFMPEG") == "" || os.Getenv("GOBY_FFPROBE") == "" {
		t.Fatal("private recovery database/control and actual media tools are required")
	}
	t.Setenv("GOBY_TEST_DATABASE_URL", databaseURL)
	f := newServerFixtureWithTimeout(t, 30*time.Minute)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	var schema string
	if err := f.pool.QueryRow(f.ctx, "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-phase3-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("recovery failure artifacts retained in private directory %s", base)
			return
		}
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove recovery media/configuration: %v", err)
		}
	})
	mediaRoot, evidenceRoot := filepath.Join(base, "media"), filepath.Join(base, "evidence")
	for _, path := range []string{mediaRoot, evidenceRoot} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	seed := filepath.Join(base, "seed.mp4")
	hlsHTTPMediaCommand(t, os.Getenv("GOBY_FFMPEG"), "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=45",
		"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=45",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast",
		"-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "45", seed)
	seedBytes, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	const mediaCount = 20
	for index := 0; index < mediaCount; index++ {
		box := make([]byte, 16)
		binary.BigEndian.PutUint32(box[:4], 16)
		copy(box[4:8], "free")
		binary.BigEndian.PutUint64(box[8:], uint64(index))
		if err := os.WriteFile(filepath.Join(mediaRoot, fmt.Sprintf("Movie%02d.mp4", index)), append(append([]byte(nil), seedBytes...), box...), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.cfg.DatabaseURL, f.cfg.MediaRoots = databaseURL, []string{mediaRoot}
	f.cfg.ListenAddress, f.cfg.CookieSecure = "127.0.0.1:0", false
	f.cfg.FFmpegPath, f.cfg.FFprobePath = os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	f.cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: evidenceRoot, MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	f.cfg.MediaAnalysis = config.MediaAnalysisConfig{Enabled: true, CacheDirectory: filepath.Join(base, "analysis"),
		CacheMaxBytes: 512 << 20, CacheMaxEntries: 64, MaxEntryBytes: 128 << 20, MaxFileBytes: 32 << 20}
	readyPath, configPath := filepath.Join(base, "ready.json"), filepath.Join(base, "child.json")
	encoded, err := json.Marshal(phase3RecoveryProcessConfig{Server: f.cfg, Schema: schema, Ready: readyPath})
	if err != nil || os.WriteFile(configPath, encoded, 0o600) != nil {
		t.Fatal("write private recovery child configuration")
	}
	pgStopped := false
	controlPostgres := func(action string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, pgControl, action).CombinedOutput()
		if err != nil {
			return fmt.Errorf("private PostgreSQL %s: %w; output=%s", action, err, output)
		}
		pgStopped = action == "stop"
		t.Logf("private_postgres_control=%s completed", action)
		return nil
	}
	t.Cleanup(func() {
		if pgStopped {
			if err := controlPostgres("start"); err != nil {
				t.Errorf("restore private PostgreSQL for schema cleanup: %v", err)
			}
		}
	})
	var current *phase3RecoveryChild
	stopChild := func(signal syscall.Signal, tolerateDatabaseLoss bool) {
		t.Helper()
		if current == nil || current.joined {
			return
		}
		children := phase3RecoveryDescendants(current.command.Process.Pid)
		if err := current.command.Process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Fatal(err)
		}
		var waitErr error
		select {
		case waitErr = <-current.done:
		case <-time.After(35 * time.Second):
			_ = current.command.Process.Kill()
			waitErr = <-current.done
			t.Error("recovery child required forced cleanup after graceful shutdown timeout")
		}
		current.joined = true
		if signal == syscall.SIGKILL {
			status, ok := current.command.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("recovery did not exercise an actual SIGKILL: %v", waitErr)
			}
		} else if waitErr != nil && !tolerateDatabaseLoss {
			t.Errorf("recovery child did not stop normally: %v", waitErr)
		}
		t.Logf("recovery app_pid=%d signal=%s exit=%v database_loss_expected=%t", current.command.Process.Pid, signal, waitErr, tolerateDatabaseLoss)
		phase3RecoveryCheckOrphans(t, children)
	}
	t.Cleanup(func() { stopChild(syscall.SIGTERM, pgStopped) })
	generation := 0
	startChild := func() {
		t.Helper()
		generation++
		_ = os.Remove(readyPath)
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(base, fmt.Sprintf("generation-%d.log", generation))
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(executable, "-test.run=^TestHTTPPhase3RecoveryProcessHelper$", "-test.timeout=25m", "-test.v")
		command.Env = append(os.Environ(), "GOBY_PHASE3_RECOVERY_HELPER_CONFIG="+configPath)
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		command.Stdout, command.Stderr = logFile, logFile
		if err := command.Start(); err != nil {
			_ = logFile.Close()
			t.Fatal(err)
		}
		_ = logFile.Close()
		current = &phase3RecoveryChild{command: command, done: make(chan error, 1)}
		child := current
		go func() { child.done <- command.Wait() }()
		timer, ticker := time.NewTimer(90*time.Second), time.NewTicker(50*time.Millisecond)
		defer timer.Stop()
		defer ticker.Stop()
		for {
			var ready struct {
				URL string
				PID int
			}
			data, err := os.ReadFile(readyPath)
			if err == nil && json.Unmarshal(data, &ready) == nil && ready.PID == command.Process.Pid && ready.URL != "" {
				current.url = ready.URL
				t.Logf("recovery app_generation=%d pid=%d ready", generation, ready.PID)
				return
			}
			select {
			case err := <-current.done:
				current.joined = true
				t.Fatalf("recovery child startup failed: %v; inspect %s", err, logPath)
			case <-timer.C:
				t.Fatalf("recovery child startup exceeded its hang guard; inspect %s", logPath)
			case <-ticker.C:
			}
		}
	}
	startChild()
	client := &http.Client{Timeout: 30 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
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
		if req.Header == nil {
			req.Header = make(http.Header)
		}
		req.Header.Set("Origin", current.url)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatalf("recovery HTTP request %s failed (%T)", path, err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil || len(data) >= 16<<20 {
			t.Fatal("read bounded recovery HTTP response")
		}
		result := httptest.NewRecorder()
		for name, values := range response.Header {
			result.Header()[name] = append([]string(nil), values...)
		}
		result.WriteHeader(response.StatusCode)
		_, _ = result.Write(data)
		return result
	}
	bootstrap := request(http.MethodPost, "/admin/v1/bootstrap", map[string]any{
		"SetupToken": f.cfg.SetupToken, "Name": "Administrator", "Password": "administrator-password",
	}, nil)
	expectStatus(t, bootstrap, http.StatusCreated)
	adminID := stringValue(t, objectValue(t, jsonObject(t, bootstrap), "User"), "Id")
	login := request(http.MethodPost, "/admin/v1/session", map[string]any{"Name": "Administrator", "Password": "administrator-password"}, nil)
	expectStatus(t, login, http.StatusOK)
	csrf := stringValue(t, jsonObject(t, login), "CSRFToken")
	var cookie *http.Cookie
	for _, candidate := range login.Result().Cookies() {
		if candidate.Name == sessionCookie {
			cookie = candidate
		}
	}
	if cookie == nil {
		t.Fatal("recovery login omitted its cookie")
	}
	adminHeaders := http.Header{"Cookie": {cookie.String()}, "X-CSRF-Token": {csrf}}
	emby := request(http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{"Username": "Administrator", "Pw": "administrator-password"},
		http.Header{"Authorization": {`Emby Client="Recovery", DeviceId="phase3-recovery", Device="Linux", Version="1.0"`}})
	expectStatus(t, emby, http.StatusOK)
	publicHeaders := http.Header{"X-Emby-Token": {stringValue(t, jsonObject(t, emby), "AccessToken")}}
	created := request(http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Process recovery", "CollectionType": "movies", "Paths": []string{mediaRoot}, "Scan": true,
	}, adminHeaders)
	expectStatus(t, created, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")
	readScan := func(jobID string) map[string]any {
		t.Helper()
		jobs, _ := responseItems(t, request(http.MethodGet, "/admin/v1/jobs", nil, adminHeaders))
		for _, job := range jobs {
			if job["Id"] == jobID {
				return job
			}
		}
		t.Fatalf("recovery scan %s disappeared", jobID)
		return nil
	}
	waitFor := func(label string, check func() bool) {
		t.Helper()
		timer, ticker := time.NewTimer(2*time.Minute), time.NewTicker(25*time.Millisecond)
		defer timer.Stop()
		defer ticker.Stop()
		for !check() {
			select {
			case <-ticker.C:
			case <-timer.C:
				t.Fatalf("recovery %s exceeded its hang guard", label)
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
		}
	}
	waitCompletedScan := func(jobID string) {
		t.Helper()
		waitFor("scan completion", func() bool {
			job := readScan(jobID)
			if job["Status"] == "pending" || job["Status"] == "running" {
				return false
			}
			if job["Status"] != "completed" || job["Error"] != "" || job["Scanned"] != float64(mediaCount) {
				t.Fatalf("recovery scan failed: %#v", job)
			}
			return true
		})
	}
	waitCompletedScan(stringValue(t, objectValue(t, jsonObject(t, created), "Job"), "Id"))
	itemsURL := "/emby/Items?ParentId=" + libraryID + "&Recursive=true&IncludeItemTypes=Movie&SortBy=SortName&Limit=100"
	catalogSnapshot := func() map[string]string {
		t.Helper()
		items, total := responseItems(t, request(http.MethodGet, itemsURL, nil, publicHeaders))
		if total != mediaCount || len(items) != mediaCount {
			t.Fatalf("recovery catalog population changed: %d/%d", len(items), total)
		}
		result := make(map[string]string, mediaCount)
		for _, item := range items {
			result[stringValue(t, item, "Name")] = stringValue(t, item, "Id")
		}
		return result
	}
	baseline := catalogSnapshot()
	var itemIDs []string
	for _, id := range baseline {
		itemIDs = append(itemIDs, id)
	}
	stableID := baseline["Movie00"]
	if stableID == "" {
		t.Fatal("stable recovery movie name was not cataloged")
	}
	userDataURL := "/emby/Users/" + adminID + "/Items/" + stableID + "/UserData"
	expectStatus(t, request(http.MethodPost, userDataURL, map[string]any{
		"PlaybackPositionTicks": int64(50_000_000), "PlayCount": 7, "Played": false, "IsFavorite": true,
		"LastPlayedDate": "2026-09-01T03:04:05Z",
	}, publicHeaders), http.StatusOK)
	userData := jsonObject(t, request(http.MethodGet, userDataURL, nil, publicHeaders))
	settings := jsonObject(t, request(http.MethodGet, "/admin/v1/settings", nil, adminHeaders))
	savedResponse := request(http.MethodPut, "/admin/v1/settings", adminSettingsHTTPUpdate(stringValue(t, settings, "Revision"), "Recovered native settings", nil), adminHeaders)
	expectStatus(t, savedResponse, http.StatusOK)
	savedSettings := jsonObject(t, savedResponse)
	readTask := func(runID string) map[string]any {
		t.Helper()
		response := request(http.MethodGet, "/admin/v1/task-runs/"+runID+"?Limit=100", nil, adminHeaders)
		expectStatus(t, response, http.StatusOK)
		return jsonObject(t, response)
	}
	startScan := func() string {
		t.Helper()
		response := request(http.MethodPost, "/admin/v1/libraries/"+libraryID+"/scan", map[string]any{"ForceProbe": true}, adminHeaders)
		expectStatus(t, response, http.StatusAccepted)
		return stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	}
	startPreview := func(name string, ids []string) string {
		t.Helper()
		response := request(http.MethodPost, "/admin/v1/media-analysis/runs", map[string]any{
			"Kind": "previews", "RequestId": name, "LibraryIds": []string{}, "ItemIds": ids, "Force": true,
		}, adminHeaders)
		expectStatus(t, response, http.StatusAccepted)
		return stringValue(t, jsonObject(t, response), "RunId")
	}
	startActiveWork := func(label string) (string, string) {
		t.Helper()
		runID, scanID := startPreview(label+"-preview", itemIDs), startScan()
		waitFor("real active scan/task/media child", func() bool {
			job, task := readScan(scanID), readTask(runID)
			if job["Status"] != "running" || job["Scanned"].(float64) >= mediaCount-3 || objectValue(t, task, "Run")["State"] != "running" {
				return false
			}
			for _, raw := range objectValue(t, task, "Children")["Items"].([]any) {
				if raw.(map[string]any)["State"] == "running" && len(phase3RecoveryDescendants(current.command.Process.Pid)) != 0 {
					t.Logf("recovery fault=%s active_scan=%s active_task=%s scanned=%v app_pid=%d", label, scanID, runID, job["Scanned"], current.command.Process.Pid)
					return true
				}
			}
			return false
		})
		return scanID, runID
	}
	assertRecovery := func(label, scanID, runID string) {
		t.Helper()
		if got := catalogSnapshot(); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("%s recovery changed confirmed catalog identities", label)
		}
		if got := jsonObject(t, request(http.MethodGet, userDataURL, nil, publicHeaders)); !reflect.DeepEqual(got, userData) {
			t.Fatalf("%s recovery changed confirmed UserData", label)
		}
		gotSettings := jsonObject(t, request(http.MethodGet, "/admin/v1/settings", nil, adminHeaders))
		if gotSettings["Revision"] != savedSettings["Revision"] || !reflect.DeepEqual(gotSettings["Overrides"], savedSettings["Overrides"]) ||
			objectValue(t, gotSettings, "Effective")["ServerName"] != "Recovered native settings" {
			t.Fatalf("%s recovery changed confirmed native settings", label)
		}
		job, task := readScan(scanID), readTask(runID)
		if job["Status"] != "interrupted" || objectValue(t, task, "Run")["State"] != "interrupted" {
			t.Fatalf("%s did not recover abandoned work as interrupted: scan=%#v task=%#v", label, job, task)
		}
		interrupted := 0
		for _, raw := range objectValue(t, task, "Children")["Items"].([]any) {
			child := raw.(map[string]any)
			switch child["State"] {
			case "interrupted":
				interrupted++
			case "completed":
			default:
				t.Fatalf("%s retained an unfinished or unexpected task child: %#v", label, child)
			}
		}
		if interrupted == 0 {
			t.Fatal("recovery fixture did not interrupt any real task child")
		}
		waitCompletedScan(startScan())
		if got := catalogSnapshot(); !reflect.DeepEqual(got, baseline) {
			t.Fatalf("%s rescan changed confirmed identities", label)
		}
		mediaResponse := request(http.MethodGet, "/emby/Videos/"+stableID+"/stream.mp4?Static=true", nil, publicHeaders)
		expectStatus(t, mediaResponse, http.StatusOK)
		original, err := os.ReadFile(filepath.Join(mediaRoot, "Movie00.mp4"))
		if err != nil || !bytes.Equal(original, mediaResponse.Body.Bytes()) {
			t.Fatalf("%s recovered direct media bytes changed: %v", label, err)
		}
		fresh := startPreview(label+"-recovered-preview", []string{stableID})
		waitFor("recovered preview execution", func() bool {
			run := objectValue(t, readTask(fresh), "Run")
			if run["State"] == "pending" || run["State"] == "running" {
				return false
			}
			if run["State"] != "completed" {
				t.Fatalf("%s recovered preview failed: %#v", label, run)
			}
			return true
		})
		preview := request(http.MethodGet, "/emby/Videos/"+stableID+"/index.bif?Width=240", nil, publicHeaders)
		expectStatus(t, preview, http.StatusOK)
		if !bytes.HasPrefix(preview.Body.Bytes(), []byte("\x89BIF\r\n\x1a\n")) {
			t.Fatal("recovered preview did not publish an actual HTTP BIF")
		}
		t.Logf("recovery fault=%s durable_catalog=%d userdata=true settings=true interrupted_scan=true interrupted_children=%d fresh_scan=true direct_media=true fresh_preview=true", label, len(baseline), interrupted)
	}
	crashedScan, crashedTask := startActiveWork("app-sigkill")
	stopChild(syscall.SIGKILL, false)
	startChild()
	assertRecovery("app-sigkill", crashedScan, crashedTask)
	var postgresBefore time.Time
	if err := f.pool.QueryRow(f.ctx, "SELECT pg_postmaster_start_time()").Scan(&postgresBefore); err != nil {
		t.Fatal(err)
	}
	databaseScan, databaseTask := startActiveWork("postgres-immediate-stop")
	if err := controlPostgres("stop"); err != nil {
		t.Fatal(err)
	}
	outage, cancelOutage := context.WithTimeout(f.ctx, 3*time.Second)
	req, err := http.NewRequestWithContext(outage, http.MethodGet, current.url+itemsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header = publicHeaders.Clone()
	unavailable, outageErr := client.Do(req)
	if unavailable != nil {
		_ = unavailable.Body.Close()
		if unavailable.StatusCode == http.StatusOK {
			t.Error("catalog HTTP claimed success while its private PostgreSQL was stopped")
		}
		t.Logf("recovery postgres_outage_http_status=%d", unavailable.StatusCode)
	} else {
		t.Logf("recovery postgres_outage_http_error=%T", outageErr)
	}
	cancelOutage()
	// Server.New is not cmd/goby's supervisor. An explicit application restart
	// reacquires catalog ownership after the real PostgreSQL lease loss.
	stopChild(syscall.SIGTERM, true)
	if err := controlPostgres("start"); err != nil {
		t.Fatal(err)
	}
	postRestart, err := pgxpool.New(f.ctx, databaseURL)
	if err != nil {
		t.Fatal("open restarted private PostgreSQL")
	}
	var postgresAfter time.Time
	err = postRestart.QueryRow(f.ctx, "SELECT pg_postmaster_start_time()").Scan(&postgresAfter)
	postRestart.Close()
	if err != nil || !postgresAfter.After(postgresBefore) {
		t.Fatalf("private PostgreSQL did not actually restart: %v", err)
	}
	t.Logf("recovery postgres_restart_confirmed=true previous_start=%s current_start=%s", postgresBefore.Format(time.RFC3339Nano), postgresAfter.Format(time.RFC3339Nano))
	startChild()
	assertRecovery("postgres-immediate-stop", databaseScan, databaseTask)
	stopChild(syscall.SIGTERM, false)
	t.Log("recovery_boundary=production_Server.New_child_SIGKILL_and_private_PostgreSQL_immediate_restart_with_explicit_app_restart; no_cmd_supervisor_or_host_reboot_claim")
}
