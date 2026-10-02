//go:build linux

package library

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

func TestSourceMetadataQueuedPhaseObservesStoreClose(t *testing.T) {
	fixture := mediaSourceTestCatalog(t, &libraryFixtureProber{})
	waitCtx, cancelWait := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancelWait()
	snapshot, err := fixture.store.readMediaSourceFor(waitCtx, Subject{UserID: fixture.userID}, fixture.item.ID, "")
	if err != nil {
		t.Fatalf("capture authorized source metadata snapshot: %v", err)
	}
	beforeIO := originalMediaReadGovernor.Stats()
	beforeMetadata := sourceMetadataReadOwners.Stats()
	beforeBodyOwners := originalMediaReadOwners.Stats()
	caller := context.Background()
	metadataEntered := make(chan struct{})
	var metadataResult, closeResult chan error
	var metadataReceived, closeReceived bool
	type heldPhase struct {
		read        *MediaSourceReadIO
		work        context.Context
		entered     chan struct{}
		release     chan struct{}
		result      chan error
		releaseOnce sync.Once
		received    bool
	}
	var phases []*heldPhase
	releaseAll := func() {
		for _, phase := range phases {
			phase.releaseOnce.Do(func() { close(phase.release) })
		}
	}
	// Every failure opens the independent gates before joining workers. Neither
	// caller cancellation nor a waiting deadline supplies actual retirement.
	t.Cleanup(func() {
		releaseAll()
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		for _, phase := range phases {
			if !phase.received {
				select {
				case <-phase.result:
					phase.received = true
				case <-cleanupCtx.Done():
					t.Error("held source body phase did not join during bounded cleanup")
				}
			}
			if err := phase.read.Close(); err != nil {
				t.Errorf("retire held source body capability during cleanup: %v", err)
			}
		}
		if metadataResult != nil && !metadataReceived {
			select {
			case <-metadataResult:
				metadataReceived = true
			case <-cleanupCtx.Done():
				t.Error("queued source metadata caller did not join during bounded cleanup")
			}
		}
		if closeResult != nil && !closeReceived {
			select {
			case <-closeResult:
				closeReceived = true
			case <-cleanupCtx.Done():
				t.Error("initial Store.Close caller did not join during bounded cleanup")
			}
		}
		if err := fixture.store.Close(cleanupCtx); err != nil {
			t.Errorf("join actual Store source lifetimes during cleanup: %v", err)
		}
	})

	// Separate opaque capabilities consume all three foreground slots for the
	// same committed root. Their callbacks deliberately wait through shutdown.
	for range mediaSourceRootOwnerLimit {
		read, err := fixture.store.PrepareMediaSourceIO(waitCtx, snapshot.mediaFile)
		if err != nil {
			t.Fatalf("prepare retained source body capability: %v", err)
		}
		phase := &heldPhase{read: read, entered: make(chan struct{}), release: make(chan struct{}), result: make(chan error, 1)}
		phases = append(phases, phase)
		go func() {
			phase.result <- phase.read.Run(caller, func(work context.Context) error {
				phase.work = work
				close(phase.entered)
				<-phase.release
				return nil
			})
		}()
		if read.class != primaryio.Foreground {
			t.Fatalf("authorized body capability has class %v, want foreground", read.class)
		}
		select {
		case <-phase.entered:
		case <-waitCtx.Done():
			t.Fatalf("retained source body phase did not start: %v", waitCtx.Err())
		}
	}
	if stats := originalMediaReadGovernor.Stats(); stats.Active != beforeIO.Active+mediaSourceRootOwnerLimit ||
		stats.Background != beforeIO.Background || stats.Queued != beforeIO.Queued {
		t.Fatalf("foreground body phases did not saturate only their captured root: before=%+v after=%+v", beforeIO, stats)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeBodyOwners.RegisteredOwners+mediaSourceRootOwnerLimit {
		t.Fatalf("held body phases did not retain independent owners: before=%+v after=%d", beforeBodyOwners, owners)
	}

	metadataResult = make(chan error, 1)
	go func() {
		metadataResult <- fixture.store.runSourceMetadata(caller, snapshot, func(context.Context) error {
			close(metadataEntered)
			return nil
		})
	}()
	waitFor := func(message string, ready func() bool) {
		t.Helper()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for !ready() {
			select {
			case <-waitCtx.Done():
				t.Fatalf("%s: metadata=%+v governor=%+v error=%v", message,
					sourceMetadataReadOwners.Stats(), originalMediaReadGovernor.Stats(), waitCtx.Err())
			case <-ticker.C:
			}
		}
	}
	waitFor("source metadata did not retain its owner while queued", func() bool {
		return originalMediaReadGovernor.Stats().Queued == beforeIO.Queued+1 &&
			sourceMetadataReadOwners.Stats().RegisteredOwners == beforeMetadata.RegisteredOwners+1
	})
	select {
	case <-metadataEntered:
		t.Fatal("source metadata callback bypassed the saturated root")
	default:
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancelClose()
	closeResult = make(chan error, 1)
	go func() { closeResult <- fixture.store.Close(closeCtx) }()
	metadataCtx, cancelMetadata := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelMetadata()
	select {
	case err := <-metadataResult:
		metadataReceived = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Store shutdown did not cancel queued metadata admission: %v", err)
		}
	case <-metadataCtx.Done():
		t.Fatal("Store shutdown did not promptly cancel the queued metadata owner")
	}
	if err := caller.Err(); err != nil {
		t.Fatalf("queued metadata returned after caller cancellation instead of Store shutdown: %v", err)
	}
	select {
	case <-metadataEntered:
		t.Fatal("queued source metadata entered its callback during shutdown")
	default:
	}
	waitFor("canceled source metadata did not retire its queued owner", func() bool {
		return originalMediaReadGovernor.Stats().Queued == beforeIO.Queued &&
			sourceMetadataReadOwners.Stats() == beforeMetadata
	})
	select {
	case err := <-closeResult:
		closeReceived = true
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Store.Close did not respect its deadline while actual body workers were held: %v", err)
		}
	case <-waitCtx.Done():
		t.Fatalf("bounded Store.Close did not return: %v", waitCtx.Err())
	}
	if stats := originalMediaReadGovernor.Stats(); stats.Active != beforeIO.Active+mediaSourceRootOwnerLimit ||
		stats.Background != beforeIO.Background {
		t.Fatalf("Store cancellation retired held body phases: before=%+v after=%+v", beforeIO, stats)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeBodyOwners.RegisteredOwners+mediaSourceRootOwnerLimit {
		t.Fatalf("Store cancellation retired held body owners: before=%+v after=%d", beforeBodyOwners, owners)
	}
	for _, phase := range phases {
		select {
		case <-phase.work.Done():
		case <-waitCtx.Done():
			t.Fatalf("held body phase did not observe Store cancellation: %v", waitCtx.Err())
		}
		select {
		case err := <-phase.result:
			phase.received = true
			t.Fatalf("held body worker returned before its actual release: %v", err)
		default:
		}
	}
	select {
	case <-fixture.store.done:
		t.Fatal("Store completed before actual body workers joined")
	default:
	}
	if fixture.store.catalogChangesClosed.Load() {
		t.Fatal("Store retired catalog resources before actual body worker completion")
	}

	releaseAll()
	for _, phase := range phases {
		select {
		case err := <-phase.result:
			phase.received = true
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("released body phase lost Store cancellation: %v", err)
			}
		case <-waitCtx.Done():
			t.Fatalf("released body worker did not join: %v", waitCtx.Err())
		}
		if err := phase.read.Close(); err != nil {
			t.Fatalf("retire joined body capability: %v", err)
		}
	}
	joinCtx, cancelJoin := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelJoin()
	if err := fixture.store.Close(joinCtx); err != nil {
		t.Fatalf("Store did not drain after actual body workers and capabilities retired: %v", err)
	}
	if stats := originalMediaReadGovernor.Stats(); stats != beforeIO {
		t.Fatalf("actual Store drain retained primary I/O admission: before=%+v after=%+v", beforeIO, stats)
	}
	if stats := sourceMetadataReadOwners.Stats(); stats != beforeMetadata {
		t.Fatalf("actual Store drain retained metadata ownership: before=%+v after=%+v", beforeMetadata, stats)
	}
	if stats := originalMediaReadOwners.Stats(); stats != beforeBodyOwners {
		t.Fatalf("actual Store drain retained body ownership: before=%+v after=%+v", beforeBodyOwners, stats)
	}
	select {
	case <-metadataEntered:
		t.Fatal("canceled source metadata callback entered after its caller returned")
	default:
	}
}
