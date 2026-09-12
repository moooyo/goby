//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
)

func scanEvidenceTestOpen(t *testing.T, directory string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root
}

func scanEvidenceTestFiles(t *testing.T, directory string, files map[string]string) {
	t.Helper()
	for relative, contents := range files {
		filename := filepath.Join(directory, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func scanEvidenceTestAttach(t *testing.T, evidence *scanReconciliationEvidence, rootID, directory string) *os.Root {
	t.Helper()
	root := scanEvidenceTestOpen(t, directory)
	if err := evidence.AttachRoot(rootID, root); err != nil {
		t.Fatal(err)
	}
	return root
}

func scanEvidenceTestRecord(t *testing.T, evidence *scanReconciliationEvidence, rootID, relative string, root *os.Root) {
	t.Helper()
	directory, err := openScanFile(root, filepath.FromSlash(relative))
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	before, err := directory.Stat()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.RecordDirectory(rootID, relative, before, entries); err != nil {
		t.Fatalf("record directory %q: %v", relative, err)
	}
}

func scanEvidenceTestComplete(t *testing.T, evidence *scanReconciliationEvidence, rootID string, directories ...string) {
	t.Helper()
	for _, relative := range directories {
		if err := evidence.CompleteDirectory(rootID, relative); err != nil {
			t.Fatalf("complete directory %q: %v", relative, err)
		}
	}
}

func scanEvidenceTestCollector(t *testing.T) *scanReconciliationEvidence {
	t.Helper()
	evidence := newScanReconciliationEvidence()
	t.Cleanup(func() { _ = evidence.Close() })
	return evidence
}

func scanEvidenceTestUnavailable(t *testing.T, evidence *scanReconciliationEvidence, err error) {
	t.Helper()
	if !errors.Is(err, errScanReconciliationEvidenceUnavailable) || evidence.Err() == nil || !evidence.closed {
		t.Fatalf("uncertain evidence must disable the whole pass: %v", err)
	}
	if evidence.roots != nil || evidence.seen != nil {
		t.Fatal("disabled evidence retained its root or item maps")
	}
}

func TestScanReconciliationEvidenceStableCompleteMembershipAndAbsence(t *testing.T) {
	directory := t.TempDir()
	scanEvidenceTestFiles(t, directory, map[string]string{
		"visible/movie.mp4": "video", ".ignored": "hidden", "theme.mp3": "audio",
		"poster.jpg": "image", "movie.nfo": "metadata", "unknown.bin": "unknown",
	})
	if err := os.Mkdir(filepath.Join(directory, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	evidence := scanEvidenceTestCollector(t)
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestRecord(t, evidence, "root", "visible", root)
	scanEvidenceTestRecord(t, evidence, "root", "empty", root)
	scanEvidenceTestComplete(t, evidence, "root", "visible", "empty", ".")
	if err := evidence.MarkSeen("accepted-item"); err != nil || !evidence.Seen("accepted-item") {
		t.Fatalf("accepted item was not retained: %v", err)
	}
	if err := evidence.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"visible/gone.mp4", "gone/path/movie.mp4", "empty/subdir/movie.mp4"} {
		absent, err := evidence.PathAbsent(context.Background(), "root", relative)
		if err != nil || !absent {
			t.Fatalf("missing path %q did not obtain positive evidence: %v, %v", relative, absent, err)
		}
	}
	for _, relative := range []string{"visible/movie.mp4", ".ignored", "theme.mp3", "poster.jpg", "movie.nfo", "unknown.bin", "empty"} {
		absent, err := evidence.PathAbsent(context.Background(), "root", relative)
		if err != nil || absent {
			t.Fatalf("existing raw member %q was treated as absent: %v, %v", relative, absent, err)
		}
	}
	if len(evidence.roots["root"].directories["."].entries) != 7 {
		t.Fatal("pre-classification membership was not retained completely")
	}
}

func TestScanReconciliationEvidenceOwnedClonesOutliveBorrowedWalkRoot(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	evidence := scanEvidenceTestCollector(t)
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestRecord(t, evidence, "root", "child", root)
	scanEvidenceTestComplete(t, evidence, "root", "child", ".")
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Revalidate(context.Background()); err != nil {
		t.Fatalf("closing a borrowed walk root invalidated independent witnesses: %v", err)
	}
	absent, err := evidence.PathAbsent(context.Background(), "root", "child/missing.mp4")
	if err != nil || !absent {
		t.Fatalf("retained child no longer proves its stable missing member: %v, %v", absent, err)
	}
}

func TestScanReconciliationEvidenceRequiresEveryWalkToComplete(t *testing.T) {
	for _, scenario := range []string{"unrecorded root", "incomplete root", "incomplete child", "incomplete other root"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Mkdir(filepath.Join(directory, "child"), 0o700); err != nil {
				t.Fatal(err)
			}
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			if scenario != "unrecorded root" {
				scanEvidenceTestRecord(t, evidence, "root", ".", root)
			}
			if scenario == "incomplete child" {
				scanEvidenceTestRecord(t, evidence, "root", "child", root)
				scanEvidenceTestComplete(t, evidence, "root", ".")
			}
			if scenario == "incomplete other root" {
				scanEvidenceTestComplete(t, evidence, "root", ".")
				scanEvidenceTestAttach(t, evidence, "other", t.TempDir())
			}
			absent, err := evidence.PathAbsent(context.Background(), "root", "missing.mp4")
			if absent {
				t.Fatal("an incomplete library pass proved absence")
			}
			scanEvidenceTestUnavailable(t, evidence, err)
		})
	}
}

func TestScanReconciliationEvidenceRejectsUnobservedAndNonDirectoryAncestors(t *testing.T) {
	for _, relative := range []string{"unobserved/missing.mp4", "regular/missing.mp4", "link/missing.mp4"} {
		t.Run(relative, func(t *testing.T) {
			directory, outside := t.TempDir(), t.TempDir()
			scanEvidenceTestFiles(t, directory, map[string]string{"regular": "contents"})
			if err := os.Mkdir(filepath.Join(directory, "unobserved"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(directory, "link")); err != nil {
				t.Fatal(err)
			}
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			if absent, err := evidence.PathAbsent(context.Background(), "root", "link"); err != nil || absent {
				t.Fatalf("a listed symlink must remain present: %v, %v", absent, err)
			}
			absent, err := evidence.PathAbsent(context.Background(), "root", relative)
			if absent {
				t.Fatal("an unproved ancestor authorized absence")
			}
			scanEvidenceTestUnavailable(t, evidence, err)
		})
	}
}

func TestScanReconciliationEvidenceKeepsSpecialRawMembers(t *testing.T) {
	directory := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(directory, "unopened.pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nonexistent-target", filepath.Join(directory, "broken-link")); err != nil {
		t.Fatal(err)
	}
	evidence := scanEvidenceTestCollector(t)
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	if err := evidence.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"unopened.pipe", "broken-link"} {
		absent, err := evidence.PathAbsent(context.Background(), "root", relative)
		if err != nil || absent {
			t.Fatalf("special raw member %q was followed or treated as absent: %v, %v", relative, absent, err)
		}
	}
}

func TestScanReconciliationEvidencePreviouslyPresentEntriesCannotBecomeAbsence(t *testing.T) {
	for _, relative := range []string{"movie.mp4", ".ignored", "theme.mp3", "child"} {
		t.Run(relative, func(t *testing.T) {
			directory := t.TempDir()
			scanEvidenceTestFiles(t, directory, map[string]string{"movie.mp4": "video", ".ignored": "hidden", "theme.mp3": "theme"})
			if err := os.Mkdir(filepath.Join(directory, "child"), 0o700); err != nil {
				t.Fatal(err)
			}
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			if err := os.Remove(filepath.Join(directory, relative)); err != nil {
				t.Fatal(err)
			}
			absent, err := evidence.PathAbsent(context.Background(), "root", relative)
			if absent {
				t.Fatal("a previously listed member became deletion evidence")
			}
			scanEvidenceTestUnavailable(t, evidence, err)
		})
	}
}

func TestScanReconciliationEvidenceRevalidatesNamedDirectoryChain(t *testing.T) {
	for _, scenario := range []string{"renamed", "replaced", "symlink", "restored", "permissions"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			child := filepath.Join(directory, "child")
			if err := os.Mkdir(child, 0o700); err != nil {
				t.Fatal(err)
			}
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestRecord(t, evidence, "root", "child", root)
			scanEvidenceTestComplete(t, evidence, "root", "child", ".")
			if scenario == "restored" {
				// Separate the mutations from capture across the kernel's coarse
				// timestamp tick; an identical final stat cannot reveal history.
				time.Sleep(20 * time.Millisecond)
			}
			if scenario == "permissions" {
				if err := os.Chmod(child, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(child, 0o700) })
			} else {
				saved := filepath.Join(directory, "saved")
				if err := os.Rename(child, saved); err != nil {
					t.Fatal(err)
				}
				switch scenario {
				case "replaced":
					if err := os.Mkdir(child, 0o700); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(saved, child); err != nil {
						t.Fatal(err)
					}
				case "restored":
					if err := os.Rename(saved, child); err != nil {
						t.Fatal(err)
					}
				}
			}
			absent, err := evidence.PathAbsent(context.Background(), "root", "child/missing.mp4")
			if absent {
				t.Fatal("a detached or changed parent proved absence at its former name")
			}
			scanEvidenceTestUnavailable(t, evidence, err)
		})
	}
}

func TestScanReconciliationEvidenceDetectsEntryRewriteWithRestoredMtime(t *testing.T) {
	for _, filename := range []string{"movie.mp4", ".ignored", "movie.nfo", "theme.mp3"} {
		t.Run(filename, func(t *testing.T) {
			directory := t.TempDir()
			scanEvidenceTestFiles(t, directory, map[string]string{filename: "first"})
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			before := evidence.roots["root"].directories["."].entries[filename].info
			path := filepath.Join(directory, filename)
			// This fixture requires an observable ctime change, independently
			// of whether fast writes share the same kernel timestamp tick.
			time.Sleep(20 * time.Millisecond)
			if err := os.WriteFile(path, []byte("later"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
				t.Fatal(err)
			}
			after, err := os.Lstat(path)
			if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) ||
				media.FileChangeTime(after) == media.FileChangeTime(before) {
				t.Fatalf("fixture did not retain size/mtime while changing ctime: before=%+v after=%+v error=%v", before, after, err)
			}
			scanEvidenceTestUnavailable(t, evidence, evidence.Revalidate(context.Background()))
		})
	}
}

type scanEvidenceNamedMutation struct {
	os.DirEntry
	reads  int
	mutate func()
}

func (entry *scanEvidenceNamedMutation) Name() string {
	entry.reads++
	if entry.reads == 3 {
		entry.mutate()
	}
	return entry.DirEntry.Name()
}

func TestScanReconciliationEvidenceRequiresCompleteStableInitialListing(t *testing.T) {
	for _, scenario := range []string{"missing raw member", "duplicate raw member", "stale walk information", "changed while collecting"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			scanEvidenceTestFiles(t, directory, map[string]string{"movie.mp4": "video", "movie.nfo": "metadata"})
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			walked, err := openScanFile(root, ".")
			if err != nil {
				t.Fatal(err)
			}
			defer walked.Close()
			before, err := walked.Stat()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := walked.ReadDir(-1)
			if err != nil {
				t.Fatal(err)
			}
			mutate := func() {
				if err := os.WriteFile(filepath.Join(directory, "new.nfo"), []byte("new"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "missing raw member":
				raw = raw[:1]
			case "duplicate raw member":
				raw = append(raw, raw[0])
			case "stale walk information":
				mutate()
			case "changed while collecting":
				raw[0] = &scanEvidenceNamedMutation{DirEntry: raw[0], mutate: mutate}
			}
			scanEvidenceTestUnavailable(t, evidence, evidence.RecordDirectory("root", ".", before, raw))
		})
	}
}

type scanEvidenceMutationContext struct {
	context.Context
	calls  int
	at     int
	mutate func()
}

func (ctx *scanEvidenceMutationContext) Err() error {
	ctx.calls++
	if ctx.calls == ctx.at {
		ctx.mutate()
	}
	return ctx.Context.Err()
}

func TestScanReconciliationEvidenceDetectsChangesDuringFinalRead(t *testing.T) {
	for _, scenario := range []string{"membership before read", "entry before read", "entry after read"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			scanEvidenceTestFiles(t, directory, map[string]string{"movie.mp4": "video"})
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			at := 4
			if scenario == "entry after read" {
				at = 5
			}
			ctx := &scanEvidenceMutationContext{Context: context.Background(), at: at, mutate: func() {
				name := "new.nfo"
				if scenario != "membership before read" {
					name = "movie.mp4"
				}
				if err := os.WriteFile(filepath.Join(directory, name), []byte("replacement"), 0o600); err != nil {
					t.Fatal(err)
				}
			}}
			scanEvidenceTestUnavailable(t, evidence, evidence.Revalidate(ctx))
			if ctx.calls < ctx.at {
				t.Fatal("fixture did not reach the final directory-read boundary")
			}
		})
	}
}

func TestScanReconciliationEvidenceCancellationAndUnavailableHandles(t *testing.T) {
	for _, scenario := range []string{"cancelled", "nil context", "closed witness", "closed anchor"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			var ctx context.Context = context.Background()
			switch scenario {
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "nil context":
				ctx = nil
			case "closed witness":
				_ = evidence.roots["root"].directories["."].held.Close()
			case "closed anchor":
				_ = evidence.roots["root"].anchor.Close()
			}
			err := evidence.Revalidate(ctx)
			scanEvidenceTestUnavailable(t, evidence, err)
			if scenario == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("the original cancellation was not preserved")
			}
			if _, err := root.Stat("."); err != nil {
				t.Fatalf("disabling evidence closed the caller's root: %v", err)
			}
		})
	}
}

func TestScanReconciliationEvidenceBudgetsAreSharedAcrossRoots(t *testing.T) {
	for _, scenario := range []string{"directories", "entries", "seen IDs", "bytes"} {
		t.Run(scenario, func(t *testing.T) {
			limits := scanReconciliationEvidenceLimits{directories: 8, entries: 8, seenIDs: 8, bytes: 1 << 20}
			switch scenario {
			case "directories":
				limits.directories = 2
			case "entries":
				limits.entries = 1
			case "seen IDs":
				limits.seenIDs = 1
			case "bytes":
				limits.bytes = scanReconciliationScratchBytes + 2*scanReconciliationDirectoryBytes + 5 +
					scanReconciliationEntryBytes + 2*int64(len("movie.mp4")) + scanReconciliationSeenIDBytes + 4
			}
			evidence := newScanReconciliationEvidenceWithLimits(limits)
			t.Cleanup(func() { _ = evidence.Close() })
			first, second := t.TempDir(), t.TempDir()
			scanEvidenceTestFiles(t, first, map[string]string{"movie.mp4": "video"})
			scanEvidenceTestFiles(t, second, map[string]string{"other.mp4": "other"})
			root := scanEvidenceTestAttach(t, evidence, "root", first)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			if err := evidence.MarkSeen("item"); err != nil {
				t.Fatal(err)
			}
			anchor := evidence.roots["root"].anchor
			held := evidence.roots["root"].directories["."].held
			var err error
			switch scenario {
			case "directories":
				err = evidence.AttachRoot("other", scanEvidenceTestOpen(t, second))
			case "entries":
				other := scanEvidenceTestAttach(t, evidence, "other", second)
				directory, openErr := openScanFile(other, ".")
				if openErr != nil {
					t.Fatal(openErr)
				}
				defer directory.Close()
				before, statErr := directory.Stat()
				raw, readErr := directory.ReadDir(-1)
				if statErr != nil || readErr != nil {
					t.Fatalf("read second root: %v, %v", statErr, readErr)
				}
				err = evidence.RecordDirectory("other", ".", before, raw)
			default:
				if scenario == "bytes" && evidence.bytes != limits.bytes {
					t.Fatalf("fixture did not exactly fill the byte budget: %d, want %d", evidence.bytes, limits.bytes)
				}
				err = evidence.MarkSeen("next")
			}
			if !errors.Is(err, errScanReconciliationEvidenceBudget) || !evidence.closed {
				t.Fatalf("overflow did not disable the whole shared pass: %v", err)
			}
			for _, owned := range []*os.Root{anchor, held} {
				if _, err := owned.Stat("."); err == nil {
					t.Fatal("budget overflow left an owned witness open")
				}
			}
			if _, err := root.Stat("."); err != nil {
				t.Fatalf("budget overflow closed the borrowed root: %v", err)
			}
		})
	}
}

func TestScanReconciliationEvidenceCannotReplaceAnEarlierObservation(t *testing.T) {
	for _, scenario := range []string{"root", "directory", "unknown completion"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			evidence := scanEvidenceTestCollector(t)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			var err error
			switch scenario {
			case "root":
				err = evidence.AttachRoot("root", root)
			case "directory":
				info, statErr := root.Stat(".")
				if statErr != nil {
					t.Fatal(statErr)
				}
				err = evidence.RecordDirectory("root", ".", info, nil)
			case "unknown completion":
				err = evidence.CompleteDirectory("root", "unobserved")
			}
			scanEvidenceTestUnavailable(t, evidence, err)
		})
	}
}

type scanEvidenceUnknownFileInfo struct {
	os.FileInfo
}

func (scanEvidenceUnknownFileInfo) Sys() any { return nil }

func TestScanReconciliationEvidenceRejectsUnavailableChangeTime(t *testing.T) {
	directory := t.TempDir()
	evidence := scanEvidenceTestCollector(t)
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	info, err := root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	unknown := scanEvidenceUnknownFileInfo{FileInfo: info}
	if scanReconciliationInfo(unknown) || scanReconciliationDirectoryInfo(unknown) || scanReconciliationSameInfo(info, unknown) {
		t.Fatal("unknown identity or zero ctime became stable directory evidence")
	}
	scanEvidenceTestUnavailable(t, evidence, evidence.RecordDirectory("root", ".", unknown, nil))
}
