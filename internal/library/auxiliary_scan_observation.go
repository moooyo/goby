package library

import (
	"context"
	"errors"
	"os"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

// Only filesystem instability observed inside admitted work may retain the
// previous auxiliary population as a warning. Admission, authority and cleanup
// failures remain fatal even when joined with an otherwise ordinary warning.
type auxiliarySourceInstability struct{ err error }

func (failure *auxiliarySourceInstability) Error() string { return failure.err.Error() }
func (failure *auxiliarySourceInstability) Unwrap() error { return failure.err }

func auxiliarySourceFailure(err error) error {
	if err == nil {
		return nil
	}
	var readFailure *primaryScanReadFailure
	if errors.As(err, &readFailure) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errSidecarRollbackUnknown) || errors.Is(err, errSidecarRetirementUnknown) ||
		errors.Is(err, errScanPublicationRetirementUnknown) ||
		errors.Is(err, media.ErrProcessRetirementUnknown) || errors.Is(err, ErrBusy) ||
		errors.Is(err, ErrRootBindingConflict) || errors.Is(err, ErrTaskScanInactive) {
		return scanReadFailure(err)
	}
	return &auxiliarySourceInstability{err: err}
}

func auxiliaryInputWarning(err error) bool {
	if err == nil {
		return false
	}
	if _, fatal := err.(*primaryScanReadFailure); fatal {
		return false
	}
	if _, fatal := err.(*extraPublicationTransactionFailure); fatal {
		return false
	}
	if _, unstable := err.(*auxiliarySourceInstability); unstable {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		causes := joined.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !auxiliaryInputWarning(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return auxiliaryInputWarning(wrapped.Unwrap())
	}
	return err == ErrInvalidInput
}

// The operation reuses the scan's startup grant and admits only filesystem
// observation. Callers never place publication transactions inside this scope.
func (s *Store) observeScanAuxiliaryRoot(task *scanTask, root libraryRoot, observe func(context.Context, *os.Root) error) (resultErr error) {
	row, err := s.readScanOperationAuthority(task.ctx, task, root)
	if err != nil {
		return scanReadFailure(err)
	}
	operation, err := s.prepareScanOperationRootIO(task.ctx, task, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	if err != nil {
		return scanReadFailure(err)
	}
	defer func() { resultErr = errors.Join(resultErr, scanReadFailure(operation.Close())) }()
	err = operation.Run(task.ctx, root.id, primaryio.Background, func(ctx context.Context) (resultErr error) {
		fresh, err := s.readScanOperationAuthority(ctx, task, root)
		if err != nil || !row.same(fresh) {
			return scanReadFailure(errors.Join(err, ErrRootBindingConflict))
		}
		opened, err := s.openScanOperationRoot(ctx, task, root)
		if err != nil {
			var pathError *os.PathError
			if errors.As(err, &pathError) {
				return auxiliarySourceFailure(err)
			}
			return scanReadFailure(err)
		}
		defer func() { resultErr = errors.Join(resultErr, closeAuxiliaryRoot(opened)) }()
		return auxiliarySourceFailure(errors.Join(observe(ctx, opened), ctx.Err()))
	})
	if auxiliaryInputWarning(err) {
		return err
	}
	return scanReadFailure(err)
}

func (s *Store) scanAuxiliaryPathAbsent(task *scanTask, root libraryRoot, relative string) (absent bool, resultErr error) {
	err := s.observeScanAuxiliaryRoot(task, root, func(ctx context.Context, opened *os.Root) error {
		var err error
		absent, err = themePathAbsent(opened, relative)
		return err
	})
	return absent, err
}
