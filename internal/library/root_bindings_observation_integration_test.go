//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestRootBindingObservationReadUsesNoStoreOrSQLLockAndRechecksAdministrator(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 4)
	if StorageObservationSnapshot().Active != 0 {
		t.Fatal("unexpected preexisting observation")
	}
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	wanted := fixture.snapshot
	defer func() { close(release); waitRootBindingObservationIdle(t) }()
	fixture.store.ownership.mu.Lock()
	defer fixture.store.ownership.mu.Unlock()
	go func() {
		result, err := fixture.read(func(ctx context.Context, root libraryRoot) (RootTopologySnapshot, error) {
			return fixture.store.observeRootBindingWith(ctx, root, func(context.Context, string, libraryRoot) (RootTopologySnapshot, error) {
				close(started)
				<-release
				return wanted, nil
			})
		})
		if err == nil || !reflect.DeepEqual(result, RootBindingInfo{}) {
			returned <- errors.New("read returned an unauthorized or stale snapshot")
			return
		}
		returned <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("binding read attempted to acquire catalog ownership during filesystem observation")
	}
	if !fixture.store.mu.TryLock() {
		t.Fatal("binding worker retained Store.mu")
	}
	fixture.store.mu.Unlock()
	if fixture.pool.Stat().AcquiredConns() != 1 {
		t.Fatal("filesystem observation retained a SQL connection in addition to catalog ownership")
	}
	if _, err := fixture.pool.Exec(fixture.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, fixture.actor.SessionID); err != nil {
		t.Fatal(err)
	}
	// The observation can time out with its worker still blocked. Its caller
	// must still perform current authorization before returning a 503 sentinel.
	select {
	case err := <-returned:
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("observation timeout bypassed current authority: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("binding read did not return after its bounded observation")
	}
	if StorageObservationSnapshot().Active != 1 {
		t.Fatal("bounded return hid the still-blocked filesystem worker")
	}
}

func TestRootBindingObservationFullCapacityIsUnavailableRatherThanBindingProjection(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 5)
	root := libraryRoot{allowedPath: fixture.root.AllowedPath, path: fixture.root.Path, relativePath: fixture.root.RelativePath}
	occupyRootBindingObservationSlots(t, fixture.store, root)
	value, err := fixture.store.GetRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID)
	// The existing native library error mapping turns this domain sentinel into
	// HTTP 503; a nil error with BindingUnavailable would incorrectly be HTTP 200.
	if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(value, RootBindingInfo{}) {
		t.Fatalf("full capacity returned a normal binding projection: %+v, %v", value, err)
	}
	if status := StorageObservationSnapshot(); status.Active != status.Capacity {
		t.Fatal("read changed occupied observation capacity")
	}
}

func TestRootBindingObservationOrdinaryMissingPathPreservesUnavailableProjection(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 6)
	moved := fixture.root.Path + ".unavailable"
	if err := os.Rename(fixture.root.Path, moved); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Rename(moved, fixture.root.Path); err != nil {
			t.Error(err)
		}
	}()
	value, err := fixture.store.GetRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID)
	if err != nil || value.Status != RootBindingUnavailable || value.Approved == nil || value.ApprovedFingerprint == "" || value.Observed != nil {
		t.Fatalf("ordinary unreachable storage lost its approved unavailable projection: %+v, %v", value, err)
	}
	if StorageObservationSnapshot().Active != 0 {
		t.Fatal("completed unavailable observation retained capacity")
	}
}
