package transcode

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
)

func progressiveVideoPlan() Plan {
	return Plan{OutputMode: "progressive", Container: "mp4", VideoStreamIndex: 0, AudioStreamIndex: 1,
		VideoCodec: "h264", AudioCodec: "aac", DurationTicks: 12 * ticksPerSecond, SourceFormatStartKnown: true,
		VideoBitrate: 256000, Width: 160, Height: 90, AudioBitrate: 96000, AudioChannels: 2, AudioSampleRate: 48000}
}

func TestProgressiveVideoArgumentsPreserveSourceClockAndFixedOutput(t *testing.T) {
	p := progressiveVideoPlan()
	p.SourceFormatStartTicks, p.StartTicks = 14620000, 23700000
	args, err := BuildArgs(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-itsoffset", "-1.4620000"}, {"-i", "/proc/self/fd/3"}, {"-ss", "2.3700000"}, {"-t", "9.6300000"},
		{"-map", "0:0"}, {"-map", "0:1"}, {"-protocol_whitelist", "file,pipe"}, {"-map_metadata:s:v", "-1"}, {"-map_metadata:s:a", "-1"},
		{"-tag:v", "avc1"}, {"-tag:a", "mp4a"}, {"-c:v", "libx264"}, {"-profile:a", "aac_low"}, {"-threads:v", "2"}, {"-threads:a", "2"},
		{"-vf", "scale=w=160:h=90,format=yuv420p"}, {"-enc_time_base:v", "demux"}, {"-fps_mode", "passthrough"},
		{"-avoid_negative_ts", "disabled"}, {"-f", "mp4"}, {"-frag_duration", "1000000"}, {"-frag_size", "1048576"},
		{"-movflags", "+empty_moov+delay_moov+default_base_moof+skip_trailer"}} {
		if !hasArgumentPair(args, pair[0], pair[1]) {
			t.Errorf("missing %s %s in %v", pair[0], pair[1], args)
		}
	}
	if !slices.Contains(args, "-copyts") || slices.Index(args, "-ss") < slices.Index(args, "-i") || args[len(args)-1] != "pipe:4" {
		t.Fatalf("source clock or fixed output lost: %v", args)
	}
	for _, forbidden := range []string{"-r", "-af", "-vn", "-start_at_zero", "-hls_flags"} {
		if slices.Contains(args, forbidden) {
			t.Errorf("unexpected video argument: %s", forbidden)
		}
	}
	if strings.Contains(strings.Join(args, " "), "negative_cts_offsets") || strings.Contains(strings.Join(args, " "), "asetpts") {
		t.Fatal("the pipeline must not rewrite the shared audio/video origin")
	}
	p.SourceFormatStartTicks = -1234567
	args, err = BuildArgs(p, 1)
	if err != nil || !hasArgumentPair(args, "-itsoffset", "0.1234567") {
		t.Fatalf("negative source origin: %v, %v", args, err)
	}
}

func TestProgressiveVideoCopyAndOptionalAudioMatrix(t *testing.T) {
	for _, video := range []string{"copy", "h264"} {
		for _, audio := range []string{"copy", "aac", ""} {
			p := progressiveVideoPlan()
			p.VideoCodec, p.AudioCodec = video, audio
			if video == "copy" {
				p.Width, p.Height, p.VideoBitrate = 0, 0, 0
			}
			if audio != "aac" {
				p.AudioBitrate, p.AudioChannels, p.AudioSampleRate = 0, 0, 0
			}
			if audio == "" {
				p.AudioStreamIndex = -1
			}
			args, err := BuildArgs(p, 1)
			if err != nil {
				t.Fatalf("%s/%s: %v", video, audio, err)
			}
			if (video == "copy") != hasArgumentPair(args, "-c:v", "copy") || (audio == "") != slices.Contains(args, "-an") {
				t.Fatalf("incorrect stream mapping: %v", args)
			}
			if (audio == "copy") != hasArgumentPair(args, "-bsf:a", "aac_adtstoasc") {
				t.Fatalf("AAC framing conversion must match copied audio: %v", args)
			}
		}
	}
}

func TestProgressiveVideoFrameRateIsAnActualFilter(t *testing.T) {
	p := progressiveVideoPlan()
	p.FrameRate = 12.5
	args, err := BuildArgs(p, 1)
	if err != nil || !hasArgumentPair(args, "-vf", "scale=w=160:h=90,format=yuv420p,fps=12.5") || !hasArgumentPair(args, "-enc_time_base:v", "filter") || slices.Contains(args, "-r") {
		t.Fatalf("frame rate must be enforced without conflicting sync options: %v, %v", args, err)
	}
	for _, hardware := range []Hardware{{Decode: "vaapi"}, {Encode: "vaapi"}, {Decode: "qsv", Encode: "qsv"}, {Decode: "cuda", Encode: "nvenc"}} {
		p.Hardware = hardware
		args, err := BuildArgs(p, 1)
		if err != nil {
			t.Fatalf("hardware options were not preserved: %+v: %v", hardware, err)
		}
		decode, encode := hardwareSelection(hardware)
		if !hasArgumentPair(args, "-vf", videoFilter(p, decode, encode)+",fps=12.5") || (decode != "software") != slices.Contains(args, "-hwaccel") {
			t.Fatalf("independent hardware pipeline: %v", args)
		}
	}
}

func TestProgressiveVideoRejectsUnknownAndIncompatiblePlans(t *testing.T) {
	for name, mutate := range map[string]func(*Plan){
		"unknown origin":        func(p *Plan) { p.SourceFormatStartKnown = false },
		"large positive origin": func(p *Plan) { p.SourceFormatStartTicks = maxDurationTicks + 1 },
		"large negative origin": func(p *Plan) { p.SourceFormatStartTicks = -maxDurationTicks - 1 },
		"origin overflow":       func(p *Plan) { p.SourceFormatStartTicks = math.MinInt64 },
		"video disabled":        func(p *Plan) { p.VideoStreamIndex = -1 },
		"duplicate stream":      func(p *Plan) { p.AudioStreamIndex = p.VideoStreamIndex },
		"video codec":           func(p *Plan) { p.VideoCodec = "hevc" },
		"audio codec":           func(p *Plan) { p.AudioCodec = "mp3" },
		"audio sample timing":   func(p *Plan) { p.AudioSourceSampleCount = 1 },
		"audio sample rate":     func(p *Plan) { p.AudioSourceSampleRate = 48000 },
		"audio sample seek":     func(p *Plan) { p.AudioSampleSeek = true },
		"audio bit depth":       func(p *Plan) { p.AudioBitDepth = 16 },
		"HLS duration":          func(p *Plan) { p.SegmentSeconds = 1 },
		"HLS mode":              func(p *Plan) { p.SegmentMode = "vod" },
		"HLS number":            func(p *Plan) { p.SegmentStartNumber = 1 },
		"HLS end":               func(p *Plan) { p.EndTicks = ticksPerSecond },
		"HLS cuts":              func(p *Plan) { p.SegmentTimes = "10000000" },
		"HLS reference":         func(p *Plan) { p.ReferenceStartTicks = 1 },
		"frame rate NaN":        func(p *Plan) { p.FrameRate = math.NaN() },
		"frame rate bound":      func(p *Plan) { p.FrameRate = 241 },
		"AAC bitrate clamp":     func(p *Plan) { p.AudioSampleRate, p.AudioChannels, p.AudioBitrate = 8000, 1, 96000 },
		"mixed hardware":        func(p *Plan) { p.Hardware = Hardware{Decode: "cuda", Encode: "vaapi"} },
		"copy transform":        func(p *Plan) { p.VideoCodec = "copy" },
		"copy seek": func(p *Plan) {
			p.VideoCodec, p.Width, p.Height, p.VideoBitrate, p.StartTicks = "copy", 0, 0, 0, ticksPerSecond
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := progressiveVideoPlan()
			mutate(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("invalid video plan accepted: %+v, %v", p, err)
			}
		})
	}
	for _, p := range []Plan{progressivePlan("m4a", "aac"), commandPlan()} {
		p.SourceFormatStartKnown = true
		if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("unrelated output accepted the video source clock: %+v, %v", p, err)
		}
		p.SourceFormatStartKnown, p.SourceFormatStartTicks = false, 1
		if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
			t.Fatalf("unrelated output accepted a source clock value: %+v, %v", p, err)
		}
	}
}
