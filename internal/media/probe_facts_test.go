package media

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestParseProbePreservesExtendedStreamFacts(t *testing.T) {
	info, err := parseProbe([]byte(`{
		"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2"},
		"streams":[
			{
				"index":0,"codec_type":"video","codec_name":"h264",
				"bits_per_raw_sample":"10","bits_per_sample":8,
				"codec_tag":"0x31637661","codec_tag_string":"avc1",
				"pix_fmt":"yuv420p10le","time_base":"1/24000",
				"avg_frame_rate":"24000/1001","r_frame_rate":"48000/1001",
				"refs":"4","field_order":"progressive","is_avc":true,
				"color_range":"tv","color_space":"bt2020nc",
				"color_transfer":"unknown","color_primaries":"bt2020",
				"disposition":{"attached_pic":0}
			},
			{
				"index":"2","codec_type":"audio","codec_name":"pcm_s24le",
				"bits_per_raw_sample":"N/A","bits_per_sample":"24",
				"channel_layout":"5.1(side)","time_base":"1/48000",
				"refs":null,"is_avc":"0"
			},
			{
				"index":7,"codec_type":"video","codec_name":"mjpeg",
				"pix_fmt":"yuvj420p","field_order":"unknown",
				"is_avc":false,"disposition":{"attached_pic":"1"}
			}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if info.ProbeVersion != CurrentProbeVersion || info.FileChangeTimeNs != 0 || len(info.Streams) != 3 {
		t.Fatalf("incorrect probe version or stream count: %+v", info)
	}
	video := info.Streams[0]
	if video.BitDepth != 10 || video.CodecTag != "0x31637661" || video.CodecTagString != "avc1" ||
		video.TimeBase != "1/24000" || video.AverageFrameRate != "24000/1001" ||
		video.RealFrameRate != "48000/1001" || video.RefFrames != 4 ||
		video.FieldOrder != "progressive" || !video.InterlaceKnown || video.IsInterlaced ||
		!video.IsAVCKnown || !video.IsAVC || video.IsAttachedPicture {
		t.Fatalf("incorrect video facts: %+v", video)
	}
	if video.ColorRange != "tv" || video.ColorSpace != "bt2020nc" ||
		video.ColorTransfer != "unknown" || video.ColorPrimaries != "bt2020" ||
		video.VideoRange != "" || video.VideoRangeKnown {
		t.Fatalf("incorrect color facts or inferred video range: %+v", video)
	}
	audio, picture := info.Streams[1], info.Streams[2]
	if audio.BitDepth != 24 || audio.ChannelLayout != "5.1(side)" || audio.TimeBase != "1/48000" ||
		audio.RefFrames != 0 || !audio.IsAVCKnown || audio.IsAVC || audio.InterlaceKnown {
		t.Fatalf("incorrect audio facts: %+v", audio)
	}
	if !picture.IsAttachedPicture || picture.BitDepth != 0 || picture.InterlaceKnown ||
		picture.FieldOrder != "unknown" || !picture.IsAVCKnown || picture.IsAVC {
		t.Fatalf("incorrect attached picture facts: %+v", picture)
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var restored Info
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, info) {
		t.Fatalf("cached facts changed after a JSON round trip: got %+v, want %+v", restored, info)
	}
}

func TestParseProbeBitDepthUsesReportedSamples(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields string
		want   int
	}{
		{"raw precedence", `"bits_per_raw_sample":10,"bits_per_sample":"12"`, 10},
		{"zero raw fallback", `"bits_per_raw_sample":"0","bits_per_sample":16`, 16},
		{"missing raw fallback", `"bits_per_sample":"24"`, 24},
		{"unknown raw fallback", `"bits_per_raw_sample":"N/A","bits_per_sample":8`, 8},
		{"null raw fallback", `"bits_per_raw_sample":null,"bits_per_sample":8`, 8},
		{"missing samples", `"pix_fmt":"yuv420p10le"`, 0},
		{"unknown samples", `"bits_per_raw_sample":"N/A","bits_per_sample":null`, 0},
		{"zero samples", `"bits_per_raw_sample":0,"bits_per_sample":"0"`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := parseFactsStream(t, `"codec_type":"video",`+test.fields)
			if stream.BitDepth != test.want {
				t.Fatalf("got bit depth %d, want %d", stream.BitDepth, test.want)
			}
		})
	}
}

func TestParseProbeInterlaceUsesExplicitVideoFieldOrder(t *testing.T) {
	for _, test := range []struct {
		name       string
		fields     string
		interlaced bool
		known      bool
	}{
		{"progressive", `"codec_type":"video","field_order":"progressive"`, false, true},
		{"top first", `"codec_type":"video","field_order":"tt"`, true, true},
		{"bottom first", `"codec_type":"video","field_order":"bb"`, true, true},
		{"top coded", `"codec_type":"video","field_order":"tb"`, true, true},
		{"bottom coded", `"codec_type":"video","field_order":"bt"`, true, true},
		{"unknown", `"codec_type":"video","field_order":"unknown"`, false, false},
		{"missing", `"codec_type":"video"`, false, false},
		{"null", `"codec_type":"video","field_order":null`, false, false},
		{"unrecognized", `"codec_type":"video","field_order":"unspecified"`, false, false},
		{"nonvideo", `"codec_type":"audio","field_order":"tt"`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := parseFactsStream(t, test.fields)
			if stream.IsInterlaced != test.interlaced || stream.InterlaceKnown != test.known {
				t.Fatalf("incorrect interlace facts: %+v", stream)
			}
		})
	}
}

func TestParseProbeAVCKeepsKnownFalseDistinctFromUnknown(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		avc   bool
		known bool
	}{
		{"boolean true", `true`, true, true},
		{"boolean false", `false`, false, true},
		{"string true", `"true"`, true, true},
		{"string false", `"false"`, false, true},
		{"numeric one", `1`, true, true},
		{"numeric zero", `0`, false, true},
		{"string one", `"1"`, true, true},
		{"string zero", `"0"`, false, true},
		{"null", `null`, false, false},
		{"unknown", `"N/A"`, false, false},
		{"empty", `""`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			stream := parseFactsStream(t, `"is_avc":`+test.value)
			if stream.IsAVC != test.avc || stream.IsAVCKnown != test.known {
				t.Fatalf("incorrect AVC facts: %+v", stream)
			}
		})
	}
	stream := parseFactsStream(t, `"codec_type":"video","codec_name":"h264"`)
	if stream.IsAVC || stream.IsAVCKnown {
		t.Fatalf("AVC facts were inferred from the codec: %+v", stream)
	}
}

func TestParseProbeRejectsMalformedExtendedFacts(t *testing.T) {
	for name, fields := range map[string]string{
		"negative raw depth":           `"bits_per_raw_sample":-1`,
		"negative fallback depth":      `"bits_per_raw_sample":10,"bits_per_sample":"-1"`,
		"fractional raw depth":         `"bits_per_raw_sample":"10.5"`,
		"fractional fallback depth":    `"bits_per_sample":8.5`,
		"overflowing raw depth":        `"bits_per_raw_sample":"9223372036854775808"`,
		"overflowing fallback depth":   `"bits_per_sample":"9223372036854775808"`,
		"boolean depth":                `"bits_per_sample":true`,
		"negative reference frames":    `"refs":-1`,
		"fractional reference frames":  `"refs":"1/2"`,
		"overflowing reference frames": `"refs":"9223372036854775808"`,
		"negative attachment":          `"disposition":{"attached_pic":-1}`,
		"invalid attachment":           `"disposition":{"attached_pic":"2"}`,
		"boolean attachment":           `"disposition":{"attached_pic":true}`,
		"unrecognized AVC string":      `"is_avc":"yes"`,
		"unknown AVC string":           `"is_avc":"unknown"`,
		"negative AVC":                 `"is_avc":-1`,
		"invalid AVC integer":          `"is_avc":2`,
		"fractional AVC":               `"is_avc":0.5`,
		"AVC object":                   `"is_avc":{}`,
		"AVC array":                    `"is_avc":[]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseProbe(factsDocument(fields)); err == nil {
				t.Fatal("malformed ffprobe facts were accepted")
			}
		})
	}
}

func TestProbeFactVersionAndLegacyCache(t *testing.T) {
	if CurrentProbeVersion != 2 || (Prober{}).CacheVersion() != CurrentProbeVersion {
		t.Fatalf("unexpected probe cache version: %d", (Prober{}).CacheVersion())
	}
	var legacy Info
	if err := json.Unmarshal([]byte(`{"Container":"matroska,webm","Streams":[{"Index":0,"Codec":"h264","CodecType":"video","PixelFormat":"yuv420p10le"}]}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.ProbeVersion != 0 || legacy.FileChangeTimeNs != 0 || len(legacy.Streams) != 1 {
		t.Fatalf("legacy cache did not retain the unknown version: %+v", legacy)
	}
	stream := legacy.Streams[0]
	if stream.BitDepth != 0 || stream.RefFrames != 0 || stream.IsAVCKnown || stream.InterlaceKnown ||
		stream.IsAttachedPicture || stream.VideoRangeKnown || stream.VideoRange != "" {
		t.Fatalf("legacy cache inferred facts that were never probed: %+v", stream)
	}
	info, err := parseProbe(factsDocument(`"codec_type":"video"`))
	if err != nil || info.ProbeVersion != 2 {
		t.Fatalf("fresh probe did not set the current cache version: %+v, %v", info, err)
	}
}

func TestProbeActualFFprobeReportsExtendedFacts(t *testing.T) {
	ffprobe, ffmpeg := os.Getenv("GOBY_FFPROBE"), os.Getenv("GOBY_FFMPEG")
	if ffprobe == "" || ffmpeg == "" {
		t.Skip("set GOBY_FFPROBE and GOBY_FFMPEG to verify extended facts on Linux")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("media runtime verification must run on Linux")
	}
	path := filepath.Join(t.TempDir(), "facts.mp4")
	_, err := runLimited(context.Background(), 20*time.Second, 1024, ffmpeg,
		"-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30000/1001",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "1",
		"-c:v", "libx264", "-refs:v", "3", "-pix_fmt", "yuv420p", "-color_range", "tv",
		// Set the H.264 VUI explicitly: generic output color options alone do
		// not preserve these fields with the FFmpeg 9/libx264 toolchain.
		"-x264-params", "colorprim=bt709:transfer=bt709:colormatrix=bt709",
		"-colorspace", "bt709", "-color_trc", "bt709", "-color_primaries", "bt709",
		"-c:a", "aac", "-ac", "2", "-f", "mp4", path)
	if err != nil {
		t.Fatalf("generate facts fixture: %v", err)
	}
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if info.ProbeVersion != CurrentProbeVersion || info.FileChangeTimeNs <= 0 || len(info.Streams) != 2 {
		t.Fatalf("unexpected probed facts: %+v", info)
	}
	// FFmpeg 9 emits refs only when frames are read. Compare this optional
	// fact against an independent raw JSON query rather than inventing a value
	// from the encoder setting or treating omission as a known frame count.
	raw, err := runLimited(context.Background(), 10*time.Second, maxProbeOutput, ffprobe,
		"-v", "error", "-show_streams", "-of", "json", "-i", path)
	if err != nil {
		t.Fatalf("read raw fixture facts: %v", err)
	}
	var rawFacts struct {
		Streams []struct {
			RefFrames *int `json:"refs"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(raw, &rawFacts); err != nil || len(rawFacts.Streams) != 2 {
		t.Fatalf("unexpected raw fixture facts: %s, %v", raw, err)
	}
	expectedRefs := 0
	if rawFacts.Streams[0].RefFrames != nil {
		expectedRefs = *rawFacts.Streams[0].RefFrames
	}
	video, audio := info.Streams[0], info.Streams[1]
	if video.Codec != "h264" || video.CodecTag != "0x31637661" || video.CodecTagString != "avc1" ||
		video.BitDepth != 8 || video.RefFrames != expectedRefs || video.TimeBase == "" ||
		video.AverageFrameRate != "30000/1001" || video.RealFrameRate != "30000/1001" ||
		!video.IsAVCKnown || !video.IsAVC || !video.InterlaceKnown || video.IsInterlaced ||
		video.IsAttachedPicture || video.ColorRange != "tv" || video.ColorSpace != "bt709" ||
		video.ColorTransfer != "bt709" || video.ColorPrimaries != "bt709" {
		t.Fatalf("unexpected video facts: %+v", video)
	}
	if audio.Codec != "aac" || audio.ChannelLayout != "stereo" || audio.TimeBase != "1/48000" {
		t.Fatalf("unexpected audio facts: %+v", audio)
	}
}

func TestProbeFileRejectsSameSizeMutationWithRestoredModTime(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("descriptor mutation verification requires Linux")
	}
	directory := t.TempDir()
	path, reference := filepath.Join(directory, "media.mkv"), filepath.Join(directory, "timestamp")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reference, nil, 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Unix(1_000_000_000, 123)
	for _, name := range []string{path, reference} {
		if err := os.Chtimes(name, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(directory, "ffprobe-helper")
	program := "#!/bin/sh\nset -eu\nsleep 0.05\n" +
		"printf '%s' 'changed!' > /proc/self/fd/3\n" +
		"touch -r \"$GOBY_PROBE_REFERENCE\" /proc/self/fd/3\n" +
		"printf '%s\\n' '{\"format\":{\"format_name\":\"matroska,webm\"},\"streams\":[{\"index\":0}]}'\n"
	if err := os.WriteFile(helper, []byte(program), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOBY_PROBE_REFERENCE", reference)
	info, err := (Prober{FFprobePath: helper, Timeout: 5 * time.Second}).ProbeFile(context.Background(), file)
	if err == nil || !strings.Contains(err.Error(), "media file changed during probe") || !reflect.DeepEqual(info, Info{}) {
		t.Fatalf("changed media returned cached facts: %+v, %v", info, err)
	}
	after, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("fixture did not preserve the original identity, size, and modtime: %v, %v", before, after)
	}
	if FileChangeTime(before) == 0 || FileChangeTime(after) == 0 || FileChangeTime(before) == FileChangeTime(after) {
		t.Fatal("fixture did not advance the file change time")
	}
}

func parseFactsStream(t *testing.T, fields string) Stream {
	t.Helper()
	info, err := parseProbe(factsDocument(fields))
	if err != nil {
		t.Fatal(err)
	}
	return info.Streams[0]
}

func factsDocument(fields string) []byte {
	var document strings.Builder
	document.WriteString(`{"format":{"format_name":"matroska,webm"},"streams":[{"index":0,`)
	document.WriteString(fields)
	document.WriteString(`}]}`)
	return []byte(document.String())
}
