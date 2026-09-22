//go:build linux

package library

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestScanReconciliationSpoolChangesRejectRestoredNamesWithoutTimestampEvidence(t *testing.T) {
	directory := t.TempDir()
	child := filepath.Join(directory, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	held, err := os.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	tracker, err := newScanSpoolChangeTracker(1)
	if err != nil {
		t.Fatal(err)
	}
	defer tracker.close()
	if err := tracker.watch(held); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(child)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(child, child+"-parked"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(child+"-parked", child); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(child)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("the test did not restore the original directory identity")
	}
	// No sleep, artificial ctime change or changed inode supplies this proof.
	// A coalesced kernel timestamp must not hide the observed rename history.
	for attempt := 0; attempt < 2; attempt++ {
		if err := tracker.check(); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
			t.Fatalf("restored directory history was not sticky: %v", err)
		}
	}
}

func TestScanReconciliationSpoolChangesRejectLostQueues(t *testing.T) {
	for name, mask := range map[string]uint32{
		"overflow": unix.IN_Q_OVERFLOW,
		"ignored":  unix.IN_IGNORED,
		"unmount":  unix.IN_UNMOUNT,
	} {
		t.Run(name, func(t *testing.T) {
			var descriptors [2]int
			if err := unix.Pipe2(descriptors[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			tracker := &scanSpoolDirectoryChanges{descriptor: descriptors[0], remaining: 1}
			defer tracker.close()
			defer unix.Close(descriptors[1])
			if err := tracker.check(); err != nil {
				t.Fatalf("an empty nonblocking queue was unavailable: %v", err)
			}
			// Supply the kernel event layout through a controlled read descriptor.
			// There is no global sysctl change or unbounded event storm.
			var event [unix.SizeofInotifyEvent]byte
			binary.NativeEndian.PutUint32(event[4:8], mask)
			if _, err := unix.Write(descriptors[1], event[:]); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := tracker.check(); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
					t.Fatalf("a lost observation queue became reusable: %v", err)
				}
			}
		})
	}
}

func TestScanReconciliationSpoolChangesBoundRegistrationsAndClosedState(t *testing.T) {
	held, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	tracker, err := newScanSpoolChangeTracker(1)
	if err != nil {
		t.Fatal(err)
	}
	defer tracker.close()
	if err := tracker.watch(held); err != nil {
		t.Fatal(err)
	}
	if err := tracker.watch(held); !errors.Is(err, errScanReconciliationEvidenceBudget) {
		t.Fatalf("a duplicate registration bypassed the finite budget: %v", err)
	}
	if err := tracker.close(); err != nil {
		t.Fatal(err)
	}
	if err := tracker.check(); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("a closed history witness remained usable: %v", err)
	}
}

func TestScanReconciliationSpoolChangesRejectUnreliableFilesystems(t *testing.T) {
	for _, kind := range []int64{unix.NFS_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC, unix.FUSE_SUPER_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, 0} {
		if scanSpoolLocalNotificationFilesystem(kind) {
			t.Fatalf("filesystem %#x acquired unproved remote change history", kind)
		}
	}
	for _, kind := range []int64{unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC, unix.TMPFS_MAGIC} {
		if !scanSpoolLocalNotificationFilesystem(kind) {
			t.Fatalf("supported local filesystem %#x was refused", kind)
		}
	}
}

func TestScanReconciliationSpoolRequiresPreReadObservation(t *testing.T) {
	for _, beforeRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing boundary", true: "change after boundary"}[beforeRead], func(t *testing.T) {
			directory := t.TempDir()
			evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
			root := scanEvidenceTestAttach(t, evidence, "root", directory)
			held, err := openScanFile(root, ".")
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			before, err := held.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if beforeRead {
				if err := evidence.BeginDirectoryObservation("root", ".", held, before); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := held.ReadDir(-1)
			if err != nil {
				t.Fatal(err)
			}
			if beforeRead {
				name := filepath.Join(directory, "transient")
				if err := os.Mkdir(name, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
			}
			if err := evidence.RecordDirectory("root", ".", before, entries); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
				t.Fatalf("an unobserved or changed read boundary produced evidence: %v", err)
			}
		})
	}
}

func TestScanReconciliationSpoolChangeQueueRetiresWithActualWorker(t *testing.T) {
	evidence := scanSpoolTestCollector(t, scanReconciliationSpoolOptions{})
	root := scanEvidenceTestAttach(t, evidence, "root", t.TempDir())
	scanEvidenceTestRecord(t, evidence, "root", ".", root)
	scanEvidenceTestComplete(t, evidence, "root", ".")
	tracker := evidence.spool.changes.(*scanSpoolDirectoryChanges)
	if !evidence.observation.retain() {
		t.Fatal("the observation worker was not retained")
	}
	if err := evidence.Close(); err != nil {
		evidence.observation.release()
		t.Fatal(err)
	}
	if err := tracker.check(); err != nil {
		evidence.observation.release()
		t.Fatalf("retirement closed the live worker's change queue: %v", err)
	}
	evidence.observation.release()
	if err := tracker.check(); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("the last worker did not retire its change queue: %v", err)
	}
	if done, receipt := evidence.CleanupStatus(); !done || receipt.Err != nil {
		t.Fatalf("change queue cleanup did not finish: done=%t receipt=%+v", done, receipt)
	}
	if _, err := evidence.PathAbsent(context.Background(), "root", "missing"); !errors.Is(err, errScanReconciliationEvidenceUnavailable) {
		t.Fatalf("retired evidence regained absence authority: %v", err)
	}
}
