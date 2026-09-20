package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// ApplyMediaOCR verifies the actual source before entering the operation
// framework's atomic publication boundary. Restored jobs are not replayed:
// callers still need a fresh administrator apply claim and revision.
func (s *Store) ApplyMediaOCR(ctx context.Context, work MediaOperationWork) error {
	if work.Operation.Kind != MediaOperationOCR || !work.Apply || work.Discard || !validMediaOperationWork(work) {
		return ErrInvalidInput
	}
	current := work.Operation
	current.RequestActor = current.ApplyActor
	snapshot, err := s.readMediaOCRSnapshot(ctx, current)
	if err != nil {
		return err
	}
	file, _, err := runMediaSourceWorker(ctx, mediaSourceWorkers, func() (*os.File, MediaFile, error) {
		file, err := s.openMediaSource(ctx, snapshot)
		return file, snapshot.mediaFile, err
	})
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return s.CommitMediaOperationApply(ctx, work, applyMediaOCR)
}

// applyMediaOCR is bounded, transaction-local work. The framework holds the
// account, item, root and operation locks, checks the worker token and review
// revision, and commits the owned bytes and completed state together.
func applyMediaOCR(ctx context.Context, tx pgx.Tx, operation MediaOperation) error {
	if operation.Kind != MediaOperationOCR || operation.ID == "" || !validOwnedSubtitleMetadata(operation.Parameters.Language, operation.Parameters.Title) {
		return ErrInvalidInput
	}
	var rootID, libraryID, parentID, sourceRevision string
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT i.root_id,i.library_id,COALESCE(i.parent_id,''),i.media,`+MediaOperationSourceRevisionSQL+`
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND NOT i.is_folder AND i.media IS NOT NULL FOR UPDATE OF i`, operation.ItemID).
		Scan(&rootID, &libraryID, &parentID, &raw, &sourceRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if sourceRevision != operation.SourceRevision || rootID != operation.RootID || libraryID != operation.LibraryID || operation.MediaSourceID != media.SourceID(operation.ItemID) {
		return ErrSourceChanged
	}
	var info media.Info
	if err := json.Unmarshal(raw, &info); err != nil {
		return fmt.Errorf("%w: OCR source probe is invalid", ErrUnavailable)
	}
	cues, err := readMediaOCRApplyCues(ctx, tx, operation.ID)
	if err != nil {
		return err
	}
	content, err := renderMediaOCRCues(cues, operation.Parameters.OutputFormat, info.DurationTicks)
	if err != nil {
		return err
	}
	total, highest, active, err := subtitleCatalogCapacity(ctx, tx, operation.ItemID)
	if err != nil {
		return err
	}
	if embedded := highestEmbeddedStreamIndex(&info); embedded > highest {
		highest = embedded
	}
	if total >= maxSubtitleIdentities || active >= maxActiveSubtitles || highest >= maxSubtitleStreamIndex {
		return fmt.Errorf("%w: subtitle catalog capacity exceeded", ErrInvalidInput)
	}
	index := highest + 1
	digest := sha256.Sum256(content)
	_, err = tx.Exec(ctx, `INSERT INTO item_owned_subtitles
		(item_id,root_id,stream_index,operation_id,source_revision,codec,language,title,
		 is_default,is_forced,is_hearing_impaired,content,content_sha256)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, operation.ItemID, rootID, index, operation.ID,
		operation.SourceRevision, operation.Parameters.OutputFormat, operation.Parameters.Language, operation.Parameters.Title,
		operation.Parameters.IsDefault, operation.Parameters.IsForced, operation.Parameters.IsHearingImpaired, content, hex.EncodeToString(digest[:]))
	if err != nil {
		return fmt.Errorf("publish reviewed OCR subtitle: %w", err)
	}
	// Keep the immutable OCR observation and reviewed result hash while exposing
	// the newly published stream identity in the durable operation result.
	if _, err := tx.Exec(ctx, `UPDATE media_operations SET result_summary=result_summary ||
		jsonb_build_object('AppliedStreamIndex',$2::integer,'AppliedContentSHA256',$3::text,'AppliedFormat',$4::text)
		WHERE id=$1`, operation.ID, index, hex.EncodeToString(digest[:]), operation.Parameters.OutputFormat); err != nil {
		return err
	}
	return recordCatalogChanges(tx, CatalogChange{Kind: CatalogUpdated, ItemID: operation.ItemID, LibraryID: libraryID, ParentID: parentID})
}

func readMediaOCRApplyCues(ctx context.Context, tx pgx.Tx, operationID string) ([]MediaOperationCue, error) {
	rows, err := tx.Query(ctx, `SELECT ordinal,start_ticks,end_ticks,text,included
		FROM media_operation_cues WHERE operation_id=$1 ORDER BY ordinal LIMIT $2`, operationID, MaxMediaOperationCues+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cues []MediaOperationCue
	textBytes := 0
	for rows.Next() {
		var cue MediaOperationCue
		if err := rows.Scan(&cue.Ordinal, &cue.StartTicks, &cue.EndTicks, &cue.Text, &cue.Included); err != nil {
			return nil, err
		}
		textBytes += len(cue.Text)
		if len(cues) >= MaxMediaOperationCues || textBytes > MaxMediaOperationTextBytes {
			return nil, fmt.Errorf("%w: OCR review exceeds its resource limit", ErrInvalidInput)
		}
		cues = append(cues, cue)
	}
	return cues, rows.Err()
}
