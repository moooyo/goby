package transcode

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
)

const maxWorkspaceWriters = 4096

var (
	errWorkspaceWriterClosed   = errors.New("transcode workspace writer admission is closed")
	errWorkspaceWriterAbnormal = errors.New("transcode workspace writer did not return normally")
	errWorkspaceWriterClose    = errors.New("transcode workspace descriptor close did not complete normally")
)

type workspaceWriterKind uint8

const (
	workspaceWriterOpening workspaceWriterKind = iota
	workspaceWriterFile
	workspaceWriterMutation
	workspaceWriterTask
	workspaceWriterQuarantined
)

type workspaceWriterEntry struct {
	index int
	kind  workspaceWriterKind
	file  *workspaceWritableFile
	task  *workspaceWriterTaskHandle
}

// workspaceWriterSnapshot is diagnostic, not retirement authority. Done marks
// the fixed closer's exit; a failed scope can still own a live quarantined task.
type workspaceWriterSnapshot struct {
	Capacity        int
	Opening         int
	Files           int
	Mutations       int
	Tasks           int
	Quarantined     int
	AdmissionClosed bool
	RootClosed      bool
	Done            bool
	Failure         error
}

// workspaceWriterScope owns only trusted Go writers routed through this API.
// It opens and retains its own Root, never exposes a raw descriptor or *os.File,
// and bounds live opens, writable files, mutations and tasks with one fixed
// array. One fixed closer exists for the scope; neither drain nor wait creates
// a goroutine. The enclosing job must retain its storage lease until this scope
// and the separate command domain have both retired.
//
// Rooted path resolution does not prohibit bind mounts, filesystem boundaries,
// device files or /proc access. This module supplies no native mount identity,
// storage-capacity, write-confinement or command-domain proof. Trusted callers
// must route every workspace mutation here, must not duplicate descriptors or
// start untracked writer goroutines, and must supply the approved workspace
// root. The finalizer opens its own read-only root after a successful drain;
// any later finalizer mutation needs a separate owned writer phase.
type workspaceWriterScope struct {
	mu         sync.Mutex
	root       *os.Root
	entries    []*workspaceWriterEntry
	stopped    bool
	rootClosed bool
	finished   bool
	firstErr   error
	changed    chan struct{}
	done       chan struct{}
}

func newWorkspaceWriterScope(directory string, capacity int) (*workspaceWriterScope, error) {
	if directory == "" || capacity < 1 || capacity > maxWorkspaceWriters {
		return nil, ErrInvalidOptions
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	scope := &workspaceWriterScope{root: root, entries: make([]*workspaceWriterEntry, capacity),
		changed: make(chan struct{}, 1), done: make(chan struct{})}
	go scope.closeOwnedWriters()
	return scope, nil
}

func (s *workspaceWriterScope) reserveLocked(kind workspaceWriterKind) (*workspaceWriterEntry, error) {
	if s.stopped {
		return nil, errWorkspaceWriterClosed
	}
	for index, entry := range s.entries {
		if entry == nil {
			entry = &workspaceWriterEntry{index: index, kind: kind}
			s.entries[index] = entry
			return entry, nil
		}
	}
	return nil, ErrBusy
}

func (s *workspaceWriterScope) reserve(kind workspaceWriterKind) (*workspaceWriterEntry, error) {
	if s == nil {
		return nil, ErrInvalidOptions
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reserveLocked(kind)
}

func (s *workspaceWriterScope) signal() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// finish returns a slot only after the owned operation returned normally or
// the actual descriptor closed successfully. An abnormal return or close error
// retains the exact entry, closes admission, and prevents a successful drain.
func (s *workspaceWriterScope) finish(entry *workspaceWriterEntry, quarantine bool, err error) {
	s.mu.Lock()
	s.finishLocked(entry, quarantine, err)
	s.mu.Unlock()
	s.signal()
}

func (s *workspaceWriterScope) finishLocked(entry *workspaceWriterEntry, quarantine bool, err error) {
	if entry != nil && entry.index >= 0 && entry.index < len(s.entries) && s.entries[entry.index] == entry {
		if quarantine {
			entry.kind = workspaceWriterQuarantined
			s.stopped = true
			if s.firstErr == nil {
				s.firstErr = err
				if s.firstErr == nil {
					s.firstErr = errWorkspaceWriterAbnormal
				}
			}
		} else if entry.kind != workspaceWriterQuarantined {
			s.entries[entry.index] = nil
		}
	}
}

// openFile reserves before the first open/create side effect. The wrapper owns
// the descriptor until Close succeeds; only writable regular files are returned.
// An open already in progress when admission closes remains owned and will be
// closed by the fixed closer after validation completes. Existing wrappers may
// write until their Close begins, but no later open or metadata mutation enters.
func (s *workspaceWriterScope) openFile(name string, flag int, perm os.FileMode) (result *workspaceWritableFile, err error) {
	const supported = os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_CREATE | os.O_EXCL | os.O_SYNC | os.O_TRUNC
	access := flag & (os.O_WRONLY | os.O_RDWR)
	if flag & ^supported != 0 || access != os.O_WRONLY && access != os.O_RDWR || perm & ^os.FileMode(0o777) != 0 {
		return nil, ErrInvalidOptions
	}
	entry, err := s.reserve(workspaceWriterOpening)
	if err != nil {
		return nil, err
	}
	returned := false
	var wrapped *workspaceWritableFile
	defer func() {
		if !returned {
			_ = recover()
			err = errWorkspaceWriterAbnormal
			s.finish(entry, true, err)
			if wrapped != nil {
				_ = wrapped.Close()
			}
		}
	}()
	file, err := s.root.OpenFile(name, flag, perm)
	if err != nil {
		s.finish(entry, false, nil)
		returned = true
		return nil, err
	}
	wrapped = &workspaceWritableFile{scope: s, entry: entry, file: file}
	s.mu.Lock()
	entry.file = wrapped
	s.mu.Unlock()
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		closeErr := wrapped.Close()
		returned = true
		if statErr == nil {
			statErr = os.ErrInvalid
		}
		return nil, errors.Join(statErr, closeErr)
	}
	s.mu.Lock()
	entry.kind = workspaceWriterFile
	s.mu.Unlock()
	s.signal()
	returned = true
	return wrapped, nil
}

// mutate is private to the rooted metadata methods below: no caller receives
// the Root or a release token that could be returned before its mutation ends.
// A normal filesystem error is a joined operation and remains the caller's
// production error; panic or Goexit quarantines ownership instead.
func (s *workspaceWriterScope) mutate(operation func() error) (err error) {
	entry, err := s.reserve(workspaceWriterMutation)
	if err != nil {
		return err
	}
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			err = errWorkspaceWriterAbnormal
		}
		s.finish(entry, !returned, err)
	}()
	err = operation()
	returned = true
	return err
}

func (s *workspaceWriterScope) mkdir(name string, perm os.FileMode) error {
	return s.mutate(func() error { return s.root.Mkdir(name, perm) })
}

func (s *workspaceWriterScope) mkdirAll(name string, perm os.FileMode) error {
	return s.mutate(func() error { return s.root.MkdirAll(name, perm) })
}

func (s *workspaceWriterScope) rename(oldName, newName string) error {
	return s.mutate(func() error { return s.root.Rename(oldName, newName) })
}

func (s *workspaceWriterScope) remove(name string) error {
	return s.mutate(func() error { return s.root.Remove(name) })
}

// start creates one writer callback goroutine after reserving a fixed entry.
// The task must not launch untracked writers. A normal callback error is available from
// its handle; its return still joins the writer. The outermost task defer catches
// both panic and Goexit, retaining the entry even after its descriptors close.
func (s *workspaceWriterScope) start(ctx context.Context, callback func(context.Context) error) (result *workspaceWriterTaskHandle, err error) {
	if s == nil || ctx == nil || callback == nil {
		return nil, ErrInvalidOptions
	}
	// Even context propagation may create a goroutine for a custom parent.
	// Reserve before constructing it, including starts later rejected by stop.
	entry, err := s.reserve(workspaceWriterTask)
	if err != nil {
		return nil, err
	}
	returned := false
	var cancel context.CancelFunc
	var task *workspaceWriterTaskHandle
	defer func() {
		if !returned {
			_ = recover()
			err = errWorkspaceWriterAbnormal
			if task == nil {
				s.finish(entry, true, err)
			} else {
				task.err = err
				s.finishTask(entry, task, true)
			}
			// Commit quarantine before cancellation can invoke custom parent
			// code. A second panic or Goexit cannot lose this ownership entry.
			defer func() { _ = recover() }()
			if cancel != nil {
				cancel()
			}
		}
	}()
	var taskContext context.Context
	taskContext, cancel = context.WithCancel(ctx)
	task = &workspaceWriterTaskHandle{scope: s, entry: entry, cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	entry.task = task
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		if cancelErr := task.stop(); cancelErr != nil {
			task.err = cancelErr
			s.finishTask(entry, task, true)
			returned = true
			return task, cancelErr
		}
	}
	s.signal()
	go func() {
		taskReturned := false
		defer func() {
			if !taskReturned {
				_ = recover()
				task.err = errWorkspaceWriterAbnormal
			}
			cleanupReturned := false
			var cancelErr error
			defer func() {
				if !cleanupReturned {
					_ = recover()
					task.err = errWorkspaceWriterAbnormal
				} else if cancelErr != nil {
					task.err = cancelErr
				}
				s.finishTask(entry, task, !taskReturned || !cleanupReturned || cancelErr != nil)
			}()
			cancelErr = task.stop()
			cleanupReturned = true
		}()
		task.err = callback(taskContext)
		taskReturned = true
	}()
	returned = true
	return task, nil
}

// Closing the task completion channel and committing its exact ownership entry
// share the scope lock, so both wait and drain observe the completed transition.
func (s *workspaceWriterScope) finishTask(entry *workspaceWriterEntry, task *workspaceWriterTaskHandle, quarantine bool) {
	s.mu.Lock()
	s.finishLocked(entry, quarantine, task.err)
	close(task.done)
	s.mu.Unlock()
	s.signal()
}

type workspaceWriterTaskHandle struct {
	scope  *workspaceWriterScope
	entry  *workspaceWriterEntry
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func (t *workspaceWriterTaskHandle) stop() (err error) {
	if t == nil {
		return ErrInvalidOptions
	}
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			err = errWorkspaceWriterAbnormal
			t.scope.finish(t.entry, true, err)
		}
	}()
	t.cancel()
	returned = true
	return nil
}

// wait joins only this callback. A normal callback result does not erase scope
// quarantine caused by an abnormal cancellation or descriptor close elsewhere.
func (t *workspaceWriterTaskHandle) wait(ctx context.Context) error {
	if t == nil || ctx == nil {
		return ErrInvalidOptions
	}
	select {
	case <-t.done:
		return t.err
	default:
	}
	select {
	case <-t.done:
		return t.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// stop closes only new admission. The fixed closer cancels owned tasks and
// closes writable descriptors outside the scope lock, then joins admitted
// callbacks and mutations. Cancellation does not replace an actual return.
func (s *workspaceWriterScope) stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.signal()
}

// drain starts owned closure and bounds only the caller's observation. A timeout
// does not disown active work, spawn a waiter, or authorize storage retirement.
// Any quarantine keeps the root and capacity reachable and returns a failure.
func (s *workspaceWriterScope) drain(ctx context.Context) error {
	if s == nil || ctx == nil {
		return ErrInvalidOptions
	}
	s.stop()
	select {
	case <-s.done:
		return s.snapshot().Failure
	default:
	}
	select {
	case <-s.done:
		return s.snapshot().Failure
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *workspaceWriterScope) closeOwnedWriters() {
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			s.mu.Lock()
			s.stopped = true
			if s.firstErr == nil {
				s.firstErr = errWorkspaceWriterClose
			}
			s.finished = true
			close(s.done)
			s.mu.Unlock()
		}
	}()
	for {
		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if !stopped {
			<-s.changed
			continue
		}
		// Calls are outside the scope lock. The fixed array bounds this scan;
		// no new task can enter after stop and cancel is idempotent.
		for index := range s.entries {
			s.mu.Lock()
			entry := s.entries[index]
			var task *workspaceWriterTaskHandle
			if entry != nil && entry.kind == workspaceWriterTask {
				task = entry.task
			}
			s.mu.Unlock()
			if task != nil {
				_ = task.stop()
			}
		}
		s.mu.Lock()
		var file *workspaceWritableFile
		active, quarantined := false, false
		for _, entry := range s.entries {
			if entry == nil {
				continue
			}
			switch entry.kind {
			case workspaceWriterFile:
				if file == nil {
					file = entry.file
				}
			case workspaceWriterQuarantined:
				quarantined = true
			default:
				active = true
			}
		}
		s.mu.Unlock()
		if file != nil {
			_ = file.Close()
			continue
		}
		if active {
			<-s.changed
			continue
		}
		if !quarantined {
			closeErr := s.root.Close()
			s.mu.Lock()
			s.rootClosed = closeErr == nil
			if closeErr != nil && s.firstErr == nil {
				s.firstErr = closeErr
			}
			s.mu.Unlock()
		}
		s.mu.Lock()
		s.finished = true
		close(s.done)
		s.mu.Unlock()
		returned = true
		return
	}
}

func (s *workspaceWriterScope) snapshot() workspaceWriterSnapshot {
	if s == nil {
		return workspaceWriterSnapshot{AdmissionClosed: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := workspaceWriterSnapshot{Capacity: len(s.entries), AdmissionClosed: s.stopped,
		RootClosed: s.rootClosed, Done: s.finished, Failure: s.firstErr}
	for _, entry := range s.entries {
		if entry == nil {
			continue
		}
		switch entry.kind {
		case workspaceWriterOpening:
			snapshot.Opening++
		case workspaceWriterFile:
			snapshot.Files++
		case workspaceWriterMutation:
			snapshot.Mutations++
		case workspaceWriterTask:
			snapshot.Tasks++
		case workspaceWriterQuarantined:
			snapshot.Quarantined++
		}
	}
	return snapshot
}

// workspaceWritableFile intentionally has no descriptor, File, Root, ReaderFrom
// or SyscallConn accessors. Each operation is serialized with actual Close, so
// a successful close joins writes already in progress and rejects later writes.
type workspaceWritableFile struct {
	mu       sync.Mutex
	scope    *workspaceWriterScope
	entry    *workspaceWriterEntry
	file     *os.File
	closed   bool
	closeErr error
}

func (f *workspaceWritableFile) Write(data []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	return f.file.Write(data)
}

func (f *workspaceWritableFile) WriteAt(data []byte, offset int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	return f.file.WriteAt(data, offset)
}

func (f *workspaceWritableFile) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	return f.file.Seek(offset, whence)
}

func (f *workspaceWritableFile) Truncate(size int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	return f.file.Truncate(size)
}

func (f *workspaceWritableFile) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	return f.file.Sync()
}

func (f *workspaceWritableFile) Close() (err error) {
	if f == nil {
		return os.ErrInvalid
	}
	f.mu.Lock()
	if f.closed {
		err = f.closeErr
		f.mu.Unlock()
		return err
	}
	f.closed = true
	returned := false
	defer func() {
		if !returned {
			_ = recover()
			err = errWorkspaceWriterClose
		}
		f.closeErr = err
		f.mu.Unlock()
		f.scope.finish(f.entry, err != nil, err)
	}()
	err = f.file.Close()
	returned = true
	return err
}

var _ io.WriteCloser = (*workspaceWritableFile)(nil)
var _ io.WriterAt = (*workspaceWritableFile)(nil)
var _ io.Seeker = (*workspaceWritableFile)(nil)
