package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAudioWaveformActualMultipleTracksDelayAndPhaseInversion(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "waveform-tracks.mkv")
	audioProbeRunFFmpeg(t, ffmpeg,
		"-f", "lavfi", "-i", "color=c=black:size=16x16:rate=1:duration=3",
		"-f", "lavfi", "-i", "aevalsrc=0.5|-0.5:s=48000:d=3",
		"-f", "lavfi", "-i", "aevalsrc=0.25:s=48000:d=1",
		"-filter_complex", "[2:a]asetpts=PTS+1/TB[late]", "-map", "0:v", "-map", "1:a", "-map", "[late]",
		"-c:v", "ffv1", "-threads:v", "1", "-c:a", "pcm_f32le", "-threads:a", "1", "-f", "matroska", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(19, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FFprobePath: ffprobe, Limits: AnalysisLimits{Timeout: 30 * time.Second}}
	capabilities, err := extractor.AudioWaveformAvailability(context.Background())
	if err != nil || !capabilities.Available {
		t.Fatalf("availability %+v %v", capabilities, err)
	}
	extractor.ExpectedFFmpegSHA256 = capabilities.FFmpegSHA256
	var output bytes.Buffer
	var progress []AudioWaveformProgress
	summary, err := extractor.GenerateAudioWaveforms(context.Background(), file, info, &output, func(value AudioWaveformProgress) { progress = append(progress, value) })
	if err != nil {
		t.Fatal(err)
	}
	data, err := ParseAudioWaveforms(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Tracks) != 2 || summary.Bytes != int64(output.Len()) || len(progress) == 0 || progress[len(progress)-1].CompletedTracks != 2 {
		t.Fatalf("missing complete tracks: %+v", summary)
	}
	first, second := data.Tracks[0], data.Tracks[1]
	if first.StreamIndex != 1 || second.StreamIndex != 2 || first.SampleCount != 144000 || second.SampleCount != 48000 {
		t.Fatalf("source streams or presentation count changed: %+v %+v", first.AudioWaveformTrackSummary, second.AudioWaveformTrackSummary)
	}
	for _, level := range first.Levels {
		for index := range level.BucketCount {
			if level.Peaks[index] != 32768 || level.RMS[index] != 32768 {
				t.Fatalf("phase inversion cancelled or changed amplitude: %d/%d = %d/%d", level.BucketCount, index, level.Peaks[index], level.RMS[index])
			}
		}
	}
	if second.CoverageStartTicks != TicksPerSecond || second.CoverageEndTicks != 2*TicksPerSecond {
		t.Fatalf("late track shifted: %+v", second.AudioWaveformTrackSummary)
	}
	fine := second.Levels[3]
	if fine.Validity[0] != 0 || fine.Validity[len(fine.Validity)-1] != 0 || fine.Peaks[2048] != 16384 {
		t.Fatal("delayed track has fabricated coverage")
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 19 {
		t.Fatalf("borrowed source offset changed: %d %v", position, err)
	}
}

func TestAudioWaveformActualAudioCodecCoverage(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	for _, fixture := range []struct {
		name, codec string
		channels    int
	}{{"aac", "aac", 2}, {"ac3", "ac3", 6}, {"eac3", "eac3", 6}, {"flac", "flac", 2}, {"opus", "libopus", 2}, {"pcm", "pcm_s16le", 2}} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), fixture.name+".mka")
			audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=997:sample_rate=48000:duration=1.25", "-ac", strconv.Itoa(fixture.channels), "-c:a", fixture.codec, "-threads:a", "1", "-f", "matroska", path)
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			_, err = (AnalysisExtractor{FFmpegPath: ffmpeg, Limits: AnalysisLimits{Timeout: 30 * time.Second}}).GenerateAudioWaveforms(context.Background(), file, info, &output, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := ParseAudioWaveforms(output.Bytes())
			if err != nil || len(data.Tracks) != 1 {
				t.Fatalf("decode %v", err)
			}
			track := data.Tracks[0]
			if track.SampleCount < 48000 || track.SampleCount > 65000 || track.CoverageStartTicks > TicksPerSecond/50 || track.CoverageEndTicks < 12*TicksPerSecond/10 {
				t.Fatalf("lost priming or duration: %+v", track.AudioWaveformTrackSummary)
			}
		})
	}
}

func TestAudioWaveformActualCancellationDoesNotEmitArtifact(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	path := filepath.Join(t.TempDir(), "cancel.mka")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=997:sample_rate=48000:duration=20", "-c:a", "pcm_s16le", "-threads:a", "1", "-f", "matroska", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := (Prober{FFprobePath: ffprobe, FFmpegPath: ffmpeg, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var output bytes.Buffer
	_, err = (AnalysisExtractor{FFmpegPath: ffmpeg, Limits: AnalysisLimits{Timeout: 30 * time.Second}}).GenerateAudioWaveforms(ctx, file, info, &output, func(value AudioWaveformProgress) {
		if value.PositionTicks > 0 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || output.Len() != 0 {
		t.Fatalf("cancelled artifact published: %d %v", output.Len(), err)
	}
}
