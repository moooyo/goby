//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func scanSpoolTestCollector(t *testing.T, options scanReconciliationSpoolOptions) *scanReconciliationEvidence {
	t.Helper()
	if options.Directory == "" {
		options.Directory = t.TempDir()
	}
	evidence := newScanReconciliationSpoolEvidence(context.Background(), options)
	if err := evidence.Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = evidence.Close() })
	return evidence
}

func scanSpoolTestReadDirectory(t *testing.T, root *os.Root, relative string) (os.FileInfo, []os.DirEntry) {
	t.Helper()
	directory, err := openScanFile(root, filepath.FromSlash(relative))
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	info, err := directory.Stat()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	return info, entries
}

func scanSpoolTestCleanup(t *testing.T, evidence *scanReconciliationEvidence) scanReconciliationSpoolCleanup {
	t.Helper()
	select {
	case <-evidence.CleanupDone():
	case <-time.After(5 * time.Second):
		t.Fatal("scan spool cleanup did not produce a terminal receipt")
	}
	done, receipt := evidence.CleanupStatus()
	if !done {
		t.Fatal("scan spool cleanup completion lost its receipt")
	}
	return receipt
}

func TestScanReconciliationSpoolPreservesExactRawNames(t *testing.T) {
	directory := t.TempDir()
	names := []string{"a\xff", "a\xfe", "a\ufffd", "a\\b", "line\nbreak", strings.Repeat("n", 255)}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	record, err := evidence.spool.open("root", ".", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer record.file.Close()
	sort.Strings(names)
	if record.count != len(names) {
		t.Fatalf("raw member count = %d, want %d", record.count, len(names))
	}
	for index, name := range names {
		entry, err := record.entry(index)
		if err != nil || entry.name != name {
			t.Fatalf("raw member %d = %q, want %q: %v", index, entry.name, name, err)
		}
		entry, position, found, err := record.lookup(name)
		if err != nil || !found || position != index || entry.name != name {
			t.Fatalf("exact lookup %q changed its bytes or index: %q, %d, %v, %v", name, entry.name, position, found, err)
		}
	}
	for _, name := range []string{"a\xfd", "a\ufffd\ufffd", "line", strings.Repeat("n", 254)} {
		if _, _, found, err := record.lookup(name); err != nil || found {
			t.Fatalf("distinct raw name %q collided with a recorded member: %v, %v", name, found, err)
		}
	}
	if err := record.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if absent, err := evidence.PathAbsent(context.Background(), "root", "missing.mp4"); err != nil || !absent {
		t.Fatalf("stable raw membership lost its absent candidate: %v, %v", absent, err)
	}
	for _, name := range names {
		if name == "a\\b" {
			continue
		}
		if absent, err := evidence.PathAbsent(context.Background(), "root", name); err != nil || absent {
			t.Fatalf("existing raw member %q became absence: %v, %v", name, absent, err)
		}
	}
	if evidence.roots["root"].directories != nil {
		t.Fatal("spooled membership retained a whole-pass directory map")
	}
}

func TestScanReconciliationSpoolCorruptionDisablesWholePass(t *testing.T) {
	for _, scenario := range []string{"header", "header checksum", "entry", "entry checksum", "truncated header", "truncated entry"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			scanEvidenceTestFiles(t, directory, map[string]string{"movie.mp4": "video", ".ignored": "hidden"})
			evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			record, err := evidence.spool.open("root", ".", os.O_RDONLY)
			if err != nil {
				t.Fatal(err)
			}
			offset := record.offset
			if err := record.file.Close(); err != nil {
				t.Fatal(err)
			}
			filename := filepath.Join(evidence.spool.path, scanSpoolFileName("root", "."))
			file, err := os.OpenFile(filename, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "truncated header":
				err = file.Truncate(5)
			case "truncated entry":
				err = file.Truncate(offset + int64(record.count)*scanSpoolEntrySize - 1)
			default:
				position := int64(4)
				switch scenario {
				case "header checksum":
					position = offset - 1
				case "entry":
					position = offset + 2
				case "entry checksum":
					position = offset + scanSpoolEntrySize - 1
				}
				var data [1]byte
				if _, err = file.ReadAt(data[:], position); err == nil {
					data[0] ^= 0x80
					_, err = file.WriteAt(data[:], position)
				}
			}
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("corrupt spool fixture: %v, %v", err, closeErr)
			}
			scanEvidenceTestUnavailable(t, evidence, evidence.Revalidate(context.Background()))
			if absent, err := evidence.PathAbsent(context.Background(), "root", "missing.mp4"); absent || !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
				t.Fatalf("corrupt evidence authorized a later absence: %v, %v", absent, err)
			}
			receipt := scanSpoolTestCleanup(t, evidence)
			if receipt.Err != nil || receipt.Files != 1 || receipt.Bytes <= 0 {
				t.Fatalf("corrupt spool did not retain its cleanup accounting: %+v", receipt)
			}
		})
	}
}

func TestScanReconciliationSpoolRequiresEveryDistinctDirectoryRecord(t *testing.T) {
	for _, scenario := range []string{"missing record", "duplicate ordinal", "record substitution"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			for _, name := range []string{"left", "rght"} {
				if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			for _, relative := range []string{".", "left", "rght"} {
				scanEvidenceTestRecord(t, evidence, "root", relative, root)
			}
			scanEvidenceTestComplete(t, evidence, "root", "left", "rght", ".")
			left := filepath.Join(evidence.spool.path, scanSpoolFileName("root", "left"))
			right := filepath.Join(evidence.spool.path, scanSpoolFileName("root", "rght"))
			switch scenario {
			case "missing record":
				if err := os.Remove(left); err != nil {
					t.Fatal(err)
				}
			case "duplicate ordinal":
				first, err := evidence.spool.open("root", "left", os.O_RDONLY)
				if err != nil {
					t.Fatal(err)
				}
				ordinal := first.ordinal
				if err := first.file.Close(); err != nil {
					t.Fatal(err)
				}
				second, err := evidence.spool.open("root", "rght", os.O_RDWR)
				if err != nil {
					t.Fatal(err)
				}
				second.ordinal = ordinal
				writeErr := scanSpoolWriteHeader(second)
				closeErr := second.file.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatalf("duplicate directory ordinal fixture: %v, %v", writeErr, closeErr)
				}
			case "record substitution":
				data, err := os.ReadFile(left)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(right, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			scanEvidenceTestUnavailable(t, evidence, evidence.Revalidate(context.Background()))
			receipt := scanSpoolTestCleanup(t, evidence)
			if receipt.Err != nil || receipt.Files != 3 {
				t.Fatalf("incomplete directory membership lost its cleanup ownership: %+v", receipt)
			}
		})
	}
}

func TestScanReconciliationSpoolRejectsLinkedAndSpecialRecordFiles(t *testing.T) {
	for _, scenario := range []string{"symlink", "fifo"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			name := scanSpoolFileName("root", ".")
			filename := filepath.Join(evidence.spool.path, name)
			if err := os.Rename(filename, filename+".saved"); err != nil {
				t.Fatal(err)
			}
			if scenario == "symlink" {
				if err := os.Symlink(name+".saved", filename); err != nil {
					t.Fatal(err)
				}
			} else if err := syscall.Mkfifo(filename, 0o600); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				record, err := evidence.spool.open("root", ".", os.O_RDONLY)
				if record != nil {
					_ = record.file.Close()
				}
				result <- err
			}()
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("a linked or special record was accepted as private evidence")
				}
			case <-time.After(time.Second):
				// Keep a regressed blocking FIFO open from leaking a test worker.
				fd, err := syscall.Open(filename, syscall.O_RDWR|syscall.O_NONBLOCK, 0)
				if err == nil {
					defer syscall.Close(fd)
				}
				select {
				case <-result:
				case <-time.After(time.Second):
				}
				t.Fatal("opening a special spool record blocked before rejecting its type")
			}
		})
	}
}

func TestScanReconciliationSpoolBudgetFailureProducesOneCleanupReceipt(t *testing.T) {
	for _, scenario := range []string{"directories", "entries", "bytes"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			options := scanReconciliationSpoolOptions{}
			child := "child"
			if scenario == "bytes" {
				for index := 0; index < 32; index++ {
					if err := os.Mkdir(filepath.Join(directory, fmt.Sprintf("child-%02d", index)), 0o700); err != nil {
						t.Fatal(err)
					}
				}
				child = "child-00"
				header := scanSpoolEncodeHeader(&scanSpoolDirectory{rootID: "root", relative: ".", count: 32})
				options.MaxBytes = int64(4+len(header)) + 32*scanSpoolEntrySize
			} else {
				scanEvidenceTestFiles(t, directory, map[string]string{"child/movie.mp4": "video"})
				if scenario == "directories" {
					options.MaxDirectories = 1
				} else {
					options.MaxEntries = 1
				}
			}
			var cleanupCalls atomic.Int32
			options.OnCleanup = func(scanReconciliationSpoolCleanup) { cleanupCalls.Add(1) }
			evidence := scanSpoolTestCollector(t, options)
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			before := evidence.SpoolStats()
			if scenario == "bytes" && before.Bytes != options.MaxBytes {
				t.Fatalf("fixture did not exactly fill its byte budget: %d, want %d", before.Bytes, options.MaxBytes)
			}
			info, raw := scanSpoolTestReadDirectory(t, root, child)
			if err := evidence.RecordDirectory("root", child, info, raw); !errors.Is(err, errScanReconciliationEvidenceBudget) {
				t.Fatalf("spool budget overflow did not disable the pass: %v", err)
			}
			receipt := scanSpoolTestCleanup(t, evidence)
			if receipt.Err != nil || receipt.Bytes != before.Bytes || receipt.Files != before.Directories || receipt.Path == "" {
				t.Fatalf("budget failure lost its reserved-resource receipt: %+v, before=%+v", receipt, before)
			}
			if _, err := os.Stat(receipt.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("successful cleanup retained its private spool: %v", err)
			}
			if _, err := root.Stat("."); err != nil {
				t.Fatalf("spool overflow closed the borrowed media root: %v", err)
			}
			if err := evidence.Close(); err != nil || cleanupCalls.Load() != 1 {
				t.Fatalf("cleanup was not idempotent: calls=%d error=%v", cleanupCalls.Load(), err)
			}
			if !errors.Is(evidence.Err(), errScanReconciliationEvidenceBudget) {
				t.Fatal("cleanup replaced the original budget failure")
			}
		})
	}
}

func TestScanReconciliationSpoolRetirementWaitsForBlockedWorker(t *testing.T) {
	for _, scenario := range []string{"close", "disable"} {
		t.Run(scenario, func(t *testing.T) {
			directory := t.TempDir()
			cleanupReceipts := make(chan scanReconciliationSpoolCleanup, 2)
			evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{
				OnCleanup: func(receipt scanReconciliationSpoolCleanup) { cleanupReceipts <- receipt },
			})
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			scanEvidenceTestRecord(t, evidence, "root", ".", root)
			scanEvidenceTestComplete(t, evidence, "root", ".")
			before := evidence.SpoolStats()
			spoolPath := evidence.spool.path
			anchor := evidence.roots["root"].anchor
			entered, unblock := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			workerResult := make(chan error, 1)
			t.Cleanup(func() {
				releaseOnce.Do(func() { close(unblock) })
				_ = evidence.Close()
				select {
				case <-evidence.CleanupDone():
				case <-time.After(5 * time.Second):
					t.Error("retired spool worker did not release its test resources")
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			slots := make(chan struct{}, 1)
			result := make(chan error, 1)
			go func() {
				result <- runStorageObservationWithLimit(ctx, slots, time.Minute,
					[]*storageObservationLifetime{&evidence.observation}, func(workContext context.Context) error {
						close(entered)
						<-unblock
						record, err := evidence.spool.open("root", ".", os.O_RDONLY)
						if err == nil {
							err = record.file.Close()
						}
						if _, statErr := anchor.Stat("."); err == nil {
							err = statErr
						}
						workerResult <- err
						return workContext.Err()
					})
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("controlled spool observation did not start")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("original cancellation was not preserved: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("blocked spool observation retained its caller")
			}
			if scenario == "close" {
				if err := evidence.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err := evidence.Disable(context.Canceled); !errors.Is(err, context.Canceled) {
				t.Fatal("disabling retained evidence lost its original reason")
			}
			select {
			case <-evidence.CleanupDone():
				t.Fatal("retirement reported cleanup while a worker still retained the spool")
			default:
			}
			if done, _ := evidence.CleanupStatus(); done {
				t.Fatal("retirement marked a pending cleanup receipt complete")
			}
			if _, err := os.Stat(spoolPath); err != nil {
				t.Fatalf("retirement removed its worker's spool: %v", err)
			}
			if after := evidence.SpoolStats(); after.Bytes != before.Bytes || after.Directories != before.Directories {
				t.Fatalf("retirement released still-owned quota: before=%+v after=%+v", before, after)
			}
			called := false
			if err := runStorageObservationWithLimit(context.Background(), slots, time.Second, nil, func(context.Context) error {
				called = true
				return nil
			}); !errors.Is(err, errStorageObservationUnavailable) || called {
				t.Fatalf("retired worker released its occupied observation slot: %v, %v", called, err)
			}
			releaseOnce.Do(func() { close(unblock) })
			receipt := scanSpoolTestCleanup(t, evidence)
			select {
			case err := <-workerResult:
				if err != nil {
					t.Fatalf("retired worker lost retained filesystem resources: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("retired worker did not report its retained-resource check")
			}
			if receipt.Err != nil || receipt.Bytes != before.Bytes || receipt.Files != before.Directories {
				t.Fatalf("retired cleanup lost the original resource accounting: %+v", receipt)
			}
			select {
			case callbackReceipt := <-cleanupReceipts:
				if callbackReceipt.Path != receipt.Path || callbackReceipt.Err != nil {
					t.Fatalf("cleanup callback disagreed with its terminal receipt: %+v", callbackReceipt)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("deferred cleanup did not notify its owner")
			}
			if _, err := os.Stat(spoolPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("finished retired worker retained its private spool: %v", err)
			}
			if err := evidence.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-cleanupReceipts:
				t.Fatal("retired spool cleanup ran more than once")
			default:
			}
		})
	}
}

func TestScanReconciliationSpoolCleanupFailurePreservesReceiptAndForeignDirectory(t *testing.T) {
	directory := t.TempDir()
	var cleanupCalls atomic.Int32
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{
		OnCleanup: func(scanReconciliationSpoolCleanup) { cleanupCalls.Add(1) },
	})
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	before := evidence.SpoolStats()
	original := evidence.spool.path
	retained := original + "-retained"
	if err := os.Rename(original, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(original, "foreign-owner")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := evidence.Close(); err == nil {
		t.Fatal("cleanup accepted a replacement directory as its owned spool")
	}
	receipt := scanSpoolTestCleanup(t, evidence)
	if receipt.Err == nil || receipt.Path != original || receipt.Bytes != before.Bytes || receipt.Files != before.Directories {
		t.Fatalf("failed cleanup discarded ownership and charges: %+v", receipt)
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "preserve" {
		t.Fatalf("cleanup modified another directory's contents: %q, %v", data, err)
	}
	if _, err := os.Stat(retained); err != nil {
		t.Fatalf("fixture lost the unreclaimed original spool: %v", err)
	}
	_ = evidence.Close()
	_, repeated := evidence.CleanupStatus()
	if repeated.Err == nil || repeated.Bytes != receipt.Bytes || repeated.Files != receipt.Files || cleanupCalls.Load() != 1 {
		t.Fatalf("repeated close erased a failed cleanup receipt: %+v, calls=%d", repeated, cleanupCalls.Load())
	}
}

func scanSpoolTestDescriptorCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("descriptor accounting requires procfs: %v", err)
	}
	return len(entries)
}

func TestScanReconciliationSpoolDirectoryCountDoesNotRetainDescriptors(t *testing.T) {
	const directoryCount = 4097
	directory := t.TempDir()
	for index := 0; index < directoryCount; index++ {
		if err := os.Mkdir(filepath.Join(directory, fmt.Sprintf("child-%05d", index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{
		MaxDirectories:     directoryCount + 1,
		MaxFallbackHandles: 1,
		directoryIdentity: func(file *os.File) (scanSpoolIdentity, bool, error) {
			// This deterministic fixture isolates descriptor accounting. The
			// exported-handle generation contract has separate identity tests.
			info, err := file.Stat()
			if err != nil {
				return scanSpoolIdentity{}, false, err
			}
			return scanSpoolIdentity{kind: 1, mountID: 1, handleType: 1, bytes: fileIdentity(info)}, true, nil
		},
	})
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	baseline, peak := scanSpoolTestDescriptorCount(t), 0
	for index := 0; index < directoryCount; index++ {
		relative := fmt.Sprintf("child-%05d", index)
		scanEvidenceTestRecord(t, evidence, "root", relative, root)
		scanEvidenceTestComplete(t, evidence, "root", relative)
		if index%128 == 0 || index == directoryCount-1 {
			peak = max(peak, scanSpoolTestDescriptorCount(t))
		}
	}
	scanEvidenceTestComplete(t, evidence, "root", ".")
	stats := evidence.SpoolStats()
	if stats.Directories != directoryCount+1 || stats.Entries != directoryCount || stats.FallbackHandles != 0 {
		t.Fatalf("large directory topology retained unexpected evidence: %+v", stats)
	}
	if evidence.roots["root"].directories != nil {
		t.Fatal("large directory topology rebuilt an in-memory directory index")
	}
	if peak > baseline+12 {
		t.Fatalf("directory count retained descriptors: baseline=%d peak=%d directories=%d", baseline, peak, stats.Directories)
	}
	if err := evidence.Revalidate(context.Background()); err != nil {
		t.Fatalf("large directory topology could not revalidate its disk evidence: %v", err)
	}
	if absent, err := evidence.PathAbsent(context.Background(), "root", "child-04096/missing.mp4"); err != nil || !absent {
		t.Fatalf("large directory topology lost its last child's absence proof: %v, %v", absent, err)
	}
}
