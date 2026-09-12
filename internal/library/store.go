package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/identity"
)

// New acquires exclusive catalog ownership before recovering interrupted jobs.
// Missing configured roots do not prevent server startup.
func New(pool *pgxpool.Pool, prober Prober, allowedRoots []string) (*Store, error) {
	if pool == nil || prober == nil {
		return nil, fmt.Errorf("%w: database and media prober are required", ErrInvalidInput)
	}
	s := &Store{pool: pool, prober: prober, active: make(map[string]*scanTask), queue: make(chan *scanTask, 128),
		scanUpdates: make(chan struct{}, 1), done: make(chan struct{})}
	seen := make(map[string]bool)
	for _, path := range allowedRoots {
		if strings.TrimSpace(path) == "" || strings.ContainsRune(path, '\x00') {
			return nil, fmt.Errorf("%w: configured roots must identify local directories", ErrInvalidInput)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid configured root", ErrInvalidInput)
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			absolute = canonical
		}
		absolute = filepath.Clean(absolute)
		if !seen[absolute] {
			seen[absolute] = true
			s.roots = append(s.roots, approvedRoot{path: absolute})
		}
	}
	// Prefer the narrowest approved root for nested configurations.
	sort.Slice(s.roots, func(i, j int) bool { return len(s.roots[i].path) > len(s.roots[j].path) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ownership, err := acquireScanOwnership(ctx, pool)
	if err != nil {
		return nil, err
	}
	s.ownership = ownership
	s.ctx, s.cancel = context.WithCancel(context.Background())
	if err := s.recoverTaskScans(ctx); err != nil {
		s.cancel()
		return nil, errors.Join(fmt.Errorf("recover interrupted scans: %w", err), ownership.release())
	}
	for i := 0; i < 2; i++ {
		s.workers.Add(1)
		go s.worker()
	}
	return s, nil
}

// CreateLibrary registers metadata only; it never modifies media directories.
func (s *Store) CreateLibrary(ctx context.Context, name, collectionType string, paths []string) (Library, error) {
	return s.createLibrary(ctx, nil, name, collectionType, paths)
}

// CreateLibraryAsAdministrator revalidates the API credential inside the owned
// catalog transaction and records the committed registration without paths.
func (s *Store) CreateLibraryAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, name, collectionType string, paths []string) (Library, error) {
	return s.createLibrary(ctx, &catalogAdministrator{actor: actor, audience: audience}, name, collectionType, paths)
}

func (s *Store) createLibrary(ctx context.Context, administrator *catalogAdministrator, name, collectionType string, paths []string) (Library, error) {
	return s.createLibraryWithCapture(ctx, administrator, name, collectionType, paths, captureRootBindingRegistrationTopology)
}

func (s *Store) createLibraryWithCapture(ctx context.Context, administrator *catalogAdministrator, name, collectionType string, paths []string, captureRoot rootBindingRegistrationCaptureFactory) (Library, error) {
	if ctx == nil || captureRoot == nil {
		return Library{}, ErrInvalidInput
	}
	if s == nil {
		return Library{}, ErrUnavailable
	}
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 128 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Library{}, fmt.Errorf("%w: library name must contain 1 to 128 printable characters", ErrInvalidInput)
	}
	collectionType, err := normalizeCollectionType(collectionType)
	if err != nil {
		return Library{}, err
	}
	if len(paths) < 1 || len(paths) > 32 {
		return Library{}, fmt.Errorf("%w: a library requires 1 to 32 directories", ErrInvalidInput)
	}
	if err := s.checkLibraryRegistrationAdministrator(ctx, administrator); err != nil {
		return Library{}, err
	}
	boundBy, err := rootBindingRegistrationActorID(administrator)
	if err != nil {
		return Library{}, err
	}
	id, err := randomID()
	if err != nil {
		return Library{}, err
	}
	roots := make([]*rootBindingRegistration, 0, len(paths))
	defer func() {
		for _, registration := range roots {
			_ = registration.Close()
		}
	}()
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return Library{}, err
		}
		registration, err := s.authorizePath(path)
		if err != nil {
			return Library{}, err
		}
		root := &registration.root
		overlaps := false
		for _, existing := range roots {
			if pathWithin(existing.root.path, root.path) || pathWithin(root.path, existing.root.path) {
				overlaps = true
				break
			}
		}
		roots = append(roots, registration)
		if overlaps {
			return Library{}, fmt.Errorf("%w: library directories must not overlap", ErrInvalidInput)
		}
		root.libraryID = id
		root.id, err = randomID()
		if err != nil {
			return Library{}, err
		}
		if err := registration.prepare(ctx, captureRoot); err != nil {
			return Library{}, err
		}
	}
	for _, registration := range roots {
		if err := registration.Revalidate(ctx); err != nil {
			return Library{}, err
		}
	}
	// Hold admission through commit and anchor publication. Filesystem capture
	// happened outside Store.mu; its revalidation never consults the Store.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Library{}, ErrUnavailable
	}
	for _, registration := range roots {
		if !s.rootBindingPathConfiguredLocked(registration.root.allowedPath) {
			return Library{}, ErrUnavailable
		}
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return Library{}, fmt.Errorf("begin library creation: %w", err)
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if err := administrator.check(protected, tx, true); err != nil {
		return Library{}, err
	}
	var createdAt time.Time
	if err := tx.QueryRow(protected, `INSERT INTO libraries (id, name, collection_type)
		VALUES ($1, $2, $3) RETURNING created_at`, id, name, collectionType).Scan(&createdAt); err != nil {
		return Library{}, fmt.Errorf("create library: %w", err)
	}
	if _, err := tx.Exec(protected, `INSERT INTO items (id, library_id, name, sort_name, type, is_folder)
		VALUES ($1, $1, $2, $3, 'CollectionFolder', true)`, id, name, strings.ToLower(name)); err != nil {
		return Library{}, fmt.Errorf("create library root item: %w", err)
	}
	library := Library{ID: id, Name: name, CollectionType: collectionType, CreatedAt: createdAt, Paths: make([]string, 0, len(roots))}
	for _, registration := range roots {
		root := registration.root
		var document, actor any
		if registration.document != nil {
			document, actor = string(registration.document), boundBy
		}
		if _, err := tx.Exec(protected, `INSERT INTO library_roots
			(id, library_id, path, allowed_path, relative_path, binding_revision, storage_binding, bound_at, bound_by)
			VALUES ($1, $2, $3, $4, $5, 1, $6::jsonb,
				CASE WHEN $6::jsonb IS NOT NULL THEN clock_timestamp() END, $7)`,
			root.id, id, root.path, root.allowedPath, root.relativePath, document, actor); err != nil {
			return Library{}, fmt.Errorf("register library directory: %w", err)
		}
		library.Paths = append(library.Paths, root.path)
	}
	event := administrator.event(activity.ActionLibraryCreated, activity.Resource{Kind: activity.ResourceLibrary, ID: id})
	event.Count = int64(len(roots))
	if err := activity.RecordOwned(catalogActivityTx{tx: tx}, event); err != nil {
		return Library{}, err
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogAdded, ItemID: id, LibraryID: id,
		IsFolder: true, IsCollectionFolder: true}); err != nil {
		return Library{}, err
	}
	for _, registration := range roots {
		if err := registration.Revalidate(protected); err != nil {
			return Library{}, err
		}
	}
	if err := administrator.check(protected, tx, false); err != nil {
		return Library{}, err
	}
	if err := tx.Commit(protected); err != nil {
		return Library{}, fmt.Errorf("commit library creation: %w", err)
	}
	for _, registration := range roots {
		s.installRootBindingAnchorLocked(registration.root, registration.anchor)
		registration.anchor = nil
	}
	sort.Strings(library.Paths)
	return library, nil
}

func (s *Store) ListLibraries(ctx context.Context) ([]Library, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+libraryColumns+" FROM libraries l ORDER BY lower(l.name), l.id")
	if err != nil {
		return nil, fmt.Errorf("list libraries: %w", err)
	}
	defer rows.Close()
	libraries := make([]Library, 0)
	for rows.Next() {
		library, err := scanLibrary(rows)
		if err != nil {
			return nil, fmt.Errorf("read library: %w", err)
		}
		libraries = append(libraries, library)
	}
	return libraries, rows.Err()
}

func (s *Store) GetLibrary(ctx context.Context, id string) (Library, error) {
	library, err := scanLibrary(s.pool.QueryRow(ctx, "SELECT "+libraryColumns+" FROM libraries l WHERE l.id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Library{}, ErrNotFound
	}
	if err != nil {
		return Library{}, fmt.Errorf("get library: %w", err)
	}
	return library, nil
}

// DeleteLibrary removes catalog records, retaining every file on disk. An active
// scan must be cancelled and reach its terminal status before deletion.
func (s *Store) DeleteLibrary(ctx context.Context, id string) error {
	return s.deleteLibrary(ctx, nil, id)
}

// DeleteLibraryAsAdministrator records removal only after a fresh credential
// check, retaining the same active-scan guard and catalog ownership.
func (s *Store) DeleteLibraryAsAdministrator(ctx context.Context, actor identity.Principal, audience identity.AdministratorAudience, id string) error {
	return s.deleteLibrary(ctx, &catalogAdministrator{actor: actor, audience: audience}, id)
}

func (s *Store) deleteLibrary(ctx context.Context, administrator *catalogAdministrator, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return fmt.Errorf("begin library deletion: %w", err)
	}
	defer rollback(tx)
	if err := administrator.check(ctx, tx, true); err != nil {
		return err
	}
	var exists string
	if err := tx.QueryRow(ctx, "SELECT id FROM libraries WHERE id = $1 FOR UPDATE", id).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock library: %w", err)
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return err
	}
	var active bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM scan_jobs WHERE library_id = $1 AND status IN ('Queued', 'Running'))`, id).Scan(&active); err != nil {
		return fmt.Errorf("read active library scan: %w", err)
	}
	if active {
		return ErrBusy
	}
	var paths int64
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM library_roots WHERE library_id = $1", id).Scan(&paths); err != nil {
		return fmt.Errorf("count removed library directories: %w", err)
	}
	if err := recordCatalogChanges(tx, CatalogChange{Kind: CatalogRemoved, ItemID: exists, LibraryID: exists,
		IsFolder: true, IsCollectionFolder: true}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM libraries WHERE id = $1", id); err != nil {
		return fmt.Errorf("delete library: %w", err)
	}
	event := administrator.event(activity.ActionLibraryRemoved, activity.Resource{Kind: activity.ResourceLibrary, ID: id})
	event.Count = paths
	if err := activity.RecordOwned(catalogActivityTx{tx: tx}, event); err != nil {
		return err
	}
	if err := administrator.check(ctx, tx, false); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.retireRootBindingAnchorsLocked(id)
	return nil
}

// Close cancels all owned jobs, drains the queue, then releases catalog ownership
// and root handles. A caller deadline does not abandon cleanup.
func (s *Store) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.cancel()
		close(s.queue)
		go func() {
			s.workers.Wait()
			ownershipErr := s.ownership.release()
			s.closeCatalogChangeListener()
			s.mu.Lock()
			s.shutdownErr = errors.Join(s.shutdownErr, ownershipErr)
			for _, root := range s.roots {
				if root.root != nil {
					_ = root.root.Close()
				}
			}
			s.retireRootBindingAnchorsLocked("")
			s.mu.Unlock()
			close(s.done)
		}()
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shutdownErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func normalizeCollectionType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "movie", "movies":
		return "movies", nil
	case "series", "tvshows":
		return "tvshows", nil
	case "audio", "music":
		return "music", nil
	case "", "mixed":
		return "mixed", nil
	default:
		return "", fmt.Errorf("%w: unsupported collection type", ErrInvalidInput)
	}
}

func randomID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate library identifier: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
