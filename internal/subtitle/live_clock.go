package subtitle

import (
	"fmt"
	"math"
	"strings"
)

// LiveTimestampMap exposes the input representation's 33-bit transport mapping.
// A missing map is represented by a nil *LiveTimestampMap, never by zero values.
type LiveTimestampMap struct {
	LocalTicks int64
	MPEGTS     uint64
}

// LiveClockAnchor is supplied from measured media in the same source generation.
// MPEGTS is unwrapped 90 kHz PTS. MaxDistance90k must be explicit and less than
// half the 33-bit period; it bounds the permitted distance from that observation.
type LiveClockAnchor struct {
	Known          bool
	Generation     uint64
	MPEGTS         int64
	SourceTicks    int64
	MaxDistance90k int64
}

// ResolveLiveTimestampMap returns the additive LOCAL-to-source tick difference.
// The caller owns generation selection. Nearest-epoch transport values outside
// its measured distance bound, including half-period ambiguity, are rejected.
// The 90 kHz difference is rounded once to the nearest 100 ns tick.
func ResolveLiveTimestampMap(generation uint64, mapping LiveTimestampMap, anchor LiveClockAnchor) (int64, error) {
	const period = int64(1 << 33)
	if generation == 0 || !anchor.Known || anchor.Generation != generation || anchor.MPEGTS < 0 ||
		anchor.SourceTicks < 0 || mapping.LocalTicks < 0 || mapping.MPEGTS >= uint64(period) ||
		anchor.MaxDistance90k <= 0 || anchor.MaxDistance90k >= period/2 {
		return 0, ErrLiveTimestampMap
	}
	difference := int64(mapping.MPEGTS) - anchor.MPEGTS%period
	if difference == period/2 || difference == -period/2 {
		return 0, ErrLiveTimestampMap
	}
	if difference > period/2 {
		difference -= period
	}
	if difference < -period/2 {
		difference += period
	}
	if difference > anchor.MaxDistance90k || difference < -anchor.MaxDistance90k {
		return 0, ErrLiveTimestampMap
	}
	unwrapped, err := liveAddTicks(anchor.MPEGTS, difference)
	if err != nil || unwrapped < 0 {
		return 0, ErrLiveTimestampMap
	}
	// difference is bounded to less than 2^32, so this product cannot overflow.
	product := difference * TicksPerSecond
	if product >= 0 {
		product += 45_000
	} else {
		product -= 45_000
	}
	delta := product / 90_000
	source, err := liveAddTicks(anchor.SourceTicks, delta)
	if err != nil || source < 0 {
		return 0, ErrLiveTimestampMap
	}
	return source - mapping.LocalTicks, nil
}

// MapLiveDocument explicitly rebases cues and inline WebVTT timestamps and
// removes imported transport maps. It also drops source-layout snapshots so
// appending a small batch never retains an entire downloaded source document.
// Cues before logical zero are omitted; a crossing cue is clipped only at zero.
func MapLiveDocument(document Document, deltaTicks int64) (Document, error) {
	if document.Format != FormatWebVTT && document.Format != FormatSRT {
		return Document{}, ErrUnsupportedFormat
	}
	if err := validateCues(document.Cues); err != nil {
		return Document{}, err
	}
	if len(document.header) > MaxLineCount || len(document.blocks) > MaxLineCount {
		return Document{}, ErrLimitExceeded
	}
	result := Document{Format: document.Format}
	for _, line := range document.header {
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "X-TIMESTAMP-MAP") {
			result.header = append(result.header, line)
		}
	}
	before := make([]int, len(document.Cues)+1)
	for index, cue := range document.Cues {
		before[index] = len(result.Cues)
		shifted, include, err := transformCue(cue, Options{CopyTimestamps: true, OffsetTicks: deltaTicks})
		if err != nil {
			return Document{}, err
		}
		if !include {
			continue
		}
		shifted.Text = renderCueText(shifted, document.Format, document.Format, Options{CopyTimestamps: true, OffsetTicks: deltaTicks})
		result.Cues = append(result.Cues, shifted)
	}
	before[len(document.Cues)] = len(result.Cues)
	for _, block := range document.blocks {
		if block.beforeCue < 0 || block.beforeCue > len(document.Cues) {
			return Document{}, ErrInvalidDocument
		}
		result.blocks = append(result.blocks, metadataBlock{beforeCue: before[block.beforeCue], text: block.text})
	}
	if _, _, err := liveDocumentParts(result); err != nil {
		return Document{}, err
	}
	return result, nil
}

func liveAddTicks(value, delta int64) (int64, error) {
	if delta > 0 && value > math.MaxInt64-delta || delta < 0 && value < math.MinInt64-delta {
		return 0, fmt.Errorf("%w: live subtitle clock overflow", ErrInvalidRange)
	}
	return value + delta, nil
}
