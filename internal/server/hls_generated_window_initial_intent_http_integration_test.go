//go:build linux

package server

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestHTTPGeneratedWindowInitialIntentKeepsMatchingPrefetchAndCancelsChangedStart(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		position int64
		matching bool
	}{{"matching_negotiated_start", 90, true}, {"changed_initial_position", 30, false}} {
		t.Run(fixture.name, func(t *testing.T) {
			h := newHLSGeneratedWindowHTTPFixture(t)
			graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
			var state string
			var position, revision int64
			if err := h.f.pool.QueryRow(h.f.ctx, "SELECT state,position_ticks,playback_revision FROM play_sessions WHERE id=$1 AND auth_session_id=$2",
				graph.playID, graph.owner.id).Scan(&state, &position, &revision); err != nil {
				t.Fatal("read prepared initial-intent evidence")
			}
			graph.session.mu.Lock()
			intent := graph.session.demand.initialIntent
			prepared := graph.session.demand
			graph.session.mu.Unlock()
			if state != "Prepared" || position != 0 || revision != 0 || prepared.state != state || prepared.position != position ||
				prepared.revision != revision || !intent.valid || !intent.captured || intent.position != 90*media.TicksPerSecond || intent.revision != revision {
				t.Fatal("verified PlaybackInfo rewrote committed Prepared progress or omitted its fenced intention")
			}
			hlsGeneratedWindowHTTPLoad(t, h, &graph)
			initialIDs := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
			if len(initialIDs) != 1 {
				t.Fatal("initial source graph did not retain exactly one actual high window")
			}

			// Hold publication after the actual next-window job has completed.
			// A report must preserve or supersede that real owned admission;
			// a fake manager record cannot establish this cancellation boundary.
			probes := h.f.app.hls.probes
			reserved := 0
			var releaseOnce sync.Once
			release := func() {
				releaseOnce.Do(func() {
					for range reserved {
						<-probes
					}
				})
			}
			t.Cleanup(release)
			for reserved < cap(probes) {
				select {
				case probes <- struct{}{}:
					reserved++
				default:
					t.Fatal("initial source proofs retained their probe reservations")
				}
			}
			ctx, cancel := context.WithTimeout(h.f.ctx, 30*time.Second)
			type outcome struct {
				response hlsHTTPResponse
				err      error
			}
			result := make(chan outcome, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				response, err := hlsGeneratedWindowHTTPDo(ctx, h, http.MethodGet, graph.mediaURLs[0][16], nil, nil)
				result <- outcome{response: response, err: err}
			}()
			t.Cleanup(func() {
				cancel()
				release()
				select {
				case <-finished:
				case <-time.After(5 * time.Second):
					t.Error("initial-intent HTTP request did not join before fixture retirement")
				}
			})
			var pending *hlsAdmission
			var admissionRevision uint64
			hlsGeneratedWindowHTTPWait(t, h, 10*time.Second, func() bool {
				graph.session.mu.Lock()
				defer graph.session.mu.Unlock()
				pending = graph.session.admission
				admissionRevision = graph.session.admissionRevision
				return pending != nil && pending.first == 16 && pending.ctx.Err() == nil &&
					pending.spec.Plan.HLS.Window.RequireInputEvidence && pending.spec.Plan.HLS.Window.NativeClockVersion == transcode.GeneratedWindowNativeClockV1
			}, "actual next-window prefetch did not enter its owned admission")
			var targetID string
			hlsGeneratedWindowHTTPWait(t, h, 10*time.Second, func() bool {
				ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
				if len(ids) != 2 {
					return false
				}
				for _, id := range ids {
					if id != initialIDs[0] {
						targetID = id
					}
				}
				record, err := h.f.app.hls.manager.Snapshot(graph.session.key.scope, targetID)
				if err != nil || record.State == "failed" || record.State == "cancelled" {
					t.Fatalf("actual prefetch failed before controlled publication: %+v, %v", record, err)
				}
				return record.State == "completed" && record.Spec.Plan.StartTicks == 96*media.TicksPerSecond &&
					record.Spec.Plan.HLS.Window.EndTicks == 100*media.TicksPerSecond && record.Spec.Plan.HLS.Window.StartNumber == 16
			}, "actual next-window job did not complete before its held publication proof")
			started := map[string]any{"ItemId": h.item.ID, "PlaySessionId": graph.playID,
				"PositionTicks": fixture.position * media.TicksPerSecond, "IsPaused": false}
			expectHLSHTTPStatus(t, hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing", started, graph.owner.headers), http.StatusNoContent)
			if err := h.f.pool.QueryRow(h.f.ctx, "SELECT state,position_ticks,playback_revision FROM play_sessions WHERE id=$1 AND auth_session_id=$2",
				graph.playID, graph.owner.id).Scan(&state, &position, &revision); err != nil {
				t.Fatal("read committed Started initial-intent evidence")
			}
			graph.session.mu.Lock()
			current := graph.session.demand
			preserved := graph.session.admission == pending && pending.ctx.Err() == nil && graph.session.admissionRevision == admissionRevision
			canceled := pending.ctx.Err() != nil && graph.session.admissionRevision > admissionRevision
			graph.session.mu.Unlock()
			if state != "Playing" || position != fixture.position*media.TicksPerSecond || revision != 1 || current.state != state ||
				current.position != position || current.revision != revision || current.paused || current.initialIntent.valid || !current.initialIntent.captured {
				t.Fatal("initial-intent handling changed committed Started demand or retained its one-shot intention")
			}
			if fixture.matching && !preserved || !fixture.matching && !canceled {
				t.Fatal("first Started did not distinguish the negotiated initial position from a changed source position")
			}
			release()
			select {
			case completed := <-result:
				if completed.err != nil {
					t.Fatalf("controlled initial-intent request failed: error_type=%T", completed.err)
				}
				if fixture.matching {
					expectHLSHTTPStatus(t, completed.response, http.StatusTemporaryRedirect)
					location := hlsHTTPURL(t, completed.response.header.Get("Location"), graph.owner.headers.Get("X-Emby-Token"))
					if location.Query().Get(hlsProducerQuery) != targetID {
						t.Fatal("matching Started redirected the same prefetch to a replacement producer")
					}
					exact := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, location.String(), nil, http.Header{"Range": {"bytes=0-31"}})
					expectHLSHTTPStatus(t, exact, http.StatusPartialContent)
					if len(exact.body) != 32 {
						t.Fatal("matching prefetch did not publish its actual bounded exact bytes")
					}
				} else {
					expectHLSHTTPStatus(t, completed.response, http.StatusNotFound)
				}
			case <-ctx.Done():
				t.Fatal("controlled initial-intent request did not finish after proof release")
			}
			select {
			case <-pending.done:
			case <-ctx.Done():
				t.Fatal("initial-intent admission did not retire its actual owned worker")
			}
			if ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph); len(ids) != 2 {
				t.Fatal("initial-intent handling created a replacement or complete-source producer")
			}
			graph.session.mu.Lock()
			binding, bound := graph.session.windowGraph.slots[16]
			graph.session.mu.Unlock()
			if fixture.matching && (!bound || binding.producer.id != targetID) || !fixture.matching && bound {
				t.Fatal("initial-intent publication did not preserve only the current proved prefetch")
			}
			t.Logf("actual initial intent: requested_seconds=90 reported_seconds=%d matching=%t committed_revision=%d replacement_jobs=0", fixture.position, fixture.matching, revision)
		})
	}
}
