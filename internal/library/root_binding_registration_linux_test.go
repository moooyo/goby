//go:build linux

package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRootBindingRegistrationUnsupportedKernelFailuresHaveNoIOFallback(t *testing.T) {
	for _, errno := range []error{unix.ENOTTY, unix.EOPNOTSUPP, unix.ENOSYS, unix.EOVERFLOW} {
		if !rootBindingRegistrationUnboundError(rootStorageOperationError("identity observation", errno)) {
			t.Errorf("adapter's explicit unsupported failure was rejected: %v", errno)
		}
	}
	for _, errno := range []error{unix.EIO, unix.EACCES, unix.EPERM, unix.EBADF, unix.ENOENT, unix.ENOTDIR} {
		for _, capability := range []error{ErrRootStorageIdentityUnsupported, ErrRootTopologyLimit} {
			if rootBindingRegistrationUnboundError(errors.Join(capability, errno)) {
				t.Errorf("a capability sentinel hid a different kernel failure: %v", errno)
			}
		}
	}
	if rootBindingRegistrationUnboundError(errors.Join(ErrRootTopologyLimit, unix.ENOTTY)) {
		t.Fatal("a topology limit invented an unsupported identity classification for a syscall")
	}
}

func TestRootBindingRegistrationUnsupportedRejectsLaterSymlinks(t *testing.T) {
	for _, target := range []string{"approved", "registered"} {
		t.Run(target, func(t *testing.T) {
			store, approved, registered := rootBindingRegistrationDirectory(t)
			registration, err := store.authorizePath(registered)
			if err != nil {
				t.Fatal(err)
			}
			defer registration.Close()
			if err := registration.prepare(context.Background(), rootBindingRegistrationUnsupported); err != nil {
				t.Fatal(err)
			}
			path := registered
			if target == "approved" {
				path = approved
			}
			original := path + "-original"
			if err := os.Rename(path, original); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(original, path); err != nil {
				t.Fatal(err)
			}
			if err := registration.Revalidate(context.Background()); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unsupported registration followed a later %s symlink: %v", target, err)
			}
			rootBindingPathsAssertContents(t, registration.registered, "authorized directory")
		})
	}
}

func TestRootBindingRegistrationAuthorizationResolvesSuppliedSymlinksBeforeContainment(t *testing.T) {
	store, approved, registered := rootBindingRegistrationDirectory(t)
	alias := filepath.Join(approved, "supplied-alias")
	if err := os.Symlink(registered, alias); err != nil {
		t.Fatal(err)
	}
	registration, err := store.authorizePath(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	if registration.root.path != registered || registration.root.relativePath != "registered" {
		t.Fatal("registration persisted an administrator-supplied symlink instead of its canonical directory")
	}
	if err := registration.prepare(context.Background(), rootBindingRegistrationUnsupported); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(approved, "outside-alias")
	if err := os.Symlink(t.TempDir(), escape); err != nil {
		t.Fatal(err)
	}
	if rejected, err := store.authorizePath(escape); rejected != nil || !errors.Is(err, ErrForbidden) {
		t.Fatalf("supplied symlink widened configured directory authorization: %v", err)
	}
}
