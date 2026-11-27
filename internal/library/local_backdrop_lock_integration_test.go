//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func acquireBackdropFixtureLock(t *testing.T, f localBackdropFixture) LocalBackdropLockInfo {
	t.Helper()
	lock, err := f.store.AcquireLocalBackdropLock(f.ctx, f.actor, f.item.ID)
	if err != nil || lock.ItemID != f.item.ID || !validLocalBackdropLockID(lock.LockID) || !lock.ExpiresAt.After(time.Now()) {
		t.Fatalf("acquire lock = %+v, %v", lock, err)
	}
	return lock
}

func TestLocalBackdropLockFencesScansAndDeferredImportThroughCommit(t *testing.T) {
	f := newLocalBackdropFixture(t, false)
	_, heldChildren := taskScanFixture(t, f.ctx, f.pool, f.collection)
	outsidePath := filepath.Join(filepath.Dir(f.dir), "Outside")
	libraryIntegrationFile(t, outsidePath, "Film.mp4", "unrelated scan source")
	outside := libraryIntegrationCreate(t, f.ctx, f.store, "Disjoint movie library", "movies", outsidePath)
	_, outsideChildren := taskScanFixture(t, f.ctx, f.pool, outside)
	// The same physical directory registered as a different library's root is
	// not an unrelated scan, even though its item and root IDs would differ.
	alias := libraryIntegrationCreate(t, f.ctx, f.store, "Overlapping TV library", "tvshows", f.dir)
	// Use a real active overlapping scan slot without altering the pending
	// generation of the target library. Acquisition must not race that scan.
	f.exec(t, `INSERT INTO scan_jobs(id,library_id,status) VALUES('overlap',$1,'Queued')`, alias.ID)
	if _, err := f.store.AcquireLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("acquired while an overlapping scan was already admitted: %v", err)
	}
	if f.store.backdropExecution.current() != nil {
		t.Fatal("an unsuccessful acquisition installed a hold")
	}
	f.exec(t, `DELETE FROM scan_jobs WHERE id='overlap'`)

	var queue, backdrops string
	readTarget := func() (string, string) {
		t.Helper()
		var pending, images string
		if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE((SELECT to_jsonb(q)::text FROM local_metadata_ingest q WHERE item_id=$1),''),
			COALESCE((SELECT jsonb_agg(to_jsonb(im) ORDER BY image_index)::text FROM item_images im WHERE item_id=$1 AND image_type='Backdrop'),'')`, f.item.ID).
			Scan(&pending, &images); err != nil {
			t.Fatal(err)
		}
		return pending, images
	}
	queue, backdrops = readTarget()
	lock := acquireBackdropFixtureLock(t, f)
	if current, err := f.store.GetLocalBackdropLock(f.ctx, f.actor, f.item.ID); err != nil || current != lock {
		t.Fatalf("durable lock readback = %+v, %v", current, err)
	}
	if _, err := f.store.AcquireLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrLocalBackdropLocked) {
		t.Fatalf("duplicate acquire = %v", err)
	}
	assertScansHeld := func() {
		t.Helper()
		for _, id := range []string{f.collection.ID, alias.ID} {
			if job, err := f.store.StartScan(f.ctx, id); !errors.Is(err, ErrLocalBackdropLocked) || job.ID != "" {
				t.Fatalf("overlapping scan admitted: %+v, %v", job, err)
			}
		}
		if admission, err := f.store.AdmitTaskScan(f.ctx, heldChildren[0]); err != nil || admission.Kind != ScanSourceHeld || admission.Job.ID != "" {
			t.Fatalf("task scan did not stay waiting: %+v, %v", admission, err)
		}
		child := taskScanReadChild(t, f.ctx, f.pool, heldChildren[0])
		if child.State != "waiting" || child.ScanJobID != "" {
			t.Fatalf("fence advanced the waiting child: %+v", child)
		}
	}
	assertScansHeld() // before the external rename
	f.isolateBlocker(t)
	assertScansHeld() // scanner still cannot re-authorize work after the rename
	admission, err := f.store.AdmitTaskScan(f.ctx, outsideChildren[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("disjoint task scan = %+v, %v", admission, err)
	}
	libraryIntegrationWaitJob(t, f.ctx, f.store, admission.Job.ID, "Completed")

	// The real startup-triggered importer must skip the held row even with
	// all its files now valid. It can still finish another directory.
	child, fence := metadataIngestTestWorker(t, f.ctx, f.pool, f.collection, TaskLocalMetadataIngestKey)
	if _, err := f.store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrLocalBackdropLocked) {
		t.Fatalf("reread without lock ID = %v", err)
	}
	otherAdmin := metadataEditTestActor(t, f.ctx, f.pool, "other-backdrop-admin")
	if _, err := f.store.RereadLockedLocalBackdrop(f.ctx, otherAdmin, f.item.ID, lock.LockID); !errors.Is(err, ErrLocalBackdropLockChanged) {
		t.Fatalf("a different session borrowed the hold: %v", err)
	}
	var progress ProviderProgress
	if err := f.store.ImportLocalScanMetadata(f.ctx, child, f.collection.ID, fence, func(current ProviderProgress) error {
		progress = current
		return nil
	}); err != nil || progress.Updated < 1 {
		t.Fatalf("real startup importer could not process unrelated work: %+v, %v", progress, err)
	}
	if pending, images := readTarget(); pending == "" || pending != queue || images != backdrops {
		t.Fatal("automatic importer changed the locked artwork or its queue row")
	}
	before := f.state(t, true)
	notifications := catalogChangesTestListener(t, f.store)
	got, err := f.store.rereadLocalBackdrop(f.ctx, f.actor, f.item.ID, lock.LockID, func(ctx context.Context, op *PrimaryRootIO,
		plan seriesNFOAuditRootPlan, target localBackdropTarget) (localBackdropObservation, error) {
		observed, err := f.store.observeLocalBackdrop(ctx, op, plan, target)
		if err == nil {
			assertScansHeld()
			if err := f.store.ReleaseLocalBackdropLock(ctx, f.actor, f.item.ID, lock.LockID); !errors.Is(err, ErrBusy) {
				t.Fatalf("release while reread has pinned the lock = %v", err)
			}
		}
		return observed, err
	})
	if err != nil || !got.Changed || got.ItemID != f.item.ID {
		t.Fatalf("locked reread = %+v, %v", got, err)
	}
	if after := f.state(t, true); after != before {
		t.Fatal("locked reread changed unrelated images, work, scan/task rows, or task signals")
	}
	if pending, images := readTarget(); pending != "" || images == backdrops {
		t.Fatal("locked reread failed to atomically complete the held target")
	}
	assertCatalogTestChanges(t, nextCatalogTestNotification(t, notifications), []CatalogChange{{Kind: CatalogUpdated,
		ItemID: f.item.ID, LibraryID: f.collection.ID, ParentID: f.item.ParentID, IsFolder: true}})
	assertScansHeld() // a successful commit is not an implicit release
	if err := f.store.ReleaseLocalBackdropLock(f.ctx, otherAdmin, f.item.ID, lock.LockID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("released lock still visible: %v", err)
	}
	admission, err = f.store.AdmitTaskScan(f.ctx, heldChildren[0])
	if err != nil || admission.Kind != ScanAdmitted {
		t.Fatalf("waiting task scan did not resume: %+v, %v", admission, err)
	}
	libraryIntegrationWaitJob(t, f.ctx, f.store, admission.Job.ID, "Completed")
}

func TestLocalBackdropLockRefusesAnAlreadyAdmittedTarget(t *testing.T) {
	f := newLocalBackdropFixture(t, false)
	entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	go func() {
		_, err := f.store.rereadLocalBackdrop(ctx, f.actor, f.item.ID, "", func(context.Context, *PrimaryRootIO, seriesNFOAuditRootPlan, localBackdropTarget) (localBackdropObservation, error) {
			close(entered)
			select {
			case <-finish:
			case <-ctx.Done():
			}
			return localBackdropObservation{}, ErrLocalBackdropRejected
		})
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("target exited before the admission race: %v", err)
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	if _, err := f.store.AcquireLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("lock overlapped already admitted target work: %v", err)
	}
	close(finish)
	if err := <-done; !errors.Is(err, ErrLocalBackdropRejected) {
		t.Fatal(err)
	}
	lock := acquireBackdropFixtureLock(t, f)
	if err := f.store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, lock.LockID); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBackdropLockRestoresExpiredHoldUntilExplicitRelease(t *testing.T) {
	f := newLocalBackdropFixture(t, true)
	lock := acquireBackdropFixtureLock(t, f)
	// Simulate elapsed wall time without holding a real test window open.
	f.exec(t, `UPDATE server_settings SET value=jsonb_set(value::jsonb,'{info,ExpiresAt}',to_jsonb('2000-01-01T00:00:00Z'::text))::text WHERE key=$1`, localBackdropLockKey)
	if err := f.store.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	store, err := New(f.pool, forceProbeVersionedFunc(versionedEpisodePairTestProbe), []string{filepath.Dir(f.dir)}, WithDeferredLocalMetadata())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := store.Close(cleanup); err != nil {
			t.Error(err)
		}
	})
	f.store = store
	current, err := store.GetLocalBackdropLock(f.ctx, f.actor, f.item.ID)
	if err != nil || current.LockID != lock.LockID || current.ExpiresAt.After(time.Now()) {
		t.Fatalf("restart lost expired hold: %+v, %v", current, err)
	}
	if _, err := store.StartScan(f.ctx, f.collection.ID); !errors.Is(err, ErrLocalBackdropLocked) {
		t.Fatalf("expired lock opened the scanner after restart: %v", err)
	}
	if _, err := store.RereadLockedLocalBackdrop(f.ctx, f.actor, f.item.ID, lock.LockID); !errors.Is(err, ErrLocalBackdropLockExpired) {
		t.Fatalf("expired lock still authorizes publication: %v", err)
	}
	if _, err := store.RereadLocalBackdrop(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrLocalBackdropLocked) {
		t.Fatalf("expired lock can be bypassed by omitting the ID: %v", err)
	}
	stale, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, stale); !errors.Is(err, ErrLocalBackdropLockChanged) {
		t.Fatalf("unmatched release = %v", err)
	}
	if err := store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, lock.LockID); err != nil {
		t.Fatal(err)
	}
	// A delayed release from the old reservation cannot clear a successor.
	next := acquireBackdropFixtureLock(t, f)
	if err := store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, lock.LockID); !errors.Is(err, ErrLocalBackdropLockChanged) || next.LockID == lock.LockID {
		t.Fatalf("old lock could release a newer reservation: %v", err)
	}
	if err := store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, next.LockID); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBackdropLockRefusesChangedDirectoryAndStaleGeneration(t *testing.T) {
	for _, scenario := range []string{"directory", "scan"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLocalBackdropFixture(t, false)
			switch scenario {
			case "directory":
				if err := os.Rename(f.dir, f.dir+"-detached"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(f.dir, 0700); err != nil {
					t.Fatal(err)
				}
				imageScanTestWrite(t, filepath.Join(f.dir, "fanart.png"), color.White)
			case "scan":
				f.exec(t, `INSERT INTO scan_jobs(id,library_id,status,finished_at) VALUES('newer-scan',$1,'Completed',clock_timestamp())`, f.collection.ID)
			}
			if _, err := f.store.AcquireLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrSourceChanged) {
				t.Fatalf("acquire after %s change = %v", scenario, err)
			}
			if _, err := f.store.GetLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrNotFound) || f.store.backdropExecution.current() != nil {
				t.Fatalf("failed acquire left a hold: %v", err)
			}
		})
	}
}

func TestLocalBackdropLockRejectsDurableChangeBeforePublication(t *testing.T) {
	f := newLocalBackdropFixture(t, false)
	lock := acquireBackdropFixtureLock(t, f)
	f.isolateBlocker(t)
	before := f.state(t, false)
	_, err := f.store.rereadLocalBackdrop(f.ctx, f.actor, f.item.ID, lock.LockID, func(ctx context.Context, op *PrimaryRootIO,
		plan seriesNFOAuditRootPlan, target localBackdropTarget) (localBackdropObservation, error) {
		observed, err := f.store.observeLocalBackdrop(ctx, op, plan, target)
		if err == nil {
			f.exec(t, `UPDATE server_settings SET value=jsonb_set(value::jsonb,'{info,ExpiresAt}',to_jsonb('2000-01-01T00:00:00Z'::text))::text WHERE key=$1`, localBackdropLockKey)
		}
		return observed, err
	})
	if !errors.Is(err, ErrLocalBackdropLockChanged) || f.state(t, false) != before {
		t.Fatalf("changed durable lock authorized publication: %v", err)
	}
	if _, err := f.store.StartScan(f.ctx, f.collection.ID); !errors.Is(err, ErrLocalBackdropLocked) {
		t.Fatalf("failed publication opened the scanner: %v", err)
	}
	if err := f.store.ReleaseLocalBackdropLock(f.ctx, f.actor, f.item.ID, lock.LockID); err != nil {
		t.Fatal(err)
	}
}

func TestLocalBackdropLockRejectsRevokedSessionWithoutOpeningHold(t *testing.T) {
	f := newLocalBackdropFixture(t, false)
	f.exec(t, `DELETE FROM sessions WHERE id=$1`, f.actor.SessionID)
	if _, err := f.store.AcquireLocalBackdropLock(f.ctx, f.actor, f.item.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked native session acquired hold: %v", err)
	}
	other := metadataEditTestActor(t, f.ctx, f.pool, "active-backdrop-admin")
	if _, err := f.store.GetLocalBackdropLock(f.ctx, other, f.item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked session left a reservation: %v", err)
	}
}

// Keep the durable row and its administrative projection aligned: release is
// deliberately an exact token CAS, not an unconditional deletion of a setting.
func TestLocalBackdropLockRecordTiesSessionRootAndGeneration(t *testing.T) {
	f := newLocalBackdropFixture(t, false)
	lock := acquireBackdropFixtureLock(t, f)
	var encoded []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT value FROM server_settings WHERE key=$1`, localBackdropLockKey).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var saved localBackdropLock
	if err := json.Unmarshal(encoded, &saved); err != nil || saved.Info != lock || saved.SessionID != f.actor.SessionID || saved.Directory != f.dir {
		t.Fatalf("durable lock is not bound to accepted scope: %v", err)
	}
	// A different capability cannot publish merely because it has administrator
	// permission and the original token.
	actor := f.actor
	actor.Kind = "emby"
	if _, err := f.store.RereadLockedLocalBackdrop(f.ctx, actor, f.item.ID, lock.LockID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-native actor published under the hold: %v", err)
	}
	if err := f.store.ReleaseLocalBackdropLock(f.ctx, identity.Principal{}, f.item.ID, lock.LockID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthenticated store caller released the hold: %v", err)
	}
}
