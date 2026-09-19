package media

import (
	"fmt"
	"strings"
	"testing"
)

func TestHardwareEncodingEvidenceRejectsPaddingDepthAndProfileChanges(t *testing.T) {
	request := HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "av1", Profile: "main", BitDepth: 10, Width: 318, Height: 190}
	metadata := func(codec, profile, pixel, frames string, width, height int) []byte {
		return []byte(fmt.Sprintf(`{"streams":[{"codec_name":%q,"profile":%q,"pix_fmt":%q,"width":%d,"height":%d,"nb_read_frames":%q}]}`, codec, profile, pixel, width, height, frames))
	}
	if !hardwareEncodingMetadataMatches(metadata("av1", "Main", "yuv420p10le", "3", 318, 190), request) {
		t.Fatal("the exact output tuple was rejected")
	}
	for _, output := range [][]byte{
		metadata("av1", "Main", "yuv420p10le", "3", 320, 192),
		metadata("av1", "Main", "yuv420p", "3", 318, 190),
		metadata("av1", "High", "yuv420p10le", "3", 318, 190),
		metadata("hevc", "Main", "yuv420p10le", "3", 318, 190),
		metadata("av1", "Main", "yuv420p10le", "2", 318, 190),
		[]byte(`{"streams":[]}`), []byte(`{"streams":[{},{}]}`), []byte(`{`),
	} {
		if hardwareEncodingMetadataMatches(output, request) {
			t.Fatalf("inexact or incomplete hardware output was admitted: %s", output)
		}
	}
}

func TestHardwareEncodingPatternRejectsBlankSwappedAndTruncatedFrames(t *testing.T) {
	data := make([]byte, 3*8*8*3)
	for frame := 0; frame < 3; frame++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				channel := 0
				if x >= 4 {
					channel = 2
				}
				data[(frame*64+y*8+x)*3+channel] = 250
			}
		}
	}
	if !hardwareEncodingPixelsMatch(data) {
		t.Fatal("the expected decoded pattern was rejected")
	}
	if hardwareEncodingPixelsMatch(data[:len(data)-1]) || hardwareEncodingPixelsMatch(make([]byte, len(data))) {
		t.Fatal("blank or incomplete decoded frames were admitted")
	}
	for index := 0; index < len(data); index += 3 {
		data[index], data[index+2] = data[index+2], data[index]
	}
	if hardwareEncodingPixelsMatch(data) {
		t.Fatal("a spatially incorrect color pattern was admitted")
	}
}

func TestHardwareEncodingArgumentsRemainSyntheticAndBounded(t *testing.T) {
	request := HardwareEncodingRequest{Device: "/dev/dri/renderD128", Codec: "hevc", Profile: "main10", BitDepth: 10, Width: 318, Height: 190, FrameRate: 29.97, Bitrate: 700_000}
	if !request.valid() {
		t.Fatal("the supported tuple was rejected")
	}
	args := strings.Join(hardwareEncodingArgs(request), " ")
	for _, expected := range []string{"-frames:v 3", "color=c=red:s=318x190:r=29.97", "drawbox=", "format=p010le,hwupload", "-c:v hevc_vaapi", "-profile:v main10", "-b:v 700000", "-filter_threads 1", "-threads 1", "-max_alloc 268435456", "-timelimit 3", "-f matroska pipe:1"} {
		if !strings.Contains(args, expected) {
			t.Fatalf("admission omitted its exact tuple or budget: %s", expected)
		}
	}
	for _, change := range []func(*HardwareEncodingRequest){
		func(r *HardwareEncodingRequest) { r.Device = "http://untrusted/" }, func(r *HardwareEncodingRequest) { r.Width = 8194 },
		func(r *HardwareEncodingRequest) { r.Height = 191 }, func(r *HardwareEncodingRequest) { r.Profile = "main" },
		func(r *HardwareEncodingRequest) { r.Codec = "untrusted" }, func(r *HardwareEncodingRequest) { r.FrameRate = 241 },
	} {
		changed := request
		change(&changed)
		if changed.valid() {
			t.Fatal("an unsupported synthetic request was accepted")
		}
	}
}
