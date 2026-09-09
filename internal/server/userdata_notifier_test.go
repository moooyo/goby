package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/library"
)

func TestUserDataNotifierCoalescesPendingMarkersAndReadsLatestState(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	sub := subscribeNotifierUser(t, hub, "user", "session")
	notifier.Enqueue("user", "blocker", false)
	blocking := nextNotificationRequest(t, store)
	notifier.Enqueue("user", "target", false)
	notifier.Enqueue("user", "target", true)
	notifier.Enqueue("user", "target", false)
	notifier.Enqueue("user", "sentinel", false)
	blocking.reply <- notificationStoreResult{}
	request := nextNotificationRequest(t, store)
	if request.query.UserID != "user" || request.query.ItemID != "target" || !request.query.Recursive || request.query.Limit != 64 || request.query.AfterID != "" {
		t.Fatalf("coalesced query = %+v", request.query)
	}
	// The simulated database returns a value committed after Enqueue; there is
	// no response snapshot in the queue that could override this current state.
	request.reply <- notificationStoreResult{page: library.UserDataNotificationResult{Items: []library.UserData{
		{ItemID: "target", PlayCount: 42, Played: true},
	}}}
	message := nextNotificationEvent(t, sub)
	if message.UserID != "user" || len(message.UserDataList) != 1 || message.UserDataList[0].ItemID != "target" || message.UserDataList[0].PlayCount != 42 || !message.UserDataList[0].Played {
		t.Fatalf("current notification = %+v", message)
	}
	sentinel := nextNotificationRequest(t, store)
	if sentinel.query.ItemID != "sentinel" {
		t.Fatalf("duplicate pending marker caused another read: %+v", sentinel.query)
	}
	sentinel.reply <- notificationStoreResult{}
}

func TestUserDataNotifierRequeuesChangesMadeDuringAnActiveRead(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	sub := subscribeNotifierUser(t, hub, "user", "session")
	notifier.Enqueue("user", "target", false)
	first := nextNotificationRequest(t, store)
	if first.query.Recursive {
		t.Fatal("first query was unexpectedly recursive")
	}
	for index := 0; index < 10; index++ {
		notifier.Enqueue("user", "target", index == 5)
	}
	first.reply <- notificationStoreResult{page: library.UserDataNotificationResult{Items: []library.UserData{{ItemID: "target", PlayCount: 1}}}}
	if got := nextNotificationEvent(t, sub); got.UserDataList[0].PlayCount != 1 {
		t.Fatalf("first observed state = %+v", got)
	}
	second := nextNotificationRequest(t, store)
	if second.query.ItemID != "target" || !second.query.Recursive || second.query.AfterID != "" {
		t.Fatalf("follow-up query = %+v", second.query)
	}
	notifier.Enqueue("user", "sentinel", false)
	second.reply <- notificationStoreResult{page: library.UserDataNotificationResult{Items: []library.UserData{{ItemID: "target", PlayCount: 2}}}}
	if got := nextNotificationEvent(t, sub); got.UserDataList[0].PlayCount != 2 {
		t.Fatalf("final observed state = %+v", got)
	}
	sentinel := nextNotificationRequest(t, store)
	if sentinel.query.ItemID != "sentinel" {
		t.Fatalf("active duplicates were not coalesced: %+v", sentinel.query)
	}
	sentinel.reply <- notificationStoreResult{}
}

func TestUserDataNotifierPagesInOrderWithSharedIDsAndUserIsolation(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	first := subscribeNotifierUser(t, hub, "user-a", "first")
	second := subscribeNotifierUser(t, hub, "user-a", "second")
	other := subscribeNotifierUser(t, hub, "user-b", "other")
	notifier.Enqueue("user-a", "root", true)
	pageOne := nextNotificationRequest(t, store)
	if pageOne.query.Limit != userDataNotificationPageSize || !pageOne.query.Recursive || pageOne.query.AfterID != "" {
		t.Fatalf("first page query = %+v", pageOne.query)
	}
	pageOne.reply <- notificationStoreResult{page: library.UserDataNotificationResult{
		Items: []library.UserData{{ItemID: "item-a"}, {ItemID: "item-b"}}, NextAfterID: "item-b",
	}}
	firstMessage := nextNotificationEvent(t, first)
	secondMessage := nextNotificationEvent(t, second)
	if firstMessage.messageID == "" || firstMessage.messageID != secondMessage.messageID || firstMessage.UserID != "user-a" || len(firstMessage.UserDataList) != 2 {
		t.Fatalf("first page messages = (%+v, %+v)", firstMessage, secondMessage)
	}
	pageTwo := nextNotificationRequest(t, store)
	if pageTwo.query.AfterID != "item-b" || pageTwo.query.ItemID != "root" || pageTwo.query.Limit != userDataNotificationPageSize {
		t.Fatalf("second page query = %+v", pageTwo.query)
	}
	pageTwo.reply <- notificationStoreResult{page: library.UserDataNotificationResult{Items: []library.UserData{{ItemID: "item-c", IsFavorite: true}}}}
	lastFirst, lastSecond := nextNotificationEvent(t, first), nextNotificationEvent(t, second)
	if lastFirst.messageID != lastSecond.messageID || lastFirst.messageID == firstMessage.messageID || len(lastFirst.UserDataList) != 1 || lastFirst.UserDataList[0].ItemID != "item-c" || !lastFirst.UserDataList[0].IsFavorite {
		t.Fatalf("last page messages = (%+v, %+v)", lastFirst, lastSecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if event, err := other.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cross-user delivery = (%s, %v)", event.Bytes(), err)
	}
}

func TestUserDataNotifierSkipsUsersWithoutSubscribers(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	_ = subscribeNotifierUser(t, hub, "active", "active-session")
	departing := subscribeNotifierUser(t, hub, "departing", "departing-session")
	notifier.Enqueue("active", "blocker", false)
	blocking := nextNotificationRequest(t, store)
	notifier.Enqueue("offline", "never-read", false)
	notifier.Enqueue("departing", "also-never-read", false)
	_ = departing.Close()
	notifier.Enqueue("active", "sentinel", false)
	blocking.reply <- notificationStoreResult{}
	sentinel := nextNotificationRequest(t, store)
	if sentinel.query.UserID != "active" || sentinel.query.ItemID != "sentinel" {
		t.Fatalf("read for a user without subscribers: %+v", sentinel.query)
	}
	sentinel.reply <- notificationStoreResult{}
}

func TestUserDataNotifierDatabaseFailureRequiresOnlyAffectedUserToResync(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	failed := subscribeNotifierUser(t, hub, "failed", "failed-session")
	healthy := subscribeNotifierUser(t, hub, "healthy", "healthy-session")
	notifier.Enqueue("failed", "target", false)
	failing := nextNotificationRequest(t, store)
	notifier.Enqueue("failed", "discarded", false)
	notifier.Enqueue("healthy", "target", false)
	failing.reply <- notificationStoreResult{err: errors.New("database unavailable")}
	assertNotificationClosed(t, failed)
	next := nextNotificationRequest(t, store)
	if next.query.UserID != "healthy" {
		t.Fatalf("failed user kept pending work: %+v", next.query)
	}
	next.reply <- notificationStoreResult{page: library.UserDataNotificationResult{Items: []library.UserData{{ItemID: "target", Played: true}}}}
	if got := nextNotificationEvent(t, healthy); got.UserID != "healthy" || !got.UserDataList[0].Played {
		t.Fatalf("healthy user's notification = %+v", got)
	}
}

func TestUserDataNotifierPerUserOverflowCancelsAndReleasesCapacity(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	overflowed := subscribeNotifierUser(t, hub, "overflow", "overflow-session")
	healthy := subscribeNotifierUser(t, hub, "healthy", "healthy-session")
	notifier.Enqueue("overflow", "active", false)
	active := nextNotificationRequest(t, store)
	for index := 1; index < userDataNotificationPerUser; index++ {
		notifier.Enqueue("overflow", fmt.Sprintf("item-%d", index), false)
	}
	if overflowed.Reason() != nil {
		t.Fatal("user was disconnected before reaching capacity")
	}
	notifier.Enqueue("overflow", "one-too-many", false)
	assertNotificationClosed(t, overflowed)
	select {
	case <-active.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("overflow did not cancel the active database read")
	}
	notifier.Enqueue("healthy", "target", false)
	next := nextNotificationRequest(t, store)
	if next.query.UserID != "healthy" {
		t.Fatalf("overflow left pending work for its user: %+v", next.query)
	}
	next.reply <- notificationStoreResult{page: library.UserDataNotificationResult{Items: []library.UserData{{ItemID: "target"}}}}
	_ = nextNotificationEvent(t, healthy)
	if healthy.Reason() != nil {
		t.Fatalf("overflow affected another user: %v", healthy.Reason())
	}
}

func TestUserDataNotifierGlobalOverflowDoesNotEvictOtherUsers(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	blocker := subscribeNotifierUser(t, hub, "blocker", "blocker-session")
	notifier.Enqueue("blocker", "active", false)
	active := nextNotificationRequest(t, store)
	var retained []*events.Subscription
	remaining := userDataNotificationCapacity - 1
	for index := 0; remaining > 0; index++ {
		userID := fmt.Sprintf("queued-%d", index)
		retained = append(retained, subscribeNotifierUser(t, hub, userID, userID+"-session"))
		count := min(remaining, userDataNotificationPerUser)
		for item := 0; item < count; item++ {
			notifier.Enqueue(userID, fmt.Sprintf("item-%d", item), false)
		}
		remaining -= count
	}
	overflowed := subscribeNotifierUser(t, hub, "overflow", "overflow-session")
	notifier.Enqueue("overflow", "target", false)
	assertNotificationClosed(t, overflowed)
	if blocker.Reason() != nil || active.ctx.Err() != nil {
		t.Fatal("global overflow interrupted another user's active work")
	}
	for _, sub := range retained {
		if sub.Reason() != nil {
			t.Fatalf("global overflow disconnected another user: %v", sub.Reason())
		}
	}
	notifier.mu.Lock()
	count := len(notifier.pending)
	notifier.mu.Unlock()
	if count != userDataNotificationCapacity {
		t.Fatalf("pending marker count = %d", count)
	}
}

func TestUserDataNotifierCloseCancelsWaitsAndIsIdempotent(t *testing.T) {
	notifier, store, hub := newTestUserDataNotifier(t)
	sub := subscribeNotifierUser(t, hub, "user", "session")
	notifier.Enqueue("user", "active", false)
	active := nextNotificationRequest(t, store)
	if deadline, ok := active.ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > userDataNotificationTimeout {
		t.Fatalf("task deadline = (%v, %v)", deadline, ok)
	}
	notifier.Enqueue("user", "discarded", false)
	var workers sync.WaitGroup
	for index := 0; index < 3; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			notifier.Close()
		}()
	}
	finished := make(chan struct{})
	go func() {
		workers.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not wait for a canceled worker to exit")
	}
	if !errors.Is(active.ctx.Err(), context.Canceled) {
		t.Fatalf("active query cancellation = %v", active.ctx.Err())
	}
	select {
	case <-notifier.done:
	default:
		t.Fatal("Close returned before the worker exited")
	}
	notifier.Enqueue("user", "ignored-after-close", false)
	select {
	case request := <-store.requests:
		t.Fatalf("closed notifier started a query: %+v", request.query)
	default:
	}
	if sub.Reason() != nil {
		t.Fatalf("notifier Close took ownership of the Hub: %v", sub.Reason())
	}
}

func TestUserDataNotifierCanceledWorkCannotReachReconnectedUser(t *testing.T) {
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hub.Close() })
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	reads := 0
	store := notificationStoreFunc(func(ctx context.Context, query library.UserDataNotificationQuery) (library.UserDataNotificationResult, error) {
		reads++
		if reads == 1 {
			entered <- ctx
			<-release
		}
		return library.UserDataNotificationResult{Items: []library.UserData{{ItemID: query.ItemID, PlayCount: reads}}}, nil
	})
	notifier := newUserDataNotifier(store, hub)
	var releaseOnce sync.Once
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		notifier.Close()
	})
	old := subscribeNotifierUser(t, hub, "user", "old-session")
	notifier.Enqueue("user", "active", false)
	var active context.Context
	select {
	case active = <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("database read did not begin")
	}
	for index := 1; index <= userDataNotificationPerUser; index++ {
		notifier.Enqueue("user", fmt.Sprintf("item-%d", index), false)
	}
	assertNotificationClosed(t, old)
	if active.Err() == nil {
		t.Fatal("overflow did not cancel old work")
	}
	reconnected := subscribeNotifierUser(t, hub, "user", "new-session")
	notifier.Enqueue("user", "active", false)
	releaseOnce.Do(func() { close(release) })
	if got := nextNotificationEvent(t, reconnected); got.UserDataList[0].PlayCount != 2 {
		t.Fatalf("canceled state reached the new connection: %+v", got)
	}
	notifier.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if event, err := reconnected.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled old task reached new connection: (%s, %v)", event.Bytes(), err)
	}
}

type notificationStoreResult struct {
	page library.UserDataNotificationResult
	err  error
}

type notificationStoreRequest struct {
	ctx   context.Context
	query library.UserDataNotificationQuery
	reply chan notificationStoreResult
}

type notificationTestStore struct {
	requests chan notificationStoreRequest
}

func (s *notificationTestStore) UserDataNotificationPage(ctx context.Context, query library.UserDataNotificationQuery) (library.UserDataNotificationResult, error) {
	request := notificationStoreRequest{ctx: ctx, query: query, reply: make(chan notificationStoreResult, 1)}
	select {
	case s.requests <- request:
	case <-ctx.Done():
		return library.UserDataNotificationResult{}, ctx.Err()
	}
	select {
	case result := <-request.reply:
		return result.page, result.err
	case <-ctx.Done():
		return library.UserDataNotificationResult{}, ctx.Err()
	}
}

type notificationStoreFunc func(context.Context, library.UserDataNotificationQuery) (library.UserDataNotificationResult, error)

func (fn notificationStoreFunc) UserDataNotificationPage(ctx context.Context, query library.UserDataNotificationQuery) (library.UserDataNotificationResult, error) {
	return fn(ctx, query)
}

func newTestUserDataNotifier(t *testing.T) (*userDataNotifier, *notificationTestStore, *events.Hub) {
	t.Helper()
	hub, err := events.New(events.Options{})
	if err != nil {
		t.Fatal(err)
	}
	store := &notificationTestStore{requests: make(chan notificationStoreRequest, 1)}
	notifier := newUserDataNotifier(store, hub)
	t.Cleanup(func() {
		notifier.Close()
		_ = hub.Close()
	})
	return notifier, store, hub
}

func subscribeNotifierUser(t *testing.T, hub *events.Hub, userID, sessionID string) *events.Subscription {
	t.Helper()
	sub, err := hub.Subscribe(events.Scope{UserID: userID, SessionID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

func nextNotificationRequest(t *testing.T, store *notificationTestStore) notificationStoreRequest {
	t.Helper()
	select {
	case request := <-store.requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("notification database read did not begin")
		return notificationStoreRequest{}
	}
}

type notificationTestMessage struct {
	messageID    string
	UserID       string `json:"UserId"`
	UserDataList []struct {
		ItemID     string `json:"ItemId"`
		PlayCount  int
		Played     bool
		IsFavorite bool
	} `json:"UserDataList"`
}

func nextNotificationEvent(t *testing.T, sub *events.Subscription) notificationTestMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	event, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var envelope events.Envelope
	if err := json.Unmarshal(event.Bytes(), &envelope); err != nil || envelope.MessageType != "UserDataChanged" || envelope.MessageID == "" {
		t.Fatalf("notification envelope = (%+v, %v)", envelope, err)
	}
	var message notificationTestMessage
	if err := json.Unmarshal(envelope.Data, &message); err != nil {
		t.Fatal(err)
	}
	message.messageID = envelope.MessageID
	if len(message.UserDataList) == 0 {
		t.Fatal("notification had no user data")
	}
	return message
}

func assertNotificationClosed(t *testing.T, sub *events.Subscription) {
	t.Helper()
	select {
	case <-sub.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("affected user was not disconnected for resynchronization")
	}
	if !errors.Is(sub.Reason(), events.ErrUserRevoked) {
		t.Fatalf("resynchronization close reason = %v", sub.Reason())
	}
}
