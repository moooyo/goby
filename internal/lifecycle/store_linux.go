//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// Store keeps both directory and named-file exclusive locks until Close.
// Operations are serialized through a context-aware gate. Cancellation is
// checked between bounded filesystem operations; it cannot interrupt a kernel
// fsync already in progress. A cancelled operation never rolls back a rename.
type Store struct {
	gate          chan struct{}
	path          string
	directory     *os.File
	directoryID   identity
	lock          *os.File
	lockID        identity
	marker        marker
	markerFile    trackedFile
	registry      registry
	registryFile  trackedFile
	active        *activeManifest
	activeFile    trackedFile
	journal       *activationJournal
	journalFile   trackedFile
	closed        bool
	degraded      bool
	syncFile      func(*os.File) error
	syncDirectory func(*os.File) error
}

func Open(ctx context.Context, directory string) (*Store, error) {
	if directory == "" {
		directory = DefaultDirectory
	}
	if !validDirectory(directory) {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, id, err := openRoot(ctx, directory, true)
	if err != nil {
		return nil, err
	}
	s := &Store{gate: make(chan struct{}, 1), path: directory, directory: root, directoryID: id, syncFile: func(file *os.File) error { return file.Sync() }, syncDirectory: func(file *os.File) error { return file.Sync() }}
	s.gate <- struct{}{}
	success := false
	createdLock := false
	defer func() {
		if !success {
			// A failed, unpublished initial claim can remove only the exact
			// mutex inode created by this call, while the root flock is held.
			// A process crash has no such in-memory ownership proof.
			if createdLock && s.lock != nil {
				_, actualMarker, readErr := readRegular(context.Background(), root, markerName, maxMetadataBytes)
				if readErr == nil && !actualMarker.Present && checkNamed(root, lockName, s.lockID, false) == nil {
					_ = unix.Unlinkat(int(root.Fd()), lockName, 0)
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
	data, markerFile, err := readRegular(ctx, root, markerName, maxMetadataBytes)
	if err != nil {
		return nil, err
	}
	if markerFile.Present {
		if err := decode(data, &s.marker); err != nil || s.marker.Version != 1 || !validHex(s.marker.DeploymentID, 32) || s.marker.Lock == (identity{}) {
			return nil, ErrUnavailable
		}
		s.markerFile = markerFile
		file, stat, err := openRegular(root, lockName, unix.O_RDWR)
		if err != nil || stat.Size != 0 || fileIdentity(stat) != s.marker.Lock {
			if file != nil {
				file.Close()
			}
			return nil, ErrUnavailable
		}
		s.lock = file
		s.lockID = fileIdentity(stat)
	} else {
		names, err := listNames(root)
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
		s.lockID = fileIdentity(stat)
		createdLock = true
		token, err := randomID()
		if err != nil {
			return nil, err
		}
		s.marker = marker{Version: 1, DeploymentID: token, Lock: s.lockID}
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
		data, _ := encode(s.marker)
		written, err := s.atomicWrite(ctx, markerName, data, trackedFile{})
		if err != nil {
			return nil, err
		}
		s.markerFile = written
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
	if err := unix.Fstat(int(s.directory.Fd()), &stat); err != nil || !ownedDirectory(stat) || fileIdentity(stat) != s.directoryID || stat.Nlink == 0 {
		return ErrUnavailable
	}
	if err := checkNamed(s.directory, lockName, s.lockID, false); err != nil {
		return err
	}
	if err := unix.Fstat(int(s.lock.Fd()), &stat); err != nil || !ownedRegular(stat) || fileIdentity(stat) != s.lockID || stat.Size != 0 {
		return ErrUnavailable
	}
	if err := unix.Flock(int(s.directory.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return ErrUnavailable
	}
	if err := unix.Flock(int(s.lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return ErrUnavailable
	}
	if s.markerFile.Present {
		return s.checkTracked(ctx, markerName, s.markerFile)
	}
	return nil
}

func (s *Store) load(ctx context.Context) error {
	data, file, err := readRegular(ctx, s.directory, registryName, maxMetadataBytes)
	if err != nil {
		return err
	}
	if !file.Present {
		names, err := listNames(s.directory)
		if err != nil {
			return err
		}
		for _, name := range names {
			if name != markerName && name != lockName && !temporaryName(name) {
				return ErrRecoveryRequired
			}
		}
		s.registry = registry{Version: 1, DeploymentID: s.marker.DeploymentID, BaselineDigest: digest(nil), Generations: []registeredGeneration{}}
		if err := s.persistRegistry(ctx); err != nil {
			return err
		}
	} else {
		if err := decode(data, &s.registry); err != nil {
			return err
		}
		s.registryFile = file
	}
	if err := s.validateRegistry(); err != nil {
		return err
	}
	data, file, err = readRegular(ctx, s.directory, activeName, maxMetadataBytes)
	if err != nil {
		return err
	}
	if file.Present {
		var value activeManifest
		if err := decode(data, &value); err != nil {
			return err
		}
		s.active = &value
	}
	s.activeFile = file
	data, file, err = readRegular(ctx, s.directory, journalName, maxMetadataBytes)
	if err != nil {
		return err
	}
	if file.Present {
		var value activationJournal
		if err := decode(data, &value); err != nil {
			return err
		}
		s.journal = &value
	}
	s.journalFile = file
	return s.verify(ctx)
}

func temporaryName(name string) bool {
	return strings.HasPrefix(name, ".next-") && validHex(strings.TrimPrefix(name, ".next-"), 32)
}

func (s *Store) validateRegistry() error {
	if s.registry.Version != 1 || s.registry.DeploymentID != s.marker.DeploymentID || !validHex(s.registry.BaselineDigest, 64) || len(s.registry.Generations) > MaxGenerations || s.registry.Generations == nil {
		return ErrUnavailable
	}
	seen := make(map[string]bool, len(s.registry.Generations))
	for _, entry := range s.registry.Generations {
		if !validHex(entry.ID, 32) || seen[entry.ID] || !validDescriptor(entry.Config.FileDescriptor, false) {
			return ErrUnavailable
		}
		seen[entry.ID] = true
		if entry.Master != nil && !validDescriptor(entry.Master.FileDescriptor, true) {
			return ErrUnavailable
		}
		if entry.Complete && (entry.Directory == (identity{}) || entry.Config.Identity == (identity{}) || entry.Master != nil && entry.Master.Identity == (identity{})) {
			return ErrUnavailable
		}
	}
	return nil
}

func (s *Store) persistRegistry(ctx context.Context) error {
	data, err := encode(s.registry)
	if err != nil {
		return err
	}
	file, err := s.atomicWrite(ctx, registryName, data, s.registryFile)
	if err != nil {
		s.degraded = true
		return err
	}
	s.registryFile = file
	return nil
}

func (s *Store) verify(ctx context.Context) error {
	if err := s.checkRoot(ctx); err != nil {
		return err
	}
	for name, expected := range map[string]trackedFile{registryName: s.registryFile, activeName: s.activeFile, journalName: s.journalFile} {
		if err := s.checkTracked(ctx, name, expected); err != nil {
			return err
		}
	}
	if err := s.validateRegistry(); err != nil {
		return err
	}
	if err := s.validateManifest(s.active); err != nil {
		return err
	}
	if err := s.validateJournal(); err != nil {
		return err
	}
	names, err := listNames(s.directory)
	if err != nil {
		return err
	}
	allowed := map[string]bool{markerName: true, lockName: true, registryName: true, activeName: s.activeFile.Present, journalName: s.journalFile.Present}
	for _, entry := range s.registry.Generations {
		allowed[generationName(entry.ID)] = true
	}
	for _, name := range names {
		if allowed[name] {
			continue
		}
		if temporaryName(name) {
			_, file, err := readRegular(ctx, s.directory, name, maxMetadataBytes)
			if err != nil || !file.Present {
				return ErrUnavailable
			}
			continue
		}
		return ErrRecoveryRequired
	}
	for _, entry := range s.registry.Generations {
		if err := s.verifyGeneration(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func generationName(id string) string { return "generation-" + id }

func (s *Store) validateManifest(manifest *activeManifest) error {
	if manifest == nil {
		return nil
	}
	if manifest.Version != 1 || manifest.DeploymentID != s.marker.DeploymentID || manifest.Revision == 0 || !validHex(manifest.GenerationID, 32) || manifest.DatabaseSlot != DatabasePrimary && manifest.DatabaseSlot != DatabaseRecovery || manifest.Master != MasterDefault && manifest.Master != MasterGeneration {
		return ErrUnavailable
	}
	entry, ok := s.findGeneration(manifest.GenerationID)
	if !ok || !entry.Complete || manifest.Config != entry.Config.FileDescriptor {
		return ErrUnavailable
	}
	if manifest.Master == MasterGeneration {
		if entry.Master == nil || manifest.MasterKey == nil || *manifest.MasterKey != entry.Master.FileDescriptor {
			return ErrUnavailable
		}
	} else if manifest.MasterKey != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) validateJournal() error {
	current := s.stateOf(s.active)
	if s.journal == nil {
		if s.registry.BaselineDigest != current.Digest {
			return ErrRecoveryRequired
		}
		return nil
	}
	j := s.journal
	if j.Version != 1 || j.DeploymentID != s.marker.DeploymentID || !validHex(j.ID, 32) {
		return ErrRecoveryRequired
	}
	if s.validateManifest(j.Before) != nil || s.validateManifest(&j.After) != nil {
		return ErrRecoveryRequired
	}
	before, after := s.stateOf(j.Before), s.stateOf(&j.After)
	if before.Digest != j.BeforeDigest || after.Digest != j.AfterDigest || before.Revision == ^uint64(0) || after.Revision != before.Revision+1 {
		return ErrRecoveryRequired
	}
	if current != before && current != after {
		return ErrRecoveryRequired
	}
	if s.registry.BaselineDigest != before.Digest && (current != after || s.registry.BaselineDigest != after.Digest) {
		return ErrRecoveryRequired
	}
	return nil
}

func (s *Store) findGeneration(id string) (registeredGeneration, bool) {
	for _, entry := range s.registry.Generations {
		if entry.ID == id {
			return entry, true
		}
	}
	return registeredGeneration{}, false
}

func (s *Store) Current() (State, error) {
	ctx := context.Background()
	if err := s.enter(ctx); err != nil {
		return State{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return State{}, err
	}
	return s.stateOf(s.active), nil
}

func (s *Store) stateOf(manifest *activeManifest) State {
	state := stateOf(manifest)
	state.DeploymentID = s.marker.DeploymentID
	return state
}

func (s *Store) Generations(ctx context.Context) ([]Generation, error) {
	if err := s.enter(ctx); err != nil {
		return nil, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return nil, err
	}
	result := make([]Generation, 0, len(s.registry.Generations))
	for _, entry := range s.registry.Generations {
		result = append(result, publicGeneration(entry))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

// Close waits for an already running bounded operation before releasing both
// process locks. It is safe to call concurrently or more than once.
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
	return result
}
