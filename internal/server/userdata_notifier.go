package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

const (
	userDataNotificationCapacity = 256
	userDataNotificationPerUser  = 64
	userDataNotificationPageSize = 64
	userDataNotificationIDBytes  = 256
	userDataNotificationTimeout  = 15 * time.Second
)

type userDataNotificationStore interface {
	UserDataNotificationPage(context.Context, library.UserDataNotificationQuery) (library.UserDataNotificationResult, error)
}

var _ userDataNotificationStore = (*library.Store)(nil)

type userDataNotificationKey struct {
	userID string
	itemID string
}

type userDataNotificationMarker struct {
	key       userDataNotificationKey
	recursive bool
	running   bool
	dirty     bool
	cancel    context.CancelFunc
}

// userDataNotifier retains only post-commit identifiers, never an HTTP response
// snapshot. One worker reads current state and preserves read/publication order.
// Capacity includes the active marker. Repeated active markers request another
// read, so a commit that races an existing read cannot lose its final update.
type userDataNotifier struct {
	store userDataNotificationStore
	hub   *events.Hub
	ctx   context.Context
	stop  context.CancelFunc
	done  chan struct{}
	ready chan struct{}

	mu        sync.Mutex
	closed    bool
	pending   map[userDataNotificationKey]*userDataNotificationMarker
	userCount map[string]int
	queue     []*userDataNotificationMarker
}

func newUserDataNotifier(store userDataNotificationStore, hub *events.Hub) *userDataNotifier {
	ctx, stop := context.WithCancel(context.Background())
	notifier := &userDataNotifier{
		store:     store,
		hub:       hub,
		ctx:       ctx,
		stop:      stop,
		done:      make(chan struct{}),
		ready:     make(chan struct{}, 1),
		pending:   make(map[userDataNotificationKey]*userDataNotificationMarker),
		userCount: make(map[string]int),
		queue:     make([]*userDataNotificationMarker, 0, userDataNotificationCapacity),
	}
	go notifier.run()
	return notifier
}

// Enqueue performs no database work and never waits for queue capacity. Pending
// identifiers coalesce; recursive requests dominate non-recursive requests.
// A lost notification requires a fresh client snapshot, so overflow disconnects
// only the affected user's sockets and removes that user's remaining markers.
func (n *userDataNotifier) Enqueue(userID, itemID string, recursive bool) {
	if n == nil || n.hub == nil || n.store == nil || !validNotificationID(userID) || n.hub.CountForUser(userID) == 0 {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return
	}
	if !validNotificationID(itemID) {
		n.resyncUserLocked(userID)
		return
	}
	key := userDataNotificationKey{userID: userID, itemID: itemID}
	if marker := n.pending[key]; marker != nil {
		marker.recursive = marker.recursive || recursive
		if marker.running {
			marker.dirty = true
		}
		return
	}
	if len(n.pending) >= userDataNotificationCapacity || n.userCount[userID] >= userDataNotificationPerUser {
		n.resyncUserLocked(userID)
		return
	}
	// Request path values can be substrings of a much larger URL. Retain only
	// the bounded identifiers instead of keeping that backing storage alive.
	key.userID = strings.Clone(key.userID)
	key.itemID = strings.Clone(key.itemID)
	marker := &userDataNotificationMarker{key: key, recursive: recursive}
	n.pending[key] = marker
	n.userCount[key.userID]++
	n.queue = append(n.queue, marker)
	n.signal()
}

// Close cancels the current database request, drops pending work, and waits for
// the worker. Store methods must honor context cancellation, as the library
// store does. Hub ownership remains with the server.
func (n *userDataNotifier) Close() {
	if n == nil {
		return
	}
	n.mu.Lock()
	if !n.closed {
		n.closed = true
		n.stop()
		clear(n.pending)
		clear(n.userCount)
		n.queue = nil
	}
	n.mu.Unlock()
	<-n.done
}

func (n *userDataNotifier) run() {
	defer close(n.done)
	for {
		n.mu.Lock()
		if n.closed {
			n.mu.Unlock()
			return
		}
		if len(n.queue) == 0 {
			n.mu.Unlock()
			select {
			case <-n.ctx.Done():
				return
			case <-n.ready:
			}
			continue
		}
		marker := n.queue[0]
		copy(n.queue, n.queue[1:])
		n.queue[len(n.queue)-1] = nil
		n.queue = n.queue[:len(n.queue)-1]
		marker.running = true
		marker.dirty = false
		ctx, cancel := context.WithTimeout(n.ctx, userDataNotificationTimeout)
		marker.cancel = cancel
		recursive := marker.recursive
		n.mu.Unlock()

		err := n.notify(ctx, marker, recursive)
		cancel()
		n.finish(marker, err)
	}
}

func (n *userDataNotifier) notify(ctx context.Context, marker *userDataNotificationMarker, recursive bool) error {
	query := library.UserDataNotificationQuery{
		UserID: marker.key.userID, ItemID: marker.key.itemID,
		Recursive: recursive, Limit: userDataNotificationPageSize,
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n.hub.CountForUser(query.UserID) == 0 {
			return nil
		}
		page, err := n.store.UserDataNotificationPage(ctx, query)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(page.Items) > userDataNotificationPageSize ||
			(page.NextAfterID != "" && (!validNotificationID(page.NextAfterID) || page.NextAfterID <= query.AfterID)) {
			return errors.New("invalid user data notification page")
		}
		if len(page.Items) > 0 {
			data := struct {
				UserID       string           `json:"UserId"`
				UserDataList []map[string]any `json:"UserDataList"`
			}{UserID: query.UserID, UserDataList: make([]map[string]any, 0, len(page.Items))}
			for _, item := range page.Items {
				if !validNotificationID(item.ItemID) {
					return errors.New("invalid user data notification item identifier")
				}
				data.UserDataList = append(data.UserDataList, userDataDTO(item, true))
			}
			encoded, err := json.Marshal(data)
			if err != nil {
				return err
			}
			// Serialize the final activity check with overflow and Close. A
			// canceled old task must not publish to a newly reconnected socket.
			n.mu.Lock()
			if n.closed || n.pending[marker.key] != marker || ctx.Err() != nil {
				n.mu.Unlock()
				return context.Canceled
			}
			_, err = n.hub.PublishUser(query.UserID, events.Envelope{MessageType: "UserDataChanged", Data: encoded})
			n.mu.Unlock()
			if err != nil {
				return err
			}
		}
		if page.NextAfterID == "" {
			return nil
		}
		query.AfterID = page.NextAfterID
	}
}

func (n *userDataNotifier) finish(marker *userDataNotificationMarker, err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed || n.pending[marker.key] != marker {
		return
	}
	marker.cancel = nil
	if err != nil {
		// Database, encoding, and publication failures use the same explicit
		// resynchronization policy as overflow rather than silently dropping.
		n.resyncUserLocked(marker.key.userID)
		return
	}
	if marker.dirty {
		marker.running = false
		n.queue = append(n.queue, marker)
		n.signal()
		return
	}
	delete(n.pending, marker.key)
	if n.userCount[marker.key.userID] <= 1 {
		delete(n.userCount, marker.key.userID)
	} else {
		n.userCount[marker.key.userID]--
	}
}

// resyncUserLocked requires n.mu. Hub methods never call into this notifier, so
// taking the Hub lock while holding n.mu cannot invert the lock order.
func (n *userDataNotifier) resyncUserLocked(userID string) {
	for key, marker := range n.pending {
		if key.userID == userID {
			if marker.cancel != nil {
				marker.cancel()
			}
			delete(n.pending, key)
		}
	}
	delete(n.userCount, userID)
	retained := n.queue[:0]
	for _, marker := range n.queue {
		if marker.key.userID != userID {
			retained = append(retained, marker)
		}
	}
	clear(n.queue[len(retained):])
	n.queue = retained
	n.hub.DisconnectUser(userID)
}

func (n *userDataNotifier) signal() {
	select {
	case n.ready <- struct{}{}:
	default:
	}
}

func validNotificationID(value string) bool {
	return len(value) <= userDataNotificationIDBytes && strings.TrimSpace(value) != "" &&
		utf8.ValidString(value) && !strings.ContainsRune(value, '\x00')
}
