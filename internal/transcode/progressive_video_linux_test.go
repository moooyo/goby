//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestProgressiveVideoActualContainersCodecsAndSeek(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	for _, source := range progressiveVideoSources(t, ctx, ffmpeg) {
		t.Run(filepath.Ext(source)[1:], func(t *testing.T) {
			reference := readProgressiveVideoReference(t, ctx, ffmpeg, ffprobe, source)
			if len(reference.facts.video) != 150 {
				t.Fatalf("fixture has %d video frames, want 150", len(reference.facts.video))
			}
			bFrames := 0
			for _, frame := range reference.facts.video {
				if frame.PictureType == "B" {
					bFrames++
				}
			}
			if bFrames == 0 {
				t.Fatal("fixture does not exercise reordered video frames")
			}
			if filepath.Ext(source) == ".ts" {
				delay := reference.facts.video[0].time(t) - float64(reference.info.FormatStartTicks)/float64(ticksPerSecond)
				if math.Abs(delay-1024.0/48000) > .002 {
					t.Fatalf("transport fixture lost its video/audio origin difference: %.6f", delay)
				}
			}
			for _, start := range []int64{0, 23_700_000} {
				for _, videoCodec := range []string{"copy", "h264"} {
					if start > 0 && videoCodec == "copy" {
						continue
					}
					var firstVideoTime float64
					for audioIndex, audioCodec := range []string{"copy", "aac", ""} {
						name := fmt.Sprintf("start-%d/%s/%s", start, videoCodec, audioCodec)
						if audioCodec == "" {
							name += "no-audio"
						}
						t.Run(name, func(t *testing.T) {
							plan := progressiveVideoFixturePlan(reference.info, videoCodec, audioCodec, start)
							path := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
							facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
							assertProgressiveVideoStreams(t, facts, audioCodec != "")
							strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
							pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, path)
							assertProgressiveVideoWindow(t, reference, plan, facts, pixels)
							if audioIndex == 0 {
								firstVideoTime = facts.video[0].time(t)
							} else if math.Abs(facts.video[0].time(t)-firstVideoTime) > .002 {
								t.Fatalf("audio selection shifted video origin: %.6f versus %.6f", facts.video[0].time(t), firstVideoTime)
							}
							if audioCodec != "" {
								pcm := decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
								assertProgressiveChirpWindow(t, reference, plan, facts, pcm)
							}
						})
					}
				}
			}
		})
	}
}

func TestProgressiveVideoFrameRatePreservesVFRAndPhysicallyLimitsFrames(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "variable.mp4")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=2.5", "-an",
		"-vf", "select='not(eq(mod(n,5),2))'", "-fps_mode", "passthrough",
		"-c:v", "libx264", "-threads:v", "1", "-preset", "veryfast", "-crf", "18", "-g", "48", "-bf", "2", "-pix_fmt", "yuv420p", source)
	reference := readProgressiveVideoReference(t, ctx, ffmpeg, ffprobe, source)
	if len(reference.facts.video) != 48 {
		t.Fatalf("VFR fixture has %d frames, want 48", len(reference.facts.video))
	}
	shortGap, longGap := false, false
	for i := 1; i < len(reference.facts.video); i++ {
		gap := reference.facts.video[i].time(t) - reference.facts.video[i-1].time(t)
		shortGap = shortGap || math.Abs(gap-1.0/24) < .002
		longGap = longGap || math.Abs(gap-2.0/24) < .002
	}
	if !shortGap || !longGap {
		t.Fatal("VFR fixture did not retain its unequal presentation intervals")
	}
	for _, rate := range []float64{0, 12} {
		t.Run(fmt.Sprintf("rate-%g", rate), func(t *testing.T) {
			plan := progressiveVideoFixturePlan(reference.info, "h264", "", 0)
			plan.FrameRate = rate
			path := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
			assertProgressiveVideoStreams(t, facts, false)
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
			pixels := decodeProgressiveVideoPixels(t, ctx, ffmpeg, path)
			if rate == 0 {
				// An unspecified plan rate must preserve the actual timestamps;
				// an average-rate metadata value cannot establish a CFR timeline.
				assertProgressiveVideoWindow(t, reference, plan, facts, pixels)
				return
			}
			if len(facts.video) != 30 || len(pixels) != 30*progressiveVideoFrameBytes {
				t.Fatalf("12 FPS was not applied to physical frames: probe=%d pixels=%d", len(facts.video), len(pixels)/progressiveVideoFrameBytes)
			}
			for i, frame := range facts.video {
				if math.Abs(frame.time(t)-float64(i)/rate) > .002 {
					t.Fatalf("limited frame %d has PTS %.6f, want %.6f", i, frame.time(t), float64(i)/rate)
				}
			}
		})
	}
}

const progressiveVideoFrameBytes = 160 * 90

type progressiveVideoFrame struct {
	Type           string `json:"media_type"`
	Timestamp      string `json:"best_effort_timestamp_time"`
	Duration       string `json:"duration_time"`
	PacketDuration string `json:"pkt_duration_time"`
	PictureType    string `json:"pict_type"`
	Samples        int    `json:"nb_samples"`
}

func (f progressiveVideoFrame) time(t *testing.T) float64 {
	t.Helper()
	value, err := strconv.ParseFloat(f.Timestamp, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		t.Fatalf("missing or invalid decoded frame timestamp %q", f.Timestamp)
	}
	return value
}

func (f progressiveVideoFrame) duration(t *testing.T) float64 {
	t.Helper()
	value := f.Duration
	if value == "" || value == "N/A" {
		value = f.PacketDuration
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		t.Fatalf("missing or invalid decoded frame duration %q", value)
	}
	return seconds
}

type progressiveVideoFacts struct {
	streams []struct {
		Type  string `json:"codec_type"`
		Codec string `json:"codec_name"`
	}
	video []progressiveVideoFrame
	audio []progressiveVideoFrame
}

type progressiveVideoReference struct {
	info   media.Info
	facts  progressiveVideoFacts
	pixels []byte
	pcm    []byte
}

func progressiveVideoTools(t *testing.T) (string, string) {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("FFmpeg and ffprobe are required for progressive video verification")
	}
	return ffmpeg, ffprobe
}

func progressiveVideoSources(t *testing.T, ctx context.Context, ffmpeg string) []string {
	t.Helper()
	directory := t.TempDir()
	source := filepath.Join(directory, "source.mp4")
	// The changing video and linear chirp provide independent content clocks.
	// Fixed two-second GOPs put the fractional seek between random-access points.
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=6.25",
		"-f", "lavfi", "-i", "aevalsrc=0.6*sin(2*PI*(300*t+90*t*t)):s=48000:d=6.25",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1",
		"-preset", "veryfast", "-crf", "18", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", "-bf", "2", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "6.25", source)
	paths := []string{source}
	for _, extension := range []string{".mkv", ".ts"} {
		path := filepath.Join(directory, "source"+extension)
		progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-i", source,
			"-map", "0:v:0", "-map", "0:a:0", "-c", "copy", path)
		paths = append(paths, path)
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 3<<20 {
			t.Fatalf("video fixture exceeds its small-file bound: %s, %d bytes", path, info.Size())
		}
	}
	return paths
}

func readProgressiveVideoReference(t *testing.T, ctx context.Context, ffmpeg, ffprobe, path string) progressiveVideoReference {
	t.Helper()
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.FormatStartKnown || info.DurationTicks <= 0 {
		t.Fatalf("fixture lacks a trusted format clock and duration: %+v", info)
	}
	reference := progressiveVideoReference{info: info, facts: probeProgressiveVideoFrames(t, ctx, ffprobe, path), pixels: decodeProgressiveVideoPixels(t, ctx, ffmpeg, path)}
	if len(reference.pixels) != len(reference.facts.video)*progressiveVideoFrameBytes || len(reference.facts.video) == 0 {
		t.Fatalf("source frame scan and complete decode disagree: frames=%d bytes=%d", len(reference.facts.video), len(reference.pixels))
	}
	if len(reference.facts.audio) != 0 {
		reference.pcm = decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
	}
	return reference
}

func progressiveVideoFixturePlan(info media.Info, videoCodec, audioCodec string, start int64) Plan {
	plan := Plan{OutputMode: "progressive", Container: "mp4", VideoCodec: videoCodec, AudioCodec: audioCodec,
		VideoStreamIndex: -1, AudioStreamIndex: -1, DurationTicks: info.DurationTicks, StartTicks: start,
		SourceFormatStartKnown: info.FormatStartKnown, SourceFormatStartTicks: info.FormatStartTicks}
	for _, stream := range info.Streams {
		if stream.CodecType == "video" {
			plan.VideoStreamIndex = stream.Index
		}
		if stream.CodecType == "audio" && audioCodec != "" {
			plan.AudioStreamIndex = stream.Index
		}
	}
	if videoCodec == "h264" {
		plan.VideoBitrate = 768000
	}
	if audioCodec == "aac" {
		plan.AudioBitrate, plan.AudioChannels, plan.AudioSampleRate = 96000, 1, 48000
	}
	return plan
}

func runProgressiveVideo(t *testing.T, ctx context.Context, ffmpeg, source string, plan Plan) string {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	directory := t.TempDir()
	var ready, ended bool
	result, err := Run(ctx, ffmpeg, directory, input, plan, 1, func(progress Progress) {
		ready = ready || progress.Ready && progress.Bytes > 0
		ended = ended || progress.Ended
	})
	if err != nil || !ready || !ended {
		t.Fatalf("progressive video failed: %v ready=%t ended=%t: %s", err, ready, ended, result.StderrTail)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "stream.bin" {
		t.Fatalf("unexpected progressive output entries: %v", entries)
	}
	path := filepath.Join(directory, "stream.bin")
	assertProgressiveFileClosed(t, path)
	return path
}

func progressiveVideoCommand(t *testing.T, ctx context.Context, executable string, args ...string) []byte {
	t.Helper()
	command := exec.CommandContext(ctx, executable, args...)
	var output, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err != nil || diagnostic.Len() != 0 {
		t.Fatalf("progressive video media command: %v: %s", err, diagnostic.String())
	}
	return output.Bytes()
}

func probeProgressiveVideoFrames(t *testing.T, ctx context.Context, ffprobe, path string) progressiveVideoFacts {
	t.Helper()
	data := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-show_frames", "-show_streams",
		"-show_entries", "frame=media_type,best_effort_timestamp_time,duration_time,pkt_duration_time,pict_type,nb_samples:stream=codec_type,codec_name", "-of", "json", path)
	var document struct {
		Frames  []progressiveVideoFrame `json:"frames"`
		Streams []struct {
			Type  string `json:"codec_type"`
			Codec string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	facts := progressiveVideoFacts{streams: document.Streams}
	for _, frame := range document.Frames {
		_ = frame.time(t)
		switch frame.Type {
		case "video":
			facts.video = append(facts.video, frame)
		case "audio":
			facts.audio = append(facts.audio, frame)
		}
	}
	return facts
}

func assertProgressiveVideoStreams(t *testing.T, facts progressiveVideoFacts, audio bool) {
	t.Helper()
	videoStreams, audioStreams := 0, 0
	for _, stream := range facts.streams {
		switch stream.Type {
		case "video":
			videoStreams++
			if stream.Codec != "h264" {
				t.Fatalf("unexpected video codec %q", stream.Codec)
			}
		case "audio":
			audioStreams++
			if stream.Codec != "aac" {
				t.Fatalf("unexpected audio codec %q", stream.Codec)
			}
		default:
			t.Fatalf("unexpected output stream %q", stream.Type)
		}
	}
	wantAudio := 0
	if audio {
		wantAudio = 1
	}
	if videoStreams != 1 || audioStreams != wantAudio || len(facts.video) == 0 || audio && len(facts.audio) == 0 {
		t.Fatalf("unexpected output stream counts: video=%d audio=%d", videoStreams, audioStreams)
	}
}

func strictDecodeProgressiveVideo(t *testing.T, ctx context.Context, ffmpeg, path string) {
	t.Helper()
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1",
		"-i", path, "-map", "0:v:0", "-map", "0:a:0?", "-threads", "1", "-fps_mode", "passthrough", "-f", "null", "-")
}

func decodeProgressiveVideoPixels(t *testing.T, ctx context.Context, ffmpeg, path string) []byte {
	t.Helper()
	return progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-xerror", "-threads", "1", "-filter_threads", "1",
		"-i", path, "-map", "0:v:0", "-an", "-sn", "-dn", "-c:v", "rawvideo", "-threads:v", "1", "-pix_fmt", "gray", "-fps_mode", "passthrough", "-f", "rawvideo", "pipe:1")
}

func assertProgressiveVideoWindow(t *testing.T, reference progressiveVideoReference, plan Plan, facts progressiveVideoFacts, pixels []byte) {
	t.Helper()
	origin := float64(reference.info.FormatStartTicks) / float64(ticksPerSecond)
	start := float64(plan.StartTicks) / float64(ticksPerSecond)
	first := 0
	for first < len(reference.facts.video) && reference.facts.video[first].time(t)-origin < start-.000001 {
		first++
	}
	want := len(reference.facts.video) - first
	if len(facts.video) != want || len(pixels) != want*progressiveVideoFrameBytes {
		t.Fatalf("selected source window [%d,%d) produced probe=%d decoded=%d frames", first, len(reference.facts.video), len(facts.video), len(pixels)/progressiveVideoFrameBytes)
	}
	for i, frame := range facts.video {
		expected := reference.facts.video[first+i].time(t) - origin - start
		if math.Abs(frame.time(t)-expected) > .002 {
			t.Fatalf("frame %d has PTS %.6f, source frame %d requires %.6f", i, frame.time(t), first+i, expected)
		}
		if i > 0 && frame.time(t) <= facts.video[i-1].time(t) {
			t.Fatalf("video presentation timestamps are not strictly increasing at frame %d", i)
		}
	}
	last, sourceLast := facts.video[len(facts.video)-1], reference.facts.video[len(reference.facts.video)-1]
	end, expectedEnd := last.time(t)+last.duration(t), sourceLast.time(t)+sourceLast.duration(t)-origin-start
	if math.Abs(end-expectedEnd) > .002 {
		t.Fatalf("video presentation ends at %.6f, source window ends at %.6f", end, expectedEnd)
	}
	if plan.VideoCodec == "copy" {
		if !bytes.Equal(pixels, reference.pixels[first*progressiveVideoFrameBytes:]) {
			t.Fatal("video copy changed, repeated, omitted, or reordered decoded source frames")
		}
		return
	}
	// Compare with the independently decoded source, including nearby wrong
	// frames. A valid H264 stream alone cannot prove an accurate content seek.
	for _, index := range []int{0, want / 2, want - 1} {
		actual := pixels[index*progressiveVideoFrameBytes : (index+1)*progressiveVideoFrameBytes]
		expectedIndex := first + index
		expected := reference.pixels[expectedIndex*progressiveVideoFrameBytes : (expectedIndex+1)*progressiveVideoFrameBytes]
		mse := progressiveVideoMSE(actual, expected)
		if mse > .001 {
			t.Fatalf("frame %d differs from source frame %d: normalized MSE %.6f", index, expectedIndex, mse)
		}
		for _, neighbor := range []int{expectedIndex - 1, expectedIndex + 1} {
			if neighbor < 0 || neighbor >= len(reference.facts.video) {
				continue
			}
			wrong := reference.pixels[neighbor*progressiveVideoFrameBytes : (neighbor+1)*progressiveVideoFrameBytes]
			if other := progressiveVideoMSE(actual, wrong); other <= mse*2 {
				t.Fatalf("frame %d does not identify source frame %d unambiguously: correct MSE %.6f, neighbor %d MSE %.6f", index, expectedIndex, mse, neighbor, other)
			}
		}
	}
}

func progressiveVideoMSE(a, b []byte) float64 {
	var sum float64
	for i, value := range a {
		difference := float64(value) - float64(b[i])
		sum += difference * difference
	}
	return sum / (float64(len(a)) * 255 * 255)
}

func assertProgressiveChirpWindow(t *testing.T, reference progressiveVideoReference, plan Plan, facts progressiveVideoFacts, pcm []byte) {
	t.Helper()
	const sampleRate = 48000
	var decodedSamples int
	for i, frame := range facts.audio {
		if frame.Samples <= 0 {
			t.Fatalf("AAC frame %d has no decoded samples", i)
		}
		decodedSamples += frame.Samples
		if i > 0 {
			previous := facts.audio[i-1]
			expected := previous.time(t) + float64(previous.Samples)/sampleRate
			if math.Abs(frame.time(t)-expected) > .002 {
				t.Fatalf("AAC frame %d has a presentation gap or overlap: %.6f, want %.6f", i, frame.time(t), expected)
			}
		}
	}
	if len(pcm) != decodedSamples*2 || decodedSamples < sampleRate {
		t.Fatalf("AAC frame/sample decode mismatch: probe=%d samples, decode=%d bytes", decodedSamples, len(pcm))
	}
	first := facts.audio[0].time(t)
	last := facts.audio[len(facts.audio)-1]
	end := last.time(t) + float64(last.Samples)/sampleRate
	sourceLast := reference.facts.audio[len(reference.facts.audio)-1]
	wantEnd := min(float64(plan.DurationTicks-plan.StartTicks)/float64(ticksPerSecond),
		sourceLast.time(t)+float64(sourceLast.Samples)/sampleRate-float64(plan.StartTicks+reference.info.FormatStartTicks)/float64(ticksPerSecond))
	if first < -.05 || first > .05 || math.Abs(end-wantEnd) > 1024.0/sampleRate+.002 {
		t.Fatalf("AAC presentation window [%.6f, %.6f] does not cover source audio end %.6f", first, end, wantEnd)
	}
	if math.Abs(float64(decodedSamples)/sampleRate-(end-first)) > .002 {
		t.Fatalf("AAC timestamp span %.6f differs from decoded duration %.6f", end-first, float64(decodedSamples)/sampleRate)
	}
	// Compare the chirp frequency at two windows on the source clock. This
	// tolerates AAC phase changes while detecting stale or shifted seek content.
	sourceOffset := first + float64(plan.StartTicks+reference.info.FormatStartTicks)/float64(ticksPerSecond) - reference.facts.audio[0].time(t)
	for _, offset := range []float64{.25, float64(decodedSamples)/sampleRate - .5} {
		actualFrequency := progressiveChirpFrequency(t, pcm, int(math.Round(offset*sampleRate)), sampleRate/4)
		referenceFrequency := progressiveChirpFrequency(t, reference.pcm, int(math.Round((sourceOffset+offset)*sampleRate)), sampleRate/4)
		if math.Abs(actualFrequency-referenceFrequency) > 6 {
			t.Fatalf("AAC seek content at %.3fs has frequency %.1f Hz, source window has %.1f Hz", offset, actualFrequency, referenceFrequency)
		}
	}
}

func progressiveChirpFrequency(t *testing.T, pcm []byte, start, samples int) float64 {
	t.Helper()
	if start < 0 || start+samples > len(pcm)/2 {
		t.Fatalf("chirp comparison window [%d,%d) exceeds %d samples", start, start+samples, len(pcm)/2)
	}
	positiveCrossings := 0
	var firstCrossing, lastCrossing float64
	var energy float64
	previous := int16(binary.LittleEndian.Uint16(pcm[start*2:]))
	for i := start + 1; i < start+samples; i++ {
		value := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		if previous <= 0 && value > 0 {
			// Interpolated crossing times avoid the whole-cycle frequency
			// quantization that could hide a short audio/video displacement.
			crossing := float64(i-1) + float64(-int(previous))/float64(int(value)-int(previous))
			if positiveCrossings == 0 {
				firstCrossing = crossing
			}
			lastCrossing = crossing
			positiveCrossings++
		}
		energy += float64(value) * float64(value)
		previous = value
	}
	if energy/float64(samples) < 1_000_000 || positiveCrossings < 2 {
		t.Fatal("chirp comparison window is silent or lacks meaningful audio content")
	}
	return float64(positiveCrossings-1) * 48000 / (lastCrossing - firstCrossing)
}
