//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type automaticDiscoveryTrace struct {
	mu    sync.Mutex
	seeds map[string]int
}

func (trace *automaticDiscoveryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.ToLower(strings.Join(strings.Fields(data.SQL), " "))
	for _, table := range []string{"background_preview_queue", "audio_waveform_queue", "subtitle_timeline_queue"} {
		if strings.HasPrefix(statement, "insert into "+table+"(item_id)") {
			trace.mu.Lock()
			if trace.seeds == nil {
				trace.seeds = make(map[string]int)
			}
			trace.seeds[table]++
			trace.mu.Unlock()
		}
	}
	return ctx
}

func (*automaticDiscoveryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *automaticDiscoveryTrace) reset() {
	trace.mu.Lock()
	trace.seeds = nil
	trace.mu.Unlock()
}

func (trace *automaticDiscoveryTrace) require(t *testing.T, table string, want int) {
	t.Helper()
	trace.mu.Lock()
	defer trace.mu.Unlock()
	if got := trace.seeds[table]; got != want {
		t.Fatalf("automatic discovery for %s executed %d full inserts, want %d", table, got, want)
	}
}

type automaticDiscoveryProber struct {
	mu      sync.Mutex
	count   int
	blockAt int
	entered chan struct{}
}

func (*automaticDiscoveryProber) ProbeFileJoinedContract() bool { return true }

func (prober *automaticDiscoveryProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return media.Info{}, err
	}
	prober.mu.Lock()
	prober.count++
	block := prober.blockAt != 0 && prober.count == prober.blockAt
	entered := prober.entered
	prober.mu.Unlock()
	if block {
		close(entered)
		<-ctx.Done()
		return media.Info{}, ctx.Err()
	}
	info := libraryMediaFixture(data)
	info.Streams = append(info.Streams, media.Stream{Index: 3, Codec: "hdmv_pgs_subtitle", CodecType: "subtitle", Language: "eng"})
	return info, nil
}

type automaticDiscoveryFamily struct {
	table        string
	prepare      func(*Store, context.Context, AnalysisFence, string) error
	continuation func(*Store, context.Context, AnalysisFence, string) error
	manual       func(*Store, context.Context, identity.Principal, string) (int64, error)
}

func automaticDiscoveryFamilies() []automaticDiscoveryFamily {
	return []automaticDiscoveryFamily{
		{"background_preview_queue", (*Store).PrepareAutomaticBackgroundPreviews, (*Store).RequestBackgroundPreviewContinuation,
			func(store *Store, ctx context.Context, actor identity.Principal, id string) (int64, error) {
				result, err := store.QueueBackgroundPreviews(ctx, actor, AnalysisSelection{ItemIDs: []string{id}}, "")
				return result.Queued, err
			}},
		{"audio_waveform_queue", (*Store).PrepareAutomaticAudioWaveforms, (*Store).RequestAudioWaveformContinuation,
			func(store *Store, ctx context.Context, actor identity.Principal, id string) (int64, error) {
				result, err := store.QueueAudioWaveforms(ctx, actor, AnalysisSelection{ItemIDs: []string{id}}, "")
				return result.Queued, err
			}},
		{"subtitle_timeline_queue", (*Store).PrepareAutomaticSubtitleTimelines, (*Store).RequestSubtitleTimelineContinuation,
			func(store *Store, ctx context.Context, actor identity.Principal, id string) (int64, error) {
				result, err := store.QueueSubtitleTimelines(ctx, actor, AnalysisSelection{ItemIDs: []string{id}}, "")
				return result.Queued, err
			}},
	}
}

type automaticDiscoveryFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *Store
	root    string
	library Library
	actor   identity.Principal
	trace   *automaticDiscoveryTrace
	prober  *automaticDiscoveryProber
}

func newAutomaticDiscoveryFixture(t *testing.T, count int) *automaticDiscoveryFixture {
	t.Helper()
	prober := &automaticDiscoveryProber{}
	wrapped := mediaSourceTestProber{inner: prober}
	ctx, pool, initial, root, _ := libraryIntegrationStoreWithTimeout(t, wrapped, 3*time.Minute)
	if err := initial.Close(ctx); err != nil {
		t.Fatal(err)
	}
	trace := &automaticDiscoveryTrace{}
	configuration := pool.Config()
	configuration.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, traced)
	store, err := New(traced, wrapped, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	fixture := &automaticDiscoveryFixture{ctx: ctx, pool: traced, store: store, root: root, trace: trace, prober: prober}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := fixture.store.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	for index := 0; index < count; index++ {
		fixture.addFile(t, index)
	}
	fixture.library = libraryIntegrationCreate(t, ctx, store, "Automatic sidecar discovery", "movies", filepath.Join(root, "automatic"))
	libraryIntegrationScan(t, ctx, store, fixture.library.ID, "Completed")
	fixture.actor = metadataEditTestActor(t, ctx, traced, "automatic-discovery-administrator")
	// Invalidation is an internal commit boundary, not an installed listener's
	// side effect. All tests intentionally run with no catalog consumer.
	store.SetCatalogChangeListener(nil)
	trace.reset()
	return fixture
}

func (fixture *automaticDiscoveryFixture) addFile(t *testing.T, index int) {
	t.Helper()
	libraryIntegrationFile(t, fixture.root, fmt.Sprintf("automatic/Film-%03d.mp4", index), "video:automatic-discovery")
}

func (fixture *automaticDiscoveryFixture) enable(t *testing.T) {
	t.Helper()
	current, err := fixture.store.GetLibraryEditingAsAdministrator(fixture.ctx, fixture.actor, identity.AdministratorNative, fixture.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	_, err = fixture.store.UpdateLibraryAsAdministrator(fixture.ctx, fixture.actor, identity.AdministratorNative, fixture.library.ID,
		LibraryUpdate{Revision: current.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{
			EnableBackgroundPreviewGeneration: &enabled, EnableAudioWaveformGeneration: &enabled, EnableSubtitleTimelineGeneration: &enabled}})
	if err != nil {
		t.Fatal(err)
	}
}

func (fixture *automaticDiscoveryFixture) requireQueue(t *testing.T, table string, wantTotal, wantPending int) {
	t.Helper()
	var total, pending int
	err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*),count(*) FILTER(WHERE q.requested_revision>q.completed_revision)
		FROM `+pgx.Identifier{table}.Sanitize()+` q JOIN items i ON i.id=q.item_id WHERE i.library_id=$1`, fixture.library.ID).Scan(&total, &pending)
	if err != nil || total != wantTotal || pending != wantPending {
		t.Fatalf("%s queue = %d total / %d pending, want %d / %d: %v", table, total, pending, wantTotal, wantPending, err)
	}
}

func automaticDiscoveryFence(OwnedTx) error { return nil }

func TestAutomaticSidecarDiscoveryReusesContinuationsAndTracksCatalogCommits(t *testing.T) {
	fixture := newAutomaticDiscoveryFixture(t, 17)
	families := automaticDiscoveryFamilies()
	var firstID string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT id FROM items WHERE library_id=$1 AND NOT is_folder ORDER BY id LIMIT 1`, fixture.library.ID).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
			t.Fatal(err)
		}
		fixture.requireQueue(t, family.table, 0, 0)
		if queued, err := family.manual(fixture.store, fixture.ctx, fixture.actor, firstID); err != nil || queued != 1 {
			t.Fatalf("disabled automatic policy lost explicit manual admission: %d, %v", queued, err)
		}
	}
	fixture.enable(t)
	fixture.trace.reset()
	for _, family := range families {
		for turn, pending := range []int{9, 1, 0} {
			if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
				t.Fatal(err)
			}
			// Complete an eight-item turn without changing catalog membership,
			// matching the queue-only effect of the real Complete methods.
			err := fixture.store.WithOwnedTx(fixture.ctx, func(tx OwnedTx) error {
				_, err := tx.Exec(`UPDATE `+pgx.Identifier{family.table}.Sanitize()+` q SET completed_revision=requested_revision,state='ready',finished_at=clock_timestamp()
					WHERE item_id IN (SELECT q.item_id FROM `+pgx.Identifier{family.table}.Sanitize()+` q JOIN items i ON i.id=q.item_id
					WHERE i.library_id=$1 AND q.requested_revision>q.completed_revision ORDER BY q.item_id LIMIT 8)`, fixture.library.ID)
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := family.continuation(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
				t.Fatal(err)
			}
			fixture.requireQueue(t, family.table, 17, pending)
			fixture.trace.require(t, family.table, 1)
			if turn == 0 {
				var manual bool
				if err := fixture.pool.QueryRow(fixture.ctx, `SELECT manual FROM `+pgx.Identifier{family.table}.Sanitize()+` WHERE item_id=$1`, firstID).Scan(&manual); err != nil || !manual {
					t.Fatalf("automatic discovery reset an existing manual record: %v", err)
				}
			}
		}
	}
	// New source commits invalidate discovery even while an earlier automatic
	// item remains pending and continuation events keep arriving.
	for index := 17; index < 19; index++ {
		fixture.addFile(t, index)
		libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
		for _, family := range families {
			if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
				t.Fatal(err)
			}
			if err := family.continuation(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
				t.Fatal(err)
			}
			fixture.requireQueue(t, family.table, index+1, index-16)
			fixture.trace.require(t, family.table, index-15)
		}
	}
	if err := fixture.store.Close(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(fixture.pool, mediaSourceTestProber{inner: fixture.prober}, []string{fixture.root})
	if err != nil {
		t.Fatal(err)
	}
	fixture.store = reopened
	for _, family := range families {
		if err := family.prepare(reopened, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
			t.Fatal(err)
		}
		fixture.trace.require(t, family.table, 4)
		fixture.requireQueue(t, family.table, 19, 2)
	}
}

func TestAutomaticSidecarDiscoverySeesCommittedItemsFromCancelledScan(t *testing.T) {
	fixture := newAutomaticDiscoveryFixture(t, 1)
	fixture.enable(t)
	for _, family := range automaticDiscoveryFamilies() {
		if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
			t.Fatal(err)
		}
	}
	events := func() int64 {
		var sequence int64
		if err := fixture.pool.QueryRow(fixture.ctx, `SELECT sum(sequence) FROM task_system_events WHERE name IN
			('BackgroundPreviewGenerationRequested','AudioWaveformGenerationRequested','SubtitleTimelineGenerationRequested')`).Scan(&sequence); err != nil {
			t.Fatal(err)
		}
		return sequence
	}
	beforeEvents := events()
	fixture.prober.mu.Lock()
	fixture.prober.count, fixture.prober.blockAt, fixture.prober.entered = 0, 10, make(chan struct{})
	entered := fixture.prober.entered
	fixture.prober.mu.Unlock()
	for index := 1; index <= 20; index++ {
		fixture.addFile(t, index)
	}
	job, err := fixture.store.StartScan(fixture.ctx, fixture.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-fixture.ctx.Done():
		t.Fatal("the partial scan never reached its blocked probe")
	}
	var committed int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM items WHERE library_id=$1 AND NOT is_folder`, fixture.library.ID).Scan(&committed); err != nil || committed <= 1 || committed >= 21 {
		t.Fatalf("the cancellation fixture did not publish a strict prefix: %d, %v", committed, err)
	}
	if err := fixture.store.CancelJob(fixture.ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationWaitJob(t, fixture.ctx, fixture.store, job.ID, "Cancelled")
	if events() != beforeEvents {
		t.Fatal("the cancelled scan unexpectedly supplied a completed-scan discovery event")
	}
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM items WHERE library_id=$1 AND NOT is_folder`, fixture.library.ID).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	for _, family := range automaticDiscoveryFamilies() {
		if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
			t.Fatal(err)
		}
		fixture.trace.require(t, family.table, 2)
		fixture.requireQueue(t, family.table, committed, committed)
	}
}

func automaticDiscoveryResync(t *testing.T, fixture *automaticDiscoveryFixture, table string, commit bool) {
	t.Helper()
	tx, err := fixture.store.beginOwnedTx(fixture.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(fixture.ctx)
	if table != "" {
		if _, err := tx.Exec(fixture.ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()); err != nil {
			t.Fatal(err)
		}
	}
	tx.(*ownedTx).catalogChanges.requireResync()
	if commit {
		if err := tx.Commit(fixture.ctx); err != nil {
			t.Fatal(err)
		}
	} else if err := tx.Rollback(fixture.ctx); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticSidecarDiscoveryFailedPreparationDoesNotRemember(t *testing.T) {
	fixture := newAutomaticDiscoveryFixture(t, 1)
	fixture.enable(t)
	if _, err := fixture.pool.Exec(fixture.ctx, `CREATE FUNCTION automatic_discovery_reject() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN RAISE EXCEPTION 'automatic discovery test rejection'; RETURN NULL; END $$`); err != nil {
		t.Fatal(err)
	}
	for _, family := range automaticDiscoveryFamilies() {
		for _, phase := range []string{"statement", "final-fence", "caller-cancel", "commit"} {
			t.Run(family.table+"/"+phase, func(t *testing.T) {
				automaticDiscoveryResync(t, fixture, family.table, true)
				fixture.trace.reset()
				if phase == "statement" || phase == "commit" {
					definition := `CREATE TRIGGER automatic_discovery_failure BEFORE INSERT ON ` + pgx.Identifier{family.table}.Sanitize() + ` FOR EACH STATEMENT EXECUTE FUNCTION automatic_discovery_reject()`
					if phase == "commit" {
						definition = `CREATE CONSTRAINT TRIGGER automatic_discovery_failure AFTER INSERT ON ` + pgx.Identifier{family.table}.Sanitize() + ` DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION automatic_discovery_reject()`
					}
					if _, err := fixture.pool.Exec(fixture.ctx, definition); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						if _, err := fixture.pool.Exec(fixture.ctx, `DROP TRIGGER IF EXISTS automatic_discovery_failure ON `+pgx.Identifier{family.table}.Sanitize()); err != nil {
							t.Error(err)
						}
					})
				}
				ctx, cancel := context.WithCancel(fixture.ctx)
				defer cancel()
				checks := 0
				fenceFailure := errors.New("automatic discovery final fence rejected")
				fence := func(OwnedTx) error {
					checks++
					if checks == 2 {
						if phase == "final-fence" {
							return fenceFailure
						}
						if phase == "caller-cancel" {
							cancel()
						}
					}
					return nil
				}
				err := family.prepare(fixture.store, ctx, fence, fixture.library.ID)
				if err == nil || phase == "final-fence" && !errors.Is(err, fenceFailure) || phase == "caller-cancel" && !errors.Is(err, context.Canceled) {
					t.Fatalf("failed automatic preparation returned the wrong outcome: %v", err)
				}
				fixture.trace.require(t, family.table, 1)
				fixture.requireQueue(t, family.table, 0, 0)
				if phase == "statement" || phase == "commit" {
					if _, err := fixture.pool.Exec(fixture.ctx, `DROP TRIGGER automatic_discovery_failure ON `+pgx.Identifier{family.table}.Sanitize()); err != nil {
						t.Fatal(err)
					}
				}
				if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
					t.Fatal(err)
				}
				fixture.trace.require(t, family.table, 2)
				fixture.requireQueue(t, family.table, 1, 1)
				// A rolled-back invalidation cannot turn a successful hint into a
				// miss; an actual resync commit must invalidate it immediately.
				automaticDiscoveryResync(t, fixture, "", false)
				if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
					t.Fatal(err)
				}
				fixture.trace.require(t, family.table, 2)
				automaticDiscoveryResync(t, fixture, "", true)
				if err := family.prepare(fixture.store, fixture.ctx, automaticDiscoveryFence, fixture.library.ID); err != nil {
					t.Fatal(err)
				}
				fixture.trace.require(t, family.table, 3)
			})
		}
	}
}
