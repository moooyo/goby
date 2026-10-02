package server

import (
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const hlsProductionLookaheadTicks = 60 * media.TicksPerSecond

// PlaybackRevision orders callbacks after database commit. Emby report and GET
// requests contain no client demand generation; their network order cannot be
// reconstructed from this revision or from a segment number.
type hlsPlaybackDemand struct {
	initialized   bool
	revision      int64
	state         string
	paused        bool
	position      int64
	updated       time.Time
	initialIntent hlsGeneratedWindowInitialIntent
}

func hlsLegacyDemandPlan(plan transcode.Plan) bool {
	return plan.OutputMode == "" && plan.SourceMode == "" && !transcode.GeneratedHLS(plan)
}

// applyPlaybackSnapshot receives only a currently authorized, committed row.
// It also runs before source I/O on each HLS authorization, so a registration
// created after a pause inherits that pause without another database query.
func (h *hlsRuntime) applyPlaybackSnapshot(play library.PlaySession) {
	if h == nil || play.ID == "" || play.IsDynamic || play.PlaybackRevision < 0 {
		return
	}
	scope := transcode.Scope{PlaySessionID: play.ID, AuthSessionID: play.AuthSessionID, UserID: play.UserID,
		DeviceID: play.DeviceID, ApplicationKey: play.ApplicationKey, ApplicationClientID: play.ApplicationClientID,
		ItemID: play.ItemID, SourceID: play.MediaSourceID}
	var matching [maxHLSSessions]*hlsSession
	count := 0
	h.mu.Lock()
	registrations := h.byScope[scope]
	if h.byScope == nil {
		// Compatibility for internal adapters that construct their own registry.
		registrations = h.sessions
	}
	for _, session := range registrations {
		if session.key.scope == scope && count < len(matching) {
			matching[count] = session
			count++
		}
	}
	h.mu.Unlock()
	for _, session := range matching[:count] {
		session.mu.Lock()
		if session.closed || session.demand.initialized && play.PlaybackRevision <= session.demand.revision {
			session.mu.Unlock()
			continue
		}
		previous := session.demand
		intent := previous.initialIntent
		if play.State != "Prepared" || play.PlaybackRevision != intent.revision {
			intent.valid = false
		}
		session.demand = hlsPlaybackDemand{initialized: true, revision: play.PlaybackRevision,
			state: play.State, paused: play.State == "Paused", position: play.PositionTicks, updated: play.UpdatedAt, initialIntent: intent}
		seek := previous.initialized && (play.PositionTicks < previous.position-hlsProductionLookaheadTicks ||
			play.PositionTicks > previous.position+hlsProductionLookaheadTicks)
		if !hlsLegacyDemandPlan(session.key.plan) {
			privateWindow := session.windowGraph != nil && !session.windowGraph.fallback || session.admission != nil &&
				(session.admission.first == hlsGeneratedGraphPreparation || session.admission.spec.Plan.HLS.Window.RequireInputEvidence)
			if privateWindow {
				seek = hlsGeneratedReportSeek(hlsGeneratedWindowInitialComparison(previous, session.demand), session.demand, session.key.plan.SegmentSeconds)
				if pending := session.admission; seek && pending != nil {
					start, end := pending.spec.Plan.StartTicks, pending.spec.Plan.HLS.Window.EndTicks
					if pending.first == hlsGeneratedGraphPreparation {
						start, end = pending.anchorTicks, pending.anchorEndTicks
					}
					// A newly committed report may acknowledge the requested target
					// of the work already in flight. Preserve that exact interval.
					seek = play.PositionTicks < start || play.PositionTicks >= end
				}
			}
			transition := session.demand.paused || privateWindow && (seek || previous.initialized && previous.paused != session.demand.paused)
			if session.key.plan.OutputMode == "" && session.key.plan.SourceMode == "" && transition {
				// A generated graph may finish its already advertised producer.
				// It cannot admit a new producer after a committed pause. Its
				// cancellation-tail contract is separate from legacy VOD sealing.
				session.admissionRevision++
				if session.admission != nil {
					session.admission.cancel()
				}
			}
			session.accessed = time.Now()
			session.mu.Unlock()
			continue
		}
		transition := session.demand.paused || previous.initialized && previous.paused != session.demand.paused || seek
		if transition {
			// Fence late admission results before releasing any production lease.
			session.admissionRevision++
			if session.admission != nil {
				session.admission.cancel()
			}
			for _, producer := range session.producers {
				h.sealProducerDemand(scope, producer)
			}
		}
		session.accessed = time.Now()
		session.mu.Unlock()
	}
}

// The server's committed report timestamps distinguish ordinary delayed
// progress from a forward position jump. Backward movement beyond one slot is
// a changed interval. These facts do not identify an unsequenced old GET.
func hlsGeneratedReportSeek(previous, current hlsPlaybackDemand, seconds int) bool {
	if !previous.initialized || seconds < 1 || seconds > 10 {
		return false
	}
	margin := int64(seconds) * media.TicksPerSecond
	delta := current.position - previous.position
	if delta < -margin {
		return true
	}
	if delta <= margin {
		return false
	}
	if previous.state == "Prepared" {
		// Waiting before the first actual play report is not observed playback.
		return true
	}
	if previous.updated.IsZero() || current.updated.IsZero() || current.updated.Before(previous.updated) {
		return delta > hlsProductionLookaheadTicks
	}
	elapsed := current.updated.Sub(previous.updated)
	if elapsed > 30*24*time.Hour {
		return false
	}
	expected := elapsed.Nanoseconds()/100 + margin
	return delta > expected
}

// sealProducerDemand retains the artifact ownership lease. The last production
// lease stops the encoder, while cache references and readers stay independent.
func (h *hlsRuntime) sealProducerDemand(scope transcode.Scope, producer hlsProducer) {
	key := hlsProducerKey{scope: scope, id: producer.id}
	h.producerMu.Lock()
	defer h.producerMu.Unlock()
	ownership := producer.ownership
	if ownership == nil || ownership.key != key || ownership.released || !ownership.production {
		return
	}
	ownership.production = false
	h.producerDemands[key]--
	if h.producerDemands[key] == 0 {
		delete(h.producerDemands, key)
		h.sealProducerJobLocked(scope, producer.id)
	}
}

func (h *hlsRuntime) producerProducing(producer hlsProducer) bool {
	h.producerMu.Lock()
	defer h.producerMu.Unlock()
	return producer.ownership != nil && !producer.ownership.released && producer.ownership.production
}

func (h *hlsRuntime) sealProducerJobLocked(scope transcode.Scope, id string) {
	state, err := h.manager.Snapshot(scope, id)
	if err == nil && state.State != "queued" && state.State != "running" {
		return
	}
	if err == nil && (state.Spec.Plan.OutputMode != "" || state.Spec.Plan.SourceMode != "" || transcode.GeneratedHLS(state.Spec.Plan)) {
		return
	}
	if sealer, ok := h.manager.(interface {
		SealProduction(string, transcode.Scope) error
	}); ok {
		if err := sealer.SealProduction(id, scope); err == nil {
			return
		}
	}
	// Adapters without retained-output sealing must still stop production. The
	// concrete manager implements the cache-preserving contract separately.
	_ = h.manager.CancelJob(id, scope)
}

// A copied GOP can exceed the time budget. Preserve the requested authoritative
// segment intact and bound only additional lookahead rather than inventing a cut.
func hlsProductionLast(timeline transcode.Timeline, first int) int {
	last := first
	end := timeline.Segments[first].StartTicks + hlsProductionLookaheadTicks
	for next := first + 1; next < len(timeline.Segments) && next < first+hlsProducerSpan; next++ {
		segment := timeline.Segments[next]
		if segment.StartTicks+segment.DurationTicks > end {
			break
		}
		last = next
	}
	return last
}

// makeProducerRoomLocked requires session.mu. An unsequenced GET may evict a
// retained terminal window, but cannot evict a newer active production demand.
func (h *hlsRuntime) makeProducerRoomLocked(session *hlsSession, retainedIDs ...string) bool {
	if session.windowGraph != nil && !session.windowGraph.fallback && session.windowGraph.published {
		return h.makeGeneratedWindowRoomLocked(session, time.Now())
	}
	if len(session.producers) < maxHLSProducers {
		return true
	}
	for index, producer := range session.producers {
		released := h.producerReleased(producer)
		state, err := h.manager.Snapshot(session.key.scope, producer.id)
		if !released && err == nil && (state.State == "queued" || state.State == "running") {
			continue
		}
		h.releaseProducer(session.key.scope, producer)
		retainClock := false
		for _, id := range retainedIDs {
			retainClock = retainClock || id == producer.id
		}
		for otherIndex, other := range session.producers {
			retainClock = retainClock || otherIndex != index && other.id == producer.id && !h.producerReleased(other)
		}
		if !retainClock {
			delete(session.subtitleProducerClocks, producer.id)
		}
		copy(session.producers[index:], session.producers[index+1:])
		session.producers[len(session.producers)-1] = hlsProducer{}
		session.producers = session.producers[:len(session.producers)-1]
		return true
	}
	return false
}
