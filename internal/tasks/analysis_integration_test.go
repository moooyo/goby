//go:build linux

package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

type analysisTestExecutor struct {
	started chan Work
	release chan struct{}
}

func (*analysisTestExecutor) Available() bool { return true }
func (executor *analysisTestExecutor) Execute(ctx context.Context, work Work, progress func(Progress) error) error {
	if executor.started != nil {
		select {
		case executor.started <- work:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if executor.release != nil {
		select {
		case <-executor.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return progress(Progress{Processed: 1})
}

type analysisTestFixture struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	owner       *library.Store
	store       *Store
	actor       Actor
	definitions map[string]Definition
	executors   map[string]*analysisTestExecutor
	fingerprint string
}

func newAnalysisTestFixture(t *testing.T, chunks int) *analysisTestFixture {
	t.Helper()
	ctx, pool, owner, _, actor, _ := taskRepository(t, 2)
	f := &analysisTestFixture{ctx: ctx, pool: pool, owner: owner, actor: actor, definitions: map[string]Definition{}, executors: map[string]*analysisTestExecutor{}, fingerprint: strings.Repeat("a", 64)}
	if _, err := pool.Exec(ctx, `CREATE TABLE analysis_binding_witness(run_id text PRIMARY KEY REFERENCES task_runs(id),fingerprint text NOT NULL,selection jsonb NOT NULL);
		CREATE TABLE analysis_publication_witness(value text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	registrations := []ExecutorRegistration{}
	for _, key := range []string{library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey, CacheMaintainKey} {
		executor := &analysisTestExecutor{started: make(chan Work, 8)}
		f.executors[key] = executor
		registration := ExecutorRegistration{Key: key, Name: key, Category: "Analysis test", Executor: executor, Global: key == CacheMaintainKey}
		if isAnalysisTask(key) {
			registration.AnalysisAdmission = func(tx library.OwnedTx, request AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
				fingerprint := f.fingerprint
				selection := *cloneAnalysisSelection(&request.Selection)
				binding := AnalysisAdmissionBinding{ConfigurationFingerprint: fingerprint, Bind: func(tx library.OwnedTx, runID string) error {
					encoded, _ := json.Marshal(selection)
					_, err := tx.Exec(`INSERT INTO analysis_binding_witness(run_id,fingerprint,selection) VALUES($1,$2,$3)`, runID, fingerprint, encoded)
					return err
				}}
				if chunks > 0 {
					binding.SnapshotChildren = func(tx library.OwnedTx, runID string) (int64, error) {
						var present bool
						if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM analysis_binding_witness WHERE run_id=$1)`, runID).Scan(&present); err != nil {
							return 0, err
						}
						if !present {
							return 0, errors.New("profile was not bound before child snapshot")
						}
						ids := selection.LibraryIDs
						if len(ids) == 0 {
							ids = []string{"library-1", "library-2"}
						}
						ordinal := 0
						for _, libraryID := range ids {
							for index := 0; index < chunks; index++ {
								scope := fmt.Sprintf("cohort-%d", index)
								if _, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key)
								VALUES(md5($1||':'||$2||':'||$3),$1,$2,$2,$4,$3)`, runID, libraryID, scope, ordinal); err != nil {
									return 0, err
								}
								ordinal++
							}
						}
						return int64(ordinal), nil
					}
				}
				return binding, nil
			}
		}
		registrations = append(registrations, registration)
	}
	registry, err := NewExecutorRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	f.store, err = New(pool, owner, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{library.TaskIntroAnalysisKey, library.TaskPreviewGenerationKey, CacheMaintainKey, LibraryScanKey} {
		definition, err := f.store.GetByKey(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		f.definitions[key] = definition
	}
	return f
}

func (f *analysisTestFixture) start(t *testing.T, key string, selection *library.AnalysisSelection) Run {
	t.Helper()
	admission, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definitions[key].ID, AnalysisInput: selection})
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.store.BeginRun(f.ctx, admission.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *analysisTestFixture) children(t *testing.T, run Run) []Child {
	t.Helper()
	page, err := f.store.ListChildren(f.ctx, run.ID, Page{})
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func (f *analysisTestFixture) claim(t *testing.T) (Run, Child, Work) {
	t.Helper()
	run := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
	children := f.children(t, run)
	if len(children) != 1 {
		t.Fatal("missing analysis child")
	}
	child := children[0]
	token, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.claimExecution(f.ctx, run.ID, child.ID, token); err != nil {
		t.Fatal(err)
	}
	work := executionWork(f.ctx, run, child, token)
	if err = f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
		t.Fatalf("initial publication fence: %v", err)
	}
	return run, child, work
}

func TestAnalysisAdmissionBindsSelectionAndConfigurationWithoutChangingReceiptReplay(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	request := StartRequest{TaskID: f.definitions[library.TaskIntroAnalysisKey].ID, RequestID: "original", AnalysisInput: &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}}
	first, err := f.store.Start(f.ctx, f.actor, request)
	if err != nil || !first.Admitted {
		t.Fatalf("admit: %+v %v", first, err)
	}
	first.Run.AnalysisInput.LibraryIDs[0] = "changed-response"
	request.AnalysisInput.LibraryIDs[0] = "changed-request"
	stored, err := f.store.GetRun(f.ctx, first.Run.ID)
	if err != nil || !reflect.DeepEqual(stored.AnalysisInput.LibraryIDs, []string{"library-1"}) {
		t.Fatal("request/response mutation changed the admitted selection")
	}
	request.AnalysisInput = &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	request.RequestID = "alias"
	alias, err := f.store.Start(f.ctx, f.actor, request)
	if err != nil || alias.Admitted || alias.Run.ID != first.Run.ID {
		t.Fatalf("equal active selection did not coalesce: %v", err)
	}
	request.RequestID = "different-scope"
	request.AnalysisInput = &library.AnalysisSelection{LibraryIDs: []string{"library-2"}}
	if _, err := f.store.Start(f.ctx, f.actor, request); !errors.Is(err, ErrActiveRunConflict) {
		t.Fatalf("different active scope: %v", err)
	}
	request.RequestID = "original"
	if _, err := f.store.Start(f.ctx, f.actor, request); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("changed receipt input: %v", err)
	}
	f.fingerprint = strings.Repeat("b", 64)
	request.AnalysisInput = &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	replayed, err := f.store.Start(f.ctx, f.actor, request)
	if err != nil || replayed.Run.AnalysisConfigFingerprint != strings.Repeat("a", 64) {
		t.Fatal("a retry replaced its original profile with today's configuration")
	}
	request.RequestID = "different-config"
	if _, err := f.store.Start(f.ctx, f.actor, request); !errors.Is(err, ErrActiveRunConflict) {
		t.Fatalf("different active configuration: %v", err)
	}
	var bindings, requests int
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM analysis_binding_witness),(SELECT count(*) FROM task_run_requests WHERE task_id=$1)`, request.TaskID).Scan(&bindings, &requests); err != nil || bindings != 1 || requests != 2 {
		t.Fatalf("replay/conflict rebound profile or stored a rejected receipt: %d %d %v", bindings, requests, err)
	}
	legacy := StartRequest{TaskID: f.definitions[LibraryScanKey].ID, AnalysisInput: &library.AnalysisSelection{}}
	if _, err := f.store.Start(f.ctx, f.actor, legacy); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("legacy executor accepted analysis input")
	}
}

func TestAnalysisSelectionChecksActualLibraryAndLeafOwnership(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES('analysis-root','library-1','/not-opened/analysis','/not-opened','analysis');
		INSERT INTO items(id,library_id,root_id,name,sort_name,type,is_folder,relative_path) VALUES('analysis-leaf','library-1','analysis-root','Leaf','leaf','Movie',false,'leaf.mp4'),('analysis-folder','library-1','analysis-root','Folder','folder','Folder',true,'folder');
		INSERT INTO items(id,library_id,name,sort_name,type) VALUES('analysis-missing','library-1','Expected','expected','Episode');
		INSERT INTO libraries(id,name,collection_type) VALUES('analysis-music','Music','music')`); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []*library.AnalysisSelection{
		{LibraryIDs: []string{"unknown"}}, {LibraryIDs: []string{"analysis-music"}}, {ItemIDs: []string{"unknown"}},
		{ItemIDs: []string{"analysis-folder"}}, {ItemIDs: []string{"analysis-missing"}},
		{LibraryIDs: []string{"library-2"}, ItemIDs: []string{"analysis-leaf"}},
	} {
		if _, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definitions[library.TaskPreviewGenerationKey].ID, AnalysisInput: selection}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid physical selection accepted: %+v %v", selection, err)
		}
	}
	admission, err := f.store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definitions[library.TaskPreviewGenerationKey].ID, AnalysisInput: &library.AnalysisSelection{ItemIDs: []string{"analysis-leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	children := f.children(t, admission.Run)
	if len(children) != 1 || children[0].LibraryID != "library-1" || children[0].AnalysisScopeKey == "" {
		t.Fatalf("item selection included unrelated library children: %+v", children)
	}
}

func TestAnalysisBindingFailureRollsBackRunProfileAndChildSnapshots(t *testing.T) {
	for _, failure := range []string{"foreign-library", "wrong-count"} {
		t.Run(failure, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			entry := f.store.executors.entries[library.TaskIntroAnalysisKey]
			prepare := entry.AnalysisAdmission
			entry.AnalysisAdmission = func(tx library.OwnedTx, request AnalysisAdmissionRequest) (AnalysisAdmissionBinding, error) {
				binding, err := prepare(tx, request)
				if err != nil {
					return binding, err
				}
				binding.SnapshotChildren = func(tx library.OwnedTx, runID string) (int64, error) {
					if failure == "wrong-count" {
						return 1, nil
					}
					_, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal,analysis_scope_key)
					VALUES(md5($1||':bad'),$1,'library-2','Foreign selection',0,'wrong-library')`, runID)
					return 1, err
				}
				return binding, nil
			}
			registry, err := NewExecutorRegistry(entry)
			if err != nil {
				t.Fatal(err)
			}
			store, err := New(f.pool, f.owner, registry)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Start(f.ctx, f.actor, StartRequest{TaskID: f.definitions[library.TaskIntroAnalysisKey].ID, AnalysisInput: &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}})
			if !errors.Is(err, ErrInconsistent) {
				t.Fatalf("bad snapshot binding: %v", err)
			}
			var runs, profiles, children int
			if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs),(SELECT count(*) FROM analysis_binding_witness),(SELECT count(*) FROM task_run_children)`).Scan(&runs, &profiles, &children); err != nil || runs != 0 || profiles != 0 || children != 0 {
				t.Fatal("failed admission committed partial profile or task state")
			}
		})
	}
}

func TestAnalysisSchedulerUsesExplicitSystemAuthorityAndRecoveryFencesOldWork(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	definition := f.definitions[library.TaskIntroAnalysisKey]
	if _, err := f.store.ReplaceTriggers(f.ctx, f.actor, ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: "UTC", Triggers: []ScheduleRule{{Kind: ScheduleStartup}}}); err != nil {
		t.Fatal(err)
	}
	now, err := f.store.ScheduleClock(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.InitializeSchedules(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	current, err := f.store.Get(f.ctx, definition.ID)
	if err != nil || current.CurrentRun == nil {
		t.Fatalf("scheduled analysis absent: %v", err)
	}
	run, err := f.store.BeginRun(f.ctx, current.CurrentRun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.ActorKind != "system" || run.Source != "startup" || run.ActorUserID != "" || run.ActorSessionID != "" {
		t.Fatal("scheduled work inherited a manual identity")
	}
	child := f.children(t, run)[0]
	token, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.claimExecution(f.ctx, run.ID, child.ID, token); err != nil {
		t.Fatal(err)
	}
	work := executionWork(f.ctx, run, child, token)
	if _, err = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if err = f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
		t.Fatalf("explicit system source depended on the former schedule editor: %v", err)
	}
	if err = f.store.RecoverRuns(f.ctx); err != nil {
		t.Fatal(err)
	}
	if recovered, err := f.store.BeginRun(f.ctx, run.ID); err != nil || recovered.State != RunInterrupted {
		t.Fatal("startup recovery resumed abandoned analysis")
	}
	if err = f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("recovered old token published: %v", err)
	}
}

func TestAnalysisPublicationFenceRejectsCurrentAuthorityAndExecutionChanges(t *testing.T) {
	for _, scenario := range []string{"revoked", "disabled", "demoted", "expired", "stopped", "deadline", "token", "library", "scope", "recovered"} {
		t.Run(scenario, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			run, child, work := f.claim(t)
			var err error
			switch scenario {
			case "revoked":
				_, err = f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.actor.Principal.SessionID)
			case "disabled":
				_, err = f.pool.Exec(f.ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, f.actor.Principal.User.ID)
			case "demoted":
				_, err = f.pool.Exec(f.ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, f.actor.Principal.User.ID)
			case "expired":
				_, err = f.pool.Exec(f.ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',expires_at=clock_timestamp()-interval '1 hour' WHERE id=$1`, f.actor.Principal.SessionID)
			case "stopped":
				_, err = f.store.Stop(f.ctx, f.actor, run.ID)
			case "deadline":
				_, err = f.pool.Exec(f.ctx, `UPDATE task_runs SET deadline_at=clock_timestamp()-interval '1 second' WHERE id=$1`, run.ID)
			case "token":
				_, err = f.pool.Exec(f.ctx, `UPDATE task_run_children SET executor_token=repeat('b',32) WHERE id=$1`, child.ID)
			case "library":
				_, err = f.pool.Exec(f.ctx, `UPDATE task_run_children SET library_id='library-2' WHERE id=$1`, child.ID)
			case "scope":
				_, err = f.pool.Exec(f.ctx, `UPDATE task_run_children SET analysis_scope_key='another-chunk' WHERE id=$1`, child.ID)
			case "recovered":
				err = f.store.RecoverRuns(f.ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) })
			if err == nil {
				t.Fatal("stale worker retained publication authority")
			}
			if scenario == "revoked" || scenario == "disabled" || scenario == "demoted" || scenario == "expired" {
				if !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("authority denial: %v", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("execution denial: %v", err)
			}
		})
	}
}

func TestAnalysisPublicationFenceRollsBackLateInvalidationAndCannotBeRedirected(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	run, _, work := f.claim(t)
	changed := work
	changed.RunID = "other-run"
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return changed.Fence(tx) }); !errors.Is(err, ErrInconsistent) {
		t.Fatal("exported work fields redirected the sealed capability")
	}
	changed = work
	changed.AnalysisInput = cloneAnalysisSelection(work.AnalysisInput)
	changed.AnalysisInput.LibraryIDs[0] = "library-2"
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return changed.Fence(tx) }); !errors.Is(err, ErrInconsistent) {
		t.Fatal("selection mutation bypassed the sealed scope")
	}
	err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO analysis_publication_witness(value) VALUES('must-rollback')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE task_runs SET deadline_at=clock_timestamp()-interval '1 second' WHERE id=$1`, run.ID); err != nil {
			return err
		}
		return work.Fence(tx)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("late deadline fence: %v", err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_publication_witness`).Scan(&count); err != nil || count != 0 {
		t.Fatal("late fence denial committed publication")
	}
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); err != nil {
		t.Fatal("rolled-back deadline damaged the original running claim")
	}
}

func TestAnalysisApplicationFenceRetainsOriginalKeyAndClientBinding(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	identities := identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "analysis-key-vault")))
	key, err := identities.CreateApplicationKey(f.ctx, f.actor.Principal, "Analysis application", "", identity.Client{Name: "Analysis", DeviceID: "analysis-server", Device: "Server", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := identities.ResolveEmby(f.ctx, key.Token)
	if err != nil {
		t.Fatal(err)
	}
	f.actor = Actor{Principal: principal, Audience: identity.AdministratorEmby}
	run, _, work := f.claim(t)
	var keyID int64
	var client string
	if err := f.pool.QueryRow(f.ctx, `SELECT actor_application_key_id,actor_client_session_id FROM task_runs WHERE id=$1`, run.ID).Scan(&keyID, &client); err != nil || keyID != principal.ApplicationKeyID || client != principal.ClientSessionID {
		t.Fatal("application admission lost its exact identity")
	}
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM application_key_clients WHERE id=$1`, principal.ClientSessionID); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) }); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("removed application client retained publication: %v", err)
	}
	var raw string
	if err := f.pool.QueryRow(f.ctx, `SELECT to_jsonb(r)::text FROM task_runs r WHERE id=$1`, run.ID).Scan(&raw); err != nil || strings.Contains(raw, key.Token) {
		t.Fatal("task authority persisted an original bearer token")
	}
}

func TestAnalysisFinalFenceRejectsCredentialExpiryAfterPublicationWrites(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	_, _, work := f.claim(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, f.actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	entered := false
	err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		if err := work.Fence(tx); err != nil {
			return err
		}
		entered = true
		if _, err := tx.Exec(`INSERT INTO analysis_publication_witness(value) VALUES('expired-publication')`); err != nil {
			return err
		}
		if _, err := tx.Exec(`SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))+0.05) FROM sessions WHERE id=$1`, f.actor.Principal.SessionID); err != nil {
			return err
		}
		return work.Fence(tx)
	})
	if !entered || !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("late credential expiry was not independently fenced: entered=%t %v", entered, err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_publication_witness`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expired credential committed publication")
	}
}

func TestAnalysisPublicationContextOnlyNarrowsSealedAuthority(t *testing.T) {
	for _, cancelOriginal := range []bool{false, true} {
		t.Run(fmt.Sprintf("original=%t", cancelOriginal), func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			run := f.start(t, library.TaskIntroAnalysisKey, &library.AnalysisSelection{LibraryIDs: []string{"library-1"}})
			child := f.children(t, run)[0]
			token, err := randomID()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.claimExecution(f.ctx, run.ID, child.ID, token); err != nil {
				t.Fatal(err)
			}
			original, stopOriginal := context.WithCancel(f.ctx)
			defer stopOriginal()
			limited, stopLimited := context.WithCancel(f.ctx)
			defer stopLimited()
			base := executionWork(original, run, child, token)
			narrowed := base.WithContext(limited).WithContext(context.Background())
			err = f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
				if err := narrowed.Fence(tx); err != nil {
					return err
				}
				if _, err := tx.Exec(`INSERT INTO analysis_publication_witness(value) VALUES('narrowed')`); err != nil {
					return err
				}
				if cancelOriginal {
					stopOriginal()
				} else {
					stopLimited()
				}
				return narrowed.Fence(tx)
			})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("a later context widened cancelled work: %v", err)
			}
			var count int
			if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_publication_witness`).Scan(&count); err != nil || count != 0 {
				t.Fatal("context cancellation committed publication")
			}
			if !cancelOriginal {
				if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return base.Fence(tx) }); err != nil {
					t.Fatal("narrowing cancelled the original work rather than its returned capability")
				}
			}
			if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return base.WithContext(nil).Fence(tx) }); !errors.Is(err, context.Canceled) {
				t.Fatal("nil context granted publication")
			}
		})
	}
}

func TestAnalysisPublicationContextIsRecheckedAfterTheSealedDatabaseFence(t *testing.T) {
	f := newAnalysisTestFixture(t, 0)
	_, _, work := f.claim(t)
	limited, cancel := context.WithCancel(f.ctx)
	defer cancel()
	narrowed := work.WithContext(limited)
	sealed := narrowed.fence
	// Publish cancellation exactly after the real SQL fence has returned. This
	// deterministic boundary proves the outer capability checks its new context
	// after the database authority check, not only before it.
	narrowed.fence = func(tx library.OwnedTx, presented Work) error {
		if err := sealed(tx, presented); err != nil {
			return err
		}
		cancel()
		return nil
	}
	if err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return narrowed.Fence(tx) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-fence context cancellation was ignored: %v", err)
	}
}

func TestAnalysisFenceRetainsAuthenticatedPeerForCurrentRemotePolicy(t *testing.T) {
	for _, peer := range []string{"127.0.0.1", "198.51.100.8"} {
		t.Run(peer, func(t *testing.T) {
			f := newAnalysisTestFixture(t, 0)
			f.actor = taskCompatibilityActor(t, f.ctx, f.pool, f.actor)
			f.actor.Principal.PeerIP = peer
			run, _, work := f.claim(t)
			var saved string
			if err := f.pool.QueryRow(f.ctx, `SELECT actor_peer_ip FROM task_runs WHERE id=$1`, run.ID).Scan(&saved); err != nil || saved != peer {
				t.Fatal("task admission did not retain the authenticated transport peer")
			}
			if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=policy||'{"EnableRemoteAccess":false}'::jsonb WHERE id=$1`, f.actor.Principal.User.ID); err != nil {
				t.Fatal(err)
			}
			err := f.owner.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error { return work.Fence(tx) })
			if peer == "127.0.0.1" && err != nil || peer != "127.0.0.1" && !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("publication did not use current policy with its original peer: %v", err)
			}
		})
	}
}

func analysisReceiveWork(t *testing.T, executor *analysisTestExecutor) Work {
	t.Helper()
	select {
	case work := <-executor.started:
		return work
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not start")
		return Work{}
	}
}
func analysisWaitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executor did not return ownership")
	}
}

func TestAnalysisGroupAlternatesBoundedRunsWithoutOccupyingOtherProviderSlots(t *testing.T) {
	f := newAnalysisTestFixture(t, 2)
	intro, preview := f.executors[library.TaskIntroAnalysisKey], f.executors[library.TaskPreviewGenerationKey]
	intro.release, preview.release = make(chan struct{}), make(chan struct{})
	selection := &library.AnalysisSelection{LibraryIDs: []string{"library-1"}}
	first := f.start(t, library.TaskIntroAnalysisKey, selection)
	second := f.start(t, library.TaskPreviewGenerationKey, selection)
	ordinary := f.start(t, CacheMaintainKey, nil)
	a, b, c := f.children(t, first), f.children(t, second), f.children(t, ordinary)
	if len(a) != 2 || len(b) != 2 || len(c) != 1 {
		t.Fatal("bounded child snapshot did not retain two scopes in one library")
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	manager := &Manager{store: f.store, ctx: ctx, wake: make(chan struct{}, 1), executions: map[string]*workerExecution{}, runtimeDeadlines: map[string]time.Time{}, options: ManagerOptions{MaxConcurrent: func() int { return 2 }}}
	t.Cleanup(func() { manager.drainExecutions() })
	dispatch := func(run Run, child Child) {
		t.Helper()
		if full, err := manager.reconcileExecution(f.ctx, run, child, false); err != nil || full {
			t.Fatalf("dispatch: full=%t %v", full, err)
		}
	}
	dispatch(first, a[0])
	analysisReceiveWork(t, intro)
	dispatch(second, b[0])
	dispatch(first, a[1])
	if len(manager.executions) != 1 {
		t.Fatal("waiting analysis children occupied execution slots")
	}
	dispatch(ordinary, c[0])
	analysisReceiveWork(t, f.executors[CacheMaintainKey])
	analysisWaitDone(t, manager.executions[c[0].ID].done)
	intro.release <- struct{}{}
	analysisWaitDone(t, manager.executions[a[0].ID].done)
	if err := manager.reapExecutions(f.ctx, false); err != nil {
		t.Fatal(err)
	}
	dispatch(first, a[1])
	if len(manager.executions) != 0 {
		t.Fatal("oldest run stole the next slot from another runnable analysis run")
	}
	dispatch(second, b[0])
	if work := analysisReceiveWork(t, preview); work.RunID != second.ID {
		t.Fatal("analysis group did not alternate runs")
	}
	preview.release <- struct{}{}
	analysisWaitDone(t, manager.executions[b[0].ID].done)
	if err := manager.reapExecutions(f.ctx, false); err != nil {
		t.Fatal(err)
	}
	dispatch(second, b[1])
	if len(manager.executions) != 0 {
		t.Fatal("preview stole a consecutive group turn")
	}
	dispatch(first, a[1])
	analysisReceiveWork(t, intro)
	var unclaimed bool
	if err := f.pool.QueryRow(f.ctx, `SELECT executor_token IS NULL AND state='waiting' FROM task_run_children WHERE id=$1`, b[1].ID).Scan(&unclaimed); err != nil || !unclaimed {
		t.Fatal("group waiting manufactured an execution claim")
	}
}
