package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/storagebinding"
)

// A scan operation authorizes its complete root set once, before storage work.
// Its identity and approval rows are immutable until the task is retired. Root
// permission edits affect the next operation; cancellation and Store ownership
// loss still stop this operation immediately.
type scanOperationAuthority struct {
	store                     *Store
	jobID, childID, libraryID string
	forceProbe                bool
	roots                     map[string]rootBindingRow
	routes                    map[string]primaryRootIORoute
}

const scanOperationAuthorityRootsSQL = `/* scan_operation_authority */ SELECT ` + rootBindingMetadataColumns + `,
	r.storage_binding IS NOT NULL,
	CASE WHEN octet_length(r.storage_binding::text) <= $2 THEN r.storage_binding::text END,
	r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
	FROM library_roots r WHERE r.library_id=$1 ORDER BY r.id LIMIT $3 FOR UPDATE OF r`

// The explicit scanner entry supplies its enumerated roots. Standalone scan
// helpers use nil and still capture the entire library, never extending an
// existing operation with a newly authorized root halfway through a scan.
func (s *Store) prepareScanOperationAuthority(ctx context.Context, task *scanTask, expected []libraryRoot) (resultErr error) {
	if ctx == nil || s == nil || s.pool == nil || task == nil || task.ctx == nil {
		return ErrUnavailable
	}
	if grant := task.authority.Load(); grant != nil {
		if err := s.checkScanOperationActive(ctx, task, grant); err != nil {
			return err
		}
		return grant.matchesRoots(expected)
	}
	task.authorityMu.Lock()
	defer task.authorityMu.Unlock()
	if grant := task.authority.Load(); grant != nil {
		if err := s.checkScanOperationActive(ctx, task, grant); err != nil {
			return err
		}
		return grant.matchesRoots(expected)
	}
	grant := &scanOperationAuthority{store: s, jobID: task.job.ID, childID: task.job.TaskChildID,
		libraryID: task.job.LibraryID, forceProbe: task.job.ForceProbe, roots: make(map[string]rootBindingRow),
		routes: make(map[string]primaryRootIORoute)}
	if err := s.checkScanOperationActive(ctx, task, grant); err != nil {
		return err
	}
	work, finish, err := s.beginMediaSourceLifetime(ctx)
	if err != nil {
		return err
	}
	defer finish()
	ctx = work
	s.mediaSourceOwners.mu.Lock()
	catalog := s.mediaSourceOwners.catalogScope
	s.mediaSourceOwners.mu.Unlock()
	s.mu.Lock()
	configured := make([]string, 0, len(s.roots))
	for _, root := range s.roots {
		configured = append(configured, root.path)
	}
	s.mu.Unlock()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	relation, err := lockTaskScanRelation(scanProbeAuthorityTx{ctx: ctx, tx: tx}, grant.jobID, grant.childID)
	if err != nil {
		return err
	}
	if relation.missing || relation.job.ID != grant.jobID || relation.job.LibraryID != grant.libraryID ||
		relation.job.TaskChildID != grant.childID || relation.job.ForceProbe != grant.forceProbe || relation.job.Status != "Running" {
		return ErrTaskScanInactive
	}
	if relation.job.CancelRequested || relation.child != nil && (!activeTaskRun(relation.child.runState) || relation.child.state != "running") {
		return context.Canceled
	}
	rows, err := tx.Query(ctx, scanOperationAuthorityRootsSQL, grant.libraryID, storagebinding.MaxDocumentBytes, maxRegisteredRootBindingList+1)
	if err != nil {
		return fmt.Errorf("read scan operation roots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row rootBindingRow
		if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
			&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy); err != nil {
			return fmt.Errorf("read scan operation root: %w", err)
		}
		if row.root.libraryID != grant.libraryID || row.validateMapping() != nil {
			return ErrRootBindingConflict
		}
		if _, err := row.validate(); err != nil {
			return err
		}
		route, err := scanOperationRootRoute(catalog, row.root, configured)
		if err != nil {
			return err
		}
		grant.roots[row.root.id] = row
		grant.routes[row.root.id] = route
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("complete scan operation roots: %w", err)
	}
	rows.Close()
	if len(grant.roots) > maxRegisteredRootBindingList {
		return ErrUnavailable
	}
	if err := grant.matchesRoots(expected); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := task.ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	// Publish only after releasing all database locks. No Store mutex is held
	// while waiting for a task/root row, preserving owned-writer lock ordering.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownership == nil || s.ownership.lost.Load() {
		return ErrUnavailable
	}
	if !s.scanOperationActiveLocked(task, grant) {
		return ErrTaskScanInactive
	}
	if err := task.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.job.CancelRequested {
		return context.Canceled
	}
	task.authority.Store(grant)
	return nil
}

func (grant *scanOperationAuthority) matchesRoots(expected []libraryRoot) error {
	if expected == nil {
		return nil
	}
	if len(expected) != len(grant.roots) {
		return ErrRootBindingConflict
	}
	for _, root := range expected {
		if row, ok := grant.roots[root.id]; !ok || row.root != root {
			return ErrRootBindingConflict
		}
	}
	return nil
}

func (s *Store) scanOperationActiveLocked(task *scanTask, grant *scanOperationAuthority) bool {
	return grant.store == s && !s.closed && !s.closing.Load() &&
		s.ownership != nil && !s.ownership.lost.Load() && s.active[grant.jobID] == task &&
		task.job.ID == grant.jobID && task.job.LibraryID == grant.libraryID &&
		task.job.TaskChildID == grant.childID && task.job.ForceProbe == grant.forceProbe && task.job.Status == "Running"
}

func (s *Store) checkScanOperationActive(ctx context.Context, task *scanTask, grant *scanOperationAuthority) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := task.ctx.Err(); err != nil {
		return err
	}
	if task.job.CancelRequested {
		return context.Canceled
	}
	s.mu.Lock()
	unavailable := s.ownership == nil || s.ownership.lost.Load()
	active := s.scanOperationActiveLocked(task, grant)
	s.mu.Unlock()
	if unavailable {
		return ErrUnavailable
	}
	if !active {
		return ErrTaskScanInactive
	}
	if err := task.ctx.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Store) readScanOperationRoot(ctx context.Context, task *scanTask, rootID string) (rootBindingRow, error) {
	if err := s.prepareScanOperationAuthority(ctx, task, nil); err != nil {
		return rootBindingRow{}, err
	}
	row, ok := task.authority.Load().roots[rootID]
	if !ok {
		return rootBindingRow{}, ErrRootBindingConflict
	}
	return row, nil
}

func (s *Store) readScanOperationAuthority(ctx context.Context, task *scanTask, root libraryRoot) (rootBindingRow, error) {
	row, err := s.readScanOperationRoot(ctx, task, root.id)
	if err != nil {
		return rootBindingRow{}, err
	}
	if row.root != root {
		return rootBindingRow{}, ErrRootBindingConflict
	}
	return row, nil
}

// Final writers fence the physical catalog mapping independently from the
// operation's permission grant. Approval revisions and documents may change;
// an accepted source must never be written under a different registered path.
func (s *Store) checkScanOperationRootTx(ctx context.Context, tx pgx.Tx, task *scanTask, expected rootBindingRow) error {
	if ctx == nil || s == nil || task == nil || task.ctx == nil || task.authority.Load() == nil {
		return ErrTaskScanInactive
	}
	grant := task.authority.Load()
	if err := s.checkScanOperationActive(ctx, task, grant); err != nil {
		return err
	}
	granted, ok := grant.roots[expected.root.id]
	if !ok || !granted.same(expected) {
		return ErrRootBindingConflict
	}
	return checkScanOperationRootMappingTx(ctx, tx, expected.root)
}

// Callers that already hold Store admission use this data-only check after
// obtaining their operation grant, without acquiring Store.mu recursively.
func checkScanOperationRootMappingTx(ctx context.Context, tx pgx.Tx, expected libraryRoot) error {
	var current libraryRoot
	err := tx.QueryRow(ctx, `SELECT
		CASE WHEN octet_length(id) <= 256 THEN id ELSE '' END,
		CASE WHEN octet_length(library_id) <= 256 THEN library_id ELSE '' END,
		CASE WHEN octet_length(path) <= 4096 THEN path ELSE '' END,
		CASE WHEN octet_length(allowed_path) <= 4096 THEN allowed_path ELSE '' END,
		CASE WHEN octet_length(relative_path) <= 4096 THEN relative_path ELSE '..' END
		FROM library_roots WHERE id=$1 AND library_id=$2 FOR UPDATE`, expected.id, expected.libraryID).
		Scan(&current.id, &current.libraryID, &current.path, &current.allowedPath, &current.relativePath)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRootBindingConflict
	}
	if err != nil {
		return err
	}
	if current != expected {
		return ErrRootBindingConflict
	}
	return nil
}
