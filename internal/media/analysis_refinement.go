package media

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"os"

	"github.com/moooyo/goby/internal/introdetect"
)

const (
	// IntroRefinementProfile identifies a separate bounded raster stream. The
	// original audio and 500 ms visual evidence keep their existing policies.
	IntroRefinementProfile                = "r16-120s-100ms-optcad2"
	analysisRefinementSide                = 16
	analysisRefinementIntervalTicks       = TicksPerSecond / 10
	analysisExactFloatInteger       int64 = 1<<53 - 1
)

// ErrRefinementCadenceUnsupported is returned only after a clean bounded
// decode and all source/tool checks prove that actual source frames cannot
// cover every dense slot. It is distinct from unproven timestamps or failures.
var ErrRefinementCadenceUnsupported = errors.New("source cadence cannot cover the refinement sampling slots")

const RefinementCadenceUnavailableReason = "source_cadence_unsupported"

// FFmpeg expressions use doubles. Multiplying selected_n by decimal 0.1 can
// skip an exactly aligned source frame, while the independent integer audit
// correctly expects it. Use reduced integer PTS products for this new stream.
// The audit verifies the declared time base and every evaluated product stays
// in the exactly representable integer range; it never adds a clock tolerance.
func analysisRefinementPTSSelection(info Info, stream Stream, plan *analysisVisualPlan) error {
	base, err := analysisTimeBase(stream.TimeBase)
	if err != nil {
		return fmt.Errorf("%w: refinement selector time base", ErrAnalysisUnproven)
	}
	start := new(big.Int).Add(big.NewInt(info.FormatStartTicks), big.NewInt(plan.start))
	start.Mul(start, base.Denom())
	step := new(big.Int).Mul(big.NewInt(plan.interval), base.Denom())
	denominator := new(big.Int).Mul(big.NewInt(TicksPerSecond), base.Num())
	divisor := new(big.Int).GCD(nil, nil, start, step)
	divisor.GCD(nil, nil, divisor, denominator)
	start.Quo(start, divisor)
	step.Quo(step, divisor)
	denominator.Quo(denominator, divisor)
	span := new(big.Int).Mul(new(big.Int).Set(step), big.NewInt(int64(plan.frames-1)))
	last := new(big.Int).Add(new(big.Int).Set(span), start)
	for _, value := range []*big.Int{start, step, denominator, span, last} {
		if !value.IsInt64() || value.Int64() > analysisExactFloatInteger || value.Int64() < -analysisExactFloatInteger {
			return fmt.Errorf("%w: refinement selector integer range", ErrAnalysisUnproven)
		}
	}
	plan.selectExpression = "gte(pts*" + denominator.String() + "," + start.String() + "+selected_n*" + step.String() + ")"
	plan.selectorTimeBase = base
	plan.selectorMultiplier = denominator.Int64()
	return nil
}

func analysisIntroRefinementPlan(info Info, limits AnalysisLimits) (analysisVisualPlan, error) {
	horizon := min(info.DurationTicks, introdetect.RefinementPrefixTicks)
	end := horizon / analysisRefinementIntervalTicks * analysisRefinementIntervalTicks
	if end <= 0 {
		return analysisVisualPlan{}, fmt.Errorf("%w: no complete refinement sampling interval", ErrAnalysisUnproven)
	}
	plan, err := analysisVisualRasterOptions(info, VisualAnalysisOptions{EndTicks: end, IntervalTicks: analysisRefinementIntervalTicks}, limits, analysisRefinementSide)
	if err != nil {
		return analysisVisualPlan{}, err
	}
	if plan.frames > introdetect.MaxRefinementSamples {
		return analysisVisualPlan{}, fmt.Errorf("%w: refinement sample plan", ErrAnalysisBudget)
	}
	plan.optionalRefinement = true
	return plan, nil
}

// ExtractRefinement decodes actual source frames into small grayscale rasters.
// These samples supplement visual sequence discovery; they do not interpolate
// or replace the coarse audiovisual matcher evidence. The admitted source,
// executable descriptors, display geometry, original packet PTS and complete
// sampling slots are checked by the same decoder used by ExtractVisual. A
// proven cadence shortfall returns only ErrRefinementCadenceUnsupported and no
// partial rasters. Other extraction failures never authorize this abstention.
func (extractor AnalysisExtractor) ExtractRefinement(ctx context.Context, file *os.File, info Info, streamIndex int) ([]introdetect.RefinementSample, error) {
	if ctx == nil {
		return nil, ErrAnalysisUnavailable
	}
	limits, err := extractor.analysisLimits()
	if err != nil {
		return nil, err
	}
	plan, err := analysisIntroRefinementPlan(info, limits)
	if err != nil {
		return nil, err
	}
	samples := make([]introdetect.RefinementSample, 0, plan.frames)
	err = extractor.extractVisualPlan(ctx, file, info, streamIndex, plan, limits, func(frame analysisVisualFrame, gray []byte) error {
		sample := introdetect.RefinementSample{Ticks: frame.actual}
		if len(gray) != len(sample.Raster) {
			return fmt.Errorf("%w: incomplete refinement raster", ErrAnalysisUnproven)
		}
		copy(sample.Raster[:], gray)
		samples = append(samples, sample)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return samples, nil
}
