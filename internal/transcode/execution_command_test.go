package transcode

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func executionCommandPlan(output string) Plan {
	if output == "progressive" {
		return progressiveVideoPlan()
	}
	if output == "adaptive" {
		return adaptiveHLSPlan()
	}
	p := commandPlan()
	switch output {
	case "generated-mpegts":
		p.HLS.SegmentType = "mpegts"
	case "generated-fmp4":
		p.Container, p.HLS.SegmentType = "mp4", "fmp4"
	}
	return p
}

func TestExecutionCPUQualityUsesSelectedCodecAcrossOutputs(t *testing.T) {
	for _, output := range []string{"legacy-hls", "generated-mpegts", "generated-fmp4", "adaptive", "progressive"} {
		for _, codec := range []string{"h264", "hevc"} {
			for _, preset := range []string{"veryfast", "fast", "medium", "slow"} {
				for _, rateControl := range []string{"bitrate", "capped_crf"} {
					t.Run(output+"/"+codec+"/"+preset+"/"+rateControl, func(t *testing.T) {
						p := executionCommandPlan(output)
						p.VideoCodec = codec
						legacy, err := BuildArgs(p, 3)
						if err != nil {
							t.Fatal(err)
						}
						options := DefaultExecutionOptions(3)
						options.H264 = CPUQuality{Preset: preset, RateControl: rateControl, CRF: 20}
						options.HEVC = CPUQuality{Preset: preset, RateControl: rateControl, CRF: 31}
						p, err = CaptureExecution(p, options)
						if err != nil {
							t.Fatal(err)
						}
						// A later caller's thread setting must not reinterpret this job.
						args, err := BuildArgs(p, 1)
						if err != nil {
							t.Fatal(err)
						}
						bitrates := []int64{p.VideoBitrate}
						if p.HLS.RenditionCount > 0 {
							bitrates = nil
							for _, rendition := range p.HLS.Renditions[:p.HLS.RenditionCount] {
								bitrates = append(bitrates, rendition.VideoBitrate)
							}
						}
						if countExecutionArgumentPair(args, "-preset", preset) != len(bitrates) ||
							countExecutionArgumentPair(args, "-c:v", VideoEncoder(codec, "software")) != len(bitrates) ||
							countExecutionArgumentPair(args, "-threads:v", "3") != len(bitrates) {
							t.Fatalf("selected CPU encoder, preset, or captured threads missing: %v", args)
						}
						crf := 20
						if codec == "hevc" {
							crf = 31
						}
						for _, bitrate := range bitrates {
							assertExecutionRateArguments(t, args, rateControl, bitrate, crf)
						}
						if !slices.Equal(withoutExecutionQualityArgs(legacy), withoutExecutionQualityArgs(args)) {
							t.Fatalf("CPU quality changed unrelated timing, mapping, or codec options:\nlegacy: %v\nactual: %v", legacy, args)
						}
					})
				}
			}
		}
	}
}

func TestExecutionDefaultQualityPreservesLegacyArguments(t *testing.T) {
	for _, output := range []string{"legacy-hls", "generated-mpegts", "generated-fmp4", "adaptive", "progressive"} {
		for _, codec := range []string{"h264", "hevc", "av1"} {
			if codec == "av1" && (output == "legacy-hls" || output == "generated-mpegts") {
				continue
			}
			for _, bitrate := range []int64{0, 256000} {
				if output == "adaptive" && bitrate == 0 {
					continue
				}
				t.Run(output+"/"+codec+"/"+strconv.FormatInt(bitrate, 10), func(t *testing.T) {
					p := executionCommandPlan(output)
					p.VideoCodec, p.VideoBitrate = codec, bitrate
					legacy, err := BuildArgs(p, 3)
					if err != nil {
						t.Fatal(err)
					}
					p, err = CaptureExecution(p, DefaultExecutionOptions(3))
					if err != nil {
						t.Fatal(err)
					}
					args, err := BuildArgs(p, 1)
					if err != nil || !slices.Equal(legacy, args) {
						t.Fatalf("captured defaults changed historical arguments:\nlegacy: %v\nactual: %v\nerror: %v", legacy, args, err)
					}
					assertExecutionRateArguments(t, args, "bitrate", bitrate, 0)
				})
			}
		}
	}
}

func TestExecutionCPUQualityDoesNotChangeAV1OrHardwareAlgorithms(t *testing.T) {
	for _, tc := range []struct{ codec, backend string }{
		{"av1", "software"}, {"av1", "vaapi"}, {"h264", "vaapi"}, {"hevc", "vaapi"}, {"h264", "qsv"}, {"h264", "nvenc"},
	} {
		for _, output := range []string{"legacy-hls", "generated-mpegts", "generated-fmp4", "adaptive", "progressive"} {
			if tc.codec == "av1" && (output == "legacy-hls" || output == "generated-mpegts") {
				continue
			}
			t.Run(tc.codec+"/"+tc.backend+"/"+output, func(t *testing.T) {
				p := executionCommandPlan(output)
				p.VideoCodec, p.Hardware.Encode = tc.codec, tc.backend
				legacy, err := BuildArgs(p, 2)
				if err != nil {
					t.Fatal(err)
				}
				options := DefaultExecutionOptions(2)
				options.H264 = CPUQuality{Preset: "slow", RateControl: "capped_crf", CRF: 18}
				options.HEVC = CPUQuality{Preset: "medium", RateControl: "capped_crf", CRF: 35}
				p, err = CaptureExecution(p, options)
				if err != nil {
					t.Fatal(err)
				}
				args, err := BuildArgs(p, 1)
				if err != nil || !slices.Equal(legacy, args) {
					t.Fatalf("CPU settings changed the AV1 or hardware algorithm:\nlegacy: %v\nactual: %v\nerror: %v", legacy, args, err)
				}
				for _, forbidden := range []string{"-preset", "-crf", "-x265-params", "-sc_threshold"} {
					if slices.Contains(args, forbidden) {
						t.Fatalf("CPU option %s leaked to %s/%s: %v", forbidden, tc.codec, tc.backend, args)
					}
				}
				assertExecutionRateArguments(t, args, "bitrate", p.VideoBitrate, 0)
			})
		}
	}
}

func assertExecutionRateArguments(t *testing.T, args []string, mode string, bitrate int64, crf int) {
	t.Helper()
	if bitrate == 0 {
		bitrate = 4000000
	}
	want := []string{"-b:v", strconv.FormatInt(bitrate, 10)}
	forbidden := "-crf"
	if mode == "capped_crf" {
		want, forbidden = []string{"-crf", strconv.Itoa(crf)}, "-b:v"
	}
	want = append(want, "-maxrate", strconv.FormatInt(bitrate, 10), "-bufsize", strconv.FormatInt(2*bitrate, 10))
	if !strings.Contains(strings.Join(args, " "), strings.Join(want, " ")) || slices.Contains(args, forbidden) {
		t.Fatalf("rate control must preserve the ordered bitrate cap without %s: want %v in %v", forbidden, want, args)
	}
}

func countExecutionArgumentPair(args []string, key, value string) int {
	count := 0
	for index := 0; index+1 < len(args); index++ {
		if args[index] == key && args[index+1] == value {
			count++
		}
	}
	return count
}

func withoutExecutionQualityArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		if args[index] == "-preset" || args[index] == "-b:v" || args[index] == "-crf" {
			index++
			continue
		}
		filtered = append(filtered, args[index])
	}
	return filtered
}
