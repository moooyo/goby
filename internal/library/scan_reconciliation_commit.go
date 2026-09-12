package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

const (
	scanReconciliationMaxRoots = 256
	scanReconciliationMaxItems = 32768
	scanReconciliationMaxBytes = 32 << 20
	scanReconciliationMaxDepth = 128
	// Cover row slices, closure/ancestor maps, proof scratch and removal facts.
	scanReconciliationItemBytes = 1536
)

// Only marked observation failures can become a skipped deletion pass. A
// database or rollback failure joined by the owned transaction remains fatal.
type scanReconciliationObservationFailure struct{ err error }

func (failure scanReconciliationObservationFailure) Error() string { return failure.err.Error() }

func (failure scanReconciliationObservationFailure) Unwrap() error { return failure.err }

func scanReconciliationObservationOnly(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if _, ok := err.(scanReconciliationObservationFailure); ok {
		return true
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return false
	}
	found := false
	for _, child := range joined.Unwrap() {
		if child != nil {
			if !scanReconciliationObservationOnly(child) {
				return false
			}
			found = true
		}
	}
	return found
}

func scanReconciliationUnavailable(message string) error {
	return scanReconciliationObservationFailure{fmt.Errorf("%w: %s", errScanReconciliationEvidenceUnavailable, message)}
}

func scanReconciliationBudget() error {
	return scanReconciliationObservationFailure{errScanReconciliationEvidenceBudget}
}

func scanReconciliationObservation(ctx context.Context, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if err == nil {
		return nil
	}
	if !errors.Is(err, errScanReconciliationEvidenceUnavailable) && !errors.Is(err, errScanReconciliationEvidenceBudget) {
		err = errors.Join(errScanReconciliationEvidenceUnavailable, err)
	}
	return scanReconciliationObservationFailure{err}
}

type scanReconciliationBudgetState struct {
	items int
	bytes int
}

func (budget *scanReconciliationBudgetState) charge(bytes int) error {
	if bytes < 0 || bytes > scanReconciliationMaxBytes-budget.bytes {
		return scanReconciliationBudget()
	}
	budget.bytes += bytes
	return nil
}

type scanReconciliationItem struct {
	id, libraryID, rootID, parentID        string
	typeName, path, relative               string
	themeOwner, extraOwner                 string
	isFolder, ordinary, roleValid, visible bool
	oversized                              bool
}

func (item scanReconciliationItem) fact() CatalogChange {
	parent := item.parentID
	if !item.ordinary {
		parent = ""
	}
	return CatalogChange{Kind: CatalogRemoved, ItemID: item.id, LibraryID: item.libraryID,
		ParentID: parent, IsFolder: item.isFolder, IsCollectionFolder: item.typeName == "CollectionFolder"}
}

func (budget *scanReconciliationBudgetState) retain(item scanReconciliationItem) error {
	if item.oversized || budget.items >= scanReconciliationMaxItems {
		return scanReconciliationBudget()
	}
	if err := budget.charge(scanReconciliationItemBytes + len(item.id) + len(item.libraryID) + len(item.rootID) +
		len(item.parentID) + len(item.typeName) + len(item.path) + len(item.relative) + len(item.themeOwner) + len(item.extraOwner)); err != nil {
		return err
	}
	budget.items++
	return nil
}

// Bound text in PostgreSQL before transferring it. Invalid persisted fields
// produce an explicit overflow flag, never a truncated deletion identity.
var scanReconciliationItemColumns = `
	CASE WHEN octet_length(i.id)<=256 THEN i.id ELSE '' END,
	CASE WHEN octet_length(i.library_id)<=256 THEN i.library_id ELSE '' END,
	CASE WHEN octet_length(i.root_id)<=256 THEN i.root_id ELSE '' END,
	CASE WHEN octet_length(i.parent_id)<=256 THEN i.parent_id ELSE '' END,
	CASE WHEN octet_length(i.type)<=64 THEN i.type ELSE '' END,
	CASE WHEN octet_length(i.path)<=4096 THEN i.path ELSE '' END,
	CASE WHEN octet_length(i.relative_path)<=4096 THEN i.relative_path ELSE '' END,
	CASE WHEN octet_length(theme.owner_item_id)<=256 THEN theme.owner_item_id ELSE '' END,
	CASE WHEN octet_length(extra.owner_item_id)<=256 THEN extra.owner_item_id ELSE '' END,
	i.is_folder, ` + ordinaryItemSQL("i") + `,
	(` + ordinaryItemSQL("i") + ` OR ` + database.ThemeResourceItemSQL("i", false) + ` OR ` + database.ExtraResourceItemSQL("i", false) + `),
	` + directItemSQL("i") + `,
	(octet_length(i.id)>256 OR octet_length(i.library_id)>256 OR COALESCE(octet_length(i.root_id)>256,false)
	OR COALESCE(octet_length(i.parent_id)>256,false) OR octet_length(i.type)>64 OR octet_length(i.path)>4096
	OR octet_length(i.relative_path)>4096 OR COALESCE(octet_length(theme.owner_item_id)>256,false)
	OR COALESCE(octet_length(extra.owner_item_id)>256,false))`

func readScanReconciliationItems(tx OwnedTx, budget *scanReconciliationBudgetState, predicate string, argument any) ([]scanReconciliationItem, error) {
	rows, err := tx.Query(`SELECT `+scanReconciliationItemColumns+`
		FROM items i LEFT JOIN item_theme_resources theme ON theme.resource_item_id=i.id
		LEFT JOIN item_extra_resources extra ON extra.resource_item_id=i.id
		WHERE `+predicate+` ORDER BY i.id LIMIT $2 FOR UPDATE OF i`, argument, scanReconciliationMaxItems-budget.items+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []scanReconciliationItem
	for rows.Next() {
		var item scanReconciliationItem
		var root, parent, themeOwner, extraOwner *string
		if err := rows.Scan(&item.id, &item.libraryID, &root, &parent, &item.typeName, &item.path, &item.relative,
			&themeOwner, &extraOwner, &item.isFolder, &item.ordinary, &item.roleValid, &item.visible, &item.oversized); err != nil {
			return nil, err
		}
		if root != nil {
			item.rootID = *root
		}
		if parent != nil {
			item.parentID = *parent
		}
		if themeOwner != nil {
			item.themeOwner = *themeOwner
		}
		if extraOwner != nil {
			item.extraOwner = *extraOwner
		}
		if err := budget.retain(item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// reconcileMissingScanItems follows all root walks, accepted cross-root moves
// and collection-theme completion. Its proof is bounded observation, not an
// atomic lock shared by the filesystem and PostgreSQL. No media is removed.
func (s *Store) reconcileMissingScanItems(task *scanTask, library Library, captures []*rootBindingScanCapture, evidence *scanReconciliationEvidence, musicParents map[string]bool) ([]string, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	if task == nil || task.ctx == nil || !validCatalogLibraryIdentifier(task.job.ID) ||
		!validCatalogLibraryIdentifier(library.ID) || task.job.LibraryID != library.ID {
		return nil, ErrInvalidInput
	}
	budget := &scanReconciliationBudgetState{}
	roots, err := scanReconciliationCapturedRoots(task.ctx, library.ID, captures, budget)
	if err != nil {
		return nil, err
	}
	if err := revalidateScanReconciliation(task.ctx, captures, evidence); err != nil {
		return nil, err
	}
	// Admission must precede the owner mutex. The callback never acquires
	// Store.mu, including its final observations and original-context checks.
	s.mu.Lock()
	err = s.admitScanReconciliationLocked(task, roots)
	var raw pgx.Tx
	if err == nil {
		raw, err = s.beginOwnedTx(task.ctx)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var albums []string
	err = s.withOwnedTxCallback(raw, func(tx OwnedTx) error {
		if err := lockScanReconciliationTask(tx, task, library.ID); err != nil {
			return err
		}
		if err := validateScanReconciliationRoots(tx, library, roots); err != nil {
			return err
		}
		if err := revalidateScanReconciliation(task.ctx, captures, evidence); err != nil {
			return err
		}
		candidates, err := readScanReconciliationItems(tx, budget, `i.library_id=$1 AND i.root_id IS NOT NULL
			AND i.type<>'CollectionFolder' AND i.path<>'' AND i.relative_path<>''
			AND left(i.path,2)<>'//' AND left(i.relative_path,2)<>'//' AND `+ordinaryItemSQL("i"), library.ID)
		if err != nil {
			return err
		}
		members := make(map[string]scanReconciliationItem)
		for _, item := range candidates {
			if evidence.Seen(item.id) {
				continue
			}
			if err := validateScanReconciliationPhysical(item, library.ID, roots); err != nil {
				return err
			}
			absent, err := evidence.PathAbsent(task.ctx, item.rootID, item.relative)
			if err != nil {
				return scanReconciliationObservation(task.ctx, err)
			}
			if absent {
				members[item.id] = item
			}
		}
		if len(members) == 0 {
			return task.ctx.Err()
		}
		if err := expandScanReconciliation(tx, task.ctx, library.ID, roots, evidence, budget, members); err != nil {
			return err
		}
		ancestors, err := readScanReconciliationAncestors(tx, library.ID, roots, budget, members)
		if err != nil {
			return err
		}
		albums, err = scanReconciliationAlbums(members, ancestors)
		if err != nil {
			return err
		}
		ids := scanReconciliationIDs(members)
		before, err := readAuxiliaryCatalogSnapshot(task.ctx, raw, ids)
		if err != nil {
			return err
		}
		if err := validateScanReconciliationRoots(tx, library, roots); err != nil {
			return err
		}
		if err := revalidateScanReconciliation(task.ctx, captures, evidence); err != nil {
			return err
		}
		if err := proveScanReconciliationMembers(task.ctx, library.ID, roots, evidence, members); err != nil {
			return err
		}
		if _, supportsMusic := s.prober.(interface{ MusicMetadataVersion() int }); supportsMusic {
			// Preflight the exact post-deletion album membership before committing
			// any removal. Keep the caller's pending refresh set unchanged until
			// the owned transaction succeeds and returns its surviving albums.
			parents := make(map[string]bool)
			addParent := func(id string) error {
				if parents[id] {
					return nil
				}
				if !validCatalogLibraryIdentifier(id) {
					return scanReconciliationUnavailable("pending music parent has an invalid identity")
				}
				if len(parents) >= scanReconciliationMaxItems {
					return scanReconciliationBudget()
				}
				if err := budget.charge(128 + len(id)); err != nil {
					return err
				}
				parents[id] = true
				return nil
			}
			for id := range musicParents {
				if err := addParent(id); err != nil {
					return err
				}
			}
			for _, id := range albums {
				if err := addParent(id); err != nil {
					return err
				}
			}
			if err := checkScanReconciliationMusic(task.ctx, raw, library.ID, parents, members); err != nil {
				return err
			}
			if err := task.ctx.Err(); err != nil {
				return err
			}
		}
		// Capture authoritative removal facts before any cascading DELETE. The
		// auxiliary snapshot supplies surviving semantic-owner invalidations.
		facts := make([]CatalogChange, 0, len(members))
		for _, id := range ids {
			if item := members[id]; item.visible {
				facts = append(facts, item.fact())
			}
		}
		if err := recordCatalogChanges(raw, facts...); err != nil {
			return err
		}
		rootIDs := make([]string, 0, len(roots))
		for id := range roots {
			rootIDs = append(rootIDs, id)
		}
		if _, err := tx.Exec(`DELETE FROM items WHERE id=ANY($1::text[]) AND library_id=$2 AND root_id=ANY($3::text[])`, ids, library.ID, rootIDs); err != nil {
			return err
		}
		var retained bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM items WHERE id=ANY($1::text[]))`, ids).Scan(&retained); err != nil {
			return err
		}
		if retained {
			return scanReconciliationUnavailable("validated deletion members were retained")
		}
		if err := before.record(task.ctx, raw, nil); err != nil {
			return err
		}
		if err := lockScanReconciliationTask(tx, task, library.ID); err != nil {
			return err
		}
		if err := validateScanReconciliationRoots(tx, library, roots); err != nil {
			return err
		}
		if err := revalidateScanReconciliation(task.ctx, captures, evidence); err != nil {
			return err
		}
		if err := proveScanReconciliationMembers(task.ctx, library.ID, roots, evidence, members); err != nil {
			return err
		}
		// SQL uses the owned transaction's protected context. Cancellation of
		// the original scan must still roll back even after successful DELETE.
		return task.ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	return albums, nil
}

func scanReconciliationCapturedRoots(ctx context.Context, libraryID string, captures []*rootBindingScanCapture, budget *scanReconciliationBudgetState) (map[string]*rootBindingScanCapture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(captures) == 0 {
		return nil, scanReconciliationUnavailable("no complete registered root set")
	}
	if len(captures) > scanReconciliationMaxRoots {
		return nil, scanReconciliationBudget()
	}
	roots := make(map[string]*rootBindingScanCapture, len(captures))
	for _, capture := range captures {
		if capture == nil || capture.closed || capture.status != RootBindingVerified || capture.row.root.libraryID != libraryID ||
			!validCatalogLibraryIdentifier(capture.row.root.id) || roots[capture.row.root.id] != nil {
			return nil, scanReconciliationUnavailable("root capture is missing, repeated or unverified")
		}
		approved, err := capture.row.validate()
		if err != nil {
			return nil, err
		}
		if approved == nil {
			return nil, scanReconciliationUnavailable("root capture has no persisted approval")
		}
		if err := budget.charge(1024 + len(capture.row.document) + len(capture.row.root.path) + len(capture.row.root.allowedPath)); err != nil {
			return nil, err
		}
		roots[capture.row.root.id] = capture
	}
	return roots, nil
}

func (s *Store) admitScanReconciliationLocked(task *scanTask, roots map[string]*rootBindingScanCapture) error {
	if err := task.ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrUnavailable
	}
	if s.active[task.job.ID] != task || task.job.Status != "Running" {
		return ErrTaskScanInactive
	}
	if task.job.CancelRequested {
		return context.Canceled
	}
	for _, capture := range roots {
		if !s.rootBindingPathConfiguredLocked(capture.row.root.allowedPath) {
			return ErrUnavailable
		}
	}
	return nil
}

func lockScanReconciliationTask(tx OwnedTx, task *scanTask, libraryID string) error {
	if err := task.ctx.Err(); err != nil {
		return err
	}
	relation, err := lockTaskScanRelation(tx, task.job.ID, task.job.TaskChildID)
	if err != nil {
		return err
	}
	if relation.missing || relation.job.ID != task.job.ID || relation.job.LibraryID != libraryID ||
		relation.job.TaskChildID != task.job.TaskChildID || relation.job.Status != "Running" {
		return ErrTaskScanInactive
	}
	if relation.job.CancelRequested || relation.child != nil && (!activeTaskRun(relation.child.runState) || relation.child.state != "running") {
		return context.Canceled
	}
	return task.ctx.Err()
}

func validateScanReconciliationRoots(tx OwnedTx, library Library, roots map[string]*rootBindingScanCapture) error {
	var collectionType string
	if err := tx.QueryRow(`SELECT collection_type FROM libraries WHERE id=$1 FOR UPDATE`, library.ID).Scan(&collectionType); err != nil {
		return err
	}
	if collectionType != library.CollectionType {
		return ErrRootBindingConflict
	}
	// Locking the library also fences insertion of additional registered roots
	// through their foreign key, while exact existing rows are locked below.
	rows, err := tx.Query(`SELECT CASE WHEN octet_length(id)<=256 THEN id ELSE '' END
		FROM library_roots WHERE library_id=$1 ORDER BY id LIMIT $2 FOR UPDATE`, library.ID, scanReconciliationMaxRoots+1)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		if len(ids) >= scanReconciliationMaxRoots {
			rows.Close()
			return scanReconciliationBudget()
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 || len(ids) != len(roots) {
		return ErrRootBindingConflict
	}
	for _, id := range ids {
		capture := roots[id]
		if capture == nil {
			return ErrRootBindingConflict
		}
		current, err := readRootBindingScanRow(tx, library.ID, id)
		if err != nil {
			return err
		}
		if !capture.row.same(current) {
			return ErrRootBindingConflict
		}
	}
	return nil
}

func revalidateScanReconciliation(ctx context.Context, captures []*rootBindingScanCapture, evidence *scanReconciliationEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if evidence == nil {
		return scanReconciliationUnavailable("directory evidence is missing")
	}
	if err := evidence.Err(); err != nil {
		return scanReconciliationObservation(ctx, err)
	}
	if len(evidence.roots) != len(captures) {
		return scanReconciliationUnavailable("directory evidence has a different registered root set")
	}
	for _, capture := range captures {
		if err := capture.Revalidate(ctx); err != nil {
			return scanReconciliationObservation(ctx, err)
		}
		root := evidence.roots[capture.row.root.id]
		if root == nil || root.anchor == nil {
			return scanReconciliationUnavailable("directory evidence has no matching root anchor")
		}
		info, err := capture.opened.Stat(".")
		if err != nil || !scanReconciliationSameInfo(root.info, info) {
			return scanReconciliationUnavailable("directory evidence belongs to a different approved root")
		}
	}
	return scanReconciliationObservation(ctx, evidence.Revalidate(ctx))
}

func validateScanReconciliationPhysical(item scanReconciliationItem, libraryID string, roots map[string]*rootBindingScanCapture) error {
	if !validCatalogLibraryIdentifier(item.id) || item.libraryID != libraryID || roots[item.rootID] == nil ||
		item.parentID != "" && !validCatalogLibraryIdentifier(item.parentID) || item.typeName == "CollectionFolder" ||
		!item.roleValid || item.path == "" || strings.HasPrefix(item.path, "//") {
		return scanReconciliationUnavailable("deletion member has an unproven scope or role")
	}
	relative, valid := scanReconciliationRelative(item.relative, false)
	if !valid || relative != item.relative || item.relative == "." ||
		filepath.Join(roots[item.rootID].row.root.path, filepath.FromSlash(relative)) != item.path {
		return scanReconciliationUnavailable("deletion member has no exact physical path")
	}
	return nil
}

func proveScanReconciliationMembers(ctx context.Context, libraryID string, roots map[string]*rootBindingScanCapture, evidence *scanReconciliationEvidence, members map[string]scanReconciliationItem) error {
	for _, item := range members {
		if err := validateScanReconciliationPhysical(item, libraryID, roots); err != nil {
			return err
		}
		if evidence.Seen(item.id) {
			return scanReconciliationUnavailable("a deletion descendant was observed during this scan")
		}
		absent, err := evidence.PathAbsent(ctx, item.rootID, item.relative)
		if err != nil {
			return scanReconciliationObservation(ctx, err)
		}
		if !absent {
			return scanReconciliationUnavailable("a deletion descendant has no positive absence proof")
		}
	}
	return ctx.Err()
}

func scanReconciliationIDs(items map[string]scanReconciliationItem) []string {
	ids := make([]string, 0, len(items))
	for id := range items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func expandScanReconciliation(tx OwnedTx, ctx context.Context, libraryID string, roots map[string]*rootBindingScanCapture, evidence *scanReconciliationEvidence, budget *scanReconciliationBudgetState, members map[string]scanReconciliationItem) error {
	frontier := scanReconciliationIDs(members)
	for depth := 0; len(frontier) != 0; depth++ {
		if depth >= scanReconciliationMaxDepth {
			return scanReconciliationBudget()
		}
		// Do not filter by library or active status: the parent and auxiliary
		// owner foreign keys can remove foreign children or inactive history.
		items, err := readScanReconciliationItems(tx, budget, `(i.parent_id=ANY($1::text[])
			OR theme.owner_item_id=ANY($1::text[]) OR extra.owner_item_id=ANY($1::text[]))`, frontier)
		if err != nil {
			return err
		}
		frontier = nil
		for _, item := range items {
			if _, exists := members[item.id]; !exists {
				members[item.id] = item
				frontier = append(frontier, item.id)
			}
		}
	}
	return proveScanReconciliationMembers(ctx, libraryID, roots, evidence, members)
}

func readScanReconciliationAncestors(tx OwnedTx, libraryID string, roots map[string]*rootBindingScanCapture, budget *scanReconciliationBudgetState, members map[string]scanReconciliationItem) (map[string]scanReconciliationItem, error) {
	known := make(map[string]scanReconciliationItem, len(members))
	for id, item := range members {
		known[id] = item
	}
	frontier := members
	for depth := 0; len(frontier) != 0; depth++ {
		missing := make(map[string]scanReconciliationItem)
		for _, item := range frontier {
			if item.parentID != "" {
				if _, exists := known[item.parentID]; !exists {
					missing[item.parentID] = scanReconciliationItem{}
				}
			}
		}
		if len(missing) == 0 {
			break
		}
		if depth >= scanReconciliationMaxDepth {
			return nil, scanReconciliationBudget()
		}
		items, err := readScanReconciliationItems(tx, budget, `i.id=ANY($1::text[])`, scanReconciliationIDs(missing))
		if err != nil {
			return nil, err
		}
		if len(items) != len(missing) {
			return nil, scanReconciliationUnavailable("deletion ancestor is missing")
		}
		frontier = make(map[string]scanReconciliationItem, len(items))
		for _, item := range items {
			if item.libraryID != libraryID || !item.ordinary || !validCatalogLibraryIdentifier(item.id) ||
				(item.typeName != "CollectionFolder" && roots[item.rootID] == nil) ||
				(item.typeName == "CollectionFolder" && (item.id != libraryID || !item.isFolder || item.parentID != "")) {
				return nil, scanReconciliationUnavailable("deletion ancestor has a foreign scope or role")
			}
			known[item.id], frontier[item.id] = item, item
		}
	}
	return known, nil
}

func scanReconciliationAlbums(members, known map[string]scanReconciliationItem) ([]string, error) {
	albums := make(map[string]bool)
	for _, item := range members {
		visited := make(map[string]bool)
		current := item
		albumID := ""
		for depth := 0; ; depth++ {
			if depth >= scanReconciliationMaxDepth {
				return nil, scanReconciliationBudget()
			}
			if visited[current.id] {
				return nil, scanReconciliationUnavailable("deletion hierarchy contains a cycle")
			}
			visited[current.id] = true
			if item.typeName == "Audio" && albumID == "" && current.typeName == "MusicAlbum" && current.isFolder && current.ordinary {
				if _, removed := members[current.id]; !removed {
					albumID = current.id
				}
			}
			if current.parentID == "" {
				break
			}
			parent, exists := known[current.parentID]
			if !exists {
				return nil, scanReconciliationUnavailable("deletion hierarchy is incomplete")
			}
			current = parent
		}
		if albumID != "" {
			albums[albumID] = true
		}
	}
	ids := make([]string, 0, len(albums))
	for id := range albums {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}
