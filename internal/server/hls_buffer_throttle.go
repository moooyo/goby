package server

import (
	"context"
	"errors"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// An explicit segment request may spend only this bounded time waiting for
// playback feedback. Expiry grants that request delivery, not a new position.
const hlsBufferDeliveryWait = 2 * time.Second

// hlsBufferThrottle is captured once per playback registration. It is separate
// from the immutable output key and from cache and production ownership leases.
type hlsBufferThrottle struct {
	enabled         bool
	admissionPaused bool
	changed         chan struct{}
}

func playbackBufferDemandLocked(session *hlsSession) transcode.PlaybackBufferDemand {
	demand := session.demand
	return transcode.PlaybackBufferDemand{Known: demand.initialized && (demand.state == "Playing" || demand.state == "Paused"),
		Revision: demand.revision, PositionTicks: demand.position}
}

// All broadcasts require session.mu and perform no callbacks or I/O.
func notifyHLSBufferThrottleLocked(session *hlsSession) {
	if session.bufferThrottle.changed != nil {
		close(session.bufferThrottle.changed)
		session.bufferThrottle.changed = nil
	}
}

func (h *hlsRuntime) syncPlaybackBufferDemand(session *hlsSession) {
	receiver, supported := h.manager.(interface {
		UpdatePlaybackBufferDemand(transcode.Scope, transcode.PlaybackBufferDemand) error
	})
	if !supported {
		return
	}
	session.mu.Lock()
	closed := session.closed
	demand, scope := playbackBufferDemandLocked(session), session.key.scope
	session.mu.Unlock()
	if !closed {
		_ = receiver.UpdatePlaybackBufferDemand(scope, demand)
	}
}

func hlsBufferThrottleThresholds(seconds int) (int64, int64) {
	segment := int64(seconds) * media.TicksPerSecond
	// Leave two slots for an in-flight request and its complete publication.
	high := min(transcode.PlaybackBufferHighTicks, int64(maxHLSProducers-2)*segment)
	margin := min(transcode.PlaybackBufferHighTicks-transcode.PlaybackBufferLowTicks, 2*segment)
	return high, high - margin
}

// bufferThrottleAdmissionLocked considers only continuous complete coverage
// from committed playback. A far seek, a missing interval, and a cache hit do
// not become speculative lookahead because a GET has no demand generation.
func (h *hlsRuntime) bufferThrottleAdmissionLocked(session *hlsSession, plan transcode.Plan) (bool, <-chan struct{}) {
	graph := session.windowGraph
	demand := playbackBufferDemandLocked(session)
	if !session.bufferThrottle.enabled || !demand.Known || session.demand.paused || graph == nil ||
		graph.fallback || !graph.published || !plan.HLS.Window.RequireInputEvidence || plan.OutputMode != "" ||
		plan.SourceMode != "" || plan.VideoStreamIndex < 0 || !transcode.VideoEncodingSupported(plan.VideoCodec) {
		session.bufferThrottle.admissionPaused = false
		return false, nil
	}
	cursor := demand.PositionTicks
	for range len(graph.slots) {
		previous := cursor
		for _, binding := range graph.slots {
			if binding.producer.first != hlsGeneratedWindowProducer || h.producerReleased(binding.producer) {
				continue
			}
			start, end := binding.closure.StartTicks, binding.closure.EndTicks
			if start == binding.plan.StartTicks && end == binding.plan.HLS.Window.EndTicks && start <= cursor && end > cursor {
				cursor = end
			}
		}
		if cursor == previous {
			break
		}
	}
	high, low := hlsBufferThrottleThresholds(plan.SegmentSeconds)
	buffered := cursor - demand.PositionTicks
	if high <= 0 || buffered <= low {
		session.bufferThrottle.admissionPaused = false
	} else if buffered >= high {
		session.bufferThrottle.admissionPaused = true
	}
	// Existing delivery inside coverage and distant non-contiguous requests
	// remain independently constructible without changing playback position.
	if !session.bufferThrottle.admissionPaused || plan.StartTicks != cursor {
		return false, nil
	}
	if session.bufferThrottle.changed == nil {
		session.bufferThrottle.changed = make(chan struct{})
	}
	return true, session.bufferThrottle.changed
}

// Production runtimes reauthorize after the added scheduling wait. Internal
// adapters without authority I/O retain their existing source/ownership checks.
func (h *hlsRuntime) revalidateHLSBufferWait(ctx context.Context, session *hlsSession) error {
	if h.verify == nil {
		return nil
	}
	check, cancel := context.WithTimeout(ctx, 10*time.Second)
	stop := context.AfterFunc(session.ctx, cancel)
	defer stop()
	defer cancel()
	loan, current, err := h.authorizePlaybackSource(check, session.principal, session)
	if err == nil && (loan == nil || loan.file == nil || current.Item.ID != session.key.scope.ItemID ||
		current.SourceID != session.key.scope.SourceID || current.ETag != session.key.stamp) {
		err = library.ErrSourceChanged
	}
	if loan != nil {
		err = errors.Join(err, loan.close())
	}
	return errors.Join(err, check.Err(), session.ctx.Err())
}
