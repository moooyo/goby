package library

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
)

func episodeRosterBatchFixture(t *testing.T, trace pgx.QueryTracer) episodeRosterFixture {
	t.Helper()
	ctx, pool, store, _, _ := catalogBatchTestStoreWithTracer(t, trace)
	seedLibraryQueryFixture(t, ctx, pool)
	return episodeRosterFixture{ctx: ctx, pool: pool, store: store,
		actor: metadataEditTestActor(t, ctx, pool, "episode-roster-batch-admin")}
}

func episodeRosterBatchEdit(count int) EpisodeRosterEdit {
	edit := episodeRosterTestEdit()
	edit.Entries = make([]EpisodeRosterEntryInput, count)
	for index := range edit.Entries {
		edit.Entries[index] = EpisodeRosterEntryInput{
			Key: fmt.Sprintf("episode-%04d", index), SeasonNumber: 10, EpisodeNumber: index,
			Name:         fmt.Sprintf("Episode %d: 'quoted', \\ slash, café", index),
			PremiereDate: []string{"", "0001-01-01", "9999-12-31"}[index%3],
		}
	}
	return edit
}

func episodeRosterBatchSnapshot(t *testing.T, f episodeRosterFixture) string {
	t.Helper()
	var snapshot string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'rosters', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY series_id), '[]') FROM series_episode_rosters r),
		'imports', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY series_id,revision), '[]') FROM episode_roster_imports r),
		'entries', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM expected_episodes r),
		'system_events', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY name), '[]') FROM task_system_events r),
		'notification_events', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]') FROM notification_source_events r),
		'notification_state', (SELECT to_jsonb(r) FROM notification_journal_state r WHERE id=1))::text`).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot roster facts, source history and events: %v", err)
	}
	return snapshot
}

func TestEpisodeRosterBatchMaximumEntriesUseOneStatementAndPreserveFacts(t *testing.T) {
	trace := &itemCountTracer{}
	f := episodeRosterBatchFixture(t, trace)
	var fullStatementCount int
	var originalCreation time.Time
	var edit EpisodeRosterEdit
	for step, count := range []int{1, MaxEpisodeRosterEntries, 3} {
		edit = episodeRosterBatchEdit(count)
		edit.Revision, edit.Source.Revision = fmt.Sprint(step), fmt.Sprintf("v%d", step+1)
		if step == 2 {
			// Reuse both identities while exchanging their unique active numbers.
			edit.Entries[0].EpisodeNumber, edit.Entries[1].EpisodeNumber = 1, 0
			edit.Entries[0].PremiereDate, edit.Entries[1].PremiereDate = "2024-02-29", ""
		}
		trace.reset()
		started := time.Now()
		detail := f.replace(t, edit)
		elapsed := time.Since(started)
		if detail.Revision != fmt.Sprint(step+1) || len(detail.Entries) != count || detail.State != "active" {
			t.Fatalf("batch result: revision=%s entries=%d state=%s", detail.Revision, len(detail.Entries), detail.State)
		}
		if got := catalogBatchStatementCount(trace, "INSERT INTO expected_episodes"); got != 1 {
			t.Fatalf("%d entries used %d entry statements, want one", count, got)
		}
		trace.mu.Lock()
		statements := append([]string(nil), trace.statements...)
		trace.mu.Unlock()
		if len(statements) < 2 || !strings.EqualFold(statements[0], "begin") || !strings.EqualFold(statements[len(statements)-1], "commit") {
			t.Fatalf("roster batch did not use one complete transaction: %v", statements)
		}
		for _, statement := range statements[1 : len(statements)-1] {
			if strings.EqualFold(statement, "begin") || strings.EqualFold(statement, "commit") {
				t.Fatal("roster batch split its transaction")
			}
		}
		if step == 1 {
			fullStatementCount = len(statements)
			t.Logf("full roster: entries=%d SQL statements=%d entry statements=1 request duration=%s", count, len(statements), elapsed)
			if err := f.pool.QueryRow(f.ctx, `SELECT created_at FROM expected_episodes
				WHERE series_id='series-b' AND entry_key='episode-0001'`).Scan(&originalCreation); err != nil {
				t.Fatal(err)
			}
		} else if step == 2 && len(statements) != fullStatementCount {
			t.Fatalf("replacement SQL statement count depends on roster size: maximum=%d small=%d", fullStatementCount, len(statements))
		}
		want := make(map[string]EpisodeRosterEntryInput, count)
		for _, entry := range edit.Entries {
			want[entry.Key] = entry
		}
		for _, entry := range detail.Entries {
			if entry.EpisodeRosterEntryInput != want[entry.Key] || entry.ID != expectedEpisodeID("series-b", edit.Source.Key, entry.Key) {
				t.Fatalf("batch changed identity or typed values: %+v", entry)
			}
		}
	}
	var nullDate, retainedCreation bool
	if err := f.pool.QueryRow(f.ctx, `SELECT e.premiere_date IS NULL,e.created_at=$1
		FROM expected_episodes e WHERE e.series_id='series-b' AND e.entry_key='episode-0001'`, originalCreation).Scan(&nullDate, &retainedCreation); err != nil || !nullDate || !retainedCreation {
		t.Fatalf("upsert lost nullable date or original creation: null=%t retained=%t error=%v", nullDate, retainedCreation, err)
	}
	var payload []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT payload FROM episode_roster_imports WHERE series_id='series-b' AND revision=2`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	prior, _, err := ParseEpisodeRosterPayload(payload)
	if err != nil || !reflect.DeepEqual(prior.Entries, episodeRosterBatchEdit(MaxEpisodeRosterEntries).Entries) {
		t.Fatalf("replacement changed the full historical import: %v", err)
	}
	before := episodeRosterBatchSnapshot(t, f)
	duplicate := edit
	duplicate.Revision = "3"
	duplicate.Source.Revision = "invalid-duplicate"
	duplicate.Entries = append(append([]EpisodeRosterEntryInput(nil), edit.Entries...), edit.Entries[0])
	if _, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", duplicate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate batch input: %v", err)
	}
	if episodeRosterBatchSnapshot(t, f) != before {
		t.Fatal("duplicate batch changed retained facts")
	}
	empty := edit
	empty.Revision, empty.Source.Revision, empty.Entries = "3", "empty", []EpisodeRosterEntryInput{}
	trace.reset()
	retired := f.replace(t, empty)
	if retired.State != "active" || retired.Revision != "4" || len(retired.Entries) != 0 || retired.RetiredCount != MaxEpisodeRosterEntries {
		t.Fatalf("empty batch did not retire all facts: %+v", retired)
	}
	if catalogBatchStatementCount(trace, "INSERT INTO expected_episodes") != 0 {
		t.Fatal("empty batch issued an entry write")
	}
	edit.Revision = "4"
	reactivated := f.replace(t, edit)
	if reactivated.Revision != "5" || len(reactivated.Entries) != 3 || reactivated.RetiredCount != MaxEpisodeRosterEntries-3 {
		t.Fatalf("historical source reactivation: %+v", reactivated)
	}
	for _, entry := range reactivated.Entries {
		if entry.ID != expectedEpisodeID("series-b", edit.Source.Key, entry.Key) {
			t.Fatal("historical source reactivation replaced a stable identity")
		}
	}
}

func TestEpisodeRosterBatchRollsBackEntryFailureAndFinalActorFailure(t *testing.T) {
	for _, expireActor := range []bool{false, true} {
		name := "entry failure"
		if expireActor {
			name = "final actor failure"
		}
		t.Run(name, func(t *testing.T) {
			f := episodeRosterBatchFixture(t, nil)
			f.replace(t, episodeRosterBatchEdit(3))
			notifications := catalogChangesTestListener(t, f.store)
			before := episodeRosterBatchSnapshot(t, f)
			body := `RAISE EXCEPTION USING ERRCODE='P0001', MESSAGE='Injected episode roster batch failure';`
			if expireActor {
				body = `UPDATE sessions SET created_at=clock_timestamp()-interval '2 days',
					expires_at=clock_timestamp()-interval '1 day' WHERE user_id='episode-roster-batch-admin';`
			}
			// A sequence records that the last row reached the injected fault even
			// though all transactional data and actor changes must roll back.
			if _, err := f.pool.Exec(f.ctx, fmt.Sprintf(`CREATE SEQUENCE episode_roster_batch_hits;
				CREATE FUNCTION episode_roster_batch_fault() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN
					IF NEW.import_revision=2 AND NEW.entry_key='episode-1999' THEN
						PERFORM nextval('episode_roster_batch_hits');
						%s
					END IF;
					RETURN NEW;
				END;
				$$;
				CREATE TRIGGER episode_roster_batch_fault BEFORE INSERT OR UPDATE ON expected_episodes
				FOR EACH ROW EXECUTE FUNCTION episode_roster_batch_fault()`, body)); err != nil {
				t.Fatal(err)
			}
			edit := episodeRosterBatchEdit(MaxEpisodeRosterEntries)
			edit.Revision, edit.Source.Revision = "1", "v2"
			_, err := f.store.ReplaceEpisodeRoster(f.ctx, f.actor, "series-b", edit)
			if expireActor {
				if !errors.Is(err, ErrForbidden) || !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("batch committed after final actor failure: %v", err)
				}
			} else {
				var databaseError *pgconn.PgError
				if !errors.As(err, &databaseError) || databaseError.Code != "P0001" {
					t.Fatalf("batch did not report the injected entry failure: %v", err)
				}
			}
			var fired bool
			if err := f.pool.QueryRow(f.ctx, `SELECT is_called FROM episode_roster_batch_hits`).Scan(&fired); err != nil || !fired {
				t.Fatalf("batch did not reach its final row: fired=%t error=%v", fired, err)
			}
			if episodeRosterBatchSnapshot(t, f) != before {
				t.Fatal("failed batch changed facts, retirement, source history or events")
			}
			assertNoCatalogTestNotification(t, notifications)
			if _, err := f.pool.Exec(f.ctx, `DROP TRIGGER episode_roster_batch_fault ON expected_episodes`); err != nil {
				t.Fatal(err)
			}
			if retry := f.replace(t, edit); retry.Revision != "2" || len(retry.Entries) != MaxEpisodeRosterEntries {
				t.Fatal("the same revision could not retry after batch rollback")
			}
			nextCatalogTestNotification(t, notifications)
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

type episodeRosterBatchCancellationTrace struct {
	itemCountTracer
	cancel   context.CancelFunc
	observed bool
	err      error
}

func (trace *episodeRosterBatchCancellationTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.itemCountTracer.TraceQueryStart(ctx, conn, data)
	if trace.cancel != nil && strings.HasPrefix(data.SQL, "INSERT INTO expected_episodes") {
		trace.cancel()
		trace.cancel = nil
		trace.observed, trace.err = true, ctx.Err()
	}
	return ctx
}

func TestEpisodeRosterBatchSurvivesCallerCancellation(t *testing.T) {
	trace := &episodeRosterBatchCancellationTrace{}
	f := episodeRosterBatchFixture(t, trace)
	ownerPID := int32(f.store.ownership.conn.Conn().PgConn().PID())
	request, cancel := context.WithCancel(f.ctx)
	defer cancel()
	trace.cancel = cancel
	detail, err := f.store.ReplaceEpisodeRoster(request, f.actor, "series-b", episodeRosterBatchEdit(MaxEpisodeRosterEntries))
	if err != nil || detail.Revision != "1" || len(detail.Entries) != MaxEpisodeRosterEntries {
		t.Fatalf("cancelled request interrupted the owned batch: revision=%s entries=%d error=%v", detail.Revision, len(detail.Entries), err)
	}
	if !trace.observed || trace.err != nil || !errors.Is(request.Err(), context.Canceled) {
		t.Fatalf("entry write did not isolate cancellation: observed=%t write error=%v caller error=%v", trace.observed, trace.err, request.Err())
	}
	ownedTransactionsAssertReusable(t, f.ctx, f.store, ownerPID)
}
