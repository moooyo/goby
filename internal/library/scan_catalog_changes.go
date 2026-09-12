package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type scanCatalogSnapshot struct {
	present    bool
	change     CatalogChange
	properties string
}

// Compare accepted item properties, not scan timestamps or metadata source
// bookkeeping. Source identity/media remain relevant for file replacement;
// effective metadata and ordered entities retain administrator overlays.
var scanCatalogSnapshotColumns = `i.id, i.library_id, COALESCE(i.parent_id, ''), i.is_folder,
	i.type = 'CollectionFolder', jsonb_build_object(
		'RootId', i.root_id, 'ParentId', i.parent_id, 'Type', i.type, 'IsFolder', i.is_folder,
		'Path', i.path, 'RelativePath', i.relative_path, 'Name', i.name, 'SortName', i.sort_name,
		'Overview', i.overview, 'IndexNumber', i.index_number, 'ParentIndexNumber', i.parent_index_number,
		'Media', i.media, 'FileIdentity', i.file_identity, 'FileSize', i.file_size, 'ModifiedAt', i.modified_at,
		'Metadata', COALESCE(ms.effective, i.local_metadata), 'Entities', ` + itemEntitiesColumn + `)::text`

func readScanCatalogItem(ctx context.Context, tx pgx.Tx, id string) (scanCatalogSnapshot, error) {
	return scanCatalogSnapshotRow(tx.QueryRow(ctx, "SELECT "+scanCatalogSnapshotColumns+`
		FROM items i LEFT JOIN item_metadata_state ms ON ms.item_id = i.id
		WHERE i.id = $1 FOR UPDATE OF i`, id))
}

func readScanCatalogFolder(ctx context.Context, tx pgx.Tx, rootID, relative string) (scanCatalogSnapshot, error) {
	return scanCatalogSnapshotRow(tx.QueryRow(ctx, "SELECT "+scanCatalogSnapshotColumns+`
		FROM items i LEFT JOIN item_metadata_state ms ON ms.item_id = i.id
		WHERE i.root_id = $1 AND i.relative_path = $2 FOR UPDATE OF i`, rootID, relative))
}

func scanCatalogSnapshotRow(row rowScanner) (scanCatalogSnapshot, error) {
	var snapshot scanCatalogSnapshot
	err := row.Scan(&snapshot.change.ItemID, &snapshot.change.LibraryID, &snapshot.change.ParentID,
		&snapshot.change.IsFolder, &snapshot.change.IsCollectionFolder, &snapshot.properties)
	if errors.Is(err, pgx.ErrNoRows) {
		return scanCatalogSnapshot{}, nil
	}
	if err != nil {
		return scanCatalogSnapshot{}, fmt.Errorf("read scanned catalog notification properties: %w", err)
	}
	snapshot.present = true
	return snapshot, nil
}

func recordScanCatalogChange(tx pgx.Tx, libraryID string, before, after scanCatalogSnapshot, explicitRefresh bool) error {
	if !after.present || after.change.LibraryID != libraryID || before.present &&
		(before.change.LibraryID != libraryID || before.change.ItemID != after.change.ItemID) {
		return fmt.Errorf("%w: scanned notification identities changed inside the transaction", ErrUnavailable)
	}
	change := after.change
	change.Kind = CatalogAdded
	if before.present {
		if !explicitRefresh && before.properties == after.properties {
			return nil
		}
		change.Kind = CatalogUpdated
		if before.change.ParentID != "" && change.ParentID != "" && before.change.ParentID != change.ParentID {
			change.PreviousParentID = before.change.ParentID
		}
	}
	return recordCatalogChanges(tx, change)
}
