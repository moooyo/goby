//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScanReconciliationSpoolReadCacheRetainsFreshFinalRecords(t *testing.T) {
	directory := t.TempDir()
	const count = 17
	for index := 0; index < count; index++ {
		name := fmt.Sprintf("Film-%03d.mp4", index)
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	root := scanEvidenceTestAttach(t, evidence, "root", directory)
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	record, err := evidence.spool.open("root", ".", os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	defer record.file.Close()
	ctx := &scanEvidenceMutationContext{Context: context.Background(), at: 7, mutate: func() {
		// The first membership enumeration already loaded the final block. Damage
		// a later record before its fresh final read, after lookup cache admission.
		position := record.offset + int64(count-1)*scanSpoolEntrySize + scanSpoolEntrySize - 1
		var data [1]byte
		if _, err := record.file.ReadAt(data[:], position); err != nil {
			t.Fatal(err)
		}
		data[0] ^= 0x80
		if _, err := record.file.WriteAt(data[:], position); err != nil {
			t.Fatal(err)
		}
	}}
	scanEvidenceTestUnavailable(t, evidence, evidence.Revalidate(ctx))
	if ctx.calls < ctx.at {
		t.Fatal("verification did not reach its fresh final read")
	}
	if absent, err := evidence.PathAbsent(context.Background(), "root", "Missing.mp4"); absent ||
		!errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("record lookup cache authorized absence after final corruption: %t, %v", absent, err)
	}
}

func BenchmarkScanReconciliationSpoolDirectoryVerification(b *testing.B) {
	for _, count := range []int{0, 1, 8, 16, 64, 128, 1024, 8192} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			directory := b.TempDir()
			for index := 0; index < count; index++ {
				name := fmt.Sprintf("Film-%05d.mp4", index)
				if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
					b.Fatal(err)
				}
			}
			evidence := newScanReconciliationSpoolEvidence(context.Background(), scanReconciliationSpoolOptions{Directory: b.TempDir()})
			b.Cleanup(func() {
				if err := evidence.Close(); err != nil {
					b.Error(err)
				}
			})
			root, err := os.OpenRoot(directory)
			if err != nil {
				b.Fatal(err)
			}
			defer root.Close()
			if err := evidence.AttachRoot("root", root); err != nil {
				b.Fatal(err)
			}
			file, err := openScanFile(root, ".")
			if err != nil {
				b.Fatal(err)
			}
			before, err := file.Stat()
			if err != nil {
				b.Fatal(err)
			}
			if err := evidence.BeginDirectoryObservation("root", ".", file, before); err != nil {
				b.Fatal(err)
			}
			entries, err := file.ReadDir(-1)
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				b.Fatalf("read benchmark membership: %v, %v", err, closeErr)
			}
			if err := evidence.RecordDirectory("root", ".", before, entries); err != nil {
				b.Fatal(err)
			}
			if err := evidence.CompleteDirectory("root", "."); err != nil {
				b.Fatal(err)
			}
			record, err := evidence.spool.open("root", ".", os.O_RDONLY)
			if err != nil {
				b.Fatal(err)
			}
			defer record.file.Close()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := evidence.verifySpoolDirectory(context.Background(), record, nil); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(count), "entries/op")
		})
	}
}
