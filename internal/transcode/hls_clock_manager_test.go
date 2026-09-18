package transcode

import (
	"context"
	"errors"
	"testing"
)

func TestHLSClockEvidenceRequiresExactScopeAndLiveProducer(t *testing.T) {
	scope := Scope{UserID: "owner", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: "play", ItemID: "item", SourceID: "source"}
	plan := Plan{Subtitle: SubtitlePlan{Mode: "hls", Codec: "subrip", StreamIndex: 2}}
	clock := HLSMuxClock{PTS: 7, TimeBaseNumerator: 1, TimeBaseDenominator: 90000}
	job := &managedJob{record: Record{ID: "job", Spec: Spec{Scope: scope, Plan: plan}, State: "running"}, ready: true, changed: make(chan struct{})}
	job.hlsClocks[0], job.hlsClockKnown[0] = clock, true
	manager := &Manager{jobs: map[string]*managedJob{"job": job}}
	got, err := manager.HLSClock(context.Background(), scope, "job", 0)
	if err != nil || got != clock {
		t.Fatalf("exact clock evidence = %+v, %v", got, err)
	}
	foreign := scope
	foreign.AuthSessionID = "other"
	if _, err := manager.HLSClock(context.Background(), foreign, "job", 0); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("foreign clock read = %v", err)
	}
	if _, err := manager.HLSClock(context.Background(), scope, "job", 1); !errors.Is(err, ErrInvalidTimeline) {
		t.Fatalf("unplanned rendition clock read = %v", err)
	}
	job.stopCode = "cancelled"
	if _, err := manager.HLSClock(context.Background(), scope, "job", 0); !errors.Is(err, ErrJobCancelled) {
		t.Fatalf("cancelled clock read = %v", err)
	}
}
