package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/systemevents"
)

// QueueBackgroundPreviews persists selection before task admission. A committed
// event recovers an admission failure, and receipts prevent duplicate Force work.
func (s *Store) QueueBackgroundPreviews(ctx context.Context, actor identity.Principal, selection AnalysisSelection, requestID string) (BackgroundPreviewQueueResult, error) {
	selection, err := NormalizeAnalysisSelection(selection)
	if err != nil {
		return BackgroundPreviewQueueResult{}, err
	}
	if requestID != "" && !analysisOpaque(requestID, 128) {
		return BackgroundPreviewQueueResult{}, ErrInvalidInput
	}
	raw, _ := json.Marshal(struct {
		Actor     string
		Selection AnalysisSelection
	}{actor.User.ID, selection})
	hash := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(hash[:])
	var result BackgroundPreviewQueueResult
	err = s.withBackgroundAdministrator(ctx, actor, func(tx OwnedTx) error {
		// WithOwnedTx holds the catalog's unique owner-session mutex for this whole
		// transaction, serializing receipt admission across administrators. The
		// shared settings lock fixes lock ordering; it is not the receipt mutex.
		var singleton int
		if err := tx.QueryRow(`SELECT id FROM background_preview_settings WHERE id=1 FOR SHARE`).Scan(&singleton); err != nil {
			return err
		}
		if requestID != "" {
			var previous string
			err := tx.QueryRow(`SELECT fingerprint,queued FROM background_preview_requests WHERE request_id=$1`, requestID).Scan(&previous, &result.Queued)
			if err == nil {
				if previous != fingerprint {
					return ErrBackgroundPreviewConflict
				}
				result.Replayed = true
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		if len(selection.LibraryIDs) > 0 {
			var count int
			if err := tx.QueryRow(`SELECT count(*) FROM libraries WHERE id=ANY($1::text[]) AND collection_type IN ('movies','tvshows','mixed') AND id<>$2`, selection.LibraryIDs, CollectionsLibraryID).Scan(&count); err != nil {
				return err
			}
			if count != len(selection.LibraryIDs) {
				return ErrNotFound
			}
		}
		if len(selection.ItemIDs) > 0 {
			var count int
			if err := tx.QueryRow(`SELECT count(*) FROM items i JOIN libraries l ON l.id=i.library_id WHERE i.id=ANY($1::text[])
    AND (cardinality($2::text[])=0 OR i.library_id=ANY($2::text[])) AND i.type IN ('Movie','Episode') AND `+analysisPhysicalSQL+`
    AND l.collection_type IN ('movies','tvshows','mixed')`, selection.ItemIDs, selection.LibraryIDs).Scan(&count); err != nil {
				return err
			}
			if count != len(selection.ItemIDs) {
				return ErrNotFound
			}
		}
		tag, err := tx.Exec(`INSERT INTO background_preview_queue(item_id,force,manual,actor_user_id,actor_session_id)
   SELECT i.id,$3,true,$4,$5 FROM items i JOIN libraries l ON l.id=i.library_id
   WHERE `+analysisPhysicalSQL+` AND i.type IN ('Movie','Episode') AND l.collection_type IN ('movies','tvshows','mixed')
    AND (cardinality($1::text[])=0 OR i.library_id=ANY($1::text[]))
    AND (cardinality($2::text[])=0 OR i.id=ANY($2::text[])) ORDER BY i.id
   ON CONFLICT(item_id) DO UPDATE SET
    operation_id=CASE WHEN $3 OR background_preview_queue.requested_revision=background_preview_queue.completed_revision
      OR background_preview_queue.actor_user_id<>$4 OR background_preview_queue.actor_session_id<>$5
      OR EXISTS(SELECT 1 FROM task_runs r WHERE r.task_key=$6 AND r.state='stopping')
      THEN EXCLUDED.operation_id ELSE background_preview_queue.operation_id END,
    requested_revision=CASE WHEN $3 OR background_preview_queue.requested_revision=background_preview_queue.completed_revision
      OR background_preview_queue.actor_user_id<>$4 OR background_preview_queue.actor_session_id<>$5
      OR EXISTS(SELECT 1 FROM task_runs r WHERE r.task_key=$6 AND r.state='stopping')
      THEN background_preview_queue.requested_revision+1 ELSE background_preview_queue.requested_revision END,
    force=CASE WHEN background_preview_queue.requested_revision=background_preview_queue.completed_revision
      OR background_preview_queue.actor_user_id<>$4 OR background_preview_queue.actor_session_id<>$5
      OR EXISTS(SELECT 1 FROM task_runs r WHERE r.task_key=$6 AND r.state='stopping') THEN $3 ELSE background_preview_queue.force OR $3 END,
    manual=true,actor_user_id=$4,actor_session_id=$5,
    state=CASE WHEN background_preview_queue.state='running' THEN 'running' ELSE 'pending' END,
    requested_at=clock_timestamp(),error_code=''`, selection.LibraryIDs, selection.ItemIDs, selection.Force, actor.User.ID, actor.SessionID, TaskBackgroundPreviewGenerationKey)
		if err != nil {
			return err
		}
		result.Queued = tag.RowsAffected()
		if requestID != "" {
			if _, err := tx.Exec(`INSERT INTO background_preview_requests(request_id,fingerprint,queued) VALUES($1,$2,$3)`, requestID, fingerprint, result.Queued); err != nil {
				return err
			}
		}
		if result.Queued > 0 {
			return systemevents.Record(tx.Exec, systemevents.BackgroundPreviewGenerationRequested)
		}
		return nil
	})
	return result, err
}

func withBackgroundFence(ctx context.Context, s *Store, fence AnalysisFence, callback func(OwnedTx) error) error {
	if fence == nil || callback == nil {
		return ErrInvalidInput
	}
	if err := analysisContext(ctx); err != nil {
		return err
	}
	return s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		if err := callback(tx); err != nil {
			return err
		}
		if err := fence(tx); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
}

// PrepareAutomaticBackgroundPreviews only inserts absent records. Neither
// scans nor configuration/source changes reset a completed or failed artifact.
func (s *Store) PrepareAutomaticBackgroundPreviews(ctx context.Context, fence AnalysisFence, libraryID string) error {
	return s.prepareAutomaticSidecars(ctx, fence, libraryID, TaskBackgroundPreviewGenerationKey, `INSERT INTO background_preview_queue(item_id)
   SELECT i.id FROM items i JOIN libraries l ON l.id=i.library_id WHERE i.library_id=$1
    AND l.options->'EnableBackgroundPreviewGeneration'='true'::jsonb
    AND l.collection_type IN ('movies','tvshows','mixed') AND i.type IN ('Movie','Episode') AND `+analysisPhysicalSQL+`
   ORDER BY i.id ON CONFLICT(item_id) DO NOTHING`)
}

// A bounded child yields the shared media execution slot between batches. The
// dedicated event remains unconsumed while this run is active and schedules a
// fresh child snapshot after completion, without creating a private goroutine.
func (s *Store) RequestBackgroundPreviewContinuation(ctx context.Context, fence AnalysisFence, libraryID string) error {
	return withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error {
		var pending bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM background_preview_queue q JOIN items i ON i.id=q.item_id
   JOIN libraries l ON l.id=i.library_id WHERE i.library_id=$1 AND q.requested_revision>q.completed_revision
    AND (q.manual OR l.options->'EnableBackgroundPreviewGeneration'='true'::jsonb))`, libraryID).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return systemevents.Record(tx.Exec, systemevents.BackgroundPreviewGenerationRequested)
		}
		return nil
	})
}

// Administrators may remove automatic schedules. A manually admitted child
// must then drain its accepted queue instead of yielding to an absent trigger.
func (s *Store) BackgroundPreviewBatchCanYield(ctx context.Context, fence AnalysisFence) (bool, error) {
	var allowed bool
	err := withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error {
		return tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_definitions d JOIN task_triggers t ON t.task_id=d.id
   WHERE d.key=$1 AND d.enabled AND t.retired_at IS NULL AND t.calculation_error=''
    AND t.kind='system_event' AND t.system_event=$2)`, TaskBackgroundPreviewGenerationKey, string(systemevents.BackgroundPreviewGenerationRequested)).Scan(&allowed)
	})
	return allowed, err
}

func (s *Store) ClaimBackgroundPreview(ctx context.Context, fence AnalysisFence, libraryID, runID, childID string) (*BackgroundPreviewJob, error) {
	if !metadataIdentifier(libraryID) || !analysisOpaque(runID, 128) || !analysisOpaque(childID, 128) {
		return nil, ErrInvalidInput
	}
	var job *BackgroundPreviewJob
	err := withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error {
		value := BackgroundPreviewJob{LibraryID: libraryID, RunID: runID, ChildID: childID}
		err := tx.QueryRow(`SELECT q.item_id,q.requested_revision,q.operation_id,q.force,i.name,settings.manual_start_ticks,q.actor_user_id,q.actor_session_id
   FROM background_preview_queue q JOIN items i ON i.id=q.item_id JOIN libraries l ON l.id=i.library_id
   LEFT JOIN item_background_preview_settings settings ON settings.item_id=i.id
   WHERE i.library_id=$1 AND q.requested_revision>q.completed_revision
    AND (q.manual OR l.options->'EnableBackgroundPreviewGeneration'='true'::jsonb)
    AND (q.state<>'running' OR NOT EXISTS(SELECT 1 FROM task_run_children c JOIN task_runs r ON r.id=c.run_id
      WHERE c.id=q.child_id AND c.state='running' AND r.state IN ('pending','running','stopping')))
   ORDER BY q.requested_at,q.item_id LIMIT 1 FOR UPDATE OF q`, libraryID).Scan(&value.ItemID, &value.Revision, &value.OperationID, &value.Force, &value.Name, &value.ManualStartTicks, &value.ActorUserID, &value.ActorSessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := checkBackgroundPreviewActor(ctx, tx, value, true); err != nil {
			if !errors.Is(err, ErrForbidden) {
				return err
			}
			if _, err := tx.Exec(`UPDATE background_preview_queue SET completed_revision=requested_revision,state='cancelled',error_code='request_authority_revoked',
    run_id=$2,child_id=$3,finished_at=clock_timestamp() WHERE item_id=$1`, value.ItemID, runID, childID); err != nil {
				return err
			}
			job = &value
			return nil
		}
		source, err := readAnalysisSource(tx, value.ItemID, true)
		if err != nil {
			// Invalid current probe facts become visible terminal item failures rather
			// than preventing every later item in this library from running.
			if _, updateErr := tx.Exec(`UPDATE background_preview_queue SET completed_revision=requested_revision,state='failed',error_code='source_unavailable',
    run_id=$2,child_id=$3,finished_at=clock_timestamp() WHERE item_id=$1`, value.ItemID, runID, childID); updateErr != nil {
				return updateErr
			}
			value.SourceRevision = ""
			job = &value
			return nil
		}
		value.SourceRevision, value.MediaSourceID = source.SourceRevision, source.MediaSourceID
		config, err := readBackgroundPreviewConfiguration(tx.QueryRow(`SELECT ` + backgroundConfigurationColumns + ` FROM background_preview_settings WHERE id=1`))
		if err != nil {
			return err
		}
		value.Profile = config.Profile
		var encodedMedia, encodedIntro, encodedCredits []byte
		if err := tx.QueryRow(`SELECT i.media,`+itemIntroColumn+`,`+itemCreditsColumn+` FROM items i WHERE i.id=$1`, value.ItemID).Scan(&encodedMedia, &encodedIntro, &encodedCredits); err != nil {
			return err
		}
		var info media.Info
		if err := json.Unmarshal(encodedMedia, &info); err != nil {
			return err
		}
		item := Item{ID: value.ItemID, Type: source.ItemType, Media: &info}
		if err := projectItemIntro(&item, encodedIntro); err != nil {
			return err
		}
		if err := projectItemCredits(&item, encodedCredits); err != nil {
			return err
		}
		value.StartTicks, value.DurationTicks, err = selectBackgroundPreviewInterval(source.DurationTicks, value.ManualStartTicks, item.Intro, item.Credits, int64(config.Profile.DurationSeconds)*backgroundTicksPerSecond)
		if err != nil {
			// A saved artifact must remain reusable even if later marker/configuration
			// changes make a new interval invalid. The encoder rejects this empty
			// interval only after the sidecar reuse check.
			value.StartTicks, value.DurationTicks = 0, 0
		}
		if _, err := tx.Exec(`UPDATE background_preview_queue SET state='running',claimed_revision=$2,run_id=$3,child_id=$4,
   source_revision=$5,reused=false,error_code='',started_at=clock_timestamp(),finished_at=NULL WHERE item_id=$1`, value.ItemID, value.Revision, runID, childID, value.SourceRevision); err != nil {
			return err
		}
		job = &value
		return nil
	})
	return job, err
}

func checkBackgroundPreviewActor(ctx context.Context, tx OwnedTx, job BackgroundPreviewJob, lock bool) error {
	if job.ActorUserID == "" && job.ActorSessionID == "" {
		return nil
	}
	actor := identity.Principal{Kind: "admin", SessionID: job.ActorSessionID, User: identity.User{ID: job.ActorUserID}}
	administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
	return administrator.check(ctx, catalogAuthorizationTx{tx: tx}, lock)
}

func validateBackgroundPreviewJob(tx OwnedTx, job BackgroundPreviewJob) (AnalysisSource, error) {
	var revision, claimed int64
	var state, runID, childID, sourceRevision, operationID, actorUserID, actorSessionID string
	if err := checkBackgroundPreviewActor(context.Background(), tx, job, true); err != nil {
		return AnalysisSource{}, err
	}
	if err := tx.QueryRow(`SELECT requested_revision,claimed_revision,state,run_id,child_id,source_revision,operation_id,actor_user_id,actor_session_id FROM background_preview_queue
  WHERE item_id=$1 FOR UPDATE`, job.ItemID).Scan(&revision, &claimed, &state, &runID, &childID, &sourceRevision, &operationID, &actorUserID, &actorSessionID); err != nil {
		return AnalysisSource{}, err
	}
	if state != "running" || revision != job.Revision || claimed != job.Revision || runID != job.RunID || childID != job.ChildID || sourceRevision != job.SourceRevision || operationID != job.OperationID || actorUserID != job.ActorUserID || actorSessionID != job.ActorSessionID {
		return AnalysisSource{}, ErrBackgroundPreviewConflict
	}
	source, err := readAnalysisSource(tx, job.ItemID, true)
	if err != nil {
		return source, err
	}
	if source.LibraryID != job.LibraryID || source.SourceRevision != job.SourceRevision || source.MediaSourceID != job.MediaSourceID {
		return AnalysisSource{}, ErrAnalysisSourceChanged
	}
	if err := checkBackgroundPreviewActor(context.Background(), tx, job, false); err != nil {
		return AnalysisSource{}, err
	}
	return source, nil
}

func (s *Store) ValidateBackgroundPreviewJob(ctx context.Context, fence AnalysisFence, job BackgroundPreviewJob) (AnalysisSource, error) {
	var result AnalysisSource
	err := withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error { var err error; result, err = validateBackgroundPreviewJob(tx, job); return err })
	return result, err
}

// WithBackgroundPreviewPublication holds task and item claim locks for one
// prepared atomic manifest replacement. Encoding and file synchronization must
// finish before calling this method; the callback may only rename the manifest.
func (s *Store) WithBackgroundPreviewPublication(ctx context.Context, fence AnalysisFence, job BackgroundPreviewJob, publish func() error) error {
	if publish == nil {
		return ErrInvalidInput
	}
	return withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error {
		if _, err := validateBackgroundPreviewJob(tx, job); err != nil {
			return err
		}
		if err := publish(); err != nil {
			return err
		}
		return checkBackgroundPreviewActor(ctx, tx, job, false)
	})
}

func (s *Store) CompleteBackgroundPreview(ctx context.Context, fence AnalysisFence, job BackgroundPreviewJob, result BackgroundPreviewResult) error {
	if len(result.ErrorCode) > 128 || strings.ContainsAny(result.ErrorCode, "\r\n\x00") {
		return ErrInvalidInput
	}
	return withBackgroundFence(ctx, s, fence, func(tx OwnedTx) error {
		// A newer request survives completion of the old claim. In particular a
		// Force request arriving during generation cannot be erased by this result.
		state := "ready"
		if result.ErrorCode != "" {
			state = "failed"
		}
		tag, err := tx.Exec(`UPDATE background_preview_queue SET completed_revision=GREATEST(completed_revision,$2),
   state=CASE WHEN requested_revision>$2 THEN 'pending' ELSE $5 END,
   force=CASE WHEN requested_revision>$2 THEN force ELSE false END,reused=$6,error_code=$7,finished_at=clock_timestamp()
   WHERE item_id=$1 AND claimed_revision=$2 AND run_id=$3 AND child_id=$4 AND state='running'`, job.ItemID, job.Revision, job.RunID, job.ChildID, state, result.Reused, result.ErrorCode)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("%w: background item claim is no longer active", ErrBackgroundPreviewConflict)
		}
		return nil
	})
}
