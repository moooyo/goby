package library

import "errors"

// This state carries only a hint and an observation failure across an ownership
// gap. No catalog page, cascade closure, locked row or successful filesystem
// proof may be reused as final deletion authority.
type scanReconciliationPreflight struct {
	performed      bool
	hasCandidates  bool
	observationErr error
}

// preflightScanReconciliation performs no filesystem work. Even an empty hint
// requires current memory admission, task/root authority and this exact sealed
// pass on the reserved owner session. The caller must repeat those checks and
// reread current candidates in the final transaction after this one commits.
func (s *Store) preflightScanReconciliation(task *scanTask, library Library, roots map[string]*rootBindingScanCapture, staging *scanReconciliationStaging) (scanReconciliationPreflight, error) {
	var result scanReconciliationPreflight
	if staging == nil {
		return result, ErrInvalidInput
	}
	raw, err := s.beginOwnedAdmission(task.ctx, false)
	if err != nil {
		return result, err
	}
	admissionErr := s.admitScanReconciliationLocked(task, roots)
	s.mu.Unlock()
	err = s.withOwnedTxCallback(raw, func(tx OwnedTx) error {
		if admissionErr != nil {
			return admissionErr
		}
		if err := lockScanReconciliationTask(tx, task, library.ID); err != nil {
			return err
		}
		if err := validateScanReconciliationRoots(tx, library, roots); err != nil {
			return err
		}
		if err := validateScanReconciliationStaging(tx, task, library.ID, staging); err != nil {
			return err
		}
		page, err := readScanReconciliationPage(tx, library.ID, "", true, staging)
		if err != nil {
			return err
		}
		result.hasCandidates = len(page) != 0
		return task.ctx.Err()
	})
	if err != nil {
		return scanReconciliationPreflight{}, err
	}
	result.performed = true
	return result, nil
}

func validateScanReconciliationStaging(tx OwnedTx, task *scanTask, libraryID string, staging *scanReconciliationStaging) error {
	if staging == nil {
		return nil
	}
	_, scanID, stagedLibrary := staging.Scope()
	if scanID != task.job.ID || stagedLibrary != libraryID {
		return ErrInvalidInput
	}
	return staging.RequireSealed(tx)
}

// Call only after fresh final task/root/sealed-pass checks. A saved filesystem
// failure must reach protected rollback even when the new candidate page is
// empty. Conversely, a new nonempty page after an empty preview has no first
// observation and must retain the entire pass instead of authorizing removal.
func readScanReconciliationFinalPage(tx OwnedTx, libraryID string, staging *scanReconciliationStaging, preflight scanReconciliationPreflight) ([]scanReconciliationItem, error) {
	if preflight.observationErr != nil {
		return nil, preflight.observationErr
	}
	page, err := readScanReconciliationPage(tx, libraryID, "", true, staging)
	if err != nil {
		return nil, err
	}
	if preflight.performed && !preflight.hasCandidates && len(page) != 0 {
		return nil, scanReconciliationUnavailable("catalog candidates appeared after an empty preflight")
	}
	return page, nil
}

func joinScanReconciliationPreflightError(err error, preflight scanReconciliationPreflight) error {
	if preflight.observationErr == nil || errors.Is(err, preflight.observationErr) {
		return err
	}
	return errors.Join(err, preflight.observationErr)
}
