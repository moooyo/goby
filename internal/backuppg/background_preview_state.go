package backuppg

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Background clips live beside the source, outside the database archive. Raw
// recovery validates queue ownership without opening, recreating, or removing
// those files. A retained request is not execution authority: the worker must
// recheck its current source, task fence, and administrator session after restore.
func validateBackgroundPreviewState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 57 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var valid bool
	err := tx.QueryRow(ctx, backgroundPreviewStateRelationsSQL).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	return analysisStateRows(ctx, tx, `SELECT actor_user_id,actor_session_id,source_revision,error_code FROM background_preview_queue
		UNION ALL SELECT request_id,'','','' FROM background_preview_requests`, func(rows pgx.Rows) error {
		var user, session, source, code string
		if err := rows.Scan(&user, &session, &source, &code); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if !analysisStateIdentifier(user, 128, true) || !analysisStateIdentifier(session, 128, true) ||
			!analysisStateIdentifier(source, 256, true) || !analysisStateIdentifier(code, 128, true) {
			return ErrSchema
		}
		return nil
	})
}

// Current probe facts and manual start bounds may become stale after a source
// replacement. Likewise, a cancelled or interrupted queue may retain an older
// child, operation, or claim. Do not normalize these durable facts. An active
// claim must still identify its own library child; terminal history need not
// keep a live task, user, session, or source file.
const backgroundPreviewStateRelationsSQL = `SELECT
	(SELECT count(*) FROM background_preview_settings)=1
	AND NOT EXISTS(SELECT 1 FROM (
		SELECT item_id FROM item_background_preview_settings UNION SELECT item_id FROM background_preview_queue
	) owned LEFT JOIN items item ON item.id=owned.item_id
		LEFT JOIN libraries library ON library.id=item.library_id
		LEFT JOIN library_roots root ON root.id=item.root_id
		WHERE item.id IS NULL OR item.type NOT IN ('Movie','Episode') OR item.is_folder
		OR root.id IS NULL OR root.library_id IS DISTINCT FROM item.library_id
		OR library.collection_type NOT IN ('movies','tvshows','mixed'))
	AND NOT EXISTS(SELECT 1 FROM background_preview_queue queue
		LEFT JOIN items item ON item.id=queue.item_id
		LEFT JOIN task_runs run ON run.id=queue.run_id
		LEFT JOIN task_run_children child ON child.id=queue.child_id
		LEFT JOIN sessions session ON session.id=queue.actor_session_id
		WHERE (queue.manual AND (queue.actor_user_id='' OR queue.actor_session_id=''))
		OR (NOT queue.manual AND (queue.actor_user_id<>'' OR queue.actor_session_id<>'' OR queue.force))
		OR (session.id IS NOT NULL AND (session.kind<>'admin' OR session.user_id IS DISTINCT FROM queue.actor_user_id))
		OR (queue.run_id='') IS DISTINCT FROM (queue.child_id='')
		OR (queue.state IN ('pending','running') AND queue.requested_revision<=queue.completed_revision)
		OR (queue.state IN ('ready','failed','cancelled') AND queue.requested_revision<>queue.completed_revision)
		OR (queue.state='running' AND (queue.claimed_revision<=queue.completed_revision OR queue.source_revision=''
			OR run.id IS NULL OR run.task_key<>'media.background_preview_generation' OR child.id IS NULL
			OR child.run_id<>run.id OR child.library_id<>item.library_id OR child.analysis_scope_key<>''))
		OR (queue.state='ready' AND (queue.claimed_revision=0 OR queue.error_code<>'')))`
