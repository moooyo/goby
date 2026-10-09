//go:build linux

package analysiscache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func temporaryBatchFixture(t *testing.T) (*Store, *Builder, []string) {
	t.Helper()
	store := cacheTestOpen(t, cacheTestConfig(t))
	builder, err := store.Begin(context.Background(), cacheTestKey(1), 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := builder.Abort(ctx); err != nil {
			t.Errorf("abort temporary batch fixture: %v", err)
		}
	})
	names := []string{"frame-000001.jpg", "frame-000002.jpg", "frame-000003.jpg"}
	for _, name := range names {
		if _, err := builder.WriteTemporary(context.Background(), name, cacheTestProducer([]byte(name))); err != nil {
			t.Fatal(err)
		}
	}
	return store, builder, names
}

func TestCacheTemporaryBatchPreflightsEveryNameBeforeDeletion(t *testing.T) {
	for _, kind := range []string{"missing", "busy", "duplicate", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			_, builder, names := temporaryBatchFixture(t)
			batch := []string{names[0], names[1]}
			want := ErrInvalidInput
			switch kind {
			case "missing":
				batch[1], want = "frame-000004.jpg", ErrNotFound
			case "busy":
				lease, err := builder.OpenTemporary(context.Background(), names[1])
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
				want = ErrBusy
			case "duplicate":
				batch[1] = names[0]
			case "invalid":
				batch[1] = "../frame-000002.jpg"
			}
			before := builder.used
			if err := builder.DeleteTemporaries(context.Background(), batch); !errors.Is(err, want) {
				t.Fatalf("batch preflight = %v, want %v", err, want)
			}
			if builder.ctx.Err() != nil || builder.used != before || len(builder.temporaries) != len(names) {
				t.Fatal("rejected preflight changed the builder or its accounting")
			}
			for _, name := range names {
				if _, err := builder.root.Lstat(name); err != nil || builder.temporaries[name].deleting {
					t.Fatalf("rejected batch changed temporary %s: %v", name, err)
				}
			}
		})
	}
}

func TestCacheTemporaryBatchSyncsOnceBeforeReusingFrameNames(t *testing.T) {
	ctx := context.Background()
	store, builder, names := temporaryBatchFixture(t)
	before := builder.used
	syncs := 0
	builder.mu.Lock()
	err := builder.deleteTemporaries(ctx, names, func(root *os.Root) error {
		syncs++
		if builder.used != before || len(builder.temporaries) != len(names) || store.Stats().ReservedBytes != builder.reservation {
			t.Error("batch released names or accounting before its durable boundary")
		}
		for _, name := range names {
			if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("temporary %s remained before the single sync: %v", name, err)
			}
			if lease, err := builder.OpenTemporary(ctx, name); lease != nil || !errors.Is(err, ErrBusy) {
				if lease != nil {
					_ = lease.Close()
				}
				t.Errorf("pending deletion admitted a reader: %v", err)
			}
		}
		return syncDirectory(root)
	})
	builder.mu.Unlock()
	if err != nil || syncs != 1 || len(builder.temporaries) != 0 || builder.used != builder.ownerBytes {
		t.Fatalf("batch did not commit exactly one durable deletion: err=%v syncs=%d used=%d", err, syncs, builder.used)
	}
	for _, name := range names {
		if _, err := builder.WriteTemporary(ctx, name, cacheTestProducer([]byte("next width"))); err != nil {
			t.Fatalf("successfully deleted frame name could not be reused: %v", err)
		}
	}
	if err := builder.DeleteTemporaries(ctx, names); err != nil {
		t.Fatal(err)
	}
	if _, err := builder.WriteFile(ctx, "240.bif", cacheTestProducer([]byte("durable final artifact"))); err != nil {
		t.Fatal(err)
	}
	publication, err := builder.Publish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := publication.Discard(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestCacheTemporaryBatchFailureRetainsReservationUntilAbort(t *testing.T) {
	for _, kind := range []string{"partial_identity", "sync", "cancel_at_sync", "cancel_before"} {
		t.Run(kind, func(t *testing.T) {
			store, builder, names := temporaryBatchFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("temporary directory sync failed")
			want := failure
			syncs := 0
			if kind == "partial_identity" {
				path := filepath.Join(store.config.Root, builder.directory, names[1])
				if err := os.WriteFile(path, []byte("changed temporary identity"), 0600); err != nil {
					t.Fatal(err)
				}
				want = ErrUnsafe
			} else if kind == "cancel_at_sync" || kind == "cancel_before" {
				want = context.Canceled
				if kind == "cancel_before" {
					cancel()
				}
			}
			before := builder.used
			builder.mu.Lock()
			err := builder.deleteTemporaries(ctx, names, func(root *os.Root) error {
				syncs++
				if kind == "cancel_at_sync" {
					cancel()
					return syncDirectory(root)
				}
				return failure
			})
			if !errors.Is(err, want) || builder.ctx.Err() == nil || builder.used != before || len(builder.temporaries) != len(names) ||
				store.Stats().ReservedBytes != builder.reservation {
				t.Errorf("failed batch lost its abort fence or charged names: error=%v used=%d", err, builder.used)
			}
			if kind == "partial_identity" {
				if _, err := builder.root.Lstat(names[0]); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("identity failure did not occur after a real partial unlink: %v", err)
				}
			}
			if kind == "partial_identity" || kind == "cancel_before" {
				if syncs != 0 {
					t.Error("failed pre-sync work reached the durable boundary")
				}
			} else if syncs != 1 {
				t.Errorf("sync-boundary failure used %d sync calls", syncs)
			}
			builder.mu.Unlock()
			if _, err := builder.WriteTemporary(context.Background(), names[0], cacheTestProducer([]byte("unsafe reuse"))); err == nil {
				t.Fatal("failed batch permitted a frame name to be reused")
			}
			if err := builder.Abort(context.Background()); err != nil {
				t.Fatalf("owned partial batch could not be cleaned up: %v", err)
			}
			if stats := store.Stats(); stats.BuildingEntries != 0 || stats.ReservedBytes != 0 || stats.TotalBytes != stats.ControlBytes {
				t.Fatalf("completed abort did not release the retired workspace: %+v", stats)
			}
		})
	}
}

func TestCacheTemporaryBatchCloseWaitsForDirectorySync(t *testing.T) {
	store, builder, names := temporaryBatchFixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	releaseSync := sync.OnceFunc(func() { close(release) })
	defer releaseSync()
	done := make(chan error, 1)
	go func() {
		builder.mu.Lock()
		defer builder.mu.Unlock()
		done <- builder.deleteTemporaries(context.Background(), names, func(root *os.Root) error {
			close(started)
			<-release
			return syncDirectory(root)
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("the controlled directory sync did not start")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Close(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("close did not preserve the blocked directory sync: %v", err)
	}
	if stats := store.Stats(); stats.ReservedBytes != builder.reservation || stats.BuildingEntries != 1 {
		t.Fatalf("close released an active deletion batch: %+v", stats)
	}
	select {
	case <-builder.done:
		t.Fatal("abort finished while directory sync was still active")
	default:
	}
	releaseSync()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled batch reported success after its sync returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("directory sync did not retire")
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
