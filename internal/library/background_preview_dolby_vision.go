package library

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/systemevents"
)

// Match the encoder's first internal, non-attached video stream. A proof or
// wait for a different stream must not hold this request indefinitely.
const backgroundPreviewVideoIndexSQL = `(SELECT stream.value->>'Index' FROM jsonb_array_elements(
	CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN i.media->'Streams' ELSE '[]'::jsonb END)
	WITH ORDINALITY stream(value,position) WHERE stream.value->>'CodecType'='video'
	AND stream.value->'IsExternal' IS DISTINCT FROM 'true'::jsonb
	AND stream.value->'IsAttachedPicture' IS DISTINCT FROM 'true'::jsonb ORDER BY stream.position LIMIT 1)`

func (s *Store) backgroundPreviewDolbyVisionCondition() string {
	if !s.DolbyVisionVerificationConfigured() {
		return "true"
	}
	// Unsuccessful scans leave their current rows pending until a successful
	// rescan reauthorizes verification. Waiting here preserves the preview
	// request instead of turning an interrupted scan into a terminal failure.
	return `NOT (q.state='pending' AND q.error_code='dolby_vision_pending' AND q.claimed_revision=q.requested_revision
		AND q.source_revision=` + introSourceRevisionSQL + ` AND EXISTS(SELECT 1 FROM dolby_vision_verifications dv
			WHERE dv.item_id=i.id AND dv.evidence IS NULL AND dv.verifier_version=` + strconv.Itoa(media.DolbyVisionVerificationVersion) + `
			AND dv.stream_index::text=` + backgroundPreviewVideoIndexSQL + `
			AND dv.source_revision=` + MediaOperationSourceRevisionSQL + `))`
}

// DeferBackgroundPreview releases just the live claim and keeps the original
// request, force flag, and already published artifact intact. A newer request
// may try again immediately, including checking for an already saved artifact.
func (s *Store) DeferBackgroundPreview(ctx context.Context, fence AnalysisFence, job BackgroundPreviewJob) error {
	return withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error {
		tag, err := tx.Exec(`UPDATE background_preview_queue SET state='pending',run_id='',child_id='',started_at=NULL,finished_at=NULL,
			error_code=CASE WHEN requested_revision=claimed_revision THEN 'dolby_vision_pending' ELSE '' END
			WHERE item_id=$1 AND claimed_revision=$2 AND run_id=$3 AND child_id=$4 AND state='running'`,
			job.ItemID, job.Revision, job.RunID, job.ChildID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrBackgroundPreviewConflict
		}
		return nil
	})
}

// A terminal result wakes waiting previews for the selected stream. A positive
// result can also prompt the existing automatic discovery of missing previews.
func signalDolbyVisionPreview(ctx context.Context, tx pgx.Tx, itemID string, streamIndex int, verified bool) error {
	var pending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM items i JOIN libraries l ON l.id=i.library_id
		LEFT JOIN background_preview_queue q ON q.item_id=i.id
		WHERE i.id=$1 AND $2::integer::text=`+backgroundPreviewVideoIndexSQL+`
		AND ((q.requested_revision>q.completed_revision AND (q.manual OR l.options->'EnableBackgroundPreviewGeneration'='true'::jsonb)
			AND ($3 OR (q.state='pending' AND q.error_code='dolby_vision_pending' AND q.claimed_revision=q.requested_revision
			AND q.source_revision=`+introSourceRevisionSQL+`)))
		OR ($3 AND q.item_id IS NULL AND l.options->'EnableBackgroundPreviewGeneration'='true'::jsonb)))`, itemID, streamIndex, verified).Scan(&pending); err != nil {
		return err
	}
	if !pending {
		return nil
	}
	return systemevents.Record(taskSourcePublicationTx{ctx: ctx, tx: tx}.Exec, systemevents.BackgroundPreviewGenerationRequested)
}
