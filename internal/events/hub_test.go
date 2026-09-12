package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func TestPublishAllSharesOnePublicationAcrossTypedScopes(t *testing.T) {
	hub := newTestHub(t, Options{})
	first := subscribeTestScope(t, hub, "user-a", "session-a", "shared-device")
	second := subscribeTestScope(t, hub, "user-a", "session-b", "shared-device")
	other := subscribeTestScope(t, hub, "user-b", "session-c", "shared-device")
	applicationScope := Scope{SessionID: "application-a", CredentialID: "credential", ApplicationKey: true}
	application, err := hub.Subscribe(applicationScope)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := hub.Subscribe(Scope{SessionID: "application-b", CredentialID: "credential", ApplicationKey: true})
	if err != nil {
		t.Fatal(err)
	}
	subscribers := []*Subscription{first, second, other, application, sibling}
	if count, err := hub.PublishAll(Envelope{MessageType: "Changed", Data: json.RawMessage(`{"Value":1}`)}); err != nil || count != len(subscribers) {
		t.Fatalf("broadcast = (%d, %v), want %d", count, err, len(subscribers))
	}
	want := nextTestEvent(t, first)
	if want.MessageID() == "" {
		t.Fatal("broadcast has no generated publication identity")
	}
	for _, sub := range subscribers[1:] {
		got := nextTestEvent(t, sub)
		if got.MessageID() != want.MessageID() || !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Fatal("broadcast recipients did not share the same publication")
		}
	}

	targeted := Envelope{MessageType: "Changed", MessageID: "targeted"}
	if count, err := hub.PublishUser("user-a", targeted); err != nil || count != 2 {
		t.Fatalf("user publication after broadcast = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, first)
	_ = nextTestEvent(t, second)
	if count, err := hub.PublishSession("user-b", "session-c", targeted); err != nil || count != 1 {
		t.Fatalf("session publication after broadcast = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, other)
	if count, err := hub.PublishScope(applicationScope, targeted); err != nil || count != 1 {
		t.Fatalf("application publication after broadcast = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, application)
	for _, sub := range subscribers {
		assertNoTestEvent(t, sub)
	}
	if err := hub.Close(); err != nil {
		t.Fatal(err)
	}
	if count, err := hub.PublishAll(targeted); count != 0 || !errors.Is(err, ErrClosed) {
		t.Fatalf("broadcast after close = (%d, %v)", count, err)
	}
}

func TestPublishedCatalogScopesArePrivateImmutableSnapshots(t *testing.T) {
	hub := newTestHub(t, Options{})
	first := subscribeTestScope(t, hub, "user-a", "session-a", "")
	second := subscribeTestScope(t, hub, "user-b", "session-b", "")
	backing := strings.Repeat("x", 1<<20) + "private-itemprivate-library"
	itemID := backing[1<<20 : (1<<20)+len("private-item")]
	libraryID := backing[(1<<20)+len("private-item"):]
	input := []CatalogScope{{ItemID: itemID, LibraryID: libraryID}, {ItemID: itemID, LibraryID: "prior-library"}}
	want := append([]CatalogScope(nil), input...)
	envelope := Envelope{MessageType: "Changed", MessageID: "snapshot", Data: json.RawMessage(`{"Value":1}`), CatalogScopes: input}
	if count, err := hub.PublishAll(envelope); err != nil || count != 2 {
		t.Fatalf("catalog publication = (%d, %v)", count, err)
	}
	input[0] = CatalogScope{ItemID: "replaced-item", LibraryID: "replaced-library"}
	input[1].LibraryID = "changed-after-publication"
	firstEvent := nextTestEvent(t, first)
	got := firstEvent.CatalogScopes()
	if !slices.Equal(got, want) {
		t.Fatalf("published catalog scopes = %+v, want %+v", got, want)
	}
	// The event must own the identifier bytes, not keep a megabyte allocation
	// alive through a short substring. No mutation through these pointers occurs.
	if unsafe.StringData(got[0].ItemID) == unsafe.StringData(itemID) || unsafe.StringData(got[0].LibraryID) == unsafe.StringData(libraryID) {
		t.Fatal("catalog identifiers retained the producer's large backing storage")
	}
	got[0] = CatalogScope{ItemID: "recipient-change", LibraryID: "recipient-change"}
	secondEvent := nextTestEvent(t, second)
	if !slices.Equal(firstEvent.CatalogScopes(), want) || !slices.Equal(secondEvent.CatalogScopes(), want) {
		t.Fatal("a recipient changed the retained catalog snapshot")
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(firstEvent.Bytes(), &decoded); err != nil || len(decoded) != 3 {
		t.Fatalf("wire envelope = (%s, %v)", firstEvent.Bytes(), err)
	}
	if _, exists := decoded["CatalogScopes"]; exists || bytes.Contains(firstEvent.Bytes(), []byte("private-item")) || bytes.Contains(firstEvent.Bytes(), []byte("private-library")) {
		t.Fatal("private catalog metadata entered the client message")
	}
	var injected Envelope
	if err := json.Unmarshal([]byte(`{"MessageType":"Changed","CatalogScopes":[{"ItemID":"forged","LibraryID":"forged"}]}`), &injected); err != nil || len(injected.CatalogScopes) != 0 {
		t.Fatalf("JSON supplied trusted catalog scopes: %+v, %v", injected, err)
	}
	if scopes := (Event{}).CatalogScopes(); scopes != nil {
		t.Fatalf("zero event catalog scopes = %+v", scopes)
	}
}

func TestCatalogScopeValidationAndMessageBudgetPreserveExistingQueues(t *testing.T) {
	base := Envelope{MessageType: "Changed", MessageID: "id", Data: json.RawMessage(`{}`)}
	payload, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	valid := base
	valid.CatalogScopes = []CatalogScope{{ItemID: "item", LibraryID: "library"}}
	limit := len(payload) + catalogScopeOverheadBytes + len("item") + len("library")
	hub := newTestHub(t, Options{QueueBytes: 2 * limit, MaxMessageBytes: limit})
	sub := subscribeTestScope(t, hub, "user", "session", "")
	if count, err := hub.PublishAll(valid); err != nil || count != 1 {
		t.Fatalf("exact catalog message boundary = (%d, %v)", count, err)
	}
	tests := []struct {
		name   string
		scopes []CatalogScope
		want   error
	}{
		{"one extra retained byte", []CatalogScope{{ItemID: "item", LibraryID: "library!"}}, ErrMessageTooLarge},
		{"empty item", []CatalogScope{{LibraryID: "library"}}, ErrInvalidEvent},
		{"empty library", []CatalogScope{{ItemID: "item"}}, ErrInvalidEvent},
		{"blank item", []CatalogScope{{ItemID: " ", LibraryID: "library"}}, ErrInvalidEvent},
		{"padded library", []CatalogScope{{ItemID: "item", LibraryID: " library"}}, ErrInvalidEvent},
		{"NUL item", []CatalogScope{{ItemID: "item\x00", LibraryID: "library"}}, ErrInvalidEvent},
		{"invalid UTF-8 library", []CatalogScope{{ItemID: "item", LibraryID: string([]byte{0xff})}}, ErrInvalidEvent},
		{"oversized item", []CatalogScope{{ItemID: strings.Repeat("x", maxScopeFieldBytes+1), LibraryID: "library"}}, ErrInvalidEvent},
		{"oversized library", []CatalogScope{{ItemID: "item", LibraryID: strings.Repeat("x", maxScopeFieldBytes+1)}}, ErrInvalidEvent},
		{"excessive count", make([]CatalogScope, maxCatalogScopes+1), ErrMessageTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := base
			envelope.CatalogScopes = test.scopes
			if count, err := hub.PublishAll(envelope); count != 0 || !errors.Is(err, test.want) {
				t.Fatalf("invalid catalog publication = (%d, %v), want %v", count, err, test.want)
			}
			if sub.Reason() != nil {
				t.Fatalf("invalid catalog publication disconnected the subscriber: %v", sub.Reason())
			}
		})
	}
	event := nextTestEvent(t, sub)
	if !bytes.Equal(event.Bytes(), payload) || !slices.Equal(event.CatalogScopes(), valid.CatalogScopes) || event.retainedBytes != limit {
		t.Fatalf("the valid queued catalog event changed: %s, %+v, %d", event.Bytes(), event.CatalogScopes(), event.retainedBytes)
	}
	assertNoTestEvent(t, sub)
	// The count bound remains finite even when byte limits approach MaxInt.
	if _, err := encodeEvent(Envelope{MessageType: "Changed", CatalogScopes: make([]CatalogScope, maxCatalogScopes+1)}, int(^uint(0)>>1)); !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("excessive catalog scope count with a large byte limit = %v", err)
	}
}

func TestCatalogScopeQueueBytesAccumulateAndAreReleasedByNext(t *testing.T) {
	envelope := Envelope{MessageType: "Changed", MessageID: "id", Data: json.RawMessage(`{}`),
		CatalogScopes: []CatalogScope{{ItemID: "item", LibraryID: "library"}}}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	size := len(payload) + catalogScopeOverheadBytes + len("item") + len("library")
	hub := newTestHub(t, Options{QueueMessages: 16, QueueBytes: 2 * size, MaxMessageBytes: size})
	slow := subscribeTestScope(t, hub, "slow-user", "slow", "")
	fast := subscribeTestScope(t, hub, "fast-user", "fast", "")
	for index := 0; index < 2; index++ {
		if count, err := hub.PublishAll(envelope); err != nil || count != 2 {
			t.Fatalf("broadcast to retained byte boundary = (%d, %v)", count, err)
		}
		_ = nextTestEvent(t, fast)
	}
	_ = nextTestEvent(t, slow)
	if count, err := hub.PublishAll(envelope); err != nil || count != 2 {
		t.Fatalf("broadcast after catalog byte dequeue = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, fast)
	if count, err := hub.PublishAll(envelope); err != nil || count != 1 {
		t.Fatalf("broadcast beyond retained byte boundary = (%d, %v)", count, err)
	}
	assertTestClosed(t, slow, ErrSlowConsumer)
	if got := nextTestEvent(t, fast); !slices.Equal(got.CatalogScopes(), envelope.CatalogScopes) || fast.Reason() != nil {
		t.Fatal("the slow consumer affected another user's catalog event")
	}
	if fast.bytes != 0 || slow.bytes != 0 {
		t.Fatalf("catalog byte accounting after dequeue and removal = (%d, %d)", fast.bytes, slow.bytes)
	}
}

func TestAbsentCatalogScopesPreserveLegacyWireAndByteBudget(t *testing.T) {
	base := Envelope{MessageType: "Changed", MessageID: "id", Data: json.RawMessage(`{"Value":1}`)}
	want, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, scopes := range [][]CatalogScope{nil, {}} {
		hub := newTestHub(t, Options{QueueBytes: len(want), MaxMessageBytes: len(want)})
		sub := subscribeTestScope(t, hub, "user", "session", "")
		envelope := base
		envelope.CatalogScopes = scopes
		if count, err := hub.PublishAll(envelope); err != nil || count != 1 {
			t.Fatalf("scope-free event at legacy boundary = (%d, %v)", count, err)
		}
		got := nextTestEvent(t, sub)
		if !bytes.Equal(got.Bytes(), want) || got.retainedBytes != len(want) || got.CatalogScopes() != nil {
			t.Fatalf("scope-free event changed: %s, %d, %+v", got.Bytes(), got.retainedBytes, got.CatalogScopes())
		}
	}
}

func TestUserAndSessionIsolation(t *testing.T) {
	hub := newTestHub(t, Options{})
	first := subscribeTestScope(t, hub, "user-a", "session-a", "shared-device")
	second := subscribeTestScope(t, hub, "user-a", "session-b", "shared-device")
	other := subscribeTestScope(t, hub, "user-b", "session-c", "shared-device")
	if firstCount, otherCount := hub.CountForUser("user-a"), hub.CountForUser("user-b"); firstCount != 2 || otherCount != 1 {
		t.Fatalf("user counts = (%d, %d)", firstCount, otherCount)
	}
	if got := first.Scope(); got != (Scope{UserID: "user-a", SessionID: "session-a", DeviceID: "shared-device"}) {
		t.Fatalf("scope = %+v", got)
	}
	if count, err := hub.PublishUser("user-a", Envelope{MessageType: "UserDataChanged", Data: json.RawMessage(`{"ItemId":"item-a"}`)}); err != nil || count != 2 {
		t.Fatalf("publish user = (%d, %v)", count, err)
	}
	firstEvent, secondEvent := nextTestEvent(t, first), nextTestEvent(t, second)
	if firstEvent.MessageID() == "" || firstEvent.MessageID() != secondEvent.MessageID() || !bytes.Equal(firstEvent.Bytes(), secondEvent.Bytes()) {
		t.Fatal("recipients did not receive the same encoded event and identifier")
	}
	var decoded Envelope
	if err := json.Unmarshal(firstEvent.Bytes(), &decoded); err != nil || decoded.MessageType != "UserDataChanged" || decoded.MessageID != firstEvent.MessageID() {
		t.Fatalf("decoded event = (%+v, %v)", decoded, err)
	}
	assertNoTestEvent(t, other)

	targeted := Envelope{MessageType: "Playstate", MessageID: "command-id", Data: json.RawMessage(`{"Command":"Pause"}`)}
	if count, err := hub.PublishSession("user-a", "session-b", targeted); err != nil || count != 1 {
		t.Fatalf("publish session = (%d, %v)", count, err)
	}
	if got := nextTestEvent(t, second); got.MessageType() != "Playstate" || got.MessageID() != "command-id" {
		t.Fatalf("target event = %+v", got)
	}
	if count, err := hub.PublishSession("user-b", "session-a", targeted); err != nil || count != 0 {
		t.Fatalf("cross-user session publish = (%d, %v)", count, err)
	}
	assertNoTestEvent(t, first)
	assertNoTestEvent(t, other)
}

func TestPublishedPayloadIsImmutable(t *testing.T) {
	hub := newTestHub(t, Options{})
	first := subscribeTestScope(t, hub, "user", "first", "")
	second := subscribeTestScope(t, hub, "user", "second", "")
	input := json.RawMessage(`{"Value":"original"}`)
	want := append([]byte(nil), input...)
	if count, err := hub.PublishUser("user", Envelope{MessageType: "Changed", Data: input}); err != nil || count != 2 {
		t.Fatalf("publish = (%d, %v)", count, err)
	}
	for index := range input {
		input[index] = 'x'
	}
	firstEvent := nextTestEvent(t, first)
	encoded := firstEvent.Bytes()
	var envelope Envelope
	if err := json.Unmarshal(encoded, &envelope); err != nil || !bytes.Equal(envelope.Data, want) {
		t.Fatalf("published data = (%s, %v)", envelope.Data, err)
	}
	for index := range encoded {
		encoded[index] = 'x'
	}
	secondEvent := nextTestEvent(t, second)
	if !bytes.Equal(firstEvent.Bytes(), secondEvent.Bytes()) {
		t.Fatal("mutating one recipient's bytes changed another event")
	}
	if !json.Valid(firstEvent.Bytes()) {
		t.Fatal("mutating Bytes changed the retained event")
	}
}

func TestConnectionLimitsAndReleasedCapacity(t *testing.T) {
	tests := []struct {
		name   string
		opts   Options
		scopes []Scope
		excess Scope
		want   error
	}{
		{
			name: "global", opts: Options{MaxConnections: 2},
			scopes: []Scope{{UserID: "a", SessionID: "a"}, {UserID: "b", SessionID: "b"}},
			excess: Scope{UserID: "c", SessionID: "c"}, want: ErrConnectionLimit,
		},
		{
			name: "user", opts: Options{MaxConnectionsPerUser: 2},
			scopes: []Scope{{UserID: "a", SessionID: "a"}, {UserID: "a", SessionID: "b"}},
			excess: Scope{UserID: "a", SessionID: "c"}, want: ErrUserConnectionLimit,
		},
		{
			name: "session", opts: Options{MaxConnectionsPerSession: 2},
			scopes: []Scope{{UserID: "a", SessionID: "a", DeviceID: "first"}, {UserID: "a", SessionID: "a", DeviceID: "second"}},
			excess: Scope{UserID: "a", SessionID: "a", DeviceID: "third"}, want: ErrSessionConnectionLimit,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hub := newTestHub(t, test.opts)
			var first *Subscription
			for _, scope := range test.scopes {
				sub, err := hub.Subscribe(scope)
				if err != nil {
					t.Fatal(err)
				}
				if first == nil {
					first = sub
				}
			}
			if _, err := hub.Subscribe(test.excess); !errors.Is(err, test.want) {
				t.Fatalf("excess subscribe = %v, want %v", err, test.want)
			}
			before := hub.CountForSession(first.Scope().SessionID)
			if before == 0 {
				t.Fatal("active session was not counted")
			}
			_ = first.Close()
			_ = first.Close()
			if count := hub.CountForSession(first.Scope().SessionID); count != before-1 {
				t.Fatalf("count after closing twice = %d, want %d", count, before-1)
			}
			if _, err := hub.Subscribe(test.excess); err != nil {
				t.Fatalf("released capacity was not reusable: %v", err)
			}
		})
	}
}

func TestSlowConsumerDoesNotBlockOtherSubscribers(t *testing.T) {
	hub := newTestHub(t, Options{QueueMessages: 2})
	slow := subscribeTestScope(t, hub, "user", "slow", "")
	fast := subscribeTestScope(t, hub, "user", "fast", "")
	for index := 0; index < 3; index++ {
		envelope := Envelope{MessageType: "Changed", MessageID: fmt.Sprintf("event-%d", index)}
		result := make(chan publishTestResult, 1)
		go func() {
			count, err := hub.PublishUser("user", envelope)
			result <- publishTestResult{count: count, err: err}
		}()
		wantCount := 2
		if index == 2 {
			wantCount = 1
		}
		select {
		case got := <-result:
			if got.err != nil || got.count != wantCount {
				t.Fatalf("publish %d = (%d, %v), want %d", index, got.count, got.err, wantCount)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("publisher blocked on a slow subscriber")
		}
		if got := nextTestEvent(t, fast); got.MessageID() != envelope.MessageID {
			t.Fatalf("fast subscriber event = %q", got.MessageID())
		}
	}
	assertTestClosed(t, slow, ErrSlowConsumer)
	if count := hub.CountForSession("slow"); count != 0 {
		t.Fatalf("disconnected session count = %d", count)
	}
	if fast.Reason() != nil {
		t.Fatalf("fast subscriber was closed: %v", fast.Reason())
	}
	if _, err := hub.Subscribe(slow.Scope()); err != nil {
		t.Fatalf("slow subscriber capacity was not released: %v", err)
	}
}

func TestQueueByteBudgetAndDequeueAccounting(t *testing.T) {
	envelope := Envelope{MessageType: "Changed", MessageID: "id", Data: json.RawMessage(`{"Value":"content"}`)}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	hub := newTestHub(t, Options{QueueMessages: 16, QueueBytes: 2 * len(payload), MaxMessageBytes: len(payload)})
	sub := subscribeTestScope(t, hub, "user", "session", "")
	for index := 0; index < 2; index++ {
		if count, err := hub.PublishUser("user", envelope); err != nil || count != 1 {
			t.Fatalf("publish to byte boundary = (%d, %v)", count, err)
		}
	}
	_ = nextTestEvent(t, sub)
	if count, err := hub.PublishUser("user", envelope); err != nil || count != 1 {
		t.Fatalf("publish after dequeue = (%d, %v)", count, err)
	}
	if count, err := hub.PublishUser("user", envelope); err != nil || count != 0 {
		t.Fatalf("publish beyond byte boundary = (%d, %v)", count, err)
	}
	assertTestClosed(t, sub, ErrSlowConsumer)
}

func TestMessageSizeAndInvalidDataDoNotDisconnectSubscribers(t *testing.T) {
	envelope := Envelope{MessageType: "Changed", MessageID: "id", Data: json.RawMessage(`"value"`)}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	hub := newTestHub(t, Options{QueueBytes: 2 * len(payload), MaxMessageBytes: len(payload)})
	sub := subscribeTestScope(t, hub, "user", "session", "")
	if count, err := hub.PublishUser("user", envelope); err != nil || count != 1 {
		t.Fatalf("exact-size event = (%d, %v)", count, err)
	}
	tests := []struct {
		name     string
		envelope Envelope
		want     error
	}{
		{"encoded size", Envelope{MessageType: "Changed", MessageID: "id", Data: json.RawMessage(`"value!"`)}, ErrMessageTooLarge},
		{"raw size", Envelope{MessageType: "Changed", Data: json.RawMessage(strings.Repeat("x", len(payload)+1))}, ErrMessageTooLarge},
		{"invalid JSON", Envelope{MessageType: "Changed", Data: json.RawMessage(`{`)}, ErrInvalidEvent},
		{"invalid UTF-8", Envelope{MessageType: "Changed", Data: json.RawMessage{'"', 0xff, '"'}}, ErrInvalidEvent},
		{"empty type", Envelope{}, ErrInvalidEvent},
		{"blank id", Envelope{MessageType: "Changed", MessageID: " "}, ErrInvalidEvent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if count, err := hub.PublishUser("user", test.envelope); count != 0 || !errors.Is(err, test.want) {
				t.Fatalf("invalid publish = (%d, %v), want %v", count, err, test.want)
			}
			if sub.Reason() != nil {
				t.Fatalf("invalid publish disconnected subscriber: %v", sub.Reason())
			}
		})
	}
	if got := nextTestEvent(t, sub).Bytes(); !bytes.Equal(got, payload) {
		t.Fatalf("valid queued event changed: %s", got)
	}
}

func TestDisconnectIsScopedIdempotentAndDiscardsQueuedEvents(t *testing.T) {
	hub := newTestHub(t, Options{})
	first := subscribeTestScope(t, hub, "user-a", "session-a", "")
	second := subscribeTestScope(t, hub, "user-a", "session-b", "")
	other := subscribeTestScope(t, hub, "user-b", "session-c", "")
	if _, err := hub.PublishUser("user-a", Envelope{MessageType: "Changed"}); err != nil {
		t.Fatal(err)
	}
	hub.DisconnectSession("session-a")
	hub.DisconnectSession("session-a")
	assertTestClosed(t, first, ErrSessionRevoked)
	if second.Reason() != nil || other.Reason() != nil {
		t.Fatal("session disconnect affected another session")
	}
	hub.DisconnectUser("user-a")
	hub.DisconnectUser("user-a")
	assertTestClosed(t, second, ErrUserRevoked)
	assertTestClosed(t, first, ErrSessionRevoked)
	if firstCount, otherCount := hub.CountForUser("user-a"), hub.CountForUser("user-b"); firstCount != 0 || otherCount != 1 {
		t.Fatalf("user counts after disconnect = (%d, %d)", firstCount, otherCount)
	}
	if other.Reason() != nil {
		t.Fatal("user disconnect affected another user")
	}
	if count := hub.CountForSession("session-a") + hub.CountForSession("session-b"); count != 0 {
		t.Fatalf("disconnected session count = %d", count)
	}
	_ = other.Close()
	assertTestClosed(t, other, ErrUnsubscribed)
	_ = hub.Close()
	_ = hub.Close()
	assertTestClosed(t, other, ErrUnsubscribed)
}

func TestCloseWakesReadersAndPermanentlyClosesHub(t *testing.T) {
	hub := newTestHub(t, Options{})
	sub := subscribeTestScope(t, hub, "user", "session", "")
	result := make(chan error, 1)
	go func() {
		_, err := sub.Next(context.Background())
		result <- err
	}()
	_ = hub.Close()
	_ = hub.Close()
	select {
	case err := <-result:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("blocked Next = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("closed hub did not wake a reader")
	}
	assertTestClosed(t, sub, ErrClosed)
	if _, err := hub.Subscribe(sub.Scope()); !errors.Is(err, ErrClosed) {
		t.Fatalf("subscribe after close = %v", err)
	}
	if _, err := hub.PublishUser("user", Envelope{MessageType: "Changed"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("publish after close = %v", err)
	}
	if count := hub.CountForSession("session"); count != 0 {
		t.Fatalf("closed hub session count = %d", count)
	}
	if count := hub.CountForUser("user"); count != 0 {
		t.Fatalf("closed hub user count = %d", count)
	}
}

func TestNextCancellationLeavesSubscriptionActive(t *testing.T) {
	hub := newTestHub(t, Options{})
	sub := subscribeTestScope(t, hub, "user", "session", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sub.Next(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Next = %v", err)
	}
	if count, err := hub.PublishUser("user", Envelope{MessageType: "Changed"}); err != nil || count != 1 {
		t.Fatalf("publish after cancellation = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, sub)
}

func TestConcurrentPublishSubscribeDisconnectAndClose(t *testing.T) {
	hub := newTestHub(t, Options{MaxConnections: 128, MaxConnectionsPerUser: 128, MaxConnectionsPerSession: 128})
	start := make(chan struct{})
	closeHub := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	var subscriptionsMu sync.Mutex
	var subscriptions []*Subscription
	for index := 0; index < 8; index++ {
		sub := subscribeTestScope(t, hub, "user", fmt.Sprintf("session-%d", index%4), "")
		subscriptions = append(subscriptions, sub)
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for {
				if _, err := sub.Next(ctx); err != nil {
					return
				}
			}
		}()
	}
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for index := 0; index < 200; index++ {
				envelope := Envelope{MessageType: "Changed", Data: json.RawMessage(`{"Value":1}`)}
				var err error
				if index%3 == 0 {
					envelope.CatalogScopes = []CatalogScope{{ItemID: "item", LibraryID: "library"}, {ItemID: "item", LibraryID: "prior-library"}}
					_, err = hub.PublishAll(envelope)
				} else if index%3 == 1 {
					_, err = hub.PublishUser("user", envelope)
				} else {
					_, err = hub.PublishSession("user", fmt.Sprintf("session-%d", index%4), envelope)
				}
				if err != nil && !errors.Is(err, ErrClosed) {
					t.Errorf("concurrent publish: %v", err)
					return
				}
				if worker == 0 && index == 25 {
					close(closeHub)
				}
			}
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 200; index++ {
			scope := Scope{UserID: "user", SessionID: fmt.Sprintf("session-%d", index%4)}
			sub, err := hub.Subscribe(scope)
			if errors.Is(err, ErrClosed) {
				return
			}
			if err != nil {
				t.Errorf("concurrent subscribe: %v", err)
				return
			}
			subscriptionsMu.Lock()
			subscriptions = append(subscriptions, sub)
			subscriptionsMu.Unlock()
			_ = hub.CountForSession(scope.SessionID)
			_ = sub.Reason()
			_ = sub.Close()
			_ = sub.Close()
		}
	}()
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for index := 0; index < 200; index++ {
			hub.DisconnectSession(fmt.Sprintf("session-%d", index%4))
			if index%5 == 0 {
				hub.DisconnectUser("user")
			}
		}
	}()
	workers.Add(1)
	go func() {
		defer workers.Done()
		select {
		case <-closeHub:
		case <-ctx.Done():
		}
		_ = hub.Close()
		_ = hub.Close()
	}()
	close(start)
	finished := make(chan struct{})
	go func() {
		workers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("concurrent operations did not finish")
	}
	for _, sub := range subscriptions {
		select {
		case <-sub.Done():
		default:
			t.Fatal("subscription remained open after hub close")
		}
		if sub.Reason() == nil {
			t.Fatal("closed subscription has no reason")
		}
	}
	for index := 0; index < 4; index++ {
		if count := hub.CountForSession(fmt.Sprintf("session-%d", index)); count != 0 {
			t.Fatalf("session-%d count = %d", index, count)
		}
	}
}

func TestInvalidOptionsAndScopes(t *testing.T) {
	for _, opts := range []Options{
		{MaxConnections: -1}, {MaxConnectionsPerUser: -1}, {MaxConnectionsPerSession: -1},
		{QueueMessages: -1}, {QueueBytes: -1}, {MaxMessageBytes: -1},
		{QueueBytes: 32, MaxMessageBytes: 33},
	} {
		if _, err := New(opts); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("New(%+v) = %v", opts, err)
		}
	}
	hub := newTestHub(t, Options{})
	for _, scope := range []Scope{
		{}, {UserID: "user"}, {SessionID: "session"}, {UserID: " ", SessionID: "session"},
		{UserID: "user", SessionID: "session", DeviceID: "bad\x00device"},
		{UserID: "user", SessionID: "session", DeviceID: strings.Repeat("x", 257)},
	} {
		if _, err := hub.Subscribe(scope); !errors.Is(err, ErrInvalidScope) {
			t.Errorf("Subscribe(%+v) = %v", scope, err)
		}
	}
	if _, err := hub.PublishUser("", Envelope{MessageType: "Changed"}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("empty user publish = %v", err)
	}
	if _, err := hub.PublishSession("user", "", Envelope{MessageType: "Changed"}); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("empty session publish = %v", err)
	}
}

type publishTestResult struct {
	count int
	err   error
}

func newTestHub(t *testing.T, opts Options) *Hub {
	t.Helper()
	hub, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	return hub
}

func subscribeTestScope(t *testing.T, hub *Hub, userID, sessionID, deviceID string) *Subscription {
	t.Helper()
	sub, err := hub.Subscribe(Scope{UserID: userID, SessionID: sessionID, DeviceID: deviceID})
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func nextTestEvent(t *testing.T, sub *Subscription) Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func assertNoTestEvent(t *testing.T, sub *Subscription) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if event, err := sub.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected event or close = (%s, %v)", event.Bytes(), err)
	}
}

func assertTestClosed(t *testing.T, sub *Subscription, want error) {
	t.Helper()
	select {
	case <-sub.Done():
	default:
		t.Fatal("subscription Done remained open")
	}
	if err := sub.Reason(); !errors.Is(err, want) {
		t.Fatalf("close reason = %v, want %v", err, want)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if event, err := sub.Next(ctx); !errors.Is(err, want) {
		t.Fatalf("closed Next = (%s, %v), want %v", event.Bytes(), err, want)
	}
}
