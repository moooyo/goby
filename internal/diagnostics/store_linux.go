//go:build linux

package diagnostics

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	markerName       = ".goby-diagnostics.json"
	lockName         = ".goby-diagnostics.lock"
	maxManifestBytes = 128 << 10
	maxDirectorySize = 4096
)

type identity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

type entry struct {
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	ID       identity  `json:"identity"`
	Closed   bool      `json:"closed"`
	Size     int64     `json:"size"`
	Deleting bool      `json:"deleting,omitempty"`
}

type manifest struct {
	Version int     `json:"version"`
	Token   string  `json:"token"`
	Files   []entry `json:"files"`
}

// Store owns a dedicated directory and an exclusive process lock. All path
// operations below are relative to its pinned directory descriptor. Files are
// readable only after matching their persisted device/inode registration.
type Store struct {
	mu         sync.Mutex
	cfg        Config
	directory  *os.File
	lock       *os.File
	lockID     identity
	markerID   identity
	registry   manifest
	active     *os.File
	activeName string
	activeSize int64
	activeDay  string
	degraded   bool
	closed     bool
	closeDone  chan struct{}
	closeErr   error
	readers    map[*Snapshot]struct{}
	now        func() time.Time
	write      func(*os.File, []byte) (int, error)
	syncFile   func(*os.File) error
	freeBytes  func(int) (uint64, error)
}

// Open never imports arbitrary existing logs. A new directory must be empty;
// a reused directory must contain a valid ownership marker and registry.
func Open(config Config) (*Store, error) {
	cfg := config.WithDefaults()
	if !validConfig(cfg) {
		return nil, ErrInvalid
	}
	directory, err := openDirectory(cfg.Directory)
	if err != nil {
		return nil, ErrUnavailable
	}
	s := &Store{
		cfg: cfg, directory: directory, readers: make(map[*Snapshot]struct{}),
		now: time.Now, write: func(f *os.File, p []byte) (int, error) { return f.Write(p) },
		syncFile: func(f *os.File) error { return f.Sync() }, freeBytes: availableBytes,
	}
	success := false
	defer func() {
		if !success {
			s.closeDescriptors()
		}
	}()
	if err := s.acquireLock(); err != nil {
		return nil, err
	}
	if err := s.loadRegistry(); err != nil {
		return nil, err
	}
	if err := s.recoverFiles(); err != nil {
		return nil, err
	}
	if err := s.pruneLocked(s.now().UTC(), true); err != nil {
		return nil, err
	}
	if err := s.createActiveLocked(s.now().UTC()); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

func validConfig(c Config) bool {
	return filepath.IsAbs(c.Directory) && filepath.Clean(c.Directory) == c.Directory && c.Directory != "/" &&
		c.MaxFileBytes >= MaxRecordBytes && c.MaxFileBytes <= MaxReadBytes && c.MaxFiles >= 1 && c.MaxFiles <= 256 &&
		c.RetentionDays >= 1 && c.RetentionDays <= 365 && c.MinFreeBytes > 0 && c.MinFreeBytes <= 1<<40
}

func openDirectory(path string) (*os.File, error) {
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, part := range parts {
		flags := syscall.O_RDONLY | syscall.O_CLOEXEC | syscall.O_DIRECTORY | syscall.O_NOFOLLOW
		next, openErr := syscall.Openat(fd, part, flags, 0)
		if errors.Is(openErr, syscall.ENOENT) && index == len(parts)-1 {
			if mkdirErr := syscall.Mkdirat(fd, part, 0700); mkdirErr != nil && !errors.Is(mkdirErr, syscall.EEXIST) {
				syscall.Close(fd)
				return nil, mkdirErr
			}
			next, openErr = syscall.Openat(fd, part, flags, 0)
		}
		syscall.Close(fd)
		if openErr != nil {
			return nil, openErr
		}
		fd = next
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&07777 != 0700 {
		syscall.Close(fd)
		return nil, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "diagnostics directory"), nil
}

func (s *Store) openFile(name string, flags int) (*os.File, syscall.Stat_t, error) {
	var stat syscall.Stat_t
	if !validBasename(name) {
		return nil, stat, ErrInvalid
	}
	fd, err := syscall.Openat(int(s.directory.Fd()), name, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, stat, err
	}
	if err := syscall.Fstat(fd, &stat); err != nil || !ownedRegular(stat) {
		syscall.Close(fd)
		return nil, stat, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), name), stat, nil
}

func ownedRegular(stat syscall.Stat_t) bool {
	return stat.Mode&syscall.S_IFMT == syscall.S_IFREG && stat.Mode&07777 == 0600 && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid())
}

func fileIdentity(stat syscall.Stat_t) identity {
	return identity{Device: uint64(stat.Dev), Inode: stat.Ino}
}

func validBasename(name string) bool {
	return name != "" && len(name) <= 160 && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

func (s *Store) acquireLock() error {
	file, stat, err := s.openFile(lockName, syscall.O_RDWR|syscall.O_CREAT)
	if err != nil || stat.Size != 0 {
		if file != nil {
			file.Close()
		}
		return ErrUnavailable
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrBusy
		}
		return ErrUnavailable
	}
	s.lock = file
	s.lockID = fileIdentity(stat)
	return nil
}

func (s *Store) directoryNames() ([]string, error) {
	fd, err := syscall.Openat(int(s.directory.Fd()), ".", syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "diagnostics directory listing")
	defer file.Close()
	names := make([]string, 0)
	for {
		batch, err := file.Readdirnames(128)
		names = append(names, batch...)
		if len(names) > maxDirectorySize {
			return nil, ErrUnavailable
		}
		if errors.Is(err, io.EOF) {
			return names, nil
		}
		if err != nil {
			return nil, ErrUnavailable
		}
	}
}

func (s *Store) loadRegistry() error {
	file, stat, err := s.openMarkerForLoad()
	if errors.Is(err, syscall.ENOENT) {
		names, err := s.directoryNames()
		if err != nil {
			return err
		}
		for _, name := range names {
			if name != lockName {
				return ErrUnavailable
			}
		}
		token, err := randomToken(16)
		if err != nil {
			return ErrUnavailable
		}
		s.registry = manifest{Version: 1, Token: token, Files: []entry{}}
		return s.persistRegistryLocked()
	}
	if err != nil {
		return ErrUnavailable
	}
	defer file.Close()
	if stat.Size < 1 || stat.Size > maxManifestBytes {
		return ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil || len(data) > maxManifestBytes {
		return ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s.registry); err != nil || decoder.Decode(new(any)) != io.EOF {
		return ErrUnavailable
	}
	if s.registry.Version != 1 || !hexToken(s.registry.Token, 32) || len(s.registry.Files) > 256 {
		return ErrUnavailable
	}
	s.markerID = fileIdentity(stat)
	if stat.Nlink == 2 {
		if err := s.recoverInitialMarkerLink(file); err != nil {
			return err
		}
	}
	seen := make(map[string]bool)
	active := 0
	for _, item := range s.registry.Files {
		if !s.validLogName(item.Name) || seen[item.Name] || item.Created.IsZero() || item.Created.After(s.now().Add(24*time.Hour)) ||
			item.Size < 0 || item.Size > MaxReadBytes || item.ID.Inode == 0 {
			return ErrUnavailable
		}
		seen[item.Name] = true
		if item.Deleting && !item.Closed {
			return ErrUnavailable
		}
		if !item.Closed {
			active++
		}
	}
	if active > 1 {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) openMarkerForLoad() (*os.File, syscall.Stat_t, error) {
	var stat syscall.Stat_t
	fd, err := syscall.Openat(int(s.directory.Fd()), markerName, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, stat, err
	}
	if err := syscall.Fstat(fd, &stat); err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG ||
		stat.Mode&07777 != 0600 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink < 1 || stat.Nlink > 2 {
		syscall.Close(fd)
		return nil, stat, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), markerName), stat, nil
}

func (s *Store) recoverInitialMarkerLink(marker *os.File) error {
	// linkat publishes the very first, empty registry before removing its
	// temporary name. Only that exact same-inode, service-owned link is eligible
	// for recovery; arbitrary external or extra hard links remain forbidden.
	if len(s.registry.Files) != 0 {
		return ErrUnavailable
	}
	names, err := s.directoryNames()
	if err != nil || len(names) != 3 {
		return ErrUnavailable
	}
	var temporary string
	prefix := ".goby-" + s.registry.Token + "-"
	for _, name := range names {
		if name == markerName || name == lockName {
			continue
		}
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".tmp") ||
			!hexToken(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".tmp"), 16) {
			return ErrUnavailable
		}
		temporary = name
	}
	if temporary == "" {
		return ErrUnavailable
	}
	fd, err := syscall.Openat(int(s.directory.Fd()), temporary, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return ErrUnavailable
	}
	defer syscall.Close(fd)
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil || fileIdentity(stat) != s.markerID || stat.Nlink != 2 {
		return ErrUnavailable
	}
	if err := syscall.Unlinkat(int(s.directory.Fd()), temporary); err != nil {
		return ErrUnavailable
	}
	if err := syscall.Fstat(int(marker.Fd()), &stat); err != nil || !ownedRegular(stat) {
		return ErrUnavailable
	}
	if err := s.syncFile(s.directory); err != nil {
		return ErrUnavailable
	}
	return nil
}

func hexToken(token string, length int) bool {
	if len(token) != length {
		return false
	}
	for _, char := range token {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func (s *Store) validLogName(name string) bool {
	prefix := "goby-" + s.registry.Token + "-"
	return validBasename(name) && strings.HasPrefix(name, prefix) && strings.HasSuffix(name, ".jsonl") &&
		hexToken(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".jsonl"), 32)
}

func (s *Store) registeredFile(item entry, flags int) (*os.File, syscall.Stat_t, error) {
	file, stat, err := s.openFile(item.Name, flags)
	if err != nil {
		return nil, stat, ErrUnavailable
	}
	if fileIdentity(stat) != item.ID || stat.Size < 0 || stat.Size > MaxReadBytes || item.Closed && stat.Size != item.Size {
		file.Close()
		return nil, stat, ErrUnavailable
	}
	return file, stat, nil
}

func (s *Store) recoverFiles() error {
	changed := false
	for index := 0; index < len(s.registry.Files); {
		item := &s.registry.Files[index]
		if item.Deleting {
			if err := s.completeDeletionLocked(index); err != nil {
				return err
			}
			continue
		}
		flags := syscall.O_RDONLY
		if !item.Closed {
			flags = syscall.O_RDWR
		}
		file, stat, err := s.registeredFile(*item, flags)
		if err != nil {
			return err
		}
		if !item.Closed {
			// A crash can leave only the last record incomplete. Its bounded suffix
			// is discarded before the previous active file becomes immutable.
			start := stat.Size - MaxRecordBytes
			if start < 0 {
				start = 0
			}
			tail := make([]byte, stat.Size-start)
			if _, err := file.ReadAt(tail, start); err != nil && !errors.Is(err, io.EOF) {
				file.Close()
				return ErrUnavailable
			}
			last := bytes.LastIndexByte(tail, '\n')
			if last < 0 && start != 0 {
				file.Close()
				return ErrUnavailable
			}
			size := start + int64(last+1)
			if size != stat.Size {
				if err := file.Truncate(size); err != nil {
					file.Close()
					return ErrUnavailable
				}
			}
			if err := s.syncFile(file); err != nil {
				file.Close()
				return ErrUnavailable
			}
			item.Closed, item.Size = true, size
			changed = true
		}
		if err := file.Close(); err != nil {
			return ErrUnavailable
		}
		index++
	}
	if changed {
		return s.persistRegistryLocked()
	}
	return nil
}

func (s *Store) verifyMarkerLocked() error {
	if s.markerID.Inode == 0 {
		return nil
	}
	file, stat, err := s.openFile(markerName, syscall.O_RDONLY)
	if err != nil {
		return ErrUnavailable
	}
	err = file.Close()
	if err != nil || fileIdentity(stat) != s.markerID {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) verifyLockLocked() error {
	file, stat, err := s.openFile(lockName, syscall.O_RDONLY)
	if err != nil {
		return ErrUnavailable
	}
	err = file.Close()
	if err != nil || stat.Size != 0 || fileIdentity(stat) != s.lockID {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) persistRegistryLocked() error {
	data, err := json.Marshal(s.registry)
	if err != nil || len(data) > maxManifestBytes {
		return ErrUnavailable
	}
	data = append(data, '\n')
	token, err := randomToken(8)
	if err != nil {
		return ErrUnavailable
	}
	name := ".goby-" + s.registry.Token + "-" + token + ".tmp"
	file, stat, err := s.openFile(name, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL)
	if err != nil {
		return ErrUnavailable
	}
	committed := false
	defer func() {
		file.Close()
		if !committed {
			s.unlinkIdentity(name, fileIdentity(stat))
		}
	}()
	if err := s.writeAll(file, data); err != nil || s.syncFile(file) != nil {
		return ErrUnavailable
	}
	if err := s.verifyMarkerLocked(); err != nil {
		return err
	}
	if s.markerID.Inode == 0 {
		// linkat publishes the initial marker without ever replacing a file
		// introduced by another actor between the empty-directory check and now.
		if err := linkAt(int(s.directory.Fd()), name, markerName); err != nil {
			return ErrUnavailable
		}
		if err := syscall.Unlinkat(int(s.directory.Fd()), name); err != nil {
			return ErrUnavailable
		}
	} else if err := syscall.Renameat(int(s.directory.Fd()), name, int(s.directory.Fd()), markerName); err != nil {
		return ErrUnavailable
	}
	committed = true
	s.markerID = fileIdentity(stat)
	if err := s.syncFile(s.directory); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) writeAll(file *os.File, data []byte) error {
	for len(data) > 0 {
		n, err := s.write(file, data)
		if n < 0 || n > len(data) {
			return ErrUnavailable
		}
		data = data[n:]
		if err != nil || n == 0 {
			return ErrUnavailable
		}
	}
	return nil
}

func (s *Store) unlinkIdentity(name string, id identity) error {
	file, stat, err := s.openFile(name, syscall.O_RDONLY)
	if err != nil {
		return ErrUnavailable
	}
	defer file.Close()
	if fileIdentity(stat) != id {
		return ErrUnavailable
	}
	if err := syscall.Unlinkat(int(s.directory.Fd()), name); err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) pruneLocked(now time.Time, reserveSlot bool) error {
	sort.Slice(s.registry.Files, func(i, j int) bool {
		if s.registry.Files[i].Created.Equal(s.registry.Files[j].Created) {
			return s.registry.Files[i].Name < s.registry.Files[j].Name
		}
		return s.registry.Files[i].Created.Before(s.registry.Files[j].Created)
	})
	maximum := s.cfg.MaxFiles
	if reserveSlot {
		maximum--
	}
	cutoff := now.Add(-time.Duration(s.cfg.RetentionDays) * 24 * time.Hour)
	for index := 0; index < len(s.registry.Files); {
		item := s.registry.Files[index]
		if !item.Closed || !(item.Created.Before(cutoff) || len(s.registry.Files) > maximum) {
			index++
			continue
		}
		// A durable deletion intent makes either side of an interrupted unlink
		// recoverable without adopting or deleting an unregistered file.
		s.registry.Files[index].Deleting = true
		if err := s.persistRegistryLocked(); err != nil {
			return err
		}
		if err := s.completeDeletionLocked(index); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) completeDeletionLocked(index int) error {
	item := s.registry.Files[index]
	file, stat, err := s.openFile(item.Name, syscall.O_RDONLY)
	if err != nil && !errors.Is(err, syscall.ENOENT) {
		return ErrUnavailable
	}
	if file != nil {
		file.Close()
		if fileIdentity(stat) != item.ID || stat.Size != item.Size {
			return ErrUnavailable
		}
		if err := s.unlinkIdentity(item.Name, item.ID); err != nil {
			return err
		}
	}
	s.registry.Files = append(s.registry.Files[:index], s.registry.Files[index+1:]...)
	return s.persistRegistryLocked()
}

func (s *Store) createActiveLocked(now time.Time) error {
	if err := s.ensureSpaceLocked(int64(MaxRecordBytes)); err != nil {
		return err
	}
	token, err := randomToken(16)
	if err != nil {
		return ErrUnavailable
	}
	name := "goby-" + s.registry.Token + "-" + token + ".jsonl"
	file, stat, err := s.openFile(name, syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_EXCL)
	if err != nil {
		return ErrUnavailable
	}
	item := entry{Name: name, Created: now.UTC(), ID: fileIdentity(stat)}
	s.registry.Files = append(s.registry.Files, item)
	previousMarkerID := s.markerID
	if err := s.persistRegistryLocked(); err != nil {
		file.Close()
		// A failed directory sync can follow a successful registry publication.
		// Preserve its registered inode so the next Open can recover the file.
		if s.markerID == previousMarkerID {
			s.registry.Files = s.registry.Files[:len(s.registry.Files)-1]
			s.unlinkIdentity(name, item.ID)
		}
		return err
	}
	s.active, s.activeName, s.activeSize, s.activeDay = file, name, 0, now.Format("2006-01-02")
	return nil
}

func (s *Store) finishActiveLocked() error {
	if s.active == nil {
		return nil
	}
	var item *entry
	for index := range s.registry.Files {
		if s.registry.Files[index].Name == s.activeName {
			item = &s.registry.Files[index]
			break
		}
	}
	if item == nil {
		return ErrUnavailable
	}
	check, stat, err := s.registeredFile(*item, syscall.O_RDONLY)
	if err != nil {
		return ErrUnavailable
	}
	if err := check.Close(); err != nil || stat.Size != s.activeSize {
		return ErrUnavailable
	}
	if err := s.syncFile(s.active); err != nil {
		return ErrUnavailable
	}
	if err := s.active.Close(); err != nil {
		s.active = nil
		return ErrUnavailable
	}
	s.active = nil
	item.Closed = true
	item.Size = s.activeSize
	return s.persistRegistryLocked()
}

func availableBytes(fd int) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(fd, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 || stat.Bavail > ^uint64(0)/uint64(stat.Bsize) {
		return 0, ErrUnavailable
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func (s *Store) ensureSpaceLocked(extra int64) error {
	free, err := s.freeBytes(int(s.directory.Fd()))
	if err != nil || free < uint64(s.cfg.MinFreeBytes+extra+maxManifestBytes) {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) healthyLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.degraded || s.directory == nil || s.lock == nil {
		return ErrUnavailable
	}
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(s.directory.Fd()), &stat); err != nil || stat.Uid != uint32(os.Geteuid()) || stat.Mode&07777 != 0700 {
		s.degraded = true
		return ErrUnavailable
	}
	if err := s.verifyMarkerLocked(); err != nil {
		s.degraded = true
		return ErrUnavailable
	}
	if err := s.verifyLockLocked(); err != nil {
		s.degraded = true
		return ErrUnavailable
	}
	return nil
}

func (s *Store) appendRecord(ctx context.Context, line []byte) error {
	if len(line) == 0 || len(line) > MaxRecordBytes || line[len(line)-1] != '\n' || bytes.Count(line, []byte{'\n'}) != 1 || !json.Valid(line) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthyLocked(ctx); err != nil {
		return err
	}
	now := s.now().UTC()
	if s.active == nil || s.activeSize+int64(len(line)) > s.cfg.MaxFileBytes || s.activeDay != now.Format("2006-01-02") {
		if err := s.finishActiveLocked(); err != nil {
			return s.degradeLocked()
		}
		if err := s.pruneLocked(now, true); err != nil {
			return s.degradeLocked()
		}
		if err := s.createActiveLocked(now); err != nil {
			return s.degradeLocked()
		}
	}
	if err := s.pruneLocked(now, false); err != nil {
		return s.degradeLocked()
	}
	if err := s.ensureSpaceLocked(int64(len(line))); err != nil {
		return s.degradeLocked()
	}
	var current *entry
	for index := range s.registry.Files {
		if s.registry.Files[index].Name == s.activeName {
			current = &s.registry.Files[index]
			break
		}
	}
	if current == nil {
		return s.degradeLocked()
	}
	check, stat, err := s.registeredFile(*current, syscall.O_RDONLY)
	if err != nil {
		return s.degradeLocked()
	}
	check.Close()
	if stat.Size != s.activeSize {
		return s.degradeLocked()
	}
	before := s.activeSize
	if err := s.writeAll(s.active, line); err != nil {
		// Restore the complete-record boundary after a partial write. Even if
		// rollback succeeds, the store remains degraded until an explicit reopen.
		_ = s.active.Truncate(before)
		_ = s.syncFile(s.active)
		return s.degradeLocked()
	}
	s.activeSize += int64(len(line))
	if err := s.syncFile(s.active); err != nil {
		return s.degradeLocked()
	}
	return nil
}

func (s *Store) degradeLocked() error {
	s.degraded = true
	return ErrUnavailable
}

func (s *Store) markDegraded() {
	s.mu.Lock()
	s.degraded = true
	s.mu.Unlock()
}

func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Healthy: !s.closed && !s.degraded && s.directory != nil, Degraded: s.degraded || !s.closed && s.directory == nil, Closed: s.closed,
		MaxFileBytes: s.cfg.MaxFileBytes, MaxFiles: s.cfg.MaxFiles, RetentionDays: s.cfg.RetentionDays,
		MinFreeBytes: s.cfg.MinFreeBytes, Format: "jsonl",
	}
}

func (s *Store) List(ctx context.Context, options ListOptions) (Page, error) {
	page := Page{Items: []File{}}
	if options.StartIndex < 0 || options.Limit < 1 || options.Limit > 200 {
		return page, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthyLocked(ctx); err != nil {
		return page, err
	}
	if err := s.pruneLocked(s.now().UTC(), false); err != nil {
		return page, s.degradeLocked()
	}
	items := make([]File, 0, len(s.registry.Files))
	for _, item := range s.registry.Files {
		if err := ctx.Err(); err != nil {
			return Page{}, err
		}
		file, stat, err := s.registeredFile(item, syscall.O_RDONLY)
		if err != nil {
			return Page{}, s.degradeLocked()
		}
		if err := file.Close(); err != nil || !item.Closed && stat.Size != s.activeSize {
			return Page{}, s.degradeLocked()
		}
		items = append(items, File{Name: item.Name, DateCreated: item.Created, DateModified: time.Unix(int64(stat.Mtim.Sec), int64(stat.Mtim.Nsec)).UTC(), Size: stat.Size})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DateCreated.Equal(items[j].DateCreated) {
			return items[i].Name > items[j].Name
		}
		return items[i].DateCreated.After(items[j].DateCreated)
	})
	page.TotalRecordCount = len(items)
	if options.StartIndex >= len(items) {
		return page, nil
	}
	end := options.StartIndex + options.Limit
	if end > len(items) {
		end = len(items)
	}
	page.Items = items[options.StartIndex:end]
	return page, nil
}

func (s *Store) Snapshot(ctx context.Context, name string) (*Snapshot, error) {
	if !validBasename(name) {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.healthyLocked(ctx); err != nil {
		return nil, err
	}
	if len(s.readers) >= MaxReaders {
		return nil, ErrBusy
	}
	var found *entry
	for index := range s.registry.Files {
		if s.registry.Files[index].Name == name {
			found = &s.registry.Files[index]
			break
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	file, stat, err := s.registeredFile(*found, syscall.O_RDONLY)
	if err != nil || !found.Closed && stat.Size != s.activeSize {
		if file != nil {
			file.Close()
		}
		return nil, s.degradeLocked()
	}
	snapshot := &Snapshot{file: file, ctx: ctx, name: name, size: stat.Size, modTime: time.Unix(int64(stat.Mtim.Sec), int64(stat.Mtim.Nsec)).UTC(), failure: s.markDegraded}
	snapshot.release = func(reader *Snapshot) {
		s.mu.Lock()
		delete(s.readers, reader)
		s.mu.Unlock()
	}
	s.readers[snapshot] = struct{}{}
	snapshot.mu.Lock()
	snapshot.stop = context.AfterFunc(ctx, func() { _ = snapshot.Close() })
	snapshot.mu.Unlock()
	return snapshot, nil
}

// Close stops new writes and readers, flushes complete records, releases the
// process lock, and closes outstanding snapshots. It is safe to call repeatedly.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		done := s.closeDone
		s.mu.Unlock()
		if done != nil {
			<-done
		}
		s.mu.Lock()
		err := s.closeErr
		s.mu.Unlock()
		return err
	}
	s.closed = true
	s.closeDone = make(chan struct{})
	var closeErr error
	if s.degraded {
		closeErr = ErrUnavailable
	} else if err := s.finishActiveLocked(); err != nil {
		s.degraded = true
		closeErr = ErrUnavailable
	}
	readers := make([]*Snapshot, 0, len(s.readers))
	for reader := range s.readers {
		readers = append(readers, reader)
	}
	s.mu.Unlock()
	for _, reader := range readers {
		if err := reader.Close(); err != nil {
			closeErr = ErrUnavailable
		}
	}
	s.mu.Lock()
	// Keep the process lock until every pinned reader has released its inode.
	if err := s.closeDescriptors(); err != nil {
		s.degraded = true
		closeErr = ErrUnavailable
	}
	s.closeErr = closeErr
	close(s.closeDone)
	s.mu.Unlock()
	return closeErr
}

func (s *Store) closeDescriptors() error {
	var err error
	if s.active != nil {
		if closeErr := s.active.Close(); closeErr != nil {
			err = ErrUnavailable
		}
		s.active = nil
	}
	if s.lock != nil {
		if closeErr := s.lock.Close(); closeErr != nil {
			err = ErrUnavailable
		}
		s.lock = nil
	}
	if s.directory != nil {
		if closeErr := s.directory.Close(); closeErr != nil {
			err = ErrUnavailable
		}
		s.directory = nil
	}
	return err
}
