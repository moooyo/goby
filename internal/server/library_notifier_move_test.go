package server

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func TestCatalogNotificationEnvelopePreservesMovedItemAndBothParentScopes(t *testing.T) {
	move := library.CatalogChange{Kind: library.CatalogUpdated, ItemID: "movie", LibraryID: "library",
		ParentID: "destination", PreviousParentID: "source"}
	envelope, err := catalogNotificationEnvelope(library.CatalogNotification{Changes: []library.CatalogChange{move, move}})
	if err != nil {
		t.Fatal(err)
	}
	want := libraryNotifierEmptyData()
	want.ItemsUpdated = []string{"movie"}
	want.FoldersRemovedFrom = []string{"source"}
	want.FoldersAddedTo = []string{"destination"}
	assertLibraryNotifierData(t, envelope, want)
	if !slices.Equal(envelope.CatalogScopes, []events.CatalogScope{
		{ItemID: "movie", LibraryID: "library"},
		{ItemID: "source", LibraryID: "library"},
		{ItemID: "destination", LibraryID: "library"},
	}) {
		t.Fatalf("move lost its trusted parent scopes: %+v", envelope.CatalogScopes)
	}
	// Historical membership still requires a directory when the same batch
	// supplies that parent's role; it cannot turn a known leaf into a container.
	invalid := library.CatalogNotification{Changes: []library.CatalogChange{move,
		{Kind: library.CatalogUpdated, ItemID: "source", LibraryID: "library"}}}
	if envelope, err := catalogNotificationEnvelope(invalid); !errors.Is(err, events.ErrResyncRequired) || len(envelope.Data) != 0 {
		t.Fatalf("move accepted a known non-folder source: %+v, %v", envelope, err)
	}
}

func TestCatalogNotificationEnvelopeRejectsInvalidMoveFacts(t *testing.T) {
	valid := library.CatalogChange{Kind: library.CatalogUpdated, ItemID: "item", LibraryID: "library",
		ParentID: "new-parent", PreviousParentID: "old-parent"}
	for _, test := range []struct {
		name string
		edit func(*library.CatalogChange)
	}{
		{"added item", func(c *library.CatalogChange) { c.Kind = library.CatalogAdded }},
		{"removed item", func(c *library.CatalogChange) { c.Kind = library.CatalogRemoved }},
		{"missing destination", func(c *library.CatalogChange) { c.ParentID = "" }},
		{"unchanged parent", func(c *library.CatalogChange) { c.PreviousParentID = c.ParentID }},
		{"self source", func(c *library.CatalogChange) { c.PreviousParentID = c.ItemID }},
		{"self destination", func(c *library.CatalogChange) { c.ParentID = c.ItemID }},
		{"invalid old parent", func(c *library.CatalogChange) { c.PreviousParentID = "old\x00parent" }},
		{"oversized old parent", func(c *library.CatalogChange) { c.PreviousParentID = strings.Repeat("p", 257) }},
		{"collection root", func(c *library.CatalogChange) { c.ItemID = c.LibraryID; c.IsFolder = true; c.IsCollectionFolder = true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			change := valid
			test.edit(&change)
			envelope, err := catalogNotificationEnvelope(library.CatalogNotification{Changes: []library.CatalogChange{change}})
			if !errors.Is(err, events.ErrResyncRequired) || len(envelope.Data) != 0 {
				t.Fatalf("invalid move was published: %+v, %v", envelope, err)
			}
		})
	}
}

func TestLibraryNotifierMoveParentBytesAreIncludedInBatchBudget(t *testing.T) {
	move := library.CatalogChange{Kind: library.CatalogUpdated, ItemID: strings.Repeat("i", 256),
		LibraryID: strings.Repeat("l", 256), ParentID: strings.Repeat("n", 256), PreviousParentID: strings.Repeat("o", 256)}
	changes := make([]library.CatalogChange, 64)
	for index := range changes {
		changes[index] = move
	}
	if _, valid := catalogNotificationSize(library.CatalogNotification{Changes: changes}); valid {
		t.Fatal("the batch ignored the retained previous parent strings")
	}
	for index := range changes {
		changes[index].PreviousParentID = ""
	}
	if _, valid := catalogNotificationSize(library.CatalogNotification{Changes: changes}); !valid {
		t.Fatal("the control batch without old parent strings must fit")
	}
}
