//go:build linux

package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestScanReconciliationSpoolBatchedEntriesRemainProvable(t *testing.T) {
	directory := t.TempDir()
	const count = 2*scanSpoolWriteBatchEntries + 7
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
	record, err := evidence.spool.open("root", ".", os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}
	defer record.file.Close()
	if record.count != count {
		t.Fatalf("batched entry count = %d, want %d", record.count, count)
	}
	for index := 0; index < count; index++ {
		want := fmt.Sprintf("Film-%03d.mp4", index)
		entry, position, found, err := record.lookup(want)
		if err != nil || !found || position != index || entry.name != want {
			t.Fatalf("batched lookup %q = %q at %d, found=%t: %v", want, entry.name, position, found, err)
		}
	}
	if err := evidence.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if absent, err := evidence.PathAbsent(context.Background(), "root", "Missing.mp4"); err != nil || !absent {
		t.Fatalf("batched membership did not prove an absent pathname: %t, %v", absent, err)
	}
}
