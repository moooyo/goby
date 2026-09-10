//go:build linux

package backupstore

import (
	"context"
	"time"

	"golang.org/x/sys/unix"
)

func (s *Store) Snapshot(ctx context.Context, id string) (*Snapshot, error) {
	if !hexValue(id, 32) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return nil, err
	}
	i := s.index(id)
	if i < 0 || s.registry.Entries[i].Deleting {
		return nil, ErrNotFound
	}
	rec := s.registry.Entries[i]
	if rec.Phase != "ready" || rec.Metadata.State != StateReady {
		return nil, ErrNotReady
	}
	if len(s.readers) >= MaxReaders {
		return nil, ErrBusy
	}
	f, st, err := s.openFile(basename(id, true), unix.O_RDONLY)
	if err != nil {
		return nil, s.unhealthy(ErrUnavailable)
	}
	if identity(st) != rec.Identity || st.Size != rec.Metadata.Size || stamp(st) != rec.Stamp {
		f.Close()
		return nil, s.unhealthy(ErrIntegrity)
	}
	snapshot := &Snapshot{file: f, ctx: ctx, name: id + ".age", size: rec.Metadata.Size, modTime: time.Unix(int64(st.Mtim.Sec), int64(st.Mtim.Nsec)).UTC(), failure: s.markDegraded}
	snapshot.release = func(reader *Snapshot) { s.mu.Lock(); delete(s.readers, reader); s.mu.Unlock() }
	s.readers[snapshot] = id
	snapshot.mu.Lock()
	snapshot.stop = context.AfterFunc(ctx, func() { _ = snapshot.Close() })
	snapshot.mu.Unlock()
	return snapshot, nil
}

// Verify records a caller-supplied inspection summary only after rehashing the
// same ready object. Wrong CAS input never poisons a healthy object or store.
func (s *Store) Verify(ctx context.Context, id, expectedDigest string, summary SourceSummary) (Metadata, error) {
	if !hexValue(id, 32) || !hexValue(expectedDigest, 64) || !validSummary(&summary) {
		return Metadata{}, ErrInvalid
	}
	metadata, err := s.Get(ctx, id)
	if err != nil {
		return Metadata{}, err
	}
	if metadata.Digest != expectedDigest {
		return Metadata{}, ErrConflict
	}
	snapshot, err := s.Snapshot(ctx, id)
	if err != nil {
		return Metadata{}, err
	}
	defer snapshot.Close()
	digest, err := hashFile(ctx, snapshot.file, snapshot.size)
	if err != nil {
		return Metadata{}, err
	}
	if digest != expectedDigest {
		s.markDegraded()
		return Metadata{}, ErrIntegrity
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Metadata{}, err
	}
	i := s.index(id)
	if i < 0 {
		return Metadata{}, ErrNotFound
	}
	rec := s.registry.Entries[i]
	if rec.Metadata.Digest != expectedDigest {
		return Metadata{}, ErrConflict
	}
	if err := s.checkObject(rec); err != nil {
		return Metadata{}, s.unhealthy(err)
	}
	rec.Metadata.Summary = &summary
	rec.Metadata.Verified = true
	rec.Metadata.UpdatedAt = s.now().UTC()
	rec.Metadata = copyMetadata(rec.Metadata)
	next := s.clone()
	next.Entries[i] = rec
	if err := s.persist(next); err != nil {
		return Metadata{}, s.unhealthy(err)
	}
	return copyMetadata(rec.Metadata), nil
}
