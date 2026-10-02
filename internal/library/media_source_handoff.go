package library

import (
	"context"
	"fmt"
	"os"
	"sync"
)

// The owner retains its handoff budget and Store lifetime until work and all
// undelivered-file cleanup actually finish. Cleanup belongs exclusively to the
// worker and must close its file while holding actual IO or cleanup admission.
// A successful receiver owns the delivered file independently of that lifetime.
func runOwnedMediaSourceHandoff(ctx context.Context, release func(), work func() (*os.File, MediaFile, error), cleanup func(*os.File)) (*os.File, MediaFile, error) {
	if release == nil {
		return nil, MediaFile{}, ErrInvalidInput
	}
	var releaseOnce sync.Once
	finish := func() { releaseOnce.Do(release) }
	if err := ctx.Err(); err != nil {
		finish()
		return nil, MediaFile{}, err
	}
	if work == nil || cleanup == nil {
		finish()
		return nil, MediaFile{}, ErrInvalidInput
	}
	result := make(chan mediaSourceWorkResult)
	accepted := make(chan bool, 1)
	go func() {
		defer finish()
		if ctx.Err() != nil {
			return
		}
		file, source, err := executeOwnedMediaSourceHandoff(ctx, work)
		if err != nil && file != nil {
			cleanup(file)
			file = nil
		}
		select {
		case result <- mediaSourceWorkResult{file: file, source: source, err: err}:
			// After sending, only the receiver's acknowledgement determines
			// file ownership. Selecting cancellation here could reclaim a file
			// whose successful receiver had already decided to accept it.
			delivered := <-accepted
			if !delivered && file != nil {
				cleanup(file)
			}
		case <-ctx.Done():
			if file != nil {
				cleanup(file)
			}
		}
	}()
	select {
	case outcome := <-result:
		acknowledged := false
		// The buffered acknowledgement cannot block if worker scheduling or
		// cancellation changes after this receiver selected the result.
		defer func() { accepted <- acknowledged }()
		if err := ctx.Err(); err != nil {
			return nil, MediaFile{}, err
		}
		acknowledged = outcome.err == nil
		return outcome.file, outcome.source, outcome.err
	case <-ctx.Done():
		return nil, MediaFile{}, ctx.Err()
	}
}

// Work owns every descriptor until it returns successfully and must unwind its
// own admitted cleanup on panic. This boundary translates the panic without
// performing a Close outside the captured IO domain.
func executeOwnedMediaSourceHandoff(ctx context.Context, work func() (*os.File, MediaFile, error)) (file *os.File, source MediaFile, err error) {
	defer func() {
		if recover() != nil {
			file, source = nil, MediaFile{}
			err = fmt.Errorf("%w: media source handoff worker panicked", ErrUnavailable)
			if canceled := ctx.Err(); canceled != nil {
				err = canceled
			}
		}
	}()
	return work()
}
