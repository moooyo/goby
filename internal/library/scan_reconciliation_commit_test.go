package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanReconciliationObservationClassificationPreservesFatalErrors(t *testing.T) {
	observation := scanReconciliationUnavailable("changed directory")
	budget := scanReconciliationBudget()
	for _, test := range []struct {
		name string
		err  error
		soft bool
	}{
		{"observation", observation, true},
		{"budget", budget, true},
		{"only observations joined", errors.Join(observation, budget), true},
		{"empty", nil, false},
		{"unmarked sentinel", errScanReconciliationEvidenceUnavailable, false},
		{"database failure", errors.Join(observation, errors.New("rollback failed")), false},
		{"lost ownership", errors.Join(budget, ErrUnavailable), false},
		{"cancelled observation", scanReconciliationObservationFailure{errors.Join(errScanReconciliationEvidenceUnavailable, context.Canceled)}, false},
		{"cancelled rollback", errors.Join(observation, context.Canceled), false},
		{"deadline", errors.Join(observation, context.DeadlineExceeded), false},
		{"unclassified wrapper", fmt.Errorf("database callback: %w", observation), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := scanReconciliationObservationOnly(test.err); got != test.soft {
				t.Fatalf("observation classification = %v, want %v: %v", got, test.soft, test.err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scanReconciliationObservation(ctx, errScanReconciliationEvidenceBudget); !errors.Is(err, context.Canceled) || scanReconciliationObservationOnly(err) {
		t.Fatalf("original cancellation was hidden by a budget failure: %v", err)
	}
}

func TestScanReconciliationPhysicalMembersRequireExactCurrentScope(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "media")
	roots := map[string]*rootBindingScanCapture{"root": {row: rootBindingRow{root: libraryRoot{path: rootPath}}}}
	valid := scanReconciliationItem{id: "item", libraryID: "library", rootID: "root", parentID: "parent",
		typeName: "Movie", relative: "missing/movie.mkv", path: filepath.Join(rootPath, "missing", "movie.mkv"),
		ordinary: true, roleValid: true, visible: true}
	if err := validateScanReconciliationPhysical(valid, "library", roots); err != nil {
		t.Fatalf("valid physical member rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(*scanReconciliationItem)
	}{
		{"foreign library", func(item *scanReconciliationItem) { item.libraryID = "foreign" }},
		{"foreign root", func(item *scanReconciliationItem) { item.rootID = "unknown" }},
		{"null root", func(item *scanReconciliationItem) { item.rootID = "" }},
		{"collection", func(item *scanReconciliationItem) { item.typeName = "CollectionFolder" }},
		{"unproven reserved history", func(item *scanReconciliationItem) { item.ordinary, item.roleValid = false, false }},
		{"invalid parent", func(item *scanReconciliationItem) { item.parentID = "bad\nparent" }},
		{"synthetic series", func(item *scanReconciliationItem) { item.relative = "//series/root" }},
		{"synthetic season", func(item *scanReconciliationItem) { item.relative = "//season/root/1" }},
		{"synthetic album", func(item *scanReconciliationItem) { item.relative = "//album/root" }},
		{"empty physical path", func(item *scanReconciliationItem) { item.path = "" }},
		{"wrong physical path", func(item *scanReconciliationItem) { item.path = filepath.Join(rootPath, "other.mkv") }},
		{"root directory", func(item *scanReconciliationItem) { item.relative, item.path = ".", rootPath }},
		{"parent traversal", func(item *scanReconciliationItem) { item.relative = "../movie.mkv" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			item := valid
			test.change(&item)
			err := validateScanReconciliationPhysical(item, "library", roots)
			if !errors.Is(err, errScanReconciliationEvidenceUnavailable) || !scanReconciliationObservationOnly(err) {
				t.Fatalf("unproven physical member was accepted: %v", err)
			}
		})
	}
	auxiliary := valid
	auxiliary.ordinary, auxiliary.themeOwner = false, "parent"
	if err := validateScanReconciliationPhysical(auxiliary, "library", roots); err != nil {
		t.Fatalf("independently validated auxiliary role was rejected: %v", err)
	}
	if fact := auxiliary.fact(); fact.Kind != CatalogRemoved || fact.ParentID != "" || fact.LibraryID != "library" {
		t.Fatalf("auxiliary semantic owner leaked into browse parent: %+v", fact)
	}
}

func TestScanReconciliationMusicAncestorsSurviveRemovedIntermediateParents(t *testing.T) {
	known := map[string]scanReconciliationItem{
		"library":      {id: "library", typeName: "CollectionFolder", isFolder: true, ordinary: true},
		"album":        {id: "album", parentID: "library", typeName: "MusicAlbum", isFolder: true, ordinary: true},
		"disc":         {id: "disc", parentID: "album", typeName: "Folder", isFolder: true, ordinary: true},
		"track":        {id: "track", parentID: "disc", typeName: "Audio", ordinary: true},
		"nested":       {id: "nested", parentID: "album", typeName: "MusicAlbum", isFolder: true, ordinary: true},
		"nested-track": {id: "nested-track", parentID: "nested", typeName: "Audio", ordinary: true},
	}
	members := map[string]scanReconciliationItem{"disc": known["disc"], "track": known["track"], "nested": known["nested"], "nested-track": known["nested-track"]}
	got, err := scanReconciliationAlbums(members, known)
	if err != nil || !reflect.DeepEqual(got, []string{"album"}) {
		t.Fatalf("surviving album was lost with removed intermediate parents: %v, %v", got, err)
	}
	members["album"] = known["album"]
	if got, err := scanReconciliationAlbums(members, known); err != nil || len(got) != 0 {
		t.Fatalf("removed album was scheduled for refresh: %v, %v", got, err)
	}
}

func TestScanReconciliationRejectsCyclesAndExcessiveHierarchyDepth(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		t.Run(fmt.Sprintf("cycle=%v", cycle), func(t *testing.T) {
			known := make(map[string]scanReconciliationItem)
			for index := 0; index <= scanReconciliationMaxDepth; index++ {
				id := fmt.Sprintf("node-%d", index)
				item := scanReconciliationItem{id: id, typeName: "Folder", ordinary: true, isFolder: true}
				if index < scanReconciliationMaxDepth {
					item.parentID = fmt.Sprintf("node-%d", index+1)
				}
				known[id] = item
			}
			want := errScanReconciliationEvidenceBudget
			if cycle {
				known["node-1"] = scanReconciliationItem{id: "node-1", parentID: "node-0", typeName: "Folder"}
				want = errScanReconciliationEvidenceUnavailable
			}
			_, err := scanReconciliationAlbums(map[string]scanReconciliationItem{"node-0": known["node-0"]}, known)
			if !errors.Is(err, want) || !scanReconciliationObservationOnly(err) {
				t.Fatalf("unsafe hierarchy was accepted: %v", err)
			}
		})
	}
}

func TestScanReconciliationBudgetsRejectRatherThanTruncateProof(t *testing.T) {
	for _, test := range []struct {
		name   string
		budget scanReconciliationBudgetState
		item   scanReconciliationItem
	}{
		{"item count", scanReconciliationBudgetState{items: scanReconciliationMaxItems}, scanReconciliationItem{}},
		{"byte count", scanReconciliationBudgetState{bytes: scanReconciliationMaxBytes - scanReconciliationItemBytes}, scanReconciliationItem{id: "x"}},
		{"oversized SQL text", scanReconciliationBudgetState{}, scanReconciliationItem{oversized: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := test.budget
			err := test.budget.retain(test.item)
			if !errors.Is(err, errScanReconciliationEvidenceBudget) || !scanReconciliationObservationOnly(err) || test.budget != previous {
				t.Fatalf("exhausted proof was retained: before=%+v, after=%+v, error=%v", previous, test.budget, err)
			}
		})
	}
	budget := scanReconciliationBudgetState{bytes: scanReconciliationMaxBytes - scanReconciliationItemBytes - 1}
	if err := budget.retain(scanReconciliationItem{id: "x"}); err != nil || budget.bytes != scanReconciliationMaxBytes || budget.items != 1 {
		t.Fatalf("exact finite budget was rejected: %+v, %v", budget, err)
	}
}
