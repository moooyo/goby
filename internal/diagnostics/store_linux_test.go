//go:build linux

package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

func testStore(t *testing.T, config Config) *Store {
	t.Helper()
	if config.Directory == "" {
		config.Directory = t.TempDir()
	}
	if err := os.Chmod(config.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	store, err := Open(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func appendTestRecord(t *testing.T, store *Store, value string) []byte {
	t.Helper()
	line, err := json.Marshal(map[string]string{"message": value})
	if err != nil {
		t.Fatal(err)
	}
	line = append(line, '\n')
	if err := store.appendRecord(context.Background(), line); err != nil {
		t.Fatal(err)
	}
	return line
}

func firstTestFile(t *testing.T, store *Store) File {
	t.Helper()
	page, err := store.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("list files: count=%d error=%v", len(page.Items), err)
	}
	return page.Items[0]
}

func TestStoreRotationRetentionAndRestart(t *testing.T) {
	config := Config{Directory: t.TempDir(), MaxFileBytes: MaxRecordBytes, MaxFiles: 3}
	store := testStore(t, config)
	for index := 0; index < 7; index++ {
		appendTestRecord(t, store, strings.Repeat("x", 5000))
	}
	page, err := store.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || page.TotalRecordCount != 3 {
		t.Fatalf("rotation retention: %+v %v", page, err)
	}
	for _, item := range page.Items {
		if item.Size <= 0 || item.Size > MaxRecordBytes || item.DateCreated.IsZero() || item.DateModified.IsZero() {
			t.Fatalf("invalid retained file metadata: %+v", item)
		}
		info, err := os.Lstat(filepath.Join(config.Directory, item.Name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe log mode: %v %v", info, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testStore(t, config)
	page, err = reopened.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || page.TotalRecordCount != 3 {
		t.Fatalf("restart retention: %+v %v", page, err)
	}
	appendTestRecord(t, reopened, "after restart")
}

func TestStoreDayBoundaryAndAgeRetention(t *testing.T) {
	store := testStore(t, Config{RetentionDays: 2})
	appendTestRecord(t, store, "old")
	old := firstTestFile(t, store)
	base := store.now().UTC()
	store.now = func() time.Time { return base.Add(25 * time.Hour) }
	appendTestRecord(t, store, "next day")
	page, err := store.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || page.TotalRecordCount != 2 {
		t.Fatalf("day rotation: %+v %v", page, err)
	}
	store.now = func() time.Time { return base.Add(74 * time.Hour) }
	appendTestRecord(t, store, "later")
	if _, err := os.Lstat(filepath.Join(store.cfg.Directory, old.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired closed file remains: %v", err)
	}
	page, err = store.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || page.TotalRecordCount != 1 || page.Items[0].Size == 0 {
		t.Fatalf("age retention: %+v %v", page, err)
	}
}

func TestStoreRejectsForeignDirectoriesAndUnsafeAliases(t *testing.T) {
	t.Run("foreign directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "foreign.log"), []byte("private"), 0600); err != nil {
			t.Fatal(err)
		}
		if store, err := Open(Config{Directory: dir}); !errors.Is(err, ErrUnavailable) {
			if store != nil {
				store.Close()
			}
			t.Fatalf("foreign directory accepted: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(dir, "foreign.log")); err != nil || string(data) != "private" {
			t.Fatalf("foreign file changed: %q %v", data, err)
		}
	})
	t.Run("symlink directory", func(t *testing.T) {
		parent := t.TempDir()
		real := filepath.Join(parent, "real")
		if err := os.Mkdir(real, 0700); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(parent, "alias")
		if err := os.Symlink(real, alias); err != nil {
			t.Fatal(err)
		}
		if store, err := Open(Config{Directory: alias}); !errors.Is(err, ErrUnavailable) {
			if store != nil {
				store.Close()
			}
			t.Fatalf("symlink directory accepted: %v", err)
		}
	})
	t.Run("noncanonical directory", func(t *testing.T) {
		if _, err := Open(Config{Directory: t.TempDir() + "/../alias"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("noncanonical path accepted: %v", err)
		}
	})
	t.Run("shared directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Chmod(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(Config{Directory: dir}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("shared directory accepted: %v", err)
		}
	})
}

func TestStoreIgnoresUnregisteredFiles(t *testing.T) {
	store := testStore(t, Config{})
	appendTestRecord(t, store, "owned")
	foreign := "goby-" + store.registry.Token + "-" + strings.Repeat("a", 32) + ".jsonl"
	if err := os.WriteFile(filepath.Join(store.cfg.Directory, foreign), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := store.List(context.Background(), ListOptions{Limit: 200})
	if err != nil || page.TotalRecordCount != 1 {
		t.Fatalf("foreign file imported: %+v %v", page, err)
	}
	if _, err := store.Snapshot(context.Background(), foreign); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign snapshot accepted: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := testStore(t, store.cfg)
	if _, err := reopened.Snapshot(context.Background(), foreign); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign file imported on restart: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(store.cfg.Directory, foreign)); err != nil || string(data) != "private" {
		t.Fatalf("foreign file removed or changed: %q %v", data, err)
	}
}

func TestStoreRejectsReplacementAndSpecialFiles(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "hardlink", "fifo", "directory"} {
		t.Run(kind, func(t *testing.T) {
			store := testStore(t, Config{})
			appendTestRecord(t, store, "owned")
			item := firstTestFile(t, store)
			path := filepath.Join(store.cfg.Directory, item.Name)
			private := filepath.Join(t.TempDir(), "private")
			if err := os.WriteFile(private, []byte("secret"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "regular":
				err = os.WriteFile(path, []byte("secret"), 0600)
			case "symlink":
				err = os.Symlink(private, path)
			case "hardlink":
				err = os.Link(private, path)
			case "fifo":
				err = syscall.Mkfifo(path, 0600)
			case "directory":
				err = os.Mkdir(path, 0700)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Snapshot(context.Background(), item.Name); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("replacement snapshot accepted: %v", err)
			}
			if _, err := store.List(context.Background(), ListOptions{Limit: 200}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("degraded list did not fail: %v", err)
			}
			if !store.Status().Degraded {
				t.Fatal("unsafe replacement did not degrade the store")
			}
			if data, err := os.ReadFile(private); err != nil || string(data) != "secret" {
				t.Fatalf("unowned target changed: %q %v", data, err)
			}
		})
	}
}

func TestStoreRejectsHardlinkAddedToActiveFile(t *testing.T) {
	store := testStore(t, Config{})
	appendTestRecord(t, store, "owned")
	item := firstTestFile(t, store)
	if err := os.Link(filepath.Join(store.cfg.Directory, item.Name), filepath.Join(t.TempDir(), "alias")); err != nil {
		t.Fatal(err)
	}
	if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("hardlinked active file accepted a write: %v", err)
	}
}

func TestStoreProcessLockAndReplacedLock(t *testing.T) {
	store := testStore(t, Config{})
	if _, err := Open(store.cfg); !errors.Is(err, ErrBusy) {
		t.Fatalf("second process lock accepted: %v", err)
	}
	path := filepath.Join(store.cfg.Directory, lockName)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("replaced lock accepted: %v", err)
	}
}

func TestSnapshotPinsFixedPrefixAcrossAppendAndRotation(t *testing.T) {
	store := testStore(t, Config{MaxFileBytes: MaxRecordBytes, MaxFiles: 1})
	first := appendTestRecord(t, store, strings.Repeat("x", 5000))
	item := firstTestFile(t, store)
	snapshot, err := store.Snapshot(context.Background(), item.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	appendTestRecord(t, store, strings.Repeat("y", 5000))
	if _, err := os.Lstat(filepath.Join(store.cfg.Directory, item.Name)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closed file was not unlinked: %v", err)
	}
	data, err := io.ReadAll(snapshot)
	if err != nil || !bytes.Equal(data, first) || snapshot.Size() != int64(len(first)) {
		t.Fatalf("snapshot changed after rotation: size=%d error=%v", len(data), err)
	}
	if _, err := snapshot.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err = io.ReadAll(snapshot)
	if err != nil || !bytes.Equal(data, first) {
		t.Fatalf("snapshot seek changed bytes: %v", err)
	}
	if _, err := snapshot.Seek(1, io.SeekEnd); !errors.Is(err, ErrInvalid) {
		t.Fatalf("snapshot seek exceeded fixed limit: %v", err)
	}
}

func TestSnapshotDoesNotIncludeLaterActiveWrites(t *testing.T) {
	store := testStore(t, Config{})
	first := appendTestRecord(t, store, "first")
	item := firstTestFile(t, store)
	snapshot, err := store.Snapshot(context.Background(), item.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	appendTestRecord(t, store, "later")
	data, err := io.ReadAll(snapshot)
	if err != nil || !bytes.Equal(data, first) {
		t.Fatalf("snapshot included appended data: %q %v", data, err)
	}
}

func TestSnapshotReaderLimitCancellationAndStoreClosure(t *testing.T) {
	store := testStore(t, Config{})
	appendTestRecord(t, store, "first")
	item := firstTestFile(t, store)
	readers := make([]*Snapshot, 0, MaxReaders)
	for index := 0; index < MaxReaders; index++ {
		reader, err := store.Snapshot(context.Background(), item.Name)
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, reader)
	}
	if _, err := store.Snapshot(context.Background(), item.Name); !errors.Is(err, ErrBusy) {
		t.Fatalf("reader limit ignored: %v", err)
	}
	if err := readers[0].Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader, err := store.Snapshot(ctx, item.Name)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("reader cancellation ignored: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, reader := range readers {
		if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("reader survived store close: %v", err)
		}
	}
	if _, err := store.List(context.Background(), ListOptions{Limit: 1}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed store returned a list: %v", err)
	}
}

func TestSnapshotCloseWaitsForCancellationReaderSlotRelease(t *testing.T) {
	store := testStore(t, Config{})
	item := firstTestFile(t, store)
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		readers := make([]*Snapshot, 0, MaxReaders)
		for index := 0; index < MaxReaders; index++ {
			readerContext := context.Background()
			if index == 0 {
				readerContext = ctx
			}
			reader, err := store.Snapshot(readerContext, item.Name)
			if err != nil {
				t.Fatal(err)
			}
			readers = append(readers, reader)
			defer reader.Close()
		}
		reader := readers[0]
		releaseEntered := make(chan struct{})
		allowRelease := make(chan struct{})
		var allowOnce sync.Once
		allow := func() { allowOnce.Do(func() { close(allowRelease) }) }
		defer allow()
		originalRelease := reader.release
		reader.release = func(snapshot *Snapshot) {
			close(releaseEntered)
			<-allowRelease
			originalRelease(snapshot)
		}
		cancel()
		<-releaseEntered
		manualClose := make(chan error, 1)
		go func() { manualClose <- reader.Close() }()
		synctest.Wait()
		select {
		case err := <-manualClose:
			t.Fatalf("Close returned before cancellation released its reader slot: %v", err)
		default:
		}
		if _, err := store.Snapshot(context.Background(), item.Name); !errors.Is(err, ErrBusy) {
			t.Fatalf("blocked release unexpectedly freed the reader slot: %v", err)
		}
		allow()
		if err := <-manualClose; err != nil {
			t.Fatal(err)
		}
		// Do not wait separately for the cancellation callback. Close itself
		// guarantees that its slot can be reused immediately after it returns.
		replacement, err := store.Snapshot(context.Background(), item.Name)
		if err != nil {
			t.Fatalf("reader slot was not immediately reusable after Close: %v", err)
		}
		if err := replacement.Close(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStoreCloseWaitsForConcurrentSnapshotRelease(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		store, err := Open(Config{Directory: directory})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		item := firstTestFile(t, store)
		reader, err := store.Snapshot(context.Background(), item.Name)
		if err != nil {
			t.Fatal(err)
		}
		releaseEntered := make(chan struct{})
		allowRelease := make(chan struct{})
		var allowOnce sync.Once
		allow := func() { allowOnce.Do(func() { close(allowRelease) }) }
		defer allow()
		originalRelease := reader.release
		reader.release = func(snapshot *Snapshot) {
			close(releaseEntered)
			<-allowRelease
			originalRelease(snapshot)
		}
		readerClose := make(chan error, 1)
		go func() { readerClose <- reader.Close() }()
		<-releaseEntered
		storeClose := make(chan error, 1)
		go func() { storeClose <- store.Close() }()
		synctest.Wait()
		select {
		case err := <-storeClose:
			t.Fatalf("store Close returned before snapshot release completed: %v", err)
		default:
		}
		allow()
		if err := <-storeClose; err != nil {
			t.Fatal(err)
		}
		if err := <-readerClose; err != nil {
			t.Fatal(err)
		}
	})
}

func TestSnapshotRepeatedClosePreservesFailure(t *testing.T) {
	store := testStore(t, Config{})
	item := firstTestFile(t, store)
	reader, err := store.Snapshot(context.Background(), item.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.file.Close(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := reader.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("Close did not preserve its original failure: %v", err)
		}
	}
	store.mu.Lock()
	remainingReaders := len(store.readers)
	store.mu.Unlock()
	if remainingReaders != 0 {
		t.Fatal("failed Close retained its reader slot")
	}
}

func TestStoreLinesPaginationAndInputBounds(t *testing.T) {
	store := testStore(t, Config{})
	for index := 0; index < 7; index++ {
		appendTestRecord(t, store, "multibyte \u4e2d\u6587 and escaped\nnewline")
	}
	item := firstTestFile(t, store)
	page, err := store.Lines(context.Background(), item.Name, LinesOptions{StartIndex: 2, Limit: 3})
	if err != nil || len(page.Items) != 3 || page.TotalRecordCount != 7 || page.NextIndex != 5 || page.SnapshotSize != item.Size {
		t.Fatalf("line page: %+v %v", page, err)
	}
	for _, line := range page.Items {
		if !json.Valid([]byte(line)) || strings.Contains(line, "\n") {
			t.Fatalf("invalid physical JSONL line: %q", line)
		}
	}
	for _, options := range []LinesOptions{{StartIndex: -1, Limit: 1}, {Limit: 0}, {Limit: 501}} {
		if _, err := store.Lines(context.Background(), item.Name, options); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid line bounds accepted: %+v %v", options, err)
		}
	}
	for _, name := range []string{"../private", "/etc/passwd", "..", "a\\b", "x\x00y"} {
		if _, err := store.Snapshot(context.Background(), name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid snapshot name accepted: %q %v", name, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Lines(ctx, item.Name, LinesOptions{Limit: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("line cancellation ignored: %v", err)
	}
}

func TestStoreRejectsInvalidRecordWithoutDegrading(t *testing.T) {
	store := testStore(t, Config{})
	for _, line := range [][]byte{nil, []byte("{}"), []byte("{}\n{}\n"), []byte("invalid\n"), []byte(strings.Repeat("x", MaxRecordBytes) + "\n")} {
		if err := store.appendRecord(context.Background(), line); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid record accepted: %v", err)
		}
	}
	if !store.Status().Healthy {
		t.Fatal("invalid caller record degraded the disk store")
	}
}

func TestStorePartialWriteAndDiskReserveFailure(t *testing.T) {
	t.Run("partial write", func(t *testing.T) {
		store := testStore(t, Config{})
		first := appendTestRecord(t, store, "first")
		item := firstTestFile(t, store)
		store.write = func(file *os.File, data []byte) (int, error) {
			n, _ := file.Write(data[:len(data)/2])
			return n, syscall.ENOSPC
		}
		if err := store.appendRecord(context.Background(), []byte("{\"message\":\"later\"}\n")); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("partial disk write did not fail: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(store.cfg.Directory, item.Name))
		if err != nil || !bytes.Equal(data, first) || !store.Status().Degraded {
			t.Fatalf("partial record was retained: %q %v", data, err)
		}
		if _, err := store.Lines(context.Background(), item.Name, LinesOptions{Limit: 1}); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("degraded lines returned success: %v", err)
		}
	})
	t.Run("disk reserve", func(t *testing.T) {
		store := testStore(t, Config{})
		store.freeBytes = func(int) (uint64, error) { return uint64(store.cfg.MinFreeBytes), nil }
		if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) || !store.Status().Degraded {
			t.Fatalf("disk reserve did not degrade writes: %v", err)
		}
	})
	t.Run("flush failure", func(t *testing.T) {
		store := testStore(t, Config{})
		store.syncFile = func(*os.File) error { return syscall.EIO }
		if err := store.appendRecord(context.Background(), []byte("{}\n")); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("flush failure ignored: %v", err)
		}
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("failed flush shutdown reported success: %v", err)
		}
	})
}

func TestStoreRecoversIncompleteActiveRecord(t *testing.T) {
	store := testStore(t, Config{})
	first := appendTestRecord(t, store, "complete")
	item := firstTestFile(t, store)
	if _, err := store.active.Write([]byte("{\"message\":\"incomplete")); err != nil {
		t.Fatal(err)
	}
	if err := store.active.Sync(); err != nil {
		t.Fatal(err)
	}
	// Close descriptors without publishing a clean shutdown, as after a crash.
	if err := store.closeDescriptors(); err != nil {
		t.Fatal(err)
	}
	store.closed = true
	reopened := testStore(t, store.cfg)
	snapshot, err := reopened.Snapshot(context.Background(), item.Name)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	data, err := io.ReadAll(snapshot)
	if err != nil || !bytes.Equal(data, first) {
		t.Fatalf("incomplete record recovery: %q %v", data, err)
	}
}

func TestStoreRecoversPublishedRegistryAfterDirectorySyncFailure(t *testing.T) {
	store := testStore(t, Config{MaxFileBytes: MaxRecordBytes})
	appendTestRecord(t, store, strings.Repeat("x", 5000))
	if err := store.finishActiveLocked(); err != nil {
		t.Fatal(err)
	}
	store.syncFile = func(file *os.File) error {
		if file == store.directory {
			return syscall.EIO
		}
		return file.Sync()
	}
	if err := store.createActiveLocked(store.now().UTC()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("directory sync injection did not fail: %v", err)
	}
	if err := store.closeDescriptors(); err != nil {
		t.Fatal(err)
	}
	store.closed = true
	reopened := testStore(t, store.cfg)
	appendTestRecord(t, reopened, "recovered")
}

func TestStoreRecoversInterruptedRetentionDeletion(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(map[bool]string{false: "before unlink", true: "after unlink"}[removed], func(t *testing.T) {
			store := testStore(t, Config{})
			appendTestRecord(t, store, "old")
			if err := store.finishActiveLocked(); err != nil {
				t.Fatal(err)
			}
			store.registry.Files[0].Deleting = true
			if err := store.persistRegistryLocked(); err != nil {
				t.Fatal(err)
			}
			item := store.registry.Files[0]
			if removed {
				if err := os.Remove(filepath.Join(store.cfg.Directory, item.Name)); err != nil {
					t.Fatal(err)
				}
			}
			if err := store.closeDescriptors(); err != nil {
				t.Fatal(err)
			}
			store.closed = true
			reopened := testStore(t, store.cfg)
			if _, err := reopened.Snapshot(context.Background(), item.Name); !errors.Is(err, ErrNotFound) {
				t.Fatalf("pending deletion not recovered: %v", err)
			}
		})
	}
}

func TestStoreRecoversInitialMarkerPublicationLink(t *testing.T) {
	store := testStore(t, Config{})
	item := store.registry.Files[0]
	if err := store.active.Close(); err != nil {
		t.Fatal(err)
	}
	store.active = nil
	if err := store.unlinkIdentity(item.Name, item.ID); err != nil {
		t.Fatal(err)
	}
	store.registry.Files = []entry{}
	if err := store.persistRegistryLocked(); err != nil {
		t.Fatal(err)
	}
	temporary := ".goby-" + store.registry.Token + "-" + strings.Repeat("a", 16) + ".tmp"
	if err := os.Link(filepath.Join(store.cfg.Directory, markerName), filepath.Join(store.cfg.Directory, temporary)); err != nil {
		t.Fatal(err)
	}
	if err := store.closeDescriptors(); err != nil {
		t.Fatal(err)
	}
	store.closed = true
	reopened := testStore(t, store.cfg)
	appendTestRecord(t, reopened, "recovered initial publication")
	if _, err := os.Lstat(filepath.Join(store.cfg.Directory, temporary)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("initial publication temporary link remains: %v", err)
	}
}

func TestStoreRejectsExternalMarkerHardlink(t *testing.T) {
	store := testStore(t, Config{})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "marker-link")
	if err := os.Link(filepath.Join(store.cfg.Directory, markerName), external); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(store.cfg); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("external marker hard link accepted: %v", err)
	}
	if _, err := os.Lstat(external); err != nil {
		t.Fatalf("external hard link was changed: %v", err)
	}
}

func TestStoreConcurrentWritesAndClose(t *testing.T) {
	store := testStore(t, Config{})
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for count := 0; count < 16; count++ {
				if err := store.appendRecord(context.Background(), []byte("{}\n")); err != nil && !errors.Is(err, ErrUnavailable) {
					t.Errorf("concurrent write: %v", err)
				}
			}
		}()
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	workers.Wait()
	if !store.Status().Closed || store.Status().Healthy {
		t.Fatal("closed lifecycle state is inconsistent")
	}
}
