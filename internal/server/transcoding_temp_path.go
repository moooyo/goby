package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
)

// TranscodingTempPathState separates the requested restart location from the
// cache location captured by this process. Saving a path never opens a cache.
type TranscodingTempPathState struct {
	Desired         string
	Active          *string
	RestartRequired bool
}

func (s *Server) ManagedTranscodingTempPathState(desired string) TranscodingTempPathState {
	state := TranscodingTempPathState{Desired: desired}
	if s.activeTranscodingTempPath != "" {
		active := s.activeTranscodingTempPath
		state.Active = &active
		state.RestartRequired = desired != active
	}
	return state
}

// Startup checks both lexical roots and aliases of the existing parent paths.
// The selected cache itself is opened only by the manager's no-follow walk.
// This read-only check never creates a reserved directory or migrates data.
func (s *Server) validateStartupTranscodingTempPath(selected string, contexts ...context.Context) error {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	roots := s.cfg.TranscodingTempPathExclusions()
	if err := config.ValidateTranscodingTempPath(selected, roots); err != nil {
		return err
	}
	selectedResolved, err := transcodingReservedRoot(selected)
	if err != nil {
		return err
	}
	selectedRoot, err := transcodingExistingRootFor(selectedResolved)
	if err != nil {
		return err
	}
	resolvedRoots := make([]string, 0, len(roots))
	for _, root := range roots {
		resolved, err := transcodingReservedRoot(root)
		if err != nil {
			return err
		}
		if err := config.ValidateTranscodingTempPath(selected, []string{resolved}); err != nil {
			return err
		}
		reservedRoot, err := transcodingExistingRootFor(resolved)
		if err != nil {
			return err
		}
		overlap, err := transcodingExistingRootsOverlap(selectedRoot, reservedRoot)
		if err != nil {
			return err
		}
		if overlap {
			return errors.New("transcoding temporary path must use a dedicated directory separate from other deployment roots")
		}
		resolvedRoots = append(resolvedRoots, resolved)
	}
	return library.CheckTranscodingTempPathMountIsolation(ctx, selectedResolved, resolvedRoots)
}

type transcodingExistingRoot struct {
	path, suffix string
	info         os.FileInfo
}

// Directory identity also recognizes bind-mount aliases, which do not appear
// in symlink resolution. Missing future components remain relative suffixes.
func transcodingExistingRootFor(root string) (transcodingExistingRoot, error) {
	var suffix string
	for {
		info, err := os.Stat(root)
		if err == nil {
			if !info.IsDir() {
				return transcodingExistingRoot{}, errors.New("transcoding deployment roots must be directories")
			}
			return transcodingExistingRoot{path: root, suffix: suffix, info: info}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return transcodingExistingRoot{}, err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return transcodingExistingRoot{}, err
		}
		suffix = filepath.Join(filepath.Base(root), suffix)
		root = parent
	}
}

func transcodingExistingRootsOverlap(first, second transcodingExistingRoot) (bool, error) {
	contains := func(descendant, ancestor transcodingExistingRoot) (bool, error) {
		current, info := descendant.path, descendant.info
		for {
			if os.SameFile(info, ancestor.info) {
				relative, err := filepath.Rel(current, descendant.path)
				if err != nil {
					return false, err
				}
				left, right := filepath.Clean(filepath.Join(relative, descendant.suffix)), filepath.Clean(ancestor.suffix)
				separator := string(os.PathSeparator)
				return left == "." || right == "." || left == right ||
					strings.HasPrefix(left, right+separator) || strings.HasPrefix(right, left+separator), nil
			}
			parent := filepath.Dir(current)
			if parent == current {
				return false, nil
			}
			var err error
			info, err = os.Stat(parent)
			if err != nil {
				return false, err
			}
			current = parent
		}
	}
	if overlap, err := contains(first, second); overlap || err != nil {
		return overlap, err
	}
	return contains(second, first)
}

// A reserved store may not exist until its own runtime starts. Resolve its
// nearest existing ancestor, then append the unresolved suffix without opening
// or creating any of those future components.
func transcodingReservedRoot(root string) (string, error) {
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(root)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", err
		}
		suffix = append(suffix, filepath.Base(root))
		root = parent
	}
}
