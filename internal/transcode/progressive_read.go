package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// ProgressiveReader follows an append-only audio output without exposing a
// seekable file. Reads are serialized, use the caller's buffer, and wait at a
// temporary EOF until more bytes arrive or the job reaches a real terminal
// state. Close can run concurrently with Read. A successful EOF still requires
// Close; cancellation, a reader context deadline, or a job error closes the
// underlying file automatically and releases its cache reservation.
type ProgressiveReader struct {
	manager     *Manager
	job         *managedJob
	file        *os.File
	ctx         context.Context
	readMu      sync.Mutex
	offset      int64
	largestSize int64
	closeOnce   sync.Once
	closed      chan struct{}
	closeCause  error
	closeErr    error
}

type progressiveState struct {
	completed bool
	changed   <-chan struct{}
}

// OpenProgressive opens only the append-only stream of a progressive plan. The
// runner must first confirm initial media payload; a container header alone
// is insufficient readiness evidence. The caller must authenticate and verify
// current account policy, library access, and the indexed source before this
// call. ctx controls both startup waiting and the returned reader's lifetime.
func (m *Manager) OpenProgressive(ctx context.Context, scope Scope, id string) (*ProgressiveReader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	j, err := m.lookupLocked(scope, id)
	if err == nil {
		err = jobError(j)
	}
	if err == nil && j.record.Spec.Plan.OutputMode != "progressive" {
		err = ErrOutputUnavailable
	}
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if _, err := m.WaitReady(ctx, scope, id); err != nil {
		return nil, err
	}
	m.mu.Lock()
	j, err = m.lookupLocked(scope, id)
	if err == nil {
		err = jobError(j)
	}
	if err == nil && (!j.ready || j.record.Spec.Plan.OutputMode != "progressive") {
		err = ErrOutputUnavailable
	}
	if err == nil && (m.readers >= m.options.MaxReaders || j.readers >= m.options.MaxJobReaders) {
		err = ErrBusy
	}
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	j.readers++
	m.readers++
	m.touchLocked(j)
	m.mu.Unlock()
	m.filesMu.Lock()
	file, openErr := m.cache.OpenProgressiveFile(id)
	m.filesMu.Unlock()
	var size int64
	if openErr == nil {
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > m.options.MaxJobBytes {
			openErr = ErrOutputUnavailable
		} else {
			size = info.Size()
		}
	}
	// The pin prevents deletion while the filesystem call runs. It does not
	// permit a cancelled job or a closing manager to publish a new reader.
	m.mu.Lock()
	current, err := m.lookupLocked(scope, id)
	if err == nil && current != j {
		err = ErrJobNotFound
	}
	if err == nil {
		err = jobError(j)
	}
	if err == nil && (!j.ready || openErr != nil) {
		err = ErrOutputUnavailable
	}
	if err == nil {
		err = ctx.Err()
	}
	m.mu.Unlock()
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		m.releaseReader(j)
		return nil, err
	}
	r := &ProgressiveReader{manager: m, job: j, file: file, ctx: ctx, largestSize: size, closed: make(chan struct{})}
	go r.watch()
	return r, nil
}

// Close releases the underlying file and its reader reservation once. An
// explicit close interrupts waiting reads with os.ErrClosed. If the context or
// job had already closed this reader, its earlier failure classification stays
// available to Read.
func (r *ProgressiveReader) Close() error {
	return r.closeWithError(os.ErrClosed)
}

func (r *ProgressiveReader) closeWithError(cause error) error {
	r.closeOnce.Do(func() {
		r.closeCause = cause
		close(r.closed)
		r.closeErr = r.file.Close()
		r.manager.releaseReader(r.job)
	})
	return r.closeErr
}

func (r *ProgressiveReader) failRead(err error) error {
	_ = r.closeWithError(err)
	return r.closeCause
}

func (r *ProgressiveReader) state() (progressiveState, error) {
	select {
	case <-r.closed:
		return progressiveState{}, r.closeCause
	default:
	}
	if err := r.ctx.Err(); err != nil {
		return progressiveState{}, err
	}
	m := r.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	j, err := m.lookupLocked(r.job.record.Spec.Scope, r.job.record.ID)
	if err == nil && j != r.job {
		err = ErrJobNotFound
	}
	if err == nil {
		err = jobError(j)
	}
	if err != nil {
		return progressiveState{}, err
	}
	return progressiveState{completed: j.finished && j.record.State == "completed", changed: j.changed}, nil
}

func (r *ProgressiveReader) watch() {
	for {
		state, err := r.state()
		if err != nil {
			_ = r.closeWithError(err)
			return
		}
		select {
		case <-r.closed:
			return
		case <-r.ctx.Done():
			_ = r.closeWithError(r.ctx.Err())
			return
		case <-r.manager.ctx.Done():
			_ = r.closeWithError(ErrManagerClosed)
			return
		case <-state.changed:
		}
	}
}

// Read returns io.EOF only after durable successful completion and after all
// final bytes have been consumed. Process failure and cancellation remain
// errors, including when they occur after a partial response has been read.
func (r *ProgressiveReader) Read(buffer []byte) (int, error) {
	r.readMu.Lock()
	defer r.readMu.Unlock()
	for {
		if _, err := r.state(); err != nil {
			return 0, r.failRead(err)
		}
		if len(buffer) == 0 {
			return 0, nil
		}
		if _, err := r.outputSize(); err != nil {
			return 0, r.failRead(err)
		}
		n, readErr := r.file.ReadAt(buffer, r.offset)
		r.offset += int64(n)
		state, stateErr := r.state()
		if n > 0 {
			r.manager.mu.Lock()
			r.manager.touchLocked(r.job)
			r.manager.mu.Unlock()
		}
		if stateErr != nil {
			return n, r.failRead(stateErr)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return n, r.failRead(r.invalidate("cache_unavailable"))
		}
		if n > 0 {
			return n, nil
		}
		size, err := r.outputSize()
		if err != nil {
			return 0, r.failRead(err)
		}
		if size > r.offset {
			// Publication may have raced the EOF read, including immediately
			// before the worker persisted its successful completion.
			continue
		}
		if state.completed {
			if _, err := r.state(); err != nil {
				return 0, r.failRead(err)
			}
			return 0, io.EOF
		}
		timer := time.NewTimer(r.manager.options.pollInterval)
		select {
		case <-r.closed:
			timer.Stop()
			return 0, r.failRead(r.closeCause)
		case <-r.ctx.Done():
			timer.Stop()
			return 0, r.failRead(r.ctx.Err())
		case <-r.manager.ctx.Done():
			timer.Stop()
			return 0, r.failRead(ErrManagerClosed)
		case <-state.changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (r *ProgressiveReader) outputSize() (int64, error) {
	info, err := r.file.Stat()
	if err != nil {
		if _, stateErr := r.state(); stateErr != nil {
			return 0, stateErr
		}
		return 0, r.invalidate("cache_unavailable")
	}
	if !info.Mode().IsRegular() || info.Size() < r.largestSize || info.Size() < r.offset {
		return 0, r.invalidate("invalid_output")
	}
	if info.Size() > r.manager.options.MaxJobBytes {
		return 0, r.invalidate("job_quota")
	}
	r.largestSize = info.Size()
	return info.Size(), nil
}

func (r *ProgressiveReader) invalidate(code string) error {
	m := r.manager
	m.mu.Lock()
	j, err := m.lookupLocked(r.job.record.Spec.Scope, r.job.record.ID)
	if err == nil {
		err = jobError(j)
	}
	if err == nil {
		if j.finished {
			m.invalidateFinishedLocked(j, code)
		} else {
			m.stopLocked(j, code)
		}
		err = jobError(j)
	}
	m.mu.Unlock()
	m.signal()
	return err
}
