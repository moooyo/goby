//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func versionedEpisodePairTestProbe(_ context.Context, file *os.File) (media.Info, error) {
	return forceProbeTestInfo(file)
}

func createEpisodePairTestLibrary(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, root string) Library {
	t.Helper()
	actor := metadataEditTestActor(t, ctx, pool, "pair-library-editor")
	collection, err := store.CreateLibraryWithOptionsAsAdministrator(ctx, actor, identity.AdministratorNative,
		"Synthetic episode pair", "tvshows", []string{root}, LibraryOptions{EnableLocalMetadata: true})
	if err != nil {
		t.Fatal(err)
	}
	return collection
}

func pairedTestNFO(number int, title, rating, tag string) string {
	return fmt.Sprintf(`<episodedetails><season>1</season><episode>%d</episode><title>%s</title><plot>Plot %s</plot><tmdbid>%d</tmdbid><mpaa>%s</mpaa><tag>%s</tag></episodedetails>`,
		number, title, title, 1000+number, rating, tag)
}

func writeEpisodePairTestNFO(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertEpisodePairPending(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, job string, want bool) {
	t.Helper()
	var pending bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$1 AND import_nfo AND ($2='' OR scan_job_id=$2))`, id, job).
		Scan(&pending); err != nil || pending != want {
		t.Fatalf("episode NFO pending = %v, want %v; error=%v", pending, want, err)
	}
}

func runEpisodePairTestImport(t *testing.T, ctx context.Context, pool *pgxpool.Pool, store *Store, collection Library) error {
	t.Helper()
	child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
	result := store.ImportLocalScanMetadata(ctx, child, collection.ID, fence, func(ProviderProgress) error { return nil })
	if _, err := pool.Exec(ctx, `DELETE FROM task_runs WHERE id=(SELECT run_id FROM task_run_children WHERE id=$1)`, child); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestEpisodePairNFOScanAndImportKeepAccessPendingUntilVerified(t *testing.T) {
	for _, mode := range []struct {
		name     string
		deferred bool
	}{{"synchronous", false}, {"deferred", true}} {
		t.Run(mode.name, func(t *testing.T) {
			ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			root := filepath.Join(approved, "tv")
			const relative = "Show/Season 01/Show - S01E03E04 - 第 3-4 集.mp4"
			mediaPath := libraryIntegrationFile(t, root, relative, "one physical video")
			nfoPath := strings.TrimSuffix(mediaPath, ".mp4") + ".nfo"
			first := pairedTestNFO(3, "First", "PG", "Family")
			valid := first + pairedTestNFO(4, "Second", "PG", "Family")
			writeEpisodePairTestNFO(t, nfoPath, first+pairedTestNFO(4, "Wrong rating", "R", "Family"))
			collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
			store.deferLocalMetadata = mode.deferred
			viewers := map[string]string{"rating-viewer": `{"MaxParentalRating":5}`, "tag-viewer": `{"BlockedTags":["Restricted"]}`}
			for user, policy := range viewers {
				libraryIntegrationUser(t, ctx, pool, user, false, true, nil)
				if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||$2::jsonb WHERE id=$1`, user, policy); err != nil {
					t.Fatal(err)
				}
			}
			read := func() Item { return nfoCatalogItem(t, ctx, store, admin, collection.ID, mediaPath) }
			checkHidden := func(item Item, job string) {
				t.Helper()
				assertEpisodePairPending(t, ctx, pool, item.ID, job, true)
				for user := range viewers {
					if _, err := store.GetItem(ctx, user, item.ID); !errors.Is(err, ErrNotFound) {
						t.Fatalf("%s could read unverified episode: %v", user, err)
					}
					items := libraryIntegrationQuery(t, ctx, store, Query{UserID: user, ParentID: collection.ID,
						Recursive: true, IncludeItemTypes: []string{"Episode"}})
					if items.TotalRecordCount != 0 || len(items.Items) != 0 {
						t.Fatalf("%s saw unverified episode in results: %+v", user, items)
					}
					file, _, err := store.OpenMedia(ctx, user, item.ID, "")
					if file != nil {
						_ = file.Close()
					}
					if file != nil || !errors.Is(err, ErrNotFound) {
						t.Fatalf("%s could open unverified episode: %v", user, err)
					}
				}
			}
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			before := read()
			season := nfoCatalogItem(t, ctx, store, admin, collection.ID, filepath.Dir(mediaPath))
			if season.Type != "Season" || season.IndexNumber != 1 || before.ParentID != season.ID || before.IndexNumber != 3 || before.ParentIndexNumber != 1 {
				t.Fatalf("the labeled pair did not retain its physical season and starting number: %+v", before)
			}
			if mode.deferred && runEpisodePairTestImport(t, ctx, pool, store, collection) == nil {
				t.Fatal("deferred importer accepted differing official ratings")
			}
			checkHidden(before, job.ID)
			if got := read(); got.Metadata != nil || got.Name != "Show - S01E03E04 - 第 3-4 集" {
				t.Fatalf("conflicting first visit published part of the NFO: %+v", got)
			}
			if end, ok := before.EpisodeEndNumber(); !ok || end != 4 {
				t.Fatalf("bad NFO changed the filename range: %d, %t", end, ok)
			}

			writeEpisodePairTestNFO(t, nfoPath, valid)
			job = libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if mode.deferred {
				checkHidden(before, job.ID)
				if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
					t.Fatal(err)
				}
			}
			accepted := read()
			if accepted.ID != before.ID || accepted.ParentID != before.ParentID || accepted.Name != "First,  Second" ||
				accepted.Overview != "Plot First\n\nPlot Second" || accepted.Metadata == nil || accepted.Metadata.OfficialRating != "PG" ||
				!maps.Equal(accepted.Metadata.ProviderIDs, map[string]string{"Tmdb": "1003"}) || !reflect.DeepEqual(accepted.Metadata.Tags, []string{"Family"}) {
				t.Fatalf("wrong accepted episode pair: %+v", accepted)
			}
			assertEpisodePairPending(t, ctx, pool, accepted.ID, "", false)
			const viewer = "rating-viewer"
			position := int64(123456)
			if _, err := store.UpdateUserDataFor(ctx, Subject{UserID: viewer}, accepted.ID, UserDataPatch{PlaybackPositionTicks: &position}); err != nil {
				t.Fatal(err)
			}
			file, source, err := store.OpenMedia(ctx, viewer, accepted.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(file)
			if closeErr := file.Close(); err != nil || closeErr != nil || string(data) != "one physical video" || source.Item.Path != mediaPath {
				t.Fatalf("merged episode did not open the original video: %v %v", err, closeErr)
			}

			// A warm scan can retain the old description even though it must set a
			// new pending gate. The tag conflict is not reflected in either title.
			var parentRefresh atomic.Bool
			store.SetCatalogChangeListener(func(notification CatalogNotification) {
				for _, change := range notification.Changes {
					if change.ItemID == accepted.ParentID && change.ChildrenRemoved {
						parentRefresh.Store(true)
					}
				}
			})
			defer store.SetCatalogChangeListener(nil)
			writeEpisodePairTestNFO(t, nfoPath, first+pairedTestNFO(4, "Wrong tag", "PG", "Restricted"))
			job = libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if job.Updated != 0 {
				t.Fatalf("invalid same-binding edit did not exercise the cached visit: %+v", job)
			}
			if mode.deferred && runEpisodePairTestImport(t, ctx, pool, store, collection) == nil {
				t.Fatal("deferred importer accepted differing tags")
			}
			checkHidden(accepted, job.ID)
			if !parentRefresh.Load() {
				t.Fatal("rejection did not tell visible parents to refresh membership")
			}
			if got := read(); got.Name != accepted.Name || !reflect.DeepEqual(got.Metadata, accepted.Metadata) {
				t.Fatalf("invalid replacement changed accepted facts: %+v", got)
			}
			if _, err := pool.Exec(ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableLocalMetadata}','false'::jsonb), revision=revision+1 WHERE id=$1`, collection.ID); err != nil {
				t.Fatal(err)
			}
			job = libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err == nil {
				t.Fatal("a disabled importer claimed the pending NFO was verified")
			}
			checkHidden(accepted, job.ID)

			// The synchronous path must recover even when no background importer
			// was registered at startup or the marker came from a completed job.
			if _, err := pool.Exec(ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableLocalMetadata}','true'::jsonb), revision=revision+1 WHERE id=$1`, collection.ID); err != nil {
				t.Fatal(err)
			}
			store.deferLocalMetadata = false
			writeEpisodePairTestNFO(t, nfoPath, valid)
			job = libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if job.Updated != 0 {
				t.Fatalf("revalidating identical facts was not a cached visit: %+v", job)
			}
			assertEpisodePairPending(t, ctx, pool, accepted.ID, "", false)
			writeEpisodePairTestNFO(t, nfoPath, first)
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if got := read(); got.Name != "First" || got.ID != accepted.ID {
				t.Fatalf("verified single document did not replace the merge: %+v", got)
			}
			var merged bool
			if err := pool.QueryRow(ctx, `SELECT local_metadata ? '_EpisodePairBinding' FROM items WHERE id=$1`, accepted.ID).
				Scan(&merged); err != nil || merged {
				t.Fatalf("single document retained merged source provenance: %t %v", merged, err)
			}
			if err := os.Remove(nfoPath); err != nil {
				t.Fatal(err)
			}
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			assertEpisodePairPending(t, ctx, pool, accepted.ID, "", false)
			item, err := store.GetItem(ctx, viewer, accepted.ID)
			if err != nil || item.ParentID != accepted.ParentID || item.Metadata != nil || item.Name != before.Name ||
				item.UserData == nil || item.UserData.PlaybackPositionTicks != position {
				t.Fatalf("verified absence lost hierarchy or watch state, or kept merged facts: %+v %v", item, err)
			}
		})
	}
}

func TestEpisodePairNFOOnlyAcceptsPhysicalSeasonBindings(t *testing.T) {
	for _, layout := range []struct {
		name, relative string
		merge          bool
	}{
		{"directly under registered show root", "Season 01/Show.S01E03E04.mp4", true},
		{"flat virtual season", "Show/Show.S01E03E04.mp4", false},
		{"descendant of a real season", "Show/Season 01/Disc 1/Show.S01E03E04.mp4", false},
		{"different season directory", "Show/Season 02/Show.S01E03E04.mp4", false},
	} {
		t.Run(layout.name, func(t *testing.T) {
			ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			root := filepath.Join(approved, "tv")
			mediaPath := libraryIntegrationFile(t, root, layout.relative, "one physical video")
			writeEpisodePairTestNFO(t, strings.TrimSuffix(mediaPath, ".mp4")+".nfo",
				pairedTestNFO(3, "First", "PG", "Family")+pairedTestNFO(4, "Second", "PG", "Family"))
			collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			item := nfoCatalogItem(t, ctx, store, admin, collection.ID, mediaPath)
			assertEpisodePairPending(t, ctx, pool, item.ID, job.ID, !layout.merge)
			if end, ok := item.EpisodeEndNumber(); !ok || end != 4 {
				t.Fatalf("layout altered the independent display range: %d, %t", end, ok)
			}
			if layout.merge && item.Name != "First,  Second" || !layout.merge && item.Metadata != nil {
				t.Fatalf("wrong NFO acceptance for %s: %+v", layout.name, item)
			}
		})
	}
}

func TestEpisodePairNFORenameDoesNotCarryMergedFacts(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "tv")
	mediaPath := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "one physical video")
	first, second := pairedTestNFO(3, "First", "PG", "Family"), pairedTestNFO(4, "Second", "PG", "Family")
	writeEpisodePairTestNFO(t, strings.TrimSuffix(mediaPath, ".mp4")+".nfo", first+second)
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, admin, collection.ID, mediaPath)
	if before.Name != "First,  Second" {
		t.Fatalf("fixture did not establish an accepted pair: %q", before.Name)
	}
	newPath := filepath.Join(filepath.Dir(mediaPath), "Renamed.S01E03-E04.mp4")
	if err := os.Rename(mediaPath, newPath); err != nil {
		t.Fatal(err)
	}
	writeEpisodePairTestNFO(t, strings.TrimSuffix(newPath, ".mp4")+".nfo", second+first)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	after := nfoCatalogItem(t, ctx, store, admin, collection.ID, newPath)
	if after.ID != before.ID || after.ParentID != before.ParentID || after.Name != "Renamed S01E03-E04" || after.Metadata != nil {
		t.Fatalf("changed binding shared the old merge or lost the retained item ID: %+v", after)
	}
	assertEpisodePairPending(t, ctx, pool, after.ID, job.ID, true)
}

func TestEpisodePairNFODoesNotRetainFactsAfterRootRevisionChanges(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "tv")
	path := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "the same physical video")
	nfo := strings.TrimSuffix(path, ".mp4") + ".nfo"
	first, second := pairedTestNFO(3, "First", "PG", "Family"), pairedTestNFO(4, "Second", "PG", "Family")
	writeEpisodePairTestNFO(t, nfo, first+second)
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if before.Name != "First,  Second" {
		t.Fatalf("fixture did not establish an accepted pair: %q", before.Name)
	}
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	writeEpisodePairTestNFO(t, nfo, second+first)
	store.deferLocalMetadata = true
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	after := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if after.ID != before.ID || after.ParentID != before.ParentID || after.Metadata != nil {
		t.Fatalf("new root revision retained old local metadata or lost stable identity: %+v", after)
	}
	assertEpisodePairPending(t, ctx, pool, after.ID, job.ID, true)
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err == nil {
		t.Fatal("deferring under the new root revision accepted invalid episode order")
	}
	assertEpisodePairPending(t, ctx, pool, after.ID, job.ID, true)
}

func TestEpisodePairNFOCannotRetainAMergeAcrossAReplacedPhysicalRoot(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "tv")
	path := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "retained video bytes")
	nfo := strings.TrimSuffix(path, ".mp4") + ".nfo"
	first, second := pairedTestNFO(3, "First", "PG", "Family"), pairedTestNFO(4, "Second", "PG", "Family")
	writeEpisodePairTestNFO(t, nfo, first+second)
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	// Exercise the unbound fallback, where this scan can open a new physical
	// root without a topology document proving the original inode.
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET storage_binding=NULL,bound_at=NULL,bound_by=NULL WHERE library_id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if before.Name != "First,  Second" {
		t.Fatal("fixture has no accepted merge")
	}
	retired := filepath.Join(approved, "retired")
	if err := os.Rename(root, retired); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(retired, "Show"), filepath.Join(root, "Show")); err != nil {
		t.Fatal(err)
	}
	writeEpisodePairTestNFO(t, nfo, second+first)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	after := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if after.ID != before.ID || after.Metadata != nil {
		t.Fatalf("retained a merge across a physical root replacement: %+v", after)
	}
	assertEpisodePairPending(t, ctx, pool, after.ID, job.ID, true)
}

func TestEpisodePairNFORootRemapBlocksStaleRatingBeforeRescan(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "before")
	const relative = "Show/Season 01/Show.S01E03E04.mp4"
	path := libraryIntegrationFile(t, root, relative, "retained video")
	nfo := strings.TrimSuffix(path, ".mp4") + ".nfo"
	writeEpisodePairTestNFO(t, nfo, pairedTestNFO(3, "First", "PG", "Family")+pairedTestNFO(4, "Second", "PG", "Family"))
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if before.Name != "First,  Second" {
		t.Fatal("fixture has no accepted pair")
	}
	const viewer = "remapped-pair-viewer"
	libraryIntegrationUser(t, ctx, pool, viewer, false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, viewer, before.ID); err != nil {
		t.Fatal(err)
	}
	newRoot := filepath.Join(approved, "after")
	if err := os.Rename(root, newRoot); err != nil {
		t.Fatal(err)
	}
	actor := metadataEditTestActor(t, ctx, pool, "pair-root-remap-admin")
	settings, err := store.GetLibraryEditing(ctx, collection.ID)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{newRoot}
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID, LibraryUpdate{
		Revision: settings.Library.Revision, Paths: &paths, PathReplacements: []LibraryPathReplacement{{From: root, To: newRoot}},
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetItem(ctx, admin, before.ID); err != nil || got.Path != filepath.Join(newRoot, relative) {
		t.Fatalf("root remap lost the stable item: %+v %v", got, err)
	}
	assertEpisodePairPending(t, ctx, pool, before.ID, "", false) // A remap must not authorize an import.
	if _, err := store.GetItem(ctx, viewer, before.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale rating authorized the remapped source: %v", err)
	}
	file, _, err := store.OpenMedia(ctx, viewer, before.ID, "")
	if file != nil {
		_ = file.Close()
	}
	if file != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale source exposed playback after remap: %v", err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if item, err := store.GetItem(ctx, viewer, before.ID); err != nil || item.Name != before.Name {
		t.Fatalf("verification did not restore the original identity and description: %+v %v", item, err)
	}
}

func TestEpisodePairNFOKeepsAncestorGateWithImageOnlyScan(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "tv")
	path := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "retained video")
	writeEpisodePairTestNFO(t, strings.TrimSuffix(path, ".mp4")+".nfo",
		pairedTestNFO(3, "First", "PG", "Family")+pairedTestNFO(4, "Second", "PG", "Family"))
	libraryIntegrationFile(t, root, "Show/tvshow.nfo", "invalid show sidecar")
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	store.deferLocalMetadata = true
	firstScan := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err == nil {
		t.Fatal("invalid show sidecar unexpectedly passed")
	}
	item := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	series := nfoCatalogItem(t, ctx, store, admin, collection.ID, filepath.Join(root, "Show"))
	if item.Name != "First,  Second" {
		t.Fatalf("invalid show sidecar prevented independent episode import: %q", item.Name)
	}
	assertEpisodePairPending(t, ctx, pool, item.ID, "", false)
	assertEpisodePairPending(t, ctx, pool, series.ID, firstScan.ID, true)
	const viewer = "pending-ancestor-viewer"
	libraryIntegrationUser(t, ctx, pool, viewer, false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, viewer, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("initial ancestor gate was not enforced: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE libraries SET options=options||'{"EnableLocalMetadata":false,"EnableLocalImages":true}'::jsonb,
		revision=revision+1 WHERE id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	secondScan := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	assertEpisodePairPending(t, ctx, pool, series.ID, secondScan.ID, true)
	var pendingImages bool
	if err := pool.QueryRow(ctx, `SELECT import_images FROM local_metadata_ingest WHERE item_id=$1`, series.ID).Scan(&pendingImages); err != nil || !pendingImages {
		t.Fatalf("independent image stage did not queue: %t %v", pendingImages, err)
	}
	if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err == nil {
		t.Fatal("the disabled NFO reader claimed to verify the pending document")
	}
	imageChild, imageFence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalArtworkIngestKey)
	if err := store.ImportLocalScanArtwork(ctx, imageChild, collection.ID, imageFence, func(ProviderProgress) error { return nil }); err != nil {
		t.Fatalf("artwork could not run independently of the failed NFO reader: %v", err)
	}
	assertEpisodePairPending(t, ctx, pool, series.ID, secondScan.ID, true)
	if err := pool.QueryRow(ctx, `SELECT import_images FROM local_metadata_ingest WHERE item_id=$1`, series.ID).Scan(&pendingImages); err != nil || pendingImages {
		t.Fatalf("uncertain NFO blocked the independent image stage: %t %v", pendingImages, err)
	}
	if _, err := store.GetItem(ctx, viewer, item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("image-only work dropped the ancestor policy gate: %v", err)
	}
}

func TestEpisodePairDeferredNFORechecksCurrentAncestryAndSidecar(t *testing.T) {
	for _, mutation := range []string{"ancestor", "sidecar", "sidecar_at_final_fence"} {
		t.Run(mutation, func(t *testing.T) {
			ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			root := filepath.Join(approved, "tv")
			mediaPath := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "one physical video")
			nfoPath := strings.TrimSuffix(mediaPath, ".mp4") + ".nfo"
			first, second := pairedTestNFO(3, "First", "PG", "Family"), pairedTestNFO(4, "Second", "PG", "Family")
			writeEpisodePairTestNFO(t, nfoPath, first+second)
			collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
			store.deferLocalMetadata = true
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			before := nfoCatalogItem(t, ctx, store, admin, collection.ID, mediaPath)
			child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
			calls := 0
			var mutationErr error
			guard := func(tx OwnedTx) error {
				if err := fence(tx); err != nil {
					return err
				}
				calls++
				// This fence belongs to the episode transaction, after the candidate
				// and its source hierarchy were first observed. The media, leaf
				// parent ID, and queued generation are unchanged in both cases.
				if calls == 5 && mutation == "ancestor" {
					_, mutationErr = tx.Exec(`UPDATE items SET parent_id=$2 WHERE id=$1`, before.ParentID, collection.ID)
				} else if calls == 5 && mutation == "sidecar" || calls == 6 && mutation == "sidecar_at_final_fence" {
					mutationErr = rewriteIngestFixture(nfoPath, []byte(second+first))
				}
				return mutationErr
			}
			wantCalls := 5
			if mutation == "sidecar_at_final_fence" {
				wantCalls = 6
			}
			if err := store.ImportLocalScanMetadata(ctx, child, collection.ID, guard, func(ProviderProgress) error { return nil }); err == nil || calls < wantCalls || mutationErr != nil {
				t.Fatalf("race did not reject the publication: calls=%d mutation=%v import=%v", calls, mutationErr, err)
			}
			assertEpisodePairPending(t, ctx, pool, before.ID, job.ID, true)
			if got := nfoCatalogItem(t, ctx, store, admin, collection.ID, mediaPath); got.Name != before.Name || got.Metadata != nil {
				t.Fatalf("stale publication left accepted NFO data: %+v", got)
			}
		})
	}
}

func TestEpisodePairNFOPromotionRetiresTheOrdinaryGate(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "tv")
	path := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "retained video")
	libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E05.mp4", "other episode")
	writeEpisodePairTestNFO(t, strings.TrimSuffix(path, ".mp4")+".nfo",
		pairedTestNFO(3, "First", "PG", "Family")+pairedTestNFO(4, "Second", "R", "Family"))
	collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	series := nfoCatalogItem(t, ctx, store, admin, collection.ID, filepath.Join(root, "Show"))
	assertEpisodePairPending(t, ctx, pool, before.ID, job.ID, true)
	backdrops := filepath.Join(root, "Show", "backdrops")
	if err := os.Mkdir(backdrops, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(backdrops, "Theme.mp4")); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	assertEpisodePairPending(t, ctx, pool, before.ID, "", false)
	const viewer = "theme-promotion-viewer"
	libraryIntegrationUser(t, ctx, pool, viewer, false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	themes, err := store.QueryThemeMedia(ctx, series.ID, ThemeQuery{Subject: Subject{UserID: viewer}, EnableThemeVideos: true})
	if err != nil || themes.ThemeVideosResult.TotalRecordCount != 1 || len(themes.ThemeVideosResult.Items) != 1 ||
		themes.ThemeVideosResult.Items[0].ID != before.ID {
		t.Fatalf("promoted theme retained the episode gate or lost its identity: %+v %v", themes, err)
	}
}

func TestEpisodePairNFOAudioReclassificationRetiresTheEpisodeGate(t *testing.T) {
	ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
	root := filepath.Join(approved, "mixed")
	path := libraryIntegrationFile(t, root, "Show.S01E03E04.mp4", "retained source")
	writeEpisodePairTestNFO(t, strings.TrimSuffix(path, ".mp4")+".nfo",
		pairedTestNFO(3, "First", "PG", "Family")+pairedTestNFO(4, "Second", "R", "Family"))
	collection := libraryIntegrationCreate(t, ctx, store, "Mixed episode to audio", "mixed", root)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	before := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if before.Type != "Episode" {
		t.Fatalf("fixture is not an episode: %+v", before)
	}
	assertEpisodePairPending(t, ctx, pool, before.ID, job.ID, true)
	audio := filepath.Join(root, "Track.m4a")
	if err := os.Rename(path, audio); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	const viewer = "changed-type-viewer"
	libraryIntegrationUser(t, ctx, pool, viewer, false, true, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, viewer); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetItem(ctx, viewer, before.ID)
	if err != nil || after.Type != "Audio" || after.Path != audio {
		t.Fatalf("audio inherited a gate from its former episode type: %+v %v", after, err)
	}
	assertEpisodePairPending(t, ctx, pool, before.ID, "", false)
}

// Inject after observation, in the item or cached-visit transaction. The
// first attempt must roll back and retry pending rather than bless old evidence.
type episodePairScanTrace struct {
	armed                     atomic.Bool
	prefix, path, replacement string
	injected                  chan error
}

func (trace *episodePairScanTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, trace.prefix) && trace.armed.Swap(false) {
		trace.injected <- os.WriteFile(trace.path, []byte(trace.replacement), 0600)
	}
	return ctx
}

func (trace *episodePairScanTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestEpisodePairSynchronousNFORechecksAtPublication(t *testing.T) {
	t.Run("replacement during new visit", func(t *testing.T) { testEpisodePairScanProof(t, false) })
	t.Run("appearance after cached absence", func(t *testing.T) { testEpisodePairScanProof(t, true) })
}

func testEpisodePairScanProof(t *testing.T, cachedAbsence bool) {
	t.Helper()
	ctx, pool, previous, approved, admin := libraryIntegrationStore(t, &libraryFixtureProber{})
	if err := previous.Close(ctx); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(approved, "tv")
	path := libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E03E04.mp4", "one physical video")
	nfo := strings.TrimSuffix(path, ".mp4") + ".nfo"
	first, second := pairedTestNFO(3, "First", "PG", "Family"), pairedTestNFO(4, "Second", "PG", "Family")
	trace := &episodePairScanTrace{path: nfo, replacement: second + first, injected: make(chan error, 1)}
	if cachedAbsence {
		trace.prefix = "SELECT file_identity=$5 AND file_size=$6"
	} else {
		trace.prefix = "SELECT import_nfo,import_images,enrich_online FROM local_metadata_ingest"
		writeEpisodePairTestNFO(t, nfo, first+second)
	}
	cfg := pool.Config()
	cfg.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationPoolCleanup(t, traced)
	store, err := New(traced, forceProbeVersionedFunc(versionedEpisodePairTestProbe), []string{approved})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	collection := createEpisodePairTestLibrary(t, ctx, traced, store, root)
	if cachedAbsence {
		libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	}
	trace.armed.Store(true)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	select {
	case err := <-trace.injected:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("the sidecar was not replaced at publication")
	}
	item := nfoCatalogItem(t, ctx, store, admin, collection.ID, path)
	if item.Metadata != nil || item.Name != "Show S01E03E04" || job.Error == "" {
		t.Fatalf("accepted a stale NFO or omitted a warning: %+v job=%+v", item, job)
	}
	assertEpisodePairPending(t, ctx, traced, item.ID, job.ID, true)
}
