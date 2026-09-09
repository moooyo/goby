package media

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type audioProbePacketExpectation struct {
	count              int64
	firstSamples       int64
	lastSamples        int64
	maxDurationSamples int64
}

func TestProbeActualAudioPresentationMatchesDecodedSamples(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	fixtures := []struct {
		name         string
		extension    string
		rate         int
		inputSamples int64
		wantSamples  int64
		encoding     []string
		packets      *audioProbePacketExpectation
	}{
		{
			name: "adts_aac_six_seconds", extension: ".aac", rate: 48000,
			inputSamples: 288000, wantSamples: 289792,
			encoding: []string{"-c:a", "aac", "-b:a", "32k", "-f", "adts"},
			packets:  &audioProbePacketExpectation{283, 0, 288768, 1024},
		},
		{
			name: "mp3_fully_discarded_tail", extension: ".mp3", rate: 48000,
			inputSamples: 288047, wantSamples: 288047,
			encoding: []string{"-c:a", "libmp3lame", "-b:a", "32k"},
			packets:  &audioProbePacketExpectation{252, -1105, 286895, 1152},
		},
		{
			name: "mp3_one_sample_tail", extension: ".mp3", rate: 48000,
			inputSamples: 288048, wantSamples: 288048,
			encoding: []string{"-c:a", "libmp3lame", "-b:a", "32k"},
			packets:  &audioProbePacketExpectation{252, -1105, 288047, 1152},
		},
		{
			name: "mp3_gapless_44100", extension: ".mp3", rate: 44100,
			inputSamples: 264644, wantSamples: 264644,
			encoding: []string{"-c:a", "libmp3lame", "-b:a", "32k"},
			packets:  &audioProbePacketExpectation{231, -1105, 263855, 1152},
		},
		{
			name: "mp3_tiny_fully_discarded_tail", extension: ".mp3", rate: 48000,
			inputSamples: 47, wantSamples: 47,
			encoding: []string{"-c:a", "libmp3lame", "-b:a", "32k"},
			packets:  &audioProbePacketExpectation{2, -1105, -1105, 1152},
		},
		{
			name: "flac", extension: ".flac", rate: 48000,
			inputSamples: 288048, wantSamples: 288048,
			encoding: []string{"-c:a", "flac"},
		},
		{
			name: "pcm_wav", extension: ".wav", rate: 48000,
			inputSamples: 288048, wantSamples: 288048,
			encoding: []string{"-c:a", "pcm_s16le"},
		},
		{
			name: "aac_m4a_edit_and_padding", extension: ".m4a", rate: 48000,
			inputSamples: 288048, wantSamples: 288048,
			encoding: []string{"-c:a", "aac", "-b:a", "32k"},
			packets:  &audioProbePacketExpectation{283, 0, 287744, 1024},
		},
		{
			name: "alac_audio_mp4", extension: ".mp4", rate: 48000,
			inputSamples: 288048, wantSamples: 288048,
			encoding: []string{"-c:a", "alac"},
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+fixture.extension)
			audioProbeGenerateFixture(t, ffmpeg, path, fixture.rate, fixture.inputSamples, fixture.encoding...)
			_, stream := audioProbeAssertExact(t, ffprobe, ffmpeg, path, fixture.rate, fixture.wantSamples)
			if fixture.packets != nil {
				audioProbeAssertPackets(t, stream.AudioTiming, fixture.rate, *fixture.packets)
			}
		})
	}
}

func TestProbeActualAudioPartialFirstPacketEdit(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "source.m4a")
	path := filepath.Join(directory, "partial-first.m4a")
	audioProbeGenerateFixture(t, ffmpeg, source, 48000, 288048, "-c:a", "aac", "-b:a", "32k")
	audioProbeRunFFmpeg(t, ffmpeg, "-ss", "0.01", "-i", source, "-t", "0.1",
		"-map", "0:a:0", "-c:a", "copy", path)
	info, stream := audioProbeAssertExact(t, ffprobe, ffmpeg, path, 48000, 5664)
	if info.PresentationOriginTicks != 0 {
		t.Fatalf("the edit did not place the first decoded sample at zero: %+v", info)
	}
	// The negative first packet still yields 544 samples after its 480-sample edit.
	audioProbeAssertPackets(t, stream.AudioTiming, 48000, audioProbePacketExpectation{6, -480, 4640, 1024})
}

func TestProbeActualAudioIgnoresAttachedArtworkForTiming(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	directory := t.TempDir()
	source := filepath.Join(directory, "audio.m4a")
	cover := filepath.Join(directory, "cover.png")
	path := filepath.Join(directory, "audio-with-artwork.m4a")
	audioProbeGenerateFixture(t, ffmpeg, source, 48000, 48048, "-c:a", "aac", "-b:a", "32k")
	audioProbeRunFFmpeg(t, ffmpeg,
		"-f", "lavfi", "-i", "color=c=blue:s=16x16:d=0.04",
		"-c:v", "png", "-frames:v", "1", cover)
	audioProbeRunFFmpeg(t, ffmpeg,
		"-i", source, "-i", cover, "-map", "0:a:0", "-map", "1:v:0", "-c", "copy",
		"-disposition:v:0", "attached_pic", path)
	info, _ := audioProbeAssertExact(t, ffprobe, ffmpeg, path, 48000, 48048)
	var artwork int
	for _, stream := range info.Streams {
		if stream.CodecType == "video" {
			if !stream.IsAttachedPicture || stream.AudioTiming != nil {
				t.Fatalf("the artwork was treated as timed media: %+v", stream)
			}
			artwork++
		}
	}
	if artwork != 1 {
		t.Fatalf("the fixture must contain one attached picture: %+v", info.Streams)
	}
}

func TestProbeActualAudioUnprovenSourcesRetainMetadata(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	fixtures := []struct {
		name      string
		extension string
		codec     string
		encoding  []string
	}{
		{
			name: "aac_timestamp_offset", extension: ".m4a", codec: "aac",
			encoding: []string{"-c:a", "aac", "-b:a", "32k", "-output_ts_offset", "0.125"},
		},
		{
			name: "unsupported_webm_opus", extension: ".webm", codec: "opus",
			encoding: []string{"-c:a", "libopus", "-b:a", "32k"},
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+fixture.extension)
			audioProbeGenerateFixture(t, ffmpeg, path, 48000, 288048, fixture.encoding...)
			info, err := (Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(context.Background(), path)
			if err != nil {
				t.Fatalf("unproven audio must retain its ordinary metadata: %v", err)
			}
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.AudioDurationExact || info.AudioDurationReason == "" || info.Container == "" ||
				info.DurationTicks <= 0 || info.Size != stat.Size() {
				t.Fatalf("unproven audio lost metadata or was marked exact: %+v", info)
			}
			stream := audioProbeSingleAudioStream(t, info)
			if stream.Codec != fixture.codec || stream.SampleRate != 48000 || stream.Channels != 1 || stream.AudioTiming != nil {
				t.Fatalf("unproven audio lost its original stream facts: %+v", stream)
			}
			if fixture.codec == "aac" {
				// This edit leaves 1024 decoded samples in a final packet whose duration is 304.
				if got := audioProbeDecodedSamples(t, ffmpeg, path, stream); got != 289792 {
					t.Fatalf("the timestamp-offset fixture no longer exposes its padded tail: %d samples", got)
				}
			}
		})
	}
}

func TestProbeActualAudioBorrowsStableDescriptor(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "audio; $filename.mp3")
	audioProbeGenerateFixture(t, ffmpeg, path, 48000, 288048, "-c:a", "libmp3lame", "-b:a", "32k")
	info, originalStream := audioProbeAssertExact(t, ffprobe, ffmpeg, path, 48000, 288048)
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Seek(17, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not an audio file"), 0600); err != nil {
		t.Fatal(err)
	}
	prober := Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}
	held, err := prober.ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatalf("scan the original audio through its borrowed descriptor: %v", err)
	}
	heldStream := audioProbeSingleAudioStream(t, held)
	if !held.AudioDurationExact || held.AudioDurationReason != "" || held.Size != info.Size ||
		held.DurationTicks != info.DurationTicks || held.PresentationOriginTicks != info.PresentationOriginTicks ||
		heldStream.AudioTiming == nil || *heldStream.AudioTiming != *originalStream.AudioTiming {
		t.Fatalf("the audio scan followed the replacement pathname: %+v", held)
	}
	position, err := file.Seek(0, io.SeekCurrent)
	if err != nil || position != 17 {
		t.Fatalf("the audio scan closed or sought the borrowed descriptor: %d, %v", position, err)
	}
	if _, err := prober.Probe(context.Background(), path); err == nil {
		t.Fatal("the replacement invalid audio file was accepted")
	}
}

func audioProbeIntegrationTools(t *testing.T) (string, string) {
	t.Helper()
	ffprobe, ffmpeg := os.Getenv("GOBY_FFPROBE"), os.Getenv("GOBY_FFMPEG")
	if ffprobe == "" || ffmpeg == "" {
		t.Skip("set GOBY_FFPROBE and GOBY_FFMPEG to run the Linux audio integration tests")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("media runtime verification must run on Linux")
	}
	return ffprobe, ffmpeg
}

func audioProbeGenerateFixture(t *testing.T, ffmpeg, path string, rate int, samples int64, encoding ...string) {
	t.Helper()
	args := []string{"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=800:sample_rate=%d", rate),
		"-af", fmt.Sprintf("atrim=end_sample=%d,asetpts=PTS-STARTPTS", samples)}
	args = append(args, encoding...)
	args = append(args, path)
	audioProbeRunFFmpeg(t, ffmpeg, args...)
}

func audioProbeRunFFmpeg(t *testing.T, ffmpeg string, args ...string) {
	t.Helper()
	command := append([]string{"-v", "error", "-nostdin"}, args...)
	if _, err := runLimited(context.Background(), 20*time.Second, 1024, ffmpeg, command...); err != nil {
		t.Fatalf("generate audio fixture: %v", err)
	}
}

func audioProbeAssertExact(t *testing.T, ffprobe, ffmpeg, path string, rate int, wantSamples int64) (Info, Stream) {
	t.Helper()
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 20 * time.Second}).Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("probe audio fixture: %v", err)
	}
	if !info.AudioDurationExact || info.AudioDurationReason != "" {
		t.Fatalf("the audio presentation was not proven exact: %+v", info)
	}
	stream := audioProbeSingleAudioStream(t, info)
	if stream.SampleRate != rate || stream.Channels != 1 || stream.AudioTiming == nil || !stream.AudioTiming.Exact {
		t.Fatalf("missing exact mono audio timing at %d Hz: %+v", rate, stream)
	}
	decodedSamples := audioProbeDecodedSamples(t, ffmpeg, path, stream)
	if decodedSamples != wantSamples || stream.AudioTiming.SampleCount != decodedSamples {
		t.Fatalf("sample counts differ: wanted %d, PCM decoded %d, probed %+v", wantSamples, decodedSamples, stream.AudioTiming)
	}
	wantDuration := audioProbeCeilSampleTicks(decodedSamples, rate)
	timing := stream.AudioTiming
	if info.DurationTicks != wantDuration || timing.StartTicks != 0 || timing.EndTicks != wantDuration ||
		timing.PacketCount <= 0 || timing.MaxPacketDurationTicks <= 0 ||
		timing.FirstPacketStartTicks > 0 || timing.LastPacketStartTicks < timing.FirstPacketStartTicks ||
		timing.LastPacketStartTicks >= timing.EndTicks {
		t.Fatalf("presentation bounds do not preserve the decoded samples: info=%+v, timing=%+v", info, timing)
	}
	return info, stream
}

func audioProbeSingleAudioStream(t *testing.T, info Info) Stream {
	t.Helper()
	var audio Stream
	var count int
	for _, stream := range info.Streams {
		if stream.CodecType == "audio" {
			audio = stream
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one audio stream, got %d: %+v", count, info.Streams)
	}
	return audio
}

func audioProbeDecodedSamples(t *testing.T, ffmpeg, path string, stream Stream) int64 {
	t.Helper()
	pcm, err := runLimited(context.Background(), 20*time.Second, 4*1024*1024, ffmpeg,
		"-v", "error", "-nostdin", "-xerror", "-err_detect", "explode", "-i", path,
		"-map", fmt.Sprintf("0:%d", stream.Index), "-c:a", "pcm_s16le", "-f", "s16le", "-")
	if err != nil {
		t.Fatalf("decode audio fixture to PCM: %v", err)
	}
	frameBytes := stream.Channels * 2
	if frameBytes <= 0 || len(pcm) == 0 || len(pcm)%frameBytes != 0 {
		t.Fatalf("invalid decoded PCM size %d for %d channels", len(pcm), stream.Channels)
	}
	return int64(len(pcm) / frameBytes)
}

func audioProbeAssertPackets(t *testing.T, timing *AudioTiming, rate int, want audioProbePacketExpectation) {
	t.Helper()
	if timing.PacketCount != want.count ||
		timing.FirstPacketStartTicks != audioProbeFloorSampleTicks(want.firstSamples, rate) ||
		timing.LastPacketStartTicks != audioProbeFloorSampleTicks(want.lastSamples, rate) ||
		timing.MaxPacketDurationTicks != audioProbeCeilSampleTicks(want.maxDurationSamples, rate) {
		t.Fatalf("sample-bearing packet bounds are wrong: got %+v, expected %+v at %d Hz", timing, want, rate)
	}
}

func audioProbeFloorSampleTicks(samples int64, rate int) int64 {
	numerator, denominator := samples*TicksPerSecond, int64(rate)
	ticks := numerator / denominator
	if numerator < 0 && numerator%denominator != 0 {
		ticks--
	}
	return ticks
}

func audioProbeCeilSampleTicks(samples int64, rate int) int64 {
	return (samples*TicksPerSecond + int64(rate) - 1) / int64(rate)
}
