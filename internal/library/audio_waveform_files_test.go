package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func audioWaveformTestData(tracks int) media.AudioWaveformData {
	data := media.AudioWaveformData{Profile: media.AudioWaveformProfile, FFmpegSHA256: strings.Repeat("a", 64), DurationTicks: 20 * media.TicksPerSecond}
	for index := 0; index < tracks; index++ {
		track := media.AudioWaveformTrack{AudioWaveformTrackSummary: media.AudioWaveformTrackSummary{StreamIndex: index*2 + 1,
			Channels: 2, SampleRate: 48000, ChannelLayout: "stereo", SampleCount: 20 * 48000,
			CoverageStartTicks: 0, CoverageEndTicks: data.DurationTicks}}
		for _, count := range []int{512, 1024, 2048, 4096} {
			level := media.AudioWaveformLevel{BucketCount: count, Peaks: make([]uint16, count), RMS: make([]uint16, count), Validity: make([]byte, count/8)}
			for bucket := range count {
				level.Peaks[bucket], level.RMS[bucket] = uint16(1000+index), uint16(500+index)
				level.Validity[bucket/8] |= 1 << uint(bucket%8)
			}
			track.Levels = append(track.Levels, level)
		}
		data.Tracks = append(data.Tracks, track)
	}
	return data
}

func audioWaveformTestManifest(t *testing.T, directory *os.Root, generation string, tracks int) (audioWaveformManifest, os.FileInfo) {
	t.Helper()
	data := audioWaveformTestData(tracks)
	encoded, err := media.MarshalAudioWaveforms(data)
	if err != nil {
		t.Fatal(err)
	}
	file, err := directory.OpenFile(generation, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(encoded); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	return audioWaveformManifest{Format: audioWaveformFileFormat, SourceName: "movie.mkv", SourceRevision: "catalog-source-1",
		SourceStamp: "waveform-source-v1-" + strings.Repeat("b", 64), OperationID: strings.Repeat("c", 32), Generation: generation,
		SHA256: hex.EncodeToString(digest[:]), Summary: data.Summary(int64(len(encoded))), CreatedAt: time.Now().UTC()}, info
}

func audioWaveformTestSnapshot() indexedMediaSource {
	return indexedMediaSource{identity: "10:20", relativePath: "Movie/movie.mkv", root: libraryRoot{id: "root-1", path: "/movies", libraryID: "library-1"},
		mediaFile: MediaFile{SourceID: "source-1", Size: 12345, ModifiedAt: time.Unix(1000, 123).UTC(), Item: Item{ID: "item-1", Path: "/movies/Movie/movie.mkv",
			Media: &media.Info{DurationTicks: 20 * media.TicksPerSecond, FileChangeTimeNs: 123456, FormatStartKnown: true,
				Streams: []media.Stream{{Index: 1, CodecType: "audio", Codec: "aac", Channels: 2, SampleRate: 48000, ChannelLayout: "stereo", TimeBase: "1/48000"}}}}}}
}

func TestAudioWaveformSourceStampSurvivesCatalogRebuildAndDirectoryMove(t *testing.T) {
	original := audioWaveformTestSnapshot()
	stamp, err := audioWaveformSourceStamp(original)
	if err != nil {
		t.Fatal(err)
	}
	moved := original
	moved.relativePath = "Relocated/movie.mkv"
	moved.identity = original.identity
	moved.mediaFile.Item.ID, moved.mediaFile.SourceID, moved.root.id = "rebuilt-item", "rebuilt-source", "rebuilt-root"
	moved.mediaFile.Item.Path, moved.root.path, moved.root.libraryID = "/new/Relocated/movie.mkv", "/new", "rebuilt-library"
	moved.rootBindingRevision = 999
	current, err := audioWaveformSourceStamp(moved)
	if err != nil || current != stamp {
		t.Fatalf("catalog/path metadata invalidated an unchanged source: %s, %s, %v", stamp, current, err)
	}
	for _, change := range []func(*indexedMediaSource){
		func(s *indexedMediaSource) { s.identity = "10:21" },
		func(s *indexedMediaSource) { s.mediaFile.Size++ },
		func(s *indexedMediaSource) { s.mediaFile.ModifiedAt = s.mediaFile.ModifiedAt.Add(time.Second) },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.FileChangeTimeNs++ },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.DurationTicks++ },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].Index++ },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams = nil },
	} {
		changed := audioWaveformTestSnapshot()
		change(&changed)
		got, err := audioWaveformSourceStamp(changed)
		if err != nil || got == stamp {
			t.Fatalf("source mutation did not produce a stale stamp: %s, %v", got, err)
		}
	}
}

func TestAudioWaveformNamespaceIsPrivateAndExcludedFromScanning(t *testing.T) {
	root := backgroundClipTestRoot(t)
	waveforms, err := openAudioWaveformDirectory(root, "movie.mkv", true)
	if err != nil {
		t.Fatal(err)
	}
	defer waveforms.Close()
	background, err := openBackgroundClipDirectory(root, "movie.mkv", true)
	if err != nil {
		t.Fatal(err)
	}
	defer background.Close()
	if sameMediaSourceDirectory(waveforms, background) {
		t.Fatal("waveforms shared the background manifest namespace")
	}
	if err := claimAudioWaveformDirectory(waveforms, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".", ".owner.json"} {
		info, err := waveforms.Stat(name)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("generated metadata widened filesystem read permissions: %s %v", name, err)
		}
	}
	relative := "Movie/backdrops/goby-waveforms/" + backgroundClipDirectoryName("movie.mkv") + "/gen-" + strings.Repeat("a", 32) + ".gawf"
	classification, err := classifyThemePath(relative, 0)
	if err != nil || !classification.Reserved || classification.Kind != themePathKindNone {
		t.Fatalf("waveform entered ordinary/theme scan: %+v %v", classification, err)
	}
}

func TestAudioWaveformOriginSelectionChangeMakesExistingTimelineStale(t *testing.T) {
	snapshot := audioWaveformTestSnapshot()
	snapshot.mediaFile.Item.Media.FormatStartTicks = 10 * media.TicksPerSecond
	snapshot.mediaFile.Item.Media.PresentationOriginTicks = 12 * media.TicksPerSecond
	before, err := audioWaveformSourceStamp(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.mediaFile.Item.Media.AudioDurationExact = true
	after, err := audioWaveformSourceStamp(snapshot)
	if err != nil || before == after {
		t.Fatalf("switching playback origin retained the old waveform timeline: %s %s %v", before, after, err)
	}
}

func TestAudioWaveformReadChecksBinaryAndPreservesTrackIdentity(t *testing.T) {
	root := backgroundClipTestRoot(t)
	manifest, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", 2)
	if _, err := writeAudioWaveformJSON(root, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	file, _, artifact, err := readAudioWaveform(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !artifact.Available || artifact.Stale || len(artifact.Tracks) != 2 || artifact.Tracks[0].StreamIndex != 1 || artifact.Tracks[1].StreamIndex != 3 || artifact.ETag == "" {
		t.Fatalf("track identity or current source metadata was lost: %+v", artifact)
	}
	if offset, err := file.Seek(0, io.SeekCurrent); err != nil || offset != 0 {
		t.Fatalf("validated binary was not returned at offset zero: %d %v", offset, err)
	}
	staleFile, _, stale, err := readAudioWaveform(context.Background(), root, "movie.mkv", "waveform-source-v1-"+strings.Repeat("d", 64), true)
	if err != nil {
		t.Fatal(err)
	}
	staleFile.Close()
	if !stale.Available || !stale.Stale {
		t.Fatalf("old material was not retained and marked stale: %+v", stale)
	}
	staleFile, _, stale, err = readAudioWaveform(context.Background(), root, "movie.mkv", manifest.SourceStamp, false)
	if err != nil {
		t.Fatal(err)
	}
	staleFile.Close()
	if !stale.Stale {
		t.Fatal("unscanned on-disk source mutation was not stale")
	}
	if _, err := root.Stat(manifest.Generation); err != nil {
		t.Fatal("stale read removed persistent material", err)
	}
}

func TestAudioWaveformRejectsCorruptionAndFalseManifestSummary(t *testing.T) {
	for _, mode := range []string{"hash", "format", "summary"} {
		t.Run(mode, func(t *testing.T) {
			root := backgroundClipTestRoot(t)
			manifest, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", 2)
			if mode == "summary" {
				manifest.Summary.Tracks = slices.Clone(manifest.Summary.Tracks)
				manifest.Summary.Tracks[1].StreamIndex = 5
			} else {
				data, err := root.ReadFile(manifest.Generation)
				if err != nil {
					t.Fatal(err)
				}
				data[0] = 'X'
				file, err := root.OpenFile(manifest.Generation, os.O_WRONLY|os.O_TRUNC, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.Write(data); err != nil {
					t.Fatal(err)
				}
				file.Close()
				if mode == "format" {
					digest := sha256.Sum256(data)
					manifest.SHA256 = hex.EncodeToString(digest[:])
				}
			}
			if _, err := writeAudioWaveformJSON(root, "manifest.json", manifest); err != nil {
				t.Fatal(err)
			}
			file, _, _, err := readAudioWaveform(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, ErrAudioWaveformStorageConflict) {
				t.Fatalf("invalid %s was accepted: %v", mode, err)
			}
		})
	}
}

func TestAudioWaveformManifestSupportsMaximumTrackInventory(t *testing.T) {
	root := backgroundClipTestRoot(t)
	manifest, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", media.MaxAudioWaveformTracks)
	if _, err := writeAudioWaveformJSON(root, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	file, _, artifact, err := readAudioWaveform(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if len(artifact.Tracks) != media.MaxAudioWaveformTracks || artifact.Size > media.MaxAudioWaveformBytes {
		t.Fatalf("maximum inventory was incomplete: %+v", artifact)
	}
}

func TestAudioWaveformRetentionAndForceOperationIdentity(t *testing.T) {
	artifact := AudioWaveformArtifact{Available: true, Stale: true, OperationID: strings.Repeat("a", 32)}
	if !audioWaveformCanReuse(AudioWaveformJob{OperationID: strings.Repeat("b", 32)}, artifact) {
		t.Fatal("ordinary work rebuilt a stale existing artifact")
	}
	if !audioWaveformCanReuse(AudioWaveformJob{Force: true, OperationID: artifact.OperationID}, artifact) {
		t.Fatal("recovered Force operation encoded twice")
	}
	if audioWaveformCanReuse(AudioWaveformJob{Force: true, OperationID: strings.Repeat("b", 32)}, artifact) {
		t.Fatal("new explicit Force operation did not replace")
	}
}

func TestAudioWaveformForeignFilesAndOrphansArePreserved(t *testing.T) {
	root := backgroundClipTestRoot(t)
	if err := claimAudioWaveformDirectory(root, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	manifest, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", 1)
	if err := checkUnpublishedAudioWaveforms(root, false); !errors.Is(err, ErrAudioWaveformStorageConflict) {
		t.Fatalf("automatic work ignored persistent orphan: %v", err)
	}
	if err := checkUnpublishedAudioWaveforms(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(manifest.Generation); err != nil {
		t.Fatal("orphan was cleaned without replacement", err)
	}
	if _, err := writeAudioWaveformJSON(root, "foreign.json", audioWaveformOwner{}); err != nil {
		t.Fatal(err)
	}
	if err := checkUnpublishedAudioWaveforms(root, true); !errors.Is(err, ErrAudioWaveformStorageConflict) {
		t.Fatalf("force claimed foreign material: %v", err)
	}
	other := backgroundClipTestRoot(t)
	if err := os.Symlink(root.Name(), filepath.Join(other.Name(), "backdrops")); err != nil {
		t.Fatal(err)
	}
	if directory, err := openAudioWaveformDirectory(other, "movie.mkv", true); !errors.Is(err, ErrAudioWaveformStorageConflict) {
		if directory != nil {
			directory.Close()
		}
		t.Fatalf("symlinked storage accepted: %v", err)
	}
}

func audioWaveformTestPublication(t *testing.T, force bool) *audioWaveformPublication {
	t.Helper()
	root := backgroundClipTestRoot(t)
	old := audioWaveformManifest{}
	if force {
		old, _ = audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gawf", 1)
		if _, err := writeAudioWaveformJSON(root, "manifest.json", old); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := audioWaveformTestManifest(t, root, "gen-"+strings.Repeat("b", 32)+".gawf", 2)
	identity, err := writeAudioWaveformJSON(root, ".manifest-new.part", current)
	if err != nil {
		t.Fatal(err)
	}
	publication := &audioWaveformPublication{directory: root, current: current, previous: old, newManifestIdentity: identity, replaced: true, temporaryOwned: true, temporaryIdentity: identity}
	if force {
		if err := mediaEditExchange(root, ".manifest-new.part", root, "manifest.json"); err != nil {
			t.Fatal(err)
		}
		publication.displaced = ".manifest-new.part"
		publication.temporaryIdentity, err = root.Lstat(publication.displaced)
		if err != nil {
			t.Fatal(err)
		}
	} else if err := backgroundClipRenameNoReplace(root, ".manifest-new.part", "manifest.json"); err != nil {
		t.Fatal(err)
	}
	return publication
}

func TestAudioWaveformPublicationFailureRestoresPreviousManifest(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, stage := range []string{"fence", "directory_sync", "read_back", "read_back_close"} {
			t.Run(fmt.Sprintf("force_%t_%s", force, stage), func(t *testing.T) {
				publication := audioWaveformTestPublication(t, force)
				fault := errors.New("injected " + stage)
				var earlier error
				if stage == "fence" {
					earlier = fault
				}
				artifact, err := publication.finish(earlier, func() error {
					if stage == "directory_sync" {
						return fault
					}
					return nil
				}, func() (AudioWaveformArtifact, error) { return AudioWaveformArtifact{Available: true}, fault })
				if !errors.Is(err, fault) || artifact.Available || publication.retainGeneration || !publication.temporaryOwned {
					t.Fatalf("failure committed new data: %+v %v", publication, err)
				}
				var restored audioWaveformManifest
				_, err = readAudioWaveformJSON(publication.directory, "manifest.json", &restored)
				if force {
					if err != nil || !sameAudioWaveformManifest(restored, publication.previous) {
						t.Fatalf("old manifest not restored: %+v %v", restored, err)
					}
					if _, err := publication.directory.Stat(publication.previous.Generation); err != nil {
						t.Fatal("old data removed", err)
					}
				} else if !errors.Is(err, ErrNotFound) {
					t.Fatalf("failed first publication remained visible: %v", err)
				}
			})
		}
	}
}

func TestAudioWaveformRollbackConflictPreservesBothGenerations(t *testing.T) {
	publication := audioWaveformTestPublication(t, true)
	fault := errors.New("read-back failed")
	artifact, err := publication.finish(nil, func() error { return nil }, func() (AudioWaveformArtifact, error) {
		if err := publication.directory.Rename("manifest.json", ".external-saved.json"); err != nil {
			t.Fatal(err)
		}
		if _, err := writeAudioWaveformJSON(publication.directory, "manifest.json", audioWaveformOwner{Format: "foreign"}); err != nil {
			t.Fatal(err)
		}
		return AudioWaveformArtifact{}, fault
	})
	if !errors.Is(err, fault) || !errors.Is(err, ErrAudioWaveformStorageConflict) || artifact.Available || !publication.retainGeneration || publication.temporaryOwned {
		t.Fatalf("failed restoration claimed cleanup: %+v %v", publication, err)
	}
	for _, name := range []string{publication.previous.Generation, publication.current.Generation, publication.displaced} {
		if _, err := publication.directory.Stat(name); err != nil {
			t.Fatalf("recovery material disappeared: %s %v", name, err)
		}
	}
}

func TestAudioWaveformCommittedResultSurvivesLateCancellation(t *testing.T) {
	publication := audioWaveformTestPublication(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var artifact AudioWaveformArtifact
	committed := false
	_, _, workerErr := runAdmittedAnalysisSourceWorker(ctx, func() {}, func() (*os.File, MediaFile, error) {
		var err error
		artifact, err = publication.finish(nil, func() error { return nil }, func() (AudioWaveformArtifact, error) { return AudioWaveformArtifact{Available: true}, nil })
		if err != nil {
			return nil, MediaFile{}, err
		}
		committed = true
		cancel()
		return nil, MediaFile{}, nil
	})
	if !errors.Is(workerErr, context.Canceled) {
		t.Fatal("fixture missed the worker cancellation epilogue", workerErr)
	}
	result, err := audioWaveformWorkerResult(artifact, committed, workerErr)
	if err != nil || !result.Available {
		t.Fatalf("committed generation became failed: %+v %v", result, err)
	}
	for _, failure := range []error{context.Canceled, errors.Join(context.Canceled, ErrUnavailable)} {
		result, err := audioWaveformWorkerResult(artifact, failure != context.Canceled, failure)
		if err == nil || result.Available {
			t.Fatalf("precommit/composite error swallowed: %+v %v", result, err)
		}
	}
}

func TestAudioWaveformSummaryMustIncludeEverySourceTrack(t *testing.T) {
	snapshot := audioWaveformTestSnapshot()
	summary := audioWaveformTestData(1).Summary(100)
	if !audioWaveformSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("matching audio inventory rejected")
	}
	summary.Tracks[0].StreamIndex = 3
	if audioWaveformSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("another audio stream was accepted")
	}
	summary = audioWaveformTestData(2).Summary(100)
	if audioWaveformSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("unindexed track was accepted")
	}
}
