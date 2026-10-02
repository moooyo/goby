package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/moooyo/goby/internal/commanddomain"
)

var (
	ErrProcessRetirementUnknown = errors.New("media process retirement could not be proved")
	errBackgroundProcessInput   = errors.New("media process requires a context, fresh command and retirement callback")
	processRetirementUnknown    atomic.Uint64
)

// ProcessCapacitySnapshot contains process-wide aggregate counts without
// command paths, source identities, credentials or other sensitive labels.
type ProcessCapacitySnapshot struct {
	Active            int
	Background        int
	Queued            int
	RetirementUnknown uint64
}

func GetProcessCapacityStats() ProcessCapacitySnapshot {
	return processCapacityStatsFor(mediaProcessAdmission, &processRetirementUnknown)
}

func processCapacityStatsFor(governor *mediaProcessGovernor, unknown *atomic.Uint64) ProcessCapacitySnapshot {
	governor.mu.Lock()
	defer governor.mu.Unlock()
	return ProcessCapacitySnapshot{
		Active: governor.active, Background: governor.background,
		Queued: len(governor.waiters), RetirementUnknown: unknown.Load(),
	}
}

// RunBackgroundProcess forces background classification for one fresh command.
// It shares the strict retirement contract of RunProcessWithRetirement.
func RunBackgroundProcess(ctx context.Context, command *exec.Cmd, retireProcessGroup func() error) error {
	return RunProcessWithRetirement(WithBackgroundProcess(ctx), command, retireProcessGroup)
}

// RunProcessWithRetirement owns admission, Start and Wait for one fresh command.
// Classification comes from the trusted caller's background context marker;
// ordinary playback proof uses foreground capacity. Request metadata must never
// choose or remove the marker. Background scans and maintenance retain it.
// The caller supplies its existing process-group Cancel and a strict retirement
// callback that pins the exited leader until descendant signals are complete.
// The callback runs once, before Wait reaps the leader and joins exec's copiers.
// It must not reap the leader itself, and must release its locks on every exit.
// If Cancel exits abnormally, the runner kills its still-owned direct child;
// that fallback does not prove cleanup of the child's descendants.
// The command must use this context's lifetime and must not have been started.
// Neither the callback nor command setup may call another admitted media runner.
// Queue waiting consumes the same deadline and does not own an active permit.
//
// A failed or abnormally exiting retirement or cancellation callback retains its
// process charge, including panic(nil) and runtime.Goexit. Callback result
// publication belongs to the runner rather than the callback's return path.
// Each affected command increments RetirementUnknown once. Reaping permits no
// later numeric-PID retry. Only a process restart reconstructs this budget;
// a restart is not proof
// that unknown operating-system descendants were cleaned up. The caller must
// report failed evidence rather than claim complete closure or successful seal.
func RunProcessWithRetirement(ctx context.Context, command *exec.Cmd, retireProcessGroup func() error) error {
	return runProcessWithRetirement(ctx, command, retireProcessGroup, mediaProcessAdmission, &processRetirementUnknown)
}

func runBackgroundProcess(ctx context.Context, command *exec.Cmd, retireProcessGroup func() error, governor *mediaProcessGovernor, unknown *atomic.Uint64) error {
	return runProcessWithRetirement(WithBackgroundProcess(ctx), command, retireProcessGroup, governor, unknown)
}

func runProcessWithRetirement(ctx context.Context, command *exec.Cmd, retireProcessGroup func() error, governor *mediaProcessGovernor, unknown *atomic.Uint64) error {
	if ctx == nil || command == nil || command.Process != nil || retireProcessGroup == nil {
		return errBackgroundProcessInput
	}
	if scope, required := commanddomain.CommandScopeFromContext(ctx); required {
		// The immutable template has no native PID. Its legacy callback must
		// never signal or reap a process owned only by the concrete native leaf.
		process, startErr := startNativeMediaProcessWithCounter(ctx, command, governor, scope, unknown)
		if process == nil {
			return startErr
		}
		return errors.Join(startErr, process.Wait())
	}
	release, err := governor.acquire(ctx)
	if err != nil {
		return err
	}
	owned := false
	defer func() {
		if !owned {
			release()
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	// exec's context watcher can call Cancel after Process.Wait has reaped the
	// leader but before Wait joins that watcher. Fence both explicit cleanup
	// and watcher cancellation before permitting Wait, including callback panic.
	var signalMu sync.Mutex
	signalsRetired := false
	var retirementUnproven atomic.Bool
	var faultOnce sync.Once
	markUnknown := func() {
		faultOnce.Do(func() {
			governor.mu.Lock()
			retirementUnproven.Store(true)
			unknown.Add(1)
			governor.mu.Unlock()
		})
	}
	originalCancel := command.Cancel
	cancelOwned := func() error {
		signalMu.Lock()
		defer signalMu.Unlock()
		if signalsRetired {
			return os.ErrProcessDone
		}
		if originalCancel != nil {
			err := runBackgroundRetirement(originalCancel)
			if errors.Is(err, ErrProcessRetirementUnknown) {
				markUnknown()
				// A callback can exit before delivering any cancellation signal.
				// This direct child remains owned and unreaped behind signalMu;
				// stop it without retrying an unproved numeric process group.
				err = errors.Join(err, command.Process.Kill())
			}
			return err
		}
		return command.Process.Kill()
	}
	if originalCancel != nil {
		command.Cancel = cancelOwned
	}
	done := make(chan error, 1)
	process := &mediaProcess{command: command, retired: done, release: release, retainForCleanup: true}
	if err := process.attachProbeRetirement(ctx); err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		process.probeChild.notStarted()
		return err
	}
	process.probeChild.started()
	owned = true
	var captureErr error
	if conventionalRetirementSupported() {
		captureErr = process.ensureConventionalOwner()
	}
	joined := false
	defer func() {
		if !joined {
			_ = process.Close()
		}
	}()
	go func() {
		var retireErr error
		completed := false
		defer func() {
			_ = recover()
			if !completed {
				markUnknown()
				retireErr = errors.Join(ErrProcessRetirementUnknown, retireErr, runBackgroundRetirement(cancelOwned))
			}
			// The owner always publishes its result, including an abnormal
			// callback exit. Close the signal fence before Wait can reap.
			signalMu.Lock()
			signalsRetired = true
			if retirementUnproven.Load() {
				retireErr = errors.Join(ErrProcessRetirementUnknown, retireErr)
			}
			signalMu.Unlock()
			done <- retireErr
		}()
		retireErr = runBackgroundRetirement(retireProcessGroup)
		if retireErr != nil {
			// Record the fault before a possibly blocking join. This charge is
			// retained even when Wait later reports a successful leader exit.
			markUnknown()
			retireErr = errors.Join(retireErr, runBackgroundRetirement(cancelOwned))
		}
		completed = true
	}()
	err = process.Wait()
	if process.retireErr != nil || captureErr != nil || errors.Is(err, ErrProcessRetirementUnknown) {
		// Outer callback/domain failure keeps its unknown charge, but must still
		// independently fence the exact original group and join exec/copiers.
		// Never treat either semantic callback return or leader exit as that join.
		process.observeProbeRetirement(false, true)
		cleanupErr := process.Close()
		joined = process.detachBackgroundKernelOwnerAfterJoin()
		return errors.Join(ErrProcessRetirementUnknown, captureErr, err, cleanupErr)
	}
	joined = true
	process.completeCleanup()
	return err
}

func runBackgroundRetirement(callback func() error) error {
	done := make(chan error, 1)
	go func() {
		var err error
		completed := false
		defer func() {
			_ = recover()
			if !completed {
				// Panic details may contain source text or filesystem paths.
				// A completion flag also covers panic(nil) and Goexit.
				err = ErrProcessRetirementUnknown
			}
			done <- err
		}()
		err = callback()
		completed = true
	}()
	return <-done
}
