//go:build linux

package transcode

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestVideoCopySeekActualTSQuantizedGlobalClock(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	for _, codec := range []string{"h264", "hevc"} {
		t.Run(codec, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "source.ts")
			args := []string{"-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=6.25",
				"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=6.25",
				"-map", "0:v:0", "-map", "1:a:0", "-threads:v", "1", "-pix_fmt", "yuv420p"}
			if codec == "h264" {
				args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-bf", "0", "-g", "64", "-keyint_min", "64", "-sc_threshold", "0")
			} else {
				args = append(args, "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "pools=none:frame-threads=1:wpp=0:log-level=error:bframes=0:open-gop=0:repeat-headers=1:keyint=64:min-keyint=64:scenecut=0")
			}
			args = append(args, "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "6.25", "-f", "mpegts", path)
			progressiveVideoCommand(t, ctx, ffmpeg, args...)
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			info, err := (media.Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: true, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
			if err != nil || len(info.VideoSeekIndexes) != 1 {
				t.Fatalf("transport stream has no proven restart index: %+v, %v", info.VideoSeekIndexes, err)
			}
			index := info.VideoSeekIndexes[0]
			if index.TimeBaseNumerator != 1 || index.TimeBaseDenominator != 90000 || len(index.Entries) < 2 {
				t.Fatalf("fixture did not establish the real MPEG-TS native clock: %+v", index)
			}
			point := index.Entries[1]
			nativeTicks := media.VideoSeekPointTime(index, point)
			nativeTicks.Mul(nativeTicks, new(big.Rat).SetInt64(media.TicksPerSecond))
			nativeTicks.Sub(nativeTicks, new(big.Rat).SetInt64(info.FormatStartTicks))
			if nativeTicks.IsInt() {
				t.Fatal("the transport fixture did not exercise sub-tick quantization")
			}
			floor := new(big.Int).Quo(nativeTicks.Num(), nativeTicks.Denom())
			if !floor.IsInt64() || floor.Sign() <= 0 {
				t.Fatal("transport fixture has no bounded positive restart")
			}
			actual := floor.Int64()
			exact := progressiveVideoFixturePlan(info, "copy", "aac", actual)
			if AttachVideoCopySeekCandidate(&exact, info) {
				t.Fatal("the legacy exact route accepted a rounded-only native boundary")
			}
			plan := progressiveVideoFixturePlan(info, "copy", "aac", actual+3_700_000)
			before := plan
			if AttachVideoCopySeekCandidateAligned(&plan, info, 10*media.TicksPerSecond) || plan != before {
				t.Fatal("native-clock quantization escaped into zero-normalized output")
			}
			plan.CopyTimestamps = true
			if !AttachVideoCopySeekCandidateAligned(&plan, info, 10*media.TicksPerSecond) || plan.StartTicks != actual {
				t.Fatal("the global-clock route did not retain its real TS restart")
			}
			candidate, err := media.ValidateVideoCopySeekCandidate(plan.VideoCopySeekCandidate)
			if err != nil || !candidate.QuantizedStart || !candidate.CopyTimestamps || candidate.Index.Entries[0].PTS != point.PTS || candidate.Index.Entries[0].CodedSHA256 != point.CodedSHA256 {
				t.Fatalf("quantization replaced the native packet evidence: %+v, %v", candidate, err)
			}
			verification, err := media.VerifyVideoCopySeekCandidate(ctx, ffmpeg, input, plan.VideoCopySeekCandidate, 1)
			if err != nil || !verification.Verified || verification.InputSeekTicks != plan.StartTicks {
				t.Fatalf("actual transport packet clock did not pass its fresh proof: %+v, %v", verification, err)
			}
			reference := readProgressiveVideoReference(t, ctx, ffmpeg, ffprobe, path)
			output := runProgressiveVideo(t, ctx, ffmpeg, path, plan)
			assertVideoCopySeekSourceClock(t, ctx, ffmpeg, ffprobe, output, plan, reference)
		})
	}
}
