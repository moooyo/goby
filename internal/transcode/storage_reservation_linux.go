//go:build linux

package transcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
)

const (
	storageJournalMarker = ".goby-storage-reservations"
	storageJournalLock   = ".goby-storage-lock"
	storageJournalName   = "reservations.json"
	storageJournalNext   = "reservations.next"
)

// fileStorageReservationJournal belongs to the optional privileged broker. The
// media service cannot open it. Provisioning must use a separately owned root;
// the journal is kept outside every mounted job filesystem.
type fileStorageReservationJournal struct {
	mu            sync.Mutex
	dir           *os.File
	lock          *os.File
	device        uint64
	inode         uint64
	path          string
	expectedToken string
	lockDevice    uint64
	lockInode     uint64
	closed        bool
	ledgerClaimed bool
}

// A flock excludes other open owners, but cannot exclude two cached ledgers
// sharing this same file object. Consume its production ownership once.
func (j *fileStorageReservationJournal) claimLedger() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.ledgerClaimed {
		return ErrStorageUnavailable
	}
	if err := j.validateRoot(); err != nil {
		return err
	}
	j.ledgerClaimed = true
	return nil
}

// openStorageReservationJournal requires an explicitly prepared root and exact
// owner token. It never bootstraps an arbitrary directory or silently repairs a
// partial transaction. Initialization is a separately reviewable broker setup.
func openStorageReservationJournal(path, expectedToken string) (_ storageReservationJournal, err error) {
	if os.Geteuid() != 0 || !validJobID(expectedToken) {
		return nil, ErrStorageUnavailable
	}
	dir, err := openCacheDirectory(path, false)
	if err != nil {
		return nil, errors.Join(ErrStorageUnsafe, err)
	}
	j := &fileStorageReservationJournal{dir: dir, path: path, expectedToken: expectedToken}
	defer func() {
		if err != nil {
			_ = j.close()
		}
	}()
	stat, err := storageOwnedDirectoryStat(dir)
	if err != nil {
		return nil, err
	}
	j.device, j.inode = uint64(stat.Dev), stat.Ino
	marker, err := storageOpenOwnedJournalFile(dir, storageJournalMarker, syscall.O_RDONLY)
	if err != nil {
		return nil, errors.Join(ErrStorageUnsafe, err)
	}
	markerData, readErr := io.ReadAll(io.LimitReader(marker, 34))
	closeErr := marker.Close()
	if readErr != nil || closeErr != nil || string(markerData) != expectedToken+"\n" {
		return nil, errors.Join(ErrStorageUnsafe, readErr, closeErr)
	}
	j.lock, err = storageOpenOwnedJournalFile(dir, storageJournalLock, syscall.O_RDWR|syscall.O_CREAT)
	if err != nil {
		return nil, err
	}
	lockInfo, err := j.lock.Stat()
	if err != nil {
		return nil, err
	}
	lockStat, ok := lockInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, ErrStorageUnsafe
	}
	j.lockDevice, j.lockInode = uint64(lockStat.Dev), lockStat.Ino
	if err = syscall.Flock(int(j.lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.Join(ErrStorageUnavailable, err)
	}
	if err = j.validateRoot(); err != nil {
		return nil, err
	}
	if err = dir.Sync(); err != nil {
		return nil, errors.Join(ErrStoragePersistence, err)
	}
	return j, nil
}

func storageOwnedDirectoryStat(dir *os.File) (*syscall.Stat_t, error) {
	info, err := dir.Stat()
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || stat.Uid != 0 || info.Mode().Perm() != 0o700 {
		return nil, ErrStorageUnsafe
	}
	return stat, nil
}

func storageOpenOwnedJournalFile(dir *os.File, name string, flags int) (*os.File, error) {
	file, info, err := cacheOpenRegularInfo(dir, name, flags, 0o600)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, ErrStorageUnsafe
	}
	return file, nil
}

func (j *fileStorageReservationJournal) validateRoot() error {
	if j.closed {
		return os.ErrClosed
	}
	stat, err := storageOwnedDirectoryStat(j.dir)
	if err != nil || uint64(stat.Dev) != j.device || stat.Ino != j.inode {
		return errors.Join(ErrStorageUnsafe, err)
	}
	currentRoot, err := openCacheDirectory(j.path, false)
	if err != nil {
		return errors.Join(ErrStorageUnsafe, err)
	}
	currentStat, statErr := storageOwnedDirectoryStat(currentRoot)
	closeErr := currentRoot.Close()
	if statErr != nil || closeErr != nil || uint64(currentStat.Dev) != j.device || currentStat.Ino != j.inode {
		return errors.Join(ErrStorageUnsafe, statErr, closeErr)
	}
	lock, err := storageOpenOwnedJournalFile(j.dir, storageJournalLock, syscall.O_RDONLY)
	if err != nil {
		return errors.Join(ErrStorageUnsafe, err)
	}
	lockInfo, statErr := lock.Stat()
	closeErr = lock.Close()
	if statErr != nil || closeErr != nil {
		return errors.Join(ErrStorageUnsafe, statErr, closeErr)
	}
	lockStat, ok := lockInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(lockStat.Dev) != j.lockDevice || lockStat.Ino != j.lockInode {
		return ErrStorageUnsafe
	}
	marker, err := storageOpenOwnedJournalFile(j.dir, storageJournalMarker, syscall.O_RDONLY)
	if err != nil {
		return errors.Join(ErrStorageUnsafe, err)
	}
	markerData, readErr := io.ReadAll(io.LimitReader(marker, 34))
	closeErr = marker.Close()
	if readErr != nil || closeErr != nil || string(markerData) != j.expectedToken+"\n" {
		return errors.Join(ErrStorageUnsafe, readErr, closeErr)
	}
	names, err := cacheDirectoryNames(j.dir, 4)
	if err != nil {
		return errors.Join(ErrStorageUnsafe, err)
	}
	for _, name := range names {
		switch name {
		case storageJournalMarker, storageJournalLock, storageJournalName:
		case storageJournalNext:
			return ErrStorageRecoveryRequired
		default:
			return ErrStorageUnsafe
		}
	}
	return nil
}

func (j *fileStorageReservationJournal) load() (storageReservationState, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.validateRoot(); err != nil {
		return storageReservationState{}, false, err
	}
	file, err := storageOpenOwnedJournalFile(j.dir, storageJournalName, syscall.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return storageReservationState{}, false, nil
	}
	if err != nil {
		return storageReservationState{}, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxStorageJournalBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxStorageJournalBytes {
		return storageReservationState{}, false, errors.Join(ErrStorageUnsafe, readErr, closeErr)
	}
	var state storageReservationState
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return storageReservationState{}, false, errors.Join(ErrStorageUnsafe, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return storageReservationState{}, false, ErrStorageUnsafe
	}
	return state, true, nil
}

func (j *fileStorageReservationJournal) store(state storageReservationState) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := j.validateRoot(); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > maxStorageJournalBytes {
		return errors.Join(ErrStorageUnsafe, err)
	}
	file, err := storageOpenOwnedJournalFile(j.dir, storageJournalNext, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL)
	if err != nil {
		return err
	}
	// A failed write deliberately leaves the staging inode owned. The ledger
	// fences subsequent actions, and reopening requires explicit recovery.
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := syscall.Renameat(int(j.dir.Fd()), storageJournalNext, int(j.dir.Fd()), storageJournalName); err != nil {
		return err
	}
	if err := j.dir.Sync(); err != nil {
		return err
	}
	return nil
}

func (j *fileStorageReservationJournal) close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	var err error
	if j.lock != nil {
		err = j.lock.Close()
	}
	if j.dir != nil {
		err = errors.Join(err, j.dir.Close())
	}
	if err != nil {
		return fmt.Errorf("close storage reservation journal: %w", err)
	}
	return nil
}
