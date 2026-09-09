package transcode

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func TestAudioSampleSeekArgumentsAndBoundedWindow(t *testing.T) {
	p := progressivePlan("wav", "pcm_s16le")
	p.AudioSampleSeek = true
	p.AudioSourceSampleCount = 289792
	p.DurationTicks = 60373334
	p.StartTicks = 12345
	p.AudioSampleRate = 44100
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "-ss") || slices.Contains(args, "-t") || !hasArgumentPair(args, "-af", "atrim=start_sample=60:end_sample=289792,asetpts=N/SR/TB,aresample=44100,atrim=end_sample=266192,asetpts=N/SR/TB") {
		t.Fatalf("sample seek must decode the prefix before trimming: %v", args)
	}
	p.OutputMode, p.Container, p.AudioCodec = "", "ts", "aac"
	p.SegmentMode, p.SegmentSeconds, p.SegmentStartNumber = "vod", 3, 2
	p.AudioBitrate = 96000
	p.EndTicks = 234567
	args, err = BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "-ss") || slices.Contains(args, "-t") || !hasArgumentPair(args, "-af", "atrim=start_sample=60:end_sample=1126,asetpts=N/SR/TB,aresample=44100,atrim=end_sample=980,asetpts=N/SR/TB") ||
		!hasArgumentPair(args, "-initial_offset", "1.0012345") || !hasArgumentPair(args, "-segment_start_number", "2") {
		t.Fatalf("bounded HLS sample seek lost its global timeline: %v", args)
	}
	// The outward-rounded source duration must never recreate a phantom sample.
	p.EndTicks, p.StartTicks, p.AudioSampleRate = p.DurationTicks, 0, p.AudioSourceSampleRate
	start, end, samples, err := audioSampleSeekWindow(p, p.AudioSampleRate)
	if err != nil || start != 0 || end != 289792 || samples != 289792 {
		t.Fatalf("whole source window: %d..%d -> %d, %v", start, end, samples, err)
	}
	p.DurationTicks, p.EndTicks = maxDurationTicks, 0
	p.AudioSourceSampleRate = 768000
	p.AudioSourceSampleCount = maxDurationTicks / ticksPerSecond * 768000
	_, end, samples, err = audioSampleSeekWindow(p, 96000)
	if err != nil || end != p.AudioSourceSampleCount || samples != maxDurationTicks/ticksPerSecond*96000 {
		t.Fatalf("maximum bounded sample rescale: %d -> %d, %v", end, samples, err)
	}
}

func TestAudioSampleSeekRejectsUnprovenOrIncompatiblePlans(t *testing.T) {
	for _, mode := range []string{"progressive", ""} {
		p := progressivePlan("wav", "pcm_s16le")
		p.AudioSampleSeek, p.AudioSourceSampleCount = true, 192000
		if mode == "" {
			p.OutputMode, p.Container, p.AudioCodec = "", "ts", "aac"
			p.SegmentMode, p.SegmentSeconds = "vod", 3
			p.AudioBitrate = 96000
		}
		for name, mutate := range map[string]func(*Plan){
			"unknown samples":  func(p *Plan) { p.AudioSourceSampleCount = 0 },
			"unknown rate":     func(p *Plan) { p.AudioSourceSampleRate = 0 },
			"negative samples": func(p *Plan) { p.AudioSourceSampleCount = -1 },
			"overflow samples": func(p *Plan) { p.AudioSourceSampleCount = math.MaxInt64 },
			"excess rate":      func(p *Plan) { p.AudioSourceSampleRate = 768001 },
			"copy": func(p *Plan) {
				p.AudioCodec, p.AudioChannels, p.AudioSampleRate, p.AudioBitrate = "copy", 0, 0, 0
			},
			"video":               func(p *Plan) { p.VideoStreamIndex, p.VideoCodec = 1, "copy" },
			"hardware":            func(p *Plan) { p.Hardware.Decode = "vaapi" },
			"reference origin":    func(p *Plan) { p.ReferenceStartTicks = 1 },
			"empty sample window": func(p *Plan) { p.StartTicks, p.EndTicks = 1, 2 },
		} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				q := p
				mutate(&q)
				if err := ValidatePlan(q); !errors.Is(err, ErrInvalidPlan) {
					t.Fatalf("unproven sample seek accepted: %+v, %v", q, err)
				}
			})
		}
	}
}
