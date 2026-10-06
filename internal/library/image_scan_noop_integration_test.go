//go:build linux

package library

import (
	"context"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func imageScanNoopRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, imageType string) string {
	t.Helper()
	var row string
	if err := pool.QueryRow(ctx, `SELECT to_jsonb(im)::text FROM item_images im
		WHERE item_id=$1 AND image_type=$2 AND image_index=0`, itemID, imageType).Scan(&row); err != nil {
		t.Fatal(err)
	}
	return row
}

func TestScanImageNoopRepairsSameHashRowDifferences(t *testing.T) {
	ctx, pool, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Feature.mp4", "video:image-noop-row-repair")
	imageScanTestWrite(t, filepath.Join(directory, "Feature-poster.png"), color.White)
	imageScanTestWrite(t, filepath.Join(directory, "backdrop.png"), color.Black)
	oldRoot := imageStoreTestRoot(t, ctx, pool, store, filepath.Dir(directory), "retired-noop-image-root")
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Feature.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	wantRow := imageScanNoopRow(t, ctx, pool, itemID, "Primary")
	notifications := catalogChangesTestListener(t, store)
	scan := func(t *testing.T) {
		t.Helper()
		state := imageScanTestState(t, ctx, pool, store, library, ".")
		trace.reset()
		if err := state.scanImages(itemID, "Movie", "Feature.mp4", false); err != nil || state.warnings != 0 {
			t.Fatalf("scan image row repair: warnings=%d error=%v", state.warnings, err)
		}
	}
	for _, scenario := range []struct {
		name       string
		assignment string
		value      any
		notify     bool
	}{
		{"identity", "file_identity=$2", "stale-noop-image-identity", false},
		{"mtime", "modified_at=$2", time.Unix(1, 0).UTC(), false},
		{"mime", "mime_type=$2", "image/jpeg", false},
		{"old_root", "root_id=$2", oldRoot.id, true},
		{"relative_path", "relative_path=$2", "stale-poster.png", true},
		{"size", "file_size=file_size + $2", int64(1), true},
		{"width", "width=width + $2", 1, true},
		{"height", "height=height + $2", 1, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if tag, err := pool.Exec(ctx, "UPDATE item_images SET "+scenario.assignment+" WHERE item_id=$1 AND image_type='Primary'", itemID, scenario.value); err != nil || tag.RowsAffected() != 1 {
				t.Fatalf("prepare one stale image row: rows=%d error=%v", tag.RowsAffected(), err)
			}
			before := scanImageDiffVersions(t, ctx, pool, itemID)
			scan(t)
			if got := imageScanNoopRow(t, ctx, pool, itemID, "Primary"); got != wantRow {
				t.Fatalf("same-hash repair did not restore all source fields: got=%s want=%s", got, wantRow)
			}
			after := scanImageDiffVersions(t, ctx, pool, itemID)
			if len(after) != 2 || after["Primary:0"] == before["Primary:0"] || after["Backdrop:0"] != before["Backdrop:0"] ||
				trace.inserts.Load() != 1 || trace.deletes.Load() != 1 {
				t.Fatalf("same-hash repair skipped its row or rewrote an unchanged image: before=%v after=%v inserts=%d deletes=%d",
					before, after, trace.inserts.Load(), trace.deletes.Load())
			}
			if scenario.notify {
				assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
					Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: library.ID,
				}})
			}
			assertNoCatalogTestNotification(t, notifications)
			scan(t)
			if current := scanImageDiffVersions(t, ctx, pool, itemID); !reflect.DeepEqual(current, after) || trace.inserts.Load() != 0 || trace.deletes.Load() != 0 {
				t.Fatalf("repaired warm rowset did not become a no-op: before=%v after=%v inserts=%d deletes=%d",
					after, current, trace.inserts.Load(), trace.deletes.Load())
			}
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}

func TestScanImageNoopPreservesInvalidTypeAndDeletesExtraIndex(t *testing.T) {
	ctx, pool, store, library, directory, trace := scanNewItemImageFixture(t)
	libraryIntegrationFile(t, directory, "Feature.mp4", "video:image-noop-preserved-type")
	poster := filepath.Join(directory, "Feature-poster.png")
	imageScanTestWrite(t, poster, color.White)
	imageScanTestWrite(t, filepath.Join(directory, "backdrop.png"), color.Black)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	var itemID string
	if err := pool.QueryRow(ctx, "SELECT id FROM items WHERE library_id=$1 AND relative_path='Feature.mp4'", library.ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE item_images SET file_identity='retained-invalid-source' WHERE item_id=$1 AND image_type='Primary'", itemID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(poster, []byte("invalid replacement for the preserved type"), 0600); err != nil {
		t.Fatal(err)
	}
	preserved := imageScanNoopRow(t, ctx, pool, itemID, "Primary")
	before := scanImageDiffVersions(t, ctx, pool, itemID)
	notifications := catalogChangesTestListener(t, store)
	scan := func() {
		t.Helper()
		state := imageScanTestState(t, ctx, pool, store, library, ".")
		trace.reset()
		if err := state.scanImages(itemID, "Movie", "Feature.mp4", false); err != nil || state.warnings != 1 {
			t.Fatalf("scan preserved invalid type: warnings=%d error=%v", state.warnings, err)
		}
	}
	scan()
	if after := scanImageDiffVersions(t, ctx, pool, itemID); !reflect.DeepEqual(after, before) || trace.inserts.Load() != 0 || trace.deletes.Load() != 0 {
		t.Fatalf("a preserved invalid type forced a write: before=%v after=%v inserts=%d deletes=%d",
			before, after, trace.inserts.Load(), trace.deletes.Load())
	}
	assertNoCatalogTestNotification(t, notifications)
	if _, err := pool.Exec(ctx, `INSERT INTO item_images
		(item_id, root_id, image_type, image_index, relative_path, file_identity, source_hash,
		file_size, modified_at, width, height, mime_type)
		SELECT item_id, root_id, image_type, 31, relative_path, file_identity, source_hash,
		file_size, modified_at, width, height, mime_type
		FROM item_images WHERE item_id=$1 AND image_type='Backdrop' AND image_index=0`, itemID); err != nil {
		t.Fatal(err)
	}
	scan()
	if after := scanImageDiffVersions(t, ctx, pool, itemID); !reflect.DeepEqual(after, before) || trace.inserts.Load() != 1 || trace.deletes.Load() != 1 {
		t.Fatalf("extra index deletion lost a retained row or changed a surviving tuple: before=%v after=%v inserts=%d deletes=%d",
			before, after, trace.inserts.Load(), trace.deletes.Load())
	}
	if got := imageScanNoopRow(t, ctx, pool, itemID, "Primary"); got != preserved {
		t.Fatalf("independent deletion rewrote the preserved invalid type: before=%s after=%s", preserved, got)
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{
		Kind: CatalogUpdated, ItemID: itemID, LibraryID: library.ID, ParentID: library.ID,
	}})
	assertNoCatalogTestNotification(t, notifications)
	scan()
	if after := scanImageDiffVersions(t, ctx, pool, itemID); !reflect.DeepEqual(after, before) || trace.inserts.Load() != 0 || trace.deletes.Load() != 0 {
		t.Fatalf("trimmed rowset did not return to a preserved no-op: before=%v after=%v inserts=%d deletes=%d",
			before, after, trace.inserts.Load(), trace.deletes.Load())
	}
	assertNoCatalogTestNotification(t, notifications)
}

type imageScanNoopProofTrace struct {
	publication scanPublicationIOTrace
	authority   primarySidecarQueuedAuthorityTrace
}

func (trace *imageScanNoopProofTrace) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.authority.TraceQueryStart(ctx, conn, data)
	return trace.publication.TraceQueryStart(ctx, conn, data)
}

func (trace *imageScanNoopProofTrace) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.publication.TraceQueryEnd(ctx, conn, data)
	trace.authority.TraceQueryEnd(ctx, conn, data)
}

func TestScanImageNoopRetainsOwnedTransactionAndFinalSourceProof(t *testing.T) {
	for _, scenario := range []string{"stable", "replaced_source", "cancelled_context"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, pool, originalStore, approved, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			mediaPath := libraryIntegrationFile(t, approved, "movies/Film.mp4", "video:image-noop-final-proof")
			directory := filepath.Dir(mediaPath)
			poster := filepath.Join(directory, "Film-poster.png")
			contents := imageScanTestWrite(t, poster, color.White)
			library := libraryIntegrationCreate(t, ctx, originalStore, "Image no-op final proof", "movies", directory)
			libraryIntegrationScan(t, ctx, originalStore, library.ID, "Completed")
			item := nfoCatalogItem(t, ctx, originalStore, userID, library.ID, mediaPath)
			before := scanImageDiffVersions(t, ctx, pool, item.ID)
			beforeRow := imageScanNoopRow(t, ctx, pool, item.ID, "Primary")
			if err := originalStore.Close(ctx); err != nil {
				t.Fatal(err)
			}
			trace := &imageScanNoopProofTrace{}
			trace.publication.rolledBack = make(chan struct{})
			configuration := pool.Config()
			configuration.ConnConfig.Tracer = trace
			tracedPool, err := pgxpool.NewWithConfig(ctx, configuration)
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationPoolCleanup(t, tracedPool)
			store, err := New(tracedPool, &libraryFixtureProber{}, []string{approved})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := store.Close(cleanup); err != nil {
					t.Errorf("close image no-op proof store: %v", err)
				}
			})
			stamp, err := os.Stat(poster)
			if err != nil {
				t.Fatal(err)
			}
			replacement := filepath.Join(directory, ".poster-replacement")
			if scenario == "replaced_source" {
				if err := os.WriteFile(replacement, contents, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(replacement, stamp.ModTime(), stamp.ModTime()); err != nil {
					t.Fatal(err)
				}
			}
			state := imageScanTestState(t, ctx, pool, store, library, ".")
			notifications := catalogChangesTestListener(t, store)
			owner := store.ownership.conn.Conn()
			trace.authority.owner.Store(owner)
			trace.publication.expectedOwner = owner
			trace.publication.libraryID, trace.publication.rootID, trace.publication.itemID = library.ID, state.root.id, item.ID
			var inject func() error
			switch scenario {
			case "replaced_source":
				inject = func() error {
					if err := os.Rename(replacement, poster); err != nil {
						return err
					}
					// Keep the directory mtime stable while replacing the source
					// after inspection. The final source proof must still reject it.
					modified := state.directoryIdentities["."].ModTime()
					return os.Chtimes(directory, modified, modified)
				}
			case "cancelled_context":
				inject = func() error {
					state.task.cancel()
					return nil
				}
			}
			trace.authority.reset()
			trace.publication.arm(inject)
			err = state.scanImages(item.ID, item.Type, "Film.mp4", false)
			wantCommits, wantRollbacks, wantWarnings := int64(1), 0, 0
			if scenario != "stable" {
				wantCommits, wantRollbacks = 0, 1
			}
			if scenario == "replaced_source" {
				wantWarnings = 1
			}
			if scenario == "cancelled_context" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("image no-op ignored cancellation after its owned root read: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			roots, _, _, rollbacks, traceErr := trace.publication.snapshot()
			if traceErr != nil || roots != 1 || rollbacks != wantRollbacks || state.warnings != wantWarnings ||
				trace.authority.ownedBegins.Load() != 1 || trace.authority.ownedCommits.Load() != wantCommits || trace.authority.imageWrites.Load() != 0 {
				t.Fatalf("image no-op lost its owned publication or final source proof: roots=%d begins=%d commits=%d rollbacks=%d writes=%d warnings=%d trace_error=%v",
					roots, trace.authority.ownedBegins.Load(), trace.authority.ownedCommits.Load(), rollbacks, trace.authority.imageWrites.Load(), state.warnings, traceErr)
			}
			if after := scanImageDiffVersions(t, ctx, pool, item.ID); !reflect.DeepEqual(after, before) || imageScanNoopRow(t, ctx, pool, item.ID, "Primary") != beforeRow {
				t.Fatalf("image no-op proof changed the retained rowset: before=%v after=%v", before, after)
			}
			assertNoCatalogTestNotification(t, notifications)
		})
	}
}
