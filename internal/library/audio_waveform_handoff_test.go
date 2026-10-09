package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestAudioWaveformDecodedReadsOwnMaximumTrackInventory(t *testing.T) {
	root := backgroundClipTestRoot(t)
	manifest, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", media.MaxAudioWaveformTracks)
	if _, err := writeAudioWaveformJSON(root, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	want := audioWaveformTestData(media.MaxAudioWaveformTracks)
	file, gotManifest, result, err := readAudioWaveformData(context.Background(), root, manifest.SourceName, manifest.SourceStamp, true)
	if file != nil {
		t.Cleanup(func() { file.Close() })
	}
	if err != nil || file == nil {
		t.Fatalf("read decoded waveform: file=%t error=%v", file != nil, err)
	}
	if !sameAudioWaveformManifest(gotManifest, manifest) || !result.artifact.Available || result.artifact.Stale ||
		result.artifact.Size != manifest.Summary.Bytes || !reflect.DeepEqual(result.artifact.Tracks, manifest.Summary.Tracks) {
		t.Fatal("decoded read lost its validated manifest or artifact summary")
	}
	if !reflect.DeepEqual(result.data, want) {
		t.Fatal("decoded read did not preserve every track, level, peak, RMS value, and validity bit")
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 0 {
		t.Fatalf("decoded read returned a moved file offset: %d %v", offset, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	payloadFile, payload, err := readAudioWaveformPayloadData(context.Background(), root, manifest.Generation, manifest.Summary.Bytes, manifest.SHA256)
	if payloadFile != nil {
		t.Cleanup(func() { payloadFile.Close() })
	}
	if err != nil || payloadFile == nil {
		t.Fatalf("read decoded payload: file=%t error=%v", payloadFile != nil, err)
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatal("payload read did not preserve the maximum track inventory")
	}
	if err := payloadFile.Close(); err != nil {
		t.Fatal(err)
	}

	// Returned arrays must remain usable after both descriptors close and the
	// original generation is overwritten in place.
	if err := os.WriteFile(filepath.Join(root.Name(), manifest.Generation), make([]byte, int(manifest.Summary.Bytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.data, want) || !reflect.DeepEqual(payload, want) {
		t.Fatal("closed decoded reads retained an alias to the overwritten file")
	}
	payload.Tracks[0].Levels[0].Peaks[0]++
	payload.Tracks[0].Levels[0].RMS[0]++
	payload.Tracks[0].Levels[0].Validity[0] = 0
	if !reflect.DeepEqual(result.data, want) {
		t.Fatal("separate decoded reads shared mutable level arrays")
	}
	result.data.Tracks[0].StreamIndex++
	if !reflect.DeepEqual(result.artifact.Tracks, manifest.Summary.Tracks) {
		t.Fatal("decoded track mutation changed the artifact summary")
	}
}

func TestAudioWaveformDecodedReadsReturnNoDataOnCancellation(t *testing.T) {
	root := backgroundClipTestRoot(t)
	manifest, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", media.MaxAudioWaveformTracks)
	if _, err := writeAudioWaveformJSON(root, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("manifest", func(t *testing.T) {
		file, gotManifest, result, err := readAudioWaveformData(ctx, root, manifest.SourceName, manifest.SourceStamp, true)
		if file != nil {
			defer file.Close()
		}
		if !errors.Is(err, context.Canceled) || file != nil || !reflect.DeepEqual(result, audioWaveformReadResult{}) {
			t.Fatalf("cancelled manifest read exposed data: file=%t zero_result=%t error=%v", file != nil, reflect.DeepEqual(result, audioWaveformReadResult{}), err)
		}
		if !sameAudioWaveformManifest(gotManifest, manifest) {
			t.Fatal("cancelled payload read lost the already validated manifest")
		}
	})
	t.Run("payload", func(t *testing.T) {
		file, data, err := readAudioWaveformPayloadData(ctx, root, manifest.Generation, manifest.Summary.Bytes, manifest.SHA256)
		if file != nil {
			defer file.Close()
		}
		if !errors.Is(err, context.Canceled) || file != nil || !reflect.DeepEqual(data, media.AudioWaveformData{}) {
			t.Fatalf("cancelled payload read exposed data: file=%t zero_data=%t error=%v", file != nil, reflect.DeepEqual(data, media.AudioWaveformData{}), err)
		}
	})
}

func BenchmarkAudioWaveformDecodedReadHandoff(b *testing.B) {
	root, err := os.OpenRoot(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { root.Close() })
	data := audioWaveformTestData(media.MaxAudioWaveformTracks)
	encoded, err := media.MarshalAudioWaveforms(data)
	if err != nil {
		b.Fatal(err)
	}
	generation := "gen-" + strings.Repeat("a", 32) + ".gawf"
	if err := os.WriteFile(filepath.Join(root.Name(), generation), encoded, 0o600); err != nil {
		b.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	manifest := audioWaveformManifest{Format: audioWaveformFileFormat, SourceName: "movie.mkv", SourceRevision: "catalog-source-1",
		SourceStamp: "waveform-source-v1-" + strings.Repeat("b", 64), OperationID: strings.Repeat("c", 32), Generation: generation,
		SHA256: hex.EncodeToString(digest[:]), Summary: data.Summary(int64(len(encoded))), CreatedAt: time.Unix(1700000000, 0).UTC()}
	if _, err := writeAudioWaveformJSON(root, "manifest.json", manifest); err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	for _, benchmark := range []struct {
		name string
		read func() (media.AudioWaveformData, error)
	}{
		{
			name: "owned_decoded",
			read: func() (media.AudioWaveformData, error) {
				file, _, result, err := readAudioWaveformData(ctx, root, manifest.SourceName, manifest.SourceStamp, true)
				if err != nil {
					return media.AudioWaveformData{}, err
				}
				if err := file.Close(); err != nil {
					return media.AudioWaveformData{}, err
				}
				return result.data, nil
			},
		},
		{
			name: "legacy_reread_and_parse",
			read: func() (media.AudioWaveformData, error) {
				file, _, _, err := readAudioWaveform(ctx, root, manifest.SourceName, manifest.SourceStamp, true)
				if err != nil {
					return media.AudioWaveformData{}, err
				}
				payload, readErr := io.ReadAll(io.LimitReader(file, media.MaxAudioWaveformBytes+1))
				if err := errors.Join(readErr, file.Close()); err != nil {
					return media.AudioWaveformData{}, err
				}
				return media.ParseAudioWaveforms(payload)
			},
		},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(encoded)))
			for b.Loop() {
				decoded, err := benchmark.read()
				if err != nil {
					b.Fatal(err)
				}
				if len(decoded.Tracks) != media.MaxAudioWaveformTracks {
					b.Fatal("decoded benchmark result lost tracks")
				}
				runtime.KeepAlive(decoded)
			}
		})
	}
}
