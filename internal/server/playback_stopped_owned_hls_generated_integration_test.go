//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// These opt-in real-media cases cover the whole generated producer and finite
// preparation/slot worker owner chains. They supplement the paced actual
// row-lock witness; they do not substitute for its CPU/IO/reap-before-commit.
func TestHTTPPlaybackStoppedOwnedHLSGeneratedWorkerLifetimes(t *testing.T) {
	runID := os.Getenv("GOBY_STOPPED_USERDATA_LOCK_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_STOPPED_USERDATA_LOCK_RUN_ID admits actual generated ownership cases")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("generated ownership cases require a bounded run identity")
	}
	if testing.Short() {
		t.Skip("generated ownership cases require real media and database fixtures")
	}
	for _, finite := range []bool{false, true} {
		name := "whole_generated"
		if finite {
			name = "finite_generated"
		}
		t.Run(name, func(t *testing.T) {
			h := newHLSGeneratedWindowHTTPFixture(t)
			sourceInfo, err := os.Stat(h.path)
			if err != nil {
				t.Fatal("stat the genuine generated source before retaining worker inputs")
			}
			app := h.f.app
			if !app.correlatedHLSOwnershipEnabled || !app.correlatedHLSEarlyStopEnabled {
				t.Fatal("Server.New did not enable the qualified correlated file-HLS owner chain")
			}
			// This existing experimental selector chooses the actual production
			// branch. Neither the negotiated plan nor source metadata is changed.
			app.hls.generatedWindowsEnabled = finite
			graph := hlsGeneratedWindowHTTPPrepare(t, h, 90*media.TicksPerSecond)
			if graph.session.playbackReference == nil {
				t.Fatal("genuine negotiation omitted its actual registry owner")
			}
			if finite {
				hlsGeneratedWindowHTTPLoad(t, h, &graph)
				_, _, received := hlsGeneratedWindowHTTPExact(t, h, graph, 0, 15)
				hlsGeneratedWindowHTTPDecode(t, h, graph, 0, received.body)
				_, _, _ = hlsGeneratedWindowHTTPExact(t, h, graph, 0, 16)
			} else {
				response := hlsGeneratedWindowHTTPRequest(t, h, http.MethodGet, graph.mainURLs[0], nil, nil)
				expectHLSHTTPStatus(t, response, http.StatusOK)
				if graph.session.windowGraph != nil && graph.session.windowGraph.published {
					t.Fatal("whole producer was silently replaced by finite windows")
				}
			}
			ids := hlsGeneratedWindowHTTPJobIDs(t, h, graph)
			if len(ids) == 0 || finite && len(ids) < 2 {
				t.Fatal("the selected actual generated worker chain did not admit real jobs")
			}
			for _, id := range ids {
				record, err := app.hls.manager.Snapshot(graph.session.key.scope, id)
				if err != nil || record.State != "running" && record.State != "completed" {
					t.Fatal("actual generated job did not retain its scoped manager record")
				}
				if finite != record.Spec.Plan.HLS.Window.RequireInputEvidence || !finite &&
					(record.Spec.Plan.StartTicks != 0 || record.Spec.Plan.HLS.Window != (transcode.HLSWindow{})) {
					t.Fatal("actual producer crossed the selected whole/finite plan contract")
				}
			}
			principal, err := h.f.users.ResolveEmbyForClient(h.f.ctx, graph.owner.headers.Get("X-Emby-Token"), identity.Client{DeviceID: graph.owner.deviceID})
			if err != nil {
				t.Fatal("freshly resolve the genuine generated owner")
			}
			loan, _, err := app.hls.authorizePlaybackSource(h.f.ctx, principal, graph.session)
			if err != nil || loan == nil || loan.owned == nil {
				t.Fatal("retain an actual old generated source loan")
			}
			defer loan.close()
			oldInput, err := loan.owned.duplicate()
			if err != nil {
				t.Fatal("duplicate the actual independently owned generated source")
			}
			defer app.hls.closePlaybackInput(oldInput)
			stopped := hlsGeneratedWindowHTTPRequest(t, h, http.MethodPost, "/emby/Sessions/Playing/Stopped", map[string]any{
				"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, graph.owner.headers)
			expectHLSHTTPStatus(t, stopped, http.StatusNoContent)
			if _, err := oldInput.ensure(context.Background(), app.hls.manager,
				transcode.Spec{Scope: graph.session.key.scope, SourceStamp: graph.session.key.stamp, Plan: graph.session.key.plan}); !errors.Is(err, transcode.ErrJobCancelled) {
				t.Fatal("a delayed generated source FD recreated an actual job after Stop")
			}
			if err := loan.close(); err != nil {
				t.Fatal("close the actual retained generated source FD")
			}
			closed, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := app.hls.Close(closed); err != nil {
				t.Fatal("join the actual generated workers, manager and reader owners")
			}
			if usage := app.playbackStopIntents.usage(); usage != (playbackStopIntentUsage{}) {
				t.Fatal("generated pending/source/Ensure lifetimes survived the actual join")
			}
			resources, ok := app.hls.manager.(interface {
				ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
			})
			if !ok {
				t.Fatal("actual generated manager lacks scoped resource observation")
			}
			usage, err := resources.ResourceUsage(closed, graph.session.key.scope)
			if err != nil || usage != (transcode.ResourceUsage{}) {
				t.Fatal("actual generated scoped jobs/readers/accounting did not drain")
			}
			for _, id := range ids {
				if _, err := os.Stat(filepath.Join(app.cfg.Transcoding.CacheDirectory, id)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("actual stopped generated cache survived join")
				}
			}
			if descriptors := playbackStopAliasSourceFDs(t, sourceInfo); descriptors != 0 {
				t.Fatal("actual generated source descriptors survived join")
			}
			t.Logf("actual_generated_owner_chain=%s old_input_rejected=true joined_scope_zero=true original_emby_player_used=false", name)
		})
	}
}
