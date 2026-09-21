//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func scanEvidenceTestOptions(t *testing.T) ScanEvidenceOptions {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return ScanEvidenceOptions{Directory: directory, ServerID: "scan-evidence-test-server"}
}

func scanEvidenceTestManager(t *testing.T, options ScanEvidenceOptions) *scanEvidenceManager {
	t.Helper()
	manager, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, nil)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestScanEvidenceStoreQuotaRetainsBlockedRetirementUntilActualCleanup(t *testing.T) {
	manager := scanEvidenceTestManager(t, scanEvidenceTestOptions(t))
	store := &Store{scanEvidence: manager}
	var passes []*scanReconciliationEvidence
	for index := 0; index < 4; index++ {
		pass, err := store.newScanReconciliationEvidence(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		passes = append(passes, pass)
	}
	if status := store.ScanEvidenceStatus(); status.ActivePasses != 4 || status.ReservedBytes != 4<<30 ||
		status.ReservedFileDescriptors != scanEvidenceMaxDescriptors {
		t.Fatalf("unexpected reserved capacity: %+v", status)
	}
	if !passes[0].observation.retain() {
		t.Fatal("cannot retain actual observation")
	}
	released := false
	t.Cleanup(func() {
		if !released {
			passes[0].observation.release()
		}
		for _, pass := range passes {
			_ = pass.Close()
		}
		if err := manager.close(); err != nil {
			t.Error(err)
		}
	})
	if err := passes[0].Close(); err != nil {
		t.Fatal(err)
	}
	if finished, _ := passes[0].CleanupStatus(); finished {
		t.Fatal("blocked observation was declared cleaned")
	}
	if _, err := store.newScanReconciliationEvidence(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatalf("blocked pass released admission: %v", err)
	}
	if status := store.ScanEvidenceStatus(); status.RetiringPasses != 1 || status.ReservedBytes != 4<<30 {
		t.Fatalf("blocked retirement lost its charge: %+v", status)
	}
	closed := make(chan error, 1)
	go func() { closed <- manager.close() }()
	select {
	case err := <-closed:
		t.Fatalf("close did not join the blocked observation: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	passes[0].observation.release()
	released = true
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not finish after the actual observation")
	}
	if status := store.ScanEvidenceStatus(); status.ActivePasses != 0 || status.ReservedBytes != 0 {
		t.Fatalf("successful cleanup retained a charge: %+v", status)
	}
}

func TestScanEvidenceStoreConstructorCancellationDoesNotLeakReservation(t *testing.T) {
	manager := scanEvidenceTestManager(t, scanEvidenceTestOptions(t))
	defer func() {
		if err := manager.close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := manager.admit(ctx); err == nil {
		t.Fatal("cancelled constructor was admitted")
	}
	if status := (&Store{scanEvidence: manager}).ScanEvidenceStatus(); status.ActivePasses != 0 {
		t.Fatalf("early callback leaked a reservation: %+v", status)
	}
	if len(manager.disk.document.Leases) != 0 {
		t.Fatal("cancelled constructor created durable work")
	}
	legacy, err := (&Store{}).newScanReconciliationEvidence(context.Background())
	if err != nil || legacy.spool != nil {
		t.Fatal("disabled deployment changed the bounded-memory path")
	}
	_ = legacy.Close()
}

func TestScanEvidenceStoreCallerDeadlineDoesNotTransferRetirementOwnership(t *testing.T) {
	options := scanEvidenceTestOptions(t)
	manager := scanEvidenceTestManager(t, options)
	storeCtx, cancel := context.WithCancel(context.Background())
	store := &Store{scanEvidence: manager, ctx: storeCtx, cancel: cancel,
		queue: make(chan *scanTask), done: make(chan struct{})}
	pass, err := manager.admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !pass.observation.retain() {
		t.Fatal("cannot retain observation")
	}
	if err := pass.Close(); err != nil {
		t.Fatal(err)
	}
	deadline, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := store.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("caller deadline lost: %v", err)
	}
	if successor, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, nil); err == nil {
		_ = successor.close()
		t.Error("caller deadline released a still-used evidence root")
	}
	pass.observation.release()
	joined, finish := context.WithTimeout(context.Background(), 5*time.Second)
	defer finish()
	if err := store.Close(joined); err != nil {
		t.Fatal(err)
	}
}

func TestScanEvidenceStoreCleanupFailureKeepsChargeLockAndRestartResponsibility(t *testing.T) {
	options := scanEvidenceTestOptions(t)
	manager := scanEvidenceTestManager(t, options)
	pass, err := manager.admit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.disk.root.Rename(pass.spool.name, "unexpected-moved-pass"); err != nil {
		t.Fatal(err)
	}
	if err := pass.Close(); err == nil {
		t.Fatal("renamed cleanup target was silently accepted")
	}
	status := (&Store{scanEvidence: manager}).ScanEvidenceStatus()
	if status.ActivePasses != 1 || status.CleanupFailures != 1 || status.ReservedBytes != 1<<30 {
		t.Fatalf("failed cleanup lost its charge: %+v", status)
	}
	if err := manager.close(); err == nil {
		t.Fatal("store close discarded cleanup failure")
	}
	if successor, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, nil); err == nil {
		_ = successor.close()
		t.Fatal("failed retirement released the parent lock")
	}
	// Simulate process exit only after every actual pass descriptor is closed.
	if err := manager.disk.close(); err != nil {
		t.Fatal(err)
	}
	if successor, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, nil); err == nil {
		_ = successor.close()
		t.Fatal("restart adopted an unknown sibling by prefix")
	}
	if _, err := os.Stat(filepath.Join(options.Directory, "unexpected-moved-pass")); err != nil {
		t.Fatal("restart removed unrecognized evidence")
	}
}

func TestScanEvidenceStoreRestartReclaimsOnlyExplicitEmptyReservation(t *testing.T) {
	for _, created := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "empty"}[created], func(t *testing.T) {
			options := scanEvidenceTestOptions(t)
			manager := scanEvidenceTestManager(t, options)
			name := "scan-evidence-" + strings.Repeat("1", 32)
			if err := manager.disk.reserve(name, 1<<30); err != nil {
				t.Fatal(err)
			}
			if created {
				if err := manager.disk.root.Mkdir(name, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.disk.close(); err != nil {
				t.Fatal(err)
			}
			successor := scanEvidenceTestManager(t, options)
			defer func() {
				if err := successor.close(); err != nil {
					t.Error(err)
				}
			}()
			if len(successor.disk.document.Leases) != 0 {
				t.Fatal("confirmed empty reservation was not retired")
			}
			if _, err := os.Stat(filepath.Join(options.Directory, name)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("old reserved directory remains")
			}
		})
	}
}

func TestScanEvidenceStoreRestartRejectsUnexportableNonemptyOrphan(t *testing.T) {
	options := scanEvidenceTestOptions(t)
	manager := scanEvidenceTestManager(t, options)
	name := "scan-evidence-" + strings.Repeat("2", 32)
	if err := manager.disk.reserve(name, 1<<30); err != nil {
		t.Fatal(err)
	}
	if err := manager.disk.root.Mkdir(name, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := manager.disk.root.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.disk.created(name, info); err != nil {
		t.Fatal(err)
	}
	document := manager.disk.document
	document.Leases[0].HandleType, document.Leases[0].Handle = 0, nil
	if err := manager.disk.writeDocument(document); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(options.Directory, name, strings.Repeat("3", 64)+".dir")
	if err := os.WriteFile(record, []byte("retained evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := manager.disk.close(); err != nil {
		t.Fatal(err)
	}
	if successor, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, nil); err == nil {
		_ = successor.close()
		t.Fatal("inode-only evidence authorized destructive restart cleanup")
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatal("unprovable orphan evidence was deleted")
	}
}

func TestScanEvidenceStoreRejectsAliasesForeignOwnersAndUnknownFiles(t *testing.T) {
	options := scanEvidenceTestOptions(t)
	alias := filepath.Join(t.TempDir(), "media-alias")
	if err := os.Symlink(options.Directory, alias); err != nil {
		t.Fatal(err)
	}
	if manager, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, []string{alias}); err == nil {
		_ = manager.close()
		t.Fatal("media alias overlapped the evidence parent")
	}
	manager := scanEvidenceTestManager(t, options)
	if err := manager.close(); err != nil {
		t.Fatal(err)
	}
	if other, err := newScanEvidenceManager(context.Background(), strings.Repeat("b", 64), options, nil); err == nil {
		_ = other.close()
		t.Fatal("another catalog adopted the private spool")
	}
	foreign := filepath.Join(options.Directory, "scan-evidence-"+strings.Repeat("f", 32))
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	if other, err := newScanEvidenceManager(context.Background(), strings.Repeat("a", 64), options, nil); err == nil {
		_ = other.close()
		t.Fatal("an unregistered prefixed directory was adopted")
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatal("unknown prefixed directory was removed")
	}
}
