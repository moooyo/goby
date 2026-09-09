package transcode

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func progressivePlan(container, codec string) Plan {
	p := Plan{OutputMode: "progressive", Container: container, AudioCodec: codec, VideoStreamIndex: -1, AudioStreamIndex: 0, DurationTicks: 4 * ticksPerSecond, AudioChannels: 2, AudioSampleRate: 48000, AudioBitrate: 96000, AudioSourceSampleRate: 48000}
	if codec == "flac" || codec == "pcm_s16le" {
		p.AudioBitrate = 0
	}
	if codec == "flac" {
		p.AudioBitDepth = 16
	}
	if codec == "copy" {
		p.AudioChannels, p.AudioSampleRate, p.AudioBitrate = 0, 0, 0
	}
	return p
}

func TestProgressiveOutputSamplesUsesExactSourceInteger(t *testing.T) {
	p := progressivePlan("wav", "pcm_s16le")
	p.AudioSourceSampleCount = 289792
	p.DurationTicks = 60373334
	for _, c := range []struct {
		start int64
		rate  int
		want  int64
	}{{0, 48000, 289792}, {1, 48000, 289791}, {12345, 44100, 266192}} {
		p.StartTicks = c.start
		n, err := ProgressiveOutputSamples(p, c.rate)
		if err != nil || n != c.want {
			t.Fatalf("start=%d rate=%d: %d, %v; want %d", c.start, c.rate, n, err, c.want)
		}
	}
	p.StartTicks = 0
	p.AudioSourceSampleCount = 0
	if n, err := ProgressiveOutputSamples(p, 48000); err != nil || n != 289793 {
		t.Fatalf("legacy fallback: %d, %v", n, err)
	}
	p.AudioSourceSampleRate = 44100
	p.AudioSourceSampleCount = 1
	p.DurationTicks = 227
	if n, err := ProgressiveOutputSamples(p, 44100); err != nil || n != 1 {
		t.Fatalf("single outward-rounded sample: %d, %v", n, err)
	}
	p.DurationTicks = 10 * ticksPerSecond
	p.AudioSourceSampleRate = 48000
	p.AudioSourceSampleCount = 48000
	if n, err := ProgressiveOutputSamples(p, 48000); err != nil || n != 48000 {
		t.Fatalf("shorter selected stream: %d, %v", n, err)
	}
	for name, change := range map[string]func(*Plan){
		"negative":                  func(p *Plan) { p.AudioSourceSampleCount = -1 },
		"overflow":                  func(p *Plan) { p.AudioSourceSampleCount = math.MaxInt64 },
		"missing rate":              func(p *Plan) { p.AudioSourceSampleRate = 0 },
		"count exceeds duration":    func(p *Plan) { p.AudioSourceSampleCount = 480001 },
		"selected stream exhausted": func(p *Plan) { p.StartTicks = ticksPerSecond },
	} {
		t.Run(name, func(t *testing.T) {
			q := p
			change(&q)
			if _, err := ProgressiveOutputSamples(q, 48000); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("got %v", err)
			}
		})
	}
	p.DurationTicks = maxDurationTicks
	p.AudioSourceSampleRate = 768000
	p.AudioSourceSampleCount = maxDurationTicks / ticksPerSecond * 768000
	p.StartTicks = 0
	if n, err := ProgressiveOutputSamples(p, 768000); err != nil || n != p.AudioSourceSampleCount {
		t.Fatalf("large safe rescale: %d, %v", n, err)
	}
	hls := commandPlan()
	hls.AudioSourceSampleCount = 1
	if err := ValidatePlan(hls); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("HLS accepted audio sample count: %v", err)
	}
}

func TestProgressiveCommandClosedMatrixAndPipeOutput(t *testing.T) {
	for _, pair := range [][2]string{{"mp3", "mp3"}, {"aac", "aac"}, {"flac", "flac"}, {"ogg", "vorbis"}, {"ogg", "opus"}, {"ogg", "flac"}, {"wav", "pcm_s16le"}, {"m4a", "aac"}} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			p := progressivePlan(pair[0], pair[1])
			args, err := BuildArgs(p, 1)
			if err != nil {
				t.Fatal(err)
			}
			if args[len(args)-1] != "pipe:4" || !hasArgumentPair(args, "-i", "/proc/self/fd/3") || !hasArgumentPair(args, "-map", "0:0") {
				t.Fatalf("unanchored progressive output: %v", args)
			}
			if !hasArgumentPair(args, "-af", "aresample=48000,atrim=end_sample=192000,asetpts=N/SR/TB") {
				t.Fatalf("sample bound missing: %v", args)
			}
			if strings.Contains(strings.Join(args, " "), "main.m3u8") {
				t.Fatal("progressive output must not create a playlist")
			}
		})
	}
	for name, change := range map[string]func(*Plan){
		"video":             func(p *Plan) { p.VideoStreamIndex = 1; p.VideoCodec = "h264" },
		"hardware":          func(p *Plan) { p.Hardware.Decode = "vaapi" },
		"segment":           func(p *Plan) { p.SegmentSeconds = 3 },
		"segment mode":      func(p *Plan) { p.SegmentMode = "vod" },
		"end":               func(p *Plan) { p.EndTicks = p.DurationTicks },
		"unsupported codec": func(p *Plan) { p.AudioCodec = "vorbis" },
		"CBR rounding":      func(p *Plan) { p.AudioBitrate = 97000 },
		"low rate bitrate":  func(p *Plan) { p.AudioSampleRate = 8000; p.AudioBitrate = 96000 },
		"unknown mode":      func(p *Plan) { p.OutputMode = "file" },
	} {
		t.Run(name, func(t *testing.T) {
			p := progressivePlan("mp3", "mp3")
			change(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("got %v", err)
			}
		})
	}
	for _, p := range []Plan{
		func() Plan { p := progressivePlan("flac", "flac"); p.AudioBitDepth = 0; return p }(),
		func() Plan { p := progressivePlan("flac", "flac"); p.AudioBitDepth = 20; return p }(),
		func() Plan { p := progressivePlan("flac", "copy"); p.StartTicks = ticksPerSecond; return p }(),
		func() Plan { p := progressivePlan("ogg", "vorbis"); p.AudioSampleRate = 96000; return p }(),
		func() Plan { p := progressivePlan("aac", "aac"); p.AudioChannels = 7; return p }(),
	} {
		if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("invalid progressive plan accepted: %+v: %v", p, err)
		}
	}
}
