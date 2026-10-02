package server

import (
	"context"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedWindowReportSeekUsesCommittedTimeAndSlotScale(t *testing.T) {
	stamp := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
	previous := hlsPlaybackDemand{initialized: true, position: 90 * media.TicksPerSecond, updated: stamp}
	for _, fixture := range []struct {
		name     string
		position int64
		elapsed  time.Duration
		seek     bool
	}{
		{"backward thirty seconds", 60, time.Second, true},
		{"forward thirty seconds in one second", 120, time.Second, true},
		{"ordinary delayed progress", 120, 30 * time.Second, false},
		{"within current slot", 94, time.Second, false},
		{"small backward report jitter", 89, time.Second, false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			current := hlsPlaybackDemand{initialized: true, position: fixture.position * media.TicksPerSecond, updated: stamp.Add(fixture.elapsed)}
			if seek := hlsGeneratedReportSeek(previous, current, 6); seek != fixture.seek {
				t.Fatalf("committed position/time did not preserve the expected private interval: got=%v want=%v", seek, fixture.seek)
			}
		})
	}
}

func TestGeneratedWindowPreparationReportAcknowledgesOnlyAlignedSourceInterval(t *testing.T) {
	for _, position := range []int64{91, 96} {
		h, _ := hlsRuntimeTestFixture(t)
		session := hlsRuntimeTestSession(t, h, "aligned-preparation", false)
		plan, _, _ := generatedWindowGraphPlanFixture()
		session.key.plan = plan
		stamp := time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC)
		previous := hlsDemandTestPlay(session, 1, "Playing", 0)
		previous.UpdatedAt = stamp
		h.applyPlaybackSnapshot(previous)
		pending := newHLSAdmission(context.Background(), session, transcode.Spec{Scope: session.key.scope, Plan: plan},
			hlsGeneratedGraphPreparation, hlsGeneratedGraphPreparation)
		pending.anchorTicks, pending.anchorEndTicks = 90*media.TicksPerSecond, 96*media.TicksPerSecond
		session.admission = pending
		current := hlsDemandTestPlay(session, 2, "Playing", position*media.TicksPerSecond)
		current.UpdatedAt = stamp.Add(time.Second)
		h.applyPlaybackSnapshot(current)
		canceled := pending.ctx.Err() != nil
		pending.stopSession()
		pending.cancel()
		if canceled != (position == 96) {
			t.Fatalf("report position %d acknowledged a different source interval: canceled=%v", position, canceled)
		}
	}
}
