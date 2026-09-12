package library

import (
	"fmt"
	"slices"
	"testing"
)

func TestAuxiliaryCatalogChangesMergeOwnerMembershipWithCommittedMove(t *testing.T) {
	tx := &ownedTx{}
	moved := CatalogChange{Kind: CatalogUpdated, ItemID: "owner", LibraryID: "library", ParentID: "new-parent",
		PreviousParentID: "old-parent", IsFolder: true}
	if err := recordCatalogChanges(tx, moved); err != nil {
		t.Fatal(err)
	}
	membership := moved
	membership.PreviousParentID = ""
	membership.ChildrenRemoved = true
	added := membership
	added.ChildrenRemoved, added.ChildrenAdded = false, true
	if err := mergeAuxiliaryCatalogChanges(tx, []CatalogChange{membership, added}, false); err != nil {
		t.Fatal(err)
	}
	moved.ChildrenAdded, moved.ChildrenRemoved = true, true
	notification := tx.catalogChanges.take()
	if notification.Resync || !slices.Equal(notification.Changes, []CatalogChange{moved}) {
		t.Fatalf("owner invalidation discarded the move or duplicated its fact: %+v", notification)
	}
}

func TestAuxiliaryCatalogChangesPreserveNewOwnerAndRejectConflicts(t *testing.T) {
	initial := CatalogChange{Kind: CatalogAdded, ItemID: "owner", LibraryID: "library", ParentID: "parent", IsFolder: true}
	updated := initial
	updated.Kind, updated.ChildrenAdded = CatalogUpdated, true
	tx := &ownedTx{}
	if err := recordCatalogChanges(tx, initial); err != nil {
		t.Fatal(err)
	}
	if err := mergeAuxiliaryCatalogChanges(tx, []CatalogChange{updated}, false); err != nil {
		t.Fatal(err)
	}
	if got := tx.catalogChanges.take(); got.Resync || !slices.Equal(got.Changes, []CatalogChange{initial}) {
		t.Fatalf("a newly added owner lost its committed creation: %+v", got)
	}
	for _, scenario := range []string{"library", "parent", "kind", "shape", "invalid flags"} {
		t.Run(scenario, func(t *testing.T) {
			tx := &ownedTx{}
			if err := recordCatalogChanges(tx, initial); err != nil {
				t.Fatal(err)
			}
			conflict := updated
			switch scenario {
			case "library":
				conflict.LibraryID = "foreign"
			case "parent":
				conflict.ParentID = "different"
			case "kind":
				conflict.Kind, conflict.ChildrenAdded = CatalogRemoved, false
			case "shape":
				conflict.IsFolder, conflict.ChildrenAdded = false, false
			case "invalid flags":
				conflict.Kind = CatalogAdded
			}
			if err := mergeAuxiliaryCatalogChanges(tx, []CatalogChange{conflict}, false); err != nil {
				t.Fatal(err)
			}
			if got := tx.catalogChanges.take(); !got.Resync || got.Changes != nil {
				t.Fatalf("a conflicting producer fact became a partial notification: %+v", got)
			}
		})
	}
}

func TestAuxiliaryCatalogChangesWholeBatchLimitAndPriorResync(t *testing.T) {
	tx := &ownedTx{}
	changes := make([]CatalogChange, maxCatalogChanges+1)
	for index := range changes {
		changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("item-%04d", index), LibraryID: "library"}
	}
	if err := mergeAuxiliaryCatalogChanges(tx, changes, false); err != nil {
		t.Fatal(err)
	}
	if !tx.catalogChanges.resync || tx.catalogChanges.changes != nil {
		t.Fatal("overflow retained a partial auxiliary batch")
	}
	if err := mergeAuxiliaryCatalogChanges(tx, changes[:1], false); err != nil {
		t.Fatal(err)
	}
	if got := tx.catalogChanges.take(); !got.Resync || got.Changes != nil {
		t.Fatal("a later fact cleared the required resynchronization")
	}
	if err := mergeAuxiliaryCatalogChanges(&ownedTx{finished: true}, nil, false); err == nil {
		t.Fatal("a finished transaction accepted notification facts")
	}
}

func TestAuxiliaryCatalogChangesSeparateSemanticOwnersFromBrowseParents(t *testing.T) {
	item := auxiliaryCatalogItem{change: CatalogChange{ItemID: "resource", LibraryID: "library", ParentID: "movie"},
		visible: true, extraOwner: "movie"}
	for _, kind := range []CatalogChangeKind{CatalogAdded, CatalogUpdated, CatalogRemoved} {
		fact := item.fact(kind)
		if fact.ParentID != "" || fact.Kind != kind || fact.LibraryID != "library" || fact.ItemID != "resource" {
			t.Fatalf("a resource manufactured an ordinary Movie container: %+v", fact)
		}
	}
	item.ordinary = true
	if fact := item.fact(CatalogRemoved); fact.ParentID != "movie" {
		t.Fatal("an ordinary logical removal lost its previous browse container")
	}
}
