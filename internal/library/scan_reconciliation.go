package library

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
)

// scanReconciliationPass retains every root approval before any root is walked.
// Additions and updates retain their existing transactions. An incomplete proof
// disables the one final deletion transaction for the entire library.
type scanReconciliationPass struct {
	captures   []*rootBindingScanCapture
	byRoot     map[string]*rootBindingScanCapture
	evidence   *scanReconciliationEvidence
	eligible   bool
	individual bool
	task       *scanTask
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
	pass.evidence = newScanReconciliationEvidence()
	retainedBytes, retainedHandles := int64(0), 0
	for _, root := range roots {
		capture, err := s.prepareRootBindingScan(task, root)
		if err != nil {
			_ = capture.Close()
			_ = pass.Close()
			return nil, err
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
			_ = pass.evidence.AttachRoot(root.id, capture.opened)
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
	var err error
	if pass.evidence != nil {
		err = pass.evidence.Close()
	}
	for _, capture := range pass.captures {
		err = errors.Join(err, capture.Close())
	}
	pass.captures, pass.byRoot = nil, nil
	return err
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
			return capture.opened.OpenRoot(".")
		}
		return s.openLibraryRoot(root)
	}
	if capture := pass.byRoot[root.id]; capture != nil && capture.status == RootBindingVerified {
		// Keep the binding capture alive after the walker releases its own root.
		return capture.opened.OpenRoot(".")
	}
	return s.openLibraryRoot(root)
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
	if err := task.ctx.Err(); err != nil {
		return "", err
	}
	if !pass.eligible || !complete {
		return "", nil
	}
	const retained = "; missing catalog records were retained because complete storage and directory evidence could not be established"
	if pass.evidence.Err() != nil {
		return retained, nil
	}
	parents, err := s.reconcileMissingScanItems(task, library, pass.captures, pass.evidence, musicParents)
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

func (state *scanState) recordScanSeen(id string) {
	if state.reconciliation != nil {
		_ = state.reconciliation.MarkSeen(id)
	}
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
