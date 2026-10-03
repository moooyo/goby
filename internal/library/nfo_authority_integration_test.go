//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestLocalNFOObservationRejectsSameInodeRewriteWithRestoredModifiedTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Film.nfo")
	if err := os.WriteFile(path, []byte(`<movie><title>First</title></movie>`), 0o600); err != nil {
		t.Fatal(err)
	}
	opened, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	before, err := opened.Stat()
	if err != nil {
		t.Fatal(err)
	}
	// Advance the same filesystem's observed clock without changing the NFO.
	// The single rewrite below must cross a real ctime tick even when timestamp
	// granularity is coarser than the read and rewrite operations.
	clockPath := filepath.Join(filepath.Dir(path), "nfo-clock-marker")
	clockContext, cancelClock := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelClock()
	clockTick := time.NewTicker(time.Millisecond)
	defer clockTick.Stop()
	for {
		if err := clockContext.Err(); err != nil {
			t.Fatalf("filesystem clock did not advance beyond NFO ctime %d: %v", media.FileChangeTime(before), err)
		}
		if err := os.WriteFile(clockPath, []byte("clock"), 0o600); err != nil {
			t.Fatal(err)
		}
		marker, err := os.Stat(clockPath)
		if err != nil {
			t.Fatal(err)
		}
		if media.FileChangeTime(marker) > media.FileChangeTime(before) {
			break
		}
		select {
		case <-clockContext.Done():
			t.Fatalf("filesystem clock did not advance beyond NFO ctime %d: %v", media.FileChangeTime(before), clockContext.Err())
		case <-clockTick.C:
		}
	}
	file := &localNFOFaultFile{File: opened}
	var once sync.Once
	var mutationErr error
	file.afterRead = func() {
		once.Do(func() {
			mutationErr = os.WriteFile(path, []byte(`<movie><title>Other</title></movie>`), 0o600)
			if mutationErr == nil {
				mutationErr = os.Chtimes(path, before.ModTime(), before.ModTime())
			}
		})
	}
	data, invalid, err := readOpenedLocalNFO(context.Background(), file, before, func() (os.FileInfo, error) { return os.Stat(path) })
	after, statErr := os.Stat(path)
	if mutationErr != nil || statErr != nil || !os.SameFile(before, after) || before.Size() != after.Size() ||
		!before.ModTime().Equal(after.ModTime()) || media.FileChangeTime(before) == media.FileChangeTime(after) {
		t.Fatalf("NFO rewrite fixture did not isolate change time: mutation=%v stat=%v before=%+v after=%+v", mutationErr, statErr, before, after)
	}
	if err != nil || !invalid || len(data) != 0 || file.closes != 1 {
		t.Fatalf("rewritten NFO was accepted or its descriptor was retained: bytes=%d invalid=%v close_calls=%d error=%v", len(data), invalid, file.closes, err)
	}
	if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("rejected NFO rewrite retained its descriptor: %v", err)
	}
}

func TestLocalNFOFolderQueuedReadRequiresFreshAuthority(t *testing.T) {
	for _, change := range []string{"task_cancel", "binding_revision"} {
		t.Run(change, func(t *testing.T) {
			fixture := primaryScanReadFixtureAt(t, &primaryScanReadTestProber{joined: true}, "")
			state := fixture.state
			path := filepath.Join(state.root.path, "tvshow.nfo")
			if err := os.WriteFile(path, []byte(`<tvshow><title>Must remain unread</title></tvshow>`), 0o600); err != nil {
				t.Fatal(err)
			}
			directory, err := state.opened.Lstat(".")
			if err != nil {
				t.Fatal(err)
			}
			state.directoryIdentities = map[string]os.FileInfo{".": directory}
			operation := primaryScanRoutingRetainWalk(t, state)
			assertUnread := primaryScanRoutingWatchSource(t, path)
			baseline := originalMediaReadGovernor.Stats()
			owners := originalMediaReadOwners.Stats().RegisteredOwners
			if baseline.Active != 0 || baseline.Background != 0 || baseline.Queued != 0 {
				t.Fatalf("NFO queue fixture needs idle admission: %+v", baseline)
			}
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			blockers := make([]*primarySidecarRetryBlocker, 0, 2)
			for range 2 {
				blockerIO, err := state.store.preparePrimaryRootIO(fixture.ctx,
					[]mediaSourceRootHint{{root: state.walkRow.root, bindingRevision: state.walkRow.revision}})
				if err != nil {
					t.Fatal(err)
				}
				blocker := &primarySidecarRetryBlocker{operation: blockerIO, rootID: state.root.id,
					entered: make(chan struct{}), done: make(chan struct{})}
				blockers = append(blockers, blocker)
				t.Cleanup(func() {
					unblock()
					if blocker.started.Load() {
						mediaSourceAdmissionTestWait(t, blocker.done, "queued folder NFO blocker cleanup")
					}
					if err := blockerIO.Close(); err != nil {
						t.Errorf("close folder NFO blocker: %v", err)
					}
				})
				blocker.start(fixture.ctx, release)
				primaryScanReadSignal(t, fixture.ctx, blocker.entered, "folder NFO background blocker")
			}
			result := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				state.task.cancel()
				unblock()
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				primaryScanReadSignal(t, cleanup, finished, "queued folder NFO reader cleanup")
			})
			go func() {
				defer close(finished)
				_, err := state.folderLocalMetadata("//series/root", ".", "Series", 0)
				result <- err
			}()
			wait, cancel := context.WithTimeout(fixture.ctx, 5*time.Second)
			defer cancel()
			primaryReadTestWaitQueued(t, wait, baseline.Queued+1)
			assertUnread()
			if scanProbeOpenDescriptors(t, []string{path})[0] != 0 || state.warnings != 0 {
				t.Fatal("queued folder metadata opened NFO bytes or mutated scanner warnings")
			}
			want := error(context.Canceled)
			if change == "task_cancel" {
				_, err = fixture.pool.Exec(fixture.ctx, "UPDATE scan_jobs SET cancel_requested=true WHERE id=$1", state.task.job.ID)
			} else {
				want = ErrRootBindingConflict
				_, err = fixture.pool.Exec(fixture.ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE id=$1", state.root.id)
			}
			if err != nil {
				t.Fatal(err)
			}
			unblock()
			primaryScanReadSignal(t, fixture.ctx, finished, "folder NFO fresh authority rejection")
			if err := <-result; !errors.Is(err, want) {
				t.Fatalf("queued folder NFO reused stale authority: got=%v want=%v", err, want)
			}
			assertUnread()
			if scanProbeOpenDescriptors(t, []string{path})[0] != 0 || state.warnings != 0 {
				t.Fatal("rejected folder NFO read changed resources or warnings")
			}
			for _, blocker := range blockers {
				primaryScanReadSignal(t, fixture.ctx, blocker.done, "folder NFO blocker retirement")
				if blocker.err != nil {
					t.Fatal(blocker.err)
				}
				if err := blocker.operation.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if stats := originalMediaReadGovernor.Stats(); stats != baseline || originalMediaReadOwners.Stats().RegisteredOwners != owners {
				t.Fatalf("rejected NFO read retained admission charges: before=%+v after=%+v owners=%+v", baseline, stats, originalMediaReadOwners.Stats())
			}
			if err := operation.Close(); err != nil {
				t.Fatal(err)
			}
			state.walkIO = nil
		})
	}
}
