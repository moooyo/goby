//go:build linux

package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type generatedWindowSlotRepository struct{}

func (*generatedWindowSlotRepository) Recover(context.Context) error                  { return nil }
func (*generatedWindowSlotRepository) Create(context.Context, transcode.Record) error { return nil }
func (*generatedWindowSlotRepository) Update(context.Context, transcode.Record) error { return nil }

type generatedWindowSlotJobs struct {
	mu                sync.Mutex
	backend           *transcode.Manager
	calls             int
	cancels           []string
	started           chan *os.File
	unblock           chan struct{}
	unblockOnce       sync.Once
	cancelStarted     chan struct{}
	cancelRelease     chan struct{}
	cancelStartedOnce sync.Once
	cancelReleaseOnce sync.Once
	forceCacheMiss    bool
	forceMissingName  string
}

func (jobs *generatedWindowSlotJobs) releaseInput() {
	jobs.unblockOnce.Do(func() {
		if jobs.unblock != nil {
			close(jobs.unblock)
		}
	})
}

func (jobs *generatedWindowSlotJobs) releaseCancel() {
	jobs.cancelReleaseOnce.Do(func() {
		if jobs.cancelRelease != nil {
			close(jobs.cancelRelease)
		}
	})
}

func (jobs *generatedWindowSlotJobs) release() { jobs.releaseInput(); jobs.releaseCancel() }

func (jobs *generatedWindowSlotJobs) Ensure(ctx context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	jobs.mu.Lock()
	jobs.calls++
	first := jobs.calls == 1 && jobs.started != nil
	jobs.mu.Unlock()
	if first {
		jobs.started <- input
		<-jobs.unblock
		_ = input.Close()
		return transcode.Record{ID: "abandoned-private-window", Spec: spec, State: "queued"}, nil
	}
	if jobs.backend == nil {
		_ = input.Close()
		return transcode.Record{}, transcode.ErrBusy
	}
	return jobs.backend.Ensure(ctx, spec, input)
}

func (jobs *generatedWindowSlotJobs) Snapshot(scope transcode.Scope, id string) (transcode.Record, error) {
	if jobs.backend != nil {
		return jobs.backend.Snapshot(scope, id)
	}
	return transcode.Record{}, transcode.ErrJobNotFound
}

func (jobs *generatedWindowSlotJobs) TryOpen(scope transcode.Scope, id, name string) (*transcode.ReadHandle, error) {
	if jobs.forceCacheMiss || jobs.forceMissingName == name {
		return nil, transcode.ErrOutputUnavailable
	}
	if jobs.backend != nil {
		return jobs.backend.TryOpen(scope, id, name)
	}
	return nil, transcode.ErrOutputUnavailable
}

func (jobs *generatedWindowSlotJobs) Open(ctx context.Context, scope transcode.Scope, id, name string) (*transcode.ReadHandle, error) {
	if jobs.backend != nil {
		return jobs.backend.Open(ctx, scope, id, name)
	}
	return nil, transcode.ErrOutputUnavailable
}

func (jobs *generatedWindowSlotJobs) CancelJob(id string, scope transcode.Scope) error {
	jobs.mu.Lock()
	jobs.cancels = append(jobs.cancels, id)
	jobs.mu.Unlock()
	if id == "abandoned-private-window" && jobs.cancelStarted != nil {
		jobs.cancelStartedOnce.Do(func() { close(jobs.cancelStarted) })
		<-jobs.cancelRelease
	}
	if jobs.backend != nil {
		return jobs.backend.CancelJob(id, scope)
	}
	return nil
}

func (jobs *generatedWindowSlotJobs) Close(ctx context.Context) error {
	if jobs.backend != nil {
		return jobs.backend.Close(ctx)
	}
	return nil
}

func (jobs *generatedWindowSlotJobs) GeneratedWindowInputEvidence(ctx context.Context, scope transcode.Scope, id string) ([transcode.MaxHLSRenditions]transcode.GeneratedInputEvidence, error) {
	if err := ctx.Err(); err != nil {
		return [transcode.MaxHLSRenditions]transcode.GeneratedInputEvidence{}, err
	}
	if jobs.backend != nil {
		return jobs.backend.GeneratedWindowInputEvidence(ctx, scope, id)
	}
	return [transcode.MaxHLSRenditions]transcode.GeneratedInputEvidence{}, transcode.ErrOutputUnavailable
}

func (jobs *generatedWindowSlotJobs) HLSClock(ctx context.Context, scope transcode.Scope, id string, rendition int) (transcode.HLSMuxClock, error) {
	if jobs.backend != nil {
		return jobs.backend.HLSClock(ctx, scope, id, rendition)
	}
	return transcode.HLSMuxClock{}, transcode.ErrOutputUnavailable
}

func generatedWindowSlotFixture(t *testing.T, jobs *generatedWindowSlotJobs, source *os.File) (*hlsRuntime, *hlsSession, media.Info) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	h := &hlsRuntime{manager: jobs, ctx: ctx, cancel: cancel, probes: make(chan struct{}, 2),
		sessions: make(map[string]*hlsSession), byKey: make(map[hlsKey]*hlsSession)}
	session := hlsRuntimeTestSession(t, h, "window-slot", false)
	plan, info, certificate := generatedWindowGraphPlanFixture()
	session.key.plan = plan
	stat, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	certificate.SourceIdentity, err = media.VideoSeekSourceIdentity(stat)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := hlsGeneratedWindowTimeline(plan, info, certificate)
	if err != nil {
		t.Fatal(err)
	}
	session.windowGraph = &hlsGeneratedWindowGraph{endpoint: certificate, timeline: timeline, basePlan: plan, published: true, slots: make(map[int]hlsGeneratedWindowBinding)}
	t.Cleanup(func() {
		cancel()
		jobs.release()
		h.workers.Wait()
		session.mu.Lock()
		h.releaseGeneratedWindowPinsLocked(session)
		session.mu.Unlock()
	})
	return h, session, info
}

func TestGeneratedWindowSlotRejectsUnpublishedForeignAndPausedColdDemand(t *testing.T) {
	for _, condition := range []string{"foreign_graph", "unpublished", "paused", "foreign_slot", "negative_variant", "foreign_variant"} {
		t.Run(condition, func(t *testing.T) {
			jobs := &generatedWindowSlotJobs{}
			source := hlsRuntimeInput(t)
			h, session, info := generatedWindowSlotFixture(t, jobs, source)
			graphID, number, variant, want := session.id, 0, 0, transcode.ErrJobNotFound
			switch condition {
			case "foreign_graph":
				graphID = "another-graph"
			case "unpublished":
				session.windowGraph.published = false
			case "paused":
				session.demand.paused, want = true, transcode.ErrOutputUnavailable
			case "foreign_slot":
				number, want = len(session.windowGraph.timeline.Segments), errHLSRequestInvalid
			case "negative_variant":
				variant, want = -1, errHLSRequestInvalid
			case "foreign_variant":
				variant, want = max(1, session.windowGraph.basePlan.HLS.RenditionCount), errHLSRequestInvalid
			}
			if _, pin, err := h.generatedWindowSlot(context.Background(), session, source, info, graphID, number, variant); pin != nil || !errors.Is(err, want) {
				t.Fatalf("invalid cold demand reached a producer: pin=%v, error=%v", pin, err)
			}
			if jobs.calls != 0 || len(jobs.cancels) != 0 || session.admission != nil {
				t.Fatal("rejected demand changed admission or producer ownership")
			}
		})
	}
}

func TestGeneratedWindowSlotUnrelatedGETCannotCancelPrivateDemand(t *testing.T) {
	jobs := &generatedWindowSlotJobs{}
	source := hlsRuntimeInput(t)
	h, session, info := generatedWindowSlotFixture(t, jobs, source)
	plan, err := hlsGeneratedWindowSlotPlan(session.key.plan, session.windowGraph.timeline, 15)
	if err != nil {
		t.Fatal(err)
	}
	pending := newHLSAdmission(context.Background(), session, transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: plan}, 15, 15)
	defer pending.stopSession()
	defer pending.cancel()
	session.admission = pending
	if _, _, err := h.generatedWindowSlot(context.Background(), session, source, info, session.id, 0, 0); !errors.Is(err, transcode.ErrBusy) {
		t.Fatalf("unrelated GET displaced active private work: %v", err)
	}
	if pending.ctx.Err() != nil || session.admission != pending || session.admissionRevision != pending.revision || jobs.calls != 0 || len(jobs.cancels) != 0 {
		t.Fatal("an unsequenced GET changed a newer admission's cancellation fence")
	}
}

func TestGeneratedWindowSlotCancellationKeepsBorrowerAndFencesOrphan(t *testing.T) {
	jobs := &generatedWindowSlotJobs{started: make(chan *os.File, 1), unblock: make(chan struct{})}
	source := hlsRuntimeInput(t)
	h, session, info := generatedWindowSlotFixture(t, jobs, source)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
		done <- err
	}()
	var owned *os.File
	select {
	case owned = <-jobs.started:
	case <-time.After(5 * time.Second):
		t.Fatal("private admission did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request cancellation was lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not leave cancelled admission")
	}
	if _, err := source.Stat(); err != nil || owned == source {
		t.Fatal("private admission consumed the borrowed HTTP source")
	}
	jobs.release()
	h.workers.Wait()
	if _, err := owned.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Ensure input escaped cancellation cleanup: %v", err)
	}
	if session.admission != nil || len(session.producers) != 0 || len(session.windowGraph.slots) != 0 || len(jobs.cancels) != 1 {
		t.Fatal("late private completion published a lease or escaped scoped orphan cancellation")
	}
}

func generatedWindowSlotActualFixture(t *testing.T) (*hlsRuntime, *hlsSession, *generatedWindowSlotJobs, *os.File, media.Info) {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual private-slot and reader-pin verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	path := filepath.Join(t.TempDir(), "source.mp4")
	command := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=160x96:r=24:d=12",
		"-an", "-c:v", "libx264", "-threads:v", "1", "-bf", "0", "-g", "24", "-use_editlist", "0", "-movflags", "+faststart", path)
	if data, err := command.CombinedOutput(); err != nil || len(data) != 0 {
		t.Fatalf("source fixture generation failed: %v: %s", err, data)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = source.Close() })
	manager, err := transcode.NewManager(ctx, transcode.Options{Root: filepath.Join(t.TempDir(), "cache"), FFmpegPath: ffmpeg,
		Repository: &generatedWindowSlotRepository{}, Threads: 1, MinFreeBytes: 1, MaxJobs: 1, MaxUserJobs: 1, MaxSessionJobs: 1,
		MaxBytes: 32 << 20, MaxJobBytes: 8 << 20, MaxReaders: 16, MaxJobReaders: 16})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if err := manager.Close(closeCtx); err != nil {
			t.Errorf("private-window cache closure failed: %v", err)
		}
	})
	jobs := &generatedWindowSlotJobs{backend: manager}
	h, session, _ := generatedWindowSlotFixture(t, jobs, source)
	h.server = &Server{cfg: config.Config{FFprobePath: ffprobe}}
	plan, _, _ := generatedWindowGraphPlanFixture()
	plan.DurationTicks = 12 * media.TicksPerSecond
	plan.HLS.RenditionCount = 2
	plan.HLS.Renditions[0] = transcode.HLSRendition{Width: plan.Width, Height: plan.Height, VideoBitrate: plan.VideoBitrate}
	plan.HLS.Renditions[1] = transcode.HLSRendition{Width: 80, Height: 48, VideoBitrate: 96000}
	session.key.plan = plan
	info := media.Info{FormatStartKnown: true, DurationTicks: plan.DurationTicks,
		Streams: []media.Stream{{Index: 0, CodecType: "video", Codec: "h264"}}}
	certificate, err := transcode.MeasureGeneratedMP4SourceEndpoint(ctx, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	timeline, err := hlsGeneratedWindowTimeline(plan, info, certificate)
	if err != nil {
		t.Fatal(err)
	}
	session.windowGraph = &hlsGeneratedWindowGraph{endpoint: certificate, timeline: timeline, basePlan: hlsGeneratedWindowProductionBase(plan, certificate), published: true, slots: make(map[int]hlsGeneratedWindowBinding)}
	return h, session, jobs, source, info
}

func TestGeneratedWindowSlotActualPinsSurvivePauseAndRelease(t *testing.T) {
	h, session, _, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binding, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		t.Cleanup(func() { _ = pin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, binding, pin); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	stored := session.windowGraph.slots[0].redirectPins[0]
	session.demand.paused = true
	session.admissionRevision++
	session.mu.Unlock()
	if stored != pin {
		t.Fatal("redirect grace did not retain the actual manager reader pin")
	}
	cached, next, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if next != nil {
		t.Cleanup(func() { _ = next.Close() })
	}
	if err != nil || cached.producer.id != binding.producer.id {
		t.Fatalf("committed pause denied completed cached media: %+v, %v", cached, err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, cached, next); err != nil {
		t.Fatal(err)
	}
	if _, err := next.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("duplicate redirect retained a second pin for one slot: %v", err)
	}
	if _, err := stored.Stat(); err != nil {
		t.Fatalf("renewing redirect grace closed its original pin: %v", err)
	}
	sibling, siblingPin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 1)
	if siblingPin != nil {
		t.Cleanup(func() { _ = siblingPin.Close() })
	}
	if err != nil || sibling.producer.id != binding.producer.id || sibling.artifactName == binding.artifactName {
		t.Fatalf("ABR variants did not resolve to distinct resources of one completed job: %+v, %v", sibling, err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, sibling, siblingPin); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	root := session.windowGraph.slots[0]
	session.mu.Unlock()
	if root.artifactName != binding.artifactName || root.redirectPins[0] != stored || root.redirectPins[1] != siblingPin {
		t.Fatal("variant redirect overwrote the root artifact or another rendition's pin")
	}
	if _, _, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 1, 0); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("pause admitted a new cold slot: %v", err)
	}
	session.mu.Lock()
	h.releaseExpiredGeneratedWindowPinsLocked(session, time.Now().Add(2*hlsGeneratedWindowGrace))
	remaining := session.windowGraph.slots[0].redirectPins[0]
	session.mu.Unlock()
	if remaining != nil {
		t.Fatal("expired grace retained a reader pin")
	}
	if _, err := stored.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expired grace did not close its descriptor: %v", err)
	}
	if _, err := siblingPin.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expired grace retained sibling pin: %v", err)
	}
}

func TestGeneratedWindowMissingSiblingPreservesIssuedRedirect(t *testing.T) {
	h, session, jobs, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	primary, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		t.Cleanup(func() { _ = pin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, primary, pin); err != nil {
		t.Fatal(err)
	}
	jobs.forceMissingName = primary.artifactNames[1]
	if _, _, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 1); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("missing sibling escaped its bounded cache failure: %v", err)
	}
	available, err := h.generatedWindowExactArtifact(ctx, session, primary.producer.id, primary.artifactName)
	if available != nil {
		t.Cleanup(func() { _ = available.Close() })
	}
	if err != nil {
		t.Fatalf("sibling miss revoked an issued primary redirect: %v", err)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if jobs.calls != 1 || len(jobs.cancels) != 0 {
		t.Fatalf("grace miss started new work or revoked the complete job: calls=%d, cancels=%v", jobs.calls, jobs.cancels)
	}
}

func TestGeneratedWindowExpiredPinAliasCannotBeReattached(t *testing.T) {
	h, session, _, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binding, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 1)
	if pin != nil {
		t.Cleanup(func() { _ = pin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, binding, pin); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	root := session.windowGraph.slots[0]
	root.redirectedUntil = time.Now().Add(-time.Second)
	session.windowGraph.slots[0] = root
	session.mu.Unlock()
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, binding, pin); !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("an expired owned alias became successful redirect retention: %v", err)
	}
	session.mu.Lock()
	retained := session.windowGraph.slots[0].redirectPins[1]
	session.mu.Unlock()
	if retained != nil {
		t.Fatal("expired alias remained in the live pin array")
	}
	if _, err := pin.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("expired alias remained open: %v", err)
	}
}

func TestGeneratedWindowRedirectCannotBorrowAnotherVariantPin(t *testing.T) {
	h, session, _, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, primaryPin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if primaryPin != nil {
		t.Cleanup(func() { _ = primaryPin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	sibling, siblingPin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 1)
	if siblingPin != nil {
		t.Cleanup(func() { _ = siblingPin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, sibling, primaryPin); !errors.Is(err, transcode.ErrInvalidInput) {
		t.Fatalf("one producer ID authorized a different rendition's artifact: %v", err)
	}
	if _, err := primaryPin.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("rejected variant pin leaked: %v", err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, sibling, siblingPin); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	root := session.windowGraph.slots[0]
	session.mu.Unlock()
	if root.redirectPins[0] != nil || root.redirectPins[1] != siblingPin {
		t.Fatal("cross-variant rejection changed another pin slot")
	}
}

func TestGeneratedWindowRejectedRetainedAliasKeepsRootOwnership(t *testing.T) {
	h, session, _, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binding, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		t.Cleanup(func() { _ = pin.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, binding, pin); err != nil {
		t.Fatal(err)
	}
	for _, condition := range []string{"wrong_graph", "wrong_plan", "wrong_variant", "cancelled_context", "foreign_registration"} {
		t.Run(condition, func(t *testing.T) {
			requestContext, graphID, selected, target := ctx, session.id, binding, session
			if condition == "wrong_graph" {
				graphID = "another-graph"
			}
			if condition == "wrong_plan" {
				selected.plan.HLS.Window.StartNumber++
			}
			if condition == "wrong_variant" {
				var valid bool
				selected, valid = hlsGeneratedWindowBindingVariant(binding, 1)
				if !valid {
					t.Fatal("actual sibling binding was not available")
				}
			}
			if condition == "cancelled_context" {
				var abandon context.CancelFunc
				requestContext, abandon = context.WithCancel(ctx)
				abandon()
			}
			if condition == "foreign_registration" {
				target = &hlsSession{id: "other-registration", ctx: ctx, key: session.key}
				graphID = target.id
			}
			if err := h.commitGeneratedWindowRedirect(requestContext, target, graphID, selected, pin); err == nil {
				t.Fatal("a mismatched retained alias became a valid redirect")
			}
			if _, err := pin.Stat(); err != nil {
				t.Fatalf("rejection closed a root-owned pin: %v", err)
			}
			session.mu.Lock()
			retained := session.windowGraph.slots[0].redirectPins[0]
			session.mu.Unlock()
			if retained != pin {
				t.Fatal("rejection changed retained pin ownership")
			}
		})
	}
	session.mu.Lock()
	root := session.windowGraph.slots[0]
	selected, valid := hlsGeneratedWindowBindingVariant(root, 0)
	session.mu.Unlock()
	if !valid || selected.redirectPin != nil || selected.redirectPins != ([transcode.MaxHLSRenditions]*transcode.ReadHandle{}) {
		t.Fatal("selected metadata exposed graph-owned redirect pins")
	}
}

func TestGeneratedWindowRedirectRejectsMismatchedPlanAndReleasesAllPins(t *testing.T) {
	h, session, _, source, info := generatedWindowSlotActualFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binding, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		retained := pin
		t.Cleanup(func() { _ = retained.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	forged := binding
	forged.plan.HLS.Window.StartNumber++
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, forged, pin); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatalf("a mismatched plan inserted a pin under another source slot: %v", err)
	}
	if _, err := pin.Stat(); !errors.Is(err, os.ErrClosed) || len(session.windowGraph.slots) != 1 {
		t.Fatal("rejected redirect leaked its pin or changed the bounded slot map")
	}
	binding, pin, err = h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		retained := pin
		t.Cleanup(func() { _ = retained.Close() })
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := h.commitGeneratedWindowRedirect(ctx, session, session.id, binding, pin); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	h.releaseGeneratedWindowPinsLocked(session)
	h.releaseGeneratedWindowPinsLocked(session)
	remaining := session.windowGraph.slots[0].redirectPins[0]
	session.mu.Unlock()
	if remaining != nil {
		t.Fatal("retirement kept a redirect cache pin")
	}
	if _, err := pin.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("retirement did not close the transferred pin: %v", err)
	}
}

func TestGeneratedWindowCoalescedRetryReturnsCompletedReplacement(t *testing.T) {
	h, session, jobs, source, info := generatedWindowSlotActualFixture(t)
	jobs.started, jobs.unblock = make(chan *os.File, 1), make(chan struct{})
	jobs.cancelStarted, jobs.cancelRelease = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	initiator, abandon := context.WithCancel(ctx)
	first := make(chan error, 1)
	go func() {
		_, pin, err := h.generatedWindowSlot(initiator, session, source, info, session.id, 0, 0)
		if pin != nil {
			_ = pin.Close()
		}
		first <- err
	}()
	select {
	case <-jobs.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	type result struct {
		binding hlsGeneratedWindowBinding
		pin     *transcode.ReadHandle
		err     error
	}
	second := make(chan result, 1)
	secondDone := make(chan struct{})
	coalesced := hlsAdmissionObserve(ctx)
	go func() {
		defer close(secondDone)
		binding, pin, err := h.generatedWindowSlot(coalesced, session, source, info, session.id, 0, 0)
		second <- result{binding: binding, pin: pin, err: err}
	}()
	t.Cleanup(func() {
		cancel()
		jobs.release()
		h.workers.Wait()
		select {
		case <-secondDone:
		case <-time.After(5 * time.Second):
			t.Error("coalesced request did not leave after cancellation")
			return
		}
		select {
		case got := <-second:
			if got.pin != nil {
				_ = got.pin.Close()
			}
		default:
		}
	})
	hlsAdmissionJoined(t, coalesced)
	abandon()
	select {
	case err := <-first:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("initiator did not abandon its private work: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	jobs.releaseInput()
	select {
	case <-jobs.cancelStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	jobs.mu.Lock()
	callsBeforeFence := jobs.calls
	jobs.mu.Unlock()
	if callsBeforeFence != 1 {
		t.Fatal("replacement started before the orphan cancellation fence")
	}
	select {
	case got := <-second:
		if got.pin != nil {
			_ = got.pin.Close()
		}
		t.Fatalf("coalesced demand left before orphan cleanup: %v", got.err)
	default:
	}
	jobs.releaseCancel()
	select {
	case got := <-second:
		if got.pin != nil {
			defer got.pin.Close()
		}
		if got.err != nil || got.pin == nil || got.binding.producer.id == "abandoned-private-window" {
			t.Fatalf("active coalesced demand did not receive its completed replacement: %+v", got)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	h.workers.Wait()
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if jobs.calls != 2 || len(jobs.cancels) != 1 || jobs.cancels[0] != "abandoned-private-window" {
		t.Fatalf("replacement crossed the orphan fence or retried without a bound: calls=%d, cancels=%v", jobs.calls, jobs.cancels)
	}
}

func TestGeneratedWindowSlotAllRedirectGraceEntriesBlockColdAdmission(t *testing.T) {
	jobs := &generatedWindowSlotJobs{}
	source := hlsRuntimeInput(t)
	h, session, info := generatedWindowSlotFixture(t, jobs, source)
	for number := 0; number < maxHLSProducers; number++ {
		plan, err := hlsGeneratedWindowSlotPlan(session.key.plan, session.windowGraph.timeline, number)
		if err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("retained-window-%d", number)
		producer := hlsProducer{id: id, first: hlsGeneratedWindowProducer, last: hlsGeneratedWindowProducer,
			ownership: &hlsProducerOwnership{key: hlsProducerKey{scope: session.key.scope, id: id}}}
		session.producers = append(session.producers, producer)
		session.windowGraph.slots[number] = hlsGeneratedWindowBinding{producer: producer, plan: plan,
			artifactName: fmt.Sprintf("segment-%06d.ts", number), redirectedUntil: time.Now().Add(time.Minute)}
	}
	if _, _, err := h.generatedWindowSlot(context.Background(), session, source, info, session.id, maxHLSProducers, 0); !errors.Is(err, transcode.ErrBusy) {
		t.Fatalf("cold admission revoked a recently redirected artifact: %v", err)
	}
	if jobs.calls != 0 || len(jobs.cancels) != 0 || len(session.producers) != maxHLSProducers || len(session.windowGraph.slots) != maxHLSProducers {
		t.Fatal("grace pressure grew the registry or evicted a protected ownership lease")
	}
}

func TestGeneratedWindowCacheMissClassificationKeepsFailuresClosed(t *testing.T) {
	for _, err := range []error{transcode.ErrJobNotFound, transcode.ErrOutputUnavailable, transcode.ErrJobCancelled} {
		if !generatedWindowCacheMiss(err) {
			t.Fatalf("known cache miss was not eligible for bounded admission: %v", err)
		}
	}
	for _, err := range []error{transcode.ErrBusy, transcode.ErrQuota, transcode.ErrInvalidInput, transcode.ErrPersistence, context.Canceled, fmt.Errorf("different failure"),
		errors.Join(transcode.ErrOutputUnavailable, transcode.ErrQuota), errors.Join(transcode.ErrJobNotFound, context.Canceled),
		errors.Join(transcode.ErrJobCancelled, media.ErrProcessRetirementUnknown)} {
		if generatedWindowCacheMiss(err) {
			t.Fatalf("an operational or identity failure became new production: %v", err)
		}
	}
}

func TestGeneratedWindowSlotRepeatedCacheMissHasOneReplacementBound(t *testing.T) {
	h, session, jobs, source, info := generatedWindowSlotActualFixture(t)
	jobs.forceCacheMiss = true
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, pin, err := h.generatedWindowSlot(ctx, session, source, info, session.id, 0, 0)
	if pin != nil {
		defer pin.Close()
	}
	if !errors.Is(err, transcode.ErrOutputUnavailable) {
		t.Fatalf("repeated cache unavailability lost its bounded failure: %v", err)
	}
	h.workers.Wait()
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if jobs.calls != 2 {
		t.Fatalf("one request admitted an unbounded sequence of completed replacements: %d", jobs.calls)
	}
	if len(session.windowGraph.slots) != 0 || len(session.producers) != 0 {
		t.Fatal("unreadable completed replacements retained publication leases")
	}
}
