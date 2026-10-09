package library

import (
	"fmt"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/notificationjournal"
)

func TestNotificationReferencesPreserveFullScopeAndFirstInsertionOrder(t *testing.T) {
	tx := new(ownedTx)
	tx.rememberNotificationScope("library-a", "first", "parent", "parent")
	tx.rememberNotificationScope("library-a", "first", "parent")
	tx.rememberNotificationScope("library-a", "second", "parent")
	tx.rememberNotificationReferences(
		notificationjournal.Reference{Kind: "Library", ID: "parent", LibraryID: "library-a"},
		notificationjournal.Reference{Kind: "Item", ID: "parent", LibraryID: "library-b", SourceID: "first"},
		notificationjournal.Reference{Kind: "Item", ID: "parent", LibraryID: "library-a", SourceID: "first"},
	)
	want := []notificationjournal.Reference{
		{Kind: "Item", ID: "first", LibraryID: "library-a", SourceID: "first"},
		{Kind: "Item", ID: "parent", LibraryID: "library-a", SourceID: "first"},
		{Kind: "Item", ID: "second", LibraryID: "library-a", SourceID: "second"},
		{Kind: "Item", ID: "parent", LibraryID: "library-a", SourceID: "second"},
		{Kind: "Library", ID: "parent", LibraryID: "library-a"},
		{Kind: "Item", ID: "parent", LibraryID: "library-b", SourceID: "first"},
	}
	if !slices.Equal(tx.notificationReferences, want) {
		t.Fatalf("reference order or source scopes changed: %+v", tx.notificationReferences)
	}
	if len(tx.notificationReferenceSet) != len(want) {
		t.Fatal("the transaction reference index diverged from its ordered references")
	}
}

func TestNotificationReferencesRetainExactOverflowSentinel(t *testing.T) {
	tx := new(ownedTx)
	for index := 0; index < 4096; index++ {
		id := fmt.Sprintf("item-%04d", index)
		tx.rememberNotificationScope("library", id)
	}
	tx.rememberNotificationScope("library", "item-0000")
	if len(tx.notificationReferences) != 4096 {
		t.Fatal("an exact duplicate overflowed the legal reference limit")
	}
	overflow := notificationjournal.Reference{Kind: "Library", ID: "library", LibraryID: "library"}
	tx.rememberNotificationReferences(overflow)
	before := slices.Clone(tx.notificationReferences)
	changes := make([]CatalogChange, 32768)
	for index := range changes {
		changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("late-%05d", index), LibraryID: "library", ParentID: "parent"}
	}
	tx.rememberNotificationChanges(changes)
	tx.rememberNotificationReferences(notificationjournal.Reference{Kind: "Library", ID: "other", LibraryID: "other"})
	if len(tx.notificationReferenceSet) != 4097 || !slices.Equal(tx.notificationReferences, before) || tx.notificationReferences[4096] != overflow {
		t.Fatal("saturated collection changed its bounded overflow sentinel or retained later scopes")
	}
}

func TestNotificationReferencesBoundLargeCatalogBatches(t *testing.T) {
	for _, count := range []int{0, 1024, 32768} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			changes := make([]CatalogChange, count)
			for index := range changes {
				changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: fmt.Sprintf("item-%05d", index), LibraryID: "library", ParentID: "shared-parent"}
			}
			tx := new(ownedTx)
			tx.rememberNotificationChanges(changes)
			want := min(2*count, 4097)
			if len(tx.notificationReferences) != want || len(tx.notificationReferenceSet) != want {
				t.Fatalf("%d changes retained %d references and %d index entries, want %d", count, len(tx.notificationReferences), len(tx.notificationReferenceSet), want)
			}
			for index, ref := range tx.notificationReferences {
				source := changes[index/2].ItemID
				id := source
				if index%2 != 0 {
					id = "shared-parent"
				}
				if ref != (notificationjournal.Reference{Kind: "Item", ID: id, LibraryID: "library", SourceID: source}) {
					t.Fatalf("large batch lost first-insertion source order at reference %d", index)
				}
			}
		})
	}
}
