package server

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// generatedWindowSlot borrows the freshly authorized source and returns a
// separately owned pin for the exact completed artifact. The HTTP caller closes
// that pin on failure or transfers it to commitGeneratedWindowRedirect before
// writing a redirect. A logical GET carries no playback demand generation.
func (h *hlsRuntime) generatedWindowSlot(ctx context.Context, session *hlsSession, source *os.File, sourceInfo media.Info, graphID string, number, variant int) (hlsGeneratedWindowBinding, *transcode.ReadHandle, error) {
	return h.generatedWindowSlotResource(ctx, session, source, sourceInfo, graphID, hlsGeneratedWindowResource{number: number, variant: variant})
}

func (h *hlsRuntime) generatedWindowSlotResource(ctx context.Context, session *hlsSession, source *os.File, sourceInfo media.Info, graphID string, resource hlsGeneratedWindowResource) (hlsGeneratedWindowBinding, *transcode.ReadHandle, error) {
	number, variant := resource.number, resource.variant
	var empty hlsGeneratedWindowBinding
	if ctx == nil || session == nil {
		return empty, nil, transcode.ErrInvalidOptions
	}
	var gate *hlsAdmissionGate
	var retryFrom *hlsAdmission
	retried := false
	admissions := 0
	releaseReservation := func() {
		if gate != nil {
			reserved := gate
			gate = nil
			h.releaseAdmission(session.key, reserved)
		}
	}
	defer releaseReservation()
	for {
		session.mu.Lock()
		graph := session.windowGraph
		if session.closed || session.ctx.Err() != nil || ctx.Err() != nil || graphID != session.id ||
			graph == nil || graph.fallback || !graph.published {
			session.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return empty, nil, err
			}
			return empty, nil, transcode.ErrJobNotFound
		}
		if variant < 0 || variant >= max(1, graph.basePlan.HLS.RenditionCount) || hlsGeneratedWindowResourceName(graph.basePlan, resource) == "" {
			session.mu.Unlock()
			return empty, nil, errHLSRequestInvalid
		}
		plan, err := hlsGeneratedWindowSlotPlan(graph.basePlan, graph.timeline, number)
		if err != nil {
			session.mu.Unlock()
			return empty, nil, errHLSRequestInvalid
		}
		h.releaseExpiredGeneratedWindowPinsLocked(session, time.Now())
		binding, cached := graph.slots[number]
		session.mu.Unlock()
		if err := transcode.ValidateGeneratedMP4SourceEndpointIdentity(source, graph.endpoint); err != nil {
			return empty, nil, err
		}
		if cached {
			selected, valid := hlsGeneratedWindowBindingResource(binding, resource)
			if !valid {
				return empty, nil, transcode.ErrOutputUnavailable
			}
			pin, openErr := h.generatedWindowExactArtifact(ctx, session, selected.producer.id, selected.artifactName)
			if openErr == nil {
				releaseReservation()
				return selected, pin, nil
			}
			if !generatedWindowCacheMiss(openErr) {
				return empty, nil, openErr
			}
			session.mu.Lock()
			if session.windowGraph == graph {
				if current, found := graph.slots[number]; found && current.producer.ownership == binding.producer.ownership && time.Now().Before(current.redirectedUntil) {
					// A missing sibling cannot revoke an already issued redirect
					// for another rendition of this same complete job.
					session.mu.Unlock()
					return empty, nil, openErr
				}
				h.discardGeneratedWindowBindingLocked(session, number, binding)
			}
			session.mu.Unlock()
		}
		session.mu.Lock()
		if session.closed || session.ctx.Err() != nil || ctx.Err() != nil || session.windowGraph != graph || !graph.published {
			session.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return empty, nil, err
			}
			return empty, nil, transcode.ErrJobNotFound
		}
		if _, won := graph.slots[number]; won {
			session.mu.Unlock()
			continue
		}
		if retryFrom != nil && session.admissionRevision != retryFrom.revision {
			session.mu.Unlock()
			return empty, nil, context.Canceled
		}
		if session.demand.paused {
			session.mu.Unlock()
			return empty, nil, transcode.ErrOutputUnavailable
		}
		if pending := session.admission; pending != nil {
			if pending.first != number || pending.last != number || !pending.spec.Plan.HLS.Window.RequireInputEvidence {
				// An old or unrelated GET cannot revoke the current private work.
				session.mu.Unlock()
				return empty, nil, transcode.ErrBusy
			}
			if admissions >= 2 {
				session.mu.Unlock()
				return empty, nil, transcode.ErrOutputUnavailable
			}
			admissions++
			session.mu.Unlock()
			releaseReservation()
			if retry, waitErr := waitGeneratedWindowAdmission(ctx, session, pending); waitErr != nil {
				if retry && !retried {
					retried, retryFrom = true, pending
					continue
				}
				return empty, nil, waitErr
			}
			continue
		}
		if !h.generatedWindowRoomAvailableLocked(session, time.Now()) {
			session.mu.Unlock()
			return empty, nil, transcode.ErrBusy
		}
		if admissions >= 2 {
			session.mu.Unlock()
			return empty, nil, transcode.ErrOutputUnavailable
		}
		if gate == nil {
			session.mu.Unlock()
			gate, err = h.reserveAdmission(session.key)
			if err != nil {
				return empty, nil, err
			}
			continue
		}
		owned, playbackInput, playbackWorker, err := h.duplicateAdmissionInputLocked(session, source)
		if err != nil {
			session.mu.Unlock()
			return empty, nil, err
		}
		pending := newHLSAdmission(ctx, session, transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: plan}, number, number)
		pending.installPlaybackInput(playbackInput, playbackWorker)
		admissions++
		// The previous revision fence was satisfied before installing this
		// replacement. Its own revision now guards the next cached open.
		retryFrom = pending
		session.admission, session.accessed = pending, time.Now()
		session.mu.Unlock()
		workerGate := gate
		gate = nil
		go h.runGeneratedWindowSlotAdmission(session, graph, pending, workerGate, owned, sourceInfo)
		if retry, waitErr := waitGeneratedWindowAdmission(ctx, session, pending); waitErr != nil {
			if retry && !retried {
				retried, retryFrom = true, pending
				continue
			}
			return empty, nil, waitErr
		}
	}
}

func generatedWindowCacheMiss(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, transcode.ErrBusy) ||
		errors.Is(err, transcode.ErrQuota) || errors.Is(err, transcode.ErrPersistence) || errors.Is(err, transcode.ErrManagerClosed) ||
		errors.Is(err, transcode.ErrInvalidInput) || errors.Is(err, media.ErrProcessRetirementUnknown) {
		return false
	}
	return errors.Is(err, transcode.ErrJobNotFound) || errors.Is(err, transcode.ErrOutputUnavailable) || errors.Is(err, transcode.ErrJobCancelled)
}

// The gate remains owned through exact orphan cancellation, before another
// admission can reuse the same manager spec. No partial producer is attached.
func (h *hlsRuntime) runGeneratedWindowSlotAdmission(session *hlsSession, graph *hlsGeneratedWindowGraph, pending *hlsAdmission, gate *hlsAdmissionGate, source *os.File, sourceInfo media.Info) {
	defer h.releaseAdmission(session.key, gate)
	defer pending.playbackReference.release()
	defer pending.stopSession()
	defer pending.cancel()
	defer h.closeAdmissionInput(pending, source, false)
	acquired := false
	select {
	case gate.slot <- struct{}{}:
		acquired = true
	case <-pending.ctx.Done():
	}
	if acquired {
		defer func() { <-gate.slot }()
	}
	var record transcode.Record
	var binding hlsGeneratedWindowBinding
	err := pending.ctx.Err()
	if acquired && err == nil {
		record, binding, err = h.closedGeneratedWindow(pending.ctx, session, source, graph.endpoint, pending.spec.Plan, sourceInfo)
	}
	session.mu.Lock()
	current := session.admission == pending && session.admissionRevision == pending.revision && session.windowGraph == graph &&
		graph.published && !graph.fallback && !session.closed && session.ctx.Err() == nil && pending.ctx.Err() == nil && !session.demand.paused
	if !current {
		err = pending.ctx.Err()
		if err == nil {
			err = transcode.ErrJobNotFound
		}
	}
	cleanupHandled := false
	if current && err == nil {
		err = graph.rememberBinding(session.id, binding)
	}
	if current && err == nil {
		producer, ownershipErr := h.ownProducer(session.key.scope, record.ID, hlsGeneratedWindowProducer, hlsGeneratedWindowProducer)
		if ownershipErr != nil {
			err = ownershipErr
		} else if producer.ownership.production || !h.makeGeneratedWindowRoomLocked(session, time.Now()) {
			h.releaseProducer(session.key.scope, producer)
			cleanupHandled = true
			err = transcode.ErrBusy
		} else {
			binding.producer = producer
			graph.slots[pending.first] = binding
			session.producers = append(session.producers, producer)
			session.accessed = time.Now()
		}
	}
	if err != nil && record.ID != "" && !cleanupHandled {
		session.mu.Unlock()
		h.cancelUnattachedProducer(pending.spec.Scope, record.ID)
		session.mu.Lock()
	}
	if session.admission == pending {
		session.admission = nil
	}
	pending.record, pending.err = record, err
	close(pending.done)
	session.mu.Unlock()
}

func (h *hlsRuntime) discardGeneratedWindowBindingLocked(session *hlsSession, number int, expected hlsGeneratedWindowBinding) {
	graph := session.windowGraph
	if graph == nil {
		return
	}
	current, found := graph.slots[number]
	if !found || current.producer.id != expected.producer.id || current.producer.ownership != expected.producer.ownership {
		return
	}
	for _, pin := range current.redirectPins {
		if pin != nil {
			h.releaseGeneratedWindowPinLocked(session, pin)
		}
	}
	for _, pin := range current.initializationPins {
		if pin != nil {
			h.releaseGeneratedWindowPinLocked(session, pin)
		}
	}
	if current.redirectPin != nil {
		h.releaseGeneratedWindowPinLocked(session, current.redirectPin)
	}
	delete(graph.slots, number)
	for index, producer := range session.producers {
		if producer.ownership == current.producer.ownership {
			h.releaseProducer(session.key.scope, producer)
			copy(session.producers[index:], session.producers[index+1:])
			session.producers[len(session.producers)-1] = hlsProducer{}
			session.producers = session.producers[:len(session.producers)-1]
			return
		}
	}
}

func (h *hlsRuntime) generatedWindowEvictionCandidateLocked(session *hlsSession, now time.Time) (int, bool) {
	graph := session.windowGraph
	if graph == nil {
		return 0, false
	}
	for _, producer := range session.producers {
		if producer.first != hlsGeneratedWindowProducer {
			continue
		}
		state, err := h.manager.Snapshot(session.key.scope, producer.id)
		if !h.producerReleased(producer) && err == nil && (state.State == "queued" || state.State == "running") {
			continue
		}
		for number, binding := range graph.slots {
			if binding.producer.ownership == producer.ownership && !now.Before(binding.redirectedUntil) {
				return number, true
			}
		}
	}
	return 0, false
}

func (h *hlsRuntime) generatedWindowRoomAvailableLocked(session *hlsSession, now time.Time) bool {
	if session.windowGraph == nil {
		return false
	}
	if len(session.producers) < maxHLSProducers && len(session.windowGraph.slots) < maxHLSProducers {
		return true
	}
	_, available := h.generatedWindowEvictionCandidateLocked(session, now)
	return available
}

func (h *hlsRuntime) makeGeneratedWindowRoomLocked(session *hlsSession, now time.Time) bool {
	h.releaseExpiredGeneratedWindowPinsLocked(session, now)
	if len(session.producers) < maxHLSProducers && len(session.windowGraph.slots) < maxHLSProducers {
		return true
	}
	number, available := h.generatedWindowEvictionCandidateLocked(session, now)
	if !available {
		return false
	}
	h.discardGeneratedWindowBindingLocked(session, number, session.windowGraph.slots[number])
	return len(session.producers) < maxHLSProducers && len(session.windowGraph.slots) < maxHLSProducers
}

// commitGeneratedWindowRedirect consumes a caller-owned pin on every outcome.
// A previously retained alias carries no new ownership and remains its graph's
// responsibility even when a malformed alias request is rejected. At most one
// pin per rendition of a bounded slot survives the response. Maintenance releases
// expired pins; no per-redirect goroutine or timer is created here.
func (h *hlsRuntime) commitGeneratedWindowRedirect(ctx context.Context, session *hlsSession, graphID string, binding hlsGeneratedWindowBinding, pin *transcode.ReadHandle) error {
	if pin == nil {
		return transcode.ErrOutputUnavailable
	}
	transferred := false
	defer func() {
		if !transferred {
			h.closeUnretainedGeneratedWindowPin(session, pin)
		}
	}()
	if ctx == nil || session == nil {
		return transcode.ErrInvalidOptions
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if pin.EncodingID() != binding.producer.id {
		return transcode.ErrJobNotFound
	}
	stat, err := pin.Stat()
	if err != nil {
		return err
	}
	identity, err := media.VideoSeekSourceIdentity(stat)
	if err != nil || identity != binding.artifactIdentity {
		return transcode.ErrInvalidInput
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if session.closed || session.ctx.Err() != nil || graphID != session.id {
		return transcode.ErrJobNotFound
	}
	current, owned := h.generatedWindowBindingLocked(session, binding.producer.id, binding.artifactName)
	if !owned || current.producer.ownership != binding.producer.ownership || current.plan != binding.plan ||
		current.artifactIdentity != binding.artifactIdentity || current.closure != binding.closure ||
		current.artifactNames != binding.artifactNames || current.artifactIdentities != binding.artifactIdentities ||
		current.artifacts != binding.artifacts || current.nativeEmission != binding.nativeEmission {
		return transcode.ErrJobNotFound
	}
	number := planNumber(current.plan)
	root, owned := session.windowGraph.slots[number]
	if !owned || root.producer.ownership != binding.producer.ownership {
		return transcode.ErrJobNotFound
	}
	variant, initialization := -1, false
	if root.plan.HLS.SegmentType == "fmp4" {
		resource, found := root.artifacts.resourceForExactName(root.producer.id, root.plan, binding.artifactName)
		if found {
			artifact, valid := root.artifacts.selectResource(root.producer.id, root.plan, resource)
			if valid && artifact.identity == binding.artifactIdentity {
				variant, initialization = resource.variant, resource.initialization
			}
		}
	} else {
		for index := 0; index < max(1, root.plan.HLS.RenditionCount); index++ {
			if root.artifactNames[index] == binding.artifactName && root.artifactIdentities[index] == binding.artifactIdentity {
				variant = index
			}
		}
	}
	if variant < 0 {
		return transcode.ErrJobNotFound
	}
	now := time.Now()
	if owner := h.generatedWindowPinOwner(pin); owner != nil {
		if owner == session {
			h.releaseExpiredGeneratedWindowPinsLocked(session, now)
		}
		return transcode.ErrOutputUnavailable
	}
	h.releaseExpiredGeneratedWindowPinsLocked(session, now)
	root = session.windowGraph.slots[number]
	target := &root.redirectPins[variant]
	if initialization {
		target = &root.initializationPins[variant]
	}
	if *target == nil {
		if err := h.retainGeneratedWindowPinLocked(session, pin, binding.artifactIdentity); err != nil {
			return err
		}
		*target = pin
		transferred = true
	} else if *target == pin {
		return transcode.ErrOutputUnavailable
	}
	root.redirectedUntil = now.Add(hlsGeneratedWindowGrace)
	session.windowGraph.slots[number] = root
	session.accessed = time.Now()
	return nil
}

func (h *hlsRuntime) releaseExpiredGeneratedWindowPinsLocked(session *hlsSession, now time.Time) {
	if session.windowGraph == nil {
		return
	}
	for number, binding := range session.windowGraph.slots {
		if !now.Before(binding.redirectedUntil) {
			for index, pin := range binding.initializationPins {
				if pin != nil {
					h.releaseGeneratedWindowPinLocked(session, pin)
					binding.initializationPins[index] = nil
				}
			}
			for index, pin := range binding.redirectPins {
				if pin != nil {
					h.releaseGeneratedWindowPinLocked(session, pin)
					binding.redirectPins[index] = nil
				}
			}
			if binding.redirectPin != nil {
				h.releaseGeneratedWindowPinLocked(session, binding.redirectPin)
				binding.redirectPin = nil
			}
			session.windowGraph.slots[number] = binding
		}
	}
}

func (h *hlsRuntime) releaseGeneratedWindowPinsLocked(session *hlsSession) {
	if session.windowGraph == nil {
		return
	}
	for number, binding := range session.windowGraph.slots {
		for index, pin := range binding.initializationPins {
			if pin != nil {
				h.releaseGeneratedWindowPinLocked(session, pin)
				binding.initializationPins[index] = nil
			}
		}
		for index, pin := range binding.redirectPins {
			if pin != nil {
				h.releaseGeneratedWindowPinLocked(session, pin)
				binding.redirectPins[index] = nil
			}
		}
		if binding.redirectPin != nil {
			h.releaseGeneratedWindowPinLocked(session, binding.redirectPin)
			binding.redirectPin = nil
		}
		binding.redirectedUntil = time.Time{}
		session.windowGraph.slots[number] = binding
	}
}
