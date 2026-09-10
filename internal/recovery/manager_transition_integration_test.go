//go:build linux

package recovery

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func TestRecoveryManagerApplyRestartAndRollback(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	var verificationPool *pgxpool.Pool
	t.Cleanup(func() {
		if verificationPool != nil {
			verificationPool.Close()
		}
	})
	f := newManagerIntegrationFixture(t)
	ctx := f.seed.ctx
	if err := f.runtime.BindDatabase(ctx, f.seed.configuration, f.seed.source, f.lease); err != nil {
		t.Fatal("bind source ownership before transition capture")
	}
	originalHistory := recoveryEngineRetainedState(t, ctx, f.seed.source)
	plan := createTransitionPlan(t, f)
	if _, err := f.manager.Apply(ctx, f.seed.actor, plan.Id, ApplyRequest{Revision: plan.Revision, GenerationRevision: "0"}); err != nil {
		t.Fatal("authorize exact ready plan application")
	}
	if id, activated, err := f.manager.PendingSwitch(ctx); err != nil || id != plan.Id || activated {
		t.Fatal("apply admission did not expose an unactivated pending switch")
	}
	retiring := f.manager
	candidate, err := retiring.PrepareSwitch(ctx, plan.Id)
	if err != nil || candidate.State.Revision != 1 || candidate.State.DatabaseSlot != lifecycle.DatabaseRecovery {
		t.Fatalf("activate the exclusively leased restored target: %v", err)
	}
	registerTransitionCandidateCleanup(t, candidate)
	closeTransitionManager(t, retiring)
	if err := f.lease.Close(); err != nil {
		t.Fatal("release retiring source deployment lease")
	}
	verificationPool = retireTransitionSourcePool(t, f)
	f.manager = managerForTransitionCandidate(t, f, candidate)
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatal("reconcile new-generation history before application startup")
	}
	if id, activated, err := f.manager.PendingSwitch(ctx); err != nil || id != plan.Id || !activated {
		t.Fatal("target manager did not recognize its activated switch")
	}
	if err := f.manager.ValidateSwitch(ctx); err != nil {
		t.Fatalf("validate exact target before startup writers: %v", err)
	}
	stopStartup := initializeTransitionApplication(t, ctx, candidate.Pool, f.manager.cfg)
	commitTransitionAcceptanceReceipt(t, f.manager, plan.Id)
	if err := f.runtime.ReturnUnaccepted(ctx); !errors.Is(err, ErrConflict) {
		t.Fatal("an uncertain acceptance was eligible for automatic source return")
	}
	stopStartup()
	closeTransitionManager(t, f.manager)
	if err := candidate.Lease.Close(); err != nil {
		t.Fatal("close activated target lease for restart")
	}
	candidate.Pool.Close()
	if err := f.runtime.Close(); err != nil {
		t.Fatal("close runtime at committed acceptance-before-CAS boundary")
	}
	f.runtime, err = Open(ctx, f.seed.configuration)
	if err != nil {
		t.Fatal("reopen the original deployment policy after acceptance interruption")
	}
	if id, activated, err := f.runtime.RecoverStartupTransition(ctx); err != nil || id != plan.Id || !activated {
		t.Fatal("startup did not resume the activated target after acceptance commit")
	}
	resumed := openCurrentTransitionCandidate(t, f, plan.Id)
	f.manager = managerForTransitionCandidate(t, f, resumed)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("restart compared obsolete staged facts after startup and acceptance writes")
	}
	stopStartup = initializeTransitionApplication(t, ctx, resumed.Pool, f.manager.cfg)
	if err := f.manager.AcceptSwitch(ctx); err != nil {
		t.Fatalf("finish interrupted target acceptance: %v", err)
	}
	if err := f.manager.AcceptSwitch(ctx); err != nil {
		t.Fatal("accepted transition was not idempotent")
	}
	assertTransitionAppliedReceipt(t, ctx, resumed.Pool, plan.Id)
	if id, _, err := f.manager.PendingSwitch(ctx); err != nil || id != "" {
		t.Fatal("accepted target retained an unfinished transition")
	}
	if pending, err := f.runtime.lifecycle.Pending(ctx); err != nil || pending != nil {
		t.Fatal("accepted transition retained its lifecycle activation journal")
	}
	assertTransitionCredentialsRevoked(t, ctx, resumed.Pool, f)
	activeIdentity := identity.NewWithApplicationKeyVault(resumed.Pool, identity.NewApplicationKeyVault(f.manager.cfg.APIKeyMasterKeyFile))
	login, err := activeIdentity.Authenticate(ctx, f.seed.actor.User.Name, "recovery-administrator-password", identity.Client{Name: "Post-restore administrator"}, "admin")
	if err != nil {
		t.Fatal("accepted restored account could not issue a new credential")
	}
	actor, err := activeIdentity.Resolve(ctx, login.Token, "admin")
	if err != nil {
		t.Fatal("resolve newly issued target administrator")
	}
	status, err := f.manager.Status(ctx, actor)
	if err != nil || !status.Rollback.Available || !status.Rollback.MustReplace || status.GenerationRevision != "1" {
		t.Fatal("accepted target did not retain an explicit original rollback image")
	}
	rollback, err := f.manager.Rollback(ctx, actor, RollbackRequest{RequestId: recoveryEngineTestID(t), GenerationRevision: "1"})
	if err != nil || rollback.State != "applying" {
		t.Fatal("authorize rollback to the exact retained source")
	}
	stopStartup()
	if id, activated, err := f.manager.PendingSwitch(ctx); err != nil || id != rollback.Id || activated {
		t.Fatal("rollback admission did not retain a separate unactivated switch")
	}
	retiring = f.manager
	returned, err := retiring.PrepareSwitch(ctx, rollback.Id)
	if err != nil || returned.State.Revision != 2 || returned.State.DatabaseSlot != lifecycle.DatabasePrimary || returned.State.Master != lifecycle.MasterGeneration {
		t.Fatalf("activate normalized retained primary as a new generation: %v", err)
	}
	registerTransitionCandidateCleanup(t, returned)
	closeTransitionManager(t, retiring)
	if err := resumed.Lease.Close(); err != nil {
		t.Fatal("release retired restored-generation lease")
	}
	resumed.Pool.Close()
	f.manager = managerForTransitionCandidate(t, f, returned)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("validate the reactivated primary before startup")
	}
	stopPrimary := initializeTransitionApplication(t, ctx, returned.Pool, f.manager.cfg)
	defer stopPrimary()
	if err := f.manager.AcceptSwitch(ctx); err != nil {
		t.Fatalf("accept manual rollback generation: %v", err)
	}
	assertTransitionAppliedReceipt(t, ctx, returned.Pool, rollback.Id)
	assertTransitionCredentialsRevoked(t, ctx, returned.Pool, f)
	if current := recoveryEngineRetainedState(t, ctx, returned.Pool); current != originalHistory {
		t.Fatal("manual rollback changed retained original account, key, device, or catalog history")
	}
	if _, err := identity.New(returned.Pool).Authenticate(ctx, f.seed.actor.User.Name, "recovery-administrator-password", identity.Client{Name: "Post-rollback administrator"}, "admin"); err != nil {
		t.Fatal("manual rollback lost the original account password")
	}
}

func TestRecoveryManagerReturnsUnacceptedTargetAfterRestart(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	var verificationPool *pgxpool.Pool
	t.Cleanup(func() {
		if verificationPool != nil {
			verificationPool.Close()
		}
	})
	f := newManagerIntegrationFixture(t)
	ctx := f.seed.ctx
	if err := f.runtime.BindDatabase(ctx, f.seed.configuration, f.seed.source, f.lease); err != nil {
		t.Fatal("bind initial source before retaining it for return")
	}
	originalHistory := recoveryEngineRetainedState(t, ctx, f.seed.source)
	plan := createTransitionPlan(t, f)
	if _, err := f.manager.Apply(ctx, f.seed.actor, plan.Id, ApplyRequest{Revision: plan.Revision, GenerationRevision: "0"}); err != nil {
		t.Fatal("admit generation that will fail listener reservation")
	}
	candidate, err := f.manager.PrepareSwitch(ctx, plan.Id)
	if err != nil {
		t.Fatalf("activate candidate before startup-failure fixture: %v", err)
	}
	registerTransitionCandidateCleanup(t, candidate)
	closeTransitionManager(t, f.manager)
	if err := f.lease.Close(); err != nil {
		t.Fatal("close retired source lease before candidate startup")
	}
	verificationPool = retireTransitionSourcePool(t, f)
	f.manager = managerForTransitionCandidate(t, f, candidate)
	if err := f.manager.Reconcile(ctx); err != nil || f.manager.ValidateSwitch(ctx) != nil {
		t.Fatal("validate unaccepted target startup")
	}
	if _, err := candidate.Pool.Exec(ctx, `UPDATE public.task_definitions SET name='Pending startup reconciliation'
		WHERE key='library.scan'`); err != nil {
		t.Fatal("seed startup work after the exact staged-fingerprint boundary")
	}
	stopStartup := initializeTransitionApplication(t, ctx, candidate.Pool, f.manager.cfg)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve listener failure fixture")
	}
	if duplicate, err := net.Listen("tcp", occupied.Addr().String()); err == nil {
		duplicate.Close()
		occupied.Close()
		t.Fatal("target startup fixture did not fail address reservation")
	}
	occupied.Close()
	stopStartup()
	closeTransitionManager(t, f.manager)
	if err := candidate.Lease.Close(); err != nil {
		t.Fatal("close failed target lease")
	}
	candidate.Pool.Close()
	if err := f.runtime.Close(); err != nil {
		t.Fatal("close runtime after unaccepted target failure")
	}
	f.runtime, err = Open(ctx, f.seed.configuration)
	if err != nil {
		t.Fatal("reopen runtime after startup failure")
	}
	if id, activated, err := f.runtime.RecoverStartupTransition(ctx); err != nil || id != plan.Id || !activated {
		t.Fatal("startup silently abandoned the activated unaccepted generation")
	}
	if err := f.runtime.ReturnUnaccepted(ctx); err != nil {
		t.Fatalf("return explicitly to unchanged retained source after restart: %v", err)
	}
	current, err := f.runtime.lifecycle.Current()
	if err != nil || current.Revision != 2 || current.DatabaseSlot != lifecycle.DatabasePrimary || current.GenerationID == "" {
		t.Fatal("return rewound the lifecycle revision or lost the retained source image")
	}
	returned := openCurrentTransitionCandidate(t, f, "")
	f.manager = managerForTransitionCandidate(t, f, returned)
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatal("reconcile explicitly returned source")
	}
	if id, _, err := f.manager.PendingSwitch(ctx); err != nil || id != "" {
		t.Fatal("returned source retained an unfinished activation")
	}
	if history := recoveryEngineRetainedState(t, ctx, returned.Pool); history != originalHistory {
		t.Fatal("return changed the untouched original business state")
	}
	state, err := f.manager.operationCopy(plan.Id)
	if err != nil || state.State != "failed" || state.ErrorCode != "activation_failed" || state.ActivationAccepted || state.FailureGeneration != current.Digest {
		t.Fatal("failed target startup was reported as accepted")
	}
	f.auditID(t, activity.ActionRestoreFailed, plan.Id, activity.StateFailed)
	if err := f.manager.Reconcile(ctx); err != nil {
		t.Fatal("repeat returned-generation failure receipt recovery")
	}
	f.auditID(t, activity.ActionRestoreFailed, plan.Id, activity.StateFailed)
	originalIdentity := identity.NewWithApplicationKeyVault(returned.Pool, identity.NewApplicationKeyVault(f.manager.cfg.APIKeyMasterKeyFile))
	if _, err := originalIdentity.Resolve(ctx, f.seed.adminLogin.Token, "admin"); err != nil {
		t.Fatal("return unnecessarily revoked untouched original credentials")
	}
	if _, err := originalIdentity.ResolveEmby(ctx, f.seed.activeKey.Token); err != nil {
		t.Fatal("return lost the original active application-key history")
	}
}

func TestRecoveryManagerRejectsChangedCandidateBeforeActivation(t *testing.T) {
	defer releaseRecoveryEngineTestMemory()
	f := newManagerIntegrationFixture(t)
	ctx := f.seed.ctx
	if err := f.runtime.BindDatabase(ctx, f.seed.configuration, f.seed.source, f.lease); err != nil {
		t.Fatal("bind initial source before candidate-drift fixture")
	}
	before, err := f.runtime.lifecycle.Current()
	if err != nil {
		t.Fatal("read initial lifecycle identity")
	}
	history := recoveryEngineRetainedState(t, ctx, f.seed.source)
	plan := createTransitionPlan(t, f)
	var changedRevision int64
	if err := f.seed.target.QueryRow(ctx, `UPDATE public.managed_settings SET revision=revision+1 RETURNING revision`).Scan(&changedRevision); err != nil {
		t.Fatal("change the ready target after its retained proof was captured")
	}
	if _, err := f.manager.Apply(ctx, f.seed.actor, plan.Id, ApplyRequest{Revision: plan.Revision, GenerationRevision: "0"}); err != nil {
		t.Fatal("admit application that must recheck its candidate")
	}
	if candidate, err := f.manager.PrepareSwitch(ctx, plan.Id); !errors.Is(err, ErrConflict) || candidate != nil {
		if candidate != nil {
			_ = candidate.Lease.Close()
			candidate.Pool.Close()
		}
		t.Fatalf("changed target was not rejected at activation preparation: %v", err)
	}
	if current, err := f.runtime.lifecycle.Current(); err != nil || current != before {
		t.Fatal("failed candidate verification changed the active deployment")
	}
	if current := recoveryEngineRetainedState(t, ctx, f.seed.source); current != history {
		t.Fatal("candidate rejection changed retained source business state")
	}
	var remaining int64
	if err := f.seed.target.QueryRow(ctx, `SELECT revision FROM public.managed_settings`).Scan(&remaining); err != nil || remaining != changedRevision {
		t.Fatal("candidate rejection silently repaired or discarded unknown target changes")
	}
}

func createTransitionPlan(t *testing.T, f *managerIntegrationFixture) OperationView {
	t.Helper()
	passphrase := []byte("actual-generation-switch-passphrase")
	defer clear(passphrase)
	created, err := f.manager.Create(f.seed.ctx, f.seed.actor, CreateRequest{RequestId: recoveryEngineTestID(t), Passphrase: passphrase})
	if err != nil {
		t.Fatal("create real archive for generation transition")
	}
	created = f.waitOperation(t, created.Id, "completed")
	releaseRecoveryEngineTestMemory()
	backup, err := f.manager.Backup(f.seed.ctx, f.seed.actor, created.BackupId)
	if err != nil {
		t.Fatal("read transition archive digest")
	}
	plan, err := f.manager.Plan(f.seed.ctx, f.seed.actor, PlanRequest{RequestId: recoveryEngineTestID(t), BackupId: backup.Id,
		SHA256: backup.SHA256, Passphrase: passphrase, RestoreDefaults: true, GenerationRevision: "0"})
	if err != nil {
		t.Fatal("stage real transition target")
	}
	plan = f.waitOperation(t, plan.Id, "ready")
	releaseRecoveryEngineTestMemory()
	return plan
}

func managerForTransitionCandidate(t *testing.T, f *managerIntegrationFixture, candidate *SwitchCandidate) *Manager {
	t.Helper()
	cfg, state, err := f.runtime.ActiveConfig(f.seed.ctx)
	if err != nil || state != candidate.State || cfg.DatabaseURL != candidate.Pool.Config().ConnString() {
		t.Fatal("active configuration selected a different candidate database")
	}
	if err := f.runtime.CheckDatabase(f.seed.ctx, cfg, candidate.Pool, candidate.Lease); err != nil {
		t.Fatal("candidate database marker did not match local lifecycle ownership")
	}
	manager, err := NewManager(f.seed.ctx, f.runtime, cfg, candidate.Pool, candidate.Lease,
		identity.NewApplicationKeyVault(cfg.APIKeyMasterKeyFile), "transition-integration")
	if err != nil {
		t.Fatalf("construct active candidate manager: %v", err)
	}
	return manager
}

func openCurrentTransitionCandidate(t *testing.T, f *managerIntegrationFixture, operationID string) *SwitchCandidate {
	t.Helper()
	cfg, state, err := f.runtime.ActiveConfig(f.seed.ctx)
	if err != nil {
		t.Fatal("resolve current generation after process restart")
	}
	pool, err := database.Open(f.seed.ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal("open exact configured active database")
	}
	lease, err := database.AcquireLease(f.seed.ctx, pool)
	if err != nil {
		pool.Close()
		t.Fatal("acquire restarted active generation lease")
	}
	candidate := &SwitchCandidate{Pool: pool, Lease: lease, State: state, OperationID: operationID}
	registerTransitionCandidateCleanup(t, candidate)
	return candidate
}

func registerTransitionCandidateCleanup(t *testing.T, candidate *SwitchCandidate) {
	t.Helper()
	t.Cleanup(func() {
		_ = candidate.Lease.Close()
		candidate.Pool.Close()
	})
}

// The retired application pool closes before candidate startup. A separate,
// lazy verification pool remains outside the application solely for history
// reads and the parent fixture's named-object cleanup.
func retireTransitionSourcePool(t *testing.T, f *managerIntegrationFixture) *pgxpool.Pool {
	t.Helper()
	configuration := f.seed.source.Config()
	configuration.MinConns = 0
	f.seed.source.Close()
	verification, err := pgxpool.NewWithConfig(f.seed.ctx, configuration)
	if err != nil {
		t.Fatal("create independent source verification pool")
	}
	f.seed.source = verification
	return verification
}

func closeTransitionManager(t *testing.T, manager *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatal("drain retiring generation manager")
	}
}

func initializeTransitionApplication(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config.Config) func() {
	t.Helper()
	catalog, err := library.New(pool, media.Prober{FFprobePath: cfg.FFprobePath, FFmpegPath: cfg.FFmpegPath, Timeout: time.Second}, cfg.MediaRoots)
	if err != nil {
		t.Fatal("initialize actual catalog owner for candidate startup")
	}
	store, err := tasks.New(pool, catalog)
	if err != nil || store.Reconcile(ctx) != nil || store.RecoverRuns(ctx) != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = catalog.Close(closeCtx)
		cancel()
		t.Fatal("initialize actual scheduled-task repository without a running manager")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = catalog.Close(closeCtx)
		cancel()
		t.Fatal("reserve target listener without serving before acceptance")
	}
	stop := func() {
		_ = listener.Close()
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := catalog.Close(closeCtx); err != nil {
			t.Error("close candidate startup catalog owner")
		}
	}
	t.Cleanup(stop)
	return stop
}

func commitTransitionAcceptanceReceipt(t *testing.T, manager *Manager, id string) {
	t.Helper()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	j, err := loadSwitchJournal(manager.ctx, manager.runtime)
	if err != nil || j.data.Transition == nil || j.data.Transition.OperationID != id || j.data.Transition.Phase != "initializing" {
		t.Fatal("acceptance fixture did not start after validated application initialization")
	}
	j.data.Transition.Phase = "accepting"
	if err := j.save(manager.ctx); err != nil {
		t.Fatal("persist irreversible accepting intent before database receipt")
	}
	manager.adoptSwitchJournal(j)
	before := bytes.Clone(j.snapshot.Payload)
	if err := manager.recordSystem(manager.ctx, *j.operation(), activity.ActionRestoreApplied, activity.StateCompleted); err != nil {
		t.Fatal("commit target acceptance receipt before coordinator CAS")
	}
	stored, err := manager.runtime.control.Read(manager.ctx)
	if err != nil || !bytes.Equal(stored.Payload, before) {
		t.Fatal("acceptance fixture advanced beyond the intended committed receipt boundary")
	}
}

func assertTransitionAppliedReceipt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.activity_entries
		WHERE action=$1 AND resource_id=$2 AND source='system' AND actor_kind='system' AND state='completed'`,
		string(activity.ActionRestoreApplied), id).Scan(&count); err != nil || count != 1 {
		t.Fatal("accepted transition lacks its one exact target audit receipt")
	}
}

func assertTransitionCredentialsRevoked(t *testing.T, ctx context.Context, pool *pgxpool.Pool, f *managerIntegrationFixture) {
	t.Helper()
	var unrevoked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.sessions WHERE revoked_at IS NULL`).Scan(&unrevoked); err != nil || unrevoked != 0 {
		t.Fatal("activated restored or rollback database revived old credentials")
	}
	store := identity.New(pool)
	if _, err := store.Resolve(ctx, f.seed.adminLogin.Token, "admin"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("original login token became authorized in a new generation")
	}
	if _, err := store.ResolveEmby(ctx, f.seed.activeKey.Token); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("original application token became authorized in a new generation")
	}
	files, err := f.runtime.lifecycle.ReadGeneration(ctx, f.manager.current.GenerationID)
	if err != nil {
		t.Fatal("read active generation master for history witness")
	}
	defer clear(files.Master)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin active-generation sealed-history witness")
	}
	witness, err := identity.ValidateApplicationKeyRecovery(ctx, tx, files.Master)
	_ = tx.Rollback(ctx)
	if err != nil || witness.SealedKeyCount != 2 || !witness.HasMasterKey {
		t.Fatal("generation transition lost recoverable key history")
	}
}
