package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func scanReconciliationDirectoryFile(t *testing.T) *os.File {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{".ignored", "theme.mp3", "film.mp4"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	directory, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = directory.Close() })
	return directory
}

func TestScanReconciliationDirectoryReadCompleteRawMembership(t *testing.T) {
	entries, err := readScanDirectoryEntries(context.Background(), scanReconciliationDirectoryFile(t))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{".ignored", "film.mp4", "theme.mp3"}) {
		t.Fatalf("raw scan directory membership was filtered: %v", names)
	}
}

func TestScanReconciliationDirectoryReadLimitsNeverReturnPartialEntries(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		entries, bytes int
	}{
		{"entry count", 2, 1 << 20},
		{"byte count", 3, 512},
		{"invalid bounds", -1, 1 << 20},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			entries, err := readScanDirectoryEntriesLimit(context.Background(), scanReconciliationDirectoryFile(t), scenario.entries, scenario.bytes)
			if !errors.Is(err, errScanReconciliationEvidenceBudget) || len(entries) != 0 {
				t.Fatalf("excessive directory supplied partial proof: entries=%d error=%v", len(entries), err)
			}
		})
	}
}

func TestScanReconciliationDirectoryReadCancellationPrecedesIO(t *testing.T) {
	directory := scanReconciliationDirectoryFile(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	entries, err := readScanDirectoryEntries(ctx, directory)
	if !errors.Is(err, context.Canceled) || len(entries) != 0 {
		t.Fatalf("cancelled scan read returned a directory: %v", err)
	}
	if entries, err := readScanDirectoryEntries(context.Background(), directory); err != nil || len(entries) != 3 {
		t.Fatalf("cancelled scan advanced the directory cursor: entries=%d error=%v", len(entries), err)
	}
}
