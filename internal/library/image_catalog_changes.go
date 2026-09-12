package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type imageCatalogSnapshot struct {
	owner      CatalogChange
	properties string
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
