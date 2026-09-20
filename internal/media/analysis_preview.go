package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
)

const PreviewAnalysisProfile = "source-pts-display-preceding-hold-jpeg-v3;geometry=" + AnalysisGeometryProfile

// PreviewAnalysisOptions creates fixed nominal slots across the complete
// source timeline. Width is one of the admitted cache variants, not an HTTP
// transformation parameter. Zero values select 320 pixels, ten seconds and
// JPEG quality 80. Explicit qualities must be in the admitted range 40..95.
type PreviewAnalysisOptions struct {
	IntervalTicks int64
	Width         int
	Quality       int
}

// PreviewFrame is one provisional derivative. JPEG is read-only borrowed data
// for the duration of the call; callers must persist it before returning.
// NominalTicks identifies its fixed slot. ActualTicks is the matched original
// source timestamp and can repeat when that picture spans multiple slots.
// Slots before the first source picture hold that first picture backward;
// later slots use the preceding picture, including a final hold after video EOF.
type PreviewFrame struct {
	NominalTicks int64
	ActualTicks  int64
	Width        int
	Height       int
	SHA256       [sha256.Size]byte
	JPEG         []byte
}

type PreviewAnalysisSummary struct {
	Profile       string
	FFmpegSHA256  string
	FFprobeSHA256 string
	IntervalTicks int64
	FrameCount    int
	Width         int
	Height        int
	Quality       int
	JPEGBytes     int64
}

// ExtractPreviews audits one complete decode, then emits each uniquely selected
// picture from a second complete decode and reuses its JPEG for held slots.
// Both passes share one deadline and cumulative source/diagnostic budgets.
// Emission is provisional: the caller must stage bytes in owned storage and publish
// only after this method succeeds. Any decoder, callback, identity, timestamp,
// or cancellation error invalidates all frames emitted by this invocation.
// The synchronous callback must return promptly and honor the caller context.
func (extractor AnalysisExtractor) ExtractPreviews(ctx context.Context, file *os.File, info Info, streamIndex int, options PreviewAnalysisOptions, emit func(PreviewFrame) error) (summary PreviewAnalysisSummary, resultErr error) {
	if ctx == nil {
		return summary, ErrAnalysisUnavailable
	}
	if emit == nil {
		return summary, fmt.Errorf("%w: preview callback is required", ErrAnalysisUnproven)
	}
	quality, err := analysisPreviewQuality(options.Quality)
	if err != nil {
		return summary, err
	}
	limits, err := extractor.analysisLimits()
	if err != nil {
		return summary, err
	}
	before, err := analysisCheckSource(file, info)
	if err != nil {
		return summary, err
	}
	finalContext := ctx
	var release func()
	defer func() {
		if err := finalizeAnalysisPreview(finalContext, func() error { return videoSeekCheckSource(file, before) }, release); err != nil {
			summary, resultErr = PreviewAnalysisSummary{}, errors.Join(resultErr, err)
		}
	}()
	stream, err := analysisVisualStream(info, streamIndex, limits)
	if err != nil {
		return summary, err
	}
	processContext, operationRelease, err := analysisAcquire(ctx, limits.Timeout)
	if err != nil {
		return summary, err
	}
	finalContext, release = processContext, operationRelease
	tool, err := analysisOpenToolExpected(processContext, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return summary, err
	}
	defer tool.file.Close()
	if err := analysisValidateFFmpeg(processContext, tool); err != nil {
		return summary, err
	}
	defer func() {
		if err := tool.check(); err != nil {
			summary, resultErr = PreviewAnalysisSummary{}, err
		}
	}()
	geometry, err := extractor.analysisGeometry(processContext, file, stream, limits)
	if err != nil {
		return summary, err
	}
	plan, err := analysisPreviewGeometryOptions(info, stream, geometry, options, limits)
	if err != nil {
		return summary, err
	}
	budget := &analysisPreviewHoldBudget{limits: limits}
	proof, err := runAnalysisPreviewHoldPlan(processContext, tool, file, before, info, stream, plan, limits, budget)
	if err != nil {
		return summary, err
	}
	log, err := newAnalysisPreviewHoldLog(info, stream, plan, limits, budget, &proof)
	if err != nil {
		return summary, err
	}
	args, err := buildAnalysisPreviewHoldArgs(stream, plan, limits, &proof)
	if err != nil {
		return summary, err
	}
	summary = PreviewAnalysisSummary{Profile: PreviewAnalysisProfile, FFmpegSHA256: tool.sha, FFprobeSHA256: geometry.ffprobeSHA, IntervalTicks: plan.interval, Width: plan.width, Height: plan.height, Quality: quality}
	rawLimit := int64(len(proof.points)) * int64(plan.width) * int64(plan.height) * 3
	err = runAnalysisStream(processContext, "/proc/self/fd/4", file, args, limits.Timeout, rawLimit, log,
		func(reader io.Reader) error {
			return readAnalysisPreviewHeldFrames(processContext, reader, log, func(point analysisPreviewHoldPoint, pixels []byte) error {
				if err := processContext.Err(); err != nil {
					return err
				}
				frameLimit := min(limits.MaxJPEGBytes, limits.MaxOutputBytes-summary.JPEGBytes)
				jpegBytes, err := analysisEncodePreviewJPEG(pixels, plan.width, plan.height, quality, frameLimit)
				if err != nil {
					return err
				}
				if err := processContext.Err(); err != nil {
					return err
				}
				digest := sha256.Sum256(jpegBytes)
				for offset := 0; offset < point.slotCount; offset++ {
					if err := processContext.Err(); err != nil {
						return err
					}
					if int64(len(jpegBytes)) > limits.MaxOutputBytes-summary.JPEGBytes {
						return fmt.Errorf("%w: held preview JPEG bytes", ErrAnalysisBudget)
					}
					frame := PreviewFrame{NominalTicks: int64(point.firstSlot+offset) * plan.interval, ActualTicks: point.actual,
						Width: plan.width, Height: plan.height, SHA256: digest, JPEG: jpegBytes}
					if err := emit(frame); err != nil {
						return err
					}
					if sha256.Sum256(jpegBytes) != digest {
						return fmt.Errorf("%w: callback modified borrowed preview bytes", ErrAnalysisUnproven)
					}
					summary.FrameCount++
					summary.JPEGBytes += int64(len(jpegBytes))
				}
				return processContext.Err()
			})
		}, tool.file)
	if err == nil {
		err = log.result()
	}
	if err == nil {
		err = processContext.Err()
	}
	if err == nil && summary.FrameCount != plan.frames {
		err = fmt.Errorf("%w: incomplete held preview slots", ErrAnalysisUnproven)
	}
	if err != nil {
		return PreviewAnalysisSummary{}, err
	}
	return summary, nil
}

func analysisPreviewOptions(info Info, stream Stream, options PreviewAnalysisOptions, limits AnalysisLimits) (analysisVisualPlan, error) {
	return analysisPreviewGeometryOptions(info, stream, analysisSquareGeometry(stream), options, limits)
}

func analysisPreviewGeometryOptions(info Info, stream Stream, geometry analysisDisplayGeometry, options PreviewAnalysisOptions, limits AnalysisLimits) (analysisVisualPlan, error) {
	if _, err := analysisPreviewQuality(options.Quality); err != nil {
		return analysisVisualPlan{}, err
	}
	if options.IntervalTicks == 0 {
		options.IntervalTicks = 10 * TicksPerSecond
	}
	if options.Width == 0 {
		options.Width = 320
	}
	if options.Width != 240 && options.Width != 320 && options.Width != 400 {
		return analysisVisualPlan{}, fmt.Errorf("%w: unsupported preview width", ErrAnalysisUnproven)
	}
	if info.DurationTicks <= 0 || info.DurationTicks > MaxAnalysisDurationTicks ||
		options.IntervalTicks < TicksPerSecond || options.IntervalTicks > 600*TicksPerSecond || stream.Width <= 0 || stream.Height <= 0 ||
		int64(stream.Width) > limits.MaxSourcePixels/int64(stream.Height) {
		return analysisVisualPlan{}, fmt.Errorf("%w: unsupported preview duration or interval", ErrAnalysisUnproven)
	}
	frames := (info.DurationTicks-1)/options.IntervalTicks + 1
	height, err := geometry.previewHeight(options.Width, limits)
	if err != nil {
		return analysisVisualPlan{}, err
	}
	pixels := int64(options.Width) * int64(height)
	if frames > int64(limits.MaxPreviewFrames) || pixels > limits.MaxFramePixels ||
		pixels > limits.MaxRawBytes/3/frames {
		return analysisVisualPlan{}, fmt.Errorf("%w: preview frames or raw pixels", ErrAnalysisBudget)
	}
	return analysisVisualPlan{end: info.DurationTicks, interval: options.IntervalTicks, frames: int(frames),
		width: options.Width, height: height, pixelFormat: "rgb24", channels: 3, geometry: geometry}, nil
}

func analysisPreviewQuality(quality int) (int, error) {
	if quality == 0 {
		return 80, nil
	}
	if quality < 40 || quality > 95 {
		return 0, fmt.Errorf("%w: preview quality must be between 40 and 95", ErrAnalysisUnproven)
	}
	return quality, nil
}

func analysisEncodePreviewJPEG(pixels []byte, width, height, quality int, limit int64) ([]byte, error) {
	quality, err := analysisPreviewQuality(quality)
	if err != nil {
		return nil, err
	}
	if width <= 0 || height <= 0 || int64(width) > (4<<20)/int64(height) || int64(width)*int64(height)*3 != int64(len(pixels)) || limit < 1 || limit > 8<<20 {
		return nil, ErrAnalysisBudget
	}
	encoded := analysisJPEGBuffer{limit: limit}
	if err := jpeg.Encode(&encoded, analysisRGBImage{pixels: pixels, width: width, height: height}, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return encoded.buffer.Bytes(), nil
}

type analysisJPEGBuffer struct {
	buffer bytes.Buffer
	limit  int64
}

func (output *analysisJPEGBuffer) Write(data []byte) (int, error) {
	if int64(len(data)) > output.limit-int64(output.buffer.Len()) {
		return 0, fmt.Errorf("%w: preview JPEG bytes", ErrAnalysisBudget)
	}
	return output.buffer.Write(data)
}

// analysisRGBImage views the single reusable raw frame without allocating a
// second RGBA raster. The standard library encoder never receives unchecked
// dimensions or unbounded compressed input.
type analysisRGBImage struct {
	pixels        []byte
	width, height int
}

func (frame analysisRGBImage) ColorModel() color.Model { return color.RGBAModel }
func (frame analysisRGBImage) Bounds() image.Rectangle {
	return image.Rect(0, 0, frame.width, frame.height)
}
func (frame analysisRGBImage) At(x, y int) color.Color {
	if x < 0 || x >= frame.width || y < 0 || y >= frame.height {
		return color.RGBA{}
	}
	position := (y*frame.width + x) * 3
	return color.RGBA{R: frame.pixels[position], G: frame.pixels[position+1], B: frame.pixels[position+2], A: 255}
}
