//go:build linux

package recoverycontrol

import (
	"bytes"
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Store holds a directory lock and a registered mutex-file lock until Close.
// Callers must keep it open for the coordinator's complete operation lifetime.
type Store struct {
	gate          chan struct{}
	path          string
	directory     *os.File
	directoryID   identity
	lock          *os.File
	lockID        identity
	marker        ownerMarker
	markerFile    trackedFile
	currentFile   trackedFile
	proof         publicationProof
	proofFile     trackedFile
	closed        bool
	degraded      bool
	syncFile      func(*os.File) error
	syncDirectory func(*os.File) error
	rename        func(int, string, int, string, uint) error
}

func Open(ctx context.Context, directory, deploymentID string) (*Store, error) {
	if directory == "" {
		directory = DefaultDirectory
	}
	if !validDirectory(directory) || !validHex(deploymentID, 32) {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, id, err := openRoot(ctx, directory, true)
	if err != nil {
		return nil, err
	}
	s := &Store{gate: make(chan struct{}, 1), path: directory, directory: root, directoryID: id, syncFile: func(f *os.File) error { return f.Sync() }, syncDirectory: func(f *os.File) error { return f.Sync() }, rename: unix.Renameat2}
	s.gate <- struct{}{}
	success := false
	created := make(map[string]trackedFile)
	defer func() {
		if !success {
			// Only a still-unpublished claim can clean up its own fixed files.
			// A crash cannot reconstruct this in-memory inode authority.
			_, marker, readErr := readFile(context.Background(), root, markerName, maxMetadataBytes)
			if readErr == nil && !marker.Present {
				for name, file := range created {
					if namedIdentity(root, name, file.Identity) == nil {
						_ = unix.Unlinkat(int(root.Fd()), name, 0)
					}
				}
				if len(created) > 0 {
					_ = root.Sync()
				}
			}
			s.closeDescriptors()
		}
	}()
	if err := unix.Flock(int(root.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, ErrUnavailable
	}
	data, markerFile, err := readFile(ctx, root, markerName, maxMetadataBytes)
	if err != nil {
		return nil, err
	}
	if markerFile.Present {
		if err := decode(data, &s.marker); err != nil || s.marker.Version != 1 || !validHex(s.marker.DeploymentID, 32) || !validHex(s.marker.StoreID, 32) || s.marker.Lock == (identity{}) {
			return nil, ErrUnavailable
		}
		if s.marker.DeploymentID != deploymentID {
			return nil, ErrConflict
		}
		s.markerFile = markerFile
		file, stat, err := openRegular(root, lockName, unix.O_RDWR)
		if err != nil || stat.Size != 0 || statIdentity(stat) != s.marker.Lock {
			if file != nil {
				file.Close()
			}
			return nil, ErrUnavailable
		}
		s.lock = file
		s.lockID = statIdentity(stat)
	} else {
		names, err := directoryNames(root)
		if err != nil {
			return nil, err
		}
		if len(names) != 0 {
			return nil, ErrRecoveryRequired
		}
		file, stat, err := openRegular(root, lockName, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
		if err != nil {
			return nil, ErrUnavailable
		}
		s.lock = file
		s.lockID = statIdentity(stat)
		created[lockName] = trackedFile{Present: true, Identity: s.lockID, Digest: hash(nil)}
		storeID, err := newID()
		if err != nil {
			return nil, err
		}
		s.marker = ownerMarker{Version: 1, DeploymentID: deploymentID, StoreID: storeID, Lock: s.lockID}
	}
	if err := unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrBusy
		}
		return nil, ErrUnavailable
	}
	if !s.markerFile.Present {
		if err := s.lock.Sync(); err != nil {
			return nil, ErrUnavailable
		}
		initial := record{Version: 1, DeploymentID: deploymentID, StoreID: s.marker.StoreID}
		data, _ := encode(initial)
		file, err := s.replace(ctx, currentName, data, trackedFile{})
		if file.Present {
			created[currentName] = file
		}
		if err != nil {
			return nil, err
		}
		s.currentFile = file
		s.proof = publicationProof{Version: 1, DeploymentID: deploymentID, StoreID: s.marker.StoreID, Candidate: recordReference{Digest: file.Digest, Identity: file.Identity}}
		data, _ = encode(s.proof)
		file, err = s.replace(ctx, proofName, data, trackedFile{})
		if file.Present {
			created[proofName] = file
		}
		if err != nil {
			return nil, err
		}
		s.proofFile = file
		data, _ = encode(s.marker)
		file, err = s.replace(ctx, markerName, data, trackedFile{})
		if err != nil {
			return nil, err
		}
		s.markerFile = file
	}
	if err := s.load(ctx); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

func (s *Store) enter(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.gate:
	}
	if err := ctx.Err(); err != nil {
		s.leave()
		return err
	}
	if s.closed {
		s.leave()
		return ErrUnavailable
	}
	if s.degraded {
		s.leave()
		return ErrRecoveryRequired
	}
	return nil
}
func (s *Store) leave() { s.gate <- struct{}{} }

func (s *Store) checkRoot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.directory == nil || s.lock == nil {
		return ErrUnavailable
	}
	root, id, err := openRoot(ctx, s.path, false)
	if err != nil {
		return err
	}
	root.Close()
	if id != s.directoryID {
		return ErrUnavailable
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(s.directory.Fd()), &stat); err != nil || !ownedDirectory(stat) || statIdentity(stat) != s.directoryID {
		return ErrUnavailable
	}
	if err := namedIdentity(s.directory, lockName, s.lockID); err != nil {
		return err
	}
	if err := unix.Fstat(int(s.lock.Fd()), &stat); err != nil || !ownedRegular(stat) || statIdentity(stat) != s.lockID || stat.Size != 0 {
		return ErrUnavailable
	}
	if err := unix.Flock(int(s.directory.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return ErrUnavailable
	}
	if err := unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return ErrUnavailable
	}
	if s.markerFile.Present {
		return s.checkFile(ctx, markerName, s.markerFile)
	}
	return nil
}

func (s *Store) load(ctx context.Context) error {
	data, file, err := readFile(ctx, s.directory, proofName, maxMetadataBytes)
	if err != nil {
		return err
	}
	if !file.Present {
		return ErrRecoveryRequired
	}
	if err := decode(data, &s.proof); err != nil {
		return err
	}
	s.proofFile = file
	_, file, err = readFile(ctx, s.directory, currentName, maxRecordBytes)
	if err != nil {
		return err
	}
	if !file.Present {
		return ErrRecoveryRequired
	}
	s.currentFile = file
	_, _, err = s.readCurrent(ctx)
	return err
}

func (s *Store) validateProof() error {
	p := s.proof
	if p.Version != 1 || p.DeploymentID != s.marker.DeploymentID || p.StoreID != s.marker.StoreID || !validReference(p.Candidate) {
		return ErrRecoveryRequired
	}
	if p.Before == nil {
		if p.Candidate.Revision != 0 {
			return ErrRecoveryRequired
		}
		return nil
	}
	if !validReference(*p.Before) || p.Before.Revision == ^uint64(0) || p.Candidate.Revision != p.Before.Revision+1 || p.Candidate.PreviousDigest != p.Before.Digest || p.Before.Identity == p.Candidate.Identity {
		return ErrRecoveryRequired
	}
	return nil
}

func (s *Store) readCurrent(ctx context.Context) (Snapshot, recordReference, error) {
	if err := s.checkRoot(ctx); err != nil {
		return Snapshot{}, recordReference{}, err
	}
	if err := s.checkFile(ctx, proofName, s.proofFile); err != nil {
		return Snapshot{}, recordReference{}, err
	}
	if err := s.validateProof(); err != nil {
		return Snapshot{}, recordReference{}, err
	}
	data, file, err := readFile(ctx, s.directory, currentName, maxRecordBytes)
	if err != nil {
		return Snapshot{}, recordReference{}, err
	}
	if !file.Present || file != s.currentFile {
		return Snapshot{}, recordReference{}, ErrUnavailable
	}
	var current record
	if err := decode(data, &current); err != nil {
		return Snapshot{}, recordReference{}, err
	}
	if current.Version != 1 || current.DeploymentID != s.marker.DeploymentID || current.StoreID != s.marker.StoreID {
		return Snapshot{}, recordReference{}, ErrRecoveryRequired
	}
	if current.Revision == 0 {
		if current.PreviousDigest != "" || !bytes.Equal(current.Payload, []byte("null")) {
			return Snapshot{}, recordReference{}, ErrRecoveryRequired
		}
	} else {
		if !validHex(current.PreviousDigest, 64) {
			return Snapshot{}, recordReference{}, ErrRecoveryRequired
		}
		normalized, err := normalizePayload(current.Payload)
		if err != nil || !bytes.Equal(normalized, current.Payload) {
			return Snapshot{}, recordReference{}, ErrRecoveryRequired
		}
	}
	actual := recordReference{Revision: current.Revision, Digest: file.Digest, PreviousDigest: current.PreviousDigest, Identity: file.Identity}
	if actual != s.proof.Candidate && (s.proof.Before == nil || actual != *s.proof.Before) {
		return Snapshot{}, recordReference{}, ErrRecoveryRequired
	}
	names, err := directoryNames(s.directory)
	if err != nil {
		return Snapshot{}, recordReference{}, err
	}
	for _, name := range names {
		if name == markerName || name == lockName || name == currentName || name == proofName {
			continue
		}
		if !temporaryName(name) {
			return Snapshot{}, recordReference{}, ErrRecoveryRequired
		}
		// Temporary files from a crashed process are inert. They must be safe
		// regular files but are never read into a record or deleted by name.
		file, stat, err := openRegular(s.directory, name, unix.O_RDONLY)
		if err != nil {
			return Snapshot{}, recordReference{}, ErrUnavailable
		}
		file.Close()
		if stat.Size < 0 || stat.Size > maxRecordBytes {
			return Snapshot{}, recordReference{}, ErrUnavailable
		}
	}
	if err := s.checkRoot(ctx); err != nil {
		return Snapshot{}, recordReference{}, err
	}
	return recordSnapshot(current, data), actual, nil
}

func (s *Store) Read(ctx context.Context) (Snapshot, error) {
	if err := s.enter(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.leave()
	snapshot, _, err := s.readCurrent(ctx)
	return snapshot, err
}

// CompareAndSwap replaces the record only when expectedDigest identifies the
// actual current record. Every successful write advances the revision, even
// for identical payloads. ErrRecoveryRequired may mean the candidate is already
// published: close, reopen and Read instead of blindly repeating the mutation.
func (s *Store) CompareAndSwap(ctx context.Context, expectedDigest string, payload []byte) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if !validHex(expectedDigest, 64) {
		return Snapshot{}, ErrInvalid
	}
	normalized, err := normalizePayload(payload)
	if err != nil {
		return Snapshot{}, err
	}
	if err := s.enter(ctx); err != nil {
		return Snapshot{}, err
	}
	defer s.leave()
	current, before, err := s.readCurrent(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if current.Digest != expectedDigest || current.Revision == ^uint64(0) {
		return Snapshot{}, ErrConflict
	}
	candidate := record{Version: 1, DeploymentID: s.marker.DeploymentID, StoreID: s.marker.StoreID, Revision: current.Revision + 1, PreviousDigest: current.Digest, Payload: normalized}
	data, err := encode(candidate)
	if err != nil || len(data) > maxRecordBytes {
		return Snapshot{}, ErrInvalid
	}
	temporary, candidateFile, err := s.createTemporary(ctx, data)
	if err != nil {
		return Snapshot{}, err
	}
	defer s.removeOwnedTemporary(temporary, candidateFile)
	proof := publicationProof{Version: 1, DeploymentID: s.marker.DeploymentID, StoreID: s.marker.StoreID, Before: &before, Candidate: recordReference{Revision: candidate.Revision, Digest: candidateFile.Digest, PreviousDigest: current.Digest, Identity: candidateFile.Identity}}
	proofData, _ := encode(proof)
	proofFile, err := s.replace(ctx, proofName, proofData, s.proofFile)
	if err != nil {
		return Snapshot{}, err
	}
	s.proof = proof
	s.proofFile = proofFile
	if err := s.checkFile(ctx, proofName, s.proofFile); err != nil {
		return Snapshot{}, err
	}
	// A cancelled or failed pre-rename call can leave a newer proof with the
	// old current record. The next CAS must use this actual before-reference.
	file, err := s.publish(ctx, temporary, candidateFile, currentName, s.currentFile)
	if err != nil {
		return Snapshot{}, err
	}
	s.currentFile = file
	return recordSnapshot(candidate, data), nil
}

// Close waits for the current bounded operation and then releases both locks.
// It is safe concurrently and idempotent. Contexts cannot interrupt a kernel
// filesystem call, including an fsync, that is already in progress.
func (s *Store) Close() error {
	<-s.gate
	defer s.leave()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.closeDescriptors()
}

func (s *Store) closeDescriptors() error {
	var result error
	if s.lock != nil {
		result = errors.Join(result, s.lock.Close())
		s.lock = nil
	}
	if s.directory != nil {
		result = errors.Join(result, s.directory.Close())
		s.directory = nil
	}
	if result != nil {
		return ErrUnavailable
	}
	return nil
}
