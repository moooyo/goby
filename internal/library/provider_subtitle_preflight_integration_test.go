//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/providers"
	"golang.org/x/sys/unix"
)

func providerSubtitlePreflightNames(t *testing.T, directory string) []string {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func assertProviderSubtitlePreflightUnchanged(t *testing.T, fixture mediaSourceFixture, names []string) {
	t.Helper()
	if actual := providerSubtitlePreflightNames(t, filepath.Dir(fixture.path)); !reflect.DeepEqual(actual, names) {
		t.Fatalf("preflight retained or removed a directory entry: before=%v after=%v", names, actual)
	}
	var sidecars, sources, owned int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT
		(SELECT count(*) FROM item_subtitles WHERE item_id=$1),
		(SELECT count(*) FROM item_subtitle_provider_sources WHERE item_id=$1),
		(SELECT count(*) FROM item_owned_subtitles WHERE item_id=$1)`, fixture.item.ID).
		Scan(&sidecars, &sources, &owned); err != nil || sidecars != 0 || sources != 0 || owned != 0 {
		t.Fatalf("preflight changed the subtitle catalog: sidecars=%d sources=%d owned=%d error=%v", sidecars, sources, owned, err)
	}
}

func TestProviderSubtitlePreflightPublishesAndRetainsSidecar(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := ownedSubtitleManagementActor(t, fixture, "provider-preflight-editor")
	before := providerSubtitlePreflightNames(t, filepath.Dir(fixture.path))
	snapshot, err := fixture.store.readProviderSubtitleSnapshot(fixture.ctx, &actor, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, "")
	if err != nil || tag != mediaSnapshotTag(snapshot.primary) {
		t.Fatalf("writable target did not return a source tag: tag=%q error=%v", tag, err)
	}
	assertProviderSubtitlePreflightUnchanged(t, fixture, before)
	if repeated, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, tag); err != nil || repeated != tag {
		t.Fatalf("same-source preflight did not retain its tag: %q %v", repeated, err)
	}
	assertProviderSubtitlePreflightUnchanged(t, fixture, before)
	download := providers.SubtitleDownload{Provider: "opensubtitles", RemoteID: "12:34", Language: "en", Format: "srt",
		Data: []byte(subtitleTestSRT), HearingImpaired: true}
	if err := fixture.store.RegisterDownloadedSubtitleForSource(fixture.ctx, actor, fixture.item.ID, tag, download); err != nil {
		t.Fatalf("publish offline provider bytes after the preflight: %v", err)
	}
	index, err := fixture.store.DownloadedSubtitleIndex(fixture.ctx, actor, fixture.item.ID, download.RemoteID)
	if err != nil {
		t.Fatal(err)
	}
	content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), index)
	if err != nil || string(content.Data) != subtitleTestSRT || content.Info.Owned || !content.Info.IsHearingImpaired ||
		!strings.Contains(content.Info.Filename, ".en.opensubtitles-") {
		t.Fatalf("download was not a readable, file-backed provider subtitle: info=%+v error=%v", content.Info, err)
	}
	sidecar := filepath.Join(filepath.Dir(fixture.path), content.Info.Filename)
	if err := fixture.store.RegisterDownloadedSubtitleForSource(fixture.ctx, actor, fixture.item.ID, tag, download); err != nil {
		t.Fatalf("identical offline publication was not idempotent: %v", err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if actual, err := fixture.store.DownloadedSubtitleIndex(fixture.ctx, actor, fixture.item.ID, download.RemoteID); err != nil || actual != index {
		t.Fatalf("rescan changed provider identity: index=%d error=%v", actual, err)
	}
	if err := fixture.store.Close(fixture.ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(fixture.pool, mediaSourceTestProber{inner: &libraryFixtureProber{}}, []string{fixture.allowedRoot})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := reopened.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	fixture.store = reopened
	if content, err := reopened.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), index); err != nil || string(content.Data) != subtitleTestSRT {
		t.Fatalf("provider subtitle did not survive Store reopen: %v", err)
	}
	if err := reopened.DeleteSubtitleAsUser(fixture.ctx, actor, fixture.item.ID, index); err != nil {
		t.Fatalf("delete the owned offline sidecar: %v", err)
	}
	if _, err := os.Stat(sidecar); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("provider sidecar remains after deletion: %v", err)
	}
	if _, err := reopened.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired provider subtitle remained readable: %v", err)
	}
	libraryIntegrationScan(t, fixture.ctx, reopened, fixture.library.ID, "Completed")
	if tracks := subtitleTestTracks(t, fixture); len(tracks) != 0 {
		t.Fatalf("ordinary rescan revived a deleted provider subtitle: %+v", tracks)
	}
	var providerRows int
	if err := fixture.pool.QueryRow(fixture.ctx, `SELECT count(*) FROM item_subtitle_provider_sources WHERE item_id=$1`, fixture.item.ID).Scan(&providerRows); err != nil || providerRows != 0 {
		t.Fatalf("deletion retained provider provenance: rows=%d error=%v", providerRows, err)
	}
}

func TestProviderSubtitlePreflightRejectsAuthorityAndSourceChanges(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, nil)
	actor := ownedSubtitleManagementActor(t, fixture, "provider-preflight-authority")
	before := providerSubtitlePreflightNames(t, filepath.Dir(fixture.path))
	tag, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(fixture.ctx)
	cancel()
	if result, err := fixture.store.CheckWritableSubtitleTarget(ctx, actor, fixture.item.ID, tag); result != "" || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preflight returned a usable target: %q %v", result, err)
	}
	if result, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, "different-source-tag"); result != "" || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("stale expected source was admitted: %q %v", result, err)
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if result, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, tag); result != "" || !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked actor was admitted: %q %v", result, err)
	}
	assertProviderSubtitlePreflightUnchanged(t, fixture, before)
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.path, fixture.path+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, []byte(fixture.contents), 0o600); err != nil {
		t.Fatal(err)
	}
	before = providerSubtitlePreflightNames(t, filepath.Dir(fixture.path))
	if result, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, tag); result != "" || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("replaced primary was admitted from a stale catalog: %q %v", result, err)
	}
	assertProviderSubtitlePreflightUnchanged(t, fixture, before)
}

func TestProviderSubtitlePreflightActualPermissionDenial(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("actual permission denial requires a nonroot verification process")
	}
	fixture := mediaSourceTestCatalog(t, nil)
	actor := ownedSubtitleManagementActor(t, fixture, "provider-preflight-permission")
	directory := filepath.Dir(fixture.path)
	before := providerSubtitlePreflightNames(t, directory)
	if err := os.Chmod(directory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, 0o700); err != nil {
			t.Error(err)
		}
	})
	if tag, err := fixture.store.CheckWritableSubtitleTarget(fixture.ctx, actor, fixture.item.ID, ""); tag != "" ||
		!errors.Is(err, ErrSubtitleTargetNotWritable) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("actual EACCES was not rejected before download: tag=%q error=%v", tag, err)
	}
	assertProviderSubtitlePreflightUnchanged(t, fixture, before)
}

// The launcher supplies an existing read-only fixture mount. This case neither
// changes a mount nor accepts mode bits as evidence of a read-only filesystem.
func TestProviderSubtitlePreflightActualReadOnlyMount(t *testing.T) {
	directory := os.Getenv("GOBY_TEST_PROVIDER_SUBTITLE_READONLY_DIR")
	if directory == "" {
		t.Skip("an explicit read-only subtitle fixture mount is required")
	}
	if !filepath.IsAbs(directory) {
		t.Fatal("the read-only fixture path must be absolute")
	}
	parent, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	descriptor, err := parent.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer descriptor.Close()
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(descriptor.Fd()), &filesystem); err != nil || filesystem.Flags&unix.ST_RDONLY == 0 {
		t.Fatalf("selected fixture is not on an actual read-only mount: %v", err)
	}
	before := providerSubtitlePreflightNames(t, directory)
	err = probeWritableSubtitleParent(context.Background(), parent)
	if !errors.Is(err, ErrSubtitleTargetNotWritable) || !errors.Is(err, unix.EROFS) {
		t.Fatalf("read-only mount did not reject the actual exclusive create: %v", err)
	}
	if after := providerSubtitlePreflightNames(t, directory); !reflect.DeepEqual(after, before) {
		t.Fatalf("read-only preflight changed directory entries: before=%v after=%v", before, after)
	}
}

func TestProviderSubtitlePreflightCleanupPreservesReplacement(t *testing.T) {
	directory := t.TempDir()
	parent, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	const name = ".goby-provider-subtitle-check-fixture.tmp"
	file, err := parent.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	defer file.Close()
	if err != nil {
		t.Fatalf("observe the created cleanup fixture: %v", err)
	}
	if err := parent.Rename(name, name+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte("unrelated replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeSubtitlePreflightFile(parent, name, info); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("cleanup accepted an unrelated replacement: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(directory, name)); err != nil || string(data) != "unrelated replacement" {
		t.Fatalf("cleanup removed replacement data: %v", err)
	}
}
