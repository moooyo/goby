package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

type BackgroundClipDolbyVisionCapabilities struct {
	Available     bool
	Reason        string
	FFmpegSHA256  string
	FFprobeSHA256 string
	Device        string
}

// BackgroundClipDolbyVisionAvailability checks the private strict-renderer
// interface and actually converts a finite PQ source through the selected
// Vulkan device to CPU-encoded SDR. It proves the execution dependencies only;
// each real DV source still needs source-bound RPU and strict per-frame checks.
// In particular, this synthetic check does not establish Profile 5 acceptance.
func (extractor AnalysisExtractor) BackgroundClipDolbyVisionAvailability(ctx context.Context, device string, validateHardware func(context.Context) error) (result BackgroundClipDolbyVisionCapabilities, resultErr error) {
	result.Device = device
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	if !backgroundClipVulkanDeviceValid(device) {
		result.Reason = "dolby_vision_device_unavailable"
		return result, nil
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unavailable := func(reason string, err error) (BackgroundClipDolbyVisionCapabilities, error) {
		result.Reason = reason
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
	var help bytes.Buffer
	sink := &analysisDiscardStderr{}
	err = runAnalysisProcess(bounded, "/proc/self/fd/3", nil, nil,
		[]string{"-hide_banner", "-h", "filter=libplacebo"}, 5*time.Second, 256<<10, sink,
		func(reader io.Reader) error { _, err := io.Copy(&help, reader); return err }, ffmpeg.file)
	if err == nil {
		err = sink.failure()
	}
	if err != nil || !backgroundClipStrictDolbyVisionOptions(help.String()) {
		return unavailable("dolby_vision_strict_filter_unavailable", err)
	}
	options, _ := normalizeBackgroundClipOptions(BackgroundClipOptions{DolbyVision: &BackgroundClipDolbyVisionOptions{Device: device}})
	limits := DefaultAnalysisLimits()
	limits.Timeout = 30 * time.Second
	// This source has real PQ tags but no RPU, so the dependency check uses
	// ordinary PQ processing through the same Vulkan/output graph. Strict
	// option presence above and real DV corpus checks have separate roles.
	plan := backgroundClipPlan{duration: TicksPerSecond, frames: backgroundClipFPS, width: 1280, height: 720,
		toneMapped: true, dolbyVision: true, options: options,
		colorFilter: "format=yuv420p10le,setparams=range=limited:color_primaries=bt2020:color_trc=smpte2084:colorspace=bt2020nc," +
			"libplacebo=format=yuv420p:colorspace=bt709:color_primaries=bt709:color_trc=bt709:range=tv:apply_dolbyvision=0:tonemapping=bt.2390:peak_detect=0,format=yuv420p"}
	production := backgroundClipEncodeArgs(Stream{Index: 0}, plan, limits)
	args := []string{"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-xerror", "-max_alloc", "268435456",
		"-filter_threads", "1", "-filter_complex_threads", "1"}
	args = append(args, backgroundClipVulkanDeviceArgs(device)...)
	args = append(args, "-f", "lavfi", "-i", "testsrc2=size=128x72:rate=24")
	for index, argument := range production {
		if argument == "-map" {
			args = append(args, production[index:]...)
			break
		}
	}
	var encoded bytes.Buffer
	sink = &analysisDiscardStderr{}
	err = runBackgroundClipProcess(backgroundClipHardwareContext(bounded, true, validateHardware), "/proc/self/fd/3", nil, args, limits.Timeout, 4<<20, sink,
		func(reader io.Reader) error { _, err := io.Copy(&encoded, reader); return err }, true, ffmpeg.file)
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
		return unavailable("dolby_vision_vulkan_execution_unavailable", err)
	}
	result.Available = true
	return result, nil
}

func backgroundClipStrictDolbyVisionOptions(help string) bool {
	required := map[string]bool{"apply_dolbyvision": false, "strict_dolbyvision": false, "strict_dolbyvision_profile": false}
	for _, line := range strings.Split(help, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if _, exists := required[fields[0]]; exists {
				required[fields[0]] = true
			}
		}
	}
	for _, present := range required {
		if !present {
			return false
		}
	}
	return true
}
