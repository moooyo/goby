package library

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// syncScannedMetadata runs after the scanner has written its newly accepted
// automatic fields, inside that same owned transaction. It reads administrator
// state only now, never from a stale snapshot taken before probing a file.
func syncScannedMetadata(ctx context.Context, tx pgx.Tx, itemID string) error {
	var automatic, sourceKey, localSource, rawOverrides, rawLocks []byte
	var itemType string
	var changed bool
	err := tx.QueryRow(ctx, `SELECT candidate.automatic, candidate.source_key,
		i.local_metadata, ms.overrides, ms.locked_values, i.type,
		ms.automatic IS DISTINCT FROM candidate.automatic OR ms.source_key IS DISTINCT FROM candidate.source_key
		FROM items i JOIN item_metadata_state ms ON ms.item_id = i.id
		CROSS JOIN LATERAL (SELECT catalog_metadata_automatic_values(i.name, i.sort_name,
			i.overview, i.type, i.index_number, i.parent_index_number, i.local_metadata) AS automatic,
			catalog_metadata_source_key(i) AS source_key) candidate
		WHERE i.id = $1 FOR UPDATE OF ms`, itemID).
		Scan(&automatic, &sourceKey, &localSource, &rawOverrides, &rawLocks, &itemType, &changed)
	if err != nil {
		return fmt.Errorf("read scanned metadata state: %w", err)
	}
	overrides, err := metadataSourceObject(rawOverrides)
	if err != nil {
		return err
	}
	locks, err := metadataSourceObject(rawLocks)
	if err != nil {
		return err
	}
	activeOverrides := activeMetadataControls(itemType, overrides)
	activeLocks := activeMetadataControls(itemType, locks)
	effective, _, err := composeMetadataValues(automatic, activeOverrides, activeLocks)
	if err != nil {
		return err
	}
	projection, err := buildMetadataProjection(localSource, activeOverrides, activeLocks, effective)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE item_metadata_state SET automatic = $2, source_key = $3,
		effective = $4, revision = revision + CASE WHEN $5 THEN 1 ELSE 0 END,
		updated_at = CASE WHEN $5 THEN clock_timestamp() ELSE updated_at END WHERE item_id = $1`,
		itemID, automatic, sourceKey, projection, changed); err != nil {
		return fmt.Errorf("update scanned metadata state: %w", err)
	}
	return applyEffectiveMetadata(ctx, tx, itemID, effective, projection)
}
