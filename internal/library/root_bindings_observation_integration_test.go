//go:build linux

package library

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestRootBindingObservationReadUsesNoStoreOrSQLLockAndRechecksAdministrator(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 4)
	if originalMediaReadGovernor.Stats().Active != 0 {
		t.Fatal("unexpected preexisting observation")
	}
	started, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	opened := make(chan *os.File, 1)
	ownerReady := make(chan (<-chan struct{}), 1)
	var actualOwner <-chan struct{}
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	wanted := fixture.snapshot
	defer func() {
		close(release)
		if actualOwner != nil {
			select {
			case <-actualOwner:
			case <-time.After(5 * time.Second):
				t.Error("actual filesystem observation owner did not retire")
			}
		}
	}()
	fixture.store.ownership.mu.Lock()
	defer fixture.store.ownership.mu.Unlock()
	go func() {
		result, err := fixture.read(func(ctx context.Context, root libraryRoot) (RootTopologySnapshot, error) {
			return fixture.store.observeRootBindingWith(ctx, root, func(work context.Context, path string, _ libraryRoot) (RootTopologySnapshot, error) {
				directory, err := os.Open(path)
				if err != nil {
					return RootTopologySnapshot{}, err
				}
				defer directory.Close()
				if _, err := directory.ReadDir(1); err != nil && !errors.Is(err, io.EOF) {
					return RootTopologySnapshot{}, err
				}
				ownerReady <- PrimaryRootIOFromContext(work).handle.state.done
				opened <- directory
				close(started)
				<-release
				if _, err := directory.Stat(); err != nil {
					return RootTopologySnapshot{}, err
				}
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
	held := <-opened
	actualOwner = <-ownerReady
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
	if status := originalMediaReadGovernor.Stats(); status.Active != 1 || status.Background != 0 || status.Queued != 0 || status.ActiveRoots != 1 || status.ActiveDomains != 1 {
		t.Fatalf("bounded return hid or changed the retained foreground filesystem phase: %+v", status)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+1 {
		t.Fatalf("bounded return dropped its actual retained worker owner: before=%d after=%d", beforeOwners, owners)
	}
	if _, err := held.Stat(); err != nil {
		t.Fatalf("bounded return closed the descriptor still held by its filesystem worker: %v", err)
	}
}

func TestRootBindingObservationFullCapacityIsUnavailableRatherThanBindingProjection(t *testing.T) {
	fixture := newRootBindingReadFixture(t)
	fixture.bind(t, 5)
	root := libraryRoot{allowedPath: fixture.root.AllowedPath, path: fixture.root.Path, relativePath: fixture.root.RelativePath}
	occupyRootBindingObservationSlots(t, fixture.store, root)
	beforeOwners := originalMediaReadOwners.Stats().RegisteredOwners
	started := time.Now()
	value, err := fixture.store.GetRootBinding(fixture.ctx, fixture.actor, fixture.library.ID, fixture.root.RootID)
	// The existing native library error mapping turns this domain sentinel into
	// HTTP 503; a nil error with BindingUnavailable would incorrectly be HTTP 200.
	if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(value, RootBindingInfo{}) {
		t.Fatalf("full capacity returned a normal binding projection: %+v, %v", value, err)
	}
	if elapsed := time.Since(started); elapsed > storageObservationTimeout+3*time.Second {
		t.Fatalf("root binding admission escaped its bounded observation deadline: %v", elapsed)
	}
	if status := originalMediaReadGovernor.Stats(); status.Active != 3 || status.Background != 0 || status.Queued != 0 || status.ActiveRoots != 1 || status.ActiveDomains != 1 {
		t.Fatalf("read changed the saturated configured-domain foreground phases: %+v", status)
	}
	if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners {
		t.Fatalf("failed admission retained another worker owner: before=%d after=%d", beforeOwners, owners)
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
	if originalMediaReadGovernor.Stats().Active != 0 {
		t.Fatal("completed unavailable observation retained capacity")
	}
}
