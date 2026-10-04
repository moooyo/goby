package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moooyo/goby/internal/primaryio"
)

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func hasTraversal(path string) bool {
	for _, component := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if component == ".." {
			return true
		}
	}
	return false
}

func (s *Store) authorizePath(path string) (*rootBindingRegistration, error) {
	return s.authorizePathContext(context.Background(), path)
}

func (s *Store) authorizePathContext(ctx context.Context, path string) (*rootBindingRegistration, error) {
	if ctx == nil {
		return nil, ErrInvalidInput
	}
	remaining := maxDirectorySymlinkTraversals
	for {
		anchor, err := s.configuredPrimaryAnchorForPath(path)
		if err != nil {
			return nil, err
		}
		operation, err := s.prepareConfiguredPrimaryRootIO(ctx, anchor)
		if err != nil {
			return nil, err
		}
		var registration *rootBindingRegistration
		err = operation.Run(ctx, "", primaryio.Foreground, func(work context.Context) error {
			var err error
			registration, err = s.authorizePathAtAnchor(work, path, anchor, remaining)
			return err
		})
		if redirect, ok := directoryPrimaryRedirect(err); ok {
			if err := operation.Close(); err != nil {
				return nil, err
			}
			remaining -= redirect.spent
			if remaining < 0 {
				return nil, fmt.Errorf("%w: media directory has too many symbolic links", ErrUnavailable)
			}
			path = redirect.path
			continue
		}
		if err != nil {
			return nil, errors.Join(err, operation.Close())
		}
		registration.primaryIO = operation
		return registration, nil
	}
}

func (s *Store) authorizePathObserved(ctx context.Context, path string) (*rootBindingRegistration, error) {
	anchor, err := s.configuredPrimaryAnchorForPath(path)
	if err != nil {
		return nil, err
	}
	return s.authorizePathAtAnchor(ctx, path, anchor, directoryResolutionBudget(ctx))
}

func (s *Store) authorizePathAtAnchor(ctx context.Context, path, allowedPath string, remaining int) (*rootBindingRegistration, error) {
	if strings.TrimSpace(path) == "" || len(path) > 4096 || strings.ContainsRune(path, '\x00') || !filepath.IsAbs(path) || hasTraversal(path) {
		return nil, fmt.Errorf("%w: media paths must be absolute directories without traversal", ErrInvalidInput)
	}
	relative, err := filepath.Rel(allowedPath, path)
	if err != nil || !pathWithin(allowedPath, path) {
		return nil, fmt.Errorf("%w: media directory is outside the approved root", ErrInvalidInput)
	}
	approved := approvedRoot{path: allowedPath}
	if err := openApprovedRoot(&approved); err != nil {
		return nil, err
	}
	// Resolve supplied symlinks through the already admitted configured anchor.
	// Reading an outside target to choose a different domain is never allowed.
	relative, err = resolveApprovedDirectoryName(approved.root, allowedPath, relative, remaining)
	if err != nil {
		closeErr := closeDirectoryPrimaryResource(ctx, approved.root.Close)
		return nil, errors.Join(err, closeErr)
	}
	canonical := filepath.Join(allowedPath, relative)
	authorizedPath, err := s.configuredAuthorizationAnchorForPath(canonical)
	if err != nil {
		return nil, errors.Join(err, closeDirectoryPrimaryResource(ctx, approved.root.Close))
	}
	if authorizedPath != allowedPath {
		anchorRelative, err := filepath.Rel(allowedPath, authorizedPath)
		if err != nil {
			return nil, errors.Join(ErrUnavailable, closeDirectoryPrimaryResource(ctx, approved.root.Close))
		}
		narrowed, openErr := openRegisteredRoot(approved.root, anchorRelative)
		closeErr := closeDirectoryPrimaryResource(ctx, approved.root.Close)
		if openErr != nil || closeErr != nil {
			if narrowed != nil {
				closeErr = errors.Join(closeErr, closeDirectoryPrimaryResource(ctx, narrowed.Close))
			}
			return nil, errors.Join(ErrUnavailable, openErr, closeErr)
		}
		approved.root = narrowed
		allowedPath = authorizedPath
		relative, err = filepath.Rel(allowedPath, canonical)
		if err != nil {
			return nil, errors.Join(ErrUnavailable, closeDirectoryPrimaryResource(ctx, approved.root.Close))
		}
	}
	lease := &libraryRootLease{approved: approved.root, relativePath: relative}
	registered, err := lease.Open()
	if err != nil {
		return nil, errors.Join(err, closeDirectoryPrimaryResource(ctx, lease.Close))
	}
	return &rootBindingRegistration{root: libraryRoot{path: canonical, allowedPath: allowedPath, relativePath: relative},
		lease: lease, registered: registered}, nil
}

func resolveApprovedDirectoryName(approved *os.Root, allowedPath, relative string, remaining int) (string, error) {
	resolved := make([]string, 0)
	pending := strings.Split(filepath.Clean(relative), string(filepath.Separator))
	symlinks := 0
	for len(pending) != 0 {
		component := pending[0]
		pending = pending[1:]
		if component == "" || component == "." {
			continue
		}
		name := filepath.Join(append(append([]string(nil), resolved...), component)...)
		info, err := approved.Lstat(name)
		if err != nil {
			return "", fmt.Errorf("%w: media directory cannot be resolved safely", ErrUnavailable)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.IsDir() {
				return "", fmt.Errorf("%w: media directory is not an available directory", ErrUnavailable)
			}
			resolved = append(resolved, component)
			continue
		}
		symlinks++
		if symlinks > remaining {
			return "", fmt.Errorf("%w: media directory has too many symbolic links", ErrUnavailable)
		}
		target, err := approved.Readlink(name)
		if err != nil {
			return "", fmt.Errorf("%w: media directory cannot be resolved safely", ErrUnavailable)
		}
		if len(target) > 4096 || strings.ContainsRune(target, '\x00') {
			return "", fmt.Errorf("%w: media directory link is outside its observation budget", ErrUnavailable)
		}
		if filepath.IsAbs(target) {
			if !pathWithin(allowedPath, target) {
				redirected := filepath.Join(append([]string{target}, pending...)...)
				return "", &primaryDirectoryRedirect{path: redirected, spent: symlinks}
			}
			target, err = filepath.Rel(allowedPath, target)
		} else {
			target = filepath.Join(filepath.Dir(name), target)
		}
		target = filepath.Clean(target)
		if err != nil || len(target) > 4096 || filepath.IsAbs(target) || hasTraversal(target) {
			redirected := filepath.Join(append([]string{filepath.Join(allowedPath, target)}, pending...)...)
			return "", &primaryDirectoryRedirect{path: redirected, spent: symlinks}
		}
		resolved = resolved[:0]
		pending = append(strings.Split(target, string(filepath.Separator)), pending...)
	}
	if len(resolved) == 0 {
		return ".", nil
	}
	return filepath.Join(resolved...), nil
}

// openApprovedRoot retains the initial configured anchor for roots without an
// explicit binding override. Relative opens prevent pathname swaps from
// redirecting access outside the directory that was actually approved.
func openApprovedRoot(approved *approvedRoot) error {
	if approved.root != nil {
		return nil
	}
	canonical, err := filepath.EvalSymlinks(approved.path)
	if err != nil || filepath.Clean(canonical) != approved.path {
		return fmt.Errorf("%w: configured root cannot be resolved safely", ErrUnavailable)
	}
	before, err := os.Stat(canonical)
	if err != nil || !before.IsDir() {
		return fmt.Errorf("%w: configured root is not an available directory", ErrUnavailable)
	}
	root, err := os.OpenRoot(canonical)
	if err != nil {
		return fmt.Errorf("%w: configured root cannot be opened", ErrUnavailable)
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return fmt.Errorf("%w: configured root changed while opening", ErrUnavailable)
	}
	approved.root = root
	return nil
}

func (s *Store) openLibraryRoot(root libraryRoot) (*os.Root, error) {
	return s.withLibraryRootAnchor(root, func(approved *os.Root) (*os.Root, error) {
		opened, err := openRegisteredRoot(approved, root.relativePath)
		if err != nil {
			return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
		}
		return opened, nil
	})
}

// Keep admission and descriptor retention in the same short memory-only
// critical section. The borrowed generation remains usable across replacement
// while every filesystem operation, including lazy initialization, runs outside
// Store.mu. The private callback permits deterministic slow-storage tests.
func (s *Store) withLibraryRootAnchor(root libraryRoot, open func(*os.Root) (*os.Root, error)) (*os.Root, error) {
	return s.withLibraryRootAnchorCapture(root, openApprovedRoot, open)
}

func (s *Store) withLibraryRootAnchorCapture(root libraryRoot, capture func(*approvedRoot) error, open func(*os.Root) (*os.Root, error)) (*os.Root, error) {
	s.mu.Lock()
	approved, err := s.approvedLibraryRootLocked(root)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.rootOpens.Add(1)
	defer s.rootOpens.Done()
	if approved != nil {
		reference := s.borrowRootAnchorLocked(approved)
		s.mu.Unlock()
		defer s.releaseRootAnchor(reference)
		return open(approved)
	}
	s.mu.Unlock()

	// More than one first opener may prepare an anchor. Only the first current
	// candidate is installed; the others use that winner or a newer explicit
	// binding, and close their own unused candidates without holding the mutex.
	candidate := approvedRoot{path: root.allowedPath}
	defer func() {
		if candidate.root != nil {
			_ = candidate.root.Close()
		}
	}()
	if err := capture(&candidate); err != nil {
		return nil, err
	}
	if candidate.root == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	approved, err = s.approvedLibraryRootLocked(root)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if approved == nil {
		for index := range s.roots {
			if s.roots[index].path == root.allowedPath {
				s.roots[index].root = candidate.root
				approved, candidate.root = candidate.root, nil
				break
			}
		}
	}
	reference := s.borrowRootAnchorLocked(approved)
	s.mu.Unlock()
	defer s.releaseRootAnchor(reference)
	return open(approved)
}

// The caller holds Store.mu. A nil descriptor requests lazy initialization;
// this lookup never opens or closes storage while holding the admission mutex.
func (s *Store) approvedLibraryRootLocked(root libraryRoot) (*os.Root, error) {
	if s.closed || s.closing.Load() {
		return nil, ErrUnavailable
	}
	if hasTraversal(root.relativePath) || filepath.IsAbs(root.relativePath) {
		return nil, fmt.Errorf("%w: stored media directory is outside the approved root", ErrForbidden)
	}
	bound, rebound := s.rootBindingAnchors[root.id]
	if rebound && (bound.root != root || bound.approved == nil) {
		return nil, fmt.Errorf("%w: registered media directory no longer matches its approved binding", ErrUnavailable)
	}
	for index := range s.roots {
		approved := &s.roots[index]
		if approved.path != root.allowedPath {
			continue
		}
		if rebound {
			return bound.approved, nil
		}
		return approved.root, nil
	}
	return nil, fmt.Errorf("%w: media directory is no longer configured", ErrUnavailable)
}

// A publication lease owns a clone of the approved anchor, not the registered
// directory. Each Open rechecks the registered name chain without Store.mu,
// which transaction callbacks must not acquire to perform filesystem work.
type libraryRootLease struct {
	approved           *os.Root
	relativePath       string
	expectedRoot       *os.Root
	expectedAnchorPath string
}

func (s *Store) leaseLibraryRoot(root libraryRoot) (*libraryRootLease, error) {
	held, err := s.withLibraryRootAnchor(root, func(approved *os.Root) (*os.Root, error) {
		held, err := approved.OpenRoot(".")
		if err != nil {
			return nil, fmt.Errorf("%w: approved media anchor cannot be retained", ErrUnavailable)
		}
		return held, nil
	})
	if err != nil {
		return nil, err
	}
	return &libraryRootLease{approved: held, relativePath: strings.Clone(root.relativePath)}, nil
}

func (lease *libraryRootLease) Open() (*os.Root, error) {
	if lease.expectedAnchorPath != "" {
		if err := checkTaskSourceAnchorName(lease.expectedAnchorPath, lease.approved); err != nil {
			return nil, err
		}
	}
	opened, err := openRegisteredRoot(lease.approved, lease.relativePath)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
	}
	if lease.expectedRoot != nil {
		info, err := opened.Stat(".")
		held, heldErr := lease.expectedRoot.Stat(".")
		if err != nil || heldErr != nil || !os.SameFile(held, info) {
			return nil, errors.Join(ErrSourceChanged, err, heldErr, opened.Close())
		}
	}
	if lease.expectedAnchorPath != "" {
		if err := checkTaskSourceAnchorName(lease.expectedAnchorPath, lease.approved); err != nil {
			return nil, errors.Join(err, opened.Close())
		}
	}
	return opened, nil
}

func (lease *libraryRootLease) Close() error {
	if lease.expectedRoot != nil {
		return errors.Join(lease.expectedRoot.Close(), lease.approved.Close())
	}
	return lease.approved.Close()
}

// Open each registered path component from its already opened parent. A later
// symlink to a sibling library must not silently widen this library's contents,
// even when both libraries happen to share the same configured storage root.
func openRegisteredRoot(approved *os.Root, relative string) (*os.Root, error) {
	if relative == "." || relative == "" {
		return approved.OpenRoot(".")
	}
	current := approved
	for _, component := range strings.Split(filepath.Clean(relative), string(filepath.Separator)) {
		before, err := current.Lstat(component)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			if current != approved {
				_ = current.Close()
			}
			return nil, fmt.Errorf("registered media directory changed or contains a symlink")
		}
		next, err := current.OpenRoot(component)
		if current != approved {
			_ = current.Close()
		}
		if err != nil {
			return nil, err
		}
		after, err := next.Stat(".")
		if err != nil || !os.SameFile(before, after) {
			_ = next.Close()
			return nil, fmt.Errorf("registered media directory changed while opening")
		}
		current = next
	}
	return current, nil
}
