//go:build linux

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/moooyo/goby/internal/transcode"
)

func TestHLSAdmissionGeneratedArtifactsShareCreationAndBorrowInputs(t *testing.T) {
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "generated-admission", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	firstInput := hlsRuntimeInput(t)
	firstResult := make(chan error, 1)
	go func() {
		_, err := h.generatedArtifact(context.Background(), session, firstInput, "main.m3u8")
		firstResult <- err
	}()
	call := hlsAdmissionCall(t, jobs)
	if call.input == firstInput {
		t.Fatal("generated producer received the borrowed request file object")
	}
	if _, err := firstInput.Stat(); err != nil {
		t.Fatalf("Ensure closed the borrowed request input: %v", err)
	}
	var results []<-chan error
	for _, name := range []string{"init.mp4", "segment-000000.m4s"} {
		input := hlsRuntimeInput(t)
		result := make(chan error, 1)
		results = append(results, result)
		ctx := hlsAdmissionObserve(context.Background())
		go func() { _, err := h.generatedArtifact(ctx, session, input, name); result <- err }()
		hlsAdmissionJoined(t, ctx)
	}
	call.finish()
	hlsAdmissionResult(t, firstResult, errHLSRuntimeTestReleased)
	for _, result := range results {
		hlsAdmissionResult(t, result, errHLSRuntimeTestReleased)
	}
	jobs.mu.Lock()
	count := len(jobs.calls)
	jobs.mu.Unlock()
	if count != 1 {
		t.Fatalf("playlist, map and fragment created %d independent producers", count)
	}
	seen := make(map[string]bool)
	for range 3 {
		opened := <-jobs.opens
		if opened.id != call.id || opened.scope != session.key.scope {
			t.Fatalf("generated artifact borrowed a different producer or scope: %+v", opened)
		}
		seen[opened.name] = true
	}
	if len(seen) != 3 {
		t.Fatalf("generated artifact names were mixed: %v", seen)
	}
}

func TestHLSAdmissionGeneratedCancelledCreatorKeepsBorrowedInputOpen(t *testing.T) {
	h, jobs := hlsAdmissionFixture(t)
	session := hlsRuntimeTestSession(t, h, "generated-cancel", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	input := hlsRuntimeInput(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := h.generatedArtifact(ctx, session, input, "main.m3u8"); result <- err }()
	call := hlsAdmissionCall(t, jobs)
	cancel()
	hlsAdmissionResult(t, result, context.Canceled)
	if _, err := input.Stat(); err != nil {
		t.Fatalf("cancelled generated request closed its borrowed input: %v", err)
	}
	call.finish()
	h.workers.Wait()
	if _, err := jobs.Snapshot(session.key.scope, call.id); !errors.Is(err, transcode.ErrJobNotFound) {
		t.Fatalf("cancelled generated admission retained a late record: %v", err)
	}
}
