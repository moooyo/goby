package transcode

import (
	"fmt"
	"strconv"
)

// audioSampleSeekWindow uses decoded source samples, excluding decoder priming
// and padding. This avoids relying on a demuxer's page seek or format timestamp
// origin. All arithmetic is bounded before multiplication.
func audioSampleSeekWindow(p Plan, outputRate int) (start, end, samples int64, err error) {
	invalid := func() (int64, int64, int64, error) {
		return 0, 0, 0, fmt.Errorf("%w: audio sample seek", ErrInvalidPlan)
	}
	if !p.AudioSampleSeek || p.VideoStreamIndex != -1 || p.VideoCodec != "" || p.AudioStreamIndex < 0 || p.AudioCodec == "" || p.AudioCodec == "copy" ||
		p.AudioSourceSampleCount <= 0 || p.AudioSourceSampleRate <= 0 || p.ReferenceStartTicks != 0 {
		return invalid()
	}
	if _, err := ProgressiveOutputSamples(p, outputRate); err != nil {
		return 0, 0, 0, err
	}
	if p.EndTicks < 0 || p.EndTicks > p.DurationTicks || p.EndTicks > 0 && p.EndTicks <= p.StartTicks {
		return invalid()
	}
	start = progressiveSampleLimit(p.StartTicks, p.AudioSourceSampleRate)
	end = p.AudioSourceSampleCount
	if p.EndTicks > 0 {
		end = min(end, progressiveSampleLimit(p.EndTicks, p.AudioSourceSampleRate))
	}
	if end <= start {
		return invalid()
	}
	remaining, sourceRate, rate := end-start, int64(p.AudioSourceSampleRate), int64(outputRate)
	samples = remaining/sourceRate*rate + (remaining%sourceRate*rate+sourceRate-1)/sourceRate
	return start, end, samples, nil
}

func audioSampleSeekFilter(p Plan, outputRate int) string {
	start, end, samples, _ := audioSampleSeekWindow(p, outputRate)
	// Reset timestamps before resampling so a nonzero presentation origin cannot
	// leak into the output timeline. Sample bounds replace output -t: FFmpeg would
	// otherwise round that duration back into a potentially shorter sample limit.
	// The VOD muxer separately applies the global source-time offset.
	return "atrim=start_sample=" + strconv.FormatInt(start, 10) + ":end_sample=" + strconv.FormatInt(end, 10) +
		",asetpts=N/SR/TB,aresample=" + strconv.Itoa(outputRate) +
		",atrim=end_sample=" + strconv.FormatInt(samples, 10) + ",asetpts=N/SR/TB"
}
