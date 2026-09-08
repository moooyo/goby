package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readProbeFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "mixed-scalars.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseProbePreservesMediaMetadata(t *testing.T) {
	info, err := parseProbe(readProbeFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Container != "matroska,webm" || info.DurationTicks != 9_007_199_254_740_993 ||
		info.Size != 9_007_199_254_740_993 || info.Bitrate != 1_000_000 {
		t.Fatalf("incorrect format metadata: %+v", info)
	}
	if len(info.Streams) != 4 {
		t.Fatalf("got %d streams", len(info.Streams))
	}
	video := info.Streams[0]
	if video.Index != 0 || video.Codec != "h264" || video.Width != 1920 || video.Height != 1080 ||
		video.Profile != "High" || video.Level != 41 || video.PixelFormat != "yuv420p" ||
		video.AverageFrameRate != "24000/1001" || !video.IsDefault || video.IsForced || video.IsExternal {
		t.Fatalf("incorrect video: %+v", video)
	}
	audio := info.Streams[1]
	if audio.Index != 3 || audio.Codec != "aac" || audio.CodecType != "audio" ||
		audio.SampleRate != 48000 || audio.Channels != 6 || audio.Bitrate != 192000 ||
		audio.Language != "eng" || audio.Title != "Original audio" || !audio.IsDefault {
		t.Fatalf("incorrect audio: %+v", audio)
	}
	text, bitmap := info.Streams[2], info.Streams[3]
	if text.Index != 7 || !text.IsTextSubtitleStream || !text.IsForced || text.Language != "fra" ||
		text.Title != "Forced dialogue" || text.Bitrate != 0 || bitmap.Index != 9 ||
		bitmap.IsTextSubtitleStream || bitmap.Level != -99 {
		t.Fatalf("incorrect subtitle streams: %+v, %+v", text, bitmap)
	}
	wantChapters := []Chapter{{StartTicks: 1_234_568, EndTicks: 10_000_000, Title: "Opening"}}
	if !reflect.DeepEqual(info.Chapters, wantChapters) {
		t.Fatalf("got chapters %+v, want %+v", info.Chapters, wantChapters)
	}
}

func TestSecondsToTicksUsesExactArithmetic(t *testing.T) {
	for _, test := range []struct {
		input string
		want  int64
	}{
		{"", 0}, {"N/A", 0}, {"0", 0}, {"1e-7", 1}, {"1e3", 10_000_000_000},
		{"0.00000004", 0}, {"0.00000005", 1}, {"-0.00000005", -1},
		{"1.23456789", 12_345_679}, {"1/3", 3_333_333},
		{"900719925.4740993", 9_007_199_254_740_993},
		{"922337203685.4775807", 9_223_372_036_854_775_807},
		{"-922337203685.4775808", -9_223_372_036_854_775_808},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, err := secondsToTicks(scalar(test.input))
			if err != nil || got != test.want {
				t.Fatalf("secondsToTicks(%q) = %d, %v; want %d", test.input, got, err, test.want)
			}
		})
	}
	for _, invalid := range []string{"NaN", "Infinity", "0/0", "922337203685.4775808", "1e1000000000", "0x1p1000000000", strings.Repeat("1", 257)} {
		if _, err := secondsToTicks(scalar(invalid)); err == nil {
			t.Errorf("accepted invalid or unbounded number %q", invalid)
		}
	}
}

func TestParseProbeMissingFormatDurationUsesStreamTimeBase(t *testing.T) {
	info, err := parseProbe([]byte(`{
		"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2","duration":"N/A"},
		"streams":[
			{"index":0,"codec_type":"video","codec_name":"h264","duration":"0.3"},
			{"index":4,"codec_type":"audio","codec_name":"aac","duration_ts":16001,"time_base":"1/48000","duration":"0.333"}
		]
	}`))
	if err != nil || info.DurationTicks != 3_333_542 {
		t.Fatalf("incorrect timestamp-based duration: %+v, %v", info, err)
	}
}

func TestParseProbeRejectsMalformedValues(t *testing.T) {
	fixture := string(readProbeFixture(t))
	for name, data := range map[string]string{
		"missing media":     `{}`,
		"invalid JSON":      `{"format":`,
		"trailing JSON":     fixture + `{}`,
		"boolean numeric":   strings.Replace(fixture, `"width": 1920`, `"width": true`, 1),
		"fractional width":  strings.Replace(fixture, `"width": 1920`, `"width": 1920.5`, 1),
		"negative width":    strings.Replace(fixture, `"width": 1920`, `"width": -1`, 1),
		"duplicate index":   strings.Replace(fixture, `"index": "3"`, `"index": 0`, 1),
		"missing index":     strings.Replace(fixture, `"index": "3",`, ``, 1),
		"invalid time base": strings.Replace(fixture, `"1/48000"`, `"0/0"`, 1),
		"invalid flag":      strings.Replace(fixture, `"default": 1`, `"default": 2`, 1),
		"negative duration": strings.Replace(fixture, `"900719925.4740993"`, `"-1"`, 1),
		"enormous exponent": strings.Replace(fixture, `"900719925.4740993"`, `"1e1000000000"`, 1),
		"reversed chapter":  strings.Replace(fixture, `"999999999"`, `"1"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProbe([]byte(data)); err == nil {
				t.Fatal("malformed ffprobe data was accepted")
			}
		})
	}
}

func TestProbeRejectsNonFileInputsBeforeExecuting(t *testing.T) {
	prober := Prober{FFprobePath: "/missing/ffprobe"}
	for _, path := range []string{"", "http://localhost/media.mp4", "https://localhost/media.mp4", "file:///tmp/media.mp4", "pipe:0", t.TempDir(), filepath.Join(t.TempDir(), "missing.mkv")} {
		if _, err := prober.Probe(context.Background(), path); err == nil || strings.Contains(err.Error(), "execute") {
			t.Errorf("input %q was not rejected before process launch: %v", path, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prober.Probe(ctx, "anything"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe returned %v", err)
	}
	if _, err := prober.ProbeFile(context.Background(), nil); err == nil {
		t.Fatal("nil file was accepted")
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	if _, err := prober.ProbeFile(context.Background(), reader); err == nil || strings.Contains(err.Error(), "execute") {
		t.Fatalf("pipe descriptor was not rejected before process launch: %v", err)
	}
}
