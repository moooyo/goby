package library

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

func scanReconciliationAncestorTestFixture(depth, leaves int) (map[string]scanReconciliationItem, map[string]scanReconciliationItem) {
	known := make(map[string]scanReconciliationItem)
	parent := ""
	for index := 0; index < depth; index++ {
		id := fmt.Sprintf("ancestor-%03d", index)
		item := scanReconciliationItem{id: id, libraryID: "library", parentID: parent,
			typeName: "Folder", ordinary: true, isFolder: true}
		if index == depth/2 {
			item.typeName = "MusicAlbum"
		}
		known[id] = item
		parent = id
	}
	members := make(map[string]scanReconciliationItem)
	for index := 0; index < leaves; index++ {
		id := fmt.Sprintf("leaf-%04d", index)
		item := scanReconciliationItem{id: id, libraryID: "library", parentID: parent,
			typeName: "Audio", ordinary: true, visible: true}
		known[id], members[id] = item, item
	}
	return members, known
}

func TestScanReconciliationMemoizedAncestorsMatchIndependentWalks(t *testing.T) {
	for _, test := range []struct {
		name   string
		depth  int
		leaves int
		change func(map[string]scanReconciliationItem, map[string]scanReconciliationItem)
	}{
		{name: "shared long chain", depth: 100, leaves: 1000},
		{name: "exact depth limit", depth: scanReconciliationMaxDepth - 1, leaves: 8},
		{name: "origin depth survives memo hits", depth: scanReconciliationMaxDepth, leaves: 8,
			change: func(members, known map[string]scanReconciliationItem) {
				for id, item := range known {
					if item.isFolder {
						members[id] = item
					}
				}
			}},
		{name: "removed intermediate album", depth: 10, leaves: 8,
			change: func(members, known map[string]scanReconciliationItem) {
				outer := known["ancestor-002"]
				outer.typeName = "MusicAlbum"
				known[outer.id] = outer
				members["ancestor-005"] = known["ancestor-005"]
			}},
		{name: "nearest surviving album", depth: 10, leaves: 8,
			change: func(_ map[string]scanReconciliationItem, known map[string]scanReconciliationItem) {
				inner := known["ancestor-008"]
				inner.typeName = "MusicAlbum"
				known[inner.id] = inner
			}},
		{name: "nonordinary and nonfolder albums", depth: 10, leaves: 8,
			change: func(_ map[string]scanReconciliationItem, known map[string]scanReconciliationItem) {
				for _, id := range []string{"ancestor-007", "ancestor-008"} {
					item := known[id]
					item.typeName = "MusicAlbum"
					item.ordinary = id != "ancestor-007"
					item.isFolder = id != "ancestor-008"
					known[id] = item
				}
			}},
		{name: "non Audio members do not contribute", depth: 10, leaves: 8,
			change: func(members, known map[string]scanReconciliationItem) {
				for id, item := range members {
					item.typeName = "Movie"
					members[id], known[id] = item, item
				}
			}},
		{name: "unrelated invalid nodes are ignored", depth: 10, leaves: 8,
			change: func(_ map[string]scanReconciliationItem, known map[string]scanReconciliationItem) {
				known["unrelated-cycle"] = scanReconciliationItem{id: "unrelated-cycle", parentID: "unrelated-cycle"}
				known["unrelated-missing"] = scanReconciliationItem{id: "unrelated-missing", parentID: "missing"}
			}},
		{name: "missing ancestor above an album", depth: 10, leaves: 1,
			change: func(_ map[string]scanReconciliationItem, known map[string]scanReconciliationItem) {
				delete(known, "ancestor-002")
			}},
		{name: "cycle above an album", depth: 10, leaves: 1,
			change: func(_ map[string]scanReconciliationItem, known map[string]scanReconciliationItem) {
				item := known["ancestor-000"]
				item.parentID = "ancestor-001"
				known[item.id] = item
			}},
		{name: "non Audio missing ancestor", depth: 10, leaves: 1,
			change: func(members, known map[string]scanReconciliationItem) {
				item := members["leaf-0000"]
				item.typeName = "Movie"
				members[item.id], known[item.id] = item, item
				delete(known, "ancestor-002")
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			members, known := scanReconciliationAncestorTestFixture(test.depth, test.leaves)
			if test.change != nil {
				test.change(members, known)
			}
			want, wantErr := scanReconciliationAlbums(members, known)
			budget := &scanReconciliationBudgetState{bytes: 12345, items: len(known)}
			got, complete, err := scanReconciliationMemoizedAlbums(members, known, budget)
			if !complete || !reflect.DeepEqual(got, want) ||
				(err == nil) != (wantErr == nil) ||
				errors.Is(err, errScanReconciliationEvidenceBudget) != errors.Is(wantErr, errScanReconciliationEvidenceBudget) ||
				errors.Is(err, errScanReconciliationEvidenceUnavailable) != errors.Is(wantErr, errScanReconciliationEvidenceUnavailable) {
				t.Fatalf("memoized ancestors changed the independent result: got=%v, %v, complete=%v; want=%v, %v", got, err, complete, want, wantErr)
			}
			if budget.bytes != 12345 || budget.items != len(known) {
				t.Fatalf("ancestor scratch displaced later proof capacity: %+v", budget)
			}
		})
	}
}

func TestScanReconciliationMemoBudgetFallsBackWithoutNewRejection(t *testing.T) {
	members, known := scanReconciliationAncestorTestFixture(100, 32)
	want, err := scanReconciliationAlbums(members, known)
	if err != nil {
		t.Fatal(err)
	}
	for _, remaining := range []int{0, scanReconciliationAncestorScratchBytes - 1,
		scanReconciliationAncestorScratchBytes + 3*scanReconciliationAncestorMemoBytes} {
		t.Run(fmt.Sprintf("remaining=%d", remaining), func(t *testing.T) {
			budget := &scanReconciliationBudgetState{bytes: scanReconciliationMaxBytes - remaining}
			before := budget.bytes
			if _, complete, err := scanReconciliationMemoizedAlbums(members, known, budget); complete || err != nil || budget.bytes != before {
				t.Fatalf("optional memo did not abandon its reservation: complete=%v, error=%v, budget=%+v", complete, err, budget)
			}
			got, err := scanReconciliationAlbumsWithBudget(members, known, budget)
			if err != nil || !reflect.DeepEqual(got, want) || budget.bytes != before {
				t.Fatalf("memo capacity changed the deletion result: got=%v, error=%v, budget=%+v", got, err, budget)
			}
		})
	}
}

func TestScanReconciliationSharedAncestorsReduceAllocations(t *testing.T) {
	members, known := scanReconciliationAncestorTestFixture(100, 1000)
	legacy := testing.AllocsPerRun(3, func() {
		if _, err := scanReconciliationAlbums(members, known); err != nil {
			t.Fatal(err)
		}
	})
	memoized := testing.AllocsPerRun(3, func() {
		if _, err := scanReconciliationAlbumsWithBudget(members, known, &scanReconciliationBudgetState{}); err != nil {
			t.Fatal(err)
		}
	})
	if memoized >= legacy {
		t.Fatalf("shared ancestor memo did not reduce allocations: memoized=%g, independent=%g", memoized, legacy)
	}
	t.Logf("allocations per shared ancestor batch: memoized=%g, independent=%g", memoized, legacy)
}

func BenchmarkScanReconciliationSharedAncestors(b *testing.B) {
	members, known := scanReconciliationAncestorTestFixture(100, 1000)
	for _, memoized := range []bool{false, true} {
		b.Run(fmt.Sprintf("memoized=%v", memoized), func(b *testing.B) {
			b.ReportAllocs()
			for index := 0; index < b.N; index++ {
				var err error
				if memoized {
					_, err = scanReconciliationAlbumsWithBudget(members, known, &scanReconciliationBudgetState{})
				} else {
					_, err = scanReconciliationAlbums(members, known)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestScanReconciliationCollectionClosureUsesOnlyVisibleSeedAncestry(t *testing.T) {
	known := map[string]scanReconciliationItem{
		"library":       {id: "library", libraryID: "library"},
		"hidden-parent": {id: "hidden-parent", libraryID: "library", parentID: "library"},
		"auxiliary":     {id: "auxiliary", libraryID: "library", parentID: "hidden-parent", themeOwner: "owner", visible: true},
		"sibling":       {id: "sibling", libraryID: "library", parentID: "hidden-parent", ordinary: true, visible: true},
		"owner":         {id: "owner", libraryID: "library", parentID: "owner-parent"},
		"owner-parent":  {id: "owner-parent", libraryID: "library"},
		"invisible":     {id: "invisible", libraryID: "library", parentID: "unrelated"},
		"unrelated":     {id: "unrelated", libraryID: "library"},
		"updated":       {id: "updated", libraryID: "library", parentID: "unrelated"},
	}
	changes := []CatalogChange{known["auxiliary"].fact(), known["sibling"].fact(), known["auxiliary"].fact(),
		{Kind: CatalogUpdated, ItemID: "updated", LibraryID: "library"}}
	if changes[0].ParentID != "" {
		t.Fatal("auxiliary fixture retained its notification parent")
	}
	budget := &scanReconciliationBudgetState{bytes: 9876}
	ids, reserved := scanReconciliationCollectionAncestors(changes, known, budget)
	slices.Sort(ids)
	want := []string{"auxiliary", "hidden-parent", "library", "sibling"}
	if !reflect.DeepEqual(ids, want) || reserved <= 0 || budget.bytes != 9876+reserved {
		t.Fatalf("collection closure expanded the wrong removal scope: got=%v, reserved=%d, budget=%+v", ids, reserved, budget)
	}
	budget.bytes -= reserved
	if budget.bytes != 9876 {
		t.Fatal("collection closure scratch was not releasable after its query")
	}
}

func TestScanReconciliationCollectionClosureStopsAtForeignLibrary(t *testing.T) {
	known := map[string]scanReconciliationItem{
		"seed":    {id: "seed", libraryID: "library", parentID: "foreign"},
		"foreign": {id: "foreign", libraryID: "other", parentID: "library"},
		"library": {id: "library", libraryID: "library"},
	}
	budget := &scanReconciliationBudgetState{}
	ids, reserved := scanReconciliationCollectionAncestors([]CatalogChange{known["seed"].fact()}, known, budget)
	if !reflect.DeepEqual(ids, []string{"seed"}) || reserved != budget.bytes {
		t.Fatalf("collection closure crossed a library edge: ids=%v, reserved=%d, budget=%+v", ids, reserved, budget)
	}
}

func TestScanReconciliationCollectionClosureBudgetAndIncompleteInputFallBack(t *testing.T) {
	members, known := scanReconciliationAncestorTestFixture(10, 1)
	changes := []CatalogChange{members["leaf-0000"].fact()}
	for _, remaining := range []int{0, scanReconciliationClosureBaseBytes - 1,
		scanReconciliationClosureBaseBytes + scanReconciliationClosureIDBytes + 4*len("leaf-0000")} {
		t.Run(fmt.Sprintf("remaining=%d", remaining), func(t *testing.T) {
			budget := &scanReconciliationBudgetState{bytes: scanReconciliationMaxBytes - remaining}
			before := budget.bytes
			if ids, reserved := scanReconciliationCollectionAncestors(changes, known, budget); ids != nil || reserved != 0 || budget.bytes != before {
				t.Fatalf("partial closure survived an optional budget failure: ids=%v, reserved=%d, budget=%+v", ids, reserved, budget)
			}
		})
	}
	for _, test := range []struct {
		name    string
		changes []CatalogChange
	}{
		{name: "empty"},
		{name: "missing seed", changes: []CatalogChange{{Kind: CatalogRemoved, ItemID: "missing", LibraryID: "library"}}},
		{name: "library deletion", changes: []CatalogChange{{Kind: CatalogRemoved, ItemID: "library", LibraryID: "library", IsCollectionFolder: true}}},
		{name: "non removal", changes: []CatalogChange{{Kind: CatalogUpdated, ItemID: "leaf-0000", LibraryID: "library"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			budget := &scanReconciliationBudgetState{bytes: 777}
			if ids, reserved := scanReconciliationCollectionAncestors(test.changes, known, budget); ids != nil || reserved != 0 || budget.bytes != 777 {
				t.Fatalf("ineligible closure bypassed the ordinary SQL path: ids=%v, reserved=%d, budget=%+v", ids, reserved, budget)
			}
		})
	}
	delete(known, "ancestor-000")
	budget := &scanReconciliationBudgetState{bytes: 777}
	if ids, reserved := scanReconciliationCollectionAncestors(changes, known, budget); ids != nil || reserved != 0 || budget.bytes != 777 {
		t.Fatalf("incomplete locked relation produced a partial SQL replacement: ids=%v, reserved=%d, budget=%+v", ids, reserved, budget)
	}
}
