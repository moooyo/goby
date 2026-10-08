package library

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPlaybackStopItemBatchRechecksVisibilityAfterItemLockWait(t *testing.T) {
	for _, finish := range []string{"commit", "rollback"} {
		t.Run(finish, func(t *testing.T) {
			ctx, store := libraryQueryTestStore(t)
			seedLibraryQueryFixture(t, ctx, store.pool)
			observer := store.pool
			configuration := observer.Config().Copy()
			configuration.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
			configuration.ConnConfig.RuntimeParams["default_transaction_isolation"] = "read committed"
			pool, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationPoolCleanup(t, pool)
			store.pool = pool
			if _, err := pool.Exec(ctx, `UPDATE items SET root_id='root-b', path='/media/b/Owner/theme.mp3',
				relative_path='Owner/theme.mp3', media=jsonb_build_object('DurationTicks',$1::bigint)
				WHERE id='movie-b'`, playbackTestDuration); err != nil {
				t.Fatal(err)
			}
			owner := playSessionOwnerFixture(t, ctx, pool, "restricted", "item-batch-device")
			prepared := playSessionPrepare(t, ctx, store, owner, "movie-b", "")
			beforePlay, beforeData := playSessionSnapshot(t, ctx, pool, prepared)
			operation, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			classifier, err := observer.Begin(operation)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(classifier)
			// Do not update the item tuple: the original statement snapshot can
			// only miss this change in another relation, not recheck a new tuple.
			if _, err := classifier.Exec(operation, "SELECT id FROM items WHERE id='movie-b' FOR UPDATE"); err != nil {
				t.Fatal(err)
			}
			if _, err := classifier.Exec(operation, `INSERT INTO theme_reserved_paths
				(root_id,relative_path,is_directory) VALUES('root-b','Owner/theme.mp3',false)`); err != nil {
				t.Fatal(err)
			}
			var factories atomic.Int32
			results := make(chan playSessionReportResult, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				session, data, err := store.ReportPlaybackWithValidatedStop(operation, owner,
					PlaybackReport{PlaySessionID: prepared.ID, Event: "Stopped"},
					func(ValidatedPlaybackStop) (PlaybackValidatedStopAction, error) {
						factories.Add(1)
						return PlaybackValidatedStopAction{}, nil
					})
				results <- playSessionReportResult{session: session, data: data, err: err}
			}()
			defer func() {
				cancel()
				rollback(classifier)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("the bounded Stop worker did not release its transaction")
				}
			}()
			waitCatalogApplicationBlock(t, operation, observer, classifier.Conn().PgConn().PID())
			select {
			case result := <-results:
				t.Fatalf("Stop crossed the held item lock: %v", result.err)
			default:
			}
			if factories.Load() != 0 {
				t.Fatal("Stop action ran before item visibility completed")
			}
			if finish == "commit" {
				err = classifier.Commit(operation)
			} else {
				err = classifier.Rollback(operation)
			}
			if err != nil {
				t.Fatal(err)
			}
			var result playSessionReportResult
			select {
			case result = <-results:
			case <-operation.Done():
				t.Fatal("Stop did not finish after the classification transaction ended")
			}
			if finish == "commit" {
				if !errors.Is(result.err, ErrNotFound) || result.session.ID != "" || result.data.ItemID != "" || factories.Load() != 0 {
					t.Fatalf("stale visibility reached Stop validation or mutation: factories=%d error=%v", factories.Load(), result.err)
				}
				afterPlay, afterData := playSessionSnapshot(t, ctx, pool, prepared)
				if afterPlay != beforePlay || afterData != beforeData {
					t.Fatal("rejected Stop changed playback or user data")
				}
			} else if result.err != nil || result.session.State != "Stopped" || result.session.DurationTicks != playbackTestDuration || factories.Load() != 1 {
				t.Fatalf("rolled-back classification prevented the normal Stop path: factories=%d error=%v", factories.Load(), result.err)
			}
		})
	}
}

// Execute real PostgreSQL results while injecting a later SQL or Close failure.
// Only the selected error cases replace a statement; other SQL stays unchanged.
type stateItemBatchFaultTx struct {
	pgx.Tx
	queryFailure int
	closeFailure error
	closed       bool
}

func (tx *stateItemBatchFaultTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	if tx.queryFailure >= 0 {
		injected := &pgx.Batch{}
		for index, query := range batch.QueuedQueries {
			if index == tx.queryFailure {
				injected.Queue("SELECT 1/0")
			} else {
				injected.Queue(query.SQL, query.Arguments...)
			}
		}
		batch = injected
	}
	return &stateItemBatchFaultResults{BatchResults: tx.Tx.SendBatch(ctx, batch), owner: tx}
}

type stateItemBatchFaultResults struct {
	pgx.BatchResults
	owner *stateItemBatchFaultTx
}

func (results *stateItemBatchFaultResults) Close() error {
	err := results.BatchResults.Close()
	results.owner.closed = true
	return errors.Join(err, results.owner.closeFailure)
}

func TestLockStateItemBatchPreservesResultsAndErrorOrder(t *testing.T) {
	ctx, store := libraryQueryTestStore(t)
	seedLibraryQueryFixture(t, ctx, store.pool)
	closeFailure := errors.New("injected item batch completion failure")
	for _, test := range []struct {
		name, itemID, errorPrefix string
		queryFailure              int
		denied, malformed, close  bool
		want                      error
	}{
		{name: "visible", itemID: "episode-b1", queryFailure: -1},
		{name: "missing", itemID: "missing", queryFailure: -1, want: ErrNotFound},
		{name: "unauthorized", itemID: "episode-b1", queryFailure: -1, denied: true, want: ErrNotFound},
		{name: "full_media_decode", itemID: "episode-b1", queryFailure: -1, malformed: true, errorPrefix: "read user state media duration:"},
		{name: "first_sql_error", itemID: "episode-b1", queryFailure: 0, close: true, errorPrefix: "authorize user state item:"},
		{name: "missing_before_later_sql_error", itemID: "missing", queryFailure: 1, close: true, want: ErrNotFound},
		{name: "second_sql_error", itemID: "episode-b1", queryFailure: 1, close: true, errorPrefix: "recheck user state item visibility:"},
		{name: "close_error", itemID: "episode-b1", queryFailure: -1, close: true, want: closeFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := store.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if test.malformed {
				if _, err := tx.Exec(ctx, `UPDATE items SET media='{"DurationTicks":15000000,"Streams":"invalid"}'::jsonb WHERE id=$1`, test.itemID); err != nil {
					t.Fatal(err)
				}
			}
			access := unrestrictedLibraryAccess()
			if test.denied {
				access.all = false
			}
			faults := &stateItemBatchFaultTx{Tx: tx, queryFailure: test.queryFailure}
			if test.close {
				faults.closeFailure = closeFailure
			}
			item, err := lockStateItem(ctx, faults, access, test.itemID, true)
			if !faults.closed || tx.Conn().PgConn().IsBusy() {
				t.Fatal("item validation returned without draining its batch")
			}
			if test.name == "visible" {
				want := stateItem{id: "episode-b1", itemType: "Episode", libraryID: "library-b", duration: 15000000}
				if err != nil || item != want {
					t.Fatalf("item validation changed its complete result: item=%+v error=%v", item, err)
				}
				return
			}
			if err == nil || item != (stateItem{}) || test.want != nil && !errors.Is(err, test.want) || test.errorPrefix != "" && !strings.HasPrefix(err.Error(), test.errorPrefix) {
				t.Fatalf("item validation changed error precedence or returned authority: item=%+v error=%v", item, err)
			}
			if test.queryFailure >= 0 && test.want == nil {
				var postgresError *pgconn.PgError
				if !errors.As(err, &postgresError) || postgresError.Code != "22012" {
					t.Fatalf("item validation lost its PostgreSQL statement error: %v", err)
				}
			}
			if test.name != "close_error" && errors.Is(err, closeFailure) {
				t.Fatalf("batch completion replaced an earlier item error: %v", err)
			}
		})
	}
}
