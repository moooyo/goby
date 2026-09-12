package library

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') || !filepath.IsAbs(path) || hasTraversal(path) {
		return nil, fmt.Errorf("%w: media paths must be absolute directories without traversal", ErrInvalidInput)
	}
	// Resolve the administrator-supplied name before deriving a relative path.
	// A new registration retains its own current configured anchor. Existing
	// registrations continue using their previously admitted shared or root anchor.
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory cannot be resolved", ErrUnavailable)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	var allowedPath string
	for _, approved := range s.roots {
		if !pathWithin(approved.path, canonical) {
			continue
		}
		allowedPath = strings.Clone(approved.path)
		break
	}
	s.mu.Unlock()
	if allowedPath == "" {
		return nil, fmt.Errorf("%w: media directory is outside configured roots", ErrForbidden)
	}
	relative, err := filepath.Rel(allowedPath, canonical)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory is outside the approved root", ErrInvalidInput)
	}
	approved := approvedRoot{path: allowedPath}
	if err := openApprovedRoot(&approved); err != nil {
		return nil, err
	}
	lease := &libraryRootLease{approved: approved.root, relativePath: relative}
	registered, err := lease.Open()
	if err != nil {
		_ = lease.Close()
		return nil, err
	}
	return &rootBindingRegistration{root: libraryRoot{path: canonical, allowedPath: allowedPath, relativePath: relative},
		lease: lease, registered: registered}, nil
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
	s.mu.Lock()
	defer s.mu.Unlock()
	approved, err := s.approvedLibraryRootLocked(root)
	if err != nil {
		return nil, err
	}
	opened, err := openRegisteredRoot(approved, root.relativePath)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
	}
	return opened, nil
}

// The caller holds Store.mu while finding or initializing the approved anchor.
func (s *Store) approvedLibraryRootLocked(root libraryRoot) (*os.Root, error) {
	if s.closed {
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
		if err := openApprovedRoot(approved); err != nil {
			return nil, err
		}
		return approved.root, nil
	}
	return nil, fmt.Errorf("%w: media directory is no longer configured", ErrUnavailable)
}

// A publication lease owns a clone of the approved anchor, not the registered
// directory. Each Open rechecks the registered name chain without Store.mu,
// which must never be acquired while an owned transaction holds ownership.mu.
type libraryRootLease struct {
	approved     *os.Root
	relativePath string
}

func (s *Store) leaseLibraryRoot(root libraryRoot) (*libraryRootLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	approved, err := s.approvedLibraryRootLocked(root)
	if err != nil {
		return nil, err
	}
	held, err := approved.OpenRoot(".")
	if err != nil {
		return nil, fmt.Errorf("%w: approved media anchor cannot be retained", ErrUnavailable)
	}
	return &libraryRootLease{approved: held, relativePath: strings.Clone(root.relativePath)}, nil
}

func (lease *libraryRootLease) Open() (*os.Root, error) {
	opened, err := openRegisteredRoot(lease.approved, lease.relativePath)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
	}
	return opened, nil
}

func (lease *libraryRootLease) Close() error {
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
