// Package events provides bounded, in-memory delivery to authenticated scopes.
// It does not authenticate callers or retain credentials. Transport code must
// revalidate current account and resource permissions before delivering events.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	DefaultMaxConnections           = 128
	DefaultMaxConnectionsPerUser    = 8
	DefaultMaxConnectionsPerSession = 4
	DefaultQueueMessages            = 16
	DefaultQueueBytes               = 256 * 1024
	DefaultMaxMessageBytes          = 128 * 1024
	maxScopeFieldBytes              = 256
	maxCatalogScopes                = 4096
	// Two string headers per retained scope, charged conservatively on both
	// 32-bit and 64-bit builds in addition to the copied string contents.
	catalogScopeOverheadBytes = 32
)

var (
	ErrClosed                    = errors.New("event hub is closed")
	ErrUnsubscribed              = errors.New("event subscription is closed")
	ErrSlowConsumer              = errors.New("event subscriber queue is full")
	ErrSessionRevoked            = errors.New("event session was disconnected")
	ErrUserRevoked               = errors.New("event user was disconnected")
	ErrResyncRequired            = errors.New("event catalog requires a fresh snapshot")
	ErrInvalidOptions            = errors.New("invalid event hub options")
	ErrInvalidScope              = errors.New("invalid event scope")
	ErrInvalidEvent              = errors.New("invalid event envelope")
	ErrConnectionLimit           = errors.New("event connection limit reached")
	ErrUserConnectionLimit       = errors.New("event user connection limit reached")
	ErrCredentialConnectionLimit = errors.New("event credential connection limit reached")
	ErrSessionConnectionLimit    = errors.New("event session connection limit reached")
	ErrMessageTooLarge           = errors.New("event exceeds message size limit")
)

// Options bounds both connection counts and the event data retained by each
// subscription. Zero values use the defaults. Negative values are invalid.
// Byte limits include encoded JSON and private catalog scope metadata.
// MaxMessageBytes must not exceed QueueBytes after applying defaults.
type Options struct {
	MaxConnections           int
	MaxConnectionsPerUser    int
	MaxConnectionsPerSession int
	QueueMessages            int
	QueueBytes               int
	MaxMessageBytes          int
}

// Scope is the immutable identity associated with a connection. Application keys
// have no user; their SessionID identifies a client context and CredentialID its
// revocable parent. Ordinary sessions require a user and no CredentialID.
// DeviceID is metadata and never grants access to another scope's events.
type Scope struct {
	UserID         string
	SessionID      string
	DeviceID       string
	ApplicationKey bool
	CredentialID   string
}

// Authority is trusted, in-memory publication metadata. It never appears in a
// client message and cannot be supplied through a command's JSON body. An
// application sender retains identifiers needed for current revalidation.
type Authority struct {
	CredentialID     string
	ClientSessionID  string
	ApplicationKeyID int64
}

// CatalogScope is trusted publication metadata for current resource permission
// checks. It never appears in client JSON. Producers may retain multiple library
// scopes for one item when a move or deletion changes its catalog membership.
type CatalogScope struct {
	ItemID    string
	LibraryID string
}

// Envelope is encoded once per publish. An omitted MessageID is generated once
// and shared by all recipients. Data and CatalogScopes are copied; callers must
// not mutate them concurrently with a publish but may reuse them afterward.
type Envelope struct {
	MessageType   string          `json:"MessageType"`
	MessageID     string          `json:"MessageId"`
	Data          json.RawMessage `json:"Data"`
	Authority     Authority       `json:"-"`
	CatalogScopes []CatalogScope  `json:"-"`
}

// Event holds immutable JSON shared safely by recipient queues. Its zero value
// is not a published event. Bytes returns an independent copy for the caller.
type Event struct {
	payload       string
	messageType   string
	messageID     string
	authority     Authority
	catalogScopes []CatalogScope
	retainedBytes int
}

func (e Event) Bytes() []byte        { return []byte(e.payload) }
func (e Event) MessageType() string  { return e.messageType }
func (e Event) MessageID() string    { return e.messageID }
func (e Event) Authority() Authority { return e.authority }

// CatalogScopes returns a separate slice containing the immutable publication
// snapshot. Changing one recipient's result cannot change another event.
func (e Event) CatalogScopes() []CatalogScope {
	return append([]CatalogScope(nil), e.catalogScopes...)
}

// Hub owns subscriptions without starting background goroutines. The mutex
// protects every queue and close transition; publishing never waits for readers.
type Hub struct {
	mu               sync.Mutex
	opts             Options
	closed           bool
	subscribers      map[*Subscription]struct{}
	userCounts       map[string]int
	credentialCounts map[string]int
	sessionCounts    map[string]int
}

// Subscription receives broadcasts, its user's events, and exact-session messages.
// Next, Close, and all Hub methods may be called concurrently. Concurrent Next
// calls divide events between readers; each event is dequeued at most once.
type Subscription struct {
	hub    *Hub
	scope  Scope
	queue  []Event
	head   int
	count  int
	bytes  int
	ready  chan struct{}
	done   chan struct{}
	reason error
}

func New(opts Options) (*Hub, error) {
	fields := []struct {
		value        *int
		defaultValue int
	}{
		{&opts.MaxConnections, DefaultMaxConnections},
		{&opts.MaxConnectionsPerUser, DefaultMaxConnectionsPerUser},
		{&opts.MaxConnectionsPerSession, DefaultMaxConnectionsPerSession},
		{&opts.QueueMessages, DefaultQueueMessages},
		{&opts.QueueBytes, DefaultQueueBytes},
		{&opts.MaxMessageBytes, DefaultMaxMessageBytes},
	}
	for _, field := range fields {
		if *field.value < 0 {
			return nil, ErrInvalidOptions
		}
		if *field.value == 0 {
			*field.value = field.defaultValue
		}
	}
	if opts.MaxMessageBytes > opts.QueueBytes {
		return nil, fmt.Errorf("%w: message limit exceeds queue byte limit", ErrInvalidOptions)
	}
	return &Hub{
		opts:             opts,
		subscribers:      make(map[*Subscription]struct{}),
		userCounts:       make(map[string]int),
		credentialCounts: make(map[string]int),
		sessionCounts:    make(map[string]int),
	}, nil
}

func (h *Hub) Subscribe(scope Scope) (*Subscription, error) {
	if !validScope(scope) {
		return nil, ErrInvalidScope
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrClosed
	}
	if len(h.subscribers) >= h.opts.MaxConnections {
		return nil, ErrConnectionLimit
	}
	if !scope.ApplicationKey && h.userCounts[scope.UserID] >= h.opts.MaxConnectionsPerUser {
		return nil, ErrUserConnectionLimit
	}
	if scope.ApplicationKey && h.credentialCounts[scope.CredentialID] >= h.opts.MaxConnectionsPerUser {
		return nil, ErrCredentialConnectionLimit
	}
	if h.sessionCounts[scope.SessionID] >= h.opts.MaxConnectionsPerSession {
		return nil, ErrSessionConnectionLimit
	}
	sub := &Subscription{
		hub:   h,
		scope: scope,
		queue: make([]Event, h.opts.QueueMessages),
		ready: make(chan struct{}, 1),
		done:  make(chan struct{}),
	}
	h.subscribers[sub] = struct{}{}
	if scope.ApplicationKey {
		h.credentialCounts[scope.CredentialID]++
	} else {
		h.userCounts[scope.UserID]++
	}
	h.sessionCounts[scope.SessionID]++
	return sub, nil
}

// PublishUser delivers only to subscriptions belonging to userID. The result is
// the number of queues that accepted the event, excluding disconnected readers.
func (h *Hub) PublishUser(userID string, envelope Envelope) (int, error) {
	if !validScopeField(userID, true) {
		return 0, ErrInvalidScope
	}
	return h.publish(Scope{UserID: userID}, publicationUser, envelope)
}

// PublishSession requires both identifiers to match, preventing a session target
// from expanding the caller's authorized user scope.
func (h *Hub) PublishSession(userID, sessionID string, envelope Envelope) (int, error) {
	if !validScopeField(userID, true) || !validScopeField(sessionID, true) {
		return 0, ErrInvalidScope
	}
	return h.publish(Scope{UserID: userID, SessionID: sessionID}, publicationScope, envelope)
}

// PublishScope requires an exact typed client identity. An application's parent
// credential never expands a publication to sibling client contexts.
func (h *Hub) PublishScope(scope Scope, envelope Envelope) (int, error) {
	if !validScope(scope) {
		return 0, ErrInvalidScope
	}
	return h.publish(scope, publicationScope, envelope)
}

// PublishAll queues one shared event for all current subscriptions, including
// application clients. It does not authorize resource disclosure: transports
// must revalidate each recipient and filter its payload before network delivery.
// The result excludes subscribers disconnected by the existing queue limits.
func (h *Hub) PublishAll(envelope Envelope) (int, error) {
	return h.publish(Scope{}, publicationAll, envelope)
}

type publicationAudience uint8

const (
	publicationScope publicationAudience = iota
	publicationUser
	publicationAll
)

func (h *Hub) publish(scope Scope, audience publicationAudience, envelope Envelope) (int, error) {
	event, err := encodeEvent(envelope, h.opts.MaxMessageBytes)
	if err != nil {
		return 0, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return 0, ErrClosed
	}
	delivered := 0
	for sub := range h.subscribers {
		if audience != publicationAll && (sub.scope.ApplicationKey != scope.ApplicationKey || sub.scope.UserID != scope.UserID ||
			sub.scope.CredentialID != scope.CredentialID || (audience != publicationUser && sub.scope.SessionID != scope.SessionID)) {
			continue
		}
		if sub.count == len(sub.queue) || event.retainedBytes > h.opts.QueueBytes-sub.bytes {
			h.remove(sub, ErrSlowConsumer)
			continue
		}
		sub.queue[(sub.head+sub.count)%len(sub.queue)] = event
		sub.count++
		sub.bytes += event.retainedBytes
		sub.signal()
		delivered++
	}
	return delivered, nil
}

// DisconnectCredential closes every application client authenticated by a key,
// or the single ordinary authentication session, without conflating delivery IDs.
func (h *Hub) DisconnectCredential(credentialID string) {
	if !validScopeField(credentialID, true) {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subscribers {
		if (sub.scope.ApplicationKey && sub.scope.CredentialID == credentialID) ||
			(!sub.scope.ApplicationKey && sub.scope.SessionID == credentialID) {
			h.remove(sub, ErrSessionRevoked)
		}
	}
}

// DisconnectSession closes current subscriptions; it is not a persistent
// revocation registry. Authentication must reject revoked sessions on reconnect.
func (h *Hub) DisconnectSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subscribers {
		if sub.scope.SessionID == sessionID {
			h.remove(sub, ErrSessionRevoked)
		}
	}
}

// DisconnectUser closes all current subscriptions for the given user.
func (h *Hub) DisconnectUser(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subscribers {
		if !sub.scope.ApplicationKey && sub.scope.UserID == userID {
			h.remove(sub, ErrUserRevoked)
		}
	}
}

// DisconnectAll discards every current subscription when a committed catalog
// change cannot be delivered completely. It is not a permanent revocation or a
// Hub shutdown: newly authenticated subscribers may obtain a fresh snapshot.
func (h *Hub) DisconnectAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subscribers {
		h.remove(sub, ErrResyncRequired)
	}
}

func (h *Hub) CountForSession(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionCounts[sessionID]
}

func (h *Hub) CountForUser(userID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.userCounts[userID]
}

// Close permanently rejects new subscriptions and discards all queued events.
func (h *Hub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		for sub := range h.subscribers {
			h.remove(sub, ErrClosed)
		}
	}
	return nil
}

func (s *Subscription) Scope() Scope          { return s.scope }
func (s *Subscription) Done() <-chan struct{} { return s.done }

// Reason returns nil while active and the first close reason after Done closes.
func (s *Subscription) Reason() error {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	return s.reason
}

// Next waits for an event or cancellation. A closed subscription discards queued
// events and returns its close reason. Context cancellation leaves it subscribed.
func (s *Subscription) Next(ctx context.Context) (Event, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		s.hub.mu.Lock()
		if s.reason != nil {
			err := s.reason
			s.hub.mu.Unlock()
			return Event{}, err
		}
		if s.count > 0 {
			event := s.queue[s.head]
			s.queue[s.head] = Event{}
			s.head = (s.head + 1) % len(s.queue)
			s.count--
			s.bytes -= event.retainedBytes
			if s.count > 0 {
				s.signal()
			}
			s.hub.mu.Unlock()
			return event, nil
		}
		s.hub.mu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-s.done:
			return Event{}, s.Reason()
		case <-s.ready:
		}
	}
}

func (s *Subscription) Close() error {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.remove(s, ErrUnsubscribed)
	return nil
}

// remove requires h.mu. The notification channel stays open, so a publisher can
// never send to a closed channel. All blocked readers observe the closed done.
func (h *Hub) remove(sub *Subscription, reason error) {
	if sub.reason != nil {
		return
	}
	delete(h.subscribers, sub)
	if sub.scope.ApplicationKey {
		decrement(h.credentialCounts, sub.scope.CredentialID)
	} else {
		decrement(h.userCounts, sub.scope.UserID)
	}
	decrement(h.sessionCounts, sub.scope.SessionID)
	sub.queue = nil
	sub.head = 0
	sub.count = 0
	sub.bytes = 0
	sub.reason = reason
	close(sub.done)
}

func (s *Subscription) signal() {
	select {
	case s.ready <- struct{}{}:
	default:
	}
}

func decrement(counts map[string]int, key string) {
	if counts[key] <= 1 {
		delete(counts, key)
	} else {
		counts[key]--
	}
}

func validScopeField(value string, required bool) bool {
	return len(value) <= maxScopeFieldBytes && utf8.ValidString(value) &&
		!strings.ContainsRune(value, '\x00') && (!required || strings.TrimSpace(value) != "")
}

func validScope(scope Scope) bool {
	if !validScopeField(scope.SessionID, true) || !validScopeField(scope.DeviceID, false) {
		return false
	}
	if scope.ApplicationKey {
		return scope.UserID == "" && validScopeField(scope.CredentialID, true)
	}
	return scope.CredentialID == "" && validScopeField(scope.UserID, true)
}

func catalogScopesSize(scopes []CatalogScope, maxBytes int) (int, error) {
	// Check the count before multiplication or copying, even when callers use
	// an unusually large byte limit. Every subsequent addition uses subtraction.
	if len(scopes) > maxCatalogScopes || len(scopes) > maxBytes/catalogScopeOverheadBytes {
		return 0, ErrMessageTooLarge
	}
	size := len(scopes) * catalogScopeOverheadBytes
	for _, scope := range scopes {
		if !validScopeField(scope.ItemID, true) || strings.TrimSpace(scope.ItemID) != scope.ItemID ||
			!validScopeField(scope.LibraryID, true) || strings.TrimSpace(scope.LibraryID) != scope.LibraryID {
			return 0, ErrInvalidEvent
		}
		if len(scope.ItemID) > maxBytes-size {
			return 0, ErrMessageTooLarge
		}
		size += len(scope.ItemID)
		if len(scope.LibraryID) > maxBytes-size {
			return 0, ErrMessageTooLarge
		}
		size += len(scope.LibraryID)
	}
	return size, nil
}

func encodeEvent(envelope Envelope, maxBytes int) (Event, error) {
	if envelope.Authority != (Authority{}) && (envelope.Authority.ApplicationKeyID <= 0 ||
		!validScopeField(envelope.Authority.CredentialID, true) || !validScopeField(envelope.Authority.ClientSessionID, true)) {
		return Event{}, ErrInvalidEvent
	}
	if len(envelope.Data) > maxBytes || len(envelope.MessageType) > maxBytes || len(envelope.MessageID) > maxBytes {
		return Event{}, ErrMessageTooLarge
	}
	scopeBytes, err := catalogScopesSize(envelope.CatalogScopes, maxBytes)
	if err != nil {
		return Event{}, err
	}
	maxPayloadBytes := maxBytes - scopeBytes
	if len(envelope.Data) > maxPayloadBytes || len(envelope.MessageType) > maxPayloadBytes || len(envelope.MessageID) > maxPayloadBytes {
		return Event{}, ErrMessageTooLarge
	}
	if strings.TrimSpace(envelope.MessageType) == "" || !utf8.ValidString(envelope.MessageType) ||
		!utf8.ValidString(envelope.MessageID) || !utf8.Valid(envelope.Data) ||
		(envelope.MessageID != "" && strings.TrimSpace(envelope.MessageID) == "") {
		return Event{}, ErrInvalidEvent
	}
	if envelope.MessageID == "" {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return Event{}, fmt.Errorf("generate event identifier: %w", err)
		}
		envelope.MessageID = hex.EncodeToString(id[:])
	}
	if envelope.Data != nil {
		envelope.Data = append(json.RawMessage{}, envelope.Data...)
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if len(payload) > maxPayloadBytes {
		return Event{}, ErrMessageTooLarge
	}
	var scopes []CatalogScope
	if len(envelope.CatalogScopes) != 0 {
		scopes = make([]CatalogScope, len(envelope.CatalogScopes))
		for index, scope := range envelope.CatalogScopes {
			// A short identifier may be a substring of a much larger request or
			// snapshot. Retain only the bytes charged to this bounded event.
			scopes[index] = CatalogScope{ItemID: strings.Clone(scope.ItemID), LibraryID: strings.Clone(scope.LibraryID)}
		}
	}
	return Event{
		payload:       string(payload),
		messageType:   envelope.MessageType,
		messageID:     envelope.MessageID,
		authority:     envelope.Authority,
		catalogScopes: scopes,
		retainedBytes: len(payload) + scopeBytes,
	}, nil
}
