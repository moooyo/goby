package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/moooyo/goby/internal/primaryio"
)

func (s *Store) primaryReadRouteContext(ctx context.Context, hint mediaSourceRootHint) (primaryio.Route, error) {
	if prepared, granted, err := s.taskSourceRoute(ctx, hint); granted {
		return prepared.route, err
	}
	return s.primaryReadRoute(hint)
}

func (s *Store) mediaSourceRootLaneContext(ctx context.Context, hint mediaSourceRootHint) (mediaSourceRootKey, string, error) {
	if prepared, granted, err := s.taskSourceRoute(ctx, hint); granted {
		if err != nil || len(prepared.route.Roots) != 1 {
			return mediaSourceRootKey{}, "", errors.Join(ErrUnavailable, err)
		}
		root := prepared.route.Roots[0]
		return mediaSourceRootKey{catalog: root.Catalog, id: root.RootID}, prepared.domain, nil
	}
	return s.mediaSourceRootLane(hint)
}

func (s *Store) leaseLibraryRootContext(ctx context.Context, root libraryRoot) (*libraryRootLease, error) {
	if _, granted, err := s.taskSourceRootHint(ctx, root); !granted {
		return s.leaseLibraryRoot(root)
	} else if err != nil {
		return nil, err
	}
	grant := taskSourceGrant(ctx)
	// Keep the exact anchor borrowed until the clone finishes. Close waits for
	// this actual open; cancellation alone cannot retire its descriptor.
	grant.mu.Lock()
	defer grant.mu.Unlock()
	if err := grant.check(ctx, s, grant.childID); err != nil {
		return nil, err
	}
	reference := grant.anchors[root.id]
	if reference == nil || reference.approved == nil || grant.registered[root.id] == nil {
		return nil, ErrUnavailable
	}
	held, err := reference.approved.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	identity, err := grant.registered[root.id].approved.OpenRoot(".")
	if err != nil {
		return nil, errors.Join(err, closeDirectoryPrimaryResource(ctx, held.Close))
	}
	return &libraryRootLease{approved: held, relativePath: root.relativePath,
		expectedRoot: identity, expectedAnchorPath: root.allowedPath}, nil
}

// The retained anchor supplies containment. Its original configured name must
// still denote that physical directory, independently from mutable permission.
func checkTaskSourceAnchorName(path string, approved *os.Root) error {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || filepath.Clean(canonical) != path {
		return errors.Join(ErrSourceChanged, err)
	}
	current, err := os.Stat(path)
	if err != nil {
		return errors.Join(ErrSourceChanged, err)
	}
	held, err := approved.Stat(".")
	if err != nil || !os.SameFile(current, held) {
		return errors.Join(ErrSourceChanged, err)
	}
	return nil
}

func (s *Store) openLibraryRootContext(ctx context.Context, root libraryRoot) (*os.Root, error) {
	if taskSourceGrant(ctx) == nil {
		return s.openLibraryRoot(root)
	}
	lease, err := s.leaseLibraryRootContext(ctx, root)
	if err != nil {
		return nil, err
	}
	opened, openErr := lease.Open()
	closeErr := closeDirectoryPrimaryResource(ctx, lease.Close)
	if openErr != nil || closeErr != nil {
		if opened != nil {
			closeErr = errors.Join(closeErr, closeDirectoryPrimaryResource(ctx, opened.Close))
		}
		return nil, errors.Join(openErr, closeErr)
	}
	return opened, nil
}
