package library

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestServerDirectoriesStayWithinApprovedRootsAndFreshAuthority(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	actor := metadataEditTestActor(t, ctx, pool, "directory-browser-admin")
	first := filepath.Dir(libraryIntegrationFile(t, approved, "alpha/file.txt", "not media"))
	second := filepath.Dir(libraryIntegrationFile(t, approved, "beta/file.txt", "not media"))
	libraryIntegrationFile(t, approved, "loose-file.txt", "not a directory")
	outside := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(approved, "escape")); err != nil {
			t.Fatal(err)
		}
	}
	roots, err := store.BrowseServerDirectories(ctx, actor, identity.AdministratorNative, "", 0, 100)
	if err != nil || len(roots.Items) != 1 || roots.Items[0].Path != approved || roots.ParentPath != "" {
		t.Fatalf("root browser leaked or omitted approved paths: %+v, %v", roots, err)
	}
	page, err := store.BrowseServerDirectories(ctx, actor, identity.AdministratorNative, approved, 0, 1)
	if err != nil || page.TotalRecordCount != 2 || len(page.Items) != 1 || page.Items[0].Path != first || page.ParentPath != "" {
		t.Fatalf("directory filtering/paging: %+v, %v", page, err)
	}
	page, err = store.BrowseServerDirectories(ctx, actor, identity.AdministratorNative, approved, 1, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].Path != second {
		t.Fatalf("second directory page: %+v, %v", page, err)
	}
	parent, err := store.ServerDirectoryParent(ctx, actor, identity.AdministratorNative, second)
	if err != nil || parent != approved {
		t.Fatalf("approved parent: %q, %v", parent, err)
	}
	parent, err = store.ServerDirectoryParent(ctx, actor, identity.AdministratorNative, approved)
	if err != nil || parent != "" {
		t.Fatalf("escaped approved parent boundary: %q, %v", parent, err)
	}
	if canonical, err := store.ValidateServerDirectory(ctx, actor, identity.AdministratorNative, first); err != nil || canonical != first {
		t.Fatalf("validate allowed directory: %q, %v", canonical, err)
	}
	for _, path := range []string{outside, filepath.Join(approved, "escape")} {
		if runtime.GOOS == "windows" && filepath.Base(path) == "escape" {
			continue
		}
		if _, err := store.ValidateServerDirectory(ctx, actor, identity.AdministratorNative, path); !errors.Is(err, ErrForbidden) {
			t.Fatalf("outside directory accepted: %q, %v", path, err)
		}
	}
	traversal := approved + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(approved)
	if _, err := store.ValidateServerDirectory(ctx, actor, identity.AdministratorNative, traversal); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("traversal accepted: %v", err)
	}
	if _, err := store.ValidateServerDirectory(ctx, actor, identity.AdministratorNative, filepath.Join(approved, "loose-file.txt")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("file accepted as directory: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BrowseServerDirectories(ctx, actor, identity.AdministratorNative, approved, 0, 100); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked administrator browsed storage: %v", err)
	}
}
