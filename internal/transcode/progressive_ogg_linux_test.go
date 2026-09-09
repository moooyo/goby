//go:build linux

package transcode

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestProgressiveExactOggProbePreservesDecodedSamplesAndSeek(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("FFmpeg and ffprobe are required for exact Ogg audio verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, fixture := range []struct {
		codec, encoder string
		rate           int
	}{
		{"vorbis", "libvorbis", 44100},
		{"opus", "libopus", 48000},
		{"flac", "flac", 48000},
	} {
		for _, duration := range []string{"0.1", "2.013"} {
			t.Run(fixture.codec+"/"+duration, func(t *testing.T) {
				source := filepath.Join(t.TempDir(), "source.ogg")
				// A chirp makes a shifted window distinguishable from the expected
				// sample slice; a stationary tone can hide a whole-period offset.
				args := []string{"-v", "error", "-nostdin", "-f", "lavfi", "-i",
					fmt.Sprintf("aevalsrc=0.15*sin(2*PI*(173*t+511*t*t)):s=%d:d=%s", fixture.rate, duration),
					"-c:a", fixture.encoder, "-threads", "1"}
				if fixture.codec != "flac" {
					args = append(args, "-b:a", "64000")
				}
				args = append(args, "-f", "ogg", source)
				if data, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
					t.Fatalf("create Ogg fixture: %v: %s", err, data)
				}
				info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 15 * time.Second}).Probe(ctx, source)
				if err != nil {
					t.Fatal(err)
				}
				if info.ProbeVersion != media.CurrentProbeVersion || !info.AudioDurationExact || len(info.Streams) != 1 ||
					info.Streams[0].AudioTiming == nil || !info.Streams[0].AudioTiming.Exact || info.Streams[0].AudioTiming.StartTicks != 0 {
					t.Fatalf("missing exact Ogg presentation: %+v", info)
				}
				stream := info.Streams[0]
				baseline := decodeProgressivePCM(t, ctx, ffmpeg, source, 16)
				frameBytes := int64(stream.Channels * 2)
				if stream.Codec != fixture.codec || stream.SampleRate != fixture.rate || stream.Channels != 1 || int64(len(baseline)) != stream.AudioTiming.SampleCount*frameBytes {
					t.Fatalf("probed Ogg facts differ from full PCM decode: %+v, %d bytes", stream, len(baseline))
				}
				if fixture.codec == "vorbis" && duration == "0.1" && (info.PresentationOriginTicks <= 0 || stream.AudioTiming.SampleCount != 4282) {
					t.Fatalf("fixture lost the 128-sample Vorbis priming boundary: %+v", info)
				}
				seek := int64(234567)
				if duration != "0.1" {
					// This position also exercises Ogg FLAC's unreliable page seek:
					// direct input -ss can land 11264 samples before this boundary.
					seek = 12378912
				}
				for _, start := range []int64{0, 1, seek} {
					input, err := os.Open(source)
					if err != nil {
						t.Fatal(err)
					}
					p := progressivePlan("wav", "pcm_s16le")
					p.DurationTicks, p.StartTicks = info.DurationTicks, start
					p.AudioSourceSampleRate, p.AudioSampleRate = stream.SampleRate, stream.SampleRate
					p.AudioSourceSampleCount, p.AudioChannels = stream.AudioTiming.SampleCount, stream.Channels
					p.AudioSampleSeek = true
					out := t.TempDir()
					var ready bool
					result, err := Run(ctx, ffmpeg, out, input, p, 1, func(progress Progress) { ready = ready || progress.Ready })
					input.Close()
					if err != nil || !ready {
						t.Fatalf("Ogg to WAV start=%d: %v, ready=%t: %s", start, err, ready, result.StderrTail)
					}
					path := filepath.Join(out, "stream.bin")
					pcm := decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
					skip := progressiveSampleLimit(start, stream.SampleRate)
					want := baseline[skip*frameBytes:]
					if len(pcm) != len(want) {
						t.Fatalf("exact WAV samples start=%d: got %d, want %d", start, int64(len(pcm))/frameBytes, int64(len(want))/frameBytes)
					}
					// Both paths decode from the same source beginning, preserving
					// lossy decoder history as well as the exact selected sample.
					if !bytes.Equal(pcm, want) {
						t.Fatalf("WAV source sample boundary differs at start=%d (source sample %d)", start, skip)
					}
					assertProgressiveFileClosed(t, path)
				}
			})
		}
	}
}

func TestProgressiveOggSampleSeekResamplingPreservesFinalSample(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("FFmpeg and ffprobe are required for exact resampling verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.ogg")
	args := []string{"-v", "error", "-nostdin", "-f", "lavfi", "-i", "aevalsrc=0.15*sin(2*PI*(173*t+511*t*t)):s=44100:d=1.1",
		"-af", "atrim=end_sample=44101", "-c:a", "flac", "-sample_fmt", "s16", "-threads", "1", "-f", "ogg", source}
	if data, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("create exact resampling source: %v: %s", err, data)
	}
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 10 * time.Second}).Probe(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.AudioDurationExact || len(info.Streams) != 1 || info.Streams[0].AudioTiming == nil || !info.Streams[0].AudioTiming.Exact || info.Streams[0].AudioTiming.SampleCount != 44101 {
		t.Fatalf("missing exact resampling facts: %+v", info)
	}
	// 44101 source samples become 48002 output samples. A duration-derived -t
	// rounds this down to 48001 inside FFmpeg, silently removing the final sample.
	baseline, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-xerror", "-threads", "1", "-i", source,
		"-map", "0:a:0", "-af", "aresample=48000", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1").Output()
	if err != nil || len(baseline) != 48002*2 || bytes.Equal(baseline[len(baseline)-2:], []byte{0, 0}) {
		t.Fatalf("resampling reference must retain a nonzero final sample: %d bytes, %v", len(baseline), err)
	}
	for _, pair := range [][2]string{{"wav", "pcm_s16le"}, {"flac", "flac"}} {
		t.Run(pair[0], func(t *testing.T) {
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			p := progressivePlan(pair[0], pair[1])
			p.DurationTicks, p.AudioSourceSampleRate = info.DurationTicks, 44100
			p.AudioSourceSampleCount, p.AudioSampleRate, p.AudioChannels = 44101, 48000, 1
			p.AudioSampleSeek = true
			out := t.TempDir()
			result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
			if err != nil {
				t.Fatalf("resampled Ogg conversion: %v: %s", err, result.StderrTail)
			}
			pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(out, "stream.bin"), 16)
			if !bytes.Equal(pcm, baseline) {
				t.Fatalf("resampling changed the exact source samples: got %d bytes, want %d", len(pcm), len(baseline))
			}
		})
	}
}
