package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

func TestPlaybackStopRecoveryMaintenanceBudgetMatchesEligibleReservations(t *testing.T) {
	for _, reservations := range []int{0, 1, 4} {
		t.Run(fmt.Sprint(reservations), func(t *testing.T) {
			runtime := &hlsRuntime{server: &Server{
				library: &library.Store{}, correlatedHLSOwnershipEnabled: true, correlatedHLSEarlyStopEnabled: true,
			}}
			for index := 0; index < reservations; index++ {
				stop, err := runtime.server.playbackStopIntents.acceptValidatedStop(playbackStopIntentTestScope(fmt.Sprint("budget-", index)))
				if err != nil {
					t.Fatal(err)
				}
				stop.finish(false)
			}
			cycle, cancelCycle := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancelCycle()
			checks, cancelChecks := runtime.sessionMaintenanceBeforeStopRecovery(cycle)
			defer cancelChecks()
			cycleDeadline, _ := cycle.Deadline()
			checksDeadline, bounded := checks.Deadline()
			if !bounded || cycleDeadline.Sub(checksDeadline) != time.Duration(reservations)*750*time.Millisecond {
				t.Fatalf("eligible reservations did not retain their statement budget: reservations=%d reserved=%v bounded=%v",
					reservations, cycleDeadline.Sub(checksDeadline), bounded)
			}
			if reservations == 0 && checks != cycle {
				t.Fatal("an empty recovery set changed the active-session context")
			}
			cancelChecks()
			if cycle.Err() != nil {
				t.Fatal("finishing active-session checks cancelled the recovery opportunity")
			}
		})
	}
}

func TestPlaybackStopRecoveryCommitFencesCurrentLateOwnersWithoutMint(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("late-commit")
	gate.publishCommittedTerminal(scope)
	if gate.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("commit-only publication minted an entry from a scope")
	}
	// The factory can see no entry, while a later fresh Get creates an owner
	// during the actual report's userdata wait. Publication must look it up now.
	late, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	gate.publishCommittedTerminal(scope)
	if !gate.blocked(scope) || gate.usage().References != 1 || gate.usage().StopReservations != 0 {
		t.Fatal("the commit fence lost a late real owner or minted a Stop reservation")
	}
	if _, err := late.fork(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("late metadata forked after the committed terminal fence")
	}
	if _, err := gate.acquireValidated(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("old-scope admission bypassed the committed fence")
	}
	late.release()
	if gate.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("the actual final owner did not collect its committed entry")
	}
}

func TestPlaybackStopRecoveryBoundAndFinalOwnerChecks(t *testing.T) {
	var gate playbackStopIntentGate
	for index := 0; index < maxPlaybackStopReservations; index++ {
		scope := playbackStopIntentTestScope(fmt.Sprint("failed-", index))
		owner, err := gate.acquireValidated(scope)
		if err != nil {
			t.Fatal(err)
		}
		stop, err := gate.acceptValidatedStop(scope)
		if err != nil {
			t.Fatal(err)
		}
		owner.release()
		stop.finish(false)
	}
	candidates := gate.recoveryCandidates()
	if gate.usage().StopReservations != 4 || len(candidates) != 4 {
		t.Fatal("failed intents escaped the existing four-slot bound")
	}
	for _, candidate := range candidates {
		if candidate.entry == nil {
			t.Fatal("an eligible actual entry was not returned")
		}
	}
	first := candidates[0]
	retry, err := gate.acceptValidatedStop(first.scope)
	if err != nil {
		t.Fatal(err)
	}
	if gate.recoverTerminal(first) {
		t.Fatal("a concurrent Stop retry was collected before its actual join")
	}
	retry.finish(false)
	foreign := first
	foreign.scope.SourceID = "different-source"
	if gate.recoverTerminal(foreign) {
		t.Fatal("a foreign witness retired an exact scoped entry")
	}
	if !gate.recoverTerminal(first) || gate.usage().StopReservations != 3 {
		t.Fatal("an actual ownerless terminal witness did not recover its one slot")
	}
	if gate.recoverTerminal(first) {
		t.Fatal("a stale candidate claimed a second collection")
	}
	for _, candidate := range candidates[1:] {
		if !gate.recoverTerminal(candidate) {
			t.Fatal("an independent terminal witness did not collect")
		}
	}
	if gate.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("bounded terminal recovery accumulated history")
	}
}

func TestPlaybackStopRecoveryNeverSelectsHeldOrActiveOwners(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("held-source")
	owner, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop.finish(false)
	for _, candidate := range gate.recoveryCandidates() {
		if candidate.entry != nil {
			t.Fatal("a held actual source owner became a recovery candidate")
		}
	}
	gate.mu.Lock()
	candidate := playbackStopRecoveryCandidate{entry: owner.entry, scope: scope}
	gate.mu.Unlock()
	if gate.recoverTerminal(candidate) {
		t.Fatal("a deliberately retained source owner was collected")
	}
	owner.release()
	if !gate.recoverTerminal(candidate) {
		t.Fatal("actual owner release did not permit the separately proven terminal recovery")
	}
}
