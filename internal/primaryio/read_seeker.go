package primaryio

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

const MaxReadChunkBytes = 32 * 1024

var (
	ErrConsumerRetirement = errors.New("primary read consumer cleanup could not establish retirement")
	ErrReadResult         = errors.New("primary source returned an invalid read result")
)

// ReadSeekCloser must perform synchronous source reads and memory-only seeks.
// Original files use a bounded SectionReader over the authorized descriptor.
// Close must retire that descriptor; it must not wait for this adapter's mutex.
type ReadSeekCloser interface {
	Read([]byte) (int, error)
	Seek(int64, int) (int64, error)
	Close() error
}

// ReadSeeker exposes no WriterTo, ReaderAt, raw descriptor, or unwrap fast path.
// It borrows a bounded portion of its caller's buffer for each synchronous read.
// Network writes occur after Read returns and its actual-I/O lease is released.
type ReadSeeker struct {
	mu           sync.Mutex
	ioMu         sync.Mutex
	methods      sync.WaitGroup
	owner        *Owner
	route        preparedRoute
	class        Class
	source       ReadSeekCloser
	chunk        int
	retired      func()
	closing      bool
	firstErr     error
	closeErr     error
	closeOnce    sync.Once
	closeDone    chan struct{}
	callbackDone chan struct{}
	stopCallback func() bool
	cleanupErr   error
}

// NewReadSeeker consumes idle retained-owner rights only on success. Failure
// leaves the caller responsible for source cleanup, Complete, and its Store pin.
// The retired callback runs only after source Close and all actual methods join.
func NewReadSeeker(owner *Owner, route Route, class Class, source ReadSeekCloser, maxChunk int, retired func()) (*ReadSeeker, error) {
	if owner == nil || owner.Context() == nil || source == nil || maxChunk < 1 || maxChunk > MaxReadChunkBytes || class != Foreground && class != Background {
		return nil, ErrInvalid
	}
	prepared, err := prepareRoute(route)
	if err != nil {
		return nil, err
	}
	if err := owner.Context().Err(); err != nil {
		return nil, err
	}
	owned, err := owner.Transfer()
	if err != nil {
		return nil, err
	}
	reader := &ReadSeeker{owner: owned, route: prepared, class: class, source: source, chunk: maxChunk,
		retired: retired, closeDone: make(chan struct{}), callbackDone: make(chan struct{})}
	reader.stopCallback = context.AfterFunc(owned.Context(), func() {
		defer close(reader.callbackDone)
		// An abnormal Close cannot fabricate successful cleanup. The callback
		// receipt still permits Close to report uncertainty without retiring it.
		returned := false
		defer func() {
			_ = recover()
			if !returned {
				reader.cleanupErr = ErrConsumerRetirement
			}
		}()
		reader.cleanupErr = source.Close()
		if errors.Is(reader.cleanupErr, os.ErrClosed) {
			reader.cleanupErr = nil
		}
		returned = true
	})
	return reader, nil
}

func (r *ReadSeeker) Context() context.Context {
	if r == nil {
		return nil
	}
	return r.owner.Context()
}

// Cancel requests interruption and descriptor Close; it releases neither an
// in-flight read lease nor retained ownership. Close joins the actual callback.
func (r *ReadSeeker) Cancel() error {
	if r == nil {
		return ErrInvalid
	}
	return r.owner.Cancel()
}

func (r *ReadSeeker) begin() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return ErrClosed
	}
	r.methods.Add(1)
	return nil
}

func (r *ReadSeeker) remember(err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	r.mu.Lock()
	if r.firstErr == nil {
		r.firstErr = err
	}
	r.mu.Unlock()
}

func (r *ReadSeeker) Read(buffer []byte) (n int, err error) {
	if r == nil {
		return 0, ErrInvalid
	}
	if err := r.begin(); err != nil {
		return 0, err
	}
	defer r.methods.Done()
	r.ioMu.Lock()
	defer r.ioMu.Unlock()
	defer func() { r.remember(err) }()
	if err := r.owner.Context().Err(); err != nil {
		return 0, err
	}
	if len(buffer) == 0 {
		return 0, nil
	}
	lease, err := r.owner.acquirePrepared(r.route, r.class)
	if err != nil {
		return 0, err
	}
	defer func() {
		if releaseErr := lease.Release(); err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()
	bounded := buffer[:min(len(buffer), r.chunk)]
	n, err = r.source.Read(bounded)
	if n < 0 || n > len(bounded) {
		n, err = 0, ErrReadResult
	}
	if err != nil && r.owner.Context().Err() != nil {
		err = r.owner.Context().Err()
	}
	return n, err
}

// Seek changes only the logical SectionReader offset. It is serialized with
// reads and joined by Close, but does not consume a disk-read reservation.
func (r *ReadSeeker) Seek(offset int64, whence int) (position int64, err error) {
	if r == nil {
		return 0, ErrInvalid
	}
	if err := r.begin(); err != nil {
		return 0, err
	}
	defer r.methods.Done()
	r.ioMu.Lock()
	defer r.ioMu.Unlock()
	defer func() { r.remember(err) }()
	if err := r.owner.Context().Err(); err != nil {
		return 0, err
	}
	return r.source.Seek(offset, whence)
}

// Err reports the first admission/read/seek failure. EOF is a normal reader
// boundary; the HTTP adapter separately verifies the declared body length.
func (r *ReadSeeker) Err() error {
	if r == nil {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.firstErr
}

// Close fences future calls, cancels queued admission, joins native descriptor
// Close and every entered Read/Seek, then completes retained ownership. It does
// not replay a historical read cancellation as a cleanup error. Multipart HTTP
// producers attempting a late call see the closed fence and perform no I/O.
func (r *ReadSeeker) Close() error {
	if r == nil {
		return ErrInvalid
	}
	r.closeOnce.Do(func() {
		defer close(r.closeDone)
		// An abnormal retirement callback leaves explicit uncertainty rather
		// than letting a subsequent Close report a default successful result.
		r.closeErr = ErrConsumerRetirement
		r.mu.Lock()
		r.closing = true
		r.mu.Unlock()
		if err := r.owner.Cancel(); err != nil {
			r.closeErr = err
			return
		}
		<-r.callbackDone
		r.stopCallback()
		r.methods.Wait()
		if r.cleanupErr != nil {
			r.closeErr = r.cleanupErr
			return
		}
		if err := r.owner.Complete(); err != nil {
			r.closeErr = err
			return
		}
		// The adapter's callback releases only external metadata/Store pins.
		// Complete failure must leave those pins intact. Callback abnormal exit
		// still reports uncertainty; it cannot undo verified native retirement.
		if r.retired != nil {
			r.retired()
		}
		r.closeErr = nil
	})
	<-r.closeDone
	return r.closeErr
}
