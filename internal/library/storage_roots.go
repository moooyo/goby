package library

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/primaryio"
)

type configuredStorageRoot struct {
	path, absolute, anchor string
}

// StorageRoot reports directory readability without returning a content listing.
type StorageRoot struct {
	Path      string
	Available bool
}

type storageRootReader func(context.Context, *os.File) error

// Resolve only a missing suffix through an existing canonical ancestor. This
// fixes the same startup anchor for availability, registration and admission;
// request-time alias changes never select a new storage domain.
func canonicalConfiguredStorageRoot(absolute string) (string, error) {
	remaining := maxDirectorySymlinkTraversals
	return resolveConfiguredStorageRoot(absolute, &remaining)
}

func resolveConfiguredStorageRoot(absolute string, remaining *int) (string, error) {
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return canonical, err
	}
	info, inspectErr := os.Lstat(absolute)
	if inspectErr != nil && !errors.Is(inspectErr, os.ErrNotExist) {
		return "", inspectErr
	}
	if inspectErr == nil && info.Mode()&os.ModeSymlink != 0 {
		if *remaining == 0 {
			return "", syscall.ELOOP
		}
		*remaining--
		target, err := os.Readlink(absolute)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			parent, _ := splitConfiguredStoragePath(absolute)
			canonicalParent, err := resolveConfiguredStorageRoot(parent, remaining)
			if err != nil {
				return "", err
			}
			// Keep target components intact: a symlink before '..' must be
			// resolved before the parent step, unlike filepath.Join's cleaning.
			target = strings.TrimSuffix(canonicalParent, "/") + "/" + target
		}
		return resolveConfiguredStorageRoot(target, remaining)
	}
	parent, suffix := splitConfiguredStoragePath(absolute)
	if parent == absolute {
		return "", err
	}
	canonical, err = resolveConfiguredStorageRoot(parent, remaining)
	if err != nil {
		return "", err
	}
	return filepath.Join(canonical, suffix), nil
}

func splitConfiguredStoragePath(value string) (string, string) {
	value = strings.TrimRight(value, "/")
	separator := strings.LastIndexByte(value, '/')
	if separator <= 0 {
		return "/", value[separator+1:]
	}
	return value[:separator], value[separator+1:]
}

// StorageRoots retains configuration order and spelling. Every actual read is
// admitted against the canonical anchor selected when this Store was created.
func (s *Store) StorageRoots(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience) ([]StorageRoot, error) {
	return s.storageRootsWithReader(ctx, &catalogAdministrator{actor: actor, audience: audience}, readStorageRootDirectory)
}

func (s *Store) storageRootsWithReader(ctx context.Context, administrator *catalogAdministrator, read storageRootReader) ([]StorageRoot, error) {
	if ctx == nil || read == nil {
		return nil, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
		return nil, err
	}
	s.mu.Lock()
	roots := append([]configuredStorageRoot(nil), s.configuredRoots...)
	closed := s.closed || s.closing.Load()
	s.mu.Unlock()
	if closed {
		return nil, ErrUnavailable
	}
	items := make([]StorageRoot, 0, len(roots))
	for _, root := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		operation, err := s.prepareConfiguredPrimaryRootIO(ctx, root.anchor)
		if err != nil {
			return nil, err
		}
		var available bool
		err = operation.Run(ctx, "", primaryio.Foreground, func(work context.Context) error {
			return s.runServerDirectoryWork(work, administrator, func(work context.Context, _ []string) error {
				var err error
				available, err = observeStorageRoot(work, root, read)
				return err
			})
		})
		if err = errors.Join(err, operation.Close()); err != nil {
			return nil, err
		}
		items = append(items, StorageRoot{Path: root.path, Available: available})
	}
	return items, ctx.Err()
}

func readStorageRootDirectory(ctx context.Context, directory *os.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := directory.ReadDir(1)
	return err
}

func observeStorageRoot(ctx context.Context, root configuredStorageRoot, read storageRootReader) (available bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := checkStorageRootName(root); err != nil {
		return storageRootReadFailure(ctx, err)
	}
	before, err := os.Lstat(root.anchor)
	if err != nil {
		return storageRootReadFailure(ctx, err)
	}
	if !before.IsDir() {
		return false, nil
	}
	anchor, err := os.OpenRoot(root.anchor)
	if err != nil {
		return storageRootReadFailure(ctx, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, anchor)) }()
	directory, err := anchor.Open(".")
	if err != nil {
		return storageRootReadFailure(ctx, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, directory.Close)) }()
	held, err := directory.Stat()
	if err != nil {
		return storageRootReadFailure(ctx, err)
	}
	if !os.SameFile(before, held) {
		return false, errors.Join(ErrUnavailable, ErrRootTopologyChanged)
	}
	if err := checkStorageRootName(root); err != nil {
		return false, errors.Join(ErrUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	readErr := read(ctx, directory)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return storageRootReadFailure(ctx, readErr)
	}
	if err := checkStorageRootName(root); err != nil {
		return false, errors.Join(ErrUnavailable, err)
	}
	named, err := os.Lstat(root.anchor)
	if err != nil || !named.IsDir() || !os.SameFile(held, named) {
		return false, errors.Join(ErrUnavailable, ErrRootTopologyChanged, err)
	}
	return true, ctx.Err()
}

func checkStorageRootName(root configuredStorageRoot) error {
	canonical, err := filepath.EvalSymlinks(root.absolute)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err != nil {
		var resolveErr error
		canonical, resolveErr = canonicalConfiguredStorageRoot(root.absolute)
		if resolveErr != nil {
			return resolveErr
		}
	}
	if canonical != root.anchor {
		return ErrRootTopologyChanged
	}
	return err
}

// A missing/non-directory path or denied read is a negative observation.
// Admission, cancellation, descriptor exhaustion and uncertain storage failures
// remain request errors instead of being reported as an unreadable disk.
func storageRootReadFailure(ctx context.Context, err error) (bool, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return false, contextErr
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	return false, errors.Join(ErrUnavailable, err)
}
