package media

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/introskipper"
)

// This opt-in comparison uses existing native plugin cache arrays as its oracle.
// It establishes extraction compatibility, not independent detection accuracy.
func TestIntroSkipperNativeFingerprintParity(t *testing.T) {
	fixturePath := os.Getenv("GOBY_INTRO_SKIPPER_NATIVE_FIXTURE")
	inventoryPath := os.Getenv("GOBY_INTRO_SKIPPER_SOURCE_INVENTORY")
	ffmpeg, ffprobe := os.Getenv("GOBY_INTRO_SKIPPER_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	if fixturePath == "" || inventoryPath == "" || ffmpeg == "" || ffprobe == "" {
		t.Skip("set native fingerprint, source inventory, intro FFmpeg, and FFprobe paths")
	}
	var fixture struct {
		Pools []struct {
			ID       string `json:"id"`
			Episodes []struct {
				ID              string   `json:"id"`
				Fingerprint     []uint32 `json:"fingerprint"`
				DurationSeconds float64  `json:"durationSeconds"`
				FingerprintEnd  float64  `json:"fingerprintEnd"`
			} `json:"episodes"`
		} `json:"pools"`
	}
	var inventory struct {
		Cases []struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			SHA256 string `json:"sourceSha256"`
			Bytes  int64  `json:"sourceBytes"`
		} `json:"cases"`
	}
	for _, input := range []struct {
		path   string
		target any
	}{{fixturePath, &fixture}, {inventoryPath, &inventory}} {
		data, err := os.ReadFile(input.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, input.target); err != nil {
			t.Fatal(err)
		}
	}
	if len(inventory.Cases) != 19 {
		t.Fatalf("expected frozen 19-source inventory, got %d", len(inventory.Cases))
	}
	for _, pool := range fixture.Pools {
		for _, episode := range pool.Episodes {
			t.Run(episode.ID, func(t *testing.T) {
				var path, wantedHash string
				var wantedSize int64
				for _, source := range inventory.Cases {
					if source.ID == episode.ID {
						path, wantedHash, wantedSize = source.Path, source.SHA256, source.Bytes
					}
				}
				if path == "" {
					t.Fatal("native source is missing from frozen inventory")
				}
				file, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				stat, err := file.Stat()
				if err != nil {
					t.Fatal(err)
				}
				digest := sha256.New()
				if _, err := io.Copy(digest, io.NewSectionReader(file, 0, stat.Size())); err != nil {
					t.Fatal(err)
				}
				if stat.Size() != wantedSize || hex.EncodeToString(digest.Sum(nil)) != wantedHash {
					t.Fatal("source differs from frozen native inventory")
				}
				info, err := (Prober{FFprobePath: ffprobe, Timeout: 30 * time.Second}).ProbeFile(context.Background(), file)
				if err != nil {
					t.Fatal(err)
				}
				info.DurationTicks = int64(math.Round(episode.DurationSeconds * float64(TicksPerSecond)))
				index, ok := SelectIntroSkipperAudioStream(info, "", true)
				if !ok {
					t.Fatal("native audio stream is unavailable")
				}
				result, err := (AnalysisExtractor{IntroFFmpegPath: ffmpeg, Limits: AnalysisLimits{Timeout: 2 * time.Minute}}).ExtractIntroSkipper(context.Background(), file, info,
					IntroSkipperAnalysisRequest{AudioStreamIndex: index, Options: introskipper.DefaultOptions()})
				if err != nil {
					t.Fatal(err)
				}
				if result.FingerprintEndSeconds != episode.FingerprintEnd {
					t.Fatalf("fingerprint horizon differs: %.17g != %.17g", result.FingerprintEndSeconds, episode.FingerprintEnd)
				}
				if !reflect.DeepEqual(result.RawFingerprint, episode.Fingerprint) {
					first := -1
					for index := 0; index < min(len(result.RawFingerprint), len(episode.Fingerprint)); index++ {
						if result.RawFingerprint[index] != episode.Fingerprint[index] {
							first = index
							break
						}
					}
					t.Fatalf("raw sequence differs: actual=%d native=%d first-difference=%d", len(result.RawFingerprint), len(episode.Fingerprint), first)
				}
				raw := make([]byte, 4*len(result.RawFingerprint))
				for index, word := range result.RawFingerprint {
					binary.LittleEndian.PutUint32(raw[index*4:], word)
				}
				fingerprintHash := sha256.Sum256(raw)
				t.Logf("native-parity source=%s words=%d sha256=%x horizon=%.17g ffmpeg=%s", episode.ID, len(result.RawFingerprint), fingerprintHash, result.FingerprintEndSeconds, result.FFmpegSHA256)
			})
		}
	}
}
