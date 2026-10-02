//go:build linux && primary_io_measure

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const primaryHTTPMeasureBodyBytes = 4 << 20
const primaryHTTPMeasureResultLimit = 2 << 20

type primaryHTTPMeasureCounters struct {
	UserCPU    uint64 `json:"go_user_cpu_ns"`
	SystemCPU  uint64 `json:"go_system_cpu_ns"`
	TotalAlloc uint64 `json:"go_total_alloc_bytes"`
	Mallocs    uint64 `json:"go_mallocs"`
}

func primaryHTTPMeasureReadCounters() (primaryHTTPMeasureCounters, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return primaryHTTPMeasureCounters{}, err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return primaryHTTPMeasureCounters{
		UserCPU:    uint64(usage.Utime.Sec)*1_000_000_000 + uint64(usage.Utime.Usec)*1_000,
		SystemCPU:  uint64(usage.Stime.Sec)*1_000_000_000 + uint64(usage.Stime.Usec)*1_000,
		TotalAlloc: memory.TotalAlloc, Mallocs: memory.Mallocs,
	}, nil
}

type primaryHTTPMeasureWave struct {
	ID          string `json:"wave_id"`
	Purpose     string `json:"purpose"`
	Mode        string `json:"mode"`
	Concurrency int    `json:"concurrency"`
	Warmup      bool   `json:"warmup"`
	before      primaryHTTPMeasureCounters
}

type primaryHTTPMeasureControl struct {
	mu         sync.Mutex
	fixture    *streamHTTPFixture
	principals []identity.Principal
	key        string
	device     uint64
	inode      uint64
	sourceID   string
	wave       *primaryHTTPMeasureWave
	completed  int
}

// Idle samples are assertions after actual HTTP ownership and native FD cleanup.
// Sampling resolution is never used to infer read duration, throughput or peaks.
func (c *primaryHTTPMeasureControl) drain(ctx context.Context) (library.PrimaryReadMeasurementSnapshot, error) {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		observed := library.PrimaryReadMeasurementStats()
		c.fixture.f.app.originals.mu.Lock()
		owners, sources := len(c.fixture.f.app.originals.owners), len(c.fixture.f.app.originals.sources)
		c.fixture.f.app.originals.mu.Unlock()
		fds, err := primaryHTTPMeasureSourceFDs(c.device, c.inode)
		if err != nil {
			return observed, err
		}
		if observed.IO.Active == 0 && observed.IO.Queued == 0 && observed.IO.ActiveRoots == 0 && observed.IO.ActiveDomains == 0 &&
			observed.Owners.RegisteredOwners == 0 && observed.DomainClaims == 0 && owners == 0 && sources == 0 &&
			len(c.fixture.f.app.streamSlots) == 0 && fds == 0 {
			return observed, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return observed, ctx.Err()
		}
	}
}

type primaryHTTPMeasurePolicyObservation struct {
	Total              int `json:"total"`
	ExpectedAccounts   int `json:"expected_accounts"`
	References         int `json:"references"`
	DeliveredIdle      int `json:"delivered_idle"`
	UndeliveredIdle    int `json:"undelivered_idle"`
	Terminal           int `json:"terminal"`
	Cancelled          int `json:"cancelled"`
	MissingTimer       int `json:"missing_timer"`
	UnexpectedIdentity int `json:"unexpected_identity"`
}

// Concurrent 304 requests can all fail their shared fresh lease before any
// finish decrements refs. Production deliberately leaves that undelivered idle
// identity alive. This observer distinguishes it from active/delivered leakage;
// it does not change production policy or accept any foreign binding.
func (c *primaryHTTPMeasureControl) policyObservation(wave *primaryHTTPMeasureWave) (primaryHTTPMeasurePolicyObservation, bool) {
	gate := c.fixture.f.app.playbackPolicyGate()
	gate.mu.Lock()
	defer gate.mu.Unlock()
	observation := primaryHTTPMeasurePolicyObservation{Total: len(gate.leases), ExpectedAccounts: (wave.Concurrency + 7) / 8}
	expected := make(map[mediaPolicyKey]transcode.Scope)
	if observation.ExpectedAccounts > len(c.principals) {
		return observation, false
	}
	for _, principal := range c.principals[:observation.ExpectedAccounts] {
		scope := transcode.Scope{ApplicationKey: principal.IsApplicationKey(), ApplicationClientID: principal.ClientSessionID,
			UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
			ItemID: c.fixture.video.id, SourceID: c.sourceID}
		key, _, err := mediaPolicyIdentity(principal, scope)
		if err != nil {
			return observation, false
		}
		expected[key] = scope
	}
	for key, lease := range gate.leases {
		observation.References += lease.refs
		if lease.refs == 0 && lease.delivered {
			observation.DeliveredIdle++
		}
		if lease.refs == 0 && !lease.delivered {
			observation.UndeliveredIdle++
		}
		if lease.complete || lease.failed {
			observation.Terminal++
		}
		if lease.ctx == nil || lease.ctx.Err() != nil {
			observation.Cancelled++
		}
		if lease.timer == nil {
			observation.MissingTimer++
		}
		if scope, ok := expected[key]; key != lease.key || !ok || scope != lease.scope {
			observation.UnexpectedIdentity++
		}
	}
	if wave.Purpose == "original" && wave.Mode == "range" {
		return observation, observation.Total == observation.ExpectedAccounts && observation.DeliveredIdle == observation.Total &&
			observation.References == 0 && observation.Terminal == 0 && observation.Cancelled == 0 && observation.MissingTimer == 0 && observation.UnexpectedIdentity == 0
	}
	if wave.Purpose == "original" && wave.Mode == "not-modified" {
		return observation, observation.Total <= observation.ExpectedAccounts && observation.UndeliveredIdle == observation.Total &&
			observation.References == 0 && observation.Terminal == 0 && observation.Cancelled == 0 && observation.MissingTimer == 0 && observation.UnexpectedIdentity == 0
	}
	return observation, observation.Total == 0
}

func (c *primaryHTTPMeasureControl) resourceObservation() map[string]any {
	c.fixture.f.app.originals.mu.Lock()
	owners, sources := len(c.fixture.f.app.originals.owners), len(c.fixture.f.app.originals.sources)
	c.fixture.f.app.originals.mu.Unlock()
	fds, err := primaryHTTPMeasureSourceFDs(c.device, c.inode)
	return map[string]any{"original_owners": owners, "original_sources": sources,
		"http_slots": len(c.fixture.f.app.streamSlots), "source_device_inode_fds": fds,
		"fd_census_available": err == nil, "primary_read": library.PrimaryReadMeasurementStats()}
}

func primaryHTTPMeasureControlError(w http.ResponseWriter, status int, id, code string, diagnostics any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	primaryHTTPMeasureJSON(w, map[string]any{"wave_id": id, "drained": false, "error_code": code, "diagnostics": diagnostics})
}

func (c *primaryHTTPMeasureControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("X-Measure-Key") != c.key {
		primaryHTTPMeasureControlError(w, http.StatusForbidden, "", "control_denied", nil)
		return
	}
	var requested primaryHTTPMeasureWave
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requested); err != nil || requested.ID == "" || len(requested.ID) > 128 {
		primaryHTTPMeasureControlError(w, http.StatusBadRequest, "", "invalid_wave", nil)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if r.URL.Path == "/start" {
		if c.wave != nil || requested.Purpose != "original" && requested.Purpose != "download" ||
			requested.Mode != "full" && requested.Mode != "range" && requested.Mode != "head" && requested.Mode != "not-modified" ||
			requested.Concurrency != 1 && requested.Concurrency != 8 && requested.Concurrency != 32 {
			primaryHTTPMeasureControlError(w, http.StatusConflict, requested.ID, "invalid_start", nil)
			return
		}
		if _, err := c.drain(ctx); err != nil {
			primaryHTTPMeasureControlError(w, http.StatusConflict, requested.ID, "start_drain_failed", c.resourceObservation())
			return
		}
		for _, principal := range c.principals {
			c.fixture.f.app.cancelMediaPolicy(principal.SessionID, "")
		}
		var err error
		requested.before, err = primaryHTTPMeasureReadCounters()
		if err != nil {
			primaryHTTPMeasureControlError(w, http.StatusInternalServerError, requested.ID, "counters_unavailable", nil)
			return
		}
		c.wave = &requested
		primaryHTTPMeasureJSON(w, map[string]any{"wave_id": requested.ID, "started": true})
		return
	}
	if r.URL.Path != "/end" || c.wave == nil || c.wave.ID != requested.ID {
		primaryHTTPMeasureControlError(w, http.StatusConflict, requested.ID, "wave_mismatch", nil)
		return
	}
	policyBeforeDrain, _ := c.policyObservation(c.wave)
	resourcesBeforeDrain := c.resourceObservation()
	observed, err := c.drain(ctx)
	policy, policyOK := c.policyObservation(c.wave)
	diagnostics := map[string]any{"policy_before_drain": policyBeforeDrain, "resources_before_drain": resourcesBeforeDrain,
		"policy_after_drain": policy, "resources_after_drain_attempt": c.resourceObservation()}
	if err != nil {
		primaryHTTPMeasureControlError(w, http.StatusConflict, requested.ID, "end_drain_failed", diagnostics)
		return
	}
	if !policyOK {
		primaryHTTPMeasureControlError(w, http.StatusConflict, requested.ID, "policy_state_mismatch", diagnostics)
		return
	}
	after, err := primaryHTTPMeasureReadCounters()
	before := c.wave.before
	if err != nil || after.UserCPU < before.UserCPU || after.SystemCPU < before.SystemCPU || after.TotalAlloc < before.TotalAlloc || after.Mallocs < before.Mallocs {
		primaryHTTPMeasureControlError(w, http.StatusInternalServerError, requested.ID, "counters_regressed", diagnostics)
		return
	}
	counters := primaryHTTPMeasureCounters{after.UserCPU - before.UserCPU, after.SystemCPU - before.SystemCPU, after.TotalAlloc - before.TotalAlloc, after.Mallocs - before.Mallocs}
	metadataCancelled := 0
	policyAfterCleanup := policy
	if c.wave.Purpose == "original" && c.wave.Mode == "not-modified" {
		// Counters precede cleanup. Only already joined, exactly bound,
		// undelivered idle metadata identities are eligible for this natural
		// session cancellation. A payload/active/foreign identity fails above.
		metadataCancelled = policy.Total
		for _, principal := range c.principals[:policy.ExpectedAccounts] {
			c.fixture.f.app.cancelMediaPolicy(principal.SessionID, "")
		}
		policyAfterCleanup, _ = c.policyObservation(c.wave)
		observed, err = c.drain(ctx)
		if err != nil || policyAfterCleanup.Total != 0 {
			diagnostics["policy_after_cleanup"] = policyAfterCleanup
			diagnostics["resources_after_cleanup_attempt"] = c.resourceObservation()
			primaryHTTPMeasureControlError(w, http.StatusConflict, requested.ID, "metadata_cleanup_failed", diagnostics)
			return
		}
	}
	c.wave = nil
	c.completed++
	primaryHTTPMeasureJSON(w, map[string]any{"wave_id": requested.ID, "drained": true, "counters": counters,
		"primary_read_idle": observed, "source_device_inode_fds": 0, "policy_matches": true,
		"policy_before_cleanup": policy, "policy_after_cleanup": policyAfterCleanup,
		"metadata_idle_identities_cancelled": metadataCancelled, "control_observations": diagnostics})
}

func primaryHTTPMeasureJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// Stat follows /proc/self/fd entries and compares the source's actual dev/ino.
// Directory anchors, sockets, and control/report descriptors cannot impersonate
// the source. A racing closed descriptor is absent; other errors fail evidence.
func primaryHTTPMeasureSourceFDs(device, inode uint64) (int, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return 0, errors.New("measurement FD identity unavailable")
		}
		if uint64(stat.Dev) == device && stat.Ino == inode {
			count++
		}
	}
	return count, nil
}

// This fixture is opt-in and uses the natural HTTP handlers throughout. Its
// synthetic indexed bytes and Stat-only prober make no decoding/GPU claim.
func TestPrimaryHTTPActualAdapterMatchedMeasurement(t *testing.T) {
	if os.Getenv("GOBY_PRIMARY_HTTP_MEASURE") != "1" {
		t.Skip("explicit matched HTTP measurement is required")
	}
	variant, resultPath := os.Getenv("GOBY_PRIMARY_HTTP_VARIANT"), os.Getenv("GOBY_PRIMARY_HTTP_RESULT")
	if variant != "candidate" && variant != "baseline" || resultPath == "" || !filepath.IsAbs(resultPath) {
		t.Fatal("measurement requires a variant and absolute result path")
	}
	if os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("enabled measurement requires the actual PostgreSQL fixture environment")
	}
	f := newServerFixtureWithTimeout(t, 12*time.Minute)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	catalog, err := library.New(f.pool, streamHTTPProber{}, []string{root})
	if err != nil {
		t.Fatal("create independently owned measurement catalog")
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	fixture := &streamHTTPFixture{f: f, root: root, adminID: f.bootstrap(t)}
	fixture.cookie, _ = f.adminLogin(t)
	payload := make([]byte, primaryHTTPMeasureBodyBytes)
	for index := range payload {
		payload[index] = byte((index*17 + index/251) % 256)
	}
	fixture.video = fixture.addItem(t, "Matched.Synthetic.Bytes.mp4", "movies", "Videos", "mp4", "video/mp4", payload)
	principals, tokens := make([]identity.Principal, 0, 4), make([]string, 0, 4)
	for index := range 4 {
		name := fmt.Sprintf("Primary HTTP Measure Viewer %d", index)
		user, err := f.users.CreateUser(f.ctx, name, "primary-http-measure-password", false)
		if err != nil {
			t.Fatal("create bounded measurement actor")
		}
		setHTTPUserPolicy(t, f, user.ID, `{"EnableAllFolders":true,"EnableMediaPlayback":true,"EnableContentDownloading":true,"SimultaneousStreamLimit":0}`)
		token := stringValue(t, f.embyLogin(t, name, "primary-http-measure-password"), "AccessToken")
		principal, err := f.users.ResolveWithPeer(f.ctx, token, "emby", "127.0.0.1")
		if err != nil {
			t.Fatal("resolve the actual measurement credential")
		}
		principals, tokens = append(principals, principal), append(tokens, token)
	}
	fixture.viewerID, fixture.token = principals[0].User.ID, tokens[0]
	fixture.server = httptest.NewServer(f.handler)
	t.Cleanup(fixture.server.Close)
	file, source, err := catalog.OpenMediaFor(f.ctx, librarySubject(principals[0], principals[0].User.ID), fixture.video.id, "")
	if err != nil {
		t.Fatal("prepare exact measurement representation")
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil || source.Size != int64(len(payload)) {
		t.Fatal("measurement planning descriptor did not retire")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("measurement source lacks native identity")
	}
	var randomKey [32]byte
	if _, err := rand.Read(randomKey[:]); err != nil {
		t.Fatal("prepare private measurement control")
	}
	control := &primaryHTTPMeasureControl{fixture: fixture, principals: principals, key: hex.EncodeToString(randomKey[:]), device: uint64(stat.Dev), inode: stat.Ino, sourceID: source.SourceID}
	controlServer := httptest.NewUnstartedServer(control)
	controlServer.Config.ReadHeaderTimeout = 2 * time.Second
	controlServer.Config.ReadTimeout = 5 * time.Second
	controlServer.Config.WriteTimeout = 5 * time.Second
	controlServer.Config.IdleTimeout = 15 * time.Second
	controlServer.Config.MaxHeaderBytes = 16 << 10
	controlServer.Start()
	t.Cleanup(controlServer.Close)
	initial, cancelInitial := context.WithTimeout(f.ctx, 5*time.Second)
	_, err = control.drain(initial)
	cancelInitial()
	if err != nil {
		t.Fatal("measurement setup retained a native source descriptor or admission")
	}
	fullHash, rangeStart, rangeLength := sha256.Sum256(payload), 1_048_593, 1<<20
	rangeHash := sha256.Sum256(payload[rangeStart : rangeStart+rangeLength])
	private, err := newPrimaryHTTPMeasurePrivateContext(resultPath)
	if err != nil {
		t.Fatal("create retained private measurement context")
	}
	configPath, childResult := private.config, private.result
	config := map[string]any{"version": 1, "control_url": controlServer.URL, "control_key": control.key,
		"server_url": fixture.server.URL, "tokens": tokens, "concurrency": []int{1, 8, 32},
		"requests_per_client": 2, "warmup_waves": 1, "measured_waves": 2,
		"item": map[string]any{"original_path": "/emby/Videos/" + fixture.video.id + "/original.mp4",
			"download_path": "/emby/Items/" + fixture.video.id + "/Download", "download_filename": filepath.Base(fixture.video.path),
			"size": len(payload), "etag": source.ETag, "full_sha256": hex.EncodeToString(fullHash[:]),
			"range_start": rangeStart, "range_length": rangeLength, "range_sha256": hex.EncodeToString(rangeHash[:])}}
	encoded, err := json.Marshal(config)
	if err != nil || len(encoded) > 16<<10 || primaryHTTPMeasureWritePrivate(configPath, encoded) != nil {
		t.Fatal("write bounded private measurement configuration")
	}
	// This context has no automatic TempDir/defer cleanup. Unknown retirement
	// retains its bounded inputs and ownership evidence outside fixture cleanup.
	python := os.Getenv("GOBY_PRIMARY_HTTP_PYTHON")
	if python == "" {
		python, err = exec.LookPath("python3")
	}
	if err != nil || !filepath.IsAbs(python) {
		t.Fatal("measurement requires an absolute pinned Python executable")
	}
	script, err := filepath.Abs(filepath.Join("testdata", "primary_http_measure_client.py"))
	if err != nil {
		t.Fatal("resolve frozen measurement driver")
	}
	clientJoined, ownership, clientErr := primaryHTTPMeasureRunClient(f.ctx, python, script, private)
	raw, err := primaryHTTPMeasureReadBounded(childResult, primaryHTTPMeasureResultLimit)
	result := make(map[string]any)
	parsed := err == nil && json.Unmarshal(raw, &result) == nil
	if !parsed {
		result = map[string]any{"version": 1, "success": false, "failure_code": "bounded_client_report_unavailable"}
	}
	credentialsRemoved, cleanupErr := private.cleanup(clientJoined)
	control.mu.Lock()
	completed, pending := control.completed, control.wave != nil
	control.mu.Unlock()
	measurementOK := parsed && result["success"] == true && clientErr == nil && cleanupErr == nil && clientJoined && credentialsRemoved && !pending && completed == 2*4*3*3
	for _, principal := range principals {
		f.app.cancelMediaPolicy(principal.SessionID, "")
	}
	joinStarted := time.Now()
	originalPrimaryFlushCloseStore(t, fixture)
	storeJoinNS := time.Since(joinStarted).Nanoseconds()
	finalCtx, cancelFinal := context.WithTimeout(context.Background(), 5*time.Second)
	final, err := control.drain(finalCtx)
	cancelFinal()
	if err != nil {
		t.Fatal("measurement final Store/native FD/actual admission did not drain")
	}
	result["variant"], result["source_bytes"], result["source_sha256"] = variant, len(payload), hex.EncodeToString(fullHash[:])
	result["fixture_pg_pool"], result["actors"], result["http_slots"] = 8, 4, 64
	result["go_version"], result["go_max_procs"], result["go_num_cpu"] = runtime.Version(), runtime.GOMAXPROCS(0), runtime.NumCPU()
	result["server_cpu_scope"] = "Getrusage SELF: Go fixture process including metadata/control/background work; excludes Python client, PostgreSQL and child CPU"
	result["server_alloc_scope"] = "Go fixture process TotalAlloc/Mallocs including control and background allocations; no per-handler attribution"
	result["source_scope"] = "warm synthetic indexed bytes over loopback TCP; no AV decode, GPU, NAS cold-cache or background-reader isolation claim"
	result["final_store_and_dependency_join_ns"], result["final_primary_read_idle"] = storeJoinNS, final
	result["final_source_device_inode_fds"], result["client_group_joined"] = 0, clientJoined
	result["private_credentials_removed_after_client_join"] = credentialsRemoved
	result["client_ownership"] = ownership
	if !credentialsRemoved {
		result["retained_private_context"] = private.locator()
	}
	result["success"], result["completed_waves"], result["pending_wave"] = measurementOK, completed, pending
	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil || len(output) > primaryHTTPMeasureResultLimit || primaryHTTPMeasureWritePrivate(resultPath, output) != nil {
		t.Fatal("write bounded public measurement result")
	}
	if !measurementOK {
		t.Fatalf("measurement failed after actual source cleanup: client_error_type=%T completed_waves=%d pending=%v group_joined=%v", clientErr, completed, pending, clientJoined)
	}
	t.Logf("primary_http_measurement variant=%s completed_waves=%d source_bytes=%d actual_tcp_and_ownership=passed", variant, completed, len(payload))
}

func primaryHTTPMeasureReadBounded(path string, limit int) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, errors.New("measurement file exceeds its bound")
	}
	return raw, nil
}

func primaryHTTPMeasureWritePrivate(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}

type primaryHTTPMeasureCommandBuffer struct {
	data   bytes.Buffer
	cancel context.CancelFunc
	err    error
}

type primaryHTTPMeasurePrivateContext struct {
	parent, directory, config, result, ownership, failure, stdout, stderr string
}

// The artifact owner parent is independently created at 0700. Neither it nor
// its child is registered with testing.TempDir cleanup: unknown native owners
// must retain their inputs after the Go test returns.
func newPrimaryHTTPMeasurePrivateContext(resultPath string) (*primaryHTTPMeasurePrivateContext, error) {
	if !filepath.IsAbs(resultPath) {
		return nil, errors.New("private context requires an absolute artifact path")
	}
	artifactParent := filepath.Dir(resultPath)
	info, err := os.Lstat(artifactParent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("private context artifact parent is unavailable")
	}
	parent := filepath.Join(artifactParent, "."+filepath.Base(resultPath)+".private")
	if err := os.Mkdir(parent, 0o700); err != nil {
		return nil, err
	}
	directory, err := os.MkdirTemp(parent, "client-")
	if err != nil {
		// No command or input exists during this constructor failure.
		_ = os.Remove(parent)
		return nil, err
	}
	return &primaryHTTPMeasurePrivateContext{parent: parent, directory: directory,
		config: filepath.Join(directory, "credentials.json"), result: filepath.Join(directory, "client-result.json"),
		ownership: filepath.Join(directory, "client-ownership.json"), failure: filepath.Join(directory, "client-failure.json"),
		stdout: filepath.Join(directory, "client-stdout.log"), stderr: filepath.Join(directory, "client-stderr.log")}, nil
}

func (p *primaryHTTPMeasurePrivateContext) locator() string {
	return filepath.Join(filepath.Base(p.parent), filepath.Base(p.directory))
}

func (p *primaryHTTPMeasurePrivateContext) knownFiles() []string {
	return []string{p.config, p.result, p.ownership, p.failure, p.stdout, p.stderr}
}

// cleanup is a filesystem ownership decision, not a native retirement proof.
// Only RunProcessWithRetirement's actual group/Wait/copier/capacity receipt may
// supply joined=true in the measurement path. Unknown input is left untouched.
func (p *primaryHTTPMeasurePrivateContext) cleanup(joined bool) (bool, error) {
	if !joined {
		return false, nil
	}
	known := make(map[string]bool)
	for _, path := range p.knownFiles() {
		known[filepath.Base(path)] = true
	}
	entries, err := os.ReadDir(p.directory)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		info, err := os.Lstat(filepath.Join(p.directory, entry.Name()))
		if err != nil || !known[entry.Name()] || !info.Mode().IsRegular() {
			return false, errors.New("private context has an unowned entry")
		}
	}
	parentEntries, err := os.ReadDir(p.parent)
	if err != nil || len(parentEntries) != 1 || parentEntries[0].Name() != filepath.Base(p.directory) {
		return false, errors.New("private context parent has an unowned entry")
	}
	for _, path := range p.knownFiles() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	if err := os.Remove(p.directory); err != nil {
		return false, err
	}
	if err := os.Remove(p.parent); err != nil {
		return false, err
	}
	return true, nil
}

// These bounded files are private evidence. Updating one never authorizes a
// signal or a cleanup retry against an already reaped numeric PID.
func primaryHTTPMeasurePrivateEvidence(path string, data []byte) error {
	if len(data) > 16<<10 {
		return media.ErrOutputLimit
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return primaryHTTPMeasureWritePrivate(path, data)
	}
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("private evidence target is unowned")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Close())
}

type primaryHTTPMeasureClientOwnership struct {
	Version             int    `json:"version"`
	OwnerGoPID          int    `json:"owner_go_pid"`
	LeaderPID           int    `json:"leader_pid"`
	LeaderStartTicks    uint64 `json:"leader_start_ticks"`
	LeaderProcessGroup  int    `json:"leader_process_group"`
	LeaderSession       int    `json:"leader_session"`
	ObservedParentPID   int    `json:"observed_parent_pid"`
	IdentityCaptured    bool   `json:"identity_captured_before_wait"`
	GroupRetired        bool   `json:"group_retirement_verified"`
	WaitAndCopiesJoined bool   `json:"wait_and_copies_joined"`
	CapacityRestored    bool   `json:"process_capacity_restored"`
	State               string `json:"state"`
	PostWaitSignalRetry bool   `json:"post_wait_numeric_signal_retry"`
}

func primaryHTTPMeasureCaptureClient(command *exec.Cmd, owner *primaryHTTPMeasureClientOwnership) error {
	owner.LeaderPID = command.Process.Pid
	raw, err := primaryHTTPMeasureReadBounded(filepath.Join("/proc", strconv.Itoa(owner.LeaderPID), "stat"), 8192)
	if err != nil {
		return err
	}
	end := strings.LastIndex(string(raw), ") ")
	if end < 0 {
		return media.ErrProcessRetirementUnknown
	}
	fields := strings.Fields(string(raw[end+2:]))
	if len(fields) < 20 {
		return media.ErrProcessRetirementUnknown
	}
	owner.ObservedParentPID, err = strconv.Atoi(fields[1])
	if err == nil {
		owner.LeaderProcessGroup, err = strconv.Atoi(fields[2])
	}
	if err == nil {
		owner.LeaderSession, err = strconv.Atoi(fields[3])
	}
	if err == nil {
		owner.LeaderStartTicks, err = strconv.ParseUint(fields[19], 10, 64)
	}
	if err != nil || owner.LeaderProcessGroup != owner.LeaderPID || owner.ObservedParentPID != owner.OwnerGoPID {
		return media.ErrProcessRetirementUnknown
	}
	owner.IdentityCaptured, owner.State = true, "started_identity_captured"
	return nil
}

func (b *primaryHTTPMeasureCommandBuffer) Write(data []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if len(data) > (4<<10)-b.data.Len() {
		b.err = media.ErrOutputLimit
		b.cancel()
		return 0, b.err
	}
	return b.data.Write(data)
}

func primaryHTTPMeasureRunClient(parent context.Context, python, script string, private *primaryHTTPMeasurePrivateContext) (bool, primaryHTTPMeasureClientOwnership, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-I", script, "--config", private.config, "--output", private.result)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC"}
	command.Dir, command.SysProcAttr, command.WaitDelay = "/", &syscall.SysProcAttr{Setpgid: true}, time.Second
	stdout, stderr := &primaryHTTPMeasureCommandBuffer{cancel: cancel}, &primaryHTTPMeasureCommandBuffer{cancel: cancel}
	command.Stdout, command.Stderr = stdout, stderr
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
	capacityBefore := media.GetProcessCapacityStats()
	ownership := primaryHTTPMeasureClientOwnership{Version: 1, OwnerGoPID: os.Getpid(), State: "not_started"}
	runErr := media.RunProcessWithRetirement(ctx, command, func() error {
		// Start succeeded and the leader is still owned and unreaped. Capture
		// its kernel identity before any Wait, including unknown-retirement paths.
		captureErr := primaryHTTPMeasureCaptureClient(command, &ownership)
		audit, auditErr := json.Marshal(ownership)
		if auditErr == nil {
			auditErr = primaryHTTPMeasurePrivateEvidence(private.ownership, audit)
		}
		// Evidence failure still follows the existing actual leader/group wait
		// before returning uncertainty. It must not fence cancellation while a
		// still-running client has yet to reach its own bounded termination.
		// Preserve the leader's PID/PGID through the final group fence. Wait
		// subsequently reaps it and joins stdout/stderr/context copiers.
		var information [16]uint64
		for {
			_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(command.Process.Pid), uintptr(unsafe.Pointer(&information[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
			if errno == syscall.EINTR {
				continue
			}
			if errno != 0 {
				return errors.Join(errno, captureErr, auditErr)
			}
			break
		}
		groupMu.Lock()
		defer groupMu.Unlock()
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			count, err := primaryHTTPMeasureGroupMembers(command.Process.Pid)
			if err != nil {
				return err
			}
			if count == 0 {
				retired = true
				return errors.Join(captureErr, auditErr)
			}
			if time.Now().After(deadline) {
				return media.ErrProcessRetirementUnknown
			}
			time.Sleep(time.Millisecond)
		}
	})
	groupMu.Lock()
	joined := retired && command.ProcessState != nil
	groupMu.Unlock()
	ownership.GroupRetired = retired
	ownership.WaitAndCopiesJoined = command.ProcessState != nil
	ownership.CapacityRestored = media.GetProcessCapacityStats() == capacityBefore
	if !ownership.CapacityRestored {
		joined = false
	}
	if joined {
		ownership.State = "group_wait_copies_joined"
	} else if command.Process != nil {
		ownership.State = "retirement_unproven"
	}
	audit, auditErr := json.Marshal(ownership)
	if auditErr == nil {
		auditErr = primaryHTTPMeasurePrivateEvidence(private.ownership, audit)
	}
	if auditErr != nil {
		joined = false
		ownership.State = "private_audit_unconfirmed"
	}
	if err := errors.Join(runErr, stdout.err, stderr.err, ctx.Err(), auditErr); err != nil || stderr.data.Len() != 0 {
		failure, _ := json.Marshal(map[string]any{"error_type": fmt.Sprintf("%T", err), "stdout_bytes": stdout.data.Len(), "stderr_bytes": stderr.data.Len(), "ownership": ownership})
		evidenceErr := errors.Join(primaryHTTPMeasurePrivateEvidence(private.failure, failure),
			primaryHTTPMeasurePrivateEvidence(private.stdout, stdout.data.Bytes()), primaryHTTPMeasurePrivateEvidence(private.stderr, stderr.data.Bytes()))
		return joined, ownership, errors.Join(auditErr, evidenceErr, fmt.Errorf("bounded measurement client failure: error_type=%T stdout_bytes=%d stderr_bytes=%d", err, stdout.data.Len(), stderr.data.Len()))
	}
	return joined, ownership, auditErr
}

func primaryHTTPMeasureGroupMembers(leader int) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == leader {
			continue
		}
		raw, err := primaryHTTPMeasureReadBounded(filepath.Join("/proc", entry.Name(), "stat"), 8192)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		end := strings.LastIndex(string(raw), ") ")
		if end < 0 {
			return 0, media.ErrProcessRetirementUnknown
		}
		fields := strings.Fields(string(raw[end+2:]))
		if len(fields) < 3 {
			return 0, media.ErrProcessRetirementUnknown
		}
		group, err := strconv.Atoi(fields[2])
		if err != nil {
			return 0, media.ErrProcessRetirementUnknown
		}
		if group == leader {
			count++
		}
	}
	return count, nil
}
