package media

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/creditsskipper"
)

type creditsVisualLog struct {
	mu     sync.Mutex
	limit  int64
	buffer bytes.Buffer
	closed bool
	err    error
}

func (sink *creditsVisualLog) Write(data []byte) (int, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.err != nil {
		return 0, sink.err
	}
	if sink.closed {
		return 0, io.ErrClosedPipe
	}
	if int64(len(data)) > sink.limit-int64(sink.buffer.Len()) {
		sink.err = ErrAnalysisBudget
		return 0, sink.err
	}
	return sink.buffer.Write(data)
}

func (sink *creditsVisualLog) Close(err error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.closed {
		return
	}
	sink.closed = true
	if sink.err == nil {
		sink.err = err
	}
}

func (sink *creditsVisualLog) result() (string, error) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.err != nil {
		return "", sink.err
	}
	if !sink.closed {
		return "", fmt.Errorf("%w: unfinished credits diagnostics", ErrAnalysisUnproven)
	}
	return sink.buffer.String(), nil
}

// These grammars retain the pinned FFmpegOutputParser's successful values and
// order. Bounded finite numbers, lines, and records are Goby execution limits.
var (
	// FFmpeg's output-header logger can leave a partial line while another
	// output graph emits blackframe. av_log then omits that filter's prefix,
	// despite retaining its complete frame body. Bind to every blackframe
	// field rather than requiring a prefix that is not part of the evidence.
	creditsBlackFramePattern    = regexp.MustCompile(`(?:^|\s)frame:(\d+) pblack:(\d+) pts:\S+ t:([\d.]+) type:\S+ last_keyframe:-?\d+(?:\s|$)`)
	creditsBlackIntervalPattern = regexp.MustCompile(`black_start:([-+]?(?:\d+(?:\.\d*)?|\.\d+))\s+black_end:([-+]?(?:\d+(?:\.\d*)?|\.\d+))\s+black_duration:([-+]?(?:\d+(?:\.\d*)?|\.\d+))`)
	creditsVisualTimePattern    = regexp.MustCompile(`pts_time:(-?[0-9]+(?:\.[0-9]+)?(?:[eE][-+]?[0-9]+)?)`)
	creditsEntropyPattern       = regexp.MustCompile(`lavfi\.entropy\.normalized_entropy\.normal\.Y=(-?[0-9]+(?:\.[0-9]+)?(?:[eE][-+]?[0-9]+)?)`)
	creditsSaturationPattern    = regexp.MustCompile(`lavfi\.signalstats\.SATAVG=(-?[0-9]+(?:\.[0-9]+)?(?:[eE][-+]?[0-9]+)?)`)
	creditsSilencePattern       = regexp.MustCompile(`silence_(start|end): ([0-9.]+)`)
)

func creditsParseNumber(text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%w: invalid credits diagnostic number", ErrAnalysisUnproven)
	}
	return value, nil
}

func creditsLogLines(raw string, parse func(string) error) error {
	if len(raw) > 64<<20 {
		return ErrAnalysisBudget
	}
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		if err := parse(scanner.Text()); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: credits diagnostic line", ErrAnalysisBudget)
	}
	return nil
}

func parseCreditsBlackFrames(raw string) ([]creditsskipper.BlackFrame, error) {
	var frames []creditsskipper.BlackFrame
	err := creditsLogLines(raw, func(line string) error {
		match := creditsBlackFramePattern.FindStringSubmatch(line)
		if match == nil {
			return nil
		}
		frame, err1 := strconv.Atoi(match[1])
		percentage, err2 := strconv.Atoi(match[2])
		timestamp, err3 := creditsParseNumber(match[3])
		if err1 != nil || err2 != nil || err3 != nil || frame < 0 || percentage < 0 || percentage > 100 {
			return ErrAnalysisUnproven
		}
		if len(frames) >= creditsVisualMaximumRecords {
			return ErrAnalysisBudget
		}
		frames = append(frames, creditsskipper.BlackFrame{Frame: frame, Percentage: percentage, Time: timestamp})
		return nil
	})
	return frames, err
}

func parseCreditsBlackIntervals(raw string) ([]creditsskipper.Range, error) {
	var ranges []creditsskipper.Range
	err := creditsLogLines(raw, func(line string) error {
		match := creditsBlackIntervalPattern.FindStringSubmatch(line)
		if match == nil {
			return nil
		}
		start, err1 := creditsParseNumber(match[1])
		end, err2 := creditsParseNumber(match[2])
		duration, err3 := creditsParseNumber(match[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return ErrAnalysisUnproven
		}
		if end <= start || duration <= 0 {
			return nil
		}
		if len(ranges) >= creditsVisualMaximumRecords {
			return ErrAnalysisBudget
		}
		ranges = append(ranges, creditsskipper.Range{Start: start, End: end})
		return nil
	})
	return ranges, err
}

func parseCreditsKeyframeVisuals(raw string, duration float64) ([]creditsskipper.KeyframeVisual, error) {
	var visuals []creditsskipper.KeyframeVisual
	var timestamp, entropy, saturation float64
	var hasTime, hasEntropy, hasSaturation bool
	flush := func() error {
		if !hasTime || !hasEntropy || !hasSaturation || timestamp < 0 || timestamp > duration {
			return nil
		}
		if len(visuals) >= creditsVisualMaximumRecords {
			return ErrAnalysisBudget
		}
		visuals = append(visuals, creditsskipper.KeyframeVisual{Time: timestamp, Entropy: entropy, Saturation: saturation})
		return nil
	}
	err := creditsLogLines(raw, func(line string) error {
		if match := creditsVisualTimePattern.FindStringSubmatch(line); match != nil {
			if err := flush(); err != nil {
				return err
			}
			value, err := creditsParseNumber(match[1])
			if err != nil {
				return err
			}
			timestamp, entropy, saturation = value, 0, 0
			hasTime, hasEntropy, hasSaturation = true, false, false
			return nil
		}
		if match := creditsEntropyPattern.FindStringSubmatch(line); match != nil {
			value, err := creditsParseNumber(match[1])
			if err != nil {
				return err
			}
			entropy, hasEntropy = value, true
			return nil
		}
		if match := creditsSaturationPattern.FindStringSubmatch(line); match != nil {
			value, err := creditsParseNumber(match[1])
			if err != nil {
				return err
			}
			saturation, hasSaturation = value, true
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	return visuals, err
}

func parseCreditsSilence(raw string, start float64) ([]creditsskipper.Range, error) {
	var ranges []creditsskipper.Range
	var current creditsskipper.Range
	err := creditsLogLines(raw, func(line string) error {
		for _, match := range creditsSilencePattern.FindAllStringSubmatch(line, -1) {
			value, err := creditsParseNumber(match[2])
			if err != nil {
				return err
			}
			if match[1] == "start" {
				current.Start = value + start
				continue
			}
			current.End = value + start
			if len(ranges) >= creditsVisualMaximumRecords {
				return ErrAnalysisBudget
			}
			ranges = append(ranges, current)
		}
		return nil
	})
	return ranges, err
}

func parseCreditsBoundaryKeyframes(raw string, start float64) ([]float64, error) {
	var values []float64
	err := creditsLogLines(raw, func(line string) error {
		position := strings.Index(strings.ToLower(line), "pts_time:")
		if position < 0 {
			return nil
		}
		text := strings.SplitN(line[position+9:], " ", 2)[0]
		value, err := creditsParseNumber(text)
		if err != nil {
			return nil
		}
		if len(values) >= creditsVisualMaximumRecords {
			return ErrAnalysisBudget
		}
		values = append(values, value+start)
		return nil
	})
	return values, err
}
