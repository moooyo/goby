//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type hlsWallPauseCase struct {
	Name     string
	Duration time.Duration
	Mode     string
}

func hlsWallPauseCases(selection string) ([]hlsWallPauseCase, error) {
	var selected []hlsWallPauseCase
	for _, duration := range []struct {
		name string
		span time.Duration
	}{{"2m", 2 * time.Minute}, {"10m", 10 * time.Minute}} {
		for _, mode := range []string{"retained", "natural-ttl", "retained-job-quota"} {
			candidate := hlsWallPauseCase{Name: duration.name + "-" + mode, Duration: duration.span, Mode: mode}
			if selection == "all" || selection == candidate.Name {
				selected = append(selected, candidate)
			}
		}
	}
	if selection != "" && len(selected) == 0 {
		return nil, errors.New("unknown wall-pause case")
	}
	return selected, nil
}

// Long wall-time cases require an exact opt-in and a persistent artifact path.
// They must run serially in an isolated remote verification environment. No
// shortened timer, injected clock, rewritten LastAccessAt or artificial probe
// gate is permitted to stand in for these observations.
func TestHTTPGeneratedWindowRealWallPause(t *testing.T) {
	selection := os.Getenv("GOBY_HLS_WALLPAUSE_CASE")
	if selection == "" {
		t.Skip("set GOBY_HLS_WALLPAUSE_CASE to an exact 2m/10m case or all for real wall-time verification")
	}
	cases, err := hlsWallPauseCases(selection)
	if err != nil {
		t.Fatal("GOBY_HLS_WALLPAUSE_CASE must select retained, natural-ttl or retained-job-quota with a 2m-/10m- prefix, or all")
	}
	directory := os.Getenv("GOBY_HLS_WALLPAUSE_ARTIFACT_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("GOBY_HLS_WALLPAUSE_ARTIFACT_DIR must be an absolute persistent evidence directory")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create wall-pause evidence directory: error_type=%T", err)
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) { hlsWallPauseRun(t, testCase, directory) })
	}
}

func hlsWallPauseRun(t *testing.T, testCase hlsWallPauseCase, directory string) {
	t.Helper()
	started := time.Now()
	evidence := &hlsWallPauseEvidence{Version: 1, Case: testCase.Name,
		Subset:     "Emby-compatible software AVC NativeClockV1 adaptive MPEG-TS with one silent video track",
		StartedUTC: started.UTC().Format(time.RFC3339Nano), PauseRequestedMS: testCase.Duration.Milliseconds(),
		Boundary: []string{
			"The pause uses elapsed monotonic wall time and lawful paused Ping/Progress requests.",
			"Producer CPU and IO are cumulative Linux proc counters bound to PID, start tick, exact owned cwd and pidfd.",
			"The last producer counters are a lower bound if the runner reaps between 20ms observations; pidfd and start tick prove exit and reap separately.",
			"The test server counters include HTTP, database, observer and cache maintenance overhead and are not isolated encoder utilization.",
			"Each unique exact scope reports readonly ResourceUsage manager reader pins, queue and completion phases separately from server redirect pins and descriptors.",
			"ResourceUsage ExecutionReservations are manager ownership, including final inspection, not live process counts; known charged output floor is not a physical allocation upper bound.",
			"Each sample combines separately timed readonly API, filesystem and proc observations; it is not one atomic global workspace snapshot.",
			"Filesystem names and sizes count closed TS/playlist artifacts only; they do not claim full AV, fMP4, subtitles, packed media, progressive playback or every client.",
			"Input pacing is test-only -readrate 1 on the genuine negotiated FFmpeg producer. Source generation and independent decoding are unpaced.",
			"The paced fixture supplies observability only; no throughput or general performance conclusion is supported.",
			"Retained-job quota uses real authenticated pressure producers and normal admission reclamation; it is not byte-quota eviction.",
		}}
	var observer *hlsWallPauseObserver
	// Save before setup and at bounded progress intervals so timeout/termination
	// preserves an atomic partial document. The last cleanup also records any
	// errors reported by fixture/request/observer cleanup after the main defer.
	t.Cleanup(func() { hlsWallPauseSaveEvidence(t, directory, evidence, observer) })
	defer func() { hlsWallPauseSaveEvidence(t, directory, evidence, observer) }()
	hlsWallPauseSaveEvidence(t, directory, evidence, observer)
	h, control := newHLSGeneratedWindowWallPauseFixture(t, testCase.Mode)
	observer = newHLSWallPauseObserver(t, h, control, started)
	graph := hlsWallPausePrepareOwner(t, h, 90*media.TicksPerSecond, h.accounts.viewer)
	hlsGeneratedWindowHTTPLoad(t, h, &graph)
	roles := make(map[string]hlsWallPauseOwnedJob)
	var firstLocation string
	var before [][]byte
	for variant := range graph.mainURLs {
		id, location, response := hlsGeneratedWindowHTTPExact(t, h, graph, variant, 15)
		if variant == 0 {
			evidence.FirstJobID, firstLocation = id, location
		} else if id != evidence.FirstJobID {
			t.Fatal("initial adaptive renditions used different producer identities")
		}
		roles[id] = hlsWallPauseOwnedJob{Role: "target_cached", Scope: graph.session.key.scope}
		hlsWallPauseAssertExact(t, h, graph, id, variant, response.body)
		before = append(before, bytes.Clone(response.body))
		evidence.BeforeSHA256 = append(evidence.BeforeSHA256, hlsWallPauseSHA256(response.body))
	}
	evidence.Samples = append(evidence.Samples, hlsWallPauseCapture(t, h, graph, observer, roles, started, time.Time{}, "initial_closed_window"))
	hlsWallPauseSaveEvidence(t, directory, evidence, observer)
	startedBody := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 90 * media.TicksPerSecond}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing", startedBody, graph.owner.headers), http.StatusNoContent)

	// Begin a genuine cold admission while source input is paced. Pause is
	// submitted only after this process has performed actual CPU and IO work.
	requestCtx, cancelRequest := context.WithTimeout(h.f.ctx, 30*time.Second)
	type outcome struct {
		response hlsHTTPResponse
		err      error
	}
	result, requestDone := make(chan outcome, 1), make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := hlsGeneratedWindowHTTPDo(requestCtx, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil)
		result <- outcome{response: response, err: err}
	}()
	t.Cleanup(func() {
		cancelRequest()
		select {
		case <-requestDone:
		case <-time.After(5 * time.Second):
			t.Error("owned cold admission request did not join")
		}
	})
	hlsGeneratedWindowHTTPWait(t, h, 8*time.Second, func() bool {
		processes, observationErr := observer.snapshot()
		if observationErr != "" {
			t.Fatal("owned encoder observation is unavailable")
		}
		for _, process := range processes {
			if process.JobID != evidence.FirstJobID && process.Alive && process.Samples >= 2 &&
				process.Last.UserTicks+process.Last.SystemTicks > process.First.UserTicks+process.First.SystemTicks &&
				process.Last.ReadChars > process.First.ReadChars && process.Last.WriteChars > process.First.WriteChars {
				evidence.CancelledJobID = process.JobID
				roles[process.JobID] = hlsWallPauseOwnedJob{Role: "target_cancelled_cold", Scope: graph.session.key.scope}
				return true
			}
		}
		return false
	}, "cold source-window producer did not expose genuine live CPU and IO before completion")
	evidence.Samples = append(evidence.Samples, hlsWallPauseCapture(t, h, graph, observer, roles, started, time.Time{}, "live_cold_before_pause"))
	// EventName Pause must win even when IsPaused is false. The reverse
	// conflict is checked on Unpause after the real wall pause below.
	hlsGeneratedWindowHTTPProgress(t, h, graph, "Pause", false)
	paused := time.Now()
	evidence.PauseAcceptedMS = paused.Sub(started).Milliseconds()
	select {
	case response := <-result:
		if response.err != nil {
			t.Fatal("cold HTTP admission transport failed during committed pause")
		}
		evidence.ColdAdmissionStatus = response.response.status
		if response.response.status != http.StatusNotFound && response.response.status != http.StatusServiceUnavailable {
			t.Fatal("committed pause did not return its classified unfinished cold producer response")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("committed pause did not retire its unfinished cold HTTP admission")
	}
	cancelRequest()
	hlsGeneratedWindowHTTPWait(t, h, 10*time.Second, func() bool {
		processes, observationErr := observer.snapshot()
		if observationErr != "" {
			t.Fatal("owned process retirement observation is unavailable")
		}
		found := false
		for _, process := range processes {
			if process.JobID == evidence.CancelledJobID {
				found = process.ExitMS != 0 && process.ReapMS != 0 && !process.Alive
				if found {
					evidence.CancellationExitDelayMS = process.ExitMS - evidence.PauseAcceptedMS
					evidence.CancellationReapDelayMS = process.ReapMS - evidence.PauseAcceptedMS
				}
			}
		}
		graph.session.mu.Lock()
		pending, isPaused := graph.session.admission != nil, graph.session.demand.paused
		graph.session.mu.Unlock()
		metrics := h.f.app.hls.manager.(interface{ Metrics() transcode.Metrics }).Metrics()
		usage, usageErr := h.f.app.hls.manager.(interface {
			ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
		}).ResourceUsage(h.f.ctx, graph.session.key.scope)
		if usageErr != nil {
			t.Fatal("paused producer retirement lost its scoped resource observation")
		}
		managerSettled := usage.UnfinishedJobs == 0 && usage.CreatingJobs == 0 && usage.QueuedJobs == 0 && usage.ExecutionReservations == 0 &&
			usage.CompletionReservedJobs == 0 && usage.CompletionQueuedJobs == 0 && usage.CompletionWorkingJobs == 0 && usage.CompletionUnknownJobs == 0
		return found && !pending && isPaused && metrics.Running == 0 && managerSettled
	}, "committed pause did not prove exit, reap, idle slots and closed pending admission")
	_, pausedLocation, cached := hlsGeneratedWindowHTTPExact(t, h, graph, 0, 15)
	// The real cached redirect occurs after cancellation has settled. Its
	// grace deadline therefore cannot be approximated from the Pause time.
	quietPinsAfterMS := time.Since(paused).Milliseconds() + 16_000
	evidence.RedirectPinsQuietAfterMS = quietPinsAfterMS
	if pausedLocation != firstLocation || !bytes.Equal(cached.body, before[0]) {
		t.Fatal("paused cached GET changed its exact completed artifact")
	}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil), http.StatusServiceUnavailable)
	targetIDs := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
	if len(targetIDs) != 2 {
		t.Fatal("pause created replacement producers or lost its real cancellation record")
	}
	evidence.Samples = append(evidence.Samples, hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "paused_retired"))
	hlsWallPauseSaveEvidence(t, directory, evidence, observer)

	if testCase.Mode == "retained-job-quota" {
		// A terminal canceled job is reclaimed first. Wait for both its normal
		// cleanup and the target redirect handoff's natural grace to finish.
		hlsGeneratedWindowHTTPWait(t, h, 16*time.Second, func() bool {
			sample := hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "quota_grace_wait")
			for _, job := range sample.Jobs {
				if job.ID == evidence.CancelledJobID && job.Directory {
					return false
				}
			}
			return sample.TargetPins == 0
		}, "normal canceled-job cleanup or redirect grace blocked quota pressure")
		for pressure, owner := range []clientSessionHTTPLogin{h.accounts.second, h.accounts.admin} {
			other := hlsWallPausePrepareOwner(t, h, 90*media.TicksPerSecond, owner)
			if other.playID == graph.playID || other.session.key.scope.AuthSessionID == graph.session.key.scope.AuthSessionID {
				t.Fatal("quota pressure reused the paused target's playback or authentication scope")
			}
			hlsGeneratedWindowHTTPLoad(t, h, &other)
			ids := hlsGeneratedWindowHTTPJobIDs(t, h, other)
			if len(ids) != 1 {
				t.Fatal("quota pressure did not create exactly one genuine independent closed window")
			}
			roles[ids[0]] = hlsWallPauseOwnedJob{Role: fmt.Sprintf("quota_pressure_%d", pressure+1), Scope: other.session.key.scope}
			evidence.Samples = append(evidence.Samples, hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "quota_pressure_closed"))
		}
		hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool {
			_, snapshotErr := h.f.app.hls.manager.Snapshot(graph.session.key.scope, evidence.FirstJobID)
			_, statErr := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, evidence.FirstJobID))
			return errors.Is(snapshotErr, transcode.ErrJobNotFound) && errors.Is(statErr, os.ErrNotExist)
		}, "real retained-job admission quota did not evict the oldest unpinned target cache")
	}

	tick, ping := time.NewTicker(time.Second), time.NewTicker(20*time.Second)
	deadline := time.NewTimer(time.Until(paused.Add(testCase.Duration)))
	defer tick.Stop()
	defer ping.Stop()
	defer deadline.Stop()
	wallDone := false
	for !wallDone {
		select {
		case <-tick.C:
			sample := hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "paused_wall")
			hlsWallPauseAssertQuiet(t, h, sample, targetIDs, quietPinsAfterMS)
			hlsWallPauseAssertCacheBound(t, sample, evidence.Samples[0], evidence.FirstJobID, testCase.Mode)
			evidence.Samples = append(evidence.Samples, sample)
		case <-ping.C:
			hlsWallPausePing(t, h, graph)
			hlsGeneratedWindowHTTPProgress(t, h, graph, "TimeUpdate", true)
			expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][0], nil, nil), http.StatusServiceUnavailable)
			if len(hlsGeneratedWindowHTTPJobIDs(t, h, graph)) != len(targetIDs) {
				t.Fatal("paused Ping, Progress or cold GET admitted another target producer")
			}
			hlsWallPauseSaveEvidence(t, directory, evidence, observer)
		case <-deadline.C:
			wallDone = true
		case <-h.f.ctx.Done():
			t.Fatal("long wall-pause fixture expired before the real timer")
		}
	}
	evidence.PauseObservedMS = time.Since(paused).Milliseconds()
	if time.Since(paused) < testCase.Duration {
		t.Fatal("wall-pause observation was shorter than its requested real duration")
	}
	if testCase.Mode == "natural-ttl" {
		// Reader release at the end of redirect grace naturally refreshes the
		// cache lease. Measure that actual timestamp; do not age it or pretend
		// two minutes since Pause is two minutes since the last real reader.
		record, snapshotErr := h.f.app.hls.manager.Snapshot(graph.session.key.scope, evidence.FirstJobID)
		if record.ID == evidence.FirstJobID {
			evidence.NaturalTTLLastAccessUTC = record.LastAccessAt.UTC().Format(time.RFC3339Nano)
			if record.LastAccessAt.After(paused.Add(time.Duration(quietPinsAfterMS) * time.Millisecond)) {
				t.Fatal("untouched paused cache acquired an unexpected late idle lease")
			}
		} else if !errors.Is(snapshotErr, transcode.ErrJobNotFound) {
			t.Fatal("natural cache expiry lost its classified manager observation")
		}
		expiryDeadline := paused.Add(testCase.Duration + 20*time.Second)
		if record.ID == evidence.FirstJobID && record.LastAccessAt.Add(2*time.Minute+5*time.Second).After(expiryDeadline) {
			expiryDeadline = record.LastAccessAt.Add(2*time.Minute + 5*time.Second)
		}
		for {
			_, snapshotErr := h.f.app.hls.manager.Snapshot(graph.session.key.scope, evidence.FirstJobID)
			_, statErr := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, evidence.FirstJobID))
			if errors.Is(snapshotErr, transcode.ErrJobNotFound) && errors.Is(statErr, os.ErrNotExist) {
				break
			}
			if time.Now().After(expiryDeadline) {
				t.Fatal("default two-minute idle retention did not naturally expire the untouched cache after reader-release grace")
			}
			select {
			case <-tick.C:
				sample := hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "paused_natural_expiry_tail")
				hlsWallPauseAssertQuiet(t, h, sample, targetIDs, quietPinsAfterMS)
				hlsWallPauseAssertCacheBound(t, sample, evidence.Samples[0], evidence.FirstJobID, testCase.Mode)
				evidence.Samples = append(evidence.Samples, sample)
			case <-h.f.ctx.Done():
				t.Fatal("natural idle cache expiry exceeded the fixture lifetime")
			}
		}
	}
	evidence.PauseObservedMS = time.Since(paused).Milliseconds()
	finalPaused := hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "pause_finished")
	hlsWallPauseAssertQuiet(t, h, finalPaused, targetIDs, quietPinsAfterMS)
	hlsWallPauseAssertCacheBound(t, finalPaused, evidence.Samples[0], evidence.FirstJobID, testCase.Mode)
	evidence.Samples = append(evidence.Samples, finalPaused)
	hlsWallPauseSaveEvidence(t, directory, evidence, observer)
	if testCase.Mode != "retained" {
		expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, firstLocation, nil, nil), http.StatusNotFound)
		expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mediaURLs[0][15], nil, nil), http.StatusServiceUnavailable)
	}
	hlsGeneratedWindowHTTPProgress(t, h, graph, "Unpause", true)
	for variant := range graph.mainURLs {
		id, _, response := hlsGeneratedWindowHTTPExact(t, h, graph, variant, 15)
		if variant == 0 {
			evidence.ResumeJobID = id
		} else if id != evidence.ResumeJobID {
			t.Fatal("resumed adaptive renditions mixed producer identities")
		}
		roles[id] = hlsWallPauseOwnedJob{Role: "target_resumed", Scope: graph.session.key.scope}
		hlsWallPauseAssertExact(t, h, graph, id, variant, response.body)
		evidence.ResumeSHA256 = append(evidence.ResumeSHA256, hlsWallPauseSHA256(response.body))
		if testCase.Mode == "retained" && (id != evidence.FirstJobID || !bytes.Equal(response.body, before[variant])) {
			t.Fatal("retained-cache resume replaced its complete producer bytes")
		}
		if testCase.Mode != "retained" && id == evidence.FirstJobID {
			t.Fatal("expired or quota-evicted cache resumed a retired producer identity")
		}
	}
	for variant, mainURL := range graph.mainURLs {
		manifest := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, mainURL, nil, nil)
		expectHLSHTTPStatus(t, manifest, http.StatusOK)
		if !bytes.Equal(manifest.body, graph.manifests[variant]) {
			t.Fatal("wall-pause resume rewrote the client-loaded VOD presentation")
		}
	}
	evidence.Samples = append(evidence.Samples, hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "resumed_exact_decoded"))
	stop := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "PositionTicks": 91 * media.TicksPerSecond}
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing/Stopped", stop, graph.owner.headers), http.StatusNoContent)
	// Runtime Close owns pressure producers too, and waits for manager readers.
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelClose()
	if err := h.f.app.hls.Close(closeCtx); err != nil {
		t.Fatalf("wall-pause stop did not drain runtime resources: error_type=%T", err)
	}
	hlsGeneratedWindowHTTPWait(t, h, 5*time.Second, func() bool {
		processes, observationErr := observer.snapshot()
		if observationErr != "" {
			return false
		}
		for _, process := range processes {
			if process.Alive || process.ReapMS == 0 {
				return false
			}
		}
		return true
	}, "stopped runtime retained an original waitable producer")
	ids := make([]string, 0, len(roles))
	for id := range roles {
		ids = append(ids, id)
		if _, err := os.Stat(filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("stopped runtime retained an owned job cache directory")
		}
	}
	if descendants, err := hlsWallPauseOwnedDescendants(h.f.app.cfg.Transcoding.CacheDirectory, ids); err != nil || descendants != 0 {
		t.Fatal("stopped runtime retained a process cwd into an owned cache directory")
	}
	h.f.app.hls.generatedWindowPinMu.Lock()
	pins := len(h.f.app.hls.generatedWindowPinOwners)
	h.f.app.hls.generatedWindowPinMu.Unlock()
	if pins != 0 || h.f.app.hls.manager.(interface{ Metrics() transcode.Metrics }).Metrics().Running != 0 {
		t.Fatal("stopped runtime retained a redirect reader pin or conversion slot")
	}
	stoppedSample := hlsWallPauseCapture(t, h, graph, observer, roles, started, paused, "stopped_resources_drained")
	for _, scope := range stoppedSample.ManagerScopes {
		if scope.Usage != (transcode.ResourceUsage{}) {
			t.Fatal("stopped runtime retained scoped execution, cache, reader or completion ownership")
		}
	}
	for _, job := range stoppedSample.Jobs {
		if job.ServerFDs != 0 {
			t.Fatal("stopped runtime retained a descriptor into an owned or deleted job directory")
		}
	}
	evidence.Samples = append(evidence.Samples, stoppedSample)
	if err := observer.close(); err != nil {
		t.Fatal("wall-pause process observer failed to join")
	}
	evidence.Completed = true
	t.Logf("real wall-pause completed: case=%s requested_ms=%d observed_ms=%d samples=%d owned_jobs=%d subset=silent_AVC_adaptive_TS_NativeClockV1",
		testCase.Name, evidence.PauseRequestedMS, evidence.PauseObservedMS, len(evidence.Samples), len(roles))
}

func hlsWallPauseSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func hlsWallPausePing(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph) {
	t.Helper()
	ping := "/emby/Sessions/Playing/Ping?" + url.Values{"PlaySessionId": {graph.playID}}.Encode()
	expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, ping, nil, graph.owner.headers), http.StatusNoContent)
}

func hlsWallPauseAssertExact(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph, id string, variant int, received []byte) {
	t.Helper()
	record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, id)
	if err != nil || record.State != "completed" || record.Spec.Plan.HLS.Window.NativeClockVersion != transcode.GeneratedWindowNativeClockV1 ||
		!record.Spec.Plan.HLS.Window.RequireInputEvidence || record.Spec.Plan.StartTicks != 90*media.TicksPerSecond ||
		record.Spec.Plan.HLS.Window.EndTicks != 96*media.TicksPerSecond || record.Spec.Plan.HLS.Window.StartNumber != 15 {
		t.Fatal("high-seek exact bytes lack a completed native [90,96) producer")
	}
	path := filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id, fmt.Sprintf("v%d-segment-%06d.ts", variant, 15))
	physical, err := os.ReadFile(path)
	if err != nil || len(physical) > 16<<20 || !bytes.Equal(physical, received) {
		t.Fatal("HTTP high-seek response differs from its exact closed physical TS artifact")
	}
	hlsGeneratedWindowHTTPDecode(t, h, graph, variant, received)
}

func hlsWallPauseAssertQuiet(t *testing.T, h *hlsHTTPFixture, sample hlsWallPauseSample, targetIDs []string, quietPinsAfterMS int64) {
	t.Helper()
	if !sample.TargetPaused || sample.TargetClosed || sample.Slots.Running != 0 || sample.QueuedRecords != 0 {
		t.Fatal("real paused stable interval was not paused, idle and unqueued")
	}
	for _, process := range sample.Processes {
		if process.Alive {
			t.Fatal("a real producer remained alive during the paused stable interval")
		}
	}
	if sample.PauseElapsedMS >= quietPinsAfterMS && (sample.TargetPins != 0 || sample.AllPins != 0) {
		t.Fatal("paused redirect handoff pins exceeded their real grace and maintenance cadence")
	}
	for _, scope := range sample.ManagerScopes {
		usage := scope.Usage
		if usage.ExecutionReservations != 0 || usage.QueuedJobs != 0 || usage.UnfinishedJobs != 0 || usage.CreatingJobs != 0 ||
			usage.CompletionReservedJobs != 0 || usage.CompletionQueuedJobs != 0 || usage.CompletionWorkingJobs != 0 ||
			usage.CompletionUnknownJobs != 0 || usage.AccountingUnknownJobs != 0 {
			t.Fatal("paused stable interval retained scoped execution, queued, unfinished or unknown completion/accounting ownership")
		}
		if sample.PauseElapsedMS >= quietPinsAfterMS && usage.ReaderPins != 0 {
			t.Fatal("paused manager reader reservations exceeded the observed real redirect grace")
		}
	}
	if descendants, err := hlsWallPauseOwnedDescendants(h.f.app.cfg.Transcoding.CacheDirectory, targetIDs); err != nil || descendants != 0 {
		t.Fatal("paused target retained a producer descendant in its owned cache directory")
	}
}

func hlsWallPauseAssertCacheBound(t *testing.T, sample, initial hlsWallPauseSample, id, mode string) {
	t.Helper()
	var anchor hlsWallPauseJobSample
	for _, job := range initial.Jobs {
		if job.ID == id {
			anchor = job
		}
	}
	if anchor.ID == "" || !anchor.Directory || len(anchor.Published) == 0 || anchor.ChargedBytes <= 0 {
		t.Fatal("initial completed producer has no physical and charged cache evidence")
	}
	for _, job := range sample.Jobs {
		if job.ID != id {
			continue
		}
		if job.ChargedBytes > anchor.ChargedBytes {
			t.Fatal("paused completed producer increased its charged cache floor")
		}
		files := make(map[string]int64, len(anchor.Published))
		for _, file := range anchor.Published {
			files[file.Name] = file.Bytes
		}
		for _, file := range job.Published {
			if expected, found := files[file.Name]; !found || expected != file.Bytes {
				t.Fatal("paused completed producer changed or appended a closed published artifact")
			}
		}
		if mode == "retained" && (!job.Directory || len(job.Published) != len(anchor.Published) || job.ChargedBytes != anchor.ChargedBytes) {
			t.Fatal("retained-cache pause lost its immutable physical or charged artifacts")
		}
		return
	}
	t.Fatal("paused cache observation lost its owned producer identity")
}

func TestHLSGeneratedWindowWallPauseCaseSelection(t *testing.T) {
	for _, selection := range []string{"2m-retained", "10m-natural-ttl", "2m-retained-job-quota"} {
		cases, err := hlsWallPauseCases(selection)
		if err != nil || len(cases) != 1 || cases[0].Name != selection || cases[0].Duration < 2*time.Minute {
			t.Fatal("wall-time opt-in changed or shortened an explicit case")
		}
	}
	if cases, err := hlsWallPauseCases("all"); err != nil || len(cases) != 6 {
		t.Fatal("wall-time opt-in matrix lost a required duration or cache mode")
	}
	if _, err := hlsWallPauseCases("2s-retained"); err == nil {
		t.Fatal("wall-time verification accepted a shortened duration")
	}
}
