package backuppg

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// A cached stamp retains the binding revision observed when its source facts
// were written. A later rebind is a valid cache miss, not archive corruption.
// Missing caches also remain valid; restore must not rewrite fingerprinted data.
func validateMediaOperationSourceState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 63 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var valid bool
	err := tx.QueryRow(ctx, mediaOperationSourceStateSQL).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	return nil
}

const mediaOperationSourceStateSQL = `SELECT NOT EXISTS(SELECT 1 FROM items item
	WHERE item.media_operation_source_revision IS NOT NULL
	AND item.media_operation_source_revision IS DISTINCT FROM
		'media-operation-source-v1-' || md5(jsonb_build_array(
			item.root_id, item.relative_path, item.file_identity, item.file_size,
			extract(epoch FROM item.modified_at), item.media,
			item.media_operation_source_binding_revision)::text))`
