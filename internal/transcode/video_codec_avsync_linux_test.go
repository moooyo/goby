//go:build linux

package transcode

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestVideoCodecActualSoftwareAVSyncAfterSeek(t *testing.T) {
	verifyVideoCodecAVSyncAfterSeek(t, Hardware{}, []string{"hevc", "av1"})
}

func TestVideoCodecActualVAAPIAVSyncAfterSeek(t *testing.T) {
	device, selected := os.Getenv("GOBY_VAAPI_DEVICE"), os.Getenv("GOBY_VAAPI_VIDEO_CODECS")
	if device == "" || selected == "" {
		t.Skip("GOBY_VAAPI_DEVICE and GOBY_VAAPI_VIDEO_CODECS are required")
	}
	if !validHardwareDevice("vaapi", device) {
		t.Fatal("invalid VAAPI device")
	}
	var codecs []string
	for _, codec := range strings.Split(selected, ",") {
		if !VideoEncodingSupported(codec) {
			t.Fatalf("unsupported hardware verification codec %q", codec)
		}
		if codec == "hevc" || codec == "av1" {
			codecs = append(codecs, codec)
		}
	}
	if len(codecs) == 0 {
		t.Skip("the selected worker has no HEVC or AV1 output to verify")
	}
	verifyVideoCodecAVSyncAfterSeek(t, Hardware{Decode: "vaapi", Encode: "vaapi", Device: device}, codecs)
}

func verifyVideoCodecAVSyncAfterSeek(t *testing.T, hardware Hardware, codecs []string) {
	t.Helper()
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	// The changing picture and chirp establish independent content clocks. The
	// fractional seek is between source keyframes, and audio is re-encoded so
	// retaining a valid AAC track cannot conceal an incorrect trim or offset.
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x192:rate=24:duration=4.5",
		"-f", "lavfi", "-i", "aevalsrc=0.6*sin(2*PI*(300*t+90*t*t)):s=48000:d=4.5",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast",
		"-g", "48", "-keyint_min", "48", "-sc_threshold", "0", "-bf", "2", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "4.5", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || !info.FormatStartKnown || info.DurationTicks != 45*ticksPerSecond/10 {
		t.Fatalf("A/V fixture lacks its bounded source clock: %+v, %v", info, err)
	}
	reference := progressiveVideoReference{info: info, facts: probeProgressiveVideoFrames(t, ctx, ffprobe, source),
		pcm: decodeProgressivePCM(t, ctx, ffmpeg, source, 16)}
	if len(reference.facts.video) != 108 || len(reference.facts.audio) == 0 || len(reference.pcm) == 0 {
		t.Fatal("A/V source does not contain complete independent video and audio clocks")
	}
	for _, codec := range codecs {
		for _, depth := range []int{8, 10} {
			t.Run(codec+"/"+strconv.Itoa(depth), func(t *testing.T) {
				plan := progressiveVideoFixturePlan(info, codec, "aac", 125*ticksPerSecond/100)
				plan.Width, plan.Height = videoCodecFixtureWidth, videoCodecFixtureHeight
				plan.VideoBitrate, plan.VideoBitDepth, plan.FrameRate, plan.Hardware = 768000, depth, 24, hardware
				path := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
				verifyVideoCodecFile(t, ctx, ffmpeg, ffprobe, path, plan, 78, true)
				facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
				if len(facts.video) != 78 || len(facts.audio) == 0 {
					t.Fatal("encoded output lost its complete video or AAC presentation")
				}
				origin := float64(info.FormatStartTicks+plan.StartTicks) / float64(ticksPerSecond)
				for index, frame := range facts.video {
					want := reference.facts.video[index+30].time(t) - origin
					if math.Abs(frame.time(t)-want) > .002 {
						t.Fatalf("video frame %d moved on the shared source clock: got=%.6f want=%.6f", index, frame.time(t), want)
					}
				}
				assertProgressiveChirpWindow(t, reference, plan, facts, decodeProgressivePCM(t, ctx, ffmpeg, path, 16))
				videoFirst, videoLast := facts.video[0], facts.video[len(facts.video)-1]
				audioFirst, audioLast := facts.audio[0], facts.audio[len(facts.audio)-1]
				videoEnd := videoLast.time(t) + videoLast.duration(t)
				audioEnd := audioLast.time(t) + float64(audioLast.Samples)/48000
				wantEnd := float64(plan.DurationTicks-plan.StartTicks) / float64(ticksPerSecond)
				// One AAC access unit is the explicit terminal padding budget.
				const audioClockTolerance = 1024.0/48000 + .002
				if math.Abs(videoFirst.time(t)) > .002 || math.Abs(videoEnd-wantEnd) > .002 ||
					math.Abs(audioFirst.time(t)-videoFirst.time(t)) > audioClockTolerance || math.Abs(audioEnd-videoEnd) > audioClockTolerance {
					t.Fatalf("encoded A/V clocks disagree after seek: video=[%.6f,%.6f] audio=[%.6f,%.6f] wanted_end=%.6f",
						videoFirst.time(t), videoEnd, audioFirst.time(t), audioEnd, wantEnd)
				}
			})
		}
	}
}
