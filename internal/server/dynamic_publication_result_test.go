package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

type dynamicPublicationTestConnector struct{ subtitles bool }

func (connector dynamicPublicationTestConnector) Open(context.Context, dynamicsource.Definition) (*dynamicsource.Connection, error) {
	info := dynamicTestInfo()
	if connector.subtitles {
		info.Streams = append(info.Streams, media.Stream{Index: 7, CodecType: "subtitle", Codec: "subrip", IsTextSubtitleStream: true})
	}
	return &dynamicsource.Connection{Reader: io.NopCloser(strings.NewReader("authorized publication input")), Info: info}, nil
}

type dynamicPublicationTestClock struct {
	mu        sync.Mutex
	now       time.Time
	remaining int
	advance   time.Duration
	callback  func()
	fired     bool
}

func (clock *dynamicPublicationTestClock) read() time.Time {
	clock.mu.Lock()
	var callback func()
	if clock.remaining > 0 {
		clock.remaining--
		if clock.remaining == 0 {
			clock.now = clock.now.Add(clock.advance)
			callback, clock.callback = clock.callback, nil
			clock.fired = true
		}
	}
	now := clock.now
	clock.mu.Unlock()
	if callback != nil {
		callback()
	}
	return now
}

func (clock *dynamicPublicationTestClock) atPublicationExpiry(advance time.Duration, callback func()) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	// This isolated store has no quota pressure or presentation worker. Its
	// publication reads the clock for admission, the committed segment, then
	// expiration. The last read occurs after the new segment is appended.
	clock.remaining, clock.advance, clock.callback, clock.fired = 3, advance, callback, false
}

func (clock *dynamicPublicationTestClock) assertFired(t *testing.T) {
	t.Helper()
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if !clock.fired {
		t.Fatal("publication did not reach its committed expiration boundary")
	}
}

type dynamicPublicationResultFixture struct {
	server     *Server
	session    *dynamicStreamSession
	spec       transcode.Spec
	jobID      string
	clock      *dynamicPublicationTestClock
	initial    timeshift.WindowSnapshot
	generation uint64
}

func newDynamicPublicationResultFixture(t *testing.T, withSubtitles bool) dynamicPublicationResultFixture {
	t.Helper()
	server, principal, _, request, jobs := dynamicServerFixture(t)
	t.Cleanup(server.stopMediaPolicy)
	if err := server.dynamicSources.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager, err := dynamicsource.New(context.Background(), []dynamicsource.Definition{{ItemID: request.ID,
		URL: "https://configured.invalid/publication", Infinite: true}}, dynamicsource.Options{
		Connector: dynamicPublicationTestConnector{subtitles: withSubtitles},
		Authorize: func(context.Context, dynamicsource.Owner, string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	server.dynamicSources = manager
	description, err := manager.Describe(context.Background(), dynamicSourceOwner(principal), request.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := manager.Open(context.Background(), dynamicSourceOwner(principal), dynamicsource.OpenRequest{
		ItemID: request.ID, OpenToken: description.OpenToken, PlaySessionID: request.CurrentPlaySessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.dynamicStreams.store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock := &dynamicPublicationTestClock{now: time.Now()}
	server.cfg.Timeshift.WindowSeconds = 12
	options := server.cfg.Timeshift.Options()
	options.Root, options.Now, options.SweepInterval = filepath.Join(t.TempDir(), "publication-timeshift"), clock.read, time.Minute
	server.dynamicStreams.store, err = timeshift.New(options)
	if err != nil {
		t.Fatal(err)
	}
	disabled, off, segmentLength := false, -1, 2
	request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = &disabled, &disabled
	request.SubtitleStreamIndex = &off
	request.DeviceProfile.TranscodingProfiles[0].Container = "ts"
	request.DeviceProfile.TranscodingProfiles[0].SegmentLength = &segmentLength
	request.DeviceProfile.SubtitleProfiles = []playback.SubtitleProfile{{Method: playback.SubtitleDeliveryMethodHls,
		Format: "vtt", Container: "ts", Protocol: "hls"}}
	httpRequest := dynamicTestRequest(principal, http.MethodGet, "/publication")
	limits, err := server.dynamicLimits(context.Background(), principal, request, httpRequest)
	if err != nil {
		t.Fatal(err)
	}
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
	if err != nil || conversion.Plan == nil || conversion.Plan.VideoCodec == "copy" ||
		transcode.HasHLSSubtitles(*conversion.Plan) != withSubtitles {
		t.Fatalf("publication fixture did not negotiate its intended encoded plan: %+v, %v", conversion.Plan, err)
	}
	plan := *conversion.Plan
	lifetime, cancel := context.WithCancel(server.hls.ctx)
	session := &dynamicStreamSession{id: "publication-result", key: dynamicStreamKey{owner: dynamicSourceOwner(principal).Identity(), liveID: lease.ID, plan: plan},
		request: request, principal: principal, changed: make(chan struct{}), ctx: lifetime, cancel: cancel, accessed: time.Now(),
		scope: transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
			PlaySessionID: lease.PlaySessionID, ItemID: lease.ItemID, SourceID: lease.SourceID}}
	windowOptions := server.cfg.Timeshift.WindowOptions(dynamicVariants(plan))
	windowOptions.TargetDurationTicks = dynamicTargetDuration(plan)
	window, err := server.dynamicStreams.store.Create(context.Background(), timeshiftScope(session.scope), windowOptions)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	session.windowID = window.PresentationID
	server.dynamicStreams.sessions[session.id], server.dynamicStreams.byKey[session.key] = session, session
	session.mu.Lock()
	err = server.startDynamicEpochLocked(context.Background(), session)
	if err == nil {
		session.producerStarted = true
		// Seed an already measured first interval so this test exercises the
		// publication result without invoking a media probe or an encoder.
		session.clockGeneration, session.clockDeltaTicks, session.lastSequence = session.generation, media.TicksPerSecond, 0
	}
	generation, jobID := session.generation, session.jobID
	session.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	spec := jobs.specs[len(jobs.specs)-1]
	jobs.mu.Unlock()
	fixture := dynamicPublicationResultFixture{server: server, session: session, spec: spec, jobID: jobID, clock: clock, generation: generation}
	seed := fixture.segment(t, 0)
	initial, err := server.dynamicStreams.store.Publish(context.Background(), timeshiftScope(session.scope), session.windowID,
		timeshift.Publication{Generation: generation, DurationTicks: seed.DurationTicks,
			Segments: []timeshift.ArtifactInput{{VariantID: "r0", File: seed.Renditions[0].Media}}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.initial = initial
	if session.subtitles != nil {
		// Leave this retained clock unbound. Only the complete publication
		// snapshot can bind both it and the new interval in the next callback.
		if err := session.subtitles.PublishMedia(generation, 0, seed.StartTicks, seed.StartTicks+seed.DurationTicks, media.TicksPerSecond); err != nil {
			t.Fatal(err)
		}
	}
	return fixture
}

func (fixture dynamicPublicationResultFixture) segment(t *testing.T, sequence int64) transcode.LiveSegment {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "complete-publication-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString("complete publication media"); err != nil {
		t.Fatal(err)
	}
	return transcode.LiveSegment{Sequence: sequence, StartTicks: sequence * 2 * media.TicksPerSecond,
		DurationTicks: 2 * media.TicksPerSecond, RenditionCount: 1,
		Renditions: [transcode.MaxHLSRenditions]transcode.LiveRendition{{Media: file}}}
}

func (fixture dynamicPublicationResultFixture) snapshot(t *testing.T) timeshift.WindowSnapshot {
	t.Helper()
	snapshot, err := fixture.server.dynamicStreams.store.Snapshot(context.Background(), timeshiftScope(fixture.session.scope), fixture.session.windowID)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestDynamicPublicationResultUsesLatestOrCompleteSubtitleWindow(t *testing.T) {
	for _, withSubtitles := range []bool{false, true} {
		name := "compact_without_subtitles"
		if withSubtitles {
			name = "complete_subtitle_window"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newDynamicPublicationResultFixture(t, withSubtitles)
			changed := fixture.session.changed
			segment := fixture.segment(t, 1)
			if err := fixture.server.publishDynamicSegment(context.Background(), fixture.spec, fixture.jobID, segment); err != nil {
				t.Fatal(err)
			}
			snapshot := fixture.snapshot(t)
			if len(snapshot.Segments) != 2 || snapshot.Revision <= fixture.initial.Revision ||
				fixture.session.clockGeneration != fixture.generation || fixture.session.clockDeltaTicks != media.TicksPerSecond || fixture.session.lastSequence != 1 {
				t.Fatal("publication lost committed history or failed to advance the measured producer clock")
			}
			select {
			case <-changed:
			default:
				t.Fatal("successful publication did not notify waiting consumers")
			}
			if withSubtitles {
				for _, committed := range snapshot.Segments {
					clock, exists := fixture.session.subtitles.media[committed.Sequence]
					if !exists || clock.artifactID != committed.Artifacts[0].ID || clock.Generation != committed.Generation ||
						clock.EndTicks-clock.StartTicks != committed.DurationTicks {
						t.Fatalf("complete subtitle publication did not bind retained interval %d: %+v", committed.Sequence, clock)
					}
				}
			} else if fixture.session.subtitles != nil {
				t.Fatal("subtitle-free publication created a subtitle runtime")
			}
		})
	}
}

func TestDynamicPublicationResultRechecksGenerationAfterStoreCommit(t *testing.T) {
	for _, withSubtitles := range []bool{false, true} {
		name := "compact_without_subtitles"
		if withSubtitles {
			name = "complete_subtitle_window"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newDynamicPublicationResultFixture(t, withSubtitles)
			changed := fixture.session.changed
			fixture.clock.atPublicationExpiry(0, func() {
				fixture.session.mu.Lock()
				fixture.session.generation++
				fixture.session.mu.Unlock()
			})
			err := fixture.server.publishDynamicSegment(context.Background(), fixture.spec, fixture.jobID, fixture.segment(t, 1))
			fixture.clock.assertFired(t)
			if !errors.Is(err, dynamicsource.ErrStaleGeneration) {
				t.Fatalf("an old generation completed after publication: %v", err)
			}
			snapshot := fixture.snapshot(t)
			if len(snapshot.Segments) != 2 || snapshot.Segments[1].Generation != fixture.generation || fixture.session.generation != fixture.generation+1 {
				t.Fatal("the stale-generation witness did not cross a successful store commit")
			}
			assertDynamicPublicationClockUnchanged(t, fixture, changed)
		})
	}
}

func TestDynamicPublicationResultRejectsAnExpiredCommittedInterval(t *testing.T) {
	for _, withSubtitles := range []bool{false, true} {
		name := "compact_without_subtitles"
		if withSubtitles {
			name = "complete_subtitle_window"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newDynamicPublicationResultFixture(t, withSubtitles)
			changed := fixture.session.changed
			fixture.clock.atPublicationExpiry(13*time.Second, nil)
			err := fixture.server.publishDynamicSegment(context.Background(), fixture.spec, fixture.jobID, fixture.segment(t, 1))
			fixture.clock.assertFired(t)
			if !errors.Is(err, transcode.ErrOutputUnavailable) {
				t.Fatalf("publication accepted an interval absent after expiration: %v", err)
			}
			snapshot := fixture.snapshot(t)
			if len(snapshot.Segments) != 0 || snapshot.NextSequence != 2 {
				t.Fatal("the expiration witness did not remove a committed interval")
			}
			assertDynamicPublicationClockUnchanged(t, fixture, changed)
		})
	}
}

func assertDynamicPublicationClockUnchanged(t *testing.T, fixture dynamicPublicationResultFixture, changed chan struct{}) {
	t.Helper()
	if fixture.session.clockGeneration != fixture.generation || fixture.session.clockDeltaTicks != media.TicksPerSecond || fixture.session.lastSequence != 0 || fixture.session.changed != changed {
		t.Fatal("an unsuccessful publication updated the session clock or notification channel")
	}
	select {
	case <-changed:
		t.Fatal("an unsuccessful publication notified waiting consumers")
	default:
	}
	if fixture.session.subtitles != nil {
		clock, exists := fixture.session.subtitles.media[0]
		if !exists || clock.artifactID != "" || len(fixture.session.subtitles.media) != 1 {
			t.Fatal("an unsuccessful publication changed retained subtitle coordinates")
		}
	}
}
