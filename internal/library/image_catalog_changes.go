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

// Compare every replaceable raw row on the owned transaction's first item
// read, including obsolete roots and fields outside the public projection.
// The conditional projection avoids building notification JSON for an exact
// no-op without adding a query to changed or newly populated image sets.
func readImageCatalogReplacement(ctx context.Context, tx pgx.Tx, itemID, libraryID, rootID string,
	replaceTypes []string, replacement imageScanReplacement) (imageCatalogSnapshot, bool, error) {
	snapshot := imageCatalogSnapshot{owner: CatalogChange{Kind: CatalogUpdated}}
	var changed bool
	err := tx.QueryRow(ctx, `WITH replacement AS MATERIALIZED (
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
	)
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
