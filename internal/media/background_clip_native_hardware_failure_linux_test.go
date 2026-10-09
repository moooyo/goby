//go:build linux

package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/commanddomain"
)

func TestBackgroundClipNativeHardwareFailureReturnsPrelaunchOwnership(t *testing.T) {
	for _, mode := range []string{"error", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			input, err := os.CreateTemp(t.TempDir(), "unstarted-source-")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			cohort := &probeRetirementCohort{source: input, pending: make(map[*probeRetirementChild]struct{}), drained: make(chan struct{})}
			ctx, cancel := context.WithTimeout(WithBackgroundProcess(context.Background()), 5*time.Second)
			defer cancel()
			ctx = context.WithValue(ctx, probeRetirementContextKey{}, cohort)
			rejected := errors.New("hardware admission did not complete")
			var checks atomic.Int32
			ctx = backgroundClipHardwareContext(ctx, true, func(context.Context) error {
				checks.Add(1)
				switch mode {
				case "panic":
					panic(rejected)
				case "goexit":
					runtime.Goexit()
				}
				return rejected
			})
			command := exec.CommandContext(ctx, "/must-never-spawn")
			command.ExtraFiles = []*os.File{input}
			governor := newMediaProcessAdmission(1, 1, 4)
			var unknown atomic.Uint64
			type outcome struct {
				process    *mediaProcess
				err        error
				panicValue any
				returned   bool
			}
			done := make(chan outcome, 1)
			go func() {
				var result outcome
				defer func() {
					result.panicValue = recover()
					done <- result
				}()
				result.process, result.err = startNativeMediaProcessWithCounter(ctx, command, governor, &commanddomain.CommandScope{}, &unknown)
				result.returned = true
			}()
			var result outcome
			select {
			case result = <-done:
			case <-ctx.Done():
				t.Fatal("interrupted hardware admission did not finish its prelaunch cleanup")
			}
			if checks.Load() != 1 || command.Process != nil || result.process != nil || unknown.Load() != 0 {
				t.Fatal("a prelaunch callback created or quarantined a native process")
			}
			switch mode {
			case "error":
				if !result.returned || !errors.Is(result.err, rejected) || result.panicValue != nil {
					t.Fatalf("ordinary hardware rejection changed control flow: %+v", result)
				}
			case "panic":
				if result.returned || result.panicValue != rejected {
					t.Fatalf("hardware panic was swallowed or replaced: %+v", result)
				}
			case "goexit":
				if result.returned || result.panicValue != nil {
					t.Fatalf("hardware Goexit was converted into a return or panic: %+v", result)
				}
			}
			governor.mu.Lock()
			active, background, queued := governor.active, governor.background, len(governor.waiters)
			governor.mu.Unlock()
			cohort.mu.Lock()
			pending, claims, cohortUnknown := len(cohort.pending), cohort.claims, cohort.unknown
			cohort.mu.Unlock()
			if active != 0 || background != 0 || queued != 0 || pending != 0 || claims != 1 || cohortUnknown {
				t.Fatalf("unstarted admission retained ownership: active=%d background=%d queued=%d pending=%d claims=%d unknown=%t",
					active, background, queued, pending, claims, cohortUnknown)
			}
			// The same capacity must be reusable after every control-flow outcome.
			release := mediaProcessAdmissionTestAcquire(t, governor, ctx)
			release()
		})
	}
}
