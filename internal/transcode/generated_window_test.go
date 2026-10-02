package transcode

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"slices"
	"strings"
	"testing"
)

func generatedWindowTestPlan() Plan {
	p := adaptiveHLSPlan()
	p.DurationTicks, p.StartTicks = 120*ticksPerSecond, 17*ticksPerSecond+13
	p.HLS.Window = HLSWindow{EndTicks: 24*ticksPerSecond + 19, StartNumber: 71}
	return p
}

func TestGeneratedWindowKeepsSourceFactsAndScopedClocks(t *testing.T) {
	p := generatedWindowTestPlan()
	args, err := BuildArgs(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.DurationTicks != 120*ticksPerSecond || !hasArgumentPair(args, "-ss", "17.0000013") ||
		strings.Count(strings.Join(args, " "), "-t 7.0000006") != 2 ||
		strings.Count(strings.Join(args, " "), "-start_number 71") != 2 ||
		!hasArgumentPair(args, "-stats_mux_pre:v:0", "pipe:4") || !hasArgumentPair(args, "-stats_mux_pre:v:0", "pipe:5") ||
		strings.Count(strings.Join(args, " "), "temp_file+independent_segments+discont_start") != 2 {
		t.Fatalf("window lost its source facts, epoch or rendition clocks: %v", args)
	}
	q := p
	q.HLS.Window.StartNumber++
	if q == p || q.DurationTicks != p.DurationTicks || q.StartTicks != p.StartTicks {
		t.Fatal("job namespace offset is not an independent comparable field")
	}
}

func TestGeneratedWindowRejectsMixedModesAndUnboundedRequests(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"missing end":       func(p *Plan) { p.HLS.Window.EndTicks = 0 },
		"end at start":      func(p *Plan) { p.HLS.Window.EndTicks = p.StartTicks },
		"end before start":  func(p *Plan) { p.HLS.Window.EndTicks = p.StartTicks - 1 },
		"end beyond source": func(p *Plan) { p.HLS.Window.EndTicks = p.DurationTicks + 1 },
		"negative number":   func(p *Plan) { p.HLS.Window.StartNumber = -1 },
		"number limit":      func(p *Plan) { p.HLS.Window.StartNumber = MaxPlaylistSegments },
		"nominal budget":    func(p *Plan) { p.HLS.Window.StartNumber = MaxPlaylistSegments - 1 },
		"live":              func(p *Plan) { p.SourceMode, p.StartTicks, p.DurationTicks = "stream", 0, 0 },
		"progressive":       func(p *Plan) { p.OutputMode = "progressive" },
		"legacy mode":       func(p *Plan) { p.SegmentMode = "vod" },
		"legacy end":        func(p *Plan) { p.EndTicks = p.HLS.Window.EndTicks },
		"legacy number":     func(p *Plan) { p.SegmentStartNumber = 1 },
		"legacy cuts":       func(p *Plan) { p.SegmentTimes = "200000000" },
		"legacy reference":  func(p *Plan) { p.ReferenceStartTicks = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			p := generatedWindowTestPlan()
			mutate(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("invalid window accepted: %+v: %v", p.HLS.Window, err)
			}
		})
	}
}

func TestGeneratedWindowZeroPreservesHistoricalOutput(t *testing.T) {
	p := adaptiveHLSPlan()
	p.DurationTicks, p.StartTicks = 120*ticksPerSecond, 17*ticksPerSecond
	args, err := BuildArgs(p, 1)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !hasArgumentPair(args, "-t", "103.0000000") || !hasArgumentPair(args, "-start_number", "0") ||
		strings.Contains(joined, "-stats_mux_pre") || strings.Contains(joined, "discont_start") || needsHLSClock(p) {
		t.Fatalf("zero window changed the historical command: %v", args)
	}
	encoded, err := json.Marshal(p.HLS)
	if err != nil || strings.Contains(string(encoded), "Window") {
		t.Fatalf("zero window changed serialized preparation data: %s: %v", encoded, err)
	}
	p = Plan{Container: "aac", HLS: HLSPlan{SegmentType: "packed"}, VideoStreamIndex: -1, AudioStreamIndex: 0,
		AudioCodec: "aac", DurationTicks: 20 * ticksPerSecond, SegmentSeconds: 3}
	args, err = BuildArgs(p, 1)
	if err != nil || slices.Contains(args, "-segment_start_number") || slices.Contains(args, "-stats_mux_pre:a:0") {
		t.Fatalf("zero packed window changed the historical command: %v: %v", args, err)
	}
}

func TestGeneratedWindowCopyObservesOneNativePacket(t *testing.T) {
	for _, video := range []bool{false, true} {
		p := Plan{Container: "mp4", HLS: HLSPlan{SegmentType: "fmp4", Window: HLSWindow{EndTicks: 25 * ticksPerSecond, StartNumber: 90}},
			VideoStreamIndex: -1, AudioStreamIndex: 1, AudioCodec: "copy", StartTicks: 20 * ticksPerSecond,
			DurationTicks: 120 * ticksPerSecond, SegmentSeconds: 3}
		if video {
			p.VideoStreamIndex, p.VideoCodec = 0, "copy"
		}
		args, err := BuildArgs(p, 1)
		if err != nil || !needsHLSCopyClock(p) || !hasArgumentPair(args, "-start_number", "90") ||
			!hasArgumentPair(args, "-t", "5.0000000") || !hasArgumentPair(args, "-f", "framehash") ||
			strings.Count(strings.Join(args, " "), "-i /proc/self/fd/3") != 1 {
			t.Fatalf("copy window lost its bounded native packet observer: %v: %v", args, err)
		}
		if video && !hasArgumentPair(args, "-frames:v:0", "1") || !video && !hasArgumentPair(args, "-frames:a:0", "1") {
			t.Fatalf("copy observer is not scoped to the reference stream: %v", args)
		}
	}
}

func generatedWindowSamplePlan(start, end int64) Plan {
	return Plan{Container: "aac", HLS: HLSPlan{SegmentType: "packed", Window: HLSWindow{EndTicks: end, StartNumber: 83}},
		VideoStreamIndex: -1, AudioStreamIndex: 0, AudioCodec: "aac", AudioSampleRate: 48000,
		AudioSampleSeek: true, AudioSourceSampleRate: 44100, AudioSourceSampleCount: 441000,
		StartTicks: start, DurationTicks: 10 * ticksPerSecond, SegmentSeconds: 3}
}

func generatedWindowSampleBoundary(index int64, rate int) int64 {
	if index == 0 {
		return 0
	}
	return (index-1)*ticksPerSecond/int64(rate) + 1
}

func TestGeneratedWindowSampleRangesUseAbsoluteResampledIndexes(t *testing.T) {
	startTicks, endTicks := 2*ticksPerSecond+1, generatedWindowSampleBoundary(220854, 44100)
	p := generatedWindowSamplePlan(startTicks, endTicks)
	start, end, samples, err := audioSampleSeekWindow(p, 48000)
	if err != nil || start != 88201 || end != 220854 || samples != 144384 {
		t.Fatalf("absolute sample window: %d..%d -> %d: %v", start, end, samples, err)
	}
	args, err := BuildArgs(p, 1)
	if err != nil || slices.Contains(args, "-ss") || slices.Contains(args, "-t") ||
		!hasArgumentPair(args, "-segment_start_number", "83") || !hasArgumentPair(args, "-stats_mux_pre:a:0", "pipe:4") ||
		!hasArgumentPair(args, "-af", "atrim=start_sample=88201:end_sample=220854,asetpts=N/SR/TB,aresample=48000,atrim=end_sample=144384,asetpts=N/SR/TB") {
		t.Fatalf("sample window lost absolute bounds or introduced a time trim: %v: %v", args, err)
	}
	boundary := generatedWindowSampleBoundary(132325, 44100)
	first, second := p, p
	first.HLS.Window.EndTicks, second.StartTicks = boundary, boundary
	_, _, left, leftErr := audioSampleSeekWindow(first, 48000)
	_, _, right, rightErr := audioSampleSeekWindow(second, 48000)
	if leftErr != nil || rightErr != nil || left+right != samples {
		t.Fatalf("adjacent sample ranges accumulated rounding: %d + %d != %d: %v, %v", left, right, samples, leftErr, rightErr)
	}
	p.HLS.Window.EndTicks = p.DurationTicks
	_, end, _, err = audioSampleSeekWindow(p, 48000)
	if err != nil || end != p.AudioSourceSampleCount {
		t.Fatalf("window recreated a sample beyond the full-source count: %d: %v", end, err)
	}
}

func TestGeneratedWindowRejectsEmptyResampledSampleRange(t *testing.T) {
	p := generatedWindowSamplePlan(generatedWindowSampleBoundary(12, 48000), generatedWindowSampleBoundary(13, 48000))
	p.AudioSourceSampleRate, p.AudioSourceSampleCount, p.AudioSampleRate = 48000, 480000, 44100
	if _, _, _, err := audioSampleSeekWindow(p, 44100); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("empty output sample interval was accepted: %v", err)
	}
}

func TestGeneratedWindowSourceAnchorRetainsRationalSourceEpoch(t *testing.T) {
	p := generatedWindowTestPlan()
	clock := HLSMuxClock{Rendition: 1, PTS: 1, TimeBaseNumerator: 1, TimeBaseDenominator: 90000}
	anchor, err := HLSWindowSourceAnchorTicks(p, clock)
	want := new(big.Rat).Add(new(big.Rat).SetInt64(p.StartTicks), big.NewRat(ticksPerSecond, 90000))
	if err != nil || anchor.Cmp(want) != 0 || anchor.IsInt() {
		t.Fatalf("rational video source anchor = %v: %v", anchor, err)
	}
	p = generatedWindowSamplePlan(2*ticksPerSecond+1, generatedWindowSampleBoundary(220854, 44100))
	clock = HLSMuxClock{PTS: -1024, TimeBaseNumerator: 1, TimeBaseDenominator: 48000}
	anchor, err = HLSWindowSourceAnchorTicks(p, clock)
	want = new(big.Rat).Add(big.NewRat(88201*ticksPerSecond, 44100), big.NewRat(-1024*ticksPerSecond, 48000))
	requested := new(big.Rat).Add(new(big.Rat).SetInt64(p.StartTicks), big.NewRat(-1024*ticksPerSecond, 48000))
	if err != nil || anchor.Cmp(want) != 0 || anchor.Cmp(requested) == 0 {
		t.Fatalf("sample anchor inherited the fractional request instead of the source sample: %v: %v", anchor, err)
	}
	for _, invalid := range []HLSMuxClock{{Rendition: 1, TimeBaseNumerator: 1, TimeBaseDenominator: 48000},
		{TimeBaseNumerator: 1}, {PTS: math.MaxInt64, TimeBaseNumerator: 1, TimeBaseDenominator: 1}} {
		if _, err := HLSWindowSourceAnchorTicks(p, invalid); err == nil {
			t.Fatalf("invalid source anchor evidence accepted: %+v", invalid)
		}
	}
	p.HLS.Window = HLSWindow{}
	if _, err := HLSWindowSourceAnchorTicks(p, clock); !errors.Is(err, ErrInvalidTimeline) {
		t.Fatalf("a historical producer acquired window evidence: %v", err)
	}
}
