package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanReconciliationEvidencePhysicalPaths(t *testing.T) {
	for _, relative := range []string{"movie.mp4", "folder/movie.mp4", ".ignored", "folder with spaces/track.flac"} {
		actual, valid := scanReconciliationRelative(filepath.FromSlash(relative), false)
		if !valid || actual != relative {
			t.Errorf("physical path %q: got %q, %v", relative, actual, valid)
		}
	}
	if relative, valid := scanReconciliationRelative(".", true); !valid || relative != "." {
		t.Fatal("the walk root must be a valid directory observation")
	}
	for _, relative := range []string{
		"", ".", "..", "../movie.mp4", "folder/../movie.mp4", "folder/./movie.mp4",
		"folder//movie.mp4", "folder/", "/movie.mp4", "//album/root", "//series/example",
		`C:\movie.mp4`, "C:/movie.mp4", "C:movie.mp4", `\\server\share\movie.mp4`,
		"movie\x00.mp4", strings.Repeat("a", 256), strings.Repeat("a/", scanReconciliationMaxPathBytes),
		strings.Repeat("a/", MaxThemeAncestorDepth+1) + "movie.mp4",
	} {
		if _, valid := scanReconciliationRelative(relative, false); valid {
			t.Errorf("unsafe or synthetic path %q was accepted", relative)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "a\x00b", strings.Repeat("a", 256)} {
		if scanReconciliationEntryName(name) {
			t.Errorf("invalid raw directory name %q was accepted", name)
		}
	}
}

func TestScanReconciliationEvidenceInvalidBudgetsAndEmptyPass(t *testing.T) {
	for _, limits := range []scanReconciliationEvidenceLimits{
		{directories: -1, bytes: scanReconciliationScratchBytes},
		{entries: -1, bytes: scanReconciliationScratchBytes},
		{seenIDs: -1, bytes: scanReconciliationScratchBytes},
		{bytes: scanReconciliationScratchBytes - 1},
	} {
		evidence := newScanReconciliationEvidenceWithLimits(limits)
		if !errors.Is(evidence.Err(), errScanReconciliationEvidenceBudget) || !evidence.closed {
			t.Fatal("invalid budgets must disable evidence before allocation")
		}
		if evidence.roots != nil || evidence.seen != nil || evidence.Close() != nil {
			t.Fatal("disabled construction retained allocated evidence")
		}
	}
	evidence := newScanReconciliationEvidence()
	if err := evidence.Revalidate(context.Background()); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("an empty pass must be unavailable: %v", err)
	}
	var absent *scanReconciliationEvidence
	if absent.Err() == nil || absent.Seen("item") || absent.Close() != nil || absent.Disable(nil) == nil {
		t.Fatal("nil evidence must not authorize any work")
	}
}

func TestScanReconciliationEvidenceSeenIDsAreDistinctAndIrreversible(t *testing.T) {
	evidence := newScanReconciliationEvidenceWithLimits(scanReconciliationEvidenceLimits{
		seenIDs: 1, bytes: scanReconciliationScratchBytes + scanReconciliationSeenIDBytes + 4,
	})
	if err := evidence.MarkSeen("item"); err != nil {
		t.Fatal(err)
	}
	before := evidence.bytes
	if err := evidence.MarkSeen("item"); err != nil || evidence.bytes != before || evidence.seenIDs != 1 || !evidence.Seen("item") {
		t.Fatal("a cache hit must remain seen without consuming another budget entry")
	}
	if err := evidence.MarkSeen("next"); !errors.Is(err, errScanReconciliationEvidenceBudget) {
		t.Fatalf("a second distinct item must exceed the shared bound: %v", err)
	}
	if err := evidence.MarkSeen("item"); !errors.Is(err, errScanReconciliationEvidenceBudget) {
		t.Fatal("evidence must not recover after a budget failure")
	}
	if err := evidence.Disable(context.Canceled); !errors.Is(err, errScanReconciliationEvidenceBudget) {
		t.Fatal("later failures must preserve the first budget reason")
	}
	if evidence.Close() != nil || evidence.Close() != nil {
		t.Fatal("closing disabled evidence must be idempotent")
	}
}
