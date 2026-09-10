package backupformat

import (
	"context"
	"errors"
	"io"
)

// contextReader does not launch an unbounded goroutine around blocking I/O.
// A caller that supplies a blocking reader must arrange to interrupt it when
// its context is cancelled, for example by closing the caller-owned handle.
type contextReader struct {
	ctx        context.Context
	source     io.Reader
	noProgress int
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.source.Read(data)
	if cancelled := r.ctx.Err(); cancelled != nil {
		return n, cancelled
	}
	if n == 0 && err == nil && len(data) > 0 {
		r.noProgress++
		if r.noProgress >= 100 {
			return 0, io.ErrNoProgress
		}
	} else {
		r.noProgress = 0
	}
	return n, err
}

type contextWriter struct {
	ctx         context.Context
	destination io.Writer
}

func (w contextWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.destination.Write(data)
	if cancelled := w.ctx.Err(); cancelled != nil {
		return n, cancelled
	}
	if err != nil {
		if errors.Is(err, ErrLimit) {
			return n, ErrLimit
		}
		return n, ErrSink
	}
	if n != len(data) {
		return n, ErrSink
	}
	return n, nil
}

// budgetReader permits one bounded lookahead byte to distinguish exact EOF
// from input that exceeds the limit. It never turns a size limit into a false
// EOF, which could otherwise hide trailing bytes from age's authentication.
type budgetReader struct {
	source    io.Reader
	remaining int64
	err       error
}

func (r *budgetReader) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	if r.remaining == 0 {
		var lookahead [1]byte
		n, err := r.source.Read(lookahead[:])
		if n != 0 {
			r.err = ErrLimit
			return 0, r.err
		}
		if err != nil && err != io.EOF {
			r.err = err
		}
		return 0, err
	}
	if int64(len(data)) > r.remaining {
		data = data[:r.remaining]
	}
	n, err := r.source.Read(data)
	r.remaining -= int64(n)
	return n, err
}

type budgetWriter struct {
	destination io.Writer
	remaining   int64
}

func (w *budgetWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.remaining {
		return 0, ErrLimit
	}
	n, err := w.destination.Write(data)
	w.remaining -= int64(n)
	return n, err
}

func archiveReadError(ctx context.Context, err error) error {
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	if errors.Is(err, ErrLimit) {
		return ErrLimit
	}
	// Do not return age errors: malformed headers can include attacker-chosen
	// strings. Passphrases, member contents, and OS errors are not diagnostics.
	return ErrInvalidArchive
}

func creationError(ctx context.Context, err, fallback error) error {
	if cancelled := ctx.Err(); cancelled != nil {
		return cancelled
	}
	for _, known := range []error{ErrLimit, ErrSink, ErrSource, ErrSourceChanged} {
		if errors.Is(err, known) {
			return known
		}
	}
	return fallback
}
