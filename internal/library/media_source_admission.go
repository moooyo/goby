package library

import (
	"context"
	"os"
	"sync"

	"github.com/moooyo/goby/internal/media"
)

const (
	mediaSourceOwnerLimit            = 4
	analysisSourceOwnerLimit         = 3
	mediaSourceQueueRetainedCapacity = 64
	mediaSourceRootOwnerLimit        = 3
	analysisSourceRootOwnerLimit     = 2
	mediaSourceScopedQueueLimit      = 128
)

// The process-wide budget limits source-open work, not the lifetime of a file
// successfully delivered to its caller. Analysis can own at most three slots;
// foreground-only work can use all four. A stuck foreground owner retains its
// slot, so four blocked foreground operations can also block analysis.
var mediaSourceAdmission = newMediaSourceAdmission()

type mediaSourceWaiter struct {
	ctx          context.Context
	background   bool
	ready        chan struct{}
	granted      bool
	released     bool
	root         mediaSourceRootKey
	domain       string
	unclassified bool
	queued       bool
	cleanup      bool
}

type mediaSourceAdmissionLimits struct {
	owners, background, rootOwners, rootBackground, unclassified int
}

type mediaSourceRootKey struct {
	catalog string
	id      string
}

type mediaSourceLaneCount struct {
	active, background int
}

type mediaSourceAdmissionQueue struct {
	mu           sync.Mutex
	active       int
	background   int
	waiters      []*mediaSourceWaiter
	roots        map[mediaSourceRootKey]mediaSourceLaneCount
	domains      map[string]mediaSourceLaneCount
	limits       mediaSourceAdmissionLimits
	unclassified mediaSourceLaneCount
	cleanupSpace chan struct{}
}

func newMediaSourceAdmission() *mediaSourceAdmissionQueue {
	return newMediaSourceAdmissionWithLimits(mediaSourceAdmissionLimits{mediaSourceOwnerLimit, analysisSourceOwnerLimit, mediaSourceRootOwnerLimit, analysisSourceRootOwnerLimit, 0})
}

func newMediaSourceAdmissionWithLimits(limits mediaSourceAdmissionLimits) *mediaSourceAdmissionQueue {
	return &mediaSourceAdmissionQueue{limits: limits}
}

func (a *mediaSourceAdmissionQueue) enqueue(ctx context.Context, background bool) (*mediaSourceWaiter, error) {
	return a.request(ctx, background, false)
}

func (a *mediaSourceAdmissionQueue) request(ctx context.Context, background, immediate bool) (*mediaSourceWaiter, error) {
	return a.requestRoot(ctx, background, immediate, mediaSourceRootKey{}, "", 0)
}

func (a *mediaSourceAdmissionQueue) requestRoot(ctx context.Context, background, immediate bool, root mediaSourceRootKey, domain string, queueLimit int) (*mediaSourceWaiter, error) {
	return a.requestRootPolicy(ctx, background, immediate, root, domain, queueLimit, false)
}

func (a *mediaSourceAdmissionQueue) requestRootPolicy(ctx context.Context, background, immediate bool, root mediaSourceRootKey, domain string, queueLimit int, cleanup bool) (*mediaSourceWaiter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	waiter := &mediaSourceWaiter{ctx: ctx, background: background, root: root, domain: domain, unclassified: root.id == "" && a.limits.unclassified > 0, cleanup: cleanup}
	a.mu.Lock()
	a.dispatchLocked()
	if immediate && a.eligibleLocked(waiter) {
		// An uncontended owner needs neither a wakeup channel nor queue storage.
		// Dispatching older eligible work first also permits a healthy domain
		// to use spare capacity when a saturated domain fills the queue.
		a.grantLocked(waiter)
	} else {
		queued := len(a.waiters)
		if cleanup {
			queued = 0
			for _, previous := range a.waiters {
				if previous.cleanup {
					queued++
				}
			}
		}
		if queueLimit > 0 && queued >= queueLimit {
			a.mu.Unlock()
			return nil, ErrBusy
		}
		waiter.queued = true
		waiter.ready = make(chan struct{})
		a.waiters = append(a.waiters, waiter)
		a.dispatchLocked()
	}
	a.mu.Unlock()
	return waiter, nil
}

// Only a bounded handoff owner may request cleanup. Its descriptor and Store
// lifetime remain owned while waiting, so cancellation must not abandon Close.
func (a *mediaSourceAdmissionQueue) acquireCleanupRoot(root mediaSourceRootKey, domain string) (func(), error) {
	for {
		waiter, err := a.requestRootPolicy(context.Background(), false, true, root, domain, 8, true)
		if err == nil {
			return a.wait(waiter)
		}
		if err != ErrBusy {
			return nil, err
		}
		// Handoff ownership already bounds these callers. Retain the real
		// descriptor and lifetime if a full cleanup queue needs to drain.
		a.mu.Lock()
		count := 0
		for _, queued := range a.waiters {
			if queued.cleanup {
				count++
			}
		}
		if count < 8 {
			a.mu.Unlock()
			continue
		}
		if a.cleanupSpace == nil {
			a.cleanupSpace = make(chan struct{})
		}
		space := a.cleanupSpace
		a.mu.Unlock()
		<-space
	}
}

func (a *mediaSourceAdmissionQueue) acquireRoot(ctx context.Context, background bool, root mediaSourceRootKey, domain string) (func(), error) {
	release, _, err := a.acquireRootWithQueueFact(ctx, background, root, domain, nil)
	return release, err
}

// The queue fact describes whether this exact request was ever enqueued, not
// elapsed time or the ready channel's current state. A caller with fresh local
// facts must discard them in beforeQueuedWait before any capacity wait. That
// callback runs outside the governor lock and must perform only memory work.
func (a *mediaSourceAdmissionQueue) acquireRootWithQueueFact(ctx context.Context, background bool, root mediaSourceRootKey, domain string, beforeQueuedWait func()) (func(), bool, error) {
	if root.catalog == "" || root.id == "" || domain == "" {
		return nil, false, ErrUnavailable
	}
	waiter, err := a.requestRoot(ctx, background, true, root, domain, mediaSourceScopedQueueLimit)
	if err != nil {
		return nil, false, err
	}
	// Even a callback panic must not abandon an already granted or queued owner.
	returned := false
	defer func() {
		if !returned {
			a.release(waiter)
		}
	}()
	if waiter.queued && beforeQueuedWait != nil {
		beforeQueuedWait()
	}
	release, err := a.wait(waiter)
	returned = true
	return release, waiter.queued, err
}

func (a *mediaSourceAdmissionQueue) eligibleLocked(waiter *mediaSourceWaiter) bool {
	if a.active >= a.limits.owners || (waiter.background && a.background >= a.limits.background) {
		return false
	}
	if waiter.unclassified {
		if a.unclassified.active >= a.limits.unclassified {
			return false
		}
		for _, count := range a.roots {
			if count.active+a.unclassified.active >= a.limits.rootOwners ||
				(waiter.background && count.background+a.unclassified.background >= a.limits.rootBackground) {
				return false
			}
		}
		for domain := range a.domains {
			if !a.domainEligibleLocked(domain, waiter.background) {
				return false
			}
		}
		return true
	}
	if waiter.root.id == "" {
		return true
	}
	root := a.roots[waiter.root]
	if root.active+a.unclassified.active >= a.limits.rootOwners || (waiter.background && root.background+a.unclassified.background >= a.limits.rootBackground) {
		return false
	}
	if !a.domainEligibleLocked(waiter.domain, waiter.background) {
		return false
	}
	// Every live ancestor also governs its other active descendants. Checking
	// only the incoming path would miss a sibling's charge to their shared
	// parent. Live domains are bounded by this governor's owner limit.
	for path := range a.domains {
		if pathWithin(path, waiter.domain) && !a.domainEligibleLocked(path, waiter.background) {
			return false
		}
	}
	return true
}

func (a *mediaSourceAdmissionQueue) domainEligibleLocked(domain string, background bool) bool {
	owners := a.unclassified
	for path, count := range a.domains {
		if pathWithin(path, domain) || pathWithin(domain, path) {
			owners.active += count.active
			owners.background += count.background
		}
	}
	return owners.active < a.limits.rootOwners && (!background || owners.background < a.limits.rootBackground)
}

// A transaction may use this pure memory-only attempt while holding authority
// row locks. It never enqueues, waits for capacity, or takes a Store/DB/FS lock.
func (a *mediaSourceAdmissionQueue) tryAcquireRoot(ctx context.Context, background bool, root mediaSourceRootKey, domain string) (func(), bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if root.catalog == "" || root.id == "" || domain == "" {
		return nil, false, ErrUnavailable
	}
	waiter := &mediaSourceWaiter{ctx: ctx, background: background, root: root, domain: domain}
	a.mu.Lock()
	a.dispatchLocked()
	if !a.eligibleLocked(waiter) {
		a.mu.Unlock()
		return nil, false, nil
	}
	a.grantLocked(waiter)
	a.mu.Unlock()
	return func() { a.release(waiter) }, true, nil
}

func (a *mediaSourceAdmissionQueue) acquire(ctx context.Context, background bool) (func(), error) {
	waiter, err := a.request(ctx, background, true)
	if err != nil {
		return nil, err
	}
	return a.wait(waiter)
}

func (a *mediaSourceAdmissionQueue) wait(waiter *mediaSourceWaiter) (func(), error) {
	if waiter.ready == nil {
		if err := waiter.ctx.Err(); err != nil {
			a.release(waiter)
			return nil, err
		}
		return func() { a.release(waiter) }, nil
	}
	select {
	case <-waiter.ready:
		if err := waiter.ctx.Err(); err != nil {
			a.release(waiter)
			return nil, err
		}
		return func() { a.release(waiter) }, nil
	case <-waiter.ctx.Done():
		a.release(waiter)
		return nil, waiter.ctx.Err()
	}
}

func (a *mediaSourceAdmissionQueue) removeLocked(index int) {
	if a.waiters[index].cleanup && a.cleanupSpace != nil {
		close(a.cleanupSpace)
		a.cleanupSpace = nil
	}
	copy(a.waiters[index:], a.waiters[index+1:])
	a.waiters[len(a.waiters)-1] = nil
	a.waiters = a.waiters[:len(a.waiters)-1]
	// The removed pointer is cleared before retaining a small empty backing
	// array. Large bursts must not retain an unbounded process-wide allocation.
	if len(a.waiters) == 0 && cap(a.waiters) > mediaSourceQueueRetainedCapacity {
		a.waiters = nil
	}
}

func (a *mediaSourceAdmissionQueue) grantLocked(waiter *mediaSourceWaiter) {
	waiter.granted = true
	a.active++
	if waiter.background {
		a.background++
	}
	if waiter.unclassified {
		a.unclassified.active++
		if waiter.background {
			a.unclassified.background++
		}
	}
	if waiter.root.id != "" {
		if a.roots == nil {
			a.roots = make(map[mediaSourceRootKey]mediaSourceLaneCount)
			a.domains = make(map[string]mediaSourceLaneCount)
		}
		root, domain := a.roots[waiter.root], a.domains[waiter.domain]
		root.active++
		domain.active++
		if waiter.background {
			root.background++
			domain.background++
		}
		a.roots[waiter.root], a.domains[waiter.domain] = root, domain
	}
	if waiter.ready != nil {
		close(waiter.ready)
	}
}

// Pick the oldest currently eligible waiter. An analysis waiter at its class
// limit must not prevent a later foreground waiter from using the fourth slot.
// Once analysis becomes eligible, newer foreground work cannot jump ahead.
func (a *mediaSourceAdmissionQueue) dispatchLocked() {
	a.dispatchClassLocked(true)
	a.dispatchClassLocked(false)
}

func (a *mediaSourceAdmissionQueue) dispatchClassLocked(cleanup bool) {
	for index := 0; index < len(a.waiters); {
		if a.active >= a.limits.owners {
			// No class is eligible while the process budget is full. Canceled
			// callers still remove themselves through their own wait/release.
			return
		}
		waiter := a.waiters[index]
		if waiter.cleanup != cleanup {
			index++
			continue
		}
		if waiter.ctx.Err() != nil {
			a.removeLocked(index)
			waiter.released = true
			continue
		}
		if !a.eligibleLocked(waiter) {
			index++
			continue
		}
		a.removeLocked(index)
		a.grantLocked(waiter)
	}
}

func (a *mediaSourceAdmissionQueue) release(waiter *mediaSourceWaiter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if waiter.released {
		return
	}
	waiter.released = true
	if waiter.granted {
		a.active--
		if waiter.background {
			a.background--
		}
		if waiter.unclassified {
			a.unclassified.active--
			if waiter.background {
				a.unclassified.background--
			}
		}
		if waiter.root.id != "" {
			root, domain := a.roots[waiter.root], a.domains[waiter.domain]
			root.active--
			domain.active--
			if waiter.background {
				root.background--
				domain.background--
			}
			if root.active == 0 {
				delete(a.roots, waiter.root)
			} else {
				a.roots[waiter.root] = root
			}
			if domain.active == 0 {
				delete(a.domains, waiter.domain)
			} else {
				a.domains[waiter.domain] = domain
			}
		}
	} else {
		for index, queued := range a.waiters {
			if queued == waiter {
				a.removeLocked(index)
				break
			}
		}
	}
	a.dispatchLocked()
}

// The legacy fixture adapter owns admitted workers; production routed workers
// also own bounded database preparation and queue cancellation. Registration
// and the close fence share this short lock, preventing Wait from racing Add.
type mediaSourceOwnerRuntime struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	closed       bool
	owners       sync.WaitGroup
	catalogScope string
}

func (s *Store) beginMediaSourceWorker(ctx context.Context, background bool) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if s == nil {
		return nil, nil, ErrUnavailable
	}
	runtime := &s.mediaSourceOwners
	runtime.mu.Lock()
	if runtime.closed || s.closing.Load() {
		runtime.mu.Unlock()
		return nil, nil, ErrUnavailable
	}
	if runtime.ctx == nil {
		runtime.ctx, runtime.cancel = context.WithCancel(context.Background())
	}
	lifetime := runtime.ctx
	runtime.mu.Unlock()
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(lifetime, cancel)
	finishContext := func() { stop(); cancel() }
	release, err := mediaSourceAdmission.acquire(work, background)
	if err != nil {
		finishContext()
		return nil, nil, err
	}
	runtime.mu.Lock()
	if runtime.closed || s.closing.Load() || work.Err() != nil {
		runtime.mu.Unlock()
		release()
		finishContext()
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		return nil, nil, ErrUnavailable
	}
	runtime.owners.Add(1)
	runtime.mu.Unlock()
	var once sync.Once
	return work, func() {
		once.Do(func() {
			finishContext()
			release()
			runtime.owners.Done()
		})
	}, nil
}

func (s *Store) beginCloseMediaSources() {
	runtime := &s.mediaSourceOwners
	runtime.mu.Lock()
	runtime.closed = true
	if runtime.cancel != nil {
		runtime.cancel()
	}
	runtime.mu.Unlock()
}

func (s *Store) runMediaSourceWorker(ctx context.Context, work func(context.Context) (*os.File, MediaFile, error)) (*os.File, MediaFile, error) {
	worker, release, err := s.beginMediaSourceWorker(ctx, false)
	if err != nil {
		return nil, MediaFile{}, err
	}
	return runAdmittedMediaSourceWorker(worker, release, func() (*os.File, MediaFile, error) { return work(worker) })
}

func (s *Store) runAnalysisSourceWorker(ctx context.Context, work func(context.Context) (*os.File, MediaFile, error)) (*os.File, MediaFile, error) {
	if err := analysisContext(ctx); err != nil {
		return nil, MediaFile{}, err
	}
	ctx = media.WithBackgroundProcess(ctx)
	worker, release, err := s.beginMediaSourceWorker(ctx, true)
	if err != nil {
		return nil, MediaFile{}, err
	}
	return runAdmittedAnalysisSourceWorker(worker, release, func() (*os.File, MediaFile, error) { return work(worker) })
}
