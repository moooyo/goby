package identity_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
)

func seedDeletionFolderCatalog(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO libraries(id,name,collection_type) VALUES
		('grant-library','Grant Library','mixed'),('other-library','Other Library','movies'),('virtual-library','Virtual Library','mixed');
		INSERT INTO library_roots(id,library_id,path,allowed_path,relative_path) VALUES
		('grant-root','grant-library','/grant/media','/grant','media'),('other-root','other-library','/other/media','/other','media');
		INSERT INTO items(id,library_id,parent_id,name,sort_name,type,is_folder) VALUES
		('grant-library','grant-library',NULL,'Grant Library','Grant Library','CollectionFolder',true),
		('other-library','other-library',NULL,'Other Library','Other Library','CollectionFolder',true),
		('virtual-playlist','virtual-library',NULL,'Virtual Playlist','Virtual Playlist','Playlist',true);
		INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,path,relative_path) VALUES
		('grant-folder','grant-library','grant-root','grant-library','Nested Folder','Nested Folder','Folder',true,'/grant/media/nested','nested'),
		('grant-series','grant-library','grant-root','grant-library','Synthetic Series','Synthetic Series','Series',true,'','synthetic'),
		('grant-leaf','grant-library','grant-root','grant-folder','Movie','Movie','Movie',false,'/grant/media/nested/movie.mkv','nested/movie.mkv'),
		('reserved-folder','grant-library','grant-root','grant-library','Reserved','Reserved','Folder',true,'/grant/media/reserved','reserved'),
		('wrong-root-folder','other-library','grant-root','other-library','Wrong root','Wrong root','Folder',true,'','wrong');
		INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory) VALUES('grant-root','reserved',true)`); err != nil {
		t.Fatal(err)
	}
}

func TestStoreDeletionFolderGrantRechecksTypeAfterCatalogLockWait(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	seedDeletionFolderCatalog(t, ctx, pool)
	viewer, err := store.CreateUser(ctx, "Racing Folder Grant", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	update := managedUpdate(readManagedUser(t, ctx, store, viewer.ID))
	update.Policy.EnableContentDeletionFromFolders = []string{"grant-folder"}
	before := managedSnapshot(t, ctx, pool, viewer.ID)
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var id string
	if err := blocker.QueryRow(ctx, `SELECT id FROM items WHERE id='grant-folder' FOR UPDATE`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var updateErr error
	go func() { defer close(done); _, updateErr = store.UpdateManagedUser(ctx, actor, viewer.ID, update) }()
	waitManagedBlockedQuery(t, ctx, pool, int32(blocker.Conn().PgConn().PID()), "SELECT id FROM items WHERE id=ANY", done)
	if _, err := blocker.Exec(ctx, `UPDATE items SET type='Movie',is_folder=false WHERE id='grant-folder'`); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("folder grant did not finish after the catalog lock was released")
	}
	assertManagedFieldError(t, updateErr, "Policy.EnableContentDeletionFromFolders")
	assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
}

func TestStoreDeletionFolderPickerReturnsOnlyCurrentBoundedCatalogScopes(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	seedDeletionFolderCatalog(t, ctx, pool)
	page, err := store.ListDeletionFolders(ctx, actor, identity.DeletionFolderQuery{Limit: 2})
	if err != nil || page.TotalRecordCount != 4 || len(page.Items) != 2 || page.Items[0].ID != "grant-library" {
		t.Fatalf("folder choices: %+v %v", page, err)
	}
	second, err := store.ListDeletionFolders(ctx, actor, identity.DeletionFolderQuery{StartIndex: 2, Limit: 2})
	if err != nil || second.TotalRecordCount != 4 || len(second.Items) != 2 {
		t.Fatalf("folder choice page: %+v %v", second, err)
	}
	seen := map[string]bool{}
	for _, item := range append(page.Items, second.Items...) {
		if seen[item.ID] {
			t.Fatal("duplicate choice")
		}
		seen[item.ID] = true
	}
	for _, id := range []string{"grant-library", "grant-folder", "grant-series", "other-library"} {
		if !seen[id] {
			t.Fatalf("missing choice %s", id)
		}
	}
	filtered, err := store.ListDeletionFolders(ctx, actor, identity.DeletionFolderQuery{LibraryID: "grant-library", SearchTerm: "SYNTHETIC"})
	if err != nil || filtered.TotalRecordCount != 1 || len(filtered.Items) != 1 || filtered.Items[0].ID != "grant-series" || filtered.Items[0].Path != "" {
		t.Fatalf("synthetic folder choice: %+v %v", filtered, err)
	}
	viewer, err := store.CreateUser(ctx, "Folder Picker Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	_, viewerActor := managedLogin(t, ctx, store, viewer, "viewer-password", "emby")
	if _, err := store.ListDeletionFolders(ctx, viewerActor, identity.DeletionFolderQuery{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("non-native picker authority: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListDeletionFolders(ctx, actor, identity.DeletionFolderQuery{}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("revoked picker authority: %v", err)
	}
}

func TestStoreDeletionFolderGrantsValidateNewValuesAndPreserveLegacyWithoutCopyingThem(t *testing.T) {
	ctx, pool, store := identityTestStore(t)
	admin := bootstrapTestAdmin(t, ctx, store)
	_, actor := managedLogin(t, ctx, store, admin, "administrator-password", "admin")
	seedDeletionFolderCatalog(t, ctx, pool)
	viewer, err := store.CreateUser(ctx, "Folder Grant Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	current := readManagedUser(t, ctx, store, viewer.ID)
	update := managedUpdate(current)
	update.Policy.EnableContentDeletionFromFolders = []string{"grant-folder", "grant-library", "grant-series"}
	result, err := store.UpdateManagedUser(ctx, actor, viewer.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.User.Policy.EnableContentDeletionFromFolders, []string{"grant-folder", "grant-library", "grant-series"}) {
		t.Fatal("new folder grant changed identity")
	}
	copy, err := store.CreateManagedUserCopy(ctx, actor, "Folder Grant Copy", viewer.ID, []string{"UserPolicy"})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := identity.ParseManagedPolicy(copy.Policy)
	if err != nil || !reflect.DeepEqual(copied.EnableContentDeletionFromFolders, result.User.Policy.EnableContentDeletionFromFolders) {
		t.Fatal("valid folder grants failed copying")
	}
	for _, id := range []string{"grant-leaf", "reserved-folder", "wrong-root-folder", "virtual-playlist", "virtual-library", "grant-root", "missing-0123456789abcdef0123456789abcdef", "/grant/media/nested", "unknown-folder"} {
		before := managedSnapshot(t, ctx, pool, viewer.ID)
		update = managedUpdate(readManagedUser(t, ctx, store, viewer.ID))
		update.Policy.EnableContentDeletionFromFolders = []string{id}
		_, err := store.UpdateManagedUser(ctx, actor, viewer.ID, update)
		assertManagedFieldError(t, err, "Policy.EnableContentDeletionFromFolders")
		assertManagedUnchanged(t, ctx, pool, viewer.ID, before)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletionFromFolders":["/legacy/folder","removed-folder"],"BlockUnratedItems":["Game","Movie"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	current = readManagedUser(t, ctx, store, viewer.ID)
	update = managedUpdate(current)
	update.Name = "Renamed Legacy Viewer"
	result, err = store.UpdateManagedUser(ctx, actor, viewer.ID, update)
	if err != nil || !reflect.DeepEqual(result.User.Policy.EnableContentDeletionFromFolders, current.Policy.EnableContentDeletionFromFolders) || !reflect.DeepEqual(result.User.Policy.BlockUnratedItems, current.Policy.BlockUnratedItems) {
		t.Fatalf("unrelated save discarded legacy policy: %v", err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Unchecked Legacy Copy", viewer.ID, []string{"UserPolicy"}); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("unchecked new legacy grant copy: %v", err)
	}
	update = managedUpdate(readManagedUser(t, ctx, store, viewer.ID))
	update.Policy.EnableContentDeletionFromFolders = []string{}
	update.Policy.BlockUnratedItems = []string{"Movie"}
	if _, err := store.UpdateManagedUser(ctx, actor, viewer.ID, update); err != nil {
		t.Fatal(err)
	}
	update = managedUpdate(readManagedUser(t, ctx, store, viewer.ID))
	update.Policy.BlockUnratedItems = []string{"Game"}
	_, err = store.UpdateManagedUser(ctx, actor, viewer.ID, update)
	assertManagedFieldError(t, err, "Policy.BlockUnratedItems")
	if _, err := pool.Exec(ctx, `UPDATE users SET policy=policy||'{"BlockUnratedItems":["Game"]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateManagedUserCopy(ctx, actor, "Inactive Category Copy", viewer.ID, []string{"UserPolicy"}); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatalf("inactive new category copy: %v", err)
	}
}
