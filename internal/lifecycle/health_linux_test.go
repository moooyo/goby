//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fileObservationContext struct {
	context.Context
	observe func()
}

func (ctx fileObservationContext) Err() error {
	ctx.observe()
	return ctx.Context.Err()
}

// fileIsOpen observes the real descriptor lifetime without changing the
// store's readers or relying on scheduler timing during short file reads.
func fileIsOpen(t *testing.T, path string) bool {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && target == path {
			return true
		}
	}
	return false
}

func TestLifecycleCurrentContextCancellationReleasesGate(t *testing.T) {
	store := openLifecycle(t, lifecycleDirectory(t))
	for _, deadline := range []bool{false, true} {
		name := "cancelled"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			<-store.gate
			defer store.leave()
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 10*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := store.CurrentContext(ctx)
				finished <- err
			}()
			if !deadline {
				cancel()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, want) {
					t.Fatalf("state gate wait returned %v, want %v", err, want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("state gate wait ignored its context")
			}
		})
	}
	if _, err := store.CurrentContext(context.Background()); err != nil {
		t.Fatalf("cancelled state reads blocked later work: %v", err)
	}
}

func TestLifecycleCurrentContextCancellationDuringVerification(t *testing.T) {
	for _, target := range []string{"historical generation", "inert temporary"} {
		t.Run(target, func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			stageLifecycle(t, store, generationOne, nil)
			stageLifecycle(t, store, generationTwo, nil)
			plan := planLifecycle(t, store, generationTwo, MasterDefault)
			if _, err := store.Activate(context.Background(), plan.ID); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, generationName(generationOne), configName)
			if target == "inert temporary" {
				path = filepath.Join(directory, ".next-"+generationOne)
				if err := os.WriteFile(path, []byte("unused"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := false
			readContext := fileObservationContext{Context: ctx, observe: func() {
				if fileIsOpen(t, path) {
					observed = true
					cancel()
				}
			}}
			if state, err := store.CurrentContext(readContext); !errors.Is(err, context.Canceled) || state != (State{}) || !observed {
				t.Fatalf("verification cancellation returned state=%+v error=%v observed=%v", state, err, observed)
			}
			if state, err := store.CurrentContext(context.Background()); err != nil || state != plan.After {
				t.Fatalf("cancelled verification damaged later state reads: %v", err)
			}
			pending, err := store.Pending(context.Background())
			if err != nil || pending == nil || pending.ID != plan.ID || pending.Status != PlanActivated {
				t.Fatalf("cancelled verification changed the publication journal: %v", err)
			}
		})
	}
}

func TestLifecycleInertTemporaryDoesNotReadPayload(t *testing.T) {
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	before, err := store.CurrentContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, ".next-"+generationOne)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxMetadataBytes); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	watch, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(watch)
	if _, err := unix.InotifyAddWatch(watch, path, unix.IN_ACCESS); err != nil {
		t.Fatal(err)
	}
	if state, err := store.CurrentContext(context.Background()); err != nil || state != before {
		t.Fatalf("safe inert temporary affected authority: %v", err)
	}
	var event [4096]byte
	if n, err := unix.Read(watch, event[:]); !errors.Is(err, unix.EAGAIN) || n > 0 {
		t.Fatalf("health check read inert payload: bytes=%d error=%v", n, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openLifecycle(t, directory)
	if state, err := store.CurrentContext(context.Background()); err != nil || state != before {
		t.Fatalf("reopen adopted inert debris: %v", err)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != maxMetadataBytes {
		t.Fatal("health check removed or changed inert debris")
	}
}

func TestLifecycleInertTemporaryRejectsUnsafeMetadata(t *testing.T) {
	for _, kind := range []string{"symlink", "fifo", "hardlink", "mode", "oversized", "foreign owner"} {
		t.Run(kind, func(t *testing.T) {
			directory := lifecycleDirectory(t)
			store := openLifecycle(t, directory)
			path := filepath.Join(directory, ".next-"+generationOne)
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink("/dev/null", path)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			default:
				err = os.WriteFile(path, []byte("unused"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "hardlink":
				err = os.Link(path, filepath.Join(t.TempDir(), "alias"))
			case "mode":
				err = os.Chmod(path, 0644)
			case "oversized":
				err = os.Truncate(path, maxMetadataBytes+1)
			case "foreign owner":
				if os.Geteuid() != 0 {
					t.Skip("changing file ownership requires root")
				}
				err = os.Chown(path, 1, -1)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.CurrentContext(context.Background()); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("accepted unsafe inert temporary: %v", err)
			}
		})
	}
}

func TestLifecycleInertTemporaryRejectsReplacementDuringInspection(t *testing.T) {
	directory := lifecycleDirectory(t)
	store := openLifecycle(t, directory)
	path := filepath.Join(directory, ".next-"+generationOne)
	payload := []byte("unused")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	replaced := false
	ctx := fileObservationContext{Context: context.Background(), observe: func() {
		if replaced || !fileIsOpen(t, path) {
			return
		}
		replaced = true
		if err := os.Rename(path, filepath.Join(t.TempDir(), "original")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := store.CurrentContext(ctx); !errors.Is(err, ErrUnavailable) || !replaced {
		t.Fatalf("inspection accepted a replaced inode: %v", err)
	}
}
