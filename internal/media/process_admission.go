package media

import (
	"context"
	"errors"
	"sync"
)

const (
	mediaProcessOwnerLimit      = 4
	mediaProcessBackgroundLimit = 2
	mediaProcessQueueLimit      = 128
)

var ErrProcessCapacity = errors.New("media process admission queue is full")

type backgroundProcessContextKey struct{}

// WithBackgroundProcess classifies subprocesses started with this context as
// background work. The marker inherits the caller's cancellation and deadline.
// It does not acquire capacity, and cannot raise a background operation's limit.
func WithBackgroundProcess(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	if backgroundMediaProcess(ctx) {
		return ctx
	}
	return context.WithValue(ctx, backgroundProcessContextKey{}, true)
}

func backgroundMediaProcess(ctx context.Context) bool {
	background, _ := ctx.Value(backgroundProcessContextKey{}).(bool)
	return background
}

// This budget covers media helper subprocesses, including scan probes and
// analysis. The transcode manager retains its separate execution budget.
var mediaProcessAdmission = newMediaProcessAdmission(mediaProcessOwnerLimit, mediaProcessBackgroundLimit, mediaProcessQueueLimit)

type mediaProcessWaiter struct {
	ctx        context.Context
	background bool
	ready      chan struct{}
	granted    bool
}

type mediaProcessGovernor struct {
	mu              sync.Mutex
	limit           int
	backgroundLimit int
	queueLimit      int
	active          int
	background      int
	waiters         []*mediaProcessWaiter
}

func newMediaProcessAdmission(limit, backgroundLimit, queueLimit int) *mediaProcessGovernor {
	return &mediaProcessGovernor{limit: limit, backgroundLimit: backgroundLimit, queueLimit: queueLimit}
}

func (governor *mediaProcessGovernor) available(background bool) bool {
	return governor.active < governor.limit && (!background || governor.background < governor.backgroundLimit)
}

func (governor *mediaProcessGovernor) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	background := backgroundMediaProcess(ctx)
	governor.mu.Lock()
	if len(governor.waiters) == 0 && governor.available(background) {
		governor.charge(background)
		governor.mu.Unlock()
		return governor.release(background), nil
	}
	// Canceled waiters cannot consume queue capacity or obstruct eligibility.
	governor.dispatch()
	if len(governor.waiters) >= governor.queueLimit {
		governor.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, ErrProcessCapacity
	}
	waiter := &mediaProcessWaiter{ctx: ctx, background: background, ready: make(chan struct{})}
	governor.waiters = append(governor.waiters, waiter)
	governor.dispatch()
	governor.mu.Unlock()
	select {
	case <-waiter.ready:
	case <-ctx.Done():
	}
	governor.mu.Lock()
	if waiter.granted {
		if err := ctx.Err(); err != nil {
			governor.uncharge(background)
			governor.dispatch()
			governor.mu.Unlock()
			return nil, err
		}
		governor.mu.Unlock()
		return governor.release(background), nil
	}
	governor.remove(waiter)
	governor.dispatch()
	governor.mu.Unlock()
	return nil, ctx.Err()
}

func (governor *mediaProcessGovernor) charge(background bool) {
	governor.active++
	if background {
		governor.background++
	}
}

func (governor *mediaProcessGovernor) uncharge(background bool) {
	governor.active--
	if background {
		governor.background--
	}
}

func (governor *mediaProcessGovernor) release(background bool) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			governor.mu.Lock()
			governor.uncharge(background)
			governor.dispatch()
			governor.mu.Unlock()
		})
	}
}

func (governor *mediaProcessGovernor) remove(waiter *mediaProcessWaiter) {
	for index, queued := range governor.waiters {
		if queued == waiter {
			governor.removeAt(index)
			return
		}
	}
}

func (governor *mediaProcessGovernor) removeAt(index int) {
	copy(governor.waiters[index:], governor.waiters[index+1:])
	governor.waiters[len(governor.waiters)-1] = nil
	governor.waiters = governor.waiters[:len(governor.waiters)-1]
}

// The oldest eligible waiter wins. A background waiter limited by its own
// budget does not hold a global slot or block an eligible foreground waiter.
func (governor *mediaProcessGovernor) dispatch() {
	for index := 0; index < len(governor.waiters); {
		waiter := governor.waiters[index]
		if waiter.ctx.Err() != nil {
			governor.removeAt(index)
			close(waiter.ready)
			continue
		}
		if governor.available(waiter.background) {
			governor.removeAt(index)
			governor.charge(waiter.background)
			waiter.granted = true
			close(waiter.ready)
			continue
		}
		index++
	}
}
