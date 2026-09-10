//go:build linux

package recoverydb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backupformat"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/lifecycle"
)

const recoveryFixtureDeployment = "11111111111111111111111111111111"
const recoveryFixtureGeneration = "22222222222222222222222222222222"
const recoveryFixtureNextGeneration = "33333333333333333333333333333333"

// This package needs exclusive use of the complete pair, unlike the backup
// parser tests that use random schemas. The outside operator creates and drops
// the explicitly disposable databases and roles. Failure intentionally leaves
// evidence in that owned pair; this fixture never drops a database or role and
// never uses a fallback DROP SCHEMA CASCADE cleanup.
func TestRecoveryDatabaseStoreIntegration(t *testing.T) {
	f := newRecoveryStoreFixture(t)
	run := func(name string, test func(*testing.T)) {
		if !t.Run(name, test) {
			t.Fatal("stop using the shared disposable pair after a failed recovery step")
		}
	}
	initial := lifecycle.State{DeploymentID: recoveryFixtureDeployment, DatabaseSlot: lifecycle.DatabasePrimary, Master: lifecycle.MasterDefault, Digest: strings.Repeat("a", 64)}
	var retained Retained

	run("lease_and_empty_target_boundaries", func(t *testing.T) {
		if _, err := New(f.source.pool, f.target.lease, f.source.config); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("a lease for another pool was accepted: %v", err)
		}
		otherPool, err := pgxpool.NewWithConfig(f.ctx, f.source.pool.Config())
		if err != nil {
			t.Fatal("create another pool for the same owned database")
		}
		defer otherPool.Close()
		if _, err := New(otherPool, f.source.lease, f.source.config); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("same-URL pool bypassed lease ownership: %v", err)
		}
		if _, err := New(f.source.pool, nil, f.source.config); err == nil {
			t.Fatal("a missing deployment lease was accepted")
		}
		unsupported := f.source.config
		unsupported.Postgres.Schema = "nonpublic_fixture"
		if _, err := New(f.source.pool, f.source.lease, unsupported); err == nil {
			t.Fatal("the recovery adapter accepted an unsupported schema")
		}
		f.assertEmpty(t, f.source)
		f.assertEmpty(t, f.target)
		bound, finish, err := f.source.store.BoundContext(f.ctx)
		if err != nil {
			t.Fatalf("bind an operation to the live deployment lease: %v", err)
		}
		finish()
		if !errors.Is(bound.Err(), context.Canceled) || f.ctx.Err() != nil || !f.source.lease.Protects(f.source.pool) {
			t.Fatal("finishing a bound operation cancelled its caller or deployment lease")
		}
		if err := f.target.lease.Close(); err != nil {
			t.Fatal("close the owned empty target lease")
		}
		if _, err := f.target.store.InspectEmpty(f.ctx); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("closed lease accepted an operation: %v", err)
		}
		if _, err := New(f.target.pool, f.target.lease, f.target.config); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("closed lease accepted a new store: %v", err)
		}
		f.reacquire(t, f.target)
		f.assertEmpty(t, f.target)
	})

	run("pre_migration_read_preserves_legacy_markers_without_upgrading", func(t *testing.T) {
		tx, err := f.source.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal("begin the trusted legacy migration fixture")
		}
		defer recoveryRollback(tx)
		if _, err := tx.Exec(f.ctx, `SET LOCAL search_path=public,pg_catalog`); err != nil {
			t.Fatal("set the trusted legacy migration search path")
		}
		if err := database.RecoveryMigrateTo(f.ctx, tx, 1); err != nil {
			t.Fatalf("create a genuine version-one legacy database: %v", err)
		}
		if _, err := tx.Exec(f.ctx, `INSERT INTO public.server_settings(key,value,created_at,updated_at)
			VALUES('legacy_fixture_setting','Legacy setting survives','2020-01-01T00:00:00Z','2020-01-02T00:00:00Z')`); err != nil {
			t.Fatal("seed the legacy row preservation witness")
		}
		if err := tx.Commit(f.ctx); err != nil {
			t.Fatal("commit the explicitly owned legacy fixture")
		}
		result, err := f.source.store.Read(f.ctx)
		if err != nil || !result.TablePresent || result.Raw != (RawMarker{}) {
			t.Fatalf("legacy marker absence was confused with a missing table: %v", err)
		}
		foreign, err := EncodeMarker(Marker{Version: 1, DeploymentID: strings.Repeat("f", 32), GenerationID: strings.Repeat("e", 32), Slot: lifecycle.DatabaseRecovery})
		if err != nil {
			t.Fatal("encode the foreign legacy claim")
		}
		for _, raw := range []string{foreign, "null"} {
			f.exec(t, f.source, `INSERT INTO public.server_settings(key,value) VALUES($1,$2)
				ON CONFLICT(key) DO UPDATE SET value=excluded.value`, MarkerKey, raw)
			result, err := f.source.store.Read(f.ctx)
			if err != nil || !result.TablePresent || result.Raw != (RawMarker{Present: true, Value: raw}) {
				t.Fatalf("pre-migration read lost a foreign or invalid marker: %v", err)
			}
			var version int64
			var witness string
			var laterTableAbsent bool
			if f.source.pool.QueryRow(f.ctx, `SELECT (SELECT max(version) FROM public.schema_migrations),
				(SELECT value FROM public.server_settings WHERE key='legacy_fixture_setting'),
				pg_catalog.to_regclass('public.devices') IS NULL`).Scan(&version, &witness, &laterTableAbsent) != nil || version != 1 || witness != "Legacy setting survives" || !laterTableAbsent {
				t.Fatal("pre-migration marker inspection changed legacy rows or applied later migrations")
			}
			f.assertNamespace(t, f.source, false)
		}
		f.exec(t, f.source, `DELETE FROM public.server_settings WHERE key=$1 AND value='null'`, MarkerKey)
		result, err = f.source.store.Read(f.ctx)
		if err != nil || !result.TablePresent || result.Raw.Present {
			t.Fatalf("legacy cleanup did not restore exact marker absence: %v", err)
		}
	})

	run("initial_binding_is_exact_and_idempotent", func(t *testing.T) {
		if err := database.Migrate(f.ctx, f.source.pool); err != nil {
			t.Fatalf("apply trusted source migrations: %v", err)
		}
		f.exec(t, f.source, `INSERT INTO public.server_settings(key,value,created_at,updated_at)
			VALUES('server_id','recovery-store-fixture','2020-01-01T00:00:00Z','2020-01-02T00:00:00Z');
			INSERT INTO public.users(id,name,normalized_name,password_hash,is_administrator,created_at,updated_at)
			VALUES('recovery-admin','Before capture','recovery admin','fixture-only-hash',true,'2020-01-01T00:00:00Z','2020-01-02T00:00:00Z');
			UPDATE public.managed_settings SET revision=9007199254740993,server_name='Retained fixture',server_name_mode='custom'`)
		if raw := f.read(t, f.source); raw != (RawMarker{}) {
			t.Fatal("a migrated database unexpectedly has a generation claim")
		}
		if _, err := f.source.store.Capture(f.ctx); !errors.Is(err, ErrConflict) {
			t.Fatalf("an unbound database was captured as locally retained: %v", err)
		}
		marker, err := f.source.store.BindInitial(f.ctx, initial)
		want := Marker{Version: 1, DeploymentID: recoveryFixtureDeployment, Slot: lifecycle.DatabasePrimary}
		if err != nil || marker != want {
			t.Fatalf("bind initial generation: %v", err)
		}
		retained = f.capture(t, f.source)
		if retained.Marker != want || !retained.RawMarker.Present || retained.Database != f.source.database || retained.Role != f.source.role {
			t.Fatal("initial capture lost its marker or actual database identity")
		}
		if _, err := f.source.store.InspectEmpty(f.ctx); err == nil {
			t.Fatal("the populated bound source was reported as an empty target")
		}
		if again, err := f.source.store.BindInitial(f.ctx, initial); err != nil || again != want {
			t.Fatalf("repeat initial binding: %v", err)
		}
		f.assertFacts(t, f.source, retained.Facts)
		if f.read(t, f.source) != retained.RawMarker {
			t.Fatal("idempotent binding rewrote the raw marker")
		}
		foreign := initial
		foreign.DeploymentID = strings.Repeat("f", 32)
		if _, err := f.source.store.BindInitial(f.ctx, foreign); err == nil {
			t.Fatal("a different deployment adopted the existing initial marker")
		}
		for name, mutate := range map[string]func(*lifecycle.State){
			"later_revision":     func(state *lifecycle.State) { state.Revision = 1 },
			"generation_present": func(state *lifecycle.State) { state.GenerationID = recoveryFixtureGeneration },
			"recovery_slot":      func(state *lifecycle.State) { state.DatabaseSlot = lifecycle.DatabaseRecovery },
		} {
			t.Run(name, func(t *testing.T) {
				state := initial
				mutate(&state)
				if _, err := f.source.store.BindInitial(f.ctx, state); err == nil {
					t.Fatal("a noninitial local state performed initial binding")
				}
			})
		}
		f.assertFacts(t, f.source, retained.Facts)
		f.assertNamespace(t, f.source, false)
	})

	run("initial_primary_can_replace_its_owned_failed_inactive_workspace", func(t *testing.T) {
		if err := database.Migrate(f.ctx, f.target.pool); err != nil {
			t.Fatalf("prepare failed inactive workspace: %v", err)
		}
		f.exec(t, f.target, `INSERT INTO public.server_settings(key,value) VALUES('server_id','failed-inactive-workspace');
		 INSERT INTO public.users(id,name,normalized_name,password_hash,is_administrator) VALUES('failed-workspace-admin','Failed workspace','failed workspace','fixture-only-hash',true)`)
		desired := Marker{Version: 1, DeploymentID: recoveryFixtureDeployment, GenerationID: recoveryFixtureGeneration, Slot: lifecycle.DatabaseRecovery}
		if _, err := f.target.store.Stamp(f.ctx, desired, RawMarker{}); err != nil {
			t.Fatalf("bind owned failed inactive workspace: %v", err)
		}
		workspace := f.capture(t, f.target)
		if err := f.target.store.ResetOwnedTarget(f.ctx, initial, workspace); err != nil {
			t.Fatalf("replace inactive workspace while initial primary remains active: %v", err)
		}
		f.assertNamespace(t, f.target, true)
		f.assertEmpty(t, f.target)
		f.assertFacts(t, f.source, retained.Facts)
		if f.read(t, f.source) != retained.RawMarker {
			t.Fatal("failed workspace replacement changed the initial active marker")
		}
	})

	run("stamp_compares_raw_bytes_and_foreign_claims_are_not_local", func(t *testing.T) {
		foreignRaw := " {\n \"slot\":\"recovery\", \"generationId\":\"" + strings.Repeat("e", 32) + "\", \"deploymentId\":\"" + strings.Repeat("f", 32) + "\", \"version\":1 }\n"
		f.exec(t, f.source, `UPDATE public.server_settings SET value=$1 WHERE key=$2`, foreignRaw, MarkerKey)
		raw := f.read(t, f.source)
		if raw != (RawMarker{Present: true, Value: foreignRaw}) {
			t.Fatal("reading a foreign marker normalized its exact stored bytes")
		}
		if _, err := f.source.store.Capture(f.ctx); !errors.Is(err, ErrConflict) {
			t.Fatalf("a foreign claim became a locally retained generation: %v", err)
		}
		if _, err := f.source.store.BindInitial(f.ctx, initial); err == nil {
			t.Fatal("initial binding replaced a foreign claim")
		}
		before := f.facts(t, f.source)
		foreign, err := DecodeMarker(foreignRaw)
		if err != nil {
			t.Fatal("decode the valid foreign fixture claim")
		}
		canonical, err := EncodeMarker(foreign)
		if err != nil {
			t.Fatal("encode the equivalent foreign fixture claim")
		}
		desired := Marker{Version: 1, DeploymentID: recoveryFixtureDeployment, GenerationID: recoveryFixtureGeneration, Slot: lifecycle.DatabasePrimary}
		for _, stale := range []RawMarker{{}, {Present: true, Value: ""}, {Present: true, Value: canonical}} {
			if _, err := f.source.store.Stamp(f.ctx, desired, stale); !errors.Is(err, ErrConflict) {
				t.Fatalf("an inexact raw comparison succeeded: %v", err)
			}
		}
		f.assertFacts(t, f.source, before)
		if got, err := f.source.store.Stamp(f.ctx, desired, raw); err != nil || got != desired {
			t.Fatalf("stamp verified foreign bytes into the local generation: %v", err)
		}
		retained = f.capture(t, f.source)
		f.assertFacts(t, f.source, retained.Facts)
		if _, err := f.source.store.Stamp(f.ctx, desired, raw); !errors.Is(err, ErrConflict) {
			t.Fatalf("already-desired data bypassed the exact stale CAS: %v", err)
		}
		f.assertFacts(t, f.source, retained.Facts)
		for name, mutate := range map[string]func(*Marker){
			"foreign_deployment": func(marker *Marker) { marker.DeploymentID = strings.Repeat("f", 32) },
			"wrong_slot":         func(marker *Marker) { marker.Slot = lifecycle.DatabaseRecovery },
			"initial_generation": func(marker *Marker) { marker.GenerationID = "" },
		} {
			t.Run(name, func(t *testing.T) {
				value := desired
				mutate(&value)
				if _, err := f.source.store.Stamp(f.ctx, value, retained.RawMarker); err == nil {
					t.Fatal("a nonlocal or initial claim was accepted by Stamp")
				}
			})
		}
		f.assertFacts(t, f.source, retained.Facts)
	})

	run("present_invalid_marker_is_distinct_from_absence", func(t *testing.T) {
		for _, value := range []string{"", "null", "owned-invalid-marker"} {
			f.exec(t, f.source, `UPDATE public.server_settings SET value=$1 WHERE key=$2`, value, MarkerKey)
			raw := f.read(t, f.source)
			if raw != (RawMarker{Present: true, Value: value}) {
				t.Fatal("a present invalid marker was conflated with absence")
			}
			before := f.facts(t, f.source)
			if _, err := f.source.store.Capture(f.ctx); !errors.Is(err, ErrConflict) {
				t.Fatalf("invalid marker capture = %v", err)
			}
			if _, err := f.source.store.BindInitial(f.ctx, initial); err == nil {
				t.Fatal("initial binding overwrote an existing invalid marker")
			}
			if _, err := f.source.store.Stamp(f.ctx, retained.Marker, RawMarker{}); !errors.Is(err, ErrConflict) {
				t.Fatalf("absence bypassed a present invalid marker: %v", err)
			}
			f.assertFacts(t, f.source, before)
			if _, err := f.source.store.Stamp(f.ctx, retained.Marker, raw); err != nil {
				t.Fatalf("replace exactly observed inactive marker bytes: %v", err)
			}
			retained = f.capture(t, f.source)
		}
	})

	run("concurrent_stamps_have_one_exact_cas_winner", func(t *testing.T) {
		before := f.capture(t, f.source)
		start := make(chan struct{})
		done := make(chan recoveryStampResult, 2)
		for _, generation := range []string{strings.Repeat("6", 32), strings.Repeat("7", 32)} {
			desired := before.Marker
			desired.GenerationID = generation
			go func() {
				<-start
				value, err := f.source.store.Stamp(f.ctx, desired, before.RawMarker)
				done <- recoveryStampResult{value, err}
			}()
		}
		close(start)
		var winner Marker
		var successes, conflicts int
		for range 2 {
			select {
			case result := <-done:
				if result.err == nil {
					successes++
					winner = result.value
				} else if errors.Is(result.err, ErrConflict) {
					conflicts++
				} else {
					t.Fatalf("concurrent marker CAS failed unexpectedly: %v", result.err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("concurrent marker CAS did not finish")
			}
		}
		retained = f.capture(t, f.source)
		if successes != 1 || conflicts != 1 || retained.Marker != winner {
			t.Fatal("concurrent marker compare-and-swap did not preserve exactly one winner")
		}
		recoveryAssertBusinessFacts(t, before.Facts, retained.Facts)
	})

	run("transactional_stamp_preserves_caller_transaction_ownership", func(t *testing.T) {
		before := f.capture(t, f.source)
		desired := before.Marker
		desired.GenerationID = strings.Repeat("8", 32)
		tx, err := f.source.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal("begin a caller-owned marker transaction")
		}
		defer recoveryRollback(tx)
		if got, err := f.source.store.StampTx(f.ctx, tx, desired, before.RawMarker); err != nil || got != desired {
			t.Fatalf("stamp inside the caller's transaction: %v", err)
		}
		stamped, err := recoveryReadTxRaw(f.ctx, tx)
		encoded, encodeErr := EncodeMarker(desired)
		if err != nil || encodeErr != nil || stamped != (RawMarker{Present: true, Value: encoded}) {
			t.Fatal("StampTx ended the caller's transaction or did not write its marker there")
		}
		if _, err := f.source.store.StampTx(f.ctx, tx, desired, before.RawMarker); !errors.Is(err, ErrConflict) {
			t.Fatalf("transactional stamping bypassed a stale raw CAS: %v", err)
		}
		var one int
		if tx.QueryRow(f.ctx, `SELECT 1`).Scan(&one) != nil || one != 1 {
			t.Fatal("an ordinary CAS refusal ended the caller's transaction")
		}
		if err := tx.Rollback(f.ctx); err != nil {
			t.Fatal("the caller could not roll back its successfully stamped transaction")
		}
		f.assertFacts(t, f.source, before.Facts)
		if f.read(t, f.source) != before.RawMarker {
			t.Fatal("rolling back the caller transaction left a committed marker")
		}
		other, err := f.target.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal("begin an independent target transaction")
		}
		defer recoveryRollback(other)
		if _, err := f.source.store.StampTx(f.ctx, other, desired, before.RawMarker); err == nil {
			t.Fatal("a transaction from another database or role was accepted")
		}
		var actualDatabase, actualRole string
		if other.QueryRow(f.ctx, `SELECT current_database(),current_user`).Scan(&actualDatabase, &actualRole) != nil || actualDatabase != f.target.database || actualRole != f.target.role {
			t.Fatal("rejecting a foreign transaction ended or replaced that transaction")
		}
		if err := other.Rollback(f.ctx); err != nil {
			t.Fatal("the caller could not roll back its rejected foreign transaction")
		}
		f.assertEmpty(t, f.target)
	})

	run("transactional_stamp_rejects_a_repeatable_read_stale_noop", func(t *testing.T) {
		before := f.capture(t, f.source)
		tx, err := f.source.pool.BeginTx(f.ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal("begin the caller's repeatable-read transaction")
		}
		defer recoveryRollback(tx)
		old, err := recoveryReadTxRaw(f.ctx, tx)
		if err != nil || old != before.RawMarker {
			t.Fatal("establish the old marker in the caller's repeatable snapshot")
		}
		next := before.Marker
		next.GenerationID = strings.Repeat("9", 32)
		if _, err := f.source.store.Stamp(f.ctx, next, before.RawMarker); err != nil {
			t.Fatalf("advance the committed marker after the old snapshot: %v", err)
		}
		newer := f.capture(t, f.source)
		if newer.Marker != next || newer.RawMarker == old {
			t.Fatal("the committed marker did not advance independently")
		}
		// A stale snapshot can still see desired == expected. Such an apparent
		// no-op must not count as a successful CAS against the current database.
		if _, err := f.source.store.StampTx(f.ctx, tx, before.Marker, old); !errors.Is(err, ErrInvalid) {
			t.Fatalf("repeatable-read stale no-op was accepted: %v", err)
		}
		stillOld, err := recoveryReadTxRaw(f.ctx, tx)
		var isolation string
		if err != nil || stillOld != old || tx.QueryRow(f.ctx, `SELECT current_setting('transaction_isolation')`).Scan(&isolation) != nil || isolation != "repeatable read" {
			t.Fatal("isolation refusal ended or changed the caller's transaction")
		}
		if err := tx.Rollback(f.ctx); err != nil {
			t.Fatal("the caller could not roll back the rejected repeatable snapshot")
		}
		f.assertFacts(t, f.source, newer.Facts)
		retained = newer
	})

	run("transactional_stamp_rejects_a_different_actual_peer", func(t *testing.T) {
		before := f.capture(t, f.source)
		proxyAddress := recoveryForwardSource(t, f.ctx)
		config := f.source.pool.Config()
		config.MaxConns = 1
		config.MinConns = 0
		config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" && network != "tcp4" || address != "127.0.0.1:15432" {
				return nil, errors.New("unexpected recovery fixture dial destination")
			}
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, "tcp4", proxyAddress)
		}
		proxyPool, err := pgxpool.NewWithConfig(f.ctx, config)
		if err != nil {
			t.Fatal("create the owned forwarding connection pool")
		}
		t.Cleanup(proxyPool.Close)
		tx, err := proxyPool.Begin(f.ctx)
		if err != nil {
			t.Fatal("open a real transaction through the owned loopback forwarder")
		}
		defer recoveryRollback(tx)
		declared := tx.Conn().Config()
		expected := f.source.pool.Config().ConnConfig
		if declared.ConnString() != expected.ConnString() || declared.Host != expected.Host || declared.Port != expected.Port || declared.Database != expected.Database || declared.User != expected.User {
			t.Fatal("the forwarding fixture changed the declared connection identity")
		}
		peer := tx.Conn().PgConn().Conn().RemoteAddr()
		if peer == nil || peer.String() != proxyAddress || peer.String() == "127.0.0.1:15432" {
			t.Fatal("the forwarding fixture did not establish a different actual peer")
		}
		var actualDatabase, actualRole, actualServer string
		var actualPort int
		if tx.QueryRow(f.ctx, `SELECT current_database(),current_user,host(inet_server_addr()),inet_server_port()`).Scan(&actualDatabase, &actualRole, &actualServer, &actualPort) != nil || actualDatabase != f.source.database || actualRole != f.source.role || actualServer != "127.0.0.1" || actualPort != 15432 {
			t.Fatal("the forwarder did not reach the same explicitly owned PostgreSQL database and role")
		}
		if !f.source.lease.Protects(f.source.pool) {
			t.Fatal("the source lease was already lost before testing the distinct peer")
		}
		if f.source.lease.ProtectsTransaction(f.source.pool, tx) {
			t.Fatal("matching declared names concealed a different physical connection peer")
		}
		desired := before.Marker
		desired.GenerationID = strings.Repeat("a", 32)
		if _, err := f.source.store.StampTx(f.ctx, tx, desired, before.RawMarker); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("a transaction through an unleased peer was accepted: %v", err)
		}
		if !f.source.lease.Protects(f.source.pool) {
			t.Fatal("peer refusal invalidated the otherwise live source deployment lease")
		}
		var one int
		if tx.QueryRow(f.ctx, `SELECT 1`).Scan(&one) != nil || one != 1 {
			t.Fatal("rejecting the connection peer ended the caller's transaction")
		}
		if err := tx.Rollback(f.ctx); err != nil {
			t.Fatal("the caller could not roll back the rejected forwarding transaction")
		}
		f.assertFacts(t, f.source, before.Facts)
		if f.read(t, f.source) != before.RawMarker {
			t.Fatal("a refused transaction through the forwarding peer changed source rows")
		}
	})

	run("capture_marker_and_fingerprints_share_one_snapshot", func(t *testing.T) {
		before := f.capture(t, f.source)
		blocker, pid, relation := f.block(t, f.source, "users", "ACCESS EXCLUSIVE")
		defer recoveryRollback(blocker)
		next := before.Marker
		next.GenerationID = recoveryFixtureNextGeneration
		encoded, err := EncodeMarker(next)
		if err != nil {
			t.Fatal("encode next fixture generation")
		}
		if _, err := blocker.Exec(f.ctx, `UPDATE public.server_settings SET value=$1 WHERE key=$2`, encoded, MarkerKey); err != nil {
			t.Fatal("stage the next marker in the blocked fixture transaction")
		}
		if _, err := blocker.Exec(f.ctx, `UPDATE public.users SET name='After capture' WHERE id='recovery-admin'`); err != nil {
			t.Fatal("stage the next business row in the same transaction")
		}
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		done := make(chan recoveryCaptureResult, 1)
		go func() { value, err := f.source.store.Capture(ctx); done <- recoveryCaptureResult{value, err} }()
		f.waitBlocked(t, f.source, pid, relation, "AccessShareLock")
		if err := blocker.Commit(f.ctx); err != nil {
			t.Fatal("commit concurrent marker and business changes")
		}
		captured := recoveryWaitCapture(t, done)
		if captured.err != nil || !reflect.DeepEqual(captured.value, before) {
			t.Fatalf("capture mixed marker and table snapshots: %v", captured.err)
		}
		retained = f.capture(t, f.source)
		if retained.Marker != next || reflect.DeepEqual(retained.Facts.Tables, before.Facts.Tables) {
			t.Fatal("a later capture failed to observe the committed generation and data")
		}
		f.assertFacts(t, f.source, retained.Facts)
	})

	run("reset_rejects_wrong_control_identity_or_retained_claim", func(t *testing.T) {
		active := recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration)
		for name, mutate := range map[string]func(*Retained){
			"database":          func(value *Retained) { value.Database += "_other" },
			"role":              func(value *Retained) { value.Role += "_other" },
			"deployment":        func(value *Retained) { value.Marker.DeploymentID = strings.Repeat("f", 32) },
			"generation":        func(value *Retained) { value.Marker.GenerationID = strings.Repeat("e", 32) },
			"slot":              func(value *Retained) { value.Marker.Slot = lifecycle.DatabaseRecovery },
			"raw_bytes":         func(value *Retained) { value.RawMarker.Value += " " },
			"table_fingerprint": func(value *Retained) { value.Facts.Tables[0].SHA256 = strings.Repeat("0", 64) },
		} {
			t.Run(name, func(t *testing.T) {
				expected := recoveryCloneRetained(t, retained)
				mutate(&expected)
				if err := f.source.store.ResetOwnedTarget(f.ctx, active, expected); err == nil {
					t.Fatal("an inexact retained destruction precondition was accepted")
				}
				f.assertFacts(t, f.source, retained.Facts)
				f.assertNamespace(t, f.source, false)
			})
		}
		for name, mutate := range map[string]func(*lifecycle.State){
			"active_same_slot":          func(value *lifecycle.State) { value.DatabaseSlot = lifecycle.DatabasePrimary },
			"active_foreign_deployment": func(value *lifecycle.State) { value.DeploymentID = strings.Repeat("f", 32) },
			"initial_revision":          func(value *lifecycle.State) { value.Revision = 0 },
			"missing_generation":        func(value *lifecycle.State) { value.GenerationID = "" },
		} {
			t.Run(name, func(t *testing.T) {
				state := active
				mutate(&state)
				if err := f.source.store.ResetOwnedTarget(f.ctx, state, retained); err == nil {
					t.Fatal("an active or foreign local state authorized target reset")
				}
				f.assertFacts(t, f.source, retained.Facts)
				f.assertNamespace(t, f.source, false)
			})
		}
	})

	run("reset_preserves_newer_business_rows", func(t *testing.T) {
		f.exec(t, f.source, `UPDATE public.users SET name='Newer business write' WHERE id='recovery-admin'`)
		newer := f.facts(t, f.source)
		if err := f.source.store.ResetOwnedTarget(f.ctx, recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), retained); !errors.Is(err, ErrConflict) {
			t.Fatalf("stale retained data authorized reset: %v", err)
		}
		f.assertFacts(t, f.source, newer)
		f.assertNamespace(t, f.source, false)
		f.exec(t, f.source, `UPDATE public.users SET name='After capture' WHERE id='recovery-admin'`)
		f.assertFacts(t, f.source, retained.Facts)
	})

	run("reset_rechecks_a_writer_that_commits_during_its_lock_wait", func(t *testing.T) {
		blocker, pid, relation := f.block(t, f.source, "users", "ACCESS SHARE")
		defer recoveryRollback(blocker)
		if _, err := blocker.Exec(f.ctx, `UPDATE public.users SET name='Committed while reset waited' WHERE id='recovery-admin'`); err != nil {
			t.Fatal("stage a newer business transaction before target locking")
		}
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- f.source.store.ResetOwnedTarget(ctx, recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), retained)
		}()
		f.waitBlocked(t, f.source, pid, relation, "AccessExclusiveLock")
		if err := blocker.Commit(f.ctx); err != nil {
			t.Fatal("commit the newer writer while reset is waiting")
		}
		if err := recoveryWaitError(t, done); !errors.Is(err, ErrConflict) {
			t.Fatalf("reset did not recheck the committed writer after obtaining its locks: %v", err)
		}
		var name string
		if f.source.pool.QueryRow(f.ctx, `SELECT name FROM public.users WHERE id='recovery-admin'`).Scan(&name) != nil || name != "Committed while reset waited" {
			t.Fatal("reset erased the writer that committed during its lock wait")
		}
		newer := f.facts(t, f.source)
		if reflect.DeepEqual(newer.Tables, retained.Facts.Tables) {
			t.Fatal("the newer committed business state was not retained")
		}
		f.assertNamespace(t, f.source, false)
		f.exec(t, f.source, `UPDATE public.users SET name='After capture' WHERE id='recovery-admin'`)
		f.assertFacts(t, f.source, retained.Facts)
	})

	run("reset_preserves_extra_catalog_metadata_and_grants", func(t *testing.T) {
		active := recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration)
		f.exec(t, f.source, `COMMENT ON TABLE public.users IS 'owned recovery metadata witness'`)
		if err := f.source.store.ResetOwnedTarget(f.ctx, active, retained); err == nil {
			t.Fatal("reset discarded metadata absent from the retained baseline")
		}
		var comment string
		if f.source.pool.QueryRow(f.ctx, `SELECT pg_catalog.obj_description('public.users'::regclass,'pg_class')`).Scan(&comment) != nil || comment != "owned recovery metadata witness" {
			t.Fatal("refused reset modified the metadata witness")
		}
		f.assertNamespace(t, f.source, false)
		f.exec(t, f.source, `COMMENT ON TABLE public.users IS NULL`)
		f.assertFacts(t, f.source, retained.Facts)
		f.exec(t, f.source, `GRANT SELECT ON public.users TO `+pgx.Identifier{f.target.role}.Sanitize())
		if err := f.source.store.ResetOwnedTarget(f.ctx, active, retained); err == nil {
			t.Fatal("reset discarded an additional database grant")
		}
		var granted bool
		if f.source.pool.QueryRow(f.ctx, `SELECT pg_catalog.has_table_privilege($1,'public.users','SELECT')`, f.target.role).Scan(&granted) != nil || !granted {
			t.Fatal("refused reset removed the additional grant")
		}
		f.assertNamespace(t, f.source, false)
		f.exec(t, f.source, `REVOKE SELECT ON public.users FROM `+pgx.Identifier{f.target.role}.Sanitize())
		f.assertFacts(t, f.source, retained.Facts)
	})

	run("reset_refuses_an_external_dependency_without_cascade", func(t *testing.T) {
		schema := "goby_recovery_dependency_" + f.suffix
		f.exec(t, f.source, `CREATE SCHEMA `+pgx.Identifier{schema}.Sanitize())
		var oid, owner int64
		if f.source.pool.QueryRow(f.ctx, `SELECT oid::bigint,nspowner::bigint FROM pg_catalog.pg_namespace WHERE nspname=$1`, schema).Scan(&oid, &owner) != nil || owner != f.source.roleOID {
			t.Fatal("record ownership of the exact dependency fixture schema")
		}
		view := pgx.Identifier{schema, "retained_user_view"}.Sanitize()
		f.exec(t, f.source, `CREATE VIEW `+view+` AS SELECT id FROM public.users`)
		if err := f.source.store.ResetOwnedTarget(f.ctx, recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), retained); err == nil {
			t.Fatal("reset cascaded into a relation outside the retained schema")
		}
		var count int
		if f.source.pool.QueryRow(f.ctx, `SELECT count(*) FROM `+view).Scan(&count) != nil || count != 1 {
			t.Fatal("refused reset removed or changed the external dependency")
		}
		f.assertNamespace(t, f.source, false)
		var currentOID, currentOwner int64
		if f.source.pool.QueryRow(f.ctx, `SELECT oid::bigint,nspowner::bigint FROM pg_catalog.pg_namespace WHERE nspname=$1`, schema).Scan(&currentOID, &currentOwner) != nil || currentOID != oid || currentOwner != owner {
			t.Fatal("dependency fixture ownership changed before exact cleanup")
		}
		f.exec(t, f.source, `DROP VIEW `+view)
		f.exec(t, f.source, `DROP SCHEMA `+pgx.Identifier{schema}.Sanitize()+` RESTRICT`)
		f.assertFacts(t, f.source, retained.Facts)
	})

	run("reset_cancellation_rolls_back_while_ddl_is_blocked", func(t *testing.T) {
		blocker, pid, relation := f.block(t, f.source, "users", "ACCESS SHARE")
		defer recoveryRollback(blocker)
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		done := make(chan error, 1)
		go func() {
			done <- f.source.store.ResetOwnedTarget(ctx, recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), retained)
		}()
		f.waitBlocked(t, f.source, pid, relation, "AccessExclusiveLock")
		cancel()
		if err := recoveryWaitError(t, done); err == nil {
			t.Fatal("cancelled blocked reset reported success")
		}
		// Keep the blocker held: a returned reset must already have rolled back,
		// including any earlier DDL or table locks in its own transaction.
		f.assertFacts(t, f.source, retained.Facts)
		f.assertNamespace(t, f.source, false)
		f.assertNoBlockedOperation(t, f.source, pid)
	})

	run("lease_loss_cancels_blocked_reset_without_cancelling_the_caller", func(t *testing.T) {
		blocker, pid, relation := f.block(t, f.source, "users", "ACCESS SHARE")
		defer recoveryRollback(blocker)
		ctx, cancel := context.WithCancel(f.ctx)
		defer cancel()
		bound, finishBound, err := f.source.store.BoundContext(ctx)
		if err != nil {
			t.Fatalf("bind the complete operation to its deployment lease: %v", err)
		}
		defer finishBound()
		done := make(chan error, 1)
		go func() {
			done <- f.source.store.ResetOwnedTarget(ctx, recoveryActive(lifecycle.DatabaseRecovery, recoveryFixtureGeneration), retained)
		}()
		f.waitBlocked(t, f.source, pid, relation, "AccessExclusiveLock")
		closed := make(chan error, 1)
		go func() { closed <- f.source.lease.Close() }()
		if err := recoveryWaitError(t, done); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("deployment lease loss did not cancel the blocked operation: %v", err)
		}
		if err := recoveryWaitError(t, closed); err != nil {
			t.Fatal("close the exact owned deployment lease")
		}
		if ctx.Err() != nil || f.source.lease.Protects(f.source.pool) {
			t.Fatal("lease loss was confused with caller cancellation or retained ownership")
		}
		select {
		case <-bound.Done():
		case <-time.After(3 * time.Second):
			t.Fatal("the operation context outlived its lost deployment lease")
		}
		if _, _, err := f.source.store.BoundContext(ctx); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("a closed lease accepted a new operation context: %v", err)
		}
		select {
		case <-f.source.lease.Done():
		default:
			t.Fatal("closed lease did not publish ownership loss")
		}
		f.assertFacts(t, f.source, retained.Facts)
		f.assertNamespace(t, f.source, false)
		f.assertNoBlockedOperation(t, f.source, pid)
		if _, err := f.source.store.Read(f.ctx); !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("a closed lease accepted a subsequent marker read: %v", err)
		}
		f.reacquire(t, f.source)
	})

	run("reset_restore_and_stamp_reuse_only_the_inactive_slot", func(t *testing.T) {
		archive, facts := f.archive(t, f.source)
		if !reflect.DeepEqual(facts, retained.Facts) {
			t.Fatal("retained source changed before the real restore sequence")
		}
		f.assertEmpty(t, f.target)
		if _, err := backuppg.Restore(f.ctx, f.source.pool, f.target.pool, archive, facts, f.source.config.Postgres); err != nil {
			t.Fatalf("restore the real source archive into the independent empty slot: %v", err)
		}
		imported := f.read(t, f.target)
		if imported != retained.RawMarker {
			t.Fatal("restore did not preserve the source's exact archived marker")
		}
		if _, err := f.target.store.Capture(f.ctx); !errors.Is(err, ErrConflict) {
			t.Fatalf("an imported primary marker became a local recovery claim: %v", err)
		}
		if _, err := f.target.store.BindInitial(f.ctx, initial); err == nil {
			t.Fatal("recovery slot accepted initial primary binding")
		}
		targetMarker := Marker{Version: 1, DeploymentID: recoveryFixtureDeployment, GenerationID: strings.Repeat("4", 32), Slot: lifecycle.DatabaseRecovery}
		if _, err := f.target.store.Stamp(f.ctx, targetMarker, imported); err != nil {
			t.Fatalf("stamp the verified inactive restored slot: %v", err)
		}
		targetRetained := f.capture(t, f.target)
		recoveryAssertBusinessFacts(t, facts, targetRetained.Facts)
		f.assertFacts(t, f.source, retained.Facts)
		if err := f.target.store.ResetOwnedTarget(f.ctx, recoveryActive(lifecycle.DatabasePrimary, retained.Marker.GenerationID), targetRetained); err != nil {
			t.Fatalf("reset the unchanged retained inactive slot: %v", err)
		}
		f.assertNamespace(t, f.target, true)
		f.assertEmpty(t, f.target)
		if err := f.target.store.ResetOwnedTarget(f.ctx, recoveryActive(lifecycle.DatabasePrimary, retained.Marker.GenerationID), targetRetained); err == nil {
			t.Fatal("a cleared slot accepted the previous generation's destruction claim")
		}
		f.assertEmpty(t, f.target)
		targetMarker.GenerationID = strings.Repeat("5", 32)
		refused := errors.New("owned recovery finalizer refusal")
		for _, mode := range []string{"refused", "cancelled", "lease_lost", "committed"} {
			if !t.Run("transactional_finalizer_"+mode, func(t *testing.T) {
				if _, err := archive.Seek(0, io.SeekStart); err != nil {
					t.Fatal("rewind the exact source archive before a finalized restore")
				}
				bound, finishBound, err := f.target.store.BoundContext(f.ctx)
				if err != nil {
					t.Fatalf("bind the complete restore and commit to the target lease: %v", err)
				}
				defer finishBound()
				called, stamped := false, false
				result, restoreErr := backuppg.RestoreFinalized(bound, f.source.pool, f.target.pool, archive, facts, f.source.config.Postgres,
					func(callbackCtx context.Context, tx pgx.Tx, rawResult backuppg.RestoreResult) error {
						called = true
						if rawResult.SourceVersion != facts.SchemaVersion || rawResult.CurrentVersion < rawResult.SourceVersion || !reflect.DeepEqual(rawResult.Tables, facts.Tables) {
							return ErrInvalid
						}
						observed, err := recoveryReadTxRaw(callbackCtx, tx)
						if err != nil || observed != retained.RawMarker {
							return ErrConflict
						}
						marker, err := f.target.store.StampTx(callbackCtx, tx, targetMarker, observed)
						if err != nil {
							return err
						}
						if marker != targetMarker {
							return ErrConflict
						}
						written, err := recoveryReadTxRaw(callbackCtx, tx)
						encoded, encodeErr := EncodeMarker(targetMarker)
						if err != nil || encodeErr != nil || written != (RawMarker{Present: true, Value: encoded}) {
							return ErrUnavailable
						}
						stamped = true
						if mode == "refused" {
							return refused
						}
						if mode == "cancelled" || mode == "lease_lost" {
							if mode == "lease_lost" {
								if err := f.target.lease.Close(); err != nil {
									return ErrLeaseLost
								}
							} else {
								finishBound()
							}
							select {
							case <-callbackCtx.Done():
							case <-time.After(3 * time.Second):
								return ErrUnavailable
							}
						}
						return nil
					})
				if !called || !stamped {
					t.Fatalf("finalized restore did not reach successful transactional marker stamping: %v", restoreErr)
				}
				if mode == "committed" {
					if restoreErr != nil || !reflect.DeepEqual(result.Tables, facts.Tables) || result.SourceVersion != facts.SchemaVersion {
						t.Fatalf("restore and generation marker did not commit together: %v", restoreErr)
					}
					targetRetained = f.capture(t, f.target)
					if targetRetained.Marker != targetMarker {
						t.Fatal("committed target retained the imported source marker")
					}
					recoveryAssertBusinessFacts(t, facts, targetRetained.Facts)
				} else {
					want := refused
					if mode == "cancelled" || mode == "lease_lost" {
						want = context.Canceled
					}
					if !errors.Is(restoreErr, want) || result.SourceVersion != 0 || result.CurrentVersion != 0 || len(result.Tables) != 0 {
						t.Fatalf("failed finalizer returned an unexpected error or committed result: %v", restoreErr)
					}
					if mode == "lease_lost" {
						if f.target.lease.Protects(f.target.pool) || f.ctx.Err() != nil {
							t.Fatal("the finalizer lost its caller context or retained a closed lease")
						}
						f.reacquire(t, f.target)
					}
					// This read uses the still-live fixture context, independently of
					// the cancelled restore. No table or marker may have committed.
					f.assertEmpty(t, f.target)
				}
				f.assertFacts(t, f.source, retained.Facts)
			}) {
				t.Fatal("stop reusing the owned target after a failed finalizer assertion")
			}
		}
		targetRetained = f.capture(t, f.target)
		recoveryAssertBusinessFacts(t, facts, targetRetained.Facts)
		// Exercise the original primary's first retirement separately from the
		// noninitial target reset above. This isolated fixture models an initial
		// primary claim; the protected local active state selects the other slot.
		initialMarker, err := EncodeMarker(Marker{Version: 1, DeploymentID: recoveryFixtureDeployment, Slot: lifecycle.DatabasePrimary})
		if err != nil {
			t.Fatal("encode the initial primary retirement fixture")
		}
		f.exec(t, f.source, `UPDATE public.server_settings SET value=$1 WHERE key=$2`, initialMarker, MarkerKey)
		initialRetained := f.capture(t, f.source)
		if initialRetained.Marker.GenerationID != "" {
			t.Fatal("initial primary retirement fixture has a later generation")
		}
		if err := f.source.store.ResetOwnedTarget(f.ctx, recoveryActive(lifecycle.DatabaseRecovery, targetMarker.GenerationID), initialRetained); err != nil {
			t.Fatalf("clear the retired source after activating the other slot: %v", err)
		}
		f.assertNamespace(t, f.source, true)
		f.assertEmpty(t, f.source)
		f.assertFacts(t, f.target, targetRetained.Facts)
		if f.read(t, f.target) != targetRetained.RawMarker {
			t.Fatal("retiring the old source modified the active target marker")
		}
	})
}

type recoveryStoreFixture struct {
	t              *testing.T
	ctx            context.Context
	suffix         string
	source, target *recoveryFixtureDatabase
}

type recoveryFixtureDatabase struct {
	pool                                               *pgxpool.Pool
	lease                                              *database.Lease
	store                                              *Store
	config                                             Config
	database, role, application                        string
	databaseOID, roleOID, namespaceOID, namespaceOwner int64
}

func newRecoveryStoreFixture(t *testing.T) *recoveryStoreFixture {
	t.Helper()
	urls := []string{os.Getenv("GOBY_TEST_BACKUP_SOURCE_DATABASE_URL"), os.Getenv("GOBY_TEST_BACKUP_TARGET_DATABASE_URL")}
	if urls[0] == "" && urls[1] == "" {
		t.Skip("two explicitly disposable recovery databases are required")
	}
	if urls[0] == "" || urls[1] == "" || os.Getenv("GOBY_TEST_BACKUP_DISPOSABLE_DATABASES") != "1" {
		t.Fatal("the complete independently disposable recovery pair is required")
	}
	options := backuppg.Options{PGDump: os.Getenv("GOBY_TEST_PG_DUMP"), PGRestore: os.Getenv("GOBY_TEST_PG_RESTORE"), Schema: "public", Timeout: 30 * time.Second, MaxDumpBytes: 8 << 20, ProbeVersion: 6}
	if options.PGDump == "" || options.PGRestore == "" {
		t.Fatal("explicit real PostgreSQL 17 tool paths are required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	t.Cleanup(cancel)
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("generate fixture ownership suffix")
	}
	f := &recoveryStoreFixture{t: t, ctx: ctx, suffix: hex.EncodeToString(random[:])}
	var configurations []*pgxpool.Config
	namePattern := regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
	for _, raw := range urls {
		uri, err := url.Parse(raw)
		if err != nil || uri.Scheme != "postgresql" && uri.Scheme != "postgres" || uri.Hostname() != "127.0.0.1" || uri.Port() != "15432" || uri.Fragment != "" {
			t.Fatal("recovery fixture is restricted to the isolated 127.0.0.1:15432 cluster")
		}
		query, err := url.ParseQuery(uri.RawQuery)
		if err != nil || len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" {
			t.Fatal("recovery fixture accepts only the explicit local sslmode=disable URI")
		}
		config, err := pgxpool.ParseConfig(raw)
		if err != nil || config.ConnConfig.Host != "127.0.0.1" || config.ConnConfig.Port != 15432 || len(config.ConnConfig.Fallbacks) != 0 || config.ConnConfig.TLSConfig != nil || len(config.ConnConfig.RuntimeParams) != 0 {
			t.Fatal("recovery fixture connection has an unexpected route or session policy")
		}
		if !strings.HasPrefix(config.ConnConfig.Database, "goby_backup_") || !namePattern.MatchString(config.ConnConfig.Database) || !namePattern.MatchString(config.ConnConfig.User) {
			t.Fatal("recovery fixture requires explicit owned database and role names")
		}
		config.MaxConns = 6
		config.ConnConfig.ConnectTimeout = 5 * time.Second
		configurations = append(configurations, config)
	}
	if configurations[0].ConnConfig.Database == configurations[1].ConnConfig.Database || configurations[0].ConnConfig.User == configurations[1].ConnConfig.User {
		t.Fatal("source and target must have distinct database and role identities")
	}
	for index, config := range configurations {
		slot := lifecycle.DatabasePrimary
		if index == 1 {
			slot = lifecycle.DatabaseRecovery
		}
		application := "goby_recoverydb_" + f.suffix + "_" + string(slot)
		config.ConnConfig.RuntimeParams["application_name"] = application
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal("open an owned recovery fixture pool")
		}
		t.Cleanup(pool.Close)
		postgres := options
		postgres.SourceURL = urls[index]
		entry := &recoveryFixtureDatabase{pool: pool, database: config.ConnConfig.Database, role: config.ConnConfig.User, application: application,
			config: Config{Postgres: postgres, DeploymentID: recoveryFixtureDeployment, Slot: slot}}
		f.preflight(t, entry)
		if index == 0 {
			f.source = entry
		} else {
			f.target = entry
		}
	}
	if f.source.databaseOID == f.target.databaseOID || f.source.roleOID == f.target.roleOID {
		t.Fatal("the server did not confirm distinct database and role OIDs")
	}
	// Only after both actual database identities and empty schemas are checked
	// may the fixture acquire leases and perform its first application write.
	f.reacquire(t, f.source)
	f.reacquire(t, f.target)
	return f
}

func (f *recoveryStoreFixture) preflight(t *testing.T, entry *recoveryFixtureDatabase) {
	t.Helper()
	var actualDatabase, actualRole, address, ownerName string
	var port int
	var databaseOwner int64
	var unsafeRole, member bool
	err := entry.pool.QueryRow(f.ctx, `SELECT current_database(),current_user,host(inet_server_addr()),inet_server_port(),
		d.oid::bigint,d.datdba::bigint,r.oid::bigint,n.oid::bigint,n.nspowner::bigint,pg_catalog.pg_get_userbyid(n.nspowner),
		(r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls),
		EXISTS(SELECT 1 FROM pg_catalog.pg_auth_members m WHERE m.member=r.oid)
		FROM pg_catalog.pg_database d JOIN pg_catalog.pg_roles r ON r.rolname=current_user
		CROSS JOIN pg_catalog.pg_namespace n WHERE d.datname=current_database() AND n.nspname='public'`).Scan(
		&actualDatabase, &actualRole, &address, &port, &entry.databaseOID, &databaseOwner, &entry.roleOID, &entry.namespaceOID, &entry.namespaceOwner, &ownerName, &unsafeRole, &member)
	if err != nil || actualDatabase != entry.database || actualRole != entry.role || address != "127.0.0.1" || port != 15432 || databaseOwner != entry.roleOID || unsafeRole || member || entry.namespaceOID <= 0 || entry.namespaceOwner != entry.roleOID && ownerName != "pg_database_owner" {
		t.Fatal("actual recovery fixture identity, namespace ownership, or least privilege was not proven")
	}
	var unexpectedSchemas int
	if entry.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_catalog.pg_namespace WHERE nspname NOT LIKE 'pg_%' AND nspname NOT IN ('public','information_schema')`).Scan(&unexpectedSchemas) != nil || unexpectedSchemas != 0 {
		t.Fatal("the complete disposable recovery database already contains another user schema")
	}
	f.assertNoPublicObjects(t, entry)
}

func (f *recoveryStoreFixture) reacquire(t *testing.T, entry *recoveryFixtureDatabase) {
	t.Helper()
	lease, err := database.AcquireLease(f.ctx, entry.pool)
	if err != nil {
		t.Fatalf("acquire the exact owned database lease: %v", err)
	}
	// A hijacked lease connection is outside pool.Close. Register immediately
	// against the root fixture lifetime, including partial construction failure
	// and leases reacquired inside a sequential subtest.
	f.t.Cleanup(func() {
		if err := lease.Close(); err != nil {
			f.t.Error("close an owned recovery fixture deployment lease")
		}
	})
	store, err := New(entry.pool, lease, entry.config)
	if err != nil {
		_ = lease.Close()
		t.Fatalf("construct recovery database store: %v", err)
	}
	entry.lease, entry.store = lease, store
}

func (f *recoveryStoreFixture) exec(t *testing.T, entry *recoveryFixtureDatabase, statement string, args ...any) {
	t.Helper()
	if _, err := entry.pool.Exec(f.ctx, statement, args...); err != nil {
		t.Fatal("execute the explicitly owned recovery fixture operation")
	}
}

func (f *recoveryStoreFixture) read(t *testing.T, entry *recoveryFixtureDatabase) RawMarker {
	t.Helper()
	value, err := entry.store.Read(f.ctx)
	if err != nil {
		t.Fatalf("read exact recovery marker: %v", err)
	}
	if !value.TablePresent {
		t.Fatal("expected a populated or legacy marker table")
	}
	return value.Raw
}

func (f *recoveryStoreFixture) capture(t *testing.T, entry *recoveryFixtureDatabase) Retained {
	t.Helper()
	value, err := entry.store.Capture(f.ctx)
	if err != nil {
		t.Fatalf("capture locally bound retained database: %v", err)
	}
	if value.Database != entry.database || value.Role != entry.role {
		t.Fatal("capture lost the server's actual database or role identity")
	}
	return value
}

func (f *recoveryStoreFixture) facts(t *testing.T, entry *recoveryFixtureDatabase) backupformat.SourceFacts {
	t.Helper()
	snapshot, err := backuppg.OpenSnapshot(f.ctx, entry.pool, entry.config.Postgres)
	if err != nil {
		t.Fatalf("open independent fingerprint snapshot: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(f.ctx)
	if err != nil {
		t.Fatalf("read independent table fingerprints: %v", err)
	}
	return facts
}

func (f *recoveryStoreFixture) assertFacts(t *testing.T, entry *recoveryFixtureDatabase, expected backupformat.SourceFacts) {
	t.Helper()
	if !reflect.DeepEqual(f.facts(t, entry), expected) {
		t.Fatal("the complete source facts or table fingerprints changed unexpectedly")
	}
}

func (f *recoveryStoreFixture) assertNamespace(t *testing.T, entry *recoveryFixtureDatabase, replaced bool) {
	t.Helper()
	var oid, owner int64
	if entry.pool.QueryRow(f.ctx, `SELECT oid::bigint,nspowner::bigint FROM pg_catalog.pg_namespace WHERE nspname='public'`).Scan(&oid, &owner) != nil || owner != entry.namespaceOwner || oid <= 0 {
		t.Fatal("the expected public namespace owner was not preserved")
	}
	if replaced {
		if oid == entry.namespaceOID {
			t.Fatal("successful target reset did not replace its retired schema")
		}
		entry.namespaceOID = oid
	} else if oid != entry.namespaceOID {
		t.Fatal("a refused operation replaced the public namespace")
	}
}

func (f *recoveryStoreFixture) assertNoPublicObjects(t *testing.T, entry *recoveryFixtureDatabase) {
	t.Helper()
	var count int64
	if entry.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM pg_catalog.pg_class WHERE relnamespace='public'::regnamespace)+
		(SELECT count(*) FROM pg_catalog.pg_proc WHERE pronamespace='public'::regnamespace)+
		(SELECT count(*) FROM pg_catalog.pg_type WHERE typnamespace='public'::regnamespace)+
		(SELECT count(*) FROM pg_catalog.pg_collation WHERE collnamespace='public'::regnamespace)`).Scan(&count) != nil || count != 0 {
		t.Fatal("the independently owned public namespace is not empty")
	}
}

func (f *recoveryStoreFixture) assertEmpty(t *testing.T, entry *recoveryFixtureDatabase) {
	t.Helper()
	value, err := entry.store.InspectEmpty(f.ctx)
	if err != nil || value.Database != entry.database || value.Role != entry.role || value.Schema != "public" {
		t.Fatalf("inspect the independent empty slot: %v", err)
	}
	f.assertNoPublicObjects(t, entry)
	f.assertNamespace(t, entry, false)
	observed, err := entry.store.Read(f.ctx)
	if err != nil || observed.TablePresent || observed.Raw != (RawMarker{}) {
		t.Fatalf("pre-migration empty schema was not distinguished from a legacy table: %v", err)
	}
}

func recoveryReadTxRaw(ctx context.Context, tx pgx.Tx) (RawMarker, error) {
	var value string
	err := tx.QueryRow(ctx, `SELECT value FROM public.server_settings WHERE key=$1`, MarkerKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return RawMarker{}, nil
	}
	if err != nil {
		return RawMarker{}, ErrUnavailable
	}
	return RawMarker{Present: true, Value: value}, nil
}

func (f *recoveryStoreFixture) block(t *testing.T, entry *recoveryFixtureDatabase, table, mode string) (pgx.Tx, int, int64) {
	t.Helper()
	if table != "users" || mode != "ACCESS SHARE" && mode != "ACCESS EXCLUSIVE" {
		t.Fatal("unexpected fixture lock target")
	}
	tx, err := entry.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin owned lock fixture")
	}
	var pid int
	var oid int64
	if tx.QueryRow(f.ctx, `SELECT pg_backend_pid(),'public.users'::regclass::oid::bigint`).Scan(&pid, &oid) != nil {
		recoveryRollback(tx)
		t.Fatal("identify owned blocker and table")
	}
	if _, err := tx.Exec(f.ctx, `LOCK TABLE public.users IN `+mode+` MODE`); err != nil {
		recoveryRollback(tx)
		t.Fatal("acquire the exact owned table lock")
	}
	return tx, pid, oid
}

func (f *recoveryStoreFixture) waitBlocked(t *testing.T, entry *recoveryFixtureDatabase, blockerPID int, relation int64, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		var waiting bool
		err := entry.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_locks l
			JOIN pg_catalog.pg_stat_activity a ON a.pid=l.pid
			WHERE l.locktype='relation' AND l.relation=$1::oid AND l.mode=$2 AND NOT l.granted
			AND a.datname=current_database() AND a.usename=current_user AND a.application_name=$3
			AND l.pid<>$4 AND $4=ANY(pg_catalog.pg_blocking_pids(l.pid)))`, relation, mode, entry.application, blockerPID).Scan(&waiting)
		if err != nil {
			t.Fatal("observe the exact owned database lock wait")
		}
		if waiting {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("the operation never reached the proven owned table-lock boundary")
}

func (f *recoveryStoreFixture) assertNoBlockedOperation(t *testing.T, entry *recoveryFixtureDatabase, blockerPID int) {
	t.Helper()
	var count int
	if entry.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_catalog.pg_stat_activity a
		WHERE a.datname=current_database() AND a.usename=current_user AND a.application_name=$1
		AND a.pid<>$2 AND $2=ANY(pg_catalog.pg_blocking_pids(a.pid))`, entry.application, blockerPID).Scan(&count) != nil || count != 0 {
		t.Fatal("a returned operation retained a blocked database transaction")
	}
}

func (f *recoveryStoreFixture) archive(t *testing.T, entry *recoveryFixtureDatabase) (*os.File, backupformat.SourceFacts) {
	t.Helper()
	snapshot, err := backuppg.OpenSnapshot(f.ctx, entry.pool, entry.config.Postgres)
	if err != nil {
		t.Fatalf("open real archive snapshot: %v", err)
	}
	defer snapshot.Close()
	facts, err := snapshot.Facts(f.ctx)
	if err != nil {
		t.Fatalf("fingerprint real archive snapshot: %v", err)
	}
	file, err := os.OpenFile(filepath.Join(t.TempDir(), "owned-recovery.dump"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("create owned private archive fixture")
	}
	t.Cleanup(func() { _ = file.Close() })
	if err := snapshot.Dump(f.ctx, file); err != nil {
		t.Fatalf("create real PostgreSQL archive: %v", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal("rewind owned archive fixture")
	}
	return file, facts
}

func recoveryActive(slot lifecycle.DatabaseSlot, generation string) lifecycle.State {
	return lifecycle.State{DeploymentID: recoveryFixtureDeployment, Revision: 2, GenerationID: generation, DatabaseSlot: slot, Master: lifecycle.MasterDefault, Digest: strings.Repeat("b", 64)}
}

func recoveryCloneRetained(t *testing.T, value Retained) Retained {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal("copy retained fixture preconditions")
	}
	var clone Retained
	if json.Unmarshal(encoded, &clone) != nil {
		t.Fatal("copy retained fixture preconditions")
	}
	return clone
}

func recoveryAssertBusinessFacts(t *testing.T, source, target backupformat.SourceFacts) {
	t.Helper()
	left, right := append([]backupformat.TableFact(nil), source.Tables...), append([]backupformat.TableFact(nil), target.Tables...)
	if len(left) != len(right) {
		t.Fatal("restored table inventory changed")
	}
	changedMarker := false
	for index := range left {
		if left[index].Name != right[index].Name {
			t.Fatal("restored table identity changed")
		}
		if left[index].Name == "server_settings" {
			changedMarker = left[index].SHA256 != right[index].SHA256
			continue
		}
		if left[index] != right[index] {
			t.Fatal("stamping a recovery marker changed an archived business table")
		}
	}
	if !changedMarker {
		t.Fatal("the local generation marker was not included in the captured table fingerprints")
	}
}

type recoveryCaptureResult struct {
	value Retained
	err   error
}

type recoveryStampResult struct {
	value Marker
	err   error
}

func recoveryWaitCapture(t *testing.T, done <-chan recoveryCaptureResult) recoveryCaptureResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(8 * time.Second):
		t.Fatal("owned capture did not finish after its blocker was released")
	}
	return recoveryCaptureResult{}
}

func recoveryWaitError(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(8 * time.Second):
		t.Fatal("owned database operation did not finish within the cancellation bound")
	}
	return nil
}

func recoveryRollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// recoveryForwardSource owns one transparent connection and forwards only to
// the isolated test cluster. It never logs, decodes, or persists protocol bytes.
// The returned endpoint differs physically while pgx's declared URI stays the
// same, proving the lease check is stronger than database and role names.
func recoveryForwardSource(t *testing.T, parent context.Context) string {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("open the owned loopback forwarding listener")
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.Equal(net.ParseIP("127.0.0.1")) || address.Port == 15432 || address.Port == 5432 || address.Port < 1 {
		_ = listener.Close()
		t.Fatal("the owned forwarding listener selected an unexpected endpoint")
	}
	ctx, cancel := context.WithCancel(parent)
	forwarder := &recoverySingleForwarder{listener: listener, done: make(chan struct{})}
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		forwarder.mu.Lock()
		forwarder.closing = true
		connections := append([]net.Conn(nil), forwarder.connections...)
		forwarder.mu.Unlock()
		for _, connection := range connections {
			_ = connection.Close()
		}
		select {
		case <-forwarder.done:
		case <-time.After(5 * time.Second):
			t.Error("the owned forwarding connection did not finish cleanup")
		}
	})
	go forwarder.run(ctx)
	return address.String()
}

type recoverySingleForwarder struct {
	listener    net.Listener
	done        chan struct{}
	mu          sync.Mutex
	closing     bool
	connections []net.Conn
}

func (forwarder *recoverySingleForwarder) retain(connection net.Conn) bool {
	forwarder.mu.Lock()
	defer forwarder.mu.Unlock()
	if forwarder.closing {
		_ = connection.Close()
		return false
	}
	forwarder.connections = append(forwarder.connections, connection)
	return true
}

func (forwarder *recoverySingleForwarder) run(ctx context.Context) {
	defer close(forwarder.done)
	client, err := forwarder.listener.Accept()
	if err != nil || !forwarder.retain(client) {
		return
	}
	defer client.Close()
	dialer := net.Dialer{Timeout: 3 * time.Second}
	upstream, err := dialer.DialContext(ctx, "tcp4", "127.0.0.1:15432")
	if err != nil || !forwarder.retain(upstream) {
		return
	}
	defer upstream.Close()
	finished := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, client); finished <- struct{}{} }()
	go func() { _, _ = io.Copy(client, upstream); finished <- struct{}{} }()
	<-finished
	_ = client.Close()
	_ = upstream.Close()
	<-finished
}
