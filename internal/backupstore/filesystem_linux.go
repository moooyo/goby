//go:build linux

package backupstore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	markerName      = ".goby-backup-store.json"
	lockName        = ".goby-backup-store.lock"
	catalogName     = ".goby-backup-catalog.json"
	pendingName     = ".goby-backup-catalog.pending"
	maxCatalogBytes = 16 << 20
	metadataReserve = 2*maxCatalogBytes + 65536
)

type fileID struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}
type fileStamp struct {
	Seconds     int64 `json:"seconds"`
	Nanoseconds int64 `json:"nanoseconds"`
}
type marker struct {
	Format string `json:"format"`
	Token  string `json:"token"`
}
type record struct {
	Metadata  Metadata  `json:"metadata"`
	Identity  fileID    `json:"identity"`
	Stamp     fileStamp `json:"stamp"`
	Phase     string    `json:"phase"`
	FinalName bool      `json:"finalName"`
	Deleting  bool      `json:"deleting"`
}
type catalog struct {
	Version int      `json:"version"`
	Token   string   `json:"token"`
	Entries []record `json:"entries"`
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", ErrUnavailable
	}
	return hex.EncodeToString(bytes[:]), nil
}
func identity(st unix.Stat_t) fileID { return fileID{Device: uint64(st.Dev), Inode: st.Ino} }
func stamp(st unix.Stat_t) fileStamp {
	return fileStamp{Seconds: int64(st.Mtim.Sec), Nanoseconds: int64(st.Mtim.Nsec)}
}
func ownedFile(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == 0600 && st.Uid == uint32(os.Geteuid()) && st.Nlink == 1
}
func basename(id string, final bool) string {
	if final {
		return "object-" + id + ".age"
	}
	return "object-" + id + ".partial"
}

func openDirectory(path string) (*os.File, error) {
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if errors.Is(openErr, unix.ENOENT) && i == len(parts)-1 {
			if createErr := unix.Mkdirat(fd, part, 0700); createErr != nil && !errors.Is(createErr, unix.EEXIST) {
				unix.Close(fd)
				return nil, ErrUnavailable
			}
			next, openErr = unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		}
		unix.Close(fd)
		if openErr != nil {
			return nil, ErrUnavailable
		}
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&07777 != 0700 || st.Uid != uint32(os.Geteuid()) {
		unix.Close(fd)
		return nil, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "backup directory"), nil
}

func (s *Store) openFile(name string, flags int) (*os.File, unix.Stat_t, error) {
	var st unix.Stat_t
	if name == "" || strings.ContainsAny(name, "/\\\x00") {
		return nil, st, ErrInvalid
	}
	fd, err := unix.Openat(int(s.directory.Fd()), name, flags|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, st, err
	}
	if unix.Fstat(fd, &st) != nil || !ownedFile(st) {
		unix.Close(fd)
		return nil, st, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "backup storage file"), st, nil
}

func (s *Store) directoryNames() ([]string, error) {
	fd, err := unix.Openat(int(s.directory.Fd()), ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "backup listing")
	defer f.Close()
	names, err := f.Readdirnames(s.cfg.MaxObjects + 6)
	if err != nil && !errors.Is(err, io.EOF) || len(names) > s.cfg.MaxObjects+4 {
		return nil, ErrUnavailable
	}
	return names, nil
}

func readJSON(f *os.File, limit int64, value any) error {
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) checkIdentity(name string, id fileID) error {
	f, st, err := s.openFile(name, unix.O_RDONLY)
	if err != nil {
		return ErrUnavailable
	}
	defer f.Close()
	if identity(st) != id {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) unlinkExact(name string, id fileID, missing bool) error {
	f, st, err := s.openFile(name, unix.O_RDONLY)
	if missing && errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return ErrUnavailable
	}
	defer f.Close()
	if identity(st) != id {
		return ErrUnavailable
	}
	if unix.Unlinkat(int(s.directory.Fd()), name, 0) != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) acquire() error {
	names, err := s.directoryNames()
	if err != nil {
		return err
	}
	markerFile, st, err := s.openFile(markerName, unix.O_RDONLY)
	if errors.Is(err, unix.ENOENT) {
		// Never claim a directory that already contains anything, including a
		// marker from another subsystem or an abandoned unrelated lock file.
		if len(names) != 0 {
			return ErrUnavailable
		}
		token, err := randomID()
		if err != nil {
			return err
		}
		s.ownership = marker{Format: "goby-backupstore-v1", Token: token}
		f, newIdentity, err := s.anonymousFile()
		if err != nil {
			return ErrUnavailable
		}
		data, _ := json.Marshal(s.ownership)
		n, writeErr := f.Write(data)
		syncErr := s.syncFile(f)
		// Publish an already complete marker; no visible empty/truncated
		// ownership file may survive an interrupted first initialization.
		var publishErr error
		if writeErr == nil && n == len(data) && syncErr == nil {
			publishErr = s.publishAnonymous(f, markerName)
		} else {
			publishErr = ErrUnavailable
		}
		closeErr := f.Close()
		if publishErr != nil || closeErr != nil {
			return ErrUnavailable
		}
		s.markerID = newIdentity
	} else {
		if err != nil {
			return ErrUnavailable
		}
		parseErr := readJSON(markerFile, 4096, &s.ownership)
		markerFile.Close()
		if parseErr != nil || s.ownership.Format != "goby-backupstore-v1" || !hexValue(s.ownership.Token, 32) {
			return ErrUnavailable
		}
		s.markerID = identity(st)
	}
	lock, lockStat, err := s.openFile(lockName, unix.O_RDWR|unix.O_CREAT)
	if err != nil || lockStat.Size != 0 {
		if lock != nil {
			lock.Close()
		}
		return ErrUnavailable
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return ErrBusy
		}
		return ErrUnavailable
	}
	s.lock = lock
	s.lockID = identity(lockStat)
	return nil
}

func (s *Store) loadCatalog() error {
	f, st, err := s.openFile(catalogName, unix.O_RDONLY)
	if errors.Is(err, unix.ENOENT) {
		names, listErr := s.directoryNames()
		if listErr != nil {
			return listErr
		}
		for _, name := range names {
			if name != markerName && name != lockName && name != pendingName {
				return ErrUnavailable
			}
		}
		s.registry = catalog{Version: 1, Token: s.ownership.Token, Entries: []record{}}
	} else {
		if err != nil {
			return ErrUnavailable
		}
		parseErr := readJSON(f, maxCatalogBytes, &s.registry)
		f.Close()
		if parseErr != nil {
			return parseErr
		}
		s.catalogID = identity(st)
	}
	if s.registry.Version != 1 || s.registry.Token != s.ownership.Token || len(s.registry.Entries) > s.cfg.MaxObjects {
		return ErrUnavailable
	}
	if err := s.validateRecords(); err != nil {
		return err
	}
	// This fixed metadata role is reserved by the immutable ownership marker.
	// A crash may leave any prefix of its JSON bytes; it is never an object.
	if f, st, err := s.openFile(pendingName, unix.O_RDONLY); err == nil {
		f.Close()
		if st.Size > maxCatalogBytes || s.unlinkExact(pendingName, identity(st), false) != nil || s.syncFile(s.directory) != nil {
			return ErrUnavailable
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return ErrUnavailable
	}
	if s.catalogID.Inode == 0 {
		return s.persist(s.registry)
	}
	return nil
}

func (s *Store) persist(next catalog) error {
	data, err := json.Marshal(next)
	if err != nil || len(data) > maxCatalogBytes {
		return ErrUnavailable
	}
	if s.checkIdentity(markerName, s.markerID) != nil || s.checkIdentity(lockName, s.lockID) != nil {
		return ErrUnavailable
	}
	if s.catalogID.Inode != 0 && s.checkIdentity(catalogName, s.catalogID) != nil {
		return ErrUnavailable
	}
	f, st, err := s.openFile(pendingName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return ErrUnavailable
	}
	installed := false
	defer func() {
		f.Close()
		if !installed {
			_ = s.unlinkExact(pendingName, identity(st), true)
		}
	}()
	if n, err := f.Write(data); err != nil || n != len(data) || s.syncFile(f) != nil {
		return ErrUnavailable
	}
	if s.checkIdentity(markerName, s.markerID) != nil || s.checkIdentity(lockName, s.lockID) != nil || s.catalogID.Inode != 0 && s.checkIdentity(catalogName, s.catalogID) != nil {
		return ErrUnavailable
	}
	if s.catalogID.Inode == 0 {
		err = unix.Renameat2(int(s.directory.Fd()), pendingName, int(s.directory.Fd()), catalogName, unix.RENAME_NOREPLACE)
	} else {
		err = unix.Renameat(int(s.directory.Fd()), pendingName, int(s.directory.Fd()), catalogName)
	}
	if err != nil {
		return ErrUnavailable
	}
	installed = true
	s.registry = next
	s.catalogID = identity(st)
	// A failed directory sync may already have published the new registry.
	// Retain all referenced files and require reopening to resolve its state.
	if s.syncFile(s.directory) != nil {
		s.degraded = true
		return ErrUnavailable
	}
	return nil
}

func (s *Store) anonymousFile() (*os.File, fileID, error) {
	return s.anonymousFileWithFlags(0)
}

func (s *Store) anonymousFileWithFlags(flags int) (*os.File, fileID, error) {
	fd, err := unix.Openat(int(s.directory.Fd()), ".", unix.O_TMPFILE|unix.O_RDWR|unix.O_CLOEXEC|flags, 0600)
	if err != nil {
		return nil, fileID{}, ErrUnavailable
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 0 {
		unix.Close(fd)
		return nil, fileID{}, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "anonymous backup object"), identity(st), nil
}

func (s *Store) publishAnonymous(f *os.File, name string) error {
	err := unix.Linkat(int(f.Fd()), "", int(s.directory.Fd()), name, unix.AT_EMPTY_PATH)
	if errors.Is(err, unix.EPERM) || errors.Is(err, unix.ENOENT) {
		// Unprivileged services may link O_TMPFILE descriptors through their own
		// procfs descriptor. No caller path or unrelated symlink is followed.
		err = unix.Linkat(unix.AT_FDCWD, "/proc/self/fd/"+strconv.FormatUint(uint64(f.Fd()), 10), int(s.directory.Fd()), name, unix.AT_SYMLINK_FOLLOW)
	}
	if err != nil || s.syncFile(s.directory) != nil {
		return ErrUnavailable
	}
	return nil
}

func availableBytes(fd int) (uint64, error) {
	var st unix.Statfs_t
	if unix.Fstatfs(fd, &st) != nil || st.Bsize <= 0 || st.Bavail > ^uint64(0)/uint64(st.Bsize) {
		return 0, ErrUnavailable
	}
	return st.Bavail * uint64(st.Bsize), nil
}
