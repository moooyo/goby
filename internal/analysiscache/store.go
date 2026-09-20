package analysiscache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Open acquires exclusive disk ownership before recovery. Unknown directories,
// links, malformed ownership markers and unsealed ready entries are retained
// and reported as errors; they are never treated as expendable cache output.
func Open(ctx context.Context, config Config) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	disk, err := openDiskRoot(config.Root)
	if err != nil {
		return nil, err
	}
	s := &Store{config: config, disk: disk, entries: make(map[string]*entryState), builders: make(map[string]*Builder), changed: make(chan struct{}), done: make(chan struct{})}
	s.controlBytes = disk.markerInfo.Size()
	if config.MaxBytes-s.controlBytes < controlReserve {
		return nil, errors.Join(ErrInvalidInput, disk.close())
	}
	if err := s.recover(ctx); err != nil {
		return nil, errors.Join(err, disk.close())
	}
	return s, nil
}

func (s *Store) recover(ctx context.Context) error {
	names, err := readNames(s.disk.root, maxRootNames)
	if err != nil {
		return err
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		if name == diskMarkerName || name == diskLockName {
			continue
		}
		if strings.HasPrefix(name, s.temporaryPrefix()) || strings.HasPrefix(name, "trash-"+s.disk.owner+"-") {
			root, err := openChild(s.disk.root, name)
			if err != nil {
				return err
			}
			children, err := readNames(root, hardMaxTemporaryFiles+7)
			key := ""
			if err == nil && len(children) > 0 {
				data, readErr := readControl(root, ownerName, 512)
				err = readErr
				if err == nil {
					var record ownerRecord
					err = decodeCanonical(data, &record)
					key = record.Key
					if err == nil && (record.Owner != s.disk.owner || record.Marker != "goby-analysis-entry-v1" || !keyPattern.MatchString(key) || !tokenPattern.MatchString(record.Token)) {
						err = ErrUnsafe
					}
					if err == nil && strings.HasPrefix(name, s.temporaryPrefix()) && name != s.temporaryPrefix()+record.Token {
						err = ErrUnsafe
					}
					if err == nil && strings.HasPrefix(name, "trash-") && name != s.trashDirectory(key) {
						err = ErrUnsafe
					}
				}
			}
			err = errors.Join(err, root.Close())
			if err != nil {
				return err
			}
			if strings.HasPrefix(name, s.temporaryPrefix()) && !tokenPattern.MatchString(strings.TrimPrefix(name, s.temporaryPrefix())) {
				return ErrUnsafe
			}
			if strings.HasPrefix(name, "trash-") && !keyPattern.MatchString(strings.TrimPrefix(name, "trash-"+s.disk.owner+"-")) {
				return ErrUnsafe
			}
			if err := s.removeOwnedDirectory(ctx, name, key); err != nil {
				return err
			}
			continue
		}
		key := strings.TrimPrefix(name, "entry-")
		if name != readyDirectory(key) || !keyPattern.MatchString(key) {
			return fmt.Errorf("%w: unknown cache root entry", ErrUnsafe)
		}
		root, err := openChild(s.disk.root, name)
		if err != nil {
			return err
		}
		entry, readErr := loadEntry(root, s.disk.owner, key)
		closeErr := root.Close()
		err = errors.Join(readErr, closeErr)
		if errors.Is(readErr, ErrNotFound) && closeErr == nil && entry.Key == key && entry.Seal != "" {
			// The complete ownership/control inventory was verified. Missing
			// declared derivatives are disposable, unlike lost ownership proof.
			state := &entryState{entry: entry, directory: name, missing: true}
			if removeErr := s.removeEntry(ctx, state); removeErr != nil {
				return errors.Join(err, removeErr)
			}
			continue
		}
		if err != nil {
			return err
		}
		s.clock++
		state := &entryState{entry: entry, directory: name, lastUse: s.clock}
		s.entries[key] = state
		tooLarge := entry.Bytes > s.config.MaxEntryBytes
		for _, artifact := range entry.Artifacts {
			if artifact.Size > s.config.MaxFileBytes {
				tooLarge = true
			}
		}
		if tooLarge {
			if err := s.removeEntry(ctx, state); err != nil {
				return err
			}
			delete(s.entries, key)
		}
	}
	for {
		stats := s.statsLocked()
		if stats.TotalBytes <= s.config.MaxBytes && stats.ReadyEntries <= s.config.MaxEntries {
			break
		}
		state := s.oldestLocked()
		if state == nil {
			return ErrLimit
		}
		if err := s.removeEntry(ctx, state); err != nil {
			return err
		}
		delete(s.entries, state.entry.Key)
	}
	return s.disk.check()
}

func (s *Store) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }

func (s *Store) admit() error {
	if s == nil {
		return ErrClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return ErrClosed
	}
	s.active++
	return nil
}

func (s *Store) finish() { s.mu.Lock(); s.active--; s.signalLocked(); s.mu.Unlock() }

func (s *Store) statsLocked() Stats {
	stats := Stats{ReadyEntries: len(s.entries), BuildingEntries: len(s.builders), BusyEntries: len(s.builders), ControlBytes: s.controlBytes, Closing: s.closing}
	for _, state := range s.entries {
		stats.ReadyBytes += state.entry.Bytes
		stats.Readers += state.refs
		if state.pending {
			stats.PendingPublications++
		}
		if state.pending || state.refs != 0 || state.deleting {
			stats.BusyEntries++
		}
	}
	for _, builder := range s.builders {
		stats.ReservedBytes += builder.reservation
	}
	stats.TotalBytes = stats.ReadyBytes + stats.ReservedBytes + stats.ControlBytes
	return stats
}

func (s *Store) Stats() Stats {
	if s == nil {
		return Stats{Closing: true}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statsLocked()
}

// HasArtifact is an inexpensive snapshot of the owned generation index. It is
// suitable for an inventory display, never for source authorization or a byte
// response. Acquire still verifies the real directory, manifest and full hash.
func (s *Store) HasArtifact(key, expectedSeal, name, expectedSHA256 string, expectedBytes int64) bool {
	if !keyPattern.MatchString(expectedSHA256) || expectedBytes < 1 {
		return false
	}
	artifact, found := s.ArtifactSnapshot(key, expectedSeal, name)
	return found && artifact.SHA256 == expectedSHA256 && artifact.Size == expectedBytes
}

// ArtifactSnapshot is a detached inventory value, not a byte-delivery lease.
func (s *Store) ArtifactSnapshot(key, expectedSeal, name string) (Artifact, bool) {
	if s == nil || !keyPattern.MatchString(key) || !keyPattern.MatchString(expectedSeal) || !allowedArtifact(name) {
		return Artifact{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.entries[key]
	if s.closing || state == nil || state.deleting || state.invalid || state.missing || state.entry.Seal != expectedSeal {
		return Artifact{}, false
	}
	for _, artifact := range state.entry.Artifacts {
		if artifact.Name == name {
			return artifact, true
		}
	}
	return Artifact{}, false
}

func (s *Store) oldestLocked() *entryState {
	var result *entryState
	for _, state := range s.entries {
		if state.refs != 0 || state.pending || state.deleting || state.invalid {
			continue
		}
		if result == nil || state.lastUse < result.lastUse {
			result = state
		}
	}
	return result
}

// Begin reserves the complete workspace budget and one entry before writing.
// Reservation includes temporary bytes and private metadata. LRU eviction can
// reclaim only entries without a reader or an undecided publication pin.
func (s *Store) Begin(ctx context.Context, key string, reserveBytes int64) (*Builder, error) {
	if !keyPattern.MatchString(key) {
		return nil, ErrInvalidInput
	}
	if err := s.admit(); err != nil {
		return nil, err
	}
	defer s.finish()
	if reserveBytes == 0 {
		reserveBytes = s.config.MaxEntryBytes
	}
	if reserveBytes < controlReserve || reserveBytes > s.config.MaxEntryBytes {
		return nil, ErrLimit
	}
	if err := s.disk.check(); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if s.closing {
			s.mu.Unlock()
			return nil, ErrClosed
		}
		if s.entries[key] != nil || s.builders[key] != nil {
			s.mu.Unlock()
			return nil, ErrExists
		}
		stats := s.statsLocked()
		if stats.TotalBytes <= s.config.MaxBytes-reserveBytes && stats.ReadyEntries+stats.BuildingEntries < s.config.MaxEntries {
			builderCtx, cancel := context.WithCancel(ctx)
			builder := &Builder{store: s, key: key, reservation: reserveBytes, ctx: builderCtx, cancel: cancel, done: make(chan struct{}), artifacts: make(map[string]Artifact), temporaries: make(map[string]*temporaryState), temporaryChanged: make(chan struct{}), acceptTemporary: true}
			builder.mu.Lock()
			s.builders[key] = builder
			s.active++
			s.signalLocked()
			s.mu.Unlock()
			err := builder.initialize()
			builder.mu.Unlock()
			context.AfterFunc(builderCtx, func() { builder.abortAsync() })
			if err != nil {
				builder.cancel()
				builder.abortAsync()
				<-builder.done
				return nil, errors.Join(err, builder.abortErr)
			}
			if err := builderCtx.Err(); err != nil {
				builder.abortAsync()
				<-builder.done
				return nil, errors.Join(err, builder.abortErr)
			}
			return builder, nil
		}
		state := s.oldestLocked()
		if state == nil {
			s.mu.Unlock()
			return nil, ErrLimit
		}
		state.deleting = true
		s.mu.Unlock()
		err := s.removeEntry(ctx, state)
		s.mu.Lock()
		state.deleting = false
		if err == nil {
			delete(s.entries, state.entry.Key)
		} else if state.directory != readyDirectory(state.entry.Key) || !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			state.invalid = true
		}
		s.signalLocked()
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
	}
}

// Acquire verifies the caller's expected seal and selected content hash before
// handing out an independent read-only descriptor positioned at byte zero.
func (s *Store) Acquire(ctx context.Context, key, expectedSeal, name string) (*Lease, error) {
	if !keyPattern.MatchString(key) || !keyPattern.MatchString(expectedSeal) || !allowedArtifact(name) {
		return nil, ErrInvalidInput
	}
	if err := s.admit(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	state := s.entries[key]
	if state == nil {
		s.mu.Unlock()
		s.finish()
		return nil, ErrNotFound
	}
	if state.deleting {
		s.mu.Unlock()
		s.finish()
		return nil, ErrBusy
	}
	if state.invalid {
		s.mu.Unlock()
		s.finish()
		return nil, ErrUnsafe
	}
	if state.entry.Seal != expectedSeal {
		s.mu.Unlock()
		s.finish()
		return nil, ErrSealMismatch
	}
	state.refs++
	s.clock++
	state.lastUse = s.clock
	s.mu.Unlock()
	var file *os.File
	delivered := false
	defer func() {
		if !delivered {
			if file != nil {
				_ = file.Close()
			}
			s.mu.Lock()
			state.refs--
			s.active--
			s.signalLocked()
			s.mu.Unlock()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.disk.check(); err != nil {
		return nil, err
	}
	root, err := openChild(s.disk.root, state.directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := s.disk.check(); err != nil {
				return nil, err
			}
			if _, currentErr := s.disk.root.Lstat(state.directory); errors.Is(currentErr, os.ErrNotExist) {
				s.noteMissing(state)
				return nil, ErrNotFound
			}
		}
		return nil, err
	}
	defer root.Close()
	beforeDirectory, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	entry, err := loadEntry(root, s.disk.owner, key)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			currentDirectory, directoryErr := s.disk.root.Lstat(state.directory)
			if entry.Seal != expectedSeal || directoryErr != nil || !os.SameFile(beforeDirectory, currentDirectory) {
				return nil, ErrUnsafe
			}
			if err := s.disk.check(); err != nil {
				return nil, err
			}
			if closeErr := root.Close(); closeErr != nil {
				return nil, errors.Join(ErrUnsafe, closeErr)
			}
			s.noteMissing(state)
		}
		return nil, err
	}
	if entry.Seal != expectedSeal {
		return nil, ErrSealMismatch
	}
	var selected *Artifact
	for _, artifact := range entry.Artifacts {
		if artifact.Name == name {
			copy := artifact
			selected = &copy
			break
		}
	}
	if selected == nil {
		if closeErr := root.Close(); closeErr != nil {
			return nil, errors.Join(ErrUnsafe, closeErr)
		}
		return nil, ErrNotFound
	}
	file, err = openRegular(root, name, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Recheck all ownership/control facts after a payload disappears
			// between inventory validation and opening the selected descriptor.
			current, readErr := loadEntry(root, s.disk.owner, key)
			if errors.Is(readErr, ErrNotFound) && current.Seal == expectedSeal {
				currentDirectory, directoryErr := s.disk.root.Lstat(state.directory)
				if directoryErr != nil || !os.SameFile(beforeDirectory, currentDirectory) {
					return nil, ErrUnsafe
				}
				if err := s.disk.check(); err != nil {
					return nil, err
				}
				if closeErr := root.Close(); closeErr != nil {
					return nil, errors.Join(ErrUnsafe, closeErr)
				}
				s.noteMissing(state)
				return nil, ErrNotFound
			}
			return nil, errors.Join(ErrUnsafe, readErr)
		}
		return nil, err
	}
	digest := sha256.New()
	count, err := copyWithContext(ctx, digest, io.LimitReader(file, selected.Size+1))
	if err != nil {
		return nil, err
	}
	if count != selected.Size || hex.EncodeToString(digest.Sum(nil)) != selected.SHA256 {
		return nil, ErrUnsafe
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	identity, err := fileIdentity(info)
	if err != nil || identity != selected.Identity {
		return nil, ErrUnsafe
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	currentDirectory, err := s.disk.root.Lstat(state.directory)
	if err != nil || !os.SameFile(beforeDirectory, currentDirectory) {
		return nil, ErrUnsafe
	}
	if err := s.disk.check(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := root.Close(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return nil, ErrClosed
	}
	delivered = true
	return &Lease{File: file, ownedFile: file, Artifact: *selected, Seal: entry.Seal, store: s, state: state, retirement: &closeResult{}}, nil
}

func (s *Store) noteMissing(state *entryState) {
	s.mu.Lock()
	state.missing = true
	state.lastUse = 0
	s.signalLocked()
	s.mu.Unlock()
}

func (s *Store) Delete(ctx context.Context, key, expectedSeal string) error {
	if !keyPattern.MatchString(key) || !keyPattern.MatchString(expectedSeal) {
		return ErrInvalidInput
	}
	if err := s.admit(); err != nil {
		return err
	}
	defer s.finish()
	return s.deleteEntry(ctx, key, expectedSeal, false)
}

func (s *Store) deleteEntry(ctx context.Context, key, expectedSeal string, publication bool) error {
	s.mu.Lock()
	state := s.entries[key]
	if state == nil {
		s.mu.Unlock()
		return ErrNotFound
	}
	if state.entry.Seal != expectedSeal {
		s.mu.Unlock()
		return ErrSealMismatch
	}
	if publication && !state.pending {
		s.mu.Unlock()
		return ErrClosed
	}
	if state.refs != 0 || state.deleting || state.pending && !publication {
		s.mu.Unlock()
		return ErrBusy
	}
	state.deleting = true
	s.mu.Unlock()
	err := s.removeEntry(ctx, state)
	s.mu.Lock()
	state.deleting = false
	if err == nil {
		delete(s.entries, key)
	} else if state.directory != readyDirectory(state.entry.Key) || !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		state.invalid = true
	}
	s.signalLocked()
	s.mu.Unlock()
	return err
}

// PruneUnreferenced deletes only owned ready entries absent from the caller's
// current reference snapshot. Busy entries remain charged and are skipped.
// Callers must serialize this reference snapshot against database publication;
// the package cannot determine whether an external database reference is live.
func (s *Store) PruneUnreferenced(ctx context.Context, keepKeys map[string]bool) error {
	_, err := s.PruneUnreferencedResult(ctx, keepKeys)
	return err
}

func (s *Store) PruneUnreferencedResult(ctx context.Context, keepKeys map[string]bool) (result PruneResult, resultErr error) {
	defer func() {
		stats := s.Stats()
		result.RemainingBytes, result.BusyEntries = stats.TotalBytes, stats.BusyEntries
	}()
	keep := make(map[string]bool, len(keepKeys))
	for key, value := range keepKeys {
		if !keyPattern.MatchString(key) {
			return result, ErrInvalidInput
		}
		keep[key] = value
	}
	if err := s.admit(); err != nil {
		return result, err
	}
	defer s.finish()
	s.mu.Lock()
	entries := make([]Entry, 0, len(s.entries))
	for key, state := range s.entries {
		if !keep[key] && !state.pending && state.refs == 0 && !state.deleting {
			entries = append(entries, cloneEntry(state.entry))
		}
	}
	s.mu.Unlock()
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := s.deleteEntry(ctx, entry.Key, entry.Seal, false); err == nil {
			result.RemovedEntries++
			result.RemovedBytes += entry.Bytes
		} else if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrNotFound) {
			return result, err
		}
	}
	return result, nil
}

func (publication *Publication) Keep() error {
	if publication == nil || publication.store == nil || publication.state == nil {
		return ErrInvalidInput
	}
	publication.mu.Lock()
	defer publication.mu.Unlock()
	if publication.finished {
		if publication.discarded {
			return ErrClosed
		}
		return nil
	}
	s := publication.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries[publication.state.entry.Key] != publication.state || !publication.state.pending {
		return ErrUnsafe
	}
	if publication.state.deleting {
		return ErrBusy
	}
	publication.state.pending = false
	s.active--
	s.signalLocked()
	publication.finished = true
	return nil
}

func (publication *Publication) Discard(ctx context.Context) error {
	if publication == nil || publication.store == nil || publication.state == nil {
		return ErrInvalidInput
	}
	publication.mu.Lock()
	defer publication.mu.Unlock()
	if publication.finished {
		return nil
	}
	s := publication.store
	if err := s.deleteEntry(ctx, publication.state.entry.Key, publication.state.entry.Seal, true); err != nil {
		return err
	}
	s.mu.Lock()
	s.active--
	s.signalLocked()
	s.mu.Unlock()
	publication.finished = true
	publication.discarded = true
	return nil
}

// Close refuses new admissions, cancels unfinished builders, and waits for
// actual writers, readers, filesystem calls and publication decisions to end.
// A timeout does not close their descriptors or release ownership early. A later
// Close can continue waiting for the same shutdown, without restarting work.
func (s *Store) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closing = true
		builders := make([]*Builder, 0, len(s.builders))
		for _, builder := range s.builders {
			builders = append(builders, builder)
		}
		s.signalLocked()
		s.mu.Unlock()
		for _, builder := range builders {
			builder.cancel()
			builder.abortAsync()
		}
		go func() {
			for {
				s.mu.Lock()
				if s.active == 0 {
					s.mu.Unlock()
					break
				}
				changed := s.changed
				s.mu.Unlock()
				<-changed
			}
			err := s.disk.close()
			s.mu.Lock()
			for _, builder := range s.builders {
				err = errors.Join(err, builder.abortErr)
			}
			s.closeErr = err
			s.mu.Unlock()
			close(s.done)
		}()
	})
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
