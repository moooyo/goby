//go:build linux

package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

func TestLibraryEditingReplacementRejectsOldProbeAndRefreshesExistingIdentity(t *testing.T) {
	const originalBytes, replacementBytes = "video:codec-a", "video:codec-b"
	var calls atomic.Int32
	prober := forceProbeVersionedFunc(func(_ context.Context, file *os.File) (media.Info, error) {
		calls.Add(1)
		content, err := io.ReadAll(file)
		if err != nil {
			return media.Info{}, err
		}
		info, err := forceProbeTestInfo(file)
		if err != nil {
			return media.Info{}, err
		}
		switch string(content) {
		case originalBytes:
			// This opaque index is a stored probe-cache fact, never an input to
			// a media decoder. The replacement must not inherit it.
			info.VideoSeekIndexes = []media.VideoSeekIndex{forceProbeTestIndex(info)}
		case replacementBytes:
			info.Streams[0].Codec = "hevc"
			info.Streams[0].Profile = "Main 10"
			info.Streams[0].BitDepth = 10
			info.Streams[0].PixelFormat = "yuv420p10le"
		default:
			return media.Info{}, fmt.Errorf("unexpected source replacement fixture")
		}
		return info, nil
	})
	ctx, pool, store, approved, userID := libraryIntegrationStore(t, prober)
	original := libraryIntegrationFile(t, approved, "original/Film.mp4", originalBytes)
	originalStat, err := os.Stat(original)
	if err != nil || media.FileChangeTime(originalStat) <= 0 {
		t.Fatalf("fixture needs an indexed Linux file identity: %v", err)
	}
	library := libraryIntegrationCreate(t, ctx, store, "Source replacement", "movies", filepath.Dir(original))
	libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	item := nfoCatalogItem(t, ctx, store, userID, library.ID, original)
	if item.Media == nil || item.Media.Streams[0].Codec != "h264" || len(item.Media.VideoSeekIndexes) != 1 || calls.Load() != 1 {
		t.Fatal("initial scan did not establish the old codec and probe cache")
	}
	actor := metadataEditTestActor(t, ctx, pool, "replacement-source-admin")
	metadata := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	metadata = metadataEditTestUpdate(t, ctx, store, actor, metadata,
		map[string]json.RawMessage{"Name": json.RawMessage(`"Preserved title"`)}, []string{"Overview"})
	userDataSeed(t, ctx, pool, userID, UserData{ItemID: item.ID, IsFavorite: true, PlayCount: 7, PlaybackPositionTicks: 123 * media.TicksPerSecond})
	userState := forceProbeUserDataSnapshot(t, ctx, pool, item.ID)
	oldFile, oldSource, err := store.OpenMedia(ctx, userID, item.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := oldFile.Close(); err != nil {
		t.Fatal(err)
	}
	if cached := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); cached.Updated != 0 || calls.Load() != 1 {
		t.Fatal("unchanged control scan did not reuse its existing probe")
	}

	// Preserve size and mtime deliberately. A path-only/size-only cache key
	// would now accept the wrong codec; inode and ctime must fence this source.
	replacement := libraryIntegrationFile(t, approved, "replacement/Film.mp4", replacementBytes)
	if err := os.Chtimes(replacement, originalStat.ModTime(), originalStat.ModTime()); err != nil {
		t.Fatal(err)
	}
	replacementStat, err := os.Stat(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if originalStat.Size() != replacementStat.Size() || !originalStat.ModTime().Equal(replacementStat.ModTime()) ||
		os.SameFile(originalStat, replacementStat) || media.FileChangeTime(originalStat) == media.FileChangeTime(replacementStat) {
		t.Fatal("replacement fixture did not isolate changed identity/ctime from unchanged size/mtime")
	}
	detail, err := store.GetLibraryEditing(ctx, library.ID)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Dir(replacement)}
	moved, err := store.UpdateLibraryAsAdministrator(ctx, actor, identity.AdministratorNative, library.ID, LibraryUpdate{
		Revision: detail.Library.Revision, Paths: &paths,
		PathReplacements: []LibraryPathReplacement{{From: filepath.Dir(original), To: paths[0]}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(moved.RegisteredPaths) != 1 || moved.RegisteredPaths[0].ID != detail.RegisteredPaths[0].ID {
		t.Fatal("replacement changed the registered root identity")
	}
	file, _, err := store.OpenMedia(ctx, userID, item.ID, oldSource.SourceID)
	if file != nil {
		_ = file.Close()
		t.Fatal("replacement exposed a file through the old probe snapshot")
	}
	if !errors.Is(err, ErrSourceChanged) || !errors.Is(err, ErrUnavailable) || calls.Load() != 1 {
		t.Fatalf("old source facts were reused or silently reprobed before scanning: %v", err)
	}
	refreshed := libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
	if refreshed.ForceProbe || refreshed.Added != 0 || refreshed.Updated != 1 || calls.Load() != 2 {
		t.Fatalf("ordinary rescan did not refresh the changed source in place: %+v calls=%d", refreshed, calls.Load())
	}
	updated := nfoCatalogItem(t, ctx, store, userID, library.ID, replacement)
	if updated.ID != item.ID || updated.Media == nil || updated.Media.Streams[0].Codec != "hevc" ||
		updated.Media.Streams[0].PixelFormat != "yuv420p10le" || len(updated.Media.VideoSeekIndexes) != 0 ||
		updated.Media.FileChangeTimeNs != media.FileChangeTime(replacementStat) {
		t.Fatal("rescan retained old codec/index facts or replaced the item identity")
	}
	file, source, err := store.OpenMedia(ctx, userID, item.ID, oldSource.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(content) != replacementBytes || source.ETag == oldSource.ETag || source.SourceID != oldSource.SourceID {
		t.Fatalf("new source did not receive a distinct validator and actual replacement bytes: %v %v", readErr, closeErr)
	}
	currentMetadata := metadataEditTestDetail(t, ctx, store, actor, item.ID)
	if !reflect.DeepEqual(currentMetadata.Overrides, metadata.Overrides) || !reflect.DeepEqual(currentMetadata.LockedValues, metadata.LockedValues) ||
		forceProbeUserDataSnapshot(t, ctx, pool, item.ID) != userState {
		t.Fatal("source refresh changed administrator controls or user playback state")
	}
	if cached := libraryIntegrationScan(t, ctx, store, library.ID, "Completed"); cached.Added != 0 || cached.Updated != 0 || calls.Load() != 2 {
		t.Fatal("refreshed path/source key failed to form a stable probe cache")
	}
}
