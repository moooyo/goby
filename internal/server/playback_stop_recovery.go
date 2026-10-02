package server

import (
	"context"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

const playbackStopRecoveryCheckTimeout = 750 * time.Millisecond

// publishCommittedTerminal is called only by Finish(true) of a fully committed
// validated report. It looks up the current entry, never an early snapshot,
// and cannot mint a lifetime or undurable reservation from bare scope strings.
func (gate *playbackStopIntentGate) publishCommittedTerminal(scope transcode.Scope) {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry := gate.entries[playbackStopIntentKeyFor(scope)]
	if entry == nil || entry.scope != scope {
		return
	}
	entry.stopped, entry.terminal = true, true
	if entry.stopReserved {
		entry.stopReserved = false
		gate.stopReserved--
	}
	gate.collectLocked(entry)
}

type playbackStopRecoveryCandidate struct {
	entry *playbackStopIntentEntry
	scope transcode.Scope
}

// No candidate owns media or authorization. The pointer allows the final
// memory check to reject a stale observation of a replaced/collected entry.
func (gate *playbackStopIntentGate) recoveryCandidates() [maxPlaybackStopReservations]playbackStopRecoveryCandidate {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	var candidates [maxPlaybackStopReservations]playbackStopRecoveryCandidate
	if gate.closing {
		return candidates
	}
	next := 0
	for _, entry := range gate.entries {
		if entry.stopped && !entry.terminal && entry.stopReserved && entry.references == 0 && entry.stopOwners == 0 {
			if next == len(candidates) {
				break
			}
			candidates[next] = playbackStopRecoveryCandidate{entry: entry, scope: entry.scope}
			next++
		}
	}
	return candidates
}

func (gate *playbackStopIntentGate) recoverTerminal(candidate playbackStopRecoveryCandidate) bool {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry := gate.entries[playbackStopIntentKeyFor(candidate.scope)]
	if candidate.entry == nil || entry != candidate.entry || entry.scope != candidate.scope || gate.closing ||
		!entry.stopped || entry.terminal || !entry.stopReserved || entry.references != 0 || entry.stopOwners != 0 {
		return false
	}
	entry.terminal = true
	gate.collectLocked(entry)
	return true
}

// Both phases share the existing cycle and workers. Eligible stopped lifetimes
// reserve their bounded statement windows before active-session AUTH can spend
// the whole cycle. maintainSessions still sweeps idle owners before any SQL.
func (h *hlsRuntime) maintainPlaybackCycle(cycle context.Context, sessions []*hlsSession) {
	checks, cancelChecks := h.sessionMaintenanceBeforeStopRecovery(cycle)
	h.maintainSessions(checks, sessions)
	cancelChecks()
	h.recoverPlaybackStopIntents(cycle)
}

func (h *hlsRuntime) sessionMaintenanceBeforeStopRecovery(cycle context.Context) (context.Context, context.CancelFunc) {
	unchanged := func() (context.Context, context.CancelFunc) { return cycle, func() {} }
	if h == nil || h.server == nil || !h.server.correlatedHLSOwnershipEnabled || !h.server.correlatedHLSEarlyStopEnabled || h.server.library == nil {
		return unchanged()
	}
	deadline, bounded := cycle.Deadline()
	if !bounded {
		return unchanged()
	}
	count := 0
	for _, candidate := range h.server.playbackStopIntents.recoveryCandidates() {
		if candidate.entry != nil {
			count++
		}
	}
	if count == 0 {
		return unchanged()
	}
	return context.WithDeadline(cycle, deadline.Add(-time.Duration(count)*playbackStopRecoveryCheckTimeout))
}

// This runs sequentially inside the existing maintenance worker and its cycle.
// At most four single-statement ordinary Data queries use existing capacity;
// there is no Control borrow, worker, TTL reset or admission from this witness.
func (h *hlsRuntime) recoverPlaybackStopIntents(cycle context.Context) {
	if h == nil || h.server == nil || !h.server.correlatedHLSOwnershipEnabled || !h.server.correlatedHLSEarlyStopEnabled || h.server.library == nil {
		return
	}
	gate := &h.server.playbackStopIntents
	for _, candidate := range gate.recoveryCandidates() {
		if candidate.entry == nil || cycle.Err() != nil {
			return
		}
		scope := candidate.scope
		check, cancel := context.WithTimeout(cycle, playbackStopRecoveryCheckTimeout)
		terminal, err := h.server.library.ConfirmOwnedPlaybackTerminal(check, library.PlaybackOwner{UserID: scope.UserID,
			SessionID: scope.AuthSessionID, DeviceID: scope.DeviceID, ApplicationKey: scope.ApplicationKey,
			ApplicationClientID: scope.ApplicationClientID}, scope.PlaySessionID, scope.ItemID, scope.SourceID)
		cancel()
		if err == nil && terminal {
			gate.recoverTerminal(candidate)
		}
	}
}
