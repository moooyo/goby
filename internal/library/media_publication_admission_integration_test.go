//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

type mediaPublicationTestExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// A persisted recovery journal keeps its publication barrier even when no
// worker is running. File exchange and journal validation belong to the media
// edit tests; these fixtures exercise admission against an existing barrier.
func mediaPublicationAdmissionSeed(t *testing.T, ctx context.Context, executor mediaPublicationTestExecutor, itemID, phase string) string {
	t.Helper()
	id, err := randomID()
	if err != nil {
		t.Fatal(err)
	}
	tag, err := executor.Exec(ctx, `INSERT INTO media_operations
		(id,kind,item_id,library_id,root_id,source_item_id,source_library_id,source_root_id,
		 request_actor_id,request_credential_id,request_id,request_fingerprint,media_source_id,source_revision,
		 stream_index,parameters,source_snapshot,execution_snapshot,state,publication_phase)
		SELECT $1,'remove_embedded_subtitle',i.id,i.library_id,i.root_id,i.id,i.library_id,i.root_id,
		 'publication-admission-actor','publication-admission-credential',$1,$3,$4,`+MediaOperationSourceRevisionSQL+`,
		 2,'{}','{}','{}','recovery_required',$5 FROM items i WHERE i.id=$2`,
		id, itemID, make([]byte, 32), media.SourceID(itemID), phase)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("seed media publication admission barrier failed (%T)", err)
	}
	return id
}

func mediaPublicationAdmissionReady(t *testing.T, fixture mediaSourceFixture, operationID string) {
	t.Helper()
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE media_operations
		SET publication_phase='none',state='ready' WHERE id=$1`, operationID); err != nil {
		t.Fatalf("release publication barrier into a reviewable result: %v", err)
	}
}

func mediaPublicationAdmissionBusy(t *testing.T, err error) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.Is(err, ErrBusy) || errors.As(err, &databaseError) {
		t.Fatalf("publication admission error = %v, want stable ErrBusy without a PostgreSQL trigger error", err)
	}
}

func TestMediaPublicationBlocksScanAdmissionAndLibraryDeletionOnlyForItsLibrary(t *testing.T) {
	for _, phase := range []string{"prepared", "catalog_committed"} {
		t.Run(phase, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			operation := mediaPublicationAdmissionSeed(t, fixture.ctx, fixture.pool, fixture.item.ID, phase)
			_, children := taskScanFixture(t, fixture.ctx, fixture.pool, fixture.library)
			beforeChild := taskScanRowSnapshot(t, fixture.ctx, fixture.pool, "task_run_children", children[0])
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			if job, err := fixture.store.StartScan(fixture.ctx, fixture.library.ID); job.ID != "" {
				t.Fatal("blocked manual scan returned a persistent job")
			} else {
				mediaPublicationAdmissionBusy(t, err)
			}
			if admission, err := fixture.store.AdmitTaskScan(fixture.ctx, children[0]); admission.Kind != "" || admission.Job.ID != "" {
				t.Fatal("blocked scheduled scan returned an admitted or duplicate job")
			} else {
				mediaPublicationAdmissionBusy(t, err)
			}
			mediaPublicationAdmissionBusy(t, fixture.store.DeleteLibrary(fixture.ctx, fixture.library.ID))
			if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
				t.Fatal("rejected publication admission changed catalog, scan, root, or activity state")
			}
			if after := taskScanRowSnapshot(t, fixture.ctx, fixture.pool, "task_run_children", children[0]); after != beforeChild {
				t.Fatal("blocked scheduled scan linked or changed its waiting child")
			}

			other := taskScanCreateLibrary(t, fixture.ctx, fixture.store, fixture.allowedRoot, "Other publication library")
			libraryIntegrationScan(t, fixture.ctx, fixture.store, other.ID, "Completed")
			_, otherChildren := taskScanFixture(t, fixture.ctx, fixture.pool, other)
			admission, err := fixture.store.AdmitTaskScan(fixture.ctx, otherChildren[0])
			if err != nil || admission.Kind != ScanAdmitted {
				t.Fatalf("an unrelated library could not admit its scheduled scan: %v", err)
			}
			libraryIntegrationWaitJob(t, fixture.ctx, fixture.store, admission.Job.ID, "Completed")
			if err := fixture.store.DeleteLibrary(fixture.ctx, other.ID); err != nil {
				t.Fatalf("an unrelated library could not be deleted: %v", err)
			}

			// A candidate waiting for review owns no publication barrier. The same
			// previously blocked task child must remain available for admission.
			mediaPublicationAdmissionReady(t, fixture, operation)
			libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
			admission, err = fixture.store.AdmitTaskScan(fixture.ctx, children[0])
			if err != nil || admission.Kind != ScanAdmitted {
				t.Fatalf("a ready candidate still blocked its scheduled scan: %v", err)
			}
			libraryIntegrationWaitJob(t, fixture.ctx, fixture.store, admission.Job.ID, "Completed")
			if err := fixture.store.DeleteLibrary(fixture.ctx, fixture.library.ID); err != nil {
				t.Fatalf("a ready candidate still blocked library deletion: %v", err)
			}
			var retained bool
			if err := fixture.pool.QueryRow(fixture.ctx, `SELECT state='ready' AND publication_phase='none'
				AND item_id IS NULL AND library_id IS NULL AND root_id IS NULL
				AND source_library_id=$2 FROM media_operations WHERE id=$1`, operation, fixture.library.ID).Scan(&retained); err != nil || !retained {
				t.Fatalf("library deletion did not retain detached review history: %v", err)
			}
			if contents, err := os.ReadFile(fixture.path); err != nil || string(contents) != fixture.contents {
				t.Fatal("catalog deletion changed the retained media file")
			}
		})
	}
}

func mediaPublicationRootBindingFixture(t *testing.T, fixture mediaSourceFixture, actor identity.Principal, collection Library) rootBindingReadFixture {
	t.Helper()
	roots, err := fixture.store.ListRegisteredRoots(fixture.ctx, actor, collection.ID)
	if err != nil || len(roots) != 1 {
		t.Fatalf("read publication root binding fixture: %v", err)
	}
	storageIdentity := RootStorageIdentity{Version: RootStorageIdentityVersion, Profile: RootStorageIdentityProfile,
		FilesystemUUID: "0102030405060708090a0b0c0d0e0f10", HandleType: 1, Handle: []byte("publication-admission-directory")}
	snapshot := RootTopologySnapshot{Version: RootTopologyVersion,
		Mapping: RootTopologyMapping{ApprovedPath: roots[0].AllowedPath, RegisteredPath: roots[0].Path},
		Anchor:  storageIdentity, RegisteredRoot: storageIdentity, Boundaries: []RootTopologyBoundary{}}
	return rootBindingReadFixture{fixture.ctx, fixture.pool, fixture.store, actor, collection, roots[0], snapshot}
}

func TestMediaPublicationRootRebindingRechecksAfterObservationAndAllowsOtherLibraries(t *testing.T) {
	for _, phase := range []string{"prepared", "catalog_committed"} {
		t.Run(phase, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			actor := metadataEditTestActor(t, fixture.ctx, fixture.pool, "publication-root-administrator")
			root := mediaPublicationRootBindingFixture(t, fixture, actor, fixture.library)
			other := taskScanCreateLibrary(t, fixture.ctx, fixture.store, fixture.allowedRoot, "Independent rebind")
			otherRoot := mediaPublicationRootBindingFixture(t, fixture, actor, other)
			var operation, afterObservation string
			capture := &rootBindingWriteTestCapture{snapshot: root.snapshot, snapshotHook: func() {
				operation = mediaPublicationAdmissionSeed(t, fixture.ctx, fixture.pool, fixture.item.ID, phase)
				afterObservation = catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			}}
			result, err := fixture.store.updateRootBinding(fixture.ctx, actor, fixture.library.ID, root.root.RootID,
				rootBindingWriteTestInput(t, root, root.root.Revision), rootBindingWriteTestFactory(t, root, capture))
			mediaPublicationAdmissionBusy(t, err)
			rootBindingWriteTestReject(t, root, result, err, ErrBusy, afterObservation)
			if capture.snapshots != 1 || capture.closes != 1 {
				t.Fatal("blocked publication rebind did not release its completed observation")
			}
			otherCapture := &rootBindingWriteTestCapture{snapshot: otherRoot.snapshot}
			if result, err := fixture.store.updateRootBinding(fixture.ctx, actor, other.ID, otherRoot.root.RootID,
				rootBindingWriteTestInput(t, otherRoot, otherRoot.root.Revision), rootBindingWriteTestFactory(t, otherRoot, otherCapture)); err != nil || result.Revision == otherRoot.root.Revision {
				t.Fatalf("publication barrier prevented an unrelated root rebind: %v", err)
			}
			mediaPublicationAdmissionReady(t, fixture, operation)
			capture = &rootBindingWriteTestCapture{snapshot: root.snapshot}
			if result, err := fixture.store.updateRootBinding(fixture.ctx, actor, fixture.library.ID, root.root.RootID,
				rootBindingWriteTestInput(t, root, root.root.Revision), rootBindingWriteTestFactory(t, root, capture)); err != nil || result.Revision == root.root.Revision {
				t.Fatalf("ready media candidate prevented a root rebind: %v", err)
			}
		})
	}
}

func TestMediaPublicationBlocksFileDeletionBeforeStagingAndAllowsReadyCandidates(t *testing.T) {
	for _, phase := range []string{"prepared", "catalog_committed"} {
		t.Run(phase, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			actor := mediaDeletionTestActor(t, fixture)
			if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletion":true}' WHERE id=$1`, fixture.userID); err != nil {
				t.Fatal(err)
			}
			operation := mediaPublicationAdmissionSeed(t, fixture.ctx, fixture.pool, fixture.item.ID, phase)
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			mediaPublicationAdmissionBusy(t, fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID))
			if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
				t.Fatal("blocked media deletion changed its source catalog or audit history")
			}
			if contents, err := os.ReadFile(fixture.path); err != nil || string(contents) != fixture.contents {
				t.Fatal("blocked media deletion changed or staged the source file")
			}
			taskScanExpectCount(t, fixture.ctx, fixture.pool, "SELECT count(*) FROM media_deletion_operations", 0)
			entries, err := os.ReadDir(filepath.Dir(fixture.path))
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(fixture.path) {
				t.Fatal("blocked media deletion retained a new staging directory")
			}

			other := taskScanCreateLibrary(t, fixture.ctx, fixture.store, fixture.allowedRoot, "Independent delete")
			libraryIntegrationScan(t, fixture.ctx, fixture.store, other.ID, "Completed")
			otherItems := libraryIntegrationQuery(t, fixture.ctx, fixture.store, Query{UserID: fixture.userID, ParentID: other.ID, Recursive: true, IncludeItemTypes: []string{"Movie"}})
			if len(otherItems.Items) != 1 {
				t.Fatal("unrelated deletion fixture did not expose exactly one source")
			}
			if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, otherItems.Items[0].ID); err != nil {
				t.Fatalf("publication prevented deletion in another library: %v", err)
			}
			mediaPublicationAdmissionReady(t, fixture, operation)
			if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID); err != nil {
				t.Fatalf("ready candidate prevented source deletion: %v", err)
			}
			if _, err := os.Stat(fixture.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("permitted source deletion retained the original path")
			}
		})
	}
}

func TestMediaPublicationScanAdmissionRechecksAfterLibraryLockWait(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "manual"
		if scheduled {
			name = "scheduled"
		}
		t.Run(name, func(t *testing.T) {
			fixture := mediaSourceTestCatalog(t, nil)
			_, children := taskScanFixture(t, fixture.ctx, fixture.pool, fixture.library)
			beforeChild := taskScanRowSnapshot(t, fixture.ctx, fixture.pool, "task_run_children", children[0])
			before := catalogAuditSnapshot(t, fixture.ctx, fixture.pool)
			gate, err := fixture.pool.Begin(fixture.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(gate)
			if _, err := gate.Exec(fixture.ctx, `SELECT id FROM libraries WHERE id=$1 FOR UPDATE`, fixture.library.ID); err != nil {
				t.Fatal("lock the publication library fixture")
			}
			finished := make(chan error, 1)
			go func() {
				var err error
				if scheduled {
					_, err = fixture.store.AdmitTaskScan(fixture.ctx, children[0])
				} else {
					_, err = fixture.store.StartScan(fixture.ctx, fixture.library.ID)
				}
				finished <- err
			}()
			taskScanWaitOwnerBlocked(t, fixture.ctx, fixture.pool, fixture.store, gate.Conn().PgConn().PID())
			mediaPublicationAdmissionSeed(t, fixture.ctx, gate, fixture.item.ID, "prepared")
			if err := gate.Commit(fixture.ctx); err != nil {
				t.Fatal("commit publication barrier before releasing scan admission")
			}
			select {
			case err := <-finished:
				mediaPublicationAdmissionBusy(t, err)
			case <-time.After(15 * time.Second):
				t.Fatal("scan admission did not finish after publication released its library lock")
			}
			if after := catalogAuditSnapshot(t, fixture.ctx, fixture.pool); after != before {
				t.Fatal("waiting scan used its earlier view and changed the catalog after publication admission")
			}
			if after := taskScanRowSnapshot(t, fixture.ctx, fixture.pool, "task_run_children", children[0]); after != beforeChild {
				t.Fatal("waiting scheduled scan linked its child after publication admission")
			}
		})
	}
}
