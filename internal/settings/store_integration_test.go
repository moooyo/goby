package settings

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type settingsTestProber struct{}

func (settingsTestProber) ProbeFile(context.Context, *os.File) (media.Info, error) {
	return media.Info{}, errors.New("settings repository tests must not start media probes")
}

func settingsTestID(t *testing.T) string {
	t.Helper()
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(raw[:])
}

func settingsTestDefaults() Values {
	return Values{ServerName: "Deployment Alpha", MaxBitrate: config.DefaultMaxBitrate,
		MaxWidth: config.DefaultMaxWidth, MaxHeight: config.DefaultMaxHeight, MaxAudioChannels: config.DefaultMaxAudioChannels}
}

func settingsTestPointer[T any](value T) *T { return &value }

func settingsRepository(t *testing.T) (context.Context, *pgxpool.Pool, *library.Store, *Store, Actor) {
	t.Helper()
	url := os.Getenv("GOBY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL settings integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("create settings integration database connection")
	}
	t.Cleanup(admin.Close)
	schemaName := "goby_settings_test_" + settingsTestID(t)
	schema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, finish := context.WithTimeout(context.Background(), 15*time.Second)
		defer finish()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("remove owned settings schema: %v", err)
		}
	})
	options, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("parse settings integration database connection")
	}
	options.ConnConfig.RuntimeParams["search_path"] = schemaName
	options.ConnConfig.RuntimeParams["timezone"] = "Asia/Shanghai"
	options.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, options)
	if err != nil {
		t.Fatal("create isolated settings database pool")
	}
	t.Cleanup(pool.Close)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	scanner, err := library.New(pool, settingsTestProber{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, finish := context.WithTimeout(context.Background(), 15*time.Second)
		defer finish()
		if err := scanner.Close(cleanup); err != nil {
			t.Errorf("close settings test owner: %v", err)
		}
	})
	store, err := New(ctx, pool, scanner, settingsTestDefaults())
	if err != nil {
		t.Fatal(err)
	}
	userID, sessionID := settingsTestID(t), settingsTestID(t)
	digest := sha256.Sum256([]byte(sessionID))
	if _, err := pool.Exec(ctx, `INSERT INTO users
		(id,name,normalized_name,password_hash,is_administrator)
		VALUES ($1,'Settings Administrator','settings administrator','',true)`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions(id,user_id,token_hash,kind,expires_at)
		VALUES ($1,$2,$3,'admin',clock_timestamp()+interval '1 hour')`, sessionID, userID, digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO server_settings(key,value)
		VALUES ('server_id','settings-stable-server'),('setup_completed','true')`); err != nil {
		t.Fatal(err)
	}
	actor := Actor{Principal: identity.Principal{Kind: "admin", SessionID: sessionID,
		User: identity.User{ID: userID, IsAdministrator: true}}, Audience: identity.AdministratorNative}
	return ctx, pool, scanner, store, actor
}

func settingsRowSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(m)::text FROM managed_settings m WHERE id=1`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func settingsIdentitySnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	if err := pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'legacy', (SELECT jsonb_agg(to_jsonb(s) ORDER BY key) FROM server_settings s),
		'users', (SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM users u),
		'sessions', (SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM sessions a))::text`).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestManagedSettingsOverridesResetAndReloadPreserveDefaultSources(t *testing.T) {
	ctx, pool, owner, store, actor := settingsRepository(t)
	initial := store.Snapshot()
	identities := settingsIdentitySnapshot(t, ctx, pool)
	if initial.Revision != 1 || initial.Overrides != (Overrides{}) || initial.Effective != settingsTestDefaults() || initial.UpdatedAt.Location() != time.UTC {
		t.Fatal("initial settings did not retain null overrides, startup defaults, or UTC time")
	}
	name, bitrate, height := "Living Room", int64(9_000_000), 720
	result, err := store.Update(ctx, actor, UpdateRequest{Revision: 1,
		Overrides: Overrides{ServerName: &name, MaxBitrate: &bitrate, MaxHeight: &height}})
	if err != nil || result.Revision != 2 || result.Effective.ServerName != name || result.Effective.MaxBitrate != bitrate ||
		result.Effective.MaxHeight != height || result.Effective.MaxWidth != initial.Defaults.MaxWidth {
		t.Fatalf("mixed overrides did not become effective: %v", err)
	}
	name, bitrate, height = "Caller changed input", 1, 1
	*result.Overrides.ServerName = "Caller changed output"
	*result.Overrides.MaxBitrate = 2
	published := store.Snapshot()
	if published.Effective.ServerName != "Living Room" || *published.Overrides.ServerName != "Living Room" || published.Effective.MaxBitrate != 9_000_000 || *published.Overrides.MaxBitrate != 9_000_000 {
		t.Fatal("input or returned override pointers changed published state")
	}
	get, err := store.Get(ctx, actor)
	if err != nil || !reflect.DeepEqual(get, published) {
		t.Fatalf("management GET differs from the runtime snapshot: %v", err)
	}
	beforeReload := settingsRowSnapshot(t, ctx, pool)
	newDefaults := Values{ServerName: "Deployment Beta", MaxBitrate: 30_000_000, MaxWidth: 3840, MaxHeight: 2160, MaxAudioChannels: 6}
	reloaded, err := New(ctx, pool, owner, newDefaults)
	if err != nil {
		t.Fatal(err)
	}
	if settingsRowSnapshot(t, ctx, pool) != beforeReload {
		t.Fatal("startup reload rewrote persisted overrides or their timestamps")
	}
	afterReload := reloaded.Snapshot()
	if afterReload.Revision != 2 || afterReload.Effective.ServerName != "Living Room" || afterReload.Effective.MaxBitrate != 9_000_000 || afterReload.Effective.MaxHeight != 720 ||
		afterReload.Effective.MaxWidth != 3840 || afterReload.Effective.MaxAudioChannels != 6 || afterReload.Defaults != newDefaults {
		t.Fatal("reload did not distinguish explicit overrides from new startup defaults")
	}
	reset, err := reloaded.Reset(ctx, actor, ResetRequest{Revision: 2, Fields: []Field{FieldServerName, FieldMaxBitrate}})
	if err != nil || reset.Revision != 3 || reset.Overrides.ServerName != nil || reset.Overrides.MaxBitrate != nil ||
		reset.Effective.ServerName != newDefaults.ServerName || reset.Effective.MaxBitrate != newDefaults.MaxBitrate || reset.Overrides.MaxHeight == nil {
		t.Fatalf("selective reset did not resume only the chosen defaults: %v", err)
	}
	noop, err := reloaded.Reset(ctx, actor, ResetRequest{Revision: 3, Fields: []Field{FieldServerName}})
	if err != nil || !reflect.DeepEqual(noop, reset) {
		t.Fatalf("no-op reset rewrote an unchanged revision: %v", err)
	}
	cleared, err := reloaded.Update(ctx, actor, UpdateRequest{Revision: 3})
	if err != nil || cleared.Revision != 4 || cleared.Overrides != (Overrides{}) || cleared.Effective != newDefaults {
		t.Fatalf("complete null override replacement did not restore defaults: %v", err)
	}
	if settingsIdentitySnapshot(t, ctx, pool) != identities {
		t.Fatal("settings operations changed legacy identity state")
	}
}

func TestManagedSettingsExplicitDefaultRemainsDistinctAndStaleNoopIsRejected(t *testing.T) {
	ctx, _, _, store, actor := settingsRepository(t)
	initial := store.Snapshot()
	name := initial.Defaults.ServerName
	result, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: &name}})
	if err != nil || result.Revision != 2 || result.Effective != initial.Effective || result.Overrides.ServerName == nil {
		t.Fatalf("explicit default value was collapsed into absence: %v", err)
	}
	if _, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: &name}}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale no-op bypassed revision checking: %v", err)
	}
	result, err = store.Reset(ctx, actor, ResetRequest{Revision: 2, Fields: []Field{FieldServerName}})
	if err != nil || result.Revision != 3 || result.Effective != initial.Effective || result.Overrides.ServerName != nil {
		t.Fatalf("reset did not remove explicit-default provenance: %v", err)
	}
}

func TestManagedSettingsConcurrentCASPublishesOnlyTheCommittedWinner(t *testing.T) {
	ctx, pool, _, store, actor := settingsRepository(t)
	const count = 16
	type outcome struct {
		snapshot Snapshot
		err      error
	}
	results := make(chan outcome, count)
	var workers sync.WaitGroup
	for index := range count {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			name := fmt.Sprintf("Concurrent %d", index)
			value, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: &name}})
			results <- outcome{value, err}
		}(index)
	}
	workers.Wait()
	close(results)
	winners, conflicts := 0, 0
	var winning Snapshot
	for result := range results {
		switch {
		case result.err == nil:
			winning = result.snapshot
			winners++
		case errors.Is(result.err, ErrRevisionConflict):
			conflicts++
		default:
			t.Fatalf("concurrent settings write returned unexpected error: %v", result.err)
		}
	}
	if winners != 1 || conflicts != count-1 || winning.Revision != 2 || !reflect.DeepEqual(store.Snapshot(), winning) {
		t.Fatalf("CAS/publication lost its single winner: winners=%d conflicts=%d", winners, conflicts)
	}
	var revision int64
	var name string
	if err := pool.QueryRow(ctx, `SELECT revision,server_name FROM managed_settings WHERE id=1`).Scan(&revision, &name); err != nil {
		t.Fatal(err)
	}
	if revision != winning.Revision || name != winning.Effective.ServerName {
		t.Fatal("runtime winner differs from the committed database winner")
	}
}

func TestManagedSettingsInvalidMixedRequestsAndResetSelectionsLeaveStateUntouched(t *testing.T) {
	ctx, pool, _, store, actor := settingsRepository(t)
	baseline := settingsRowSnapshot(t, ctx, pool)
	initial := store.Snapshot()
	validName := "Must not be partly saved"
	for _, test := range []struct {
		name      string
		overrides Overrides
	}{
		{"empty-name", Overrides{ServerName: settingsTestPointer("")}},
		{"whitespace-name", Overrides{ServerName: settingsTestPointer(" \t ")}},
		{"long-name", Overrides{ServerName: settingsTestPointer(strings.Repeat("a", 129))}},
		{"zero-bitrate", Overrides{ServerName: &validName, MaxBitrate: settingsTestPointer(int64(0))}},
		{"large-bitrate", Overrides{MaxBitrate: settingsTestPointer(int64(1_000_000_001))}},
		{"zero-width", Overrides{MaxWidth: settingsTestPointer(0)}},
		{"large-width", Overrides{MaxWidth: settingsTestPointer(8193)}},
		{"zero-height", Overrides{MaxHeight: settingsTestPointer(0)}},
		{"large-height", Overrides{MaxHeight: settingsTestPointer(8193)}},
		{"zero-channels", Overrides{MaxAudioChannels: settingsTestPointer(0)}},
		{"large-channels", Overrides{MaxAudioChannels: settingsTestPointer(9)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: test.overrides}); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("invalid override returned %v", err)
			}
		})
	}
	for _, fields := range [][]Field{nil, {}, {FieldServerName, FieldServerName}, {"DatabaseURL"}} {
		if _, err := store.Reset(ctx, actor, ResetRequest{Revision: 1, Fields: fields}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid reset selection returned %v", err)
		}
	}
	for _, revision := range []int64{0, -1, math.MaxInt64} {
		if _, err := store.Update(ctx, actor, UpdateRequest{Revision: revision}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid revision returned %v", err)
		}
	}
	if settingsRowSnapshot(t, ctx, pool) != baseline || !reflect.DeepEqual(store.Snapshot(), initial) {
		t.Fatal("a rejected settings request partially changed persistence or runtime state")
	}
}

func TestManagedSettingsAuthorityAndAudiencesAreRechecked(t *testing.T) {
	ctx, pool, _, store, actor := settingsRepository(t)
	initial := store.Snapshot()
	name := "Denied setting"
	wrongAudience := actor
	wrongAudience.Audience = identity.AdministratorEmby
	for _, candidate := range []Actor{{}, wrongAudience} {
		if _, err := store.Get(ctx, candidate); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("invalid settings read audience returned %v", err)
		}
		if _, err := store.Update(ctx, candidate, UpdateRequest{Revision: initial.Revision, Overrides: Overrides{ServerName: &name}}); !errors.Is(err, identity.ErrUnauthorized) {
			t.Fatalf("invalid settings write audience returned %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.Principal.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Reset(ctx, actor, ResetRequest{Revision: 1, Fields: []Field{FieldServerName}}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked actor retained settings authority: %v", err)
	}
	if !reflect.DeepEqual(store.Snapshot(), initial) {
		t.Fatal("rejected authority changed runtime settings")
	}
}

type settingsHookOwner struct {
	owner       library.OwnedTransactions
	afterWrite  func(library.OwnedTx) error
	afterCommit func()
}

func (owner settingsHookOwner) WithOwnedTx(ctx context.Context, callback func(library.OwnedTx) error) error {
	wrote := false
	err := owner.owner.WithOwnedTx(ctx, func(tx library.OwnedTx) error {
		return callback(settingsHookTx{OwnedTx: tx, after: func() error {
			wrote = true
			if owner.afterWrite != nil {
				return owner.afterWrite(tx)
			}
			return nil
		}})
	})
	if err == nil && wrote && owner.afterCommit != nil {
		owner.afterCommit()
	}
	return err
}

type settingsHookTx struct {
	library.OwnedTx
	after func() error
}

func (tx settingsHookTx) QueryRow(statement string, args ...any) library.OwnedRow {
	row := tx.OwnedTx.QueryRow(statement, args...)
	if strings.HasPrefix(strings.TrimSpace(statement), "UPDATE managed_settings SET") {
		return settingsHookRow{row: row, after: tx.after}
	}
	return row
}

type settingsHookRow struct {
	row   library.OwnedRow
	after func() error
}

func (row settingsHookRow) Scan(dest ...any) error {
	if err := row.row.Scan(dest...); err != nil {
		return err
	}
	return row.after()
}

func TestManagedSettingsCancelledCallerStillPublishesItsCommittedRevision(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	caller, cancel := context.WithCancel(ctx)
	defer cancel()
	var reached atomic.Bool
	store, err := New(ctx, pool, settingsHookOwner{owner: owner, afterWrite: func(library.OwnedTx) error {
		reached.Store(true)
		cancel()
		return nil
	}}, settingsTestDefaults())
	if err != nil {
		t.Fatal(err)
	}
	name := "Committed despite disconnected caller"
	result, err := store.Update(caller, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: &name}})
	if err != nil || !reached.Load() || caller.Err() != context.Canceled || result.Revision != 2 || !reflect.DeepEqual(result, store.Snapshot()) {
		t.Fatalf("cancelled caller prevented committed publication: %v", err)
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM managed_settings WHERE id=1`).Scan(&revision); err != nil || revision != result.Revision {
		t.Fatalf("successful settings response preceded its committed revision: %v", err)
	}
}

func TestManagedSettingsFinalAuthorizationAndPostWriteFailureNeverPublish(t *testing.T) {
	for _, expiry := range []bool{false, true} {
		name := "post-write-error"
		if expiry {
			name = "expired-before-commit"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, owner, _, actor := settingsRepository(t)
			if expiry {
				if _, err := pool.Exec(ctx, `UPDATE sessions SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, actor.Principal.SessionID); err != nil {
					t.Fatal(err)
				}
			}
			failure := errors.New("synthetic post-write failure")
			var reached atomic.Bool
			store, err := New(ctx, pool, settingsHookOwner{owner: owner, afterWrite: func(tx library.OwnedTx) error {
				reached.Store(true)
				if !expiry {
					return failure
				}
				_, err := tx.Exec(`SELECT pg_sleep((GREATEST(0,EXTRACT(EPOCH FROM
					(expires_at-clock_timestamp())))+0.03)::double precision) FROM sessions WHERE id=$1`, actor.Principal.SessionID)
				return err
			}}, settingsTestDefaults())
			if err != nil {
				t.Fatal(err)
			}
			initial, row := store.Snapshot(), settingsRowSnapshot(t, ctx, pool)
			value := "Must roll back"
			_, err = store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: &value}})
			want := failure
			if expiry {
				want = identity.ErrUnauthorized
			}
			if !reached.Load() || !errors.Is(err, want) {
				t.Fatalf("post-write rejection returned %v", err)
			}
			if settingsRowSnapshot(t, ctx, pool) != row || !reflect.DeepEqual(store.Snapshot(), initial) {
				t.Fatal("failed transaction published or retained uncommitted settings")
			}
		})
	}
}

func TestManagedSettingsGetWaitsForPostCommitPublication(t *testing.T) {
	ctx, pool, owner, _, actor := settingsRepository(t)
	committed, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	store, err := New(ctx, pool, settingsHookOwner{owner: owner, afterCommit: func() {
		close(committed)
		select {
		case <-release:
		case <-ctx.Done():
		}
	}}, settingsTestDefaults())
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		value Snapshot
		err   error
	}
	writeDone, getDone := make(chan outcome, 1), make(chan outcome, 1)
	go func() {
		name := "Publication barrier"
		result, err := store.Update(ctx, actor, UpdateRequest{Revision: 1, Overrides: Overrides{ServerName: &name}})
		writeDone <- outcome{result, err}
	}()
	select {
	case <-committed:
	case <-ctx.Done():
		t.Fatal("write did not reach its post-commit publication barrier")
	}
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM managed_settings WHERE id=1`).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("barrier was not after commit: %v", err)
	}
	if store.Snapshot().Revision != 1 {
		t.Fatal("test barrier did not pause publication")
	}
	go func() { value, err := store.Get(ctx, actor); getDone <- outcome{value, err} }()
	once.Do(func() { close(release) })
	var write, get outcome
	select {
	case write = <-writeDone:
	case <-ctx.Done():
		t.Fatal("write did not complete after publication release")
	}
	select {
	case get = <-getDone:
	case <-ctx.Done():
		t.Fatal("GET did not complete after publication release")
	}
	if write.err != nil || get.err != nil || write.value.Revision != 2 || !reflect.DeepEqual(get.value, write.value) || !reflect.DeepEqual(store.Snapshot(), write.value) {
		t.Fatalf("successful response returned unpublished state: write=%v get=%v", write.err, get.err)
	}
}
