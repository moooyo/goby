package library

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/moooyo/goby/internal/primaryio"
)

type primaryDirectoryImmediateKey struct{}
type primaryDirectoryResolutionBudgetKey struct{}

// Allow 255 walked links, matching filepath.EvalSymlinks.
// One budget is shared across every configured-anchor handoff.
const maxDirectorySymlinkTraversals = 255

type primaryDirectoryRedirect struct {
	path  string
	spent int
}

func (redirect *primaryDirectoryRedirect) Error() string {
	return "configured directory resolution requires another admitted anchor"
}

func directoryPrimaryRedirect(err error) (*primaryDirectoryRedirect, bool) {
	if redirect, ok := err.(*primaryDirectoryRedirect); ok {
		return redirect, true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 1 {
			return directoryPrimaryRedirect(causes[0])
		}
	}
	return nil, false
}

func directoryResolutionBudget(ctx context.Context) int {
	if remaining, ok := ctx.Value(primaryDirectoryResolutionBudgetKey{}).(int); ok {
		return remaining
	}
	return maxDirectorySymlinkTraversals
}

func closeDirectoryPrimaryResource(ctx context.Context, closer func() error) error {
	err := closeStorageObservationResources(closer)
	if err != nil {
		if operation := PrimaryRootIOFromContext(ctx); operation != nil {
			_ = operation.MarkUnknown(err)
		}
	}
	return err
}

// The marker changes only admission behavior. It carries no SQL handle or
// permission to perform I/O and is used solely after route preparation commits.
func immediateDirectoryPrimaryContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, primaryDirectoryImmediateKey{}, true)
}

func runDirectoryPrimaryPhase(ctx context.Context, rootID string, class primaryio.Class, work func(context.Context) error) error {
	operation := PrimaryRootIOFromContext(ctx)
	if operation == nil {
		// The descriptor-only evidence helpers remain usable by isolated unit
		// fixtures. Every Store entry point prepares an operation before calling
		// them, and transaction proof entry points reject a missing operation.
		return work(ctx)
	}
	if immediate, _ := ctx.Value(primaryDirectoryImmediateKey{}).(bool); immediate {
		return runDirectoryPrimaryImmediate(ctx, operation, rootID, class, work)
	}
	return operation.Run(ctx, rootID, class, work)
}

// configuredPrimaryAnchorForPath selects only an existing Store configuration.
// No filesystem result, client-provided identity, or path string becomes a
// governor key. Name resolution and containment are repeated after admission.
func (s *Store) configuredPrimaryAnchorForPath(path string) (string, error) {
	return s.selectConfiguredAnchorForPath(path, false)
}

// Authorization uses the most specific configured boundary independently from
// the broader governing anchor selected for admission. Routing is not scope.
func (s *Store) configuredAuthorizationAnchorForPath(path string) (string, error) {
	return s.selectConfiguredAnchorForPath(path, true)
}

func (s *Store) selectConfiguredAnchorForPath(path string, narrowest bool) (string, error) {
	if s == nil || !filepath.IsAbs(path) || hasTraversal(path) {
		return "", ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.closing.Load() {
		return "", ErrUnavailable
	}
	anchor := ""
	for _, configured := range s.roots {
		preferred := anchor == "" || narrowest && len(configured.path) > len(anchor) || !narrowest && len(configured.path) < len(anchor)
		if pathWithin(configured.path, path) && preferred {
			anchor = strings.Clone(configured.path)
		}
	}
	if anchor == "" {
		return "", ErrForbidden
	}
	return anchor, nil
}

func primaryDirectoryBusy(err error) bool {
	return errors.Is(err, primaryio.ErrBusy)
}

type primaryDirectoryRetry struct {
	operation *PrimaryRootIO
	rootID    string
	class     primaryio.Class
}

func (retry *primaryDirectoryRetry) Error() string { return primaryio.ErrBusy.Error() }
func (retry *primaryDirectoryRetry) Unwrap() error { return primaryio.ErrBusy }

func runDirectoryPrimaryImmediate(ctx context.Context, operation *PrimaryRootIO, rootID string, class primaryio.Class, work func(context.Context) error) error {
	err := operation.RunImmediate(ctx, rootID, class, work)
	if !primaryDirectoryBusy(err) {
		return err
	}
	retained, retainErr := operation.Fork()
	if retainErr != nil {
		return errors.Join(err, retainErr)
	}
	return &primaryDirectoryRetry{operation: retained, rootID: rootID, class: class}
}

func waitDirectoryPrimaryError(ctx context.Context, err error) (bool, error) {
	var retry *primaryDirectoryRetry
	if !errors.As(err, &retry) {
		return false, err
	}
	defer retry.operation.Close()
	if !directoryPrimaryRetryOnly(err) {
		return false, err
	}
	if err := waitDirectoryPrimaryRetry(ctx, retry.operation, retry.rootID, retry.class); err != nil {
		return true, err
	}
	return true, nil
}

func directoryPrimaryRetryOnly(err error) bool {
	if _, ok := err.(*primaryDirectoryRetry); ok {
		return true
	}
	if err == ErrUnavailable || err == errScanReconciliationEvidenceUnavailable {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if child != nil && !directoryPrimaryRetryOnly(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return directoryPrimaryRetryOnly(wrapped.Unwrap())
	}
	return false
}

// Wait only after the complete owned transaction has rolled back. The next
// transaction repeats its authority, catalog, and filesystem proofs in full.
func waitDirectoryPrimaryRetry(ctx context.Context, operation *PrimaryRootIO, rootID string, class primaryio.Class) error {
	if operation == nil {
		return ErrUnavailable
	}
	return operation.Run(ctx, rootID, class, func(context.Context) error { return nil })
}
