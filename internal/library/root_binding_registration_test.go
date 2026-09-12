package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func rootBindingRegistrationDirectory(t *testing.T) (*Store, string, string) {
	t.Helper()
	approved := filepath.Join(t.TempDir(), "approved")
	registered := filepath.Join(approved, "registered")
	if err := os.MkdirAll(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(registered, "marker.txt"), []byte("authorized directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(approved)
	if err != nil {
		t.Fatal(err)
	}
	approved, registered = canonical, filepath.Join(canonical, "registered")
	return &Store{roots: []approvedRoot{{path: approved}}}, approved, registered
}

func rootBindingRegistrationUnsupported(context.Context, *libraryRootLease, RootTopologyMapping, *os.Root) (rootBindingRegistrationTopology, error) {
	return nil, fmt.Errorf("unsupported directory identity: %w: %w", ErrRootTopologyUnavailable, ErrRootStorageIdentityUnsupported)
}

func TestRootBindingRegistrationUnsupportedRetainsAuthorizedDirectories(t *testing.T) {
	store, _, path := rootBindingRegistrationDirectory(t)
	registration, err := store.authorizePath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	if err := registration.prepare(context.Background(), rootBindingRegistrationUnsupported); err != nil {
		t.Fatal(err)
	}
	if registration.document != nil || registration.topology != nil || registration.anchor == nil {
		t.Fatal("unsupported identity either fabricated a binding or discarded the authorized anchor")
	}
	if store.roots[0].root != nil || len(store.rootBindingAnchors) != 0 {
		t.Fatal("preparing registration changed an existing Store anchor")
	}
	if err := registration.Revalidate(context.Background()); err != nil {
		t.Fatal(err)
	}
	approved, registered, prepared := registration.lease.approved, registration.registered, registration.anchor
	opened, err := openRegisteredRoot(prepared, registration.root.relativePath)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	rootBindingPathsAssertContents(t, opened, "authorized directory")
	if err := registration.Close(); err != nil {
		t.Fatal(err)
	}
	for _, held := range []*os.Root{approved, registered, prepared} {
		if _, err := held.Stat("."); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("registration did not close an owned directory: %v", err)
		}
	}
	// A consumer's independently opened directory outlives candidate cleanup.
	rootBindingPathsAssertContents(t, opened, "authorized directory")
}

func TestRootBindingRegistrationUnsupportedStillRejectsNamedReplacement(t *testing.T) {
	for _, target := range []string{"approved", "registered"} {
		t.Run(target, func(t *testing.T) {
			store, approved, path := rootBindingRegistrationDirectory(t)
			registration, err := store.authorizePath(path)
			if err != nil {
				t.Fatal(err)
			}
			defer registration.Close()
			if err := registration.prepare(context.Background(), rootBindingRegistrationUnsupported); err != nil {
				t.Fatal(err)
			}
			replaced := path
			if target == "approved" {
				replaced = approved
			}
			if err := os.Rename(replaced, replaced+"-original"); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := registration.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyChanged) || !errors.Is(err, ErrUnavailable) {
				t.Fatalf("unsupported identity accepted a different current directory: %v", err)
			}
			rootBindingPathsAssertContents(t, registration.registered, "authorized directory")
		})
	}
}

func TestRootBindingRegistrationAuthorizationPreservesInputAndConfigurationRules(t *testing.T) {
	store, approved, path := rootBindingRegistrationDirectory(t)
	outside := t.TempDir()
	for _, test := range []struct {
		name string
		path string
		want error
	}{
		{"empty", "", ErrInvalidInput},
		{"relative", "registered", ErrInvalidInput},
		{"traversal", approved + string(filepath.Separator) + ".." + string(filepath.Separator) + "approved", ErrInvalidInput},
		{"nul", path + "\x00", ErrInvalidInput},
		{"outside", outside, ErrForbidden},
		{"missing", filepath.Join(approved, "missing"), ErrUnavailable},
		{"regular file", filepath.Join(path, "marker.txt"), ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			registration, err := store.authorizePath(test.path)
			if registration != nil {
				_ = registration.Close()
				t.Fatal("invalid directory retained a registration candidate")
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("directory authorization returned %v, want %v", err, test.want)
			}
		})
	}
	// Store.New orders configuration this way: the narrowest containing root wins.
	store.roots = []approvedRoot{{path: path}, {path: approved}}
	registration, err := store.authorizePath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	if registration.root.allowedPath != path || registration.root.relativePath != "." {
		t.Fatal("new registration widened a nested configured root")
	}
	store.closed = true
	if rejected, err := store.authorizePath(path); rejected != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed Store authorized a new directory: %v", err)
	}
}

func TestRootBindingRegistrationCaptureRejectsCancellationAndInvalidMapping(t *testing.T) {
	store, _, path := rootBindingRegistrationDirectory(t)
	for _, reason := range []string{"cancelled", "mapping", "lease"} {
		t.Run(reason, func(t *testing.T) {
			registration, err := store.authorizePath(path)
			if err != nil {
				t.Fatal(err)
			}
			defer registration.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := ErrInvalidRootTopology
			switch reason {
			case "cancelled":
				cancel()
				want = context.Canceled
			case "mapping":
				registration.root.relativePath = "../registered"
			case "lease":
				registration.lease.relativePath = "different"
			}
			called := false
			err = registration.prepare(ctx, func(context.Context, *libraryRootLease, RootTopologyMapping, *os.Root) (rootBindingRegistrationTopology, error) {
				called = true
				return nil, ErrRootStorageIdentityUnsupported
			})
			if !errors.Is(err, want) || called || registration.anchor != nil || registration.document != nil {
				t.Fatalf("invalid registration reached capture or acquired approval: called=%t error=%v", called, err)
			}
		})
	}
}

func TestRootBindingRegistrationCapabilityFallbackPreservesCancellationAndLimits(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
			store, _, path := rootBindingRegistrationDirectory(t)
			registration, err := store.authorizePath(path)
			if err != nil {
				t.Fatal(err)
			}
			defer registration.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err = registration.prepare(ctx, func(context.Context, *libraryRootLease, RootTopologyMapping, *os.Root) (rootBindingRegistrationTopology, error) {
				if cancelled {
					cancel()
					return nil, ErrRootStorageIdentityUnsupported
				}
				return nil, ErrRootTopologyLimit
			})
			if cancelled {
				if !errors.Is(err, context.Canceled) || registration.anchor != nil {
					t.Fatalf("unsupported fallback swallowed request cancellation: %v", err)
				}
				return
			}
			if err != nil || registration.anchor == nil || registration.document != nil || registration.topology != nil {
				t.Fatalf("explicit topology limit did not preserve a complete unbound registration: %v", err)
			}
			if err := registration.Revalidate(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRootBindingRegistrationUnboundFallbackRejectsUnrelatedFailures(t *testing.T) {
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, ErrInvalidRootTopology,
		ErrRootTopologyChanged, ErrRootTopologyAmbiguous, ErrInvalidRootStorageIdentity,
		ErrRootStorageIdentityUnavailable, ErrUnavailable, ErrForbidden, ErrInvalidInput,
		os.ErrPermission, os.ErrNotExist, os.ErrClosed, errors.New("unclassified I/O failure"),
		&os.PathError{Op: "open", Path: "private-directory", Err: ErrRootStorageIdentityUnsupported}} {
		for _, capability := range []error{ErrRootStorageIdentityUnsupported, ErrRootTopologyLimit} {
			if rootBindingRegistrationUnboundError(errors.Join(capability, failure)) {
				t.Errorf("unbound fallback swallowed %v", failure)
			}
		}
	}
	for _, failure := range []error{nil, ErrRootTopologyUnavailable, errors.New("unexpected observation failure")} {
		if rootBindingRegistrationUnboundError(failure) {
			t.Errorf("unbound fallback accepted an unclassified failure: %v", failure)
		}
	}
	for _, capability := range []error{ErrRootStorageIdentityUnsupported, ErrRootTopologyLimit} {
		if !rootBindingRegistrationUnboundError(fmt.Errorf("bounded observation: %w: %w", ErrRootTopologyUnavailable, capability)) {
			t.Errorf("explicit capability classification was lost: %v", capability)
		}
	}
}

func TestRootBindingRegistrationActorIdentifiesUsersApplicationsAndSystem(t *testing.T) {
	application := identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 9007199254740993,
		SessionID: "application-credential", ClientSessionID: "application-client"}
	for _, test := range []struct {
		name          string
		administrator *catalogAdministrator
		want          string
	}{
		{"system", nil, "system"},
		{"native", &catalogAdministrator{actor: identity.Principal{User: identity.User{ID: "native-user"}}, audience: identity.AdministratorNative}, "native-user"},
		{"Emby", &catalogAdministrator{actor: identity.Principal{User: identity.User{ID: "emby-user"}}, audience: identity.AdministratorEmby}, "emby-user"},
		{"application", &catalogAdministrator{actor: application, audience: identity.AdministratorEmby}, "application_key:9007199254740993"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id, err := rootBindingRegistrationActorID(test.administrator)
			if err != nil || id != test.want || !validRootBindingActorID(id) {
				t.Fatalf("initial binding actor = %q, %v; want %q", id, err, test.want)
			}
		})
	}
	for _, id := range []string{"", "/private/administrator", "administrator\n"} {
		if _, err := rootBindingRegistrationActorID(&catalogAdministrator{actor: identity.Principal{User: identity.User{ID: id}}}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("malformed initial binding actor was accepted: %v", err)
		}
	}
}
