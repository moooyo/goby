package server

import (
	"errors"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func TestLibraryNotifierChildrenFlagsProjectFolderAndCollectionChanges(t *testing.T) {
	for _, test := range []struct {
		name    string
		changes []library.CatalogChange
		added   []string
		removed []string
		updated []string
		scopes  []events.CatalogScope
	}{
		{
			name: "children added",
			changes: []library.CatalogChange{{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library",
				ParentID: "parent", IsFolder: true, ChildrenAdded: true}},
			added: []string{"folder"}, removed: []string{}, updated: []string{"folder"},
			scopes: []events.CatalogScope{{ItemID: "folder", LibraryID: "library"}},
		},
		{
			name: "children removed",
			changes: []library.CatalogChange{{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library",
				ParentID: "parent", IsFolder: true, ChildrenRemoved: true}},
			added: []string{}, removed: []string{"folder"}, updated: []string{"folder"},
			scopes: []events.CatalogScope{{ItemID: "folder", LibraryID: "library"}},
		},
		{
			name: "both directions and identical duplicate",
			changes: []library.CatalogChange{
				{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library", ParentID: "parent",
					IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true},
				{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library", ParentID: "parent",
					IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true},
			},
			added: []string{"folder"}, removed: []string{"folder"}, updated: []string{"folder"},
			scopes: []events.CatalogScope{{ItemID: "folder", LibraryID: "library"}},
		},
		{
			name: "collection root",
			changes: []library.CatalogChange{{Kind: library.CatalogUpdated, ItemID: "library", LibraryID: "library",
				IsFolder: true, IsCollectionFolder: true, ChildrenAdded: true, ChildrenRemoved: true}},
			added: []string{"library"}, removed: []string{"library"}, updated: []string{"library"},
			scopes: []events.CatalogScope{{ItemID: "library", LibraryID: "library"}},
		},
		{
			name: "moved folder and changed children",
			changes: []library.CatalogChange{{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library",
				ParentID: "new-parent", PreviousParentID: "old-parent", IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true}},
			added: []string{"new-parent", "folder"}, removed: []string{"old-parent", "folder"}, updated: []string{"folder"},
			scopes: []events.CatalogScope{{ItemID: "folder", LibraryID: "library"},
				{ItemID: "old-parent", LibraryID: "library"}, {ItemID: "new-parent", LibraryID: "library"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope, err := catalogNotificationEnvelope(library.CatalogNotification{Changes: test.changes})
			if err != nil {
				t.Fatal(err)
			}
			want := libraryNotifierEmptyData()
			want.FoldersAddedTo, want.FoldersRemovedFrom, want.ItemsUpdated = test.added, test.removed, test.updated
			assertLibraryNotifierData(t, envelope, want)
			if !slices.Equal(envelope.CatalogScopes, test.scopes) {
				t.Fatalf("child membership invalidation lost its exact trusted scopes: %+v, want %+v", envelope.CatalogScopes, test.scopes)
			}
		})
	}
}

func TestLibraryNotifierChildrenFlagsDeduplicateFieldsAndKeepLibraryScopes(t *testing.T) {
	envelope, err := catalogNotificationEnvelope(library.CatalogNotification{Changes: []library.CatalogChange{
		{Kind: library.CatalogUpdated, ItemID: "shared-folder", LibraryID: "library-a", IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true},
		{Kind: library.CatalogUpdated, ItemID: "shared-folder", LibraryID: "library-b", IsFolder: true, ChildrenAdded: true, ChildrenRemoved: true},
		{Kind: library.CatalogUpdated, ItemID: "private-folder", LibraryID: "library-b", IsFolder: true, ChildrenAdded: true},
		{Kind: library.CatalogAdded, ItemID: "new-item", LibraryID: "library-a", ParentID: "shared-folder"},
		{Kind: library.CatalogRemoved, ItemID: "old-item", LibraryID: "library-a", ParentID: "shared-folder"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := libraryNotifierEmptyData()
	want.FoldersAddedTo = []string{"shared-folder", "private-folder"}
	want.FoldersRemovedFrom = []string{"shared-folder"}
	want.ItemsUpdated = []string{"shared-folder", "private-folder"}
	want.ItemsAdded, want.ItemsRemoved, want.CollectionFolders = []string{"new-item"}, []string{"old-item"}, []string{"library-a"}
	assertLibraryNotifierData(t, envelope, want)
	wantScopes := []events.CatalogScope{
		{ItemID: "shared-folder", LibraryID: "library-a"},
		{ItemID: "shared-folder", LibraryID: "library-b"},
		{ItemID: "private-folder", LibraryID: "library-b"},
		{ItemID: "new-item", LibraryID: "library-a"},
		{ItemID: "library-a", LibraryID: "library-a"},
		{ItemID: "old-item", LibraryID: "library-a"},
	}
	if !slices.Equal(envelope.CatalogScopes, wantScopes) {
		t.Fatalf("field deduplication widened or dropped trusted library scopes: %+v", envelope.CatalogScopes)
	}
}

func TestLibraryNotifierChildrenFlagsRejectInvalidKindsAndLeafFacts(t *testing.T) {
	for _, test := range []struct {
		name   string
		change library.CatalogChange
	}{
		{"added folder children added", library.CatalogChange{Kind: library.CatalogAdded, IsFolder: true, ChildrenAdded: true}},
		{"added folder children removed", library.CatalogChange{Kind: library.CatalogAdded, IsFolder: true, ChildrenRemoved: true}},
		{"removed folder children added", library.CatalogChange{Kind: library.CatalogRemoved, IsFolder: true, ChildrenAdded: true}},
		{"removed folder children removed", library.CatalogChange{Kind: library.CatalogRemoved, IsFolder: true, ChildrenRemoved: true}},
		{"added leaf both directions", library.CatalogChange{Kind: library.CatalogAdded, ChildrenAdded: true, ChildrenRemoved: true}},
		{"removed leaf both directions", library.CatalogChange{Kind: library.CatalogRemoved, ChildrenAdded: true, ChildrenRemoved: true}},
		{"updated leaf children added", library.CatalogChange{Kind: library.CatalogUpdated, ChildrenAdded: true}},
		{"updated leaf children removed", library.CatalogChange{Kind: library.CatalogUpdated, ChildrenRemoved: true}},
		{"updated leaf both directions", library.CatalogChange{Kind: library.CatalogUpdated, ChildrenAdded: true, ChildrenRemoved: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			change := test.change
			change.ItemID, change.LibraryID, change.ParentID = "item", "library", "parent"
			notification := library.CatalogNotification{Changes: []library.CatalogChange{change}}
			if _, valid := catalogNotificationSize(notification); valid {
				t.Fatal("invalid child membership flags passed the incoming batch validation")
			}
			if envelope, err := catalogNotificationEnvelope(notification); !errors.Is(err, events.ErrResyncRequired) ||
				len(envelope.Data) != 0 || len(envelope.CatalogScopes) != 0 {
				t.Fatalf("invalid child membership facts produced partial frame data: %+v, %v", envelope, err)
			}
		})
	}
}

func TestLibraryNotifierChildrenFlagsConflictRequiresWholeBatchResync(t *testing.T) {
	notifier, _, hub, start := newPausedLibraryNotifier(t)
	ordinary := subscribeNotifierUser(t, hub, "viewer", "session")
	application := subscribeLibraryNotifierApplication(t, hub, "application")
	notification := library.CatalogNotification{Changes: []library.CatalogChange{
		{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library", IsFolder: true, ChildrenAdded: true},
		{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library", IsFolder: true, ChildrenRemoved: true},
	}}
	if _, valid := catalogNotificationSize(notification); !valid {
		t.Fatal("individually valid conflicting flags must reach the whole-batch check")
	}
	if envelope, err := catalogNotificationEnvelope(notification); !errors.Is(err, events.ErrResyncRequired) ||
		len(envelope.Data) != 0 || len(envelope.CatalogScopes) != 0 {
		t.Fatalf("conflicting same-key flags were silently combined: %+v, %v", envelope, err)
	}
	if count, err := hub.PublishAll(events.Envelope{MessageType: "PreviouslyQueued"}); err != nil || count != 2 {
		t.Fatalf("queue the prior publication: %d, %v", count, err)
	}
	notifier.Enqueue(notification)
	start()
	assertLibraryNotifierResync(t, ordinary)
	assertLibraryNotifierResync(t, application)
	assertLibraryNotifierQueueEmpty(t, notifier)
}

func TestLibraryNotifierChildrenFlagsCopyAndRetainExistingByteBudget(t *testing.T) {
	change := library.CatalogChange{Kind: library.CatalogUpdated, ItemID: "folder", LibraryID: "library",
		ParentID: "new-parent", PreviousParentID: "old-parent", IsFolder: true}
	withoutFlags, valid := catalogNotificationSize(library.CatalogNotification{Changes: []library.CatalogChange{change}})
	if !valid {
		t.Fatal("the unflagged folder fixture is invalid")
	}
	change.ChildrenAdded, change.ChildrenRemoved = true, true
	notification := library.CatalogNotification{Changes: []library.CatalogChange{change}}
	withFlags, valid := catalogNotificationSize(notification)
	wantBytes := 32 + libraryNotificationChangeBytes + len(change.ItemID) + len(change.LibraryID) + len(change.ParentID) + len(change.PreviousParentID)
	if !valid || withoutFlags != wantBytes || withFlags != withoutFlags || libraryNotificationChangeBytes != 80 {
		t.Fatalf("boolean flags changed the retained string budget: without=%d with=%d want=%d", withoutFlags, withFlags, wantBytes)
	}
	notifier, _, hub, start := newPausedLibraryNotifier(t)
	sub := subscribeNotifierUser(t, hub, "viewer", "session")
	notifier.Enqueue(notification)
	notification.Changes[0].ChildrenAdded, notification.Changes[0].ChildrenRemoved = false, false
	notification.Changes[0].ItemID, notification.Changes[0].LibraryID = "caller-item", "caller-library"
	notifier.mu.Lock()
	count, retainedBytes := notifier.count, notifier.bytes
	notifier.mu.Unlock()
	if count != 1 || retainedBytes != wantBytes {
		t.Fatalf("queued child membership facts have incorrect accounting: count=%d bytes=%d", count, retainedBytes)
	}
	start()
	event := nextLibraryNotifierEvent(t, sub)
	want := libraryNotifierEmptyData()
	want.ItemsUpdated = []string{"folder"}
	want.FoldersAddedTo, want.FoldersRemovedFrom = []string{"new-parent", "folder"}, []string{"old-parent", "folder"}
	assertLibraryNotifierData(t, decodeLibraryNotifierEvent(t, event), want)
	wantScopes := []events.CatalogScope{{ItemID: "folder", LibraryID: "library"},
		{ItemID: "old-parent", LibraryID: "library"}, {ItemID: "new-parent", LibraryID: "library"}}
	if !slices.Equal(event.CatalogScopes(), wantScopes) {
		t.Fatalf("caller mutation changed child membership authority: %+v", event.CatalogScopes())
	}
	notifier.Close()
	assertLibraryNotifierQueueEmpty(t, notifier)
	assertLibraryNotifierQuiet(t, sub)
}
