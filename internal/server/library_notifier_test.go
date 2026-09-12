package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func TestCatalogNotificationEnvelopePreservesOrderedFieldsAndTrustedScopes(t *testing.T) {
	added := library.CatalogChange{Kind: library.CatalogAdded, ItemID: "movie-b", LibraryID: "library-one", ParentID: "folder-p"}
	removed := library.CatalogChange{Kind: library.CatalogRemoved, ItemID: "removed-z", LibraryID: "library-one", ParentID: "removed-parent"}
	changes := []library.CatalogChange{
		added, added,
		{Kind: library.CatalogAdded, ItemID: "movie-a", LibraryID: "library-two", ParentID: "shared-parent"},
		{Kind: library.CatalogAdded, ItemID: "movie-c", LibraryID: "library-one", ParentID: "folder-p"},
		{Kind: library.CatalogAdded, ItemID: "shared-item", LibraryID: "library-one", ParentID: "folder-p"},
		{Kind: library.CatalogAdded, ItemID: "shared-item", LibraryID: "library-two", ParentID: "shared-parent"},
		{Kind: library.CatalogAdded, ItemID: "library-three", LibraryID: "library-three", IsFolder: true, IsCollectionFolder: true},
		{Kind: library.CatalogUpdated, ItemID: "updated-z", LibraryID: "library-one"},
		{Kind: library.CatalogUpdated, ItemID: "updated-a", LibraryID: "library-two"},
		{Kind: library.CatalogUpdated, ItemID: "updated-z", LibraryID: "library-one"},
		removed,
		{Kind: library.CatalogRemoved, ItemID: "removed-a", LibraryID: "library-two", ParentID: "removed-parent"},
		removed,
	}
	envelope, err := catalogNotificationEnvelope(library.CatalogNotification{Changes: changes})
	if err != nil {
		t.Fatal(err)
	}
	want := libraryNotifierEmptyData()
	want.FoldersAddedTo = []string{"folder-p", "shared-parent"}
	want.FoldersRemovedFrom = []string{"removed-parent"}
	want.ItemsAdded = []string{"movie-b", "movie-a", "movie-c", "shared-item", "library-three"}
	want.ItemsRemoved = []string{"removed-z", "removed-a"}
	want.ItemsUpdated = []string{"updated-z", "updated-a"}
	want.CollectionFolders = []string{"library-one", "library-two", "library-three"}
	assertLibraryNotifierData(t, envelope, want)
	wantScopes := []events.CatalogScope{
		{ItemID: "movie-b", LibraryID: "library-one"},
		{ItemID: "library-one", LibraryID: "library-one"},
		{ItemID: "folder-p", LibraryID: "library-one"},
		{ItemID: "movie-a", LibraryID: "library-two"},
		{ItemID: "library-two", LibraryID: "library-two"},
		{ItemID: "shared-parent", LibraryID: "library-two"},
		{ItemID: "movie-c", LibraryID: "library-one"},
		{ItemID: "shared-item", LibraryID: "library-one"},
		{ItemID: "shared-item", LibraryID: "library-two"},
		{ItemID: "library-three", LibraryID: "library-three"},
		{ItemID: "updated-z", LibraryID: "library-one"},
		{ItemID: "updated-a", LibraryID: "library-two"},
		{ItemID: "removed-z", LibraryID: "library-one"},
		{ItemID: "removed-parent", LibraryID: "library-one"},
		{ItemID: "removed-a", LibraryID: "library-two"},
		{ItemID: "removed-parent", LibraryID: "library-two"},
	}
	if !slices.Equal(envelope.CatalogScopes, wantScopes) {
		t.Fatalf("trusted scopes = %+v, want %+v", envelope.CatalogScopes, wantScopes)
	}
	if envelope.MessageID != "" {
		t.Fatal("the batch encoder generated a recipient-specific publication identity")
	}
}

func TestCatalogNotificationEnvelopeSuppressesOnlyRemovedAncestorsInTheSameLibrary(t *testing.T) {
	for _, test := range []struct {
		name    string
		changes []library.CatalogChange
		removed []string
		parents []string
		scopes  []events.CatalogScope
	}{
		{
			name: "highest ancestor and independent shared item",
			changes: []library.CatalogChange{
				{Kind: library.CatalogRemoved, ItemID: "leaf", LibraryID: "one", ParentID: "branch"},
				{Kind: library.CatalogRemoved, ItemID: "leaf", LibraryID: "two", ParentID: "branch"},
				{Kind: library.CatalogRemoved, ItemID: "branch", LibraryID: "one", ParentID: "top", IsFolder: true},
				{Kind: library.CatalogRemoved, ItemID: "top", LibraryID: "one", ParentID: "surviving-parent", IsFolder: true},
			},
			removed: []string{"leaf", "top"}, parents: []string{"branch", "surviving-parent"},
			scopes: []events.CatalogScope{{ItemID: "leaf", LibraryID: "two"}, {ItemID: "branch", LibraryID: "two"},
				{ItemID: "top", LibraryID: "one"}, {ItemID: "surviving-parent", LibraryID: "one"}},
		},
		{
			name: "removed collection root",
			changes: []library.CatalogChange{
				{Kind: library.CatalogRemoved, ItemID: "child", LibraryID: "collection", ParentID: "collection"},
				{Kind: library.CatalogRemoved, ItemID: "collection", LibraryID: "collection", IsFolder: true, IsCollectionFolder: true},
			},
			removed: []string{"collection"}, parents: []string{},
			scopes: []events.CatalogScope{{ItemID: "collection", LibraryID: "collection"}},
		},
		{
			name: "cross-library names do not form a cycle",
			changes: []library.CatalogChange{
				{Kind: library.CatalogRemoved, ItemID: "alpha", LibraryID: "one", ParentID: "beta", IsFolder: true},
				{Kind: library.CatalogRemoved, ItemID: "beta", LibraryID: "two", ParentID: "alpha", IsFolder: true},
			},
			removed: []string{"alpha", "beta"}, parents: []string{"beta", "alpha"},
			scopes: []events.CatalogScope{{ItemID: "alpha", LibraryID: "one"}, {ItemID: "beta", LibraryID: "one"},
				{ItemID: "beta", LibraryID: "two"}, {ItemID: "alpha", LibraryID: "two"}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			envelope, err := catalogNotificationEnvelope(library.CatalogNotification{Changes: test.changes})
			if err != nil {
				t.Fatal(err)
			}
			want := libraryNotifierEmptyData()
			want.ItemsRemoved, want.FoldersRemovedFrom = test.removed, test.parents
			assertLibraryNotifierData(t, envelope, want)
			if !slices.Equal(envelope.CatalogScopes, test.scopes) {
				t.Fatalf("removed scopes = %+v, want %+v", envelope.CatalogScopes, test.scopes)
			}
		})
	}
}

func TestCatalogNotificationEnvelopeRejectsAmbiguousFactsAndInvalidBounds(t *testing.T) {
	base := library.CatalogChange{Kind: library.CatalogUpdated, ItemID: "item", LibraryID: "library"}
	change := func(edit func(*library.CatalogChange)) library.CatalogNotification {
		value := base
		edit(&value)
		return library.CatalogNotification{Changes: []library.CatalogChange{value}}
	}
	tests := []struct {
		name         string
		notification library.CatalogNotification
	}{
		{"explicit resynchronization", library.CatalogNotification{Resync: true, Changes: []library.CatalogChange{base}}},
		{"unknown kind", change(func(value *library.CatalogChange) { value.Kind = 0 })},
		{"missing item", change(func(value *library.CatalogChange) { value.ItemID = "" })},
		{"missing library", change(func(value *library.CatalogChange) { value.LibraryID = "" })},
		{"padded item", change(func(value *library.CatalogChange) { value.ItemID = " item" })},
		{"control character", change(func(value *library.CatalogChange) { value.ParentID = "parent\x00" })},
		{"invalid UTF-8", change(func(value *library.CatalogChange) { value.LibraryID = string([]byte{0xff}) })},
		{"oversized identifier", change(func(value *library.CatalogChange) { value.ItemID = strings.Repeat("i", maxLibraryChangedIDBytes+1) })},
		{"self parent", change(func(value *library.CatalogChange) { value.ParentID = value.ItemID })},
		{"collection is not a folder", change(func(value *library.CatalogChange) { value.ItemID = value.LibraryID; value.IsCollectionFolder = true })},
		{"collection has a foreign identifier", change(func(value *library.CatalogChange) { value.IsFolder = true; value.IsCollectionFolder = true })},
		{"collection has a parent", change(func(value *library.CatalogChange) {
			value.ItemID = value.LibraryID
			value.IsFolder = true
			value.IsCollectionFolder = true
			value.ParentID = "parent"
		})},
		{"conflicting parent", library.CatalogNotification{Changes: []library.CatalogChange{base, {Kind: base.Kind, ItemID: base.ItemID, LibraryID: base.LibraryID, ParentID: "other-parent"}}}},
		{"conflicting role", library.CatalogNotification{Changes: []library.CatalogChange{base, {Kind: base.Kind, ItemID: base.ItemID, LibraryID: "another-library", IsFolder: true}}}},
		{"mixed kinds across libraries", library.CatalogNotification{Changes: []library.CatalogChange{base, {Kind: library.CatalogRemoved, ItemID: base.ItemID, LibraryID: "another-library"}}}},
		{"known parent is not a folder", library.CatalogNotification{Changes: []library.CatalogChange{base, {Kind: base.Kind, ItemID: "child", LibraryID: base.LibraryID, ParentID: base.ItemID}}}},
	}
	tooMany := make([]library.CatalogChange, libraryNotificationChanges+1)
	for index := range tooMany {
		tooMany[index] = base
	}
	tests = append(tests, struct {
		name         string
		notification library.CatalogNotification
	}{"too many changes", library.CatalogNotification{Changes: tooMany}})
	tooLarge := libraryNotifierLargeBatch()
	tooLarge.Changes = append(tooLarge.Changes, tooLarge.Changes[0])
	tests = append(tests, struct {
		name         string
		notification library.CatalogNotification
	}{"batch byte bound", tooLarge})
	for _, kind := range []library.CatalogChangeKind{library.CatalogAdded, library.CatalogUpdated, library.CatalogRemoved} {
		tests = append(tests, struct {
			name         string
			notification library.CatalogNotification
		}{fmt.Sprintf("parent cycle for kind %d", kind), library.CatalogNotification{Changes: []library.CatalogChange{
			{Kind: kind, ItemID: "alpha", LibraryID: "library", ParentID: "beta", IsFolder: true},
			{Kind: kind, ItemID: "beta", LibraryID: "library", ParentID: "alpha", IsFolder: true},
		}}})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if envelope, err := catalogNotificationEnvelope(test.notification); !errors.Is(err, events.ErrResyncRequired) || len(envelope.Data) != 0 || len(envelope.CatalogScopes) != 0 {
				t.Fatalf("unsafe batch encoded a partial event: %v", err)
			}
		})
	}
	if envelope, err := catalogNotificationEnvelope(library.CatalogNotification{}); err != nil || len(envelope.Data) != 0 || len(envelope.CatalogScopes) != 0 {
		t.Fatalf("empty committed batch = (%+v, %v)", envelope, err)
	}
}

func TestCatalogNotificationEnvelopeChecksEncodedAndScopeByteBudget(t *testing.T) {
	// JSON escaping expands these valid identifiers beyond the input budget's
	// raw string bytes, so the encoded publication needs an independent bound.
	changes := make([]library.CatalogChange, 110)
	for index := range changes {
		changes[index] = library.CatalogChange{Kind: library.CatalogAdded, LibraryID: "l",
			ItemID: strings.Repeat("\"", 250) + fmt.Sprintf("%06d", index), ParentID: strings.Repeat("p", 250) + fmt.Sprintf("%06d", index)}
	}
	notification := library.CatalogNotification{Changes: changes}
	if _, valid := catalogNotificationSize(notification); !valid {
		t.Fatal("encoded budget fixture must fit the incoming batch budget")
	}
	if envelope, err := catalogNotificationEnvelope(notification); !errors.Is(err, events.ErrResyncRequired) || len(envelope.Data) != 0 {
		t.Fatalf("encoded publication exceeded its retained byte budget without resynchronization: %v", err)
	}
}

func TestLibraryNotifierCopiesInputsAndPublishesBatchesInOrder(t *testing.T) {
	notifier, store, hub, start := newPausedLibraryNotifier(t)
	ordinary := subscribeNotifierUser(t, hub, "viewer", "viewer-session")
	application := subscribeLibraryNotifierApplication(t, hub, "application-session")
	callback := store.capture()
	if callback == nil {
		t.Fatal("notifier did not register its callback")
	}
	for index := 0; index < 3; index++ {
		notification := libraryNotifierUpdate(fmt.Sprintf("item-%d", index))
		callback(notification)
		notification.Changes[0] = library.CatalogChange{Kind: library.CatalogRemoved, ItemID: "mutated-item", LibraryID: "mutated-library", ParentID: "mutated-parent"}
	}
	start()
	identities := make(map[string]bool)
	for index := 0; index < 3; index++ {
		first := nextLibraryNotifierEvent(t, ordinary)
		second := nextLibraryNotifierEvent(t, application)
		if first.MessageID() == "" || identities[first.MessageID()] || first.MessageID() != second.MessageID() || !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("one batch did not retain one distinct publication identity across recipients")
		}
		identities[first.MessageID()] = true
		want := libraryNotifierEmptyData()
		want.ItemsUpdated = []string{fmt.Sprintf("item-%d", index)}
		assertLibraryNotifierData(t, decodeLibraryNotifierEvent(t, first), want)
		if scopes := first.CatalogScopes(); !slices.Equal(scopes, []events.CatalogScope{{ItemID: want.ItemsUpdated[0], LibraryID: "library"}}) {
			t.Fatalf("caller mutation changed publication scopes: %+v", scopes)
		}
	}
	notifier.Close()
	assertLibraryNotifierQuiet(t, ordinary)
	assertLibraryNotifierQuiet(t, application)
}

func TestLibraryNotifierQueueOverflowDisconnectsAllAndAcceptsFutureBatches(t *testing.T) {
	for _, mode := range []string{"count", "bytes"} {
		t.Run(mode, func(t *testing.T) {
			notifier, _, hub, start := newPausedLibraryNotifier(t)
			ordinary := subscribeNotifierUser(t, hub, "viewer", "viewer-session")
			application := subscribeLibraryNotifierApplication(t, hub, "application-session")
			if count, err := hub.PublishAll(events.Envelope{MessageType: "AlreadyQueued"}); err != nil || count != 2 {
				t.Fatalf("queue preexisting hub events = (%d, %v)", count, err)
			}
			notification := libraryNotifierUpdate("before-overflow")
			capacity := libraryNotificationCapacity
			if mode == "bytes" {
				notification = libraryNotifierLargeBatch()
				size, valid := catalogNotificationSize(notification)
				if !valid {
					t.Fatal("byte overflow fixture must be one valid batch")
				}
				capacity = libraryNotificationQueueBytes / size
				if capacity >= libraryNotificationCapacity {
					t.Fatal("byte overflow fixture reached the count limit first")
				}
			}
			for index := 0; index < capacity; index++ {
				notifier.Enqueue(notification)
			}
			notifier.Enqueue(library.CatalogNotification{})
			if ordinary.Reason() != nil || application.Reason() != nil {
				t.Fatal("queue rejected an entry within its budget or treated an empty callback as overflow")
			}
			notifier.Enqueue(notification)
			assertLibraryNotifierResync(t, ordinary)
			assertLibraryNotifierResync(t, application)
			assertLibraryNotifierQueueEmpty(t, notifier)
			freshOrdinary := subscribeNotifierUser(t, hub, "viewer", "viewer-session")
			freshApplication := subscribeLibraryNotifierApplication(t, hub, "application-session")
			notifier.Enqueue(libraryNotifierUpdate("after-overflow"))
			start()
			first := nextLibraryNotifierEvent(t, freshOrdinary)
			second := nextLibraryNotifierEvent(t, freshApplication)
			want := libraryNotifierEmptyData()
			want.ItemsUpdated = []string{"after-overflow"}
			assertLibraryNotifierData(t, decodeLibraryNotifierEvent(t, first), want)
			if !bytes.Equal(first.Bytes(), second.Bytes()) {
				t.Fatal("reconnected recipients did not share the future publication")
			}
			notifier.Close()
			assertLibraryNotifierQuiet(t, freshOrdinary)
			assertLibraryNotifierQuiet(t, freshApplication)
		})
	}
}

func TestLibraryNotifierAmbiguousBatchDisconnectsWithoutPublishingPartialData(t *testing.T) {
	notifier, _, hub, start := newPausedLibraryNotifier(t)
	ordinary := subscribeNotifierUser(t, hub, "viewer", "viewer-session")
	application := subscribeLibraryNotifierApplication(t, hub, "application-session")
	batch := library.CatalogNotification{Changes: []library.CatalogChange{
		{Kind: library.CatalogUpdated, ItemID: "item", LibraryID: "one"},
		{Kind: library.CatalogRemoved, ItemID: "item", LibraryID: "two"},
	}}
	if _, valid := catalogNotificationSize(batch); !valid {
		t.Fatal("ambiguous fixture must reach the worker's envelope validation")
	}
	notifier.Enqueue(batch)
	start()
	assertLibraryNotifierResync(t, ordinary)
	assertLibraryNotifierResync(t, application)
	assertLibraryNotifierQueueEmpty(t, notifier)
	fresh := subscribeNotifierUser(t, hub, "viewer", "fresh-session")
	notifier.Enqueue(libraryNotifierUpdate("after-invalid-batch"))
	want := libraryNotifierEmptyData()
	want.ItemsUpdated = []string{"after-invalid-batch"}
	assertLibraryNotifierData(t, decodeLibraryNotifierEvent(t, nextLibraryNotifierEvent(t, fresh)), want)
}

func TestLibraryNotifierPublicationFailureDisconnectsAndRecovers(t *testing.T) {
	hub, err := events.New(events.Options{MaxMessageBytes: 512})
	if err != nil {
		t.Fatal(err)
	}
	notifier := newLibraryNotifier(nil, hub)
	t.Cleanup(func() { notifier.Close(); _ = hub.Close() })
	ordinary := subscribeNotifierUser(t, hub, "viewer", "viewer-session")
	application := subscribeLibraryNotifierApplication(t, hub, "application-session")
	large := libraryNotifierUpdate(strings.Repeat("i", maxLibraryChangedIDBytes))
	if _, err := catalogNotificationEnvelope(large); err != nil {
		t.Fatal("publication fixture must pass the notifier's standard message budget")
	}
	notifier.Enqueue(large)
	assertLibraryNotifierResync(t, ordinary)
	assertLibraryNotifierResync(t, application)
	assertLibraryNotifierQueueEmpty(t, notifier)
	fresh := subscribeNotifierUser(t, hub, "viewer", "fresh-session")
	notifier.Enqueue(libraryNotifierUpdate("small"))
	want := libraryNotifierEmptyData()
	want.ItemsUpdated = []string{"small"}
	assertLibraryNotifierData(t, decodeLibraryNotifierEvent(t, nextLibraryNotifierEvent(t, fresh)), want)
}

func TestLibraryNotifierExplicitResyncDiscardsPendingAndPartialBatches(t *testing.T) {
	notifier, store, hub, start := newPausedLibraryNotifier(t)
	old := subscribeNotifierUser(t, hub, "viewer", "old-session")
	callback := store.capture()
	callback(libraryNotifierUpdate("old-pending"))
	partial := libraryNotifierUpdate("partial-resync")
	partial.Resync = true
	callback(partial)
	assertLibraryNotifierResync(t, old)
	assertLibraryNotifierQueueEmpty(t, notifier)
	fresh := subscribeNotifierUser(t, hub, "viewer", "new-session")
	callback(library.CatalogNotification{})
	start()
	assertLibraryNotifierQuiet(t, fresh)
	callback(libraryNotifierUpdate("after-resync"))
	want := libraryNotifierEmptyData()
	want.ItemsUpdated = []string{"after-resync"}
	assertLibraryNotifierData(t, decodeLibraryNotifierEvent(t, nextLibraryNotifierEvent(t, fresh)), want)
	// A store may have loaded the callback before listener detachment.
	notifier.Close()
	callback(libraryNotifierUpdate("captured-after-close"))
	callback(library.CatalogNotification{Resync: true})
	assertLibraryNotifierQuiet(t, fresh)
}

func TestLibraryNotifierCloseDetachesAndIgnoresCapturedCallbacks(t *testing.T) {
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	store := &libraryNotifierTestStore{}
	notifier := newLibraryNotifier(store, hub)
	t.Cleanup(func() { notifier.Close(); _ = hub.Close() })
	sub := subscribeNotifierUser(t, hub, "viewer", "session")
	callback := store.capture()
	if callback == nil {
		t.Fatal("constructor did not install the catalog callback")
	}
	notifier.Close()
	if store.capture() != nil {
		t.Fatal("Close retained the registered catalog callback")
	}
	select {
	case <-notifier.done:
	default:
		t.Fatal("Close returned before its worker finished")
	}
	var callers sync.WaitGroup
	for index := 0; index < 4; index++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			callback(libraryNotifierUpdate("late-callback"))
			callback(library.CatalogNotification{Resync: true})
			notifier.Close()
		}()
	}
	finished := make(chan struct{})
	go func() {
		callers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("repeated Close or a captured callback did not return")
	}
	assertLibraryNotifierQueueEmpty(t, notifier)
	assertLibraryNotifierQuiet(t, sub)
	if count, err := hub.PublishAll(events.Envelope{MessageType: "HubStillOpen"}); err != nil || count != 1 {
		t.Fatalf("notifier Close took ownership of the hub = (%d, %v)", count, err)
	}
	if event := nextLibraryNotifierEvent(t, sub); event.MessageType() != "HubStillOpen" {
		t.Fatal("a captured closed callback emitted a delayed catalog event")
	}
}

type libraryNotifierTestStore struct {
	mu       sync.Mutex
	listener func(library.CatalogNotification)
}

func (store *libraryNotifierTestStore) SetCatalogChangeListener(listener func(library.CatalogNotification)) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.listener = listener
}

func (store *libraryNotifierTestStore) capture() func(library.CatalogNotification) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.listener
}

// A paused worker makes queue limits and caller ownership deterministic without
// a production hook. Cleanup starts it before Close waits for its completion.
func newPausedLibraryNotifier(t *testing.T) (*libraryNotifier, *libraryNotifierTestStore, *events.Hub, func()) {
	t.Helper()
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	store := &libraryNotifierTestStore{}
	notifier := &libraryNotifier{store: store, hub: hub, ready: make(chan struct{}, 1), done: make(chan struct{}),
		queue: make([]*queuedLibraryNotification, 0, libraryNotificationCapacity)}
	store.SetCatalogChangeListener(notifier.Enqueue)
	var once sync.Once
	start := func() { once.Do(func() { go notifier.run() }) }
	t.Cleanup(func() { start(); notifier.Close(); _ = hub.Close() })
	return notifier, store, hub, start
}

func libraryNotifierUpdate(itemID string) library.CatalogNotification {
	return library.CatalogNotification{Changes: []library.CatalogChange{{Kind: library.CatalogUpdated, ItemID: itemID, LibraryID: "library"}}}
}

func libraryNotifierLargeBatch() library.CatalogNotification {
	change := library.CatalogChange{Kind: library.CatalogUpdated, ItemID: strings.Repeat("i", 256),
		LibraryID: strings.Repeat("l", 256), ParentID: strings.Repeat("p", 256)}
	changes := make([]library.CatalogChange, 77)
	for index := range changes {
		changes[index] = change
	}
	return library.CatalogNotification{Changes: changes}
}

func libraryNotifierEmptyData() libraryChangedData {
	return libraryChangedData{FoldersAddedTo: []string{}, FoldersRemovedFrom: []string{}, ItemsAdded: []string{},
		ItemsRemoved: []string{}, ItemsUpdated: []string{}, CollectionFolders: []string{}}
}

func assertLibraryNotifierData(t *testing.T, envelope events.Envelope, want libraryChangedData) {
	t.Helper()
	var fields map[string]json.RawMessage
	var got libraryChangedData
	if envelope.MessageType != "LibraryChanged" || json.Unmarshal(envelope.Data, &fields) != nil || len(fields) != 7 || json.Unmarshal(envelope.Data, &got) != nil {
		t.Fatal("catalog notification must contain exactly seven public fields")
	}
	for _, name := range []string{"FoldersAddedTo", "FoldersRemovedFrom", "ItemsAdded", "ItemsRemoved", "ItemsUpdated", "CollectionFolders", "IsEmpty"} {
		if _, exists := fields[name]; !exists {
			t.Fatalf("catalog notification omitted %s", name)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog notification data = %+v, want %+v", got, want)
	}
}

func subscribeLibraryNotifierApplication(t *testing.T, hub *events.Hub, sessionID string) *events.Subscription {
	t.Helper()
	sub, err := hub.Subscribe(events.Scope{ApplicationKey: true, CredentialID: "application-key", SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func nextLibraryNotifierEvent(t *testing.T, sub *events.Subscription) events.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func decodeLibraryNotifierEvent(t *testing.T, event events.Event) events.Envelope {
	t.Helper()
	var envelope events.Envelope
	var fields map[string]json.RawMessage
	if json.Unmarshal(event.Bytes(), &envelope) != nil || json.Unmarshal(event.Bytes(), &fields) != nil || len(fields) != 3 || envelope.MessageID == "" {
		t.Fatal("published catalog envelope must contain only the public fields and a shared identity")
	}
	return envelope
}

func assertLibraryNotifierQuiet(t *testing.T, sub *events.Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if event, err := sub.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected notification or disconnect = (%s, %v)", event.Bytes(), err)
	}
}

func assertLibraryNotifierResync(t *testing.T, sub *events.Subscription) {
	t.Helper()
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("resynchronization did not disconnect an existing subscriber")
	}
	if !errors.Is(sub.Reason(), events.ErrResyncRequired) {
		t.Fatalf("resynchronization reason = %v", sub.Reason())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if event, err := sub.Next(ctx); !errors.Is(err, events.ErrResyncRequired) || len(event.Bytes()) != 0 {
		t.Fatalf("resynchronization retained an old queued event: %v", err)
	}
}

func assertLibraryNotifierQueueEmpty(t *testing.T, notifier *libraryNotifier) {
	t.Helper()
	notifier.mu.Lock()
	retained := len(notifier.queue) != 0 || notifier.active != nil || notifier.count != 0 || notifier.bytes != 0
	notifier.mu.Unlock()
	if retained {
		t.Fatal("notifier retained discarded work or queue accounting")
	}
}
