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

func subtitleTimelineTestData(tracks int) media.SubtitleTimelineData {
	data := media.SubtitleTimelineData{Profile: media.SubtitleTimelineProfile, FFprobeSHA256: strings.Repeat("a", 64), DurationTicks: 20 * media.TicksPerSecond}
	for index := 0; index < tracks; index++ {
		track := media.SubtitleTimelineTrack{SubtitleTimelineTrackSummary: media.SubtitleTimelineTrackSummary{StreamIndex: index*2 + 1,
			Codec: "hdmv_pgs_subtitle", IntervalCount: 2},
			Intervals: []media.SubtitleTimelineInterval{{StartTicks: 2 * media.TicksPerSecond, EndTicks: 4 * media.TicksPerSecond},
				{StartTicks: 8 * media.TicksPerSecond, EndTicks: 11 * media.TicksPerSecond}}}
		data.Tracks = append(data.Tracks, track)
	}
	return data
}

func subtitleTimelineTestManifest(t *testing.T, directory *os.Root, generation string, tracks int) (subtitleTimelineManifest, os.FileInfo) {
	t.Helper()
	if err := claimSubtitleTimelineDirectory(directory, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	data := subtitleTimelineTestData(tracks)
	encoded, err := media.MarshalSubtitleTimelines(data)
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
	return subtitleTimelineManifest{Format: subtitleTimelineFileFormat, SourceName: "movie.mkv", SourceRevision: "catalog-source-1",
		SourceStamp: "subtitle-timeline-source-v1-" + strings.Repeat("b", 64), OperationID: strings.Repeat("c", 32), Generation: generation,
		SHA256: hex.EncodeToString(digest[:]), Summary: data.Summary(int64(len(encoded))), CreatedAt: time.Now().UTC()}, info
}

func subtitleTimelineTestSnapshot() indexedMediaSource {
	return indexedMediaSource{identity: "10:20", relativePath: "Movie/movie.mkv", root: libraryRoot{id: "root-1", path: "/movies", libraryID: "library-1"},
		mediaFile: MediaFile{SourceID: "source-1", Size: 12345, ModifiedAt: time.Unix(1000, 123).UTC(), Item: Item{ID: "item-1", Path: "/movies/Movie/movie.mkv",
			Media: &media.Info{DurationTicks: 20 * media.TicksPerSecond, FileChangeTimeNs: 123456, FormatStartKnown: true,
				Streams: []media.Stream{{Index: 1, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", Width: 1920, Height: 1080, TimeBase: "1/90000"}}}}}}
}

func TestSubtitleTimelineSourceStampSurvivesCatalogRebuildAndDirectoryMove(t *testing.T) {
	original := subtitleTimelineTestSnapshot()
	stamp, err := subtitleTimelineSourceStamp(original)
	if err != nil {
		t.Fatal(err)
	}
	moved := original
	moved.relativePath = "Relocated/movie.mkv"
	moved.identity = original.identity
	moved.mediaFile.Item.ID, moved.mediaFile.SourceID, moved.root.id = "rebuilt-item", "rebuilt-source", "rebuilt-root"
	moved.mediaFile.Item.Path, moved.root.path, moved.root.libraryID = "/new/Relocated/movie.mkv", "/new", "rebuilt-library"
	moved.rootBindingRevision = 999
	current, err := subtitleTimelineSourceStamp(moved)
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
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].Codec = "dvd_subtitle" },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].TimeBase = "1/1000" },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].Width++ },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].CodecTag = "changed" },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].IsForced = true },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams[0].IsExternal = true },
		func(s *indexedMediaSource) { s.mediaFile.Item.Media.Streams = nil },
	} {
		changed := subtitleTimelineTestSnapshot()
		change(&changed)
		got, err := subtitleTimelineSourceStamp(changed)
		if err != nil || got == stamp {
			t.Fatalf("source mutation did not produce a stale stamp: %s, %v", got, err)
		}
	}
}

func TestSubtitleTimelineNamespaceIsPrivateAndExcludedFromScanning(t *testing.T) {
	root := backgroundClipTestRoot(t)
	timelines, err := openSubtitleTimelineDirectory(root, "movie.mkv", true)
	if err != nil {
		t.Fatal(err)
	}
	defer timelines.Close()
	background, err := openBackgroundClipDirectory(root, "movie.mkv", true)
	if err != nil {
		t.Fatal(err)
	}
	defer background.Close()
	if sameMediaSourceDirectory(timelines, background) {
		t.Fatal("subtitle timelines shared the background manifest namespace")
	}
	if err := claimSubtitleTimelineDirectory(timelines, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".", ".owner.json"} {
		info, err := timelines.Stat(name)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("generated metadata widened filesystem read permissions: %s %v", name, err)
		}
	}
	relative := "Movie/backdrops/goby-subtitle-timelines/" + backgroundClipDirectoryName("movie.mkv") + "/gen-" + strings.Repeat("a", 32) + ".gstl"
	classification, err := classifyThemePath(relative, 0)
	if err != nil || !classification.Reserved || classification.Kind != themePathKindNone {
		t.Fatalf("subtitle timeline entered ordinary/theme scan: %+v %v", classification, err)
	}
}

func TestSubtitleTimelineOriginSelectionChangeMakesExistingTimelineStale(t *testing.T) {
	snapshot := subtitleTimelineTestSnapshot()
	snapshot.mediaFile.Item.Media.FormatStartTicks = 10 * media.TicksPerSecond
	snapshot.mediaFile.Item.Media.PresentationOriginTicks = 12 * media.TicksPerSecond
	before, err := subtitleTimelineSourceStamp(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.mediaFile.Item.Media.AudioDurationExact = true
	after, err := subtitleTimelineSourceStamp(snapshot)
	if err != nil || before == after {
		t.Fatalf("switching playback origin retained the old subtitle timeline: %s %s %v", before, after, err)
	}
}

func TestSubtitleTimelineReadChecksBinaryAndPreservesTrackIdentity(t *testing.T) {
	root := backgroundClipTestRoot(t)
	manifest, _ := subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gstl", 2)
	if _, err := writeSubtitleTimelineJSON(root, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	file, _, artifact, err := readSubtitleTimeline(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
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
	staleFile, _, stale, err := readSubtitleTimeline(context.Background(), root, "movie.mkv", "subtitle-timeline-source-v1-"+strings.Repeat("d", 64), true)
	if err != nil {
		t.Fatal(err)
	}
	staleFile.Close()
	if !stale.Available || !stale.Stale {
		t.Fatalf("old material was not retained and marked stale: %+v", stale)
	}
	staleFile, _, stale, err = readSubtitleTimeline(context.Background(), root, "movie.mkv", manifest.SourceStamp, false)
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

func TestSubtitleTimelineRejectsCorruptionAndFalseManifestSummary(t *testing.T) {
	for _, mode := range []string{"hash", "format", "summary", "codec", "warnings", "tool"} {
		t.Run(mode, func(t *testing.T) {
			root := backgroundClipTestRoot(t)
			manifest, _ := subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gstl", 2)
			if mode == "summary" || mode == "codec" || mode == "warnings" || mode == "tool" {
				manifest.Summary.Tracks = slices.Clone(manifest.Summary.Tracks)
				switch mode {
				case "summary":
					manifest.Summary.Tracks[1].StreamIndex = 5
				case "codec":
					manifest.Summary.Tracks[1].Codec = "dvd_subtitle"
				case "warnings":
					manifest.Summary.Tracks[1].Warnings = []string{"dvd_palette_missing_monochrome_review"}
				case "tool":
					manifest.Summary.FFprobeSHA256 = strings.Repeat("f", 64)
				}
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
			if _, err := writeSubtitleTimelineJSON(root, "manifest.json", manifest); err != nil {
				t.Fatal(err)
			}
			file, _, _, err := readSubtitleTimeline(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
			if file != nil {
				file.Close()
			}
			if !errors.Is(err, ErrSubtitleTimelineStorageConflict) {
				t.Fatalf("invalid %s was accepted: %v", mode, err)
			}
		})
	}
}

func TestSubtitleTimelineManifestSupportsMaximumTrackInventory(t *testing.T) {
	root := backgroundClipTestRoot(t)
	manifest, _ := subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gstl", media.MaxSubtitleTimelineTracks)
	if _, err := writeSubtitleTimelineJSON(root, "manifest.json", manifest); err != nil {
		t.Fatal(err)
	}
	file, _, artifact, err := readSubtitleTimeline(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if len(artifact.Tracks) != media.MaxSubtitleTimelineTracks || artifact.Size > media.MaxSubtitleTimelineBytes {
		t.Fatalf("maximum inventory was incomplete: %+v", artifact)
	}
}

func TestSubtitleTimelineRetentionAndForceOperationIdentity(t *testing.T) {
	artifact := SubtitleTimelineArtifact{Available: true, Stale: true, OperationID: strings.Repeat("a", 32)}
	if !subtitleTimelineCanReuse(SubtitleTimelineJob{OperationID: strings.Repeat("b", 32)}, artifact) {
		t.Fatal("ordinary work rebuilt a stale existing artifact")
	}
	if !subtitleTimelineCanReuse(SubtitleTimelineJob{Force: true, OperationID: artifact.OperationID}, artifact) {
		t.Fatal("recovered Force operation encoded twice")
	}
	if subtitleTimelineCanReuse(SubtitleTimelineJob{Force: true, OperationID: strings.Repeat("b", 32)}, artifact) {
		t.Fatal("new explicit Force operation did not replace")
	}
}

func TestSubtitleTimelineForeignFilesAndOrphansArePreserved(t *testing.T) {
	root := backgroundClipTestRoot(t)
	if err := claimSubtitleTimelineDirectory(root, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	manifest, _ := subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gstl", 1)
	if err := checkUnpublishedSubtitleTimelines(root, false); !errors.Is(err, ErrSubtitleTimelineStorageConflict) {
		t.Fatalf("automatic work ignored persistent orphan: %v", err)
	}
	if err := checkUnpublishedSubtitleTimelines(root, true); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(manifest.Generation); err != nil {
		t.Fatal("orphan was cleaned without replacement", err)
	}
	if _, err := writeSubtitleTimelineJSON(root, "foreign.json", subtitleTimelineOwner{}); err != nil {
		t.Fatal(err)
	}
	if err := checkUnpublishedSubtitleTimelines(root, true); !errors.Is(err, ErrSubtitleTimelineStorageConflict) {
		t.Fatalf("force claimed foreign material: %v", err)
	}
	other := backgroundClipTestRoot(t)
	if err := os.Symlink(root.Name(), filepath.Join(other.Name(), "backdrops")); err != nil {
		t.Fatal(err)
	}
	if directory, err := openSubtitleTimelineDirectory(other, "movie.mkv", true); !errors.Is(err, ErrSubtitleTimelineStorageConflict) {
		if directory != nil {
			directory.Close()
		}
		t.Fatalf("symlinked storage accepted: %v", err)
	}
}

func subtitleTimelineTestPublication(t *testing.T, force bool) *subtitleTimelinePublication {
	t.Helper()
	root := backgroundClipTestRoot(t)
	old := subtitleTimelineManifest{}
	if force {
		old, _ = subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gstl", 1)
		if _, err := writeSubtitleTimelineJSON(root, "manifest.json", old); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("b", 32)+".gstl", 2)
	identity, err := writeSubtitleTimelineJSON(root, ".manifest-new.part", current)
	if err != nil {
		t.Fatal(err)
	}
	publication := &subtitleTimelinePublication{directory: root, current: current, previous: old, newManifestIdentity: identity, replaced: true, temporaryOwned: true, temporaryIdentity: identity}
	if force {
		if err := mediaEditExchange(root, ".manifest-new.part", root, "manifest.json"); err != nil {
			t.Fatal(err)
		}
		publication.displaced = ".manifest-new.part"
		publication.temporaryIdentity, err = root.Lstat(publication.displaced)
		if err != nil {
			t.Fatal(err)
		}
		publication.displacedIdentity = publication.temporaryIdentity
	} else if err := backgroundClipRenameNoReplace(root, ".manifest-new.part", "manifest.json"); err != nil {
		t.Fatal(err)
	}
	return publication
}

func TestSubtitleTimelinePublicationFailureRestoresPreviousManifest(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, stage := range []string{"fence", "directory_sync", "read_back", "read_back_close"} {
			t.Run(fmt.Sprintf("force_%t_%s", force, stage), func(t *testing.T) {
				publication := subtitleTimelineTestPublication(t, force)
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
				}, func() (SubtitleTimelineArtifact, error) { return SubtitleTimelineArtifact{Available: true}, fault })
				if !errors.Is(err, fault) || artifact.Available || publication.retainGeneration || !publication.temporaryOwned {
					t.Fatalf("failure committed new data: %+v %v", publication, err)
				}
				var restored subtitleTimelineManifest
				_, err = readSubtitleTimelineJSON(publication.directory, "manifest.json", &restored)
				if force {
					if err != nil || !sameSubtitleTimelineManifest(restored, publication.previous) {
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

func TestSubtitleTimelineRollbackConflictPreservesBothGenerations(t *testing.T) {
	publication := subtitleTimelineTestPublication(t, true)
	fault := errors.New("read-back failed")
	artifact, err := publication.finish(nil, func() error { return nil }, func() (SubtitleTimelineArtifact, error) {
		if err := publication.directory.Rename("manifest.json", ".external-saved.json"); err != nil {
			t.Fatal(err)
		}
		if _, err := writeSubtitleTimelineJSON(publication.directory, "manifest.json", subtitleTimelineOwner{Format: "foreign"}); err != nil {
			t.Fatal(err)
		}
		return SubtitleTimelineArtifact{}, fault
	})
	if !errors.Is(err, fault) || !errors.Is(err, ErrSubtitleTimelineStorageConflict) || artifact.Available || !publication.retainGeneration || publication.temporaryOwned {
		t.Fatalf("failed restoration claimed cleanup: %+v %v", publication, err)
	}
	for _, name := range []string{publication.previous.Generation, publication.current.Generation, publication.displaced} {
		if _, err := publication.directory.Stat(name); err != nil {
			t.Fatalf("recovery material disappeared: %s %v", name, err)
		}
	}
}

func TestSubtitleTimelineCommittedResultSurvivesLateCancellation(t *testing.T) {
	publication := subtitleTimelineTestPublication(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var artifact SubtitleTimelineArtifact
	committed := false
	_, _, workerErr := runAdmittedAnalysisSourceWorker(ctx, func() {}, func() (*os.File, MediaFile, error) {
		var err error
		artifact, err = publication.finish(nil, func() error { return nil }, func() (SubtitleTimelineArtifact, error) { return SubtitleTimelineArtifact{Available: true}, nil })
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
	result, err := subtitleTimelineWorkerResult(artifact, committed, workerErr)
	if err != nil || !result.Available {
		t.Fatalf("committed generation became failed: %+v %v", result, err)
	}
	for _, failure := range []error{context.Canceled, errors.Join(context.Canceled, ErrUnavailable)} {
		result, err := subtitleTimelineWorkerResult(artifact, failure != context.Canceled, failure)
		if err == nil || result.Available {
			t.Fatalf("precommit/composite error swallowed: %+v %v", result, err)
		}
	}
}

func TestSubtitleTimelineRollbackRejectsDisplacedManifestReplacement(t *testing.T) {
	for _, mutation := range []string{"content", "identity"} {
		t.Run(mutation, func(t *testing.T) {
			publication := subtitleTimelineTestPublication(t, true)
			fault := errors.New("read-back failed")
			artifact, err := publication.finish(nil, func() error { return nil }, func() (SubtitleTimelineArtifact, error) {
				if err := publication.directory.Rename(publication.displaced, ".saved-previous.json"); err != nil {
					t.Fatal(err)
				}
				value := publication.previous
				if mutation == "content" {
					value.SourceName = "foreign.mkv"
				}
				if _, err := writeSubtitleTimelineJSON(publication.directory, publication.displaced, value); err != nil {
					t.Fatal(err)
				}
				return SubtitleTimelineArtifact{}, fault
			})
			if !errors.Is(err, fault) || !errors.Is(err, ErrSubtitleTimelineStorageConflict) || artifact.Available || !publication.retainGeneration || publication.temporaryOwned {
				t.Fatalf("foreign displaced manifest was installed or removed: %+v %v", publication, err)
			}
			var current subtitleTimelineManifest
			if _, err := readSubtitleTimelineJSON(publication.directory, "manifest.json", &current); err != nil || !sameSubtitleTimelineManifest(current, publication.current) {
				t.Fatalf("rollback installed unverified prior material: %+v %v", current, err)
			}
			for _, name := range []string{publication.previous.Generation, publication.current.Generation, publication.displaced, ".saved-previous.json"} {
				if _, err := publication.directory.Stat(name); err != nil {
					t.Fatalf("recovery material disappeared: %s %v", name, err)
				}
			}
		})
	}
}

func TestSubtitleTimelineSummaryMustIncludeEverySourceTrack(t *testing.T) {
	snapshot := subtitleTimelineTestSnapshot()
	summary := subtitleTimelineTestData(1).Summary(100)
	if !subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("matching bitmap subtitle inventory rejected")
	}
	summary.Tracks[0].StreamIndex = 3
	if subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("another subtitle stream was accepted")
	}
	summary = subtitleTimelineTestData(2).Summary(100)
	if subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("unindexed track was accepted")
	}
	snapshot.mediaFile.Item.Media.Streams = append(snapshot.mediaFile.Item.Media.Streams,
		media.Stream{Index: 3, CodecType: "subtitle", Codec: "dvd_subtitle"},
		media.Stream{Index: 5, CodecType: "subtitle", Codec: "subrip"},
		media.Stream{Index: 7, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", IsExternal: true})
	summary.Tracks[1].Codec = "dvd_subtitle"
	if !subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("complete embedded bitmap inventory rejected because unrelated text or external tracks exist")
	}
	summary.Tracks = summary.Tracks[:1]
	if subtitleTimelineSummaryMatchesSource(summary, snapshot.mediaFile.Item.Media) {
		t.Fatal("partial generation omitted an admitted DVD track")
	}
}

func TestSubtitleTimelineReadRequiresMatchingOwnerAndRejectsSymlinks(t *testing.T) {
	for _, mode := range []string{"owner_missing", "owner_foreign", "owner_symlink", "manifest_symlink", "payload_symlink"} {
		t.Run(mode, func(t *testing.T) {
			root := backgroundClipTestRoot(t)
			manifest, _ := subtitleTimelineTestManifest(t, root, "gen-"+strings.Repeat("a", 32)+".gstl", 1)
			if _, err := writeSubtitleTimelineJSON(root, "manifest.json", manifest); err != nil {
				t.Fatal(err)
			}
			name := ".owner.json"
			if mode == "manifest_symlink" {
				name = "manifest.json"
			} else if mode == "payload_symlink" {
				name = manifest.Generation
			}
			if err := root.Rename(name, ".saved-original"); err != nil {
				t.Fatal(err)
			}
			if mode == "owner_foreign" {
				if _, err := writeSubtitleTimelineJSON(root, name, subtitleTimelineOwner{Format: subtitleTimelineFileFormat, SourceName: "other.mkv"}); err != nil {
					t.Fatal(err)
				}
			} else if strings.HasSuffix(mode, "symlink") {
				if err := os.Symlink(".saved-original", filepath.Join(root.Name(), name)); err != nil {
					t.Fatal(err)
				}
			}
			file, _, _, err := readSubtitleTimeline(context.Background(), root, "movie.mkv", manifest.SourceStamp, true)
			if file != nil {
				file.Close()
			}
			if mode == "owner_missing" {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("missing owner was not hidden: %v", err)
				}
			} else if !errors.Is(err, ErrSubtitleTimelineStorageConflict) {
				t.Fatalf("unsafe %s returned material: %v", mode, err)
			}
			if _, err := root.Stat(".saved-original"); err != nil {
				t.Fatal("read removed retained material", err)
			}
		})
	}
}

func TestSubtitleTimelineWarningSummaryIsCopied(t *testing.T) {
	tracks := []media.SubtitleTimelineTrackSummary{{StreamIndex: 1, Codec: "dvd_subtitle", Warnings: []string{"dvd_palette_missing_monochrome_review"}}}
	copy := cloneSubtitleTimelineTracks(tracks)
	copy[0].Warnings[0] = "transparent_dvd_display_ignored"
	if tracks[0].Warnings[0] != "dvd_palette_missing_monochrome_review" {
		t.Fatal("artifact metadata shared mutable warning storage")
	}
}
