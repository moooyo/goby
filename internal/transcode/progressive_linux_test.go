//go:build linux

package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestProgressiveActualAudioContainersAndCopy(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for progressive media verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	source := writeProgressivePCM(t, 16, 4*48000)
	for _, pair := range [][2]string{{"mp3", "mp3"}, {"aac", "aac"}, {"flac", "flac"}, {"ogg", "vorbis"}, {"ogg", "opus"}, {"ogg", "flac"}, {"wav", "pcm_s16le"}, {"m4a", "aac"}} {
		t.Run(pair[0]+"/"+pair[1], func(t *testing.T) {
			var fullOutput string
			for _, start := range []int64{0, 12500000} {
				input, err := os.Open(source)
				if err != nil {
					t.Fatal(err)
				}
				p := progressivePlan(pair[0], pair[1])
				p.StartTicks = start
				out := t.TempDir()
				var ready bool
				result, err := Run(ctx, ffmpeg, out, input, p, 1, func(p Progress) { ready = ready || p.Ready })
				input.Close()
				if err != nil || !ready {
					t.Fatalf("progressive encode start=%d: %v ready=%t: %s", start, err, ready, result.StderrTail)
				}
				path := filepath.Join(out, "stream.bin")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if yes, err := ProgressiveAudioReady(pair[0], data[:min(len(data), MaxProgressivePrefixBytes)]); err != nil || !yes {
					t.Fatalf("final payload detection: %t, %v", yes, err)
				}
				pcm := decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
				want := int((p.DurationTicks-start)*48000/ticksPerSecond) * 4
				if math.Abs(float64(len(pcm)-want)) > 48000*4*.15 {
					t.Fatalf("decoded bytes %d, expected about %d", len(pcm), want)
				}
				if pair[1] == "flac" || pair[1] == "pcm_s16le" {
					if len(pcm) != want {
						t.Fatalf("lossless sample bound: bytes=%d, want=%d", len(pcm), want)
					}
				}
				assertProgressiveFileClosed(t, path)
				if start == 0 {
					fullOutput = path
				}
			}
			input, err := os.Open(fullOutput)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			p := progressivePlan(pair[0], "copy")
			out := t.TempDir()
			result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
			if err != nil {
				t.Fatalf("progressive copy: %v: %s", err, result.StderrTail)
			}
			_ = decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(out, "stream.bin"), 16)
			if pair[1] != "flac" {
				p.StartTicks = 12500000
				seek := t.TempDir()
				result, err = Run(ctx, ffmpeg, seek, input, p, 1, nil)
				if err != nil {
					t.Fatalf("progressive copy seek: %v: %s", err, result.StderrTail)
				}
				pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(seek, "stream.bin"), 16)
				if math.Abs(float64(len(pcm)-528000)) > 48000*4*.15 {
					t.Fatalf("copy seek duration: %d bytes", len(pcm))
				}
			}
		})
	}
	// ADTS carries configuration in packets; fragmented MP4 must delay its
	// immutable moov until aac_adtstoasc has produced the AudioSpecificConfig.
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	adts := t.TempDir()
	if result, err := Run(ctx, ffmpeg, adts, input, progressivePlan("aac", "aac"), 1, nil); err != nil {
		t.Fatalf("ADTS source: %v: %s", err, result.StderrTail)
	}
	aac, err := os.Open(filepath.Join(adts, "stream.bin"))
	if err != nil {
		t.Fatal(err)
	}
	defer aac.Close()
	fmp4 := t.TempDir()
	result, err := Run(ctx, ffmpeg, fmp4, aac, progressivePlan("m4a", "copy"), 1, nil)
	if err != nil {
		t.Fatalf("ADTS to fragmented M4A: %v: %s", err, result.StderrTail)
	}
	_ = decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(fmp4, "stream.bin"), 16)
}

func TestProgressiveExactADTSProbeProducesPreciselyDecodedWAV(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("FFmpeg and ffprobe are required for exact audio verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	source := filepath.Join(t.TempDir(), "source.aac")
	command := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-nostdin", "-i", writeProgressivePCM(t, 16, 4*48000), "-map", "0:a:0", "-c:a", "aac", "-threads", "1", "-ar", "44100", "-b:a", "128000", "-f", "adts", source)
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("create ADTS: %v: %s", err, data)
	}
	info, err := (media.Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if !info.AudioDurationExact || len(info.Streams) != 1 || info.Streams[0].AudioTiming == nil || !info.Streams[0].AudioTiming.Exact || info.Streams[0].AudioTiming.StartTicks != 0 {
		t.Fatalf("missing exact audio timeline: %+v", info)
	}
	stream := info.Streams[0]
	samples := stream.AudioTiming.SampleCount
	baseline := decodeProgressivePCM(t, ctx, ffmpeg, source, 16)
	if int64(len(baseline)) != samples*int64(stream.Channels)*2 {
		t.Fatalf("probe count %d differs from direct decode %d", samples, len(baseline))
	}
	if progressiveSampleLimit(info.DurationTicks, stream.SampleRate) != samples+1 {
		t.Fatal("fixture does not exercise outward-tick double rounding")
	}
	for _, start := range []int64{0, 1, 12345, 12345780} {
		for _, rate := range []int{44100, 48000} {
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			p := progressivePlan("wav", "pcm_s16le")
			p.DurationTicks = info.DurationTicks
			p.StartTicks = start
			p.AudioSourceSampleRate = stream.SampleRate
			p.AudioSourceSampleCount = samples
			p.AudioSampleRate = rate
			p.AudioChannels = stream.Channels
			out := t.TempDir()
			result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
			input.Close()
			if err != nil {
				t.Fatalf("exact ADTS start=%d rate=%d: %v: %s", start, rate, err, result.StderrTail)
			}
			pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(out, "stream.bin"), 16)
			want, err := ProgressiveOutputSamples(p, rate)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(pcm)) != want*int64(stream.Channels)*2 {
				t.Fatalf("output samples %d, want %d", len(pcm)/(stream.Channels*2), want)
			}
			if start == 0 && rate == stream.SampleRate && !bytes.Equal(pcm, baseline) {
				t.Fatalf("full ADTS to WAV changed samples: got %d bytes, want %d", len(pcm), len(baseline))
			}
			// The first AAC packet shares decoder history with the full decode.
			// Later lossy-codec seeks may restart overlap/PNS state, so their
			// sample count is checked without claiming byte-identical waveforms.
			if rate == stream.SampleRate && start < 100000 {
				skip := progressiveSampleLimit(start, stream.SampleRate) * int64(stream.Channels) * 2
				if !bytes.Equal(pcm, baseline[skip:]) {
					t.Fatalf("ADTS source sample boundary differs at start=%d", start)
				}
			}
		}
	}
}

func TestProgressiveExactNativeWAVCopyUsesSelectedSampleBoundary(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("FFmpeg is required for native sample-boundary verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, rate := range []int{44100, 48000, 96000, 768000} {
		path := writeProgressivePCM(t, 16, 1000)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint32(original[24:], uint32(rate))
		binary.LittleEndian.PutUint32(original[28:], uint32(rate*4))
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		for _, start := range []int64{1, 12, 59, 12345} {
			p := progressivePlan("wav", "copy")
			p.AudioSourceSampleRate = rate
			p.AudioSourceSampleCount = 1000
			p.DurationTicks = (1000*ticksPerSecond + int64(rate) - 1) / int64(rate)
			p.StartTicks = start
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			out := t.TempDir()
			result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
			input.Close()
			if err != nil {
				t.Fatalf("native WAV rate=%d start=%d: %v: %s", rate, start, err, result.StderrTail)
			}
			data, err := os.ReadFile(filepath.Join(out, "stream.bin"))
			if err != nil {
				t.Fatal(err)
			}
			skip := progressiveSampleLimit(start, rate)
			if !bytes.Equal(data[44:], original[44+skip*4:]) {
				t.Fatalf("native PCM sample boundary changed at rate=%d start=%d", rate, start)
			}
		}
	}
}

func TestProgressiveWAVCopyChecksExactNativeDataCount(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("FFmpeg is required for native WAV exact-count verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, err := os.Open(writeProgressivePCM(t, 16, 192000))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	p := progressivePlan("wav", "copy")
	p.AudioSourceSampleCount = 192000
	out := t.TempDir()
	result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
	if err != nil {
		t.Fatalf("exact native copy: %v: %s", err, result.StderrTail)
	}
	if pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(out, "stream.bin"), 16); len(pcm) != 192000*4 {
		t.Fatalf("native copy sample count: %d bytes", len(pcm))
	}
	p.AudioSourceSampleCount--
	if _, err := Run(ctx, ffmpeg, t.TempDir(), input, p, 1, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("native data/count mismatch accepted: %v", err)
	}
}

func TestProgressiveWAVFractionalSeekAndResamplingBounds(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for WAV boundary verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, sourceRate := range []int{44100, 48000} {
		path := writeProgressivePCM(t, 16, 4*sourceRate)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		binary.LittleEndian.PutUint32(data[24:], uint32(sourceRate))
		binary.LittleEndian.PutUint32(data[28:], uint32(sourceRate*4))
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		for _, rate := range []int{44100, 48000, 96000} {
			for _, start := range []int64{0, 10000130, 12345780, 19999999} {
				input, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				p := progressivePlan("wav", "pcm_s16le")
				p.AudioSourceSampleRate = sourceRate
				p.AudioSampleRate = rate
				p.StartTicks = start
				out := t.TempDir()
				result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
				input.Close()
				if err != nil {
					t.Fatalf("WAV source=%d target=%d start=%d: %v: %s", sourceRate, rate, start, err, result.StderrTail)
				}
				pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(out, "stream.bin"), 16)
				want := progressiveSampleLimit(p.DurationTicks-start, rate) * 4
				if int64(len(pcm)) != want {
					t.Fatalf("WAV exact length %d, want %d", len(pcm), want)
				}
			}
		}
	}
}

func TestProgressiveWAVRejectsLargeShortfallAndRIFFOverflow(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for WAV shortfall verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, err := os.Open(writeProgressivePCM(t, 16, 48000))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	p := progressivePlan("wav", "pcm_s16le")
	_, err = Run(ctx, ffmpeg, t.TempDir(), input, p, 1, nil)
	if !errors.Is(err, ErrProcess) {
		t.Fatalf("short source was padded into success: %v", err)
	}
	p.DurationTicks = maxDurationTicks
	p.AudioSampleRate = 96000
	p.AudioChannels = 8
	if err := ValidatePlan(p); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("classic RIFF overflow accepted: %v", err)
	}
	format := progressivePCMFormat(2, 48000)
	limit := (int64(1<<32) - 1 - 36) / 4
	if _, _, err := progressiveWAVHeader(format, limit); err != nil {
		t.Fatal(err)
	}
	if _, _, err := progressiveWAVHeader(format, limit+1); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("RIFF boundary overflow accepted: %v", err)
	}
}

func TestProgressiveRejectsSourceMutationEvenWhenSizeAndMtimeAreRestored(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for source mutation verification")
	}
	t.Setenv("PATH", filepath.Dir(ffmpeg)+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	path := writeProgressivePCM(t, 16, 4*48000)
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	before, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	executable := helperExecutable(t, "progressive-slow")
	go func() {
		_, err := Run(ctx, executable, out, input, progressivePlan("wav", "pcm_s16le"), 1, func(p Progress) {
			if p.Ready {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("early completion: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	changed, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = changed.WriteAt([]byte{1, 2, 3, 4}, before.Size()-4)
	changed.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("changed input was accepted: %v", err)
	}
	assertProgressiveFileClosed(t, filepath.Join(out, "stream.bin"))
}

func TestProgressiveFLACPreserves24BitSamples(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for 24-bit verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := writeProgressivePCM(t, 24, 4*48000)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, container := range []string{"flac", "ogg"} {
		for _, start := range []int64{0, 12500000} {
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			p := progressivePlan(container, "flac")
			p.AudioBitDepth = 24
			p.StartTicks = start
			out := t.TempDir()
			result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
			input.Close()
			if err != nil {
				t.Fatalf("24-bit %s: %v: %s", container, err, result.StderrTail)
			}
			pcm := decodeProgressivePCM(t, ctx, ffmpeg, filepath.Join(out, "stream.bin"), 24)
			want := original[44+int(start*48000/ticksPerSecond)*6:]
			if !bytes.Equal(pcm, want) {
				t.Fatalf("24-bit samples changed for %s start=%d: got %d bytes, want %d", container, start, len(pcm), len(want))
			}
		}
	}
}

func TestProgressiveInitialPayloadPrecedesExitAndHeaderNeverChanges(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for paced media verification")
	}
	t.Setenv("PATH", filepath.Dir(ffmpeg)+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	input, err := os.Open(writeProgressivePCM(t, 16, 4*48000))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	out := t.TempDir()
	ready := make(chan Progress, 1)
	done := make(chan error, 1)
	go func() {
		result, err := Run(ctx, helperExecutable(t, "progressive-slow"), out, input, progressivePlan("m4a", "aac"), 1, func(p Progress) {
			if p.Ready && !p.Ended {
				select {
				case ready <- p:
				default:
				}
			}
		})
		if err != nil {
			err = fmt.Errorf("%w: %s", err, result.StderrTail)
		}
		done <- err
	}()
	select {
	case p := <-ready:
		if p.Bytes == 0 {
			t.Fatal("ready output is empty")
		}
	case err := <-done:
		t.Fatalf("encoder exited before progressive readiness: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	path := filepath.Join(out, "stream.bin")
	early, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		t.Fatalf("encoder had already exited at first payload: %v", err)
	default:
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(final) <= len(early) || !bytes.HasPrefix(final, early) {
		t.Fatal("progressive output did not append without rewriting")
	}
	_ = decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
	assertProgressiveFileClosed(t, path)
}

func TestProgressiveCancellationClosesOutputAndKeepsPrefix(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for cancellation verification")
	}
	t.Setenv("PATH", filepath.Dir(ffmpeg)+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	input, err := os.Open(writeProgressivePCM(t, 16, 4*48000))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	out := t.TempDir()
	ready := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, helperExecutable(t, "progressive-slow"), out, input, progressivePlan("mp3", "mp3"), 1, func(p Progress) {
			if p.Ready {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		})
		done <- err
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("unexpected completion: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	path := filepath.Join(out, "stream.bin")
	early, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation returned %v", err)
	}
	final, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(final, early) {
		t.Fatal("cancel rewrote progressive prefix")
	}
	assertProgressiveFileClosed(t, path)
}

func TestProgressiveIncompatibleCopyDoesNotBecomeReady(t *testing.T) {
	ffmpeg := os.Getenv("GOBY_FFMPEG")
	if ffmpeg == "" {
		t.Skip("GOBY_FFMPEG is required for invalid-copy verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input, err := os.Open(writeProgressivePCM(t, 16, 48000))
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	out := t.TempDir()
	var ready bool
	_, err = Run(ctx, ffmpeg, out, input, progressivePlan("aac", "copy"), 1, func(p Progress) { ready = ready || p.Ready })
	if !errors.Is(err, ErrProcess) || ready {
		t.Fatalf("incompatible PCM-in-ADTS copy: %v ready=%t", err, ready)
	}
	assertProgressiveFileClosed(t, filepath.Join(out, "stream.bin"))
}

func TestProgressiveDeclaredChannelAndRateBoundaries(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("FFmpeg and ffprobe are required for codec boundary verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	source := writeProgressivePCM(t, 16, 48000)
	for _, c := range []struct {
		container, codec      string
		rate, channels, depth int
		bitrate               int64
	}{
		{"mp3", "mp3", 8000, 1, 0, 8000}, {"mp3", "mp3", 12000, 2, 0, 64000}, {"mp3", "mp3", 22050, 2, 0, 160000}, {"mp3", "mp3", 48000, 2, 0, 320000},
		{"aac", "aac", 8000, 1, 0, 48000}, {"aac", "aac", 48000, 6, 0, 288000}, {"m4a", "aac", 48000, 8, 0, 768000},
		{"ogg", "vorbis", 8000, 1, 0, 8000}, {"ogg", "vorbis", 8000, 2, 0, 64000}, {"ogg", "vorbis", 48000, 8, 0, 768000},
		{"ogg", "opus", 48000, 3, 0, 128000}, {"ogg", "opus", 48000, 8, 0, 768000},
		{"flac", "flac", 96000, 8, 24, 0}, {"wav", "pcm_s16le", 96000, 8, 16, 0},
	} {
		t.Run(fmt.Sprintf("%s-%s-%d-%d", c.container, c.codec, c.rate, c.channels), func(t *testing.T) {
			input, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			p := progressivePlan(c.container, c.codec)
			p.DurationTicks = ticksPerSecond
			p.AudioSampleRate = c.rate
			p.AudioChannels = c.channels
			p.AudioBitDepth = c.depth
			p.AudioBitrate = c.bitrate
			out := t.TempDir()
			result, err := Run(ctx, ffmpeg, out, input, p, 1, nil)
			if err != nil {
				t.Fatalf("declared encoder options failed: %v: %s", err, result.StderrTail)
			}
			path := filepath.Join(out, "stream.bin")
			_ = decodeProgressivePCM(t, ctx, ffmpeg, path, 16)
			var facts struct {
				Streams []struct {
					Channels int    `json:"channels"`
					Rate     string `json:"sample_rate"`
				} `json:"streams"`
			}
			data, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "stream=channels,sample_rate", "-of", "json", path).Output()
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &facts); err != nil {
				t.Fatal(err)
			}
			if len(facts.Streams) != 1 || facts.Streams[0].Channels != c.channels || facts.Streams[0].Rate != fmt.Sprint(c.rate) {
				t.Fatalf("encoder silently changed declared dimensions: %s", data)
			}
		})
	}
}

func writeProgressivePCM(t *testing.T, bits, samples int) string {
	t.Helper()
	bytesPerSample := bits / 8
	data := make([]byte, 44+samples*2*bytesPerSample)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 2)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], uint32(48000*2*bytesPerSample))
	binary.LittleEndian.PutUint16(data[32:], uint16(2*bytesPerSample))
	binary.LittleEndian.PutUint16(data[34:], uint16(bits))
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(data)-44))
	for i := 0; i < samples*2; i++ {
		value := int32((i*257)%0x7fffff - 0x400000)
		if bits == 16 {
			value >>= 8
		}
		for b := 0; b < bytesPerSample; b++ {
			data[44+i*bytesPerSample+b] = byte(value >> (8 * b))
		}
	}
	path := filepath.Join(t.TempDir(), "source.wav")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeProgressivePCM(t *testing.T, ctx context.Context, ffmpeg, path string, bits int) []byte {
	t.Helper()
	format := fmt.Sprintf("s%dle", bits)
	codec := "pcm_" + format
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "error", "-xerror", "-threads", "1", "-i", path, "-map", "0:a:0", "-c:a", codec, "-threads", "1", "-f", format, "pipe:1")
	var output, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	if err := cmd.Run(); err != nil || diagnostic.Len() != 0 {
		t.Fatalf("decode progressive %s: %v: %s", filepath.Base(path), err, diagnostic.String())
	}
	return output.Bytes()
}

func assertProgressiveFileClosed(t *testing.T, path string) {
	t.Helper()
	expected, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && os.SameFile(expected, info) {
			t.Fatalf("progressive output descriptor %s remained open", entry.Name())
		}
	}
}
