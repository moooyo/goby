//go:build linux

package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestBuildAudioTimelineActualNativeShortTails(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual audio timeline verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, rate := range []int{44100, 48000} {
		for _, format := range []string{"mp3", "flac", "wav"} {
			for _, duration := range []string{"6.001", "6.005"} {
				t.Run(fmt.Sprintf("%s/%d/%s", format, rate, duration), func(t *testing.T) {
					source := filepath.Join(t.TempDir(), "source."+format)
					codec := map[string]string{"mp3": "libmp3lame", "flac": "flac", "wav": "pcm_s16le"}[format]
					args := []string{"-v", "error", "-nostdin", "-f", "lavfi", "-i", "sine=frequency=800:sample_rate=" + strconv.Itoa(rate) + ":duration=" + duration,
						"-map", "0:a:0", "-c:a", codec, "-threads:a", "1", "-ar", strconv.Itoa(rate), "-ac", "1"}
					if format == "mp3" {
						args = append(args, "-b:a", "32k")
					}
					runTimelineMediaCommand(t, ctx, ffmpeg, append(args, source)...)
					input, err := os.Open(source)
					if err != nil {
						t.Fatal(err)
					}
					defer input.Close()
					info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
					if err != nil {
						t.Fatal(err)
					}
					outputs := []string{"aac", "mp3"}
					if format == "mp3" {
						outputs = append(outputs, "copy")
					}
					for _, outputCodec := range outputs {
						t.Run(outputCodec, func(t *testing.T) {
							options := AudioTimelineOptions{Codec: outputCodec, SampleRate: 48000}
							if outputCodec == "copy" {
								options.LastPacketStartTicks, options.MaxPacketDurationTicks = audioTimelineCopyFacts(t, ctx, ffprobe, source, rate)
								options.SampleRate = 0
							}
							fixed, err := BuildAudioTimeline(info.DurationTicks, 3, options)
							if err != nil || len(fixed.Segments) != 2 || fixed.TargetDuration != 4 {
								t.Fatalf("corrected timeline: %+v, %v", fixed, err)
							}
							legacy, err := BuildTimeline(info.DurationTicks, 3, nil, false)
							if err != nil || len(legacy.Segments) != 3 {
								t.Fatalf("regression source does not expose the nominal tail: %+v, %v", legacy, err)
							}
							plan := Plan{Container: "ts", AudioCodec: outputCodec, VideoStreamIndex: -1, AudioStreamIndex: 0,
								DurationTicks: info.DurationTicks, SegmentSeconds: 3, SegmentMode: "vod"}
							if outputCodec != "copy" {
								plan.AudioBitrate, plan.AudioChannels, plan.AudioSampleRate = 32000, 1, 48000
							}
							legacyDirectory := t.TempDir()
							legacyPlan := audioTimelineTestPlan(t, plan, legacy, 0)
							_, legacyErr := Run(ctx, ffmpeg, legacyDirectory, input, legacyPlan, 1, nil)
							if (outputCodec == "aac" || outputCodec == "copy" && rate == 44100) && legacyErr == nil {
								t.Fatal("the original nominal timeline unexpectedly generated its phantom tail")
							}
							baselineSamples, _ := audioTimelineDecodeOutputs(t, ctx, ffmpeg, legacyDirectory)
							for _, first := range []int{0, len(fixed.Segments) - 1} {
								directory := t.TempDir()
								correctedPlan := audioTimelineTestPlan(t, plan, fixed, first)
								result, err := Run(ctx, ffmpeg, directory, input, correctedPlan, 1, nil)
								if err != nil {
									t.Fatalf("corrected producer from segment %d: %v: %s", first, err, result.StderrTail)
								}
								samples, count := audioTimelineDecodeOutputs(t, ctx, ffmpeg, directory)
								if count != len(fixed.Segments)-first || samples == 0 {
									t.Fatalf("corrected output: segments=%d, samples=%d", count, samples)
								}
								if first == 0 && samples != baselineSamples {
									t.Fatalf("merging discarded samples: %d != %d", samples, baselineSamples)
								}
								data, err := os.ReadFile(filepath.Join(directory, "main.m3u8"))
								if err != nil {
									t.Fatal(err)
								}
								list, err := ParseMediaPlaylist(data)
								if err != nil || !list.Ended || len(list.Segments) != count || list.Sequence != int64(first) {
									t.Fatalf("published list: %+v, %v", list, err)
								}
								for index, segment := range list.Segments {
									if segment.Number != int64(index+first) || !strings.HasSuffix(segment.Name, ".ts") {
										t.Fatalf("producer numbering changed: %+v", segment)
									}
								}
							}
						})
					}
				})
			}
		}
	}
}

func TestBuildAudioTimelineActualEncoderSampleRateBoundaries(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("GOBY_FFMPEG and GOBY_FFPROBE are required for actual encoder frame verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for _, codec := range []string{"aac", "mp3"} {
		for _, rate := range []int{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000} {
			if codec == "mp3" && rate > 48000 {
				continue
			}
			t.Run(fmt.Sprintf("%s/%d", codec, rate), func(t *testing.T) {
				frame, err := audioOutputFrameTicks(codec, rate)
				if err != nil {
					t.Fatal(err)
				}
				for _, end := range []int64{ticksPerSecond + 10_000, ticksPerSecond + frame + 20_000} {
					source := filepath.Join(t.TempDir(), "source.wav")
					runTimelineMediaCommand(t, ctx, ffmpeg, "-v", "error", "-nostdin", "-f", "lavfi", "-i",
						"sine=frequency=800:sample_rate=96000:duration="+tickSeconds(end), "-c:a", "pcm_s16le", "-threads:a", "1", source)
					input, err := os.Open(source)
					if err != nil {
						t.Fatal(err)
					}
					defer input.Close()
					info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).ProbeFile(ctx, input)
					if err != nil {
						t.Fatal(err)
					}
					timeline, err := BuildAudioTimeline(info.DurationTicks, 1, AudioTimelineOptions{Codec: codec, SampleRate: rate})
					if err != nil {
						t.Fatal(err)
					}
					plan := Plan{Container: "ts", AudioCodec: codec, VideoStreamIndex: -1, AudioStreamIndex: 0,
						AudioSampleRate: rate, AudioChannels: 1, AudioBitrate: 32000, DurationTicks: info.DurationTicks, SegmentSeconds: 1, SegmentMode: "vod"}
					for first := range len(timeline.Segments) {
						directory := t.TempDir()
						plan = audioTimelineTestPlan(t, plan, timeline, first)
						result, err := Run(ctx, ffmpeg, directory, input, plan, 1, nil)
						if err != nil {
							t.Fatalf("end=%d, first=%d: %v: %s", info.DurationTicks, first, err, result.StderrTail)
						}
						_, count := audioTimelineDecodeOutputs(t, ctx, ffmpeg, directory)
						if count != len(timeline.Segments)-first {
							t.Fatalf("missing generated segment: count=%d", count)
						}
						segment := filepath.Join(directory, fmt.Sprintf("segment-%06d.ts", first))
						data, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-read_intervals", "%+#1", "-select_streams", "a:0",
							"-show_entries", "frame=nb_samples", "-of", "json", segment).Output()
						if err != nil {
							t.Fatal(err)
						}
						var document struct {
							Frames []struct {
								Samples int64 `json:"nb_samples"`
							} `json:"frames"`
						}
						if err := json.Unmarshal(data, &document); err != nil || len(document.Frames) != 1 {
							t.Fatalf("first encoded packet: %v: %s", err, data)
						}
						// Check the decoded frame's integer sample count rather than
						// packet duration_time, which MPEG-TS rounds to its 90 kHz clock.
						measured := (document.Frames[0].Samples*ticksPerSecond + int64(rate) - 1) / int64(rate)
						if measured != frame {
							t.Fatalf("encoder frame differs from the proven bound: measured=%d, bound=%d", measured, frame)
						}
					}
				}
			})
		}
	}
}

func audioTimelineTestPlan(t *testing.T, plan Plan, timeline Timeline, first int) Plan {
	t.Helper()
	cuts, err := timeline.BoundaryTicks(first, len(timeline.Segments)-1)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]string, len(cuts))
	for index, cut := range cuts {
		values[index] = strconv.FormatInt(cut, 10)
	}
	plan.SegmentStartNumber, plan.StartTicks = first, timeline.Segments[first].StartTicks
	plan.SegmentTimes, plan.EndTicks = strings.Join(values, ","), plan.DurationTicks
	return plan
}

func audioTimelineCopyFacts(t *testing.T, ctx context.Context, ffprobe, source string, rate int) (*int64, int64) {
	t.Helper()
	output, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "format=start_time:packet=pts_time:packet_side_data=", "-of", "json", source).Output()
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Format struct {
			StartTime string `json:"start_time"`
		} `json:"format"`
		Packets []struct {
			PTS string `json:"pts_time"`
		} `json:"packets"`
	}
	if err := json.Unmarshal(output, &document); err != nil || len(document.Packets) == 0 {
		t.Fatalf("packet facts: %v", err)
	}
	origin, ok := timelineTimestamp(document.Format.StartTime)
	if !ok {
		t.Fatal("MP3 presentation origin was not measured")
	}
	last, ok := timelineTimestamp(document.Packets[len(document.Packets)-1].PTS)
	if !ok {
		t.Fatal("last MP3 packet was not measured")
	}
	last -= origin
	// These fixtures are MPEG-1 MP3, whose untrimmed packet contains 1152
	// samples. Round the rational duration up instead of truncating ffprobe's
	// six-decimal duration_time output and understating the phase allowance.
	maximum := (1152*ticksPerSecond + int64(rate) - 1) / int64(rate)
	return &last, maximum
}

func audioTimelineDecodeOutputs(t *testing.T, ctx context.Context, ffmpeg, directory string) (samples, count int) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "segment-") || (!strings.HasSuffix(entry.Name(), ".ts") && !strings.HasSuffix(entry.Name(), ".ts.tmp")) {
			continue
		}
		stat, err := entry.Info()
		if err != nil || stat.Size() == 0 {
			t.Fatalf("empty or unreadable generated segment %s: %v", entry.Name(), err)
		}
		cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-xerror", "-err_detect", "explode", "-threads", "1",
			"-i", filepath.Join(directory, entry.Name()), "-map", "0:a:0", "-ac", "1", "-ar", "48000", "-f", "s16le", "pipe:1")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil || len(data) == 0 || len(data)%2 != 0 {
			t.Fatalf("decode %s: %v: %s", entry.Name(), err, stderr.String())
		}
		samples += len(data) / 2
		count++
	}
	return samples, count
}
