package tasks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type managerProbeGate struct {
	release chan struct{}
	once    sync.Once
}

func newManagerProbeGate() *managerProbeGate { return &managerProbeGate{release: make(chan struct{})} }
func (g *managerProbeGate) open()            { g.once.Do(func() { close(g.release) }) }

type managerTestProber struct {
	mu              sync.Mutex
	defaultGate     *managerProbeGate
	gates           map[string]*managerProbeGate
	entered         map[string]int
	cancelled       map[string]int
	active, maximum int
}

func newManagerTestProber() *managerTestProber {
	return &managerTestProber{gates: make(map[string]*managerProbeGate), entered: make(map[string]int), cancelled: make(map[string]int)}
}

func (*managerTestProber) CacheVersion() int { return media.CurrentProbeVersion }

func (p *managerTestProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return media.Info{}, err
	}
	stat, err := file.Stat()
	if err != nil {
		return media.Info{}, err
	}
	key := string(data)
	p.mu.Lock()
	gate := p.gates[key]
	if gate == nil {
		gate = p.defaultGate
	}
	p.entered[key]++
	p.active++
	if p.active > p.maximum {
		p.maximum = p.active
	}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); p.active--; p.mu.Unlock() }()
	if gate != nil {
		select {
		case <-gate.release:
		case <-ctx.Done():
			p.mu.Lock()
			p.cancelled[key]++
			p.mu.Unlock()
			// A cancellation request is not proof that the worker has finished.
			// Tests explicitly release this final cleanup phase.
			<-gate.release
			return media.Info{}, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return media.Info{}, err
	}
	return media.Info{ProbeVersion: media.CurrentProbeVersion, FileChangeTimeNs: media.FileChangeTime(stat), Size: stat.Size(),
		DurationTicks: 10 * media.TicksPerSecond, Container: "mov,mp4", Streams: []media.Stream{{Index: 0, Codec: "h264", CodecType: "video", Width: 160, Height: 90}}}, nil
}

func (p *managerTestProber) releaseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.defaultGate != nil {
		p.defaultGate.open()
	}
	for _, gate := range p.gates {
		gate.open()
	}
}

func (p *managerTestProber) observed(key string, cancelled bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cancelled {
		return p.cancelled[key] > 0
	}
	return p.entered[key] > 0
}

type managerObservedScans struct {
	*library.Store
	mu          sync.Mutex
	admissions  map[library.ScanAdmissionKind]int
	dropUpdates bool
}

func (s *managerObservedScans) AdmitTaskScan(ctx context.Context, childID string) (library.ScanAdmission, error) {
	result, err := s.Store.AdmitTaskScan(ctx, childID)
	if err == nil {
		s.mu.Lock()
		s.admissions[result.Kind]++
		s.mu.Unlock()
	}
	return result, err
}

func (s *managerObservedScans) ScanUpdates() <-chan struct{} {
	if s.dropUpdates {
		return nil
	}
	return s.Store.ScanUpdates()
}

func (s *managerObservedScans) count(kind library.ScanAdmissionKind) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.admissions[kind]
}

type managerTestLibrary struct{ id, path, payload string }
type managerFixture struct {
	ctx          context.Context
	pool         *pgxpool.Pool
	catalog      *library.Store
	store        *Store
	actor        Actor
	definition   Definition
	root, schema string
	prober       *managerTestProber
	managers     []*Manager
	catalogs     []*library.Store
	probers      []*managerTestProber
}

func newManagerFixture(t *testing.T) *managerFixture {
	t.Helper()
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL task-manager tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("create manager integration database connection")
	}
	t.Cleanup(admin.Close)
	var suffix [12]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	schema := "goby_manager_test_" + hex.EncodeToString(suffix[:])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove owned manager schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse manager database configuration")
	}
	if config.ConnConfig.RuntimeParams == nil {
		config.ConnConfig.RuntimeParams = make(map[string]string)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	config.ConnConfig.RuntimeParams["application_name"] = schema
	config.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("create isolated manager database pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	f := &managerFixture{ctx: ctx, pool: pool, root: t.TempDir(), schema: schema, prober: newManagerTestProber()}
	f.catalog, err = library.New(pool, f.prober, []string{f.root})
	if err != nil {
		t.Fatal(err)
	}
	f.catalogs = append(f.catalogs, f.catalog)
	f.probers = append(f.probers, f.prober)
	t.Cleanup(func() {
		for _, prober := range f.probers {
			prober.releaseAll()
		}
		for index := len(f.managers) - 1; index >= 0; index-- {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			err := f.managers[index].Close(cleanup)
			stop()
			if err != nil && !errors.Is(err, library.ErrUnavailable) && !errors.Is(err, ErrUnavailable) {
				t.Errorf("close task manager: %v", err)
			}
		}
		for index := len(f.catalogs) - 1; index >= 0; index-- {
			cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
			err := f.catalogs[index].Close(cleanup)
			stop()
			if err != nil && !errors.Is(err, library.ErrUnavailable) {
				t.Errorf("close manager scan executor: %v", err)
			}
		}
	})
	f.store, err = New(pool, f.catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RecoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	f.definition, err = f.store.GetByKey(ctx, LibraryScanKey)
	if err != nil {
		t.Fatal(err)
	}
	users := identity.New(pool)
	if _, err := users.Bootstrap(ctx, "Task Administrator", "task-administrator-password"); err != nil {
		t.Fatal(err)
	}
	credentials, err := users.Authenticate(ctx, "Task Administrator", "task-administrator-password", identity.Client{Name: "Task Manager Tests", DeviceID: "task-administrator"}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	principal, err := users.Resolve(ctx, credentials.Token, "admin")
	if err != nil {
		t.Fatal(err)
	}
	f.actor = Actor{Principal: principal, Audience: identity.AdministratorNative}
	return f
}

func (f *managerFixture) addLibraries(t *testing.T, count int) []managerTestLibrary {
	t.Helper()
	result := make([]managerTestLibrary, 0, count)
	for index := 0; index < count; index++ {
		root, err := os.MkdirTemp(f.root, "library-")
		if err != nil {
			t.Fatal(err)
		}
		payload := filepath.Base(root)
		if err := os.WriteFile(filepath.Join(root, "Fixture.mp4"), []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		collection, err := f.catalog.CreateLibrary(f.ctx, "Manager "+payload, "movies", []string{root})
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, managerTestLibrary{id: collection.ID, path: root, payload: payload})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].id < result[j].id })
	return result
}

func (f *managerFixture) manager(t *testing.T, dropUpdates bool) (*Manager, *managerObservedScans) {
	t.Helper()
	executor := &managerObservedScans{Store: f.catalog, admissions: make(map[library.ScanAdmissionKind]int), dropUpdates: dropUpdates}
	manager, err := NewManager(f.store, executor, ManagerOptions{ReconcileInterval: 250 * time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.managers = append(f.managers, manager)
	return manager, executor
}

func managerWait(t *testing.T, parent context.Context, description string, condition func(context.Context) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if condition(ctx) {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %s", description)
		case <-tick.C:
		}
	}
}

func managerWaitRun(t *testing.T, f *managerFixture, id string, state RunState) Run {
	t.Helper()
	var result Run
	managerWait(t, f.ctx, "task run "+string(state), func(ctx context.Context) bool {
		var err error
		result, err = f.store.GetRun(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return result.State == state
	})
	return result
}

func TestManagerRetriesCapacityAndIndependentScansBeyondTerminalPage(t *testing.T) {
	f := newManagerFixture(t)
	collections := f.addLibraries(t, 205)
	ownedGate, independentGate := newManagerProbeGate(), newManagerProbeGate()
	last := collections[len(collections)-1]
	f.prober.defaultGate = ownedGate
	f.prober.gates[last.payload] = independentGate
	independent, err := f.catalog.StartScan(f.ctx, last.id)
	if err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "independent scan probe", func(context.Context) bool { return f.prober.observed(last.payload, false) })
	manager, observed := f.manager(t, true)
	requestCtx, cancelRequest := context.WithCancel(f.ctx)
	admission, err := manager.Start(requestCtx, f.actor, StartRequest{TaskID: f.definition.ID, RequestID: "bounded-manual-run"})
	cancelRequest()
	if err != nil || !admission.Admitted || admission.Run.TotalChildren != 205 {
		t.Fatalf("admit full snapshot: %v", err)
	}
	managerWait(t, f.ctx, "bounded scanner queue saturation", func(context.Context) bool { return observed.count(library.ScanQueueFull) > 0 })
	// A library added after admission is not silently appended to the snapshot.
	f.addLibraries(t, 1)
	var missing string
	if err := f.pool.QueryRow(f.ctx, `SELECT library_id FROM task_run_children WHERE run_id=$1
		AND state='waiting' AND library_id<>$2 ORDER BY ordinal LIMIT 1`, admission.Run.ID, last.id).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if err := f.catalog.DeleteLibrary(f.ctx, missing); err != nil {
		t.Fatal(err)
	}
	ownedGate.open()
	managerWait(t, f.ctx, "terminal prefix with one independent-scan waiter", func(ctx context.Context) bool {
		var terminal, waiting int
		if err := f.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE state NOT IN ('waiting','queued','running')),
			count(*) FILTER (WHERE state='waiting' AND library_id=$2 AND scan_job_id IS NULL)
			FROM task_run_children WHERE run_id=$1`, admission.Run.ID, last.id).Scan(&terminal, &waiting); err != nil {
			t.Fatal(err)
		}
		return terminal == 204 && waiting == 1 && observed.count(library.ScanDuplicate) > 0
	})
	job, err := f.catalog.GetJob(f.ctx, independent.ID)
	if err != nil || job.Status != "Running" || job.CancelRequested || job.TaskChildID != "" {
		t.Fatal("task coordinator adopted or cancelled an independent scan")
	}
	independentGate.open()
	finished := managerWaitRun(t, f, admission.Run.ID, RunFailed)
	if finished.TotalChildren != 205 || finished.TerminalChildren != 205 || finished.CompletedChildren != 204 || finished.UnavailableChildren != 1 {
		t.Fatal("capacity retries or missing-library handling lost snapshot children")
	}
	var lastJob string
	if err := f.pool.QueryRow(f.ctx, "SELECT scan_job_id FROM task_run_children WHERE run_id=$1 AND library_id=$2", finished.ID, last.id).Scan(&lastJob); err != nil || lastJob == independent.ID || lastJob == "" {
		t.Fatal("waiting task child failed to obtain its own scan after the independent scan ended")
	}
	f.prober.mu.Lock()
	maximum := f.prober.maximum
	f.prober.mu.Unlock()
	if maximum > 2 {
		t.Fatal("task coordination bypassed the scanner's bounded worker count")
	}
	if observed.count(library.ScanAdmitted) != 204 {
		t.Fatal("a task child was admitted twice or lost during queue retries")
	}
	page, err := f.store.ListRuns(f.ctx, f.definition.ID, Page{})
	if err != nil || page.TotalRecordCount != 1 {
		t.Fatal("manual coordination automatically created an additional run")
	}
}

func TestManagerStopWaitsForOwnedWorkersAndLeavesIndependentScanAlive(t *testing.T) {
	f := newManagerFixture(t)
	collections := f.addLibraries(t, 2)
	independentGate, ownedGate := newManagerProbeGate(), newManagerProbeGate()
	f.prober.gates[collections[0].payload] = independentGate
	f.prober.gates[collections[1].payload] = ownedGate
	independent, err := f.catalog.StartScan(f.ctx, collections[0].id)
	if err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "standalone scan", func(context.Context) bool { return f.prober.observed(collections[0].payload, false) })
	manager, _ := f.manager(t, true)
	admission, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID, RequestID: "stop-owned-only"})
	if err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "owned scan", func(context.Context) bool { return f.prober.observed(collections[1].payload, false) })
	stopping, err := manager.Stop(f.ctx, f.actor, admission.Run.ID)
	if err != nil || stopping.State != RunStopping {
		t.Fatalf("request owned task stop: %v", err)
	}
	managerWait(t, f.ctx, "owned scan cancellation signal", func(context.Context) bool { return f.prober.observed(collections[1].payload, true) })
	stillStopping, err := f.store.GetRun(f.ctx, admission.Run.ID)
	if err != nil || stillStopping.State != RunStopping || stillStopping.FinishedAt != nil {
		t.Fatal("task stop reported completion before its owned worker finished")
	}
	job, err := f.catalog.GetJob(f.ctx, independent.ID)
	if err != nil || job.Status != "Running" || job.CancelRequested || f.prober.observed(collections[0].payload, true) {
		t.Fatal("task stop cancelled an independent library scan")
	}
	ownedGate.open()
	finished := managerWaitRun(t, f, admission.Run.ID, RunCancelled)
	if finished.TotalChildren != 2 || finished.CancelledChildren != 2 || finished.StopReason != "administrator" {
		t.Fatal("owned task stop lost its waiting and running child outcomes")
	}
	if !manager.Available() {
		t.Fatal("stopping one run closed the task manager")
	}
	if err := manager.Close(f.ctx); err != nil {
		t.Fatalf("close idle task manager while standalone scan remains active: %v", err)
	}
	job, err = f.catalog.GetJob(f.ctx, independent.ID)
	if err != nil || job.Status != "Running" || job.CancelRequested || !f.catalog.Available() || f.prober.observed(collections[0].payload, true) {
		t.Fatal("task manager close waited for or cancelled an independent scan")
	}
	independentGate.open()
}

func TestManagerBeginCloseFencesAdmissionAndCallerTimeoutPreservesCleanup(t *testing.T) {
	f := newManagerFixture(t)
	collection := f.addLibraries(t, 1)[0]
	gate := newManagerProbeGate()
	f.prober.gates[collection.payload] = gate
	manager, _ := f.manager(t, true)
	admission, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID, RequestID: "shutdown-owned"})
	if err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "shutdown scan", func(context.Context) bool { return f.prober.observed(collection.payload, false) })
	manager.BeginClose()
	if manager.Available() {
		t.Fatal("BeginClose did not synchronously fence task admission")
	}
	if _, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closing manager accepted another user start")
	}
	if _, err := manager.Stop(f.ctx, f.actor, admission.Run.ID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("closing manager accepted another user mutation")
	}
	waitCtx, cancel := context.WithTimeout(f.ctx, 20*time.Millisecond)
	err = manager.Close(waitCtx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("Close did not honor its caller deadline while a worker remained active")
	}
	managerWait(t, f.ctx, "background shutdown cancellation", func(context.Context) bool { return f.prober.observed(collection.payload, true) })
	run, err := f.store.GetRun(f.ctx, admission.Run.ID)
	if err != nil || run.State != RunStopping || run.StopReason != "shutdown" || run.FinishedAt != nil {
		t.Fatal("shutdown did not persist its cause while awaiting the worker")
	}
	gate.open()
	finished := managerWaitRun(t, f, admission.Run.ID, RunInterrupted)
	if finished.InterruptedChildren != 1 || finished.StopReason != "shutdown" {
		t.Fatal("shutdown failed to preserve interrupted child and run outcomes")
	}
	if err := manager.Close(f.ctx); err != nil {
		t.Fatalf("join background task cleanup: %v", err)
	}
	if !f.catalog.Available() || f.catalog.CheckOwnership(f.ctx) != nil {
		t.Fatal("task manager released catalog ownership before library shutdown")
	}
	f.addLibraries(t, 1)
	page, err := f.store.ListRuns(f.ctx, f.definition.ID, Page{})
	if err != nil || page.TotalRecordCount != 1 {
		t.Fatal("shutdown created a replacement execution")
	}
}

func TestManagerOwnerLossFencesWritesAndRecoveryDoesNotResumeOldRun(t *testing.T) {
	f := newManagerFixture(t)
	collection := f.addLibraries(t, 1)[0]
	gate := newManagerProbeGate()
	f.prober.gates[collection.payload] = gate
	manager, _ := f.manager(t, false)
	admission, err := manager.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID, RequestID: "owner-loss-run"})
	if err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "old owner scan", func(context.Context) bool { return f.prober.observed(collection.payload, false) })
	var ownerPID int32
	if err := f.pool.QueryRow(f.ctx, `SELECT locks.pid FROM pg_locks locks JOIN pg_stat_activity activity ON activity.pid=locks.pid
		WHERE locks.locktype='advisory' AND locks.granted AND activity.application_name=$1`, f.schema).Scan(&ownerPID); err != nil {
		t.Fatal(err)
	}
	var terminated bool
	if err := f.pool.QueryRow(f.ctx, "SELECT pg_terminate_backend($1)", ownerPID).Scan(&terminated); err != nil || !terminated {
		t.Fatal("terminate the isolated catalog ownership session")
	}
	manager.Wake()
	managerWait(t, f.ctx, "task owner fencing", func(context.Context) bool { return !manager.Available() })
	if err := manager.Close(f.ctx); !errors.Is(err, library.ErrUnavailable) {
		t.Fatalf("lost owner manager close error = %v", err)
	}
	beforeRecovery, err := f.store.GetRun(f.ctx, admission.Run.ID)
	if err != nil || beforeRecovery.State != RunRunning || beforeRecovery.StopReason != "" {
		t.Fatal("fenced manager wrote shutdown state after losing its catalog ownership")
	}
	newProber := newManagerTestProber()
	successor, err := library.New(f.pool, newProber, []string{f.root})
	if err != nil {
		t.Fatalf("acquire successor catalog ownership: %v", err)
	}
	f.catalogs = append(f.catalogs, successor)
	f.probers = append(f.probers, newProber)
	recovered, err := New(f.pool, successor)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := recovered.RecoverRuns(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.catalog, f.store = successor, recovered
	finished, err := f.store.GetRun(f.ctx, admission.Run.ID)
	if err != nil || finished.State != RunInterrupted || finished.InterruptedChildren != 1 {
		t.Fatal("successor failed to recover the abandoned owned scan before its parent")
	}
	next, _ := f.manager(t, true)
	gate.open()
	if err := f.catalogs[0].Close(f.ctx); !errors.Is(err, library.ErrUnavailable) {
		t.Fatal("old catalog did not retain its ownership-loss result")
	}
	after, err := f.store.GetRun(f.ctx, admission.Run.ID)
	if err != nil || after.State != RunInterrupted || after.FinishedAt == nil || finished.FinishedAt == nil || !after.FinishedAt.Equal(*finished.FinishedAt) {
		t.Fatal("late old-owner cleanup overwrote successor recovery")
	}
	retry, err := next.Start(f.ctx, f.actor, StartRequest{TaskID: f.definition.ID, RequestID: "owner-loss-run"})
	if err != nil || retry.Admitted || retry.Run.ID != admission.Run.ID || retry.Run.State != RunInterrupted {
		t.Fatal("a recovered request receipt resumed the interrupted execution")
	}
	page, err := f.store.ListRuns(f.ctx, f.definition.ID, Page{})
	if err != nil || page.TotalRecordCount != 1 {
		t.Fatal("manager startup automatically resumed an abandoned run")
	}
	active, err := f.store.ListActiveRuns(f.ctx, Page{})
	if err != nil || active.TotalRecordCount != 0 {
		t.Fatal("recovery left an old run active for the new manager")
	}
}

func managerReplaceTriggers(t *testing.T, f *managerFixture, rules ...ScheduleRule) Definition {
	t.Helper()
	current, err := f.store.Get(f.ctx, f.definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{
		TaskID: current.ID, Revision: current.Revision, ScheduleTimezone: "UTC", Triggers: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.definition = updated
	return updated
}

func TestManagerExecutesStartupAndIntervalOccurrencesAndStopsSchedulingOnClose(t *testing.T) {
	f := newManagerFixture(t)
	collection := f.addLibraries(t, 1)[0]
	gate := newManagerProbeGate()
	f.prober.gates[collection.payload] = gate
	interval := ScheduleTicksPerSecond
	definition := managerReplaceTriggers(t, f, ScheduleRule{Kind: ScheduleStartup}, ScheduleRule{Kind: ScheduleInterval, IntervalTicks: &interval})
	manager, _ := f.manager(t, true)
	managerWait(t, f.ctx, "startup-triggered scan", func(context.Context) bool { return f.prober.observed(collection.payload, false) })
	active, err := f.store.ListActiveRuns(f.ctx, Page{})
	if err != nil || len(active.Items) != 1 {
		t.Fatal("startup scheduling did not admit exactly one active library run")
	}
	startup := active.Items[0]
	if startup.Source != "startup" || startup.TriggerID == nil || *startup.TriggerID != definition.Triggers[0].ID || startup.ScheduledFor == nil ||
		startup.ActorUserID != "" || startup.ActorSessionID != "" || startup.ActorKind != "" {
		t.Fatal("startup execution did not preserve explicit system provenance and its trigger snapshot")
	}
	managerWait(t, f.ctx, "real interval overlap", func(ctx context.Context) bool {
		var overlap bool
		if err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_occurrences
			WHERE trigger_id=$1 AND disposition='overlap' AND run_id=$2)`, definition.Triggers[1].ID, startup.ID).Scan(&overlap); err != nil {
			t.Fatal(err)
		}
		return overlap
	})
	page, err := f.store.ListRuns(f.ctx, f.definition.ID, Page{})
	if err != nil || page.TotalRecordCount != 1 {
		t.Fatal("an interval overlapping an active startup scan created a second execution")
	}
	gate.open()
	managerWaitRun(t, f, startup.ID, RunCompleted)
	managerWait(t, f.ctx, "subsequent interval execution", func(ctx context.Context) bool {
		var completed bool
		if err := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM task_runs
			WHERE task_id=$1 AND source='schedule' AND state='completed' AND trigger_id=$2)`, definition.ID, definition.Triggers[1].ID).Scan(&completed); err != nil {
			t.Fatal(err)
		}
		return completed
	})
	if !manager.Available() {
		t.Fatal("successful scheduler initialization and dispatch did not report readiness")
	}
	manager.BeginClose()
	if err := manager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	var before int64
	var next time.Time
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs),next_fire_at
		FROM task_triggers WHERE id=$1`, definition.Triggers[1].ID).Scan(&before, &next); err != nil {
		t.Fatal(err)
	}
	managerWait(t, f.ctx, "next interval after closed manager", func(ctx context.Context) bool {
		var due bool
		if err := f.pool.QueryRow(ctx, "SELECT clock_timestamp() > $1::timestamptz", next).Scan(&due); err != nil {
			t.Fatal(err)
		}
		return due
	})
	manager.Wake()
	var after int64
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM task_runs").Scan(&after); err != nil || after != before {
		t.Fatal("a closed manager admitted another due occurrence")
	}
}

func TestManagerRuntimeLimitUsesMonotonicSnapshotWhileWaitingForIndependentScan(t *testing.T) {
	for _, limited := range []bool{true, false} {
		name := "unlimited-ignores-audit-deadline"
		if limited {
			name = "limited-ignores-future-audit-deadline"
		}
		t.Run(name, func(t *testing.T) {
			f := newManagerFixture(t)
			collection := f.addLibraries(t, 1)[0]
			gate := newManagerProbeGate()
			f.prober.gates[collection.payload] = gate
			independent, err := f.catalog.StartScan(f.ctx, collection.id)
			if err != nil {
				t.Fatal(err)
			}
			managerWait(t, f.ctx, "independent scan holding the library slot", func(context.Context) bool { return f.prober.observed(collection.payload, false) })
			rule := ScheduleRule{Kind: ScheduleStartup}
			limit := 2 * ScheduleTicksPerSecond
			if limited {
				rule.MaxRuntimeTicks = &limit
			}
			managerReplaceTriggers(t, f, rule)
			manager, observed := f.manager(t, true)
			var run Run
			managerWait(t, f.ctx, "startup run waiting on the occupied library", func(ctx context.Context) bool {
				page, err := f.store.ListActiveRuns(ctx, Page{})
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) == 1 && page.Items[0].State == RunRunning && observed.count(library.ScanDuplicate) > 0 {
					run = page.Items[0]
					return true
				}
				return false
			})
			statement := "UPDATE task_runs SET deadline_at=clock_timestamp()-interval '1 day' WHERE id=$1 AND state='running'"
			if limited {
				statement = "UPDATE task_runs SET deadline_at=clock_timestamp()+interval '1 day' WHERE id=$1 AND state='running'"
			}
			changed, err := f.pool.Exec(f.ctx, statement, run.ID)
			if err != nil || changed.RowsAffected() != 1 {
				t.Fatal("replace only the audit deadline of the active fixture")
			}
			if limited {
				// Replacing the rule cannot extend an already admitted limit.
				replacementLimit := 60 * ScheduleTicksPerSecond
				managerReplaceTriggers(t, f, ScheduleRule{Kind: ScheduleStartup, MaxRuntimeTicks: &replacementLimit})
				finished := managerWaitRun(t, f, run.ID, RunCancelled)
				if finished.StopReason != "max_runtime" || finished.ErrorCode != "max_runtime" || finished.MaxRuntimeTicks == nil || *finished.MaxRuntimeTicks != limit || finished.CancelledChildren != 1 {
					t.Fatal("maximum runtime did not use the admitted snapshot while waiting for scan capacity")
				}
			} else {
				before := observed.count(library.ScanDuplicate)
				manager.Wake()
				managerWait(t, f.ctx, "another unlimited-run coordination pass", func(context.Context) bool { return observed.count(library.ScanDuplicate) > before })
				current, err := f.store.GetRun(f.ctx, run.ID)
				if err != nil || current.State != RunRunning || current.StopReason != "" {
					t.Fatal("an audit timestamp became an execution timer for an unlimited run")
				}
				if _, err := manager.Stop(f.ctx, f.actor, run.ID); err != nil {
					t.Fatal(err)
				}
				managerWaitRun(t, f, run.ID, RunCancelled)
			}
			job, err := f.catalog.GetJob(f.ctx, independent.ID)
			if err != nil || job.Status != "Running" || job.CancelRequested || f.prober.observed(collection.payload, true) {
				t.Fatal("task runtime enforcement cancelled the independent scan occupying its library slot")
			}
			gate.open()
		})
	}
}

func TestManagerInitializationRetriesOneStartupIdentityWithoutDispatchingOldDueWork(t *testing.T) {
	f := newManagerFixture(t)
	f.addLibraries(t, 1)
	interval := int64(3600) * ScheduleTicksPerSecond
	definition := managerReplaceTriggers(t, f, ScheduleRule{Kind: ScheduleStartup}, ScheduleRule{Kind: ScheduleInterval, IntervalTicks: &interval})
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_triggers SET anchor_at=anchor_at-interval '2 hours',
		next_fire_at=next_fire_at-interval '2 hours' WHERE id=$1`, definition.Triggers[1].ID); err != nil {
		t.Fatal(err)
	}
	// Sequences retain observations across a deliberately rolled-back startup
	// transaction. The gate fails only startup receipts, so an incorrect call
	// to DispatchDue after initialization failure remains independently visible.
	for _, statement := range []string{
		"CREATE SEQUENCE manager_initialization_attempts",
		"CREATE SEQUENCE manager_initialization_startup_micros",
		`CREATE FUNCTION manager_initialization_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		DECLARE attempt bigint; first_stamp bigint; incoming_stamp bigint;
		BEGIN
			IF (SELECT kind FROM task_triggers WHERE id=NEW.trigger_id)='startup' THEN
				attempt := nextval('manager_initialization_attempts');
				incoming_stamp := (extract(epoch FROM NEW.due_at)*1000000)::bigint;
				IF attempt=1 THEN
					PERFORM setval('manager_initialization_startup_micros',incoming_stamp);
				ELSE
					SELECT last_value INTO first_stamp FROM manager_initialization_startup_micros;
					IF first_stamp<>incoming_stamp THEN RAISE EXCEPTION 'startup identity changed between retries'; END IF;
				END IF;
				IF attempt<1000 THEN RAISE EXCEPTION 'injected startup initialization failure'; END IF;
			END IF;
			RETURN NEW;
		END $$`,
		"CREATE TRIGGER manager_initialization_gate BEFORE INSERT ON task_occurrences FOR EACH ROW EXECUTE FUNCTION manager_initialization_gate()",
	} {
		if _, err := f.pool.Exec(f.ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	manual, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: definition.ID, RequestID: "manual-during-scheduler-failure"})
	if err != nil {
		t.Fatal(err)
	}
	manager, _ := f.manager(t, true)
	managerWaitRun(t, f, manual.Run.ID, RunCompleted)
	managerWait(t, f.ctx, "repeated failed initialization", func(ctx context.Context) bool {
		var attempts int64
		if err := f.pool.QueryRow(ctx, "SELECT last_value FROM manager_initialization_attempts").Scan(&attempts); err != nil {
			t.Fatal(err)
		}
		return attempts >= 2
	})
	if manager.Available() || !f.catalog.Available() {
		t.Fatal("failed scheduler initialization reported ready or disabled otherwise healthy manual execution")
	}
	var runs, occurrences int64
	if err := f.pool.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM task_runs),(SELECT count(*) FROM task_occurrences)").Scan(&runs, &occurrences); err != nil || runs != 1 || occurrences != 0 {
		t.Fatal("old due work was dispatched before schedule initialization succeeded")
	}
	if _, err := f.pool.Exec(f.ctx, "SELECT setval('manager_initialization_attempts',999,true)"); err != nil {
		t.Fatal(err)
	}
	manager.Wake()
	var startup Run
	managerWait(t, f.ctx, "successful startup initialization retry", func(ctx context.Context) bool {
		page, err := f.store.ListRuns(ctx, definition.ID, Page{})
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range page.Items {
			if run.Source == "startup" && run.State == RunCompleted {
				startup = run
				return manager.Available()
			}
		}
		return false
	})
	var firstMicros int64
	if err := f.pool.QueryRow(f.ctx, "SELECT last_value FROM manager_initialization_startup_micros").Scan(&firstMicros); err != nil || startup.ScheduledFor == nil || startup.ScheduledFor.UnixMicro() != firstMicros {
		t.Fatal("initialization retries changed the manager's fixed startup event identity")
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs),
		(SELECT count(*) FROM task_occurrences WHERE trigger_id=$1 AND disposition='admitted')`, definition.Triggers[0].ID).Scan(&runs, &occurrences); err != nil || runs != 2 || occurrences != 1 {
		t.Fatal("initialization retry duplicated startup work or admitted historical timed occurrences")
	}
}
