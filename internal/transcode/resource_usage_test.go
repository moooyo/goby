package transcode

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func resourceUsageTestScope() Scope {
	return Scope{UserID: "viewer", AuthSessionID: "auth", DeviceID: "device", PlaySessionID: "play", ItemID: "movie", SourceID: "source"}
}

func resourceUsageTestManager(t *testing.T, capacity int) *Manager {
	t.Helper()
	pool, err := newCompletionTicketPool(capacity, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &Manager{options: Options{MaxRetainedJobs: capacity}, jobs: make(map[string]*managedJob), completions: pool}
}

func resourceUsageTestTicket(t *testing.T, pool *completionTicketPool) *completionTicket {
	t.Helper()
	ticket, err := pool.reserve()
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestResourceUsageExactScopeAndNoMutation(t *testing.T) {
	scope := resourceUsageTestScope()
	m := resourceUsageTestManager(t, 4)
	lastAccess := time.Date(2026, time.October, 2, 4, 0, 0, 0, time.UTC)
	j := &managedJob{record: Record{ID: "job", Spec: Spec{Scope: scope}, State: "running", OutputBytes: 123, LastAccessAt: lastAccess},
		durable: true, running: true, readers: 2, accountingPending: true, completion: resourceUsageTestTicket(t, m.completions)}
	m.jobs[j.record.ID] = j
	// A closing or failed manager still owns these records. Observing them does
	// not bypass any admission or lookup fence and does not expose those fences.
	m.closing, m.cacheFailed, m.readers, m.bytes = true, true, 2, 123
	beforeRecord, beforePool := j.record, m.completions.snapshot()
	want := ResourceUsage{RetainedJobs: 1, UnfinishedJobs: 1, ExecutionReservations: 1, ReaderPins: 2,
		KnownChargedOutputFloorBytes: 123, AccountingPendingJobs: 1, CompletionReservedJobs: 1}
	for attempt := 0; attempt < 3; attempt++ {
		got, err := m.ResourceUsage(context.Background(), scope)
		if err != nil || got != want {
			t.Fatalf("exact scope observation = %+v, %v; want %+v", got, err, want)
		}
	}
	if j.record != beforeRecord || j.readers != 2 || !j.running || !j.accountingPending || m.readers != 2 || m.bytes != 123 ||
		m.completions.snapshot() != beforePool || !m.closing || !m.cacheFailed {
		t.Fatal("observation mutated ownership, accounting, idle access or manager fences")
	}
	foreign := []Scope{}
	for _, change := range []func(*Scope){
		func(s *Scope) { s.UserID = "other-viewer" },
		func(s *Scope) { s.AuthSessionID = "other-auth" },
		func(s *Scope) { s.DeviceID = "other-device" },
		func(s *Scope) { s.DeviceID = "" },
		func(s *Scope) { s.PlaySessionID = "other-play" },
		func(s *Scope) { s.ItemID = "other-movie" },
		func(s *Scope) { s.SourceID = "other-source" },
		func(s *Scope) { s.ApplicationKey, s.ApplicationClientID, s.UserID = true, "application", "" },
	} {
		other := scope
		change(&other)
		foreign = append(foreign, other)
	}
	for _, other := range foreign {
		got, err := m.ResourceUsage(context.Background(), other)
		if err != nil || got != (ResourceUsage{}) {
			t.Fatalf("foreign scope leaked ownership: %+v, %v for %+v", got, err, other)
		}
	}
	appScope := scope
	appScope.ApplicationKey, appScope.ApplicationClientID, appScope.UserID = true, "application", ""
	m.jobs["application-job"] = &managedJob{record: Record{Spec: Spec{Scope: appScope}, OutputBytes: 19}, finished: true, durable: true}
	got, err := m.ResourceUsage(context.Background(), appScope)
	if err != nil || got != (ResourceUsage{RetainedJobs: 1, KnownChargedOutputFloorBytes: 19, UnticketedJobs: 1}) {
		t.Fatalf("application scope observation = %+v, %v", got, err)
	}
	appScope.ApplicationClientID = "other-application"
	if got, err = m.ResourceUsage(context.Background(), appScope); err != nil || got != (ResourceUsage{}) {
		t.Fatalf("foreign application client leaked ownership: %+v, %v", got, err)
	}
}

func TestResourceUsageRetainedAccountingAndCompletionPhases(t *testing.T) {
	scope := resourceUsageTestScope()
	m := resourceUsageTestManager(t, 8)
	reserved := resourceUsageTestTicket(t, m.completions)
	queued := resourceUsageTestTicket(t, m.completions)
	working := resourceUsageTestTicket(t, m.completions)
	released := resourceUsageTestTicket(t, m.completions)
	if !queued.queue() || !working.queue() || !working.work() || !released.release() {
		t.Fatal("could not establish owned completion phases")
	}
	jobs := []*managedJob{
		{record: Record{Spec: Spec{Scope: scope}, State: "queued", OutputBytes: 10}, completion: reserved},
		{record: Record{Spec: Spec{Scope: scope}, State: "running", OutputBytes: 20}, durable: true, running: true, finalizationQueued: true,
			accountingPending: true, readers: 1, completion: queued},
		// Inspection has returned its execution slot while terminal handling is
		// still active. Its remembered floor and original ticket remain owned.
		{record: Record{Spec: Spec{Scope: scope}, State: "completed", OutputBytes: 30}, durable: true, finalizationQueued: true,
			accountingUnknown: true, readers: 2, completion: working},
		{record: Record{Spec: Spec{Scope: scope}, State: "cancelled", OutputBytes: 40}, durable: true, finished: true, reclaiming: true, completion: released},
		{record: Record{Spec: Spec{Scope: scope}, State: "completed", OutputBytes: 50}, durable: true, finished: true},
	}
	for index, j := range jobs {
		m.jobs[string(rune('a'+index))] = j
	}
	foreignScope := scope
	foreignScope.PlaySessionID = "other-play"
	m.jobs["foreign"] = &managedJob{record: Record{Spec: Spec{Scope: foreignScope}, OutputBytes: math.MaxInt64}, running: true, readers: 20, accountingUnknown: true}
	want := ResourceUsage{RetainedJobs: 5, UnfinishedJobs: 3, CreatingJobs: 1, QueuedJobs: 1, ExecutionReservations: 1,
		ReaderPins: 3, ReclaimingJobs: 1, KnownChargedOutputFloorBytes: 150, AccountingPendingJobs: 1, AccountingUnknownJobs: 1,
		CompletionReservedJobs: 1, CompletionQueuedJobs: 1, CompletionWorkingJobs: 1, CompletionReleasedJobs: 1, UnticketedJobs: 1}
	got, err := m.ResourceUsage(context.Background(), scope)
	if err != nil || got != want {
		t.Fatalf("retained observation = %+v, %v; want %+v", got, err, want)
	}
	if !reserved.queue() || !queued.work() || !working.release() {
		t.Fatal("could not advance owned completion phases")
	}
	want.CompletionReservedJobs, want.CompletionQueuedJobs, want.CompletionWorkingJobs, want.CompletionReleasedJobs = 0, 1, 1, 2
	if got, err = m.ResourceUsage(context.Background(), scope); err != nil || got != want {
		t.Fatalf("advanced observation = %+v, %v; want %+v", got, err, want)
	}
	foreignPool, err := newCompletionTicketPool(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.jobs["foreign-pool"] = &managedJob{record: Record{Spec: Spec{Scope: scope}}, durable: true, finished: true,
		completion: resourceUsageTestTicket(t, foreignPool)}
	want.RetainedJobs, want.CompletionUnknownJobs = 6, 1
	if got, err = m.ResourceUsage(context.Background(), scope); err != nil || got != want {
		t.Fatalf("foreign pool observation = %+v, %v; want %+v", got, err, want)
	}
}

type resourceUsageObservedContext struct {
	context.Context
	check    int32
	want     int32
	observed chan struct{}
	once     sync.Once
}

func (c *resourceUsageObservedContext) Err() error {
	err := c.Context.Err()
	if atomic.AddInt32(&c.check, 1) == c.want {
		c.once.Do(func() { close(c.observed) })
	}
	return err
}

func TestResourceUsageCancellationAfterLockWait(t *testing.T) {
	for _, heldLock := range []string{"manager", "completion"} {
		t.Run(heldLock, func(t *testing.T) {
			m := resourceUsageTestManager(t, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed := &resourceUsageObservedContext{Context: ctx, want: 1, observed: make(chan struct{})}
			var unlock func()
			if heldLock == "manager" {
				m.mu.Lock()
				unlock = m.mu.Unlock
			} else {
				m.completions.mu.Lock()
				unlock = m.completions.mu.Unlock
				observed.want = 2
			}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(unlock) }
			defer release()
			done := make(chan error, 1)
			joined := false
			go func() {
				usage, err := m.ResourceUsage(observed, resourceUsageTestScope())
				if usage != (ResourceUsage{}) {
					err = errors.New("cancelled observation returned partial ownership")
				}
				done <- err
			}()
			defer func() {
				cancel()
				release()
				if !joined {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("observation goroutine did not join during fixture cleanup")
					}
				}
			}()
			select {
			case <-observed.observed:
			case <-time.After(time.Second):
				t.Fatal("observation did not reach its pre-lock context check")
			}
			cancel()
			release()
			select {
			case err := <-done:
				joined = true
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lock-wait cancellation = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("observation did not actually return after the lock was released")
			}
		})
	}
}

func TestResourceUsageRejectsInvalidOrUnrepresentableObservation(t *testing.T) {
	scope := resourceUsageTestScope()
	m := resourceUsageTestManager(t, 2)
	invalid := scope
	invalid.AuthSessionID = ""
	if _, err := m.ResourceUsage(context.Background(), invalid); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("invalid scope = %v", err)
	}
	var absent *Manager
	if _, err := absent.ResourceUsage(context.Background(), scope); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("absent manager = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.ResourceUsage(ctx, scope); !errors.Is(err, context.Canceled) {
		t.Fatalf("already cancelled observation = %v", err)
	}
	for _, test := range []struct {
		name     string
		capacity int
		output   []int64
	}{
		{name: "negative floor", capacity: 2, output: []int64{-1}},
		{name: "overflow floor", capacity: 2, output: []int64{math.MaxInt64, 1}},
		{name: "registry exceeds bound", capacity: 1, output: []int64{1, 2}},
		{name: "invalid retained bound", capacity: maxCompletionTickets + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := resourceUsageTestManager(t, 2)
			m.options.MaxRetainedJobs = test.capacity
			for index, bytes := range test.output {
				m.jobs[string(rune('a'+index))] = &managedJob{record: Record{Spec: Spec{Scope: scope}, OutputBytes: bytes}}
			}
			got, err := m.ResourceUsage(context.Background(), scope)
			if !errors.Is(err, ErrOutputUnavailable) || !reflect.DeepEqual(got, ResourceUsage{}) {
				t.Fatalf("invalid observation = %+v, %v", got, err)
			}
		})
	}
}
