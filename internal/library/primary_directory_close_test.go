package library

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestDirectoryPrimaryRootCloseRetiresConcreteDescriptor(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := closeDirectoryPrimaryRoot(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("concrete root remained open after synchronous retirement: %v", err)
	}
}

func TestDirectoryPrimaryWarmReleaseKeepsOtherPinUntilFinalRetirement(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	s := &Store{}
	s.mu.Lock()
	leaseReference := s.borrowRootAnchorLocked(root)
	openingReference := s.borrowRootAnchorLocked(root)
	s.retireRootAnchorLocked(root)
	s.mu.Unlock()
	lease := &warmMediaSourceRoot{store: s, reference: leaseReference}
	if err := lease.releasePrimary(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("."); err != nil {
		t.Fatalf("warm lease release closed the concurrent opening's descriptor: %v", err)
	}
	if err := lease.releasePrimary(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.releasePrimaryRootAnchor(context.Background(), openingReference); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("last retired pin kept its descriptor open: %v", err)
	}
	s.mu.Lock()
	remaining := len(s.rootAnchorReferences)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("retired root remained registered: %d", remaining)
	}
}
