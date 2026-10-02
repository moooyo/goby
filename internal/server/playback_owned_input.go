package server

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/moooyo/goby/internal/transcode"
)

type playbackAdmissionContextKey struct{}

type playbackOwnedInputState uint8

const (
	playbackOwnedInputReady playbackOwnedInputState = iota
	playbackOwnedInputConsuming
	playbackOwnedInputConsumed
	playbackOwnedInputClosed
	playbackOwnedInputUnknown
)

// This couples an actual descriptor loan with cancellation lifetime ownership.
// It is not a source, input, packet, native-clock or authorization certificate.
// Raw files never reconstruct references through a pointer/FD lookup table.
type playbackOwnedInput struct {
	mu        sync.Mutex
	file      *os.File
	reference *playbackAdmissionReference
	scope     transcode.Scope
	state     playbackOwnedInputState
}

type playbackOwnedInputConsumer interface {
	// The existing Ensure contract consumes the actual FD on every return.
	Ensure(context.Context, transcode.Spec, *os.File) (transcode.Record, error)
}

// transfer moves an already owned lifetime without increasing its reference
// count. All old/copy handles become unusable. Cleanup transfers remain legal
// after Stop/Close; an admission transfer additionally requires a live scope.
func (reference *playbackAdmissionReference) transfer(admission bool) (*playbackAdmissionReference, error) {
	if reference == nil || reference.gate == nil || reference.entry == nil || reference.released == nil {
		return nil, transcode.ErrInvalidScope
	}
	gate := reference.gate
	gate.mu.Lock()
	defer gate.mu.Unlock()
	entry := reference.entry
	if *reference.released || gate.entries[playbackStopIntentKeyFor(entry.scope)] != entry ||
		admission && (entry.stopped || gate.closing) {
		return nil, transcode.ErrJobCancelled
	}
	*reference.released = true
	return &playbackAdmissionReference{gate: gate, entry: entry, released: new(bool)}, nil
}

// newPlaybackOwnedInput consumes file and its separately minted FD owner.
// The reference must exist before opening/borrowing the descriptor. Transfer
// invalidates every old handle instead of relying on caller release discipline.
func newPlaybackOwnedInput(file *os.File, parent *playbackAdmissionReference) (*playbackOwnedInput, error) {
	if file == nil {
		parent.release()
		return nil, transcode.ErrInvalidInput
	}
	child, err := parent.transfer(false)
	if err != nil {
		closeErr := file.Close()
		if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			// This is a broken incoming ownership contract. Retain the actual
			// descriptor as unknown; never turn its failed Close into admission.
			return &playbackOwnedInput{file: file, reference: parent, state: playbackOwnedInputUnknown}, errors.Join(err, closeErr)
		}
		return nil, err
	}
	return &playbackOwnedInput{file: file, reference: child, scope: child.entry.scope}, nil
}

// duplicate keeps the original loan intact. The child exists before actual FD
// duplication, so an HTTP parent release cannot outlive an untracked worker.
func (input *playbackOwnedInput) duplicate() (*playbackOwnedInput, error) {
	if input == nil {
		return nil, transcode.ErrInvalidInput
	}
	input.mu.Lock()
	defer input.mu.Unlock()
	if input.state != playbackOwnedInputReady || input.file == nil || input.reference == nil {
		return nil, transcode.ErrInvalidInput
	}
	child, err := input.reference.fork(input.scope)
	if err != nil {
		return nil, err
	}
	file, err := transcode.DuplicateInput(input.file)
	if err != nil {
		child.release()
		return nil, err
	}
	return &playbackOwnedInput{file: file, reference: child, scope: input.scope}, nil
}

// close releases only after actual FD closure. Unknown close/consumption keeps
// its lifetime fenced; cancellation or a request return cannot fake ownership
// recovery. Explicit recovery must establish actual descriptor/consumer join.
func (input *playbackOwnedInput) close() error {
	if input == nil {
		return nil
	}
	input.mu.Lock()
	defer input.mu.Unlock()
	switch input.state {
	case playbackOwnedInputClosed, playbackOwnedInputConsumed:
		return nil
	case playbackOwnedInputConsuming, playbackOwnedInputUnknown:
		return transcode.ErrOutputUnavailable
	}
	if input.file == nil || input.reference == nil {
		input.state = playbackOwnedInputUnknown
		return transcode.ErrInvalidInput
	}
	if err := input.file.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		input.state = playbackOwnedInputUnknown
		return err
	}
	input.state = playbackOwnedInputClosed
	input.reference.release()
	input.file, input.reference = nil, nil
	return nil
}

// ensure transfers the FD to an existing consuming manager, retaining its
// independent lifetime through actual admission/rejection. A panic/Goexit is
// unknown consumption and retains ownership instead of claiming cleanup.
func (input *playbackOwnedInput) ensure(ctx context.Context, consumer playbackOwnedInputConsumer, spec transcode.Spec) (transcode.Record, error) {
	if input == nil {
		return transcode.Record{}, transcode.ErrInvalidInput
	}
	if ctx == nil || consumer == nil {
		return transcode.Record{}, errors.Join(transcode.ErrInvalidInput, input.close())
	}
	if err := ctx.Err(); err != nil {
		return transcode.Record{}, errors.Join(err, input.close())
	}
	input.mu.Lock()
	if input.state != playbackOwnedInputReady || input.file == nil || input.reference == nil {
		input.mu.Unlock()
		return transcode.Record{}, transcode.ErrInvalidInput
	}
	if input.scope != spec.Scope {
		input.mu.Unlock()
		return transcode.Record{}, errors.Join(transcode.ErrInvalidScope, input.close())
	}
	// This consumes the FD owner's handle; a concurrent request cleanup can
	// neither release nor reuse it. A stopped/released loan never reaches Ensure.
	consumerOwner, err := input.reference.transfer(true)
	if err != nil {
		input.mu.Unlock()
		return transcode.Record{}, errors.Join(err, input.close())
	}
	file := input.file
	input.reference = consumerOwner
	input.state = playbackOwnedInputConsuming
	input.mu.Unlock()
	returned := false
	defer func() {
		input.mu.Lock()
		defer input.mu.Unlock()
		if !returned {
			input.state = playbackOwnedInputUnknown
			// Retain the actual FD and consumer owner in input. Neither caller
			// cleanup nor panic recovery establishes the receiving owner/join.
			return
		}
		input.state = playbackOwnedInputConsumed
		input.file, input.reference = nil, nil
		consumerOwner.release()
	}()
	work := context.WithValue(ctx, playbackAdmissionContextKey{}, consumerOwner)
	record, err := consumer.Ensure(work, spec, file)
	returned = true
	return record, err
}

// holdContext is the optional manager admission callback. It only forks a real
// owner from this gate/generation. It never authorizes a scope from strings,
// caches AUTH facts, waits, performs IO, or reenters a resource-domain mutex.
func (gate *playbackStopIntentGate) holdContext(ctx context.Context, scope transcode.Scope) (context.Context, func(), error) {
	if ctx == nil {
		return nil, nil, transcode.ErrInvalidScope
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	parent, _ := ctx.Value(playbackAdmissionContextKey{}).(*playbackAdmissionReference)
	if parent == nil || parent.gate != gate {
		return nil, nil, transcode.ErrInvalidScope
	}
	child, err := parent.fork(scope)
	if err != nil {
		return nil, nil, err
	}
	return context.WithValue(ctx, playbackAdmissionContextKey{}, child), child.release, nil
}
