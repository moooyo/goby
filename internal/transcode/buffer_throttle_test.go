//go:build linux

package transcode

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func bufferThrottleTestSpec(t *testing.T, index int) Spec {
	t.Helper()
	spec := managerTestSpec(index)
	spec.Plan = Plan{Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0, AudioStreamIndex: -1,
		DurationTicks: 120 * ticksPerSecond, Width: 160, Height: 90, FrameRate: 8, VideoBitrate: 250_000, SegmentSeconds: 6}
	var err error
	spec.Plan, err = CaptureExecution(spec.Plan, DefaultExecutionOptions(1))
	if err != nil {
		t.Fatal(err)
	}
	return spec
}

func TestPlaybackBufferPolicyCaptureSurvivesQueueAndOutputReuse(t *testing.T) {
	type invocation struct {
		plan       Plan
		controller *playbackBufferController
		demand     PlaybackBufferDemand
	}
	started := make(chan invocation, 4)
	run := func(ctx context.Context, _, _ string, _ *os.File, plan Plan, _ int, _ func(Progress)) (RunResult, error) {
		control := playbackBufferRuntimeFromContext(ctx).controller
		var demand PlaybackBufferDemand
		if control != nil {
			control.mu.Lock()
			demand = control.demand
			control.mu.Unlock()
		}
		started <- invocation{plan: plan, controller: control, demand: demand}
		<-ctx.Done()
		return RunResult{}, ctx.Err()
	}
	options := managerTestOptions(t, run)
	options.MaxJobs = 1
	manager := newTestManager(t, options)
	await := func() invocation {
		t.Helper()
		select {
		case got := <-started:
			return got
		case <-time.After(3 * time.Second):
			t.Fatal("captured HLS producer did not launch")
			return invocation{}
		}
	}
	first := bufferThrottleTestSpec(t, 1)
	firstDemand := PlaybackBufferDemand{Known: true, Revision: 1}
	firstContext := WithPlaybackBufferThrottling(context.Background(), true, firstDemand)
	firstRecord, err := manager.Ensure(firstContext, first, managerTestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	firstRun := await()
	if firstRun.controller == nil || firstRun.demand != firstDemand || firstRun.plan != first.Plan {
		t.Fatal("first producer lost its scheduling capture or changed its output plan")
	}
	// A settings change for the same output cannot change an existing producer.
	changedContext := WithPlaybackBufferThrottling(context.Background(), false, PlaybackBufferDemand{Known: true, Revision: 9, PositionTicks: 100 * ticksPerSecond})
	reused, err := manager.Ensure(changedContext, first, managerTestInput(t))
	if err != nil || reused.ID != firstRecord.ID || reused.Spec != firstRecord.Spec {
		t.Fatalf("scheduling preferences split an identical output identity: %+v/%v", reused, err)
	}
	firstRun.controller.mu.Lock()
	retained := firstRun.controller.demand
	firstRun.controller.mu.Unlock()
	if retained != firstDemand {
		t.Fatal("output reuse reinterpreted the original producer's scheduling policy")
	}
	second := bufferThrottleTestSpec(t, 2)
	secondDemand := PlaybackBufferDemand{Known: true, Revision: 2, PositionTicks: 10 * ticksPerSecond}
	secondContext := WithPlaybackBufferThrottling(context.Background(), true, secondDemand)
	queued, err := manager.Ensure(secondContext, second, managerTestInput(t))
	if err != nil || queued.State != "queued" || queued.Spec.Plan != second.Plan {
		t.Fatalf("second producer was not admitted with its immutable output: %+v/%v", queued, err)
	}
	queuedReuse, err := manager.Ensure(changedContext, second, managerTestInput(t))
	if err != nil || queuedReuse.ID != queued.ID {
		t.Fatalf("later settings replaced a queued producer: %+v/%v", queuedReuse, err)
	}
	if err := manager.Cancel(context.Background(), first.Scope); err != nil {
		t.Fatal(err)
	}
	secondRun := await()
	if secondRun.controller == nil || secondRun.demand != secondDemand || secondRun.plan != queued.Spec.Plan {
		t.Fatal("queue launch adopted a later scheduling policy or output tuple")
	}
	if err := manager.Cancel(context.Background(), second.Scope); err != nil {
		t.Fatal(err)
	}
	third := bufferThrottleTestSpec(t, 3)
	if _, err := manager.Ensure(changedContext, third, managerTestInput(t)); err != nil {
		t.Fatal(err)
	}
	if got := await(); got.controller != nil || got.plan != third.Plan {
		t.Fatal("a newly disabled policy changed the output or retained a throttle controller")
	}
}

func bufferThrottleTestManager(t *testing.T, maxReaders, maxJobReaders int) (*Manager, *managedJob) {
	t.Helper()
	spec := bufferThrottleTestSpec(t, 1)
	spec.Plan.SegmentMode = "vod"
	spec.Plan.EndTicks = 60 * ticksPerSecond
	spec.Plan.SegmentTimes = "60000000,120000000,180000000,240000000,300000000,360000000,420000000,480000000,540000000"
	controller := newPlaybackBufferController(spec.Plan, playbackBufferAdmission{enabled: true, demand: PlaybackBufferDemand{Known: true, Revision: 1}})
	controller.setReady(true)
	job := &managedJob{record: Record{ID: "owned-buffer-producer", Spec: spec, State: "running"}, bufferThrottle: controller, changed: make(chan struct{})}
	manager := &Manager{options: Options{MaxReaders: maxReaders, MaxJobReaders: maxJobReaders}, jobs: map[string]*managedJob{job.record.ID: job}}
	return manager, job
}

func TestPlaybackBufferPendingReadersHaveIndependentBoundedDemand(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		revision int64
	}{{"historical_zero", 0}, {"captured_prepared_revision", 5}} {
		t.Run(fixture.name, func(t *testing.T) { checkPlaybackBufferPendingReaders(t, fixture.revision) })
	}
}

func checkPlaybackBufferPendingReaders(t *testing.T, initialRevision int64) {
	t.Helper()
	manager, job := bufferThrottleTestManager(t, 2, 2)
	originalOutput := job.record.Spec
	job.bufferThrottle = newPlaybackBufferController(job.record.Spec.Plan,
		playbackBufferAdmission{enabled: true, demand: PlaybackBufferDemand{Revision: initialRevision}})
	job.bufferThrottle.setReady(true)
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan bool, 8)
	done := make(chan error, 1)
	runtime := playbackBufferRuntime{controller: job.bufferThrottle}
	go func() { done <- runtime.run(ctx, func(paused bool) error { signals <- paused; return nil }) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("stop scheduling observer: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("scheduling observer did not join cancellation")
		}
	}()
	awaitSignal := func(wanted bool) {
		t.Helper()
		select {
		case got := <-signals:
			if got != wanted {
				t.Fatalf("unexpected process control transition: got=%v wanted=%v", got, wanted)
			}
		case <-time.After(time.Second):
			t.Fatalf("process control did not transition to paused=%v", wanted)
		}
	}
	job.bufferThrottle.progress(Progress{OutputTicks: 40 * ticksPerSecond})
	if initialRevision > 0 {
		if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Known: true, Revision: initialRevision - 1}); err != nil {
			t.Fatal(err)
		}
		select {
		case paused := <-signals:
			t.Fatalf("an older report changed captured unknown playback: paused=%v", paused)
		case <-time.After(50 * time.Millisecond):
		}
		job.bufferThrottle.mu.Lock()
		retained := job.bufferThrottle.demand
		job.bufferThrottle.mu.Unlock()
		if retained.Known || retained.Revision != initialRevision {
			t.Fatal("an older known position replaced a newer captured unknown revision")
		}
	}
	// Fresh authorization may synchronize an unknown historical revision zero
	// before its first committed known position is delivered at revision zero.
	if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Revision: initialRevision}); err != nil {
		t.Fatal(err)
	}
	select {
	case paused := <-signals:
		t.Fatalf("unknown playback position caused process control: paused=%v", paused)
	case <-time.After(50 * time.Millisecond):
	}
	if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Known: true, Revision: initialRevision}); err != nil {
		t.Fatal(err)
	}
	awaitSignal(true)
	if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Known: true, Revision: initialRevision, PositionTicks: 100 * ticksPerSecond}); err != nil {
		t.Fatal(err)
	}
	select {
	case paused := <-signals:
		t.Fatalf("a repeated known revision changed playback position: paused=%v", paused)
	case <-time.After(50 * time.Millisecond):
	}
	first, err := manager.beginPlaybackBufferOutputDemand(job.record.Spec.Scope, job.record.ID, "segment-000007.ts", false)
	if err != nil || first == nil {
		t.Fatalf("first pending delivery did not acquire its bounded demand: %v", err)
	}
	awaitSignal(false)
	second, err := manager.beginPlaybackBufferOutputDemand(job.record.Spec.Scope, job.record.ID, "segment-000007.ts", false)
	if err != nil || second == nil {
		t.Fatalf("a shared artifact waiter did not retain independent demand: %v", err)
	}
	if release, err := manager.beginPlaybackBufferOutputDemand(job.record.Spec.Scope, job.record.ID, "segment-000008.ts", false); release != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("pending delivery escaped its waiter budget: release=%v err=%v", release != nil, err)
	}
	first()
	first()
	select {
	case paused := <-signals:
		t.Fatalf("one departing waiter revoked another pending delivery: paused=%v", paused)
	case <-time.After(50 * time.Millisecond):
	}
	second()
	awaitSignal(true)
	manager.mu.Lock()
	waiters, jobWaiters, output := manager.bufferWaiters, job.bufferWaiters, job.record.Spec
	manager.mu.Unlock()
	job.bufferThrottle.mu.Lock()
	demand := job.bufferThrottle.demand
	job.bufferThrottle.mu.Unlock()
	if waiters != 0 || jobWaiters != 0 || demand.PositionTicks != 0 || demand.Revision != initialRevision || output != originalOutput {
		t.Fatal("delivery demand leaked a waiter or rewrote committed playback/output state")
	}
}

func TestPlaybackBufferScopeAndArtifactBoundsCannotBypassScheduling(t *testing.T) {
	manager, job := bufferThrottleTestManager(t, 1, 1)
	foreign := job.record.Spec.Scope
	foreign.UserID = "another-viewer"
	if release, err := manager.beginPlaybackBufferOutputDemand(foreign, job.record.ID, "segment-000002.ts", false); release != nil || !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("foreign ownership granted production demand: release=%v err=%v", release != nil, err)
	}
	for _, name := range []string{"segment-000010.ts", "segment-999999.ts", "segment-00000000.ts", "segment-0.ts", "v0-segment-000002.ts", "segment-000002.m4s", "init.mp4"} {
		if release, err := manager.beginPlaybackBufferOutputDemand(job.record.Spec.Scope, job.record.ID, name, false); release != nil || !errors.Is(err, ErrOutputUnavailable) {
			t.Fatalf("unconstructible artifact %q granted production demand: release=%v err=%v", name, release != nil, err)
		}
	}
	if err := manager.UpdatePlaybackBufferDemand(foreign, PlaybackBufferDemand{Known: true, Revision: 10, PositionTicks: 100 * ticksPerSecond}); err != nil {
		t.Fatal(err)
	}
	if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Known: true, Revision: 1, PositionTicks: 100 * ticksPerSecond}); err != nil {
		t.Fatal(err)
	}
	job.bufferThrottle.mu.Lock()
	retained := job.bufferThrottle.demand
	job.bufferThrottle.mu.Unlock()
	if retained != (PlaybackBufferDemand{Known: true, Revision: 1}) || manager.bufferWaiters != 0 || job.bufferWaiters != 0 {
		t.Fatal("foreign or late demand changed scheduling or escaped artifact validation")
	}
	if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Known: true, Revision: 2, PositionTicks: 20 * ticksPerSecond}); err != nil {
		t.Fatal(err)
	}
	job.finished, job.record.State = true, "cancelled"
	if err := manager.UpdatePlaybackBufferDemand(job.record.Spec.Scope, PlaybackBufferDemand{Known: true, Revision: 3, PositionTicks: 100 * ticksPerSecond}); err != nil {
		t.Fatal(err)
	}
	job.bufferThrottle.mu.Lock()
	retained = job.bufferThrottle.demand
	job.bufferThrottle.mu.Unlock()
	if retained != (PlaybackBufferDemand{Known: true, Revision: 2, PositionTicks: 20 * ticksPerSecond}) {
		t.Fatal("late delivery demand reactivated a terminal producer")
	}
	// A complete-source generated job can publish a rounding tail at EOF, but
	// guessing that extra ordinal must not grant future production demand.
	generatedSpec := job.record.Spec
	generatedSpec.Plan.SegmentMode, generatedSpec.Plan.EndTicks, generatedSpec.Plan.SegmentTimes = "", 0, ""
	generatedSpec.Plan.Container, generatedSpec.Plan.HLS = "mp4", HLSPlan{SegmentType: "fmp4"}
	generatedDemand := PlaybackBufferDemand{Known: true, Revision: 2, PositionTicks: 20 * ticksPerSecond}
	generated := &managedJob{record: Record{ID: "owned-generated-producer", Spec: generatedSpec, State: "running"}, changed: make(chan struct{}),
		bufferThrottle: newPlaybackBufferController(generatedSpec.Plan, playbackBufferAdmission{enabled: true, demand: generatedDemand})}
	manager.jobs[generated.record.ID] = generated
	for _, request := range []struct {
		name    string
		failure error
	}{{"segment-000020.m4s", ErrOutputUnavailable}, {"segment-000019.m4s", context.DeadlineExceeded}} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		handle, err := manager.Open(ctx, generatedSpec.Scope, generated.record.ID, request.name)
		cancel()
		if handle != nil {
			_ = handle.Close()
		}
		if handle != nil || !errors.Is(err, request.failure) {
			t.Fatalf("generated nominal range changed delivery admission: name=%s err=%v", request.name, err)
		}
	}
	generated.bufferThrottle.mu.Lock()
	retained = generated.bufferThrottle.demand
	pendingOutput := generated.bufferThrottle.pendingOutput
	generated.bufferThrottle.mu.Unlock()
	if manager.bufferWaiters != 0 || generated.bufferWaiters != 0 || pendingOutput != 0 || generated.record.Spec != generatedSpec || retained != generatedDemand {
		t.Fatal("cancelled nominal delivery leaked its single lease or changed playback/output state")
	}
}

func TestPlaybackBufferWaitersRespectSeparateJobAndGlobalLimits(t *testing.T) {
	manager, firstJob := bufferThrottleTestManager(t, 2, 1)
	newJob := func(id, playID string) *managedJob {
		job := &managedJob{record: firstJob.record, changed: make(chan struct{})}
		job.record.ID = id
		job.record.Spec.Scope.PlaySessionID = playID
		job.bufferThrottle = newPlaybackBufferController(job.record.Spec.Plan,
			playbackBufferAdmission{enabled: true, demand: PlaybackBufferDemand{Known: true, Revision: 1}})
		manager.jobs[id] = job
		return job
	}
	secondJob := newJob("second-owned-producer", "second-playback")
	thirdJob := newJob("third-owned-producer", "third-playback")
	firstRelease, err := manager.beginPlaybackBufferOutputDemand(firstJob.record.Spec.Scope, firstJob.record.ID, "segment-000007.ts", false)
	if err != nil || firstRelease == nil {
		t.Fatalf("first job did not acquire a delivery lease: %v", err)
	}
	defer firstRelease()
	if release, err := manager.beginPlaybackBufferOutputDemand(firstJob.record.Spec.Scope, firstJob.record.ID, "segment-000008.ts", false); release != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("one job used another job's spare waiter capacity: release=%v err=%v", release != nil, err)
	}
	secondRelease, err := manager.beginPlaybackBufferOutputDemand(secondJob.record.Spec.Scope, secondJob.record.ID, "segment-000007.ts", false)
	if err != nil || secondRelease == nil {
		t.Fatalf("another scoped job could not use spare global capacity: %v", err)
	}
	defer secondRelease()
	if release, err := manager.beginPlaybackBufferOutputDemand(thirdJob.record.Spec.Scope, thirdJob.record.ID, "segment-000007.ts", false); release != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("a third job escaped the global waiter budget: release=%v err=%v", release != nil, err)
	}
	firstRelease()
	thirdRelease, err := manager.beginPlaybackBufferOutputDemand(thirdJob.record.Spec.Scope, thirdJob.record.ID, "segment-000007.ts", false)
	if err != nil || thirdRelease == nil {
		t.Fatalf("released global capacity was not reusable by another scoped job: %v", err)
	}
	thirdRelease()
	secondRelease()
	if manager.bufferWaiters != 0 || firstJob.bufferWaiters != 0 || secondJob.bufferWaiters != 0 || thirdJob.bufferWaiters != 0 {
		t.Fatal("independent scoped delivery leases leaked or were released twice")
	}
}
