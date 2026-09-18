//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

type fileDeletionTestFixture struct {
	store     *Store
	spec      fileDeletionSpec
	path      string
	stagePath string
	contents  string
}

func newFileDeletionTestFixture(t *testing.T) fileDeletionTestFixture {
	t.Helper()
	roots := newRootBindingPathsFixture(t)
	path := filepath.Join(roots.first.path, "Nested", "Feature.mkv")
	const contents = "video:delete-only-the-indexed-file"
	fileDeletionTestWrite(t, path, contents)
	info := fileDeletionTestStat(t, path)
	const stageName = ".goby-delete-0123456789abcdef0123456789abcdef"
	return fileDeletionTestFixture{
		store: roots.store,
		spec: fileDeletionSpec{
			Root: roots.first, RelativePath: "Nested/Feature.mkv", Identity: fileIdentity(info),
			Size: info.Size(), ModifiedAt: catalogModifiedTime(info), ChangeTimeNs: media.FileChangeTime(info),
			StageName: stageName,
		},
		path: path, stagePath: filepath.Join(filepath.Dir(path), stageName), contents: contents,
	}
}

func fileDeletionTestWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fileDeletionTestStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	return info
}

func fileDeletionTestContents(t *testing.T, path, expected string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != expected {
		t.Fatalf("read %q: got %q, %v; want %q", path, contents, err, expected)
	}
}

func fileDeletionTestMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %q survived or could not be checked: %v", path, err)
	}
}

func fileDeletionTestPrepare(t *testing.T, fixture fileDeletionTestFixture) *fileDeletionCapture {
	t.Helper()
	capture, err := fixture.store.prepareFileDeletion(context.Background(), fixture.spec)
	if err != nil {
		t.Fatalf("prepare indexed file deletion: %v", err)
	}
	t.Cleanup(func() { _ = capture.Close() })
	return capture
}

func fileDeletionTestStage(t *testing.T, fixture fileDeletionTestFixture, capture *fileDeletionCapture) fileDeletionSpec {
	t.Helper()
	identity, changeTime, err := capture.Stage(context.Background())
	if err != nil {
		t.Fatalf("stage indexed file deletion: %v", err)
	}
	if !capture.Staged() {
		t.Fatal("capture did not record its staging directory")
	}
	if !capture.HasStagedPayload() {
		t.Fatal("capture did not retain the staged payload state")
	}
	payload := filepath.Join(fixture.stagePath, "payload")
	info := fileDeletionTestStat(t, payload)
	if !info.Mode().IsRegular() || fileIdentity(info) != fixture.spec.Identity ||
		changeTime <= 0 || changeTime != media.FileChangeTime(info) {
		t.Fatalf("staged payload does not match its captured snapshot: change time = %d, info = %+v", changeTime, info)
	}
	stage := fileDeletionTestStat(t, fixture.stagePath)
	if !stage.IsDir() || stage.Mode().Perm() != 0o700 || identity != fileIdentity(stage) {
		t.Fatalf("deletion staging directory is not private or lost its recorded identity: mode = %v, identity = %q", stage.Mode(), identity)
	}
	fileDeletionTestMissing(t, fixture.path)
	fileDeletionTestContents(t, payload, fixture.contents)
	spec := fixture.spec
	spec.StageIdentity, spec.StagedChangeTimeNs = identity, changeTime
	return spec
}

func TestFileDeletionStageRestorePreservesTheIndexedFile(t *testing.T) {
	fixture := newFileDeletionTestFixture(t)
	capture := fileDeletionTestPrepare(t, fixture)
	fileDeletionTestStage(t, fixture, capture)
	if err := capture.Restore(context.Background()); err != nil {
		t.Fatalf("restore staged media: %v", err)
	}
	if capture.HasStagedPayload() {
		t.Fatal("completed restoration retained the staged payload state")
	}
	fileDeletionTestContents(t, fixture.path, fixture.contents)
	if restored := fileDeletionTestStat(t, fixture.path); fileIdentity(restored) != fixture.spec.Identity {
		t.Fatalf("restoration replaced the original file identity: got %q, want %q", fileIdentity(restored), fixture.spec.Identity)
	}
	fileDeletionTestMissing(t, filepath.Join(fixture.stagePath, "payload"))
	if err := capture.Close(); err != nil {
		t.Fatalf("close restored capture: %v", err)
	}
	fileDeletionTestContents(t, fixture.path, fixture.contents)
}

func TestFileDeletionCloseRetainsUnstagedSource(t *testing.T) {
	fixture := newFileDeletionTestFixture(t)
	capture := fileDeletionTestPrepare(t, fixture)
	if capture.Staged() {
		t.Fatal("fresh capture reported a staging directory")
	}
	if capture.HasStagedPayload() {
		t.Fatal("fresh capture reported a staged payload")
	}
	if err := capture.Close(); err != nil {
		t.Fatalf("close prepared capture: %v", err)
	}
	fileDeletionTestContents(t, fixture.path, fixture.contents)
	fileDeletionTestMissing(t, fixture.stagePath)
}

func TestFileDeletionReopensStagedPayloadForRecovery(t *testing.T) {
	for _, operation := range []string{"restore", "purge"} {
		t.Run(operation, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			capture := fileDeletionTestPrepare(t, fixture)
			fixture.spec = fileDeletionTestStage(t, fixture, capture)
			if err := capture.Close(); err != nil {
				t.Fatalf("close staged capture: %v", err)
			}
			fileDeletionTestMissing(t, fixture.path)
			fileDeletionTestContents(t, filepath.Join(fixture.stagePath, "payload"), fixture.contents)
			recovered := fileDeletionTestPrepare(t, fixture)
			if !recovered.HasStagedPayload() {
				t.Fatal("recovered capture did not report its existing payload")
			}
			if operation == "restore" {
				if err := recovered.Restore(context.Background()); err != nil {
					t.Fatalf("restore recovered payload: %v", err)
				}
				fileDeletionTestContents(t, fixture.path, fixture.contents)
			} else {
				if err := recovered.Purge(context.Background()); err != nil {
					t.Fatalf("purge recovered payload: %v", err)
				}
				if recovered.HasStagedPayload() {
					t.Fatal("completed purge retained the staged payload state")
				}
				fileDeletionTestMissing(t, fixture.path)
				if err := recovered.Close(); err != nil {
					t.Fatalf("close purged capture: %v", err)
				}
				repeated := fileDeletionTestPrepare(t, fixture)
				if err := repeated.Purge(context.Background()); err != nil {
					t.Fatalf("repeat completed purge during recovery: %v", err)
				}
				if repeated.HasStagedPayload() {
					t.Fatal("empty recovery reported a staged payload")
				}
			}
			fileDeletionTestMissing(t, filepath.Join(fixture.stagePath, "payload"))
			fileDeletionTestContents(t, filepath.Join(fixture.spec.Root.path, "marker.txt"), "original-first")
		})
	}
}

func TestFileDeletionRequiresPersistedStageMetadataForPurge(t *testing.T) {
	fixture := newFileDeletionTestFixture(t)
	capture := fileDeletionTestPrepare(t, fixture)
	fileDeletionTestStage(t, fixture, capture)
	if err := capture.Purge(context.Background()); err == nil {
		t.Fatal("uncommitted capture permanently deleted the staged payload")
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	fileDeletionTestContents(t, filepath.Join(fixture.stagePath, "payload"), fixture.contents)
	// The prepared journal can recover a rename that happened before its staged
	// metadata was persisted, but that state never authorizes permanent deletion.
	recovered := fileDeletionTestPrepare(t, fixture)
	if err := recovered.Purge(context.Background()); err == nil {
		t.Fatal("prepared recovery permanently deleted an uncommitted payload")
	}
	fileDeletionTestContents(t, filepath.Join(fixture.stagePath, "payload"), fixture.contents)
	if err := recovered.Restore(context.Background()); err != nil {
		t.Fatalf("restore prepared crash recovery payload: %v", err)
	}
	fileDeletionTestContents(t, fixture.path, fixture.contents)
}

func TestFileDeletionPreparedRecoveryRejectsMissingSourceAndStage(t *testing.T) {
	fixture := newFileDeletionTestFixture(t)
	if err := os.Remove(fixture.path); err != nil {
		t.Fatal(err)
	}
	capture, err := fixture.store.prepareFileDeletion(context.Background(), fixture.spec)
	if capture != nil {
		defer capture.Close()
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("prepared recovery accepted a missing source and missing stage: %v", err)
	}
}

func TestFileDeletionEmptyStageRecoveryRetainsSourceAndCannotRestage(t *testing.T) {
	for _, interruption := range []string{"before-rename", "after-restore"} {
		t.Run(interruption, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			if interruption == "before-rename" {
				if err := os.Mkdir(fixture.stagePath, 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				capture := fileDeletionTestPrepare(t, fixture)
				fileDeletionTestStage(t, fixture, capture)
				if err := capture.Restore(context.Background()); err != nil {
					t.Fatalf("restore staged source: %v", err)
				}
				if err := capture.Restore(context.Background()); err != nil {
					t.Fatalf("repeat completed restoration: %v", err)
				}
				if err := capture.Close(); err != nil {
					t.Fatal(err)
				}
				// Require recovery to tolerate the ctime difference even on a
				// filesystem that coalesces timestamps for the two renames.
				fixture.spec.ChangeTimeNs = media.FileChangeTime(fileDeletionTestStat(t, fixture.path)) + 1
			}
			capture := fileDeletionTestPrepare(t, fixture)
			if !capture.Staged() {
				t.Fatal("empty private staging directory was not classified as recovery state")
			}
			if capture.HasStagedPayload() {
				t.Fatal("empty private staging directory was classified as holding a payload")
			}
			if _, _, err := capture.Stage(context.Background()); err == nil {
				t.Fatal("recovery reused an existing staging directory for a new deletion")
			}
			if err := capture.Purge(context.Background()); err == nil {
				t.Fatal("empty prepared recovery authorized permanent deletion")
			}
			if err := capture.Restore(context.Background()); err != nil {
				t.Fatalf("complete empty-stage recovery: %v", err)
			}
			fileDeletionTestContents(t, fixture.path, fixture.contents)
			fileDeletionTestMissing(t, filepath.Join(fixture.stagePath, "payload"))
			if !fileDeletionTestStat(t, fixture.stagePath).IsDir() {
				t.Fatal("recovery removed its existing private directory")
			}
		})
	}
}

func TestFileDeletionCommittedRecoveryAcceptsMissingStageWithoutRemovingNewSource(t *testing.T) {
	fixture := newFileDeletionTestFixture(t)
	capture := fileDeletionTestPrepare(t, fixture)
	fixture.spec = fileDeletionTestStage(t, fixture, capture)
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	committed := fileDeletionTestPrepare(t, fixture)
	if err := committed.Purge(context.Background()); err != nil {
		t.Fatalf("complete committed purge: %v", err)
	}
	if err := committed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.stagePath); err != nil {
		t.Fatalf("simulate completed staging-directory cleanup: %v", err)
	}
	fileDeletionTestWrite(t, fixture.path, "later source occupant")
	recovered := fileDeletionTestPrepare(t, fixture)
	if recovered.Staged() {
		t.Fatal("missing private staging directory was reported as present")
	}
	if err := recovered.Purge(context.Background()); err != nil {
		t.Fatalf("repeat purge after staging-directory cleanup: %v", err)
	}
	fileDeletionTestContents(t, fixture.path, "later source occupant")
	fileDeletionTestMissing(t, fixture.stagePath)
}

func TestFileDeletionStageRejectsSourceReplacement(t *testing.T) {
	for _, replacement := range []string{"symlink", "directory", "regular-file"} {
		t.Run(replacement, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			capture := fileDeletionTestPrepare(t, fixture)
			saved := fixture.path + ".saved"
			if err := os.Rename(fixture.path, saved); err != nil {
				t.Fatal(err)
			}
			var protected string
			switch replacement {
			case "symlink":
				protected = filepath.Join(t.TempDir(), "outside.mkv")
				fileDeletionTestWrite(t, protected, "outside media")
				if err := os.Symlink(protected, fixture.path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				protected = filepath.Join(fixture.path, "child.mkv")
				fileDeletionTestWrite(t, protected, "replacement child")
			case "regular-file":
				protected = fixture.path
				fileDeletionTestWrite(t, protected, "replacement media")
			}
			before := fileDeletionTestStat(t, fixture.path)
			protectedBefore := fileDeletionTestStat(t, protected)
			if _, _, err := capture.Stage(context.Background()); err == nil {
				t.Fatal("staging accepted a replacement source")
			}
			if capture.HasStagedPayload() {
				t.Fatal("source validation failure was classified as a rename attempt")
			}
			after := fileDeletionTestStat(t, fixture.path)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("rejected source replacement changed: before = %v, after = %v", before, after)
			}
			if after := fileDeletionTestStat(t, protected); !os.SameFile(protectedBefore, after) || after.Size() != protectedBefore.Size() {
				t.Fatal("staging changed the replacement source or its target")
			}
			fileDeletionTestContents(t, saved, fixture.contents)
		})
	}
}

func TestFileDeletionRejectsReplacedRootAndParent(t *testing.T) {
	for _, component := range []string{"approved-root", "root", "parent"} {
		for _, operation := range []string{"stage", "restore", "purge"} {
			t.Run(component+"/"+operation, func(t *testing.T) {
				fixture := newFileDeletionTestFixture(t)
				capture := fileDeletionTestPrepare(t, fixture)
				if operation != "stage" {
					fixture.spec = fileDeletionTestStage(t, fixture, capture)
					if operation == "purge" {
						if err := capture.Close(); err != nil {
							t.Fatal(err)
						}
						capture = fileDeletionTestPrepare(t, fixture)
					}
				}
				directory := filepath.Dir(fixture.path)
				if component == "root" {
					directory = fixture.spec.Root.path
				} else if component == "approved-root" {
					directory = fixture.spec.Root.allowedPath
				}
				saved := directory + ".saved"
				if err := os.Rename(directory, saved); err != nil {
					t.Fatal(err)
				}
				fileDeletionTestWrite(t, fixture.path, "new directory occupant")
				var err error
				switch operation {
				case "stage":
					_, _, err = capture.Stage(context.Background())
				case "restore":
					err = capture.Restore(context.Background())
				case "purge":
					err = capture.Purge(context.Background())
				}
				if err == nil {
					t.Fatalf("%s accepted a replacement %s directory", operation, component)
				}
				fileDeletionTestContents(t, fixture.path, "new directory occupant")
				original := fixture.path
				if operation != "stage" {
					original = filepath.Join(fixture.stagePath, "payload")
				}
				relative, err := filepath.Rel(directory, original)
				if err != nil {
					t.Fatal(err)
				}
				fileDeletionTestContents(t, filepath.Join(saved, relative), fixture.contents)
			})
		}
	}
}

func TestFileDeletionRestoreDoesNotOverwriteNewSource(t *testing.T) {
	for _, replacement := range []string{"regular-file", "directory", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			capture := fileDeletionTestPrepare(t, fixture)
			fileDeletionTestStage(t, fixture, capture)
			protected := fixture.path
			switch replacement {
			case "regular-file":
				fileDeletionTestWrite(t, fixture.path, "new source")
			case "directory":
				protected = filepath.Join(fixture.path, "child.mkv")
				fileDeletionTestWrite(t, protected, "new source")
			case "symlink":
				protected = filepath.Join(t.TempDir(), "outside.mkv")
				fileDeletionTestWrite(t, protected, "new source")
				if err := os.Symlink(protected, fixture.path); err != nil {
					t.Fatal(err)
				}
			}
			before := fileDeletionTestStat(t, fixture.path)
			if err := capture.Restore(context.Background()); err == nil {
				t.Fatal("restoration overwrote a new source path occupant")
			}
			if after := fileDeletionTestStat(t, fixture.path); !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("failed restoration replaced the conflicting source")
			}
			fileDeletionTestContents(t, protected, "new source")
			fileDeletionTestContents(t, filepath.Join(fixture.stagePath, "payload"), fixture.contents)
		})
	}
}

func TestFileDeletionStageDoesNotOverwriteExistingStagePath(t *testing.T) {
	for _, replacement := range []string{"directory", "regular-file", "symlink"} {
		t.Run(replacement, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			capture := fileDeletionTestPrepare(t, fixture)
			protected := fixture.stagePath
			switch replacement {
			case "directory":
				protected = filepath.Join(fixture.stagePath, "payload")
				fileDeletionTestWrite(t, protected, "existing staged occupant")
			case "regular-file":
				fileDeletionTestWrite(t, fixture.stagePath, "existing staged occupant")
			case "symlink":
				outside := t.TempDir()
				protected = filepath.Join(outside, "payload")
				fileDeletionTestWrite(t, protected, "existing staged occupant")
				if err := os.Symlink(outside, fixture.stagePath); err != nil {
					t.Fatal(err)
				}
			}
			before := fileDeletionTestStat(t, fixture.stagePath)
			if _, _, err := capture.Stage(context.Background()); err == nil {
				t.Fatal("staging accepted an occupied private directory path")
			}
			if after := fileDeletionTestStat(t, fixture.stagePath); !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("failed staging replaced the existing stage path")
			}
			fileDeletionTestContents(t, fixture.path, fixture.contents)
			fileDeletionTestContents(t, protected, "existing staged occupant")
		})
	}
}

func TestFileDeletionRecoveryRejectsWrongStagedIdentityAndChangeTime(t *testing.T) {
	for _, mismatch := range []string{"identity", "change-time"} {
		t.Run(mismatch, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			capture := fileDeletionTestPrepare(t, fixture)
			fixture.spec = fileDeletionTestStage(t, fixture, capture)
			if err := capture.Close(); err != nil {
				t.Fatal(err)
			}
			if mismatch == "identity" {
				other := filepath.Join(fixture.spec.Root.path, "other.mkv")
				fileDeletionTestWrite(t, other, fixture.contents)
				fixture.spec.StageIdentity = fileIdentity(fileDeletionTestStat(t, other))
			} else {
				fixture.spec.StagedChangeTimeNs++
			}
			recovered, err := fixture.store.prepareFileDeletion(context.Background(), fixture.spec)
			if recovered != nil {
				defer recovered.Close()
			}
			if err == nil {
				if recovered == nil {
					t.Fatal("recovery returned neither a capture nor an error")
				}
				if err := recovered.Purge(context.Background()); err == nil {
					t.Fatal("purge accepted incorrect persisted staged metadata")
				}
			}
			fileDeletionTestMissing(t, fixture.path)
			fileDeletionTestContents(t, filepath.Join(fixture.stagePath, "payload"), fixture.contents)
		})
	}
}

func TestFileDeletionPurgeRejectsChangedPrivateStagingNamespace(t *testing.T) {
	for _, replacement := range []string{"directory-permissions", "payload-inode"} {
		t.Run(replacement, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			capture := fileDeletionTestPrepare(t, fixture)
			fixture.spec = fileDeletionTestStage(t, fixture, capture)
			if err := capture.Close(); err != nil {
				t.Fatal(err)
			}
			committed := fileDeletionTestPrepare(t, fixture)
			payload := filepath.Join(fixture.stagePath, "payload")
			if replacement == "directory-permissions" {
				if err := os.Chmod(fixture.stagePath, 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(payload, payload+".saved"); err != nil {
					t.Fatal(err)
				}
				fileDeletionTestWrite(t, payload, fixture.contents)
				if err := os.Chtimes(payload, fixture.spec.ModifiedAt, fixture.spec.ModifiedAt); err != nil {
					t.Fatal(err)
				}
			}
			before := fileDeletionTestStat(t, payload)
			if err := committed.Purge(context.Background()); err == nil {
				t.Fatal("purge accepted a changed private staging namespace")
			}
			if after := fileDeletionTestStat(t, payload); !os.SameFile(before, after) {
				t.Fatal("failed purge removed or replaced the staged occupant")
			}
			fileDeletionTestContents(t, payload, fixture.contents)
			if replacement == "directory-permissions" {
				if permissions := fileDeletionTestStat(t, fixture.stagePath).Mode().Perm(); permissions != 0o755 {
					t.Fatalf("failed purge changed the staging permissions: got %v", permissions)
				}
			} else {
				fileDeletionTestContents(t, payload+".saved", fixture.contents)
			}
		})
	}
}

func TestDeletionStageIsEmptyDoesNotDependOnTheCurrentSource(t *testing.T) {
	for _, stageState := range []string{"missing", "empty", "registered-empty", "registered-missing"} {
		for _, sourceState := range []string{"replacement", "missing"} {
			t.Run(stageState+"/"+sourceState, func(t *testing.T) {
				fixture := newFileDeletionTestFixture(t)
				if stageState != "missing" {
					if err := os.Mkdir(fixture.stagePath, 0o700); err != nil {
						t.Fatal(err)
					}
					if stageState == "registered-empty" || stageState == "registered-missing" {
						fixture.spec.StageIdentity = fileIdentity(fileDeletionTestStat(t, fixture.stagePath))
						fixture.spec.StagedChangeTimeNs = fixture.spec.ChangeTimeNs
					}
					if stageState == "registered-missing" {
						if err := os.Remove(fixture.stagePath); err != nil {
							t.Fatal(err)
						}
					}
				}
				saved := fixture.path + ".saved"
				if err := os.Rename(fixture.path, saved); err != nil {
					t.Fatal(err)
				}
				var replacement os.FileInfo
				if sourceState == "replacement" {
					fileDeletionTestWrite(t, fixture.path, "later source occupant")
					replacement = fileDeletionTestStat(t, fixture.path)
				}
				empty, err := fixture.store.deletionStageIsEmpty(context.Background(), fixture.spec)
				if err != nil || !empty {
					t.Fatalf("observe unused stage with %s source: empty = %t, error = %v", sourceState, empty, err)
				}
				if sourceState == "replacement" {
					if after := fileDeletionTestStat(t, fixture.path); !os.SameFile(replacement, after) {
						t.Fatal("stage observation replaced the current source")
					}
					fileDeletionTestContents(t, fixture.path, "later source occupant")
				} else {
					fileDeletionTestMissing(t, fixture.path)
				}
				fileDeletionTestContents(t, saved, fixture.contents)
				if stageState == "empty" || stageState == "registered-empty" {
					if !fileDeletionTestStat(t, fixture.stagePath).IsDir() {
						t.Fatal("stage observation removed its existing directory")
					}
				} else {
					fileDeletionTestMissing(t, fixture.stagePath)
				}
			})
		}
	}
}

func TestDeletionStageIsEmptyRetainsEveryKindOfDirectoryEntry(t *testing.T) {
	for _, entryType := range []string{"payload", "other-file", "directory", "symlink"} {
		t.Run(entryType, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			if err := os.Mkdir(fixture.stagePath, 0o700); err != nil {
				t.Fatal(err)
			}
			entry := filepath.Join(fixture.stagePath, "unexpected")
			switch entryType {
			case "payload":
				entry = filepath.Join(fixture.stagePath, "payload")
				fileDeletionTestWrite(t, entry, "unrecognized payload")
			case "other-file":
				fileDeletionTestWrite(t, entry, "unrecognized entry")
			case "directory":
				if err := os.Mkdir(entry, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing-target"), entry); err != nil {
					t.Fatal(err)
				}
			}
			before := fileDeletionTestStat(t, entry)
			empty, err := fixture.store.deletionStageIsEmpty(context.Background(), fixture.spec)
			if err != nil || empty {
				t.Fatalf("directory entry was not retained as recovery evidence: empty = %t, error = %v", empty, err)
			}
			if after := fileDeletionTestStat(t, entry); !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("stage observation changed an existing directory entry")
			}
			fileDeletionTestContents(t, fixture.path, fixture.contents)
		})
	}
}

func TestDeletionStageIsEmptyRejectsUntrustedStagePaths(t *testing.T) {
	for _, stageType := range []string{"symlink", "public-directory", "foreign-identity", "regular-file"} {
		t.Run(stageType, func(t *testing.T) {
			fixture := newFileDeletionTestFixture(t)
			switch stageType {
			case "symlink":
				if err := os.Symlink(t.TempDir(), fixture.stagePath); err != nil {
					t.Fatal(err)
				}
			case "public-directory":
				if err := os.Mkdir(fixture.stagePath, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(fixture.stagePath, 0o755); err != nil {
					t.Fatal(err)
				}
			case "foreign-identity":
				if err := os.Mkdir(fixture.stagePath, 0o700); err != nil {
					t.Fatal(err)
				}
				fixture.spec.StageIdentity = fileIdentity(fileDeletionTestStat(t, t.TempDir()))
				fixture.spec.StagedChangeTimeNs = fixture.spec.ChangeTimeNs
			case "regular-file":
				fileDeletionTestWrite(t, fixture.stagePath, "existing stage-name occupant")
			}
			before := fileDeletionTestStat(t, fixture.stagePath)
			empty, err := fixture.store.deletionStageIsEmpty(context.Background(), fixture.spec)
			if empty || err == nil {
				t.Fatalf("untrusted stage path was accepted: empty = %t, error = %v", empty, err)
			}
			if after := fileDeletionTestStat(t, fixture.stagePath); !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("failed stage observation changed the untrusted pathname")
			}
			fileDeletionTestContents(t, fixture.path, fixture.contents)
		})
	}
}
