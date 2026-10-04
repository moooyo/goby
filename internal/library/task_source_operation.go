package library

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/storagebinding"
)

type taskSourceOperationKey struct{}

// This authority is created only by a trusted executor with a live Work.Fence.
// It is never serialized, reconstructed from catalog facts, or shared by jobs.
type internalTaskSourceGrant struct {
	store              *Store
	ctx                context.Context
	childID, libraryID string
	roots              map[string]rootBindingRow
	rootIDs            []string
	routes             map[string]primaryRootIORoute
	anchors            map[string]*rootAnchorReference
	registered         map[string]*rootAnchorReference
	revisions          string
	finish             func()
	fence              func(OwnedTx) error
	mu                 sync.Mutex
	closed             atomic.Bool
	closeOnce          sync.Once
	closeErr           error
}

const taskSourceOperationRootsSQL = `SELECT ` + rootBindingMetadataColumns + `,
	r.storage_binding IS NOT NULL,
	CASE WHEN octet_length(r.storage_binding::text) <= $2 THEN r.storage_binding::text END,
	r.bound_at, CASE WHEN r.bound_by IS NULL THEN NULL WHEN octet_length(r.bound_by) <= 256 THEN r.bound_by ELSE '' END
	FROM library_roots r WHERE r.library_id=$1 ORDER BY r.id LIMIT $3 FOR SHARE OF r`

// BeginTaskSourceOperation approves the complete root set for one actual task
// execution. fence must be the process-local Work.Fence supplied by the task
// manager, not a reconstructed identity or a caller-provided authorization DTO.
// The executor releases the returned grant after its actual consumers retire.
func (s *Store) BeginTaskSourceOperation(ctx context.Context, childID string, fence func(OwnedTx) error) (context.Context, func() error, error) {
	if ctx == nil || s == nil || s.pool == nil || fence == nil || !validCatalogLibraryIdentifier(childID) || taskSourceGrant(ctx) != nil {
		return nil, nil, ErrInvalidInput
	}
	work, finish, err := s.beginMediaSourceLifetime(ctx)
	if err != nil {
		return nil, nil, err
	}
	grant := &internalTaskSourceGrant{store: s, ctx: work, childID: childID, finish: finish, fence: fence,
		roots: make(map[string]rootBindingRow), routes: make(map[string]primaryRootIORoute),
		anchors: make(map[string]*rootAnchorReference), registered: make(map[string]*rootAnchorReference)}
	fail := func(err error) (context.Context, func() error, error) {
		return nil, nil, errors.Join(err, grant.close())
	}
	s.mediaSourceOwners.mu.Lock()
	catalog := s.mediaSourceOwners.catalogScope
	s.mediaSourceOwners.mu.Unlock()
	s.mu.Lock()
	configured := make([]string, 0, len(s.roots))
	for _, root := range s.roots {
		configured = append(configured, root.path)
	}
	s.mu.Unlock()
	err = s.WithOwnedTx(work, func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT library_id FROM task_run_children
			WHERE id=$1 AND state='running' AND executor_token IS NOT NULL AND scan_job_id IS NULL`, childID).
			Scan(&grant.libraryID); err != nil {
			return err
		}
		if !validCatalogLibraryIdentifier(grant.libraryID) {
			return ErrUnavailable
		}
		rows, err := tx.Query(taskSourceOperationRootsSQL, grant.libraryID, storagebinding.MaxDocumentBytes, scanReconciliationMaxRoots+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row rootBindingRow
			if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
				&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy); err != nil {
				return err
			}
			if row.root.libraryID != grant.libraryID || len(grant.roots) == scanReconciliationMaxRoots {
				return ErrUnavailable
			}
			if _, err := row.validate(); err != nil {
				return err
			}
			// The pure classifier uses the startup configuration only.
			route, err := scanOperationRootRoute(catalog, row.root, configured)
			if err != nil {
				return err
			}
			grant.roots[row.root.id], grant.routes[row.root.id] = row, route
			grant.rootIDs = append(grant.rootIDs, row.root.id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		return fence(tx)
	})
	if err != nil {
		return fail(err)
	}
	// Freeze the complete ordering before borrowing any root. Startup failure
	// cleanup uses the same IDs, including roots opened before a later failure.
	sort.Strings(grant.rootIDs)
	if len(grant.rootIDs) != 0 {
		hints := make([]mediaSourceRootHint, 0, len(grant.rootIDs))
		for _, id := range grant.rootIDs {
			row := grant.roots[id]
			hints = append(hints, mediaSourceRootHint{root: row.root, bindingRevision: row.revision})
		}
		operation, err := s.preparePrimaryRootIO(work, hints)
		if err != nil {
			return fail(err)
		}
		for _, id := range grant.rootIDs {
			root := grant.roots[id].root
			err = operation.Run(work, id, primaryio.Background, func(phase context.Context) error {
				_, err := s.withLibraryRootAnchor(root, func(approved *os.Root) (*os.Root, error) {
					opened, err := openRegisteredRoot(approved, root.relativePath)
					if err != nil {
						return nil, err
					}
					s.mu.Lock()
					grant.anchors[id] = s.borrowRootAnchorLocked(approved)
					grant.registered[id] = s.borrowRootAnchorLocked(opened)
					// This private root is never installed in Store configuration.
					// Existing anchor retirement retains it until the grant releases
					// its live identity witness, including uncertain close results.
					s.retireRootAnchorLocked(opened)
					s.mu.Unlock()
					return nil, nil
				})
				return err
			})
			if err != nil {
				break
			}
		}
		if err := errors.Join(err, operation.Close()); err != nil {
			return fail(err)
		}
	}
	// Initialization may have waited on storage. Complete startup against the
	// same approval rows before publishing the private operation capability.
	err = s.WithOwnedTx(work, func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		rows, err := tx.Query(taskSourceOperationRootsSQL, grant.libraryID, storagebinding.MaxDocumentBytes, scanReconciliationMaxRoots+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var row rootBindingRow
			if err := rows.Scan(&row.root.id, &row.root.libraryID, &row.root.path, &row.root.allowedPath, &row.root.relativePath,
				&row.revision, &row.stored, &row.document, &row.boundAt, &row.boundBy); err != nil {
				return err
			}
			if expected, ok := grant.roots[row.root.id]; !ok || !expected.same(row) {
				return ErrRootBindingConflict
			}
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		if count != len(grant.roots) {
			return ErrRootBindingConflict
		}
		return fence(tx)
	})
	if err != nil {
		return fail(err)
	}
	revisions := make(map[string]int64, len(grant.roots))
	for id, row := range grant.roots {
		revisions[id] = row.revision
	}
	encoded, err := json.Marshal(revisions)
	if err != nil {
		return fail(err)
	}
	grant.revisions = string(encoded)
	if err := grant.check(work, s, childID); err != nil {
		return fail(err)
	}
	return context.WithValue(work, taskSourceOperationKey{}, grant), grant.close, nil
}

func taskSourceGrant(ctx context.Context) *internalTaskSourceGrant {
	if ctx == nil {
		return nil
	}
	grant, _ := ctx.Value(taskSourceOperationKey{}).(*internalTaskSourceGrant)
	return grant
}

func (grant *internalTaskSourceGrant) check(ctx context.Context, s *Store, childID string) error {
	if ctx == nil || grant == nil || grant.store != s || grant.childID != childID || grant.closed.Load() || !s.Available() {
		return ErrUnavailable
	}
	if err := grant.ctx.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

func (grant *internalTaskSourceGrant) close() error {
	grant.closeOnce.Do(func() {
		grant.mu.Lock()
		grant.closed.Store(true)
		grant.mu.Unlock()
		for _, id := range grant.rootIDs {
			if reference := grant.registered[id]; reference != nil {
				grant.closeErr = errors.Join(grant.closeErr, grant.store.releaseRootAnchor(reference))
			}
			if reference := grant.anchors[id]; reference != nil {
				grant.closeErr = errors.Join(grant.closeErr, grant.store.releaseRootAnchor(reference))
			}
		}
		grant.finish()
	})
	return grant.closeErr
}

func taskSourceBindingRevisions(ctx context.Context, s *Store, childID string) (string, bool, error) {
	grant := taskSourceGrant(ctx)
	if grant == nil {
		return "", false, nil
	}
	if err := grant.check(ctx, s, childID); err != nil {
		return "", true, err
	}
	return grant.revisions, true, nil
}

func (s *Store) taskSourceRootHint(ctx context.Context, root libraryRoot) (mediaSourceRootHint, bool, error) {
	grant := taskSourceGrant(ctx)
	if grant == nil {
		return mediaSourceRootHint{}, false, nil
	}
	if err := grant.check(ctx, s, grant.childID); err != nil {
		return mediaSourceRootHint{}, true, err
	}
	row, ok := grant.roots[root.id]
	if !ok || row.root != root {
		return mediaSourceRootHint{}, true, ErrSourceChanged
	}
	return mediaSourceRootHint{root: row.root, bindingRevision: row.revision}, true, nil
}

// Only physical catalog mappings are live here. The retained permission row's
// revision and approval document are intentionally absent from this check.
func checkTaskSourceOperationRoots(tx OwnedTx, ctx context.Context, s *Store, childID string) error {
	grant := taskSourceGrant(ctx)
	if grant == nil {
		return nil
	}
	if err := grant.check(ctx, s, childID); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id,library_id,path,allowed_path,relative_path FROM library_roots
		WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, grant.rootIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var current libraryRoot
		if err := rows.Scan(&current.id, &current.libraryID, &current.path, &current.allowedPath, &current.relativePath); err != nil {
			return err
		}
		if expected, ok := grant.roots[current.id]; !ok || expected.root != current {
			return ErrSourceChanged
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(grant.roots) {
		return ErrSourceChanged
	}
	return grant.check(ctx, s, childID)
}

// taskSourceRoute borrows an immutable route. Callers that return its slices to
// another consumer must copy them; scalar lane lookups need no allocation.
func (s *Store) taskSourceRoute(ctx context.Context, hint mediaSourceRootHint) (primaryRootIORoute, bool, error) {
	granted, active, err := s.taskSourceRootHint(ctx, hint.root)
	if !active || err != nil {
		return primaryRootIORoute{}, active, err
	}
	if hint.bindingRevision != granted.bindingRevision {
		return primaryRootIORoute{}, true, ErrSourceChanged
	}
	route := taskSourceGrant(ctx).routes[hint.root.id]
	return route, true, nil
}
