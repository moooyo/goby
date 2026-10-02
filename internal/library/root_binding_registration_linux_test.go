//go:build linux

package library

import (
	"context"
	"errors"
	"fmt"
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

func TestRootBindingRegistrationSuppliedSymlinkTransfersBetweenConfiguredAnchors(t *testing.T) {
	store, approved, _ := rootBindingRegistrationDirectory(t)
	second := t.TempDir()
	registered := filepath.Join(second, "registered")
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	store.roots = append(store.roots, approvedRoot{path: second})
	alias := filepath.Join(approved, "cross-anchor-alias")
	if err := os.Symlink(registered, alias); err != nil {
		t.Fatal(err)
	}
	registration, err := store.authorizePath(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	if registration.root.path != registered || registration.root.allowedPath != second || registration.root.relativePath != "registered" {
		t.Fatalf("supplied cross-anchor alias lost its previously authorized target: %+v", registration.root)
	}
	if err := registration.prepare(context.Background(), rootBindingRegistrationUnsupported); err != nil {
		t.Fatal(err)
	}
	if err := registration.RevalidateQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRootBindingRegistrationCrossAnchorRedirectDoesNotProbeTarget(t *testing.T) {
	_, approved, _ := rootBindingRegistrationDirectory(t)
	missing := filepath.Join(t.TempDir(), "unavailable-target")
	alias := filepath.Join(approved, "unavailable-cross-anchor")
	if err := os.Symlink(missing, alias); err != nil {
		t.Fatal(err)
	}
	anchor, err := os.OpenRoot(approved)
	if err != nil {
		t.Fatal(err)
	}
	defer anchor.Close()
	_, err = resolveApprovedDirectoryName(anchor, approved, "unavailable-cross-anchor", maxDirectorySymlinkTraversals)
	redirect, ok := directoryPrimaryRedirect(err)
	if !ok || redirect.path != missing || redirect.spent != 1 {
		t.Fatalf("source-anchor resolution probed the unavailable target instead of transferring before I/O: %v", err)
	}
}

func TestRootBindingRegistrationSuppliedAliasKeepsNarrowestConfiguredBoundary(t *testing.T) {
	store, approved, path := rootBindingRegistrationDirectory(t)
	store.roots = []approvedRoot{{path: path}, {path: approved}}
	alias := filepath.Join(approved, "narrow-alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	registration, err := store.authorizePath(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	if registration.root.allowedPath != path || registration.root.relativePath != "." || registration.root.path != path {
		t.Fatalf("canonical alias widened the configured authorization boundary: %+v", registration.root)
	}
	if err := registration.prepare(context.Background(), rootBindingRegistrationUnsupported); err != nil {
		t.Fatal(err)
	}
	if err := registration.RevalidateQueued(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRootBindingRegistrationSuppliedSymlinkBudgetMatchesEvalSymlinks(t *testing.T) {
	for _, test := range []struct {
		name        string
		links       int
		crossAnchor bool
		extraTarget bool
		wantError   bool
	}{
		{name: "same-anchor-41", links: 41},
		{name: "same-anchor-255", links: 255},
		{name: "same-anchor-256", links: 256, wantError: true},
		{name: "cross-anchor-final-255", links: 255, crossAnchor: true},
		{name: "cross-anchor-shared-256", links: 255, crossAnchor: true, extraTarget: true, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, approved, registered := rootBindingRegistrationDirectory(t)
			allowed, target := approved, registered
			if test.crossAnchor {
				allowed = t.TempDir()
				registered = filepath.Join(allowed, "registered")
				if err := os.Mkdir(registered, 0o700); err != nil {
					t.Fatal(err)
				}
				store.roots = append(store.roots, approvedRoot{path: allowed})
				target = registered
				if test.extraTarget {
					target = filepath.Join(allowed, "target-link")
					if err := os.Symlink("registered", target); err != nil {
						t.Fatal(err)
					}
				}
			}
			first := rootBindingRegistrationSymlinkChain(t, approved, test.links, target)
			registration, err := store.authorizePath(first)
			if test.wantError {
				if registration != nil || !errors.Is(err, ErrUnavailable) {
					t.Fatalf("the 256th link escaped the shared bounded budget: registration=%v err=%v", registration, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer registration.Close()
			if registration.root.path != registered || registration.root.allowedPath != allowed || registration.root.relativePath != "registered" {
				t.Fatalf("a previously valid bounded link chain lost its authorized target: %+v", registration.root)
			}
		})
	}
}

func rootBindingRegistrationSymlinkChain(t *testing.T, approved string, count int, target string) string {
	t.Helper()
	for index := count - 1; index >= 0; index-- {
		name := filepath.Join(approved, fmt.Sprintf("link-%03d", index))
		destination := target
		if index+1 < count {
			destination = fmt.Sprintf("link-%03d", index+1)
		}
		if err := os.Symlink(destination, name); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(approved, "link-000")
}
