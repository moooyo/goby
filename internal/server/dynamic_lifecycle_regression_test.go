package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/timeshift"
)

type dynamicLifecycleFixture struct {
	server    *Server
	principal identity.Principal
	lease     dynamicsource.Lease
	request   playback.Request
	session   *dynamicStreamSession
	jobs      *dynamicTestJobs
}

func newDynamicLifecycleFixture(t *testing.T) dynamicLifecycleFixture {
	t.Helper()
	server, principal, lease, request, jobs := dynamicServerFixture(t)
	dto, err := server.dynamicPlaybackDTO(dynamicTestRequest(principal, http.MethodPost, "/emby/LiveStreams/Open"), principal, lease, request)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	server.dynamicStreams.mu.Lock()
	session := server.dynamicStreams.sessions[parsed.Query().Get("GobyLiveId")]
	server.dynamicStreams.mu.Unlock()
	if session == nil {
		t.Fatal("fixture did not register a dynamic presentation")
	}
	t.Cleanup(server.stopMediaPolicy)
	return dynamicLifecycleFixture{server: server, principal: principal, lease: lease, request: request, session: session, jobs: jobs}
}

func (fixture dynamicLifecycleFixture) httpRequest(ctx context.Context, name string) *http.Request {
	address := dynamicArtifactURL(fixture.session, name, "owned-token")
	request := dynamicTestRequest(fixture.principal, http.MethodGet, address)
	request = request.WithContext(context.WithValue(ctx, principalKey, fixture.principal))
	request.SetPathValue("LiveStreamId", fixture.lease.ID)
	request.SetPathValue("Artifact", name)
	return request
}

func (fixture dynamicLifecycleFixture) heartbeat() bool {
	scope := fixture.session.scope
	return fixture.server.heartbeatDynamicMediaPolicy(fixture.principal, library.PlaySession{ID: scope.PlaySessionID, UserID: scope.UserID,
		AuthSessionID: scope.AuthSessionID, DeviceID: scope.DeviceID, ItemID: scope.ItemID, MediaSourceID: scope.SourceID,
		ApplicationKey: scope.ApplicationKey, ApplicationClientID: scope.ApplicationClientID, IsDynamic: true, State: "Paused"})
}

func (fixture dynamicLifecycleFixture) metadata(t *testing.T) {
	t.Helper()
	response := httptest.NewRecorder()
	fixture.server.dynamicHLSArtifact(response, fixture.httpRequest(context.Background(), "window.json"))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"PresentationId"`) {
		t.Fatalf("window metadata was unavailable: %d %s", response.Code, response.Body.String())
	}
	fixture.session.mu.Lock()
	started, closed := fixture.session.producerStarted, fixture.session.closed
	fixture.session.mu.Unlock()
	if !started || closed {
		t.Fatal("metadata startup did not retain its authorized producer")
	}
}

// Real store copies exercise file and reader lifetime without invoking an
// encoder. The fixed payload is intentionally opaque to this storage layer.
func (fixture dynamicLifecycleFixture) publish(t *testing.T, payload string) timeshift.WindowSnapshot {
	t.Helper()
	store := fixture.server.dynamicStreams.store
	snapshot, err := store.Snapshot(context.Background(), timeshiftScope(fixture.session.scope), fixture.session.windowID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.session.mu.Lock()
	generation := fixture.session.generation
	fixture.session.mu.Unlock()
	if generation == 0 {
		t.Fatal("fixture must start its authenticated source before publishing")
	}
	open := func(body string) *os.File {
		file, err := os.CreateTemp(t.TempDir(), "complete-dynamic-media-")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(body); err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		return file
	}
	publication := timeshift.Publication{Generation: generation, DurationTicks: snapshot.TargetDurationTicks}
	var files []*os.File
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for _, variant := range snapshot.Variants {
		file := open(payload)
		files = append(files, file)
		publication.Segments = append(publication.Segments, timeshift.ArtifactInput{VariantID: variant.ID, File: file})
		if len(snapshot.Epochs) == 0 && variant.Format == "fmp4" {
			initialization := open("complete initialization")
			files = append(files, initialization)
			publication.Initializations = append(publication.Initializations, timeshift.ArtifactInput{VariantID: variant.ID, File: initialization})
		}
	}
	snapshot, err = store.Publish(context.Background(), timeshiftScope(fixture.session.scope), fixture.session.windowID, publication)
	if err != nil {
		t.Fatal(err)
	}
	fixture.session.mu.Lock()
	signalDynamicSessionLocked(fixture.session)
	fixture.session.mu.Unlock()
	return snapshot
}

func (fixture dynamicLifecycleFixture) assertAlive(t *testing.T) {
	t.Helper()
	fixture.session.mu.Lock()
	closed, ended := fixture.session.closed, fixture.session.producerEnded
	fixture.session.mu.Unlock()
	fixture.jobs.mu.Lock()
	cancelled := fixture.jobs.cancelled
	fixture.jobs.mu.Unlock()
	if closed || ended || fixture.session.ctx.Err() != nil || cancelled != 0 {
		t.Fatal("a retryable request retired the active presentation or producer")
	}
	if _, err := fixture.server.dynamicStreams.store.Snapshot(context.Background(), timeshiftScope(fixture.session.scope), fixture.session.windowID); err != nil {
		t.Fatalf("retained window was destroyed: %v", err)
	}
}

func TestDynamicLifecycleMetadataAndManifestDoNotClaimMediaDelivery(t *testing.T) {
	for _, partial := range []bool{false, true} {
		name := "full-response"
		if partial {
			name = "partial-response"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newDynamicLifecycleFixture(t)
			fixture.metadata(t)
			if fixture.heartbeat() {
				t.Fatal("metadata alone authorized a delivered-media heartbeat")
			}
			const payload = "0123456789-complete-media"
			first := fixture.publish(t, payload)
			fixture.publish(t, payload)
			fixture.publish(t, payload)
			manifest := httptest.NewRecorder()
			fixture.server.dynamicHLSArtifact(manifest, fixture.httpRequest(context.Background(), "main.m3u8"))
			if manifest.Code != http.StatusOK || strings.Count(manifest.Body.String(), "#EXTINF:") != 3 {
				t.Fatalf("ready media manifest failed: %d %s", manifest.Code, manifest.Body.String())
			}
			if fixture.heartbeat() {
				t.Fatal("a manifest response was counted as media delivery")
			}
			artifact := first.Segments[0].Artifacts[0]
			request := fixture.httpRequest(context.Background(), dynamicArtifactName(artifact.ID, first.Variants[0].Format, false))
			wantStatus, wantBody := http.StatusOK, payload
			if partial {
				request.Header.Set("Range", "bytes=2-6")
				wantStatus, wantBody = http.StatusPartialContent, payload[2:7]
			}
			response := httptest.NewRecorder()
			fixture.server.dynamicHLSArtifact(response, request)
			if response.Code != wantStatus || response.Body.String() != wantBody {
				t.Fatalf("real stored media was not delivered: %d %q", response.Code, response.Body.String())
			}
			if !fixture.heartbeat() {
				t.Fatal("successful media delivery was lost before the policy reference was released")
			}
			fixture.assertAlive(t)
		})
	}
}

func TestDynamicLifecycleShortWindowWaitsAndSurvivesCancelledStartupRequest(t *testing.T) {
	fixture := newDynamicLifecycleFixture(t)
	fixture.metadata(t)
	fixture.publish(t, "first complete interval")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	response := httptest.NewRecorder()
	fixture.server.dynamicHLSArtifact(response, fixture.httpRequest(ctx, "main.m3u8"))
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		cancel()
		t.Fatalf("short live media returned before advertisement readiness or cancellation: %d %s", response.Code, response.Body.String())
	}
	cancel()
	fixture.assertAlive(t)
	if fixture.heartbeat() {
		t.Fatal("a cancelled startup wait invented delivered media")
	}
	work, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	ready := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fixture.server.dynamicHLSArtifact(ready, fixture.httpRequest(work, "main.m3u8"))
	}()
	select {
	case <-done:
		t.Fatalf("a short window escaped the live startup wait: %d %s", ready.Code, ready.Body.String())
	case <-time.After(20 * time.Millisecond):
	}
	fixture.publish(t, "second complete interval")
	fixture.publish(t, "third complete interval")
	select {
	case <-done:
	case <-work.Done():
		t.Fatal("additional committed media did not wake the waiting playlist")
	}
	if ready.Code != http.StatusOK || strings.Count(ready.Body.String(), "#EXTINF:") != 3 {
		t.Fatalf("ready window did not produce a complete manifest: %d %s", ready.Code, ready.Body.String())
	}
	fixture.assertAlive(t)
}

func TestDynamicLifecycleMediaInfoKeepsCurrentPresentationAcrossRetainedProfiles(t *testing.T) {
	fixture := newDynamicLifecycleFixture(t)
	fixture.metadata(t)
	old := fixture.session
	nextRequest := fixture.request
	profile := *fixture.request.DeviceProfile
	profile.TranscodingProfiles = append([]playback.TranscodingProfile(nil), profile.TranscodingProfiles...)
	profile.TranscodingProfiles[0].VideoCodec = "hevc"
	nextRequest.DeviceProfile = &profile
	dto, err := fixture.server.dynamicPlaybackDTO(dynamicTestRequest(fixture.principal, http.MethodPost, "/emby/LiveStreams/Open"), fixture.principal, fixture.lease, nextRequest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dto["TranscodingUrl"].(string))
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.dynamicStreams.mu.Lock()
	current := fixture.server.dynamicStreams.sessions[parsed.Query().Get("GobyLiveId")]
	oldIndexed := fixture.server.dynamicStreams.byKey[old.key]
	fixture.server.dynamicStreams.mu.Unlock()
	if current == nil || current == old || oldIndexed != nil {
		t.Fatal("profile replacement did not retain history separately from the current presentation")
	}
	old.mu.Lock()
	oldEnded, oldClosed := old.producerEnded, old.closed
	old.mu.Unlock()
	if !oldEnded || oldClosed {
		t.Fatal("fixture did not preserve the old completed presentation for history")
	}
	fixture.session = current
	fixture.metadata(t)
	fixture.jobs.mu.Lock()
	beforeCancelled, beforeJobs := fixture.jobs.cancelled, len(fixture.jobs.specs)
	fixture.jobs.mu.Unlock()
	lease, err := fixture.server.dynamicSources.Info(context.Background(), dynamicSourceOwner(fixture.principal), fixture.lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Repeated public refreshes must be independent of map iteration order.
	for attempt := 0; attempt < 24; attempt++ {
		refreshed, err := fixture.server.dynamicMediaInfoDTO(dynamicTestRequest(fixture.principal, http.MethodPost, "/emby/LiveStreams/MediaInfo"), fixture.principal, lease)
		if err != nil {
			t.Fatal(err)
		}
		if refreshed["TranscodingUrl"] != dto["TranscodingUrl"] {
			t.Fatal("media-info refresh renegotiated a retained historical profile")
		}
		current.mu.Lock()
		ended, closed := current.producerEnded, current.closed
		current.mu.Unlock()
		fixture.jobs.mu.Lock()
		cancelled, jobs := fixture.jobs.cancelled, len(fixture.jobs.specs)
		fixture.jobs.mu.Unlock()
		if ended || closed || cancelled != beforeCancelled || jobs != beforeJobs {
			t.Fatal("media-info refresh stopped or replaced the current producer")
		}
	}
	fixture.server.dynamicStreams.mu.Lock()
	indexed, currentCount := fixture.server.dynamicStreams.byKey[current.key], len(fixture.server.dynamicStreams.sessions)
	fixture.server.dynamicStreams.mu.Unlock()
	if indexed != current || currentCount != 2 {
		t.Fatal("media-info refresh changed the current/history presentation registry")
	}
}
