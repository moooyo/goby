package media

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"time"
)

type SubtitleTimelineCapabilities struct {
	Available     bool
	Reason        string
	FFprobeSHA256 string
}

// SubtitleTimelineAvailability inventories only the pinned demuxer. An empty
// configured hash permits controlled startup inventory; generation requires the
// returned hash. No media source, OCR model, or global analysis switch is read.
func SubtitleTimelineAvailability(ctx context.Context, config BitmapSubtitleConfig) (result SubtitleTimelineCapabilities, resultErr error) {
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if runtime.GOOS != "linux" {
		result.Reason = "platform_unavailable"
		return result, nil
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	unavailable := func(reason string, err error) (SubtitleTimelineCapabilities, error) {
		result.Available, result.Reason = false, reason
		if bounded.Err() != nil {
			return result, errors.Join(bounded.Err(), err)
		}
		return result, nil
	}
	if !filepath.IsAbs(config.FFprobePath) {
		return unavailable("ffprobe_unavailable", ErrAnalysisUnavailable)
	}
	tool, err := analysisOpenToolExpected(bounded, config.FFprobePath, config.FFprobeSHA256)
	if err != nil {
		return unavailable("ffprobe_unavailable", err)
	}
	defer tool.file.Close()
	if tool.before.Size() > 128<<20 {
		return unavailable("ffprobe_unavailable", ErrAnalysisUnavailable)
	}
	result.FFprobeSHA256 = tool.sha
	if err := analysisValidateFFprobe(bounded, tool); err != nil {
		return unavailable("ffprobe_profile_unavailable", err)
	}
	if err := tool.check(); err != nil {
		return unavailable("ffprobe_changed", err)
	}
	result.Available = true
	return result, nil
}
