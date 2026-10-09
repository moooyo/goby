//go:build linux

package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"syscall"
)

const (
	maxQueuedRecords = 64
	maxQueuedBytes   = maxQueuedRecords * MaxRecordBytes
	maxBatchRecords  = 32
	maxBatchBytes    = 128 << 10
)

type diagnosticWrite struct {
	ctx       context.Context
	line      []byte
	done      chan error
	started   bool
	completed bool
}

// Only the writer completes requests. Keeping completion explicit also lets
// panic cleanup fail the unfinished suffix without changing an earlier result.
func (request *diagnosticWrite) complete(err error) {
	if !request.completed {
		request.completed = true
		request.done <- err
	}
}

func (s *Store) startWriter() {
	s.queueChanged = make(chan struct{})
	s.writeStop = make(chan struct{})
	s.writerDone = make(chan struct{})
	go s.runWriter()
}

func (s *Store) stoppingWrites() bool {
	select {
	case <-s.writeStop:
		return true
	default:
		return false
	}
}

func (s *Store) notifyWriterLocked() {
	close(s.queueChanged)
	s.queueChanged = make(chan struct{})
}

func (s *Store) stopWriter() {
	s.writerMu.Lock()
	if s.writerDone == nil {
		s.writerMu.Unlock()
		return
	}
	if !s.stoppingWrites() {
		close(s.writeStop)
		s.notifyWriterLocked()
	}
	done := s.writerDone
	s.writerMu.Unlock()
	// The writer needs Store.mu to drain accepted records. Never wait while
	// holding that mutex or the admission mutex.
	<-done
}

func (s *Store) appendRecord(ctx context.Context, line []byte) error {
	if len(line) == 0 || len(line) > MaxRecordBytes || line[len(line)-1] != '\n' || bytes.Count(line, []byte{'\n'}) != 1 || !json.Valid(line) {
		return ErrInvalid
	}
	for {
		s.writerMu.Lock()
		if err := ctx.Err(); err != nil {
			s.writerMu.Unlock()
			return err
		}
		if s.writerDone == nil || s.stoppingWrites() {
			s.writerMu.Unlock()
			return ErrUnavailable
		}
		if len(s.writeQueue) < maxQueuedRecords && s.queuedBytes+len(line) <= maxQueuedBytes {
			request := &diagnosticWrite{ctx: ctx, line: bytes.Clone(line), done: make(chan error, 1)}
			s.writeQueue = append(s.writeQueue, request)
			s.queuedBytes += len(line)
			s.notifyWriterLocked()
			s.writerMu.Unlock()
			// Accepted callers wait for the actual write outcome. Cancellation is
			// checked before writing, but cannot retract a write already started.
			return <-request.done
		}
		changed := s.queueChanged
		s.writerMu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Store) nextWriteBatch() []*diagnosticWrite {
	for {
		s.writerMu.Lock()
		if len(s.writeQueue) != 0 {
			count, size := 0, 0
			for _, request := range s.writeQueue {
				if count == maxBatchRecords || size+len(request.line) > maxBatchBytes {
					break
				}
				count++
				size += len(request.line)
			}
			batch := append([]*diagnosticWrite(nil), s.writeQueue[:count]...)
			copy(s.writeQueue, s.writeQueue[count:])
			clear(s.writeQueue[len(s.writeQueue)-count:])
			s.writeQueue = s.writeQueue[:len(s.writeQueue)-count]
			s.queuedBytes -= size
			s.notifyWriterLocked()
			s.writerMu.Unlock()
			return batch
		}
		if s.stoppingWrites() {
			s.writerMu.Unlock()
			return nil
		}
		changed := s.queueChanged
		s.writerMu.Unlock()
		<-changed
	}
}

func completeDiagnosticWrites(requests []*diagnosticWrite, err error) {
	for _, request := range requests {
		request.complete(err)
	}
}

func failDiagnosticWrites(requests []*diagnosticWrite) {
	for _, request := range requests {
		err := ErrUnavailable
		if !request.started && request.ctx.Err() != nil {
			err = request.ctx.Err()
		}
		request.complete(err)
	}
}

func (s *Store) runWriter() {
	var batch []*diagnosticWrite
	defer func() {
		if recover() != nil {
			// A failed writer must not strand synchronous callers or let Close
			// release the process lock while accepted work remains unaccounted for.
			s.markDegraded()
			s.writerMu.Lock()
			if !s.stoppingWrites() {
				close(s.writeStop)
			}
			failDiagnosticWrites(batch)
			failDiagnosticWrites(s.writeQueue)
			s.writeQueue = nil
			s.queuedBytes = 0
			s.notifyWriterLocked()
			s.writerMu.Unlock()
		}
		close(s.writerDone)
	}()
	for {
		batch = s.nextWriteBatch()
		if len(batch) == 0 {
			return
		}
		s.writeBatch(batch)
		batch = nil
	}
}

func (s *Store) writeBatch(batch []*diagnosticWrite) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var pending []*diagnosticWrite
	fail := func() {
		s.degraded = true
		failDiagnosticWrites(batch)
	}
	for _, request := range batch {
		request.started = true
		if err := s.healthyWriteLocked(request.ctx); err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				request.complete(err)
				continue
			}
			fail()
			return
		}
		now := s.now().UTC()
		if s.active == nil || s.activeSize+int64(len(request.line)) > s.cfg.MaxFileBytes || s.activeDay != now.Format("2006-01-02") {
			// finishActiveLocked syncs this file before closing it and publishing
			// its final registry size. No later-file failure can undo these acks.
			if err := s.finishActiveLocked(); err != nil {
				fail()
				return
			}
			completeDiagnosticWrites(pending, nil)
			pending = pending[:0]
			if err := s.pruneLocked(now, true); err != nil {
				fail()
				return
			}
			if err := s.createActiveLocked(now); err != nil {
				fail()
				return
			}
		}
		if err := s.pruneLocked(now, false); err != nil {
			fail()
			return
		}
		if err := s.ensureSpaceLocked(int64(len(request.line))); err != nil {
			fail()
			return
		}
		var current *entry
		for index := range s.registry.Files {
			if s.registry.Files[index].Name == s.activeName {
				current = &s.registry.Files[index]
				break
			}
		}
		if current == nil {
			fail()
			return
		}
		check, stat, err := s.registeredFile(*current, syscall.O_RDONLY)
		if err != nil {
			fail()
			return
		}
		if err := check.Close(); err != nil || stat.Size != s.activeSize {
			fail()
			return
		}
		before := s.activeSize
		if err := s.writeAll(s.active, request.line); err != nil {
			// Only the current incomplete line is rolled back. Earlier batches
			// and files may already be acknowledged and must never be truncated.
			_ = s.active.Truncate(before)
			_ = s.syncFile(s.active)
			fail()
			return
		}
		s.activeSize += int64(len(request.line))
		pending = append(pending, request)
	}
	if len(pending) != 0 {
		if err := s.syncFile(s.active); err != nil {
			fail()
			return
		}
		completeDiagnosticWrites(pending, nil)
	}
}
