package timeshift

import (
	"context"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// ReadHandle implements the bounded reader surface needed by ServeContent.
// It deliberately exposes no File, Fd, or duplication operation: Close,
// revocation and the absolute read timeout retain control of the actual inode.
type ReadHandle struct {
	file        *os.File
	store       *Store
	window      *windowState
	artifact    *artifactState
	closed      atomic.Bool
	done        chan struct{}
	timer       *time.Timer
	stopContext func() bool
}

var _ io.ReadSeekCloser = (*ReadHandle)(nil)
var _ io.ReaderAt = (*ReadHandle)(nil)

func (handle *ReadHandle) Read(data []byte) (int, error) {
	if handle.closed.Load() {
		return 0, ErrClosed
	}
	return handle.file.Read(data)
}
func (handle *ReadHandle) ReadAt(data []byte, offset int64) (int, error) {
	if handle.closed.Load() {
		return 0, ErrClosed
	}
	return handle.file.ReadAt(data, offset)
}
func (handle *ReadHandle) Seek(offset int64, whence int) (int64, error) {
	if handle.closed.Load() {
		return 0, ErrClosed
	}
	return handle.file.Seek(offset, whence)
}
func (handle *ReadHandle) Stat() (os.FileInfo, error) {
	if handle.closed.Load() {
		return nil, ErrClosed
	}
	return handle.file.Stat()
}
func (handle *ReadHandle) Done() <-chan struct{} { return handle.done }

func (handle *ReadHandle) Close() error {
	handle.store.mu.Lock()
	defer handle.store.mu.Unlock()
	return handle.store.closeReaderLocked(handle)
}

func (store *Store) closeReaderLocked(handle *ReadHandle) error {
	if !handle.closed.CompareAndSwap(false, true) {
		return nil
	}
	if handle.timer != nil {
		handle.timer.Stop()
	}
	if handle.stopContext != nil {
		handle.stopContext()
	}
	err := handle.file.Close()
	delete(handle.window.readers, handle)
	handle.artifact.readers--
	store.readers--
	close(handle.done)
	store.removeExpiredLocked(handle.window, handle.artifact)
	store.destroyClosedLocked(handle.window)
	return err
}

func (store *Store) OpenArtifact(ctx context.Context, scope Scope, id, artifactID string) (*ReadHandle, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	window, err := store.checkedWindow(ctx, scope, id, true)
	if err != nil {
		return nil, err
	}
	artifact := window.artifacts[artifactID]
	if artifact == nil || !artifact.visible && !store.options.Now().Before(artifact.graceUntil) {
		return nil, ErrNotFound
	}
	if store.readers >= store.options.MaxReaders || len(window.readers) >= store.options.MaxWindowReaders {
		return nil, ErrBusy
	}
	file, err := window.storage.open(artifactID)
	if err != nil {
		return nil, ErrStorage
	}
	info, err := file.Stat()
	if err != nil || info.Size() != artifact.artifact.Size {
		_ = file.Close()
		return nil, ErrStorage
	}
	if err := ctx.Err(); err != nil {
		_ = file.Close()
		return nil, err
	}
	handle := &ReadHandle{file: file, store: store, window: window, artifact: artifact, done: make(chan struct{})}
	artifact.readers++
	store.readers++
	window.readers[handle] = struct{}{}
	handle.timer = time.AfterFunc(store.options.ReadTimeout, func() { _ = handle.Close() })
	handle.stopContext = context.AfterFunc(ctx, func() { _ = handle.Close() })
	return handle, nil
}
