//go:build linux

package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

var analysisRotationTestKeys = []string{
	library.TaskIntroAnalysisKey,
	library.TaskPreviewGenerationKey,
	library.TaskCreditsAnalysisKey,
	library.TaskBackgroundPreviewGenerationKey,
	library.TaskAudioWaveformGenerationKey,
	library.TaskSubtitleTimelineGenerationKey,
}

func newAnalysisRotationFixture(t *testing.T, chunks int) *analysisTestFixture {
	t.Helper()
	f := newAnalysisTestFixture(t, chunks)
	registrations := make([]ExecutorRegistration, 0, len(analysisRotationTestKeys)+1)
	for _, key := range append([]string{CacheMaintainKey}, analysisRotationTestKeys...) {
		entry, exists := f.store.executors.lookup(key)
		if !exists {
			executor := &analysisTestExecutor{started: make(chan Work, 8)}
			f.executors[key] = executor
			entry = ExecutorRegistration{Key: key, Name: key, Category: "Analysis rotation test", Executor: executor}
			if isAnalysisTask(key) {
				entry.AnalysisAdmission = f.store.executors.entries[library.TaskIntroAnalysisKey].AnalysisAdmission
			}
		}
		f.executors[key].release = make(chan struct{})
		registrations = append(registrations, entry)
	}
	registry, err := NewExecutorRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = New(f.pool, f.owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range analysisRotationTestKeys {
		definition, err := f.store.GetByKey(f.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		f.definitions[key] = definition
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||
		'{"EnableBackgroundPreviewGeneration":true,"EnableAudioWaveformGeneration":true,"EnableSubtitleTimelineGeneration":true}'::jsonb`); err != nil {
		t.Fatal(err)
	}
	return f
}

func newAnalysisRotationManager(t *testing.T, f *analysisTestFixture) *Manager {
	t.Helper()
	ctx, cancel := context.WithCancel(f.ctx)
	manager := &Manager{
		store: f.store, scans: f.owner, ctx: ctx, wake: make(chan struct{}, 1),
		options:      ManagerOptions{BatchSize: MaxPageLimit, MaxConcurrent: func() int { return 2 }},
		childOffsets: map[string]int{}, runtimeDeadlines: map[string]time.Time{}, executions: map[string]*workerExecution{},
	}
	t.Cleanup(func() {
		cancel()
		manager.drainExecutions()
	})
	return manager
}

func finishAnalysisRotationWork(t *testing.T, f *analysisTestFixture, manager *Manager, work Work) {
	t.Helper()
	execution := manager.executions[work.ChildID]
	if execution == nil {
		t.Fatal("started work has no owned execution")
	}
	select {
	case f.executors[work.TaskKey].release <- struct{}{}:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not accept completion")
	}
	analysisWaitDone(t, execution.done)
	if execution.err != nil {
		t.Fatalf("execution failed: %v", execution.err)
	}
	if err := manager.reapExecutions(f.ctx, false); err != nil {
		t.Fatal(err)
	}
}

type analysisRotationQueryTracer struct{ elections atomic.Int64 }

func (tracer *analysisRotationQueryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "SELECT r.id FROM task_runs r") && strings.Contains(data.SQL, "c.state='waiting'") {
		tracer.elections.Add(1)
	}
	return ctx
}

func (*analysisRotationQueryTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func traceAnalysisRotationQueries(t *testing.T, f *analysisTestFixture) *analysisRotationQueryTracer {
	t.Helper()
	tracer := &analysisRotationQueryTracer{}
	config := f.pool.Config()
	config.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f.store.pool = pool
	return tracer
}

func TestAnalysisRotationDefersLargeWaitingRunWithoutSpendingThePassBudget(t *testing.T) {
	f := newAnalysisRotationFixture(t, MaxPageLimit+5)
	tracer := traceAnalysisRotationQueries(t, f)
	manager := newAnalysisRotationManager(t, f)
	selection := &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	first := f.start(t, library.TaskIntroAnalysisKey, selection)
	child := f.children(t, first)[0]
	if disposition, err := manager.reconcileExecution(f.ctx, first, child, false); err != nil || disposition != executionContinue {
		t.Fatalf("initial dispatch: disposition=%v error=%v", disposition, err)
	}
	finishAnalysisRotationWork(t, f, manager, analysisReceiveWork(t, f.executors[first.TaskKey]))
	ordinary := f.start(t, CacheMaintainKey, nil)
	second := f.start(t, library.TaskPreviewGenerationKey, selection)
	tracer.elections.Store(0)

	if _, err := manager.reconcile(f.ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(manager.executions) != 2 {
		t.Fatalf("the waiting backlog consumed the pass before other workers started: executions=%d", len(manager.executions))
	}
	if work := analysisReceiveWork(t, f.executors[CacheMaintainKey]); work.RunID != ordinary.ID {
		t.Fatal("ordinary work lost its independent execution slot")
	}
	if work := analysisReceiveWork(t, f.executors[second.TaskKey]); work.RunID != second.ID {
		t.Fatal("the next analysis run did not receive a worker in the same pass")
	}
	if got := tracer.elections.Load(); got != 2 {
		t.Fatalf("waiting children repeated the run election: queries=%d want=2", got)
	}
	if got := manager.childOffsets[first.ID]; got != 1 {
		t.Fatalf("skipped waiting children advanced the cursor: offset=%d want=1", got)
	}
	var waiting, claimed int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE state='waiting'),
		count(*) FILTER(WHERE state IN ('queued','running')) FROM task_run_children WHERE run_id=$1`, first.ID).Scan(&waiting, &claimed); err != nil {
		t.Fatal(err)
	}
	if waiting != MaxPageLimit+4 || claimed != 0 {
		t.Fatalf("deferral changed the waiting backlog: waiting=%d claimed=%d", waiting, claimed)
	}
}

func TestAnalysisRotationDeferralStillChecksLaterRunningChildren(t *testing.T) {
	f := newAnalysisRotationFixture(t, 4)
	tracer := traceAnalysisRotationQueries(t, f)
	manager := newAnalysisRotationManager(t, f)
	selection := &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	first := f.start(t, library.TaskIntroAnalysisKey, selection)
	children := f.children(t, first)
	if disposition, err := manager.reconcileExecution(f.ctx, first, children[0], false); err != nil || disposition != executionContinue {
		t.Fatalf("initial dispatch: disposition=%v error=%v", disposition, err)
	}
	finishAnalysisRotationWork(t, f, manager, analysisReceiveWork(t, f.executors[first.TaskKey]))
	f.start(t, library.TaskPreviewGenerationKey, selection)
	// Persist a real claim without registering a worker in this coordinator.
	// Two waiting entries precede it in the active page.
	if _, err := f.claimWork(t, first, children[3]); err != nil {
		t.Fatal(err)
	}
	tracer.elections.Store(0)

	if _, err := manager.reconcile(f.ctx, false); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("deferral hid an unowned running child: %v", err)
	}
	if got := tracer.elections.Load(); got != 1 {
		t.Fatalf("the deferred waiting entry repeated the election: queries=%d want=1", got)
	}
	if got := manager.childOffsets[first.ID]; got != 3 {
		t.Fatalf("the later running child was not visited: offset=%d want=3", got)
	}
	if len(manager.executions) != 0 || manager.lastAnalysisTaskKey != first.TaskKey {
		t.Fatal("an unowned claim changed the coordinator's execution ownership or rotation")
	}
}

func TestAnalysisRotationFairnessSurvivesBacklogsAndReplacementRuns(t *testing.T) {
	for _, keyCount := range []int{3, 6} {
		t.Run(fmt.Sprintf("keys=%d", keyCount), func(t *testing.T) {
			f := newAnalysisRotationFixture(t, 1)
			manager := newAnalysisRotationManager(t, f)
			keys := analysisRotationTestKeys[:keyCount]
			runs := make(map[string]Run, keyCount)
			completed := make(map[string]int, keyCount)
			start := func(key string) Run {
				var selection *library.AnalysisSelection
				if isAnalysisTask(key) {
					selection = &library.AnalysisSelection{LibraryIDs: []string{"library-1", "library-2"}}
				}
				admission, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definitions[key].ID, AnalysisInput: selection})
				if err != nil {
					t.Fatal(err)
				}
				run := admission.Run
				if run.State != RunPending {
					t.Fatalf("new %s run has state %s, want pending", key, run.State)
				}
				if run.TotalChildren != 2 {
					t.Fatalf("rotation fixture has %d children for %s, want 2", run.TotalChildren, key)
				}
				return run
			}
			// Creation order must not determine the first or subsequent turns.
			for index := len(keys) - 1; index >= 0; index-- {
				runs[keys[index]] = start(keys[index])
			}
			for turn := 0; turn < 4*len(keys); turn++ {
				key := keys[turn%len(keys)]
				if _, err := manager.reconcile(f.ctx, false); err != nil {
					t.Fatalf("turn %d: %v", turn, err)
				}
				if len(manager.executions) != 1 {
					t.Fatalf("turn %d owns %d analysis workers, want 1", turn, len(manager.executions))
				}
				for _, execution := range manager.executions {
					if execution.runID != runs[key].ID {
						t.Fatalf("turn %d selected run %s, want %s for %s", turn, execution.runID, runs[key].ID, key)
					}
				}
				if manager.lastAnalysisTaskKey != key {
					t.Fatalf("turn %d did not retain the successful task key", turn)
				}
				finishAnalysisRotationWork(t, f, manager, analysisReceiveWork(t, f.executors[key]))
				current, err := f.store.GetRun(f.ctx, runs[key].ID)
				if err != nil {
					t.Fatal(err)
				}
				if !current.State.Active() {
					if current.State != RunCompleted {
						t.Fatalf("turn %d ended with state %s", turn, current.State)
					}
					completed[key]++
					runs[key] = start(key)
				}
			}
			for _, key := range keys {
				if completed[key] != 2 {
					t.Fatalf("%s completed %d runs, want 2 despite continuous replacement", key, completed[key])
				}
			}
		})
	}
}

func TestAnalysisRotationSkipsAbsentKeysAndWrapsToTheOnlyRunnableKey(t *testing.T) {
	f := newAnalysisRotationFixture(t, 1)
	selection := &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	preview := f.start(t, library.TaskPreviewGenerationKey, selection)
	for _, previous := range []string{"", library.TaskPreviewGenerationKey, library.TaskCreditsAnalysisKey, library.TaskSubtitleTimelineGenerationKey, "unknown.task"} {
		if id, err := f.store.nextAnalysisRun(f.ctx, previous); err != nil || id != preview.ID {
			t.Fatalf("single runnable key after %q: run=%s error=%v", previous, id, err)
		}
	}
	intro := f.start(t, library.TaskIntroAnalysisKey, selection)
	for _, test := range []struct {
		previous string
		want     string
	}{
		{"", intro.ID},
		{library.TaskIntroAnalysisKey, preview.ID},
		{library.TaskPreviewGenerationKey, intro.ID},
		{library.TaskCreditsAnalysisKey, intro.ID},
		{library.TaskSubtitleTimelineGenerationKey, intro.ID},
	} {
		if id, err := f.store.nextAnalysisRun(f.ctx, test.previous); err != nil || id != test.want {
			t.Fatalf("sparse rotation after %q: run=%s want=%s error=%v", test.previous, id, test.want, err)
		}
	}
	if _, err := f.store.Stop(f.ctx, f.actor, intro.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := f.store.nextAnalysisRun(f.ctx, intro.TaskKey); err != nil || id != preview.ID {
		t.Fatalf("cancelled cursor run blocked the next key: run=%s error=%v", id, err)
	}
	if _, err := f.store.Stop(f.ctx, f.actor, preview.ID); err != nil {
		t.Fatal(err)
	}
	if id, err := f.store.nextAnalysisRun(f.ctx, library.TaskPreviewGenerationKey); err != nil || id != "" {
		t.Fatalf("terminal runs remained eligible: run=%s error=%v", id, err)
	}
}

func TestAnalysisRotationAuthorizationFailureStillConsumesItsClaim(t *testing.T) {
	f := newAnalysisRotationFixture(t, 2)
	manager := newAnalysisRotationManager(t, f)
	selection := &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	first := f.start(t, library.TaskIntroAnalysisKey, selection)
	second := f.start(t, library.TaskPreviewGenerationKey, selection)
	f.invalidateActor(t, "revoked")
	for _, run := range []Run{first, second} {
		if _, err := manager.reconcile(f.ctx, false); err != nil {
			t.Fatal(err)
		}
		if len(manager.executions) != 1 || manager.lastAnalysisTaskKey != run.TaskKey {
			t.Fatal("execution authorization failure pinned the rotation to its previous key")
		}
		for _, execution := range manager.executions {
			if execution.runID != run.ID {
				t.Fatalf("claimed run=%s want=%s", execution.runID, run.ID)
			}
			analysisWaitDone(t, execution.done)
			if !errors.Is(execution.err, identity.ErrUnauthorized) {
				t.Fatalf("revoked actor execution result=%v, want unauthorized", execution.err)
			}
		}
		select {
		case <-f.executors[run.TaskKey].started:
			t.Fatal("an unauthorized executor ran")
		default:
		}
		if err := manager.reapExecutions(f.ctx, false); err != nil {
			t.Fatal(err)
		}
		current, err := f.store.GetRun(f.ctx, run.ID)
		if err != nil || current.FailedChildren != 1 || current.TotalChildren != 2 {
			t.Fatalf("authorization failure did not retain the remaining backlog: run=%+v error=%v", current, err)
		}
	}
}

func TestAnalysisRotationBusyClaimDoesNotAdvanceTheSuccessfulTaskKey(t *testing.T) {
	f := newAnalysisRotationFixture(t, 1)
	manager := newAnalysisRotationManager(t, f)
	selection := &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	first := f.start(t, library.TaskIntroAnalysisKey, selection)
	firstChild := f.children(t, first)[0]
	if _, err := f.claimWork(t, first, firstChild); err != nil {
		t.Fatal(err)
	}
	second := f.start(t, library.TaskPreviewGenerationKey, selection)
	secondChild := f.children(t, second)[0]
	manager.lastAnalysisTaskKey = first.TaskKey
	if disposition, err := manager.reconcileExecution(f.ctx, second, secondChild, false); err != nil || disposition != executionContinue {
		t.Fatalf("busy claim: disposition=%v error=%v", disposition, err)
	}
	if len(manager.executions) != 0 || manager.lastAnalysisTaskKey != first.TaskKey {
		t.Fatal("a failed claim advanced the rotation or registered a worker")
	}
	ordinary := f.start(t, CacheMaintainKey, nil)
	if disposition, err := manager.reconcileExecution(f.ctx, ordinary, f.children(t, ordinary)[0], false); err != nil || disposition != executionContinue {
		t.Fatalf("ordinary dispatch while analysis is busy: disposition=%v error=%v", disposition, err)
	}
	ordinaryWork := analysisReceiveWork(t, f.executors[CacheMaintainKey])
	if manager.lastAnalysisTaskKey != first.TaskKey {
		t.Fatal("ordinary execution advanced the analysis rotation")
	}
	f.completeWork(t, first, firstChild)
	manager.options.MaxConcurrent = func() int { return 1 }
	if disposition, err := manager.reconcileExecution(f.ctx, second, secondChild, false); err != nil || disposition != executionQueueFull {
		t.Fatalf("full executor queue: disposition=%v error=%v", disposition, err)
	}
	if len(manager.executions) != 1 || manager.lastAnalysisTaskKey != first.TaskKey {
		t.Fatal("a full executor queue advanced the analysis rotation")
	}
	finishAnalysisRotationWork(t, f, manager, ordinaryWork)
	if disposition, err := manager.reconcileExecution(f.ctx, second, secondChild, false); err != nil || disposition != executionContinue {
		t.Fatalf("retry after the group was released: disposition=%v error=%v", disposition, err)
	}
	work := analysisReceiveWork(t, f.executors[second.TaskKey])
	if work.RunID != second.ID || manager.lastAnalysisTaskKey != second.TaskKey {
		t.Fatal("successful claim did not advance the rotation to its task key")
	}
	finishAnalysisRotationWork(t, f, manager, work)
}
