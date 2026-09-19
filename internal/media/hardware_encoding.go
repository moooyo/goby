package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	hardwareEncodingTimeout = 5 * time.Second
	hardwareEncodingBytes   = 8 * 1024 * 1024
)

// HardwareEncodingRequest is a single exact VAAPI output tuple. Its evidence
// does not establish decoder, filter, source, or other output tuple support.
type HardwareEncodingRequest struct {
	Device    string
	Codec     string
	Profile   string
	BitDepth  int
	Width     int
	Height    int
	FrameRate float64
	Bitrate   int64
}

// HardwareEncodingResult contains closed diagnostic classifications only.
// A usable tuple never changes the deployment-wide HardwareVerified status.
type HardwareEncodingResult struct {
	Usable bool
	Code   string
}

// ProbeVideoEncoders enumerates implementations without opening any GPU. This
// is sufficient to avoid advertising a fallback whose encoder is not compiled;
// it does not establish successful encoding of a particular source.
func ProbeVideoEncoders(ctx context.Context, ffmpeg string) ([]string, error) {
	output, err := runLimited(ctx, time.Second, capabilityOutputLimit, ffmpeg, "-hide_banner", "-encoders")
	if err != nil {
		return nil, err
	}
	return parseCodecList(output, "Encoders")
}

func (request HardwareEncodingRequest) valid() bool {
	device := strings.TrimPrefix(request.Device, "/dev/dri/renderD")
	number, err := strconv.Atoi(device)
	if err != nil || number < 128 || number > 255 || request.Device != "/dev/dri/renderD"+strconv.Itoa(number) ||
		request.Width < 2 || request.Width > 8192 || request.Width%2 != 0 || request.Height < 2 || request.Height > 8192 || request.Height%2 != 0 ||
		math.IsNaN(request.FrameRate) || math.IsInf(request.FrameRate, 0) || request.FrameRate != 0 && (request.FrameRate < 1 || request.FrameRate > 240) ||
		request.Bitrate < 0 || request.Bitrate > 200_000_000 || request.Bitrate != 0 && request.Bitrate < 64_000 {
		return false
	}
	switch request.Codec {
	case "h264":
		return request.BitDepth == 8 && (request.Profile == "baseline" || request.Profile == "main" || request.Profile == "high")
	case "hevc":
		return request.BitDepth == 8 && request.Profile == "main" || request.BitDepth == 10 && request.Profile == "main10"
	case "av1":
		return (request.BitDepth == 8 || request.BitDepth == 10) && request.Profile == "main"
	}
	return false
}

func hardwareEncodingArgs(request HardwareEncodingRequest) []string {
	pixel := "nv12"
	if request.BitDepth == 10 {
		pixel = "p010le"
	}
	rate, bitrate := request.FrameRate, request.Bitrate
	if rate == 0 {
		rate = 24
	}
	if bitrate == 0 {
		bitrate = 4_000_000
	}
	pattern := fmt.Sprintf("color=c=red:s=%dx%d:r=%s,drawbox=x=iw/2:y=0:w=iw/2:h=ih:color=blue:t=fill", request.Width, request.Height, strconv.FormatFloat(rate, 'f', -1, 64))
	args := []string{"-hide_banner", "-v", "error", "-nostdin", "-xerror", "-max_alloc", "268435456", "-timelimit", "3",
		"-init_hw_device", "vaapi=goby:" + request.Device, "-filter_hw_device", "goby",
		"-filter_threads", "1", "-threads", "1", "-f", "lavfi", "-i", pattern, "-map", "0:v:0", "-an", "-sn", "-dn",
		"-vf", "format=" + pixel + ",hwupload", "-frames:v", "3", "-c:v", request.Codec + "_vaapi", "-threads", "1",
		"-profile:v", request.Profile, "-b:v", strconv.FormatInt(bitrate, 10), "-g", "24", "-flags", "+cgop", "-idr_interval", "0"}
	if request.Codec == "h264" && request.Profile == "baseline" {
		args = append(args, "-coder", "cavlc")
	}
	return append(args, "-f", "matroska", "pipe:1")
}

func hardwareEncodingMetadataMatches(data []byte, request HardwareEncodingRequest) bool {
	var output struct {
		Streams []struct {
			Codec   string `json:"codec_name"`
			Profile string `json:"profile"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
			Pixel   string `json:"pix_fmt"`
			Frames  string `json:"nb_read_frames"`
		} `json:"streams"`
	}
	if json.Unmarshal(data, &output) != nil || len(output.Streams) != 1 {
		return false
	}
	stream := output.Streams[0]
	profile := strings.ToLower(strings.ReplaceAll(stream.Profile, " ", ""))
	if request.Codec == "h264" && profile == "constrainedbaseline" {
		profile = "baseline"
	}
	pixel := "yuv420p"
	if request.BitDepth == 10 {
		pixel = "yuv420p10le"
	}
	return stream.Codec == request.Codec && profile == request.Profile && stream.Width == request.Width && stream.Height == request.Height && stream.Pixel == pixel && stream.Frames == "3"
}

// A strict decode must contain three nonblank frames with the expected spatial
// color pattern. This is a narrow encoder sanity check, not a perceptual or
// source-dependent quality claim. Edge columns avoid lossy chroma boundaries.
func hardwareEncodingPixelsMatch(data []byte) bool {
	if len(data) != 3*8*8*3 {
		return false
	}
	for frame := 0; frame < 3; frame++ {
		for y := 0; y < 8; y++ {
			for _, x := range []int{0, 1, 6, 7} {
				pixel := data[(frame*64+y*8+x)*3:]
				if x < 4 {
					if pixel[0] < 140 || pixel[1] > 90 || pixel[2] > 90 {
						return false
					}
				} else if pixel[2] < 140 || pixel[0] > 90 || pixel[1] > 90 {
					return false
				}
			}
		}
	}
	return true
}

func hardwareEncodingRejected(ctx context.Context, code string) (HardwareEncodingResult, error) {
	if err := ctx.Err(); err != nil {
		return HardwareEncodingResult{}, err
	}
	return HardwareEncodingResult{Code: code}, nil
}
