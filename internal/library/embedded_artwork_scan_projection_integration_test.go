//go:build linux

package library

import (
	"bytes"
	"context"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/media"
)

type embeddedArtworkScanProjectionTrace struct {
	sources, catalog, subtitles atomic.Int64
	queries, snapshots, begins  atomic.Int64
	statement                   atomic.Value
}

func (trace *embeddedArtworkScanProjectionTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	statement := strings.Join(strings.Fields(data.SQL), " ")
	trace.queries.Add(1)
	if strings.HasPrefix(strings.ToLower(statement), "begin") {
		trace.begins.Add(1)
	}
	if strings.Contains(statement, "/* embedded_artwork_scan_snapshot */") {
		trace.snapshots.Add(1)
		trace.statement.Store(statement)
	}
	if strings.Contains(statement, "AND i.type IN ('Movie', 'Episode', 'Video', 'Audio')") {
		trace.sources.Add(1)
		if strings.Contains(statement, "catalog_entities") {
			trace.catalog.Add(1)
		}
	}
	if strings.Contains(statement, "s.item_id FROM item_subtitles s") ||
		strings.Contains(statement, "s.item_id FROM item_owned_subtitles s") ||
		strings.Contains(statement, "s.item_id FROM item_bitmap_subtitles s") {
		trace.subtitles.Add(1)
	}
	return ctx
}

func (*embeddedArtworkScanProjectionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (trace *embeddedArtworkScanProjectionTrace) reset() {
	trace.sources.Store(0)
	trace.catalog.Store(0)
	trace.subtitles.Store(0)
	trace.queries.Store(0)
	trace.snapshots.Store(0)
	trace.begins.Store(0)
	trace.statement.Store("")
}

func (trace *embeddedArtworkScanProjectionTrace) assertSnapshotProjection(t *testing.T) {
	t.Helper()
	if trace.sources.Load() != 1 || trace.snapshots.Load() != 1 || trace.catalog.Load() != 0 || trace.subtitles.Load() != 0 {
		t.Fatalf("embedded scan did not use one narrow source/cache snapshot: sources=%d snapshots=%d catalog=%d subtitles=%d",
			trace.sources.Load(), trace.snapshots.Load(), trace.catalog.Load(), trace.subtitles.Load())
	}
	statement, _ := trace.statement.Load().(string)
	for _, fragment := range []string{"e.content", "catalog_entities", "item_metadata_state", "item_intro_state", "item_subtitles", "item_owned_subtitles", "item_bitmap_subtitles", "FOR UPDATE", "FOR SHARE", "FROM users", "FROM sessions", "FROM application_keys"} {
		if strings.Contains(statement, fragment) {
			t.Fatalf("embedded source snapshot loaded unused bytes, projection, lock, or authority: %q", fragment)
		}
	}
}

func TestScanEmbeddedArtworkUsesSourceProjectionWithoutCatalogOrSubtitles(t *testing.T) {
	for _, status := range []string{"ready", "none"} {
		t.Run(status, func(t *testing.T) {
			picture := embeddedArtworkTestPicture(t, 3, "Front", color.NRGBA{G: 170, A: 255})
			result := media.EmbeddedArtworkResult{Version: media.EmbeddedArtworkVersion}
			if status == "ready" {
				result.Pictures = []media.EmbeddedPicture{picture}
			}
			prober := &embeddedArtworkFixtureProber{result: result}
			ctx, observer, original, approved, userID := libraryIntegrationStore(t, prober)
			if err := original.Close(ctx); err != nil {
				t.Fatal(err)
			}
			trace := &embeddedArtworkScanProjectionTrace{}
			configuration := observer.Config()
			configuration.ConnConfig.Tracer = trace
			pool, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationPoolCleanup(t, pool)
			store, err := New(pool, prober, []string{approved})
			if err != nil {
				t.Fatal(err)
			}
			entitiesStoreCleanup(t, store)
			path := libraryIntegrationFile(t, approved, "embedded-projection/Track.flac", "audio:projection")
			collection := libraryIntegrationCreate(t, ctx, store, "Embedded source projection", "music", filepath.Dir(path))
			var originalID, originalRevision string
			for _, phase := range []struct {
				name                string
				force               bool
				probes, extractions int
				added, updated      int
			}{
				{"cold", false, 1, 1, 1, 0},
				{"cached", false, 1, 1, 0, 0},
				{"forced", true, 2, 2, 0, 1},
				{"replacement", false, 3, 3, 0, 1},
			} {
				t.Run(phase.name, func(t *testing.T) {
					if phase.name == "replacement" {
						if err := os.WriteFile(path, []byte("audio:replacement-with-different-source-facts"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					trace.reset()
					job, err := store.StartScanWithOptions(ctx, collection.ID, ScanOptions{ForceProbe: phase.force})
					if err != nil {
						t.Fatal(err)
					}
					job = libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
					scanPerformanceWaitWorkerRetired(t, ctx, store, job.ID)
					if job.Error != "" || job.Scanned != 1 || job.Added != phase.added || job.Updated != phase.updated {
						t.Fatalf("source projection changed accepted scan progress: %+v", job)
					}
					trace.assertSnapshotProjection(t)
					if probes, extractions := prober.counts(); probes != phase.probes || extractions != phase.extractions {
						t.Fatalf("source projection changed probe or extraction reuse: probes=%d/%d extractions=%d/%d",
							probes, phase.probes, extractions, phase.extractions)
					}
					var itemID, revision, currentRevision, storedStatus, hash string
					var content []byte
					if err := observer.QueryRow(ctx, `SELECT i.id,e.source_revision,`+embeddedArtworkSourceRevisionSQL+`,
						e.status,COALESCE(e.source_hash,''),e.content FROM items i
						JOIN item_embedded_artwork e ON e.item_id=i.id WHERE i.library_id=$1 AND i.path=$2`, collection.ID, path).
						Scan(&itemID, &revision, &currentRevision, &storedStatus, &hash, &content); err != nil {
						t.Fatal(err)
					}
					if revision == "" || revision != currentRevision || storedStatus != status {
						t.Fatalf("artwork lost its accepted source stamp: revision=%q current=%q status=%q", revision, currentRevision, storedStatus)
					}
					if originalID == "" {
						originalID, originalRevision = itemID, revision
					} else if itemID != originalID || (phase.name == "replacement") != (revision != originalRevision) {
						t.Fatalf("artwork source identity changed incorrectly: item=%q/%q revision=%q/%q", itemID, originalID, revision, originalRevision)
					}
					if status == "ready" {
						if hash != picture.Hash || !bytes.Equal(content, picture.Data) {
							t.Fatal("source projection changed accepted embedded image bytes")
						}
						embeddedArtworkAssertOpen(t, ctx, store, userID, itemID, picture)
					} else if hash != "" || len(content) != 0 {
						t.Fatal("a complete no-picture result retained embedded bytes")
					}
				})
			}
		})
	}
}
