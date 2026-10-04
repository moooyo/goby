package library

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

// scanReconciliationPass retains every root approval before any root is walked.
// Additions and updates retain their existing transactions. An incomplete proof
// disables the one final deletion transaction for the entire library.
type scanReconciliationPass struct {
	primaryIO  *PrimaryRootIO
	captures   []*rootBindingScanCapture
	byRoot     map[string]*rootBindingScanCapture
	evidence   *scanReconciliationEvidence
	eligible   bool
	individual bool
	task       *scanTask
	staging    *scanReconciliationStaging
	closeErr   error
}

func (s *Store) prepareScanReconciliation(task *scanTask, roots []libraryRoot) (*scanReconciliationPass, error) {
	pass := &scanReconciliationPass{byRoot: make(map[string]*rootBindingScanCapture), task: task}
	// Unsupported platforms retain ordinary scanning without deletion authority.
	if runtime.GOOS != "linux" || len(roots) == 0 {
		return pass, nil
	}
	if len(roots) > scanReconciliationMaxRoots {
		pass.individual = true
		return pass, nil
	}
	pass.eligible = true
	var err error
	hints := make([]mediaSourceRootHint, 0, len(roots))
	for _, root := range roots {
		row, err := s.readScanOperationAuthority(task.ctx, task, root)
		if err != nil {
			return nil, err
		}
		if _, err := row.validate(); err != nil {
			return nil, err
		}
		hints = append(hints, mediaSourceRootHint{root: row.root, bindingRevision: row.revision})
	}
	pass.primaryIO, err = s.prepareScanOperationRootIO(task.ctx, task, hints)
	if err != nil {
		return nil, err
	}
	pass.evidence, err = s.newScanReconciliationEvidence(task.ctx)
	if err != nil {
		if pass.evidence == nil {
			pass.evidence = newScanReconciliationEvidence()
		}
		// Admission exhaustion prevents deletion but does not hide readable
		// media from an ordinary scan. Store ownership tracks any late cleanup.
		pass.evidence.Disable(err)
	}
	retainedBytes, retainedHandles := int64(0), 0
	for _, root := range roots {
		capture, err := s.prepareRootBindingScanWithCapture(task, root, s.scanRootBindingCapture(task), pass.primaryIO)
		if err != nil {
			_ = capture.Close()
			return nil, errors.Join(err, pass.Close())
		}
		pass.captures = append(pass.captures, capture)
		pass.byRoot[root.id] = capture
		if capture == nil || capture.status != RootBindingVerified {
			pass.retainIndividually()
			return pass, nil
		}
		bytes, handles, bounded := scanRootCaptureCost(capture)
		if !bounded || bytes > (64<<20)-retainedBytes || handles > 1024-retainedHandles {
			pass.retainIndividually()
			return pass, nil
		}
		retainedBytes += bytes
		retainedHandles += handles
		if pass.eligible && pass.evidence.Err() == nil {
			if err := pass.primaryIO.Run(task.ctx, root.id, primaryio.Background, func(context.Context) error {
				return pass.evidence.AttachRoot(root.id, capture.opened)
			}); err != nil {
				pass.evidence.Disable(err)
			}
		}
	}
	if pass.evidence.Err() == nil {
		pass.staging, err = s.beginScanReconciliationStaging(task.ctx, task.job.ID, task.job.LibraryID)
		if err != nil {
			return nil, errors.Join(err, pass.Close())
		}
	}
	return pass, nil
}

// Captures have an independent all-roots limit in addition to directory proof
// budgets. Larger libraries still recover each original root and scan normally,
// keeping only that walk's capture and granting no deletion authority.
func (pass *scanReconciliationPass) retainIndividually() {
	pass.eligible, pass.individual = false, true
	_ = pass.Close()
}

func scanRootCaptureCost(capture *rootBindingScanCapture) (int64, int, bool) {
	named, ok := capture.capture.(*rootBindingNamedCapture)
	if !ok || named.RootTopologyCapture == nil {
		return 0, 0, false
	}
	topology := named.RootTopologyCapture
	topology.mu.Lock()
	defer topology.mu.Unlock()
	// Include snapshot/live copies, decoded documents, maps and string headers.
	bytes := int64(16384 + 8*len(capture.row.document))
	for _, record := range topology.records {
		bytes += 512 + 2*int64(len(record.Root)+len(record.MountPoint)+len(record.FilesystemType)+len(record.Source))
		for _, fields := range [][]string{record.Options, record.OptionalFields, record.SuperOptions} {
			for _, value := range fields {
				bytes += 64 + 2*int64(len(value))
			}
		}
	}
	return bytes, len(topology.held) + 4, !topology.closed
}

func (pass *scanReconciliationPass) Close() error {
	if pass == nil {
		return nil
	}
	var catalogErr, resourceErr error
	if pass.staging != nil {
		catalogErr = pass.staging.Close()
		pass.staging = nil
	}
	if pass.evidence != nil {
		resourceErr = errors.Join(resourceErr, pass.evidence.Close())
	}
	for _, capture := range pass.captures {
		resourceErr = errors.Join(resourceErr, capture.Close())
	}
	if pass.primaryIO != nil {
		if resourceErr != nil {
			_ = pass.primaryIO.MarkUnknown(resourceErr)
		}
		resourceErr = errors.Join(resourceErr, pass.primaryIO.Close())
		pass.primaryIO = nil
	}
	pass.captures, pass.byRoot = nil, nil
	pass.closeErr = errors.Join(pass.closeErr, catalogErr, resourceErr)
	return pass.closeErr
}

func (pass *scanReconciliationPass) openRoot(s *Store, root libraryRoot) (*os.Root, error) {
	if pass.individual {
		capture, err := s.prepareRootBindingScan(pass.task, root)
		if err != nil {
			_ = capture.Close()
			return nil, &scanReconciliationPreparationFailure{err}
		}
		defer capture.Close()
		if capture != nil && capture.status == RootBindingVerified {
			var opened *os.Root
			err := capture.primaryIO.Run(pass.task.ctx, root.id, primaryio.Background, func(context.Context) error {
				var err error
				opened, err = capture.opened.OpenRoot(".")
				return err
			})
			return opened, err
		}
		return s.openScanOperationRoot(pass.task.ctx, pass.task, root)
	}
	if capture := pass.byRoot[root.id]; capture != nil && capture.status == RootBindingVerified {
		// Keep the binding capture alive after the walker releases its own root.
		var opened *os.Root
		err := pass.primaryIO.Run(pass.task.ctx, root.id, primaryio.Background, func(context.Context) error {
			var err error
			opened, err = capture.opened.OpenRoot(".")
			return err
		})
		return opened, err
	}
	return s.openScanOperationRoot(pass.task.ctx, pass.task, root)
}

type scanReconciliationPreparationFailure struct{ err error }

func (failure *scanReconciliationPreparationFailure) Error() string { return failure.err.Error() }
func (failure *scanReconciliationPreparationFailure) Unwrap() error { return failure.err }

func (pass *scanReconciliationPass) collector() *scanReconciliationEvidence {
	if !pass.eligible || pass.evidence == nil || pass.evidence.Err() != nil {
		return nil
	}
	return pass.evidence
}

func (pass *scanReconciliationPass) finish(s *Store, task *scanTask, library Library, complete bool, musicParents map[string]bool) (string, error) {
	started := time.Now()
	if err := task.ctx.Err(); err != nil {
		return "", err
	}
	if !pass.eligible || !complete {
		return "", nil
	}
	const retained = "; missing catalog records were retained because complete storage and directory evidence could not be established"
	if err := pass.evidence.Err(); err != nil {
		reportScanReconciliationIssue(task.job.ID, "evidence_readiness", err, started, time.Time{}, true)
		return retained, nil
	}
	if err := pass.evidence.requireComplete(task.ctx); err != nil {
		if task.ctx.Err() != nil {
			return "", task.ctx.Err()
		}
		reportScanReconciliationIssue(task.job.ID, "evidence_complete", err, started, time.Time{}, true)
		return retained, nil
	}
	if pass.staging == nil {
		err := errors.New("complete scan has no accepted-identity staging")
		reportScanReconciliationIssue(task.job.ID, "staging_unavailable", err, started, time.Time{}, false)
		return "", err
	}
	sealStarted := time.Now()
	if err := pass.staging.Seal(task.ctx); err != nil {
		reportScanReconciliationIssue(task.job.ID, "staging_seal", err, sealStarted, time.Time{}, false)
		return "", err
	}
	parents, err := s.reconcileMissingScanItems(task, library, pass.captures, pass.evidence, musicParents, pass.staging)
	if err != nil {
		if task.ctx.Err() != nil {
			return "", task.ctx.Err()
		}
		if scanReconciliationObservationOnly(err) {
			return retained, nil
		}
		return "", err
	}
	for _, parentID := range parents {
		musicParents[parentID] = true
	}
	return "", nil
}

type scanSeenRecordingError struct{ err error }

func (failure *scanSeenRecordingError) Error() string { return failure.err.Error() }
func (failure *scanSeenRecordingError) Unwrap() error { return failure.err }

func (state *scanState) recordScanSeen(id string) error {
	if state.reconciliationPass != nil && state.reconciliationPass.staging != nil {
		if err := state.reconciliationPass.staging.Record(state.task.ctx, id); err != nil {
			return &scanSeenRecordingError{err}
		}
		return nil
	}
	if state.reconciliation != nil {
		_ = state.reconciliation.MarkSeen(id)
	}
	return nil
}

// The scanner needs complete membership for auxiliary classification and album
// boundaries. Bound that original allocation as well as the retained evidence;
// an excessive directory fails the root instead of truncating its membership.
func readScanDirectoryEntries(ctx context.Context, directory *os.File) ([]os.DirEntry, error) {
	return readScanDirectoryEntriesLimit(ctx, directory, 262144, 64<<20)
}

func readScanDirectoryEntriesLimit(ctx context.Context, directory *os.File, maxEntries, maxBytes int) ([]os.DirEntry, error) {
	if maxEntries < 0 || maxBytes < 0 {
		return nil, errScanReconciliationEvidenceBudget
	}
	var entries []os.DirEntry
	used := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		batch, err := directory.ReadDir(64)
		for _, entry := range batch {
			charge := 512 + 2*len(entry.Name())
			if len(entries) >= maxEntries || charge > maxBytes-used {
				return nil, errScanReconciliationEvidenceBudget
			}
			used += charge
			entries = append(entries, entry)
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return entries, nil
			}
			return nil, err
		}
		if len(batch) == 0 {
			return nil, errScanReconciliationEvidenceUnavailable
		}
	}
}
