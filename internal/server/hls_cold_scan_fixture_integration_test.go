//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/tasks"
)

// The baseline keeps nil. A candidate-only addon opens the same 12+4 budget
// without registering hidden unbounded pool cleanup on testing.T.
var hlsColdPoolOptionsHook func(context.Context, *pgxpool.Pool) ([]Option, *pgxpool.Pool, error)

var hlsColdRetainedOwners sync.Map

// The real issuer's vault belongs to the same root as every other fixture
// resource. It has no independent testing.T cleanup that could remove files
// while a retained application owner is still retiring.
func hlsColdIssueKeys(t *testing.T, f *serverFixture, owner *hlsColdFixtureOwner, adminToken string) [2]applicationMediaKey {
	t.Helper()
	vaultDirectory := filepath.Join(owner.root, "vault")
	if err := os.Mkdir(vaultDirectory, 0700); err != nil {
		t.Fatal("create the coordinated application-key vault")
	}
	issuer := identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(vaultDirectory, "master.key")))
	actor, err := issuer.ResolveEmby(f.ctx, adminToken)
	if err != nil {
		t.Fatal("resolve the real application-key administrator")
	}
	var result [2]applicationMediaKey
	for index := range result {
		key, err := issuer.CreateApplicationKey(f.ctx, actor, "Cold HLS application "+strconv.Itoa(index), "127.0.0.1",
			identity.Client{DeviceID: f.app.serverID, Device: "Integration Server", Version: "integration-version"})
		if err != nil {
			t.Fatal("issue the real cold-profile application key")
		}
		principal, err := issuer.ResolveEmby(f.ctx, key.Token)
		if err != nil || !principal.IsApplicationKey() || principal.User.ID != "" || principal.SessionID != key.CredentialID || principal.ClientSessionID == "" {
			t.Fatal("cold-profile key did not resolve as an independent real credential")
		}
		result[index] = applicationMediaKey{key: key, principal: principal, headers: http.Header{"X-Emby-Token": {key.Token}}}
	}
	return result
}

type hlsColdFixtureOwner struct {
	root, parent, schema string
	cancel               context.CancelFunc
	admin, observer      *pgxpool.Pool
	data, control        *pgxpool.Pool
	apps                 []*Server
	server               *httptest.Server
	serverDone           chan struct{}
	transports           []*http.Transport
	generation           *hlsColdGeneration
	wave                 *hlsColdWave
	runID                string
	actor                tasks.Actor
	app                  *Server
}

type hlsColdDiagnostics struct {
	buffer bytes.Buffer
	limit  int
}

func (diagnostics *hlsColdDiagnostics) Write(encoded []byte) (int, error) {
	length := len(encoded)
	remaining := diagnostics.limit - diagnostics.buffer.Len()
	if len(encoded) > remaining {
		encoded = encoded[:remaining]
	}
	_, _ = diagnostics.buffer.Write(encoded)
	return length, nil
}

type hlsColdGeneration struct {
	command *exec.Cmd
	cancel  context.CancelFunc
	done    chan struct{}
	err     error // Published by closing done, including the actual pipe join.
}

func (generation *hlsColdGeneration) join(ctx context.Context) error {
	select {
	case <-generation.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Keep the group leader waitable until the final group signal is sent, so a
// reused numeric process-group ID cannot receive cleanup signals. The fixture
// retains this actual command owner when its bounded caller cannot join it.
func hlsColdGenerateMedia(t *testing.T, owner *hlsColdFixtureOwner, ctx context.Context, executable string, args ...string) {
	t.Helper()
	work, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(work, executable, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	diagnostics := &hlsColdDiagnostics{limit: 64 << 10}
	command.Stdout, command.Stderr = io.Discard, diagnostics
	var groupMu sync.Mutex
	retired := false
	command.Cancel = func() error {
		groupMu.Lock()
		defer groupMu.Unlock()
		if retired || command.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		} else {
			return err
		}
	}
	generation := &hlsColdGeneration{command: command, cancel: cancel, done: make(chan struct{})}
	owner.generation = generation
	if err := command.Start(); err != nil {
		generation.err = err
		close(generation.done)
		t.Fatal("start the owned real source-generation command")
	}
	go func() {
		defer close(generation.done)
		var information [16]uint64
		var retiredErr error
		for {
			_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(command.Process.Pid), uintptr(unsafe.Pointer(&information[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				retiredErr = errno
			}
			break
		}
		groupMu.Lock()
		if !errors.Is(retiredErr, syscall.ECHILD) {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
		retired = true
		groupMu.Unlock()
		generation.err = errors.Join(retiredErr, command.Wait())
	}()
	if err := generation.join(work); err != nil {
		t.Fatal("owned source-generation command exceeded its finite caller deadline")
	}
	if generation.err != nil || work.Err() != nil || diagnostics.buffer.Len() != 0 {
		t.Fatal("owned real source generation did not complete cleanly")
	}
}

func (owner *hlsColdFixtureOwner) client() *http.Client {
	transport := &http.Transport{MaxIdleConns: 64, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 64,
		IdleConnTimeout: 5 * time.Minute, DisableCompression: true}
	owner.transports = append(owner.transports, transport)
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

func hlsColdBounded(ctx context.Context, work func()) error {
	done := make(chan struct{})
	go func() { defer close(done); work() }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (owner *hlsColdFixtureOwner) retain(t *testing.T, stage string) {
	t.Helper()
	hlsColdRetainedOwners.Store(owner.root, owner)
	encoded, _ := json.Marshal(map[string]any{"stage": stage, "root": owner.root, "schema": owner.schema, "run_id": owner.runID, "retained_at": time.Now().UTC(), "destructive_cleanup_skipped": true})
	receipt, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_ = hlsColdBounded(receipt, func() {
		_ = os.WriteFile(filepath.Join(owner.root, "retained-owner.json"), append(encoded, '\n'), 0600)
	})
	cancel()
	t.Errorf("cold fixture cleanup did not join %s; retained owned root=%s schema=%s, pools and actual owners remain supervised", stage, owner.root, owner.schema)
}

func (owner *hlsColdFixtureOwner) cleanup(t *testing.T) {
	t.Helper()
	cleanup, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	// Stop new requests and cancel transports before waiting on any handler.
	if owner.wave != nil {
		owner.wave.requestStop(true)
	}
	for _, transport := range owner.transports {
		transport.CloseIdleConnections()
	}
	if owner.server != nil {
		owner.serverDone = make(chan struct{})
		go func() {
			defer close(owner.serverDone)
			owner.server.CloseClientConnections()
			owner.server.Close()
		}()
	}
	if owner.app != nil && owner.runID != "" {
		stop, stopCancel := context.WithTimeout(cleanup, 5*time.Second)
		_, _ = owner.app.taskManager.Stop(stop, owner.actor, owner.runID)
		stopCancel()
	}
	if owner.cancel != nil {
		owner.cancel()
	}
	if owner.generation != nil {
		owner.generation.cancel()
	}
	for i := len(owner.apps) - 1; i >= 0; i-- {
		if err := owner.apps[i].Close(cleanup); err != nil {
			owner.retain(t, "application worker retirement")
			return
		}
	}
	if owner.generation != nil {
		if err := owner.generation.join(cleanup); err != nil {
			owner.retain(t, "source-generation process and pipe retirement")
			return
		}
	}
	if owner.wave != nil {
		if err := owner.wave.join(cleanup); err != nil {
			owner.retain(t, "HTTP workload retirement")
			return
		}
	}
	if owner.serverDone != nil {
		select {
		case <-owner.serverDone:
		case <-cleanup.Done():
			owner.retain(t, "HTTP ingress retirement")
			return
		}
	}
	for _, pool := range []*pgxpool.Pool{owner.control, owner.data, owner.observer} {
		if pool != nil {
			if err := hlsColdBounded(cleanup, pool.Close); err != nil {
				owner.retain(t, "database checkout retirement")
				return
			}
		}
	}
	if owner.admin != nil && owner.schema != "" {
		if _, err := owner.admin.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{owner.schema}.Sanitize()+" CASCADE"); err != nil {
			owner.retain(t, "owned schema retirement")
			return
		}
	}
	if owner.admin != nil {
		if err := hlsColdBounded(cleanup, owner.admin.Close); err != nil {
			owner.retain(t, "administrative checkout retirement")
			return
		}
	}
	relative, err := filepath.Rel(owner.parent, owner.root)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		owner.retain(t, "owned path containment")
		return
	}
	deleted := make(chan error, 1)
	go func() { deleted <- os.RemoveAll(owner.root) }()
	select {
	case err := <-deleted:
		if err != nil {
			owner.retain(t, "owned file retirement")
		}
	case <-cleanup.Done():
		owner.retain(t, "owned file retirement")
	}
}

// This fixture registers one coordinated cleanup only. Unlike generic fixtures,
// it never automatically removes a root/schema or closes pools after an actual
// owner failed to join. Successful teardown is independently bounded; a failed
// join leaves an explicit receipt and strong references to all retained owners.
func hlsColdOwnedFixture(t *testing.T) (*hlsHTTPFixture, *hlsColdFixtureOwner) {
	t.Helper()
	parent := os.Getenv("GOBY_MIXED_FIXTURE_ROOT")
	root, err := os.MkdirTemp(parent, "completed-cold-case-")
	if err != nil {
		t.Fatal("create the explicitly owned cold fixture root")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	owner := &hlsColdFixtureOwner{root: root, parent: parent, cancel: cancel}
	t.Cleanup(func() { owner.cleanup(t) })
	url := os.Getenv("GOBY_TEST_DATABASE_URL")
	owner.admin, err = pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("create the cold fixture schema owner")
	}
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "goby_cold_hls_" + hex.EncodeToString(suffix[:])
	if _, err := owner.admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("create only the owned cold fixture schema")
	}
	owner.schema = schema
	configuration, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if configuration.ConnConfig.RuntimeParams == nil {
		configuration.ConnConfig.RuntimeParams = make(map[string]string)
	}
	configuration.ConnConfig.RuntimeParams["search_path"] = schema
	configuration.MaxConns = 8
	owner.observer, err = pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, owner.observer); err != nil {
		t.Fatal("migrate the owned cold fixture schema")
	}
	web, mediaRoot, cache := filepath.Join(root, "web"), filepath.Join(root, "media"), filepath.Join(root, "cache")
	for _, directory := range []string{web, mediaRoot, cache} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html><title>Cold profile</title>"), 0600); err != nil {
		t.Fatal(err)
	}
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	cfg := config.Config{DatabaseURL: url, PublicURL: "https://goby.example.test", ServerName: "Cold Profile",
		SetupToken: "integration-setup-token-with-32-bytes", CookieSecure: true, WebDirectory: web,
		MediaRoots: []string{mediaRoot}, FFmpegPath: ffmpeg, FFprobePath: ffprobe,
		Transcoding: config.TranscodingConfig{Enabled: true, CacheDirectory: cache, Threads: 1,
			MaxJobs: 2, MaxUserJobs: 2, MaxSessionJobs: 2, MaxQueueJobs: 8, MaxRetainedJobs: 32,
			MaxCacheBytes: 64 << 20, MaxJobBytes: 16 << 20, MinFreeBytes: 1 << 20,
			MaxBitrate: 2_000_000, MaxWidth: 1920, MaxHeight: 1080, MaxAudioChannels: 2}}
	f := &serverFixture{ctx: ctx, pool: owner.observer, cfg: cfg, users: identity.New(owner.observer), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app, err := New(ctx, cfg, owner.observer, f.users, f.log, "cold-profile-setup")
	if err != nil {
		t.Fatal("create the fully owned setup application")
	}
	owner.apps = append(owner.apps, app)
	f.app, f.handler = app, app.Handler()
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	for _, name := range []string{"Session Viewer", "Session Other"} {
		if _, err := f.users.CreateUser(ctx, name, "session-viewer-password", false); err != nil {
			t.Fatal("create the cold fixture playback account")
		}
	}
	accounts := clientSessionHTTPAccounts{
		admin:  loginClientSessionHTTP(t, f, "Administrator", "administrator-password", "session-admin"),
		viewer: loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", "session-viewer-one"),
		second: loginClientSessionHTTP(t, f, "Session Viewer", "session-viewer-password", "session-viewer-two"),
		other:  loginClientSessionHTTP(t, f, "Session Other", "session-viewer-password", "session-other"), cookie: cookie}
	source := filepath.Join(mediaRoot, "HLS.Color.Sequence.mp4")
	hlsColdGenerateMedia(t, owner, ctx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-filter_threads", "1",
		"-f", "lavfi", "-i", "color=c=red:size=160x90:rate=24:duration=12",
		"-f", "lavfi", "-i", "sine=frequency=660:sample_rate=48000:duration=12",
		"-vf", "drawbox=color=green:t=fill:enable='gte(t,3)*lt(t,6)',drawbox=color=blue:t=fill:enable='gte(t,6)*lt(t,9)',drawbox=color=yellow:t=fill:enable='gte(t,9)'",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-g", "72", "-keyint_min", "72", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "12", source)
	collection, err := app.library.CreateLibrary(ctx, "Completed Cold Movies", "movies", []string{mediaRoot})
	if err != nil {
		t.Fatal("register the owned cold source root")
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	listed, err := app.library.QueryItems(ctx, library.Query{UserID: accounts.admin.userID, ParentID: collection.ID, Recursive: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var item library.Item
	for _, candidate := range listed.Items {
		if candidate.Path == source && !candidate.IsFolder {
			item = candidate
		}
	}
	if item.ID == "" || item.Media == nil {
		t.Fatal("owned playback source was not completely indexed")
	}
	h := &hlsHTTPFixture{f: f, accounts: accounts, ffmpeg: ffmpeg, ffprobe: ffprobe, path: source, libraryID: collection.ID, item: item}
	h.policy(t, true)
	return h, owner
}
