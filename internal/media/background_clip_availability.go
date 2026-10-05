package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"
)

type BackgroundClipCapabilities struct {
	Available     bool
	Profile       string
	FFmpegSHA256  string
	FFprobeSHA256 string
	Reason        string
}

// BackgroundClipAvailability proves the SDR software path by actually encoding
// and decoding a finite synthetic clip. It is independent of preview-cache and
// fingerprint configuration. HDR metadata and its tone-map graph are separately
// checked at generation time; this result makes no hardware or HDR claim.
func (extractor AnalysisExtractor) BackgroundClipAvailability(ctx context.Context) (result BackgroundClipCapabilities, resultErr error) {
	options, _ := normalizeBackgroundClipOptions(BackgroundClipOptions{})
	result.Profile = backgroundClipProfile(options)
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unavailable := func(reason string, err error) (BackgroundClipCapabilities, error) {
		result.Available, result.Reason = false, reason
		if bounded.Err() != nil {
			return result, errors.Join(bounded.Err(), err)
		}
		return result, nil
	}
	ffmpeg, err := analysisOpenToolExpected(bounded, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return unavailable("ffmpeg_unavailable", err)
	}
	defer ffmpeg.file.Close()
	result.FFmpegSHA256 = ffmpeg.sha
	ffprobe, err := analysisOpenToolExpected(bounded, extractor.FFprobePath, extractor.ExpectedFFprobeSHA256)
	if err != nil {
		return unavailable("ffprobe_unavailable", err)
	}
	defer ffprobe.file.Close()
	result.FFprobeSHA256 = ffprobe.sha
	if err := analysisValidateFFmpeg(bounded, ffmpeg); err != nil {
		return unavailable("ffmpeg_profile_unavailable", err)
	}
	if err := analysisValidateFFprobe(bounded, ffprobe); err != nil {
		return unavailable("ffprobe_profile_unavailable", err)
	}
	limits := DefaultAnalysisLimits()
	limits.Timeout = 30 * time.Second
	plan := backgroundClipPlan{duration: TicksPerSecond, frames: backgroundClipFPS, width: 1280, height: 720, colorFilter: "format=yuv420p", options: options}
	// Keep the complete production output path. Only the input side changes;
	// the actual helper still receives an immutable executable descriptor.
	production := backgroundClipEncodeArgs(Stream{Index: 0}, plan, limits)
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-xerror", "-max_alloc", "268435456",
		"-filter_threads", "1", "-filter_complex_threads", "1", "-f", "lavfi", "-i", "testsrc2=size=128x72:rate=24"}
	for index, value := range production {
		if value == "-map" {
			args = append(args, production[index:]...)
			break
		}
	}
	var encoded bytes.Buffer
	sink := &analysisDiscardStderr{}
	err = runAnalysisProcess(bounded, "/proc/self/fd/3", nil, nil, args, limits.Timeout, 4<<20, sink,
		func(reader io.Reader) error { _, err := io.Copy(&encoded, reader); return err }, ffmpeg.file)
	if err == nil {
		err = sink.failure()
	}
	if err == nil {
		err = validateBackgroundClip(bounded, ffprobe, ffmpeg, encoded.Bytes(), plan, limits)
	}
	if err == nil {
		err = errors.Join(ffmpeg.check(), ffprobe.check(), bounded.Err())
	}
	if err != nil {
		return unavailable("background_clip_profile_unavailable", err)
	}
	result.Available = true
	return result, nil
}
