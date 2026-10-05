package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func backgroundClipTestStream() Stream {
	return Stream{Index: 2, Codec: "h264", CodecType: "video", Width: 1920, Height: 1080, BitDepth: 8, PixelFormat: "yuv420p", TimeBase: "1/24000"}
}

func TestBackgroundClipWindowAndDisplayGeometry(t *testing.T) {
	info := Info{DurationTicks: 180 * TicksPerSecond}
	for _, interval := range [][2]int64{{0, TicksPerSecond}, {120 * TicksPerSecond, MaxBackgroundClipTicks}} {
		if err := backgroundClipWindow(info, interval[0], interval[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, interval := range [][2]int64{{-1, TicksPerSecond}, {0, 0}, {0, TicksPerSecond - 1}, {0, MaxBackgroundClipTicks + 1}, {121 * TicksPerSecond, MaxBackgroundClipTicks}, {1 << 62, 1 << 62}} {
		if err := backgroundClipWindow(info, interval[0], interval[1]); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("invalid interval accepted: %v, %v", interval, err)
		}
	}
	for _, test := range []struct {
		geometry      analysisDisplayGeometry
		width, height int
	}{
		{analysisDisplayGeometry{width: 1920, height: 1080, sarNumerator: 1, sarDenominator: 1}, 1280, 720},
		{analysisDisplayGeometry{width: 3840, height: 1600, sarNumerator: 1, sarDenominator: 1}, 1280, 532},
		{analysisDisplayGeometry{width: 1080, height: 1920, sarNumerator: 1, sarDenominator: 1}, 404, 720},
		{analysisDisplayGeometry{width: 720, height: 576, sarNumerator: 16, sarDenominator: 15}, 960, 720},
	} {
		width, height, err := backgroundClipDimensions(test.geometry, DefaultAnalysisLimits())
		if err != nil || width != test.width || height != test.height {
			t.Fatalf("display geometry = %d x %d, %v", width, height, err)
		}
	}
}

func TestBackgroundClipColorMappingRequiresActualHDRMetadata(t *testing.T) {
	sdr := backgroundClipTestStream()
	filter, mapped, err := backgroundClipColorFilter(sdr)
	if err != nil || mapped || filter != "format=yuv420p" {
		t.Fatalf("untagged SDR was relabeled: %q, %t, %v", filter, mapped, err)
	}
	for _, transfer := range []string{"smpte2084", "arib-std-b67"} {
		hdr := sdr
		hdr.Codec, hdr.PixelFormat, hdr.BitDepth = "hevc", "yuv420p10le", 0
		hdr.ColorTransfer, hdr.ColorPrimaries, hdr.ColorSpace, hdr.ColorRange = transfer, "bt2020", "bt2020nc", "tv"
		filter, mapped, err := backgroundClipColorFilter(hdr)
		if err != nil || !mapped {
			t.Fatalf("HDR rejected: %v", err)
		}
		for _, stage := range []string{"transferin=" + transfer, "transfer=linear", "format=gbrpf32le", "zscale=primaries=709", "tonemap=tonemap=hable", "transfer=709:matrix=709:range=limited"} {
			if !strings.Contains(filter, stage) {
				t.Fatalf("missing real tone-map stage %q", stage)
			}
		}
		for _, change := range []func(*Stream){
			func(s *Stream) { s.ColorPrimaries = "" }, func(s *Stream) { s.ColorRange = "" },
			func(s *Stream) { s.ColorSpace = "unknown" }, func(s *Stream) { s.BitDepth = 8 },
			func(s *Stream) { s.DolbyVision = &DolbyVisionMetadata{} },
		} {
			invalid := hdr
			change(&invalid)
			if _, _, err := backgroundClipColorFilter(invalid); !errors.Is(err, ErrBackgroundClipUnsupported) {
				t.Fatalf("incomplete HDR accepted: %+v, %v", invalid, err)
			}
		}
	}
	unknown := sdr
	unknown.BitDepth, unknown.PixelFormat = 10, "yuv420p10le"
	if _, _, err := backgroundClipColorFilter(unknown); !errors.Is(err, ErrBackgroundClipUnsupported) {
		t.Fatalf("unknown high-depth color accepted: %v", err)
	}
}

func backgroundClipTestProbe(t *testing.T, frames int) []byte {
	t.Helper()
	packets := make([]map[string]any, frames)
	for index := range packets {
		packets[index] = map[string]any{"stream_index": 0, "pts": index * 1000, "dts": index * 1000, "duration": 1000, "flags": "K__"}
	}
	document := map[string]any{"packets": packets, "streams": []map[string]any{{"index": 0, "codec_name": "h264", "codec_type": "video", "pix_fmt": "yuv420p", "width": 1280, "height": 720,
		"time_base": "1/24000", "avg_frame_rate": "24/1", "r_frame_rate": "24/1"}}, "format": map[string]any{"format_name": "mov,mp4,m4a,3gp,3g2,mj2"}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestBackgroundClipOutputProofRejectsWrongStreamsAndTimestamps(t *testing.T) {
	plan := backgroundClipPlan{frames: 24, width: 1280, height: 720}
	data := backgroundClipTestProbe(t, plan.frames)
	if err := parseBackgroundClipProbe(bytes.NewReader(data), plan); err != nil {
		t.Fatal(err)
	}
	for _, replacement := range [][2]string{{`"codec_type":"video"`, `"codec_type":"audio"`}, {`"pix_fmt":"yuv420p"`, `"pix_fmt":"yuv420p10le"`},
		{`"pts":1000`, `"pts":999`}, {`"duration":1000`, `"duration":999`}, {`"flags":"K__"`, `"flags":"KC_"`},
		{`"index":0`, `"index":0,"index":1`}, {`"r_frame_rate":"24/1"`, `"r_frame_rate":"25/1"`},
	} {
		invalid := strings.Replace(string(data), replacement[0], replacement[1], 1)
		if invalid == string(data) {
			t.Fatalf("fixture replacement did not match %q", replacement[0])
		}
		if err := parseBackgroundClipProbe(strings.NewReader(invalid), plan); !errors.Is(err, ErrAnalysisUnproven) {
			t.Fatalf("invalid result accepted: %v, %v", replacement, err)
		}
	}
	if err := parseBackgroundClipProbe(bytes.NewReader(backgroundClipTestProbe(t, 23)), plan); !errors.Is(err, ErrAnalysisUnproven) {
		t.Fatalf("short output accepted: %v", err)
	}
	plan.toneMapped = true
	if err := parseBackgroundClipProbe(bytes.NewReader(data), plan); !errors.Is(err, ErrAnalysisUnproven) {
		t.Fatalf("untagged mapped HDR accepted: %v", err)
	}
}

func TestBackgroundClipDecodedPicturesRejectBlackStaticAndIncomplete(t *testing.T) {
	const frames, pixels = 72, 32 * 18
	for _, test := range []struct {
		name string
		data []byte
		want error
	}{
		{"black", make([]byte, frames*pixels), ErrBackgroundClipUnusable},
		{"static", bytes.Repeat([]byte{100}, frames*pixels), ErrBackgroundClipUnusable},
		{"short", make([]byte, frames*pixels-1), ErrAnalysisUnproven},
		{"extra", make([]byte, frames*pixels+1), ErrAnalysisUnproven},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := inspectBackgroundClipPictures(context.Background(), bytes.NewReader(test.data), frames); !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
	var moving bytes.Buffer
	for frame := 0; frame < frames; frame++ {
		moving.Write(bytes.Repeat([]byte{byte(40 + frame)}, pixels))
	}
	if err := inspectBackgroundClipPictures(context.Background(), bytes.NewReader(moving.Bytes()), frames); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := inspectBackgroundClipPictures(ctx, bytes.NewReader(moving.Bytes()), frames); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestBackgroundClipArgsKeepSourceAndOutputBounded(t *testing.T) {
	stream := backgroundClipTestStream()
	plan, err := planBackgroundClip(Info{DurationTicks: 180 * TicksPerSecond}, stream, analysisSquareGeometry(stream), 30*TicksPerSecond, 25*TicksPerSecond, DefaultAnalysisLimits())
	if err != nil {
		t.Fatal(err)
	}
	args := backgroundClipEncodeArgs(stream, plan, DefaultAnalysisLimits())
	joined := strings.Join(args, " ")
	for _, required := range []string{"-ss 30.0000000", "-i /proc/self/fd/3", "-map 0:2", "-an -sn -dn", "-threads:v 2", "-frames:v 600", "-t 25.0000000", "-b:v 1500000", "-pix_fmt yuv420p", "+empty_moov+frag_keyframe+default_base_moof", "pipe:1"} {
		if !strings.Contains(joined, required) {
			t.Fatal(fmt.Sprintf("missing bounded argument %q", required))
		}
	}
	if strings.Contains(joined, "-c:a") || strings.Contains(joined, "-y") {
		t.Fatal("encoder admitted an audio track or filesystem overwrite")
	}
}

func TestBackgroundClipOptionsUseConfiguredGeometryAndBitrate(t *testing.T) {
	stream := backgroundClipTestStream()
	limits := DefaultAnalysisLimits()
	limits.MaxFramePixels = 1920 * 1080
	for _, options := range []BackgroundClipOptions{{MaxWidth: 640, VideoBitrate: 250000}, {MaxWidth: 960, VideoBitrate: 1500000}, {MaxWidth: 1280, VideoBitrate: 3000000}, {MaxWidth: 1920, VideoBitrate: 8000000}} {
		plan, err := planBackgroundClipWithOptions(Info{DurationTicks: 180 * TicksPerSecond}, stream, analysisSquareGeometry(stream), 0, 60*TicksPerSecond, options, limits)
		if err != nil || plan.width != options.MaxWidth || plan.height != options.MaxWidth*9/16 || plan.frames != 1440 {
			t.Fatalf("configured plan: %+v, %v", plan, err)
		}
		args := strings.Join(backgroundClipEncodeArgs(stream, plan, limits), " ")
		if !strings.Contains(args, fmt.Sprintf("-b:v %d", options.VideoBitrate)) || !strings.Contains(backgroundClipProfile(options), fmt.Sprintf("bitrate=%d", options.VideoBitrate)) {
			t.Fatalf("configured bitrate lost: %s", args)
		}
	}
	for _, options := range []BackgroundClipOptions{{MaxWidth: 641, VideoBitrate: 1500000}, {MaxWidth: 1920, VideoBitrate: 249999}, {MaxWidth: 1280, VideoBitrate: 8000001}, {MaxWidth: -1, VideoBitrate: 1500000}} {
		if _, err := normalizeBackgroundClipOptions(options); !errors.Is(err, ErrAnalysisBudget) {
			t.Fatalf("invalid profile accepted: %+v, %v", options, err)
		}
	}
}
