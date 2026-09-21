package media

import "fmt"

// analysisIntroVisualOptions bounds the default intro request to complete
// source-relative sampling intervals. A final partial interval cannot promise
// a decoded frame at or after its nominal start. Explicit ExtractVisual windows
// retain their separate strict sampling contract.
func analysisIntroVisualOptions(info Info, intervalTicks int64) (VisualAnalysisOptions, error) {
	if intervalTicks == 0 {
		intervalTicks = TicksPerSecond / 2
	}
	if intervalTicks < TicksPerSecond/10 || intervalTicks > 10*TicksPerSecond {
		return VisualAnalysisOptions{}, fmt.Errorf("%w: invalid intro visual sample interval", ErrAnalysisUnproven)
	}
	horizon := min(info.DurationTicks, MaxIntroAnalysisTicks)
	if horizon <= 0 {
		return VisualAnalysisOptions{}, fmt.Errorf("%w: invalid intro visual horizon", ErrAnalysisUnproven)
	}
	end := horizon / intervalTicks * intervalTicks
	if end <= 0 {
		return VisualAnalysisOptions{}, fmt.Errorf("%w: no complete intro visual sampling interval", ErrAnalysisUnproven)
	}
	return VisualAnalysisOptions{StartTicks: 0, EndTicks: end, IntervalTicks: intervalTicks}, nil
}
