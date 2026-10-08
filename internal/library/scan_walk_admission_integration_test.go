//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanWalkWaitsForDirectoryAdmission(t *testing.T) {
	for _, cancelWalk := range []bool{false, true} {
		name := "released"
		if cancelWalk {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			ctx, _, store, state, trace, _ := scanCachedVisitFixture(t)
			row, err := state.readPrimaryScanAuthority(ctx)
			if err != nil {
				t.Fatal(err)
			}
			state.walkRow = row
			pass, err := store.prepareScanReconciliation(state.task, []libraryRoot{state.root})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := pass.Close(); err != nil {
					t.Errorf("retire directory observation evidence: %v", err)
				}
			})
			state.reconciliationPass, state.reconciliation = pass, pass.collector()
			if state.reconciliation == nil {
				t.Fatal("fixture did not retain complete directory evidence")
			}
			beforeDirectories := state.reconciliation.directories
			beforeScanned := state.task.job.Scanned
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			unblock, retire := cachedObservationBlockBackground(t, ctx, state)
			assertUnread := primaryScanRoutingWatchSource(t, state.root.path)
			trace.reset()
			done := make(chan struct{})
			var walkErr error
			t.Cleanup(func() {
				state.task.cancel()
				unblock()
				mediaSourceAdmissionTestWait(t, done, "blocked directory walker cleanup")
			})
			go func() {
				walkErr = state.walk(".", hierarchy{parentID: state.library.ID}, 0)
				close(done)
			}()
			wait, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			primaryReadTestWaitQueued(t, wait, beforeIO.Queued+1)
			assertUnread()
			if trace.begins.Load() != 0 || trace.itemRows.Load() != 0 || trace.metadataRows.Load() != 0 {
				t.Fatal("blocked directory admission started catalog publication")
			}
			if owners := originalMediaReadOwners.Stats().RegisteredOwners; owners != beforeOwners+3 {
				t.Fatalf("directory waiter lost its retained operation: owners=%d want=%d", owners, beforeOwners+3)
			}
			if cancelWalk {
				state.task.cancel()
				mediaSourceAdmissionTestWait(t, done, "cancelled directory waiter")
				assertUnread()
				if !errors.Is(walkErr, context.Canceled) || state.reconciliation.directories != beforeDirectories {
					t.Fatalf("cancelled directory waiter observed source data: directories=%d error=%v", state.reconciliation.directories, walkErr)
				}
			}
			unblock()
			mediaSourceAdmissionTestWait(t, done, "admitted directory walk")
			retire()
			if !cancelWalk {
				if walkErr != nil || state.warnings != 0 || state.task.job.Scanned != beforeScanned+1 {
					t.Fatalf("admitted directory walk did not inspect its one media file: job=%+v warnings=%d error=%v", state.task.job, state.warnings, walkErr)
				}
				if err := state.reconciliation.requireComplete(ctx); err != nil {
					t.Fatalf("admitted directory observations did not retain their phase context: %v", err)
				}
			}
			if state.walkIO != nil || originalMediaReadGovernor.Stats() != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
				t.Fatalf("directory walk retained admission after completion: IO=%+v owners=%+v", originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats())
			}
			primaryScanRoutingAssertNoRepeatedAuthority(t, trace)
		})
	}
}

func TestScanRenameCandidateWaitsForSourceAdmission(t *testing.T) {
	for _, scope := range []string{"same_root", "cross_root", "cancelled"} {
		t.Run(scope, func(t *testing.T) {
			ctx, _, store, approved, userID := libraryIntegrationStore(t, &libraryFixtureProber{})
			original := libraryIntegrationFile(t, approved, "first/Original.mp4", "video:rename-admission")
			second := filepath.Join(approved, "second")
			if err := os.Mkdir(second, 0700); err != nil {
				t.Fatal(err)
			}
			library, err := store.CreateLibrary(ctx, "Rename admission", "movies", []string{filepath.Dir(original), second})
			if err != nil {
				t.Fatal(err)
			}
			libraryIntegrationScan(t, ctx, store, library.ID, "Completed")
			previous := nfoCatalogItem(t, ctx, store, userID, library.ID, original)
			destinationRoot := filepath.Dir(original)
			if scope == "cross_root" {
				destinationRoot = second
			}
			destination := filepath.Join(destinationRoot, "Renamed.mp4")
			if err := os.Rename(original, destination); err != nil {
				t.Fatal(err)
			}
			state := scanClaimLookupState(t, ctx, store, library, destinationRoot)
			scanClaimLookupAuthorize(t, state)
			state.walkRow, err = state.readPrimaryScanAuthority(ctx)
			if err != nil {
				t.Fatal(err)
			}
			info := scanClaimLookupInfo(t, destination)
			beforeIO, beforeOwners := originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats().RegisteredOwners
			unblock, retire := cachedObservationBlockBackground(t, ctx, state)
			assertUnread := primaryScanRoutingWatchSource(t, filepath.Dir(original))
			done := make(chan struct{})
			var stored storedFile
			var lookupErr error
			t.Cleanup(func() {
				state.task.cancel()
				unblock()
				mediaSourceAdmissionTestWait(t, done, "rename candidate waiter cleanup")
			})
			go func() {
				stored, lookupErr = state.findStoredFileForRole("Renamed.mp4", info, scannedRoleOrdinary)
				close(done)
			}()
			wait, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			primaryReadTestWaitQueued(t, wait, beforeIO.Queued+1)
			assertUnread()
			if scope == "cancelled" {
				state.task.cancel()
				mediaSourceAdmissionTestWait(t, done, "cancelled rename candidate waiter")
				assertUnread()
				if !errors.Is(lookupErr, context.Canceled) || stored.id != "" {
					t.Fatalf("cancelled rename candidate transferred identity: id=%q error=%v", stored.id, lookupErr)
				}
			}
			unblock()
			mediaSourceAdmissionTestWait(t, done, "admitted rename candidate observation")
			retire()
			if scope != "cancelled" && (lookupErr != nil || stored.id != previous.ID) {
				t.Fatalf("admitted rename candidate lost identity: id=%q want=%q error=%v", stored.id, previous.ID, lookupErr)
			}
			if originalMediaReadGovernor.Stats() != beforeIO || originalMediaReadOwners.Stats().RegisteredOwners != beforeOwners {
				t.Fatalf("rename candidate retained I/O ownership: IO=%+v owners=%+v", originalMediaReadGovernor.Stats(), originalMediaReadOwners.Stats())
			}
		})
	}
}
