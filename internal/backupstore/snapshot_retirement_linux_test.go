//go:build linux

package backupstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
)

func TestSnapshotRetirementPublishesCloseFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "explicit close"
		if cancelled {
			name = "context cancellation"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := newStoreFixture(t)
				metadata := publishTestObject(t, f.store, []byte("ready"))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				snapshot, err := f.store.Snapshot(ctx, metadata.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := snapshot.file.Close(); err != nil {
					t.Fatal(err)
				}
				retired := make(chan struct{})
				allowReturn := make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(allowReturn) }) }
				defer unblock()
				release := snapshot.release
				snapshot.release = func(reader *Snapshot, closeErr error) {
					release(reader, closeErr)
					close(retired)
					<-allowReturn
				}
				closed := make(chan error, 1)
				if cancelled {
					cancel()
				} else {
					go func() { closed <- snapshot.Close() }()
				}
				<-retired
				if status := f.store.Status(); status.Readers != 0 || status.Healthy {
					t.Fatalf("retired failed reader left a healthy store: %+v", status)
				}
				for attempt := 0; attempt < 2; attempt++ {
					if err := f.store.Close(); !errors.Is(err, ErrUnavailable) {
						t.Fatalf("store close %d lost a retired reader failure: %v", attempt, err)
					}
				}
				unblock()
				synctest.Wait()
				if !cancelled {
					if err := <-closed; !errors.Is(err, ErrUnavailable) {
						t.Fatalf("snapshot close failure = %v", err)
					}
				}
				if err := snapshot.Close(); !errors.Is(err, ErrUnavailable) {
					t.Fatalf("repeated snapshot close lost its failure: %v", err)
				}
			})
		})
	}
}

func TestStoreCloseWaitsForPendingSnapshotCloseFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newStoreFixture(t)
		metadata := publishTestObject(t, f.store, []byte("ready"))
		snapshot, err := f.store.Snapshot(context.Background(), metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := snapshot.file.Close(); err != nil {
			t.Fatal(err)
		}
		entered := make(chan struct{})
		allowRelease := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(allowRelease) }) }
		defer unblock()
		release := snapshot.release
		snapshot.release = func(reader *Snapshot, closeErr error) {
			close(entered)
			<-allowRelease
			release(reader, closeErr)
		}
		snapshotClosed := make(chan error, 1)
		storeClosed := make(chan error, 2)
		go func() { snapshotClosed <- snapshot.Close() }()
		<-entered
		go func() { storeClosed <- f.store.Close() }()
		go func() { storeClosed <- f.store.Close() }()
		synctest.Wait()
		select {
		case err := <-storeClosed:
			t.Fatalf("store closed before reader failure publication: %v", err)
		default:
		}
		if status := f.store.Status(); !status.Closed || status.Readers != 1 {
			t.Fatalf("store did not retain the closing reader: %+v", status)
		}
		unblock()
		synctest.Wait()
		if err := <-snapshotClosed; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("snapshot close failure = %v", err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			if err := <-storeClosed; !errors.Is(err, ErrUnavailable) {
				t.Fatalf("store close %d lost its reader failure: %v", attempt, err)
			}
		}
		if status := f.store.Status(); status.Readers != 0 {
			t.Fatalf("failed close retained its reader slot: %+v", status)
		}
	})
}

func TestStoreCloseWaitsForPendingSnapshotReadFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		f := newStoreFixture(t)
		metadata := publishTestObject(t, f.store, []byte("ready"))
		snapshot, err := f.store.Snapshot(context.Background(), metadata.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(filepath.Join(f.config.Directory, basename(metadata.ID, true)), 0); err != nil {
			t.Fatal(err)
		}
		entered := make(chan struct{})
		allowFailure := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(allowFailure) }) }
		defer unblock()
		failure := snapshot.failure
		snapshot.failure = func() {
			close(entered)
			<-allowFailure
			failure()
		}
		readDone := make(chan error, 1)
		storeClosed := make(chan error, 1)
		closeStarted := make(chan struct{})
		go func() {
			_, err := snapshot.Read(make([]byte, 1))
			readDone <- err
		}()
		<-entered
		// The paused failure must still exclude reader retirement. A mutex
		// waiter is not durably blocked for synctest.Wait, so observe the fence
		// directly before launching shutdown and releasing the failure gate.
		if snapshot.mu.TryLock() {
			snapshot.mu.Unlock()
			t.Fatal("reader retirement was allowed before integrity publication")
		}
		go func() {
			close(closeStarted)
			storeClosed <- f.store.Close()
		}()
		<-closeStarted
		unblock()
		if err := <-readDone; !errors.Is(err, ErrIntegrity) {
			t.Fatalf("snapshot read failure = %v", err)
		}
		if err := <-storeClosed; !errors.Is(err, ErrUnavailable) {
			t.Fatalf("store close lost its reader integrity failure: %v", err)
		}
		if err := snapshot.Close(); err != nil {
			t.Fatalf("normal descriptor close failed after a read error: %v", err)
		}
	})
}
