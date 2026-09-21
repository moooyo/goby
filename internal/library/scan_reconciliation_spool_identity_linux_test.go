//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"golang.org/x/sys/unix"
)

func TestScanReconciliationSpoolRejectsChangedHandleWithIdenticalStat(t *testing.T) {
	mediaDirectory := t.TempDir()
	scanEvidenceTestFiles(t, mediaDirectory, map[string]string{"child/movie.mp4": "movie"})
	identity := scanSpoolIdentity{kind: 1, mountID: 17, handleType: 1, bytes: "original-generation"}
	evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{
		Directory:         t.TempDir(),
		directoryIdentity: func(*os.File) (scanSpoolIdentity, bool, error) { return identity, true, nil },
	})
	t.Cleanup(func() { _ = evidence.Close() })
	root := scanEvidenceTestAttach(t, evidence, "root", mediaDirectory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestRecord(t, evidence, "root", "child", root)
	scanEvidenceTestComplete(t, evidence, "root", "child", ".")
	before, err := root.Stat("child")
	if err != nil {
		t.Fatal(err)
	}
	identity.bytes = "replacement-generation"
	after, err := root.Stat("child")
	if err != nil || !scanReconciliationSameInfo(before, after) {
		t.Fatal("the controlled identity change modified the stat tuple")
	}
	absent, err := evidence.PathAbsent(context.Background(), "root", "child/missing.mp4")
	if absent || !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("a different generation with an identical stat tuple authorized absence: absent=%t err=%v", absent, err)
	}
}

func TestScanReconciliationSpoolUnsupportedHandlesRetainBoundedOriginals(t *testing.T) {
	mediaDirectory := t.TempDir()
	scanEvidenceTestFiles(t, mediaDirectory, map[string]string{"a/movie.mp4": "a", "b/movie.mp4": "b"})
	evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{
		Directory: t.TempDir(), MaxFallbackHandles: 1,
		directoryIdentity: func(*os.File) (scanSpoolIdentity, bool, error) { return scanSpoolIdentity{}, false, nil },
	})
	t.Cleanup(func() { _ = evidence.Close() })
	root := scanEvidenceTestAttach(t, evidence, "root", mediaDirectory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestRecord(t, evidence, "root", "a", root)
	if evidence.SpoolStats().FallbackHandles != 1 || len(evidence.spool.fallbacks) != 1 {
		t.Fatal("the unsupported filesystem did not retain the original directory")
	}
	held := evidence.spool.fallbacks[0]
	directory, err := openScanFile(root, "b")
	if err != nil {
		t.Fatal(err)
	}
	before, err := directory.Stat()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := evidence.RecordDirectory("root", "b", before, entries); !errors.Is(err, errScanReconciliationEvidenceBudget) {
		t.Fatalf("unsupported directory overflow did not disable all deletion: %v", err)
	}
	if _, err := held.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("the fallback descriptor outlived terminal cleanup: %v", err)
	}
	if done, receipt := evidence.CleanupStatus(); !done || receipt.Err != nil {
		t.Fatalf("fallback overflow cleanup did not finish: done=%t receipt=%+v", done, receipt)
	}
}

func TestScanReconciliationSpoolFallbackRejectsReplacedAndRestoredChains(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "replaced", true: "restored"}[restore], func(t *testing.T) {
			mediaDirectory := t.TempDir()
			scanEvidenceTestFiles(t, mediaDirectory, map[string]string{"ancestor/child/movie.mp4": "movie"})
			evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{
				Directory:         t.TempDir(),
				directoryIdentity: func(*os.File) (scanSpoolIdentity, bool, error) { return scanSpoolIdentity{}, false, nil },
			})
			t.Cleanup(func() { _ = evidence.Close() })
			root := scanEvidenceTestAttach(t, evidence, "root", mediaDirectory)
			for _, relative := range []string{".", "ancestor", "ancestor/child"} {
				scanEvidenceTestRecord(t, evidence, "root", relative, root)
			}
			scanEvidenceTestComplete(t, evidence, "root", "ancestor/child", "ancestor", ".")
			original := filepath.Join(mediaDirectory, "ancestor")
			parked := filepath.Join(mediaDirectory, "parked")
			if err := os.Rename(original, parked); err != nil {
				t.Fatal(err)
			}
			if restore {
				if err := os.Rename(parked, original); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(original, "child"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			absent, err := evidence.PathAbsent(context.Background(), "root", "ancestor/child/missing.mp4")
			if absent || !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
				t.Fatalf("a renamed chain authorized absence: absent=%t err=%v", absent, err)
			}
		})
	}
}

func TestScanReconciliationSpoolNativeHandleComesFromHeldDescriptor(t *testing.T) {
	directory := t.TempDir()
	file, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	identity, supported, err := scanSpoolDirectoryIdentity(file)
	if err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Skip("the filesystem requires the bounded descriptor fallback")
	}
	handle, mountID, err := unix.NameToHandleAt(int(file.Fd()), "", unix.AT_EMPTY_PATH)
	if err != nil {
		t.Fatal(err)
	}
	if identity.kind != 1 || identity.mountID != int32(mountID) || identity.handleType != handle.Type() || identity.bytes != string(handle.Bytes()) {
		t.Fatal("the stored exportable handle differs from the held descriptor")
	}
}

func TestScanReconciliationSpoolPreviouslyListedNameNeverProvesAbsence(t *testing.T) {
	directory := t.TempDir()
	scanEvidenceTestFiles(t, directory, map[string]string{"movie.mp4": "video"})
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	if err := os.Remove(filepath.Join(directory, "movie.mp4")); err != nil {
		t.Fatal(err)
	}
	if absent, err := evidence.PathAbsent(context.Background(), "root", "movie.mp4"); absent || !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("a previously listed disappearing file authorized deletion: absent=%t err=%v", absent, err)
	}
}

func TestScanReconciliationSpoolDetectsRestoredMtimeAndFinalReadRewrite(t *testing.T) {
	for _, duringRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "before proof", true: "after first member pass"}[duringRead], func(t *testing.T) {
			directory := t.TempDir()
			filename := filepath.Join(directory, "movie.nfo")
			scanEvidenceTestFiles(t, directory, map[string]string{"movie.nfo": "first"})
			evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			before, err := os.Lstat(filename)
			if err != nil {
				t.Fatal(err)
			}
			// Force an observable ctime change even on a coarse timestamp tick.
			time.Sleep(20 * time.Millisecond)
			mutate := func() {
				if err := os.WriteFile(filename, []byte("later"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(filename, before.ModTime(), before.ModTime()); err != nil {
					t.Fatal(err)
				}
				after, err := os.Lstat(filename)
				if err != nil || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || media.FileChangeTime(before) == media.FileChangeTime(after) {
					t.Fatalf("fixture did not isolate a ctime rewrite: before=%v after=%v error=%v", before, after, err)
				}
			}
			var ctx context.Context = context.Background()
			var changing *scanEvidenceMutationContext
			if duringRead {
				changing = &scanEvidenceMutationContext{Context: ctx, at: 7, mutate: mutate}
				ctx = changing
			} else {
				mutate()
			}
			scanEvidenceTestUnavailable(t, evidence, evidence.Revalidate(ctx))
			if changing != nil && changing.calls < changing.at {
				t.Fatal("the controlled rewrite did not reach the second member pass")
			}
		})
	}
}

func TestScanReconciliationSpoolCreatesOnlyThroughBorrowedParentAnchor(t *testing.T) {
	directory := t.TempDir()
	original := filepath.Join(directory, "parent")
	parked := filepath.Join(directory, "parent-held")
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	parent := scanEvidenceTestOpen(t, original)
	if err := os.Rename(original, parked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{Parent: parent, Directory: original})
	t.Cleanup(func() { _ = evidence.Close() })
	if err := evidence.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parked, evidence.spool.name)); err != nil {
		t.Fatalf("the private spool was not created through the retained parent: %v", err)
	}
	entries, err := os.ReadDir(original)
	if err != nil || len(entries) != 0 {
		t.Fatalf("spool creation followed the replaced parent name: entries=%v err=%v", entries, err)
	}
	if err := evidence.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(parked)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cleanup did not use the same retained parent: entries=%v err=%v", entries, err)
	}
	if _, err := parent.Stat("."); err != nil {
		t.Fatalf("cleanup closed its borrowed parent: %v", err)
	}
}

func TestScanReconciliationSpoolBeforeCreateRunsBeforeAnyChildWrite(t *testing.T) {
	parent := scanEvidenceTestOpen(t, t.TempDir())
	refused := errors.New("controlled owner manifest failure")
	name := ""
	callbacks := 0
	evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{
		Parent: parent,
		BeforeCreate: func(candidate string) error {
			name = candidate
			if _, err := parent.Lstat(candidate); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("private child existed before the owner registered it: %v", err)
			}
			return refused
		},
		OnCleanup: func(receipt scanReconciliationSpoolCleanup) {
			callbacks++
			if receipt.Err != nil || receipt.Files != 0 || receipt.Bytes != 0 {
				t.Fatalf("an uncreated child acquired cleanup responsibility: %+v", receipt)
			}
		},
	})
	if !errors.Is(evidence.Err(), refused) || name == "" || callbacks != 1 {
		t.Fatalf("owner rejection did not preserve construction and cleanup results: error=%v name=%q callbacks=%d", evidence.Err(), name, callbacks)
	}
	if _, err := parent.Lstat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed owner registration created a child: %v", err)
	}
	if err := evidence.Close(); err != nil || callbacks != 1 {
		t.Fatalf("an unused reservation was not closed once: %v, %d", err, callbacks)
	}
}

func TestScanReconciliationSpoolCreatedRegistersHeldEmptyChildBeforeRecords(t *testing.T) {
	parent := scanEvidenceTestOpen(t, t.TempDir())
	registered := ""
	createdCalls := 0
	refused := errors.New("controlled generation manifest failure")
	evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{
		Parent:       parent,
		BeforeCreate: func(name string) error { registered = name; return nil },
		Created: func(name string, original os.FileInfo) error {
			createdCalls++
			if name != registered || original == nil {
				t.Fatal("created identity did not match its prior reservation")
			}
			child, err := parent.OpenRoot(name)
			if err != nil {
				t.Fatal(err)
			}
			defer child.Close()
			info, err := child.Stat(".")
			if err != nil || !os.SameFile(original, info) {
				t.Fatalf("created callback did not receive the retained child identity: %v", err)
			}
			file, err := openScanFile(child, ".")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			entries, err := file.ReadDir(-1)
			if err != nil || len(entries) != 0 {
				t.Fatalf("records were written before generation registration: entries=%v error=%v", entries, err)
			}
			return refused
		},
	})
	if !errors.Is(evidence.Err(), refused) || createdCalls != 1 {
		t.Fatalf("generation registration failure was not preserved: error=%v calls=%d", evidence.Err(), createdCalls)
	}
	if done, receipt := evidence.CleanupStatus(); !done || receipt.Err != nil || receipt.Files != 0 || receipt.Bytes != 0 {
		t.Fatalf("generation registration failure did not reclaim its empty child: done=%t receipt=%+v", done, receipt)
	}
	if _, err := parent.Lstat(registered); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed generation registration retained a child: %v", err)
	}
}

func TestScanReconciliationSpoolRenamedChildRetainsCleanupResponsibility(t *testing.T) {
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	root := scanEvidenceTestAttach(t, evidence, "root", t.TempDir())
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	before := evidence.SpoolStats()
	parked := evidence.spool.path + "-parked"
	if err := os.Rename(evidence.spool.path, parked); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Close(); err == nil {
		t.Fatal("a renamed but still allocated spool was reported as reclaimed")
	}
	done, receipt := evidence.CleanupStatus()
	stats := evidence.SpoolStats()
	if !done || receipt.Err == nil || stats.Bytes != before.Bytes || stats.Directories != before.Directories || !stats.CleanupFinished || stats.CleanupErr == nil {
		t.Fatalf("uncertain cleanup released its resource responsibility: before=%+v after=%+v receipt=%+v", before, stats, receipt)
	}
	if _, err := os.Stat(parked); err != nil {
		t.Fatalf("cleanup crossed into the renamed spool without authority: %v", err)
	}
}

func TestScanReconciliationSpoolCleanupNeverDescendsIntoUnexpectedDirectory(t *testing.T) {
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	root := scanEvidenceTestAttach(t, evidence, "root", t.TempDir())
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	nested := filepath.Join(evidence.spool.path, "unexpected-directory")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(nested, "foreign-content")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Close(); err == nil {
		t.Fatal("cleanup acquired recursive authority for unexpected content")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve" {
		t.Fatalf("cleanup descended into an unexpected directory: contents=%q err=%v", data, err)
	}
	if done, receipt := evidence.CleanupStatus(); !done || receipt.Err == nil {
		t.Fatalf("refused recursive cleanup lost its terminal error: done=%t receipt=%+v", done, receipt)
	}
}
