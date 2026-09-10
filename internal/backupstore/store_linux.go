//go:build linux

package backupstore

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

type Store struct {
	mu                          sync.Mutex
	cfg                         Config
	directory                   *os.File
	lock                        *os.File
	markerID, lockID, catalogID fileID
	ownership                   marker
	registry                    catalog
	writers                     map[string]*Writer
	readers                     map[*Snapshot]string
	scratchFiles                map[*Scratch]scratchReservation
	scratchBytes                int64
	protected                   map[string]int
	closed, degraded            bool
	closeDone                   chan struct{}
	closeErr                    error
	now                         func() time.Time
	syncFile                    func(*os.File) error
	freeBytes                   func(int) (uint64, error)
}

func Open(config Config) (*Store, error) {
	cfg := config.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	dir, err := openDirectory(cfg.Directory)
	if err != nil {
		return nil, err
	}
	s := &Store{cfg: cfg, directory: dir, writers: make(map[string]*Writer), readers: make(map[*Snapshot]string), scratchFiles: make(map[*Scratch]scratchReservation), protected: make(map[string]int), now: time.Now, syncFile: func(f *os.File) error { return f.Sync() }, freeBytes: availableBytes}
	success := false
	defer func() {
		if !success {
			if s.lock != nil {
				s.lock.Close()
			}
			dir.Close()
		}
	}()
	if err := s.acquire(); err != nil {
		return nil, err
	}
	if err := s.loadCatalog(); err != nil {
		return nil, err
	}
	if err := s.recover(); err != nil {
		return nil, err
	}
	if err := s.space(0); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

func (s *Store) clone() catalog {
	next := s.registry
	next.Entries = append([]record(nil), s.registry.Entries...)
	return next
}
func (s *Store) index(id string) int {
	for i := range s.registry.Entries {
		if s.registry.Entries[i].Metadata.ID == id {
			return i
		}
	}
	return -1
}
func (s *Store) total() int64 {
	var total int64
	for _, rec := range s.registry.Entries {
		total += rec.Metadata.Size
	}
	return total
}
func (s *Store) markDegraded()             { s.mu.Lock(); s.degraded = true; s.mu.Unlock() }
func (s *Store) unhealthy(err error) error { s.degraded = true; return err }

func (s *Store) healthy(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.degraded {
		return ErrUnavailable
	}
	var st unix.Stat_t
	if unix.Fstat(int(s.directory.Fd()), &st) != nil || st.Mode&07777 != 0700 || st.Uid != uint32(os.Geteuid()) || s.checkIdentity(markerName, s.markerID) != nil || s.checkIdentity(lockName, s.lockID) != nil || s.checkIdentity(catalogName, s.catalogID) != nil {
		return s.unhealthy(ErrUnavailable)
	}
	if err := s.checkInventory(); err != nil {
		return s.unhealthy(err)
	}
	return nil
}

func (s *Store) space(extra int64) error {
	if extra < 0 || extra > s.cfg.MaxTotalBytes || s.scratchBytes > s.cfg.MaxTotalBytes-extra || s.total() > s.cfg.MaxTotalBytes-extra-s.scratchBytes {
		return ErrQuota
	}
	remaining, err := s.scratchRemaining()
	if err != nil {
		return s.unhealthy(err)
	}
	free, err := s.freeBytes(int(s.directory.Fd()))
	if err != nil {
		return ErrUnavailable
	}
	if free < uint64(s.cfg.MinFreeBytes)+uint64(extra)+uint64(metadataReserve)+uint64(remaining) {
		return ErrQuota
	}
	return nil
}

func (s *Store) validateRecords() error {
	seen := make(map[string]bool)
	var size int64
	for _, rec := range s.registry.Entries {
		m := rec.Metadata
		if !hexValue(m.ID, 32) || seen[m.ID] || (m.Kind != KindGenerated && m.Kind != KindImported) || !validTime(m.CreatedAt) || !validTime(m.UpdatedAt) || m.UpdatedAt.Before(m.CreatedAt) || m.Size < 0 || m.Size > s.cfg.MaxObjectBytes || size > s.cfg.MaxTotalBytes-m.Size || !validCode(m.ErrorCode) || !identifier(m.CreatorID, 256, true) || !identifier(m.SessionID, 256, true) || !validSummary(m.Summary) || m.Verified != (m.Summary != nil) {
			return ErrUnavailable
		}
		switch m.State {
		case StateWriting, StateReady, StateFailed, StateCancelled, StateInterrupted:
		default:
			return ErrUnavailable
		}
		if m.Digest != "" && !hexValue(m.Digest, 64) {
			return ErrUnavailable
		}
		switch rec.Phase {
		case "creating", "writing":
			if rec.Identity.Inode == 0 || m.State != StateWriting || m.Digest != "" || rec.FinalName || m.Verified {
				return ErrUnavailable
			}
		case "prepared", "publishing":
			if rec.Identity.Inode == 0 || !hexValue(m.Digest, 64) || m.Size < 1 || m.State == StateReady {
				return ErrUnavailable
			}
		case "ready":
			if rec.Identity.Inode == 0 || m.State != StateReady || !hexValue(m.Digest, 64) || m.Size < 1 || !rec.FinalName {
				return ErrUnavailable
			}
		case "empty":
			if m.Size != 0 || m.Digest != "" || m.Verified || rec.Identity.Inode != 0 || rec.FinalName || m.State == StateWriting || m.State == StateReady {
				return ErrUnavailable
			}
		default:
			return ErrUnavailable
		}
		if rec.Stamp.Nanoseconds < 0 || rec.Stamp.Nanoseconds >= 1e9 {
			return ErrUnavailable
		}
		if rec.Deleting && rec.Phase != "ready" && rec.Phase != "prepared" && rec.Phase != "empty" {
			return ErrUnavailable
		}
		seen[m.ID] = true
		size += m.Size
	}
	return nil
}

func (s *Store) checkInventory() error {
	names, err := s.directoryNames()
	if err != nil {
		return err
	}
	expected := map[string]bool{markerName: true, lockName: true, catalogName: true}
	for _, rec := range s.registry.Entries {
		if rec.Phase != "empty" {
			expected[basename(rec.Metadata.ID, rec.FinalName)] = true
			if rec.Phase == "publishing" {
				expected[basename(rec.Metadata.ID, true)] = true
			}
		}
	}
	for _, name := range names {
		if !expected[name] {
			return ErrUnavailable
		}
	}
	return nil
}

func (s *Store) recover() error {
	if err := s.checkInventory(); err != nil {
		return err
	}
	for index := 0; index < len(s.registry.Entries); {
		rec := s.registry.Entries[index]
		if rec.Deleting {
			if err := s.finishDeletion(index); err != nil {
				return err
			}
			continue
		}
		if rec.Phase == "empty" {
			index++
			continue
		}
		name := basename(rec.Metadata.ID, rec.FinalName)
		f, st, openErr := s.openFile(name, unix.O_RDONLY)
		if rec.Phase == "publishing" && !rec.FinalName && openErr == nil {
			other, _, otherErr := s.openFile(basename(rec.Metadata.ID, true), unix.O_RDONLY)
			if other != nil {
				other.Close()
			}
			if !errors.Is(otherErr, unix.ENOENT) {
				f.Close()
				return ErrUnavailable
			}
		}
		if rec.Phase == "publishing" && errors.Is(openErr, unix.ENOENT) {
			rec.FinalName = true
			name = basename(rec.Metadata.ID, true)
			f, st, openErr = s.openFile(name, unix.O_RDONLY)
		}
		if (rec.Phase == "creating" || rec.Phase == "writing") && errors.Is(openErr, unix.ENOENT) {
			openErr = nil
		} else {
			if openErr != nil {
				return ErrUnavailable
			}
			f.Close()
			if identity(st) != rec.Identity || st.Size < 0 || st.Size > s.cfg.MaxObjectBytes {
				return ErrUnavailable
			}
			if rec.Phase == "prepared" || rec.Phase == "publishing" || rec.Phase == "ready" {
				if st.Size != rec.Metadata.Size || stamp(st) != rec.Stamp {
					return ErrIntegrity
				}
			}
		}
		if rec.Phase == "ready" {
			index++
			continue
		}
		if rec.Phase == "prepared" && rec.Metadata.State != StateWriting {
			// Explicit terminal results survive restarts. Only a still-running
			// job or publishing intent is classified as interrupted by recovery.
			index++
			continue
		}
		if rec.Phase == "creating" || rec.Phase == "writing" {
			if err := s.unlinkExact(name, rec.Identity, true); err != nil {
				return err
			}
			if s.syncFile(s.directory) != nil {
				return ErrUnavailable
			}
			rec.Identity = fileID{}
			rec.Stamp = fileStamp{}
			rec.Phase = "empty"
			rec.Metadata.Size = 0
			rec.Metadata.Digest = ""
		} else {
			rec.Phase = "prepared"
		}
		rec.Metadata.State = StateInterrupted
		rec.Metadata.ErrorCode = CodeInterrupted
		rec.Metadata.UpdatedAt = s.now().UTC()
		next := s.clone()
		next.Entries[index] = rec
		if err := s.persist(next); err != nil {
			return err
		}
		index++
	}
	return nil
}

func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{Healthy: !s.closed && !s.degraded, Closed: s.closed, Objects: len(s.registry.Entries), Bytes: s.total(), Readers: len(s.readers), Writers: len(s.writers), ScratchFiles: len(s.scratchFiles), ScratchBytes: s.scratchBytes}
}

func (s *Store) List(ctx context.Context, offset, limit int) (Page, error) {
	if offset < 0 || limit < 1 || limit > 200 {
		return Page{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Page{}, err
	}
	items := make([]Metadata, 0, len(s.registry.Entries))
	for _, rec := range s.registry.Entries {
		if !rec.Deleting {
			items = append(items, copyMetadata(rec.Metadata))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	page := Page{Items: []Metadata{}, TotalRecordCount: len(items), StartIndex: offset, Limit: limit}
	if offset < len(items) {
		end := len(items)
		if limit < end-offset {
			end = offset + limit
		}
		page.Items = items[offset:end]
	}
	return page, nil
}

func (s *Store) Get(ctx context.Context, id string) (Metadata, error) {
	if !hexValue(id, 32) {
		return Metadata{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return Metadata{}, err
	}
	i := s.index(id)
	if i < 0 || s.registry.Entries[i].Deleting {
		return Metadata{}, ErrNotFound
	}
	return copyMetadata(s.registry.Entries[i].Metadata), nil
}

// Protect pins a catalog entry while a coordinator holds a live reference. A
// persistent restore plan must also be checked by the coordinator after restart.
func (s *Store) Protect(id string) (func(), error) {
	if !hexValue(id, 32) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(context.Background()); err != nil {
		return nil, err
	}
	i := s.index(id)
	if i < 0 || s.registry.Entries[i].Deleting {
		return nil, ErrNotFound
	}
	s.protected[id]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.protected[id]--
			if s.protected[id] == 0 {
				delete(s.protected, id)
			}
		})
	}, nil
}

func (s *Store) Delete(ctx context.Context, id, expectedDigest string) error {
	if !hexValue(id, 32) || expectedDigest != "" && !hexValue(expectedDigest, 64) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthy(ctx); err != nil {
		return err
	}
	i := s.index(id)
	if i < 0 {
		return ErrNotFound
	}
	rec := s.registry.Entries[i]
	if rec.Metadata.Digest != expectedDigest {
		return ErrConflict
	}
	if s.writers[id] != nil || s.protected[id] > 0 {
		return ErrBusy
	}
	for _, readerID := range s.readers {
		if readerID == id {
			return ErrBusy
		}
	}
	next := s.clone()
	next.Entries[i].Deleting = true
	if err := s.persist(next); err != nil {
		return s.unhealthy(err)
	}
	if err := s.finishDeletion(i); err != nil {
		return s.unhealthy(err)
	}
	return nil
}

func (s *Store) finishDeletion(index int) error {
	rec := s.registry.Entries[index]
	if rec.Phase != "empty" {
		if err := s.unlinkExact(basename(rec.Metadata.ID, rec.FinalName), rec.Identity, true); err != nil {
			return err
		}
		if s.syncFile(s.directory) != nil {
			return ErrUnavailable
		}
	}
	next := s.clone()
	next.Entries = append(next.Entries[:index], next.Entries[index+1:]...)
	return s.persist(next)
}

func (s *Store) Close() error {
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
	writers := make([]*Writer, 0, len(s.writers))
	for _, writer := range s.writers {
		writers = append(writers, writer)
	}
	readers := make([]*Snapshot, 0, len(s.readers))
	for reader := range s.readers {
		readers = append(readers, reader)
	}
	scratches := make([]*Scratch, 0, len(s.scratchFiles))
	for scratch := range s.scratchFiles {
		scratches = append(scratches, scratch)
	}
	s.mu.Unlock()
	var closeErr error
	for _, writer := range writers {
		if writer.stopWith(CodeInterrupted) != nil {
			closeErr = ErrUnavailable
		}
	}
	for _, reader := range readers {
		if reader.Close() != nil {
			closeErr = ErrUnavailable
		}
	}
	for _, scratch := range scratches {
		if scratch.Close() != nil {
			closeErr = ErrUnavailable
		}
	}
	if s.lock.Close() != nil {
		closeErr = ErrUnavailable
	}
	if s.directory.Close() != nil {
		closeErr = ErrUnavailable
	}
	s.mu.Lock()
	s.closeErr = closeErr
	close(s.closeDone)
	s.mu.Unlock()
	return closeErr
}
