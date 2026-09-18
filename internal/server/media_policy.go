package server

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	mediaPolicyIdleTTL   = 90 * time.Second
	maxMediaPolicyLeases = 4096
)

// A lease describes actual media delivery in this server process. Prepared
// playback rows and manifest negotiation do not acquire leases. A canonical
// play ID groups ranges, output revisions and HLS variants; legacy original
// URLs without one group by credential, device and source. Such URLs cannot
// distinguish two players of the same source on the same authenticated device.
type mediaPolicyKey struct {
	application                                     bool
	owner, auth, client, device, play, item, source string
}

type mediaPolicyLease struct {
	key       mediaPolicyKey
	scope     transcode.Scope
	ctx       context.Context
	cancel    context.CancelFunc
	refs      int
	order     uint64
	last      time.Time
	timer     *time.Timer
	version   uint64
	complete  bool
	failed    bool
	delivered bool
}

type mediaPolicyRuntime struct {
	mu       sync.Mutex
	leases   map[mediaPolicyKey]*mediaPolicyLease
	sequence uint64
	closed   bool
	now      func() time.Time
	onRetire func(transcode.Scope)
}

type mediaPolicyLeaseContextKey struct{}

func newMediaPolicyRuntime(onRetire func(transcode.Scope)) *mediaPolicyRuntime {
	return &mediaPolicyRuntime{leases: make(map[mediaPolicyKey]*mediaPolicyLease), now: time.Now, onRetire: onRetire}
}

func mediaPolicyIdentity(principal identity.Principal, scope transcode.Scope) (mediaPolicyKey, int, error) {
	if scope.ApplicationKey != principal.IsApplicationKey() || scope.UserID != principal.User.ID || scope.AuthSessionID != principal.SessionID ||
		scope.ApplicationClientID != principal.ClientSessionID || scope.DeviceID != principal.Client.DeviceID || scope.AuthSessionID == "" ||
		scope.ItemID == "" || scope.SourceID == "" || len(scope.PlaySessionID) > 256 || len(scope.ItemID) > 256 || len(scope.SourceID) > 256 {
		return mediaPolicyKey{}, 0, library.ErrForbidden
	}
	key := mediaPolicyKey{application: scope.ApplicationKey, owner: scope.UserID, auth: scope.AuthSessionID,
		client: scope.ApplicationClientID, device: scope.DeviceID, play: scope.PlaySessionID, item: scope.ItemID, source: scope.SourceID}
	if scope.ApplicationKey {
		key.owner = scope.AuthSessionID
		return key, 0, nil
	}
	if principal.User.ID == "" {
		return mediaPolicyKey{}, 0, library.ErrForbidden
	}
	policy, err := identity.ParseRuntimePolicy(principal.User.Policy)
	if err != nil {
		return key, 0, library.ErrForbidden
	}
	return key, policy.SimultaneousStreamLimit, nil
}

func mediaPolicySameOwner(a, b mediaPolicyKey) bool {
	return a.application == b.application && a.owner == b.owner
}

func (runtime *mediaPolicyRuntime) retireLocked(lease *mediaPolicyLease) transcode.Scope {
	delete(runtime.leases, lease.key)
	if lease.timer != nil {
		lease.timer.Stop()
	}
	lease.cancel()
	return lease.scope
}

func (runtime *mediaPolicyRuntime) notify(scopes []transcode.Scope) {
	if runtime.onRetire != nil {
		for _, scope := range scopes {
			runtime.onRetire(scope)
		}
	}
}

// Reconciliation keeps the oldest admitted plays when a policy is tightened.
// Cancellation happens before a replacement is admitted; callbacks run outside
// the lock so conversion cancellation can safely reenter this shared gate.
func (runtime *mediaPolicyRuntime) reconcileLocked(key mediaPolicyKey, limit int) []transcode.Scope {
	var retired []transcode.Scope
	now := runtime.now()
	var owned []*mediaPolicyLease
	for _, lease := range runtime.leases {
		if lease.refs == 0 && now.Sub(lease.last) >= mediaPolicyIdleTTL {
			retired = append(retired, runtime.retireLocked(lease))
			continue
		}
		if mediaPolicySameOwner(lease.key, key) {
			owned = append(owned, lease)
		}
	}
	if limit > 0 && len(owned) > limit {
		sort.Slice(owned, func(i, j int) bool { return owned[i].order < owned[j].order })
		for _, lease := range owned[limit:] {
			retired = append(retired, runtime.retireLocked(lease))
		}
	}
	return retired
}

func (runtime *mediaPolicyRuntime) acquire(ctx context.Context, principal identity.Principal, scope transcode.Scope) (context.Context, func(), error) {
	key, limit, err := mediaPolicyIdentity(principal, scope)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	runtime.mu.Lock()
	if runtime.closed {
		runtime.mu.Unlock()
		return nil, nil, context.Canceled
	}
	retired := runtime.reconcileLocked(key, limit)
	lease := runtime.leases[key]
	if lease == nil {
		count := 0
		for _, active := range runtime.leases {
			if mediaPolicySameOwner(active.key, key) {
				count++
			}
		}
		if len(runtime.leases) >= maxMediaPolicyLeases || limit > 0 && count >= limit {
			runtime.mu.Unlock()
			runtime.notify(retired)
			return nil, nil, transcode.ErrBusy
		}
		lifetime, cancel := context.WithCancel(context.Background())
		runtime.sequence++
		lease = &mediaPolicyLease{key: key, scope: scope, ctx: lifetime, cancel: cancel, order: runtime.sequence}
		runtime.leases[key] = lease
	}
	lease.refs++
	lease.last, lease.complete, lease.failed = runtime.now(), false, false
	lease.version++
	if lease.timer != nil {
		lease.timer.Stop()
		lease.timer = nil
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lease.ctx, cancel)
	if lease.ctx.Err() != nil {
		cancel()
	}
	var once sync.Once
	done := func() {
		once.Do(func() {
			stop()
			cancel()
			runtime.finish(lease)
		})
	}
	return context.WithValue(work, mediaPolicyLeaseContextKey{}, lease), done, nil
}

func (runtime *mediaPolicyRuntime) finish(lease *mediaPolicyLease) {
	runtime.mu.Lock()
	if runtime.leases[lease.key] != lease {
		runtime.mu.Unlock()
		return
	}
	lease.refs--
	var retired []transcode.Scope
	if lease.refs == 0 {
		if lease.complete {
			scope := runtime.retireLocked(lease)
			// Successful delivery releases quota without revoking the output
			// revision used for prepared presence and completed-cache reuse.
			// Failed startup still has to cancel any unfinished producer.
			if lease.failed {
				retired = append(retired, scope)
			}
		} else {
			lease.last = runtime.now()
			lease.version++
			version := lease.version
			lease.timer = time.AfterFunc(mediaPolicyIdleTTL, func() { runtime.expire(lease, version) })
		}
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}

func (runtime *mediaPolicyRuntime) expire(lease *mediaPolicyLease, version uint64) {
	runtime.mu.Lock()
	var retired []transcode.Scope
	if runtime.leases[lease.key] == lease && lease.refs == 0 && lease.version == version && runtime.now().Sub(lease.last) >= mediaPolicyIdleTTL {
		retired = append(retired, runtime.retireLocked(lease))
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}

func (runtime *mediaPolicyRuntime) check(principal identity.Principal, scope transcode.Scope) error {
	key, limit, err := mediaPolicyIdentity(principal, scope)
	if err != nil {
		return err
	}
	runtime.mu.Lock()
	before := runtime.leases[key]
	retired := runtime.reconcileLocked(key, limit)
	removed := before != nil && runtime.leases[key] != before
	runtime.mu.Unlock()
	runtime.notify(retired)
	if removed {
		return library.ErrForbidden
	}
	return nil
}

func (runtime *mediaPolicyRuntime) cancelMatching(authID, playID string) {
	runtime.mu.Lock()
	var retired []transcode.Scope
	for _, lease := range runtime.leases {
		if lease.key.auth == authID && (playID == "" || lease.key.play == playID) {
			retired = append(retired, runtime.retireLocked(lease))
		}
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}

func (runtime *mediaPolicyRuntime) release(scope transcode.Scope) {
	runtime.mu.Lock()
	var retired []transcode.Scope
	for _, lease := range runtime.leases {
		if lease.scope == scope {
			retired = append(retired, runtime.retireLocked(lease))
		}
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}

func (runtime *mediaPolicyRuntime) complete(scope transcode.Scope, expected *mediaPolicyLease) {
	runtime.mu.Lock()
	for _, lease := range runtime.leases {
		if lease == expected && lease.scope == scope {
			lease.complete, lease.failed = true, false
			if lease.refs == 0 {
				runtime.retireLocked(lease)
			}
		}
	}
	runtime.mu.Unlock()
}

// A failed request only rolls back an unshared lease that has never delivered
// media. Failure of an obsolete segment after a seek must not complete another
// reader's playback or cancel a replacement producer.
func (runtime *mediaPolicyRuntime) fail(scope transcode.Scope, expected *mediaPolicyLease) {
	runtime.mu.Lock()
	var retired []transcode.Scope
	for _, lease := range runtime.leases {
		if lease == expected && lease.scope == scope && !lease.delivered && lease.refs <= 1 {
			lease.complete, lease.failed = true, true
			if lease.refs == 0 {
				retired = append(retired, runtime.retireLocked(lease))
			}
		}
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}

func (runtime *mediaPolicyRuntime) touch(principal identity.Principal, scope transcode.Scope, expected *mediaPolicyLease) {
	key, _, err := mediaPolicyIdentity(principal, scope)
	if err != nil {
		return
	}
	runtime.mu.Lock()
	if lease := runtime.leases[key]; lease != nil && lease == expected {
		lease.last = runtime.now()
		lease.delivered, lease.complete, lease.failed = true, false, false
		if lease.refs == 0 {
			if lease.timer != nil {
				lease.timer.Stop()
			}
			lease.version++
			version := lease.version
			lease.timer = time.AfterFunc(mediaPolicyIdleTTL, func() { runtime.expire(lease, version) })
		}
	}
	runtime.mu.Unlock()
}

func (runtime *mediaPolicyRuntime) stop() {
	runtime.mu.Lock()
	runtime.closed = true
	var retired []transcode.Scope
	for _, lease := range runtime.leases {
		retired = append(retired, runtime.retireLocked(lease))
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}

func (s *Server) playbackPolicyGate() *mediaPolicyRuntime {
	s.mediaPolicyOnce.Do(func() {
		s.mediaPolicy = newMediaPolicyRuntime(func(scope transcode.Scope) {
			if scope.PlaySessionID != "" {
				s.hls.cancelMatching(scope.AuthSessionID, scope.PlaySessionID)
				s.cancelDynamicStreams(scope.AuthSessionID, scope.PlaySessionID)
			}
		})
	})
	return s.mediaPolicy
}

func (s *Server) acquireMediaPolicy(ctx context.Context, principal identity.Principal, scope transcode.Scope) (context.Context, func(), error) {
	return s.playbackPolicyGate().acquire(ctx, principal, scope)
}

func (s *Server) checkMediaPolicy(principal identity.Principal, scope transcode.Scope) error {
	return s.playbackPolicyGate().check(principal, scope)
}

func mediaPolicyContextLease(ctx context.Context) *mediaPolicyLease {
	lease, _ := ctx.Value(mediaPolicyLeaseContextKey{}).(*mediaPolicyLease)
	return lease
}

func (s *Server) touchMediaPolicy(ctx context.Context, principal identity.Principal, scope transcode.Scope) {
	s.playbackPolicyGate().touch(principal, scope, mediaPolicyContextLease(ctx))
}

func (s *Server) releaseMediaPolicy(scope transcode.Scope) { s.playbackPolicyGate().release(scope) }
func (s *Server) completeMediaPolicy(ctx context.Context, scope transcode.Scope) {
	s.playbackPolicyGate().complete(scope, mediaPolicyContextLease(ctx))
}
func (s *Server) failMediaPolicy(ctx context.Context, scope transcode.Scope) {
	s.playbackPolicyGate().fail(scope, mediaPolicyContextLease(ctx))
}
func (s *Server) cancelMediaPolicy(authID, playID string) {
	s.playbackPolicyGate().cancelMatching(authID, playID)
}
func (s *Server) stopMediaPolicy() { s.playbackPolicyGate().stop() }

func (s *Server) cancelMediaPolicySource(principal identity.Principal, itemID, sourceID string) {
	runtime := s.playbackPolicyGate()
	runtime.mu.Lock()
	var retired []transcode.Scope
	for _, lease := range runtime.leases {
		if lease.key.play == "" && lease.key.auth == principal.SessionID && lease.key.client == principal.ClientSessionID &&
			lease.key.device == principal.Client.DeviceID && lease.key.item == itemID && lease.key.source == sourceID {
			retired = append(retired, runtime.retireLocked(lease))
		}
	}
	runtime.mu.Unlock()
	runtime.notify(retired)
}
