//go:build linux

package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func playbackAdmissionNoopActualOptIn(t *testing.T) {
	t.Helper()
	runID := os.Getenv("GOBY_STOPPED_USERDATA_LOCK_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_STOPPED_USERDATA_LOCK_RUN_ID admits actual optional-hook compatibility")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("optional-hook compatibility requires a bounded run identity")
	}
	if testing.Short() {
		t.Skip("optional-hook compatibility requires real media and database fixtures")
	}
}

func TestHTTPPlaybackAdmissionNoopDefaultHLSAndEnabledProgressiveEnsure(t *testing.T) {
	playbackAdmissionNoopActualOptIn(t)
	for _, progressive := range []bool{false, true} {
		name := "default_file_hls"
		if progressive {
			name = "enabled_uncovered_progressive"
		}
		t.Run(name, func(t *testing.T) {
			real := newPlaybackStopAliasRealFixture(t)
			f, h := real.control, real.hls
			if f.app.correlatedHLSOwnershipEnabled || f.app.correlatedHLSEarlyStopEnabled {
				t.Fatal("the actual default fixture enabled private ownership flags")
			}
			f.app.correlatedHLSOwnershipEnabled, f.app.correlatedHLSEarlyStopEnabled = progressive, progressive
			principal, headers := f.principal(t, "normal")
			graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID,
				deviceID: principal.Client.DeviceID, headers: headers}, 0)
			f.app.hls.mu.Lock()
			session := f.app.hls.sessions[graph.hlsID]
			f.app.hls.mu.Unlock()
			if session == nil || (session.playbackReference != nil) != progressive {
				t.Fatal("the actual fixture did not retain its selected ownership mode")
			}
			fixture := stoppedOwnedHLSMatrixFixture{real: real, principal: principal, headers: headers, graph: graph, session: session}
			expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing", map[string]any{
				"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, headers), http.StatusNoContent)
			var process playbackStopAliasEncoder
			if !progressive {
				process = fixture.startActualEncoder(t)
			} else {
				body := videoHTTPBody(true, videoHTTPProfile("http", true, true))
				body["CurrentPlaySessionId"] = graph.playID
				response := h.request(t, http.MethodPost, "/emby/Items/"+h.item.ID+"/PlaybackInfo", body, headers)
				expectHLSHTTPStatus(t, response, http.StatusOK)
				var negotiation struct {
					PlayID  string           `json:"PlaySessionId"`
					Sources []map[string]any `json:"MediaSources"`
				}
				if json.Unmarshal(response.body, &negotiation) != nil || negotiation.PlayID != graph.playID || len(negotiation.Sources) != 1 || negotiation.Sources[0]["TranscodingSubProtocol"] != "http" {
					t.Fatal("natural progressive negotiation changed its genuine play or protocol")
				}
				target, ok := negotiation.Sources[0]["TranscodingUrl"].(string)
				if !ok || target == "" {
					t.Fatal("natural progressive negotiation omitted its actual URI")
				}
				ctx, cancel := context.WithCancel(f.ctx)
				joined := make(chan struct{})
				go func() {
					defer close(joined)
					request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server.URL+target, nil)
					if err != nil {
						return
					}
					request.Header = headers.Clone()
					response, err := h.server.Client().Do(request)
					if err == nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
					}
				}()
				t.Cleanup(func() {
					cancel()
					select {
					case <-joined:
					case <-time.After(5 * time.Second):
						t.Error("the actual no-op progressive consumer did not join")
					}
				})
				process = playbackStopAliasObserveEncoder(t, real, session.key.scope)
			}
			record, err := f.app.hls.manager.Snapshot(session.key.scope, process.jobID)
			if err != nil || record.State != "running" || (record.Spec.Plan.OutputMode == "progressive") != progressive || record.Spec.Plan.SourceMode == "stream" {
				t.Fatal("the actual Ensure did not launch the naturally selected default/uncovered producer")
			}
			if status := fixture.stop(t, f.ctx); status != http.StatusNoContent {
				t.Fatal("standard Stop failed after real optional-hook admission")
			}
			stoppedOwnedHLSMatrixRetired(t, process)
			cleanup, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if err := f.app.hls.Close(cleanup); err != nil {
				t.Fatal("actual optional-hook compatibility owners did not join")
			}
			if playbackStopAliasSourceFDs(t, real.source) != 0 || f.app.playbackStopIntents.usage() != (playbackStopIntentUsage{}) {
				t.Fatal("actual default/uncovered admission retained source or lifetime owners")
			}
			t.Logf("actual_optional_hook_ensure=%s launched_and_reaped=true original_emby_player_used=false", name)
		})
	}
}

func TestHTTPPlaybackAdmissionNoopEnabledDynamicEnsureStream(t *testing.T) {
	playbackAdmissionNoopActualOptIn(t)
	d := newDynamicTimeshiftHTTPFixture(t, false)
	h, app := d.h, d.h.f.app
	app.correlatedHLSOwnershipEnabled, app.correlatedHLSEarlyStopEnabled = true, true
	presentation := d.open(t)
	d.waitWindow(t, presentation, func(window dynamicHTTPWindow) bool {
		return window.LiveEdgeTicks-window.EarliestTicks >= 6*media.TicksPerSecond
	})
	playlist := h.request(t, http.MethodGet, presentation.mainURL, nil, nil)
	expectHLSHTTPStatus(t, playlist, http.StatusOK)
	children := hlsHTTPManifestChildren(playlist.body)
	if len(children) == 0 {
		t.Fatal("the actual stream Ensure did not publish a real media slice")
	}
	segment := h.request(t, http.MethodGet, children[0], nil, nil)
	expectHLSHTTPStatus(t, segment, http.StatusOK)
	if len(segment.body) < 188 || segment.body[0] != 0x47 {
		t.Fatal("the actual stream producer returned no framed TS bytes")
	}
	app.dynamicStreams.mu.Lock()
	session := app.dynamicStreams.sessions[presentation.presentationID]
	app.dynamicStreams.mu.Unlock()
	if session == nil {
		t.Fatal("the actual dynamic presentation lost its scoped owner")
	}
	session.mu.Lock()
	id, scope := session.jobID, session.scope
	session.mu.Unlock()
	record, err := app.hls.manager.Snapshot(scope, id)
	if err != nil || record.Spec.Plan.SourceMode != "stream" || record.State != "running" || app.playbackStopIntents.usage() != (playbackStopIntentUsage{}) {
		t.Fatal("enabled uncovered stream admission did not preserve its actual manager path")
	}
	report := map[string]any{"PlaySessionId": presentation.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}
	expectHLSHTTPStatus(t, h.request(t, http.MethodPost, "/emby/Sessions/Playing/Stopped", report, h.accounts.viewer.headers), http.StatusNoContent)
	cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := app.closeDynamicSources(cleanup); err != nil {
		t.Fatal("join actual dynamic source/readers after Stop")
	}
	if err := app.hls.Close(cleanup); err != nil {
		t.Fatal("join actual stream runner/manager after Stop")
	}
	resources, ok := app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("actual stream compatibility lacks scoped resource observation")
	}
	usage, err := resources.ResourceUsage(cleanup, scope)
	if err != nil || usage != (transcode.ResourceUsage{}) {
		t.Fatal("actual enabled-uncovered stream admission retained resources")
	}
	t.Log("actual_optional_hook_ensure_stream=enabled_uncovered_dynamic actual_ts_published=true joined_scope_zero=true original_emby_player_used=false")
}
