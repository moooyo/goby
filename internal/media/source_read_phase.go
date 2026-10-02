package media

import (
	"context"
	"errors"
	"os"
)

// SourceReadPhase is installed only by a trusted catalog adapter. It governs
// actual source reads without exposing root identities to media subprocesses.
// A phase must retain its charge when child retirement remains unknown.
type SourceReadPhase func(context.Context, func(context.Context) error) error

type sourceReadPhaseContextKey struct{}

// A retained phase's error keeps the exact failed descriptors reachable. This
// does not treat an error, cancellation or later garbage collection as proof
// that the descriptor or any inherited reader retired.
type sourceReadRetirementFailure struct {
	err   error
	files []*os.File
}

func (failure *sourceReadRetirementFailure) Error() string { return failure.err.Error() }
func (failure *sourceReadRetirementFailure) Unwrap() error { return failure.err }

// SourceReadRetirementError preserves failed owned descriptors inside the
// retained operation's error. Callers supply only their bounded input set.
func SourceReadRetirementError(err error, files ...*os.File) error {
	if err == nil {
		return nil
	}
	return &sourceReadRetirementFailure{err: errors.Join(ErrProcessRetirementUnknown, err), files: append([]*os.File(nil), files...)}
}

// WithSourceReadPhase binds one opaque source operation to its actual readers.
// Callers must retain the operation until every borrowed descriptor is closed.
func WithSourceReadPhase(ctx context.Context, phase SourceReadPhase) context.Context {
	if ctx == nil || phase == nil {
		return ctx
	}
	return context.WithValue(ctx, sourceReadPhaseContextKey{}, phase)
}

// RunSourceReadPhase is a synchronous actual-reader boundary. Direct fixtures
// without a catalog adapter retain their original contract. Production source
// consumers install a phase before handing an input to a worker or subprocess.
func RunSourceReadPhase(ctx context.Context, work func(context.Context) error) error {
	if ctx == nil || work == nil {
		return ErrProcessRetirementUnknown
	}
	if phase, ok := ctx.Value(sourceReadPhaseContextKey{}).(SourceReadPhase); ok && phase != nil {
		return phase(ctx, work)
	}
	return work(ctx)
}
