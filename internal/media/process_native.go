package media

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/moooyo/goby/internal/commanddomain"
)

// The existing governor remains the actual media4/background2 budget. This
// bounded native registry keeps exact failed owners reachable after their
// caller returns; it does not add native concurrency or storage authority.
var nativeMediaOwners struct {
	mu      sync.Mutex
	entries [mediaProcessOwnerLimit]*nativeMediaProcess
}

func init() {
	// This marks only static cohort-hook installation. Actual retirement still
	// comes from the owned native handles; it is not backend readiness.
	nativeProbeRetirementHooksInstalled = true
}

type nativeMediaProcess struct {
	mu         sync.Mutex
	waitOnce   sync.Once
	startOnce  sync.Once
	startDone  chan struct{}
	probeChild *probeRetirementChild
	scope      *commanddomain.CommandScope
	process    *commanddomain.ScopedProcess
	pipes      *commanddomain.ScopedTemplatePipes
	release    func()
	governor   *mediaProcessGovernor
	counter    *atomic.Uint64
	slot       int
	unknown    bool
	released   bool
	err        error
}

func newNativeMediaOwner(scope *commanddomain.CommandScope, governor *mediaProcessGovernor, release func(), counter *atomic.Uint64) (*nativeMediaProcess, error) {
	nativeMediaOwners.mu.Lock()
	defer nativeMediaOwners.mu.Unlock()
	for index, current := range nativeMediaOwners.entries {
		if current == nil {
			owner := &nativeMediaProcess{scope: scope, governor: governor, release: release, counter: counter, slot: index, startDone: make(chan struct{})}
			nativeMediaOwners.entries[index] = owner
			return owner, nil
		}
	}
	return nil, ErrProcessCapacity
}

func (owner *nativeMediaProcess) markUnknown() {
	owner.mu.Lock()
	first := !owner.unknown
	owner.unknown = true
	owner.mu.Unlock()
	if first {
		owner.governor.mu.Lock()
		owner.counter.Add(1)
		owner.governor.mu.Unlock()
	}
	// Publish only after native owner/governor locks are released. Receipt
	// cleanup invokes exact owner callbacks without holding the cohort lock.
	owner.probeChild.unknown()
	owner.scope.CloseGate()
}

func (owner *nativeMediaProcess) finishStart() {
	owner.startOnce.Do(func() { close(owner.startDone) })
}

func (owner *nativeMediaProcess) returnKnownCapacity() {
	nativeMediaOwners.mu.Lock()
	owner.mu.Lock()
	if owner.unknown || owner.released {
		owner.mu.Unlock()
		nativeMediaOwners.mu.Unlock()
		return
	}
	if owner.slot < 0 || owner.slot >= len(nativeMediaOwners.entries) || nativeMediaOwners.entries[owner.slot] != owner {
		owner.mu.Unlock()
		nativeMediaOwners.mu.Unlock()
		owner.markUnknown()
		return
	}
	returned := false
	defer func() {
		owner.mu.Unlock()
		nativeMediaOwners.mu.Unlock()
		if !returned {
			owner.markUnknown()
		}
	}()
	// The private governor release may wake a successor immediately. Keep the
	// exact owner registered and serialize its registry lookup until the same
	// handoff has returned the permit and made the original slot reusable.
	// An abnormal release retains the original slot; locks are dropped before
	// recording its unknown state. No governor path holds its lock while taking
	// this registry or an owner lock.
	owner.release()
	owner.released = true
	nativeMediaOwners.entries[owner.slot] = nil
	returned = true
}

func nativeRawPipeTemplate(command *exec.Cmd) bool {
	for _, stream := range []io.Writer{command.Stdout, command.Stderr} {
		if file, ok := stream.(*os.File); ok && file != nil {
			info, err := file.Stat()
			if err != nil || info.Mode()&os.ModeNamedPipe != 0 {
				return true
			}
		}
	}
	return false
}

func startNativeMediaProcess(ctx context.Context, command *exec.Cmd, governor *mediaProcessGovernor, scope *commanddomain.CommandScope) (*mediaProcess, error) {
	return startNativeMediaProcessWithCounter(ctx, command, governor, scope, &processRetirementUnknown)
}

func startNativeMediaProcessWithCounter(ctx context.Context, command *exec.Cmd, governor *mediaProcessGovernor, scope *commanddomain.CommandScope, counter *atomic.Uint64) (result *mediaProcess, resultErr error) {
	if ctx == nil || command == nil || command.Process != nil || command.ProcessState != nil || governor == nil || counter == nil {
		return nil, errBackgroundProcessInput
	}
	child, err := beginProbeRetirementChild(ctx, command)
	if err != nil {
		return nil, err
	}
	observed := false
	defer func() {
		if !observed {
			child.unknown()
		}
	}()
	if scope == nil {
		child.notStarted()
		observed = true
		return nil, commanddomain.ErrUnavailable
	}
	if nativeRawPipeTemplate(command) {
		child.unknown()
		observed = true
		return nil, commanddomain.ErrWaitOwnership
	}
	release, err := governor.acquire(ctx)
	if err != nil {
		child.notStarted()
		observed = true
		return nil, err
	}
	owner, err := newNativeMediaOwner(scope, governor, release, counter)
	if err != nil {
		release()
		child.notStarted()
		observed = true
		return nil, err
	}
	returned := false
	defer owner.finishStart()
	defer func() {
		if !returned {
			owner.markUnknown()
		}
	}()
	owner.probeChild = child
	child.bindClose(owner.close)
	result = &mediaProcess{native: owner}
	owner.process, resultErr = scope.Start(ctx, command)
	if owner.process == nil {
		if errors.Is(resultErr, commanddomain.ErrRetained) || errors.Is(resultErr, commanddomain.ErrWaitOwnership) || resultErr == nil {
			owner.markUnknown()
			resultErr = errors.Join(resultErr, ErrProcessRetirementUnknown)
		} else {
			owner.returnKnownCapacity()
			child.notStarted()
			result = nil
		}
	} else {
		if resultErr == nil {
			child.started()
		}
		if errors.Is(resultErr, commanddomain.ErrRetained) || errors.Is(resultErr, commanddomain.ErrWaitOwnership) {
			child.unknown()
		}
		// Publish the actual returned handle before receipt cleanup may cancel
		// or join it. An opaque error handle alone is not evidence of a child.
		owner.finishStart()
		if resultErr != nil {
			// Some existing callers return immediately at Start error. Preserve
			// behavior only after the actual owner performs mandatory Wait.
			resultErr = errors.Join(resultErr, owner.wait())
		}
	}
	returned, observed = true, true
	return result, resultErr
}

func (owner *nativeMediaProcess) wait() (result error) {
	returned := false
	defer func() {
		if !returned {
			owner.markUnknown()
		}
	}()
	owner.waitOnce.Do(func() {
		if owner.process == nil {
			owner.markUnknown()
			owner.err = ErrProcessRetirementUnknown
			return
		}
		owner.err = owner.process.Wait()
		if _, joined := owner.process.ExitCode(); joined {
			owner.probeChild.started()
		}
		if owner.process.RetirementComplete() {
			owner.returnKnownCapacity()
			// The handoff above has dropped registry and owner locks before this
			// independent whole-probe receipt publication.
			owner.probeChild.retired()
		} else {
			owner.markUnknown()
			owner.err = errors.Join(owner.err, ErrProcessRetirementUnknown)
		}
	})
	owner.mu.Lock()
	unknown := owner.unknown
	owner.mu.Unlock()
	result = owner.err
	if unknown {
		result = errors.Join(result, ErrProcessRetirementUnknown)
	}
	returned = true
	return result
}

func (owner *nativeMediaProcess) close() (result error) {
	returned := false
	defer func() {
		if !returned {
			owner.markUnknown()
		}
	}()
	result = owner.closeAfterStart()
	returned = true
	return
}

func (owner *nativeMediaProcess) closeAfterStart() error {
	// Close is bound before Start. Its fixed gate prevents a receipt cleanup
	// from guessing that an in-flight or abnormal opaque Start created no child.
	<-owner.startDone
	if owner.pipes != nil {
		return owner.closePipes()
	}
	owner.mu.Lock()
	complete := owner.released
	owner.mu.Unlock()
	if complete && owner.process == nil {
		return nil
	}
	if !complete && owner.process != nil {
		if err := owner.process.SignalCancel(); err != nil {
			owner.markUnknown()
		}
	}
	return owner.wait()
}

func (owner *nativeMediaProcess) closePipes() error {
	cancelErr := owner.pipes.SignalCancel()
	if cancelErr != nil {
		owner.markUnknown()
	}
	waitErr := owner.pipes.Wait()
	closeErr := owner.pipes.Close()
	if owner.pipes.RetirementComplete() {
		owner.returnKnownCapacity()
		owner.probeChild.retired()
	} else if owner.scope.Snapshot().Quarantined || errors.Is(waitErr, commanddomain.ErrWaitOwnership) {
		owner.markUnknown()
	}
	// A callback that is still active gives normal ErrRetained: keep its child
	// pending. Closing a reader or observing child Wait is not consumer return.
	owner.mu.Lock()
	unknown := owner.unknown
	owner.mu.Unlock()
	if unknown {
		return errors.Join(cancelErr, waitErr, closeErr, ErrProcessRetirementUnknown)
	}
	return errors.Join(cancelErr, waitErr, closeErr)
}

// runMediaStdout owns one parser call without launching a parser goroutine.
// Its native path preserves template-pipe ends, observed EOF, callback return,
// actual child/copier Wait and whole leaf retirement as separate boundaries.
func runMediaStdout(ctx context.Context, command *exec.Cmd, parse func(io.Reader) error) (parseErr, waitErr, startErr error) {
	if ctx == nil || command == nil || parse == nil {
		return nil, nil, errBackgroundProcessInput
	}
	if scope, required := commanddomain.CommandScopeFromContext(ctx); required {
		return runNativeMediaStdout(ctx, command, parse, scope)
	}
	reader, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	process, err := startMediaProcess(ctx, command)
	if err != nil {
		_ = reader.Close()
		return nil, nil, err
	}
	defer process.Close()
	parseErr = parse(reader)
	if parseErr != nil && command.Cancel != nil {
		_ = command.Cancel()
	}
	waitErr = process.Wait()
	return parseErr, waitErr, nil
}

func runNativeMediaStdout(ctx context.Context, command *exec.Cmd, parse func(io.Reader) error, scope *commanddomain.CommandScope) (parseErr, waitErr, startErr error) {
	child, err := beginProbeRetirementChild(ctx, command)
	if err != nil {
		return nil, nil, err
	}
	observed := false
	defer func() {
		if !observed {
			child.unknown()
		}
	}()
	if scope == nil {
		child.notStarted()
		observed = true
		return nil, nil, commanddomain.ErrUnavailable
	}
	release, err := mediaProcessAdmission.acquire(ctx)
	if err != nil {
		child.notStarted()
		observed = true
		return nil, nil, err
	}
	owner, err := newNativeMediaOwner(scope, mediaProcessAdmission, release, &processRetirementUnknown)
	if err != nil {
		release()
		child.notStarted()
		observed = true
		return nil, nil, err
	}
	returned := false
	defer owner.finishStart()
	defer func() {
		if !returned {
			owner.markUnknown()
		}
	}()
	owner.probeChild = child
	child.bindClose(owner.close)
	owner.pipes, startErr = scope.StartTemplatePipes(ctx, command, true, false)
	if owner.pipes == nil {
		if startErr == nil || errors.Is(startErr, commanddomain.ErrRetained) || errors.Is(startErr, commanddomain.ErrWaitOwnership) {
			owner.markUnknown()
			startErr = errors.Join(startErr, ErrProcessRetirementUnknown)
		} else {
			owner.returnKnownCapacity()
			child.notStarted()
		}
		returned, observed = true, true
		return
	}
	if startErr == nil {
		child.started()
	}
	if errors.Is(startErr, commanddomain.ErrRetained) || errors.Is(startErr, commanddomain.ErrWaitOwnership) {
		child.unknown()
	}
	owner.finishStart()
	if startErr != nil {
		_ = owner.pipes.SignalCancel()
		waitErr = owner.pipes.Wait()
	} else {
		parseErr = owner.pipes.ConsumeStdout(parse)
		if parseErr != nil {
			_ = owner.pipes.SignalCancel()
		}
		waitErr = owner.pipes.Wait()
	}
	closeErr := owner.pipes.Close()
	if owner.pipes.RetirementComplete() {
		owner.returnKnownCapacity()
		child.retired()
	} else {
		owner.markUnknown()
	}
	if closeErr != nil {
		waitErr = errors.Join(waitErr, closeErr, ErrProcessRetirementUnknown)
	}
	owner.mu.Lock()
	unknown := owner.unknown
	owner.mu.Unlock()
	if unknown {
		waitErr = errors.Join(waitErr, ErrProcessRetirementUnknown)
	}
	returned, observed = true, true
	return
}
