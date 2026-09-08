package media

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"
)

func TestParseFFmpegVersion(t *testing.T) {
	for _, item := range []struct {
		name   string
		output string
		want   string
	}{
		{"release", "ffmpeg version 9.0.1 Copyright (c) 2000-2026 the FFmpeg developers\nbuilt with gcc 14\n", "9.0.1"},
		{"package suffix", "ffmpeg version 9.0.1-1ubuntu1 Copyright (c) 2000-2026 the FFmpeg developers\r\n", "9.0.1-1ubuntu1"},
		{"different program", "ffprobe version 9.0.1\n", ""},
		{"missing token", "ffmpeg version\n", ""},
		{"empty", "", ""},
	} {
		t.Run(item.name, func(t *testing.T) {
			got, err := parseFFmpegVersion([]byte(item.output))
			if got != item.want || (err != nil) != (item.want == "") {
				t.Fatalf("parseFFmpegVersion() = %q, %v; want %q", got, err, item.want)
			}
		})
	}
}

func TestParseHardwareAccelerators(t *testing.T) {
	output := "Hardware acceleration methods:\r\n\r\ncuda\r\nvaapi\r\nqsv\r\ndrm\r\nvaapi\r\n\r\n"
	got, err := parseHardwareAccelerators([]byte(output))
	if err != nil || !slices.Equal(got, []string{"cuda", "vaapi", "qsv", "drm"}) {
		t.Fatalf("parseHardwareAccelerators() = %v, %v", got, err)
	}
	got, err = parseHardwareAccelerators([]byte("Hardware acceleration methods:\n\n"))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("a build without hardware methods must return an empty list: %v, %v", got, err)
	}
	for _, malformed := range []string{"", "cuda\nvaapi\n", "Hardware acceleration methods:\ncuda supported\n"} {
		if _, err := parseHardwareAccelerators([]byte(malformed)); err == nil {
			t.Errorf("accepted malformed hardware list %q", malformed)
		}
	}
}

func TestParseCodecListPreservesImplementationNames(t *testing.T) {
	decoders := `Decoders:
 V..... = Video
 A..... = Audio
 S..... = Subtitle
 .F.... = Frame-level multithreading
 ..S... = Slice-level multithreading
 ...X.. = Codec is experimental
 ....B. = Supports draw_horiz_band
 .....D = Supports direct rendering method 1
 ------
 VFS..D h264                 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10
 V..... h264_cuvid           Nvidia CUVID H264 decoder (codec h264)
 V....D h264_qsv             H264 video (Intel Quick Sync Video acceleration) (codec h264)
 A....D aac                  AAC (Advanced Audio Coding)
 S..... subrip               SubRip subtitle
 V..... h264_cuvid           Nvidia CUVID H264 decoder (codec h264)
`
	got, err := parseCodecList([]byte(decoders), "Decoders")
	want := []string{"h264", "h264_cuvid", "h264_qsv", "aac", "subrip"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("parseCodecList(decoders) = %v, %v; want %v", got, err, want)
	}
	encoders := "Encoders:\n V..... = Video\n ------\n V....D libx264 H.264 / AVC (codec h264)\n V....D h264_nvenc NVIDIA NVENC H.264 encoder (codec h264)\n V....D h264_vaapi H.264/AVC (VAAPI) (codec h264)\n A....D aac AAC (Advanced Audio Coding)\n"
	got, err = parseCodecList([]byte(encoders), "Encoders")
	want = []string{"libx264", "h264_nvenc", "h264_vaapi", "aac"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("parseCodecList(encoders) = %v, %v; want %v", got, err, want)
	}
}

func TestParseFilterListCurrentAndLegacyFlags(t *testing.T) {
	// FFmpeg 9.0.1 retains three-column legends but emits two-column flags.
	output := `Filters:
  T.. = Timeline support
  .S. = Slice threading
  A = Audio input/output
  V = Video input/output
  N = Dynamic number and/or type of input/output
  | = Source or sink filter
  ------
 .. subtitles         V->V       Render text subtitles onto input video using the libass library.
 .. scale_vaapi       V->V       Scale to/from VAAPI surfaces.
 TS overlay           VV->V      Overlay a video source on top of the input.
 .. amix              N->A       Audio mixing.
 .. anullsrc          |->A       Null audio source.
 .. nullsink          V->|       Do absolutely nothing with the input video.
 .. concat            N->N       Concatenate audio and video streams.
 .. subtitles         V->V       Duplicate entry.
`
	got, err := parseFilterList([]byte(output))
	want := []string{"subtitles", "scale_vaapi", "overlay", "amix", "anullsrc", "nullsink", "concat"}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("parseFilterList() = %v, %v; want %v", got, err, want)
	}
	legacy := "Filters:\n ..C = Command support\n ------\n ..C volume A->A Change input volume.\n TSC overlay VV->V Overlay video.\n"
	got, err = parseFilterList([]byte(legacy))
	if err != nil || !slices.Equal(got, []string{"volume", "overlay"}) {
		t.Fatalf("legacy filter flags = %v, %v", got, err)
	}
	got, err = parseFilterList([]byte("No filters available: libavfilter disabled\n"))
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("disabled libavfilter must return an empty list: %v, %v", got, err)
	}
}

func TestParseCapabilityTablesRejectInvalidOutput(t *testing.T) {
	for _, output := range []string{
		"",
		"Encoders:\n ------\n V....D libx264 H.264 encoder\n",
		"Decoders:\n V..... h264 H.264 decoder\n",
		"Decoders:\n ------\n incomplete\n",
		"Decoders:\n ------\n DEVI.S h264 H.264 codec\n",
		"Decoders:\n ------\n V..... = Video\n",
	} {
		if _, err := parseCodecList([]byte(output), "Decoders"); err == nil {
			t.Errorf("accepted malformed decoder output %q", output)
		}
	}
	for _, output := range []string{
		"Filters:\n .. subtitles V->V Render subtitles\n",
		"Filters:\n ------\n incomplete\n",
		"Filters:\n ------\n V..... h264 V->V Invalid flags\n",
		"Filters:\n ------\n .. subtitles invalid Invalid media signature\n",
		"Filters:\n ------\n .. subtitles ->V Missing input\n",
		"Filters:\n ------\n .. subtitles V-> Missing output\n",
	} {
		if _, err := parseFilterList([]byte(output)); err == nil {
			t.Errorf("accepted malformed filter output %q", output)
		}
	}
}

func TestProbeCapabilitiesActualFFmpeg(t *testing.T) {
	path := os.Getenv("GOBY_FFMPEG")
	if path == "" {
		t.Skip("GOBY_FFMPEG is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	capabilities, err := ProbeCapabilities(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if capabilities.Version == "" {
		t.Fatal("FFmpeg version is empty")
	}
	for _, item := range []struct {
		name string
		list []string
		want string
	}{
		{"decoders", capabilities.Decoders, "h264"},
		{"decoders", capabilities.Decoders, "aac"},
		{"encoders", capabilities.Encoders, "libx264"},
		{"encoders", capabilities.Encoders, "aac"},
		{"filters", capabilities.Filters, "subtitles"},
	} {
		if !slices.Contains(item.list, item.want) {
			t.Errorf("configured FFmpeg %s do not include %q", item.name, item.want)
		}
	}
	if capabilities.HardwareVerified {
		t.Fatal("enumerating compiled interfaces must not verify hardware execution")
	}
}
