package media

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestHardwareEncodingActualVAAPIAdmission(t *testing.T) {
	device, codecs := os.Getenv("GOBY_VAAPI_DEVICE"), os.Getenv("GOBY_VAAPI_VIDEO_CODECS")
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if device == "" || codecs == "" || ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_VAAPI_DEVICE, GOBY_VAAPI_VIDEO_CODECS, GOBY_FFMPEG, and GOBY_FFPROBE are required")
	}
	for _, codec := range strings.Split(codecs, ",") {
		for _, depth := range []int{8, 10} {
			if codec == "h264" && depth == 10 {
				continue
			}
			t.Run(codec+"/"+strconv.Itoa(depth), func(t *testing.T) {
				profile := "main"
				if codec == "h264" {
					profile = "high"
				} else if codec == "hevc" && depth == 10 {
					profile = "main10"
				}
				request := HardwareEncodingRequest{Device: device, Codec: codec, Profile: profile, BitDepth: depth,
					Width: 320, Height: 192, FrameRate: 24, Bitrate: 768_000}
				result, err := ProbeHardwareEncoding(context.Background(), ffmpeg, ffprobe, request)
				if err != nil || !result.Usable {
					t.Fatalf("configured hardware could not prove an exact output tuple: %+v, %v", result, err)
				}
			})
		}
	}
}

func TestHardwareEncodingActualAV1PaddingRejection(t *testing.T) {
	if os.Getenv("GOBY_VAAPI_EXPECT_AV1_PADDING") != "1" {
		t.Skip("GOBY_VAAPI_EXPECT_AV1_PADDING=1 selects a worker with independently observed AV1 padding")
	}
	device, ffmpeg, ffprobe := os.Getenv("GOBY_VAAPI_DEVICE"), os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if device == "" || ffmpeg == "" || ffprobe == "" {
		t.Fatal("explicit VAAPI and tool paths are required")
	}
	for _, size := range [][2]int{{320, 180}, {318, 190}} {
		request := HardwareEncodingRequest{Device: device, Codec: "av1", Profile: "main", BitDepth: 8,
			Width: size[0], Height: size[1], FrameRate: 24, Bitrate: 768_000}
		result, err := ProbeHardwareEncoding(context.Background(), ffmpeg, ffprobe, request)
		if err != nil || result.Usable || result.Code != "hardware_encoding_output_mismatch" {
			t.Fatalf("silently padded output was not rejected as a contract mismatch: %+v, %v", result, err)
		}
	}
}
