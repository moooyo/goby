package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/moooyo/goby/internal/storagebinding"
)

// Registration owns the authorized directories independently of the Store's
// shared anchors. Unsupported storage can remain unbound without releasing the
// directories whose safe names were accepted for this registration.
type rootBindingRegistration struct {
	root       libraryRoot
	lease      *libraryRootLease
	registered *os.Root
	topology   rootBindingRegistrationTopology
	document   []byte
	anchor     *os.Root
}

// The private factory permits observation-boundary tests while every caller
// still passes through ordinary directory authorization and retained-name checks.
type rootBindingRegistrationCaptureFactory func(context.Context, *libraryRootLease, RootTopologyMapping, *os.Root) (rootBindingRegistrationTopology, error)

type rootBindingRegistrationTopology interface {
	Snapshot() (RootTopologySnapshot, error)
	Revalidate(context.Context) error
	Close() error
}

func captureRootBindingRegistrationTopology(ctx context.Context, lease *libraryRootLease, mapping RootTopologyMapping, registered *os.Root) (rootBindingRegistrationTopology, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Persisted topology mappings use Linux path syntax. Ordinary authorization
	// has already validated this host's paths before an unsupported platform is
	// classified; Windows paths must not be mistaken for corrupt Linux mappings.
	if runtime.GOOS != "linux" {
		return nil, ErrRootStorageIdentityUnsupported
	}
	capture, err := lease.CaptureTopology(ctx, mapping, registered)
	if err != nil {
		return nil, err
	}
	return capture, nil
}

func (registration *rootBindingRegistration) prepare(ctx context.Context, captureRoot rootBindingRegistrationCaptureFactory) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if captureRoot == nil || registration == nil || registration.lease == nil || registration.lease.approved == nil || registration.registered == nil {
		return ErrInvalidInput
	}
	if err := registration.validateMapping(); err != nil {
		return err
	}
	mapping := RootTopologyMapping{ApprovedPath: registration.root.allowedPath, RegisteredPath: registration.root.path}
	topology, err := captureRoot(ctx, registration.lease, mapping, registration.registered)
	if err != nil {
		if topology != nil {
			_ = topology.Close()
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if !rootBindingRegistrationUnboundError(err) {
			return rootBindingRegistrationError(err)
		}
	} else {
		if topology == nil {
			return ErrUnavailable
		}
		registration.topology = topology
		if err := ctx.Err(); err != nil {
			return err
		}
		snapshot, err := topology.Snapshot()
		if err != nil {
			return rootBindingRegistrationError(err)
		}
		if snapshot.Mapping != mapping {
			return rootBindingRegistrationError(ErrInvalidRootTopology)
		}
		registration.document, err = storagebinding.EncodeSnapshot(snapshot.Clone())
		if err != nil {
			return rootBindingRegistrationError(err)
		}
	}
	// Clone before catalog admission. Publishing this exact descriptor after
	// commit cannot accidentally accept a subsequent pathname replacement.
	registration.anchor, err = registration.lease.approved.OpenRoot(".")
	if err != nil {
		return rootBindingRegistrationError(err)
	}
	return nil
}

// Only explicit capability and observation-size limits permit an unbound
// registration. A limit is its own sentinel throughout the topology adapter;
// ambiguity, malformed mappings, unstable paths, and I/O failures stay errors.
func rootBindingRegistrationUnboundError(err error) bool {
	unsupported := errors.Is(err, ErrRootStorageIdentityUnsupported)
	if !unsupported && !errors.Is(err, ErrRootTopologyLimit) {
		return false
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return false
	}
	return rootBindingRegistrationUnboundCause(err, unsupported, 32)
}

// Every leaf must belong to the adapter's explicit unsupported/limit taxonomy.
// A joined I/O or cancellation error cannot be hidden by a capability sentinel.
func rootBindingRegistrationUnboundCause(err error, unsupported bool, remaining int) bool {
	if remaining == 0 {
		return false
	}
	switch err {
	case ErrRootStorageIdentityUnsupported, ErrRootTopologyLimit, ErrRootTopologyUnavailable:
		return true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := wrapped.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !rootBindingRegistrationUnboundCause(cause, unsupported, remaining-1) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return rootBindingRegistrationUnboundCause(wrapped.Unwrap(), unsupported, remaining-1)
	default:
		return unsupported && rootBindingRegistrationUnsupportedKernelError(err)
	}
}

func rootBindingRegistrationError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.Join(ErrUnavailable, err)
}

func (registration *rootBindingRegistration) validateMapping() error {
	root := registration.root
	relative, err := filepath.Rel(root.allowedPath, root.path)
	if err != nil || !filepath.IsAbs(root.allowedPath) || !filepath.IsAbs(root.path) ||
		filepath.Clean(root.allowedPath) != root.allowedPath || filepath.Clean(root.path) != root.path ||
		!pathWithin(root.allowedPath, root.path) || hasTraversal(root.relativePath) || filepath.IsAbs(root.relativePath) ||
		relative != root.relativePath || registration.lease.relativePath != relative {
		return rootBindingRegistrationError(ErrInvalidRootTopology)
	}
	return nil
}

// Revalidation never consults Store.mu. Both supported and unbound roots must
// still resolve to their retained authorized directories. SameFile here is only
// a live transaction witness, never a substitute persisted storage identity.
func (registration *rootBindingRegistration) Revalidate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if registration == nil || registration.lease == nil || registration.lease.approved == nil || registration.registered == nil {
		return ErrUnavailable
	}
	if err := registration.validateMapping(); err != nil {
		return err
	}
	if registration.topology != nil {
		if err := registration.topology.Revalidate(ctx); err != nil {
			return rootBindingRegistrationError(err)
		}
	}
	approved := approvedRoot{path: registration.root.allowedPath}
	if err := openApprovedRoot(&approved); err != nil {
		return err
	}
	defer approved.root.Close()
	registered, err := openRegisteredRoot(approved.root, registration.root.relativePath)
	if err != nil {
		return rootBindingRegistrationError(err)
	}
	defer registered.Close()
	for _, pair := range [][2]*os.Root{{registration.lease.approved, approved.root}, {registration.registered, registered}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		held, err := pair[0].Stat(".")
		if err != nil {
			return rootBindingRegistrationError(err)
		}
		named, err := pair[1].Stat(".")
		if err != nil {
			return rootBindingRegistrationError(err)
		}
		if !held.IsDir() || !named.IsDir() || !os.SameFile(held, named) {
			return rootBindingRegistrationError(ErrRootTopologyChanged)
		}
	}
	return ctx.Err()
}

func (registration *rootBindingRegistration) Close() error {
	if registration == nil {
		return nil
	}
	var result error
	if registration.topology != nil {
		result = errors.Join(result, registration.topology.Close())
		registration.topology = nil
	}
	if registration.registered != nil {
		result = errors.Join(result, registration.registered.Close())
		registration.registered = nil
	}
	if registration.lease != nil {
		result = errors.Join(result, registration.lease.Close())
		registration.lease = nil
	}
	if registration.anchor != nil {
		result = errors.Join(result, registration.anchor.Close())
		registration.anchor = nil
	}
	return result
}

func rootBindingRegistrationActorID(administrator *catalogAdministrator) (string, error) {
	id := "system"
	if administrator != nil {
		id = administrator.actor.User.ID
		if administrator.actor.IsApplicationKey() {
			id = "application_key:" + strconv.FormatInt(administrator.actor.ApplicationKeyID, 10)
		}
	}
	if !validRootBindingActorID(id) {
		return "", fmt.Errorf("%w: root binding actor is invalid", ErrInvalidInput)
	}
	return id, nil
}

// A fresh read precedes filesystem work. The owned transaction still locks and
// rechecks the same credential before writes and after its final audit insert.
func (s *Store) checkLibraryRegistrationAdministrator(ctx context.Context, administrator *catalogAdministrator) error {
	if administrator == nil {
		return ctx.Err()
	}
	tx, err := s.beginRootBindingRead(ctx, administrator)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("complete library registration authority check: %w", err)
	}
	return nil
}
