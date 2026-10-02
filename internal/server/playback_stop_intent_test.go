package server

import (
	"errors"
	"fmt"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func playbackStopIntentTestScope(play string) transcode.Scope {
	return transcode.Scope{UserID: "intent-user", AuthSessionID: "intent-auth", DeviceID: "intent-device",
		PlaySessionID: play, ItemID: "intent-item", SourceID: "intent-source"}
}

func TestPlaybackStopIntentRetainsConsumedAndForkedOwners(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-first")
	parent, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	child, err := parent.fork(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parent.fork(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("a stopped owner forked another input")
	}
	if _, err := gate.acquireValidated(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("an unresolved stopped scope acquired a new owner")
	}
	stop.finish(true)
	staleParentCopy := *parent
	parent.release()
	staleParentCopy.release()
	usage := gate.usage()
	if usage.Entries != 1 || usage.References != 1 || usage.StopReservations != 0 || !gate.blocked(scope) {
		t.Fatal("durable terminal alone retired the child's real lifetime")
	}
	child.release()
	if usage := gate.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("the final consumed owner retained a terminal entry")
	}
	if _, err := staleParentCopy.fork(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("a released handle reconstructed the collected generation")
	}
	// This is a different canonical play. No DB authorization is claimed by
	// the primitive; a future production caller must still freshly validate it.
	fresh, err := gate.acquireValidated(playbackStopIntentTestScope("play-next"))
	if err != nil {
		t.Fatal("a terminal old play blocked a separately validated play")
	}
	fresh.release()
}

func TestPlaybackStopIntentFailedCommitReusesBoundedStopLane(t *testing.T) {
	var gate playbackStopIntentGate
	for index := 0; index < maxPlaybackStopReservations; index++ {
		stop, err := gate.acceptValidatedStop(playbackStopIntentTestScope(fmt.Sprintf("play-pending-%d", index)))
		if err != nil {
			t.Fatal(err)
		}
		stop.finish(false)
	}
	if _, err := gate.acceptValidatedStop(playbackStopIntentTestScope("play-over-stop-lane")); !errors.Is(err, transcode.ErrBusy) {
		t.Fatal("uncommitted stops grew beyond Control4")
	}
	unrelated, err := gate.acquireValidated(playbackStopIntentTestScope("play-unrelated-media"))
	if err != nil {
		t.Fatal("full Stop lane globally shut down normal media")
	}
	unrelated.release()
	retry, err := gate.acceptValidatedStop(playbackStopIntentTestScope("play-pending-0"))
	if err != nil || gate.usage().StopReservations != maxPlaybackStopReservations {
		t.Fatal("a same-play fresh retry required a fifth Stop reservation")
	}
	retryCopy := *retry
	retry.finish(true)
	retryCopy.finish(false)
	if gate.usage().StopReservations != maxPlaybackStopReservations-1 {
		t.Fatal("durable retry did not release exactly one Stop reservation")
	}
	newStop, err := gate.acceptValidatedStop(playbackStopIntentTestScope("play-new-stop"))
	if err != nil {
		t.Fatal("a completed retry kept the Stop lane saturated")
	}
	newStop.finish(true)
	if gate.close() != true || gate.usage().Entries != 0 {
		t.Fatal("closed-gate cleanup retained ownerless failed intents")
	}
}

func TestPlaybackStopIntentOwnsExactCanonicalScope(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-owned")
	owner, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.release()
	foreign := scope
	foreign.SourceID = "different-source"
	if _, err := gate.acceptValidatedStop(foreign); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("a different source cancelled the established canonical owner")
	}
	if _, err := owner.fork(foreign); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("a reference moved to another source")
	}
	if gate.blocked(scope) {
		t.Fatal("a rejected foreign stop changed the owner")
	}
	invalid := scope
	invalid.PlaySessionID = ""
	if _, err := gate.acceptValidatedStop(invalid); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("a missing canonical play accepted a terminal intent")
	}
	invalid = scope
	invalid.ApplicationKey = true
	if _, err := gate.acquireValidated(invalid); !errors.Is(err, transcode.ErrInvalidScope) {
		t.Fatal("credential-kind shape was treated as an owned scope")
	}
}

func TestPlaybackStopIntentReferenceBoundDoesNotAccumulateHistory(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-capacity")
	owners := make([]*playbackAdmissionReference, 0, maxPlaybackAdmissionReferences)
	for range maxPlaybackAdmissionReferences {
		owner, err := gate.acquireValidated(scope)
		if err != nil {
			t.Fatal(err)
		}
		owners = append(owners, owner)
	}
	if _, err := gate.acquireValidated(scope); !errors.Is(err, transcode.ErrBusy) {
		t.Fatal("actual lifetime references escaped their independent bound")
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal("normal-owner pressure consumed the separate Stop lane")
	}
	stop.finish(true)
	for _, owner := range owners {
		owner.release()
	}
	for index := 0; index < 2*maxPlaybackIntentEntries; index++ {
		owner, err := gate.acquireValidated(playbackStopIntentTestScope(fmt.Sprintf("play-reclaimed-%d", index)))
		if err != nil {
			t.Fatal("joined lifetimes accumulated a generation-wide history limit")
		}
		owner.release()
	}
	if usage := gate.usage(); usage != (playbackStopIntentUsage{}) {
		t.Fatal("joined scopes retained identifier bytes or map entries")
	}
}

func TestPlaybackStopIntentCloseWaitsForActualOwners(t *testing.T) {
	var gate playbackStopIntentGate
	scope := playbackStopIntentTestScope("play-closing")
	owner, err := gate.acquireValidated(scope)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := gate.acceptValidatedStop(scope)
	if err != nil {
		t.Fatal(err)
	}
	if gate.close() {
		t.Fatal("Close claimed join while real references were outstanding")
	}
	if _, err := owner.fork(scope); !errors.Is(err, transcode.ErrJobCancelled) {
		t.Fatal("closing ownership forked another admission")
	}
	stop.finish(false)
	owner.release()
	if !gate.close() || gate.usage() != (playbackStopIntentUsage{Closing: true}) {
		t.Fatal("final owner cleanup did not complete the closed gate")
	}
}

func TestPlaybackStopIntentConcurrentForkAndReleaseRetainsExactOwner(t *testing.T) {
	var gate playbackStopIntentGate
	for index := 0; index < 64; index++ {
		scope := playbackStopIntentTestScope(fmt.Sprintf("play-fork-release-%d", index))
		parent, err := gate.acquireValidated(scope)
		if err != nil {
			t.Fatal(err)
		}
		type outcome struct {
			child *playbackAdmissionReference
			err   error
		}
		done := make(chan outcome, 1)
		joined := make(chan struct{})
		go func() {
			defer close(joined)
			child, err := parent.fork(scope)
			done <- outcome{child: child, err: err}
		}()
		parent.release()
		result := <-done
		<-joined
		if result.err != nil && !errors.Is(result.err, transcode.ErrJobCancelled) {
			t.Fatal("a fork/release race returned an unrelated failure")
		}
		stop, err := gate.acceptValidatedStop(scope)
		if err != nil {
			t.Fatal(err)
		}
		stop.finish(true)
		if result.child != nil {
			if usage := gate.usage(); usage.References != 1 || usage.Entries != 1 || !gate.blocked(scope) {
				t.Fatal("a successful fork was not retained after parent release and durable stop")
			}
			result.child.release()
		}
		if gate.usage() != (playbackStopIntentUsage{}) {
			t.Fatal("a joined fork/release race leaked or double-released its owner")
		}
	}
}
