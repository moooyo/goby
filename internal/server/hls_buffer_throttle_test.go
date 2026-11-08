//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsBufferThrottleTestGraph(t *testing.T, h *hlsRuntime, session *hlsSession, seconds int) {
	t.Helper()
	plan, info, endpoint := generatedWindowGraphPlanFixture()
	plan.SegmentSeconds = seconds
	plan.DurationTicks = 2400 * media.TicksPerSecond
	info.DurationTicks = plan.DurationTicks
	endpoint.DurationTicks = plan.DurationTicks
	endpoint.SampleCount = 2400 * 24
	endpoint.Last = transcode.GeneratedRational{Num: 2402*24 - 1, Den: 24}
	endpoint.End = transcode.GeneratedRational{Num: 2402, Den: 1}
	timeline, err := hlsGeneratedWindowTimeline(plan, info, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	delete(h.byKey, session.key)
	session.key.plan = plan
	h.byKey[session.key] = session
	session.windowGraph = &hlsGeneratedWindowGraph{endpoint: endpoint, timeline: timeline, basePlan: plan,
		published: true, slots: make(map[int]hlsGeneratedWindowBinding)}
	session.bufferThrottle.enabled = true
}

// These fixtures model the already closed bindings owned by the registry. The
// actual-consumer test below obtains its bindings through native window proof.
func hlsBufferThrottleTestPublishLocked(t *testing.T, h *hlsRuntime, session *hlsSession, number int) hlsGeneratedWindowBinding {
	t.Helper()
	plan, err := hlsGeneratedWindowSlotPlan(session.windowGraph.basePlan, session.windowGraph.timeline, number)
	if err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("buffer-closed-slot-%d", number)
	key := hlsProducerKey{scope: session.key.scope, id: id}
	producer := hlsProducer{id: id, first: hlsGeneratedWindowProducer, last: hlsGeneratedWindowProducer,
		ownership: &hlsProducerOwnership{key: key}}
	if h.producerOwners == nil {
		h.producerOwners = make(map[hlsProducerKey]int)
	}
	h.producerOwners[key]++
	binding := hlsGeneratedWindowBinding{producer: producer, plan: plan,
		closure: transcode.GeneratedWindowClosure{StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks,
			Number: number, RenditionCount: max(1, plan.HLS.RenditionCount), NativeClockVersion: plan.HLS.Window.NativeClockVersion}}
	for variant := 0; variant < max(1, plan.HLS.RenditionCount); variant++ {
		prefix := ""
		if plan.HLS.RenditionCount > 0 {
			prefix = fmt.Sprintf("v%d-", variant)
		}
		binding.artifactNames[variant] = fmt.Sprintf("%ssegment-%06d.ts", prefix, number)
		binding.artifactIdentities[variant] = fmt.Sprintf("%s-identity-%d", id, variant)
		binding.closure.Output[variant].SegmentSHA256[0] = byte(variant + 1)
	}
	binding.artifactName, binding.artifactIdentity = binding.artifactNames[0], binding.artifactIdentities[0]
	session.windowGraph.slots[number] = binding
	session.producers = append(session.producers, producer)
	notifyHLSBufferThrottleLocked(session)
	return binding
}

func hlsBufferThrottleTestFixture(t *testing.T, seconds, count int) (*hlsRuntime, *hlsSession, *hlsDemandTestJobs) {
	t.Helper()
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsDemandTestJobs{hlsRuntimeTestJobs: base, snapshotState: "completed"}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "buffer-throttle", false)
	hlsBufferThrottleTestGraph(t, h, session, seconds)
	for number := 0; number < count; number++ {
		hlsBufferThrottleTestPublish(t, h, session, number)
	}
	return h, session, jobs
}

func hlsBufferThrottleTestPublish(t *testing.T, h *hlsRuntime, session *hlsSession, number int) hlsGeneratedWindowBinding {
	t.Helper()
	session.mu.Lock()
	defer session.mu.Unlock()
	return hlsBufferThrottleTestPublishLocked(t, h, session, number)
}

func hlsBufferThrottleTestReport(h *hlsRuntime, session *hlsSession, revision int64, state string, position int64) library.PlaySession {
	play := hlsDemandTestPlay(session, revision, state, position)
	play.UpdatedAt = time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC).Add(time.Duration(position * 100))
	h.applyPlaybackSnapshot(play)
	return play
}

func hlsBufferThrottleTestAdmission(t *testing.T, h *hlsRuntime, session *hlsSession, number int) (bool, <-chan struct{}) {
	t.Helper()
	session.mu.Lock()
	defer session.mu.Unlock()
	plan, err := hlsGeneratedWindowSlotPlan(session.windowGraph.basePlan, session.windowGraph.timeline, number)
	if err != nil {
		t.Fatal(err)
	}
	return h.bufferThrottleAdmissionLocked(session, plan)
}

func hlsBufferThrottleTestChannelOpen(t *testing.T, changed <-chan struct{}) {
	t.Helper()
	if changed == nil {
		t.Fatal("buffered admission did not expose a wakeup channel")
	}
	select {
	case <-changed:
		t.Fatal("a stale report changed the buffered admission generation")
	default:
	}
}

func hlsBufferThrottleTestChannelClosed(t *testing.T, changed <-chan struct{}) {
	t.Helper()
	select {
	case <-changed:
	default:
		t.Fatal("committed demand or removed coverage did not wake buffered admission")
	}
}

func TestHLSBufferThrottleCommittedProgressKeepsOutputAndOwnership(t *testing.T) {
	h, session, jobs := hlsBufferThrottleTestFixture(t, 6, 5)
	hlsBufferThrottleTestReport(h, session, 2, "Playing", 0)
	paused, changed := hlsBufferThrottleTestAdmission(t, h, session, 5)
	if !paused {
		t.Fatal("a full published prefix did not defer its next producer")
	}
	producer := session.producers[0]
	shared, err := h.ownProducer(session.key.scope, producer.id, hlsGeneratedWindowProducer, hlsGeneratedWindowProducer)
	if err != nil {
		t.Fatal(err)
	}
	plan, revision := session.key.plan, session.admissionRevision
	hlsBufferThrottleTestReport(h, session, 1, "Paused", 25*media.TicksPerSecond)
	hlsBufferThrottleTestReport(h, session, 2, "Playing", 25*media.TicksPerSecond)
	hlsBufferThrottleTestChannelOpen(t, changed)
	if session.demand.position != 0 || session.demand.revision != 2 {
		t.Fatal("late callbacks became playback position or restored production")
	}
	hlsBufferThrottleTestReport(h, session, 3, "Playing", 25*media.TicksPerSecond)
	hlsBufferThrottleTestChannelClosed(t, changed)
	if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, 5); paused {
		t.Fatal("committed consumption did not resume the next producer")
	}
	if session.key.plan != plan || session.admissionRevision != revision || len(session.windowGraph.slots) != 5 ||
		h.producerReleased(producer) || h.producerReleased(shared) || h.producerOwners[producer.ownership.key] != 2 {
		t.Fatal("buffer control changed output identity, admission fencing or shared artifact ownership")
	}
	jobs.sealMu.Lock()
	seals := len(jobs.sealed)
	jobs.sealMu.Unlock()
	jobs.mu.Lock()
	cancels := len(jobs.cancels)
	jobs.mu.Unlock()
	if seals != 0 || cancels != 0 {
		t.Fatal("recoverable buffer control sealed or invalidated an existing producer")
	}
}

func TestHLSBufferThrottleMissingCoverageAndFarRequestsAreNotConsumption(t *testing.T) {
	h, session, _ := hlsBufferThrottleTestFixture(t, 6, 5)
	hlsBufferThrottleTestReport(h, session, 1, "Playing", 0)
	if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, 5); !paused {
		t.Fatal("the complete current prefix did not reach admission throttling")
	}
	if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, 10); paused {
		t.Fatal("a far requested interval was mistaken for the next buffered slot")
	}
	if session.demand.position != 0 || session.demand.revision != 1 {
		t.Fatal("an unsequenced far GET changed committed playback position")
	}
	_, changed := hlsBufferThrottleTestAdmission(t, h, session, 5)
	session.mu.Lock()
	removed := session.windowGraph.slots[2]
	h.discardGeneratedWindowBindingLocked(session, 2, removed)
	session.mu.Unlock()
	hlsBufferThrottleTestChannelClosed(t, changed)
	if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, 5); paused {
		t.Fatal("coverage beyond a missing slot was counted as continuous buffer")
	}
	for _, number := range []int{0, 1, 3, 4} {
		binding := session.windowGraph.slots[number]
		if h.producerReleased(binding.producer) {
			t.Fatal("repairing one missing slot revoked another retained artifact")
		}
	}
	if session.demand.position != 0 {
		t.Fatal("removing a cache binding pretended that playback advanced")
	}
}

func TestHLSBufferThrottleRemainsReachableAcrossWindowEviction(t *testing.T) {
	for _, seconds := range []int{1, 6, 10} {
		t.Run(fmt.Sprintf("segments-%ds", seconds), func(t *testing.T) {
			h, session, _ := hlsBufferThrottleTestFixture(t, seconds, maxHLSProducers)
			hlsBufferThrottleTestReport(h, session, 1, "Playing", 0)
			next, revision := maxHLSProducers, int64(1)
			for round := 0; round < 12; round++ {
				paused, _ := hlsBufferThrottleTestAdmission(t, h, session, next)
				if !paused {
					t.Fatal("the retained-window limit was reached before buffer throttling became possible")
				}
				revision++
				position := int64(next-1) * int64(seconds) * media.TicksPerSecond
				hlsBufferThrottleTestReport(h, session, revision, "Playing", position)
				if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, next); paused {
					t.Fatal("consuming the retained prefix failed to reopen window production")
				}
				admitted := 0
				for admitted < maxHLSProducers {
					if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, next); paused {
						break
					}
					room := func() bool {
						session.mu.Lock()
						defer session.mu.Unlock()
						room := h.makeGeneratedWindowRoomLocked(session, time.Now())
						if room {
							hlsBufferThrottleTestPublishLocked(t, h, session, next)
						}
						if len(session.producers) > maxHLSProducers || len(session.windowGraph.slots) > maxHLSProducers {
							t.Fatal("buffer recovery exceeded the existing producer or slot budget")
						}
						return room
					}()
					if !room {
						t.Fatal("completed window eviction could not make room for consumed playback")
					}
					next++
					admitted++
				}
				if admitted == 0 || admitted == maxHLSProducers {
					t.Fatal("window recovery either made no progress or never returned to buffering control")
				}
			}
		})
	}
}

func TestHLSBufferThrottlePreparedInitialIntentDoesNotInventPlayback(t *testing.T) {
	h, session, _ := hlsBufferThrottleTestFixture(t, 6, 0)
	prepared := hlsBufferThrottleTestReport(h, session, 0, "Prepared", 0)
	h.seedGeneratedWindowInitialIntent(session, prepared, 90*media.TicksPerSecond)
	for number := 15; number < 20; number++ {
		hlsBufferThrottleTestPublish(t, h, session, number)
	}
	if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, 20); paused {
		t.Fatal("Prepared playback was blocked on progress it has not yet reported")
	}
	generatedWindowInitialIntentAssertCommitted(t, session, prepared)
	if !session.demand.initialIntent.valid || !session.demand.initialIntent.captured {
		t.Fatal("buffer comparison consumed or refreshed the negotiated initial intention")
	}
	hlsBufferThrottleTestReport(h, session, 1, "Playing", 90*media.TicksPerSecond)
	if paused, _ := hlsBufferThrottleTestAdmission(t, h, session, 20); !paused {
		t.Fatal("the committed nonzero playback position did not control its actual buffered prefix")
	}
	if session.demand.position != 90*media.TicksPerSecond || session.demand.initialIntent.valid {
		t.Fatal("first playback did not retain its committed position and consume the one-shot intention")
	}
}

func hlsBufferThrottleTestCallerFixture(t *testing.T) (*hlsRuntime, *hlsSession, *generatedWindowSlotJobs, *os.File, media.Info) {
	t.Helper()
	jobs := &generatedWindowSlotJobs{}
	source := hlsRuntimeInput(t)
	h, session, info := generatedWindowSlotFixture(t, jobs, source)
	session.bufferThrottle.enabled = true
	for number := 0; number < 5; number++ {
		hlsBufferThrottleTestPublish(t, h, session, number)
	}
	hlsBufferThrottleTestReport(h, session, 1, "Playing", 0)
	return h, session, jobs, source, info
}

func hlsBufferThrottleTestCaller(t *testing.T, h *hlsRuntime, session *hlsSession, source *os.File, info media.Info, number int, ctx context.Context) (<-chan error, *hlsAdmissionObservedContext) {
	t.Helper()
	observed := hlsAdmissionObserve(ctx)
	done := make(chan error, 1)
	go func() {
		_, pin, err := h.generatedWindowSlot(observed, session, source, info, session.id, number, 0)
		if pin != nil {
			err = errors.Join(err, pin.Close())
		}
		done <- err
	}()
	hlsAdmissionJoined(t, observed)
	session.mu.Lock()
	paused, admission := session.bufferThrottle.admissionPaused, session.admission
	session.mu.Unlock()
	if !paused || admission != nil {
		t.Fatal("the next slot did not wait before private admission")
	}
	return done, observed
}

func hlsBufferThrottleTestNoAdmission(t *testing.T, h *hlsRuntime, session *hlsSession, jobs *generatedWindowSlotJobs, wantCalls int) {
	t.Helper()
	h.mu.Lock()
	admissions, gates := h.admissions, len(h.admissionGates)
	h.mu.Unlock()
	session.mu.Lock()
	pending := session.admission
	session.mu.Unlock()
	jobs.mu.Lock()
	calls := jobs.calls
	jobs.mu.Unlock()
	if admissions != 0 || gates != 0 || pending != nil || calls != wantCalls {
		t.Fatalf("buffered wait borrowed producer resources: admissions=%d, gates=%d, pending=%v, calls=%d", admissions, gates, pending != nil, calls)
	}
}

func TestHLSBufferThrottleCancelledWaitDoesNotAdmitOrConsumeSource(t *testing.T) {
	for _, action := range []string{"request_cancel", "session_retire"} {
		t.Run(action, func(t *testing.T) {
			h, session, jobs, source, info := hlsBufferThrottleTestCallerFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done, _ := hlsBufferThrottleTestCaller(t, h, session, source, info, 5, ctx)
			hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 0)
			want := context.Canceled
			if action == "request_cancel" {
				cancel()
			} else {
				h.retire(session)
				want = transcode.ErrJobNotFound
			}
			select {
			case err := <-done:
				if !errors.Is(err, want) {
					t.Fatalf("buffered cancellation did not retain its cause: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("buffered request did not exit on cancellation")
			}
			hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 0)
			if _, err := source.Stat(); err != nil {
				t.Fatal("buffered wait consumed its borrowed HTTP source")
			}
		})
	}
}

func TestHLSBufferThrottleDeliveryDeadlineDoesNotRefreshOrAdvancePlayback(t *testing.T) {
	for _, condition := range []string{"no_more_reports", "unchanged_position_broadcasts"} {
		t.Run(condition, func(t *testing.T) {
			hlsBufferThrottleTestDeliveryDeadline(t, condition == "unchanged_position_broadcasts")
		})
	}
}

func hlsBufferThrottleTestDeliveryDeadline(t *testing.T, broadcast bool) {
	t.Helper()
	h, session, jobs, source, info := hlsBufferThrottleTestCallerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	done, _ := hlsBufferThrottleTestCaller(t, h, session, source, info, 5, ctx)
	hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 0)
	if broadcast {
		for revision := int64(2); revision <= 4; revision++ {
			time.Sleep(hlsBufferDeliveryWait / 4)
			hlsBufferThrottleTestReport(h, session, revision, "Playing", 0)
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, transcode.ErrBusy) {
			t.Fatalf("delivery credit did not make one ordinary producer attempt: %v", err)
		}
	case <-time.After(3*time.Second - time.Since(started)):
		t.Fatal("broadcast retries refreshed the original delivery deadline")
	}
	if time.Since(started) < hlsBufferDeliveryWait || session.demand.position != 0 || !session.bufferThrottle.admissionPaused {
		t.Fatal("delivery credit bypassed its wait, invented progress or cleared session buffering control")
	}
	h.workers.Wait()
	hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 1)
	nextCtx, nextCancel := context.WithCancel(context.Background())
	next, _ := hlsBufferThrottleTestCaller(t, h, session, source, info, 5, nextCtx)
	nextCancel()
	select {
	case err := <-next:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("a later request reused an earlier request's delivery credit")
		}
	case <-time.After(time.Second):
		t.Fatal("the later buffered request did not leave after cancellation")
	}
	hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 1)
}

func hlsBufferThrottleTestWaitCalls(t *testing.T, jobs *generatedWindowSlotJobs, want int) {
	t.Helper()
	timer := time.NewTimer(hlsBufferDeliveryWait / 2)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		jobs.mu.Lock()
		calls := jobs.calls
		jobs.mu.Unlock()
		if calls == want {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("committed progress did not dispatch before delivery timeout: calls=%d, want=%d", calls, want)
		case <-ticker.C:
		}
	}
}

func TestHLSBufferThrottleActualWindowDeliveryResumesWithoutReleasingPrefix(t *testing.T) {
	h, session, jobs, source, info := generatedWindowSlotActualFixture(t)
	plan := session.key.plan
	plan.SegmentSeconds = 1
	timeline, err := hlsGeneratedWindowTimeline(plan, info, session.windowGraph.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	session.key.plan = plan
	session.windowGraph.basePlan, session.windowGraph.timeline = plan, timeline
	session.bufferThrottle.enabled = true
	hlsBufferThrottleTestReport(h, session, 1, "Playing", 0)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var prefix *transcode.ReadHandle
	var original hlsGeneratedWindowBinding
	for number := 0; number < 6; number++ {
		binding, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, number, 0)
		if err != nil || pin == nil {
			t.Fatalf("native closed prefix could not be delivered: slot=%d, error=%v", number, err)
		}
		if number == 0 {
			prefix, original = pin, binding
			t.Cleanup(func() { _ = prefix.Close() })
		} else if err := pin.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// Delivery precedes the private worker's final descriptor and gate release.
	// Join the completed prefix before attributing resource use to the next GET.
	h.workers.Wait()
	done, _ := hlsBufferThrottleTestCaller(t, h, session, source, info, 6, ctx)
	hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 6)
	session.mu.Lock()
	changed := session.bufferThrottle.changed
	session.mu.Unlock()
	cached, cachedPin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if err != nil || cachedPin == nil || cached.producer.id != original.producer.id {
		t.Fatalf("buffering blocked a completed prefix: %v", err)
	}
	if err := cachedPin.Close(); err != nil {
		t.Fatal(err)
	}
	hlsBufferThrottleTestReport(h, session, 0, "Playing", 10*media.TicksPerSecond)
	hlsBufferThrottleTestChannelOpen(t, changed)
	hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 6)
	hlsBufferThrottleTestReport(h, session, 2, "Playing", 2*media.TicksPerSecond)
	hlsBufferThrottleTestChannelClosed(t, changed)
	hlsBufferThrottleTestWaitCalls(t, jobs, 7)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("consumed buffering did not deliver its next complete window: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("resumed native window did not complete its proof and artifact publication")
	}
	h.workers.Wait()
	hlsBufferThrottleTestNoAdmission(t, h, session, jobs, 7)
	if _, err := prefix.Stat(); err != nil {
		t.Fatalf("resuming production closed the independently held prefix reader: %v", err)
	}
	session.mu.Lock()
	retained := session.windowGraph.slots[0]
	position := session.demand.position
	session.mu.Unlock()
	if retained.producer.ownership != original.producer.ownership || h.producerReleased(retained.producer) || position != 2*media.TicksPerSecond {
		t.Fatal("resumed output replaced prefix ownership or changed committed playback position")
	}
}
