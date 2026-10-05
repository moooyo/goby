package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func TestBackgroundClipExplicitGenerationChangesVersionEvenForIdenticalBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clip.mp4")
	if err := os.WriteFile(path, []byte("same generated content"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	value := backgroundClipManifest{Generation: "gen-" + strings.Repeat("a", 32) + ".mp4", SHA256: strings.Repeat("f", 64)}
	before := backgroundClipArtifact(value, info, "").ETag
	if backgroundClipArtifact(value, info, "").ETag != before {
		t.Fatal("reusing a generation changed its validator")
	}
	value.Generation = "gen-" + strings.Repeat("b", 32) + ".mp4"
	if backgroundClipArtifact(value, info, "").ETag == before {
		t.Fatal("explicit regeneration retained a failed playback URL")
	}
}

func backgroundClipTestRoot(t *testing.T) *os.Root {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("persistent sidecar publication requires Linux")
	}
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func backgroundClipTestManifest(t *testing.T, root *os.Root, source, generation string) backgroundClipManifest {
	t.Helper()
	data := make([]byte, 128)
	copy(data, []byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'})
	file, err := root.OpenFile(generation, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	return backgroundClipManifest{Format: backgroundClipFormat, SourceName: source, SourceRevision: "source-v1",
		SourceSnapshot: `"original"`, Generation: generation, Profile: "fixture-v1", StartTicks: 300000000,
		DurationTicks: 250000000, Width: 1280, Height: 720, Size: int64(len(data)), SHA256: hex.EncodeToString(digest[:]),
		FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), CreatedAt: time.Now().UTC()}
}

func TestBackgroundClipFilenameIdentityAndReservedLayout(t *testing.T) {
	name := "Example S01E01.mkv"
	key := backgroundClipDirectoryName(name)
	if len(key) != 64 || key != backgroundClipDirectoryName(filepath.Base("moved/"+name)) || key == backgroundClipDirectoryName("Example S01E02.mkv") {
		t.Fatal("sidecar identity must survive a folder move and distinguish episodes")
	}
	for _, relative := range []string{"Movie/backdrops/goby/" + key + "/gen-" + strings.Repeat("a", 32) + ".mp4", "Season 01/backdrops/goby/" + key + "/.clip-temporary.part"} {
		classification, err := classifyThemePath(relative, 0)
		if err != nil || !classification.Reserved || classification.Kind != themePathKindNone {
			t.Fatalf("generated material entered ordinary or theme inventory: %+v, %v", classification, err)
		}
	}
}

func TestBackgroundClipReadRetainsPreviousSourceSnapshot(t *testing.T) {
	root := backgroundClipTestRoot(t)
	value := backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("a", 32)+".mp4")
	if err := writeBackgroundClipJSON(root, "manifest.json", value); err != nil {
		t.Fatal(err)
	}
	file, _, artifact, err := readBackgroundClip(root, "movie.mkv", `"new-indexed-source"`)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if !artifact.Available || !artifact.SourceChanged || artifact.Size != value.Size || artifact.ETag == "" {
		t.Fatalf("source edits must retain an auditable old generation: %+v", artifact)
	}
	if file, _, _, err := readBackgroundClip(root, "other.mkv", value.SourceSnapshot); !errors.Is(err, ErrBackgroundClipConflict) {
		if file != nil {
			file.Close()
		}
		t.Fatalf("another source claimed the generation: %v", err)
	}
}

func TestBackgroundClipOwnerRejectsForeignFilesAndSymlinks(t *testing.T) {
	root := backgroundClipTestRoot(t)
	foreign, err := root.OpenFile("personal.mp4", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	foreign.Close()
	if err := claimBackgroundClipDirectory(root, "movie.mkv"); !errors.Is(err, ErrBackgroundClipConflict) {
		t.Fatalf("nonempty unowned directory was claimed: %v", err)
	}
	path := t.TempDir()
	if err := os.Symlink(path, filepath.Join(root.Name(), "backdrops")); err != nil {
		t.Fatal(err)
	}
	if directory, err := openBackgroundClipDirectory(root, "movie.mkv", true); !errors.Is(err, ErrBackgroundClipConflict) {
		if directory != nil {
			directory.Close()
		}
		t.Fatalf("symlinked sidecar subtree was accepted: %v", err)
	}
}

func TestBackgroundClipCrashOrphanRequiresExplicitRegeneration(t *testing.T) {
	root := backgroundClipTestRoot(t)
	if err := claimBackgroundClipDirectory(root, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("a", 32)+".mp4")
	if err := checkUnpublishedBackgroundClips(root, false); !errors.Is(err, ErrBackgroundClipConflict) {
		t.Fatalf("an automatic task silently bypassed an existing unpublished generation: %v", err)
	}
	if err := checkUnpublishedBackgroundClips(root, true); err != nil {
		t.Fatal(err)
	}
}

func TestBackgroundClipPublicationDoesNotReplaceForeignGeneration(t *testing.T) {
	root := backgroundClipTestRoot(t)
	for _, name := range []string{"new.part", "existing.mp4"} {
		file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
		if err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
	if err := backgroundClipRenameNoReplace(root, "new.part", "existing.mp4"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("immutable generation was replaced: %v", err)
	}
	if _, err := root.Stat("new.part"); err != nil {
		t.Fatal("failed publication lost its staging file", err)
	}
}

func TestBackgroundClipManifestRollbackKeepsOldGeneration(t *testing.T) {
	root := backgroundClipTestRoot(t)
	old := backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("a", 32)+".mp4")
	current := backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("b", 32)+".mp4")
	if err := writeBackgroundClipJSON(root, "manifest.json", current); err != nil {
		t.Fatal(err)
	}
	if err := rollbackBackgroundClipManifest(root, strings.Repeat("c", 32), current, old, ""); err != nil {
		t.Fatal(err)
	}
	file, restored, _, err := readBackgroundClip(root, "movie.mkv", old.SourceSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if restored != old {
		t.Fatalf("failed fenced publication did not restore the previous manifest: %+v", restored)
	}
	if _, err := root.Stat(current.Generation); err != nil {
		t.Fatal("rollback helper must not delete either generation", err)
	}
}

func TestBackgroundClipExchangeRollbackRestoresForeignEntry(t *testing.T) {
	root := backgroundClipTestRoot(t)
	current := backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("b", 32)+".mp4")
	if err := writeBackgroundClipJSON(root, ".manifest-pending.part", current); err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(t.TempDir(), "personal.json")
	if err := os.WriteFile(foreignPath, []byte("personal material"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignPath, filepath.Join(root.Name(), "manifest.json")); err != nil {
		t.Fatal(err)
	}
	if err := mediaEditExchange(root, ".manifest-pending.part", root, "manifest.json"); err != nil {
		t.Fatal(err)
	}
	if err := rollbackBackgroundClipManifest(root, strings.Repeat("c", 32), current, backgroundClipManifest{}, ".manifest-pending.part"); err != nil {
		t.Fatal(err)
	}
	info, err := root.Lstat("manifest.json")
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("rollback failed to restore the exact foreign directory entry", err)
	}
	if data, err := os.ReadFile(foreignPath); err != nil || string(data) != "personal material" {
		t.Fatal("foreign material was modified", err)
	}
}

func TestBackgroundClipDirectoryLockPreventsConcurrentWriters(t *testing.T) {
	root := backgroundClipTestRoot(t)
	unlock, err := lockBackgroundClipDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	second, err := lockBackgroundClipDirectory(root)
	if !errors.Is(err, ErrBusy) {
		if second != nil {
			second()
		}
		t.Fatalf("a second sidecar writer acquired the same directory: %v", err)
	}
}

func TestBackgroundClipCandidatesRespectManualStartAndCredits(t *testing.T) {
	job := BackgroundPreviewJob{StartTicks: 30 * media.TicksPerSecond, DurationTicks: 25 * media.TicksPerSecond}
	item := Item{Media: &media.Info{DurationTicks: 180 * media.TicksPerSecond}, Credits: &CreditsPoint{StartTicks: 90 * media.TicksPerSecond}}
	candidates := backgroundClipCandidates(job, item)
	if len(candidates) != 2 || candidates[0] != 30*media.TicksPerSecond || candidates[1] != 55*media.TicksPerSecond {
		t.Fatalf("candidates crossed credits or left the nearby bounded window: %v", candidates)
	}
	manual := job.StartTicks
	job.ManualStartTicks = &manual
	if candidates := backgroundClipCandidates(job, item); len(candidates) != 1 || candidates[0] != manual {
		t.Fatalf("manual editorial input was silently moved: %v", candidates)
	}
}

func TestBackgroundClipCleanupRetainsReplacementAndExclusiveCreateCollision(t *testing.T) {
	root := backgroundClipTestRoot(t)
	created, err := writeBackgroundClipJSONOwned(root, ".manifest-owned.part", backgroundClipOwner{Format: backgroundClipFormat, SourceName: "movie.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Rename(".manifest-owned.part", ".manifest-original.part"); err != nil {
		t.Fatal(err)
	}
	foreign, err := root.OpenFile(".manifest-owned.part", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreign.WriteString("personal material"); err != nil {
		t.Fatal(err)
	}
	foreign.Close()
	if err := removeOwnedBackgroundClipFile(root, ".manifest-owned.part", created); !errors.Is(err, ErrBackgroundClipConflict) {
		t.Fatalf("cleanup acquired a replacement entry: %v", err)
	}
	if _, err := writeBackgroundClipJSONOwned(root, ".manifest-owned.part", backgroundClipOwner{}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("exclusive create collision was not preserved: %v", err)
	}
	data, err := root.ReadFile(".manifest-owned.part")
	if err != nil || string(data) != "personal material" {
		t.Fatalf("foreign material was deleted or changed: %q, %v", data, err)
	}
}

func TestBackgroundClipPrivateStorageDoesNotWidenSourceReadAccess(t *testing.T) {
	root := backgroundClipTestRoot(t)
	directory, err := openBackgroundClipDirectory(root, "movie.mkv", true)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := claimBackgroundClipDirectory(directory, "movie.mkv"); err != nil {
		t.Fatal(err)
	}
	info, err := directory.Stat(".")
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("source-specific directory grants another user access", err)
	}
	info, err = directory.Stat(".owner.json")
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("generated metadata grants another user access", err)
	}
}

func backgroundClipTestPublication(t *testing.T, force bool) (*backgroundClipPublication, backgroundClipManifest) {
	t.Helper()
	root := backgroundClipTestRoot(t)
	old := backgroundClipManifest{}
	if force {
		old = backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("a", 32)+".mp4")
		if err := writeBackgroundClipJSON(root, "manifest.json", old); err != nil {
			t.Fatal(err)
		}
	}
	current := backgroundClipTestManifest(t, root, "movie.mkv", "gen-"+strings.Repeat("b", 32)+".mp4")
	identity, err := writeBackgroundClipJSONOwned(root, ".manifest-new.part", current)
	if err != nil {
		t.Fatal(err)
	}
	publication := &backgroundClipPublication{directory: root, id: strings.Repeat("c", 32), current: current, previous: old,
		newManifestIdentity: identity, replaced: true, temporaryOwned: true, temporaryIdentity: identity}
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
	return publication, old
}

func TestBackgroundClipPublicationFailuresRestorePreviousManifest(t *testing.T) {
	for _, force := range []bool{false, true} {
		for _, stage := range []string{"fence", "directory_sync", "read_back", "read_back_close"} {
			t.Run(fmt.Sprintf("force_%t_%s", force, stage), func(t *testing.T) {
				publication, old := backgroundClipTestPublication(t, force)
				fault := errors.New("injected " + stage + " failure")
				var publishErr error
				if stage == "fence" {
					publishErr = fault
				}
				readBackCalled := false
				artifact, err := publication.finish(publishErr, func() error {
					if stage == "directory_sync" {
						return fault
					}
					return syncBackgroundClipDirectory(publication.directory)
				}, func() (BackgroundClipArtifact, error) {
					readBackCalled = true
					if stage == "read_back" || stage == "read_back_close" {
						return BackgroundClipArtifact{Available: true}, fault
					}
					return BackgroundClipArtifact{Available: true}, nil
				})
				if !errors.Is(err, fault) || artifact.Available || publication.retainGeneration || !publication.temporaryOwned {
					t.Fatalf("failed publication escaped rollback: artifact=%+v, publication=%+v, error=%v", artifact, publication, err)
				}
				if (stage == "fence" || stage == "directory_sync") && readBackCalled {
					t.Fatal("read-back ran after an earlier failure")
				}
				var restored backgroundClipManifest
				_, readErr := readBackgroundClipJSON(publication.directory, "manifest.json", &restored)
				if force {
					if readErr != nil || restored != old {
						t.Fatalf("old manifest was not restored: %+v, %v", restored, readErr)
					}
					if _, err := publication.directory.Stat(old.Generation); err != nil {
						t.Fatal("old generation was lost", err)
					}
				} else if !errors.Is(readErr, ErrNotFound) {
					t.Fatalf("failed first publication stayed visible: %v", readErr)
				}
				if _, err := publication.directory.Stat(publication.current.Generation); err != nil {
					t.Fatal("finalization must leave new bytes for its owner's verified cleanup", err)
				}
			})
		}
	}
}

func TestBackgroundClipPublicationRestorationFailurePreservesBothGenerations(t *testing.T) {
	publication, old := backgroundClipTestPublication(t, true)
	fault := errors.New("injected read-back failure")
	artifact, err := publication.finish(nil, func() error { return nil }, func() (BackgroundClipArtifact, error) {
		// Model an external replacement after the atomic publication. Neither
		// restoring our old pointer nor removing the external pointer is safe.
		if err := publication.directory.Rename("manifest.json", ".external-saved.json"); err != nil {
			t.Fatal(err)
		}
		file, err := publication.directory.OpenFile("manifest.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString("external material")
		if err := errors.Join(writeErr, file.Close()); err != nil {
			t.Fatal(err)
		}
		return BackgroundClipArtifact{}, fault
	})
	if !errors.Is(err, fault) || !errors.Is(err, ErrBackgroundClipConflict) || artifact.Available || !publication.retainGeneration || publication.temporaryOwned {
		t.Fatalf("failed restoration granted cleanup ownership: %+v, %v", publication, err)
	}
	for _, name := range []string{old.Generation, publication.current.Generation, publication.displaced} {
		if _, err := publication.directory.Stat(name); err != nil {
			t.Fatalf("recovery material %q was lost: %v", name, err)
		}
	}
	data, err := publication.directory.ReadFile("manifest.json")
	if err != nil || string(data) != "external material" {
		t.Fatal("external manifest was altered", err)
	}
}

func TestBackgroundClipPublicationCommitsOnlyAfterSuccessfulVerification(t *testing.T) {
	publication, old := backgroundClipTestPublication(t, true)
	order := ""
	artifact, err := publication.finish(nil, func() error {
		order += "sync;"
		return syncBackgroundClipDirectory(publication.directory)
	}, func() (BackgroundClipArtifact, error) {
		if publication.retainGeneration {
			t.Fatal("publication committed before read-back completed")
		}
		order += "read;"
		return BackgroundClipArtifact{Available: true}, nil
	})
	if err != nil || !artifact.Available || !publication.retainGeneration || order != "sync;read;" {
		t.Fatalf("publication did not commit consistently: %+v, %s, %v", publication, order, err)
	}
	if _, err := publication.directory.Stat(old.Generation); err != nil {
		t.Fatal("successful commit must not depend on deleting old material", err)
	}
}

func TestBackgroundClipCommittedResultSurvivesWorkerEpilogueCancellation(t *testing.T) {
	publication, _ := backgroundClipTestPublication(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var artifact BackgroundClipArtifact
	committed, released := false, false
	_, _, workerErr := runAdmittedAnalysisSourceWorker(ctx, func() { released = true }, func() (*os.File, MediaFile, error) {
		var err error
		artifact, err = publication.finish(nil, func() error { return syncBackgroundClipDirectory(publication.directory) }, func() (BackgroundClipArtifact, error) {
			return BackgroundClipArtifact{Available: true}, nil
		})
		if err != nil {
			return nil, MediaFile{}, err
		}
		committed = true
		// Cancellation during post-commit old-file retirement must not turn a
		// durable successful replacement into a failed generation result.
		cancel()
		return nil, MediaFile{}, nil
	})
	if !released || !errors.Is(workerErr, context.Canceled) {
		t.Fatalf("fixture did not reach the actual worker cancellation epilogue: %v", workerErr)
	}
	result, err := backgroundClipWorkerResult(artifact, committed, workerErr)
	if err != nil || !result.Available || !publication.retainGeneration {
		t.Fatalf("committed publication became a failed result: %+v, %v", result, err)
	}
	if result, err := backgroundClipWorkerResult(artifact, false, context.Canceled); !errors.Is(err, context.Canceled) || result.Available {
		t.Fatalf("cancellation before commit was swallowed: %+v, %v", result, err)
	}
	if result, err := backgroundClipWorkerResult(artifact, true, ErrUnavailable); !errors.Is(err, ErrUnavailable) || result.Available {
		t.Fatalf("unrelated worker failure was swallowed: %+v, %v", result, err)
	}
}
