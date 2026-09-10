package backupstore

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// Snapshot is a pinned immutable-size descriptor. Its context closes the file;
// HTTP code is separately responsible for deadlines on a blocked response body.
type Snapshot struct {
	mu        sync.Mutex
	file      *os.File
	ctx       context.Context
	name      string
	size      int64
	modTime   time.Time
	offset    int64
	closed    bool
	closeDone chan struct{}
	closeErr  error
	stop      func() bool
	release   func(*Snapshot)
	failure   func()
}

func (s *Snapshot) Name() string       { return s.name }
func (s *Snapshot) Size() int64        { return s.size }
func (s *Snapshot) ModTime() time.Time { return s.modTime }

func (s *Snapshot) Read(p []byte) (int, error) {
	s.mu.Lock()
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return 0, err
	}
	if s.closed {
		s.mu.Unlock()
		return 0, ErrUnavailable
	}
	if len(p) == 0 {
		s.mu.Unlock()
		return 0, nil
	}
	if s.offset >= s.size {
		s.mu.Unlock()
		return 0, io.EOF
	}
	if int64(len(p)) > s.size-s.offset {
		p = p[:s.size-s.offset]
	}
	n, err := s.file.ReadAt(p, s.offset)
	s.offset += int64(n)
	if errors.Is(err, io.EOF) && s.offset == s.size {
		err = nil
	}
	s.mu.Unlock()
	if err != nil {
		if s.ctx.Err() != nil {
			return n, s.ctx.Err()
		}
		if s.failure != nil {
			s.failure()
		}
		return n, ErrIntegrity
	}
	return n, nil
}

func (s *Snapshot) Seek(offset int64, whence int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if s.closed {
		return 0, ErrUnavailable
	}
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		if offset > s.size-s.offset || offset < -s.offset {
			return 0, ErrInvalid
		}
		next = s.offset + offset
	case io.SeekEnd:
		if offset > 0 || offset < -s.size {
			return 0, ErrInvalid
		}
		next = s.size + offset
	default:
		return 0, ErrInvalid
	}
	if next < 0 || next > s.size {
		return 0, ErrInvalid
	}
	s.offset = next
	return next, nil
}

func (s *Snapshot) Close() error {
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
