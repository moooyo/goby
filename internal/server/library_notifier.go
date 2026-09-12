package server

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

const (
	libraryNotificationCapacity    = 64
	libraryNotificationQueueBytes  = 256 * 1024
	libraryNotificationBatchBytes  = 64 * 1024
	libraryNotificationChanges     = 512
	libraryNotificationChangeBytes = 80
)

type libraryNotificationStore interface {
	SetCatalogChangeListener(func(library.CatalogNotification))
}

type queuedLibraryNotification struct {
	notification library.CatalogNotification
	bytes        int
	generation   uint64
}

// One worker preserves post-commit callback order. Queue budgets include the
// active entry. Generation changes invalidate work already being encoded when
// resynchronization discards the queue, so it cannot reach a new subscription.
type libraryNotifier struct {
	store      libraryNotificationStore
	hub        *events.Hub
	mu         sync.Mutex
	closed     bool
	generation uint64
	queue      []*queuedLibraryNotification
	active     *queuedLibraryNotification
	count      int
	bytes      int
	ready      chan struct{}
	done       chan struct{}
}

func newLibraryNotifier(store libraryNotificationStore, hub *events.Hub) *libraryNotifier {
	n := &libraryNotifier{store: store, hub: hub, ready: make(chan struct{}, 1), done: make(chan struct{}),
		queue: make([]*queuedLibraryNotification, 0, libraryNotificationCapacity)}
	go n.run()
	if store != nil {
		store.SetCatalogChangeListener(n.Enqueue)
	}
	return n
}

// Enqueue does no database or network work and never waits for queue capacity.
// Callers may release or reuse their input immediately after this bounded copy.
// The callback's outcome cannot change the transaction that already committed.
func (n *libraryNotifier) Enqueue(notification library.CatalogNotification) {
	if n == nil || n.hub == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	size, valid := catalogNotificationSize(notification)
	if notification.Resync || !valid {
		n.resyncLocked()
		return
	}
	if len(notification.Changes) == 0 {
		return
	}
	if n.count >= libraryNotificationCapacity || size > libraryNotificationQueueBytes-n.bytes {
		n.resyncLocked()
		return
	}
	changes := make([]library.CatalogChange, len(notification.Changes))
	for index, change := range notification.Changes {
		changes[index] = change
		changes[index].ItemID = strings.Clone(change.ItemID)
		changes[index].LibraryID = strings.Clone(change.LibraryID)
		changes[index].ParentID = strings.Clone(change.ParentID)
		changes[index].PreviousParentID = strings.Clone(change.PreviousParentID)
	}
	n.queue = append(n.queue, &queuedLibraryNotification{notification: library.CatalogNotification{Changes: changes},
		bytes: size, generation: n.generation})
	n.count++
	n.bytes += size
	n.signal()
}

func catalogNotificationSize(notification library.CatalogNotification) (int, bool) {
	if len(notification.Changes) > libraryNotificationChanges {
		return 0, false
	}
	size := 32
	for _, change := range notification.Changes {
		if (change.Kind != library.CatalogAdded && change.Kind != library.CatalogUpdated && change.Kind != library.CatalogRemoved) ||
			!validLibraryChangedID(change.ItemID) || !validLibraryChangedID(change.LibraryID) ||
			(change.ParentID != "" && !validLibraryChangedID(change.ParentID)) || change.ParentID == change.ItemID ||
			(change.PreviousParentID != "" && (change.Kind != library.CatalogUpdated || change.IsCollectionFolder ||
				!validLibraryChangedID(change.PreviousParentID) || change.ParentID == "" ||
				change.PreviousParentID == change.ParentID || change.PreviousParentID == change.ItemID)) ||
			(change.IsCollectionFolder && (!change.IsFolder || change.ItemID != change.LibraryID || change.ParentID != "")) {
			return 0, false
		}
		size += libraryNotificationChangeBytes + len(change.ItemID) + len(change.LibraryID) + len(change.ParentID) + len(change.PreviousParentID)
		if size > libraryNotificationBatchBytes {
			return 0, false
		}
	}
	return size, true
}

// Detach before closing the queue. A callback loaded before detachment may
// still finish; its final Enqueue sees closed. The signal channel stays open.
func (n *libraryNotifier) Close() {
	if n == nil {
		return
	}
	if n.store != nil {
		n.store.SetCatalogChangeListener(nil)
	}
	n.mu.Lock()
	n.closed = true
	n.generation++
	n.discardQueuedLocked()
	n.signal()
	n.mu.Unlock()
	<-n.done
}

func (n *libraryNotifier) signal() {
	select {
	case n.ready <- struct{}{}:
	default:
	}
}

func (n *libraryNotifier) discardQueuedLocked() {
	for _, entry := range n.queue {
		n.count--
		n.bytes -= entry.bytes
	}
	clear(n.queue)
	n.queue = n.queue[:0]
}

// Hub methods never call back into this notifier. Serializing publication and
// disconnect under n.mu prevents an invalidated old entry from reaching a
// subscriber that reconnects after overflow. Both operations are memory-only.
func (n *libraryNotifier) resyncLocked() {
	n.generation++
	n.discardQueuedLocked()
	n.hub.DisconnectAll()
}

func (n *libraryNotifier) run() {
	defer close(n.done)
	for {
		n.mu.Lock()
		if n.closed {
			n.mu.Unlock()
			return
		}
		if len(n.queue) == 0 {
			n.mu.Unlock()
			<-n.ready
			continue
		}
		entry := n.queue[0]
		copy(n.queue, n.queue[1:])
		n.queue[len(n.queue)-1] = nil
		n.queue = n.queue[:len(n.queue)-1]
		n.active = entry
		n.mu.Unlock()

		envelope, err := catalogNotificationEnvelope(entry.notification)
		n.mu.Lock()
		if !n.closed && entry.generation == n.generation {
			if err == nil && len(envelope.Data) != 0 {
				_, err = n.hub.PublishAll(envelope)
			}
			if err != nil {
				n.resyncLocked()
			}
		}
		n.active = nil
		n.count--
		n.bytes -= entry.bytes
		n.mu.Unlock()
	}
}

type catalogChangeKey struct {
	itemID    string
	libraryID string
}

// A notification is a committed batch, not a stream to reorder or coalesce.
// Same-kind duplicates and shared-library scopes are representable. Different
// kinds for one ID, conflicting facts in one library, and cycles require a
// fresh snapshot rather than inventing a net state and silently losing a change.
func catalogNotificationEnvelope(notification library.CatalogNotification) (events.Envelope, error) {
	if _, valid := catalogNotificationSize(notification); !valid || notification.Resync {
		return events.Envelope{}, events.ErrResyncRequired
	}
	if len(notification.Changes) == 0 {
		return events.Envelope{}, nil
	}
	byKey := make(map[catalogChangeKey]library.CatalogChange, len(notification.Changes))
	byItem := make(map[string]library.CatalogChange, len(notification.Changes))
	for _, change := range notification.Changes {
		key := catalogChangeKey{change.ItemID, change.LibraryID}
		if previous, exists := byKey[key]; exists && previous != change {
			return events.Envelope{}, events.ErrResyncRequired
		}
		if previous, exists := byItem[change.ItemID]; exists &&
			(previous.Kind != change.Kind || previous.IsFolder != change.IsFolder || previous.IsCollectionFolder != change.IsCollectionFolder) {
			return events.Envelope{}, events.ErrResyncRequired
		}
		byKey[key], byItem[change.ItemID] = change, change
	}
	for _, change := range notification.Changes {
		if previous, exists := byKey[catalogChangeKey{change.PreviousParentID, change.LibraryID}]; change.PreviousParentID != "" && exists && !previous.IsFolder {
			return events.Envelope{}, events.ErrResyncRequired
		}
		seen := map[string]bool{change.ItemID: true}
		for parentID := change.ParentID; parentID != ""; {
			if seen[parentID] {
				return events.Envelope{}, events.ErrResyncRequired
			}
			seen[parentID] = true
			parent, exists := byKey[catalogChangeKey{parentID, change.LibraryID}]
			if !exists {
				break
			}
			if !parent.IsFolder {
				return events.Envelope{}, events.ErrResyncRequired
			}
			parentID = parent.ParentID
		}
	}
	data := libraryChangedData{FoldersAddedTo: []string{}, FoldersRemovedFrom: []string{}, ItemsAdded: []string{},
		ItemsRemoved: []string{}, ItemsUpdated: []string{}, CollectionFolders: []string{}}
	seenFields := make(map[*[]string]map[string]bool, 6)
	seenScopes := make(map[events.CatalogScope]bool)
	scopes := make([]events.CatalogScope, 0, len(notification.Changes))
	count := 0
	appendID := func(field *[]string, id, libraryID string) {
		if id == "" {
			return
		}
		if seenFields[field] == nil {
			seenFields[field] = make(map[string]bool)
		}
		if !seenFields[field][id] {
			seenFields[field][id] = true
			*field = append(*field, id)
			count++
		}
		scope := events.CatalogScope{ItemID: id, LibraryID: libraryID}
		if !seenScopes[scope] {
			seenScopes[scope] = true
			scopes = append(scopes, scope)
		}
	}
	for _, change := range notification.Changes {
		if change.Kind == library.CatalogRemoved {
			parentID, suppressed := change.ParentID, false
			seen := map[string]bool{change.ItemID: true}
			for parentID != "" {
				if seen[parentID] {
					return events.Envelope{}, events.ErrResyncRequired
				}
				seen[parentID] = true
				parent, exists := byKey[catalogChangeKey{parentID, change.LibraryID}]
				if !exists || parent.Kind != library.CatalogRemoved {
					break
				}
				if !parent.IsFolder {
					return events.Envelope{}, events.ErrResyncRequired
				}
				suppressed = true
				parentID = parent.ParentID
			}
			if suppressed {
				continue
			}
		}
		switch change.Kind {
		case library.CatalogAdded:
			appendID(&data.ItemsAdded, change.ItemID, change.LibraryID)
			appendID(&data.CollectionFolders, change.LibraryID, change.LibraryID)
			if !change.IsCollectionFolder {
				appendID(&data.FoldersAddedTo, change.ParentID, change.LibraryID)
			}
		case library.CatalogUpdated:
			appendID(&data.ItemsUpdated, change.ItemID, change.LibraryID)
			if change.PreviousParentID != "" {
				appendID(&data.FoldersRemovedFrom, change.PreviousParentID, change.LibraryID)
				appendID(&data.FoldersAddedTo, change.ParentID, change.LibraryID)
			}
		case library.CatalogRemoved:
			appendID(&data.ItemsRemoved, change.ItemID, change.LibraryID)
			if !change.IsCollectionFolder {
				appendID(&data.FoldersRemovedFrom, change.ParentID, change.LibraryID)
			}
		}
	}
	if count == 0 || count > maxLibraryChangedIDs || len(scopes) > maxLibraryChangedIDs {
		return events.Envelope{}, events.ErrResyncRequired
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return events.Envelope{}, err
	}
	envelope := events.Envelope{MessageType: "LibraryChanged", Data: raw, CatalogScopes: scopes}
	// Reserve the generated 32-byte MessageId and conservatively charge private
	// scope bookkeeping as well as its strings before handing work to the Hub.
	sizing := envelope
	sizing.MessageID = strings.Repeat("0", 32)
	wire, err := json.Marshal(sizing)
	if err != nil {
		return events.Envelope{}, err
	}
	retained := len(wire)
	for _, scope := range scopes {
		retained += 64 + len(scope.ItemID) + len(scope.LibraryID)
	}
	if retained > events.DefaultMaxMessageBytes {
		return events.Envelope{}, events.ErrResyncRequired
	}
	return envelope, nil
}
