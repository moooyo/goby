package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"strconv"
	"strings"
)

const (
	BackgroundClipProfile         = "background-h264-24fps-silent-v2;geometry=" + AnalysisGeometryProfile
	MaxBackgroundClipTicks  int64 = 60 * TicksPerSecond
	MaxBackgroundClipBytes  int64 = 64 << 20
	backgroundClipFPS             = 24
	backgroundClipTimescale       = 24000
)

var (
	ErrBackgroundClipUnsupported = errors.New("background clip source is unsupported")
	ErrBackgroundClipUnusable    = errors.New("background clip contains predominantly black or static pictures")
)

// BackgroundClipSummary describes a completely decoded, finite derivative.
// StartTicks is the requested source-relative seek; DurationTicks is the actual
// fixed-cadence output duration, rounded upward to the next 100 ns tick.
type BackgroundClipSummary struct {
	Profile       string
	FFmpegSHA256  string
	FFprobeSHA256 string
	StartTicks    int64
	DurationTicks int64
	Width         int
	Height        int
	Bytes         int64
}

// BackgroundClipOptions contains only server-selected encoding parameters.
// Zero values use 1280 pixels and 1.5 Mbps. The display ratio is preserved.
type BackgroundClipOptions struct {
	MaxWidth     int
	VideoBitrate int
	DolbyVision  *BackgroundClipDolbyVisionOptions
}

type backgroundClipPlan struct {
	start, duration       int64
	frames, width, height int
	geometry              analysisDisplayGeometry
	colorFilter           string
	toneMapped            bool
	deinterlace           string
	options               BackgroundClipOptions
	dolbyVision           bool
	decodeFromStart       bool
}

// GenerateBackgroundClip encodes only the supplied authorized descriptor. The
// finite MP4 is held within a fixed memory budget and fully decoded before any
// output bytes are emitted. The writer must return promptly; emitted bytes are
// provisional until the method succeeds, and publication remains the caller's
// responsibility. It never changes, creates, or removes a source-side file.
func (extractor AnalysisExtractor) GenerateBackgroundClip(ctx context.Context, file *os.File, info Info, streamIndex int, startTicks, durationTicks int64, output io.Writer) (summary BackgroundClipSummary, resultErr error) {
	return extractor.GenerateBackgroundClipWithOptions(ctx, file, info, streamIndex, startTicks, durationTicks, BackgroundClipOptions{}, output)
}

// GenerateBackgroundClipWithOptions applies a validated output profile while
// retaining the same provisional-output and authorized-descriptor contract.
func (extractor AnalysisExtractor) GenerateBackgroundClipWithOptions(ctx context.Context, file *os.File, info Info, streamIndex int, startTicks, durationTicks int64, options BackgroundClipOptions, output io.Writer) (summary BackgroundClipSummary, resultErr error) {
	if ctx == nil || output == nil {
		return summary, ErrAnalysisUnavailable
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	limits, err := extractor.analysisLimits()
	if err != nil {
		return summary, err
	}
	if extractor.Limits.MaxFramePixels == 0 {
		limits.MaxFramePixels = 1920 * 1080
	}
	options, err = normalizeBackgroundClipOptions(options)
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
			summary, resultErr = BackgroundClipSummary{}, errors.Join(resultErr, err)
		}
	}()
	stream, err := analysisVisualStream(info, streamIndex, limits)
	if err != nil {
		return summary, err
	}
	if err := backgroundClipWindow(info, startTicks, durationTicks); err != nil {
		return summary, err
	}
	processContext, operationRelease, err := analysisAcquire(ctx, limits.Timeout)
	if err != nil {
		return summary, err
	}
	finalContext, release = processContext, operationRelease
	ffmpeg, err := analysisOpenToolExpected(processContext, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return summary, err
	}
	defer ffmpeg.file.Close()
	ffprobe, err := analysisOpenToolExpected(processContext, extractor.FFprobePath, extractor.ExpectedFFprobeSHA256)
	if err != nil {
		return summary, err
	}
	defer ffprobe.file.Close()
	defer func() {
		if err := errors.Join(ffmpeg.check(), ffprobe.check()); err != nil {
			summary, resultErr = BackgroundClipSummary{}, errors.Join(resultErr, err)
		}
	}()
	if err := analysisValidateFFmpeg(processContext, ffmpeg); err != nil {
		return summary, err
	}
	if err := analysisValidateFFprobe(processContext, ffprobe); err != nil {
		return summary, err
	}
	// Bind the separate geometry probe to the already admitted executable.
	geometryExtractor := extractor
	geometryExtractor.ExpectedFFprobeSHA256 = ffprobe.sha
	geometry, err := geometryExtractor.analysisGeometry(processContext, file, stream, limits)
	if err != nil {
		return summary, err
	}
	plan, err := planBackgroundClipWithOptions(info, stream, geometry, startTicks, durationTicks, options, limits)
	if err != nil {
		return summary, err
	}
	var encoded bytes.Buffer
	sink := &analysisDiscardStderr{}
	err = runBackgroundClipProcess(processContext, "/proc/self/fd/4", file, backgroundClipEncodeArgs(stream, plan, limits), limits.Timeout,
		min(MaxBackgroundClipBytes, limits.MaxOutputBytes), sink, func(reader io.Reader) error {
			_, err := io.Copy(&encoded, reader)
			return err
		}, plan.dolbyVision, ffmpeg.file)
	if err == nil {
		err = sink.failure()
	}
	if err != nil {
		return summary, err
	}
	if encoded.Len() == 0 {
		return summary, fmt.Errorf("%w: empty background clip", ErrAnalysisUnproven)
	}
	if err := validateBackgroundClip(processContext, ffprobe, ffmpeg, encoded.Bytes(), plan, limits); err != nil {
		return summary, err
	}
	if err := errors.Join(processContext.Err(), videoSeekCheckSource(file, before), ffmpeg.check(), ffprobe.check()); err != nil {
		return summary, err
	}
	if _, err := io.Copy(output, bytes.NewReader(encoded.Bytes())); err != nil {
		return summary, err
	}
	profile := backgroundClipProfile(options)
	if plan.dolbyVision {
		profile += ";dolby_vision=strict-sdr-v1"
	}
	return BackgroundClipSummary{Profile: profile, FFmpegSHA256: ffmpeg.sha, FFprobeSHA256: ffprobe.sha,
		StartTicks: plan.start, DurationTicks: (int64(plan.frames)*TicksPerSecond + backgroundClipFPS - 1) / backgroundClipFPS,
		Width: plan.width, Height: plan.height, Bytes: int64(encoded.Len())}, nil
}

func normalizeBackgroundClipOptions(options BackgroundClipOptions) (BackgroundClipOptions, error) {
	if options.MaxWidth == 0 {
		options.MaxWidth = 1280
	}
	if options.VideoBitrate == 0 {
		options.VideoBitrate = 1500000
	}
	if options.MaxWidth != 640 && options.MaxWidth != 960 && options.MaxWidth != 1280 && options.MaxWidth != 1920 || options.VideoBitrate < 250000 || options.VideoBitrate > 8000000 {
		return BackgroundClipOptions{}, fmt.Errorf("%w: invalid background clip encoding options", ErrAnalysisBudget)
	}
	if options.DolbyVision != nil {
		settings := *options.DolbyVision
		options.DolbyVision = &settings
	}
	return options, nil
}

func backgroundClipProfile(options BackgroundClipOptions) string {
	return BackgroundClipProfile + ";max_width=" + strconv.Itoa(options.MaxWidth) + ";bitrate=" + strconv.Itoa(options.VideoBitrate)
}

func backgroundClipWindow(info Info, start, duration int64) error {
	if start < 0 || duration < TicksPerSecond || duration > MaxBackgroundClipTicks || start > info.DurationTicks || duration > info.DurationTicks-start {
		return fmt.Errorf("%w: invalid background clip interval", ErrAnalysisUnproven)
	}
	return nil
}

func planBackgroundClip(info Info, stream Stream, geometry analysisDisplayGeometry, start, duration int64, limits AnalysisLimits) (backgroundClipPlan, error) {
	return planBackgroundClipWithOptions(info, stream, geometry, start, duration, BackgroundClipOptions{}, limits)
}

func planBackgroundClipWithOptions(info Info, stream Stream, geometry analysisDisplayGeometry, start, duration int64, options BackgroundClipOptions, limits AnalysisLimits) (backgroundClipPlan, error) {
	if err := backgroundClipWindow(info, start, duration); err != nil {
		return backgroundClipPlan{}, err
	}
	options, err := normalizeBackgroundClipOptions(options)
	if err != nil {
		return backgroundClipPlan{}, err
	}
	dolbyVision := stream.DolbyVision != nil || strings.EqualFold(stream.VideoRange, "DOVI")
	colorFilter, mapped, err := backgroundClipColorFilter(stream)
	if dolbyVision {
		colorFilter, err = backgroundClipDolbyVisionFilter(stream, options.DolbyVision)
		mapped = true
	}
	if err != nil {
		return backgroundClipPlan{}, err
	}
	width, height, err := backgroundClipDimensionsWithWidth(geometry, options.MaxWidth, limits)
	if err != nil {
		return backgroundClipPlan{}, err
	}
	plan := backgroundClipPlan{start: start, duration: duration, frames: int((duration*backgroundClipFPS + TicksPerSecond - 1) / TicksPerSecond),
		width: width, height: height, geometry: geometry, colorFilter: colorFilter, toneMapped: mapped, options: options, dolbyVision: dolbyVision}
	plan.decodeFromStart = dolbyVision && stream.DolbyVision.InBandParameterSets
	if stream.IsInterlaced {
		switch stream.FieldOrder {
		case "tt", "tb":
			plan.deinterlace = "bwdif=mode=send_frame:parity=tff:deint=all,"
		case "bb", "bt":
			plan.deinterlace = "bwdif=mode=send_frame:parity=bff:deint=all,"
		default:
			return backgroundClipPlan{}, fmt.Errorf("%w: unknown interlaced field order", ErrBackgroundClipUnsupported)
		}
		if dolbyVision {
			// The strict renderer consumes source-frame metadata before any
			// temporal filter or resize can replace or discard it.
			parity := "bff"
			if stream.FieldOrder == "tt" || stream.FieldOrder == "bt" {
				parity = "tff"
			}
			plan.deinterlace = "setfield=mode=" + parity + ","
		}
	}
	return plan, nil
}

func backgroundClipDimensions(geometry analysisDisplayGeometry, limits AnalysisLimits) (int, int, error) {
	return backgroundClipDimensionsWithWidth(geometry, 1280, limits)
}

func backgroundClipDimensionsWithWidth(geometry analysisDisplayGeometry, maxWidth int, limits AnalysisLimits) (int, int, error) {
	if geometry.width < 2 || geometry.height < 2 || geometry.sarNumerator <= 0 || geometry.sarDenominator <= 0 {
		return 0, 0, ErrAnalysisUnproven
	}
	// Exact rational display dimensions avoid overflowing pathological SAR tags.
	ratio := new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(int64(geometry.width)), big.NewInt(geometry.sarNumerator)),
		new(big.Int).Mul(big.NewInt(int64(geometry.height)), big.NewInt(geometry.sarDenominator)))
	width, height := maxWidth, maxWidth*9/16
	if ratio.Cmp(big.NewRat(16, 9)) >= 0 {
		h := new(big.Rat).Quo(big.NewRat(int64(width), 1), ratio)
		value := new(big.Int).Quo(h.Num(), h.Denom())
		if !value.IsInt64() {
			return 0, 0, ErrAnalysisBudget
		}
		height = int(value.Int64()) / 2 * 2
	} else {
		w := new(big.Rat).Mul(big.NewRat(int64(height), 1), ratio)
		value := new(big.Int).Quo(w.Num(), w.Denom())
		if !value.IsInt64() {
			return 0, 0, ErrAnalysisBudget
		}
		width = int(value.Int64()) / 2 * 2
	}
	if width < 2 || height < 2 || int64(width)*int64(height) > limits.MaxFramePixels {
		return 0, 0, fmt.Errorf("%w: background display raster", ErrAnalysisBudget)
	}
	return width, height, nil
}

func backgroundClipColorFilter(stream Stream) (string, bool, error) {
	unsupported := func() (string, bool, error) {
		return "", false, fmt.Errorf("%w: incomplete or unsupported video color metadata", ErrBackgroundClipUnsupported)
	}
	depth := EffectiveVideoBitDepth(stream)
	if stream.Codec == "" || stream.PixelFormat == "" || depth < 8 || depth > 12 || VideoBitDepthConflict(stream) || stream.DolbyVision != nil || stream.VideoRange == "DOVI" {
		return unsupported()
	}
	switch stream.ColorTransfer {
	case "smpte2084", "arib-std-b67":
		if depth < 10 || stream.VideoRangeKnown && stream.VideoRange == "SDR" || stream.ColorPrimaries != "bt2020" || stream.ColorSpace != "bt2020nc" && stream.ColorSpace != "bt2020c" || stream.ColorRange != "tv" && stream.ColorRange != "pc" {
			return unsupported()
		}
		matrix, sourceRange := "2020_ncl", "limited"
		if stream.ColorSpace == "bt2020c" {
			matrix = "2020_cl"
		}
		if stream.ColorRange == "pc" {
			sourceRange = "full"
		}
		// Match the established software playback path: linear-light transfer,
		// gamut conversion, actual tone mapping, then an SDR output transfer.
		return "zscale=transferin=" + stream.ColorTransfer + ":primariesin=2020:matrixin=" + matrix + ":rangein=" + sourceRange + ":transfer=linear:npl=100," +
			"format=gbrpf32le,zscale=primaries=709,tonemap=tonemap=hable:desat=2.0," +
			"zscale=primaries=709:transfer=709:matrix=709:range=limited:dither=error_diffusion," +
			"format=yuv420p,setparams=range=limited:color_primaries=bt709:color_trc=bt709:colorspace=bt709", true, nil
	case "", "unknown", "unspecified":
		// Untagged 8-bit media is common. Preserve unspecified color metadata
		// rather than making an unsupported claim about its transfer or gamut.
		if depth != 8 || stream.VideoRange != "" && stream.VideoRange != "SDR" {
			return unsupported()
		}
	case "bt709", "smpte170m", "smpte240m", "bt470m", "bt470bg", "gamma22", "gamma28", "iec61966-2-1", "iec61966-2-4", "bt1361e", "bt2020-10", "bt2020-12":
		if stream.VideoRange != "" && stream.VideoRange != "SDR" {
			return unsupported()
		}
	default:
		return unsupported()
	}
	return "format=yuv420p", false, nil
}

func backgroundClipSeconds(ticks int64) string {
	return new(big.Rat).SetFrac64(ticks, TicksPerSecond).FloatString(7)
}

func backgroundClipEncodeArgs(stream Stream, plan backgroundClipPlan, limits AnalysisLimits) []string {
	options, _ := normalizeBackgroundClipOptions(plan.options)
	level := "3.1"
	if plan.width*plan.height > 1280*720 {
		level = "4.0"
	}
	resize := plan.geometry.filter() + "scale=w=" + strconv.Itoa(plan.width) + ":h=" + strconv.Itoa(plan.height) + ":flags=bicubic"
	trim := "trim=duration=" + backgroundClipSeconds(plan.duration)
	if plan.decodeFromStart {
		// Empty hvcC parameter arrays require the initial in-band VPS/SPS/PPS.
		// Decode the bounded prefix in software, then trim without changing
		// source pixels or RPU metadata before the strict renderer sees them.
		trim = "trim=start=" + backgroundClipSeconds(plan.start) + ":duration=" + backgroundClipSeconds(plan.duration)
	}
	filter := trim + ",setpts=PTS-STARTPTS," + plan.deinterlace
	if plan.dolbyVision {
		filter += plan.colorFilter + "," + resize
	} else {
		filter += resize + "," + plan.colorFilter
	}
	filter += ",sidedata=mode=delete,setsar=1,fps=fps=24:start_time=0:round=near,format=yuv420p"
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-xerror", "-max_alloc", "268435456",
		"-threads", "2", "-max_pixels", strconv.FormatInt(limits.MaxSourcePixels, 10), "-filter_threads", "1", "-filter_complex_threads", "1"}
	if plan.dolbyVision {
		args = append(args, backgroundClipVulkanDeviceArgs(plan.options.DolbyVision.Device)...)
		// The HEVC decoder only rejects an RPU CRC mismatch when both flags
		// are enabled. A rejected RPU then fails the strict per-frame contract;
		// -xerror alone does not enable the decoder's CRC calculation.
		args = append(args, "-err_detect", "crccheck+explode")
	}
	args = append(args,
		"-noautorotate", "-reinit_filter", "0", "-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,mp4,m4a,3gp,3g2,mj2,mpegts,avi")
	if !plan.decodeFromStart {
		args = append(args, "-ss", backgroundClipSeconds(plan.start), "-accurate_seek")
	}
	args = append(args, "-i", "/proc/self/fd/3", "-map", "0:"+strconv.Itoa(stream.Index),
		"-an", "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1", "-vf", filter, "-t", backgroundClipSeconds(plan.duration),
		"-frames:v", strconv.Itoa(plan.frames), "-fps_mode:v", "passthrough", "-c:v", "libx264", "-threads:v", "2", "-preset", "veryfast",
		"-profile:v", "high", "-level:v", level, "-pix_fmt", "yuv420p", "-b:v", strconv.Itoa(options.VideoBitrate), "-maxrate", strconv.Itoa(min(options.VideoBitrate*5/4, 8000000)), "-bufsize", strconv.Itoa(options.VideoBitrate*2),
		"-g", "48", "-keyint_min", "48", "-sc_threshold", "0", "-bf", "0", "-field_order", "progressive")
	if plan.toneMapped {
		args = append(args, "-color_primaries", "bt709", "-color_trc", "bt709", "-colorspace", "bt709", "-color_range", "tv")
	}
	return append(args, "-video_track_timescale", strconv.Itoa(backgroundClipTimescale), "-movflags", "+empty_moov+frag_keyframe+default_base_moof", "-f", "mp4", "pipe:1")
}

func backgroundClipSDRTransfer(value string) bool {
	switch strings.ToLower(value) {
	case "", "unknown", "unspecified", "bt709", "smpte170m", "smpte240m", "bt470m", "bt470bg", "gamma22", "gamma28", "iec61966-2-1", "iec61966-2-4", "bt1361e", "bt2020-10", "bt2020-12":
		return true
	}
	return false
}
