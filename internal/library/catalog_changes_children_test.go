package library

import (
	"slices"
	"strings"
	"testing"
	"unsafe"
)

func TestCatalogChangesChildrenInvalidationRetainsIndependentFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change CatalogChange
	}{
		{"added children", CatalogChange{Kind: CatalogUpdated, ItemID: "folder", LibraryID: "library", ParentID: "parent", IsFolder: true, ChildrenAdded: true}},
		{"removed children", CatalogChange{Kind: CatalogUpdated, ItemID: "folder", LibraryID: "library", ParentID: "parent", IsFolder: true, ChildrenRemoved: true}},
		{"both directions", CatalogChange{Kind: CatalogUpdated, ItemID: "folder", LibraryID: "library", ParentID: "parent", IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true}},
		{"collection root", CatalogChange{Kind: CatalogUpdated, ItemID: "library", LibraryID: "library", IsFolder: true, IsCollectionFolder: true, ChildrenAdded: true, ChildrenRemoved: true}},
		{"moved folder", CatalogChange{Kind: CatalogUpdated, ItemID: "folder", LibraryID: "library", ParentID: "new-parent", PreviousParentID: "old-parent", IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []CatalogChange{test.change}
			tx := &ownedTx{}
			if err := recordCatalogChanges(tx, input...); err != nil {
				t.Fatal(err)
			}
			expectedBytes := catalogChangeOverhead + len(test.change.ItemID) + len(test.change.LibraryID) +
				len(test.change.ParentID) + len(test.change.PreviousParentID)
			if tx.catalogChanges.bytes != expectedBytes {
				t.Fatalf("child invalidation charged %d bytes, want %d", tx.catalogChanges.bytes, expectedBytes)
			}
			input[0] = CatalogChange{}
			notification := tx.catalogChanges.take()
			if notification.Resync || !slices.Equal(notification.Changes, []CatalogChange{test.change}) {
				t.Fatalf("caller reuse changed the retained folder facts: %+v", notification)
			}
			if tx.catalogChanges.bytes != 0 || tx.catalogChanges.changes != nil || tx.catalogChanges.resync {
				t.Fatal("taking a child invalidation retained the previous batch")
			}
			if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: "other", LibraryID: "library"}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(notification.Changes, []CatalogChange{test.change}) {
				t.Fatal("a later batch reused the published folder facts")
			}
		})
	}
}

func TestCatalogChangesChildrenInvalidationRejectsNonFolderUpdates(t *testing.T) {
	for _, flags := range []struct {
		name           string
		added, removed bool
	}{
		{"added", true, false},
		{"removed", false, true},
		{"both", true, true},
	} {
		for _, invalid := range []struct {
			name     string
			kind     CatalogChangeKind
			isFolder bool
		}{
			{"added folder", CatalogAdded, true},
			{"removed folder", CatalogRemoved, true},
			{"updated leaf", CatalogUpdated, false},
			{"unknown kind", CatalogChangeKind(255), true},
		} {
			t.Run(flags.name+"/"+invalid.name, func(t *testing.T) {
				tx := &ownedTx{}
				valid := CatalogChange{Kind: CatalogUpdated, ItemID: "valid", LibraryID: "library"}
				change := CatalogChange{Kind: invalid.kind, ItemID: "invalid", LibraryID: "library",
					IsFolder: invalid.isFolder, ChildrenAdded: flags.added, ChildrenRemoved: flags.removed}
				if err := recordCatalogChanges(tx, valid, change); err != nil {
					t.Fatal(err)
				}
				if notification := tx.catalogChanges.take(); !notification.Resync || notification.Changes != nil {
					t.Fatalf("invalid child membership facts retained a partial publication: %+v", notification)
				}
			})
		}
	}
}

func TestCatalogChangesChildrenInvalidationPreservesByteAndCountLimits(t *testing.T) {
	// Four flags fit in the already charged fixed structure storage. This
	// assertion protects that bound if future fields enlarge the structure.
	if unsafe.Sizeof(CatalogChange{}) > uintptr(catalogChangeOverhead) {
		t.Fatal("catalog facts exceed their accounted fixed storage")
	}
	change := CatalogChange{Kind: CatalogUpdated, ItemID: strings.Repeat("i", 256),
		LibraryID: strings.Repeat("l", 256), ParentID: strings.Repeat("p", 256), IsFolder: true,
		ChildrenAdded: true, ChildrenRemoved: true}
	bytesPerChange := catalogChangeOverhead + len(change.ItemID) + len(change.LibraryID) + len(change.ParentID)
	byteBoundCount := maxCatalogChangeBytes / bytesPerChange
	if byteBoundCount >= maxCatalogChanges {
		t.Fatal("the byte-bound fixture does not precede the count bound")
	}
	changes := make([]CatalogChange, byteBoundCount)
	for index := range changes {
		changes[index] = change
	}
	tx := &ownedTx{}
	if err := recordCatalogChanges(tx, changes...); err != nil {
		t.Fatal(err)
	}
	if tx.catalogChanges.resync || tx.catalogChanges.bytes != byteBoundCount*bytesPerChange {
		t.Fatal("a child invalidation batch within its byte budget was rejected")
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	if notification := tx.catalogChanges.take(); !notification.Resync || notification.Changes != nil {
		t.Fatal("excess child invalidation bytes produced a partial publication")
	}
	change = CatalogChange{Kind: CatalogUpdated, ItemID: "folder", LibraryID: "library",
		IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true}
	changes = make([]CatalogChange, maxCatalogChanges)
	for index := range changes {
		changes[index] = change
	}
	if err := recordCatalogChanges(tx, changes...); err != nil {
		t.Fatal(err)
	}
	if tx.catalogChanges.resync || len(tx.catalogChanges.changes) != maxCatalogChanges {
		t.Fatal("a child invalidation batch at the fact-count limit was rejected")
	}
	if err := recordCatalogChanges(tx, change); err != nil {
		t.Fatal(err)
	}
	if notification := tx.catalogChanges.take(); !notification.Resync || notification.Changes != nil {
		t.Fatal("excess child invalidation facts produced a partial publication")
	}
}
