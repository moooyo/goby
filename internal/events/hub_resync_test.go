package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDisconnectAllDiscardsQueuedEventsAndReleasesEveryQuota(t *testing.T) {
	hub := newTestHub(t, Options{MaxConnections: 4, MaxConnectionsPerUser: 2, MaxConnectionsPerSession: 1, QueueMessages: 2})
	ordinary := subscribeTestScope(t, hub, "owner", "ordinary-a", "shared-device")
	other := subscribeTestScope(t, hub, "owner", "ordinary-b", "shared-device")
	application := applicationTestSubscription(t, hub, "owner", "application-a")
	sibling := applicationTestSubscription(t, hub, "owner", "application-b")
	subs := []*Subscription{ordinary, other, application, sibling}
	queued := Envelope{MessageType: "Changed", MessageID: "before-resync", Data: json.RawMessage(`{"Value":1}`),
		CatalogScopes: []CatalogScope{{ItemID: "retained-item", LibraryID: "retained-library"}}}
	for index := 0; index < 2; index++ {
		if count, err := hub.PublishAll(queued); err != nil || count != len(subs) {
			t.Fatalf("queue before resynchronization = (%d, %v), want %d", count, err, len(subs))
		}
	}
	hub.DisconnectAll()
	hub.DisconnectAll()
	for _, sub := range subs {
		assertTestClosed(t, sub, ErrResyncRequired)
		if event, err := sub.Next(context.Background()); !errors.Is(err, ErrResyncRequired) || len(event.Bytes()) != 0 || len(event.CatalogScopes()) != 0 {
			t.Fatalf("resynchronization retained a readable event: %v", err)
		}
		hub.mu.Lock()
		retained := len(sub.queue) != 0 || sub.head != 0 || sub.count != 0 || sub.bytes != 0
		hub.mu.Unlock()
		if retained {
			t.Fatal("resynchronization retained subscription queue storage or byte accounting")
		}
		if count := hub.CountForSession(sub.Scope().SessionID); count != 0 {
			t.Fatalf("session quota after resynchronization = %d", count)
		}
	}
	hub.mu.Lock()
	retained := len(hub.subscribers) != 0 || len(hub.userCounts) != 0 || len(hub.credentialCounts) != 0 || len(hub.sessionCounts) != 0
	hub.mu.Unlock()
	if retained || hub.CountForUser("owner") != 0 {
		t.Fatal("resynchronization retained connection quotas")
	}
	if count, err := hub.PublishAll(queued); err != nil || count != 0 {
		t.Fatalf("publication without current subscribers = (%d, %v)", count, err)
	}

	replacements := make([]*Subscription, len(subs))
	for index, sub := range subs {
		replacement, err := hub.Subscribe(sub.Scope())
		if err != nil {
			t.Fatalf("reuse released connection quotas: %v", err)
		}
		replacements[index] = replacement
	}
	fresh := Envelope{MessageType: "Changed", MessageID: "after-resync", Data: json.RawMessage(`{"Value":2}`)}
	if count, err := hub.PublishAll(fresh); err != nil || count != len(replacements) {
		t.Fatalf("publication after resubscription = (%d, %v), want %d", count, err, len(replacements))
	}
	want := nextTestEvent(t, replacements[0])
	if want.MessageID() != fresh.MessageID {
		t.Fatal("replacement subscription received an event from before resynchronization")
	}
	for _, sub := range replacements[1:] {
		got := nextTestEvent(t, sub)
		if got.MessageID() != fresh.MessageID || !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Fatal("replacement subscriptions did not share the new publication")
		}
	}
	if count, err := hub.PublishUser("owner", fresh); err != nil || count != 2 {
		t.Fatalf("typed user publication after resynchronization = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, replacements[0])
	_ = nextTestEvent(t, replacements[1])
	assertNoTestEvent(t, replacements[2])
	assertNoTestEvent(t, replacements[3])
	if count, err := hub.PublishScope(replacements[2].Scope(), fresh); err != nil || count != 1 {
		t.Fatalf("exact application publication after resynchronization = (%d, %v)", count, err)
	}
	_ = nextTestEvent(t, replacements[2])
	for _, sub := range []*Subscription{replacements[0], replacements[1], replacements[3]} {
		assertNoTestEvent(t, sub)
	}
}

func TestDisconnectAllWakesOrdinaryAndApplicationReaders(t *testing.T) {
	hub := newTestHub(t, Options{})
	subs := []*Subscription{
		subscribeTestScope(t, hub, "reader", "ordinary-reader", ""),
		applicationTestSubscription(t, hub, "reader-key", "application-reader"),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		event Event
		err   error
	}
	results := make(chan result, len(subs))
	for _, sub := range subs {
		go func(sub *Subscription) {
			event, err := sub.Next(ctx)
			results <- result{event: event, err: err}
		}(sub)
	}
	hub.DisconnectAll()
	for range subs {
		select {
		case got := <-results:
			if !errors.Is(got.err, ErrResyncRequired) || len(got.event.Bytes()) != 0 {
				t.Fatalf("reader after resynchronization = (%s, %v)", got.event.Bytes(), got.err)
			}
		case <-ctx.Done():
			t.Fatal("resynchronization did not wake a subscription reader")
		}
	}
}

func TestDisconnectAllAndClosePreserveTheFirstReasonAndClosedHub(t *testing.T) {
	for _, closeFirst := range []bool{false, true} {
		name := "resynchronize-then-close"
		if closeFirst {
			name = "close-then-resynchronize"
		}
		t.Run(name, func(t *testing.T) {
			hub := newTestHub(t, Options{})
			previous := subscribeTestScope(t, hub, "previous", "previous-session", "")
			hub.DisconnectSession(previous.Scope().SessionID)
			ordinary := subscribeTestScope(t, hub, "current", "current-session", "")
			application := applicationTestSubscription(t, hub, "current-key", "current-application")
			if count, err := hub.PublishAll(Envelope{MessageType: "Changed"}); err != nil || count != 2 {
				t.Fatalf("queue before shutdown sequence = (%d, %v)", count, err)
			}
			want := ErrResyncRequired
			if closeFirst {
				if err := hub.Close(); err != nil {
					t.Fatal(err)
				}
				want = ErrClosed
			}
			for index := 0; index < 2; index++ {
				hub.DisconnectAll()
				if err := hub.Close(); err != nil {
					t.Fatal(err)
				}
			}
			assertTestClosed(t, previous, ErrSessionRevoked)
			assertTestClosed(t, ordinary, want)
			assertTestClosed(t, application, want)
			for _, sub := range []*Subscription{ordinary, application} {
				if _, err := hub.Subscribe(sub.Scope()); !errors.Is(err, ErrClosed) {
					t.Fatalf("resynchronization revived a closed hub: %v", err)
				}
			}
			if count, err := hub.PublishAll(Envelope{MessageType: "Changed"}); count != 0 || !errors.Is(err, ErrClosed) {
				t.Fatalf("publication after permanent close = (%d, %v)", count, err)
			}
		})
	}
}
