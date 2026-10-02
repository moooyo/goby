package transcode

import (
	"bytes"
	"context"
	"encoding/json"
	"math/big"
	"strings"
	"sync"
)

const (
	maxGeneratedSourceRangeBytes  = 2 << 20
	maxGeneratedSourceRangeFrames = 2400
)

// GeneratedSourceRange describes decoded source frames covering one requested
// interval. The rational clocks are source-relative seconds, after subtracting
// the independently checked format origin. End is the last selected frame's
// observed presentation end; StartTicks and EndTicks retain the requested range.
// This interval probe does not establish source EOF or a full-source timeline.
type GeneratedSourceRange struct {
	StartTicks, EndTicks int64
	FrameCount           int64
	First, Last, End     GeneratedRational
	FrameDuration        GeneratedRational
}

type generatedSourceRangeFrame struct {
	Kind              string          `json:"media_type"`
	StreamIndex       *int64          `json:"stream_index"`
	PTS               *int64          `json:"pts"`
	Duration          *int64          `json:"duration"`
	ProjectedSideData json.RawMessage `json:"side_data_list"`
}

type generatedSourceRangeStream struct {
	Index    *int64 `json:"index"`
	Kind     string `json:"codec_type"`
	TimeBase string `json:"time_base"`
}

type generatedSourceRangeFormat struct {
	StartTime *string `json:"start_time"`
}

type generatedSourceRangeDocument struct {
	Frames       []json.RawMessage `json:"frames"`
	Streams      []json.RawMessage `json:"streams"`
	Format       json.RawMessage   `json:"format"`
	Programs     []json.RawMessage `json:"programs"`
	StreamGroups []json.RawMessage `json:"stream_groups"`
}

func generatedSourceRangePlan(plan Plan, formatOriginTicks int64) error {
	if ValidatePlan(plan) != nil || !plan.HLS.Window.RequireInputEvidence || !GeneratedWindowClosureEligible(plan) ||
		formatOriginTicks < -maxDurationTicks || formatOriginTicks > maxDurationTicks {
		return ErrInvalidPlan
	}
	span := new(big.Int).Sub(big.NewInt(plan.HLS.Window.EndTicks), big.NewInt(plan.StartTicks))
	span.Mul(span, big.NewInt(int64(plan.FrameRate)))
	limit := new(big.Int).Mul(big.NewInt(maxGeneratedSourceRangeFrames), big.NewInt(ticksPerSecond))
	if span.Sign() <= 0 || span.Cmp(limit) > 0 {
		return ErrTimelineLimit
	}
	return nil
}

func generatedSourceRangeRational(value *big.Rat) (GeneratedRational, error) {
	if !value.Num().IsInt64() || !value.Denom().IsInt64() {
		return GeneratedRational{}, ErrTimelineLimit
	}
	return GeneratedRational{Num: value.Num().Int64(), Den: value.Denom().Int64()}, nil
}

func generatedSourceRangeOrigin(value string) (int64, bool) {
	if len(value) == 0 || len(value) > 32 {
		return 0, false
	}
	unsigned := strings.TrimPrefix(value, "-")
	whole, fraction, dot := strings.Cut(unsigned, ".")
	if len(whole) == 0 || len(whole) > 12 || dot && (len(fraction) == 0 || len(fraction) > 7) {
		return 0, false
	}
	for _, digits := range []string{whole, fraction} {
		for _, digit := range digits {
			if digit < '0' || digit > '9' {
				return 0, false
			}
		}
	}
	rational, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0, false
	}
	rational.Mul(rational, new(big.Rat).SetInt64(ticksPerSecond))
	if !rational.IsInt() || !rational.Num().IsInt64() {
		return 0, false
	}
	return rational.Num().Int64(), true
}

func parseGeneratedSourceRange(data []byte, plan Plan, formatOriginTicks int64) (GeneratedSourceRange, error) {
	var empty GeneratedSourceRange
	if err := generatedSourceRangePlan(plan, formatOriginTicks); err != nil {
		return empty, err
	}
	if len(data) == 0 {
		return empty, ErrTimelineProbe
	}
	if len(data) > maxGeneratedSourceRangeBytes {
		return empty, ErrTimelineLimit
	}
	if err := generatedUniqueJSON(data); err != nil {
		return empty, err
	}
	var document generatedSourceRangeDocument
	if err := generatedBoundsDecodeRecord(data, &document, "frames", "streams", "format", "programs", "stream_groups"); err != nil ||
		len(document.Streams) != 1 || len(document.Frames) == 0 || len(document.Programs) != 0 || len(document.StreamGroups) != 0 {
		return empty, ErrTimelineProbe
	}
	if len(document.Frames) > maxGeneratedSourceRangeFrames {
		return empty, ErrTimelineLimit
	}
	var stream generatedSourceRangeStream
	if err := generatedBoundsDecodeRecord(document.Streams[0], &stream, "index", "codec_type", "time_base"); err != nil ||
		stream.Index == nil || *stream.Index != int64(plan.VideoStreamIndex) || stream.Kind != "video" {
		return empty, ErrTimelineProbe
	}
	base, err := generatedBoundsTimeBase(stream.TimeBase)
	if err != nil {
		return empty, err
	}
	var format generatedSourceRangeFormat
	if err := generatedBoundsDecodeRecord(document.Format, &format, "start_time"); err != nil || format.StartTime == nil {
		return empty, ErrTimelineProbe
	}
	origin, valid := generatedSourceRangeOrigin(*format.StartTime)
	if !valid || origin != formatOriginTicks {
		return empty, ErrInvalidTimeline
	}
	start, end, epoch := generatedTicksSeconds(plan.StartTicks), generatedTicksSeconds(plan.HLS.Window.EndTicks), generatedTicksSeconds(origin)
	frameDuration := new(big.Rat).SetFrac64(1, int64(plan.FrameRate))
	var previousAll, first, last, observedEnd *big.Rat
	var count int64
	for _, raw := range document.Frames {
		var frame generatedSourceRangeFrame
		if err := decodeGeneratedSourceFrameProjection(raw, &frame); err != nil {
			return empty, err
		}
		if frame.Kind != "video" || frame.StreamIndex == nil || *frame.StreamIndex != int64(plan.VideoStreamIndex) ||
			frame.PTS == nil || frame.Duration == nil || *frame.Duration <= 0 {
			return empty, ErrTimelineProbe
		}
		pts := generatedClockSeconds(*frame.PTS, base.Num, base.Den)
		pts.Sub(pts, epoch)
		duration := generatedClockSeconds(*frame.Duration, base.Num, base.Den)
		frameEnd := new(big.Rat).Add(pts, duration)
		if generatedRatAbs(pts).Cmp(generatedTicksSeconds(maxDurationTicks)) > 0 ||
			generatedRatAbs(frameEnd).Cmp(generatedTicksSeconds(maxDurationTicks)) > 0 ||
			previousAll != nil && pts.Cmp(previousAll) <= 0 {
			return empty, ErrInvalidTimeline
		}
		previousAll = pts
		if pts.Cmp(start) < 0 {
			// A complete preroll frame may be ignored. A frame crossing the
			// requested start cannot establish an exact source-frame boundary.
			if frameEnd.Cmp(start) > 0 {
				return empty, ErrInvalidTimeline
			}
			continue
		}
		if pts.Cmp(end) >= 0 {
			continue
		}
		if duration.Cmp(frameDuration) != 0 || count == 0 && pts.Cmp(start) != 0 ||
			count != 0 && pts.Cmp(observedEnd) != 0 {
			return empty, ErrInvalidTimeline
		}
		if count == 0 {
			first = new(big.Rat).Set(pts)
		}
		last, observedEnd = new(big.Rat).Set(pts), frameEnd
		count++
	}
	if count == 0 || observedEnd.Cmp(end) < 0 {
		return empty, ErrInvalidTimeline
	}
	result := GeneratedSourceRange{StartTicks: plan.StartTicks, EndTicks: plan.HLS.Window.EndTicks, FrameCount: count}
	for _, conversion := range []struct {
		value *big.Rat
		field *GeneratedRational
	}{{first, &result.First}, {last, &result.Last}, {observedEnd, &result.End}, {frameDuration, &result.FrameDuration}} {
		*conversion.field, err = generatedSourceRangeRational(conversion.value)
		if err != nil {
			return empty, err
		}
	}
	return result, nil
}

// The two exec copiers share only this bounded failure state. The stdout
// buffer is owned by one copier and inspected only after Wait has joined it.
type generatedSourceRangeBudget struct {
	mu     sync.Mutex
	bytes  int
	err    error
	cancel context.CancelFunc
}

func (budget *generatedSourceRangeBudget) add(length int) error {
	budget.mu.Lock()
	if budget.err == nil && length > maxGeneratedSourceRangeBytes-budget.bytes {
		budget.err = ErrTimelineLimit
	}
	if budget.err == nil {
		budget.bytes += length
	}
	err := budget.err
	budget.mu.Unlock()
	if err != nil {
		budget.cancel()
	}
	return err
}

func (budget *generatedSourceRangeBudget) fail(err error) error {
	budget.mu.Lock()
	if budget.err == nil {
		budget.err = err
	}
	err = budget.err
	budget.mu.Unlock()
	budget.cancel()
	return err
}

func (budget *generatedSourceRangeBudget) failure() error {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.err
}

type generatedSourceRangeOutput struct {
	budget *generatedSourceRangeBudget
	buffer bytes.Buffer
	frames generatedSourceRangeFrameBudget
}

func (output *generatedSourceRangeOutput) Write(data []byte) (int, error) {
	if err := output.budget.add(len(data)); err != nil {
		return 0, err
	}
	if err := output.frames.write(data); err != nil {
		return 0, output.budget.fail(err)
	}
	return output.buffer.Write(data)
}

// This small structural counter stops decoded-frame output as soon as its
// budget is exhausted. Complete, duplicate-free JSON is validated separately;
// the counter supplies no clock or coverage facts. Escaped root keys are
// decoded so an equivalent spelling of "frames" cannot evade the budget.
type generatedSourceRangeFrameBudget struct {
	depth, framesDepth, count int
	inString, escaped         bool
	rootKey                   bool
	key                       [128]byte
	keyN                      int
	pending                   string
}

func (counter *generatedSourceRangeFrameBudget) write(data []byte) error {
	for _, char := range data {
		if counter.inString {
			if counter.rootKey {
				if counter.keyN == len(counter.key) {
					return ErrTimelineLimit
				}
				counter.key[counter.keyN] = char
				counter.keyN++
			}
			if counter.escaped {
				counter.escaped = false
			} else if char == '\\' {
				counter.escaped = true
			} else if char == '"' {
				counter.inString = false
				if counter.rootKey {
					if err := json.Unmarshal(counter.key[:counter.keyN], &counter.pending); err != nil {
						return ErrTimelineProbe
					}
				}
			}
			continue
		}
		switch char {
		case '"':
			counter.inString, counter.escaped = true, false
			counter.rootKey = counter.depth == 1
			if counter.rootKey {
				counter.key[0], counter.keyN = char, 1
			}
		case '{', '[':
			if char == '{' && counter.depth == counter.framesDepth && counter.framesDepth != 0 {
				counter.count++
				if counter.count > maxGeneratedSourceRangeFrames {
					return ErrTimelineLimit
				}
			}
			if counter.depth == 1 {
				if char == '[' && counter.pending == "frames" {
					counter.framesDepth = 2
				}
				counter.pending = ""
			}
			counter.depth++
			if counter.depth > 8 {
				return ErrTimelineLimit
			}
		case '}', ']':
			if counter.depth == 0 {
				return ErrTimelineProbe
			}
			if counter.depth == counter.framesDepth {
				counter.framesDepth = 0
			}
			counter.depth--
		}
	}
	return nil
}

type generatedSourceRangeDiagnostics struct{ budget *generatedSourceRangeBudget }

func (diagnostics *generatedSourceRangeDiagnostics) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	return 0, diagnostics.budget.fail(ErrTimelineProbe)
}
