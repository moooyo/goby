package transcode

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"
)

func commandPlan() Plan {
	return Plan{Container: "ts", VideoCodec: "h264", AudioCodec: "aac", VideoStreamIndex: 0, AudioStreamIndex: 1,
		DurationTicks: 12 * ticksPerSecond, SegmentSeconds: 3, Width: 160, Height: 90, FrameRate: 10,
		VideoBitrate: 256_000, AudioBitrate: 96_000, AudioChannels: 2, AudioSampleRate: 48000}
}

func TestBuildArgsMapsSelectedStreamsAndFixedOutputs(t *testing.T) {
	p := commandPlan()
	p.VideoStreamIndex, p.AudioStreamIndex, p.StartTicks = 2, 5, 1_234_567
	args, err := BuildArgs(p, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"-i", "/proc/self/fd/3"}, {"-map", "0:2"}, {"-map", "0:5"},
		{"-ss", "0.1234567"}, {"-t", "11.8765433"}, {"-map_metadata", "-1"}, {"-map_chapters", "-1"},
		{"-protocol_whitelist", "file,pipe"}, {"-hls_flags", "temp_file"}, {"-hls_playlist_type", "event"},
		{"-hls_segment_filename", "segment-%06d.ts"}, {"-force_key_frames", "expr:gte(t,n_forced*3)"},
		{"-c:v", "libx264"}, {"-c:a", "aac"}, {"-profile:a", "aac_low"}, {"-threads:v", "2"}} {
		if !hasArgumentPair(args, pair[0], pair[1]) {
			t.Errorf("missing argument %q %q", pair[0], pair[1])
		}
	}
	if args[len(args)-1] != "main.m3u8" || !slices.Contains(args, "-sn") || !slices.Contains(args, "-dn") {
		t.Fatalf("output mapping is incomplete: %v", args)
	}
	for _, forbidden := range []string{"split_by_time", "independent_segments", "delete_segments", "http", "https", "concat"} {
		if strings.Contains(strings.Join(args, " "), forbidden) {
			t.Errorf("unexpected argument %q", forbidden)
		}
	}
	if slices.Contains(strings.Split(inputFormats, ","), "hls") || slices.Contains(strings.Split(inputFormats, ","), "dash") {
		t.Fatal("manifest input must not be allowed")
	}
}

func TestBuildArgsHardwareDecodeAndEncodeIndependently(t *testing.T) {
	for _, tc := range []struct {
		name, decode, encode, device, wantFilter, wantEncoder string
	}{
		{"software", "", "", "", "scale=w=160:h=90,format=yuv420p", "libx264"},
		{"vaapi decode", "vaapi", "software", "/dev/dri/renderD129", "scale_vaapi=w=160:h=90:format=nv12,hwdownload,format=nv12,format=yuv420p", "libx264"},
		{"qsv decode", "qsv", "software", "", "vpp_qsv=w=160:h=90:format=nv12,hwdownload,format=nv12,format=yuv420p", "libx264"},
		{"cuda decode", "cuda", "software", "1", "scale_cuda=w=160:h=90:format=nv12,hwdownload,format=nv12,format=yuv420p", "libx264"},
		{"vaapi encode", "software", "vaapi", "", "scale=w=160:h=90,format=nv12,hwupload=extra_hw_frames=64", "h264_vaapi"},
		{"qsv encode", "software", "qsv", "", "scale=w=160:h=90,format=nv12,hwupload=extra_hw_frames=64", "h264_qsv"},
		{"nvenc encode", "software", "nvenc", "0", "scale=w=160:h=90,format=nv12,hwupload=extra_hw_frames=64", "h264_nvenc"},
		{"vaapi both", "vaapi", "vaapi", "", "scale_vaapi=w=160:h=90:format=nv12", "h264_vaapi"},
		{"qsv both", "qsv", "qsv", "", "vpp_qsv=w=160:h=90:format=nv12", "h264_qsv"},
		{"cuda both", "cuda", "nvenc", "", "scale_cuda=w=160:h=90:format=nv12", "h264_nvenc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := commandPlan()
			p.Hardware = Hardware{Decode: tc.decode, Encode: tc.encode, Device: tc.device}
			args, err := BuildArgs(p, 1)
			if err != nil {
				t.Fatal(err)
			}
			if !hasArgumentPair(args, "-vf", tc.wantFilter) || !hasArgumentPair(args, "-c:v", tc.wantEncoder) {
				t.Fatalf("unexpected hardware pipeline: %v", args)
			}
			decode := tc.decode != "" && tc.decode != "software"
			if slices.Contains(args, "-hwaccel") != decode {
				t.Fatal("decoder selection must be independent from the encoder")
			}
			if decode && (!hasArgumentPair(args, "-hwaccel", tc.decode) || !hasArgumentPair(args, "-hwaccel_output_format", tc.decode)) {
				t.Fatal("hardware decoding did not request hardware frames")
			}
			if tc.encode == "qsv" && !hasArgumentPair(args, "-forced_idr", "1") {
				t.Fatal("QSV forced frames must be IDR frames")
			}
		})
	}
}

func TestBuildArgsRemuxAndAudioOnly(t *testing.T) {
	p := Plan{Container: "mpegts", VideoCodec: "copy", AudioCodec: "copy", VideoStreamIndex: 2, AudioStreamIndex: 3,
		DurationTicks: 12 * ticksPerSecond, SegmentSeconds: 3}
	args, err := BuildArgs(p, 1)
	if err != nil || !hasArgumentPair(args, "-c:v", "copy") || !hasArgumentPair(args, "-c:a", "copy") {
		t.Fatalf("remux: %v, %v", args, err)
	}
	if slices.Contains(args, "-vf") || slices.Contains(args, "-force_key_frames") {
		t.Fatal("remux must preserve source keyframe spacing")
	}
	p.VideoStreamIndex, p.VideoCodec, p.AudioCodec = -1, "", "mp3"
	args, err = BuildArgs(p, 1)
	if err != nil || !hasArgumentPair(args, "-c:a", "libmp3lame") || !slices.Contains(args, "-vn") || slices.Contains(args, "-c:v") {
		t.Fatalf("audio only: %v, %v", args, err)
	}
}

func TestValidatePlanRejectsUnsafeAndContradictoryInputs(t *testing.T) {
	for name, change := range map[string]func(*Plan){
		"manifest container":      func(p *Plan) { p.Container = "http://example.invalid/out.m3u8" },
		"codec injection":         func(p *Plan) { p.VideoCodec = "h264 -f http" },
		"negative duration":       func(p *Plan) { p.DurationTicks = -1 },
		"unknown duration":        func(p *Plan) { p.DurationTicks = 0 },
		"excess duration":         func(p *Plan) { p.DurationTicks = maxDurationTicks + 1 },
		"seek past end":           func(p *Plan) { p.StartTicks = p.DurationTicks },
		"negative seek":           func(p *Plan) { p.StartTicks = -1 },
		"zero segment":            func(p *Plan) { p.SegmentSeconds = 0 },
		"excess segment":          func(p *Plan) { p.SegmentSeconds = 11 },
		"same index":              func(p *Plan) { p.AudioStreamIndex = p.VideoStreamIndex },
		"excess index":            func(p *Plan) { p.VideoStreamIndex = maxStreamIndex + 1 },
		"odd dimension":           func(p *Plan) { p.Width = 159 },
		"partial dimensions":      func(p *Plan) { p.Height = 0 },
		"huge dimension":          func(p *Plan) { p.Width = 8194 },
		"nan frame rate":          func(p *Plan) { p.FrameRate = math.NaN() },
		"infinite frame rate":     func(p *Plan) { p.FrameRate = math.Inf(1) },
		"small frame rate":        func(p *Plan) { p.FrameRate = 0.5 },
		"overflow bitrate":        func(p *Plan) { p.VideoBitrate = math.MaxInt64 },
		"invalid sample rate":     func(p *Plan) { p.AudioSampleRate = 47999 },
		"MP3 surround":            func(p *Plan) { p.AudioCodec, p.AudioChannels = "mp3", 6 },
		"mixed backends":          func(p *Plan) { p.Hardware = Hardware{Decode: "vaapi", Encode: "nvenc"} },
		"invalid decoder":         func(p *Plan) { p.Hardware.Decode = "nvenc" },
		"device traversal":        func(p *Plan) { p.Hardware = Hardware{Decode: "vaapi", Device: "/dev/dri/../renderD128"} },
		"device option injection": func(p *Plan) { p.Hardware = Hardware{Decode: "cuda", Device: "0,primary_ctx=1"} },
		"software device":         func(p *Plan) { p.Hardware.Device = "0" },
		"copy transform":          func(p *Plan) { p.VideoCodec = "copy" },
		"audio copy transform":    func(p *Plan) { p.AudioCodec = "copy" },
	} {
		t.Run(name, func(t *testing.T) {
			p := commandPlan()
			change(&p)
			if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("got %v, want ErrInvalidPlan", err)
			}
		})
	}
	for _, threads := range []int{-1, 0, 65} {
		if _, err := BuildArgs(commandPlan(), threads); !errors.Is(err, ErrInvalidThreads) {
			t.Fatalf("threads %d: %v", threads, err)
		}
	}
}

func hasArgumentPair(args []string, flag, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}
