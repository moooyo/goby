//go:build linux

package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNFORecoveryDoesNotSaveAmbiguousMovieSidecar(t *testing.T) {
	for _, mode := range []struct {
		name     string
		deferred bool
	}{{"synchronous", false}, {"deferred", true}} {
		t.Run(mode.name, func(t *testing.T) {
			ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			root := filepath.Join(approved, "movies")
			store.nfoRecoveryRoots = map[string]struct{}{root: {}}
			store.deferLocalMetadata = mode.deferred
			removedVideo := libraryIntegrationFile(t, root, "Film.mp4", "first video")
			survivingVideo := libraryIntegrationFile(t, root, "Film.mkv", "different video")
			shared := libraryIntegrationFile(t, root, "Film.nfo", `<movie><title>First video metadata</title></movie>`)
			uniqueVideo := libraryIntegrationFile(t, root, "Different.mp4", "other video")
			libraryIntegrationFile(t, root, "Different.nfo", `<movie><title>Other metadata</title></movie>`)
			collection := libraryIntegrationCreate(t, ctx, store, "NFO provenance", "movies", root)
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if mode.deferred {
				if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
					t.Fatal(err)
				}
			}
			for _, tc := range []struct {
				path, title string
				wantSaved   bool
			}{
				{removedVideo, "First video metadata", false},
				{survivingVideo, "First video metadata", false},
				{uniqueVideo, "Other metadata", true},
			} {
				item := nfoCatalogItem(t, ctx, store, user, collection.ID, tc.path)
				var saved bool
				if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_nfo_recovery_sources WHERE item_id=$1)`, item.ID).Scan(&saved); err != nil {
					t.Fatal(err)
				}
				if item.Name != tc.title || saved != tc.wantSaved {
					t.Fatalf("%s: title=%q saved=%v, want %q %v", tc.path, item.Name, saved, tc.title, tc.wantSaved)
				}
			}

			// Uniqueness at write time alone cannot authorize the old shared bytes.
			if err := os.Remove(removedVideo); err != nil {
				t.Fatal(err)
			}
			writeEpisodePairTestNFO(t, shared, "")
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
				t.Fatal(err)
			}
			progress, err := recoveryTestRun(t, ctx, pool, store, collection, nil)
			if err == nil || !strings.Contains(err.Error(), "no trusted recovery document") || progress.Processed != 1 || progress.Updated != 0 {
				t.Fatalf("shared bytes authorized recovery: %+v %v", progress, err)
			}
			if data, err := os.ReadFile(shared); err != nil || len(data) != 0 {
				t.Fatalf("the empty sidecar was replaced: %q %v", data, err)
			}
		})
	}
}

func TestNFORecoveryDoesNotSaveContradictorySeasonNumber(t *testing.T) {
	for _, mode := range []struct {
		name     string
		deferred bool
	}{{"synchronous", false}, {"deferred", true}} {
		t.Run(mode.name, func(t *testing.T) {
			ctx, pool, store, approved, user := libraryIntegrationStore(t, forceProbeVersionedFunc(versionedEpisodePairTestProbe))
			root := filepath.Join(approved, "tv")
			store.nfoRecoveryRoots = map[string]struct{}{root: {}}
			store.deferLocalMetadata = mode.deferred
			libraryIntegrationFile(t, root, "Show/Season 01/Show.S01E01.mp4", "video")
			sidecar := libraryIntegrationFile(t, root, "Show/Season 01/season.nfo", `<season><season>1</season><seasonnumber>2</seasonnumber><title>Season metadata</title></season>`)
			collection := createEpisodePairTestLibrary(t, ctx, pool, store, root)
			libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if mode.deferred {
				if err := runEpisodePairTestImport(t, ctx, pool, store, collection); err != nil {
					t.Fatal(err)
				}
			}
			season := nfoCatalogItem(t, ctx, store, user, collection.ID, filepath.Dir(sidecar))
			var saved bool
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_nfo_recovery_sources WHERE item_id=$1)`, season.ID).Scan(&saved); err != nil {
				t.Fatal(err)
			}
			if season.Name != "Season metadata" || season.IndexNumber != 1 || saved {
				t.Fatalf("contradictory alias affected import or was saved: title=%q season=%d saved=%v", season.Name, season.IndexNumber, saved)
			}
		})
	}
}
