package backupstore

import (
	"errors"
	"os"
	"sync"
)

// Scratch owns one anonymous plaintext file and its complete quota reservation.
// It is never linked into the store directory, cataloged, or downloadable.
type Scratch struct {
	mu        sync.Mutex
	file      *os.File
	closed    bool
	closeDone chan struct{}
	closeErr  error
	stop      func() bool
	release   func(*Scratch)
	failure   func()
}

// File returns the same Go file object throughout the scratch lifetime. After
// Close, operations on that object return os.ErrClosed; no raw descriptor is
// cached or reconstructed. The trusted caller must enforce the reserved byte
// limit before writing and must not truncate, punch holes, or link the file.
// Write it once, then seek/read/hash it as necessary. An owned child may inherit
// a read descriptor, but the coordinator must stop and reap that child before
// explicitly closing the scratch; retained duplicates are otherwise forbidden.
// The raw file itself does not enforce the byte limit. Use Scratch.Close to
// release the file and quota together, rather than closing File independently.
func (s *Scratch) File() *os.File { return s.file }

// Close completes only after this process's descriptor is closed and its quota
// reservation is released. It cannot revoke an inherited child descriptor.
// Concurrent callers observe the same completed close result.
func (s *Scratch) Close() error {
	s.mu.Lock()
	if s.closed {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closed = true
	s.closeDone = make(chan struct{})
	if s.stop != nil {
		s.stop()
	}
	err := s.file.Close()
	s.mu.Unlock()
	if s.release != nil {
		s.release(s)
	}
	if errors.Is(err, os.ErrClosed) {
		// An accidentally closed raw Go file cannot refer to a subsequently
		// reused descriptor. Its reservation still belongs to this handle.
		err = nil
	}
	if err != nil {
		err = ErrUnavailable
		if s.failure != nil {
			s.failure()
		}
	}
	s.mu.Lock()
	s.closeErr = err
	close(s.closeDone)
	s.mu.Unlock()
	return err
}
