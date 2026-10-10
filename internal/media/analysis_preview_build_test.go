package media

import (
	"context"
	"errors"
	"testing"
)

func TestAnalysisPreviewBuildChecksCancellationAfterCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &analysisPreviewBuild{}
	err := state.run(ctx, AnalysisExtractor{}, nil, Info{}, 0, func(PreviewAnalysisExtract) error {
		// Cancellation may arrive during staging after the last successful
		// extraction; returning nil cannot turn that into a successful build.
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || !state.closed || state.proof != nil {
		t.Fatalf("callback cancellation retained a successful build: %v", err)
	}
}

func TestAnalysisPreviewBuildDoesNotStartCanceledCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := (AnalysisExtractor{}).WithPreviewBuild(ctx, nil, Info{}, 0, func(PreviewAnalysisExtract) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled build started its callback: called=%v, error=%v", called, err)
	}
}
