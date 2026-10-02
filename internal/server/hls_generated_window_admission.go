package server

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const hlsGeneratedGraphPreparation = -3

// Evidence rejection can select the original presentation before publication.
// Capacity, process failure, cancellation, persistence and source mutation are
// request failures; they cannot permanently demote a supported presentation.
func generatedWindowUnsupported(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, transcode.ErrStart) || errors.Is(err, transcode.ErrInvalidInput) ||
		errors.Is(err, transcode.ErrJobFailed) || errors.Is(err, transcode.ErrJobCancelled) ||
		errors.Is(err, transcode.ErrManagerClosed) || errors.Is(err, transcode.ErrPersistence) ||
		errors.Is(err, transcode.ErrBusy) || errors.Is(err, transcode.ErrQuota) || errors.Is(err, media.ErrProcessRetirementUnknown) {
		return false
	}
	var processFailure *exec.ExitError
	if errors.As(err, &processFailure) {
		return false
	}
	var fileFailure *os.PathError
	var systemFailure syscall.Errno
	if errors.As(err, &fileFailure) || errors.As(err, &systemFailure) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return false
	}
	return errors.Is(err, transcode.ErrUnsupportedTimeline) || errors.Is(err, transcode.ErrInvalidTimeline) ||
		errors.Is(err, transcode.ErrTimelineProbe) || errors.Is(err, transcode.ErrTimelineLimit) || errors.Is(err, transcode.ErrInvalidPlan)
}

// prepareGeneratedWindowGraph borrows the request's source. Its worker retains
// its own source descriptor, admission reference and gate until publication or
// scoped orphan cancellation completes. Competing requests never cancel it.
func (h *hlsRuntime) prepareGeneratedWindowGraph(ctx context.Context, session *hlsSession, input *os.File, sourceInfo media.Info, start int64) (*hlsGeneratedWindowGraph, error) {
	if start < 0 || start >= session.key.plan.DurationTicks {
		return nil, errHLSRequestInvalid
	}
	var gate *hlsAdmissionGate
	retried := false
	defer func() {
		if gate != nil {
			h.releaseAdmission(session.key, gate)
		}
	}()
	for {
		session.mu.Lock()
		if session.closed || ctx.Err() != nil {
			session.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, transcode.ErrJobNotFound
		}
		if graph := session.windowGraph; graph != nil {
			session.mu.Unlock()
			if graph.fallback {
				return nil, nil
			}
			if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(input, graph.endpoint); err != nil {
				return nil, err
			}
			return graph, nil
		}
		if _, supported := h.manager.(hlsGeneratedWindowJobs); !supported ||
			!transcode.GeneratedWindowClosureEligible(session.key.plan) ||
			(session.key.plan.HLS.SegmentType != "mpegts" && session.key.plan.HLS.SegmentType != "fmp4") {
			session.windowGraph = &hlsGeneratedWindowGraph{fallback: true}
			session.mu.Unlock()
			return nil, nil
		}
		if len(session.producers) != 0 {
			// A presentation with an already advertised complete-source producer
			// retains that mode for this immutable output revision.
			session.windowGraph = &hlsGeneratedWindowGraph{fallback: true}
			session.mu.Unlock()
			return nil, nil
		}
		if session.demand.paused {
			session.mu.Unlock()
			return nil, transcode.ErrOutputUnavailable
		}
		if pending := session.admission; pending != nil {
			if pending.first != hlsGeneratedGraphPreparation {
				session.mu.Unlock()
				return nil, transcode.ErrBusy
			}
			session.mu.Unlock()
			if retry, err := waitGeneratedWindowAdmission(ctx, session, pending); err != nil {
				if retry && !retried {
					retried = true
					continue
				}
				return nil, err
			}
			continue
		}
		if gate == nil {
			session.mu.Unlock()
			var err error
			gate, err = h.reserveAdmission(session.key)
			if err != nil {
				return nil, err
			}
			continue
		}
		owned, playbackInput, playbackWorker, err := h.duplicateAdmissionInputLocked(session, input)
		if err != nil {
			session.mu.Unlock()
			return nil, err
		}
		pending := newHLSAdmission(ctx, session, transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: session.key.plan}, hlsGeneratedGraphPreparation, hlsGeneratedGraphPreparation)
		pending.installPlaybackInput(playbackInput, playbackWorker)
		segmentTicks := int64(session.key.plan.SegmentSeconds) * media.TicksPerSecond
		pending.anchorTicks = start / segmentTicks * segmentTicks
		pending.anchorEndTicks = min(pending.anchorTicks+segmentTicks, session.key.plan.DurationTicks)
		session.admission = pending
		session.mu.Unlock()
		workerGate := gate
		gate = nil
		go h.runGeneratedGraphPreparation(session, pending, workerGate, owned, sourceInfo, start)
		if retry, err := waitGeneratedWindowAdmission(ctx, session, pending); err != nil {
			if retry && !retried {
				retried = true
				continue
			}
			return nil, err
		}
	}
}

// A still-active coalesced request may replace an abandoned initiator, but
// only after its worker has closed the orphan fence. Pause/seek revisions do
// not retry, and callers bound replacement attempts independently.
func waitGeneratedWindowAdmission(ctx context.Context, session *hlsSession, pending *hlsAdmission) (bool, error) {
	_, err := waitHLSAdmission(ctx, session, pending)
	if err == nil || !retryHLSAdmission(ctx, session, pending, err) {
		return false, err
	}
	select {
	case <-pending.done:
		return retryHLSAdmission(ctx, session, pending, err), err
	case <-ctx.Done():
		return false, ctx.Err()
	case <-session.ctx.Done():
		return false, transcode.ErrJobNotFound
	}
}

func (h *hlsRuntime) generatedGraphSourceEvidence(ctx context.Context, session *hlsSession, input *os.File, sourceInfo media.Info, start int64) (*hlsGeneratedWindowGraph, transcode.Plan, error) {
	select {
	case h.probes <- struct{}{}:
	case <-ctx.Done():
		return nil, transcode.Plan{}, ctx.Err()
	case <-session.ctx.Done():
		return nil, transcode.Plan{}, transcode.ErrJobNotFound
	}
	defer func() { <-h.probes }()
	endpoint, err := transcode.MeasureGeneratedMP4SourceEndpoint(ctx, input, session.key.plan.VideoStreamIndex)
	if err != nil {
		return nil, transcode.Plan{}, err
	}
	timeline, err := hlsGeneratedWindowTimeline(session.key.plan, sourceInfo, endpoint)
	if err != nil {
		return nil, transcode.Plan{}, err
	}
	if start < 0 || start >= endpoint.DurationTicks {
		return nil, transcode.Plan{}, errHLSRequestInvalid
	}
	base := hlsGeneratedWindowProductionBase(session.key.plan, endpoint)
	zero, err := hlsGeneratedWindowSlotPlan(base, timeline, 0)
	if err != nil {
		return nil, transcode.Plan{}, err
	}
	if _, err := transcode.MeasureGeneratedSourceRange(ctx, h.server.cfg.FFprobePath, input, zero, sourceInfo.FormatStartTicks); err != nil {
		return nil, transcode.Plan{}, err
	}
	tail, err := hlsGeneratedWindowSlotPlan(base, timeline, len(timeline.Segments)-1)
	if err != nil {
		return nil, transcode.Plan{}, err
	}
	if _, err := transcode.MeasureGeneratedMP4SourceTail(ctx, h.server.cfg.FFprobePath, input, tail, endpoint); err != nil {
		return nil, transcode.Plan{}, err
	}
	if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(input, endpoint); err != nil {
		return nil, transcode.Plan{}, err
	}
	slot, found := timeline.SegmentAt(start)
	if !found {
		return nil, transcode.Plan{}, errHLSRequestInvalid
	}
	initial, err := hlsGeneratedWindowSlotPlan(base, timeline, slot.Number)
	if err != nil {
		return nil, transcode.Plan{}, err
	}
	graph := &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline, basePlan: base, slots: make(map[int]hlsGeneratedWindowBinding)}
	if base.HLS.SegmentType == "fmp4" {
		session.mu.Lock()
		if session.closed || session.ctx.Err() != nil || ctx.Err() != nil {
			session.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, transcode.Plan{}, err
			}
			return nil, transcode.Plan{}, transcode.ErrJobNotFound
		}
		graph.initialization, err = newHLSGeneratedWindowBudgetHistory(&h.initializationBudget, session.id, endpoint.SourceIdentity,
			base, initial.HLS.Window.NativeClockVersion, len(timeline.Segments))
		session.mu.Unlock()
		if err != nil {
			return nil, transcode.Plan{}, err
		}
	}
	return graph, initial, nil
}

func (h *hlsRuntime) runGeneratedGraphPreparation(session *hlsSession, pending *hlsAdmission, gate *hlsAdmissionGate, input *os.File, sourceInfo media.Info, start int64) {
	defer h.releaseAdmission(session.key, gate)
	defer pending.playbackReference.release()
	defer pending.stopSession()
	defer pending.cancel()
	defer h.closeAdmissionInput(pending, input, false)
	acquired := false
	select {
	case gate.slot <- struct{}{}:
		acquired = true
	case <-pending.ctx.Done():
	}
	if acquired {
		defer func() { <-gate.slot }()
	}
	var graph *hlsGeneratedWindowGraph
	attached := false
	defer func() {
		if graph != nil && !attached {
			session.mu.Lock()
			graph.initialization.release()
			session.mu.Unlock()
		}
	}()
	var binding hlsGeneratedWindowBinding
	var record transcode.Record
	err := pending.ctx.Err()
	if acquired && err == nil {
		var plan transcode.Plan
		graph, plan, err = h.generatedGraphSourceEvidence(pending.ctx, session, input, sourceInfo, start)
		if err == nil {
			session.mu.Lock()
			pending.anchorTicks, pending.anchorEndTicks = plan.StartTicks, plan.HLS.Window.EndTicks
			session.mu.Unlock()
			record, binding, err = h.closedGeneratedWindow(pending.ctx, session, input, graph.endpoint, plan, sourceInfo)
		}
	}
	session.mu.Lock()
	current := session.admission == pending && session.admissionRevision == pending.revision && !session.closed &&
		session.ctx.Err() == nil && pending.ctx.Err() == nil && !session.demand.paused
	if !current {
		err = pending.ctx.Err()
		if err == nil {
			err = transcode.ErrJobNotFound
		}
	}
	fallback := current && generatedWindowUnsupported(err)
	cleanupHandled := false
	if current && err == nil && (graph == nil || session.windowGraph != nil || record.ID == "") {
		err = transcode.ErrJobNotFound
	}
	if current && err == nil {
		err = graph.rememberBinding(session.id, binding)
	}
	if current && err == nil && graph != nil && session.windowGraph == nil {
		producer, ownershipErr := h.ownProducer(session.key.scope, record.ID, hlsGeneratedWindowProducer, hlsGeneratedWindowProducer)
		if ownershipErr != nil {
			err = ownershipErr
		} else if producer.ownership.production {
			h.releaseProducer(session.key.scope, producer)
			cleanupHandled = true
			err = transcode.ErrOutputUnavailable
		} else {
			binding.producer = producer
			graph.slots[planNumber(binding.plan)] = binding
			session.producers = append(session.producers, producer)
			session.windowGraph = graph
			attached = true
			session.accessed = time.Now()
		}
	}
	if (err != nil || fallback) && record.ID != "" && !cleanupHandled {
		session.mu.Unlock()
		h.cancelUnattachedProducer(pending.spec.Scope, record.ID)
		session.mu.Lock()
	}
	if fallback {
		// Install fallback only after the orphan fence. A concurrent request
		// cannot start a replacement while the private job is still revoking.
		current = session.admission == pending && !session.closed && session.ctx.Err() == nil && pending.ctx.Err() == nil && !session.demand.paused
		if current {
			session.windowGraph = &hlsGeneratedWindowGraph{fallback: true}
			err = nil
		} else {
			err = pending.ctx.Err()
			if err == nil {
				err = transcode.ErrJobNotFound
			}
		}
	}
	if session.admission == pending {
		session.admission = nil
	}
	pending.record, pending.err = record, err
	close(pending.done)
	session.mu.Unlock()
}

func planNumber(plan transcode.Plan) int { return plan.HLS.Window.StartNumber }
