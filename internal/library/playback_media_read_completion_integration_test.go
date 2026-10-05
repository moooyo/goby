//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type playbackReadEndTestKey struct{}
type playbackReadEndTestCommandKey struct{}
type playbackReadEndTestError struct{ err error }

// Observe the real completion command without changing production callbacks.
type playbackReadEndTestTrace struct {
	connection atomic.Pointer[pgx.Conn]
	clockSeen  atomic.Bool
	injected   atomic.Bool
	commits    atomic.Int32
	rollbacks  atomic.Int32
	successes  atomic.Int32
	injection  atomic.Pointer[playbackReadEndTestError]
	beforeEnd  func(*pgx.Conn) error
}

func (trace *playbackReadEndTestTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if ctx.Value(playbackReadEndTestKey{}) != trace {
		return ctx
	}
	trace.connection.Store(connection)
	command := strings.ToLower(strings.TrimSpace(data.SQL))
	if command != "commit" && command != "rollback" {
		return ctx
	}
	if command == "commit" {
		trace.commits.Add(1)
	} else {
		trace.rollbacks.Add(1)
	}
	if trace.beforeEnd != nil && trace.clockSeen.Load() && trace.injected.CompareAndSwap(false, true) {
		if err := trace.beforeEnd(connection); err != nil {
			trace.injection.Store(&playbackReadEndTestError{err: err})
		}
	}
	return context.WithValue(ctx, playbackReadEndTestCommandKey{}, command)
}

func (trace *playbackReadEndTestTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if command, _ := ctx.Value(playbackReadEndTestCommandKey{}).(string); command != "" && data.Err == nil && strings.EqualFold(data.CommandTag.String(), command) {
		trace.successes.Add(1)
	}
}

func (*playbackReadEndTestTrace) TraceBatchStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceBatchStartData) context.Context {
	return ctx
}

func (trace *playbackReadEndTestTrace) TraceBatchQuery(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchQueryData) {
	if ctx.Value(playbackReadEndTestKey{}) == trace && strings.Contains(data.SQL, "clock_timestamp()") {
		trace.connection.Store(connection)
		trace.clockSeen.Store(true)
	}
}

func (*playbackReadEndTestTrace) TraceBatchEnd(context.Context, *pgx.Conn, pgx.TraceBatchEndData) {}

func playbackReadEndTestPool(t *testing.T, fixture mediaSourceFixture, trace *playbackReadEndTestTrace, isolation string) {
	t.Helper()
	configuration := fixture.pool.Config().Copy()
	configuration.ConnConfig.Tracer = trace
	configuration.ConnConfig.RuntimeParams["default_transaction_isolation"] = isolation
	configuration.MaxConns, configuration.MinConns = 1, 0
	pool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	fixture.store.pool = pool
	t.Cleanup(func() { fixture.store.pool = fixture.pool })
}

// Row locks may change xmax. Business row contents and xmin must stay fixed.
func playbackReadEndTestRows(t *testing.T, fixture mediaSourceFixture, sessionID, clientID, playID string) string {
	t.Helper()
	var snapshot string
	err := fixture.pool.QueryRow(fixture.ctx, `SELECT jsonb_build_object(
		'users',(SELECT jsonb_agg(to_jsonb(u)||jsonb_build_object('xmin',u.xmin::text)) FROM users u WHERE u.id=$1),
		'sessions',(SELECT jsonb_agg(to_jsonb(s)||jsonb_build_object('xmin',s.xmin::text)) FROM sessions s WHERE s.id=$2),
		'keys',(SELECT jsonb_agg(to_jsonb(k)||jsonb_build_object('xmin',k.xmin::text)) FROM application_keys k WHERE k.credential_id=$2),
		'clients',(SELECT jsonb_agg(to_jsonb(c)||jsonb_build_object('xmin',c.xmin::text)) FROM application_key_clients c WHERE c.id=$3),
		'play',(SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('xmin',p.xmin::text)) FROM play_sessions p WHERE p.id=$4),
		'item',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text)) FROM items i WHERE i.id=$5),
		'subtitles',(SELECT jsonb_agg(to_jsonb(s)||jsonb_build_object('xmin',s.xmin::text) ORDER BY s.stream_index) FROM item_subtitles s WHERE s.item_id=$5),
		'owned',(SELECT jsonb_agg(to_jsonb(s)||jsonb_build_object('xmin',s.xmin::text) ORDER BY s.stream_index) FROM item_owned_subtitles s WHERE s.item_id=$5)
	)::text`, fixture.userID, sessionID, clientID, playID, fixture.item.ID).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPlaybackMediaReadCompletionReleasesLocksWithoutBusinessWrites(t *testing.T) {
	for _, test := range []struct {
		name, isolation string
		application     bool
	}{
		{"login_read_committed", "read committed", false},
		{"key_read_committed", "read committed", true},
		{"repeatable_read_commit", "repeatable read", false},
		{"serializable_commit", "serializable", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture, _, _ := subtitleTestCatalog(t)
			ownedSubtitleTestInsert(t, fixture, []byte(subtitleTestSRT))
			principal, play := playbackMediaTestPrincipal(t, fixture, test.application)
			_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			playbackMediaPipelineTestDrain(t, fixture.store)
			before := playbackReadEndTestRows(t, fixture, principal.SessionID, principal.ClientSessionID, play.ID)
			trace := &playbackReadEndTestTrace{}
			playbackReadEndTestPool(t, fixture, trace, test.isolation)
			ctx := context.WithValue(fixture.ctx, playbackReadEndTestKey{}, trace)
			var callbacks atomic.Int32
			file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, true,
				func(current PlaybackMediaAuthorization) error {
					callbacks.Add(1)
					if fixture.store.pool.Stat().AcquiredConns() != 0 || len(current.Source.Item.Subtitles) != 2 {
						return errors.New("completion retained a connection or lost subtitle projections")
					}
					// MaxConns=1 also detects release after, instead of before, callback.
					observer, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
					defer cancel()
					var id string
					return fixture.store.pool.QueryRow(observer, "SELECT id FROM play_sessions WHERE id=$1 FOR UPDATE NOWAIT", play.ID).Scan(&id)
				})
			if file != nil {
				_ = file.Close()
			}
			playbackMediaPipelineTestDrain(t, fixture.store)
			if err != nil || file == nil || callbacks.Load() != 1 || result.Play.ID != play.ID || result.Source.primaryReadProof == nil {
				t.Fatalf("successful read completion lost delivery: callbacks=%d error=%v", callbacks.Load(), err)
			}
			wantCommit, wantRollback := int32(0), int32(1)
			if test.isolation != "read committed" {
				wantCommit, wantRollback = 1, 0
			}
			if trace.commits.Load() != wantCommit || trace.rollbacks.Load() != wantRollback || trace.successes.Load() != 1 {
				t.Fatalf("wrong actual-isolation completion: commits=%d rollbacks=%d successes=%d", trace.commits.Load(), trace.rollbacks.Load(), trace.successes.Load())
			}
			mediaSourceImmediateFallbackTestLocks(t, fixture, fixture.ctx, principal, play)
			playbackMediaPipelineTestRowLocked(t, fixture.ctx, fixture, "SELECT id FROM items WHERE id=$1 FOR UPDATE NOWAIT", fixture.item.ID, false)
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			if after := playbackReadEndTestRows(t, fixture, principal.SessionID, principal.ClientSessionID, play.ID); after != before {
				t.Fatal("lock-only authorization changed business rows or xmin")
			}
		})
	}
}

func TestPlaybackMediaReadCompletionRejectsAbortedOrEndedTransactions(t *testing.T) {
	for _, failure := range []string{"aborted", "ended", "closed_connection", "cancelled", "serializable_aborted"} {
		t.Run(failure, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			trace := &playbackReadEndTestTrace{}
			isolation := "read committed"
			if failure == "serializable_aborted" {
				isolation = "serializable"
			}
			playbackReadEndTestPool(t, fixture, trace, isolation)
			ctx, cancel := context.WithCancel(fixture.ctx)
			defer cancel()
			ctx = context.WithValue(ctx, playbackReadEndTestKey{}, trace)
			snapshot, result, err := fixture.store.readPlaybackMediaAuthorizationWithAdmission(ctx, principal, playbackMediaOwner(principal), play.ID,
				fixture.item.ID, play.MediaSourceID, false, func(indexedMediaSource) error {
					connection := trace.connection.Load()
					if connection == nil || !trace.clockSeen.Load() || connection.PgConn().TxStatus() != 'T' {
						return errors.New("test did not reach the final validated active transaction")
					}
					// Inject only after final Go validation. This test hook uses the
					// captured raw connection; production admission remains memory-only.
					if failure == "aborted" || failure == "serializable_aborted" {
						if _, err := connection.Exec(fixture.ctx, "SELECT 1/0"); err == nil {
							return errors.New("test could not abort the transaction")
						}
						if connection.PgConn().TxStatus() != 'E' {
							return errors.New("test did not create an aborted transaction")
						}
					} else if failure == "ended" {
						if _, err := connection.Exec(fixture.ctx, "ROLLBACK"); err != nil {
							return err
						}
						if connection.PgConn().TxStatus() != 'I' {
							return errors.New("test did not end the server transaction")
						}
					} else if failure == "closed_connection" {
						if err := connection.Close(fixture.ctx); err != nil {
							return err
						}
					} else {
						cancel()
					}
					return nil
				})
			if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(snapshot, indexedMediaSource{}) || !reflect.DeepEqual(result, PlaybackMediaAuthorization{}) {
				t.Fatalf("invalid completion sealed authority: failure=%s error=%v", failure, err)
			}
			if failure == "serializable_aborted" && (!errors.Is(err, pgx.ErrTxCommitRollback) || trace.commits.Load() != 1 || trace.successes.Load() != 0) {
				t.Fatalf("non-RC completion lost its COMMIT failure semantics: error=%v", err)
			}
			mediaSourceImmediateFallbackTestLocks(t, fixture, fixture.ctx, principal, play)
		})
	}
}

func TestPlaybackMediaReadCompletionFailureNeverCallsDeliveryOrOpensMedia(t *testing.T) {
	for _, failure := range []string{"cancel", "terminate"} {
		t.Run(failure, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			principal, play := playbackMediaTestPrincipal(t, fixture, false)
			_, root, _ := mediaSourceRootAdmissionTestLane(t, fixture, fixture.item.ID)
			playbackMediaPipelineTestDrain(t, fixture.store)
			assertNoMediaIO := primaryScanRoutingWatchSource(t, fixture.path)
			beforeFDs := mediaSourceWarmPipelineTestFileCount(t, fixture.path)
			ctx, cancel := context.WithCancel(fixture.ctx)
			defer cancel()
			trace := &playbackReadEndTestTrace{beforeEnd: func(connection *pgx.Conn) error {
				if failure == "cancel" {
					cancel()
					return nil
				}
				observer, finish := context.WithTimeout(fixture.ctx, 7*time.Second)
				defer finish()
				var terminated bool
				if err := fixture.pool.QueryRow(observer, "SELECT pg_terminate_backend($1::integer, 5000::bigint)", int32(connection.PgConn().PID())).Scan(&terminated); err != nil {
					return err
				}
				if !terminated {
					return errors.New("test backend did not terminate")
				}
				return nil
			}}
			playbackReadEndTestPool(t, fixture, trace, "read committed")
			ctx = context.WithValue(ctx, playbackReadEndTestKey{}, trace)
			var callbacks atomic.Int32
			file, result, err := fixture.store.AuthorizePlaybackMediaForChecked(ctx, principal, play.ID, fixture.item.ID, play.MediaSourceID, false,
				func(PlaybackMediaAuthorization) error { callbacks.Add(1); return nil })
			if file != nil {
				_ = file.Close()
			}
			playbackMediaPipelineTestDrain(t, fixture.store)
			want := error(ErrUnavailable)
			if failure == "cancel" {
				want = context.Canceled
			}
			if injection := trace.injection.Load(); injection != nil {
				t.Fatal(fmt.Errorf("completion failure injection: %w", injection.err))
			}
			if !trace.injected.Load() || !errors.Is(err, want) || file != nil || callbacks.Load() != 0 || !reflect.DeepEqual(result, PlaybackMediaAuthorization{}) || trace.successes.Load() != 0 {
				t.Fatalf("failed completion reached delivery: injected=%t callbacks=%d error=%v", trace.injected.Load(), callbacks.Load(), err)
			}
			assertNoMediaIO()
			if mediaSourceWarmPipelineTestFileCount(t, fixture.path) != beforeFDs {
				t.Fatal("failed completion leaked a media descriptor")
			}
			playbackMediaPipelineTestOwnerCounts(t, root, 0, 0)
			mediaSourceImmediateFallbackTestLocks(t, fixture, fixture.ctx, principal, play)
		})
	}
}
