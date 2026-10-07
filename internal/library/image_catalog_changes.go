package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type imageCatalogSnapshot struct {
	owner      CatalogChange
	properties string
}

type imageScanReplacement struct {
	types      []string
	indexes    []int
	paths      []string
	identities []string
	hashes     []string
	sizes      []int64
	modified   []time.Time
	widths     []int
	heights    []int
	mimes      []string
}

// Image listings expose names, paths, dimensions, size, type, and index. Item
// projections also expose content tags and the primary aspect ratio. Compare
// those accepted values without treating inode or inspection time as a change.
// The root and visibility predicates match the public stored-image projection.
var imageCatalogSnapshotProjection = `COALESCE((SELECT jsonb_agg(jsonb_build_object(
	'ImageType', im.image_type, 'ImageIndex', im.image_index,
	'RootPath', r.path, 'RelativePath', im.relative_path, 'Tag', im.source_hash,
	'Size', im.file_size, 'Width', im.width, 'Height', im.height)` + storedImageOrder + `)
	FROM item_images im WHERE im.item_id = i.id AND im.root_id = r.id AND ` + directItemSQL("i") + `), '[]'::jsonb)::text`

// Both the read-only observation and the owned writer compare the complete
// replaceable rowset, including obsolete roots and private source metadata.
const imageCatalogReplacementComparisonSQL = `WITH replacement AS MATERIALIZED (
		SELECT * FROM unnest($5::text[], $6::integer[], $7::text[], $8::text[], $9::text[],
			$10::bigint[], $11::timestamptz[], $12::integer[], $13::integer[], $14::text[])
		AS candidate(image_type, image_index, relative_path, file_identity, source_hash,
			file_size, modified_at, width, height, mime_type)
	), comparison AS MATERIALIZED (
		SELECT EXISTS (
			SELECT 1 FROM (
				SELECT root_id, image_type, image_index, relative_path, file_identity, source_hash,
					file_size, modified_at, width, height, mime_type
				FROM item_images WHERE item_id=$1 AND image_type=ANY($4::text[])
			) stored FULL JOIN replacement
				ON stored.image_type=replacement.image_type AND stored.image_index=replacement.image_index
			WHERE stored.image_type IS NULL OR replacement.image_type IS NULL OR
				(stored.root_id, stored.relative_path, stored.file_identity, stored.source_hash,
				stored.file_size, stored.modified_at, stored.width, stored.height, stored.mime_type)
				IS DISTINCT FROM ($3::text, replacement.relative_path, replacement.file_identity,
				replacement.source_hash, replacement.file_size, replacement.modified_at,
				replacement.width, replacement.height, replacement.mime_type)
		) AS changed
	)`

// imageCatalogReplacementUnchanged observes one fresh owner-session snapshot.
// It releases the owner mutex before the caller's final filesystem proof. A
// later catalog writer is later work; this observation never authorizes writes.
func (state *scanState) imageCatalogReplacementUnchanged(itemID string, replaceTypes []string,
	replacement imageScanReplacement, expected rootBindingRow) (bool, error) {
	ctx := state.task.ctx
	if !state.store.Available() {
		return false, ErrUnavailable
	}
	grant := state.task.authority.Load()
	if grant == nil {
		return false, ErrTaskScanInactive
	}
	if err := state.store.checkScanOperationActive(ctx, state.task, grant); err != nil {
		return false, err
	}
	granted, ok := grant.roots[expected.root.id]
	if !ok || !granted.same(expected) || expected.root != state.root || grant.libraryID != state.library.ID {
		return false, ErrRootBindingConflict
	}
	if err := state.store.lockOwnedSession(ctx); err != nil {
		return false, err
	}
	defer state.store.ownership.mu.Unlock()
	if !state.store.Available() {
		return false, ErrUnavailable
	}
	// As with owned writes, caller cancellation must not close the lock session
	// while its statement is in flight. Consume the result before checking it.
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	args := []any{state.store.scanOwnedReadMode, itemID, state.library.ID, state.root.id, replaceTypes,
		replacement.types, replacement.indexes, replacement.paths, replacement.identities, replacement.hashes,
		replacement.sizes, replacement.modified, replacement.widths, replacement.heights, replacement.mimes,
		expected.root.path, expected.root.allowedPath, expected.root.relativePath}
	if state.store.scanOwnedReadMode == 0 {
		args = args[1:]
	}
	var mappingMatches, changed bool
	err := state.store.ownership.conn.QueryRow(readCtx, `/* image_catalog_unchanged */ `+imageCatalogReplacementComparisonSQL+`
	SELECT (r.path, r.allowed_path, r.relative_path)
		IS NOT DISTINCT FROM ($15::text, $16::text, $17::text), comparison.changed
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		CROSS JOIN comparison
		WHERE i.id = $1 AND i.library_id = $2 AND i.root_id = $3`, args...).Scan(&mappingMatches, &changed)
	err = state.store.ownershipErrorLocked(err)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("read unchanged image catalog: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !mappingMatches {
		return false, ErrRootBindingConflict
	}
	return !changed, nil
}

// Compare every replaceable raw row again under the owned writer's item lock.
// The conditional projection avoids notification JSON for an exact match,
// including when a concurrent writer repaired a preceding read-only mismatch.
func readImageCatalogReplacement(ctx context.Context, tx pgx.Tx, itemID, libraryID, rootID string,
	replaceTypes []string, replacement imageScanReplacement) (imageCatalogSnapshot, bool, error) {
	snapshot := imageCatalogSnapshot{owner: CatalogChange{Kind: CatalogUpdated}}
	var changed bool
	err := tx.QueryRow(ctx, imageCatalogReplacementComparisonSQL+`
	SELECT i.id, i.library_id, COALESCE(i.parent_id, ''), i.is_folder,
		i.type = 'CollectionFolder', comparison.changed,
		CASE WHEN comparison.changed THEN `+imageCatalogSnapshotProjection+` ELSE '' END
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		CROSS JOIN comparison
		WHERE i.id = $1 AND i.library_id = $2 AND i.root_id = $3 FOR UPDATE OF i`,
		itemID, libraryID, rootID, replaceTypes, replacement.types, replacement.indexes, replacement.paths,
		replacement.identities, replacement.hashes, replacement.sizes, replacement.modified,
		replacement.widths, replacement.heights, replacement.mimes).
		Scan(&snapshot.owner.ItemID, &snapshot.owner.LibraryID, &snapshot.owner.ParentID,
			&snapshot.owner.IsFolder, &snapshot.owner.IsCollectionFolder, &changed, &snapshot.properties)
	if errors.Is(err, pgx.ErrNoRows) {
		return imageCatalogSnapshot{}, false, ErrNotFound
	}
	if err != nil {
		return imageCatalogSnapshot{}, false, fmt.Errorf("read image replacement projection: %w", err)
	}
	return snapshot, changed, nil
}

func readImageCatalogSnapshot(ctx context.Context, tx pgx.Tx, itemID, libraryID, rootID string) (imageCatalogSnapshot, error) {
	snapshot := imageCatalogSnapshot{owner: CatalogChange{Kind: CatalogUpdated}}
	err := tx.QueryRow(ctx, `SELECT i.id, i.library_id, COALESCE(i.parent_id, ''), i.is_folder,
		i.type = 'CollectionFolder', `+imageCatalogSnapshotProjection+`
		FROM items i JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE i.id = $1 AND i.library_id = $2 AND i.root_id = $3 FOR UPDATE OF i`,
		itemID, libraryID, rootID).Scan(&snapshot.owner.ItemID, &snapshot.owner.LibraryID,
		&snapshot.owner.ParentID, &snapshot.owner.IsFolder, &snapshot.owner.IsCollectionFolder, &snapshot.properties)
	if errors.Is(err, pgx.ErrNoRows) {
		return imageCatalogSnapshot{}, ErrNotFound
	}
	if err != nil {
		return imageCatalogSnapshot{}, fmt.Errorf("read image notification projection: %w", err)
	}
	return snapshot, nil
}
