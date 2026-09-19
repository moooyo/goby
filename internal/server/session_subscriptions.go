package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/events"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

var errSessionSubscription = errors.New("invalid Sessions subscription")

type sessionSubscriptionState struct {
	revision uint64
	active   bool
	delay    time.Duration
	interval time.Duration
}

// Each socket owns its subscription. Sharing a credential never subscribes a
// second connection or creates a durable/offline notification queue.
type sessionSubscription struct {
	mu    sync.Mutex
	state sessionSubscriptionState
	wake  chan struct{}
}

func newSessionSubscription() *sessionSubscription {
	return &sessionSubscription{wake: make(chan struct{}, 1)}
}

func parseSessionSubscription(message string, data json.RawMessage) (sessionSubscriptionState, error) {
	state := sessionSubscriptionState{active: message == "SessionsStart", interval: time.Second}
	if message != "SessionsStart" && message != "SessionsStop" {
		return state, errSessionSubscription
	}
	var value string
	if len(data) > 0 {
		if bytes.Equal(bytes.TrimSpace(data), []byte("null")) || json.Unmarshal(data, &value) != nil {
			return state, errSessionSubscription
		}
	}
	if message == "SessionsStop" {
		if value != "" {
			return state, errSessionSubscription
		}
		return state, nil
	}
	if value == "" {
		return state, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return state, errSessionSubscription
	}
	delay, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
	if err != nil || delay > 60_000 {
		return state, errSessionSubscription
	}
	interval, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil || interval < 1000 || interval > 60_000 {
		return state, errSessionSubscription
	}
	state.delay = time.Duration(delay) * time.Millisecond
	state.interval = time.Duration(interval) * time.Millisecond
	return state, nil
}

func (subscription *sessionSubscription) handle(message string, data json.RawMessage) error {
	if message != "SessionsStart" && message != "SessionsStop" {
		return nil
	}
	state, err := parseSessionSubscription(message, data)
	if err != nil {
		return err
	}
	subscription.mu.Lock()
	state.revision = subscription.state.revision + 1
	subscription.state = state
	subscription.mu.Unlock()
	select {
	case subscription.wake <- struct{}{}:
	default:
	}
	return nil
}

func (subscription *sessionSubscription) snapshot() sessionSubscriptionState {
	subscription.mu.Lock()
	defer subscription.mu.Unlock()
	return subscription.state
}

// socketSessionPayload performs the same session and media projection as HTTP.
// Its default image projection contains tags only, never paths or credentials.
func (s *Server) socketSessionPayload(ctx context.Context, principal identity.Principal) ([]byte, error) {
	sessions, err := s.identity.ListClientSessions(ctx, principal, identity.ClientSessionFilter{})
	if err != nil {
		return nil, err
	}
	result, items, err := s.clientSessionSnapshot(ctx, principal, sessions)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id, ok := item["Id"].(string); ok {
			ids = append(ids, id)
		}
	}
	images := map[string][]library.Image{}
	if len(ids) > 0 {
		images, err = s.library.ImagesForItemsFor(ctx, librarySubject(principal, principal.User.ID), ids)
		if err != nil {
			return nil, err
		}
	}
	for _, item := range items {
		id, _ := item["Id"].(string)
		tags, backdrops := map[string]string{}, []string{}
		for _, image := range images[id] {
			if image.ImageType == "Backdrop" {
				if len(backdrops) < 32 {
					backdrops = append(backdrops, image.Tag)
				}
				continue
			}
			if _, exists := tags[image.ImageType]; !exists {
				tags[image.ImageType] = image.Tag
			}
		}
		item["ImageTags"], item["BackdropImageTags"] = tags, backdrops
	}
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(events.Envelope{MessageType: "Sessions", Data: data})
	if err != nil {
		return nil, err
	}
	if len(payload) > events.DefaultMaxMessageBytes {
		return nil, events.ErrInvalidEvent
	}
	return payload, nil
}

func sameSessionSnapshotAuthority(before, after identity.Principal) bool {
	return before.User.ID == after.User.ID && before.IsApplicationKey() == after.IsApplicationKey() &&
		before.CanManageServer() == after.CanManageServer() && before.SessionID == after.SessionID &&
		bytes.Equal(before.User.Policy, after.User.Policy)
}
