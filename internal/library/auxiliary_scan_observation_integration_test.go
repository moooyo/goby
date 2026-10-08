//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestAuxiliaryPreflightWaitsForActualAdmissionAndPropagatesCancellation(t *testing.T) {
	for _, operation := range []string{"directories", "absence", "theme enumeration", "extra enumeration"} {
		for _, canceled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/canceled_%t", operation, canceled), func(t *testing.T) {
				ctx, _, _, state, trace, _ := scanCachedVisitFixture(t)
				primaryScanRoutingRetainWalk(t, state)
				rootInfo, err := state.opened.Stat(".")
				if err != nil {
					t.Fatal(err)
				}
				state.themes = &themeScan{directories: map[string]os.FileInfo{".": rootInfo}}
				beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
				unblock, retire := cachedObservationBlockBackground(t, ctx, state)
				assertUnread := primaryScanRoutingWatchSource(t, state.root.path)
				trace.reset()
				done := make(chan struct{})
				var result error
				var absent bool
				t.Cleanup(func() {
					state.task.cancel()
					unblock()
					mediaSourceAdmissionTestWait(t, done, "auxiliary preflight cleanup")
				})
				go func() {
					switch operation {
					case "directories":
						result = state.verifyThemeDirectories(".", true)
					case "absence":
						absent, result = state.store.scanAuxiliaryPathAbsent(state.task, state.root, "missing/theme.mp3")
					case "theme enumeration":
						result = state.enumerateThemeDirectory(&themeDirectoryScan{}, ".")
					case "extra enumeration":
						result = state.enumerateExtraDirectory(&extraDirectoryScan{}, ".")
					}
					close(done)
				}()
				wait, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				primaryReadTestWaitQueued(t, wait, beforeIO.Queued+1)
				assertUnread()
				if canceled {
					state.task.cancel()
				} else {
					unblock()
				}
				mediaSourceAdmissionTestWait(t, done, "auxiliary preflight result")
				if canceled {
					if !errors.Is(result, context.Canceled) || auxiliaryInputWarning(result) {
						t.Fatalf("queued cancellation became an auxiliary warning: %v", result)
					}
					assertUnread()
				} else if result != nil || operation == "absence" && !absent {
					t.Fatalf("admitted auxiliary preflight failed: absent=%t error=%v", absent, result)
				}
				retire()
				primaryScanRoutingAssertNoRepeatedAuthority(t, trace)
				if state.warnings != 0 || trace.begins.Load() != 0 || trace.commits.Load() != 0 || trace.rollbacks.Load() != 0 {
					t.Fatal("preflight performed publication SQL or downgraded admission failure")
				}
				if after := originalMediaReadGovernor.Stats(); after != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
					t.Fatalf("auxiliary preflight retained capacity: before=%+v after=%+v owners=%d/%d", beforeIO, after, beforeOwners, originalMediaReadOwners.Stats().RegisteredOwners)
				}
			})
		}
	}
}

func TestAuxiliaryPreflightRetirementFailureOutranksInputWarning(t *testing.T) {
	_, _, store, state, _, _ := scanCachedVisitFixture(t)
	beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
	var retained *PrimaryRootIO
	err := store.observeScanAuxiliaryRoot(state.task, state.root, func(ctx context.Context, root *os.Root) error {
		if _, err := root.Stat("."); err != nil {
			return err
		}
		retained = PrimaryRootIOFromContext(ctx)
		if retained == nil || originalMediaReadGovernor.Stats().Active != beforeIO.Active+1 {
			return errors.New("filesystem observation did not retain actual admission")
		}
		// Simulate a failed retirement report after synchronous filesystem work.
		// The real owner and actual lease must survive the ordinary input error.
		if err := retained.MarkUnknown(errSidecarRetirementUnknown); !errors.Is(err, errSidecarRetirementUnknown) {
			return err
		}
		return fmt.Errorf("%w: an observed layout is unsupported", ErrInvalidInput)
	})
	if retained != nil {
		t.Cleanup(func() {
			if err := retained.ConfirmRetired(func() error { return nil }); err != nil && !errors.Is(err, ErrInvalidInput) {
				t.Errorf("retire auxiliary fixture evidence: %v", err)
			}
		})
	}
	if !errors.Is(err, errSidecarRetirementUnknown) || auxiliaryInputWarning(err) || retained == nil {
		t.Fatalf("retirement evidence became a successful warning: %v", err)
	}
	if after := originalMediaReadGovernor.Stats(); after.Active != beforeIO.Active+1 || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners+1 {
		t.Fatalf("failed retirement released actual ownership: before=%+v after=%+v", beforeIO, after)
	}
	// The callback and helper have returned, and both temporary roots are
	// closed. That independent join is the fixture's actual retirement proof.
	if err := retained.ConfirmRetired(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if after := originalMediaReadGovernor.Stats(); after != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
		t.Fatalf("confirmed auxiliary cleanup retained capacity: before=%+v after=%+v", beforeIO, after)
	}
}

func TestAuxiliaryWarningClassificationRejectsJoinedCleanupAndTransactionErrors(t *testing.T) {
	ordinary := fmt.Errorf("%w: unsupported complete layout", ErrInvalidInput)
	if !auxiliaryInputWarning(ordinary) || !auxiliaryInputWarning(auxiliarySourceFailure(os.ErrNotExist)) {
		t.Fatal("ordinary input instability stopped preserving the previous population")
	}
	for _, fatal := range []error{context.Canceled, context.DeadlineExceeded, errSidecarRollbackUnknown,
		errSidecarRetirementUnknown, errScanPublicationRetirementUnknown, errors.New("commit failed"),
		scanReadFailure(ErrInvalidInput), extraTransactionFailure(ErrInvalidInput)} {
		if auxiliaryInputWarning(errors.Join(ordinary, fatal)) || auxiliaryInputWarning(fmt.Errorf("wrapped: %w", errors.Join(ordinary, fatal))) {
			t.Fatalf("a joined fatal error became a layout warning: %v", fatal)
		}
	}
}

func TestAuxiliaryOwnerResolutionCancellationDoesNotBecomeWarning(t *testing.T) {
	_, _, _, state, _, _ := scanCachedVisitFixture(t)
	group := &themeDirectoryScan{relative: ".", candidates: []themeCandidate{{relative: "theme.mp3", kind: themePathKindSong}}}
	state.themes = &themeScan{groups: map[string]*themeDirectoryScan{".": group},
		owners: map[string]themeDirectoryOwner{".": {id: state.library.ID, itemType: "CollectionFolder"}}, issues: make(map[string]int)}
	state.task.cancel()
	if err := state.publishThemeDirectory("."); !errors.Is(err, context.Canceled) || state.warnings != 0 {
		t.Fatalf("owner lookup cancellation became an unsupported-layout warning: error=%v warnings=%d", err, state.warnings)
	}
}
