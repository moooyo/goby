//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

func TestLocalMetadataIngestSkipsHistoricalSystemDirectories(t *testing.T) {
	ctx, pool, store, approved, user := libraryIntegrationStore(t, &libraryFixtureProber{})
	root := filepath.Join(approved, "television")
	path := libraryIntegrationFile(t, root, "#recycle/Deleted.S01E01.mp4", "video:excluded")
	collection := libraryIntegrationCreate(t, ctx, store, "Historical system directory", "tvshows", root)
	job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	stat, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	const excluded = "old-recycle-series"
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if _, err := tx.Exec(`INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,is_folder)
			SELECT $1,$2,id,$2,'Deleted','deleted','Series',$3,'#recycle',true FROM library_roots WHERE library_id=$2`, excluded, collection.ID, filepath.Dir(path)); err != nil {
			return err
		}
		_, err := tx.Exec(queueScanMetadataImportSQL, excluded, job.ID, fileIdentity(stat), 0, 0, false, true, false, false, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
	if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
		var run string
		if err := tx.QueryRow(`SELECT run_id FROM task_run_children WHERE id=$1`, child).Scan(&run); err != nil {
			return err
		}
		count, err := SnapshotLocalMetadataIngestChildren(tx, run)
		if err != nil || count != 0 {
			t.Fatalf("excluded work entered task admission: %d %v", count, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportLocalScanMetadata(ctx, child, collection.ID, fence, func(ProviderProgress) error {
		return errors.New("excluded work entered the worker page")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetItem(ctx, user, excluded); !errors.Is(err, ErrNotFound) {
		t.Fatalf("excluded historical row is visible: %v", err)
	}
	var retained bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE item_id=$1 AND import_nfo)`, excluded).
		Scan(&retained); err != nil || !retained {
		t.Fatalf("excluded history was retired rather than skipped: %t %v", retained, err)
	}
}

func TestStoreTVScanRetiresIncompatibleGroupingMetadataWork(t *testing.T) {
	for _, mode := range []struct {
		name                string
		deferred, oldImages bool
	}{
		{name: "deferred NFO only", deferred: true},
		{name: "synchronous recheck", oldImages: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			ctx, pool, store, approved, admin := libraryIntegrationStore(t, forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
				return forceProbeTestInfo(file)
			}))
			root := filepath.Join(approved, "television")
			mediaPath := libraryIntegrationFile(t, root, "All/Show/Season 1/Show.S01E02.mp4", "video:normal-episode")
			collection := libraryIntegrationCreate(t, ctx, store, "Grouping reclassification", "tvshows", root)
			oldScan := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			group := nfoCatalogItem(t, ctx, store, admin, collection.ID, filepath.Join(root, "All"))
			before := nfoCatalogItem(t, ctx, store, admin, collection.ID, mediaPath)
			stat, err := os.Stat(group.Path)
			if err != nil {
				t.Fatal(err)
			}
			const restricted = "restricted-grouping-viewer"
			libraryIntegrationUser(t, ctx, pool, restricted, false, true, nil)
			position, favorite := int64(42), true
			if _, err := store.UpdateUserDataFor(ctx, Subject{UserID: restricted}, before.ID, UserDataPatch{
				PlaybackPositionTicks: &position, IsFavorite: &favorite,
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.WithOwnedTx(ctx, func(tx OwnedTx) error {
				if _, err := tx.Exec(`UPDATE items SET type='Series' WHERE id=$1`, group.ID); err != nil {
					return err
				}
				if _, err := tx.Exec(queueScanMetadataImportSQL, group.ID, oldScan.ID, fileIdentity(stat), 0, 0, false, true, mode.oldImages, true, false); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE users SET policy=policy||'{"MaxParentalRating":5}'::jsonb WHERE id=$1`, restricted); err != nil {
					return err
				}
				_, err := tx.Exec(`UPDATE libraries SET options=jsonb_set(options,'{EnableLocalImages}','false'::jsonb) WHERE id=$1`, collection.ID)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.GetItem(ctx, restricted, before.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("the old pending Series NFO did not protect its descendants: %v", err)
			}

			store.deferLocalMetadata = mode.deferred
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if job.Error != "" {
				t.Fatalf("reclassification scan warned: %s", job.Error)
			}
			afterGroup := nfoCatalogItem(t, ctx, store, admin, collection.ID, group.Path)
			if afterGroup.Type != "Folder" || afterGroup.ID != group.ID {
				t.Fatalf("reclassification did not retain the grouping folder identity: %+v", afterGroup)
			}
			var work, nfoOrOnline, imagesCurrent bool
			if err := pool.QueryRow(ctx, `SELECT count(*)>0,COALESCE(bool_or(import_nfo OR enrich_online),false),
				COALESCE(bool_or(import_images AND scan_job_id=$2),false) FROM local_metadata_ingest WHERE item_id=$1`, group.ID, job.ID).
				Scan(&work, &nfoOrOnline, &imagesCurrent); err != nil || work != mode.oldImages || nfoOrOnline || imagesCurrent != mode.oldImages {
				t.Fatalf("wrong grouping work: queued=%t obsolete=%t current images=%t err=%v", work, nfoOrOnline, imagesCurrent, err)
			}
			child, fence := metadataIngestTestWorker(t, ctx, pool, collection, TaskLocalMetadataIngestKey)
			if err := store.ImportLocalScanMetadata(ctx, child, collection.ID, fence, func(ProviderProgress) error { return nil }); err != nil {
				t.Fatalf("background import did not drain accepted work: %v", err)
			}
			if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest)`).Scan(&work); err != nil || work {
				t.Fatalf("pending work after background import: %t %v", work, err)
			}
			after, err := store.GetItem(ctx, restricted, before.ID)
			if err != nil || after.Path != mediaPath || after.UserData == nil || !after.UserData.IsFavorite || after.UserData.PlaybackPositionTicks != position {
				t.Fatalf("reclassified episode is not visible with its previous user data: %+v %v", after, err)
			}
		})
	}
}
