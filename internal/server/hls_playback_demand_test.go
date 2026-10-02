package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsDemandTestPlay(session *hlsSession, revision int64, state string, position int64) library.PlaySession {
	scope := session.key.scope
	return library.PlaySession{ID: scope.PlaySessionID, AuthSessionID: scope.AuthSessionID, UserID: scope.UserID,
		DeviceID: scope.DeviceID, ItemID: scope.ItemID, MediaSourceID: scope.SourceID, ApplicationKey: scope.ApplicationKey,
		ApplicationClientID: scope.ApplicationClientID, PlaybackRevision: revision, State: state, PositionTicks: position}
}

type hlsDemandTestJobs struct {
	*hlsRuntimeTestJobs
	sealMu        sync.Mutex
	sealed        []hlsRuntimeOpenCall
	sealErr       error
	snapshotState string
	sealedIDs     map[string]bool
}

func (jobs *hlsDemandTestJobs) SealProduction(id string, scope transcode.Scope) error {
	jobs.sealMu.Lock()
	defer jobs.sealMu.Unlock()
	jobs.sealed = append(jobs.sealed, hlsRuntimeOpenCall{id: id, scope: scope})
	if jobs.sealErr == nil {
		if jobs.sealedIDs == nil {
			jobs.sealedIDs = make(map[string]bool)
		}
		jobs.sealedIDs[id] = true
	}
	return jobs.sealErr
}

func (jobs *hlsDemandTestJobs) Snapshot(scope transcode.Scope, id string) (transcode.Record, error) {
	record, err := jobs.hlsRuntimeTestJobs.Snapshot(scope, id)
	if jobs.snapshotState != "" {
		record.State = jobs.snapshotState
	}
	jobs.sealMu.Lock()
	record.ProductionSealed = jobs.sealedIDs[id]
	jobs.sealMu.Unlock()
	return record, err
}

func TestHLSPlaybackDemandCommittedPauseFencesLateCallbacksAndNewAdmission(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "demand-order", true)
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Playing", 0))
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 3, "Paused", 2*media.TicksPerSecond))
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 2, "Playing", media.TicksPerSecond))
	// Ping returns the same committed revision. Fresh authorization of that
	// snapshot cannot reactivate production or apply it a second time.
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 3, "Paused", 2*media.TicksPerSecond))
	session.mu.Lock()
	paused, revision := session.demand.paused, session.demand.revision
	retained := len(session.producers) == 1 && !h.producerReleased(session.producers[0])
	session.mu.Unlock()
	jobs.sealMu.Lock()
	seals := len(jobs.sealed)
	jobs.sealMu.Unlock()
	if !paused || revision != 3 || !retained || seals != 1 || session.ctx.Err() != nil {
		t.Fatal("late callback/Ping changed pause or lost retained cache ownership")
	}
	_, err := h.segment(context.Background(), session, hlsRuntimeInput(t), 1)
	if !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("paused cache miss admitted production: %v", err)
	}
	base.mu.Lock()
	ensures, cancels := len(base.ensured), len(base.cancels)
	base.mu.Unlock()
	if ensures != 0 || cancels != 0 {
		t.Fatal("pause reused invalidation or started another producer")
	}
}

func TestHLSPlaybackDemandSharedCacheAndProductionLeasesRemainSeparate(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	first := hlsRuntimeTestSession(t, h, "shared-demand", true)
	second := hlsRuntimeTestSession(t, h, "second-demand", false)
	second.key.scope = first.key.scope
	producer, err := h.ownProducer(first.key.scope, first.producers[0].id, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	second.producers = []hlsProducer{producer}
	h.sealProducerDemand(first.key.scope, first.producers[0])
	if h.producerReleased(first.producers[0]) || h.producerReleased(second.producers[0]) {
		t.Fatal("production retirement released a retained cache reference")
	}
	jobs.sealMu.Lock()
	before := len(jobs.sealed)
	jobs.sealMu.Unlock()
	if before != 0 {
		t.Fatal("one paused owner stopped another owner's producer")
	}
	h.sealProducerDemand(second.key.scope, second.producers[0])
	h.sealProducerDemand(second.key.scope, second.producers[0])
	jobs.sealMu.Lock()
	after := len(jobs.sealed)
	jobs.sealMu.Unlock()
	if after != 1 {
		t.Fatal("last production owner did not seal exactly once")
	}
	h.retire(first)
	h.retire(second)
	base.mu.Lock()
	cancels := len(base.cancels)
	base.mu.Unlock()
	if cancels != 1 {
		t.Fatal("final artifact retirement did not invalidate retained output")
	}
}

func TestHLSPlaybackDemandSealFailureStillStopsProduction(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base, sealErr: transcode.ErrUnsupported}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "seal-failure", true)
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 1, "Paused", 0))
	base.mu.Lock()
	cancels := len(base.cancels)
	base.mu.Unlock()
	if cancels != 1 {
		t.Fatal("failed sealing silently left an unowned encoder running")
	}
}

func TestHLSPlaybackDemandSealedProducerRejectsLateProductionAttachment(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "late-sealed-attachment", true)
	oldID := session.producers[0].id
	// Another registration already holds this old Ensure ID, but has not yet
	// acquired its production lease when the last original owner pauses.
	h.sealProducerDemand(session.key.scope, session.producers[0])
	if producer, err := h.ownProducer(session.key.scope, oldID, 0, 3); !errors.Is(err, errHLSAdmissionStale) || producer.ownership != nil {
		t.Fatalf("late Ensure result reattached sealed production: %+v/%v", producer, err)
	}
	if h.producerReleased(session.producers[0]) {
		t.Fatal("rejecting production attachment lost retained artifact ownership")
	}
}

func TestHLSPlaybackDemandCompletedCacheOwnerSurvivesOldProductionOwner(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	first := hlsRuntimeTestSession(t, h, "completed-shared", true)
	jobs.snapshotState = "completed"
	second := hlsRuntimeTestSession(t, h, "completed-owner", false)
	second.key.scope = first.key.scope
	producer, err := h.ownProducer(first.key.scope, first.producers[0].id, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	second.producers = []hlsProducer{producer}
	h.retire(first)
	jobs.sealMu.Lock()
	seals := len(jobs.sealed)
	jobs.sealMu.Unlock()
	base.mu.Lock()
	cancels := len(base.cancels)
	base.mu.Unlock()
	if seals != 0 || cancels != 0 || h.producerReleased(second.producers[0]) {
		t.Fatal("a departing old production owner invalidated completed shared cache")
	}
	h.retire(second)
}

func TestHLSPlaybackDemandRegistrationInheritsCommittedPause(t *testing.T) {
	h, _ := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "registration-paused", false)
	// Historical paused rows are upgraded with revision zero. The first fresh
	// authorization must initialize that state rather than treat it as stale.
	h.applyPlaybackSnapshot(hlsDemandTestPlay(session, 0, "Paused", 2*media.TicksPerSecond))
	_, err := h.segment(context.Background(), session, hlsRuntimeInput(t), 0)
	if !errors.Is(err, transcode.ErrOutputUnavailable) || !session.demand.paused {
		t.Fatal("a registration created after Pause resumed production")
	}
}

func TestHLSPlaybackDemandDoesNotPauseGeneratedProgressiveOrLiveProduction(t *testing.T) {
	for _, mode := range []string{"generated", "progressive", "live"} {
		t.Run(mode, func(t *testing.T) {
			h, base := hlsRuntimeTestFixture(t)
			jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base}
			h.manager = jobs
			session := hlsRuntimeTestSession(t, h, "independent-"+mode, true)
			play := hlsDemandTestPlay(session, 1, "Paused", 0)
			switch mode {
			case "generated":
				session.key.plan.HLS.SegmentType = "fmp4"
			case "progressive":
				session.key.plan.OutputMode = "progressive"
			case "live":
				session.key.plan.SourceMode, play.IsDynamic = "stream", true
			}
			h.applyPlaybackSnapshot(play)
			jobs.sealMu.Lock()
			seals := len(jobs.sealed)
			jobs.sealMu.Unlock()
			if seals != 0 || session.ctx.Err() != nil {
				t.Fatal("finite legacy demand changed another output contract")
			}
		})
	}
}

func TestHLSProductionWindowBoundsLookaheadAndPreservesLongCopiedGOP(t *testing.T) {
	for _, seconds := range []int64{1, 6, 20, 90} {
		timeline := transcode.Timeline{}
		for number := 0; number < 40; number++ {
			timeline.Segments = append(timeline.Segments, transcode.TimelineSegment{Number: number,
				StartTicks: int64(number) * seconds * media.TicksPerSecond, DurationTicks: seconds * media.TicksPerSecond})
		}
		last := hlsProductionLast(timeline, 10)
		if last < 10 || last >= 10+hlsProducerSpan {
			t.Fatal("production window exceeded segment count or lost its requested GOP")
		}
		if seconds > 60 && last != 10 || seconds <= 60 && timeline.Segments[last].StartTicks+timeline.Segments[last].DurationTicks-timeline.Segments[10].StartTicks > hlsProductionLookaheadTicks {
			t.Fatal("production window invented a cut or exceeded additional lookahead")
		}
	}
}

func TestHLSUnsequencedFarGETWaitsWithoutCancellingCurrentAdmission(t *testing.T) {
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "late-get", false)
	const ticks = int64(60_000_000)
	for number := 4; number < 600; number++ {
		session.timeline.Segments = append(session.timeline.Segments, transcode.TimelineSegment{Number: number,
			StartTicks: int64(number) * ticks, DurationTicks: ticks})
	}
	session.key.plan.DurationTicks = 600 * ticks
	_, firstResult := hlsAdmissionSegmentRequest(t, h, session, context.Background(), 0)
	first := hlsAdmissionCall(t, jobs)
	observed := hlsAdmissionObserve(context.Background())
	_, farResult := hlsAdmissionSegmentRequest(t, h, session, observed, 300)
	hlsAdmissionJoined(t, observed)
	if first.spec.Plan.EndTicks-first.spec.Plan.StartTicks > hlsProductionLookaheadTicks {
		t.Fatal("fallback admission is not a bounded window")
	}
	select {
	case <-firstResult:
		t.Fatal("a late unsequenced GET cancelled current admission")
	case <-jobs.cancels:
		t.Fatal("a late unsequenced GET invalidated current producer")
	default:
	}
	first.finish()
	hlsAdmissionResult(t, firstResult, errHLSRuntimeTestReleased)
	next := hlsAdmissionCall(t, jobs)
	if next.spec.Plan.SegmentStartNumber != 300 {
		t.Fatal("GET-before-Progress fallback could not reach the new seek")
	}
	next.finish()
	hlsAdmissionResult(t, farResult, errHLSRuntimeTestReleased)
	select {
	case <-jobs.cancels:
		t.Fatal("independent fallback windows cancelled one another")
	default:
	}
}

func TestHLSUnsequencedFarGETRetainsActiveProducerAndCreatesBoundedFallback(t *testing.T) {
	h, jobs := hlsRuntimeTestFixture(t)
	session := hlsRuntimeTestSession(t, h, "active-late-get", false)
	const ticks = int64(60_000_000)
	for number := 4; number < 600; number++ {
		session.timeline.Segments = append(session.timeline.Segments, transcode.TimelineSegment{Number: number,
			StartTicks: int64(number) * ticks, DurationTicks: ticks})
	}
	session.key.plan.DurationTicks = 600 * ticks
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, number := range []int{0, 300} {
		input := hlsRuntimeInput(t)
		go func(number int) {
			_, err := h.segment(ctx, session, input, number)
			results <- err
		}(number)
		select {
		case call := <-jobs.opens:
			if call.name != "segment-"+paddedSegmentNumber(number)+".ts" {
				t.Fatal("independent fallback selected another segment")
			}
		case <-ctx.Done():
			t.Fatal("independent fallback could not enter the output wait")
		}
	}
	jobs.mu.Lock()
	ensures, cancels := len(jobs.ensured), len(jobs.cancels)
	for _, spec := range jobs.ensured {
		if spec.Plan.EndTicks-spec.Plan.StartTicks > hlsProductionLookaheadTicks {
			t.Error("independent fallback exceeded bounded lookahead")
		}
	}
	jobs.mu.Unlock()
	if ensures != 2 || cancels != 0 {
		t.Fatal("unsequenced GET revoked current production or lost compatible seek fallback")
	}
	jobs.releaseAll()
	for range 2 {
		select {
		case err := <-results:
			if !errors.Is(err, errHLSRuntimeTestReleased) {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("released independent output waits did not finish")
		}
	}
}
