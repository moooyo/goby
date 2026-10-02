package media

import (
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/commanddomain"
)

// ProbeFileOwnershipAvailable is a static dispatch gate only. A true result
// still requires the actual opaque callback/child retirement receipt.
func ProbeFileOwnershipAvailable(ctx context.Context) bool {
	if ctx == nil || !conventionalRetirementSupported() {
		return false
	}
	_, required := commanddomain.CommandScopeFromContext(ctx)
	return !required || nativeProbeRetirementCohortReady()
}

// ProbeFileJoinedCallbacks accepts an explicit synchronous borrowing contract.
// The callback must join all direct source use before returning and must route
// every source-bearing child through this package's actual process gateways.
// External or unregistered children are unsupported. Zero tracked children
// covers only the declared callback; semantic nil is no operating-system proof.
func ProbeFileJoinedCallbacks(ctx context.Context, file *os.File, callback func(context.Context, *os.File) (Info, error)) (Info, *ProbeRetirementReceipt, error) {
	if ctx == nil || file == nil || callback == nil {
		return Info{}, nil, ErrProcessRetirementUnknown
	}
	if err := ctx.Err(); err != nil {
		return probeWithRetirement(ctx, file, func(context.Context) (Info, error) { return Info{}, err })
	}
	if !conventionalRetirementSupported() {
		return Info{}, nil, commanddomain.ErrUnavailable
	}
	if _, required := commanddomain.CommandScopeFromContext(ctx); required && !nativeProbeRetirementCohortReady() {
		return Info{}, nil, commanddomain.ErrUnavailable
	}
	cohort := &probeRetirementCohort{source: file, pending: make(map[*probeRetirementChild]struct{}), drained: make(chan struct{})}
	receipt := &ProbeRetirementReceipt{cohort: cohort}
	type result struct {
		info Info
		err  error
	}
	joined := make(chan result, 1)
	// One owned callback runs at a time. The caller joins its actual return,
	// including panic and Goexit, rather than abandoning it on cancellation.
	go func() {
		outcome := result{}
		returned := false
		defer func() {
			_ = recover()
			cohort.mu.Lock()
			cohort.returned = true
			if !returned {
				cohort.unknown = true
				outcome.info = Info{}
			}
			if cohort.unknown || len(cohort.pending) != 0 {
				outcome.err = errors.Join(outcome.err, ErrProcessRetirementUnknown)
			}
			cohort.mu.Unlock()
			joined <- outcome
		}()
		if err := ctx.Err(); err != nil {
			outcome.err = err
		} else {
			outcome.info, outcome.err = callback(context.WithValue(ctx, probeRetirementContextKey{}, cohort), file)
		}
		returned = true
	}()
	outcome := <-joined
	return outcome.info, receipt, outcome.err
}
