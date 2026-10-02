package server

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func TestGeneratedWindowFallbackDoesNotDemoteOperationalFailures(t *testing.T) {
	for _, err := range []error{
		context.Canceled, context.DeadlineExceeded, transcode.ErrBusy, transcode.ErrQuota, transcode.ErrStart,
		transcode.ErrPersistence, transcode.ErrInvalidInput, transcode.ErrJobCancelled, transcode.ErrManagerClosed,
		errors.Join(transcode.ErrTimelineProbe, syscall.EIO),
		errors.Join(transcode.ErrTimelineProbe, &os.PathError{Op: "read", Path: "controlled source", Err: syscall.EIO}),
		errors.Join(transcode.ErrTimelineProbe, io.EOF),
		errors.Join(transcode.ErrTimelineProbe, io.ErrUnexpectedEOF),
		errors.Join(transcode.ErrTimelineProbe, media.ErrProcessRetirementUnknown),
	} {
		if generatedWindowUnsupported(err) {
			t.Fatalf("an operational fault permanently demoted a source revision: %v", err)
		}
	}
	for _, err := range []error{transcode.ErrUnsupportedTimeline, transcode.ErrInvalidTimeline, transcode.ErrInvalidPlan, transcode.ErrTimelineLimit, transcode.ErrTimelineProbe} {
		if !generatedWindowUnsupported(err) {
			t.Fatalf("unsupported evidence could not preserve the complete-source path before publication: %v", err)
		}
	}
}

func TestGeneratedWindowInvalidStartDoesNotFreezeFallback(t *testing.T) {
	plan, info, _ := generatedWindowGraphPlanFixture()
	for _, start := range []int64{-1, plan.DurationTicks, plan.DurationTicks + 1} {
		session := &hlsSession{key: hlsKey{plan: plan}}
		runtime := &hlsRuntime{}
		if _, err := runtime.prepareGeneratedWindowGraph(context.Background(), session, nil, info, start); err != errHLSRequestInvalid || session.windowGraph != nil {
			t.Fatalf("invalid request changed the presentation mode: start=%d, graph=%+v, error=%v", start, session.windowGraph, err)
		}
	}
}

func TestGeneratedWindowPrivatePlaylistCannotChangeArtifactType(t *testing.T) {
	plan, _, _ := generatedWindowGraphPlanFixture()
	plan.HLS.Window = transcode.HLSWindow{StartNumber: 15, EndTicks: 96 * media.TicksPerSecond, RequireInputEvidence: true}
	for _, name := range []string{"segment-000015.aac", "segment-15.ts", "v0-segment-000015.ts", "segment-000016.ts"} {
		list := transcode.MediaPlaylist{Segments: []transcode.MediaSegment{{Name: name}}}
		if hlsGeneratedWindowPlaylistArtifact(plan, list) {
			t.Fatalf("internal playlist changed the TS-only exact URI contract: %q", name)
		}
	}
	if !hlsGeneratedWindowPlaylistArtifact(plan, transcode.MediaPlaylist{Segments: []transcode.MediaSegment{{Name: "segment-000015.ts"}}}) {
		t.Fatal("canonical private artifact was rejected")
	}
}
