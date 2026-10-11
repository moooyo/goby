package server

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func TestCatalogNotificationEnvelopeRemovedIntermediates(t *testing.T) {
	for _, test := range []struct {
		name       string
		bridgeKind library.CatalogChangeKind
		topKind    library.CatalogChangeKind
		removed    []string
		parents    []string
		updated    []string
		scopes     []events.CatalogScope
	}{
		{
			name: "surviving bridge preserves leaf", bridgeKind: library.CatalogUpdated, topKind: library.CatalogRemoved,
			removed: []string{"leaf", "top"}, parents: []string{"bridge", "outside"}, updated: []string{"bridge"},
			scopes: []events.CatalogScope{{ItemID: "leaf", LibraryID: "library"}, {ItemID: "bridge", LibraryID: "library"},
				{ItemID: "top", LibraryID: "library"}, {ItemID: "outside", LibraryID: "library"}},
		},
		{
			name: "surviving top does not restore leaf", bridgeKind: library.CatalogRemoved, topKind: library.CatalogUpdated,
			removed: []string{"bridge"}, parents: []string{"top"}, updated: []string{"top"},
			scopes: []events.CatalogScope{{ItemID: "bridge", LibraryID: "library"}, {ItemID: "top", LibraryID: "library"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			notification := library.CatalogNotification{Changes: []library.CatalogChange{
				{Kind: library.CatalogRemoved, ItemID: "leaf", LibraryID: "library", ParentID: "bridge"},
				{Kind: test.bridgeKind, ItemID: "bridge", LibraryID: "library", ParentID: "top", IsFolder: true},
				{Kind: test.topKind, ItemID: "top", LibraryID: "library", ParentID: "outside", IsFolder: true},
			}}
			envelope, err := catalogNotificationEnvelope(notification)
			if err != nil {
				t.Fatal(err)
			}
			want := libraryNotifierEmptyData()
			want.ItemsRemoved, want.FoldersRemovedFrom, want.ItemsUpdated = test.removed, test.parents, test.updated
			assertLibraryNotifierData(t, envelope, want)
			if !slices.Equal(envelope.CatalogScopes, test.scopes) {
				t.Fatalf("intermediate parent scopes = %+v, want %+v", envelope.CatalogScopes, test.scopes)
			}
		})
	}
}

func TestCatalogNotificationEnvelopeSeparatesHistoricalAndLibraryParentGraphs(t *testing.T) {
	t.Run("previous parent is not a current ancestry edge", func(t *testing.T) {
		notification := library.CatalogNotification{Changes: []library.CatalogChange{
			{Kind: library.CatalogUpdated, ItemID: "alpha", LibraryID: "library", ParentID: "outside", PreviousParentID: "beta", IsFolder: true},
			{Kind: library.CatalogUpdated, ItemID: "beta", LibraryID: "library", ParentID: "alpha", IsFolder: true},
		}}
		envelope, err := catalogNotificationEnvelope(notification)
		if err != nil {
			t.Fatal(err)
		}
		want := libraryNotifierEmptyData()
		want.ItemsUpdated = []string{"alpha", "beta"}
		want.FoldersRemovedFrom, want.FoldersAddedTo = []string{"beta"}, []string{"outside"}
		assertLibraryNotifierData(t, envelope, want)
		if !slices.Equal(envelope.CatalogScopes, []events.CatalogScope{
			{ItemID: "alpha", LibraryID: "library"}, {ItemID: "beta", LibraryID: "library"}, {ItemID: "outside", LibraryID: "library"},
		}) {
			t.Fatalf("historical parent scopes = %+v", envelope.CatalogScopes)
		}
	})
	t.Run("shared names have separate completed paths", func(t *testing.T) {
		notification := library.CatalogNotification{Changes: []library.CatalogChange{
			{Kind: library.CatalogUpdated, ItemID: "alpha", LibraryID: "one", ParentID: "beta", IsFolder: true},
			{Kind: library.CatalogUpdated, ItemID: "beta", LibraryID: "one", ParentID: "outside", IsFolder: true},
			{Kind: library.CatalogUpdated, ItemID: "beta", LibraryID: "two", ParentID: "alpha", IsFolder: true},
			{Kind: library.CatalogUpdated, ItemID: "alpha", LibraryID: "two", ParentID: "outside", IsFolder: true},
		}}
		envelope, err := catalogNotificationEnvelope(notification)
		if err != nil {
			t.Fatal(err)
		}
		want := libraryNotifierEmptyData()
		want.ItemsUpdated = []string{"alpha", "beta"}
		assertLibraryNotifierData(t, envelope, want)
		if !slices.Equal(envelope.CatalogScopes, []events.CatalogScope{
			{ItemID: "alpha", LibraryID: "one"}, {ItemID: "beta", LibraryID: "one"},
			{ItemID: "beta", LibraryID: "two"}, {ItemID: "alpha", LibraryID: "two"},
		}) {
			t.Fatalf("shared-name scopes = %+v", envelope.CatalogScopes)
		}
	})
}

func TestCatalogNotificationEnvelopeRejectsCycleBehindSuppressedDescendants(t *testing.T) {
	notification := library.CatalogNotification{Changes: []library.CatalogChange{
		{Kind: library.CatalogUpdated, ItemID: "independent", LibraryID: "library"},
		{Kind: library.CatalogRemoved, ItemID: "leaf", LibraryID: "library", ParentID: "bridge"},
		{Kind: library.CatalogRemoved, ItemID: "bridge", LibraryID: "library", ParentID: "top", IsFolder: true},
		{Kind: library.CatalogUpdated, ItemID: "top", LibraryID: "library", ParentID: "bridge", IsFolder: true},
	}}
	if _, valid := catalogNotificationSize(notification); !valid {
		t.Fatal("the mixed-kind cycle must reach graph validation")
	}
	if envelope, err := catalogNotificationEnvelope(notification); !errors.Is(err, events.ErrResyncRequired) ||
		len(envelope.Data) != 0 || len(envelope.CatalogScopes) != 0 {
		t.Fatalf("a suppressed descendant concealed an invalid graph: %+v, %v", envelope, err)
	}
}

func TestCatalogNotificationEnvelopeSharedLongChains(t *testing.T) {
	changes := append(libraryNotifierSharedParentChanges(100, 150, "one"), libraryNotifierSharedParentChanges(100, 150, "two")...)
	for range 10 {
		changes = append(changes, changes[0])
	}
	changes = append(changes,
		library.CatalogChange{Kind: library.CatalogRemoved, ItemID: "independent-one", LibraryID: "one", ParentID: "external-one"},
		library.CatalogChange{Kind: library.CatalogRemoved, ItemID: "independent-two", LibraryID: "two", ParentID: "external-two"})
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse=%t", reverse), func(t *testing.T) {
			ordered := slices.Clone(changes)
			if reverse {
				slices.Reverse(ordered)
			}
			notification := library.CatalogNotification{Changes: ordered}
			if len(ordered) != libraryNotificationChanges {
				t.Fatalf("shared-chain fixture has %d changes, want %d", len(ordered), libraryNotificationChanges)
			}
			if _, valid := catalogNotificationSize(notification); !valid {
				t.Fatal("the shared-chain fixture must fit the raw batch budget")
			}
			byKey := libraryNotifierParentGraph(ordered)
			visits, err := validateCatalogChangeParents(byKey)
			if err != nil || visits != 502 {
				t.Fatalf("shared parent validation = (%d visits, %v), want 502 unique visits", visits, err)
			}
			oldVisits, err := libraryNotifierIndependentParentWalks(ordered, byKey)
			if err != nil || oldVisits <= visits*10 {
				t.Fatalf("the fixture did not exercise shared ancestry: old visits=%d, unique visits=%d, err=%v", oldVisits, visits, err)
			}
			envelope, err := catalogNotificationEnvelope(notification)
			if err != nil {
				t.Fatal(err)
			}
			want := libraryNotifierEmptyData()
			want.ItemsRemoved = []string{"folder-099", "independent-one", "independent-two"}
			want.FoldersRemovedFrom = []string{"outside", "external-one", "external-two"}
			wantScopes := []events.CatalogScope{
				{ItemID: "folder-099", LibraryID: "one"}, {ItemID: "outside", LibraryID: "one"},
				{ItemID: "folder-099", LibraryID: "two"}, {ItemID: "outside", LibraryID: "two"},
				{ItemID: "independent-one", LibraryID: "one"}, {ItemID: "external-one", LibraryID: "one"},
				{ItemID: "independent-two", LibraryID: "two"}, {ItemID: "external-two", LibraryID: "two"},
			}
			if reverse {
				slices.Reverse(want.ItemsRemoved)
				slices.Reverse(want.FoldersRemovedFrom)
				wantScopes = []events.CatalogScope{
					{ItemID: "independent-two", LibraryID: "two"}, {ItemID: "external-two", LibraryID: "two"},
					{ItemID: "independent-one", LibraryID: "one"}, {ItemID: "external-one", LibraryID: "one"},
					{ItemID: "folder-099", LibraryID: "two"}, {ItemID: "outside", LibraryID: "two"},
					{ItemID: "folder-099", LibraryID: "one"}, {ItemID: "outside", LibraryID: "one"},
				}
			}
			assertLibraryNotifierData(t, envelope, want)
			if !slices.Equal(envelope.CatalogScopes, wantScopes) {
				t.Fatalf("shared-chain scopes = %+v, want %+v", envelope.CatalogScopes, wantScopes)
			}
		})
	}
}

func TestValidateCatalogChangeParentsMatchesIndependentWalks(t *testing.T) {
	parents := []string{"", "outside", "alpha", "beta", "gamma"}
	for shape := range 125 {
		for folders := range 8 {
			for _, previousParent := range parents {
				changes := make([]library.CatalogChange, 3)
				encoded := shape
				for index, id := range []string{"alpha", "beta", "gamma"} {
					changes[index] = library.CatalogChange{Kind: library.CatalogUpdated, ItemID: id, LibraryID: "library",
						ParentID: parents[encoded%len(parents)], IsFolder: folders&(1<<index) != 0}
					encoded /= len(parents)
				}
				changes[0].PreviousParentID = previousParent
				byKey := libraryNotifierParentGraph(changes)
				visits, gotErr := validateCatalogChangeParents(byKey)
				_, wantErr := libraryNotifierIndependentParentWalks(changes, byKey)
				if !errors.Is(gotErr, wantErr) || (gotErr == nil && visits != len(byKey)) {
					t.Fatalf("shape=%d folders=%03b previous=%q: validation=(%d, %v), independent walks=%v",
						shape, folders, previousParent, visits, gotErr, wantErr)
				}
			}
		}
	}
}

func BenchmarkCatalogNotificationParentGraph(b *testing.B) {
	for _, fixture := range []struct {
		name   string
		depth  int
		leaves int
	}{
		{name: "chain-16", depth: 16},
		{name: "chain-100", depth: 100},
		{name: "shared-100-by-400", depth: 100, leaves: 400},
	} {
		changes := libraryNotifierSharedParentChanges(fixture.depth, fixture.leaves, "library")
		byKey := libraryNotifierParentGraph(changes)
		for _, method := range []struct {
			name string
			run  func() (int, error)
		}{
			{name: "independent-walks", run: func() (int, error) { return libraryNotifierIndependentParentWalks(changes, byKey) }},
			{name: "shared-graph", run: func() (int, error) { return validateCatalogChangeParents(byKey) }},
		} {
			b.Run(fixture.name+"/"+method.name, func(b *testing.B) {
				b.ReportAllocs()
				visits := 0
				for index := 0; index < b.N; index++ {
					var err error
					visits, err = method.run()
					if err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(visits), "graph-visits/op")
			})
		}
	}
}

func BenchmarkCatalogNotificationEnvelopeSharedParents(b *testing.B) {
	notification := library.CatalogNotification{Changes: libraryNotifierSharedParentChanges(100, 400, "library")}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := catalogNotificationEnvelope(notification); err != nil {
			b.Fatal(err)
		}
	}
}

func libraryNotifierSharedParentChanges(depth, leaves int, libraryID string) []library.CatalogChange {
	changes := make([]library.CatalogChange, 0, depth+leaves)
	for index := range leaves {
		changes = append(changes, library.CatalogChange{Kind: library.CatalogRemoved, ItemID: fmt.Sprintf("leaf-%03d", index),
			LibraryID: libraryID, ParentID: "folder-000"})
	}
	for index := range depth {
		parentID := "outside"
		if index+1 < depth {
			parentID = fmt.Sprintf("folder-%03d", index+1)
		}
		changes = append(changes, library.CatalogChange{Kind: library.CatalogRemoved, ItemID: fmt.Sprintf("folder-%03d", index),
			LibraryID: libraryID, ParentID: parentID, IsFolder: true})
	}
	return changes
}

func libraryNotifierParentGraph(changes []library.CatalogChange) map[catalogChangeKey]library.CatalogChange {
	byKey := make(map[catalogChangeKey]library.CatalogChange, len(changes))
	for _, change := range changes {
		byKey[catalogChangeKey{change.ItemID, change.LibraryID}] = change
	}
	return byKey
}

// Retain the original independent walks as a differential and work-count
// reference. Production validation instead shares completed ancestor paths.
func libraryNotifierIndependentParentWalks(changes []library.CatalogChange, byKey map[catalogChangeKey]library.CatalogChange) (int, error) {
	visits := 0
	for _, change := range changes {
		visits++
		if previous, exists := byKey[catalogChangeKey{change.PreviousParentID, change.LibraryID}]; change.PreviousParentID != "" && exists && !previous.IsFolder {
			return visits, events.ErrResyncRequired
		}
		seen := map[string]bool{change.ItemID: true}
		for parentID := change.ParentID; parentID != ""; {
			if seen[parentID] {
				return visits, events.ErrResyncRequired
			}
			seen[parentID] = true
			parent, exists := byKey[catalogChangeKey{parentID, change.LibraryID}]
			if !exists {
				break
			}
			visits++
			if !parent.IsFolder {
				return visits, events.ErrResyncRequired
			}
			parentID = parent.ParentID
		}
	}
	return visits, nil
}
