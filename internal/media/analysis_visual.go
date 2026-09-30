package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"strconv"

	"github.com/moooyo/goby/internal/introdetect"
)

const (
	MaxVisualAnalysisTicks = 600 * TicksPerSecond
	// VisualHashProfile fixes the raster, hash partition, RMS, and normalized
	// luma descriptor. A cache must include it and the actual sample ticks.
	VisualHashProfile  = "gray32-dhash9x8-rms-luma8x8-z32-v2"
	analysisVisualSide = 32
)

// VisualAnalysisOptions selects source-relative time slots. Zero values use
// the first 600 seconds and a 500 ms interval. Boundary refinement can request
// a smaller window and intervals down to 100 ms, within the same prefix.
type VisualAnalysisOptions struct {
	StartTicks    int64
	EndTicks      int64
	IntervalTicks int64
}

// ExtractVisual returns evidence only after the bounded decoder has exited and
// the authorized descriptor has been rechecked. Unknown original packet PTS,
// unmatched decoded timestamps, incomplete slots, and VFR gaps decline the
// entire result. Packet timestamp membership is not frame-position identity.
func (extractor AnalysisExtractor) ExtractVisual(ctx context.Context, file *os.File, info Info, streamIndex int, options VisualAnalysisOptions) (samples []introdetect.VisualSample, resultErr error) {
	if ctx == nil {
		return nil, ErrAnalysisUnavailable
	}
	limits, err := extractor.analysisLimits()
	if err != nil {
		return nil, err
	}
	plan, err := analysisVisualOptions(info, options, limits)
	if err != nil {
		return nil, err
	}
	samples = make([]introdetect.VisualSample, 0, plan.frames)
	err = extractor.extractVisualPlan(ctx, file, info, streamIndex, plan, limits, func(frame analysisVisualFrame, gray []byte) error {
		hash, contrast := analysisVisualHash(gray)
		luma, lumaKnown := analysisVisualLuma(gray)
		samples = append(samples, introdetect.VisualSample{Ticks: frame.actual, Hash: hash, Contrast: contrast, Luma: luma, LumaKnown: lumaKnown})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return samples, nil
}

// extractVisualPlan applies the same source, tool, geometry, packet timestamp,
// and complete-slot proof to every admitted grayscale raster size. Callers
// discard their collected samples when any final check fails.
func (extractor AnalysisExtractor) extractVisualPlan(ctx context.Context, file *os.File, info Info, streamIndex int, plan analysisVisualPlan, limits AnalysisLimits, emit func(analysisVisualFrame, []byte) error) (resultErr error) {
	before, err := analysisCheckSource(file, info)
	if err != nil {
		return err
	}
	defer func() {
		if err := errors.Join(videoSeekCheckSource(file, before), ctx.Err()); err != nil {
			resultErr = err
		}
	}()
	stream, err := analysisVisualStream(info, streamIndex, limits)
	if err != nil {
		return err
	}
	if plan.width == analysisRefinementSide {
		if err := analysisRefinementPTSSelection(info, stream, &plan); err != nil {
			return err
		}
	}
	processContext, release, err := analysisAcquire(ctx, limits.Timeout)
	if err != nil {
		return err
	}
	defer release()
	tool, err := analysisOpenToolExpected(processContext, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return err
	}
	defer tool.file.Close()
	if err := analysisValidateFFmpeg(processContext, tool); err != nil {
		return err
	}
	defer func() {
		if err := tool.check(); err != nil {
			resultErr = err
		}
	}()
	geometry, err := extractor.analysisGeometry(processContext, file, stream, limits)
	if err != nil {
		return err
	}
	plan.geometry = geometry
	log, err := newAnalysisVisualLog(info, stream, plan, limits)
	if err != nil {
		return err
	}
	err = runAnalysisStream(processContext, "/proc/self/fd/4", file, buildAnalysisVisualArgs(info, stream, plan, limits), limits.Timeout,
		int64(plan.frames*plan.width*plan.height), log, func(reader io.Reader) error {
			return readAnalysisVisualFrames(processContext, reader, log, emit)
		}, tool.file)
	if err == nil {
		err = log.result()
	}
	return err
}

func analysisVisualOptions(info Info, options VisualAnalysisOptions, limits AnalysisLimits) (analysisVisualPlan, error) {
	return analysisVisualRasterOptions(info, options, limits, analysisVisualSide)
}

func analysisVisualRasterOptions(info Info, options VisualAnalysisOptions, limits AnalysisLimits, side int) (analysisVisualPlan, error) {
	if side != analysisVisualSide && side != analysisRefinementSide {
		return analysisVisualPlan{}, fmt.Errorf("%w: invalid analysis raster size", ErrAnalysisUnproven)
	}
	if options.EndTicks == 0 {
		options.EndTicks = min(info.DurationTicks, MaxVisualAnalysisTicks)
	}
	if options.IntervalTicks == 0 {
		options.IntervalTicks = TicksPerSecond / 2
	}
	if options.StartTicks < 0 || options.EndTicks <= options.StartTicks || options.EndTicks > MaxVisualAnalysisTicks ||
		options.EndTicks > info.DurationTicks || options.IntervalTicks < TicksPerSecond/10 || options.IntervalTicks > 10*TicksPerSecond {
		return analysisVisualPlan{}, fmt.Errorf("%w: invalid visual window or sample interval", ErrAnalysisUnproven)
	}
	frames := (options.EndTicks-options.StartTicks-1)/options.IntervalTicks + 1
	if frames > int64(limits.MaxVisualSamples) || int64(side*side) > limits.MaxFramePixels ||
		frames*int64(side*side) > limits.MaxRawBytes {
		return analysisVisualPlan{}, fmt.Errorf("%w: visual sample plan", ErrAnalysisBudget)
	}
	return analysisVisualPlan{start: options.StartTicks, end: options.EndTicks, interval: options.IntervalTicks,
		frames: int(frames), width: side, height: side, pixelFormat: "gray", channels: 1}, nil
}

func analysisVisualStream(info Info, index int, limits AnalysisLimits) (Stream, error) {
	if index < 0 || index > 4095 || !info.FormatStartKnown || info.DurationTicks <= 0 || info.DurationTicks > MaxAnalysisDurationTicks {
		return Stream{}, fmt.Errorf("%w: unknown source origin or unsupported duration", ErrAnalysisUnproven)
	}
	var selected *Stream
	for _, stream := range info.Streams {
		if stream.Index != index {
			continue
		}
		if selected != nil || stream.CodecType != "video" || stream.IsAttachedPicture || stream.IsExternal ||
			stream.Width <= 0 || stream.Height <= 0 || int64(stream.Width) > limits.MaxSourcePixels/int64(stream.Height) {
			return Stream{}, fmt.Errorf("%w: unsupported analysis video stream", ErrAnalysisUnproven)
		}
		copyStream := stream
		selected = &copyStream
	}
	if selected == nil {
		return Stream{}, fmt.Errorf("%w: missing analysis video stream", ErrAnalysisUnproven)
	}
	if _, err := analysisTimeBase(selected.TimeBase); err != nil {
		return Stream{}, fmt.Errorf("%w: %v", ErrAnalysisUnproven, err)
	}
	return *selected, nil
}

func buildAnalysisVisualArgs(info Info, stream Stream, plan analysisVisualPlan, limits AnalysisLimits) []string {
	absolute := func(ticks int64) string {
		value := new(big.Int).Add(big.NewInt(info.FormatStartTicks), big.NewInt(ticks))
		return new(big.Rat).SetFrac(value, big.NewInt(TicksPerSecond)).FloatString(7)
	}
	interval := new(big.Rat).SetFrac64(plan.interval, TicksPerSecond).FloatString(7)
	selection := "gte(t," + absolute(plan.start) + "+selected_n*" + interval + ")"
	if plan.selectExpression != "" {
		selection = plan.selectExpression
	}
	// trim terminates at the absolute source clock boundary without -ss/-t
	// rebasing. select's selected_n advances only when a frame is selected.
	// Integer PTS validation independently checks every nominal slot afterward.
	filter := plan.geometry.filter() + "sidedata=mode=delete,showinfo@analysis_source=checksum=0,trim=end=" + absolute(plan.end) +
		",select='" + selection + "'," +
		"scale=" + strconv.Itoa(plan.width) + ":" + strconv.Itoa(plan.height) + ":flags=area,setsar=1,format=" + plan.pixelFormat
	return []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "repeat+level+info", "-debug_ts", "-xerror",
		"-max_alloc", "268435456", "-copyts", "-fflags", "+nofillin-genpts", "-threads", "1", "-max_pixels", strconv.FormatInt(limits.MaxSourcePixels, 10),
		"-filter_threads", "1", "-filter_complex_threads", "1", "-noautorotate", "-reinit_filter", "0",
		"-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mpegts,avi", "-i", "/proc/self/fd/3",
		"-map", "0:" + strconv.Itoa(stream.Index), "-an", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
		"-vf", filter, "-fps_mode:v", "passthrough", "-enc_time_base:v", "filter", "-c:v", "rawvideo", "-threads:v", "1",
		"-pix_fmt", plan.pixelFormat, "-f", "rawvideo", "-flush_packets", "1", "pipe:1"}
}

// analysisVisualHash compares adjacent mean-luma cells on a 9 by 8 grid.
// Contrast is population RMS deviation from mean luma, normalized by 255 and
// rounded to 0..1000. Constant rasters deliberately retain zero contrast.
func analysisVisualHash(gray []byte) (uint64, uint16) {
	var sum, squared uint64
	for _, value := range gray {
		sum += uint64(value)
		squared += uint64(value) * uint64(value)
	}
	count := float64(len(gray))
	mean := float64(sum) / count
	variance := max(0, float64(squared)/count-mean*mean)
	contrast := uint16(math.Round(math.Sqrt(variance) * 1000 / 255))
	var hash uint64
	for row := 0; row < 8; row++ {
		var previousSum, previousCount uint64
		for column := 0; column < 9; column++ {
			left, right := column*analysisVisualSide/9, (column+1)*analysisVisualSide/9
			var cellSum uint64
			for y := row * 4; y < (row+1)*4; y++ {
				for x := left; x < right; x++ {
					cellSum += uint64(gray[y*analysisVisualSide+x])
				}
			}
			cellCount := uint64((right - left) * 4)
			if column > 0 && previousSum*cellCount > cellSum*previousCount {
				hash |= uint64(1) << (row*8 + column - 1)
			}
			previousSum, previousCount = cellSum, cellCount
		}
	}
	return hash, contrast
}

// analysisVisualLuma retains spatial luminance magnitudes that a binary
// gradient hash discards. Each entry is the mean of one 4 by 4 cell, centered
// by the 64-cell mean and divided by their population standard deviation.
// Scaling by 32, rounding away from zero, and saturating at +/-127 bounds the
// descriptor. Affine brightness changes preserve it until clipping occurs.
// A spatial standard deviation below one luma level is unobservable even when
// fine raster texture gives the original frame appreciable contrast.
func analysisVisualLuma(gray []byte) (luma [64]int8, known bool) {
	if len(gray) != analysisVisualSide*analysisVisualSide {
		return luma, false
	}
	var cells [64]float64
	var sum, squared float64
	for row := 0; row < 8; row++ {
		for column := 0; column < 8; column++ {
			var cellSum int
			for y := row * 4; y < (row+1)*4; y++ {
				for x := column * 4; x < (column+1)*4; x++ {
					cellSum += int(gray[y*analysisVisualSide+x])
				}
			}
			value := float64(cellSum) / 16
			cells[row*8+column] = value
			sum += value
			squared += value * value
		}
	}
	mean := sum / 64
	variance := max(0, squared/64-mean*mean)
	if variance < 1 {
		return luma, false
	}
	scale := 32 / math.Sqrt(variance)
	for index, value := range cells {
		luma[index] = int8(max(-127, min(127, math.Round((value-mean)*scale))))
	}
	return luma, true
}
