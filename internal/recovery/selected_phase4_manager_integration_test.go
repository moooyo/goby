//go:build linux

package recovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/lifecycle"
)

func TestRecoverySelectedPhase4RejectsHostChangesDuringApplyAndDrain(t *testing.T) {
	for _, duringDrain := range []bool{false, true} {
		name := "concurrent_apply"
		if duringDrain {
			name = "after_apply_before_drain"
		}
		t.Run(name, func(t *testing.T) {
			defer releaseRecoveryEngineTestMemory()
			f := newManagerIntegrationFixture(t)
			ctx := f.seed.ctx
			if err := f.runtime.BindDatabase(ctx, f.seed.configuration, f.seed.source, f.lease); err != nil {
				t.Fatal("bind the source host ownership")
			}
			plan := createTransitionPlan(t, f)
			op, err := f.manager.operationCopy(plan.Id)
			if err != nil || !op.TargetHost.valid(false) || op.TargetHost.Revision != "9007199254740993" {
				t.Fatal("the ready plan omitted its exact trusted host capture")
			}
			// Persistence is checked through the real private journal decoder. Public
			// restore input never supplies this source-bound target ownership proof.
			persisted, _, err := readControl(ctx, f.runtime)
			if err != nil {
				t.Fatal("read the captured host choices from the private journal")
			}
			found := false
			for _, stored := range persisted.Operations {
				if stored.ID == op.ID {
					found = stored.TargetHost.valid(false) && stored.TargetHost.Revision == op.TargetHost.Revision
				}
			}
			if !found {
				t.Fatal("host capture did not survive strict private journal decoding")
			}
			targetBefore := recoveryEngineJSONState(t, ctx, f.seed.target, `SELECT to_jsonb(s)::text FROM managed_settings s WHERE id=1`)
			request := ApplyRequest{Revision: plan.Revision, GenerationRevision: "0"}
			if duringDrain {
				if _, err := f.manager.Apply(ctx, f.seed.actor, plan.Id, request); err != nil {
					t.Fatalf("authorize the initially matching host capture: %v", err)
				}
				if _, err := f.seed.source.Exec(ctx, `UPDATE managed_settings SET revision=revision+1,runtime_overrides=jsonb_set(runtime_overrides,'{Threads}','7') WHERE id=1`); err != nil {
					t.Fatal("commit a host edit while ingress drains")
				}
				if _, err := f.manager.PrepareSwitch(ctx, plan.Id); !errors.Is(err, ErrConflict) {
					t.Fatalf("drained activation adopted a stale host capture: %v", err)
				}
			} else {
				writer, err := f.seed.source.Begin(ctx)
				if err != nil {
					t.Fatal("begin the concurrent host writer")
				}
				defer rollbackRestore(writer)
				var writerPID int
				if err := writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
					t.Fatal("identify the owned blocking writer")
				}
				if _, err := writer.Exec(ctx, `UPDATE managed_settings SET revision=revision+1,runtime_overrides=jsonb_set(runtime_overrides,'{Threads}','7') WHERE id=1`); err != nil {
					t.Fatal("stage the concurrent host edit")
				}
				outcome := make(chan error, 1)
				applyContext, cancelApply := context.WithCancel(ctx)
				done := make(chan struct{})
				defer func() {
					rollbackRestore(writer)
					cancelApply()
					select {
					case <-done:
					case <-time.After(10 * time.Second):
						t.Error("the owned apply waiter did not close after cancellation")
					}
				}()
				go func() {
					defer close(done)
					_, err := f.manager.Apply(applyContext, f.seed.actor, plan.Id, request)
					outcome <- err
				}()
				waitPhase4HostWaiter(t, ctx, f, writerPID, outcome)
				if err := writer.Commit(ctx); err != nil {
					t.Fatal("commit the writer after observing apply's row-lock wait")
				}
				select {
				case err := <-outcome:
					if !errors.Is(err, ErrConflict) {
						t.Fatalf("apply authorized a pre-wait host capture: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("the apply waiter did not join")
				}
				current, err := f.manager.operationCopy(plan.Id)
				if err != nil || current.ApplyAuthorized {
					t.Fatal("stale host settings gained durable apply authority")
				}
			}
			state, err := f.runtime.lifecycle.Current()
			if err != nil || state.DatabaseSlot != lifecycle.DatabasePrimary || state.Revision != 0 {
				t.Fatal("a stale host capture published a new active generation")
			}
			if got := recoveryEngineJSONState(t, ctx, f.seed.target, `SELECT to_jsonb(s)::text FROM managed_settings s WHERE id=1`); got != targetBefore {
				t.Fatal("stale-capture rejection rewrote the staged target")
			}
			var threads, revision int64
			if err := f.seed.source.QueryRow(ctx, `SELECT (runtime_overrides->>'Threads')::bigint,revision FROM managed_settings WHERE id=1`).Scan(&threads, &revision); err != nil || threads != 7 || revision != 9007199254740994 {
				t.Fatal("recovery discarded the newer target-host edit")
			}
		})
	}
}

func waitPhase4HostWaiter(t *testing.T, ctx context.Context, f *managerIntegrationFixture, writerPID int, outcome <-chan error) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := f.seed.source.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock')`, writerPID).Scan(&waiting); err != nil {
			t.Fatal("observe the owned apply lock wait")
		}
		if waiting {
			return
		}
		select {
		case err := <-outcome:
			t.Fatalf("apply completed before taking the settings row lock: %v", err)
		case <-deadline.C:
			t.Fatal("apply did not wait on the concurrent target settings writer")
		case <-ctx.Done():
			t.Fatal("host-lock observation was cancelled")
		case <-ticker.C:
		}
	}
}
