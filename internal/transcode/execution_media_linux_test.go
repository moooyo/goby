//go:build linux

package transcode

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestExecutionCPUQualityActualOutputsPreserveFramesAndSegmentClocks(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.mp4")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=320x192:rate=12:duration=3", "-an",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-bf", "2", "-pix_fmt", "yuv420p", source)
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil || !info.FormatStartKnown || info.DurationTicks != 3*ticksPerSecond {
		t.Fatalf("fixture lacks an exact source clock: %+v, %v", info, err)
	}
	for _, codec := range []string{"h264", "hevc"} {
		for _, rateControl := range []string{"bitrate", "capped_crf"} {
			for _, output := range []string{"progressive", "legacy-hls", "mpegts", "fmp4"} {
				t.Run(codec+"/"+rateControl+"/"+output, func(t *testing.T) {
					p := progressiveVideoFixturePlan(info, codec, "", ticksPerSecond/2)
					p.VideoBitrate, p.FrameRate = 768000, 12
					p.Width, p.Height = videoCodecFixtureWidth, videoCodecFixtureHeight
					options := DefaultExecutionOptions(2)
					preset := "fast"
					if rateControl == "capped_crf" {
						preset = "medium"
					}
					options.H264 = CPUQuality{Preset: preset, RateControl: rateControl, CRF: 22}
					options.HEVC = CPUQuality{Preset: preset, RateControl: rateControl, CRF: 27}
					if output != "progressive" {
						p.OutputMode, p.SourceFormatStartKnown, p.SourceFormatStartTicks, p.StartTicks = "", false, 0, 0
						p.Container, p.SegmentSeconds = "ts", 1
						if output != "legacy-hls" {
							p.HLS.SegmentType = output
						}
						if output == "fmp4" {
							p.Container = "mp4"
							if rateControl == "capped_crf" {
								p.HLS.RenditionCount = 2
								p.HLS.Renditions[0] = HLSRendition{Width: 320, Height: 192, VideoBitrate: 768000}
								p.HLS.Renditions[1] = HLSRendition{Width: 160, Height: 96, VideoBitrate: 384000}
							}
						}
					}
					p, err := CaptureExecution(p, options)
					if err != nil {
						t.Fatal(err)
					}
					if output == "progressive" {
						path := runProgressiveVideo(t, ctx, ffmpeg, source, p)
						verifyVideoCodecFile(t, ctx, ffmpeg, ffprobe, path, p, 30, true)
						facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
						assertExecutionFrameClock(t, facts.video, 0)
						return
					}
					verifyExecutionHLSSegments(t, ctx, ffmpeg, ffprobe, source, p)
				})
			}
		}
	}
}

func verifyExecutionHLSSegments(t *testing.T, ctx context.Context, ffmpeg, ffprobe, source string, p Plan) {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	directory := t.TempDir()
	// The captured thread count remains authoritative over this legacy argument.
	result, err := Run(ctx, ffmpeg, directory, input, p, 1, nil)
	if err != nil {
		t.Fatalf("CPU quality HLS encoding failed: %v: %s", err, result.StderrTail)
	}
	var firstSegmentClock float64
	for rendition := 0; rendition < max(1, p.HLS.RenditionCount); rendition++ {
		encoded := p
		if p.HLS.RenditionCount > 0 {
			selected := p.HLS.Renditions[rendition]
			encoded.Width, encoded.Height, encoded.VideoBitrate = selected.Width, selected.Height, selected.VideoBitrate
		}
		data, err := os.ReadFile(filepath.Join(directory, HLSPlaylistName(rendition, p.HLS.RenditionCount)))
		if err != nil {
			t.Fatal(err)
		}
		playlist, err := ParseMediaPlaylist(data)
		if err != nil || !playlist.Ended || len(playlist.Segments) != 3 || playlist.Independent != GeneratedHLS(p) {
			t.Fatalf("CPU quality changed the three completed HLS cuts: %+v, %v", playlist, err)
		}
		var initialization []byte
		if playlist.InitName != "" {
			initialization, err = os.ReadFile(filepath.Join(directory, playlist.InitName))
			if err != nil {
				t.Fatal(err)
			}
		}
		for index, segment := range playlist.Segments {
			payload, err := os.ReadFile(filepath.Join(directory, segment.Name))
			if err != nil {
				t.Fatal(err)
			}
			standalone := filepath.Join(t.TempDir(), "segment."+p.Container)
			if err := os.WriteFile(standalone, append(append([]byte{}, initialization...), payload...), 0600); err != nil {
				t.Fatal(err)
			}
			// Decode every segment from a fresh process so an earlier segment
			// cannot supply missing parameter sets or an open GOP reference.
			verifyVideoCodecFile(t, ctx, ffmpeg, ffprobe, standalone, encoded, 12, p.Container == "mp4")
			facts := probeProgressiveVideoFrames(t, ctx, ffprobe, standalone)
			if rendition == 0 && index == 0 {
				firstSegmentClock = facts.video[0].time(t)
			}
			// Retain one origin for every cut and rendition. Normalizing each
			// segment independently would hide a reset, a gap, or overlap.
			assertExecutionFrameClock(t, facts.video, firstSegmentClock+float64(index))
			if math.Abs(float64(segment.DurationTicks-ticksPerSecond)) > float64(ticksPerSecond)/12 {
				t.Fatalf("CPU quality drifted from the forced one-second cut: %+v", segment)
			}
		}
	}
}

func assertExecutionFrameClock(t *testing.T, frames []progressiveVideoFrame, origin float64) {
	t.Helper()
	for index, frame := range frames {
		want := origin + float64(index)/12
		if math.Abs(frame.time(t)-want) > .002 {
			t.Fatalf("CPU quality changed frame %d PTS: got %.6f, want %.6f", index, frame.time(t), want)
		}
	}
}

func TestExecutionCapturedThreadsReachActualSeekPreflight(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	recorder := progressiveVideoSeekRecorder(t, ffmpeg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := progressiveVideoSources(t, ctx, ffmpeg)[0]
	input, info, candidate := progressiveVideoSeekSource(t, ctx, ffprobe, recorder, source, 43_700_000)
	options := DefaultExecutionOptions(2)
	options.H264 = CPUQuality{Preset: "fast", RateControl: "capped_crf", CRF: 23}
	plan, err := CaptureExecution(progressiveVideoFixturePlan(info, "h264", "aac", 43_700_000), options)
	if err != nil {
		t.Fatal(err)
	}
	// A stale outer runner argument must affect neither actual proof decoders
	// nor the conversion's two input decoders, filters and output encoder.
	linear := runRecordedProgressiveVideoSeekThreads(t, ctx, recorder, ffmpeg, ffprobe, input, plan, 2, 1)
	assertRecordedProgressiveVideoSeek(t, linear.args, plan, false)
	plan.VideoSeekCandidate = candidate
	fast := runRecordedProgressiveVideoSeekThreads(t, ctx, recorder, ffmpeg, ffprobe, input, plan, 2, 1)
	assertRecordedProgressiveVideoSeek(t, fast.args, plan, true)
	assertRecordedProgressiveVideoSeekProof(t, fast)
	for _, output := range []recordedProgressiveVideoSeek{linear, fast} {
		for _, key := range []string{"-filter_threads", "-filter_complex_threads", "-threads:v", "-threads:a"} {
			if !hasArgumentPair(output.args, key, "2") {
				t.Fatalf("captured threads missing from real conversion %s", key)
			}
		}
	}
	assertProgressiveVideoSeekOutputsEqual(t, linear, fast)
	assertProgressiveVideoSeekFullColorSourceWindow(t, ctx, ffmpeg, ffprobe, source, plan, linear, fast)
}
