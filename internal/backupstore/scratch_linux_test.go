//go:build linux

package backupstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"

	"golang.org/x/sys/unix"
)

func newTestScratch(t *testing.T, store *Store, ctx context.Context, maximum int64) *Scratch {
	t.Helper()
	scratch, err := store.Scratch(ctx, maximum)
	if err != nil {
		t.Fatalf("Scratch(%d): %v", maximum, err)
	}
	if scratch.File() == nil {
		t.Fatal("Scratch returned a nil file")
	}
	return scratch
}

func requireClosedScratchFile(t *testing.T, scratch *Scratch, original *os.File) {
	t.Helper()
	if scratch.File() != original {
		t.Fatal("File returned a different Go file object after Close")
	}
	if _, err := scratch.File().Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Read on closed scratch: %v", err)
	}
	if _, err := scratch.File().Write([]byte("x")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write on closed scratch: %v", err)
	}
	if _, err := scratch.File().Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Stat on closed scratch: %v", err)
	}
}

func allocatedScratchBytes(t *testing.T, scratch *Scratch, maximum int64) int64 {
	t.Helper()
	data := bytes.Repeat([]byte("s"), 4096)
	if n, err := scratch.File().Write(data); err != nil || n != len(data) {
		t.Fatalf("allocate scratch bytes: n=%d error=%v", n, err)
	}
	if err := scratch.File().Sync(); err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(scratch.File().Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Blocks <= 0 || stat.Size != int64(len(data)) {
		t.Fatalf("scratch did not allocate real blocks: size=%d blocks=%d", stat.Size, stat.Blocks)
	}
	allocated := stat.Blocks * 512
	if allocated >= maximum {
		t.Fatalf("filesystem fixture must leave an unallocated remainder: allocated=%d maximum=%d", allocated, maximum)
	}
	return allocated
}

func TestScratchRejectsInvalidMaximumWithoutCreatingFiles(t *testing.T) {
	f := newStoreFixture(t)
	catalogPath := filepath.Join(f.config.Directory, catalogName)
	before, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, maximum := range []int64{-1, 0, f.config.MaxObjectBytes + 1} {
		scratch, err := f.store.Scratch(context.Background(), maximum)
		if scratch != nil {
			_ = scratch.Close()
			t.Fatalf("Scratch(%d) returned a handle for an invalid maximum", maximum)
		}
		requireError(t, err, ErrInvalid)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scratch, err := f.store.Scratch(ctx, 1)
	if scratch != nil {
		_ = scratch.Close()
		t.Fatal("Scratch returned a handle for a cancelled context")
	}
	requireError(t, err, context.Canceled)
	if got := f.store.Status(); !got.Healthy || got.Objects != 0 || got.Bytes != 0 || got.ScratchFiles != 0 || got.ScratchBytes != 0 {
		t.Fatalf("invalid requests changed reservations: %+v", got)
	}
	requireFileBytes(t, catalogPath, before)
	entries, err := os.ReadDir(f.config.Directory)
	if err != nil || len(entries) != 3 {
		t.Fatalf("invalid requests created files: entries=%v error=%v", entries, err)
	}
	for _, maximum := range []int64{1, f.config.MaxObjectBytes} {
		scratch := newTestScratch(t, f.store, context.Background(), maximum)
		if err := scratch.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScratchIsPrivateAnonymousAndCannotBeLinked(t *testing.T) {
	f := newStoreFixture(t)
	scratch := newTestScratch(t, f.store, context.Background(), 32)
	file := scratch.File()
	if scratch.File() != file {
		t.Fatal("File is not stable while open")
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&07777 != 0600 || stat.Nlink != 0 || stat.Uid != uint32(os.Geteuid()) {
		t.Fatalf("scratch file is not private and anonymous: mode=%#o links=%d uid=%d", stat.Mode, stat.Nlink, stat.Uid)
	}
	// Test both Linux linking routes. O_TMPFILE without O_EXCL would allow
	// publication through at least the procfs route for the owning service UID.
	for _, viaProc := range []bool{false, true} {
		name := "unexpected-scratch-link"
		if viaProc {
			name = "unexpected-scratch-proc-link"
		}
		path := filepath.Join(f.config.Directory, name)
		t.Cleanup(func() { _ = os.Remove(path) })
		var err error
		if viaProc {
			err = unix.Linkat(unix.AT_FDCWD, "/proc/self/fd/"+strconv.Itoa(int(file.Fd())), int(f.store.directory.Fd()), name, unix.AT_SYMLINK_FOLLOW)
		} else {
			err = unix.Linkat(int(file.Fd()), "", int(f.store.directory.Fd()), name, unix.AT_EMPTY_PATH)
		}
		if err == nil {
			t.Fatalf("anonymous scratch could be linked with viaProc=%v", viaProc)
		}
		requireMissing(t, path)
	}
	page, err := f.store.List(context.Background(), 0, 20)
	if err != nil || page.TotalRecordCount != 0 || len(page.Items) != 0 {
		t.Fatalf("scratch appeared in the durable catalog: %+v, %v", page, err)
	}
	if got := f.store.Status(); !got.Healthy || got.Objects != 0 || got.Bytes != 0 || got.ScratchFiles != 1 || got.ScratchBytes != 32 {
		t.Fatalf("scratch reservation status: %+v", got)
	}
	if err := scratch.Close(); err != nil {
		t.Fatal(err)
	}
	requireClosedScratchFile(t, scratch, file)
	if got := f.store.Status(); got.ScratchFiles != 0 || got.ScratchBytes != 0 {
		t.Fatalf("anonymous scratch reservation was not released: %+v", got)
	}
	crashed := newTestScratch(t, f.store, context.Background(), 32)
	crashedFile := crashed.File()
	if _, err := crashedFile.Write([]byte("unpublished plaintext")); err != nil {
		t.Fatal(err)
	}
	f.crash()
	requireClosedScratchFile(t, crashed, crashedFile)
	reopened := f.reopen()
	if got := reopened.Status(); !got.Healthy || got.Objects != 0 || got.Bytes != 0 || got.ScratchFiles != 0 || got.ScratchBytes != 0 {
		t.Fatalf("anonymous scratch survived crash recovery: %+v", got)
	}
	entries, err := os.ReadDir(f.config.Directory)
	if err != nil || len(entries) != 3 {
		t.Fatalf("anonymous scratch left a named file: entries=%v error=%v", entries, err)
	}
}

func TestScratchSlotsAreBoundedAndSeparateFromObjectCount(t *testing.T) {
	f := newStoreFixture(t, func(cfg *Config) { cfg.MaxObjects = 1 })
	scratches := make([]*Scratch, 0, MaxScratchFiles)
	for range MaxScratchFiles {
		scratches = append(scratches, newTestScratch(t, f.store, context.Background(), 1))
	}
	scratch, err := f.store.Scratch(context.Background(), 1)
	if scratch != nil {
		_ = scratch.Close()
		t.Fatal("Scratch exceeded its live file limit")
	}
	requireError(t, err, ErrBusy)
	w := beginTestWriter(t, f.store, context.Background())
	if got := f.store.Status(); got.Objects != 1 || got.Writers != 1 || got.ScratchFiles != MaxScratchFiles || got.ScratchBytes != int64(MaxScratchFiles) {
		t.Fatalf("scratch slots consumed object slots: %+v", got)
	}
	if err := scratches[0].Close(); err != nil {
		t.Fatal(err)
	}
	replacement := newTestScratch(t, f.store, context.Background(), 1)
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	for _, scratch := range scratches {
		if err := scratch.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := f.store.Status(); !got.Healthy || got.Objects != 1 || got.ScratchFiles != 0 || got.ScratchBytes != 0 {
		t.Fatalf("scratch slot accounting after Close: %+v", got)
	}
}

func TestScratchReservesFullMaximumAgainstSharedByteQuota(t *testing.T) {
	t.Run("catalog writer and scratch", func(t *testing.T) {
		f := newStoreFixture(t, func(cfg *Config) {
			cfg.MaxObjectBytes, cfg.MaxTotalBytes, cfg.MaxObjects = 16, 16, 1
		})
		scratch := newTestScratch(t, f.store, context.Background(), 8)
		w := beginTestWriter(t, f.store, context.Background())
		writeTestBytes(t, w, []byte("12345678"))
		if n, err := w.Write([]byte("9")); n != 0 || !errors.Is(err, ErrQuota) {
			t.Fatalf("Write exceeded the shared scratch quota: n=%d error=%v", n, err)
		}
		_, err := f.store.Scratch(context.Background(), 1)
		requireError(t, err, ErrQuota)
		if got := f.store.Status(); !got.Healthy || got.Objects != 1 || got.Bytes != 8 || got.ScratchFiles != 1 || got.ScratchBytes != 8 {
			t.Fatalf("shared byte accounting: %+v", got)
		}
		if err := scratch.Close(); err != nil {
			t.Fatal(err)
		}
		replacement := newTestScratch(t, f.store, context.Background(), 8)
		if err := replacement.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("multiple unwritten scratch files", func(t *testing.T) {
		f := newStoreFixture(t, func(cfg *Config) { cfg.MaxObjectBytes, cfg.MaxTotalBytes = 16, 16 })
		first := newTestScratch(t, f.store, context.Background(), 10)
		_, err := f.store.Scratch(context.Background(), 7)
		requireError(t, err, ErrQuota)
		second := newTestScratch(t, f.store, context.Background(), 6)
		if got := f.store.Status(); got.Objects != 0 || got.Bytes != 0 || got.ScratchFiles != 2 || got.ScratchBytes != 16 {
			t.Fatalf("unwritten scratch did not reserve its full maximum: %+v", got)
		}
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}
		third := newTestScratch(t, f.store, context.Background(), 10)
		if err := second.Close(); err != nil {
			t.Fatal(err)
		}
		if err := third.Close(); err != nil {
			t.Fatal(err)
		}
		if got := f.store.Status(); !got.Healthy || got.ScratchBytes != 0 || got.ScratchFiles != 0 {
			t.Fatalf("maximum reservations were not released: %+v", got)
		}
	})
}

func TestScratchFreeSpaceUsesUnallocatedRemainder(t *testing.T) {
	const maximum = int64(8 << 20)
	configure := func(cfg *Config) { cfg.MaxObjectBytes, cfg.MaxTotalBytes = 16<<20, 64<<20 }
	t.Run("new scratch admission", func(t *testing.T) {
		f := newStoreFixture(t, configure)
		first := newTestScratch(t, f.store, context.Background(), maximum)
		allocated := allocatedScratchBytes(t, first, maximum)
		remaining := maximum - allocated
		reserve := uint64(metadataReserve) + uint64(f.config.MinFreeBytes)
		f.store.freeBytes = func(int) (uint64, error) { return reserve + uint64(remaining) + 32, nil }
		second := newTestScratch(t, f.store, context.Background(), 32)
		_, err := f.store.Scratch(context.Background(), 1)
		requireError(t, err, ErrQuota)
		if got := f.store.Status(); !got.Healthy || got.ScratchFiles != 2 || got.ScratchBytes != maximum+32 {
			t.Fatalf("allocation was double-counted or reservation escaped: allocated=%d status=%+v", allocated, got)
		}
		if err := second.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("catalog write admission", func(t *testing.T) {
		f := newStoreFixture(t, configure)
		published := publishTestObject(t, f.store, bytes.Repeat([]byte("c"), 8192))
		scratch := newTestScratch(t, f.store, context.Background(), maximum)
		allocated := allocatedScratchBytes(t, scratch, maximum)
		remaining := maximum - allocated
		reserve := uint64(metadataReserve) + uint64(f.config.MinFreeBytes)
		free := reserve + uint64(remaining) + 4096
		f.store.freeBytes = func(int) (uint64, error) { return free, nil }
		w := beginTestWriter(t, f.store, context.Background())
		writeTestBytes(t, w, bytes.Repeat([]byte("w"), 4096))
		// The sampled free space already excludes existing catalog bytes and
		// allocated scratch blocks. Only future allocation remains reserved.
		free = reserve + uint64(remaining)
		if n, err := w.Write([]byte("x")); n != 0 || !errors.Is(err, ErrQuota) {
			t.Fatalf("Write did not preserve the unallocated remainder: n=%d error=%v", n, err)
		}
		if got := f.store.Status(); !got.Healthy || got.Bytes != published.Size+4096 || got.ScratchBytes != maximum {
			t.Fatalf("catalog write space accounting: allocated=%d status=%+v", allocated, got)
		}
	})
	t.Run("allocation sampled before free space", func(t *testing.T) {
		f := newStoreFixture(t, configure)
		scratch := newTestScratch(t, f.store, context.Background(), maximum)
		reserve := uint64(metadataReserve) + uint64(f.config.MinFreeBytes)
		calls := 0
		f.store.freeBytes = func(int) (uint64, error) {
			calls++
			allocated := allocatedScratchBytes(t, scratch, maximum)
			return reserve + uint64(maximum-allocated) + 32, nil
		}
		// Growth between the allocation sample and the free-space sample must
		// remain conservative rather than retroactively reducing the reserve.
		_, err := f.store.Scratch(context.Background(), 32)
		requireError(t, err, ErrQuota)
		if got := f.store.Status(); calls != 1 || !got.Healthy || got.ScratchFiles != 1 || got.ScratchBytes != maximum {
			t.Fatalf("allocation/free-space sampling order: calls=%d status=%+v", calls, got)
		}
	})
}

func TestScratchCancellationAndStoreCloseReleaseReservations(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			f := newStoreFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			scratch := newTestScratch(t, f.store, ctx, 32)
			file := scratch.File()
			if _, err := file.Write([]byte("plaintext")); err != nil {
				t.Fatal(err)
			}
			cancel()
			synctest.Wait()
			requireClosedScratchFile(t, scratch, file)
			if err := scratch.Close(); err != nil {
				t.Fatal(err)
			}
			if got := f.store.Status(); !got.Healthy || got.ScratchFiles != 0 || got.ScratchBytes != 0 || got.Objects != 0 {
				t.Fatalf("cancelled scratch reservations: %+v", got)
			}
		})
	})
	t.Run("store closes scratches before directory", func(t *testing.T) {
		f := newStoreFixture(t)
		first := newTestScratch(t, f.store, context.Background(), 8)
		second := newTestScratch(t, f.store, context.Background(), 16)
		firstFile, secondFile := first.File(), second.File()
		var releaseDirectoryError error
		releases := 0
		for _, scratch := range []*Scratch{first, second} {
			release := scratch.release
			scratch.release = func(value *Scratch) {
				if _, err := f.store.directory.Stat(); err != nil {
					releaseDirectoryError = err
				}
				release(value)
				releases++
			}
		}
		if err := f.store.Close(); err != nil {
			t.Fatal(err)
		}
		if releaseDirectoryError != nil || releases != 2 {
			t.Fatalf("scratch release order: releases=%d directory error=%v", releases, releaseDirectoryError)
		}
		requireClosedScratchFile(t, first, firstFile)
		requireClosedScratchFile(t, second, secondFile)
		if _, err := f.store.directory.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("store directory is not closed: %v", err)
		}
		if got := f.store.Status(); !got.Closed || got.Healthy || got.ScratchFiles != 0 || got.ScratchBytes != 0 {
			t.Fatalf("closed store retained scratch reservations: %+v", got)
		}
		_, err := f.store.Scratch(context.Background(), 1)
		requireError(t, err, ErrUnavailable)
	})
}

func TestScratchConcurrentCloseWaitsForReservationRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newStoreFixture(t)
		scratch := newTestScratch(t, f.store, context.Background(), 64)
		file := scratch.File()
		entered := make(chan struct{})
		allowRelease := make(chan struct{})
		var releaseOnce sync.Once
		unblockRelease := func() { releaseOnce.Do(func() { close(allowRelease) }) }
		defer unblockRelease()
		release := scratch.release
		scratch.release = func(value *Scratch) {
			close(entered)
			<-allowRelease
			release(value)
		}
		first, second := make(chan error, 1), make(chan error, 1)
		go func() { first <- scratch.Close() }()
		<-entered
		go func() { second <- scratch.Close() }()
		synctest.Wait()
		select {
		case err := <-first:
			t.Fatalf("first Close returned before reservation release: %v", err)
		default:
		}
		select {
		case err := <-second:
			t.Fatalf("second Close returned before reservation release: %v", err)
		default:
		}
		requireClosedScratchFile(t, scratch, file)
		if got := f.store.Status(); got.ScratchFiles != 1 || got.ScratchBytes != 64 {
			t.Fatalf("reservation escaped the close barrier: %+v", got)
		}
		unblockRelease()
		synctest.Wait()
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if got := f.store.Status(); !got.Healthy || got.ScratchFiles != 0 || got.ScratchBytes != 0 {
			t.Fatalf("Close completed without releasing its reservation: %+v", got)
		}
	})
}

func TestScratchFileRemainsClosedAfterDescriptorReuse(t *testing.T) {
	for _, rawClose := range []bool{false, true} {
		name := "Scratch.Close"
		if rawClose {
			name = "raw File.Close retains reservation"
		}
		t.Run(name, func(t *testing.T) {
			f := newStoreFixture(t, func(cfg *Config) { cfg.MaxObjectBytes, cfg.MaxTotalBytes = 64, 64 })
			scratch := newTestScratch(t, f.store, context.Background(), 32)
			original := scratch.File()
			originalFD := int(original.Fd())
			sentinel, err := os.CreateTemp(t.TempDir(), "sentinel-")
			if err != nil {
				t.Fatal(err)
			}
			defer sentinel.Close()
			contents := []byte("unrelated descriptor contents")
			if n, err := sentinel.Write(contents); err != nil || n != len(contents) {
				t.Fatalf("write sentinel: n=%d error=%v", n, err)
			}
			if rawClose {
				err = original.Close()
			} else {
				err = scratch.Close()
			}
			if err != nil {
				t.Fatal(err)
			}
			// Claim the old number without overwriting a descriptor that another
			// goroutine might have opened since the scratch file was closed.
			reusedFD, err := unix.FcntlInt(sentinel.Fd(), unix.F_DUPFD_CLOEXEC, originalFD)
			if err != nil {
				t.Fatal(err)
			}
			if reusedFD != originalFD {
				_ = unix.Close(reusedFD)
				t.Skip("the old descriptor was claimed by concurrent process I/O before the fixture could reserve it")
			}
			reused := os.NewFile(uintptr(reusedFD), "reused scratch descriptor")
			defer reused.Close()
			requireClosedScratchFile(t, scratch, original)
			if rawClose {
				_, err := f.store.Scratch(context.Background(), 33)
				requireError(t, err, ErrQuota)
				additional := newTestScratch(t, f.store, context.Background(), 1)
				if err := additional.Close(); err != nil {
					t.Fatal(err)
				}
				w := beginTestWriter(t, f.store, context.Background())
				writeTestBytes(t, w, bytes.Repeat([]byte("w"), 32))
				if got := f.store.Status(); !got.Healthy || got.Bytes != 32 || got.ScratchFiles != 1 || got.ScratchBytes != 32 {
					t.Fatalf("raw file Close lost its full reservation: %+v", got)
				}
			}
			if err := scratch.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := reused.Seek(0, io.SeekStart); err != nil {
				t.Fatalf("Scratch.Close closed the reused descriptor: %v", err)
			}
			got, err := io.ReadAll(reused)
			if err != nil || !bytes.Equal(got, contents) {
				t.Fatalf("old scratch handle touched the reused descriptor: data=%q error=%v", got, err)
			}
			if got := f.store.Status(); !got.Healthy || got.ScratchFiles != 0 || got.ScratchBytes != 0 {
				t.Fatalf("descriptor reuse changed scratch reservation state: %+v", got)
			}
		})
	}
}
