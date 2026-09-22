//go:build linux

package library

import (
	"errors"
	"os"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

// One queue belongs to one evidence pass, not to a library or a process-wide
// watcher. No goroutine or directory map is retained. Registration attempts
// (including aliases of an existing watch) consume the frozen directory/root
// allowance; the kernel may refuse a smaller per-user watch allowance.
type scanSpoolDirectoryChanges struct {
	mu          sync.Mutex
	descriptor  int
	remaining   int
	unavailable bool
}

func newScanSpoolChangeTracker(limit int) (scanSpoolChangeTracker, error) {
	if limit < 1 || limit > 131072+scanReconciliationMaxRoots {
		return nil, errScanReconciliationEvidenceBudget
	}
	descriptor, err := unix.InotifyInit1(unix.IN_NONBLOCK | unix.IN_CLOEXEC)
	if err != nil {
		return nil, errors.Join(errScanReconciliationEvidenceUnavailable, err)
	}
	return &scanSpoolDirectoryChanges{descriptor: descriptor, remaining: limit}, nil
}

func (changes *scanSpoolDirectoryChanges) watch(directory *os.File) error {
	changes.mu.Lock()
	defer changes.mu.Unlock()
	if err := changes.checkLocked(); err != nil {
		return err
	}
	if changes.remaining == 0 {
		changes.unavailable = true
		return errScanReconciliationEvidenceBudget
	}
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(int(directory.Fd()), &filesystem); err != nil || !scanSpoolLocalNotificationFilesystem(int64(filesystem.Type)) {
		changes.unavailable = true
		return errors.Join(errScanReconciliationEvidenceUnavailable, err)
	}
	changes.remaining--
	// The magic link names the already-held directory. There is no reopen by
	// a media pathname and no recursive traversal. File contents are still
	// proven by the immutable membership/stat records, not by this queue.
	const mask = unix.IN_ONLYDIR | unix.IN_ATTRIB | unix.IN_MODIFY | unix.IN_CLOSE_WRITE |
		unix.IN_CREATE | unix.IN_DELETE | unix.IN_DELETE_SELF | unix.IN_MOVE_SELF |
		unix.IN_MOVED_FROM | unix.IN_MOVED_TO | unix.IN_UNMOUNT
	_, err := unix.InotifyAddWatch(changes.descriptor, "/proc/self/fd/"+strconv.Itoa(int(directory.Fd())), mask)
	if err != nil {
		changes.unavailable = true
		return errors.Join(errScanReconciliationEvidenceUnavailable, err)
	}
	return changes.checkLocked()
}

func (changes *scanSpoolDirectoryChanges) check() error {
	changes.mu.Lock()
	defer changes.mu.Unlock()
	return changes.checkLocked()
}

func (changes *scanSpoolDirectoryChanges) checkLocked() error {
	if changes.unavailable || changes.descriptor < 0 {
		return errScanReconciliationEvidenceUnavailable
	}
	// Any queued byte invalidates the whole pass. In particular, overflow,
	// IN_IGNORED and unmount are never confused with an empty clean queue.
	// No drain/rearm can turn an uncertain pass back into deletion authority.
	var event [unix.SizeofInotifyEvent + unix.NAME_MAX + 1]byte
	for attempt := 0; attempt < 4; attempt++ {
		n, err := unix.Read(changes.descriptor, event[:])
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if n <= 0 && errors.Is(err, unix.EAGAIN) {
			return nil
		}
		changes.unavailable = true
		return errors.Join(errScanReconciliationEvidenceUnavailable, err)
	}
	changes.unavailable = true
	return errScanReconciliationEvidenceUnavailable
}

func scanSpoolLocalNotificationFilesystem(kind int64) bool {
	// inotify(7) explicitly excludes remote changes to network filesystems.
	// Successful registration is not enough: restrict this history witness to
	// these local filesystems. NFS, CIFS, FUSE, overlay and unknown filesystems
	// retain catalog entries instead of falling back to timestamp-only proof.
	switch kind {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.TMPFS_MAGIC:
		return true
	default:
		return false
	}
}

func (changes *scanSpoolDirectoryChanges) close() error {
	changes.mu.Lock()
	defer changes.mu.Unlock()
	if changes.descriptor < 0 {
		return nil
	}
	descriptor := changes.descriptor
	changes.descriptor = -1
	changes.unavailable = true
	// The evidence observation lifetime calls this only after its final worker
	// returns. Closing this one FD removes every watch owned by the pass.
	return unix.Close(descriptor)
}
