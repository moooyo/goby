package backuppg

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Hashes bind retained payloads to their stored identities. Only unfinished
// publication journals require live owners; stale sources, retired subtitles,
// and detached operation history remain valid historical state.
func validateSelectedPhase2State(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 44 {
		return nil
	}
	if tx == nil {
		return classifyResourceStateError(ctx, ErrDatabase)
	}
	statement := `SELECT
		NOT EXISTS (SELECT 1 FROM media_operation_cues cue
			WHERE cue.image_sha256 IS DISTINCT FROM CASE WHEN octet_length(cue.image_png)=0 THEN ''
				ELSE encode(sha256(cue.image_png),'hex') END)
		AND NOT EXISTS (SELECT 1 FROM item_owned_subtitles subtitle
			WHERE subtitle.content_sha256 IS DISTINCT FROM encode(sha256(subtitle.content),'hex'))
		AND NOT EXISTS (SELECT 1 FROM media_operations operation
			LEFT JOIN items item ON item.id=operation.item_id
			LEFT JOIN libraries library ON library.id=operation.library_id
			LEFT JOIN library_roots root ON root.id=operation.root_id
			WHERE operation.publication_phase IN ('prepared','catalog_committed') AND (
				operation.kind<>'remove_embedded_subtitle'
				OR operation.item_id IS DISTINCT FROM operation.source_item_id
				OR operation.library_id IS DISTINCT FROM operation.source_library_id
				OR operation.root_id IS DISTINCT FROM operation.source_root_id
				OR item.id IS NULL OR library.id IS NULL OR root.id IS NULL
				OR item.library_id IS DISTINCT FROM operation.library_id
				OR item.root_id IS DISTINCT FROM operation.root_id
				OR root.library_id IS DISTINCT FROM operation.library_id
				OR EXISTS (SELECT 1 FROM scan_jobs scan WHERE scan.library_id=operation.source_library_id
					AND scan.status IN ('Queued','Running'))
				OR EXISTS (SELECT 1 FROM media_deletion_operations deletion
					WHERE deletion.library_id=operation.source_library_id)))`
	if version >= 45 {
		statement += ` AND NOT EXISTS (SELECT 1 FROM item_embedded_artwork artwork
			WHERE artwork.status='ready'
				AND artwork.source_hash IS DISTINCT FROM encode(sha256(artwork.content),'hex'))`
	}
	var valid bool
	err := tx.QueryRow(ctx, statement).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	return nil
}
