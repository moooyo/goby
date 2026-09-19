//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestVideoCopySeekActualHEVCAV1AndAlignedAACPackets(t *testing.T) {
	ffmpeg, ffprobe := progressiveVideoTools(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, codec := range []string{"hevc", "av1"} {
		t.Run(codec, func(t *testing.T) {
			directory := t.TempDir()
			source := filepath.Join(directory, "source.mp4")
			args := []string{"-hide_banner", "-v", "error", "-nostdin", "-filter_threads", "1",
				"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=6.25",
				"-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=6.25",
				"-map", "0:v:0", "-map", "1:a:0", "-threads:v", "1", "-pix_fmt", "yuv420p"}
			if codec == "hevc" {
				args = append(args, "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "pools=none:frame-threads=1:wpp=0:log-level=error:bframes=0:open-gop=0:repeat-headers=1:keyint=64:min-keyint=64:scenecut=0", "-tag:v", "hvc1")
			} else {
				args = append(args, "-c:v", "libaom-av1", "-cpu-used", "8", "-g", "64", "-lag-in-frames", "0", "-auto-alt-ref", "0", "-row-mt", "0", "-tag:v", "av01")
			}
			args = append(args, "-c:a", "aac", "-threads:a", "1", "-b:a", "96000", "-t", "6.25", source)
			progressiveVideoCommand(t, ctx, ffmpeg, args...)
			for _, container := range []string{"mp4", "mkv"} {
				t.Run(container, func(t *testing.T) {
					path := source
					if container == "mkv" {
						path = filepath.Join(directory, "source.mkv")
						progressiveVideoCommand(t, ctx, ffmpeg, "-hide_banner", "-v", "error", "-nostdin", "-copyts", "-i", source, "-map", "0", "-c", "copy", "-avoid_negative_ts", "disabled", path)
					}
					input, err := os.Open(path)
					if err != nil {
						t.Fatal(err)
					}
					defer input.Close()
					info, err := (media.Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, AnalyzeVideoSeek: true, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
					if err != nil || len(info.VideoSeekIndexes) != 1 || media.VideoSeekCodec(info.VideoSeekIndexes[0]) != codec {
						t.Fatalf("source did not establish codec-specific restart evidence: %+v, %v", info.VideoSeekIndexes, err)
					}
					reference := readProgressiveVideoReference(t, ctx, ffmpeg, ffprobe, path)
					index := info.VideoSeekIndexes[0]
					if len(index.Entries) < 2 {
						t.Fatal("source has no positive random-access point")
					}
					boundary := media.VideoSeekPointTime(index, index.Entries[1])
					boundary.Mul(boundary, new(big.Rat).SetInt64(media.TicksPerSecond))
					if !boundary.IsInt() || !boundary.Num().IsInt64() {
						t.Fatal("source random-access point is not an exact playback tick")
					}
					start := boundary.Num().Int64() - info.FormatStartTicks
					// Matroska can quantize AAC packet timestamps to milliseconds.
					// A common boundary still needs native-clock evidence; encoded
					// audio remains the independent fallback when it is unavailable.
					for _, audio := range []string{"aac", "copy"} {
						t.Run(audio, func(t *testing.T) {
							plan := progressiveVideoFixturePlan(info, "copy", audio, start)
							if !AttachVideoCopySeekCandidate(&plan, info) {
								if container == "mkv" && audio == "copy" {
									return
								}
								t.Fatal("indexed exact codec/packet boundary was not proposed")
							}
							output := runProgressiveVideo(t, ctx, ffmpeg, path, plan)
							facts := probeProgressiveVideoFrames(t, ctx, ffprobe, output)
							strictDecodeProgressiveVideo(t, ctx, ffmpeg, output)
							assertProgressiveVideoWindow(t, reference, plan, facts, decodeProgressiveVideoPixels(t, ctx, ffmpeg, output))
							if len(facts.video) == 0 || len(facts.audio) == 0 || math.Abs(facts.video[0].time(t)) > .000001 || math.Abs(facts.audio[0].time(t)) > .022 {
								t.Fatal("copied video and audio did not retain the shared start clock")
							}
							for _, stream := range facts.streams {
								if stream.Type == "video" && stream.Codec != codec || stream.Type == "audio" && stream.Codec != "aac" {
									t.Fatalf("stream copying changed the negotiated codec: %+v", stream)
								}
							}
							global := plan
							global.CopyTimestamps, global.VideoCopySeekCandidate = true, ""
							if !AttachVideoCopySeekCandidate(&global, info) {
								t.Fatal("the exact source-global packet clock was not retained")
							}
							globalOutput := runProgressiveVideo(t, ctx, ffmpeg, path, global)
							assertVideoCopySeekSourceClock(t, ctx, ffmpeg, ffprobe, globalOutput, global, reference)
						})
					}
				})
			}
		})
	}
}

func assertVideoCopySeekSourceClock(t *testing.T, ctx context.Context, ffmpeg, ffprobe, path string, plan Plan, reference progressiveVideoReference) {
	t.Helper()
	start := float64(plan.StartTicks) / float64(media.TicksPerSecond)
	facts := probeProgressiveVideoFrames(t, ctx, ffprobe, path)
	strictDecodeProgressiveVideo(t, ctx, ffmpeg, path)
	if len(facts.video) == 0 || math.Abs(facts.video[0].time(t)-start) > .002 {
		t.Fatalf("CopyTimestamps did not preserve the actual source-global video origin: start=%.7f, frames=%+v", start, facts.video)
	}
	// Verify the actual demuxed packet clock independently of decoded frame
	// timestamps and the argument builder. The output offset must be carried by
	// the MP4 timeline rather than being invented by the API or player URL.
	data := progressiveVideoCommand(t, ctx, ffprobe, "-v", "error", "-show_packets", "-show_entries", "packet=codec_type,pts_time,dts_time", "-of", "json", path)
	var document struct {
		Packets []struct {
			Type string `json:"codec_type"`
			PTS  string `json:"pts_time"`
			DTS  string `json:"dts_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	videoCount := 0
	for _, packet := range document.Packets {
		pts, ptsErr := strconv.ParseFloat(packet.PTS, 64)
		dts, dtsErr := strconv.ParseFloat(packet.DTS, 64)
		if ptsErr != nil || dtsErr != nil || math.IsNaN(pts) || math.IsNaN(dts) {
			t.Fatal("source-global output has an unknown packet clock")
		}
		if packet.Type == "video" {
			if videoCount == 0 && (math.Abs(pts-start) > .002 || math.Abs(dts-start) > .002) || pts < start-.002 || dts < start-.002 {
				t.Fatalf("source-global video packet was normalized or shifted twice: %+v", packet)
			}
			videoCount++
		} else if packet.Type == "audio" && pts < start-.022 {
			t.Fatalf("source-global audio packet escaped its source clock: %+v", packet)
		}
	}
	if videoCount != len(facts.video) {
		t.Fatal("source-global output omitted or duplicated coded pictures")
	}
	for position := range facts.video {
		facts.video[position].Timestamp = strconv.FormatFloat(facts.video[position].time(t)-start, 'f', 9, 64)
	}
	assertProgressiveVideoWindow(t, reference, plan, facts, decodeProgressiveVideoPixels(t, ctx, ffmpeg, path))
}
