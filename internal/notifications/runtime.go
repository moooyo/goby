package notifications

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/notificationjournal"
)

type Payload struct {
	Version        int                             `json:"Version"`
	EventID        string                          `json:"EventId"`
	RegistrationID string                          `json:"RegistrationId"`
	Generation     string                          `json:"Generation"`
	Kind           string                          `json:"Kind"`
	OccurredAt     time.Time                       `json:"OccurredAt"`
	References     []notificationjournal.Reference `json:"References"`
	Recursive      bool                            `json:"Recursive"`
}
type activeAttempt struct {
	target target
	cancel context.CancelFunc
	done   chan struct{}
}

const notificationSenderLimit = 4

type Runtime struct {
	store    *Store
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	ready    chan struct{}
	mu       sync.Mutex
	closed   bool
	active   map[string]*activeAttempt
	claiming chan struct{}
	options  RuntimeOptions
}

// RootCAs is explicit deployment trust. Nil uses system roots; custom roots
// never disable certificate chain or hostname verification.
type RuntimeOptions struct{ RootCAs *x509.CertPool }

func NewRuntime(store *Store, options ...RuntimeOptions) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{store: store, ctx: ctx, cancel: cancel, done: make(chan struct{}), ready: make(chan struct{}, 1), active: map[string]*activeAttempt{}}
	if len(options) > 0 && options[0].RootCAs != nil {
		r.options.RootCAs = options[0].RootCAs.Clone()
	}
	go r.run()
	return r
}
func (r *Runtime) BeginClose() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.closed = true
	r.cancel()
	for _, a := range r.active {
		a.cancel()
	}
	r.mu.Unlock()
}
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.BeginClose()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (r *Runtime) fence(ctx context.Context, match func(target) bool) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	waiting := []<-chan struct{}{}
	claiming := r.claiming
	for _, a := range r.active {
		if match(a.target) {
			a.cancel()
			waiting = append(waiting, a.done)
		}
	}
	r.mu.Unlock()
	if claiming != nil {
		select {
		case <-claiming:
		case <-ctx.Done():
			return ctx.Err()
		}
		// A claim already in flight may not have exposed its owner when the
		// fence began. Admission publishes ownership before closing this gate.
		r.mu.Lock()
		for _, a := range r.active {
			if match(a.target) {
				a.cancel()
				waiting = append(waiting, a.done)
			}
		}
		r.mu.Unlock()
	}
	for _, done := range waiting {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (r *Runtime) FenceSession(ctx context.Context, id string) error {
	return r.fence(ctx, func(t target) bool { return t.session == id })
}
func (r *Runtime) FenceUser(ctx context.Context, id string) error {
	return r.fence(ctx, func(t target) bool { return t.user == id })
}

// ActiveForSession is a safe process-ownership observation for shutdown and
// delivery diagnostics. It does not expose a token or authorize an operation.
func (r *Runtime) ActiveForSession(id string) int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, a := range r.active {
		if a.target.session == id {
			count++
		}
	}
	return count
}
func (r *Runtime) FenceRegistration(ctx context.Context, id string, current int64) error {
	return r.fence(ctx, func(t target) bool { return t.id == id && t.regRevision < current })
}
func (r *Runtime) FenceConfig(ctx context.Context, current int64) error {
	return r.fence(ctx, func(t target) bool { return t.configRevision < current })
}
func (r *Runtime) run() {
	defer close(r.done)
	senderDone := make(chan struct{})
	go func() {
		defer close(senderDone)
		r.send()
	}()
	defer func() {
		r.cancel()
		<-senderDone
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var nextHistory time.Time
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
		}
		ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
		err := r.store.maintain(ctx)
		if err == nil {
			err = r.store.fanout(ctx)
		}
		if time.Now().After(nextHistory) {
			if r.store.pruneHistory(ctx) == nil {
				nextHistory = time.Now().Add(time.Hour)
			}
		}
		cancel()
		// Backpressure must let committed deliveries drain, including a
		// prefix committed by this fanout pass. Other failures still retry.
		if err != nil && !errors.Is(err, ErrLimit) {
			continue
		}
		r.signal()
	}
}

// send owns admission while each attempt owns its transport and observers. A
// database lease can expire before transport workers retire, so the occupied
// registration set, rather than lease state, limits real sender ownership.
func (r *Runtime) send() {
	var workers sync.WaitGroup
	defer workers.Wait()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.ready:
		case <-ticker.C:
		}
		for {
			r.mu.Lock()
			if r.closed || len(r.active) >= notificationSenderLimit {
				r.mu.Unlock()
				break
			}
			occupied := make([]string, 0, len(r.active))
			for id := range r.active {
				occupied = append(occupied, id)
			}
			r.claiming = make(chan struct{})
			r.mu.Unlock()
			ctx, cancel := context.WithTimeout(r.ctx, 3*time.Second)
			d, err := r.store.claim(ctx, occupied)
			cancel()
			if errors.Is(err, pgx.ErrNoRows) {
				r.endClaim()
				break
			}
			if err != nil {
				r.endClaim()
				break
			}
			ctx, attempt, ok := r.beginAttempt(d)
			r.endClaim()
			if !ok {
				r.finish(d, "pending", "interrupted", time.Second)
				break
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer r.endAttempt(d.registration, attempt)
				r.deliver(ctx, d, attempt.cancel)
			}()
		}
	}
}

func (r *Runtime) endClaim() {
	r.mu.Lock()
	close(r.claiming)
	r.claiming = nil
	r.mu.Unlock()
}

func (r *Runtime) beginAttempt(d delivery) (context.Context, *activeAttempt, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || len(r.active) >= notificationSenderLimit || r.active[d.registration] != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	// The claim returns the immutable registration owner and both generations.
	// Fences can therefore match this worker even during its first target read.
	a := &activeAttempt{target: target{id: d.registration, session: d.session, user: d.user, regRevision: d.regRevision, configRevision: d.configRevision}, cancel: cancel, done: make(chan struct{})}
	r.active[d.registration] = a
	return ctx, a, true
}

func (r *Runtime) endAttempt(registration string, attempt *activeAttempt) {
	attempt.cancel()
	r.mu.Lock()
	delete(r.active, registration)
	close(attempt.done)
	r.mu.Unlock()
	r.signal()
}

func (r *Runtime) signal() {
	select {
	case r.ready <- struct{}{}:
	default:
	}
}

func (r *Runtime) deliver(ctx context.Context, d delivery, cancel context.CancelFunc) {
	t, err := r.store.currentTarget(ctx, d.registration)
	if err != nil {
		r.fail(d, err)
		return
	}
	if t.regRevision != d.regRevision || t.configRevision != d.configRevision {
		r.finish(d, "cancelled", "generation_changed", 0)
		return
	}
	refs, err := r.store.authorizedRefs(ctx, t, d.refs)
	if err != nil {
		r.fail(d, err)
		return
	}
	if len(refs) == 0 && d.kind != "Test" && !(d.kind == "ResyncRequired" && len(d.refs) == 0) {
		r.finish(d, "suppressed", "source_unavailable", 0)
		return
	}
	wireRefs := append([]notificationjournal.Reference{}, refs...)
	for index := range wireRefs {
		wireRefs[index].SourceID = ""
	}
	payload := Payload{Version: 1, EventID: d.id, RegistrationID: t.id, Generation: identity.NotificationGeneration(t.regRevision), Kind: d.kind, OccurredAt: d.created.UTC(), References: wireRefs, Recursive: d.recursive}
	if payload.Kind == "ResyncRequired" {
		payload.References = []notificationjournal.Reference{}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		r.finish(d, "failed", "invalid_payload", 0)
		return
	}
	if len(raw) > 32*1024 {
		payload.Kind = "ResyncRequired"
		payload.References = []notificationjournal.Reference{}
		raw, _ = json.Marshal(payload)
	}
	credential, err := r.store.users.OpenNotificationSecret(ctx, identity.NotificationReceiverPurpose, identity.NotificationSecretBinding("receiver", identity.NotificationGeneration(t.credentialGeneration)), t.credential)
	if err != nil {
		r.finish(d, "failed", "secret_unavailable", 0)
		return
	}
	token, err := r.store.users.OpenNotificationSecret(ctx, identity.NotificationTargetPurpose, t.targetBinding(), t.token)
	if err != nil {
		r.finish(d, "failed", "secret_unavailable", 0)
		return
	}
	if err = r.checkDelivery(ctx, t, refs); err != nil {
		r.fail(d, err)
		return
	}
	watchDone := make(chan struct{})
	var watchErr error
	go func() {
		defer close(watchDone)
		timer := time.NewTicker(250 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if err := r.checkDelivery(ctx, t, refs); err != nil {
					// Finishing an acknowledged request cancels this observer too.
					// That cancellation is not a new authority/database failure.
					if ctx.Err() != nil {
						return
					}
					watchErr = err
					cancel()
					return
				}
			}
		}
	}()
	outcome, delay := postWebhook(ctx, t, credential, token, d.id, raw, r.options, func(check context.Context) error { return r.checkDelivery(check, t, refs) })
	cancel()
	<-watchDone
	if watchErr != nil && r.ctx.Err() == nil {
		r.fail(d, watchErr)
		return
	}
	state := "failed"
	switch outcome {
	case "delivered":
		state = "delivered"
	case "retry", "source_changed":
		outcome = "retry"
		if d.attempt < 5 && time.Since(d.created) < 24*time.Hour {
			state = "pending"
			if delay <= 0 {
				delay = time.Second * time.Duration(1<<uint(d.attempt))
			}
		} else {
			outcome = "retry_exhausted"
		}
	case "cancelled":
		if r.ctx.Err() != nil {
			state, outcome, delay = "pending", "interrupted", time.Second
		} else {
			check, stop := context.WithTimeout(context.Background(), 2*time.Second)
			err := r.checkDelivery(check, t, refs)
			stop()
			if err != nil {
				r.fail(d, err)
				return
			}
			state, outcome, delay = "pending", "retry", time.Second
		}
	case "authority_revoked", "source_unavailable":
		state = "suppressed"
	}
	r.finish(d, state, outcome, delay)
}

func (r *Runtime) fail(d delivery, err error) {
	state, code, delay := failureDisposition(d.attempt, err)
	r.finish(d, state, code, delay)
}
func failureDisposition(attempt int, err error) (string, string, time.Duration) {
	code := safeCode(err)
	if code == "unavailable" {
		if attempt < 5 {
			return "pending", "retry", time.Second * time.Duration(1<<uint(attempt))
		}
		return "failed", "retry_exhausted", 0
	}
	return "suppressed", code, 0
}

var errSourceVisibilityChanged = errors.New("notification source visibility changed")

func (r *Runtime) checkDelivery(ctx context.Context, t target, expected []notificationjournal.Reference) error {
	fresh, err := r.store.currentTargetRevisions(ctx, t.id)
	if err != nil {
		return err
	}
	if fresh.regRevision != t.regRevision || fresh.configRevision != t.configRevision {
		return identity.ErrUnauthorized
	}
	actor, err := r.store.users.RevalidateSessionAuthority(ctx, t.principal())
	if err != nil {
		return err
	}
	if len(expected) == 0 {
		return nil
	}
	refs, err := r.store.catalog.FilterNotificationReferences(ctx, library.Subject{UserID: t.user, Actor: &actor}, expected)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return library.ErrNotFound
	}
	if !slices.Equal(refs, expected) {
		return errSourceVisibilityChanged
	}
	return nil
}
func (r *Runtime) finish(d delivery, state, code string, delay time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = r.store.finish(ctx, d, state, code, delay)
}
