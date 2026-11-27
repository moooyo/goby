package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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

func newImageScanReplacement(rows int) imageScanReplacement {
	return imageScanReplacement{
		types: make([]string, rows), indexes: make([]int, rows), paths: make([]string, rows),
		identities: make([]string, rows), hashes: make([]string, rows), sizes: make([]int64, rows),
		modified: make([]time.Time, rows), widths: make([]int, rows), heights: make([]int, rows), mimes: make([]string, rows),
	}
}

func (r imageScanReplacement) set(position int, imageType string, index int, directory string, source *scannedImage) {
	image := source.image
	r.types[position], r.indexes[position] = imageType, index
	r.paths[position] = filepath.ToSlash(filepath.Join(directory, source.filename))
	r.identities[position], r.hashes[position] = source.identity, image.Tag
	r.sizes[position], r.modified[position] = image.Size, image.ModifiedAt
	r.widths[position], r.heights[position], r.mimes[position] = image.Width, image.Height, image.MIMEType
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

// publishImageCatalogReplacement changes only the requested local types. The
// comparison, publication and notification all use the caller's owned writer.
func publishImageCatalogReplacement(ctx context.Context, tx pgx.Tx, itemID, libraryID, rootID string,
	replaceTypes []string, replaceCounts []int, replacement imageScanReplacement) (bool, error) {
	before, changed, err := readImageCatalogReplacement(ctx, tx, itemID, libraryID, rootID, replaceTypes, replacement)
	if err != nil || !changed {
		return false, err
	}
	// Candidate indexes are contiguous. Keep existing keys for conditional
	// updates, including rows from a previous root.
	deleted, err := tx.Exec(ctx, `DELETE FROM item_images im
		USING unnest($2::text[], $3::integer[]) AS replacement(image_type, image_count)
		WHERE im.item_id = $1 AND im.image_type = replacement.image_type
		AND im.image_index >= replacement.image_count`, itemID, replaceTypes, replaceCounts)
	if err != nil {
		return false, err
	}
	changedRows := deleted.RowsAffected()
	if len(replacement.types) != 0 {
		written, err := tx.Exec(ctx, `INSERT INTO item_images
			(item_id, root_id, image_type, image_index, relative_path, file_identity, source_hash,
			 file_size, modified_at, width, height, mime_type)
			SELECT $1,$2,replacement.image_type,replacement.image_index,replacement.relative_path,
			 replacement.file_identity,replacement.source_hash,replacement.file_size,replacement.modified_at,
			 replacement.width,replacement.height,replacement.mime_type
			FROM unnest($3::text[],$4::integer[],$5::text[],$6::text[],$7::text[],$8::bigint[],
			 $9::timestamptz[],$10::integer[],$11::integer[],$12::text[])
			AS replacement(image_type,image_index,relative_path,file_identity,source_hash,file_size,
			 modified_at,width,height,mime_type)
			WHERE true
			ON CONFLICT (item_id, image_type, image_index) DO UPDATE SET
			 root_id = EXCLUDED.root_id, relative_path = EXCLUDED.relative_path,
			 file_identity = EXCLUDED.file_identity, source_hash = EXCLUDED.source_hash,
			 file_size = EXCLUDED.file_size, modified_at = EXCLUDED.modified_at,
			 width = EXCLUDED.width, height = EXCLUDED.height, mime_type = EXCLUDED.mime_type
			WHERE (item_images.root_id, item_images.relative_path, item_images.file_identity,
			 item_images.source_hash, item_images.file_size, item_images.modified_at,
			 item_images.width, item_images.height, item_images.mime_type)
			IS DISTINCT FROM (EXCLUDED.root_id, EXCLUDED.relative_path, EXCLUDED.file_identity,
			 EXCLUDED.source_hash, EXCLUDED.file_size, EXCLUDED.modified_at,
			 EXCLUDED.width, EXCLUDED.height, EXCLUDED.mime_type)`,
			itemID, rootID, replacement.types, replacement.indexes, replacement.paths, replacement.identities,
			replacement.hashes, replacement.sizes, replacement.modified, replacement.widths, replacement.heights, replacement.mimes)
		if err != nil {
			return false, err
		}
		changedRows += written.RowsAffected()
	}
	if changedRows == 0 {
		return false, nil
	}
	after, err := readImageCatalogSnapshot(ctx, tx, itemID, libraryID, rootID)
	if err != nil {
		return false, err
	}
	if before.properties != after.properties {
		if err := recordCatalogChanges(tx, after.owner); err != nil {
			return false, err
		}
	}
	return true, nil
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
