package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/moooyo/goby/internal/media"
)

// A warm source root pins an already published approved descriptor without
// performing any filesystem operation. It grants no source or playback
// authority. The caller must separately authorize the fresh source and hold
// actual source-IO admission throughout openMediaSource and release.
type warmMediaSourceRoot struct {
	store     *Store
	root      libraryRoot
	reference *rootAnchorReference
	released  bool // Protected by store.mu.
}

// A nil lease and nil error select the existing cold-root path. No directory
// open, descriptor clone, stat or topology observation occurs during borrowing.
func (s *Store) borrowWarmMediaSourceRoot(root libraryRoot) (*warmMediaSourceRoot, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	if err := (rootBindingRow{root: root, revision: 1}).validateMapping(); err != nil {
		return nil, fmt.Errorf("%w: invalid warm media root mapping", ErrUnavailable)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	approved, err := s.approvedLibraryRootLocked(root)
	if err != nil {
		return nil, err
	}
	if approved == nil {
		return nil, nil
	}
	if reference := s.rootAnchorReferences[approved]; reference != nil && reference.retired {
		return nil, fmt.Errorf("%w: warm media root anchor is retired", ErrUnavailable)
	}
	return &warmMediaSourceRoot{store: s, root: root, reference: s.borrowRootAnchorLocked(approved)}, nil
}

// The caller holds store.mu. This checks only the currently published memory
// mapping; fresh database root facts remain the authorization caller's duty.
func (lease *warmMediaSourceRoot) currentApprovedLocked() (*os.Root, error) {
	if lease.released || lease.reference == nil || lease.reference.approved == nil || lease.reference.retired {
		return nil, fmt.Errorf("%w: %w: warm media root is no longer admitted", ErrUnavailable, ErrSourceChanged)
	}
	approved, err := lease.store.approvedLibraryRootLocked(lease.root)
	if err != nil {
		return nil, err
	}
	if approved == nil || approved != lease.reference.approved {
		return nil, fmt.Errorf("%w: %w: warm media root anchor changed", ErrUnavailable, ErrSourceChanged)
	}
	return approved, nil
}

func (lease *warmMediaSourceRoot) currentApproved() (*os.Root, error) {
	if lease == nil || lease.store == nil {
		return nil, ErrUnavailable
	}
	lease.store.mu.Lock()
	defer lease.store.mu.Unlock()
	return lease.currentApprovedLocked()
}

// Release may close the last retired descriptor. The caller must retain its
// actual IO lease or a cleanup IO charge until this method returns.
func (lease *warmMediaSourceRoot) release() {
	_ = lease.releaseChecked()
}

func (lease *warmMediaSourceRoot) releaseChecked() error {
	if lease == nil || lease.store == nil {
		return nil
	}
	s := lease.store
	s.mu.Lock()
	if lease.released {
		s.mu.Unlock()
		return nil
	}
	lease.released = true
	reference := lease.reference
	s.mu.Unlock()
	if reference != nil {
		return s.releaseRootAnchor(reference)
	}
	return nil
}

// This primitive performs only filesystem and memory-mapping validation. The
// caller supplies fresh committed authority, publication and root-binding facts
// and owns actual IO admission before entering it. No database query runs here.
func (lease *warmMediaSourceRoot) openMediaSource(ctx context.Context, snapshot indexedMediaSource) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lease == nil || lease.store == nil {
		return nil, ErrUnavailable
	}
	return lease.store.runSourceMetadataOpen(ctx, snapshot, func(work context.Context) (*os.File, error) {
		return lease.openMediaSourceFilesystem(work, snapshot)
	})
}

func (lease *warmMediaSourceRoot) openMediaSourceAndRelease(ctx context.Context, snapshot indexedMediaSource) (*os.File, error) {
	return lease.store.runSourceMetadataOpen(ctx, snapshot, func(work context.Context) (file *os.File, resultErr error) {
		defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(work, lease.releaseChecked)) }()
		return lease.openMediaSourceFilesystem(work, snapshot)
	})
}

func (lease *warmMediaSourceRoot) openMediaSourceFilesystem(ctx context.Context, snapshot indexedMediaSource) (resultFile *os.File, resultErr error) {
	if snapshot.root != lease.root {
		return nil, fmt.Errorf("%w: %w: source does not match its warm media root", ErrUnavailable, ErrSourceChanged)
	}
	if err := validateMediaSource(snapshot); err != nil {
		return nil, err
	}
	s := lease.store
	s.mu.Lock()
	approved, err := lease.currentApprovedLocked()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	// A temporary memory-only pin makes even a concurrent original lease
	// release unable to close the descriptor during these filesystem calls.
	opening := s.borrowRootAnchorLocked(approved)
	s.rootOpens.Add(1)
	s.mu.Unlock()
	defer func() {
		resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, func() error { return s.releaseRootAnchor(opening) }))
		s.rootOpens.Done()
	}()
	openedAt := time.Now()
	defer func() { mediaSourceAdmissionMeasurement.fileOpenNS.Add(uint64(time.Since(openedAt))) }()
	root, err := openRegisteredRoot(approved, snapshot.root.relativePath)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, root.Close)) }()
	path := filepath.FromSlash(snapshot.relativePath)
	parent, err := openRegisteredRoot(root, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: media parent directory cannot be opened safely", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, parent.Close)) }()
	name := filepath.Base(path)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("%w: indexed media metadata cannot be read", ErrUnavailable)
	}
	if !snapshot.matches(before) {
		return nil, fmt.Errorf("%w: %w before opening; rescan required", ErrUnavailable, ErrSourceChanged)
	}
	file, err := openScanFile(parent, name)
	if err != nil {
		return nil, fmt.Errorf("%w: indexed media cannot be opened safely", ErrUnavailable)
	}
	success := false
	defer func() {
		if !success {
			if closeErr := file.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
				resultErr = errors.Join(resultErr, media.SourceReadRetirementError(closeErr, file))
			}
		}
	}()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: open media metadata cannot be read", ErrUnavailable)
	}
	if !snapshot.matches(opened) || !sameMediaSourceFile(before, opened) {
		return nil, fmt.Errorf("%w: %w while opening; rescan required", ErrUnavailable, ErrSourceChanged)
	}
	currentApproved, err := lease.currentApproved()
	if err != nil {
		return nil, err
	}
	currentRoot, err := openRegisteredRoot(currentApproved, snapshot.root.relativePath)
	if err != nil {
		return nil, fmt.Errorf("%w: media directory cannot be opened safely", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, currentRoot.Close)) }()
	if !sameMediaSourceDirectory(root, currentRoot) {
		return nil, fmt.Errorf("%w: registered media root changed while opening", ErrUnavailable)
	}
	currentParent, err := openRegisteredRoot(currentRoot, filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: media directory changed while opening", ErrUnavailable)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, currentParent.Close)) }()
	if !sameMediaSourceDirectory(parent, currentParent) {
		return nil, fmt.Errorf("%w: media directory was replaced while opening", ErrUnavailable)
	}
	current, err := currentParent.Lstat(name)
	after, afterErr := file.Stat()
	if err != nil || afterErr != nil {
		return nil, fmt.Errorf("%w: media metadata cannot be rechecked", ErrUnavailable)
	}
	if !snapshot.matches(current) || !snapshot.matches(after) ||
		!sameMediaSourceFile(opened, current) || !sameMediaSourceFile(opened, after) {
		return nil, fmt.Errorf("%w: %w while rechecking; rescan required", ErrUnavailable, ErrSourceChanged)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("%w: media descriptor cannot be positioned", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := lease.currentApproved(); err != nil {
		return nil, err
	}
	success = true
	return file, nil
}
