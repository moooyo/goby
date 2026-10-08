//go:build linux

package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/lifecycle"
)

type healthObservationContext struct {
	context.Context
	observe func()
}

func (ctx healthObservationContext) Err() error {
	ctx.observe()
	return ctx.Context.Err()
}

func TestManagerHealthCancellationDuringLifecycleRead(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "lifecycle")
	store, err := lifecycle.Open(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	id := "11111111111111111111111111111111"
	generation, err := store.StageGeneration(context.Background(), id, []byte("{}"), nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.CurrentContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := &Manager{runtime: &Runtime{lifecycle: store}, current: state, ctx: context.Background(), operator: true}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(directory, "generation-"+id, generation.Config.Name)
	observed := false
	readContext := healthObservationContext{Context: ctx, observe: func() {
		if observed {
			return
		}
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
			if err == nil && target == path {
				observed = true
				cancel()
				return
			}
		}
	}}
	if _, _, err := m.PendingSwitch(readContext); !errors.Is(err, context.Canceled) || !observed || m.fault {
		t.Fatalf("cancelled health read lost its error or faulted manager: %v, observed=%v fault=%v", err, observed, m.fault)
	}
	finished := make(chan error, 1)
	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		finished <- m.healthyLocked(context.Background())
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("cancelled health read blocked later management: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled health read retained manager ownership")
	}
	m.current.Digest = "changed"
	m.mu.Lock()
	err = m.healthyLocked(context.Background())
	m.mu.Unlock()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("state mismatch lost its conflict classification: %v", err)
	}
}

func TestManagerHealthCallersPreserveContextErrors(t *testing.T) {
	m := &Manager{ctx: context.Background(), operator: true}
	operations := map[string]func(context.Context) error{
		"pending":         func(ctx context.Context) error { _, _, err := m.PendingSwitch(ctx); return err },
		"prepare":         func(ctx context.Context) error { _, err := m.PrepareSwitch(ctx, "unused"); return err },
		"validate":        m.ValidateSwitch,
		"accept":          m.AcceptSwitch,
		"operator status": func(ctx context.Context) error { _, err := m.OperatorStatus(ctx); return err },
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if err := operation(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatalf("caller hid cancellation: %v", err)
			}
			expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
			if err := operation(expired); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("caller hid deadline: %v", err)
			}
			if m.fault {
				t.Fatal("request cancellation faulted manager")
			}
		})
	}
}

func TestManagerControlCancellationKeepsRecoveryBarrier(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancelled"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			m := budgetManager(t, budgetFixture(t, 0))
			before := m.control.Digest
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			want := context.Canceled
			if deadline {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				defer cancel()
				want = context.DeadlineExceeded
			}
			if err := m.persistLocked(ctx); !errors.Is(err, want) || !m.fault {
				t.Fatalf("control cancellation hid its error or bypassed the recovery barrier: %v", err)
			}
			journal := &switchJournal{runtime: m.runtime, data: m.data, snapshot: m.control}
			if err := journal.save(ctx); !errors.Is(err, want) || !journal.fault {
				t.Fatalf("switch cancellation hid its error or bypassed the recovery barrier: %v", err)
			}
			if snapshot, err := m.runtime.control.Read(context.Background()); err != nil || snapshot.Digest != before {
				t.Fatalf("cancelled control writes changed durable authority: %v", err)
			}
		})
	}
}
