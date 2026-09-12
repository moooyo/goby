package library

import (
	"strings"
	"testing"
)

func TestCatalogChangesRejectInvalidMoveParents(t *testing.T) {
	valid := CatalogChange{Kind: CatalogUpdated, ItemID: "item", LibraryID: "library",
		ParentID: "new-parent", PreviousParentID: "old-parent"}
	for _, test := range []struct {
		name string
		edit func(*CatalogChange)
	}{
		{"added item", func(c *CatalogChange) { c.Kind = CatalogAdded }},
		{"removed item", func(c *CatalogChange) { c.Kind = CatalogRemoved }},
		{"missing destination", func(c *CatalogChange) { c.ParentID = "" }},
		{"unchanged parent", func(c *CatalogChange) { c.PreviousParentID = c.ParentID }},
		{"self source", func(c *CatalogChange) { c.PreviousParentID = c.ItemID }},
		{"self destination", func(c *CatalogChange) { c.ParentID = c.ItemID }},
		{"invalid old parent", func(c *CatalogChange) { c.PreviousParentID = "old\x00parent" }},
		{"oversized old parent", func(c *CatalogChange) { c.PreviousParentID = strings.Repeat("p", 257) }},
		{"collection root", func(c *CatalogChange) { c.ItemID = c.LibraryID; c.IsFolder = true; c.IsCollectionFolder = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			change := valid
			test.edit(&change)
			tx := &ownedTx{}
			if err := recordCatalogChanges(tx, valid, change); err != nil {
				t.Fatal(err)
			}
			if notification := tx.catalogChanges.take(); !notification.Resync || notification.Changes != nil {
				t.Fatalf("invalid move retained a partial batch: %+v", notification)
			}
		})
	}
	if !validCatalogChange(valid) {
		t.Fatal("a valid same-library move was rejected")
	}
}
