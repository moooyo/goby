package subtitle

import (
	"fmt"
	"math"
)

// RenderHLSWindow selects all cues overlapping the half-open source-clock
// window after applying caption delay. RFC 8216 section 3.5 requires complete
// cue timestamps, including the portions outside this segment's interval.
// The measured transport clock remains independent of caption delay.
func RenderHLSWindow(document Document, startTicks, endTicks, offsetTicks, clockDeltaTicks int64) (Result, error) {
	if startTicks < 0 || endTicks <= startTicks || offsetTicks < -MaxOffsetTicks || offsetTicks > MaxOffsetTicks ||
		clockDeltaTicks < -MaxOffsetTicks || clockDeltaTicks > MaxOffsetTicks {
		return Result{}, fmt.Errorf("%w: invalid HLS subtitle window or clock", ErrInvalidRange)
	}
	if err := validateCues(document.Cues); err != nil {
		return Result{}, err
	}
	window := document
	window.Cues = make([]Cue, 0, len(document.Cues))
	// Metadata block positions refer to the original cue list. Keep STYLE,
	// REGION, and NOTE blocks in their corresponding retained positions.
	before := make([]int, len(document.Cues)+1)
	for index, cue := range document.Cues {
		before[index] = len(window.Cues)
		shifted, include, err := transformCue(cue, Options{CopyTimestamps: true, OffsetTicks: offsetTicks})
		if err != nil {
			return Result{}, err
		}
		if !include {
			continue
		}
		for _, timestamp := range []int64{shifted.StartTicks, shifted.EndTicks} {
			if clockDeltaTicks > 0 && timestamp > math.MaxInt64-clockDeltaTicks || clockDeltaTicks < 0 && timestamp < math.MinInt64-clockDeltaTicks {
				return Result{}, fmt.Errorf("%w: mapped HLS subtitle timestamp overflows", ErrInvalidRange)
			}
		}
		if shifted.StartTicks < shifted.EndTicks && shifted.StartTicks < endTicks && shifted.EndTicks > startTicks {
			window.Cues = append(window.Cues, cue)
		}
	}
	before[len(document.Cues)] = len(window.Cues)
	window.blocks = make([]metadataBlock, 0, len(document.blocks))
	for _, block := range document.blocks {
		if block.beforeCue < 0 || block.beforeCue > len(document.Cues) {
			return Result{}, fmt.Errorf("%w: invalid subtitle metadata position", ErrInvalidDocument)
		}
		window.blocks = append(window.blocks, metadataBlock{beforeCue: before[block.beforeCue], text: block.text})
	}
	return RenderHLS(window, offsetTicks, clockDeltaTicks)
}
