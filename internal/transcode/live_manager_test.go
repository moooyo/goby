package transcode

import (
	"errors"
	"testing"
	"time"
)

func TestTouchStreamRequiresActiveExactScopeAndCannotReviveHistory(t *testing.T) {
	scope := Scope{UserID: "owner", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: "play", ItemID: "item", SourceID: "source"}
	old := time.Now().Add(-time.Hour)
	job := &managedJob{record: Record{ID: "job", Spec: Spec{Scope: scope, Plan: Plan{SourceMode: "stream"}}, State: "running", LastAccessAt: old}}
	manager := &Manager{jobs: map[string]*managedJob{"job": job}}
	foreign := scope
	foreign.AuthSessionID = "foreign"
	if err := manager.TouchStream(foreign, "job"); !errors.Is(err, ErrJobNotFound) || !job.record.LastAccessAt.Equal(old) {
		t.Fatal("a foreign scope renewed a stream lease")
	}
	if _, err := manager.Snapshot(scope, "job"); err != nil || !job.record.LastAccessAt.Equal(old) {
		t.Fatal("monitoring renewed a stream lease")
	}
	if err := manager.TouchStream(scope, "job"); err != nil || !job.record.LastAccessAt.After(old) {
		t.Fatalf("authorized consumer activity did not renew the stream: %v", err)
	}
	for _, state := range []string{"completed", "failed", "cancelled"} {
		job.record.State, job.finished, job.record.LastAccessAt = state, true, old
		if err := manager.TouchStream(scope, "job"); err == nil || !job.record.LastAccessAt.Equal(old) {
			t.Fatalf("terminal %s was revived", state)
		}
	}
	job.record.State, job.finished, job.record.Spec.Plan.SourceMode, job.record.LastAccessAt = "running", false, "", old
	if err := manager.TouchStream(scope, "job"); err == nil || !job.record.LastAccessAt.Equal(old) {
		t.Fatal("finite conversion acquired an unrelated stream lease")
	}
}

func TestLiveClockRetainsLongRunningIntegerTimeline(t *testing.T) {
	const fortyDays = int64(40 * 24 * 60 * 60)
	clock := HLSMuxClock{Rendition: 0, PTS: fortyDays, TimeBaseNumerator: 1, TimeBaseDenominator: 1}
	if _, err := clock.Ticks(); err == nil {
		t.Fatal("finite-source clock bound unexpectedly changed")
	}
	got, err := clock.ticks(false)
	if err != nil || got != fortyDays*ticksPerSecond {
		t.Fatalf("long-running stream clock lost its integer timeline: %d %v", got, err)
	}
	parsed, err := liveSecondsTicks("3456000.000000")
	if err != nil || parsed != got {
		t.Fatalf("long-running CSV used a finite media duration cap: %d %v", parsed, err)
	}
}
