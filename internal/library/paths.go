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

func (s *Store) authorizePath(path string) (libraryRoot, error) {
	if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') || !filepath.IsAbs(path) || hasTraversal(path) {
		return libraryRoot{}, fmt.Errorf("%w: media paths must be absolute directories without traversal", ErrInvalidInput)
	}
	// Resolve the administrator-supplied name before deriving a relative path.
	// The subsequent open uses an already anchored approved root descriptor.
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return libraryRoot{}, fmt.Errorf("%w: media directory cannot be resolved", ErrUnavailable)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return libraryRoot{}, ErrUnavailable
	}
	for index := range s.roots {
		approved := &s.roots[index]
		if !pathWithin(approved.path, canonical) {
			continue
		}
		if err := openApprovedRoot(approved); err != nil {
			return libraryRoot{}, err
		}
		relative, err := filepath.Rel(approved.path, canonical)
		if err != nil {
			return libraryRoot{}, fmt.Errorf("%w: media directory is outside the approved root", ErrInvalidInput)
		}
		root, err := openRegisteredRoot(approved.root, relative)
		if err != nil {
			return libraryRoot{}, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
		}
		_ = root.Close()
		return libraryRoot{path: canonical, allowedPath: approved.path, relativePath: relative}, nil
	}
	return libraryRoot{}, fmt.Errorf("%w: media directory is outside configured roots", ErrForbidden)
}

// openApprovedRoot retains the initial safe directory descriptor. All later
// paths are opened relative to that descriptor, preventing pathname swaps from
// redirecting a scan outside the directory that was actually approved.
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
	if s.closed {
		return nil, ErrUnavailable
	}
	if hasTraversal(root.relativePath) || filepath.IsAbs(root.relativePath) {
		return nil, fmt.Errorf("%w: stored media directory is outside the approved root", ErrForbidden)
	}
	for index := range s.roots {
		approved := &s.roots[index]
		if approved.path != root.allowedPath {
			continue
		}
		if err := openApprovedRoot(approved); err != nil {
			return nil, err
		}
		opened, err := openRegisteredRoot(approved.root, root.relativePath)
		if err != nil {
			return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
		}
		return opened, nil
	}
	return nil, fmt.Errorf("%w: media directory is no longer configured", ErrUnavailable)
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
