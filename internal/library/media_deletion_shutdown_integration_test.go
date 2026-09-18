package library

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFileDeletionShutdownRetainsOwnershipUntilStorageWorkFinishes(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		name := "active_request"
		if cancelRequest {
			name = "canceled_request"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, store, allowedRoot, _ := libraryIntegrationStore(t, &libraryFixtureProber{})
			request, cancel := context.WithCancel(ctx)
			defer cancel()
			entered, observedCancellation, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			key := fileDeletionKey{store: store, itemID: "blocked-deletion", kind: "media", index: -1}
			result := make(chan error, 1)
			go func() {
				result <- store.runFileDeletion(request, key, func(work context.Context) error {
					close(entered)
					<-work.Done()
					close(observedCancellation)
					// Model an in-flight filesystem syscall: context cancellation
					// cannot prove that its namespace mutation has stopped.
					<-release
					return work.Err()
				})
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("deletion worker was not admitted")
			}
			if cancelRequest {
				cancel()
				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrMediaDeletionRecovery) {
						t.Fatalf("request cancellation did not return pending recovery: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("canceled deletion request did not return")
				}
			}
			closing, cancelClose := context.WithCancel(ctx)
			defer cancelClose()
			closed := make(chan error, 1)
			go func() { closed <- store.Close(closing) }()
			select {
			case <-observedCancellation:
			case <-ctx.Done():
				t.Fatal("shutdown did not cancel deletion work")
			}
			// A caller deadline may return while cleanup continues, but it must
			// never release the PostgreSQL ownership lock to the next Store.
			cancelClose()
			if err := <-closed; !errors.Is(err, context.Canceled) {
				t.Fatalf("Close returned before blocked deletion work finished: %v", err)
			}
			select {
			case <-store.done:
				t.Fatal("shutdown completed with an in-flight deletion")
			default:
			}
			if _, found := fileDeletionInFlight.Load(key); !found {
				t.Fatal("canceled storage work lost its in-flight operation fence")
			}
			if successor, err := New(pool, &libraryFixtureProber{}, []string{allowedRoot}); !errors.Is(err, ErrBusy) {
				if successor != nil {
					_ = successor.Close(context.Background())
				}
				t.Fatalf("successor acquired ownership while old storage work remained active: %v", err)
			}
			enteredLate := false
			if err := store.runFileDeletion(ctx, fileDeletionKey{store: store, itemID: "late-deletion", kind: "media", index: -1}, func(context.Context) error {
				enteredLate = true
				return nil
			}); !errors.Is(err, ErrUnavailable) || enteredLate {
				t.Fatalf("shutdown admitted a new deletion: entered=%t error=%v", enteredLate, err)
			}
			unblock()
			if !cancelRequest {
				select {
				case err := <-result:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("worker ignored shutdown cancellation: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("released deletion worker did not finish")
				}
			}
			finish, finishCancel := context.WithTimeout(ctx, 10*time.Second)
			defer finishCancel()
			if err := store.Close(finish); err != nil {
				t.Fatalf("drained shutdown failed: %v", err)
			}
			if _, found := fileDeletionInFlight.Load(key); found {
				t.Fatal("finished deletion retained its operation fence")
			}
			successor, err := New(pool, &libraryFixtureProber{}, []string{allowedRoot})
			if err != nil {
				t.Fatalf("drained ownership was not handed to the successor: %v", err)
			}
			if err := successor.Close(finish); err != nil {
				t.Fatalf("close successor: %v", err)
			}
		})
	}
}
