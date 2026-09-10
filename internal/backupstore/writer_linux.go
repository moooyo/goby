//go:build linux

package backupstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

const transferChunk = 256 << 10

// Writer owns one unlisted byte stream. Its job metadata is listed immediately,
// but bytes become downloadable only after an explicit successful Publish.
type Writer struct {
	mu       sync.Mutex
	store    *Store
	ctx      context.Context
	file     *os.File
	id       string
	hash     hash.Hash
	prepared bool
	proof    Prepared
	sticky   error
	closed   bool
	closeErr error
	stop     func() bool
}

func (s *Store) Begin(ctx context.Context, options BeginOptions) (*Writer, error) {
	if options.Kind != KindGenerated && options.Kind != KindImported || !identifier(options.CreatorID, 256, true) || !identifier(options.SessionID, 256, true) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return nil, err
	}
	if len(s.registry.Entries) >= s.cfg.MaxObjects {
		return nil, ErrQuota
	}
	if err := s.space(0); err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	if s.index(id) >= 0 {
		return nil, ErrUnavailable
	}
	f, fileIdentity, err := s.anonymousFile()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	rec := record{Metadata: Metadata{ID: id, Kind: options.Kind, State: StateWriting, CreatedAt: now, UpdatedAt: now, CreatorID: options.CreatorID, SessionID: options.SessionID}, Identity: fileIdentity, Phase: "creating"}
	next := s.clone()
	next.Entries = append(next.Entries, rec)
	if err := s.persist(next); err != nil {
		f.Close()
		return nil, s.unhealthy(err)
	}
	if err := s.publishAnonymous(f, basename(id, false)); err != nil {
		f.Close()
		return nil, s.unhealthy(err)
	}
	next = s.clone()
	next.Entries[len(next.Entries)-1].Phase = "writing"
	if err := s.persist(next); err != nil {
		f.Close()
		return nil, s.unhealthy(err)
	}
	w := &Writer{store: s, ctx: ctx, file: f, id: id, hash: sha256.New()}
	s.writers[id] = w
	w.mu.Lock()
	w.stop = context.AfterFunc(ctx, func() { _ = w.stopWith(CodeCancelled) })
	w.mu.Unlock()
	return w, nil
}

func (w *Writer) Metadata() Metadata {
	w.store.mu.Lock()
	defer w.store.mu.Unlock()
	i := w.store.index(w.id)
	if i < 0 {
		return Metadata{ID: w.id}
	}
	return copyMetadata(w.store.registry.Entries[i].Metadata)
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sticky != nil {
		return 0, w.sticky
	}
	if w.closed {
		return 0, ErrUnavailable
	}
	if w.prepared {
		return 0, ErrConflict
	}
	written := 0
	for len(p) > 0 {
		if err := w.ctx.Err(); err != nil {
			w.sticky = err
			return written, err
		}
		chunk := p
		if len(chunk) > transferChunk {
			chunk = chunk[:transferChunk]
		}
		s := w.store
		s.mu.Lock()
		if err := s.healthy(w.ctx); err != nil {
			s.mu.Unlock()
			w.sticky = err
			return written, err
		}
		i := s.index(w.id)
		if i < 0 {
			s.mu.Unlock()
			w.sticky = ErrUnavailable
			return written, w.sticky
		}
		rec := &s.registry.Entries[i]
		if int64(len(chunk)) > s.cfg.MaxObjectBytes-rec.Metadata.Size {
			s.mu.Unlock()
			w.sticky = ErrQuota
			return written, w.sticky
		}
		if err := s.space(int64(len(chunk))); err != nil {
			s.mu.Unlock()
			w.sticky = err
			return written, err
		}
		var st unix.Stat_t
		if unix.Fstat(int(w.file.Fd()), &st) != nil || !ownedFile(st) || identity(st) != rec.Identity || st.Size != rec.Metadata.Size {
			s.degraded = true
			s.mu.Unlock()
			w.sticky = ErrIntegrity
			return written, w.sticky
		}
		n, err := w.file.Write(chunk)
		if n > 0 && n <= len(chunk) {
			rec.Metadata.Size += int64(n)
			rec.Metadata.UpdatedAt = s.now().UTC()
			_, _ = w.hash.Write(chunk[:n])
			written += n
			p = p[n:]
		}
		s.mu.Unlock()
		if err != nil || n != len(chunk) {
			w.sticky = ErrUnavailable
			return written, w.sticky
		}
	}
	return written, nil
}

func hashFile(ctx context.Context, f *os.File, size int64) (string, error) {
	hasher := sha256.New()
	buffer := make([]byte, transferChunk)
	for offset := int64(0); offset < size; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		chunk := buffer
		if int64(len(chunk)) > size-offset {
			chunk = chunk[:size-offset]
		}
		n, err := f.ReadAt(chunk, offset)
		if n > 0 {
			_, _ = hasher.Write(chunk[:n])
			offset += int64(n)
		}
		if err != nil && !(errors.Is(err, io.EOF) && offset == size) || n != len(chunk) {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", ErrIntegrity
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (w *Writer) Prepare(ctx context.Context) (Prepared, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sticky != nil {
		return Prepared{}, w.sticky
	}
	if w.closed {
		return Prepared{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	if err := w.ctx.Err(); err != nil {
		return Prepared{}, err
	}
	if w.prepared {
		return w.proof, nil
	}
	s := w.store
	s.mu.Lock()
	if err := s.healthy(ctx); err != nil {
		s.mu.Unlock()
		return Prepared{}, err
	}
	i := s.index(w.id)
	if i < 0 {
		s.mu.Unlock()
		return Prepared{}, ErrUnavailable
	}
	rec := s.registry.Entries[i]
	s.mu.Unlock()
	if rec.Metadata.Size < 1 {
		return Prepared{}, ErrInvalid
	}
	if s.syncFile(w.file) != nil {
		w.sticky = ErrUnavailable
		return Prepared{}, w.sticky
	}
	var before, after unix.Stat_t
	if unix.Fstat(int(w.file.Fd()), &before) != nil || !ownedFile(before) || identity(before) != rec.Identity || before.Size != rec.Metadata.Size {
		w.sticky = ErrIntegrity
		return Prepared{}, w.sticky
	}
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(w.ctx, cancel)
	defer func() { stop(); cancel() }()
	digest, err := hashFile(combined, w.file, before.Size)
	if err != nil {
		return Prepared{}, err
	}
	if digest != hex.EncodeToString(w.hash.Sum(nil)) || unix.Fstat(int(w.file.Fd()), &after) != nil || identity(after) != rec.Identity || after.Size != before.Size || stamp(after) != stamp(before) {
		w.sticky = ErrIntegrity
		return Prepared{}, w.sticky
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(combined); err != nil {
		return Prepared{}, err
	}
	i = s.index(w.id)
	next := s.clone()
	rec = next.Entries[i]
	rec.Metadata.Size = before.Size
	rec.Metadata.Digest = digest
	rec.Metadata.UpdatedAt = s.now().UTC()
	rec.Phase = "prepared"
	rec.Stamp = stamp(before)
	next.Entries[i] = rec
	if err := s.persist(next); err != nil {
		return Prepared{}, s.unhealthy(err)
	}
	w.prepared = true
	w.proof = Prepared{ID: w.id, Size: before.Size, Digest: digest, writer: w}
	return w.proof, nil
}

// Publish must be called only after the coordinator has committed its external
// audit transaction. Summary is trusted only after full archive inspection.
func (w *Writer) Publish(ctx context.Context, proof Prepared, summary *SourceSummary) (Metadata, error) {
	if !validSummary(summary) {
		return Metadata{}, ErrInvalid
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return Metadata{}, ErrUnavailable
	}
	if !w.prepared || proof.writer != w || proof.ID != w.proof.ID || proof.Size != w.proof.Size || proof.Digest != w.proof.Digest {
		return Metadata{}, ErrConflict
	}
	if err := w.ctx.Err(); err != nil {
		return Metadata{}, err
	}
	s := w.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Metadata{}, err
	}
	i := s.index(w.id)
	if i < 0 {
		return Metadata{}, ErrNotFound
	}
	rec := s.registry.Entries[i]
	if rec.Phase != "prepared" || rec.Metadata.Digest != proof.Digest || rec.Metadata.Size != proof.Size {
		return Metadata{}, ErrConflict
	}
	if err := s.checkObject(rec); err != nil {
		return Metadata{}, s.unhealthy(err)
	}
	rec.Phase = "publishing"
	next := s.clone()
	next.Entries[i] = rec
	if err := s.persist(next); err != nil {
		return Metadata{}, s.unhealthy(err)
	}
	if !rec.FinalName {
		if unix.Renameat2(int(s.directory.Fd()), basename(w.id, false), int(s.directory.Fd()), basename(w.id, true), unix.RENAME_NOREPLACE) != nil {
			return Metadata{}, s.unhealthy(ErrUnavailable)
		}
		rec.FinalName = true
		if s.syncFile(s.directory) != nil {
			return Metadata{}, s.unhealthy(ErrUnavailable)
		}
	}
	rec.Phase = "ready"
	rec.Metadata.State = StateReady
	rec.Metadata.ErrorCode = CodeNone
	rec.Metadata.Verified = summary != nil
	rec.Metadata.Summary = summary
	rec.Metadata = copyMetadata(rec.Metadata)
	rec.Metadata.UpdatedAt = s.now().UTC()
	next = s.clone()
	next.Entries[i] = rec
	if err := s.persist(next); err != nil {
		return Metadata{}, s.unhealthy(err)
	}
	w.closed = true
	if w.stop != nil {
		w.stop()
	}
	delete(s.writers, w.id)
	if err := w.file.Close(); err != nil {
		w.closeErr = ErrUnavailable
		return Metadata{}, s.unhealthy(ErrUnavailable)
	}
	return copyMetadata(rec.Metadata), nil
}

func (s *Store) checkObject(rec record) error {
	f, st, err := s.openFile(basename(rec.Metadata.ID, rec.FinalName), unix.O_RDONLY)
	if err != nil {
		return ErrUnavailable
	}
	defer f.Close()
	if identity(st) != rec.Identity || st.Size != rec.Metadata.Size || stamp(st) != rec.Stamp {
		return ErrIntegrity
	}
	return nil
}

// ReopenPrepared is a storage recovery primitive, not authorization to publish.
// The coordinator must prove its external audit/intent before calling Publish.
func (s *Store) ReopenPrepared(ctx context.Context, id, expectedDigest string) (*Writer, error) {
	if !hexValue(id, 32) || !hexValue(expectedDigest, 64) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	if err := s.healthy(ctx); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	i := s.index(id)
	if i < 0 {
		s.mu.Unlock()
		return nil, ErrNotFound
	}
	rec := s.registry.Entries[i]
	if rec.Metadata.Digest != expectedDigest {
		s.mu.Unlock()
		return nil, ErrConflict
	}
	if rec.Phase != "prepared" || rec.Deleting {
		s.mu.Unlock()
		return nil, ErrNotReady
	}
	if s.writers[id] != nil {
		s.mu.Unlock()
		return nil, ErrBusy
	}
	if err := s.checkObject(rec); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	f, _, err := s.openFile(basename(id, rec.FinalName), unix.O_RDONLY)
	if err != nil {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	w := &Writer{store: s, ctx: ctx, file: f, id: id, prepared: true}
	w.proof = Prepared{ID: id, Size: rec.Metadata.Size, Digest: rec.Metadata.Digest, writer: w}
	// Registration prevents deletion while the complete object is rehashed.
	s.writers[id] = w
	s.mu.Unlock()
	w.mu.Lock()
	digest, hashErr := hashFile(ctx, f, rec.Metadata.Size)
	s.mu.Lock()
	if hashErr == nil && digest != rec.Metadata.Digest {
		hashErr = ErrIntegrity
	}
	if hashErr == nil {
		hashErr = s.healthy(ctx)
	}
	if hashErr == nil {
		hashErr = s.checkObject(rec)
	}
	if hashErr != nil {
		delete(s.writers, id)
		f.Close()
		w.closed = true
		s.mu.Unlock()
		w.mu.Unlock()
		return nil, hashErr
	}
	w.stop = context.AfterFunc(ctx, func() { _ = w.stopWith(CodeCancelled) })
	s.mu.Unlock()
	w.mu.Unlock()
	return w, nil
}

func (w *Writer) Abort(ctx context.Context, code ErrorCode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if code == CodeNone || !validCode(code) {
		return ErrInvalid
	}
	return w.stopWith(code)
}

func (w *Writer) Close() error { return w.stopWith(CodeCancelled) }

func (w *Writer) stopWith(code ErrorCode) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.closeErr
	}
	w.closed = true
	if w.stop != nil {
		w.stop()
	}
	if w.file.Close() != nil {
		w.closeErr = ErrUnavailable
	}
	s := w.store
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.writers, w.id)
	// A possible catalog commit must be recovered on reopen; guessing here
	// could destroy a prepared object whose external audit already committed.
	if s.degraded {
		if w.closeErr == nil {
			w.closeErr = ErrUnavailable
		}
		return w.closeErr
	}
	if s.checkIdentity(markerName, s.markerID) != nil || s.checkIdentity(lockName, s.lockID) != nil || s.checkIdentity(catalogName, s.catalogID) != nil || s.checkInventory() != nil {
		w.closeErr = s.unhealthy(ErrUnavailable)
		return w.closeErr
	}
	i := s.index(w.id)
	if i < 0 {
		w.closeErr = ErrUnavailable
		return w.closeErr
	}
	rec := s.registry.Entries[i]
	if rec.Phase == "ready" {
		return w.closeErr
	}
	if rec.Phase == "creating" || rec.Phase == "writing" {
		if err := s.unlinkExact(basename(w.id, rec.FinalName), rec.Identity, true); err != nil {
			w.closeErr = s.unhealthy(err)
			return w.closeErr
		}
		if s.syncFile(s.directory) != nil {
			w.closeErr = s.unhealthy(ErrUnavailable)
			return w.closeErr
		}
		rec.Phase = "empty"
		rec.Identity = fileID{}
		rec.Stamp = fileStamp{}
		rec.Metadata.Size = 0
		rec.Metadata.Digest = ""
	}
	rec.Metadata.State = StateFailed
	if code == CodeCancelled {
		rec.Metadata.State = StateCancelled
	} else if code == CodeInterrupted {
		rec.Metadata.State = StateInterrupted
	}
	rec.Metadata.ErrorCode = code
	rec.Metadata.UpdatedAt = s.now().UTC()
	next := s.clone()
	next.Entries[i] = rec
	if err := s.persist(next); err != nil {
		w.closeErr = s.unhealthy(err)
	}
	return w.closeErr
}
