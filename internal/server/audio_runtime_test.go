package server

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

var errAudioRuntimeOpen = errors.New("controlled progressive open failure")

type audioRuntimeRecord struct {
	record transcode.Record
	fenced bool
}

type audioRuntimeCancelBarrier struct {
	entered chan struct{}
	gate    chan struct{}
	claimed bool
	once    sync.Once
}

func (barrier *audioRuntimeCancelBarrier) unblock() {
	barrier.once.Do(func() { close(barrier.gate) })
}

// This fake models the manager's exact-Spec deduplication and immediate
// cancellation fence, without starting a process or reading an opaque reader.
type audioRuntimeJobs struct {
	mu            sync.Mutex
	records       map[string]*audioRuntimeRecord
	bySpec        map[transcode.Spec]*audioRuntimeRecord
	ensureCalls   int
	created       int
	fences        int
	ensureErr     error
	openErr       error
	openEntered   chan struct{}
	waitForCancel bool
	barrier       *audioRuntimeCancelBarrier
}

func newAudioRuntimeJobs() *audioRuntimeJobs {
	return &audioRuntimeJobs{
		records: map[string]*audioRuntimeRecord{},
		bySpec:  map[transcode.Spec]*audioRuntimeRecord{},
	}
}

func (jobs *audioRuntimeJobs) Ensure(ctx context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	// Ensure consumes every supplied descriptor, including duplicate admission
	// and rejection. Closing here represents the manager taking ownership.
	if input != nil {
		_ = input.Close()
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	jobs.ensureCalls++
	if err := ctx.Err(); err != nil {
		return transcode.Record{}, err
	}
	if jobs.ensureErr != nil {
		return transcode.Record{}, jobs.ensureErr
	}
	if previous := jobs.bySpec[spec]; previous != nil && !previous.fenced {
		return previous.record, nil
	}
	jobs.created++
	record := transcode.Record{ID: "audio-runtime-job-" + strconv.Itoa(jobs.created), Spec: spec, State: "running"}
	entry := &audioRuntimeRecord{record: record}
	jobs.records[record.ID], jobs.bySpec[spec] = entry, entry
	return record, nil
}

func (jobs *audioRuntimeJobs) Snapshot(scope transcode.Scope, id string) (transcode.Record, error) {
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	record := jobs.records[id]
	if record == nil || record.record.Spec.Scope != scope {
		return transcode.Record{}, transcode.ErrJobNotFound
	}
	if record.fenced {
		return record.record, transcode.ErrJobCancelled
	}
	return record.record, nil
}

func (jobs *audioRuntimeJobs) CancelJob(id string, scope transcode.Scope) error {
	jobs.mu.Lock()
	barrier, pause := jobs.barrier, false
	if barrier != nil && !barrier.claimed {
		barrier.claimed, pause = true, true
	}
	jobs.mu.Unlock()
	if pause {
		// No fake-manager lock or fence is held at this scheduling boundary.
		// Runtime locks must prevent a replacement from deduplicating the old job.
		close(barrier.entered)
		<-barrier.gate
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	record := jobs.records[id]
	if record == nil || record.record.Spec.Scope != scope {
		return transcode.ErrJobNotFound
	}
	if !record.fenced {
		record.fenced = true
		jobs.fences++
	}
	return nil
}

func (jobs *audioRuntimeJobs) OpenProgressive(ctx context.Context, scope transcode.Scope, id string) (*transcode.ProgressiveReader, error) {
	if _, err := jobs.Snapshot(scope, id); err != nil {
		return nil, err
	}
	jobs.mu.Lock()
	openErr, entered, wait := jobs.openErr, jobs.openEntered, jobs.waitForCancel
	jobs.mu.Unlock()
	if entered != nil {
		select {
		case entered <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if openErr != nil {
		return nil, openErr
	}
	// Tests exercise the HTTP lease only. Calling Read or Close on this opaque
	// value would falsely pretend the fake owns a real manager file lease.
	return &transcode.ProgressiveReader{}, nil
}

func (*audioRuntimeJobs) TryOpen(transcode.Scope, string, string) (*transcode.ReadHandle, error) {
	return nil, transcode.ErrOutputUnavailable
}

func (*audioRuntimeJobs) Open(context.Context, transcode.Scope, string, string) (*transcode.ReadHandle, error) {
	return nil, transcode.ErrOutputUnavailable
}

func (jobs *audioRuntimeJobs) Close(context.Context) error {
	jobs.mu.Lock()
	barrier := jobs.barrier
	jobs.mu.Unlock()
	if barrier != nil {
		barrier.unblock()
	}
	return nil
}

type audioRuntimeFixture struct {
	h         *hlsRuntime
	jobs      *audioRuntimeJobs
	principal identity.Principal
	source    library.MediaFile
	decision  playback.ConversionDecision
	session   *hlsSession
}

func newAudioRuntimeFixture(t *testing.T) *audioRuntimeFixture {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	jobs := newAudioRuntimeJobs()
	h := &hlsRuntime{
		manager: jobs, ctx: ctx, cancel: cancel,
		sessions: map[string]*hlsSession{}, byKey: map[hlsKey]*hlsSession{},
	}
	t.Cleanup(func() { _ = jobs.Close(context.Background()); cancel() })
	fixture := &audioRuntimeFixture{
		h: h, jobs: jobs,
		principal: identity.Principal{
			User: identity.User{ID: "audio-runtime-user"}, SessionID: "audio-runtime-auth",
			Client: identity.Client{DeviceID: "audio-runtime-device"}, Kind: "emby",
		},
		source: library.MediaFile{
			Item:     library.Item{ID: "audio-runtime-item", Type: "Audio"},
			SourceID: media.SourceID("audio-runtime-item"), ETag: "audio-runtime-source-stamp",
		},
		decision: playback.ConversionDecision{Plan: &transcode.Plan{
			OutputMode: "progressive", Container: "mp3", AudioCodec: "mp3",
			VideoStreamIndex: -1, AudioStreamIndex: 5, DurationTicks: 120 * media.TicksPerSecond,
			AudioChannels: 2, AudioSampleRate: 44_100, AudioBitrate: 128_000,
		}},
	}
	fixture.session = fixture.register(t)
	return fixture
}

func (fixture *audioRuntimeFixture) register(t *testing.T) *hlsSession {
	t.Helper()
	session, err := fixture.h.register(fixture.principal, fixture.source, "play_audio-runtime", fixture.decision, 0)
	if err != nil {
		t.Fatalf("register controlled progressive revision: %v", err)
	}
	return session
}

func audioRuntimeLease(t *testing.T, fixture *audioRuntimeFixture, session *hlsSession) (func(), string) {
	t.Helper()
	input := hlsRuntimeInput(t)
	reader, release, err := fixture.h.progressive(context.Background(), session, input)
	if err != nil || reader == nil || release == nil {
		t.Fatalf("open controlled progressive lease: %v", err)
	}
	if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Error("runtime did not transfer its source descriptor to Ensure")
	}
	session.mu.Lock()
	id := session.producers[0].id
	session.mu.Unlock()
	return release, id
}

func audioRuntimeWait(t *testing.T, done <-chan struct{}, description string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", description)
	}
}

func audioRuntimeConcurrentReleases(t *testing.T, releases ...func()) {
	t.Helper()
	start := make(chan struct{})
	var workers sync.WaitGroup
	for _, release := range releases {
		workers.Add(1)
		go func(release func()) {
			defer workers.Done()
			<-start
			release()
			release()
		}(release)
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	close(start)
	audioRuntimeWait(t, done, "concurrent consumer releases")
}

func TestAudioRuntimeOnlyLastUnfinishedConsumerCancelsAndReleaseIsIdempotent(t *testing.T) {
	fixture := newAudioRuntimeFixture(t)
	var releases []func()
	for index := 0; index < 4; index++ {
		release, _ := audioRuntimeLease(t, fixture, fixture.session)
		releases = append(releases, release)
	}
	audioRuntimeConcurrentReleases(t, releases[:2]...)
	fixture.session.mu.Lock()
	readers, closed := fixture.session.progressiveReaders, fixture.session.closed
	fixture.session.mu.Unlock()
	fixture.jobs.mu.Lock()
	fences, created := fixture.jobs.fences, fixture.jobs.created
	fixture.jobs.mu.Unlock()
	if readers != 2 || closed || fences != 0 || created != 1 {
		t.Error("departing nonfinal consumers cancelled or duplicated the shared producer")
	}
	audioRuntimeConcurrentReleases(t, releases[2:]...)
	fixture.session.mu.Lock()
	readers, closed = fixture.session.progressiveReaders, fixture.session.closed
	fixture.session.mu.Unlock()
	fixture.jobs.mu.Lock()
	fences = fixture.jobs.fences
	fixture.jobs.mu.Unlock()
	fixture.h.mu.Lock()
	remaining := len(fixture.h.sessions)
	fixture.h.mu.Unlock()
	if readers != 0 || !closed || fences != 1 || remaining != 0 {
		t.Error("final consumer release did not fence and retire exactly one unfinished producer")
	}
}

func TestAudioRuntimeCompletedResultRetainsItsRegistryAndProducer(t *testing.T) {
	fixture := newAudioRuntimeFixture(t)
	first, id := audioRuntimeLease(t, fixture, fixture.session)
	second, secondID := audioRuntimeLease(t, fixture, fixture.session)
	if secondID != id {
		t.Fatal("concurrent identical requests did not share a producer")
	}
	fixture.jobs.mu.Lock()
	fixture.jobs.records[id].record.State = "completed"
	fixture.jobs.mu.Unlock()
	audioRuntimeConcurrentReleases(t, first, second)
	reused := fixture.register(t)
	third, thirdID := audioRuntimeLease(t, fixture, reused)
	third()
	fixture.jobs.mu.Lock()
	fences, created := fixture.jobs.fences, fixture.jobs.created
	fixture.jobs.mu.Unlock()
	fixture.session.mu.Lock()
	readers, closed := fixture.session.progressiveReaders, fixture.session.closed
	fixture.session.mu.Unlock()
	if reused != fixture.session || thirdID != id || fences != 0 || created != 1 || readers != 0 || closed {
		t.Error("completed output was cancelled or lost its reusable immutable revision")
	}
}

func TestAudioRuntimeStartupFailureAndCancellationReleaseTheirSourceAndLease(t *testing.T) {
	for _, name := range []string{"manager-rejection", "unsupported-manager", "wrong-output-mode", "closed-session", "open-failure", "cancelled-startup"} {
		t.Run(name, func(t *testing.T) {
			fixture := newAudioRuntimeFixture(t)
			input := hlsRuntimeInput(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want, fences := transcode.ErrOutputUnavailable, 0
			switch name {
			case "manager-rejection":
				fixture.jobs.ensureErr, want = transcode.ErrBusy, transcode.ErrBusy
			case "unsupported-manager":
				fixture.h.manager = struct{ hlsJobs }{fixture.jobs}
			case "wrong-output-mode":
				fixture.session.key.plan.OutputMode = ""
			case "closed-session":
				fixture.h.retire(fixture.session)
				want = transcode.ErrJobNotFound
			case "open-failure":
				fixture.jobs.openErr, want, fences = errAudioRuntimeOpen, errAudioRuntimeOpen, 1
			case "cancelled-startup":
				fixture.jobs.openEntered = make(chan struct{}, 1)
				fixture.jobs.waitForCancel, want, fences = true, context.Canceled, 1
			}
			done := make(chan struct{})
			var reader *transcode.ProgressiveReader
			var release func()
			var err error
			go func() {
				reader, release, err = fixture.h.progressive(ctx, fixture.session, input)
				close(done)
			}()
			if name == "cancelled-startup" {
				audioRuntimeWait(t, fixture.jobs.openEntered, "controlled progressive startup")
				cancel()
			}
			audioRuntimeWait(t, done, "failed progressive opening")
			if !errors.Is(err, want) || reader != nil || release != nil {
				t.Errorf("failed progressive open returned invalid ownership: error=%v want=%v", err, want)
			}
			if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Error("failed progressive open leaked its input descriptor")
			}
			fixture.session.mu.Lock()
			readers := fixture.session.progressiveReaders
			fixture.session.mu.Unlock()
			fixture.jobs.mu.Lock()
			actualFences := fixture.jobs.fences
			fixture.jobs.mu.Unlock()
			if readers != 0 || actualFences != fences {
				t.Error("failed progressive open leaked a lease or cancelled the wrong producer count")
			}
		})
	}
}

func TestAudioRuntimeFailedJoiningConsumerLeavesExistingProducerRunning(t *testing.T) {
	fixture := newAudioRuntimeFixture(t)
	first, id := audioRuntimeLease(t, fixture, fixture.session)
	fixture.jobs.mu.Lock()
	fixture.jobs.openErr = errAudioRuntimeOpen
	fixture.jobs.mu.Unlock()
	input := hlsRuntimeInput(t)
	reader, release, err := fixture.h.progressive(context.Background(), fixture.session, input)
	if !errors.Is(err, errAudioRuntimeOpen) || reader != nil || release != nil {
		t.Fatal("controlled joining failure did not preserve its error contract")
	}
	fixture.session.mu.Lock()
	readers, closed := fixture.session.progressiveReaders, fixture.session.closed
	fixture.session.mu.Unlock()
	record, err := fixture.jobs.Snapshot(fixture.session.key.scope, id)
	if readers != 1 || closed || err != nil || record.State != "running" {
		t.Error("a failed joining consumer cancelled the existing consumer's producer")
	}
	if _, err := input.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Error("failed joining consumer retained its input descriptor")
	}
	first()
}

func TestAudioRuntimeRetirementFencesDeduplicationBeforeReplacementRegistration(t *testing.T) {
	for _, method := range []string{"final-consumer", "explicit-retirement"} {
		t.Run(method, func(t *testing.T) {
			fixture := newAudioRuntimeFixture(t)
			releaseOld, oldID := audioRuntimeLease(t, fixture, fixture.session)
			barrier := &audioRuntimeCancelBarrier{entered: make(chan struct{}), gate: make(chan struct{})}
			fixture.jobs.mu.Lock()
			fixture.jobs.barrier = barrier
			fixture.jobs.mu.Unlock()
			t.Cleanup(barrier.unblock)
			retired := make(chan struct{})
			go func() {
				if method == "final-consumer" {
					releaseOld()
				} else {
					fixture.h.retire(fixture.session)
				}
				close(retired)
			}()
			audioRuntimeWait(t, barrier.entered, "pre-fence cancellation boundary")
			if fixture.session.mu.TryLock() {
				fixture.session.mu.Unlock()
				t.Error("cancellation was exposed without the session lease lock")
			}
			if method == "explicit-retirement" {
				if fixture.h.mu.TryLock() {
					fixture.h.mu.Unlock()
					t.Error("retirement released the registry key before fencing manager deduplication")
				}
			} else {
				// The entered/gate pair synchronizes this read while the retiring
				// goroutine is paused before its next write to closed.
				if fixture.session.closed {
					t.Error("final release exposed a closed revision before cancelling its producer")
				}
			}
			started, replaced := make(chan struct{}), make(chan struct{})
			newInput := hlsRuntimeInput(t)
			var replacement *hlsSession
			var registerErr error
			var reader *transcode.ProgressiveReader
			var releaseNew func()
			var newID string
			go func() {
				defer close(replaced)
				close(started)
				replacement, registerErr = fixture.h.register(fixture.principal, fixture.source, "play_audio-runtime", fixture.decision, 0)
				if registerErr != nil {
					_ = newInput.Close()
					return
				}
				reader, releaseNew, registerErr = fixture.h.progressive(context.Background(), replacement, newInput)
				if registerErr == nil {
					replacement.mu.Lock()
					newID = replacement.producers[0].id
					replacement.mu.Unlock()
				}
			}()
			audioRuntimeWait(t, started, "replacement registration attempt")
			select {
			case <-replaced:
				t.Error("replacement registration passed the pre-cancellation fence")
			default:
			}
			barrier.unblock()
			audioRuntimeWait(t, retired, "old revision retirement")
			audioRuntimeWait(t, replaced, "replacement registration")
			if registerErr != nil || replacement == nil || replacement == fixture.session || reader == nil || releaseNew == nil {
				t.Fatalf("replacement registration did not create a new revision: %v", registerErr)
			}
			defer releaseNew()
			if _, err := newInput.Stat(); !errors.Is(err, os.ErrClosed) {
				t.Error("replacement failed to transfer its input descriptor")
			}
			if newID == oldID {
				t.Error("replacement deduplicated the producer that the old consumer was cancelling")
			}
			releaseOld()
			if _, err := fixture.jobs.Snapshot(replacement.key.scope, newID); err != nil {
				t.Error("late old-consumer cleanup cancelled the replacement producer")
			}
			releaseNew()
		})
	}
}
