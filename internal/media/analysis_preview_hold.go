package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
)

const maxAnalysisPreviewFilterBytes = 120 << 10

// Final source I/O and the shared deadline decision remain inside the admitted
// operation. Release cancels that context, so it must happen after both checks.
func finalizeAnalysisPreview(ctx context.Context, checkSource func() error, release func()) error {
	if release != nil {
		defer release()
	}
	sourceErr := checkSource()
	return errors.Join(sourceErr, ctx.Err())
}

type analysisPreviewHoldPoint struct {
	ordinal              int
	actual               int64
	key                  string
	firstSlot, slotCount int
}

type analysisPreviewHoldProof struct {
	points                []analysisPreviewHoldPoint
	sourceFrames, packets int
	trace                 [sha256.Size]byte
}

// Both full decodes share these operation budgets. No complete diagnostic
// stream or collection of image rasters is retained.
type analysisPreviewHoldBudget struct {
	mu              sync.Mutex
	limits          AnalysisLimits
	bytes           int64
	frames, packets int
}

func (budget *analysisPreviewHoldBudget) addBytes(count int) error {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if int64(count) > budget.limits.MaxStderrBytes-budget.bytes {
		return fmt.Errorf("%w: preview operation diagnostics", ErrAnalysisBudget)
	}
	budget.bytes += int64(count)
	return nil
}

func (budget *analysisPreviewHoldBudget) addRecords(frames, packets int) error {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if frames > budget.limits.MaxSourceFrames-budget.frames || packets > budget.limits.MaxSourceFrames*4-budget.packets {
		return fmt.Errorf("%w: preview operation source records", ErrAnalysisBudget)
	}
	budget.frames += frames
	budget.packets += packets
	return nil
}

func (budget *analysisPreviewHoldBudget) admitRepeat(frames, packets int) error {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if frames > budget.limits.MaxSourceFrames-budget.frames || packets > budget.limits.MaxSourceFrames*4-budget.packets {
		return fmt.Errorf("%w: preview second full decode", ErrAnalysisBudget)
	}
	return nil
}

type analysisPreviewHoldPlanner struct {
	plan     analysisVisualPlan
	origin   int64
	previous *analysisPreviewHoldPoint
	points   []analysisPreviewHoldPoint
	nextSlot int
}

func (planner *analysisPreviewHoldPlanner) source(point analysisPreviewHoldPoint, timestamp *big.Rat) {
	if planner.previous != nil {
		for planner.nextSlot < planner.plan.frames {
			nominal := new(big.Int).Add(big.NewInt(planner.origin), big.NewInt(int64(planner.nextSlot)*planner.plan.interval))
			requested := new(big.Rat).SetFrac(nominal, big.NewInt(TicksPerSecond))
			if requested.Cmp(timestamp) >= 0 {
				break
			}
			planner.add(*planner.previous)
		}
	}
	copyPoint := point
	planner.previous = &copyPoint
}

func (planner *analysisPreviewHoldPlanner) add(point analysisPreviewHoldPoint) {
	if count := len(planner.points); count > 0 && planner.points[count-1].ordinal == point.ordinal {
		planner.points[count-1].slotCount++
	} else {
		point.firstSlot, point.slotCount = planner.nextSlot, 1
		planner.points = append(planner.points, point)
	}
	planner.nextSlot++
}

// eof is invoked only after the first command has been killed/reaped or has
// exited successfully, and only the successful, fully audited path reaches it.
func (planner *analysisPreviewHoldPlanner) eof() error {
	if planner.previous == nil {
		return fmt.Errorf("%w: preview source has no pictures", ErrAnalysisUnproven)
	}
	for planner.nextSlot < planner.plan.frames {
		planner.add(*planner.previous)
	}
	return nil
}

// runAnalysisPreviewHoldPlan does not expose an EOF-derived plan until actual
// process success, complete diagnostics, descriptor identity and tool identity
// have all been checked. A canceled or truncated pass never authorizes tail hold.
func runAnalysisPreviewHoldPlan(ctx context.Context, tool *analysisTool, file *os.File, before os.FileInfo, info Info, stream Stream,
	plan analysisVisualPlan, limits AnalysisLimits, budget *analysisPreviewHoldBudget) (analysisPreviewHoldProof, error) {
	log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, nil)
	if err != nil {
		return analysisPreviewHoldProof{}, err
	}
	args, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, nil)
	if err != nil {
		return analysisPreviewHoldProof{}, err
	}
	processErr := runAnalysisStream(ctx, "/proc/self/fd/4", file, args, limits.Timeout, 1, log, func(reader io.Reader) error {
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if len(data) != 0 {
			return fmt.Errorf("%w: null preview pass wrote image bytes", ErrAnalysisUnproven)
		}
		return nil
	}, tool.file)
	if processErr != nil {
		return analysisPreviewHoldProof{}, processErr
	}
	if err := ctx.Err(); err != nil {
		return analysisPreviewHoldProof{}, err
	}
	if err := videoSeekCheckSource(file, before); err != nil {
		return analysisPreviewHoldProof{}, err
	}
	if err := tool.check(); err != nil {
		return analysisPreviewHoldProof{}, err
	}
	proof, err := log.finishPlan()
	if err != nil {
		return analysisPreviewHoldProof{}, err
	}
	if err := budget.admitRepeat(proof.sourceFrames, proof.packets); err != nil {
		return analysisPreviewHoldProof{}, err
	}
	return proof, nil
}

func buildAnalysisPreviewHoldArgs(stream Stream, plan analysisVisualPlan, limits AnalysisLimits, proof *analysisPreviewHoldProof) ([]string, error) {
	expression := "eq(n,0)"
	if proof != nil {
		if len(proof.points) == 0 || len(proof.points) > plan.frames {
			return nil, ErrAnalysisUnproven
		}
		var selection strings.Builder
		previous := -1
		for index, point := range proof.points {
			if point.ordinal <= previous || point.ordinal >= limits.MaxSourceFrames {
				return nil, ErrAnalysisUnproven
			}
			if index > 0 {
				selection.WriteByte('+')
			}
			fmt.Fprintf(&selection, "eq(n,%d)", point.ordinal)
			previous = point.ordinal
		}
		expression = selection.String()
	}
	// FFmpeg n9.0.1 f_select.c defines n as frame_count_out - 1 and forwards
	// the selected AVFrame unchanged. No fps, setpts, seek, duration cutoff or
	// output frame cap can stop either pass before the complete source EOF.
	filter := plan.geometry.filter() + "sidedata=mode=delete,showinfo@analysis_source=checksum=0,select='" + expression + "'"
	if proof != nil {
		filter += ",scale=" + strconv.Itoa(plan.width) + ":" + strconv.Itoa(plan.height) + ":flags=area,setsar=1,format=rgb24"
	}
	if len(filter) >= maxAnalysisPreviewFilterBytes {
		return nil, fmt.Errorf("%w: preview selection filter argument", ErrAnalysisBudget)
	}
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "repeat+level+info", "-debug_ts", "-xerror",
		"-max_alloc", "268435456", "-copyts", "-fflags", "+nofillin-genpts", "-threads", "1", "-max_pixels", strconv.FormatInt(limits.MaxSourcePixels, 10),
		"-filter_threads", "1", "-filter_complex_threads", "1", "-noautorotate", "-reinit_filter", "0",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mpegts,avi", "-i", "/proc/self/fd/3",
		"-map", "0:" + strconv.Itoa(stream.Index), "-an", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
		"-vf", filter, "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter", "-threads:v", "1"}
	if proof == nil {
		return append(args, "-c:v", "wrapped_avframe", "-f", "null", "pipe:1"), nil
	}
	return append(args, "-c:v", "rawvideo", "-pix_fmt", "rgb24", "-f", "rawvideo", "-flush_packets", "1", "pipe:1"), nil
}

func readAnalysisPreviewHeldFrames(ctx context.Context, input io.Reader, log *analysisPreviewHoldLog, emit func(analysisPreviewHoldPoint, []byte) error) error {
	raster := make([]byte, log.plan.width*log.plan.height*3)
	for index := 0; ; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(input, raster)
		if err == io.EOF && n == 0 {
			if index != len(log.proof.points) {
				return fmt.Errorf("%w: incomplete selected preview stream", ErrAnalysisUnproven)
			}
			return nil
		}
		if err != nil || index >= len(log.proof.points) {
			return fmt.Errorf("%w: truncated or excess selected preview raster", ErrAnalysisUnproven)
		}
		point, err := log.next(ctx)
		if err != nil {
			return err
		}
		if err := emit(point, raster); err != nil {
			return err
		}
	}
}
