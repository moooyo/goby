package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
)

// PreviewAnalysisExtract emits one width within WithPreviewBuild. It has the
// same provisional, borrowed-JPEG contract as ExtractPreviews. Calls must be
// synchronous and sequential within the build callback.
type PreviewAnalysisExtract func(PreviewAnalysisOptions, func(PreviewFrame) error) (PreviewAnalysisSummary, error)

// WithPreviewBuild shares one complete source audit across at most three
// distinct widths with the same interval. Each extraction separately acquires
// and releases process admission and its per-variant timeout. The caller's
// context bounds the entire build, including work between extractions.
//
// The callback owns staging and all-variant publication. Its extract function
// is invalid after any extraction failure or after the callback returns; source
// proof is private to this call and never survives it. Every output pass still
// decodes and audits the entire source with current source/tool checks.
func (extractor AnalysisExtractor) WithPreviewBuild(ctx context.Context, file *os.File, info Info, streamIndex int, build func(PreviewAnalysisExtract) error) error {
	state := &analysisPreviewBuild{}
	return state.run(ctx, extractor, file, info, streamIndex, build)
}

type analysisPreviewBuild struct {
	mu        sync.Mutex
	closed    bool
	widths    [3]int
	count     int
	err       error
	proof     *analysisPreviewHoldProof
	before    os.FileInfo
	geometry  analysisDisplayGeometry
	ffmpegSHA string
	interval  int64
	source    analysisPreviewHoldUsage
	actual    analysisPreviewHoldUsage
}

func (state *analysisPreviewBuild) run(ctx context.Context, extractor AnalysisExtractor, file *os.File, info Info, streamIndex int, build func(PreviewAnalysisExtract) error) (resultErr error) {
	if ctx == nil || build == nil {
		return ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	defer func() {
		// Wait for a running extraction to retire before releasing its proof,
		// even if a caller violates the synchronous callback contract.
		state.mu.Lock()
		defer state.mu.Unlock()
		state.closed, state.proof = true, nil
		resultErr = errors.Join(resultErr, state.err, ctx.Err())
	}()
	return build(func(options PreviewAnalysisOptions, emit func(PreviewFrame) error) (PreviewAnalysisSummary, error) {
		// Fail a reentrant callback instead of waiting for our own extraction.
		if !state.mu.TryLock() {
			return PreviewAnalysisSummary{}, fmt.Errorf("%w: concurrent preview build extraction", ErrAnalysisUnproven)
		}
		defer state.mu.Unlock()
		if state.closed {
			return PreviewAnalysisSummary{}, fmt.Errorf("%w: preview build has finished", ErrAnalysisUnproven)
		}
		if state.err != nil {
			return PreviewAnalysisSummary{}, state.err
		}
		width, interval := options.Width, options.IntervalTicks
		if width == 0 {
			width = 320
		}
		if interval == 0 {
			interval = 10 * TicksPerSecond
		}
		if state.count == len(state.widths) || state.proof != nil && interval != state.interval {
			state.err = fmt.Errorf("%w: preview build variants or interval", ErrAnalysisUnproven)
			return PreviewAnalysisSummary{}, state.err
		}
		for _, previous := range state.widths[:state.count] {
			if previous == width {
				state.err = fmt.Errorf("%w: duplicate preview build width", ErrAnalysisUnproven)
				return PreviewAnalysisSummary{}, state.err
			}
		}
		state.widths[state.count], state.count = width, state.count+1
		summary, err := extractor.extractPreviews(ctx, file, info, streamIndex, options, emit, state)
		// extractPreviews has completed all deferred source/tool checks and
		// released admission before another variant may use its source proof.
		if err != nil {
			state.err, state.proof = err, nil
		}
		return summary, err
	})
}
