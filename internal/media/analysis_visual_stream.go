package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const maxAnalysisVisualLogLine = 16 * 1024

var (
	analysisShowInfoConfig = regexp.MustCompile(`config in time_base: ([0-9]+/[0-9]+), frame_rate: [0-9]+/[0-9]+(?:\s|$)`)
	analysisShowInfoFrame  = regexp.MustCompile(`(?:^|\s)n:\s*([0-9]+) pts:\s*(\S+) pts_time:\S+.*?fmt:(\S+) .*?sar:([0-9]+)/([0-9]+) s:([0-9]+)x([0-9]+) i:\S+ iskey:\S+ type:\S+`)
	analysisShowInfoStart  = regexp.MustCompile(`(?:^|\s)n:`)
	analysisFilterRawPTS   = regexp.MustCompile(`filter_raw -> pts:(\S+) pts_time:\S+ time_base:([0-9]+/[0-9]+)(?:\s|$)`)
)

type analysisVisualPlan struct {
	start, end, interval int64
	frames               int
	width, height        int
	pixelFormat          string
	channels             int
	geometry             analysisDisplayGeometry
}

type analysisVisualFrame struct {
	nominal int64
	actual  int64
	ptsKey  string
}

type analysisShowInfoState struct {
	timeBase *big.Rat
	count    int
	lastPTS  int64
	lastTick int64
}

// analysisVisualLog is a bounded streaming stderr sink. Its small timestamp
// queues can cover the whole admitted output plan, while image bytes remain on
// stdout. A closed sink wakes a parser waiting for a missing frame record.
type analysisVisualLog struct {
	mu                           sync.Mutex
	plan                         analysisVisualPlan
	limits                       AnalysisLimits
	origin                       int64
	stream                       Stream
	sarNumerator, sarDenominator int64
	sourceSARUnknown             bool
	packets                      *analysisPacketPTS
	source                       analysisShowInfoState
	sample                       analysisShowInfoState
	selected                     []analysisVisualFrame
	head                         int
	frames                       chan analysisVisualFrame
	done                         chan struct{}
	lineBuf                      []byte
	bytes                        int64
	err                          error
	closed                       bool
}

func newAnalysisVisualLog(info Info, stream Stream, plan analysisVisualPlan, limits AnalysisLimits) (*analysisVisualLog, error) {
	packets, err := newAnalysisPacketPTS(stream, limits.MaxSourceFrames*4)
	if err != nil {
		return nil, err
	}
	geometry := plan.geometry
	if geometry.width == 0 {
		geometry = analysisSquareGeometry(stream)
	}
	stream.Width, stream.Height = geometry.width, geometry.height
	return &analysisVisualLog{plan: plan, limits: limits, origin: info.FormatStartTicks,
		stream: stream, sarNumerator: geometry.sarNumerator, sarDenominator: geometry.sarDenominator,
		sourceSARUnknown: geometry.sourceSARUnknown,
		packets:          packets, frames: make(chan analysisVisualFrame, plan.frames),
		done: make(chan struct{})}, nil
}

func (log *analysisVisualLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.closed || log.err != nil {
		if log.err != nil {
			return 0, log.err
		}
		return 0, io.ErrClosedPipe
	}
	if int64(len(data)) > log.limits.MaxStderrBytes-log.bytes {
		log.err = fmt.Errorf("%w: visual diagnostic bytes", ErrAnalysisBudget)
		return 0, log.err
	}
	log.bytes += int64(len(data))
	consumed := 0
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		complete := end >= 0
		if !complete {
			end = len(data)
		}
		if len(log.lineBuf)+end > maxAnalysisVisualLogLine {
			log.err = fmt.Errorf("%w: visual diagnostic line", ErrAnalysisBudget)
			return consumed, log.err
		}
		log.lineBuf = append(log.lineBuf, data[:end]...)
		consumed += end
		data = data[end:]
		if !complete {
			break
		}
		consumed++
		data = data[1:]
		if err := log.line(string(log.lineBuf)); err != nil {
			log.err = err
			return consumed, err
		}
		log.lineBuf = log.lineBuf[:0]
	}
	return consumed, nil
}

func (log *analysisVisualLog) Close(processErr error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.closed {
		return
	}
	log.closed = true
	if log.err == nil {
		log.err = processErr
	}
	if log.err == nil && len(log.lineBuf) != 0 {
		log.err = fmt.Errorf("%w: truncated visual diagnostics", ErrAnalysisUnproven)
	}
	if log.err == nil && (log.packets.packets == 0 || log.source.count == 0 ||
		log.sample.count != log.plan.frames || log.head != len(log.selected)) {
		log.err = fmt.Errorf("%w: incomplete visual sample plan", ErrAnalysisUnproven)
	}
	close(log.done)
}

func (log *analysisVisualLog) next(ctx context.Context) (analysisVisualFrame, error) {
	select {
	case frame := <-log.frames:
		return frame, nil
	case <-ctx.Done():
		return analysisVisualFrame{}, ctx.Err()
	case <-log.done:
		select {
		case frame := <-log.frames:
			return frame, nil
		default:
		}
		log.mu.Lock()
		defer log.mu.Unlock()
		if log.err != nil {
			return analysisVisualFrame{}, log.err
		}
		return analysisVisualFrame{}, io.EOF
	}
}

func (log *analysisVisualLog) result() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return log.err
	}
	if !log.closed || len(log.frames) != 0 {
		return fmt.Errorf("%w: unmatched visual metadata", ErrAnalysisUnproven)
	}
	return nil
}

func (log *analysisVisualLog) line(line string) error {
	if strings.Contains(line, "[error]") || strings.Contains(line, "[fatal]") || strings.Contains(line, "[panic]") {
		return fmt.Errorf("%w: decoder reported an error", ErrAnalysisUnproven)
	}
	if matched, err := log.packets.line(line); matched {
		if err != nil {
			return fmt.Errorf("%w: %w", ErrAnalysisUnproven, err)
		}
	}
	// showinfo writes its complete frame body in one av_log call, but its
	// newline and color properties in later calls. Pipeline threads can insert
	// a complete packet/filter record between them and suppress its prefix.
	// Parse all atomic record bodies in each physical line, never just one.
	if strings.Contains(line, "config in time_base:") {
		fields := analysisShowInfoConfig.FindStringSubmatch(line)
		if fields == nil || log.source.timeBase != nil {
			return fmt.Errorf("%w: visual filter was reconfigured", ErrAnalysisUnproven)
		}
		base, err := analysisTimeBase(fields[1])
		if err != nil {
			return fmt.Errorf("%w: %v", ErrAnalysisUnproven, err)
		}
		log.source.timeBase = base
	}
	if analysisShowInfoStart.MatchString(line) {
		if err := log.sourceFrame(analysisShowInfoFrame.FindStringSubmatch(line)); err != nil {
			return err
		}
	}
	if strings.Contains(line, "filter_raw ->") {
		fields := analysisFilterRawPTS.FindStringSubmatch(line)
		if fields == nil {
			return fmt.Errorf("%w: malformed output filter PTS", ErrAnalysisUnproven)
		}
		pts, ptsErr := analysisPTSInteger(fields[1])
		base, baseErr := analysisTimeBase(fields[2])
		if ptsErr != nil || baseErr != nil || log.sample.timeBase != nil && base.Cmp(log.sample.timeBase) != 0 {
			return fmt.Errorf("%w: missing or changed output filter time base", ErrAnalysisUnproven)
		}
		log.sample.timeBase = base
		return log.outputFrame(pts)
	}
	return nil
}

func (log *analysisVisualLog) sourceFrame(fields []string) error {
	state := &log.source
	if fields == nil || state.timeBase == nil {
		return fmt.Errorf("%w: unknown visual frame metadata", ErrAnalysisUnproven)
	}
	number, numberErr := strconv.Atoi(fields[1])
	pts, ptsErr := analysisPTSInteger(fields[2])
	sarNum, sarNumErr := strconv.ParseInt(fields[4], 10, 32)
	sarDen, sarDenErr := strconv.ParseInt(fields[5], 10, 32)
	width, widthErr := strconv.Atoi(fields[6])
	height, heightErr := strconv.Atoi(fields[7])
	if numberErr != nil || ptsErr != nil || widthErr != nil || heightErr != nil ||
		number != state.count || width <= 0 || height <= 0 ||
		state.count > 0 && pts <= state.lastPTS {
		return fmt.Errorf("%w: missing, duplicate, or unordered visual PTS", ErrAnalysisUnproven)
	}
	matchedSAR := sarNumErr == nil && sarDenErr == nil && sarNum > 0 && sarDen > 0 &&
		sarNum*log.sarDenominator == sarDen*log.sarNumerator
	if log.sourceSARUnknown {
		// Unknown source proof must remain canonical 0/1 on every decoded frame.
		// Do not replace the raw fields: preview trace comparison also uses them.
		matchedSAR = fields[4] == "0" && fields[5] == "1"
	}
	if !matchedSAR {
		return fmt.Errorf("%w: source display pixel ratio differs from its geometry proof", ErrAnalysisUnproven)
	}
	ticks, err := analysisVisualTicks(pts, state.timeBase, log.origin)
	if err != nil || state.count > 0 && ticks <= state.lastTick {
		return fmt.Errorf("%w: visual PTS is outside the tick grid", ErrAnalysisUnproven)
	}
	state.count++
	state.lastPTS, state.lastTick = pts, ticks
	if width != log.stream.Width || height != log.stream.Height {
		return fmt.Errorf("%w: source dimensions changed after display transforms", ErrAnalysisUnproven)
	}
	if int64(width)*int64(height) > log.limits.MaxSourcePixels || state.count > log.limits.MaxSourceFrames {
		return fmt.Errorf("%w: decoded source frames or pixels", ErrAnalysisBudget)
	}
	if err := log.packets.match(pts, state.timeBase); err != nil {
		return fmt.Errorf("%w: %v", ErrAnalysisUnproven, err)
	}
	if ticks >= log.plan.end || len(log.selected) == log.plan.frames {
		return nil
	}
	nominal := log.plan.start + int64(len(log.selected))*log.plan.interval
	if ticks < nominal {
		return nil
	}
	if ticks >= min(nominal+log.plan.interval, log.plan.end) {
		return fmt.Errorf("%w: source PTS skipped a nominal sample slot", ErrAnalysisUnproven)
	}
	log.selected = append(log.selected, analysisVisualFrame{nominal: nominal, actual: ticks, ptsKey: analysisPTSKey(pts, state.timeBase)})
	return nil
}

func (log *analysisVisualLog) outputFrame(pts int64) error {
	state := &log.sample
	ticks, err := analysisVisualTicks(pts, state.timeBase, log.origin)
	if err != nil || state.count > 0 && (pts <= state.lastPTS || ticks <= state.lastTick) {
		return fmt.Errorf("%w: unordered output filter PTS", ErrAnalysisUnproven)
	}
	state.count++
	state.lastPTS, state.lastTick = pts, ticks
	if state.count > log.plan.frames {
		return fmt.Errorf("%w: excess output filter frames", ErrAnalysisBudget)
	}
	if log.head >= len(log.selected) || log.selected[log.head].actual != ticks || log.selected[log.head].ptsKey != analysisPTSKey(pts, state.timeBase) {
		return fmt.Errorf("%w: output PTS has no selected source frame", ErrAnalysisUnproven)
	}
	frame := log.selected[log.head]
	log.head++
	select {
	case log.frames <- frame:
		return nil
	default:
		return fmt.Errorf("%w: pending visual frame metadata", ErrAnalysisBudget)
	}
}

func readAnalysisVisualFrames(ctx context.Context, input io.Reader, log *analysisVisualLog, emit func(analysisVisualFrame, []byte) error) error {
	size := log.plan.width * log.plan.height * log.plan.channels
	frame := make([]byte, size)
	for index := 0; ; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(input, frame)
		if err == io.EOF && n == 0 {
			if index != log.plan.frames {
				return fmt.Errorf("%w: incomplete raw visual frame stream", ErrAnalysisUnproven)
			}
			return nil
		}
		if err != nil || index >= log.plan.frames {
			return fmt.Errorf("%w: truncated or excess raw visual frame", ErrAnalysisUnproven)
		}
		metadata, err := log.next(ctx)
		if err != nil {
			return err
		}
		if err := emit(metadata, frame); err != nil {
			return err
		}
	}
}
