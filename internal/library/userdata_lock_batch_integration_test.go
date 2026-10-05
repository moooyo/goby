package library

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type userDataLockBatchResult struct {
	data UserData
	err  error
}

func userDataLockBatchStart(ctx context.Context, pool *pgxpool.Pool) (<-chan userDataLockBatchResult, <-chan struct{}) {
	results := make(chan userDataLockBatchResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tx, err := pool.Begin(ctx)
		if err != nil {
			results <- userDataLockBatchResult{err: err}
			return
		}
		defer rollback(tx)
		data, err := lockUserData(ctx, tx, "default", "movie-b")
		if err == nil {
			err = tx.Commit(ctx)
		}
		results <- userDataLockBatchResult{data: data, err: err}
	}()
	return results, done
}

func userDataLockBatchAwait(t *testing.T, ctx context.Context, results <-chan userDataLockBatchResult) userDataLockBatchResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		t.Fatal("user-data lock worker did not finish")
		return userDataLockBatchResult{}
	}
}

func userDataLockBatchJoin(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("user-data lock worker did not release its transaction")
	}
}

func TestLockUserDataBatchResolvesConcurrentCreation(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			ctx, store := libraryQueryTestStore(t)
			seedLibraryQueryFixture(t, ctx, store.pool)
			creator, err := store.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(creator)
			if _, err := creator.Exec(ctx, `INSERT INTO user_item_data
				(user_id,item_id,playback_position_ticks,play_count,is_favorite,played)
				VALUES('default','movie-b',73,4,true,true)`); err != nil {
				t.Fatal(err)
			}
			worker, cancel := context.WithTimeout(ctx, 10*time.Second)
			results, done := userDataLockBatchStart(worker, store.pool)
			defer func() { cancel(); rollback(creator); userDataLockBatchJoin(t, done) }()
			waitCatalogApplicationBlock(t, worker, store.pool, creator.Conn().PgConn().PID())
			select {
			case result := <-results:
				t.Fatalf("conflicting initialization returned before its creator ended: %v", result.err)
			default:
			}
			want := UserData{ItemID: "movie-b"}
			if finish == "commit" {
				err = creator.Commit(ctx)
				want = UserData{ItemID: "movie-b", PlaybackPositionTicks: 73, PlayCount: 4, IsFavorite: true, Played: true}
			} else {
				err = creator.Rollback(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			result := userDataLockBatchAwait(t, worker, results)
			userDataLockBatchJoin(t, done)
			if result.err != nil {
				t.Fatal(result.err)
			}
			userDataAssertValue(t, result.data, want)
			persisted, err := store.GetUserData(ctx, "default", "movie-b")
			if err != nil {
				t.Fatal(err)
			}
			userDataAssertValue(t, persisted, want)
			var count int
			if err := store.pool.QueryRow(ctx, "SELECT count(*) FROM user_item_data WHERE user_id='default' AND item_id='movie-b'").Scan(&count); err != nil || count != 1 {
				t.Fatalf("concurrent initialization did not retain exactly one row: count=%d error=%v", count, err)
			}
		})
	}
}

func TestLockUserDataBatchCancellationDoesNotCommitInitialization(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	creator, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(creator)
	if _, err := creator.Exec(ctx, "INSERT INTO user_item_data(user_id,item_id) VALUES('default','movie-b')"); err != nil {
		t.Fatal(err)
	}
	worker, cancel := context.WithCancel(ctx)
	results, done := userDataLockBatchStart(worker, store.pool)
	defer func() { cancel(); rollback(creator); userDataLockBatchJoin(t, done) }()
	waitCatalogApplicationBlock(t, ctx, store.pool, creator.Conn().PgConn().PID())
	cancel()
	result := userDataLockBatchAwait(t, ctx, results)
	userDataLockBatchJoin(t, done)
	if !errors.Is(result.err, context.Canceled) || !strings.HasPrefix(result.err.Error(), "initialize user item data:") {
		t.Fatalf("cancelled creation lost its first-statement error: %v", result.err)
	}
	if err := creator.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_item_data WHERE user_id='default' AND item_id='movie-b')").Scan(&exists); err != nil || exists {
		t.Fatalf("cancelled initialization committed a row: exists=%t error=%v", exists, err)
	}
}

func TestLockUserDataBatchRetainsExclusiveExistingRowLock(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	want := UserData{ItemID: "movie-b", PlaybackPositionTicks: 73, PlayCount: 4, IsFavorite: true, Played: true}
	userDataSeed(t, ctx, store.pool, "default", want)
	holder, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(holder)
	locked, err := lockUserData(ctx, holder, "default", "movie-b")
	if err != nil {
		t.Fatal(err)
	}
	userDataAssertValue(t, locked, want)
	worker, cancel := context.WithTimeout(ctx, 10*time.Second)
	results := make(chan userDataLockBatchResult, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		data, err := store.SetFavorite(worker, "default", "movie-b", false)
		results <- userDataLockBatchResult{data: data, err: err}
	}()
	defer func() { cancel(); rollback(holder); userDataLockBatchJoin(t, done) }()
	waitCatalogApplicationBlock(t, worker, store.pool, holder.Conn().PgConn().PID())
	select {
	case result := <-results:
		t.Fatalf("another writer crossed the retained exclusive lock: %v", result.err)
	default:
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	result := userDataLockBatchAwait(t, worker, results)
	userDataLockBatchJoin(t, done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	want.IsFavorite = false
	userDataAssertValue(t, result.data, want)
	persisted, err := store.GetUserData(ctx, "default", "movie-b")
	if err != nil {
		t.Fatal(err)
	}
	userDataAssertValue(t, persisted, want)
}

func TestLockUserDataBatchKeepsInitializationErrorBeforeAbortedSelect(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	if _, err := store.pool.Exec(ctx, `CREATE FUNCTION reject_batch_initialization() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected initialization failure' USING ERRCODE='P0001'; END; $$;
		CREATE TRIGGER reject_batch_initialization BEFORE INSERT ON user_item_data
		FOR EACH ROW EXECUTE FUNCTION reject_batch_initialization()`); err != nil {
		t.Fatal(err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	_, err = lockUserData(ctx, tx, "default", "movie-b")
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "P0001" || !strings.HasPrefix(err.Error(), "initialize user item data:") || tx.Conn().PgConn().TxStatus() != 'E' {
		t.Fatalf("later aborted SELECT/drain replaced the INSERT failure: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM user_item_data WHERE user_id='default' AND item_id='movie-b')").Scan(&exists); err != nil || exists {
		t.Fatalf("failed initialization committed a row: exists=%t error=%v", exists, err)
	}
}
