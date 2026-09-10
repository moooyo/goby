package recoverydb

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/backuppg"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/lifecycle"
)

// Store binds a fixed local deployment slot to one leased pool. The coordinator
// must quiesce the inactive database and supply state from its protected control
// store; neither an archive nor an HTTP payload can establish local ownership.
type Store struct {
	pool   *pgxpool.Pool
	lease  *database.Lease
	config Config
}

func New(pool *pgxpool.Pool, lease *database.Lease, cfg Config) (*Store, error) {
	if pool == nil || lease == nil || !lease.Protects(pool) {
		return nil, ErrLeaseLost
	}
	probe := Marker{Version: 1, DeploymentID: cfg.DeploymentID, GenerationID: "00000000000000000000000000000000", Slot: cfg.Slot}
	if _, err := EncodeMarker(probe); err != nil {
		return nil, ErrInvalid
	}
	if cfg.Postgres.Schema != "" && cfg.Postgres.Schema != "public" {
		return nil, ErrInvalid
	}
	cfg.Postgres.Schema = "public"
	if cfg.Postgres.SourceURL == "" {
		cfg.Postgres.SourceURL = pool.Config().ConnString()
	}
	if cfg.Postgres.SourceURL != pool.Config().ConnString() {
		return nil, ErrInvalid
	}
	if cfg.Postgres.Timeout == 0 {
		cfg.Postgres.Timeout = 5 * time.Minute
	}
	if cfg.Postgres.Timeout < time.Second || cfg.Postgres.Timeout > 30*time.Minute || cfg.Postgres.ProbeVersion < 1 {
		return nil, ErrInvalid
	}
	return &Store{pool: pool, lease: lease, config: cfg}, nil
}

// BoundContext binds an entire multi-step operation, including a restore
// finalizer and its subsequent commit, to the matching lease's lifetime. The
// returned cancellation function must be called; it also joins the watcher.
func (store *Store) BoundContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if store == nil || !store.lease.Protects(store.pool) {
		return nil, nil, ErrLeaseLost
	}
	bounded, cancel := context.WithTimeout(ctx, store.config.Postgres.Timeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-store.lease.Done():
			cancel()
		case <-bounded.Done():
		}
	}()
	finish := func() { cancel(); <-done }
	if !store.lease.Protects(store.pool) {
		finish()
		return nil, nil, ErrLeaseLost
	}
	return bounded, finish, nil
}

func (store *Store) check(ctx context.Context) error {
	if !store.lease.Protects(store.pool) {
		return ErrLeaseLost
	}
	return ctx.Err()
}

func (store *Store) Read(ctx context.Context) (ReadResult, error) {
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return ReadResult{}, err
	}
	defer finish()
	tx, err := store.pool.BeginTx(bound, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return ReadResult{}, store.operationError(bound, err)
	}
	defer rollback(tx)
	if !store.lease.ProtectsTransaction(store.pool, tx) {
		return ReadResult{}, ErrLeaseLost
	}
	identity, err := backuppg.InspectBindingTransaction(bound, tx, "public")
	if err != nil {
		return ReadResult{}, store.operationError(bound, err)
	}
	if err := store.matchesIdentity(identity); err != nil {
		return ReadResult{}, err
	}
	result, err := readRaw(bound, tx)
	if err != nil {
		return ReadResult{}, store.operationError(bound, err)
	}
	if !result.TablePresent {
		if _, err := backuppg.InspectEmptyRecoveryTransaction(bound, tx, "public"); err != nil {
			return ReadResult{}, store.operationError(bound, err)
		}
	}
	if err := store.check(bound); err != nil {
		return ReadResult{}, err
	}
	return result, nil
}

// BindInitial permits only the configured primary at local lifecycle revision
// zero to create an absent binding. It never replaces a foreign existing row.
func (store *Store) BindInitial(ctx context.Context, state lifecycle.State) (Marker, error) {
	if store == nil || state.Revision != 0 || state.GenerationID != "" || state.DatabaseSlot != lifecycle.DatabasePrimary || state.Master != lifecycle.MasterDefault || store.config.Slot != lifecycle.DatabasePrimary || state.DeploymentID != store.config.DeploymentID {
		return Marker{}, ErrInvalid
	}
	desired := Marker{Version: 1, DeploymentID: store.config.DeploymentID, Slot: lifecycle.DatabasePrimary}
	encoded, err := EncodeMarker(desired)
	if err != nil {
		return Marker{}, err
	}
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return Marker{}, err
	}
	defer finish()
	tx, err := store.pool.BeginTx(bound, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	defer rollback(tx)
	if err := store.prepareWrite(bound, tx); err != nil {
		return Marker{}, err
	}
	observed, err := readRaw(bound, tx)
	if err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	if !observed.TablePresent {
		return Marker{}, ErrConflict
	}
	if observed.Raw.Present {
		current, err := DecodeMarker(observed.Raw.Value)
		if err != nil || current != desired {
			return Marker{}, ErrConflict
		}
	} else if _, err := tx.Exec(bound, `INSERT INTO "public"."server_settings"(key,value) VALUES($1,$2)`, MarkerKey, encoded); err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	if err := store.check(bound); err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(bound); err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	return desired, nil
}

// Stamp accepts a raw value observed locally after the inactive target was
// verified and normalized. A stale expectation always conflicts, even when a
// concurrent caller happened to stamp the same desired marker.
func (store *Store) Stamp(ctx context.Context, desired Marker, expected RawMarker) (Marker, error) {
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return Marker{}, err
	}
	defer finish()
	tx, err := store.pool.BeginTx(bound, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	defer rollback(tx)
	marker, err := store.StampTx(bound, tx, desired, expected)
	if err != nil {
		return Marker{}, err
	}
	if err := store.check(bound); err != nil {
		return Marker{}, err
	}
	if err := tx.Commit(bound); err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	return marker, nil
}

// StampTx never commits or rolls back the caller's transaction. Use BoundContext
// for the whole surrounding restore so lease loss also cancels work after this
// method returns and before the outer transaction commits.
func (store *Store) StampTx(ctx context.Context, tx pgx.Tx, desired Marker, expected RawMarker) (Marker, error) {
	if store == nil || tx == nil || desired.DeploymentID != store.config.DeploymentID || desired.Slot != store.config.Slot || desired.GenerationID == "" || ValidateRawMarker(expected) != nil {
		return Marker{}, ErrInvalid
	}
	encoded, err := EncodeMarker(desired)
	if err != nil {
		return Marker{}, err
	}
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return Marker{}, err
	}
	defer finish()
	if err := store.prepareWrite(bound, tx); err != nil {
		return Marker{}, err
	}
	observed, err := readRaw(bound, tx)
	if err != nil {
		return Marker{}, store.operationError(bound, err)
	}
	if !observed.TablePresent || observed.Raw != expected {
		return Marker{}, ErrConflict
	}
	if !expected.Present {
		if _, err := tx.Exec(bound, `INSERT INTO "public"."server_settings"(key,value) VALUES($1,$2)`, MarkerKey, encoded); err != nil {
			return Marker{}, store.operationError(bound, err)
		}
	} else if expected.Value != encoded {
		if _, err := tx.Exec(bound, `UPDATE "public"."server_settings" SET value=$2,updated_at=clock_timestamp() WHERE key=$1 AND value=$3`, MarkerKey, encoded, expected.Value); err != nil {
			return Marker{}, store.operationError(bound, err)
		}
	}
	if err := store.check(bound); err != nil {
		return Marker{}, err
	}
	return desired, nil
}

func (store *Store) prepareWrite(ctx context.Context, tx pgx.Tx) error {
	if err := store.check(ctx); err != nil {
		return err
	}
	if !store.lease.ProtectsTransaction(store.pool, tx) {
		return ErrLeaseLost
	}
	var isolation string
	if tx.QueryRow(ctx, `SELECT current_setting('transaction_isolation')`).Scan(&isolation) != nil {
		return store.operationError(ctx, ErrUnavailable)
	}
	if isolation != "read committed" {
		return ErrInvalid
	}
	identity, err := backuppg.InspectBindingTransaction(ctx, tx, "public")
	if err != nil {
		return store.operationError(ctx, err)
	}
	if err := store.matchesIdentity(identity); err != nil {
		return err
	}
	inspection, err := backuppg.InspectRecoveryTransaction(ctx, tx, "public")
	if err != nil {
		return store.operationError(ctx, err)
	}
	if err := store.matchesIdentity(inspection.Identity()); err != nil {
		return err
	}
	// A table lock also serializes absent-row CAS, which SELECT FOR UPDATE alone
	// cannot do. Ordinary marker and server-setting writes cannot slip between
	// the raw comparison and its INSERT or UPDATE.
	if _, err := tx.Exec(ctx, `LOCK TABLE "public"."server_settings" IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return store.operationError(ctx, err)
	}
	return store.check(ctx)
}

func (store *Store) Capture(ctx context.Context) (Retained, error) {
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return Retained{}, err
	}
	defer finish()
	snapshot, err := backuppg.OpenSnapshot(bound, store.pool, store.config.Postgres)
	if err != nil {
		return Retained{}, store.operationError(bound, err)
	}
	defer snapshot.Close()
	if !store.lease.ProtectsTransaction(store.pool, snapshot.Tx()) {
		return Retained{}, ErrLeaseLost
	}
	inspection, err := backuppg.InspectRecoveryTransaction(snapshot.Context(), snapshot.Tx(), "public")
	if err != nil {
		return Retained{}, store.operationError(bound, err)
	}
	identity := inspection.Identity()
	if err := store.matchesIdentity(identity); err != nil {
		return Retained{}, err
	}
	observed, err := readRaw(snapshot.Context(), snapshot.Tx())
	if err != nil {
		return Retained{}, store.operationError(bound, err)
	}
	marker, err := store.localMarker(observed.Raw)
	if err != nil || !observed.TablePresent {
		return Retained{}, ErrConflict
	}
	facts, err := snapshot.Facts(bound)
	if err != nil {
		return Retained{}, store.operationError(bound, err)
	}
	if err := store.check(bound); err != nil {
		return Retained{}, err
	}
	return Retained{Marker: marker, RawMarker: observed.Raw, Database: identity.Database, Role: identity.Role, Facts: facts}, nil
}

func (store *Store) InspectEmpty(ctx context.Context) (Empty, error) {
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return Empty{}, err
	}
	defer finish()
	tx, err := store.pool.BeginTx(bound, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return Empty{}, store.operationError(bound, err)
	}
	defer rollback(tx)
	if !store.lease.ProtectsTransaction(store.pool, tx) {
		return Empty{}, ErrLeaseLost
	}
	identity, err := backuppg.InspectEmptyRecoveryTransaction(bound, tx, "public")
	if err != nil {
		return Empty{}, store.operationError(bound, err)
	}
	if err := store.matchesIdentity(identity); err != nil {
		return Empty{}, err
	}
	if err := store.check(bound); err != nil {
		return Empty{}, err
	}
	return Empty{Database: identity.Database, Role: identity.Role, Schema: identity.Schema}, nil
}

// ResetOwnedTarget consumes a retained proof from protected local control.
// active must be its current local lifecycle state, never request/archive data.
// It cannot select the active slot or silently adopt a foreign database claim.
func (store *Store) ResetOwnedTarget(ctx context.Context, active lifecycle.State, expected Retained) error {
	if store == nil || active.DeploymentID != store.config.DeploymentID || active.DatabaseSlot == store.config.Slot || active.DatabaseSlot != lifecycle.DatabasePrimary && active.DatabaseSlot != lifecycle.DatabaseRecovery || active.Master != lifecycle.MasterDefault && active.Master != lifecycle.MasterGeneration {
		return ErrInvalid
	}
	activeMarker := Marker{Version: 1, DeploymentID: active.DeploymentID, GenerationID: active.GenerationID, Slot: active.DatabaseSlot}
	if _, err := EncodeMarker(activeMarker); err != nil {
		return ErrInvalid
	}
	if active.Revision == 0 {
		if active.DatabaseSlot != lifecycle.DatabasePrimary || active.GenerationID != "" || active.Master != lifecycle.MasterDefault || store.config.Slot != lifecycle.DatabaseRecovery {
			return ErrInvalid
		}
	} else if active.GenerationID == "" {
		return ErrInvalid
	}
	marker, err := store.localMarker(expected.RawMarker)
	if err != nil || marker != expected.Marker || expected.Facts.DatabaseSchema != "public" {
		return ErrConflict
	}
	bound, finish, err := store.BoundContext(ctx)
	if err != nil {
		return err
	}
	defer finish()
	tx, err := store.pool.BeginTx(bound, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return store.operationError(bound, err)
	}
	defer rollback(tx)
	if !store.lease.ProtectsTransaction(store.pool, tx) {
		return ErrLeaseLost
	}
	inspection, err := backuppg.InspectRecoveryTransaction(bound, tx, "public")
	if err != nil {
		return store.operationError(bound, err)
	}
	identity := inspection.Identity()
	if err := store.matchesIdentity(identity); err != nil {
		return err
	}
	if identity.Database != expected.Database || identity.Role != expected.Role {
		return ErrConflict
	}
	if err := inspection.LockTables(bound); err != nil {
		return store.operationError(bound, err)
	}
	observed, err := readRaw(bound, tx)
	if err != nil {
		return store.operationError(bound, err)
	}
	if !observed.TablePresent || observed.Raw != expected.RawMarker {
		return ErrConflict
	}
	if err := store.check(bound); err != nil {
		return err
	}
	if err := inspection.EmptyTrustedSchema(bound, expected.Facts); err != nil {
		return store.operationError(bound, err)
	}
	if err := store.check(bound); err != nil {
		return err
	}
	if err := tx.Commit(bound); err != nil {
		return store.operationError(bound, err)
	}
	return nil
}

func (store *Store) localMarker(raw RawMarker) (Marker, error) {
	if !raw.Present || ValidateRawMarker(raw) != nil {
		return Marker{}, ErrConflict
	}
	marker, err := DecodeMarker(raw.Value)
	if err != nil || marker.DeploymentID != store.config.DeploymentID || marker.Slot != store.config.Slot {
		return Marker{}, ErrConflict
	}
	return marker, nil
}

func (store *Store) matchesIdentity(identity backuppg.RecoveryIdentity) error {
	config := store.pool.Config().ConnConfig
	if identity.Schema != "public" || identity.Database != config.Database || identity.Role != config.User {
		return ErrConflict
	}
	return nil
}

func (store *Store) operationError(ctx context.Context, err error) error {
	if state := store.check(ctx); state != nil {
		return state
	}
	if errors.Is(err, ErrInvalid) || errors.Is(err, backuppg.ErrConfiguration) {
		return ErrInvalid
	}
	if errors.Is(err, ErrConflict) || errors.Is(err, backuppg.ErrArchive) || errors.Is(err, backuppg.ErrSchema) || errors.Is(err, backuppg.ErrTarget) || errors.Is(err, backuppg.ErrUnsupported) {
		return ErrConflict
	}
	return ErrUnavailable
}

func readRaw(ctx context.Context, tx pgx.Tx) (ReadResult, error) {
	var safe bool
	err := tx.QueryRow(ctx, `SELECT c.relkind='r' AND c.relpersistence='p' AND NOT c.relrowsecurity AND NOT c.relforcerowsecurity AND NOT c.relispartition AND c.reloftype=0
	 AND (SELECT count(*) FROM pg_catalog.pg_attribute a WHERE a.attrelid=c.oid AND a.attname IN ('key','value') AND a.atttypid='pg_catalog.text'::regtype AND a.attnotnull AND NOT a.attisdropped)=2
	 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname='server_settings'`).Scan(&safe)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReadResult{}, nil
	}
	if err != nil {
		return ReadResult{}, ErrUnavailable
	}
	if !safe {
		return ReadResult{}, ErrConflict
	}
	rows, err := tx.Query(ctx, `SELECT CASE WHEN octet_length(value)<=$2 THEN value ELSE NULL END FROM "public"."server_settings" WHERE key=$1 LIMIT 2`, MarkerKey, MaxMarkerBytes)
	if err != nil {
		return ReadResult{}, ErrUnavailable
	}
	defer rows.Close()
	result := ReadResult{TablePresent: true}
	for rows.Next() {
		var value *string
		if rows.Scan(&value) != nil {
			return ReadResult{}, ErrUnavailable
		}
		if value == nil || result.Raw.Present {
			return ReadResult{}, ErrConflict
		}
		result.Raw = RawMarker{Present: true, Value: *value}
	}
	if rows.Err() != nil {
		return ReadResult{}, ErrUnavailable
	}
	if ValidateRawMarker(result.Raw) != nil {
		return ReadResult{}, ErrConflict
	}
	return result, nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
