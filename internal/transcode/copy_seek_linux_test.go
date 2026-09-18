//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestVideoCopySeekActualExactIDRRetainsVideoAndAACAlignment(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "copy-boundary.mp4")
	progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=6.25",
		"-f", "lavfi", "-i", "aevalsrc=0.6*sin(2*PI*(300*t+90*t*t)):s=48000:d=6.25",
		"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1",
		"-preset", "veryfast", "-crf", "18", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0", "-bf", "0", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "6.25", source)
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	info, err := (media.Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: true, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
	if err != nil || len(info.VideoSeekIndexes) != 1 {
		t.Fatalf("fixture did not provide an indexed copy candidate: %+v, %v", info.VideoSeekIndexes, err)
	}
	reference := readProgressiveVideoReference(t, ctx, ffmpeg, ffprobe, source)
	for _, audio := range []string{"aac", ""} {
		t.Run("audio-"+audio, func(t *testing.T) {
			plan := progressiveVideoFixturePlan(info, "copy", audio, 20_000_000)
			if !AttachVideoCopySeekCandidate(&plan, info) {
				t.Fatal("exact non-reordered IDR was not proposed for fresh copy proof")
			}
			path := runProgressiveVideo(t, ctx, ffmpeg, source, plan)
			facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
			assertProgressiveVideoStreams(t, facts, audio != "")
			strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
			assertProgressiveVideoWindow(t, reference, plan, facts, decodeProgressiveVideoPixels(t, ctx, ffmpeg, path))
			if len(facts.video) == 0 || math.Abs(facts.video[0].time(t)) > .000001 {
				t.Fatal("copied output did not begin at the exact requested presentation position")
			}
			if audio != "" {
				assertProgressiveChirpWindow(t, reference, plan, facts, decodeProgressivePCM(t, ctx, ffmpeg, path, 16))
			}
		})
	}
	plan := progressiveVideoFixturePlan(info, "copy", "aac", 23_700_000)
	if AttachVideoCopySeekCandidate(&plan, info) {
		t.Fatal("a between-IDR request must select encoding or fail")
	}
	plan = progressiveVideoFixturePlan(info, "copy", "copy", 20_000_000)
	if AttachVideoCopySeekCandidate(&plan, info) {
		t.Fatal("copied audio has no exact packet alignment proof")
	}

	plan = progressiveVideoFixturePlan(info, "copy", "aac", 20_000_000)
	if !AttachVideoCopySeekCandidate(&plan, info) {
		t.Fatal("copy candidate was not attached")
	}
	candidate, err := media.ValidateVideoCopySeekCandidate(plan.VideoCopySeekCandidate)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Index.Entries[0].CodedSHA256 = strings.Repeat("0", 64)
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	plan.VideoCopySeekCandidate = string(data)
	directory := t.TempDir()
	progressCalls := 0
	_, err = Run(ctx, ffmpeg, directory, input, plan, 1, func(Progress) { progressCalls++ })
	entries, readErr := os.ReadDir(directory)
	if !errors.Is(err, ErrInvalidInput) || readErr != nil || len(entries) != 0 || progressCalls != 0 {
		t.Fatalf("an unverified copy job published output: err=%v read=%v entries=%v progress=%d", err, readErr, entries, progressCalls)
	}
}
