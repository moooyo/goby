package server

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

// Admission work can outlive a cancelled HTTP request while a filesystem or
// repository operation returns. Bound that work independently of request slots.
const maxHLSAdmissions = 32

var errHLSAdmissionStale = errors.New("deduplicated HLS producer retired before attachment")

type hlsAdmissionGate struct {
	slot chan struct{}
	refs int
}

type hlsAdmissionKey struct {
	scope transcode.Scope
	stamp string
}

type hlsProducerKey struct {
	scope transcode.Scope
	id    string
}

// Each attached entry owns a separate lease, even when Ensure reuses one job
// across registrations. released is protected by the runtime's producerMu.
type hlsProducerOwnership struct {
	key        hlsProducerKey
	released   bool
	production bool
}

func (h *hlsRuntime) ownProducer(scope transcode.Scope, id string, first, last int) (hlsProducer, error) {
	key := hlsProducerKey{scope: scope, id: id}
	h.producerMu.Lock()
	defer h.producerMu.Unlock()
	state, err := h.manager.Snapshot(scope, id)
	if errors.Is(err, transcode.ErrJobNotFound) || errors.Is(err, transcode.ErrJobCancelled) {
		return hlsProducer{}, errHLSAdmissionStale
	}
	if err != nil {
		return hlsProducer{}, err
	}
	if state.ProductionSealed {
		return hlsProducer{}, errHLSAdmissionStale
	}
	if state.State != "queued" && state.State != "running" && state.State != "completed" {
		return hlsProducer{}, transcode.ErrJobFailed
	}
	if h.producerOwners == nil {
		h.producerOwners = make(map[hlsProducerKey]int)
	}
	h.producerOwners[key]++
	production := state.State == "queued" || state.State == "running"
	if production {
		if h.producerDemands == nil {
			h.producerDemands = make(map[hlsProducerKey]int)
		}
		h.producerDemands[key]++
	}
	return hlsProducer{id: id, first: first, last: last, ownership: &hlsProducerOwnership{key: key, production: production}}, nil
}

func (h *hlsRuntime) producerReleased(producer hlsProducer) bool {
	if producer.ownership == nil {
		return true
	}
	h.producerMu.Lock()
	released := producer.ownership.released
	h.producerMu.Unlock()
	return released
}

// releaseProducer is idempotent per attached entry. Retiring or evicting one
// registration must not cancel a deduplicated producer still owned by another.
// The independent ownership mutex never acquires session.mu or runtime.mu.
func (h *hlsRuntime) releaseProducer(scope transcode.Scope, producer hlsProducer) {
	key := hlsProducerKey{scope: scope, id: producer.id}
	h.producerMu.Lock()
	defer h.producerMu.Unlock()
	if ownership := producer.ownership; ownership != nil {
		if ownership.released || ownership.key != key {
			return
		}
		ownership.released = true
		endedProduction := ownership.production
		if ownership.production {
			ownership.production = false
			h.producerDemands[key]--
			if h.producerDemands[key] == 0 {
				delete(h.producerDemands, key)
			}
		}
		h.producerOwners[key]--
		if h.producerOwners[key] > 0 {
			if endedProduction && h.producerDemands[key] == 0 {
				h.sealProducerJobLocked(scope, producer.id)
			}
			return
		}
		delete(h.producerOwners, key)
	} else {
		// A descriptor without an attached ownership lease cannot revoke a job.
		return
	}
	_ = h.manager.CancelJob(producer.id, scope)
}

func (h *hlsRuntime) cancelUnattachedProducer(scope transcode.Scope, id string) {
	h.producerMu.Lock()
	defer h.producerMu.Unlock()
	if h.producerOwners[hlsProducerKey{scope: scope, id: id}] == 0 {
		_ = h.manager.CancelJob(id, scope)
	}
}

type hlsAdmission struct {
	first, last       int
	anchorTicks       int64
	anchorEndTicks    int64
	revision          uint64
	spec              transcode.Spec
	request           context.Context
	ctx               context.Context
	cancel            context.CancelFunc
	stopSession       func() bool
	done              chan struct{}
	record            transcode.Record
	err               error
	playbackInput     *playbackOwnedInput
	playbackReference *playbackAdmissionReference
}

// reserveAdmission is called without session.mu. Registration with workers and
// the closing fence use the same mutex, so Close cannot race an Add with Wait.
// A reference includes gate waiters, preventing removal and replacement of a
// gate while an earlier revision is still completing its cancellation fence.
func (h *hlsRuntime) reserveAdmission(key hlsKey) (*hlsAdmissionGate, error) {
	// Serialize cold revisions of the same playback source. Transport location
	// is absent from manager deduplication, and immutable registered plans can
	// converge after segment-window or execution normalization. None of those
	// differences may split the cancellation fence of an identical manager spec.
	admissionKey := hlsAdmissionKey{scope: key.scope, stamp: key.stamp}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return nil, transcode.ErrManagerClosed
	}
	if h.usesPlaybackOwnership(key.plan) && h.server.playbackStopIntents.blocked(key.scope) {
		return nil, transcode.ErrJobCancelled
	}
	if h.admissions >= maxHLSAdmissions {
		return nil, transcode.ErrBusy
	}
	if h.admissionGates == nil {
		h.admissionGates = make(map[hlsAdmissionKey]*hlsAdmissionGate)
	}
	gate := h.admissionGates[admissionKey]
	if gate == nil {
		gate = &hlsAdmissionGate{slot: make(chan struct{}, 1)}
		h.admissionGates[admissionKey] = gate
	}
	gate.refs++
	h.admissions++
	h.workers.Add(1)
	return gate, nil
}

func (h *hlsRuntime) releaseAdmission(key hlsKey, gate *hlsAdmissionGate) {
	admissionKey := hlsAdmissionKey{scope: key.scope, stamp: key.stamp}
	h.mu.Lock()
	gate.refs--
	if gate.refs == 0 && h.admissionGates[admissionKey] == gate {
		delete(h.admissionGates, admissionKey)
	}
	h.admissions--
	h.mu.Unlock()
	h.workers.Done()
}

// newHLSAdmission is installed with session.mu held. Its context retains the
// initiating request's cancellation and the session's retirement fence.
func newHLSAdmission(ctx context.Context, session *hlsSession, spec transcode.Spec, first, last int) *hlsAdmission {
	session.admissionRevision++
	work, cancel := context.WithCancel(ctx)
	return &hlsAdmission{first: first, last: last, revision: session.admissionRevision, spec: spec, request: ctx, ctx: work, cancel: cancel,
		stopSession: context.AfterFunc(session.ctx, cancel), done: make(chan struct{})}
}

// runAdmission consumes input in every case. Ensure owns it once called, even
// when admission fails or returns a record together with an error. The gate is
// retained through orphan cancellation, before a replacement can reuse a spec.
func (h *hlsRuntime) runAdmission(session *hlsSession, pending *hlsAdmission, gate *hlsAdmissionGate, input *os.File) {
	defer h.releaseAdmission(session.key, gate)
	defer pending.playbackReference.release()
	defer pending.stopSession()
	defer pending.cancel()
	consumed := false
	defer func() {
		h.closeAdmissionInput(pending, input, consumed)
	}()
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
	err := pending.ctx.Err()
	if acquired && err == nil {
		consumed = true
		if pending.playbackInput != nil {
			record, err = pending.playbackInput.ensure(pending.ctx, h.manager, pending.spec)
		} else {
			record, err = h.manager.Ensure(pending.ctx, pending.spec, input)
		}
	}
	session.mu.Lock()
	current := session.admission == pending && !session.closed && session.ctx.Err() == nil && pending.ctx.Err() == nil
	if session.closed || session.ctx.Err() != nil {
		err = transcode.ErrJobNotFound
	}
	if !current && !session.closed && session.ctx.Err() == nil {
		err = pending.ctx.Err()
		if err == nil {
			err = transcode.ErrJobNotFound
		}
	}
	if err == nil {
		producer, ownershipErr := h.ownProducer(session.key.scope, record.ID, pending.first, pending.last)
		if ownershipErr != nil {
			err = ownershipErr
		} else if !h.makeProducerRoomLocked(session, producer.id) {
			h.releaseProducer(session.key.scope, producer)
			err = transcode.ErrBusy
		} else {
			// Register the new reference before eviction: Ensure may have reused
			// the same completed record already present in the oldest entry.
			session.producers = append(session.producers, producer)
			session.accessed = time.Now()
		}
	}
	if err != nil && record.ID != "" {
		// No caller may borrow this record between an obsolete creation and its
		// exact cancellation. Other keys and already attached jobs stay usable.
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

// A coalesced caller retains its own input until creation succeeds. If the
// initiating caller abandons that creation, another active caller can retry;
// the shared gate still fences a late result from the abandoned admission.
// A deliberate seek supersession does not retry old demand against a new seek.
func retryHLSAdmission(ctx context.Context, session *hlsSession, pending *hlsAdmission, err error) bool {
	session.mu.Lock()
	current := !session.closed && session.admissionRevision == pending.revision
	session.mu.Unlock()
	return current && ctx.Err() == nil && session.ctx.Err() == nil &&
		(errors.Is(err, errHLSAdmissionStale) || (pending.request.Err() != nil &&
			(errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))))
}

func waitHLSAdmission(ctx context.Context, session *hlsSession, pending *hlsAdmission) (transcode.Record, error) {
	if err := ctx.Err(); err != nil {
		return transcode.Record{}, err
	}
	select {
	case <-pending.done:
		return pending.record, pending.err
	case <-ctx.Done():
		return transcode.Record{}, ctx.Err()
	case <-session.ctx.Done():
		return transcode.Record{}, transcode.ErrJobNotFound
	case <-pending.ctx.Done():
		// Worker cleanup cancels its context after publishing the result. Prefer
		// that completed result over the cleanup cancellation when both fire.
		select {
		case <-pending.done:
			return pending.record, pending.err
		default:
			if session.ctx.Err() != nil {
				return transcode.Record{}, transcode.ErrJobNotFound
			}
			return transcode.Record{}, pending.ctx.Err()
		}
	}
}
