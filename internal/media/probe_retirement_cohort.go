package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"

	"github.com/moooyo/goby/internal/commanddomain"
)

// ProbeRetirementReceipt is an opaque, live ownership receipt. Semantic probe
// success and callback return cannot manufacture a child retirement receipt.
type ProbeRetirementReceipt struct{ cohort *probeRetirementCohort }

type probeRetirementCohort struct {
	mu       sync.Mutex
	source   *os.File
	pending  map[*probeRetirementChild]struct{}
	returned bool
	unknown  bool
	closing  bool
	claims   int
	cleanup  sync.Once
	drained  chan struct{}
	closeErr error
}

type probeRetirementChild struct {
	cohort     *probeRetirementCohort
	close      func() error
	hasStarted bool
	done       bool
}

type probeRetirementContextKey struct{}

// The native gateway sets this once at package initialization only after both
// its base and parser-pipe routes install actual cohort ownership hooks. This
// feature fence is not a native backend or retirement certificate.
var nativeProbeRetirementHooksInstalled bool

func nativeProbeRetirementCohortReady() bool { return nativeProbeRetirementHooksInstalled }

func beginProbeRetirementChild(ctx context.Context, command *exec.Cmd) (*probeRetirementChild, error) {
	if ctx == nil || command == nil {
		return nil, ErrProcessRetirementUnknown
	}
	cohort, _ := ctx.Value(probeRetirementContextKey{}).(*probeRetirementCohort)
	if cohort == nil {
		return nil, nil
	}
	borrowed := false
	for _, file := range command.ExtraFiles {
		borrowed = borrowed || file == cohort.source
	}
	if !borrowed {
		return nil, nil
	}
	cohort.mu.Lock()
	defer cohort.mu.Unlock()
	if cohort.returned || cohort.closing || len(cohort.pending) >= mediaProcessOwnerLimit || cohort.claims >= 4096 {
		cohort.unknown = true
		return nil, ErrProcessCapacity
	}
	child := &probeRetirementChild{cohort: cohort}
	cohort.pending[child] = struct{}{}
	cohort.claims++
	return child, nil
}

func (child *probeRetirementChild) bindClose(close func() error) {
	if child == nil {
		return
	}
	child.cohort.mu.Lock()
	child.close = close
	child.cohort.mu.Unlock()
}
func (child *probeRetirementChild) started() {
	if child == nil {
		return
	}
	child.cohort.mu.Lock()
	child.hasStarted = true
	child.cohort.mu.Unlock()
}
func (child *probeRetirementChild) notStarted() {
	if child == nil {
		return
	}
	child.cohort.mu.Lock()
	if child.hasStarted {
		child.cohort.unknown = true
	} else {
		child.done = true
		delete(child.cohort.pending, child)
	}
	child.cohort.mu.Unlock()
}
func (child *probeRetirementChild) retired() {
	if child == nil {
		return
	}
	child.cohort.mu.Lock()
	child.done = true
	delete(child.cohort.pending, child)
	child.cohort.mu.Unlock()
}
func (child *probeRetirementChild) unknown() {
	if child == nil {
		return
	}
	child.cohort.mu.Lock()
	child.cohort.unknown = true
	child.cohort.mu.Unlock()
}

func (process *mediaProcess) attachProbeRetirement(ctx context.Context) error {
	child, err := beginProbeRetirementChild(ctx, process.command)
	if err != nil {
		return err
	}
	process.probeChild = child
	child.bindClose(process.Close)
	return nil
}

func (process *mediaProcess) observeProbeRetirement(retired, unknown bool) {
	if unknown {
		process.probeChild.unknown()
	}
	if retired {
		process.probeChild.retired()
	}
}

// RetirementComplete is independent from UnknownObserved: exact later cleanup
// can retire resources, but it never erases an earlier ownership fault.
func (receipt *ProbeRetirementReceipt) RetirementComplete() bool {
	if receipt == nil || receipt.cohort == nil {
		return false
	}
	receipt.cohort.mu.Lock()
	defer receipt.cohort.mu.Unlock()
	return receipt.cohort.returned && len(receipt.cohort.pending) == 0
}

func (receipt *ProbeRetirementReceipt) UnknownObserved() bool {
	if receipt == nil || receipt.cohort == nil {
		return true
	}
	receipt.cohort.mu.Lock()
	defer receipt.cohort.mu.Unlock()
	return receipt.cohort.unknown
}

// Close starts one actual cleanup owner. A caller deadline stops only waiting;
// it never drops a borrowed input, child, registration, or incomplete receipt.
func (receipt *ProbeRetirementReceipt) Close(ctx context.Context) error {
	if receipt == nil || receipt.cohort == nil || ctx == nil {
		return ErrProcessRetirementUnknown
	}
	cohort := receipt.cohort
	cohort.cleanup.Do(func() {
		cohort.mu.Lock()
		cohort.closing = true
		cohort.mu.Unlock()
		go func() {
			completed := false
			var closeErr error
			defer func() {
				_ = recover()
				cohort.mu.Lock()
				if !completed {
					cohort.unknown = true
					closeErr = errors.Join(closeErr, ErrProcessRetirementUnknown)
				}
				if !cohort.returned || len(cohort.pending) != 0 {
					closeErr = errors.Join(closeErr, ErrProcessRetirementUnknown)
				}
				cohort.closeErr = closeErr
				cohort.mu.Unlock()
				close(cohort.drained)
			}()
			cohort.mu.Lock()
			closers := make([]func() error, 0, len(cohort.pending))
			for child := range cohort.pending {
				if child.close == nil {
					cohort.unknown = true
					continue
				}
				closers = append(closers, child.close)
			}
			cohort.mu.Unlock()
			for _, close := range closers {
				closeErr = errors.Join(closeErr, close())
			}
			completed = true
		}()
	})
	select {
	case <-cohort.drained:
		cohort.mu.Lock()
		err := cohort.closeErr
		unknown := cohort.unknown
		cohort.mu.Unlock()
		if unknown {
			err = errors.Join(err, ErrProcessRetirementUnknown)
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func probeWithRetirement(ctx context.Context, file *os.File, probe func(context.Context) (Info, error)) (info Info, receipt *ProbeRetirementReceipt, resultErr error) {
	if ctx == nil || file == nil || probe == nil {
		return Info{}, nil, ErrProcessRetirementUnknown
	}
	cohort := &probeRetirementCohort{source: file, pending: make(map[*probeRetirementChild]struct{}), drained: make(chan struct{})}
	receipt = &ProbeRetirementReceipt{cohort: cohort}
	returned := false
	defer func() {
		cohort.mu.Lock()
		cohort.returned = returned
		if !returned {
			cohort.unknown = true
		}
		if cohort.unknown || len(cohort.pending) != 0 {
			resultErr = errors.Join(resultErr, ErrProcessRetirementUnknown)
		}
		cohort.mu.Unlock()
	}()
	info, resultErr = probe(context.WithValue(ctx, probeRetirementContextKey{}, cohort))
	returned = true
	return
}

// ProbeFileOwned covers the complete sequential probe cohort, including direct
// source reads and all primary-descriptor subprocesses. Its caller owns the FD.
func (p Prober) ProbeFileOwned(ctx context.Context, file *os.File) (Info, *ProbeRetirementReceipt, error) {
	if ctx == nil || file == nil {
		return Info{}, nil, ErrProcessRetirementUnknown
	}
	if _, required := commanddomain.CommandScopeFromContext(ctx); required && !nativeProbeRetirementCohortReady() {
		return Info{}, nil, commanddomain.ErrUnavailable
	}
	return probeWithRetirement(ctx, file, func(work context.Context) (Info, error) { return p.ProbeFile(work, file) })
}
