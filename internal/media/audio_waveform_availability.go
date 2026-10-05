package media

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

type AudioWaveformCapabilities struct {
	Available    bool
	Profile      string
	FFmpegSHA256 string
	Reason       string
}

// AudioWaveformAvailability inspects the admitted FFmpeg's fixed-version
// grammar and required built-in primitives. Individual source decoders are
// admitted by actual extraction; this inventory makes no per-codec claim.
func (extractor AnalysisExtractor) AudioWaveformAvailability(ctx context.Context) (result AudioWaveformCapabilities, resultErr error) {
	result.Profile = AudioWaveformProfile
	if ctx == nil {
		return result, ErrAnalysisUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	unavailable := func(reason string, err error) (AudioWaveformCapabilities, error) {
		result.Reason = reason
		if bounded.Err() != nil {
			return result, errors.Join(bounded.Err(), err)
		}
		return result, nil
	}
	tool, err := analysisOpenToolExpected(bounded, extractor.FFmpegPath, extractor.ExpectedFFmpegSHA256)
	if err != nil {
		return unavailable("ffmpeg_unavailable", err)
	}
	defer tool.file.Close()
	result.FFmpegSHA256 = tool.sha
	if err := analysisValidateFFmpeg(bounded, tool); err != nil {
		return unavailable("ffmpeg_profile_unavailable", err)
	}
	query := func(option string) (string, error) {
		var output []byte
		sink := &analysisDiscardStderr{}
		err := runAnalysisProcess(bounded, "/proc/self/fd/3", nil, nil, []string{"-hide_banner", option}, 5*time.Second, 256<<10, sink, func(reader io.Reader) error { var err error; output, err = io.ReadAll(reader); return err }, tool.file)
		if err == nil {
			err = sink.failure()
		}
		return string(output), err
	}
	filters, err := query("-filters")
	if err != nil {
		return unavailable("waveform_filters_unavailable", err)
	}
	for _, name := range []string{"aformat", "asettb", "ashowinfo"} {
		if !audioWaveformInventoryContains(filters, name) {
			return unavailable("waveform_filters_unavailable", ErrAnalysisUnavailable)
		}
	}
	encoders, err := query("-encoders")
	if err != nil || !audioWaveformInventoryContains(encoders, "pcm_f32le") {
		return unavailable("waveform_pcm_unavailable", err)
	}
	muxers, err := query("-muxers")
	if err != nil || !audioWaveformInventoryContains(muxers, "f32le") {
		return unavailable("waveform_pcm_unavailable", err)
	}
	if err := tool.check(); err != nil {
		return unavailable("ffmpeg_changed", err)
	}
	result.Available = true
	return result, nil
}

func audioWaveformInventoryContains(inventory, name string) bool {
	for _, line := range strings.Split(inventory, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
}
