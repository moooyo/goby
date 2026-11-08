package transcode

import (
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestNativeCPUQualityPresetAndCRFBoundaries(t *testing.T) {
	for _, codec := range []string{"h264", "hevc"} {
		presets := []string{"superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow"}
		if codec == "hevc" {
			presets = append(presets, "ultrafast", "placebo")
		}
		for _, preset := range presets {
			for _, crf := range []int{1, 51} {
				for _, threads := range []int{0, 16} {
					value := CPUQuality{Preset: preset, RateControl: "capped_crf", CRF: crf, ThreadCount: threads, ThreadCountSet: true}
					if err := ValidateCPUQuality(codec, value); err != nil {
						t.Fatalf("native %s boundary rejected: %+v: %v", codec, value, err)
					}
				}
			}
		}
	}
}

func TestNativeCPUThreadsAndX264OptionsReachEveryVideoOutput(t *testing.T) {
	for _, output := range []string{"legacy-hls", "generated-mpegts", "generated-fmp4", "adaptive", "progressive"} {
		for _, codec := range []string{"h264", "hevc"} {
			for _, threads := range []int{0, 1, 16} {
				t.Run(output+"/"+codec+"/"+strconv.Itoa(threads), func(t *testing.T) {
					plan := executionCommandPlan(output)
					plan.VideoCodec = codec
					options := DefaultExecutionOptions(3)
					options.H264.ThreadCount, options.H264.ThreadCountSet = threads, true
					options.HEVC.ThreadCount, options.HEVC.ThreadCountSet = threads, true
					options.H264.X264Options = "subme=0:me_range=4:rc_lookahead=10:partitions=none"
					plan, err := CaptureExecution(plan, options)
					if err != nil {
						t.Fatal(err)
					}
					args, err := BuildArgs(plan, 64)
					if err != nil {
						t.Fatal(err)
					}
					count := max(1, plan.HLS.RenditionCount)
					if countExecutionArgumentPair(args, "-threads:v", strconv.Itoa(threads)) != count ||
						!hasArgumentPair(args, "-filter_threads", "3") || !hasArgumentPair(args, "-threads:a", "3") {
						t.Fatalf("per-codec threads changed decode, filter or audio limits: %v", args)
					}
					if codec == "h264" {
						if countExecutionArgumentPair(args, "-x264-params", "me-range=4:partitions=none:rc-lookahead=10:subme=0") != count {
							t.Fatalf("safe native x264 options were not canonicalized and passed: %v", args)
						}
					} else {
						if slices.Contains(args, "-x264-params") {
							t.Fatal("x264 options leaked to HEVC")
						}
						for index, arg := range args {
							if arg != "-x265-params" {
								continue
							}
							parameters := args[index+1]
							if threads == 0 && (strings.Contains(parameters, "pools=") || strings.Contains(parameters, "frame-threads=") || strings.Contains(parameters, "wpp=")) ||
								threads > 0 && !strings.Contains(parameters, "frame-threads="+strconv.Itoa(threads)) {
								t.Fatalf("HEVC automatic/manual thread choice was changed: %s", parameters)
							}
						}
					}
				})
			}
		}
	}
}

func TestExecutionVersionOneRetainsJSONAndCommandIdentity(t *testing.T) {
	plan := commandPlan()
	legacyArgs, err := BuildArgs(plan, 3)
	if err != nil {
		t.Fatal(err)
	}
	plan.ExecutionVersion, plan.Execution = LegacyExecutionVersion, legacyExecutionDefaults(3)
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ThreadCount", "X264Options", "DownMixAudioBoost", "SourceAudioChannels", "SoftwareToneMapOptions"} {
		if strings.Contains(string(encoded), field) {
			t.Fatalf("version one acquired a new identity field %s: %s", field, encoded)
		}
	}
	var restored Plan
	if err := json.Unmarshal(encoded, &restored); err != nil || restored != plan {
		t.Fatal("version one did not round trip unchanged")
	}
	args, err := BuildArgs(restored, 64)
	if err != nil || !slices.Equal(args, legacyArgs) {
		t.Fatalf("version one command identity changed: %v: %v", args, err)
	}
	for name, mutate := range map[string]func(*Plan){
		"new preset":      func(p *Plan) { p.Execution.H264.Preset = "faster" },
		"new CRF":         func(p *Plan) { p.Execution.H264.CRF = 1 },
		"codec threads":   func(p *Plan) { p.Execution.H264.ThreadCountSet = true },
		"x264 options":    func(p *Plan) { p.Execution.H264.X264Options = "subme=0" },
		"boost":           func(p *Plan) { p.Execution.DownMixAudioBoost = 2 },
		"source channels": func(p *Plan) { p.SourceAudioChannels = 6 },
		"tone algorithm":  func(p *Plan) { p.Execution.SoftwareToneMapOptions = DefaultSoftwareToneMapOptions() },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := plan
			mutate(&invalid)
			if !errors.Is(ValidatePlan(invalid), ErrInvalidPlan) {
				t.Fatal("new execution field was admitted into a historical version")
			}
		})
	}
}

func TestDownMixBoostRequiresActualSurroundToStereoEncoding(t *testing.T) {
	for _, output := range []string{"legacy-hls", "generated-fmp4", "adaptive", "progressive", "audio"} {
		t.Run(output, func(t *testing.T) {
			plan := executionCommandPlan(output)
			if output == "audio" {
				plan = progressivePlan("wav", "pcm_s16le")
			}
			plan.SourceAudioChannels = 6
			options := DefaultExecutionOptions(2)
			options.DownMixAudioBoost = 2
			boosted, err := CaptureExecution(plan, options)
			if err != nil || boosted.SourceAudioChannels != 6 || boosted.Execution.DownMixAudioBoost != 2 {
				t.Fatalf("actual downmix was not captured: %+v: %v", boosted, err)
			}
			args, err := BuildArgs(boosted, 1)
			if err != nil {
				t.Fatal(err)
			}
			count := max(1, plan.HLS.RenditionCount)
			filters := 0
			for index, arg := range args {
				if arg == "-af" && strings.Contains(args[index+1], "ochl=stereo:rematrix_volume=2") &&
					!strings.Contains(args[index+1], ",volume=") && !strings.HasPrefix(args[index+1], "volume=") {
					filters++
				}
			}
			if filters != count {
				t.Fatalf("downmix gain missing from an actual audio output: %v", args)
			}
			for _, channels := range []int{0, 1, 2} {
				unchanged := plan
				unchanged.SourceAudioChannels = channels
				captured, err := CaptureExecution(unchanged, options)
				if err != nil || captured.SourceAudioChannels != 0 || captured.Execution.DownMixAudioBoost != 0 {
					t.Fatal("unknown, mono or stereo source acquired a boost")
				}
			}
			for _, channels := range []int{1, 6} {
				unchanged := plan
				unchanged.AudioChannels = channels
				captured, err := CaptureExecution(unchanged, options)
				if err != nil || captured.SourceAudioChannels != 0 || captured.Execution.DownMixAudioBoost != 0 {
					t.Fatal("non-stereo output acquired a boost")
				}
			}
			options.DownMixAudioBoost = 1
			captured, err := CaptureExecution(plan, options)
			if err != nil || captured.SourceAudioChannels != 0 || captured.Execution.DownMixAudioBoost != 0 {
				t.Fatal("unity gain split an otherwise identical output")
			}
		})
	}
	copy := commandPlan()
	copy.SourceAudioChannels, copy.AudioCodec = 6, "copy"
	copy.AudioChannels, copy.AudioBitrate, copy.AudioSampleRate = 0, 0, 0
	options := DefaultExecutionOptions(2)
	options.DownMixAudioBoost = 2
	captured, err := CaptureExecution(copy, options)
	if err != nil || captured.SourceAudioChannels != 0 || captured.Execution.DownMixAudioBoost != 0 {
		t.Fatal("audio copy acquired a gain filter")
	}
}

func TestDownMixBoostPreservesAudioSampleSeekFilter(t *testing.T) {
	plan := progressivePlan("wav", "pcm_s16le")
	plan.SourceAudioChannels, plan.AudioSampleSeek, plan.AudioSourceSampleCount = 6, true, 192000
	plan.StartTicks = ticksPerSecond / 8
	original := audioSampleSeekFilter(plan, plan.AudioSampleRate)
	options := DefaultExecutionOptions(2)
	options.DownMixAudioBoost = 2
	captured, err := CaptureExecution(plan, options)
	if err != nil {
		t.Fatal(err)
	}
	args, err := BuildArgs(captured, 1)
	expected := strings.Replace(original, "aresample=48000", "aresample=48000:ochl=stereo:rematrix_volume=2:tsf=fltp", 1)
	if err != nil || !hasArgumentPair(args, "-af", expected) {
		t.Fatalf("downmix gain replaced the exact source-sample trim or resample clock: %v: %v", args, err)
	}
}
