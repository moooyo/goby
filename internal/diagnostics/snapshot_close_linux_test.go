//go:build linux

package diagnostics

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
)

func TestStoreCloseIncludesPendingSnapshotReadFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		appendTestRecord(t, store, "read failure")
		item := firstTestFile(t, store)
		reader, err := store.Snapshot(context.Background(), item.Name)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		// A write-only descriptor fails ReadAt without changing the active file
		// or making the snapshot descriptor's eventual Close fail.
		writeOnly, err := os.OpenFile(filepath.Join(store.cfg.Directory, item.Name), os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := reader.file.Close(); err != nil {
			writeOnly.Close()
			t.Fatal(err)
		}
		reader.file = writeOnly

		gate := newDiagnosticSyncGate()
		defer gate.allow()
		active := store.active
		store.syncFile = func(file *os.File) error {
			if file == active {
				gate.block()
			}
			return file.Sync()
		}
		failureEntered := make(chan struct{})
		originalFailure := reader.failure
		reader.failure = func() {
			close(failureEntered)
			originalFailure()
		}
		storeClose := make(chan error, 1)
		go func() { storeClose <- store.Close() }()
		<-gate.entered
		readDone := make(chan error, 1)
		go func() {
			_, err := reader.Read(make([]byte, 1))
			readDone <- err
		}()
		// Store.Close has checked degradation and holds Store.mu. Read keeps
		// Snapshot.mu until its failure can acquire Store.mu and be published.
		<-failureEntered
		gate.allow()
		if err := <-readDone; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("snapshot read did not report its I/O failure: %v", err)
		}
		if err := <-storeClose; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("store Close lost the pending snapshot read failure: %v", err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("snapshot descriptor Close unexpectedly failed: %v", err)
		}
		if _, err := reader.file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("snapshot descriptor survived store Close: %v", err)
		}
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("repeated store Close lost the snapshot read failure: %v", err)
		}
		if status := store.Status(); !status.Closed || !status.Degraded || status.Healthy {
			t.Fatalf("closed store lost its degraded state: %+v", status)
		}
	})
}

func TestStoreCloseIncludesRetiredSnapshotCloseFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		item := firstTestFile(t, store)
		reader, err := store.Snapshot(context.Background(), item.Name)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if err := reader.file.Close(); err != nil {
			t.Fatal(err)
		}
		gate := newDiagnosticSyncGate()
		defer gate.allow()
		originalRelease := reader.release
		reader.release = func(snapshot *Snapshot, err error) {
			originalRelease(snapshot, err)
			gate.block()
		}
		readerClose := make(chan error, 1)
		go func() { readerClose <- reader.Close() }()
		<-gate.entered
		store.mu.Lock()
		remainingReaders, degraded := len(store.readers), store.degraded
		store.mu.Unlock()
		if remainingReaders != 0 || !degraded {
			t.Fatalf("reader retirement did not publish its failure: readers=%d degraded=%t", remainingReaders, degraded)
		}
		// The snapshot is no longer in Store.readers, but its Close has not
		// returned. Store.Close must already observe its descriptor failure.
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("store Close lost the retired snapshot failure: %v", err)
		}
		select {
		case <-reader.closeDone:
			t.Fatal("snapshot Close completed before its release callback returned")
		default:
		}
		gate.allow()
		if err := <-readerClose; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("snapshot Close did not preserve its descriptor failure: %v", err)
		}
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("repeated store Close lost the retired snapshot failure: %v", err)
		}
	})
}

func TestStoreConcurrentClosePreservesSnapshotFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := testStore(t, Config{})
		item := firstTestFile(t, store)
		reader, err := store.Snapshot(context.Background(), item.Name)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if err := reader.file.Close(); err != nil {
			t.Fatal(err)
		}
		gate := newDiagnosticSyncGate()
		defer gate.allow()
		originalRelease := reader.release
		reader.release = func(snapshot *Snapshot, err error) {
			gate.block()
			originalRelease(snapshot, err)
		}
		readerClose := make(chan error, 1)
		go func() { readerClose <- reader.Close() }()
		<-gate.entered
		const callers = 4
		storeClose := make(chan error, callers)
		for index := 0; index < callers; index++ {
			go func() { storeClose <- store.Close() }()
		}
		synctest.Wait()
		select {
		case err := <-storeClose:
			t.Fatalf("store Close returned before snapshot failure publication: %v", err)
		default:
		}
		gate.allow()
		for index := 0; index < callers; index++ {
			if err := <-storeClose; !errors.Is(err, ErrUnavailable) {
				t.Fatalf("concurrent store Close lost the snapshot failure: %v", err)
			}
		}
		if err := <-readerClose; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("snapshot Close did not preserve its descriptor failure: %v", err)
		}
		if err := store.Close(); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("repeated store Close lost the snapshot failure: %v", err)
		}
		store.mu.Lock()
		remainingReaders := len(store.readers)
		store.mu.Unlock()
		if remainingReaders != 0 {
			t.Fatal("store Close retained a failed snapshot reader")
		}
	})
}
