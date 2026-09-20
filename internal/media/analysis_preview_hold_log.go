package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
	"math/big"
	"strings"
	"sync"
)

type analysisPreviewHoldLog struct {
	mu               sync.Mutex
	plan             analysisVisualPlan
	budget           *analysisPreviewHoldBudget
	audit            *analysisVisualLog
	planner          analysisPreviewHoldPlanner
	proof            *analysisPreviewHoldProof
	trace            hash.Hash
	firstKey         string
	matched, outputs int
	frames           chan analysisPreviewHoldPoint
	done             chan struct{}
	lineBuf          []byte
	err              error
	closed           bool
}

func newAnalysisPreviewHoldLog(info Info, stream Stream, plan analysisVisualPlan, limits AnalysisLimits, budget *analysisPreviewHoldBudget, proof *analysisPreviewHoldProof) (*analysisPreviewHoldLog, error) {
	// Reuse the existing source-only PTS, packet, raster and SAR audit. Setting
	// its sample count to zero disables its intro-specific slot policy; preview
	// output records and nominal slots are handled only by this separate sink.
	auditPlan := plan
	auditPlan.frames = 0
	audit, err := newAnalysisVisualLog(info, stream, auditPlan, limits)
	if err != nil {
		return nil, err
	}
	capacity := 0
	if proof != nil {
		capacity = len(proof.points)
		if capacity < 1 || capacity > plan.frames || proof.sourceFrames < capacity {
			return nil, ErrAnalysisUnproven
		}
	}
	return &analysisPreviewHoldLog{plan: plan, budget: budget, audit: audit, proof: proof, trace: sha256.New(),
		planner: analysisPreviewHoldPlanner{plan: plan, origin: info.FormatStartTicks},
		frames:  make(chan analysisPreviewHoldPoint, capacity), done: make(chan struct{})}, nil
}

func (log *analysisPreviewHoldLog) Write(data []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return 0, log.err
	}
	if log.closed {
		return 0, io.ErrClosedPipe
	}
	if err := log.budget.addBytes(len(data)); err != nil {
		log.err = err
		return 0, err
	}
	consumed := 0
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		complete := end >= 0
		if !complete {
			end = len(data)
		}
		if len(log.lineBuf)+end > maxAnalysisVisualLogLine {
			log.err = fmt.Errorf("%w: preview diagnostic line", ErrAnalysisBudget)
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

func (log *analysisPreviewHoldLog) line(line string) error {
	if strings.Contains(line, "[error]") || strings.Contains(line, "[fatal]") || strings.Contains(line, "[panic]") {
		return fmt.Errorf("%w: preview decoder reported an error", ErrAnalysisUnproven)
	}
	beforePackets := log.audit.packets.packets
	if _, err := log.audit.packets.line(line); err != nil {
		return fmt.Errorf("%w: %w", ErrAnalysisUnproven, err)
	}
	if err := log.budget.addRecords(0, log.audit.packets.packets-beforePackets); err != nil {
		return err
	}
	// Each complete body is atomic in the pinned FFmpeg source, while log
	// prefixes and physical newlines can be shared with other pipeline threads.
	if strings.Contains(line, "config in time_base:") {
		fields := analysisShowInfoConfig.FindStringSubmatch(line)
		if fields == nil || log.audit.source.timeBase != nil {
			return fmt.Errorf("%w: preview source filter was reconfigured", ErrAnalysisUnproven)
		}
		base, err := analysisTimeBase(fields[1])
		if err != nil {
			return fmt.Errorf("%w: invalid preview source time base", ErrAnalysisUnproven)
		}
		log.audit.source.timeBase = base
	}
	if analysisShowInfoStart.MatchString(line) {
		fields := analysisShowInfoFrame.FindStringSubmatch(line)
		if err := log.audit.sourceFrame(fields); err != nil {
			return err
		}
		if err := log.budget.addRecords(1, 0); err != nil {
			return err
		}
		if err := log.source(fields); err != nil {
			return err
		}
	}
	if strings.Contains(line, "filter_raw ->") {
		fields := analysisFilterRawPTS.FindStringSubmatch(line)
		if fields == nil {
			return fmt.Errorf("%w: malformed preview output timestamp", ErrAnalysisUnproven)
		}
		pts, ptsErr := analysisPTSInteger(fields[1])
		base, baseErr := analysisTimeBase(fields[2])
		if ptsErr != nil || baseErr != nil {
			return fmt.Errorf("%w: unknown preview output timestamp", ErrAnalysisUnproven)
		}
		return log.output(analysisPTSKey(pts, base))
	}
	return nil
}

func (log *analysisPreviewHoldLog) source(fields []string) error {
	state := log.audit.source
	if state.lastTick >= log.plan.end {
		return fmt.Errorf("%w: preview picture is outside the admitted item duration", ErrAnalysisUnproven)
	}
	point := analysisPreviewHoldPoint{ordinal: state.count - 1, actual: state.lastTick, key: analysisPTSKey(state.lastPTS, state.timeBase)}
	if point.ordinal == 0 {
		log.firstKey = point.key
	}
	// This is a complete source-metadata trace, not a decoded-image checksum.
	// Include frames after the last selected picture so a truncated or changed
	// second pass cannot inherit the first pass's successful EOF evidence.
	fmt.Fprintf(log.trace, "%d\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\n",
		point.ordinal, state.timeBase.RatString(), state.lastPTS, point.key,
		fields[3], fields[4], fields[5], fields[6], fields[7])
	if log.proof == nil {
		timestamp := new(big.Rat).Mul(new(big.Rat).SetInt64(state.lastPTS), state.timeBase)
		log.planner.source(point, timestamp)
		return nil
	}
	if log.matched < len(log.proof.points) && point.ordinal == log.proof.points[log.matched].ordinal {
		want := log.proof.points[log.matched]
		if point.key != want.key || point.actual != want.actual {
			return fmt.Errorf("%w: selected preview source timestamp changed between passes", ErrAnalysisUnproven)
		}
		log.matched++
	}
	return nil
}

func (log *analysisPreviewHoldLog) output(key string) error {
	if log.proof == nil {
		if log.outputs != 0 || log.firstKey == "" || key != log.firstKey {
			return fmt.Errorf("%w: first preview pass did not preserve its first picture", ErrAnalysisUnproven)
		}
		log.outputs++
		return nil
	}
	if log.outputs >= len(log.proof.points) || log.outputs >= log.matched {
		return fmt.Errorf("%w: unexpected selected preview output", ErrAnalysisUnproven)
	}
	point := log.proof.points[log.outputs]
	if key != point.key {
		return fmt.Errorf("%w: preview output PTS does not match its selected source ordinal", ErrAnalysisUnproven)
	}
	log.outputs++
	select {
	case log.frames <- point:
		return nil
	default:
		return fmt.Errorf("%w: pending preview output metadata", ErrAnalysisBudget)
	}
}

func (log *analysisPreviewHoldLog) Close(processErr error) {
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
		log.err = fmt.Errorf("%w: truncated preview diagnostics", ErrAnalysisUnproven)
	}
	if log.err == nil && (log.audit.source.count == 0 || log.audit.packets.packets == 0) {
		log.err = fmt.Errorf("%w: missing preview source evidence", ErrAnalysisUnproven)
	}
	if log.err == nil && log.proof == nil && log.outputs != 1 {
		log.err = fmt.Errorf("%w: incomplete first preview pass", ErrAnalysisUnproven)
	}
	if log.err == nil && log.proof != nil {
		if log.outputs != len(log.proof.points) || log.matched != len(log.proof.points) ||
			log.audit.source.count != log.proof.sourceFrames || log.audit.packets.packets != log.proof.packets ||
			!bytes.Equal(log.trace.Sum(nil), log.proof.trace[:]) {
			log.err = fmt.Errorf("%w: preview second full-source trace differs", ErrAnalysisUnproven)
		}
	}
	close(log.done)
}

// finishPlan is private to the successful, source-checked command path above.
// Closing stderr alone is deliberately insufficient to call it in production.
func (log *analysisPreviewHoldLog) finishPlan() (analysisPreviewHoldProof, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	if !log.closed || log.proof != nil {
		return analysisPreviewHoldProof{}, ErrAnalysisUnproven
	}
	if log.err != nil {
		return analysisPreviewHoldProof{}, log.err
	}
	if err := log.planner.eof(); err != nil {
		return analysisPreviewHoldProof{}, err
	}
	proof := analysisPreviewHoldProof{points: append([]analysisPreviewHoldPoint(nil), log.planner.points...),
		sourceFrames: log.audit.source.count, packets: log.audit.packets.packets}
	copy(proof.trace[:], log.trace.Sum(nil))
	return proof, nil
}

func (log *analysisPreviewHoldLog) result() error {
	log.mu.Lock()
	defer log.mu.Unlock()
	if log.err != nil {
		return log.err
	}
	if !log.closed || len(log.frames) != 0 {
		return fmt.Errorf("%w: unconsumed preview metadata", ErrAnalysisUnproven)
	}
	return nil
}

func (log *analysisPreviewHoldLog) next(ctx context.Context) (analysisPreviewHoldPoint, error) {
	select {
	case point := <-log.frames:
		return point, nil
	case <-ctx.Done():
		return analysisPreviewHoldPoint{}, ctx.Err()
	case <-log.done:
		select {
		case point := <-log.frames:
			return point, nil
		default:
		}
		log.mu.Lock()
		defer log.mu.Unlock()
		if log.err != nil {
			return analysisPreviewHoldPoint{}, log.err
		}
		return analysisPreviewHoldPoint{}, io.EOF
	}
}
