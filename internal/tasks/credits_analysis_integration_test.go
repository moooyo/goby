//go:build linux

package tasks

import (
	"errors"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func newCreditsAnalysisTestFixture(t *testing.T) *analysisTestFixture {
	t.Helper()
	f := newAnalysisTestFixture(t, 0)
	entries := []ExecutorRegistration{}
	for _, entry := range f.store.executors.entries {
		entries = append(entries, entry)
	}
	credits := f.store.executors.entries[library.TaskIntroAnalysisKey]
	credits.Key, credits.Name = library.TaskCreditsAnalysisKey, "Credits fixture"
	credits.Executor = &analysisTestExecutor{started: make(chan Work, 8)}
	entries = append(entries, credits)
	registry, err := NewExecutorRegistry(entries...)
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
	definition, err := f.store.GetByKey(f.ctx, library.TaskCreditsAnalysisKey)
	if err != nil {
		t.Fatal(err)
	}
	f.definitions[definition.Key] = definition
	return f
}

func TestCreditsTaskDefaultsScopeAndLateEventsUseAnalysisAdmission(t *testing.T) {
	f := newCreditsAnalysisTestFixture(t)
	definition := f.definitions[library.TaskCreditsAnalysisKey]
	if len(definition.Triggers) != 2 || definition.Triggers[1].SystemEvent == nil || *definition.Triggers[1].SystemEvent != "CreditsAnalysisRequested" {
		t.Fatalf("credits defaults: %+v", definition)
	}
	if err := f.store.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := f.store.Get(f.ctx, definition.ID); err != nil || !reflect.DeepEqual(after, definition) {
		t.Fatalf("credits schedules changed on repeat reconcile: %v", err)
	}
	enableTestAutomaticLibraries(t, f, library.TaskCreditsAnalysisKey, "library-1")
	run := admitTestAutomaticAnalysis(t, f, library.TaskCreditsAnalysisKey, "system_event")
	if run.AnalysisInput == nil || !reflect.DeepEqual(run.AnalysisInput.LibraryIDs, []string{"library-1"}) || run.ActorKind != "system" {
		t.Fatalf("automatic credits scope/authority: %+v", run)
	}
	enableTestAutomaticLibraries(t, f, library.TaskCreditsAnalysisKey, "library-2")
	recordTestAnalysisRequest(t, f, library.TaskCreditsAnalysisKey)
	if _, err := f.store.DispatchSystemEvents(f.ctx, 10); !errors.Is(err, ErrActiveRunConflict) {
		t.Fatalf("active run consumed later credits scope: %v", err)
	}
	started, err := f.store.BeginRun(f.ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	child := f.children(t, started)[0]
	if _, err := f.store.claimExecution(f.ctx, started.ID, child.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.finishExecution(f.ctx, started.ID, child.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DispatchSystemEvents(f.ctx, 10); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.Get(f.ctx, definition.ID)
	if err != nil || after.CurrentRun == nil || after.CurrentRun.ID == run.ID || !reflect.DeepEqual(after.CurrentRun.AnalysisInput.LibraryIDs, []string{"library-1", "library-2"}) {
		t.Fatalf("later credits request lost its fresh cohort snapshot: %+v %v", after.CurrentRun, err)
	}
}

func TestCreditsTaskSharesAnalysisSlotAndScopesAuthorizationToEachWorker(t *testing.T) {
	f := newCreditsAnalysisTestFixture(t)
	intro := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	introChild := f.children(t, intro)[0]
	if _, err := f.store.claimExecution(f.ctx, intro.ID, introChild.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	credits := f.start(t, library.TaskCreditsAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1", "library-2"}})
	children := f.children(t, credits)
	if len(children) != 2 {
		t.Fatalf("credits child snapshot: %+v", children)
	}
	child := children[0]
	if _, err := f.store.claimExecution(f.ctx, credits.ID, child.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); !errors.Is(err, errAnalysisGroupBusy) {
		t.Fatalf("credits bypassed shared media slot: %v", err)
	}
	if err := f.store.finishExecution(f.ctx, intro.ID, introChild.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.claimExecution(f.ctx, credits.ID, child.ID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}
	work, err := f.store.executionWork(f.ctx, credits, child, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO analysis_publication_witness(value) VALUES('approved-credits-worker')`); err != nil {
			return err
		}
		return work.Fence(tx)
	}); err != nil {
		t.Fatalf("actor change invalidated the current credits worker's approval: %v", err)
	}
	f.completeWork(t, credits, child)
	if _, err := f.claimWork(t, credits, children[1]); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("next credits worker reused the previous worker's approval: %v", err)
	}
}

func TestCreditsTaskRecoveryRetainsFreshAutomaticIntentOnly(t *testing.T) {
	for _, reason := range []string{"crash", "administrator", "shutdown", "manual"} {
		t.Run(reason, func(t *testing.T) {
			f := newCreditsAnalysisTestFixture(t)
			enableTestAutomaticLibraries(t, f, library.TaskCreditsAnalysisKey, "library-1")
			var run Run
			if reason == "manual" {
				run = f.start(t, library.TaskCreditsAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
			} else {
				run = admitTestAutomaticAnalysis(t, f, library.TaskCreditsAnalysisKey, "system_event")
			}
			before := analysisRequestSequence(t, f, library.TaskCreditsAnalysisKey)
			if reason == "administrator" {
				if _, err := f.store.Stop(f.ctx, f.actor, run.ID); err != nil {
					t.Fatal(err)
				}
			}
			if reason == "shutdown" {
				if _, err := f.store.SystemStopRun(f.ctx, run.ID, "shutdown"); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.RecoverRuns(f.ctx); err != nil {
				t.Fatal(err)
			}
			if err := f.store.RecoverRuns(f.ctx); err != nil {
				t.Fatal(err)
			}
			want := before
			if reason == "crash" || reason == "shutdown" {
				want++
			}
			if after := analysisRequestSequence(t, f, library.TaskCreditsAnalysisKey); after != want {
				t.Fatalf("recovery intent=%d want=%d", after, want)
			}
		})
	}
}
