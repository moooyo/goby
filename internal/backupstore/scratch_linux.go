//go:build linux

package backupstore

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

type scratchReservation struct {
	maximum  int64
	identity fileID
}

// Scratch reserves maximum bytes and creates an unlinked temporary file under
// the private store root. The caller must constrain every write to maximum:
// File exposes an ordinary *os.File and cannot enforce a write limit itself.
// Allocation must only grow until Close; concurrent truncation or hole punching
// would invalidate the free-space admission calculation. Scratch reservations
// share MaxTotalBytes with objects but do not consume durable object/job slots.
func (s *Store) Scratch(ctx context.Context, maximum int64) (*Scratch, error) {
	if maximum < 1 || maximum > s.cfg.MaxObjectBytes {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return nil, err
	}
	if len(s.scratchFiles) >= MaxScratchFiles {
		return nil, ErrBusy
	}
	if err := s.space(maximum); err != nil {
		return nil, err
	}
	file, id, err := s.anonymousFileWithFlags(unix.O_EXCL)
	if err != nil {
		return nil, err
	}
	scratch := &Scratch{file: file, failure: s.markDegraded}
	scratch.release = func(value *Scratch) {
		s.mu.Lock()
		if reservation, exists := s.scratchFiles[value]; exists {
			s.scratchBytes -= reservation.maximum
			delete(s.scratchFiles, value)
		}
		s.mu.Unlock()
	}
	s.scratchFiles[scratch] = scratchReservation{maximum: maximum, identity: id}
	s.scratchBytes += maximum
	scratch.mu.Lock()
	scratch.stop = context.AfterFunc(ctx, func() { _ = scratch.Close() })
	scratch.mu.Unlock()
	return scratch, nil
}

// scratchRemaining samples allocations before statfs is sampled by space. With
// monotonically growing allocations, a concurrent write makes the resulting
// reservation conservative rather than allowing already-promised bytes to be
// promised again. Physical blocks already consumed are reflected by statfs and
// must not be deducted a second time. Sparse/unallocated regions remain fully
// reserved. A closing file retains its full reservation until release completes.
func (s *Store) scratchRemaining() (int64, error) {
	var remaining int64
	for scratch, reservation := range s.scratchFiles {
		allocated, err := scratchAllocated(scratch.file, reservation)
		if errors.Is(err, os.ErrClosed) {
			allocated, err = 0, nil
		}
		if err != nil {
			return 0, err
		}
		if allocated > reservation.maximum {
			allocated = reservation.maximum
		}
		remaining += reservation.maximum - allocated
	}
	return remaining, nil
}

func scratchAllocated(file *os.File, reservation scratchReservation) (int64, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		if errors.Is(err, os.ErrClosed) || scratchFileClosed(file) {
			return 0, os.ErrClosed
		}
		return 0, ErrUnavailable
	}
	var stat unix.Stat_t
	var statErr error
	// RawConn.Control pins the descriptor throughout fstat. Calling Fd then
	// fstat directly could inspect a reused descriptor during concurrent Close.
	if err := raw.Control(func(fd uintptr) { statErr = unix.Fstat(int(fd), &stat) }); err != nil {
		if errors.Is(err, os.ErrClosed) || scratchFileClosed(file) {
			return 0, os.ErrClosed
		}
		return 0, ErrUnavailable
	}
	if statErr != nil {
		return 0, ErrUnavailable
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&07777 != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 0 || identity(stat) != reservation.identity || stat.Size < 0 || stat.Size > reservation.maximum || stat.Blocks < 0 {
		return 0, ErrIntegrity
	}
	// Only the portion within the finite reservation can reduce future demand;
	// compare before multiplying the filesystem's signed block count.
	if uint64(stat.Blocks) >= (uint64(reservation.maximum)+511)/512 {
		return reservation.maximum, nil
	}
	return int64(stat.Blocks) * 512, nil
}

func scratchFileClosed(file *os.File) bool {
	// RawConn.Control may return internal/poll's closed-file sentinel directly,
	// rather than os.ErrClosed. Stat supplies the public classification using
	// the same Go file object and its descriptor lifetime protection.
	_, err := file.Stat()
	return errors.Is(err, os.ErrClosed)
}
