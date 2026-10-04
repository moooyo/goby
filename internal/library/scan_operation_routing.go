package library

import (
	"context"
	"path/filepath"

	"github.com/moooyo/goby/internal/primaryio"
)

// scanOperationRootRoute classifies a committed root using only the configured
// anchors captured at scan startup. Mutable Store configuration and filesystem
// observations cannot change the operation's governing domains.
func scanOperationRootRoute(catalog string, root libraryRoot, configured []string) (primaryRootIORoute, error) {
	if catalog == "" || root.id == "" {
		return primaryRootIORoute{}, ErrUnavailable
	}
	exact := false
	domain := root.allowedPath
	for _, approved := range configured {
		if approved == root.allowedPath {
			exact = true
		}
		if pathWithin(approved, root.allowedPath) && len(approved) < len(domain) {
			domain = approved
		}
	}
	if !exact || domain == "" || !filepath.IsAbs(domain) {
		return primaryRootIORoute{}, ErrUnavailable
	}
	route := primaryio.Route{Roots: []primaryio.RootKey{{Catalog: catalog, RootID: root.id}}}
	seen := make(map[string]bool)
	addDomain := func(path string) {
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			route.Domains = append(route.Domains, path)
		}
	}
	addDomain(domain)
	for _, approved := range configured {
		if pathWithin(approved, root.allowedPath) {
			addDomain(approved)
		}
	}
	if len(route.Domains) > 16 {
		return primaryRootIORoute{}, ErrUnavailable
	}
	return primaryRootIORoute{route: route, domain: filepath.Clean(domain)}, nil
}

// copyRoute returns a private route copy after the caller has checked the
// operation and exact root against this immutable grant.
func (grant *scanOperationAuthority) copyRoute(rootID string) (primaryRootIORoute, error) {
	prepared, ok := grant.routes[rootID]
	if !ok {
		return primaryRootIORoute{}, ErrUnavailable
	}
	prepared.route.Roots = append([]primaryio.RootKey(nil), prepared.route.Roots...)
	prepared.route.Domains = append([]string(nil), prepared.route.Domains...)
	return prepared, nil
}

// prepareScanOperationRootIO retains only routes captured by this scan's initial
// authorization. Each hint must exactly match its granted root and revision.
func (s *Store) prepareScanOperationRootIO(ctx context.Context, task *scanTask, hints []mediaSourceRootHint) (*PrimaryRootIO, error) {
	if ctx == nil || s == nil || task == nil || len(hints) < 1 || len(hints) > scanReconciliationMaxRoots {
		return nil, ErrInvalidInput
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Each Run carries its own cancellation. Store lifetime admission retains
	// idle handoffs and mandatory cleanup until the operation actually retires.
	work, finish, err := s.beginMediaSourceLifetime(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	if err := s.prepareScanOperationAuthority(ctx, task, nil); err != nil {
		finish()
		return nil, err
	}
	grant := task.authority.Load()
	routes := make(map[string]primaryRootIORoute, len(hints))
	for _, hint := range hints {
		if err := ctx.Err(); err != nil {
			finish()
			return nil, err
		}
		if (rootBindingRow{root: hint.root, revision: hint.bindingRevision}).validateMapping() != nil {
			finish()
			return nil, ErrUnavailable
		}
		if _, exists := routes[hint.root.id]; exists {
			finish()
			return nil, ErrInvalidInput
		}
		row, ok := grant.roots[hint.root.id]
		if !ok || row.root != hint.root || row.revision != hint.bindingRevision {
			finish()
			return nil, ErrRootBindingConflict
		}
		route, ok := grant.routes[hint.root.id]
		if !ok {
			finish()
			return nil, ErrUnavailable
		}
		routes[hint.root.id] = route
	}
	if err := s.checkScanOperationActive(ctx, task, grant); err != nil {
		finish()
		return nil, err
	}
	return newPrimaryRootIO(work, originalMediaReadOwners, &originalMediaReadDomains, routes, finish)
}
