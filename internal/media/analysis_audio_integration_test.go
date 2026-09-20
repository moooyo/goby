package media

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This generated case verifies extraction mechanics and timestamp ownership.
// It is not a labeled real-episode intro accuracy or threshold acceptance case.
func TestAnalysisAudioActualChromaprintAndNonzeroOrigin(t *testing.T) {
	ffprobe, ffmpeg := audioProbeIntegrationTools(t)
	helper := os.Getenv("GOBY_INTRO_FINGERPRINT")
	if helper == "" {
		t.Skip("set GOBY_INTRO_FINGERPRINT to the separately built pinned PCM helper")
	}
	path := filepath.Join(t.TempDir(), "audio-origin.mka")
	audioProbeRunFFmpeg(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=631:sample_rate=48000:duration=12",
		"-af", "asetpts=PTS+2/TB", "-c:a", "pcm_s16le", "-threads:a", "1", "-f", "matroska", path)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.Rename(path, path+".held"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("this replacement must never be decoded"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(19, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if !info.FormatStartKnown || info.FormatStartTicks != 2*TicksPerSecond {
		t.Fatalf("fixture lost its source origin: %+v", info)
	}
	index := -1
	for _, stream := range info.Streams {
		if stream.CodecType == "audio" {
			index = stream.Index
			break
		}
	}
	extractor := AnalysisExtractor{FFmpegPath: ffmpeg, FingerprintPath: helper}
	availability, err := extractor.Availability(context.Background())
	if err != nil || !availability.AudioAvailable || availability.Fingerprint.ChromaprintVersion != "1.6.1" {
		t.Fatalf("fixed tool availability: %+v %v", availability, err)
	}
	result, err := extractor.ExtractAudio(context.Background(), file, info, index)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Samples) < 70 || result.Metadata.InputSamples != 132300 || result.Metadata.RawCount != len(result.Samples) || result.BoundaryUncertaintyTicks != 27_239_003 || result.AlgorithmProfile == "" {
		t.Fatalf("incomplete real fingerprint evidence: %+v", result)
	}
	if result.Samples[0].StartTicks < 0 || result.Samples[0].StartTicks >= TicksPerSecond/10 || result.Samples[len(result.Samples)-1].EndTicks < 8*TicksPerSecond {
		t.Fatalf("audio was not mapped relative to its actual origin: %+v", result.Samples)
	}
	for i, sample := range result.Samples {
		if sample.EndTicks <= sample.StartTicks || i > 0 && sample.StartTicks < result.Samples[i-1].EndTicks {
			t.Fatalf("non-monotone audio anchors: %+v", result.Samples)
		}
	}
	if position, err := file.Seek(0, io.SeekCurrent); err != nil || position != 19 {
		t.Fatalf("borrowed descriptor moved: %d %v", position, err)
	}
	limited := extractor
	limited.Limits.MaxPCMBytes = 1024
	if _, err := limited.ExtractAudio(context.Background(), file, info, index); !errors.Is(err, ErrAnalysisBudget) {
		t.Fatalf("small PCM budget did not stop the real decoder: %v", err)
	}
}
