package library

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// publishLocalNFOProjection only publishes accepted bytes into the catalog.
// The caller owns authorization, numbering, storage proof and any queue work.
func publishLocalNFOProjection(ctx context.Context, tx pgx.Tx, libraryID, itemID string,
	local localMetadata, base *scannedMetadataBase, parentNumber *int) error {
	localJSON, err := encodeLocalMetadata(local)
	if err != nil {
		return err
	}
	before, err := readScanCatalogItem(ctx, tx, itemID)
	if err != nil {
		return err
	}
	if !before.present || before.change.LibraryID != libraryID {
		return ErrSourceChanged
	}
	auxiliary, err := readAuxiliaryChildCatalogSnapshot(ctx, tx, itemID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE items SET local_metadata=$2,local_metadata_hash=$3,local_metadata_path=$4,
		parent_index_number=COALESCE($5::integer,parent_index_number) WHERE id=$1`, itemID, localJSON, local.hash, local.path, parentNumber); err != nil {
		return err
	}
	if err := syncScannedMetadata(ctx, tx, itemID, scannedMetadataOptions{Base: base}); err != nil {
		return err
	}
	after, err := readScanCatalogItem(ctx, tx, itemID)
	if err != nil {
		return err
	}
	if err := recordScanCatalogChange(tx, libraryID, before, after, false); err != nil {
		return err
	}
	return auxiliary.record(ctx, tx, nil)
}
