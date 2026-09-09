//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

const subtitleTestSRT = "1\n00:00:01,000 --> 00:00:02,000\nHello subtitle\n"
const subtitleTestVTT = "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nHello subtitle\n"

func subtitleTestCatalog(t *testing.T) (mediaSourceFixture, Subtitle, string) {
	t.Helper()
	fixture := mediaSourceTestCatalog(t, nil)
	path := filepath.Join(filepath.Dir(fixture.path), "Feature.en.default.srt")
	if err := os.WriteFile(path, []byte(subtitleTestSRT), 0600); err != nil {
		t.Fatal(err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	tracks := subtitleTestTracks(t, fixture)
	if len(tracks) != 1 {
		t.Fatalf("fixture did not catalog one subtitle: %+v", tracks)
	}
	return fixture, tracks[0], path
}

func subtitleTestTracks(t *testing.T, fixture mediaSourceFixture) []Subtitle {
	t.Helper()
	item, err := fixture.store.GetItem(fixture.ctx, fixture.userID, fixture.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item.Subtitles
}

func TestReadSubtitleEnforcesCurrentPoliciesAndCatalogSelectors(t *testing.T) {
	fixture, track, _ := subtitleTestCatalog(t)
	ctx, pool, store := fixture.ctx, fixture.pool, fixture.store
	libraryIntegrationUser(t, ctx, pool, "subtitle-reader", false, false, []string{fixture.library.ID})
	libraryIntegrationUser(t, ctx, pool, "subtitle-hidden", false, false, nil)
	libraryIntegrationUser(t, ctx, pool, "subtitle-disabled", true, true, nil)
	for _, test := range []struct {
		user, item, source string
		index              int
		want               error
	}{
		{"subtitle-reader", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index, nil},
		{"subtitle-hidden", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index, ErrNotFound},
		{"subtitle-disabled", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index, ErrForbidden},
		{"missing-user", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index, ErrForbidden},
		{"subtitle-reader", "missing-item", media.SourceID(fixture.item.ID), track.Index, ErrNotFound},
		{"subtitle-reader", fixture.item.ID, "https://example.invalid/track.srt", track.Index, ErrNotFound},
		{"subtitle-reader", fixture.item.ID, filepath.Dir(fixture.path), track.Index, ErrNotFound},
		{"subtitle-reader", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index + 1, ErrNotFound},
		{"subtitle-reader", fixture.item.ID, media.SourceID(fixture.item.ID), 0, ErrNotFound},
	} {
		content, err := store.ReadSubtitle(ctx, test.user, test.item, test.source, test.index)
		if test.want == nil {
			if err != nil || string(content.Data) != subtitleTestSRT || !reflect.DeepEqual(content.Info, track) {
				t.Errorf("authorized subtitle read = %+v, %v", content, err)
			}
		} else if !errors.Is(err, test.want) || len(content.Data) != 0 {
			t.Errorf("ReadSubtitle(%q,%q,%q,%d) = %+v,%v; want %v", test.user, test.item, test.source, test.index, content, err, test.want)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id = 'subtitle-reader'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadSubtitle(ctx, "subtitle-reader", fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked library access remained readable: %v", err)
	}
	for _, administrator := range []bool{false, true} {
		if _, err := pool.Exec(ctx, `UPDATE users SET is_administrator = $2,
			policy = '{"EnableAllFolders":true,"EnableMediaPlayback":false}'::jsonb WHERE id = $1`, fixture.userID, administrator); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReadSubtitle(ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrForbidden) {
			t.Errorf("administrator=%t bypassed false playback policy: %v", administrator, err)
		}
	}
}

func TestReadSubtitleChecksSnapshotsWithoutReadingPrimaryContents(t *testing.T) {
	fixture, track, path := subtitleTestCatalog(t)
	watcher, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(watcher)
	if _, err := syscall.InotifyAddWatch(watcher, fixture.path, syscall.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	content, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index)
	if err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	wantModifiedAt := catalogModifiedTime(sourceInfo)
	if changedAt := time.Unix(0, media.FileChangeTime(sourceInfo)).UTC(); changedAt.After(wantModifiedAt) {
		wantModifiedAt = changedAt
	}
	if !content.ModifiedAt.Equal(wantModifiedAt) || !content.Info.ModifiedAt.Equal(catalogModifiedTime(sourceInfo)) {
		t.Fatalf("HTTP modification time did not include ctime while preserving indexed mtime: effective=%s indexed=%s expected=%s",
			content.ModifiedAt, content.Info.ModifiedAt, wantModifiedAt)
	}
	var events [4096]byte
	if count, err := syscall.Read(watcher, events[:]); count > 0 || (err != nil && !errors.Is(err, syscall.EAGAIN)) {
		t.Fatalf("subtitle read accessed primary media contents: bytes=%d error=%v", count, err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := []byte(subtitleTestSRT)
	changed[len(changed)-2] = 'X'
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("same-size subtitle mutation with restored mtime remained readable: %v", err)
	}
	mutated, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Isolate the content hash guard by deliberately making the catalog's
	// metadata snapshot match the mutation while retaining the original hash.
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE item_subtitles SET change_time_ns = $3
		WHERE item_id = $1 AND stream_index = $2`, fixture.item.ID, track.Index, media.FileChangeTime(mutated)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("mismatched subtitle hash remained readable despite matching metadata: %v", err)
	}
	libraryIntegrationScan(t, fixture.ctx, fixture.store, fixture.library.ID, "Completed")
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); err != nil {
		t.Fatalf("valid rescanned subtitle did not become readable: %v", err)
	}
	primaryBefore, err := os.Stat(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.path, []byte(fixture.contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fixture.path, primaryBefore.ModTime(), primaryBefore.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ReadSubtitle(fixture.ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("changed primary media snapshot remained readable through subtitle endpoint: %v", err)
	}
}

func TestReadSubtitleRejectsReplacedAndNonregularPaths(t *testing.T) {
	for _, kind := range []string{"leaf-symlink", "parent-symlink", "root-symlink", "replacement", "fifo", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			fixture, track, path := subtitleTestCatalog(t)
			switch kind {
			case "leaf-symlink", "parent-symlink", "root-symlink":
				if kind == "parent-symlink" {
					path = filepath.Dir(path)
				} else if kind == "root-symlink" {
					path = fixture.library.Paths[0]
				}
				original := path + ".original"
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, path); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(subtitleTestSRT), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.Truncate(path, 8<<20+1); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(fixture.ctx, 2*time.Second)
			defer cancel()
			if _, err := fixture.store.ReadSubtitle(ctx, fixture.userID, fixture.item.ID, media.SourceID(fixture.item.ID), track.Index); !errors.Is(err, ErrUnavailable) || ctx.Err() != nil {
				t.Fatalf("unsafe %s subtitle returned %v (context=%v)", kind, err, ctx.Err())
			}
		})
	}
}

func TestSubtitleWorkerCancellationRetainsAllBlockedSlots(t *testing.T) {
	slots := make(chan struct{}, 4)
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	finished := make(chan error, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	for index := 0; index < cap(slots); index++ {
		go func() {
			_, err := runSubtitleWorker(ctx, slots, func() (SubtitleContent, error) {
				started <- struct{}{}
				<-release
				return SubtitleContent{Data: []byte("late")}, nil
			})
			finished <- err
		}()
	}
	for index := 0; index < cap(slots); index++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("subtitle workers did not start")
		}
	}
	cancel()
	for index := 0; index < cap(slots); index++ {
		select {
		case err := <-finished:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled subtitle worker returned %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("subtitle caller did not observe cancellation")
		}
	}
	if len(slots) != cap(slots) {
		t.Fatalf("cancellation released slots before blocking work ended: %d", len(slots))
	}
	var invoked atomic.Bool
	next, nextCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer nextCancel()
	if _, err := runSubtitleWorker(next, slots, func() (SubtitleContent, error) {
		invoked.Store(true)
		return SubtitleContent{}, nil
	}); !errors.Is(err, context.DeadlineExceeded) || invoked.Load() {
		t.Fatalf("a fifth worker bypassed occupied slots: invoked=%t error=%v", invoked.Load(), err)
	}
	unblock()
	if !imageStoreWaitWorkerCleanup(slots) {
		t.Fatal("late subtitle work did not release its slots after cleanup")
	}
}
