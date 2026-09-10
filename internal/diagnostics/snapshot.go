package diagnostics

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"time"
)

// Snapshot is a descriptor pinned to a registered file with a fixed byte limit.
// Close releases a reader slot. A store shutdown also closes its snapshots.
type Snapshot struct {
	mu        sync.Mutex
	file      *os.File
	ctx       context.Context
	name      string
	size      int64
	modTime   time.Time
	offset    int64
	release   func(*Snapshot)
	failure   func()
	stop      func() bool
	closed    bool
	closeDone chan struct{}
	closeErr  error
}

func (s *Snapshot) Name() string       { return s.name }
func (s *Snapshot) Size() int64        { return s.size }
func (s *Snapshot) ModTime() time.Time { return s.modTime }

func (s *Snapshot) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return 0, err
	}
	if s.closed {
		return 0, ErrUnavailable
	}
	if len(p) == 0 {
		return 0, nil
	}
	if s.offset >= s.size {
		return 0, io.EOF
	}
	if int64(len(p)) > s.size-s.offset {
		p = p[:s.size-s.offset]
	}
	n, err := s.file.ReadAt(p, s.offset)
	s.offset += int64(n)
	if err != nil {
		// Registered files are append-only while active, and immutable afterward.
		// A short read before the captured boundary means the store is unhealthy.
		if errors.Is(err, io.EOF) && s.offset == s.size {
			return n, nil
		}
		if s.failure != nil {
			s.failure()
		}
		return n, ErrUnavailable
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
		if s.failure != nil {
			s.failure()
		}
		err = ErrUnavailable
	}
	s.mu.Lock()
	s.closeErr = err
	// Completion includes releasing the store's reader slot. Concurrent Close
	// calls must not return while cancellation is still holding that slot.
	close(s.closeDone)
	s.mu.Unlock()
	return err
}

// Lines returns a bounded page, scanning at most MaxReadBytes and retaining at
// most 500 records in memory. Cancellation is checked between individual lines.
func (s *Store) Lines(ctx context.Context, name string, options LinesOptions) (LinesPage, error) {
	page := LinesPage{Items: []string{}, StartIndex: options.StartIndex, NextIndex: options.StartIndex}
	if options.StartIndex < 0 || options.Limit < 1 || options.Limit > 500 {
		return page, ErrInvalid
	}
	snapshot, err := s.Snapshot(ctx, name)
	if err != nil {
		return page, err
	}
	defer snapshot.Close()
	page.SnapshotSize = snapshot.Size()
	if page.SnapshotSize > MaxReadBytes {
		return LinesPage{}, ErrUnavailable
	}
	reader := bufio.NewReaderSize(snapshot, MaxRecordBytes+1)
	for {
		if err := ctx.Err(); err != nil {
			return LinesPage{}, err
		}
		line, readErr := reader.ReadSlice('\n')
		if len(line) != 0 {
			if len(line) > MaxRecordBytes || line[len(line)-1] != '\n' {
				s.markDegraded()
				return LinesPage{}, ErrUnavailable
			}
			index := page.TotalRecordCount
			page.TotalRecordCount++
			if index >= options.StartIndex && len(page.Items) < options.Limit {
				page.Items = append(page.Items, string(line[:len(line)-1]))
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			if ctx.Err() != nil {
				return LinesPage{}, ctx.Err()
			}
			s.markDegraded()
			return LinesPage{}, ErrUnavailable
		}
	}
	page.NextIndex = options.StartIndex + len(page.Items)
	if err := snapshot.Close(); err != nil {
		return LinesPage{}, err
	}
	return page, nil
}
