//go:build linux

package lifecycle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type trackedFile struct {
	Present  bool
	Identity identity
	Digest   string
}

func fileIdentity(stat unix.Stat_t) identity {
	return identity{Device: uint64(stat.Dev), Inode: stat.Ino}
}
func ownedRegular(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&07777 == 0600 && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid())
}
func ownedDirectory(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&07777 == 0700 && stat.Uid == uint32(os.Geteuid())
}

func validDirectory(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && len(path) <= 4096 && !strings.ContainsAny(path, "\x00\\")
}

// openRoot walks every ancestor without following links. Only the last
// component may be created; existing ancestors are never chmodded or claimed.
func openRoot(ctx context.Context, path string, create bool) (*os.File, identity, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, identity{}, ErrUnavailable
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			unix.Close(fd)
			return nil, identity{}, err
		}
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && create && index == len(parts)-1 {
			if mkdirErr := unix.Mkdirat(fd, part, 0700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(fd)
				return nil, identity{}, ErrUnavailable
			}
			if err := unix.Fsync(fd); err != nil {
				unix.Close(fd)
				return nil, identity{}, ErrUnavailable
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		}
		unix.Close(fd)
		if openErr != nil {
			return nil, identity{}, ErrUnavailable
		}
		fd = next
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil || !ownedDirectory(stat) {
		unix.Close(fd)
		return nil, identity{}, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "lifecycle directory"), fileIdentity(stat), nil
}

func openRegular(directory *os.File, name string, flags int) (*os.File, unix.Stat_t, error) {
	var stat unix.Stat_t
	fd, err := unix.Openat(int(directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, stat, err
	}
	if err := unix.Fstat(fd, &stat); err != nil || !ownedRegular(stat) {
		unix.Close(fd)
		return nil, stat, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), name), stat, nil
}

func readRegular(ctx context.Context, directory *os.File, name string, limit int64) ([]byte, trackedFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, trackedFile{}, err
	}
	file, stat, err := openRegular(directory, name, unix.O_RDONLY)
	if errors.Is(err, unix.ENOENT) {
		return nil, trackedFile{}, nil
	}
	if err != nil {
		return nil, trackedFile{}, ErrUnavailable
	}
	defer file.Close()
	if stat.Size < 0 || stat.Size > limit {
		return nil, trackedFile{}, ErrUnavailable
	}
	data := make([]byte, stat.Size)
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return nil, trackedFile{}, err
		}
		end := min(offset+32*1024, len(data))
		n, readErr := io.ReadFull(file, data[offset:end])
		offset += n
		if readErr != nil {
			return nil, trackedFile{}, ErrUnavailable
		}
	}
	var extra [1]byte
	if n, err := file.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, trackedFile{}, ErrUnavailable
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &after); err != nil || !ownedRegular(after) || fileIdentity(after) != fileIdentity(stat) || after.Size != stat.Size || after.Mtim != stat.Mtim || after.Ctim != stat.Ctim {
		return nil, trackedFile{}, ErrUnavailable
	}
	if err := checkNamed(directory, name, fileIdentity(stat), false); err != nil {
		return nil, trackedFile{}, err
	}
	return data, trackedFile{Present: true, Identity: fileIdentity(stat), Digest: digest(data)}, nil
}

func checkNamed(directory *os.File, name string, expected identity, isDirectory bool) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return ErrUnavailable
	}
	if fileIdentity(stat) != expected {
		return ErrUnavailable
	}
	if isDirectory {
		if !ownedDirectory(stat) {
			return ErrUnavailable
		}
	} else if !ownedRegular(stat) {
		return ErrUnavailable
	}
	return nil
}

func listNames(directory *os.File) ([]string, error) {
	fd, err := unix.Openat(int(directory.Fd()), ".", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "lifecycle directory listing")
	defer file.Close()
	names, err := file.Readdirnames(MaxGenerations + 128)
	if err != nil && err != io.EOF {
		return nil, ErrUnavailable
	}
	if len(names) >= MaxGenerations+128 {
		return nil, ErrUnavailable
	}
	return names, nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(value[:]), nil
}

func (s *Store) writeBytes(ctx context.Context, file *os.File, data []byte) error {
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Write(data[offset:min(offset+32*1024, len(data))])
		offset += n
		if err != nil || n == 0 {
			return ErrUnavailable
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.syncFile(file); err != nil {
		return ErrUnavailable
	}
	return nil
}

// atomicWrite preserves both the candidate and the journal on uncertain
// publication. A failed directory fsync poisons this Store until it is reopened.
func (s *Store) atomicWrite(ctx context.Context, name string, data []byte, expected trackedFile) (trackedFile, error) {
	if err := s.checkTracked(ctx, name, expected); err != nil {
		return trackedFile{}, err
	}
	token, err := randomID()
	if err != nil {
		return trackedFile{}, err
	}
	temporary := ".next-" + token
	file, stat, err := openRegular(s.directory, temporary, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return trackedFile{}, ErrUnavailable
	}
	id := fileIdentity(stat)
	defer file.Close()
	published := false
	defer func() {
		if !published && checkNamed(s.directory, temporary, id, false) == nil {
			// This exact temporary inode belongs to this call. Failure to remove
			// it leaves inert debris, never a generation or manifest authority.
			_ = unix.Unlinkat(int(s.directory.Fd()), temporary, 0)
		}
	}()
	if err := s.writeBytes(ctx, file, data); err != nil {
		return trackedFile{}, err
	}
	if err := s.checkRoot(ctx); err != nil {
		return trackedFile{}, err
	}
	if err := s.checkTracked(ctx, name, expected); err != nil {
		return trackedFile{}, err
	}
	if err := checkNamed(s.directory, temporary, id, false); err != nil {
		return trackedFile{}, err
	}
	if err := ctx.Err(); err != nil {
		return trackedFile{}, err
	}
	flags := uint(0)
	if !expected.Present {
		flags = unix.RENAME_NOREPLACE
	}
	if err := unix.Renameat2(int(s.directory.Fd()), temporary, int(s.directory.Fd()), name, flags); err != nil {
		return trackedFile{}, ErrUnavailable
	}
	published = true
	result := trackedFile{Present: true, Identity: id, Digest: digest(data)}
	if err := s.syncDirectory(s.directory); err != nil {
		s.degraded = true
		return result, ErrRecoveryRequired
	}
	if err := checkNamed(s.directory, name, id, false); err != nil {
		s.degraded = true
		return result, ErrRecoveryRequired
	}
	return result, nil
}

func (s *Store) checkTracked(ctx context.Context, name string, expected trackedFile) error {
	_, actual, err := readRegular(ctx, s.directory, name, maxMetadataBytes)
	if err != nil {
		return err
	}
	if actual != expected {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) removeTracked(ctx context.Context, name string, expected trackedFile) error {
	if !expected.Present {
		return ErrConflict
	}
	if err := s.checkRoot(ctx); err != nil {
		return err
	}
	if err := s.checkTracked(ctx, name, expected); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(s.directory.Fd()), name, 0); err != nil {
		return ErrUnavailable
	}
	if err := s.syncDirectory(s.directory); err != nil {
		s.degraded = true
		return ErrRecoveryRequired
	}
	return nil
}
