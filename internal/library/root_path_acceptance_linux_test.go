//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestRootPathAcceptanceCreateScanAndOpenMedia(t *testing.T) {
	for _, test := range []struct {
		name, relative string
		exactAnchor    bool
	}{
		{name: "nested-root", relative: `a\b`},
		{name: "nested-components", relative: `collections\a/nested\b`},
		{name: "exact-anchor", relative: `a\b`, exactAnchor: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
				return forceProbeTestInfo(file)
			})
			ctx, pool, store, approved, userID := libraryIntegrationStore(t, prober)
			const contents = "video:backslash-root"
			registered := filepath.Join(approved, filepath.FromSlash(test.relative))
			mediaPath := libraryIntegrationFile(t, registered, "Nested/Feature.mkv", contents)
			allowed, relative := approved, filepath.FromSlash(test.relative)
			if test.exactAnchor {
				store.mu.Lock()
				store.roots = append(store.roots, approvedRoot{path: registered})
				store.mu.Unlock()
				allowed, relative = registered, "."
			}
			collection := libraryIntegrationCreate(t, ctx, store, "Backslash root", "movies", registered)
			var root libraryRoot
			if err := pool.QueryRow(ctx, `SELECT id, library_id, path, allowed_path, relative_path
				FROM library_roots WHERE library_id = $1`, collection.ID).
				Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
				t.Fatal(err)
			}
			if root.path != registered || root.allowedPath != allowed || root.relativePath != relative {
				t.Fatalf("registration changed the literal directory name or approved mapping: %+v", root)
			}
			job := libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
			if job.Scanned != 1 || job.Added != 1 || job.Error != "" {
				t.Fatalf("scan did not index the media under the accepted root: %+v", job)
			}
			item := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, ctx, store, Query{
				UserID: userID, ParentID: collection.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
			}).Items, mediaPath)
			rootPathAcceptanceOpen(t, ctx, store, userID, item.ID, "", mediaPath, contents)
		})
	}
}

func TestRootPathAcceptanceMoveAndOpenMedia(t *testing.T) {
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		return forceProbeTestInfo(file)
	})
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, prober)
	const contents = "video:moved-backslash-root"
	original := libraryIntegrationFile(t, approved, "before/Feature.mkv", contents)
	originalRoot := filepath.Dir(original)
	collection := libraryIntegrationCreate(t, ctx, store, "Moved root", "movies", originalRoot)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	item := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, ctx, store, Query{
		UserID: userID, ParentID: collection.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
	}).Items, original)
	before := rootPathAcceptanceOpen(t, ctx, store, userID, item.ID, "", original, contents)
	actor := metadataEditTestActor(t, ctx, pool, "backslash-root-editor")
	detail, err := store.GetLibraryEditing(ctx, collection.ID)
	if err != nil || len(detail.RegisteredPaths) != 1 {
		t.Fatalf("read original root registration: %+v, %v", detail, err)
	}
	movedRoot := filepath.Join(approved, `a\b`)
	if err := os.Rename(originalRoot, movedRoot); err != nil {
		t.Fatal(err)
	}
	paths := []string{movedRoot}
	updated, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, collection.ID, LibraryUpdate{
		Revision: detail.Library.Revision, Paths: &paths,
		PathReplacements: []LibraryPathReplacement{{From: originalRoot, To: movedRoot}},
	})
	if err != nil {
		t.Fatalf("register the move to a literal backslash directory: %v", err)
	}
	if len(updated.RegisteredPaths) != 1 || updated.RegisteredPaths[0].ID != detail.RegisteredPaths[0].ID ||
		updated.RegisteredPaths[0].Path != movedRoot {
		t.Fatalf("move changed the registered root identity or directory name: %+v", updated)
	}
	movedPath := filepath.Join(movedRoot, "Feature.mkv")
	rootPathAcceptanceOpen(t, ctx, store, userID, item.ID, before.SourceID, movedPath, contents)
	libraryIntegrationScan(t, ctx, store, collection.ID, "Completed")
	rescanned := libraryIntegrationItemByPath(t, libraryIntegrationQuery(t, ctx, store, Query{
		UserID: userID, ParentID: collection.ID, Recursive: true, IncludeItemTypes: []string{"Movie"},
	}).Items, movedPath)
	if rescanned.ID != item.ID {
		t.Fatal("rescan replaced the moved media identity")
	}
	rootPathAcceptanceOpen(t, ctx, store, userID, item.ID, before.SourceID, movedPath, contents)
}

func TestRootPathAcceptanceKeepsMediaRelativePathRestrictions(t *testing.T) {
	root := libraryRoot{id: "root", libraryID: "library", path: `/media/a\b`, allowedPath: "/media", relativePath: `a\b`}
	snapshot := indexedMediaSource{
		root: root, relativePath: "Feature.mkv",
		mediaFile: MediaFile{Size: 17, Item: Item{
			ID: "item", LibraryID: root.libraryID, Path: filepath.Join(root.path, "Feature.mkv"), Media: &media.Info{Size: 17},
		}},
	}
	spec := fileDeletionSpec{
		Root: root, RelativePath: snapshot.relativePath, Identity: "1:2", Size: 17,
		ModifiedAt: time.Unix(1, 0), ChangeTimeNs: 1, StageName: ".goby-delete-0123456789abcdef0123456789abcdef",
	}
	if err := validateMediaSource(snapshot); err != nil || !validFileDeletionSpec(spec) {
		t.Fatalf("a literal backslash in the root rejected a valid media snapshot: %v", err)
	}
	for name, relative := range map[string]string{
		"backslash":      `Feature\Part.mkv`,
		"traversal":      "../Feature.mkv",
		"absolute":       "/Feature.mkv",
		"noncanonical":   "Nested//Feature.mkv",
		"nul":            "Feature\x00.mkv",
		"root-directory": ".",
	} {
		t.Run(name, func(t *testing.T) {
			changed := snapshot
			changed.relativePath = relative
			changed.mediaFile.Item.Path = filepath.Join(root.path, filepath.FromSlash(relative))
			if err := validateMediaSource(changed); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("root path acceptance relaxed the media relative path policy: %v", err)
			}
			changedSpec := spec
			changedSpec.RelativePath = relative
			if validFileDeletionSpec(changedSpec) {
				t.Fatal("root path acceptance relaxed the deletion relative path policy")
			}
		})
	}
}

func rootPathAcceptanceOpen(t *testing.T, ctx context.Context, store *Store, userID, itemID, sourceID, path, contents string) MediaFile {
	t.Helper()
	file, source, err := store.OpenMedia(ctx, userID, itemID, sourceID)
	if err != nil {
		t.Fatalf("open media under the accepted root: %v", err)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(data) != contents || source.Item.ID != itemID ||
		source.Item.Path != path || source.SourceID != media.SourceID(itemID) || source.Size != int64(len(contents)) ||
		source.Item.Media == nil || source.Item.Media.ProbeVersion != media.CurrentProbeVersion || source.Item.Media.FileChangeTimeNs <= 0 {
		t.Fatalf("accepted root did not preserve the indexed source and bytes: source=%+v read=%v close=%v", source, readErr, closeErr)
	}
	return source
}
