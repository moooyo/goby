package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

func primaryRootIOTestFixture(t *testing.T, rootCount int) (*PrimaryRootIO, *primaryio.Governor, *primaryio.OwnerRuntime, <-chan struct{}) {
	t.Helper()
	governor, err := primaryio.NewGovernor(primaryio.Limits{Owners: 4, BackgroundOwners: 2,
		RootOwners: 3, RootBackgroundOwners: 2, DomainOwners: 3, DomainBackgroundOwners: 2,
		Queued: 128, RootQueued: 32, DomainQueued: 32})
	if err != nil {
		t.Fatal(err)
	}
	owners, err := primaryio.NewOwnerRuntime(governor, 64)
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	routes := make(map[string]primaryRootIORoute, rootCount)
	for index := range rootCount {
		rootID := fmt.Sprintf("root-%d", index)
		domain := filepath.Join(base, rootID)
		routes[rootID] = primaryRootIORoute{route: primaryio.Route{
			Roots: []primaryio.RootKey{{Catalog: "catalog", RootID: rootID}}, Domains: []string{domain}}, domain: domain}
	}
	finished := make(chan struct{})
	domains := &originalMediaReadDomainRegistry{claims: make(map[*originalMediaReadDomainClaim]struct{})}
	operation, err := newPrimaryRootIO(context.Background(), owners, domains, routes, func() { close(finished) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = operation.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := owners.Close(ctx); err != nil {
			t.Errorf("actual primary root owners remained at cleanup: %v", err)
		}
		governor.Close()
	})
	return operation, governor, owners, finished
}

func primaryRootIOTestWait(t *testing.T, done <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func primaryRootIOTestCondition(t *testing.T, condition func() bool, description string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for !condition() {
		if ctx.Err() != nil {
			t.Fatalf("timed out waiting for %s", description)
		}
		runtime.Gosched()
	}
}

func TestPrimaryRootIOSerial256RootsRetainOneOwner(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 256)
	for index := range 256 {
		rootID := fmt.Sprintf("root-%d", index)
		if err := operation.RunImmediate(context.Background(), rootID, primaryio.Background, func(context.Context) error {
			if stats := governor.Stats(); stats.Active != 1 || stats.Background != 1 || stats.ActiveRoots != 1 || stats.ActiveDomains != 1 {
				return fmt.Errorf("unexpected actual phase charge: %+v", stats)
			}
			if stats := owners.Stats(); stats.RegisteredOwners != 1 {
				return fmt.Errorf("metadata roots registered additional owners: %+v", stats)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
		t.Fatalf("serial phases did not retire: %+v", stats)
	}
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "single retained operation completion")
	if stats := owners.Stats(); stats.RegisteredOwners != 0 {
		t.Fatalf("serial root operation remained registered: %+v", stats)
	}
}

func TestPrimaryRootIONestedSubsetReusesAtomicPhase(t *testing.T) {
	operation, governor, _, _ := primaryRootIOTestFixture(t, 2)
	if err := operation.RunRoots(context.Background(), []string{"root-0", "root-1"}, primaryio.Background, func(ctx context.Context) error {
		before := governor.Stats()
		if err := operation.RunImmediate(ctx, "root-1", primaryio.Background, func(context.Context) error {
			if stats := governor.Stats(); stats != before || stats.Active != 1 || stats.ActiveRoots != 2 {
				return fmt.Errorf("nested phase changed atomic charge: before=%+v after=%+v", before, stats)
			}
			return nil
		}); err != nil {
			return err
		}
		if err := operation.Run(ctx, "root-0", primaryio.Foreground, func(context.Context) error {
			return errors.New("class escalation callback ran")
		}); !errors.Is(err, ErrBusy) {
			return fmt.Errorf("nested class change was admitted: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryRootIOSingleRoutePreservesAdmissionAndCallerIsolation(t *testing.T) {
	for _, scenario := range []string{"single", "bounded_duplicates", "oversized_duplicates", "invalid_root", "invalid_domain", "oversized_distinct"} {
		t.Run(scenario, func(t *testing.T) {
			fixture, governor, owners, _ := primaryRootIOTestFixture(t, 1)
			prepared := fixture.handle.state.routes["root-0"]
			expectedRoot, expectedDomain := prepared.route.Roots[0], prepared.route.Domains[0]
			source := primaryio.Route{Roots: []primaryio.RootKey{expectedRoot}, Domains: []string{expectedDomain}}
			wantError := false
			switch scenario {
			case "bounded_duplicates":
				source.Roots = append(source.Roots, expectedRoot)
				source.Domains = append(source.Domains, expectedDomain)
			case "oversized_duplicates":
				for len(source.Roots) < 9 {
					source.Roots = append(source.Roots, expectedRoot)
				}
				for len(source.Domains) < 17 {
					source.Domains = append(source.Domains, expectedDomain)
				}
			case "invalid_root":
				source.Roots[0].Catalog = ""
				wantError = true
			case "invalid_domain":
				source.Domains[0] = ""
				wantError = true
			case "oversized_distinct":
				for len(source.Domains) < 17 {
					source.Domains = append(source.Domains, fmt.Sprintf("%s-%d", expectedDomain, len(source.Domains)))
				}
				wantError = true
			}
			operation, err := newPrimaryRootIO(context.Background(), owners, fixture.handle.state.domains,
				map[string]primaryRootIORoute{"selected": {route: source, domain: prepared.domain}}, func() {})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := operation.Close(); err != nil {
					t.Error(err)
				}
			})
			// The constructor must detach these slices before any phase borrows
			// the retained route, including immediate and nested admissions.
			source.Roots[0] = primaryio.RootKey{Catalog: "changed", RootID: "changed"}
			source.Domains[0] = "changed"
			for _, run := range []func(context.Context, string, primaryio.Class, func(context.Context) error) error{operation.Run, operation.RunImmediate} {
				called := false
				err := run(context.Background(), "", primaryio.Background, func(ctx context.Context) error {
					called = true
					before := governor.Stats()
					if before.Active != 1 || before.ActiveRoots != 1 || before.ActiveDomains != 1 {
						return fmt.Errorf("single retained route changed its actual charge: %+v", before)
					}
					phase, _ := ctx.Value(primaryRootIOPhaseKey{}).(*primaryRootIOPhase)
					for _, root := range phase.route.Roots {
						if root != expectedRoot {
							return errors.New("caller mutation changed the retained root")
						}
					}
					for _, domain := range phase.route.Domains {
						if domain != expectedDomain {
							return errors.New("caller mutation changed the retained domain")
						}
					}
					return operation.RunImmediate(ctx, "selected", primaryio.Background, func(context.Context) error {
						if after := governor.Stats(); after != before {
							return fmt.Errorf("nested single route changed its existing charge: %+v", after)
						}
						return nil
					})
				})
				if wantError {
					if !errors.Is(err, ErrUnavailable) || called {
						t.Fatalf("invalid route reached actual work: called=%v error=%v", called, err)
					}
				} else if err != nil || !called {
					t.Fatalf("valid single route lost admission: called=%v error=%v", called, err)
				}
				if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
					t.Fatalf("completed single route retained a phase: %+v", stats)
				}
			}
		})
	}
}

func TestPrimaryRootIOExpandedNestedRouteNeverWaits(t *testing.T) {
	operation, governor, _, _ := primaryRootIOTestFixture(t, 2)
	if err := operation.Run(context.Background(), "root-0", primaryio.Background, func(ctx context.Context) error {
		if err := operation.Run(ctx, "root-1", primaryio.Background, func(context.Context) error {
			return errors.New("expanded nested route callback ran")
		}); !errors.Is(err, ErrBusy) {
			return fmt.Errorf("expanded nested route did not fail immediately: %v", err)
		}
		if stats := governor.Stats(); stats.Active != 1 || stats.Queued != 0 {
			return fmt.Errorf("expanded nested route changed admission: %+v", stats)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPrimaryRootIOForkAndCopiedHandleKeepCompletionRights(t *testing.T) {
	operation, _, owners, finished := primaryRootIOTestFixture(t, 1)
	fork, err := operation.Fork(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fork.Close() })
	copy := *operation
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	if err := copy.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
		t.Fatal("copied caller handle released the independent fork")
	default:
	}
	if owners.Stats().RegisteredOwners != 1 {
		t.Fatal("fork did not retain the operation owner")
	}
	if err := fork.Run(context.Background(), "", primaryio.Foreground, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := fork.Close(); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "fork completion")
}

func TestPrimaryRootIOCanceledObservationRetainsPhaseThroughActualClose(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	started, allowRead, closeStarted, allowClose := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var readOnce, closeOnce sync.Once
	t.Cleanup(func() {
		readOnce.Do(func() { close(allowRead) })
		closeOnce.Do(func() { close(allowClose) })
	})
	lifetime := &storageObservationLifetime{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		returned <- operation.Run(ctx, "", primaryio.Background, func(work context.Context) error {
			return runStorageObservationWithLimit(operation.Context(work), make(chan struct{}, 2), time.Minute,
				[]*storageObservationLifetime{lifetime}, func(context.Context) error {
					close(started)
					<-allowRead
					return nil
				})
		})
	}()
	primaryRootIOTestWait(t, started, "actual observation read")
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("observation cancellation returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("observation caller did not return after cancellation")
	}
	if err := lifetime.retire(func() error { close(closeStarted); <-allowClose; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := governor.Stats(); stats.Active != 1 || stats.Background != 1 || owners.Stats().RegisteredOwners != 1 {
		t.Fatalf("cancellation released an actual observation owner: %+v %+v", stats, owners.Stats())
	}
	readOnce.Do(func() { close(allowRead) })
	primaryRootIOTestWait(t, closeStarted, "retired descriptor close")
	if stats := governor.Stats(); stats.Active != 1 {
		t.Fatalf("blocked descriptor close released its phase: %+v", stats)
	}
	closeOnce.Do(func() { close(allowClose) })
	primaryRootIOTestWait(t, finished, "actual observation and descriptor retirement")
	if stats := governor.Stats(); stats.Active != 0 || owners.Stats().RegisteredOwners != 0 {
		t.Fatalf("actual observation retirement retained known owners: %+v %+v", stats, owners.Stats())
	}
}

func TestPrimaryRootIOUnknownRequiresIndependentActualRetirement(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	err := operation.Run(context.Background(), "", primaryio.Background, func(context.Context) error {
		return media.ErrProcessRetirementUnknown
	})
	if !errors.Is(err, media.ErrProcessRetirementUnknown) {
		t.Fatalf("unknown process evidence changed: %v", err)
	}
	if err := operation.Close(); !errors.Is(err, media.ErrProcessRetirementUnknown) {
		t.Fatalf("unknown operation close reported success: %v", err)
	}
	if stats := governor.Stats(); stats.Active != 1 || owners.Stats().RegisteredOwners != 1 {
		t.Fatalf("unknown retirement released actual ownership: %+v %+v", stats, owners.Stats())
	}
	proofError := errors.New("independent retirement still unknown")
	if err := operation.ConfirmRetired(func() error { return proofError }); !errors.Is(err, proofError) {
		t.Fatalf("failed independent proof changed: %v", err)
	}
	if governor.Stats().Active != 1 {
		t.Fatal("failed independent proof released the unknown phase")
	}
	if err := operation.ConfirmRetired(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "independent actual retirement")
	if governor.Stats().Active != 0 || owners.Stats().RegisteredOwners != 0 {
		t.Fatal("known independent retirement did not release actual ownership")
	}
	if err := operation.Close(); !errors.Is(err, media.ErrProcessRetirementUnknown) {
		t.Fatalf("historical evidence failure disappeared after known retirement: %v", err)
	}
}

func TestPrimaryRootIOFaultDuringRecoveryPreservesUnknownCharge(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	_ = operation.Run(context.Background(), "", primaryio.Background, func(context.Context) error { return media.ErrProcessRetirementUnknown })
	_ = operation.Close()
	proofStarted, allowProof := make(chan struct{}), make(chan struct{})
	var allowOnce sync.Once
	t.Cleanup(func() { allowOnce.Do(func() { close(allowProof) }) })
	recovered := make(chan error, 1)
	go func() {
		recovered <- operation.ConfirmRetired(func() error { close(proofStarted); <-allowProof; return nil })
	}()
	primaryRootIOTestWait(t, proofStarted, "independent actual recovery proof")
	newFailure := errors.New("later actual descriptor retirement remains unknown")
	if err := operation.MarkUnknown(newFailure); !errors.Is(err, newFailure) {
		t.Fatalf("later actual retirement failure was lost: %v", err)
	}
	allowOnce.Do(func() { close(allowProof) })
	select {
	case err := <-recovered:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("old proof cleared a concurrent later fault: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("recovery did not complete its bounded fixture")
	}
	if governor.Stats().Active != 1 || owners.Stats().RegisteredOwners != 1 {
		t.Fatal("concurrent later fault released the unknown actual owner")
	}
	if err := operation.ConfirmRetired(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "fresh independent retirement after concurrent fault")
}

func TestPrimaryRootIOLateUnknownCannotResurrectCompletedOwner(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	primaryRootIOTestWait(t, finished, "completed operation")
	if err := operation.MarkUnknown(errors.New("late unowned error")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("completed operation accepted another retirement fault: %v", err)
	}
	retainedPrimaryRootIO.Lock()
	_, retained := retainedPrimaryRootIO.states[operation.handle.state]
	retainedPrimaryRootIO.Unlock()
	if retained || governor.Stats().Active != 0 || owners.Stats().RegisteredOwners != 0 {
		t.Fatal("late error resurrected an uncharged retained operation")
	}
}

func TestPrimaryRootIOPinnedObservationSurvivesCallerHandleClose(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, allowCleanup := make(chan struct{}), make(chan struct{})
	var allowOnce sync.Once
	t.Cleanup(func() { allowOnce.Do(func() { close(allowCleanup) }) })
	returned := make(chan error, 1)
	actualPhase := make(chan struct{})
	go func() {
		returned <- runStorageObservationWithLimit(operation.Context(ctx), make(chan struct{}, 2), time.Minute, nil,
			func(work context.Context) error {
				close(started)
				<-allowCleanup
				return operation.RunImmediate(context.WithoutCancel(work), "", primaryio.Background, func(context.Context) error {
					close(actualPhase)
					if governor.Stats().Active != 1 {
						return errors.New("pinned actual cleanup has no read phase")
					}
					return nil
				})
			})
	}()
	primaryRootIOTestWait(t, started, "pinned observation")
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pinned observation caller returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pinned observation did not return to canceled caller")
	}
	if err := operation.Close(); err != nil {
		t.Fatal(err)
	}
	if owners.Stats().RegisteredOwners != 1 || governor.Stats().Active != 0 {
		t.Fatal("caller close released a pinned worker or charged idle work")
	}
	allowOnce.Do(func() { close(allowCleanup) })
	primaryRootIOTestWait(t, actualPhase, "already retained worker's cleanup phase")
	primaryRootIOTestWait(t, finished, "pinned worker's actual retirement")
}

func TestPrimaryRootIOContextCannotSpawnUnboundedObservationWorkers(t *testing.T) {
	for _, mode := range []string{"idle", "charged"} {
		t.Run(mode, func(t *testing.T) {
			operation, governor, _, _ := primaryRootIOTestFixture(t, 1)
			check := func(ctx context.Context) error {
				started, allowActual := make(chan struct{}), make(chan struct{})
				var allowOnce sync.Once
				defer allowOnce.Do(func() { close(allowActual) })
				firstCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				first := make(chan error, 1)
				go func() {
					first <- runStorageObservation(firstCtx, nil,
						func(context.Context) error { close(started); <-allowActual; return nil })
				}()
				primaryRootIOTestWait(t, started, "first bounded observation worker")
				cancel()
				select {
				case err := <-first:
					if !errors.Is(err, context.Canceled) {
						return fmt.Errorf("first observation cancellation changed: %w", err)
					}
				case <-time.After(5 * time.Second):
					return errors.New("first observation caller did not return")
				}
				called := false
				if err := runStorageObservation(ctx, nil,
					func(context.Context) error { called = true; return nil }); !errors.Is(err, ErrBusy) || called {
					return fmt.Errorf("copied context launched another actual worker: called=%t err=%v", called, err)
				}
				before := governor.Stats()
				allowOnce.Do(func() { close(allowActual) })
				primaryRootIOTestCondition(t, func() bool {
					state := operation.handle.state
					state.mu.Lock()
					defer state.mu.Unlock()
					return state.observers == 0
				}, "actual bounded worker retirement")
				if stats := governor.Stats(); stats != before {
					return fmt.Errorf("bounded worker changed its parent's charge: before=%+v after=%+v", before, stats)
				}
				return nil
			}
			var err error
			if mode == "charged" {
				err = operation.Run(context.Background(), "", primaryio.Background, check)
			} else {
				err = check(operation.Context(context.Background()))
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrimaryRootIOObservationCloseFailurePreservesActualOwnership(t *testing.T) {
	operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
	lifetime := &storageObservationLifetime{}
	started, allowRead := make(chan struct{}), make(chan struct{})
	var allowOnce sync.Once
	t.Cleanup(func() { allowOnce.Do(func() { close(allowRead) }) })
	closeFailure := errors.New("actual descriptor close cannot be proved")
	closeAttempts := 0
	closeDescriptor := func() error {
		closeAttempts++
		if closeAttempts == 1 {
			return closeFailure
		}
		return nil
	}
	returned := make(chan error, 1)
	go func() {
		returned <- operation.Run(context.Background(), "", primaryio.Background, func(ctx context.Context) error {
			return runStorageObservationWithLimit(ctx, make(chan struct{}, 2), time.Minute, []*storageObservationLifetime{lifetime},
				func(context.Context) error { close(started); <-allowRead; return nil })
		})
	}()
	primaryRootIOTestWait(t, started, "actual descriptor read")
	if err := lifetime.retire(closeDescriptor); err != nil {
		t.Fatal(err)
	}
	allowOnce.Do(func() { close(allowRead) })
	select {
	case err := <-returned:
		if !errors.Is(err, closeFailure) {
			t.Fatalf("actual descriptor close failure was lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual observation did not publish its close failure")
	}
	if err := operation.Close(); !errors.Is(err, closeFailure) {
		t.Fatalf("failed close completed the retained owner: %v", err)
	}
	if governor.Stats().Active != 1 || owners.Stats().RegisteredOwners != 1 {
		t.Fatal("failed actual descriptor close released I/O or Store ownership")
	}
	// The controlled descriptor's independent retry now completes its close.
	if err := operation.ConfirmRetired(closeDescriptor); err != nil {
		t.Fatal(err)
	}
	if closeAttempts != 2 {
		t.Fatalf("independent proof did not retry the same descriptor close: %d", closeAttempts)
	}
	primaryRootIOTestWait(t, finished, "independent descriptor retirement")
}

func TestPrimaryRootIOAbnormalResourceClosePreservesWorkerAndPhase(t *testing.T) {
	for _, mode := range []string{"panic_nil", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
			lifetime := &storageObservationLifetime{}
			started, allowRead := make(chan struct{}), make(chan struct{})
			var allowOnce sync.Once
			t.Cleanup(func() { allowOnce.Do(func() { close(allowRead) }) })
			closeAttempts := 0
			closeDescriptor := func() error {
				closeAttempts++
				if closeAttempts == 1 {
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic(nil)
				}
				return nil
			}
			returned := make(chan error, 1)
			go func() {
				returned <- operation.Run(context.Background(), "", primaryio.Background, func(ctx context.Context) error {
					return runStorageObservationWithLimit(ctx, make(chan struct{}, 1), time.Minute, []*storageObservationLifetime{lifetime},
						func(context.Context) error { close(started); <-allowRead; return nil })
				})
			}()
			primaryRootIOTestWait(t, started, "actual read before abnormal close")
			if err := lifetime.retire(closeDescriptor); err != nil {
				t.Fatal(err)
			}
			allowOnce.Do(func() { close(allowRead) })
			select {
			case err := <-returned:
				if err == nil {
					t.Fatal("abnormal resource close advertised successful retirement")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("abnormal close abandoned the observation worker's completion")
			}
			if err := operation.Close(); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("abnormal resource close completed retained ownership: %v", err)
			}
			if governor.Stats().Active != 1 || owners.Stats().RegisteredOwners != 1 {
				t.Fatal("abnormal resource close released the actual phase")
			}
			if err := operation.ConfirmRetired(closeDescriptor); err != nil {
				t.Fatal(err)
			}
			if closeAttempts != 2 {
				t.Fatalf("independent proof did not retry the same descriptor: %d", closeAttempts)
			}
			primaryRootIOTestWait(t, finished, "independent abnormal descriptor retirement")
		})
	}
}

func TestPrimaryRootIOCleanupUsesItsOwnLifetime(t *testing.T) {
	operation, _, _, _ := primaryRootIOTestFixture(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := operation.Run(ctx, "", primaryio.Background, func(context.Context) error {
		return errors.New("canceled payload callback ran")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled payload was admitted: %v", err)
	}
	if err := operation.RunImmediate(context.Background(), "", primaryio.Background, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("actual cleanup could not use its independent lifetime: %v", err)
	}
}

func TestPrimaryRootIOImmediateBusyNeverQueuesOrRunsProof(t *testing.T) {
	operation, governor, owners, _ := primaryRootIOTestFixture(t, 1)
	prepared := operation.handle.state.routes["root-0"]
	var held []*primaryio.PrimaryReadLease
	var holders []*primaryio.Owner
	defer func() {
		for _, lease := range held {
			_ = lease.Release()
		}
		for _, owner := range holders {
			_ = owner.Complete()
		}
	}()
	for range 2 {
		owner, err := owners.Register(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		holders = append(holders, owner)
		lease, err := owner.Acquire(prepared.route, primaryio.Background)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, lease)
	}
	called := false
	if err := operation.RunImmediate(context.Background(), "", primaryio.Background, func(context.Context) error {
		called = true
		return nil
	}); !errors.Is(err, ErrBusy) || called {
		t.Fatalf("busy final proof waited or ran: called=%t err=%v", called, err)
	}
	if stats := governor.Stats(); stats.Active != 2 || stats.Background != 2 || stats.Queued != 0 {
		t.Fatalf("busy final proof changed actual owners or queued: %+v", stats)
	}
	if err := operation.RunImmediate(context.Background(), "", primaryio.Foreground, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("background saturation consumed the foreground reserve: %v", err)
	}
}

func TestPrimaryRootIOAbnormalCallbacksRetainUntilActualWorkerDone(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
			actualDone := make(chan struct{})
			go func() {
				defer func() { _ = recover(); close(actualDone) }()
				_ = operation.Run(context.Background(), "", primaryio.Background, func(context.Context) error {
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("abnormal actual phase fixture")
				})
			}()
			primaryRootIOTestWait(t, actualDone, "abnormally exiting actual worker")
			if err := operation.Close(); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("abnormal callback advertised known retirement: %v", err)
			}
			if governor.Stats().Active != 1 || owners.Stats().RegisteredOwners != 1 {
				t.Fatal("abnormal callback released ownership without independent proof")
			}
			if err := operation.ConfirmRetired(func() error {
				<-actualDone
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			primaryRootIOTestWait(t, finished, "independently joined abnormal worker")
		})
	}
}

func TestPrimaryRootIOAbnormalObservationWorkersPreserveTheirPhase(t *testing.T) {
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
			actualDone := make(chan struct{})
			err := operation.Run(context.Background(), "", primaryio.Background, func(ctx context.Context) error {
				return runStorageObservationWithLimit(ctx, make(chan struct{}, 1), time.Minute, nil, func(context.Context) error {
					defer close(actualDone)
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("abnormal observation fixture")
				})
			})
			if err == nil {
				t.Fatal("abnormal observation returned successful evidence")
			}
			primaryRootIOTestWait(t, actualDone, "abnormally exiting observation")
			if err := operation.Close(); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("abnormal observation advertised known cleanup: %v", err)
			}
			if governor.Stats().Active != 1 || owners.Stats().RegisteredOwners != 1 {
				t.Fatal("abnormal observation released its actual phase")
			}
			if err := operation.ConfirmRetired(func() error { <-actualDone; return nil }); err != nil {
				t.Fatal(err)
			}
			primaryRootIOTestWait(t, finished, "independently joined abnormal observation")
		})
	}
}

func TestPrimaryRootIORejectsZeroSerializationAndExpandedAtomicRoutes(t *testing.T) {
	var zero PrimaryRootIO
	if err := zero.Run(context.Background(), "", primaryio.Foreground, func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero capability admitted work: %v", err)
	}
	if _, err := json.Marshal(zero); err == nil {
		t.Fatal("opaque capability was serialized")
	}
	if err := json.Unmarshal([]byte(`{}`), &zero); err == nil {
		t.Fatal("opaque capability was reconstructed from JSON")
	}
	operation, governor, _, _ := primaryRootIOTestFixture(t, 9)
	ids := make([]string, 9)
	for index := range ids {
		ids[index] = fmt.Sprintf("root-%d", index)
	}
	if err := operation.RunRootsImmediate(context.Background(), ids, primaryio.Background, func(context.Context) error { return nil }); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expanded atomic route changed its eight-root bound: %v", err)
	}
	if governor.Stats().Active != 0 || governor.Stats().Queued != 0 {
		t.Fatal("rejected atomic route entered admission")
	}
}

func primaryRootIOMetadataTestRuntime(t *testing.T, governor *primaryio.Governor) *primaryio.OwnerRuntime {
	t.Helper()
	owners, err := primaryio.NewOwnerRuntime(governor, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := owners.Close(ctx); err != nil {
			t.Errorf("actual metadata owner remained at cleanup: %v", err)
		}
	})
	return owners
}

func TestPrimaryRootIOCustomClaimFactoryFullRuntimeNeverAcquiresAlias(t *testing.T) {
	operation, governor, bodyOwners, _ := primaryRootIOTestFixture(t, 1)
	metadataOwners := primaryRootIOMetadataTestRuntime(t, governor)
	var holders []*primaryio.Owner
	defer func() {
		for _, owner := range holders {
			_ = owner.Complete()
		}
	}()
	for range 4 {
		owner, err := metadataOwners.Register(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		holders = append(holders, owner)
	}
	state := operation.handle.state
	acquired, finished := 0, 0
	created, err := newPrimaryRootIOWithClaimAcquire(context.Background(), metadataOwners, state.domains, state.routes,
		func() { finished++ }, func(domain string) (*originalMediaReadDomainClaim, error) {
			acquired++
			return state.domains.acquireShared(domain)
		})
	if created != nil || !errors.Is(err, ErrBusy) || acquired != 0 || finished != 1 {
		t.Fatalf("full metadata runtime acquired an alias: operation=%v err=%v acquired=%d finished=%d", created, err, acquired, finished)
	}
	if metadataOwners.Stats().RegisteredOwners != 4 || bodyOwners.Stats().RegisteredOwners != 1 || state.claim.refs != 1 {
		t.Fatal("failed metadata construction changed body or claim ownership")
	}
}

func TestPrimaryRootIOCustomClaimFactoryShares64EntriesWithoutBodyReservation(t *testing.T) {
	operation, governor, bodyOwners, _ := primaryRootIOTestFixture(t, 1)
	state := operation.handle.state
	domain := state.routes["root-0"].domain
	var claims []*originalMediaReadDomainClaim
	var holders []*primaryio.Owner
	defer func() {
		for _, claim := range claims {
			claim.release()
		}
		for _, owner := range holders {
			_ = owner.Complete()
		}
	}()
	for range 63 {
		owner, err := bodyOwners.Register(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		holders = append(holders, owner)
		claim, err := state.domains.acquire(domain)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
	}
	if claim, err := state.domains.acquire(domain); claim != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("ordinary claim changed its full-64 semantics: %v", err)
	}
	metadataOwners := primaryRootIOMetadataTestRuntime(t, governor)
	metadataFinished := make(chan struct{})
	metadata, err := newPrimaryRootIOWithClaimAcquire(context.Background(), metadataOwners, state.domains, state.routes,
		func() { close(metadataFinished) }, state.domains.acquireShared)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metadata.Close() })
	if bodyOwners.Stats().RegisteredOwners != 64 || metadataOwners.Stats().RegisteredOwners != 1 {
		t.Fatal("metadata construction reserved or reduced body owner capacity")
	}
	started, allowActual := make(chan struct{}), make(chan struct{})
	var allowOnce sync.Once
	t.Cleanup(func() { allowOnce.Do(func() { close(allowActual) }) })
	actual := make(chan error, 1)
	go func() {
		actual <- metadata.Run(context.Background(), "", primaryio.Foreground, func(context.Context) error {
			close(started)
			<-allowActual
			return nil
		})
	}()
	primaryRootIOTestWait(t, started, "metadata phase using shared actual governor")
	if governor.Stats().Active != 1 || governor.Stats().Background != 0 {
		t.Fatal("metadata used an independent actual-I/O governor")
	}
	if err := metadata.Close(); err != nil {
		t.Fatal(err)
	}
	alias := metadata.handle.state.claim
	alias.backing.release()
	state.domains.mu.Lock()
	_, live := state.domains.claims[alias.backing]
	entries := len(state.domains.claims)
	state.domains.mu.Unlock()
	if !live || entries != 64 || metadataOwners.Stats().RegisteredOwners != 1 {
		t.Fatal("original close or caller cancellation released the active metadata alias")
	}
	allowOnce.Do(func() { close(allowActual) })
	select {
	case err := <-actual:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual metadata phase did not retire")
	}
	primaryRootIOTestWait(t, metadataFinished, "actual metadata alias retirement")
	state.domains.mu.Lock()
	_, live = state.domains.claims[alias.backing]
	entries = len(state.domains.claims)
	state.domains.mu.Unlock()
	if live || entries != 63 || metadataOwners.Stats().RegisteredOwners != 0 || bodyOwners.Stats().RegisteredOwners != 64 {
		t.Fatal("known metadata retirement changed body capacity or retained its alias")
	}
}

func TestPrimaryRootIOCustomClaimFactoryRejectsMultipleRootsBeforeRegistration(t *testing.T) {
	operation, governor, _, _ := primaryRootIOTestFixture(t, 2)
	state := operation.handle.state
	metadataOwners := primaryRootIOMetadataTestRuntime(t, governor)
	acquired, finished := 0, 0
	created, err := newPrimaryRootIOWithClaimAcquire(context.Background(), metadataOwners, state.domains, state.routes,
		func() { finished++ }, func(domain string) (*originalMediaReadDomainClaim, error) {
			acquired++
			return state.domains.acquireShared(domain)
		})
	if created != nil || !errors.Is(err, ErrInvalidInput) || acquired != 0 || finished != 1 || metadataOwners.Stats().RegisteredOwners != 0 {
		t.Fatalf("custom factory accepted an expanded route: operation=%v err=%v acquired=%d finished=%d", created, err, acquired, finished)
	}
}

func TestPrimaryRootIOCustomClaimFactoryRetiresFailedAcquisition(t *testing.T) {
	for _, mode := range []string{"error_with_alias", "nil_claim", "wrong_registry"} {
		t.Run(mode, func(t *testing.T) {
			operation, governor, _, _ := primaryRootIOTestFixture(t, 1)
			state := operation.handle.state
			metadataOwners := primaryRootIOMetadataTestRuntime(t, governor)
			foreign := &originalMediaReadDomainRegistry{claims: make(map[*originalMediaReadDomainClaim]struct{})}
			failure := errors.New("metadata alias acquisition failed")
			finished := 0
			created, err := newPrimaryRootIOWithClaimAcquire(context.Background(), metadataOwners, state.domains, state.routes,
				func() { finished++ }, func(domain string) (*originalMediaReadDomainClaim, error) {
					if mode == "nil_claim" {
						return nil, nil
					}
					if mode == "wrong_registry" {
						return foreign.acquire(domain)
					}
					claim, err := state.domains.acquireShared(domain)
					if err != nil {
						return nil, err
					}
					return claim, failure
				})
			if created != nil || err == nil || finished != 1 || metadataOwners.Stats().RegisteredOwners != 0 {
				t.Fatalf("failed factory retained registration: operation=%v err=%v finished=%d", created, err, finished)
			}
			if mode == "error_with_alias" && !errors.Is(err, failure) {
				t.Fatalf("original claim failure changed: %v", err)
			}
			state.domains.mu.Lock()
			entries, refs := len(state.domains.claims), state.claim.refs
			state.domains.mu.Unlock()
			foreign.mu.Lock()
			foreignEntries := len(foreign.claims)
			foreign.mu.Unlock()
			if entries != 1 || refs != 1 || foreignEntries != 0 {
				t.Fatal("failed factory released a body claim early or retained its alias")
			}
		})
	}
}
