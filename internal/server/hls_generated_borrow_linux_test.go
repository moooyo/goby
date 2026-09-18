//go:build linux

package server

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
)

type hlsBorrowedSourceJobs struct {
	*hlsRuntimeTestJobs
	input *os.File
}

func (jobs *hlsBorrowedSourceJobs) Ensure(_ context.Context, spec transcode.Spec, input *os.File) (transcode.Record, error) {
	jobs.input = input
	return transcode.Record{ID: "borrowed-source-producer", Spec: spec, State: "running"}, nil
}

func TestGeneratedHLSProducerOwnsIndependentSourceDescriptor(t *testing.T) {
	h, base := hlsRuntimeTestFixture(t)
	jobs := &hlsBorrowedSourceJobs{hlsRuntimeTestJobs: base}
	h.manager = jobs
	session := hlsRuntimeTestSession(t, h, "borrowed-source", false)
	session.key.plan.Container, session.key.plan.HLS.SegmentType = "mp4", "fmp4"
	input := hlsRuntimeInput(t)
	if _, err := input.Write([]byte("pinned source bytes")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := h.generatedArtifact(ctx, session, input, "main.m3u8"); result <- err }()
	select {
	case <-base.opens:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if jobs.input == input {
		t.Fatal("producer received the HTTP-owned file object")
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	defer jobs.input.Close()
	data := make([]byte, len("pinned source bytes"))
	if _, err := jobs.input.ReadAt(data, 0); err != nil || string(data) != "pinned source bytes" {
		t.Fatalf("returning the HTTP response closed the producer input: %q, %v", data, err)
	}
	base.releaseAll()
	if err := <-result; !errors.Is(err, errHLSRuntimeTestReleased) {
		t.Fatalf("unexpected artifact completion: %v", err)
	}
}
