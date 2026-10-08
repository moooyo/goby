//go:build linux

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recovery"
)

func startupSuppliedPool(t *testing.T, f *generationStartupFixture, before func(context.Context, *pgx.ConnConfig) error,
	after func(context.Context, *pgx.Conn) error) (*pgxpool.Pool, *database.Lease) {
	t.Helper()
	configuration, err := pgxpool.ParseConfig(f.primary.uri)
	if err != nil {
		t.Fatal("parse the exact supplied startup slot")
	}
	configuration.MaxConns, configuration.MinConns = database.DataMaxConns, 1
	configuration.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	configuration.ConnConfig.RuntimeParams["application_name"] = "goby"
	configuration.ConnConfig.RuntimeParams["jit"] = "off"
	configuration.ConnConfig.RuntimeParams["statement_timeout"] = "15000"
	configuration.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "30000"
	configuration.ConnConfig.ConnectTimeout = 5 * time.Second
	configuration.BeforeConnect, configuration.AfterConnect = before, after
	pool, err := pgxpool.NewWithConfig(f.ctx, configuration)
	if err != nil {
		t.Fatal("create the real supplied startup slot")
	}
	t.Cleanup(pool.Close)
	lease, err := database.AcquireLease(f.ctx, pool)
	if err != nil {
		t.Fatal("acquire the real supplied slot deployment lease")
	}
	t.Cleanup(func() { _ = lease.Close() })
	return pool, lease
}

func startupWaitPoolClosing(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		bounded, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		connection, err := pool.Acquire(bounded)
		cancel()
		if connection != nil {
			connection.Release()
		}
		if errors.Is(err, puddle.ErrClosedPool) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the actual failed preparation did not begin its application pool drain")
}

func TestGenerationStartupPublishesReservedPairAndDrainsLeaseLast(t *testing.T) {
	f := newGenerationStartupFixture(t)
	f.openRuntime(t)
	g := f.prepare(t, nil)
	f.assertPublishedPools(t, g, f.primary)
	f.assertReady(t, g)
	borrowed, err := g.playbackControlPool.Acquire(f.ctx)
	if err != nil {
		t.Fatal("borrow the actually published reserved pool before shutdown")
	}
	t.Cleanup(borrowed.Release)
	deadline, cancel := context.WithTimeout(f.ctx, 50*time.Millisecond)
	err = g.Close(deadline)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || !g.lease.Protects(g.pool) {
		t.Fatal("the actual application startup generation released ownership before its reserved borrower drained")
	}
	select {
	case <-g.closeDone:
		t.Fatal("the actual startup generation reported completion with a retained reserved borrower")
	default:
	}
	borrowed.Release()
	if err := f.owner.close(g, true); err != nil || g.lease.Protects(g.pool) {
		t.Fatal("the actual prepared generation did not drain both application pools before ownership release")
	}
	assertGenerationPoolClosed(t, "prepared generation data", g.pool)
	assertGenerationPoolClosed(t, "prepared generation control", g.playbackControlPool)
}

func TestGenerationStartupControlOpenFailureKeepsLeaseDuringActualCleanup(t *testing.T) {
	f := newGenerationStartupFixture(t)
	f.openRuntime(t)
	entered, reject := make(chan struct{}), make(chan struct{})
	var enteredOnce, rejectOnce sync.Once
	pool, lease := startupSuppliedPool(t, f, func(ctx context.Context, configuration *pgx.ConnConfig) error {
		if configuration.RuntimeParams["application_name"] != "goby-playback-control" {
			return nil
		}
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-reject:
			return errors.New("injected reserved startup connection failure")
		case <-ctx.Done():
			return ctx.Err()
		}
	}, nil)
	borrowed, err := pool.Acquire(f.ctx)
	if err != nil {
		t.Fatal("retain a real supplied-slot borrower across failed preparation")
	}
	t.Cleanup(borrowed.Release)
	type result struct {
		generation *generation
		err        error
	}
	completed := make(chan result, 1)
	go func() {
		g, err := prepareGeneration(f.ctx, f.runtime, f.logger, nil, "startup-failure-fixture", pool, lease)
		completed <- result{g, err}
	}()
	var joined result
	var joinOnce sync.Once
	join := func() {
		joinOnce.Do(func() {
			rejectOnce.Do(func() { close(reject) })
			borrowed.Release()
			select {
			case joined = <-completed:
				f.owner.add(joined.generation)
			case <-time.After(25 * time.Second):
				t.Error("join the actual failed preparation before private runtime cleanup")
				// A diagnostic timeout is not a transfer of resource ownership.
				// Keep the runtime and exact databases alive until this owner ends.
				joined = <-completed
				f.owner.add(joined.generation)
			}
		})
	}
	t.Cleanup(join)
	select {
	case <-entered:
	case observed := <-completed:
		completed <- observed
		t.Fatalf("real preparation ended before reserved connection creation: cleanup_handle=%t failure={%s}", observed.generation != nil, generationStartupFailure(observed.err))
	case <-time.After(65 * time.Second):
		t.Fatal("real preparation exceeded its bounded startup before reserved connection creation")
	case <-f.ctx.Done():
		t.Fatal("real preparation did not reach reserved connection creation")
	}
	rejectOnce.Do(func() { close(reject) })
	startupWaitPoolClosing(t, f.ctx, pool)
	if !lease.Protects(pool) {
		t.Fatal("failed startup released the deployment lease while an actual Data borrower remained")
	}
	select {
	case observed := <-completed:
		completed <- observed
		t.Fatal("failed startup lost ownership of its still-draining cleanup pipeline")
	default:
	}
	join()
	if joined.err == nil || lease.Protects(pool) {
		t.Fatal("failed reserved connection startup did not finish actual resource cleanup")
	}
	assertGenerationPoolClosed(t, "failed startup data", pool)
	if joined.generation != nil {
		if joined.generation.app != nil || joined.generation.listener != nil || joined.generation.ctx.Err() == nil {
			t.Fatal("failed startup exposed an application or an uncancelled cleanup handle")
		}
	}
}

func TestGenerationStartupFailureAfterControlOpenClosesBothPools(t *testing.T) {
	f := newGenerationStartupFixture(t)
	// Runtime and backup defaults do not interpret the HTTP binding. Server
	// initialization must reject it after the reserved pool has authenticated.
	f.cfg.ListenAddress = "invalid-startup-binding"
	f.openRuntime(t)
	var controlPID atomic.Uint32
	pool, lease := startupSuppliedPool(t, f, nil, func(_ context.Context, connection *pgx.Conn) error {
		if connection.Config().RuntimeParams["application_name"] == "goby-playback-control" {
			controlPID.Store(connection.PgConn().PID())
		}
		return nil
	})
	g, err := prepareGeneration(f.ctx, f.runtime, f.logger, nil, "startup-post-control-failure", pool, lease)
	f.owner.add(g)
	if err == nil || controlPID.Load() == 0 {
		t.Fatalf("the application failure did not occur after a real reserved backend was established: control_pid_observed=%t failure={%s}", controlPID.Load() != 0, generationStartupFailure(err))
	}
	if g != nil {
		if err := f.owner.close(g, false); err != nil {
			t.Fatal("join the post-reservation failed preparation")
		}
	}
	if lease.Protects(pool) {
		t.Fatal("the post-reservation application failure retained its Data pool or lease")
	}
	assertGenerationPoolClosed(t, "post-reservation startup data", pool)
	var present bool
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := f.admin.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datid=$2::oid)", int64(controlPID.Load()), f.primary.databaseOID).Scan(&present)
		if err != nil || !present || time.Now().After(deadline) {
			if err != nil || present {
				t.Fatal("failed application initialization retained its actual reserved backend")
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestGenerationStartupPendingSourceAndActivatedCandidateUseRealRecovery(t *testing.T) {
	f := newGenerationStartupFixture(t)
	f.openRuntime(t)
	source := f.prepare(t, nil)
	actor := f.administrator(t, source)
	plan := f.requestedRestore(t, source, actor)
	if err := f.owner.close(source, false); err != nil {
		t.Fatal("close the live source before replaying its durable unactivated transition")
	}
	pending := f.prepare(t, nil)
	if pending.pendingSwitch != plan.Id || pending.switchActivated || pending.app != nil || pending.server != nil || pending.listener != nil || pending.playbackControlPool != nil ||
		pending.pool.Config().MaxConns != database.DataMaxConns || !pending.lease.Protects(pending.pool) {
		t.Fatal("replayed pending source initialized application writers, control capacity, or a listener")
	}
	if err := <-pending.Start(); !errors.Is(err, recovery.ErrConflict) {
		t.Fatal("the quiescent pending source was allowed to serve")
	}
	input, err := prepareGenerationSwitch(f.ctx, f.cfg, f.owner, pending, plan.Id)
	if err != nil || input == nil || input.pool == nil || input.lease == nil || input.state.DatabaseSlot != lifecycle.DatabaseRecovery || input.state.Revision != 1 {
		t.Fatalf("transfer the actual prepared recovery candidate: error_type=%T", err)
	}
	if pending.lease.Protects(pending.pool) {
		t.Fatal("the real recovery coordinator retained source resources while preparing its target application")
	}
	assertGenerationPoolClosed(t, "pending source data", pending.pool)
	target := f.prepare(t, input)
	if target.pendingSwitch != plan.Id || !target.switchActivated || target.state.DatabaseSlot != lifecycle.DatabaseRecovery || target.started || target.serveResult != nil {
		t.Fatal("the actual activated candidate lost its unaccepted publication fence")
	}
	f.assertPublishedPools(t, target, f.target)
	// A reserved listener is concrete and reviewable but serves no clients
	// before acceptance. Do not call Start here: its first result is cached.
	probe, stopProbe := context.WithTimeout(f.ctx, 100*time.Millisecond)
	request, err := http.NewRequestWithContext(probe, http.MethodGet, "http://"+target.listener.Addr().String()+"/healthz", nil)
	if err != nil {
		t.Fatal("create the pre-acceptance publication probe")
	}
	transport := &http.Transport{DisableKeepAlives: true}
	response, probeErr := (&http.Client{Transport: transport}).Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	transport.CloseIdleConnections()
	deadlineReached := errors.Is(probe.Err(), context.DeadlineExceeded)
	stopProbe()
	if probeErr == nil || !deadlineReached || !errors.Is(probeErr, context.DeadlineExceeded) {
		t.Fatal("the activated candidate served HTTP before actual recovery acceptance")
	}
	if err := acceptPreparedGeneration(f.ctx, f.cfg, target); err != nil {
		t.Fatalf("accept the actual prepared recovery generation: error_type=%T", err)
	}
	f.assertReady(t, target)
}

func TestGenerationStartupFailedActivatedCandidateDrainsAndReturnsRetainedSource(t *testing.T) {
	f := newGenerationStartupFixture(t)
	f.openRuntime(t)
	source := f.prepare(t, nil)
	actor := f.administrator(t, source)
	plan := f.requestedRestore(t, source, actor)
	input, err := prepareGenerationSwitch(f.ctx, f.cfg, f.owner, source, plan.Id)
	if err != nil || input == nil || input.state.DatabaseSlot != lifecycle.DatabaseRecovery || input.pool.Config().MaxConns != database.DataMaxConns {
		t.Fatalf("activate the real target for the startup failure: error_type=%T", err)
	}
	// Existing candidate Data and deployment-lease sessions remain usable.
	// Only new sessions, including the not-yet-open Control pool, are denied.
	var candidateDatabaseOID, candidateRoleOID int64
	if err := input.pool.QueryRow(f.ctx, `SELECT d.oid::bigint,r.oid::bigint
		FROM pg_database d JOIN pg_roles r ON r.rolname=current_user WHERE d.datname=current_database()`).
		Scan(&candidateDatabaseOID, &candidateRoleOID); err != nil || candidateDatabaseOID != f.target.databaseOID || candidateRoleOID != f.target.roleOID || !input.lease.Protects(input.pool) {
		t.Fatalf("the real activated supplied candidate has no reusable exact target Data session and lease: database_oid_match=%t role_oid_match=%t failure={%s}",
			candidateDatabaseOID == f.target.databaseOID, candidateRoleOID == f.target.roleOID, generationStartupFailure(err))
	}
	f.allowConnections(t, f.target, false)
	var restoreOnce sync.Once
	restore := func() { restoreOnce.Do(func() { f.allowConnections(t, f.target, true) }) }
	t.Cleanup(restore)
	witness, cancelWitness := context.WithTimeout(f.ctx, 5*time.Second)
	var admissionDatabaseOID, admissionOwnerOID int64
	var allowsConnections bool
	admissionErr := f.admin.QueryRow(witness, "SELECT oid::bigint,datdba::bigint,datallowconn FROM pg_database WHERE datname=$1", f.target.name).
		Scan(&admissionDatabaseOID, &admissionOwnerOID, &allowsConnections)
	activeConfig, activeState, activeErr := f.runtime.ActiveConfig(witness)
	cancelWitness()
	if admissionErr != nil || admissionDatabaseOID != candidateDatabaseOID || admissionOwnerOID != candidateRoleOID || allowsConnections ||
		activeErr != nil || activeConfig.DatabaseURL != f.target.uri || activeState != input.state {
		t.Fatalf("reserved startup failure is not fenced to the exact activated target: database_oid_match=%t owner_oid_match=%t allows_connections=%t active_target_match=%t active_state_match=%t admission_failure={%s} active_failure={%s}",
			admissionDatabaseOID == candidateDatabaseOID, admissionOwnerOID == candidateRoleOID, allowsConnections,
			activeConfig.DatabaseURL == f.target.uri, activeState == input.state, generationStartupFailure(admissionErr), generationStartupFailure(activeErr))
	}
	data, lease := input.pool, input.lease
	target, prepareErr := f.owner.prepare(f.ctx, f.logger, nil, input)
	failure := generationStartupFailure(prepareErr)
	// Production classified errors expose their class through Error; the fixed
	// startup stage is retained only in the sanitized cause graph.
	if prepareErr == nil || !strings.Contains(failure, "stage=playback control database capacity is unavailable") {
		t.Fatalf("activated target preparation did not fail at reserved connection creation: cleanup_handle=%t parent_deadline=%t preparation_deadline=%t sqlstate_present=%t target_database_oid=%d target_owner_oid=%d allows_connections=%t data_connections=%d lease_protected=%t failure={%s}",
			target != nil, errors.Is(f.ctx.Err(), context.DeadlineExceeded), errors.Is(prepareErr, context.DeadlineExceeded), strings.Contains(failure, "sqlstate="),
			admissionDatabaseOID, admissionOwnerOID, allowsConnections, data.Stat().TotalConns(), lease.Protects(data), failure)
	}
	t.Logf("activated_control_open_failure cleanup_handle=%t parent_deadline=%t preparation_deadline=%t sqlstate_present=%t target_database_oid=%d target_owner_oid=%d allows_connections=%t failure={%s}",
		target != nil, errors.Is(f.ctx.Err(), context.DeadlineExceeded), errors.Is(prepareErr, context.DeadlineExceeded), strings.Contains(failure, "sqlstate="),
		admissionDatabaseOID, admissionOwnerOID, allowsConnections, failure)
	if target != nil {
		if target.listener != nil || target.app != nil || target.ctx.Err() == nil {
			t.Fatal("failed activated startup exposed an application or uncancelled cleanup handle")
		}
		if err := f.owner.close(target, false); err != nil {
			t.Fatal("join the actual failed activated candidate cleanup")
		}
	}
	if lease.Protects(data) {
		t.Fatal("the failed activated candidate retained its supplied pool or deployment lease")
	}
	assertGenerationPoolClosed(t, "failed activated candidate data", data)
	restore()
	if err := f.runtime.ReturnUnaccepted(f.ctx); err != nil {
		t.Fatalf("return the actual unaccepted target to its retained source: error_type=%T", err)
	}
	returned := f.prepare(t, nil)
	if returned.state.DatabaseSlot != lifecycle.DatabasePrimary || returned.state.Revision != 2 || returned.pendingSwitch != "" || returned.switchActivated {
		t.Fatal("the failed activated startup did not reconstruct the authoritative retained source generation")
	}
	f.assertPublishedPools(t, returned, f.primary)
	f.assertReady(t, returned)
}
