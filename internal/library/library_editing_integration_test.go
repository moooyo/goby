package library

import (
	"context"
	"encoding/json"
	"errors"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestLibraryEditingMoveRetainsCatalogIdentityStateAndAuthority(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("indexed source reads require Linux file change identities")
	}
	// The older catalog fixture intentionally omits source-serving probe facts.
	// This test opens the actual file, so use the current version and the real
	// descriptor's size/change time both before and after the directory move.
	prober := forceProbeVersionedFunc(func(ctx context.Context, file *os.File) (media.Info, error) {
		if err := ctx.Err(); err != nil {
			return media.Info{}, err
		}
		return forceProbeTestInfo(file)
	})
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, prober)
	oldFile := libraryIntegrationFile(t, approved, "before/Film.mp4", "video:library-move")
	oldPath := filepath.Dir(oldFile)
	libraryIntegrationFile(t, approved, "before/Film.nfo", `<movie><title>Local title</title><plot>Local description</plot></movie>`)
	library := libraryIntegrationCreate(t, ctx, store, "Before", "movies", oldPath)
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, oldFile)
	actor := metadataEditTestActor(t, ctx, pool, "library-move-admin")
	metadataBefore := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	metadataBefore = metadataEditTestUpdate(t, ctx, store, actor, metadataBefore, map[string]json.RawMessage{"Name": metadataEditTestRaw(t, "Manual title")}, []string{"Overview"})
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, PlaybackPositionTicks: 123000000, PlayCount: 3, IsFavorite: true})
	libraryIntegrationUser(t, ctx, pool, "move-allowed", false, false, []string{library.ID})
	libraryIntegrationUser(t, ctx, pool, "move-denied", false, false, nil)
	beforeFile, beforeSource, err := store.OpenMedia(ctx, "move-allowed", item.ID, "")
	if err != nil {
		t.Fatalf("fixture cannot open its original indexed media before moving: %v", err)
	}
	beforeData, beforeReadErr := io.ReadAll(beforeFile)
	beforeCloseErr := beforeFile.Close()
	if beforeReadErr != nil || beforeCloseErr != nil || string(beforeData) != "video:library-move" ||
		beforeSource.SourceID != media.SourceID(item.ID) || beforeSource.Item.Media == nil ||
		beforeSource.Item.Media.ProbeVersion != media.CurrentProbeVersion || beforeSource.Item.Media.FileChangeTimeNs <= 0 {
		t.Fatalf("original source lacks current probe facts, identity, or bytes: source=%+v read=%v close=%v", beforeSource, beforeReadErr, beforeCloseErr)
	}
	detail, err := store.GetLibraryEditing(ctx, library.ID)
	if err != nil || len(detail.RegisteredPaths) != 1 || detail.RegisteredPaths[0].ItemCount < 1 {
		t.Fatalf("read editable library: %+v, %v", detail, err)
	}
	newPath := filepath.Join(approved, "after")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	name := "After"
	paths := []string{newPath}
	updated, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{
		Revision: detail.Library.Revision, Name: &name, Paths: &paths,
		PathReplacements: []LibraryPathReplacement{{From: oldPath, To: newPath}},
	})
	if err != nil {
		t.Fatalf("move library registration: %v", err)
	}
	if updated.Library.ID != library.ID || updated.Library.Name != name || updated.Library.Revision == detail.Library.Revision ||
		len(updated.RegisteredPaths) != 1 || updated.RegisteredPaths[0].ID != detail.RegisteredPaths[0].ID {
		t.Fatalf("move replaced stable identities or revision: %+v", updated)
	}
	newFile := filepath.Join(newPath, "Film.mp4")
	moved, err := store.GetItem(ctx, "move-allowed", item.ID)
	if err != nil || moved.Path != newFile || moved.Name != "Manual title" {
		t.Fatalf("moved catalog projection: %+v, %v", moved, err)
	}
	if _, err := store.GetItem(ctx, "move-denied", item.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("move changed ACL: %v", err)
	}
	file, movedSource, err := store.OpenMedia(ctx, "move-allowed", item.ID, beforeSource.SourceID)
	if err != nil {
		t.Fatalf("open moved media: %v", err)
	}
	data, readErr := io.ReadAll(file)
	file.Close()
	if readErr != nil || string(data) != "video:library-move" || movedSource.SourceID != beforeSource.SourceID {
		t.Fatalf("moved root did not retain real file access: %q, %v", data, readErr)
	}
	state, err := store.GetUserData(ctx, userID, item.ID)
	if err != nil || !state.IsFavorite || state.PlayCount != 3 || state.PlaybackPositionTicks != 123000000 {
		t.Fatalf("move lost user state: %+v, %v", state, err)
	}
	metadataAfter := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if !reflect.DeepEqual(metadataAfter.Overrides, metadataBefore.Overrides) || !reflect.DeepEqual(metadataAfter.LockedValues, metadataBefore.LockedValues) ||
		metadataAfter.Revision == metadataBefore.Revision {
		t.Fatalf("move changed controls or accepted stale source revision: %+v", metadataAfter)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	rescanned := nfoCatalogItem(t, ctx, store, userID, library.ID, newFile)
	if rescanned.ID != item.ID || rescanned.Name != "Manual title" {
		t.Fatalf("rescan lost moved identity or overrides: %+v", rescanned)
	}
	entry := catalogAuditOnlyFact(t, ctx, pool, activity.ActionLibraryUpdated, library.ID)
	if strings.Contains(entry.raw, approved) || strings.Contains(entry.raw, name) {
		t.Fatal("library audit stored a private path or name")
	}
	catalogAuditAssertActor(t, entry, actor, activity.SourceNative, activity.ResourceLibrary)
}

func TestLibraryEditingPathRemovalRequiresAcknowledgmentAndRetainsOtherRoots(t *testing.T) {
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	firstFile := libraryIntegrationFile(t, approved, "first/First.mp4", "video:first")
	secondFile := libraryIntegrationFile(t, approved, "second/Second.mp4", "video:second")
	library, err := store.CreateLibrary(ctx, "Two roots", "movies", []string{filepath.Dir(firstFile), filepath.Dir(secondFile)})
	if err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	items := libraryIntegrationQuery(t, ctx, store, Query{UserID: userID, ParentID: library.ID, Recursive: true})
	first := libraryIntegrationItemByPath(t, items.Items, firstFile)
	second := libraryIntegrationItemByPath(t, items.Items, secondFile)
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: second.ID, IsFavorite: true})
	actor := metadataEditTestActor(t, ctx, pool, "library-remove-path-admin")
	detail, err := store.GetLibraryEditing(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Dir(secondFile)}
	update := LibraryUpdate{Revision: detail.Library.Revision, Paths: &paths}
	before := catalogAuditSnapshot(t, ctx, pool)
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, update); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unacknowledged removal: %v", err)
	}
	if after := catalogAuditSnapshot(t, ctx, pool); after != before {
		t.Fatal("rejected removal mutated the catalog")
	}
	update.AcknowledgePathRemoval = true
	updated, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Library.Paths) != 1 || updated.Library.Paths[0] != paths[0] {
		t.Fatalf("wrong surviving roots: %+v", updated)
	}
	if _, err := store.GetItem(ctx, userID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed root remains in catalog: %v", err)
	}
	if _, err := os.Stat(firstFile); err != nil {
		t.Fatalf("catalog removal touched the media file: %v", err)
	}
	secondAfter, err := store.GetItem(ctx, userID, second.ID)
	if err != nil || secondAfter.UserData == nil || !secondAfter.UserData.IsFavorite {
		t.Fatalf("surviving root lost identity/state: %+v, %v", secondAfter, err)
	}
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, update); !errors.Is(err, ErrLibraryConflict) {
		t.Fatalf("stale removal was replayed: %v", err)
	}
}

func TestLibraryEditingOptionsHaveScannerConsumersAndRetainOverrides(t *testing.T) {
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, approved, "options/Film.mp4", "video:options")
	nfo := libraryIntegrationFile(t, approved, "options/Film.nfo", `<movie><title>First source title</title></movie>`)
	imagePath := filepath.Join(filepath.Dir(path), "Film-poster.png")
	imageScanTestWrite(t, imagePath, color.NRGBA{R: 255, A: 255})
	library := libraryIntegrationCreate(t, ctx, store, "Options", "movies", filepath.Dir(path))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, path)
	images := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if len(images) != 1 {
		t.Fatal("initial local artwork was not imported")
	}
	actor := metadataEditTestActor(t, ctx, pool, "library-options-admin")
	metadataBefore := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	metadataBefore = metadataEditTestUpdate(t, ctx, store, actor, metadataBefore, map[string]json.RawMessage{"Name": metadataEditTestRaw(t, "Manual title")}, nil)
	detail, err := store.GetLibraryEditing(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	updated, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{
		Revision: detail.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableLocalMetadata: &disabled, EnableLocalImages: &disabled},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nfo, []byte(`<movie><title>Second source title</title></movie>`), 0600); err != nil {
		t.Fatal(err)
	}
	imageScanTestWrite(t, imagePath, color.NRGBA{B: 255, A: 255})
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	retained := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	retainedImages := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if retained.Automatic.Name != metadataBefore.Automatic.Name || retained.Effective.Name != "Manual title" || len(retainedImages) != 1 || retainedImages[0].Tag != images[0].Tag {
		t.Fatalf("disabled importers changed accepted sources/controls: metadata=%+v images=%+v", retained, retainedImages)
	}
	enabled := true
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{
		Revision: updated.Library.Revision, LibraryOptions: &LibraryOptionsUpdate{EnableLocalMetadata: &enabled, EnableLocalImages: &enabled},
	}); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	refreshed := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	refreshedImages := imageScanTestList(t, ctx, store, userID, item.ID, "Primary")
	if refreshed.Automatic.Name != "Second source title" || refreshed.Effective.Name != "Manual title" || len(refreshedImages) != 1 || refreshedImages[0].Tag == images[0].Tag {
		t.Fatalf("re-enabled importers did not refresh actual sources: metadata=%+v images=%+v", refreshed, refreshedImages)
	}
}

func TestLibraryEditingConcurrentRevisionAndRevokedAdministrator(t *testing.T) {
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
	path := libraryIntegrationFile(t, approved, "concurrent/Film.mp4", "video:concurrent")
	library := libraryIntegrationCreate(t, ctx, store, "Initial", "movies", filepath.Dir(path))
	actor := metadataEditTestActor(t, ctx, pool, "library-concurrent-admin")
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 2)
	for _, name := range []string{"First", "Second"} {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			_, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{Revision: library.Revision, Name: &name})
			errorsChannel <- err
		}(name)
	}
	wait.Wait()
	close(errorsChannel)
	accepted, conflicts := 0, 0
	for err := range errorsChannel {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrLibraryConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent result: %v", err)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("CAS accepted=%d conflicts=%d", accepted, conflicts)
	}
	detail, err := store.GetLibraryEditing(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator=false WHERE id=$1`, actor.User.ID); err != nil {
		t.Fatal(err)
	}
	name := "Revoked"
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{Revision: detail.Library.Revision, Name: &name}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stale administrator principal authorized an edit: %v", err)
	}
}

func TestLibraryEditingRejectsActiveScanAndIndependentRootRevision(t *testing.T) {
	prober := &libraryFixtureProber{block: true, entered: make(chan struct{}, 1), release: make(chan struct{}), cancelled: make(chan struct{}, 1)}
	ctx, pool, store, approved, _ := libraryIntegrationStore(t, prober)
	path := libraryIntegrationFile(t, approved, "busy/Film.mp4", "video:busy")
	library := libraryIntegrationCreate(t, ctx, store, "Busy", "movies", filepath.Dir(path))
	actor := metadataEditTestActor(t, ctx, pool, "library-busy-admin")
	job, err := store.StartScan(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-prober.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	name := "Must wait"
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{Revision: library.Revision, Name: &name}); !errors.Is(err, ErrBusy) {
		t.Fatalf("active scan edit: %v", err)
	}
	close(prober.release)
	libraryIntegrationWaitJob(t, ctx, store, job.ID, "Completed")
	if _, err := pool.Exec(ctx, `UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1`, library.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{Revision: library.Revision, Name: &name}); !errors.Is(err, ErrLibraryConflict) {
		t.Fatalf("independent root change did not invalidate library revision: %v", err)
	}
}

func TestLibraryEditingAuditFailureAndFinalRevocationRollBackRootPublication(t *testing.T) {
	for _, expireActor := range []bool{false, true} {
		name := "audit failure"
		if expireActor {
			name = "final credential revocation"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, approved, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			original := filepath.Dir(libraryIntegrationFile(t, approved, "original/Film.mp4", "video:original"))
			replacement := filepath.Dir(libraryIntegrationFile(t, approved, "replacement/Film.mp4", "video:replacement"))
			value := libraryIntegrationCreate(t, ctx, store, "Atomic editing", "movies", original)
			actor := metadataEditTestActor(t, ctx, pool, "library-atomic-admin")
			detail, err := store.GetLibraryEditing(ctx, value.ID)
			if err != nil {
				t.Fatal(err)
			}
			rootID := detail.RegisteredPaths[0].ID
			store.mu.Lock()
			anchor := store.rootBindingAnchors[rootID]
			store.mu.Unlock()
			before := catalogAuditSnapshot(t, ctx, pool)
			catalogAuditInstallTrigger(t, ctx, pool, activity.ActionLibraryUpdated, expireActor)
			paths := []string{replacement}
			_, err = store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, value.ID, LibraryUpdate{
				Revision: value.Revision, Paths: &paths, AcknowledgePathRemoval: true,
			})
			if err == nil {
				t.Fatal("failed/finally unauthorized audit committed an edit")
			}
			catalogAuditAssertTrigger(t, ctx, pool)
			if after := catalogAuditSnapshot(t, ctx, pool); after != before {
				t.Fatal("failed edit changed persisted catalog facts")
			}
			store.mu.Lock()
			current, exists := store.rootBindingAnchors[rootID]
			count := len(store.rootBindingAnchors)
			store.mu.Unlock()
			if !exists || current != anchor || count != 1 {
				t.Fatal("failed edit published or retired a root anchor")
			}
		})
	}
}
