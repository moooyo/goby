package library

import (
	"context"
	"errors"
	"os"
)

// A scan opens names within its startup grant without consulting the current
// configured-root list. Callers retain their admitted I/O and source-identity
// checks; a permission edit cannot expand or revoke this operation's scope.
func (s *Store) leaseScanOperationRoot(ctx context.Context, task *scanTask, root libraryRoot) (*libraryRootLease, error) {
	granted, err := s.readScanOperationAuthority(ctx, task, root)
	if err != nil {
		return nil, err
	}
	approved := approvedRoot{path: granted.root.allowedPath}
	if err := openApprovedRoot(&approved); err != nil {
		return nil, err
	}
	return &libraryRootLease{approved: approved.root, relativePath: granted.root.relativePath}, nil
}

func (s *Store) openScanOperationRoot(ctx context.Context, task *scanTask, root libraryRoot) (*os.Root, error) {
	lease, err := s.leaseScanOperationRoot(ctx, task, root)
	if err != nil {
		return nil, err
	}
	opened, openErr := lease.Open()
	closeErr := closePrimarySidecarResource(ctx, lease)
	if openErr != nil || closeErr != nil {
		if opened != nil {
			closeErr = errors.Join(closeErr, closePrimarySidecarResource(ctx, opened))
		}
		return nil, errors.Join(openErr, closeErr)
	}
	return opened, nil
}
