//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type embeddedArtworkScanSnapshotFixture struct {
	ctx    context.Context
	pool   *pgxpool.Pool
	store  *Store
	state  *scanState
	prober *embeddedArtworkFixtureProber
	item   Item
	path   string
}

func embeddedArtworkScanSnapshotCatalog(t *testing.T) embeddedArtworkScanSnapshotFixture {
	t.Helper()
	picture := embeddedArtworkTestPicture(t, 3, "Front", color.NRGBA{G: 140, B: 90, A: 255})
	prober := &embeddedArtworkFixtureProber{result: media.EmbeddedArtworkResult{
		Version: media.EmbeddedArtworkVersion, Pictures: []media.EmbeddedPicture{picture},
	}}
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, approved, "embedded-snapshot/Track.flac", "audio:source-snapshot")
	library := libraryIntegrationCreate(t, ctx, store, "Embedded snapshot", "music", filepath.Dir(path))
	job := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	scanPerformanceWaitWorkerRetired(t, ctx, store, job.ID)
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	if item.Media == nil {
		t.Fatal("embedded snapshot fixture has no primary media facts")
	}
	state := imageScanTestState(t, ctx, pool, store, library, ".")
	return embeddedArtworkScanSnapshotFixture{ctx: ctx, pool: pool, store: store, state: state, prober: prober, item: item, path: path}
}

type embeddedArtworkScanSnapshotTraceKey struct{}

// A separate connection commits the next generation after the source row was
// read, before the caller can issue any subsequent cache or root query.
type embeddedArtworkScanSnapshotTrace struct {
	embeddedArtworkScanProjectionTrace
	afterRead func() error
	once      sync.Once
	err       error
}

func (trace *embeddedArtworkScanSnapshotTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.embeddedArtworkScanProjectionTrace.TraceQueryStart(ctx, conn, data)
	return context.WithValue(ctx, embeddedArtworkScanSnapshotTraceKey{}, strings.Contains(data.SQL, "/* embedded_artwork_scan_snapshot */"))
}

func (trace *embeddedArtworkScanSnapshotTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if snapshot, _ := ctx.Value(embeddedArtworkScanSnapshotTraceKey{}).(bool); snapshot && data.Err == nil {
		trace.once.Do(func() { trace.err = trace.afterRead() })
	}
}

func TestEmbeddedArtworkScanSnapshotKeepsOneCommittedGeneration(t *testing.T) {
	fixture := embeddedArtworkScanSnapshotCatalog(t)
	before, valid, err := fixture.store.readEmbeddedArtworkScanSnapshot(fixture.ctx, fixture.item.ID)
	if err != nil || !valid || before.source != before.cachedSource || before.cachedStatus != "ready" {
		t.Fatalf("snapshot fixture has no complete cached generation: valid=%t snapshot=%+v error=%v", valid, before, err)
	}
	nextAllowed := filepath.Join(before.snapshot.root.allowedPath, "relocated-base")
	nextRoot := filepath.Join(nextAllowed, "next-generation")
	trace := &embeddedArtworkScanSnapshotTrace{afterRead: func() error {
		writer, err := fixture.pool.Begin(fixture.ctx)
		if err != nil {
			return err
		}
		defer writer.Rollback(fixture.ctx)
		if _, err := writer.Exec(fixture.ctx, `UPDATE library_roots SET path=$2,relative_path=$3,allowed_path=$4,
			binding_revision=binding_revision+1 WHERE id=$1`, before.snapshot.root.id, nextRoot, "next-generation", nextAllowed); err != nil {
			return err
		}
		if _, err := writer.Exec(fixture.ctx, `UPDATE items SET path=$2,relative_path='Next.flac',
			file_identity=file_identity || ':next',file_size=file_size+17,modified_at=modified_at+interval '1 second',
			media=media || jsonb_build_object('Size',file_size+17,'DurationTicks',420000000,
			'FileChangeTimeNs',(media->>'FileChangeTimeNs')::bigint+1) WHERE id=$1`, fixture.item.ID, filepath.Join(nextRoot, "Next.flac")); err != nil {
			return err
		}
		if _, err := writer.Exec(fixture.ctx, `UPDATE item_embedded_artwork e SET
			source_revision=`+embeddedArtworkSourceRevisionSQL+`,extraction_version=e.extraction_version+1,status='none',
			stream_index=NULL,picture_type=NULL,source_hash=NULL,mime_type=NULL,width=NULL,height=NULL,content=NULL
			FROM items i WHERE e.item_id=i.id AND i.id=$1`, fixture.item.ID); err != nil {
			return err
		}
		return writer.Commit(fixture.ctx)
	}}
	configuration := fixture.pool.Config()
	configuration.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(fixture.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, pool)
	reader := &Store{pool: pool}
	trace.reset()
	during, valid, err := reader.readEmbeddedArtworkScanSnapshot(fixture.ctx, fixture.item.ID)
	if err != nil || trace.err != nil || !valid {
		t.Fatalf("read snapshot during committed replacement: valid=%t read=%v writer=%v", valid, err, trace.err)
	}
	trace.assertSnapshotProjection(t)
	if trace.queries.Load() != 1 || trace.begins.Load() != 0 {
		t.Fatalf("source/cache snapshot used multiple statements or an explicit transaction: queries=%d begins=%d", trace.queries.Load(), trace.begins.Load())
	}
	if !reflect.DeepEqual(during, before) {
		t.Fatalf("source, root, hashes, or cache metadata crossed committed generations: before=%+v during=%+v", before, during)
	}
	after, valid, err := fixture.store.readEmbeddedArtworkScanSnapshot(fixture.ctx, fixture.item.ID)
	if err != nil || !valid {
		t.Fatalf("next read did not observe a valid replacement generation: valid=%t error=%v", valid, err)
	}
	if after.snapshot.root.path != nextRoot || after.snapshot.root.allowedPath != nextAllowed || after.snapshot.root.relativePath != "next-generation" ||
		after.snapshot.rootBindingRevision != before.snapshot.rootBindingRevision+1 ||
		after.snapshot.relativePath != "Next.flac" || after.snapshot.mediaFile.Item.Path != filepath.Join(nextRoot, "Next.flac") ||
		after.snapshot.identity != before.snapshot.identity+":next" || after.snapshot.mediaFile.Size != before.snapshot.mediaFile.Size+17 ||
		!after.snapshot.mediaFile.ModifiedAt.Equal(before.snapshot.mediaFile.ModifiedAt.Add(time.Second)) ||
		after.snapshot.mediaFile.Item.Media.Size != after.snapshot.mediaFile.Size || after.snapshot.mediaFile.Item.Media.DurationTicks != 420000000 ||
		after.snapshot.mediaFile.Item.Media.FileChangeTimeNs != before.snapshot.mediaFile.Item.Media.FileChangeTimeNs+1 ||
		after.source == before.source || after.sourceFacts == before.sourceFacts || after.cachedSource != after.source ||
		after.cachedStatus != "none" || after.cachedVersion != before.cachedVersion+1 {
		t.Fatalf("external writer did not replace every source/cache generation field: before=%+v after=%+v", before, after)
	}
}

func TestScanEmbeddedArtworkRejectsSourceChangedWhileSnapshotQueryWaits(t *testing.T) {
	for _, change := range []string{"restored_mtime_write", "pathname_replacement"} {
		t.Run(change, func(t *testing.T) {
			fixture := embeddedArtworkScanSnapshotCatalog(t)
			// Force extraction for a valid source, so a cached result cannot hide
			// an omitted post-query filesystem check.
			fixture.state.task.job.ForceProbe = true
			if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE scan_jobs SET force_probe=true WHERE id=$1", fixture.state.task.job.ID); err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.prepareScanOperationAuthority(fixture.ctx, fixture.state.task, []libraryRoot{fixture.state.root}); err != nil {
				t.Fatal(err)
			}
			file, err := openScanFile(fixture.state.opened, "Track.flac")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			before, err := file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			var cached string
			if err := fixture.pool.QueryRow(fixture.ctx, "SELECT row_to_json(e)::text FROM item_embedded_artwork e WHERE item_id=$1", fixture.item.ID).Scan(&cached); err != nil {
				t.Fatal(err)
			}
			_, extractions := fixture.prober.counts()
			holder, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(fixture.ctx)
			if _, err := holder.Exec(fixture.ctx, "LOCK TABLE item_embedded_artwork IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				defer close(done)
				done <- fixture.state.scanEmbeddedArtworkAttempt(fixture.item.ID, "Audio", "Track.flac", file, *fixture.item.Media)
			}()
			t.Cleanup(func() {
				fixture.state.task.cancel()
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = holder.Rollback(cleanup)
				select {
				case <-done:
				case <-cleanup.Done():
					t.Error("embedded SQL-wait worker did not retire during cleanup")
				}
			})
			waitCatalogApplicationBlock(t, fixture.ctx, fixture.pool, holder.Conn().PgConn().PID())
			var waiting bool
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
				WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%/* embedded_artwork_scan_snapshot */%')`, holder.Conn().PgConn().PID()).Scan(&waiting); err != nil || !waiting {
				t.Fatalf("fixture did not block the actual embedded source snapshot SQL: waiting=%t error=%v", waiting, err)
			}
			if change == "pathname_replacement" {
				if err := os.Rename(fixture.path, fixture.path+".old"); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(fixture.path, []byte("audio:edited-snapshot"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(fixture.path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			current, err := os.Stat(fixture.path)
			if err != nil || current.Size() != before.Size() || !current.ModTime().Equal(before.ModTime()) ||
				change == "restored_mtime_write" && media.FileChangeTime(current) == media.FileChangeTime(before) ||
				change == "pathname_replacement" && os.SameFile(current, before) {
				t.Fatalf("replacement fixture did not preserve size/mtime while changing physical freshness: %v", err)
			}
			if err := holder.Commit(fixture.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-fixture.ctx.Done():
				t.Fatal("embedded scan did not retire after releasing its SQL wait")
			}
			var after string
			if err := fixture.pool.QueryRow(fixture.ctx, "SELECT row_to_json(e)::text FROM item_embedded_artwork e WHERE item_id=$1", fixture.item.ID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if _, actual := fixture.prober.counts(); actual != extractions || fixture.state.warnings != 1 || after != cached {
				t.Fatalf("SQL wait bypassed fresh source checks: extractions=%d/%d warnings=%d cacheChanged=%t", actual, extractions, fixture.state.warnings, after != cached)
			}
		})
	}
}

func TestScanEmbeddedArtworkRejectsInvalidSourceSnapshotsBeforeExtraction(t *testing.T) {
	fixture := embeddedArtworkScanSnapshotCatalog(t)
	before, valid, err := fixture.store.readEmbeddedArtworkScanSnapshot(fixture.ctx, fixture.item.ID)
	if err != nil || !valid {
		t.Fatalf("read valid baseline source: valid=%t error=%v", valid, err)
	}
	encoded, err := json.Marshal(before.snapshot.mediaFile.Item.Media)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, "DELETE FROM item_embedded_artwork WHERE item_id=$1", fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	file, err := openScanFile(fixture.state.opened, "Track.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	_, extractions := fixture.prober.counts()
	for _, test := range []struct{ name, statement string }{
		{"missing_item", ""},
		{"missing_media", `UPDATE items SET media=NULL WHERE id=$1`},
		{"null_media", `UPDATE items SET media='null'::jsonb WHERE id=$1`},
		{"malformed_media", `UPDATE items SET media='"invalid media object"'::jsonb WHERE id=$1`},
		{"missing_streams", `UPDATE items SET media=media-'Streams' WHERE id=$1`},
		{"outdated_probe", `UPDATE items SET media=jsonb_set(media,'{ProbeVersion}','0') WHERE id=$1`},
		{"missing_change_time", `UPDATE items SET media=media-'FileChangeTimeNs' WHERE id=$1`},
		{"missing_identity", `UPDATE items SET file_identity='' WHERE id=$1`},
		{"invalid_size", `UPDATE items SET file_size=0 WHERE id=$1`},
		{"missing_modified", `UPDATE items SET modified_at=NULL WHERE id=$1`},
		{"inconsistent_probe_size", `UPDATE items SET media=jsonb_set(media,'{Size}',to_jsonb(file_size+1)) WHERE id=$1`},
		{"mismatched_item_path", `UPDATE items SET path='/unrelated/Track.flac' WHERE id=$1`},
		{"relative_traversal", `UPDATE items SET relative_path='../Track.flac' WHERE id=$1`},
		{"missing_root", `UPDATE items SET root_id=NULL WHERE id=$1`},
		{"invalid_root", `UPDATE library_roots SET path='/unrelated/root' WHERE id=(SELECT root_id FROM items WHERE id=$1)`},
		{"non_audio", `UPDATE items SET type='Video' WHERE id=$1`},
		{"folder", `UPDATE items SET is_folder=true WHERE id=$1`},
		{"inactive_auxiliary", `INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active) SELECT id,parent_id,'song',false FROM items WHERE id=$1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(func() {
				snapshot := before.snapshot
				if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE items SET root_id=$2,type='Audio',is_folder=false,path=$3,media=$4,
					relative_path=$5,file_identity=$6,file_size=$7,modified_at=$8 WHERE id=$1`, fixture.item.ID, snapshot.root.id,
					snapshot.mediaFile.Item.Path, encoded, snapshot.relativePath, snapshot.identity, snapshot.mediaFile.Size, snapshot.mediaFile.ModifiedAt); err != nil {
					t.Errorf("restore valid primary snapshot: %v", err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, "UPDATE library_roots SET path=$2 WHERE id=$1", snapshot.root.id, snapshot.root.path); err != nil {
					t.Errorf("restore valid root snapshot: %v", err)
				}
				if _, err := fixture.pool.Exec(fixture.ctx, "DELETE FROM item_theme_resources WHERE resource_item_id=$1", fixture.item.ID); err != nil {
					t.Errorf("restore direct item visibility: %v", err)
				}
			})
			if test.statement != "" {
				if _, err := fixture.pool.Exec(fixture.ctx, test.statement, fixture.item.ID); err != nil {
					t.Fatal(err)
				}
			}
			itemID := fixture.item.ID
			if test.name == "missing_item" {
				itemID = "missing-embedded-source"
			}
			fixture.state.warnings = 0
			if err := fixture.state.scanEmbeddedArtworkAttempt(itemID, "Audio", "Track.flac", file, *fixture.item.Media); err != nil {
				t.Fatalf("invalid snapshot did not retain the scan warning contract: %v", err)
			}
			var entries int
			if err := fixture.pool.QueryRow(fixture.ctx, "SELECT count(*) FROM item_embedded_artwork WHERE item_id=$1", fixture.item.ID).Scan(&entries); err != nil {
				t.Fatal(err)
			}
			if _, actual := fixture.prober.counts(); actual != extractions || fixture.state.warnings != 1 || entries != 0 {
				t.Fatalf("invalid source reached extraction or publication: extractions=%d/%d warnings=%d entries=%d", actual, extractions, fixture.state.warnings, entries)
			}
		})
	}
}

func TestScanEmbeddedArtworkReturnsSnapshotQueryFailure(t *testing.T) {
	fixture := embeddedArtworkScanSnapshotCatalog(t)
	file, err := openScanFile(fixture.state.opened, "Track.flac")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	notifications := catalogChangesTestListener(t, fixture.store)
	probes, extractions := fixture.prober.counts()
	// This fixture owns a random schema, removed by its final cleanup. Dropping
	// its leaf cache table invalidates existing prepared plans and makes the
	// actual source query fail without changing the caller's live context.
	if _, err := fixture.pool.Exec(fixture.ctx, "DROP TABLE item_embedded_artwork"); err != nil {
		t.Fatal(err)
	}
	err = fixture.state.scanEmbeddedArtworkAttempt(fixture.item.ID, "Audio", "Track.flac", file, *fixture.item.Media)
	var queryErr *pgconn.PgError
	if !errors.As(err, &queryErr) || queryErr.Code != "42P01" || fixture.state.warnings != 0 {
		t.Fatalf("snapshot query failure became a successful invalid-source warning: error=%v warnings=%d", err, fixture.state.warnings)
	}
	if actualProbes, actualExtractions := fixture.prober.counts(); actualProbes != probes || actualExtractions != extractions {
		t.Fatalf("failed source query reached media work: probes=%d/%d extractions=%d/%d", actualProbes, probes, actualExtractions, extractions)
	}
	assertNoCatalogTestNotification(t, notifications)
}
