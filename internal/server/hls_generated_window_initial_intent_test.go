package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func generatedWindowInitialIntentFixture(t *testing.T, id string, durationSeconds int64) (*hlsRuntime, *hlsSession, library.PlaySession) {
	t.Helper()
	h, _ := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, id, false)
	plan, _, _ := generatedWindowGraphPlanFixture()
	plan.DurationTicks = durationSeconds * media.TicksPerSecond
	session.key.plan = plan
	prepared := hlsDemandTestPlay(session, 0, "Prepared", 0)
	prepared.DurationTicks = plan.DurationTicks
	prepared.UpdatedAt = time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	h.applyPlaybackSnapshot(prepared)
	return h, session, prepared
}

func generatedWindowInitialIntentPendingLookahead(t *testing.T, session *hlsSession) *hlsAdmission {
	t.Helper()
	initialPlan := session.key.plan
	initialPlan.StartTicks = 90 * media.TicksPerSecond
	initialPlan.HLS.Window = transcode.HLSWindow{StartNumber: 15, EndTicks: 96 * media.TicksPerSecond, RequireInputEvidence: true}
	// These in-memory identities exercise cancellation without starting a manager job.
	producer := hlsProducer{id: "closed-initial-window", first: 15, last: 15}
	session.producers = []hlsProducer{producer}
	session.windowGraph = &hlsGeneratedWindowGraph{published: true, basePlan: session.key.plan,
		slots: map[int]hlsGeneratedWindowBinding{15: {producer: producer, plan: initialPlan}}}
	lookaheadPlan := session.key.plan
	lookaheadPlan.StartTicks = 96 * media.TicksPerSecond
	lookaheadPlan.HLS.Window = transcode.HLSWindow{StartNumber: 16,
		EndTicks: min(102*media.TicksPerSecond, session.key.plan.DurationTicks), RequireInputEvidence: true}
	session.mu.Lock()
	pending := newHLSAdmission(context.Background(), session, transcode.Spec{Scope: session.key.scope, Plan: lookaheadPlan}, 16, 16)
	session.admission = pending
	session.mu.Unlock()
	t.Cleanup(func() { pending.stopSession(); pending.cancel() })
	return pending
}

func generatedWindowInitialIntentAssertCommitted(t *testing.T, session *hlsSession, play library.PlaySession) {
	t.Helper()
	demand := session.demand
	if !demand.initialized || demand.state != play.State || demand.position != play.PositionTicks ||
		demand.revision != play.PlaybackRevision || demand.paused != (play.State == "Paused") || !demand.updated.Equal(play.UpdatedAt) {
		t.Fatal("initial intent replaced the committed playback snapshot")
	}
}

func TestGeneratedWindowInitialIntentStartedPreservesAdjacentLookahead(t *testing.T) {
	h, session, prepared := generatedWindowInitialIntentFixture(t, "initial-started", 100)
	h.seedGeneratedWindowInitialIntent(session, prepared, 90*media.TicksPerSecond)
	generatedWindowInitialIntentAssertCommitted(t, session, prepared)
	pending := generatedWindowInitialIntentPendingLookahead(t, session)
	revision := session.admissionRevision
	graph := session.windowGraph
	producer := session.producers[0]
	started := hlsDemandTestPlay(session, 1, "Playing", 90*media.TicksPerSecond)
	started.UpdatedAt = prepared.UpdatedAt.Add(time.Second)
	h.applyPlaybackSnapshot(started)
	generatedWindowInitialIntentAssertCommitted(t, session, started)
	if session.admission != pending || pending.ctx.Err() != nil || session.admissionRevision != revision ||
		session.windowGraph != graph || session.producers[0] != producer || graph.slots[15].producer != producer {
		t.Fatal("first Started acknowledging the negotiated target canceled or replaced adjacent lookahead")
	}
	if session.demand.initialIntent.valid || !session.demand.initialIntent.captured {
		t.Fatal("first committed Started did not consume the initial intent exactly once")
	}
}

func TestGeneratedWindowInitialIntentDoesNotHideFirstSeekOrPause(t *testing.T) {
	for _, fixture := range []struct {
		name, state     string
		position        int64
		durationSeconds int64
		elapsed         time.Duration
	}{
		{"first backward seek", "Playing", 30, 100, time.Second},
		{"first forward seek", "Playing", 150, 180, time.Second},
		{"forward seek after long preparation", "Playing", 150, 180, 12 * time.Hour},
		{"first matching pause", "Paused", 90, 100, time.Second},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			h, session, prepared := generatedWindowInitialIntentFixture(t, fixture.name, fixture.durationSeconds)
			h.seedGeneratedWindowInitialIntent(session, prepared, 90*media.TicksPerSecond)
			pending := generatedWindowInitialIntentPendingLookahead(t, session)
			revision := session.admissionRevision
			graph := session.windowGraph
			producer := session.producers[0]
			current := hlsDemandTestPlay(session, 1, fixture.state, fixture.position*media.TicksPerSecond)
			current.UpdatedAt = prepared.UpdatedAt.Add(fixture.elapsed)
			h.applyPlaybackSnapshot(current)
			generatedWindowInitialIntentAssertCommitted(t, session, current)
			if !errors.Is(pending.ctx.Err(), context.Canceled) || session.admissionRevision != revision+1 {
				t.Fatal("initial intent suppressed a real first seek or committed pause")
			}
			if session.windowGraph != graph || session.producers[0] != producer || graph.slots[15].producer != producer {
				t.Fatal("canceling lookahead erased an already closed initial window")
			}
			if session.demand.initialIntent.valid || !session.demand.initialIntent.captured {
				t.Fatal("the first non-Prepared commit left initial intent reusable")
			}
		})
	}
}

func TestGeneratedWindowInitialIntentLaterSeekAndStaleSnapshotsStayFenced(t *testing.T) {
	h, session, prepared := generatedWindowInitialIntentFixture(t, "initial-later-seek", 100)
	h.seedGeneratedWindowInitialIntent(session, prepared, 90*media.TicksPerSecond)
	pending := generatedWindowInitialIntentPendingLookahead(t, session)
	started := hlsDemandTestPlay(session, 1, "Playing", 90*media.TicksPerSecond)
	started.UpdatedAt = prepared.UpdatedAt.Add(time.Second)
	h.applyPlaybackSnapshot(started)
	if pending.ctx.Err() != nil {
		t.Fatal("acknowledging initial playback canceled adjacent lookahead")
	}
	seek := hlsDemandTestPlay(session, 2, "Playing", 30*media.TicksPerSecond)
	seek.UpdatedAt = prepared.UpdatedAt.Add(2 * time.Second)
	h.applyPlaybackSnapshot(seek)
	if !errors.Is(pending.ctx.Err(), context.Canceled) {
		t.Fatal("a later committed seek reused the consumed initial intent")
	}
	revision, demand := session.admissionRevision, session.demand
	h.applyPlaybackSnapshot(started)
	ping := seek
	ping.UpdatedAt = seek.UpdatedAt.Add(time.Hour)
	h.applyPlaybackSnapshot(ping)
	h.seedGeneratedWindowInitialIntent(session, prepared, 30*media.TicksPerSecond)
	if session.demand != demand || session.admissionRevision != revision || session.admission != pending || pending.ctx.Err() == nil {
		t.Fatal("stale callbacks, Ping or registration reuse revived canceled initial demand")
	}
}

func TestGeneratedWindowInitialIntentPauseCannotBeRevivedByOldPreparationOrPing(t *testing.T) {
	h, session, prepared := generatedWindowInitialIntentFixture(t, "initial-paused", 100)
	h.seedGeneratedWindowInitialIntent(session, prepared, 90*media.TicksPerSecond)
	pending := generatedWindowInitialIntentPendingLookahead(t, session)
	paused := hlsDemandTestPlay(session, 1, "Paused", 90*media.TicksPerSecond)
	paused.UpdatedAt = prepared.UpdatedAt.Add(time.Second)
	h.applyPlaybackSnapshot(paused)
	if !errors.Is(pending.ctx.Err(), context.Canceled) || !session.demand.paused {
		t.Fatal("initial intent overrode a committed pause")
	}
	revision, demand := session.admissionRevision, session.demand
	h.applyPlaybackSnapshot(prepared)
	ping := paused
	ping.UpdatedAt = paused.UpdatedAt.Add(time.Hour)
	h.applyPlaybackSnapshot(ping)
	h.seedGeneratedWindowInitialIntent(session, prepared, 90*media.TicksPerSecond)
	if session.demand != demand || session.admissionRevision != revision || session.admission != pending || pending.ctx.Err() == nil {
		t.Fatal("an old Prepared row, Ping or repeated seed resumed paused production")
	}
}

func TestGeneratedWindowInitialIntentSeedRequiresExactPreparedOwnership(t *testing.T) {
	for name, mutate := range map[string]func(*hlsSession, *library.PlaySession, *int64){
		"another play":               func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.ID += "-foreign" },
		"another auth session":       func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.AuthSessionID += "-foreign" },
		"another user":               func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.UserID += "-foreign" },
		"another device":             func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.DeviceID += "-foreign" },
		"another item":               func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.ItemID += "-foreign" },
		"another source":             func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.MediaSourceID += "-foreign" },
		"another credential kind":    func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.ApplicationKey = !play.ApplicationKey },
		"another application client": func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.ApplicationClientID = "foreign-client" },
		"dynamic playback":           func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.IsDynamic = true },
		"reported playing state":     func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.State = "Playing" },
		"reported paused state":      func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.State = "Paused" },
		"another revision":           func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.PlaybackRevision++ },
		"another position":           func(_ *hlsSession, play *library.PlaySession, _ *int64) { play.PositionTicks++ },
		"uninitialized demand":       func(session *hlsSession, _ *library.PlaySession, _ *int64) { session.demand = hlsPlaybackDemand{} },
		"already playing demand":     func(session *hlsSession, _ *library.PlaySession, _ *int64) { session.demand.state = "Playing" },
		"legacy output": func(session *hlsSession, _ *library.PlaySession, _ *int64) {
			session.key.plan.HLS = transcode.HLSPlan{}
		},
		"progressive output": func(session *hlsSession, _ *library.PlaySession, _ *int64) {
			session.key.plan.OutputMode = "progressive"
		},
		"live output":             func(session *hlsSession, _ *library.PlaySession, _ *int64) { session.key.plan.SourceMode = "stream" },
		"unknown finite endpoint": func(session *hlsSession, _ *library.PlaySession, _ *int64) { session.key.plan.DurationTicks = 0 },
		"negative start":          func(_ *hlsSession, _ *library.PlaySession, start *int64) { *start = -1 },
		"exclusive endpoint start": func(session *hlsSession, _ *library.PlaySession, start *int64) {
			*start = session.key.plan.DurationTicks
		},
		"closed session":   func(session *hlsSession, _ *library.PlaySession, _ *int64) { session.closed = true },
		"canceled session": func(session *hlsSession, _ *library.PlaySession, _ *int64) { session.cancel() },
	} {
		t.Run(name, func(t *testing.T) {
			h, session, prepared := generatedWindowInitialIntentFixture(t, name, 100)
			start := int64(90 * media.TicksPerSecond)
			mutate(session, &prepared, &start)
			demand := session.demand
			h.seedGeneratedWindowInitialIntent(session, prepared, start)
			if session.demand != demand {
				t.Fatal("unowned or stale preparation captured or changed initial demand")
			}
		})
	}
}

func TestGeneratedWindowInitialIntentCaptureCannotBeRebound(t *testing.T) {
	for _, start := range []int64{0, 90 * media.TicksPerSecond} {
		h, session, prepared := generatedWindowInitialIntentFixture(t, "initial-capture", 100)
		h.seedGeneratedWindowInitialIntent(session, prepared, start)
		intent := hlsGeneratedWindowInitialIntent{position: start, revision: prepared.PlaybackRevision, valid: true, captured: true}
		if session.demand.initialIntent != intent {
			t.Fatal("a valid Prepared registration did not capture its independent initial intent")
		}
		ping := prepared
		ping.UpdatedAt = prepared.UpdatedAt.Add(time.Hour)
		h.applyPlaybackSnapshot(ping)
		h.seedGeneratedWindowInitialIntent(session, prepared, 30*media.TicksPerSecond)
		if session.demand.initialIntent != intent || session.demand.position != prepared.PositionTicks || !session.demand.updated.Equal(prepared.UpdatedAt) {
			t.Fatal("same-revision authorization or registration reuse replaced initial intent or committed position")
		}
		replacement := prepared
		replacement.PlaybackRevision++
		replacement.UpdatedAt = prepared.UpdatedAt.Add(2 * time.Hour)
		h.applyPlaybackSnapshot(replacement)
		if session.demand.initialIntent.valid || !session.demand.initialIntent.captured {
			t.Fatal("a different Prepared revision retained old intent or forgot prior capture")
		}
		demand := session.demand
		h.seedGeneratedWindowInitialIntent(session, replacement, 30*media.TicksPerSecond)
		if session.demand != demand {
			t.Fatal("a consumed capture was rebound to a different preparation revision")
		}
	}
}

func TestGeneratedWindowInitialComparisonUsesOnlyOnePreparedIntentWithoutMutation(t *testing.T) {
	stamp := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	previous := hlsPlaybackDemand{initialized: true, state: "Prepared", revision: 0, position: 0, updated: stamp,
		initialIntent: hlsGeneratedWindowInitialIntent{position: 90 * media.TicksPerSecond, revision: 0, valid: true, captured: true}}
	current := hlsPlaybackDemand{initialized: true, state: "Playing", revision: 1, position: 150 * media.TicksPerSecond, updated: stamp.Add(12 * time.Hour)}
	previousBefore, currentBefore := previous, current
	comparison := hlsGeneratedWindowInitialComparison(previous, current)
	expected := previous
	expected.position, expected.updated = previous.initialIntent.position, current.updated
	if comparison != expected || previous != previousBefore || current != currentBefore {
		t.Fatal("initial comparison mutated inputs or substituted fields beyond its comparison baseline")
	}
	if !hlsGeneratedReportSeek(comparison, current, 6) {
		t.Fatal("time spent Prepared was counted as observed playback and concealed a forward seek")
	}
	for name, mutate := range map[string]func(*hlsPlaybackDemand, *hlsPlaybackDemand){
		"uninitialized previous": func(previous *hlsPlaybackDemand, _ *hlsPlaybackDemand) { previous.initialized = false },
		"previous playing":       func(previous *hlsPlaybackDemand, _ *hlsPlaybackDemand) { previous.state = "Playing" },
		"previous paused": func(previous *hlsPlaybackDemand, _ *hlsPlaybackDemand) {
			previous.state, previous.paused = "Paused", true
		},
		"invalid intent":          func(previous *hlsPlaybackDemand, _ *hlsPlaybackDemand) { previous.initialIntent.valid = false },
		"another intent revision": func(previous *hlsPlaybackDemand, _ *hlsPlaybackDemand) { previous.initialIntent.revision++ },
		"current prepared":        func(_ *hlsPlaybackDemand, current *hlsPlaybackDemand) { current.state = "Prepared" },
		"current paused":          func(_ *hlsPlaybackDemand, current *hlsPlaybackDemand) { current.state, current.paused = "Paused", true },
		"same revision":           func(previous *hlsPlaybackDemand, current *hlsPlaybackDemand) { current.revision = previous.revision },
		"older revision": func(previous *hlsPlaybackDemand, current *hlsPlaybackDemand) {
			current.revision = previous.revision - 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			left, right := previous, current
			mutate(&left, &right)
			leftBefore, rightBefore := left, right
			if got := hlsGeneratedWindowInitialComparison(left, right); got != left || left != leftBefore || right != rightBefore {
				t.Fatal("an unrelated or stale snapshot borrowed the initial comparison baseline")
			}
		})
	}
}

func TestGeneratedWindowInitialIntentCannotChangeLegacySeekPolicy(t *testing.T) {
	h, session, prepared := generatedWindowInitialIntentFixture(t, "initial-legacy", 100)
	session.key.plan.HLS = transcode.HLSPlan{}
	session.demand.initialIntent = hlsGeneratedWindowInitialIntent{position: 90 * media.TicksPerSecond, revision: 0, valid: true, captured: true}
	pending := generatedWindowInitialIntentPendingLookahead(t, session)
	started := hlsDemandTestPlay(session, 1, "Playing", 90*media.TicksPerSecond)
	started.UpdatedAt = prepared.UpdatedAt.Add(time.Second)
	h.applyPlaybackSnapshot(started)
	if !errors.Is(pending.ctx.Err(), context.Canceled) {
		t.Fatal("generated initial intent bypassed the legacy committed seek fence")
	}
	generatedWindowInitialIntentAssertCommitted(t, session, started)
}
