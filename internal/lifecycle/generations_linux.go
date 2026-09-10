//go:build linux

package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (s *Store) openGeneration(entry registeredGeneration) (*os.File, error) {
	if entry.Directory == (identity{}) {
		return nil, ErrIncomplete
	}
	fd, err := unix.Openat(int(s.directory.Fd()), generationName(entry.ID), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "lifecycle generation")
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || !ownedDirectory(stat) || fileIdentity(stat) != entry.Directory {
		file.Close()
		return nil, ErrUnavailable
	}
	if err := checkNamed(s.directory, generationName(entry.ID), entry.Directory, true); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func (s *Store) verifyGeneration(ctx context.Context, entry registeredGeneration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry.Directory == (identity{}) {
		var stat unix.Stat_t
		err := unix.Fstatat(int(s.directory.Fd()), generationName(entry.ID), &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		// A crash between mkdir and durable identity registration is not
		// permission to claim an existing directory, even when it is empty.
		return ErrRecoveryRequired
	}
	directory, err := s.openGeneration(entry)
	if err != nil {
		return err
	}
	defer directory.Close()
	names, err := listNames(directory)
	if err != nil {
		return err
	}
	for _, name := range names {
		if name != configName && (entry.Master == nil || name != masterName) {
			return ErrRecoveryRequired
		}
	}
	files := []registeredFile{entry.Config}
	if entry.Master != nil {
		files = append(files, *entry.Master)
	}
	for _, expected := range files {
		if expected.Identity == (identity{}) {
			file, stat, err := openRegular(directory, expected.Name, unix.O_RDONLY)
			if errors.Is(err, unix.ENOENT) {
				continue
			}
			if err != nil {
				return ErrUnavailable
			}
			file.Close()
			if stat.Size < 0 || stat.Size > expected.Size {
				return ErrUnavailable
			}
			// Incomplete files remain inert. Their current inode or contents
			// are never imported into the registry, exposed, or activated.
			continue
		}
		_, actual, err := readRegular(ctx, directory, expected.Name, expected.Size)
		if err != nil {
			return err
		}
		if !actual.Present || actual.Identity != expected.Identity || actual.Digest != expected.SHA256 {
			return ErrUnavailable
		}
	}
	return checkNamed(s.directory, generationName(entry.ID), entry.Directory, true)
}

// StageGeneration registers intention before creating files. A partial stage
// is retained as incomplete and cannot be retried under the same ID or used by
// Plan. Complete equal-content retries are idempotent. Config is opaque; its
// application schema and any credential exclusion are the caller's contract.
func (s *Store) StageGeneration(ctx context.Context, id string, config, master []byte) (Generation, error) {
	if !validHex(id, 32) || len(config) == 0 || len(config) > MaxConfigBytes || master != nil && len(master) != 32 {
		return Generation{}, ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return Generation{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return Generation{}, err
	}
	if s.journal != nil {
		return Generation{}, ErrConflict
	}
	entry := registeredGeneration{ID: id, Config: registeredFile{FileDescriptor: FileDescriptor{Name: configName, Size: int64(len(config)), SHA256: digest(config)}}}
	if master != nil {
		entry.Master = &registeredFile{FileDescriptor: FileDescriptor{Name: masterName, Size: 32, SHA256: digest(master)}}
	}
	if existing, ok := s.findGeneration(id); ok {
		if !existing.Complete {
			return publicGeneration(existing), ErrIncomplete
		}
		if existing.Config.FileDescriptor != entry.Config.FileDescriptor || (existing.Master == nil) != (entry.Master == nil) || existing.Master != nil && existing.Master.FileDescriptor != entry.Master.FileDescriptor {
			return Generation{}, ErrConflict
		}
		return publicGeneration(existing), nil
	}
	if len(s.registry.Generations) >= MaxGenerations {
		return Generation{}, ErrInvalid
	}
	index := len(s.registry.Generations)
	s.registry.Generations = append(s.registry.Generations, entry)
	if err := s.persistRegistry(ctx); err != nil {
		return Generation{}, err
	}
	if err := ctx.Err(); err != nil {
		return publicGeneration(entry), err
	}
	if err := unix.Mkdirat(int(s.directory.Fd()), generationName(id), 0700); err != nil {
		return publicGeneration(entry), ErrRecoveryRequired
	}
	fd, err := unix.Openat(int(s.directory.Fd()), generationName(id), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return publicGeneration(entry), ErrRecoveryRequired
	}
	directory := os.NewFile(uintptr(fd), "staging lifecycle generation")
	defer directory.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || !ownedDirectory(stat) {
		return publicGeneration(entry), ErrRecoveryRequired
	}
	entry.Directory = fileIdentity(stat)
	if err := s.syncDirectory(s.directory); err != nil {
		s.degraded = true
		return publicGeneration(entry), ErrRecoveryRequired
	}
	s.registry.Generations[index] = entry
	if err := s.persistRegistry(ctx); err != nil {
		return publicGeneration(entry), err
	}
	configFile, err := s.writeGenerationFile(ctx, directory, entry, entry.Config, config)
	if err != nil {
		return publicGeneration(entry), err
	}
	entry.Config = configFile
	s.registry.Generations[index] = entry
	if err := s.persistRegistry(ctx); err != nil {
		return publicGeneration(entry), err
	}
	if entry.Master != nil {
		masterFile, err := s.writeGenerationFile(ctx, directory, entry, *entry.Master, master)
		if err != nil {
			return publicGeneration(entry), err
		}
		entry.Master = &masterFile
		s.registry.Generations[index] = entry
		if err := s.persistRegistry(ctx); err != nil {
			return publicGeneration(entry), err
		}
	}
	if err := s.verifyGeneration(ctx, entry); err != nil {
		return publicGeneration(entry), err
	}
	entry.Complete = true
	s.registry.Generations[index] = entry
	if err := s.persistRegistry(ctx); err != nil {
		return publicGeneration(entry), err
	}
	return publicGeneration(entry), nil
}

func (s *Store) writeGenerationFile(ctx context.Context, directory *os.File, entry registeredGeneration, descriptor registeredFile, data []byte) (registeredFile, error) {
	if err := s.checkRoot(ctx); err != nil {
		return descriptor, err
	}
	if err := checkNamed(s.directory, generationName(entry.ID), entry.Directory, true); err != nil {
		return descriptor, err
	}
	file, stat, err := openRegular(directory, descriptor.Name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return descriptor, ErrUnavailable
	}
	defer file.Close()
	if err := s.writeBytes(ctx, file, data); err != nil {
		return descriptor, err
	}
	if err := s.syncDirectory(directory); err != nil {
		s.degraded = true
		return descriptor, ErrRecoveryRequired
	}
	descriptor.Identity = fileIdentity(stat)
	if err := checkNamed(directory, descriptor.Name, descriptor.Identity, false); err != nil {
		return descriptor, err
	}
	return descriptor, nil
}

func (s *Store) ReadGeneration(ctx context.Context, id string) (GenerationFiles, error) {
	if !validHex(id, 32) {
		return GenerationFiles{}, ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return GenerationFiles{}, err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return GenerationFiles{}, err
	}
	entry, ok := s.findGeneration(id)
	if !ok {
		return GenerationFiles{}, ErrNotFound
	}
	if !entry.Complete {
		return GenerationFiles{}, ErrIncomplete
	}
	directory, err := s.openGeneration(entry)
	if err != nil {
		return GenerationFiles{}, err
	}
	defer directory.Close()
	config, actual, err := readRegular(ctx, directory, configName, MaxConfigBytes)
	if err != nil {
		return GenerationFiles{}, err
	}
	if actual.Identity != entry.Config.Identity || actual.Digest != entry.Config.SHA256 {
		return GenerationFiles{}, ErrUnavailable
	}
	result := GenerationFiles{Generation: publicGeneration(entry), Config: config}
	if entry.Master != nil {
		master, actual, err := readRegular(ctx, directory, masterName, 32)
		if err != nil {
			return GenerationFiles{}, err
		}
		if actual.Identity != entry.Master.Identity || actual.Digest != entry.Master.SHA256 {
			return GenerationFiles{}, ErrUnavailable
		}
		result.Master = master
	}
	if err := s.checkRoot(ctx); err != nil {
		return GenerationFiles{}, err
	}
	if err := checkNamed(s.directory, generationName(id), entry.Directory, true); err != nil {
		return GenerationFiles{}, err
	}
	return result, nil
}

// MasterKeyPath is a compatibility adapter for the existing vault loader.
// The returned fixed path is not an authority token: the loader must perform
// its own no-follow ownership checks, while this Store stays open and locked.
func (s *Store) MasterKeyPath(ctx context.Context, id string) (string, error) {
	if !validHex(id, 32) {
		return "", ErrInvalid
	}
	if err := s.enter(ctx); err != nil {
		return "", err
	}
	defer s.leave()
	if err := s.verify(ctx); err != nil {
		return "", err
	}
	entry, ok := s.findGeneration(id)
	if !ok {
		return "", ErrNotFound
	}
	if !entry.Complete {
		return "", ErrIncomplete
	}
	if entry.Master == nil {
		return "", ErrNotFound
	}
	return filepath.Join(s.path, generationName(id), masterName), nil
}
