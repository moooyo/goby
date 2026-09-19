//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

const videoCodecFixtureWidth, videoCodecFixtureHeight = 320, 192

func TestVideoCodecActualSoftwareOutputs(t *testing.T) {
	verifyVideoCodecOutputs(t, Hardware{}, []string{"h264", "hevc", "av1"})
}

func TestVideoCodecActualVAAPIOutputs(t *testing.T) {
	device, codecs := os.Getenv("GOBY_VAAPI_DEVICE"), os.Getenv("GOBY_VAAPI_VIDEO_CODECS")
	if device == "" || codecs == "" {
		t.Skip("GOBY_VAAPI_DEVICE and the hardware-supported GOBY_VAAPI_VIDEO_CODECS list are required")
	}
	if !validHardwareDevice("vaapi", device) {
		t.Fatal("invalid VAAPI device")
	}
	selected := strings.Split(codecs, ",")
	for _, codec := range selected {
		if !VideoEncodingSupported(codec) {
			t.Fatalf("unsupported hardware verification codec %q", codec)
		}
	}
	for _, pipeline := range []Hardware{{Encode: "vaapi", Device: device}, {Decode: "vaapi", Encode: "vaapi", Device: device}, {Decode: "vaapi", Device: device}} {
		decode, encode := hardwareSelection(pipeline)
		t.Run(decode+"-"+encode, func(t *testing.T) { verifyVideoCodecOutputs(t, pipeline, selected) })
	}
}

func verifyVideoCodecOutputs(t *testing.T, hardware Hardware, codecs []string) {
	t.Helper()
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x192:rate=12:duration=3", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=3",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-bf", "2", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || !info.FormatStartKnown || info.DurationTicks != 3*ticksPerSecond {
		t.Fatalf("fixture lacks an exact source clock: %+v, %v", info, err)
	}
	for _, codec := range codecs {
		for _, depth := range []int{8, 10} {
			if codec == "h264" && depth == 10 {
				continue
			}
			for _, mode := range []string{"progressive", "fmp4", "mpegts"} {
				if codec == "av1" && mode == "mpegts" {
					continue
				}
				t.Run(codec+"/"+strconv.Itoa(depth)+"/"+mode, func(t *testing.T) {
					p := progressiveVideoFixturePlan(info, codec, "aac", ticksPerSecond/2)
					p.VideoBitrate, p.VideoBitDepth, p.FrameRate, p.Hardware = 768000, depth, 12, hardware
					p.Width, p.Height = videoCodecFixtureWidth, videoCodecFixtureHeight
					if mode == "progressive" {
						path := runProgressiveVideo(t, ctx, ffmpeg, source, p)
						verifyVideoCodecFile(t, ctx, ffmpeg, ffprobe, path, p, 30, true)
						facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
						if math.Abs(facts.video[0].time(t)) > .002 || math.Abs(facts.video[29].time(t)-29.0/12) > .002 {
							t.Fatalf("encoding lost the requested source-time window: first=%g last=%g", facts.video[0].time(t), facts.video[29].time(t))
						}
						return
					}
					p.OutputMode, p.SourceFormatStartKnown, p.SourceFormatStartTicks, p.StartTicks = "", false, 0, 0
					p.Container, p.HLS.SegmentType, p.SegmentSeconds = "ts", "mpegts", 1
					if mode == "fmp4" {
						p.Container, p.HLS.SegmentType = "mp4", "fmp4"
					}
					input, err := os.Open(source)
					if err != nil {
						t.Fatal(err)
					}
					defer input.Close()
					directory := t.TempDir()
					result, err := Run(ctx, ffmpeg, directory, input, p, 1, nil)
					if err != nil {
						t.Fatalf("HLS encoding failed: %v: %s", err, result.StderrTail)
					}
					data, err := os.ReadFile(filepath.Join(directory, "main.m3u8"))
					if err != nil {
						t.Fatal(err)
					}
					playlist, err := ParseMediaPlaylist(data)
					if err != nil || !playlist.Ended || !playlist.Independent || len(playlist.Segments) != 3 {
						t.Fatalf("HLS does not expose three independent completed seconds: %+v, %v", playlist, err)
					}
					var init []byte
					if playlist.InitName != "" {
						init, err = os.ReadFile(filepath.Join(directory, playlist.InitName))
						if err != nil {
							t.Fatal(err)
						}
					}
					for _, segment := range playlist.Segments {
						payload, err := os.ReadFile(filepath.Join(directory, segment.Name))
						if err != nil {
							t.Fatal(err)
						}
						standalone := filepath.Join(t.TempDir(), "standalone."+p.Container)
						if err := os.WriteFile(standalone, append(append([]byte{}, init...), payload...), 0600); err != nil {
							t.Fatal(err)
						}
						// Decode each segment without the preceding segment. A keyframe
						// flag alone does not prove usable parameter sets or closed GOPs.
						verifyVideoCodecFile(t, ctx, ffmpeg, ffprobe, standalone, p, 12, mode == "fmp4")
						if math.Abs(float64(segment.DurationTicks-ticksPerSecond)) > float64(ticksPerSecond)/12 {
							t.Fatalf("forced segment boundary drifted: %+v", segment)
						}
					}
				})
			}
		}
	}
}

func verifyVideoCodecFile(t *testing.T, ctx context.Context, ffmpeg, ffprobe, path string, p Plan, frames int, mp4 bool) {
	t.Helper()
	data := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries",
		"stream=codec_name,codec_tag_string,profile,pix_fmt,width,height:packet=flags", "-of", "json", path)
	var document struct {
		Streams []struct {
			Codec   string `json:"codec_name"`
			Tag     string `json:"codec_tag_string"`
			Profile string `json:"profile"`
			Format  string `json:"pix_fmt"`
			Width   int    `json:"width"`
			Height  int    `json:"height"`
		} `json:"streams"`
		Packets []struct {
			Flags string `json:"flags"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(data, &document); err != nil || len(document.Streams) != 1 || len(document.Packets) == 0 {
		t.Fatalf("output has no inspectable coded stream: %s, %v", data, err)
	}
	video := document.Streams[0]
	profile := strings.ReplaceAll(strings.ToLower(video.Profile), " ", "")
	if profile == "constrainedbaseline" {
		profile = "baseline"
	}
	if video.Codec != p.VideoCodec || video.Format != videoSoftwarePixelFormat(p) || video.Width != p.Width || video.Height != p.Height ||
		profile != VideoOutputProfile(p) || mp4 && video.Tag != VideoMP4Tag(p) || !strings.Contains(document.Packets[0].Flags, "K") {
		t.Fatalf("coded output violates the codec, depth, sample-entry, canvas, or keyframe contract: %s", data)
	}
	strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
	facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
	if len(facts.video) != frames {
		t.Fatalf("output lost or duplicated decoded frames: got %d, want %d", len(facts.video), frames)
	}
}

// Output coverage above keeps one H.264 input so output codec and container
// failures are isolated. This matrix separately proves hardware decoding of
// each coded input profile. The first hardware download requires decoder-owned
// VAAPI surfaces; a later upload only feeds the selected hardware encoder.
func TestVideoCodecActualVAAPIDecodeInputMatrix(t *testing.T) {
	device, selected := os.Getenv("GOBY_VAAPI_DEVICE"), os.Getenv("GOBY_VAAPI_VIDEO_CODECS")
	if device == "" || selected == "" {
		t.Skip("GOBY_VAAPI_DEVICE and GOBY_VAAPI_VIDEO_CODECS are required")
	}
	if !validHardwareDevice("vaapi", device) {
		t.Fatal("invalid VAAPI device")
	}
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, codec := range strings.Split(selected, ",") {
		if !VideoEncodingSupported(codec) {
			t.Fatalf("unsupported input verification codec %q", codec)
		}
		for _, depth := range []int{8, 10} {
			if codec == "h264" && depth == 10 {
				continue
			}
			t.Run(codec+"/"+strconv.Itoa(depth), func(t *testing.T) {
				source := filepath.Join(t.TempDir(), "source.mp4")
				encoding := Plan{Container: "mp4", VideoCodec: codec, VideoBitDepth: depth}
				args := []string{"-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1", "-f", "lavfi", "-i", "testsrc2=size=320x192:rate=12:duration=3",
					"-vf", "format=" + videoSoftwarePixelFormat(encoding), "-an", "-c:v", VideoEncoder(codec, "software"), "-threads:v", "1", "-bf", "0", "-g", "12", "-b:v", "768000", "-tag:v", VideoMP4Tag(encoding)}
				args = appendVideoEncoderOptions(args, encoding, "software", 1)
				args = append(args, source)
				if output, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
					t.Fatalf("cannot prepare the explicit input codec/profile: %v: %s", err, output)
				}
				info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
				if err != nil || len(info.Streams) != 1 || info.Streams[0].Codec != codec || media.VideoBitDepthConflict(info.Streams[0]) ||
					media.EffectiveVideoBitDepth(info.Streams[0]) != depth || info.Streams[0].PixelFormat != videoSoftwarePixelFormat(encoding) ||
					info.Streams[0].Width != videoCodecFixtureWidth || info.Streams[0].Height != videoCodecFixtureHeight {
					t.Fatalf("input does not prove the selected codec and bit depth: %+v, %v", info, err)
				}
				reference := decodeProgressiveVideoPixels(t, ctx, ffmpeg, source)
				for _, backend := range []string{"software", "vaapi"} {
					t.Run("decode-vaapi-encode-"+backend, func(t *testing.T) {
						p := progressiveVideoFixturePlan(info, codec, "", ticksPerSecond/2)
						p.VideoBitDepth, p.VideoBitrate, p.FrameRate = depth, 768000, 12
						p.Width, p.Height = videoCodecFixtureWidth, videoCodecFixtureHeight
						p.Hardware = Hardware{Decode: "vaapi", Encode: backend, Device: device}
						if media.EffectiveVideoBitDepth(info.Streams[0]) == 10 {
							p.VideoFilters = VideoFilters{Backend: "vulkan", SourceBitDepth: 10}
						}
						arguments, err := BuildArgs(p, 1)
						if err != nil {
							t.Fatal(err)
						}
						filter := videoFilter(p, "vaapi", backend)
						download := strings.Index(filter, "hwdownload")
						upload := strings.Index(filter, "hwupload")
						if !hasArgumentPair(arguments, "-hwaccel", "vaapi") || !hasArgumentPair(arguments, "-hwaccel_output_format", "vaapi") ||
							download < 0 || upload >= 0 && upload < download || (backend == "vaapi") != (upload > download) {
							t.Fatalf("decode proof would permit software pixels to masquerade as VAAPI surfaces: %v", arguments)
						}
						path := runProgressiveVideo(t, ctx, ffmpeg, source, p)
						verifyVideoCodecFile(t, ctx, ffmpeg, ffprobe, path, p, 30, true)
						verifyVideoCodecDecodedPixels(t, reference, decodeProgressiveVideoPixels(t, ctx, ffmpeg, path), 6)
					})
				}
			})
		}
	}
}

func verifyVideoCodecDecodedPixels(t *testing.T, reference, actual []byte, skippedFrames int) {
	t.Helper()
	frameBytes := videoCodecFixtureWidth * videoCodecFixtureHeight
	if len(reference) != 36*frameBytes || len(actual) != (36-skippedFrames)*frameBytes {
		t.Fatalf("decoded input/output pixel counts disagree: input=%d output=%d", len(reference), len(actual))
	}
	reference = reference[skippedFrames*frameBytes:]
	for offset := 0; offset < len(actual); offset += frameBytes {
		var difference int64
		for index := offset; index < offset+frameBytes; index++ {
			delta := int64(actual[index]) - int64(reference[index])
			if delta < 0 {
				delta = -delta
			}
			difference += delta
		}
		// Encoding is lossy, but a valid decode path must retain the moving
		// test pattern at the requested source frame rather than blank output.
		if float64(difference)/float64(frameBytes) > 15 {
			t.Fatalf("hardware decoded frame %d differs from its software reference: mean error=%g", offset/frameBytes, float64(difference)/float64(frameBytes))
		}
	}
}
