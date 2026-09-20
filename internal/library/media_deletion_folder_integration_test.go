//go:build linux

package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func updateDeletionFolderGrant(t *testing.T, fixture mediaSourceFixture, admin identity.Principal, folders []string) {
	t.Helper()
	users := identity.New(fixture.pool)
	current, err := users.GetManagedUser(fixture.ctx, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	policy := current.Policy
	policy.EnableContentDeletion = false
	policy.EnableContentDeletionFromFolders = folders
	if _, err := users.UpdateManagedUser(fixture.ctx, admin, fixture.userID, identity.ManagedUserUpdate{
		Revision: current.Revision, Name: current.User.Name, IsAdministrator: current.User.IsAdministrator, IsDisabled: current.User.IsDisabled, Policy: policy,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMediaDeletionFolderGrantUsesManagedWriterAndProtectsSiblingFiles(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	admin := metadataEditTestActor(t, fixture.ctx, fixture.pool, "folder-deletion-administrator")
	siblingPath := libraryIntegrationFile(t, fixture.allowedRoot, "movies/Other/Sibling.mkv", "video:sibling")
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	var siblingID string
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT id FROM items WHERE path=$1`, siblingPath).Scan(&siblingID); err != nil {
		t.Fatal(err)
	}
	updateDeletionFolderGrant(t, fixture, admin, []string{fixture.item.ParentID})
	if _, err := fixture.store.MediaDeletionInfo(fixture.ctx, actor, siblingID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("folder grant reached a sibling: %v", err)
	}
	paths, err := fixture.store.MediaDeletionInfo(fixture.ctx, actor, fixture.item.ID)
	if err != nil || len(paths) != 1 || filepath.Clean(paths[0]) != fixture.path {
		t.Fatalf("folder grant did not authorize its exact leaf: %v", err)
	}
	if err := fixture.store.DeleteMediaFor(fixture.ctx, actor, fixture.item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fixture.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authorized file remained: %v", err)
	}
	if data, err := os.ReadFile(siblingPath); err != nil || string(data) != "video:sibling" {
		t.Fatal("sibling bytes changed")
	}
}

func TestMediaDeletionFolderRevocationAfterStagingRestoresExactSource(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := mediaDeletionTestActor(t, fixture)
	admin := metadataEditTestActor(t, fixture.ctx, fixture.pool, "folder-revocation-administrator")
	updateDeletionFolderGrant(t, fixture, admin, []string{fixture.item.ParentID})
	err := fixture.store.performManagedFileDeletionWithHook(fixture.ctx, actor, fixture.item.ID, "media", -1, func() {
		updateDeletionFolderGrant(t, fixture, admin, []string{})
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked folder grant published deletion: %v", err)
	}
	if data, err := os.ReadFile(fixture.path); err != nil || string(data) != fixture.contents {
		t.Fatal("revocation did not restore original source bytes")
	}
	var present, pending bool
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id=$1),
		EXISTS(SELECT 1 FROM media_deletion_operations WHERE item_id=$1)`, fixture.item.ID).Scan(&present, &pending); err != nil || !present || pending {
		t.Fatalf("revoked delete left catalog/journal inconsistency: %v", err)
	}
}
