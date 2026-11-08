package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/primaryio"
)

// LibraryStorageDirectory reports an observation of one exact registered path.
// Decimal strings preserve every uint64 byte count for JavaScript clients.
// Capacity is independent of access and never measures directory contents.
type LibraryStorageDirectory struct {
	Path           string
	Available      bool
	Readable       *bool
	Writable       *bool
	TotalBytes     *string
	UsedBytes      *string
	AvailableBytes *string
}

type libraryStorageDirectoryObservationHook func(context.Context, *os.File) error

// WithLibraryStorageDirectoryObservationHook installs an immutable observation
// boundary callback. The borrowed descriptor must not be closed, retained, or
// used to modify media. The callback cannot replace the actual kernel result.
func WithLibraryStorageDirectoryObservationHook(hook func(context.Context, *os.File) error) Option {
	return func(settings *storeOptions) error {
		if hook == nil || settings.libraryStorageDirectoryHook != nil {
			return fmt.Errorf("%w: invalid or repeated library directory observation hook", ErrInvalidInput)
		}
		settings.libraryStorageDirectoryHook = hook
		return nil
	}
}

type libraryStorageDirectoryGroup struct {
	path     string
	roots    []mediaSourceRootHint
	conflict bool
}

type libraryStorageDirectoryObservation struct {
	readable *bool
	writable *bool
	capacity *directoryCapacity
}

type libraryStorageDirectoryDescriptorObserver func(context.Context, *os.File) (libraryStorageDirectoryObservation, error)

var errLibraryStorageDirectoryChanged = errors.New("registered library directory changed during observation")

// LibraryStorageDirectories reads only committed registrations. Filesystem work
// never holds a transaction or Store.mu, and a final fresh read rechecks native
// administrator authority and every root mapping before publishing capacity.
func (s *Store) LibraryStorageDirectories(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience) ([]LibraryStorageDirectory, error) {
	return s.libraryStorageDirectoriesWithObserver(ctx, &catalogAdministrator{actor: actor, audience: audience}, observeLibraryStorageDirectoryDescriptor)
}

func (s *Store) libraryStorageDirectoriesWithObserver(ctx context.Context, administrator *catalogAdministrator, observe libraryStorageDirectoryDescriptorObserver) ([]LibraryStorageDirectory, error) {
	if ctx == nil || observe == nil {
		return nil, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	initial, err := s.readLibraryStorageDirectoryRoots(ctx, administrator)
	if err != nil {
		return nil, err
	}
	groups := groupLibraryStorageDirectoryRoots(initial)
	observed := make(map[string]libraryStorageDirectoryObservation, len(groups))
	// Reserve part of the request deadline for a fresh authority and mapping
	// read even when the filesystem budget is exhausted by a stalled root.
	observationCtx, cancelObservation := context.WithTimeout(ctx, storageObservationTimeout)
	defer cancelObservation()
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if observationCtx.Err() != nil {
			break
		}
		if group.conflict {
			continue
		}
		observation, err := s.observeLibraryStorageDirectory(observationCtx, administrator, group.roots[0], observe)
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return nil, contextErr
			}
			if observationCtx.Err() != nil {
				break
			}
			return nil, err
		}
		observed[group.path] = observation
	}
	current, err := s.readLibraryStorageDirectoryRoots(ctx, administrator)
	if err != nil {
		return nil, err
	}
	before := make(map[string]libraryStorageDirectoryGroup, len(groups))
	for _, group := range groups {
		before[group.path] = group
	}
	currentGroups := groupLibraryStorageDirectoryRoots(current)
	items := make([]LibraryStorageDirectory, 0, len(currentGroups))
	for _, group := range currentGroups {
		item := LibraryStorageDirectory{Path: group.path}
		if previous, exists := before[group.path]; exists && sameLibraryStorageDirectoryGroup(previous, group) {
			item = projectLibraryStorageDirectory(group.path, observed[group.path])
		}
		items = append(items, item)
	}
	return items, ctx.Err()
}

func (s *Store) readLibraryStorageDirectoryRoots(ctx context.Context, administrator *catalogAdministrator) ([]mediaSourceRootHint, error) {
	s.mu.Lock()
	closed := s.closed || s.closing.Load()
	s.mu.Unlock()
	if closed {
		return nil, ErrUnavailable
	}
	tx, err := s.beginRootBindingRead(ctx, administrator)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, `SELECT `+rootBindingMetadataColumns+`
		FROM library_roots r JOIN libraries l ON l.id=r.library_id
		WHERE l.id <> $1 ORDER BY r.path, r.library_id, r.id LIMIT $2`, collectionLibraryID, maxRegisteredRootBindingList+1)
	if err != nil {
		return nil, fmt.Errorf("read library directory registrations: %w", err)
	}
	defer rows.Close()
	result := make([]mediaSourceRootHint, 0)
	for rows.Next() {
		var hint mediaSourceRootHint
		if err := rows.Scan(&hint.root.id, &hint.root.libraryID, &hint.root.path,
			&hint.root.allowedPath, &hint.root.relativePath, &hint.bindingRevision); err != nil {
			return nil, fmt.Errorf("read library directory registration: %w", err)
		}
		if len(result) == maxRegisteredRootBindingList || !validLibraryStorageDirectoryPath(hint.root.path) {
			return nil, ErrUnavailable
		}
		result = append(result, hint)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finish library directory registrations: %w", err)
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete library directory registrations: %w", err)
	}
	return result, nil
}

func validLibraryStorageDirectoryPath(path string) bool {
	return path != "" && len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		!strings.ContainsRune(path, '\x00') && !hasTraversal(path)
}

func groupLibraryStorageDirectoryRoots(roots []mediaSourceRootHint) []libraryStorageDirectoryGroup {
	groups := make([]libraryStorageDirectoryGroup, 0, len(roots))
	indices := make(map[string]int, len(roots))
	for _, hint := range roots {
		index, exists := indices[hint.root.path]
		if !exists {
			index = len(groups)
			indices[hint.root.path] = index
			groups = append(groups, libraryStorageDirectoryGroup{path: hint.root.path})
		}
		group := &groups[index]
		if (rootBindingRow{root: hint.root, revision: hint.bindingRevision}).validateMapping() != nil {
			group.conflict = true
		}
		if len(group.roots) != 0 {
			first := group.roots[0].root
			if first.allowedPath != hint.root.allowedPath || libraryStorageDirectoryRelative(first) != libraryStorageDirectoryRelative(hint.root) {
				group.conflict = true
			}
		}
		group.roots = append(group.roots, hint)
	}
	return groups
}

func libraryStorageDirectoryRelative(root libraryRoot) string {
	if root.relativePath == "" && root.path == root.allowedPath {
		return "."
	}
	return root.relativePath
}

func sameLibraryStorageDirectoryGroup(before, after libraryStorageDirectoryGroup) bool {
	if before.conflict || after.conflict || before.path != after.path || len(before.roots) != len(after.roots) {
		return false
	}
	for index := range before.roots {
		if before.roots[index] != after.roots[index] {
			return false
		}
	}
	return true
}

// A queued root is checked again after primary admission, before opening any
// directory. The read-only transaction ends before the worker is launched.
func (s *Store) recheckLibraryStorageDirectoryRoot(ctx context.Context, administrator *catalogAdministrator, expected mediaSourceRootHint) error {
	tx, err := s.beginRootBindingRead(ctx, administrator)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var current mediaSourceRootHint
	err = tx.QueryRow(ctx, `SELECT `+rootBindingMetadataColumns+`
		FROM library_roots r WHERE r.id=$1 AND r.library_id=$2`, expected.root.id, expected.root.libraryID).
		Scan(&current.root.id, &current.root.libraryID, &current.root.path, &current.root.allowedPath, &current.root.relativePath, &current.bindingRevision)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && current != expected {
		return errLibraryStorageDirectoryChanged
	}
	if err != nil {
		return fmt.Errorf("recheck library directory registration: %w", err)
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("complete library directory registration recheck: %w", err)
	}
	return nil
}

func (s *Store) observeLibraryStorageDirectory(ctx context.Context, administrator *catalogAdministrator, root mediaSourceRootHint, observe libraryStorageDirectoryDescriptorObserver) (libraryStorageDirectoryObservation, error) {
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{root})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return libraryStorageDirectoryObservation{}, contextErr
		}
		return libraryStorageDirectoryObservation{}, nil
	}
	observationCtx, cancel := context.WithTimeout(ctx, storageObservationTimeout)
	defer cancel()
	result := make(chan libraryStorageDirectoryObservation, 1)
	var authorityErr error
	hook := s.libraryStorageDirectoryHook
	err = operation.Run(observationCtx, root.root.id, primaryio.Foreground, func(phase context.Context) error {
		if err := s.recheckLibraryStorageDirectoryRoot(phase, administrator, root); err != nil {
			if !errors.Is(err, errLibraryStorageDirectoryChanged) {
				authorityErr = err
			}
			return err
		}
		return runStorageObservation(phase, nil, func(work context.Context) error {
			value, err := observeNamedLibraryStorageDirectory(work, root.root, observe, hook)
			if err == nil {
				result <- value
			}
			return err
		})
	})
	closeErr := operation.Close()
	if contextErr := ctx.Err(); contextErr != nil {
		return libraryStorageDirectoryObservation{}, contextErr
	}
	if authorityErr != nil {
		return libraryStorageDirectoryObservation{}, authorityErr
	}
	if err != nil || closeErr != nil {
		return libraryStorageDirectoryObservation{}, nil
	}
	return <-result, nil
}

// This worker owns every open, syscall, revalidation and close. No name is
// reopened to obtain capacity, and no content entry is read or created.
func observeNamedLibraryStorageDirectory(ctx context.Context, root libraryRoot, observe libraryStorageDirectoryDescriptorObserver, hook libraryStorageDirectoryObservationHook) (value libraryStorageDirectoryObservation, resultErr error) {
	if err := ctx.Err(); err != nil {
		return value, err
	}
	// Legacy root registrations may use an empty relative path for the anchor.
	// Keep database comparisons exact while using the equivalent lease mapping.
	root.relativePath = libraryStorageDirectoryRelative(root)
	approved := approvedRoot{path: root.allowedPath}
	if err := openApprovedRoot(&approved); err != nil {
		return failedLibraryStorageDirectoryObservation(ctx, err)
	}
	lease := &libraryRootLease{approved: approved.root, relativePath: root.relativePath}
	registration := &rootBindingRegistration{root: root, lease: lease}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, registration.Close)) }()
	registered, err := lease.Open()
	if err != nil {
		return failedLibraryStorageDirectoryObservation(ctx, err)
	}
	registration.registered = registered
	directory, err := registered.Open(".")
	if err != nil {
		return failedLibraryStorageDirectoryObservation(ctx, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(ctx, directory.Close)) }()
	if hook != nil {
		err = hook(ctx, directory)
	}
	if err == nil {
		value, err = observe(ctx, directory)
	}
	if err != nil {
		value, err = failedLibraryStorageDirectoryObservation(ctx, err)
		if err != nil {
			return libraryStorageDirectoryObservation{}, err
		}
	}
	if err := registration.Revalidate(ctx); err != nil {
		return libraryStorageDirectoryObservation{}, err
	}
	return value, ctx.Err()
}

func observeLibraryStorageDirectoryDescriptor(ctx context.Context, directory *os.File) (libraryStorageDirectoryObservation, error) {
	readable, err := observeDirectoryReadAccess(ctx, directory)
	if err != nil {
		return libraryStorageDirectoryObservation{}, err
	}
	value := libraryStorageDirectoryObservation{readable: readable}
	if readable != nil && !*readable {
		return value, nil
	}
	value.writable, err = observeDirectoryWriteAccess(ctx, directory)
	if err != nil {
		return libraryStorageDirectoryObservation{}, err
	}
	value.capacity, err = observeDirectoryCapacity(ctx, directory)
	return value, err
}

func failedLibraryStorageDirectoryObservation(ctx context.Context, err error) (libraryStorageDirectoryObservation, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return libraryStorageDirectoryObservation{}, contextErr
	}
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		readable := false
		return libraryStorageDirectoryObservation{readable: &readable}, nil
	}
	return libraryStorageDirectoryObservation{}, err
}

func projectLibraryStorageDirectory(path string, value libraryStorageDirectoryObservation) LibraryStorageDirectory {
	item := LibraryStorageDirectory{Path: path, Readable: value.readable, Writable: value.writable}
	item.Available = item.Readable != nil && *item.Readable
	if value.capacity != nil {
		total := strconv.FormatUint(value.capacity.totalBytes, 10)
		used := strconv.FormatUint(value.capacity.usedBytes, 10)
		available := strconv.FormatUint(value.capacity.availableBytes, 10)
		item.TotalBytes, item.UsedBytes, item.AvailableBytes = &total, &used, &available
	}
	return item
}
