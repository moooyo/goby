package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
)

const mediaOperationCueColumns = `ordinal,original_start_ticks,original_end_ticks,original_text,start_ticks,end_ticks,text,included,confidence,warnings,image_sha256,is_forced,is_hearing_impaired`

func scanMediaOperationCue(row rowScanner) (MediaOperationCue, error) {
	var cue MediaOperationCue
	var warnings []byte
	err := row.Scan(&cue.Ordinal, &cue.OriginalStartTicks, &cue.OriginalEndTicks, &cue.OriginalText, &cue.StartTicks, &cue.EndTicks, &cue.Text, &cue.Included, &cue.Confidence, &warnings, &cue.ImageSHA256, &cue.IsForced, &cue.IsHearingImpaired)
	if err != nil {
		return cue, err
	}
	if json.Unmarshal(warnings, &cue.Warnings) != nil {
		return cue, ErrUnavailable
	}
	if cue.Warnings == nil {
		cue.Warnings = []string{}
	}
	return cue, nil
}

func readMediaOperationCues(ctx context.Context, tx pgx.Tx, id string) ([]MediaOperationCue, error) {
	rows, err := tx.Query(ctx, `SELECT `+mediaOperationCueColumns+` FROM media_operation_cues WHERE operation_id=$1 ORDER BY ordinal LIMIT $2`, id, MaxMediaOperationCues+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cues := []MediaOperationCue{}
	size := 0
	for rows.Next() {
		cue, e := scanMediaOperationCue(rows)
		if e != nil {
			return nil, e
		}
		size += len(cue.Text) + len(cue.OriginalText)
		if len(cues) >= MaxMediaOperationCues || size > 2*MaxMediaOperationTextBytes {
			return nil, ErrInvalidInput
		}
		if cue.Ordinal != len(cues) {
			return nil, ErrUnavailable
		}
		cues = append(cues, cue)
	}
	return cues, rows.Err()
}

func (s *Store) ListMediaOperationCues(ctx context.Context, actor identity.Principal, id string, options MediaOperationPageOptions) (MediaOperationCuePage, error) {
	options, err := normalizeMediaOperationPage(options)
	result := MediaOperationCuePage{Items: []MediaOperationCue{}, StartIndex: options.StartIndex, Limit: options.Limit}
	if err != nil || !metadataIdentifier(id) {
		return result, ErrInvalidInput
	}
	tx, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	result.Operation, err = readMediaOperation(ctx, tx, id, false)
	if err != nil {
		return result, err
	}
	if result.Operation.Kind != MediaOperationOCR {
		return result, ErrMediaOperationState
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM media_operation_cues WHERE operation_id=$1`, id).Scan(&result.TotalRecordCount); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT `+mediaOperationCueColumns+` FROM media_operation_cues WHERE operation_id=$1 ORDER BY ordinal LIMIT $2 OFFSET $3`, id, options.Limit, options.StartIndex)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		cue, e := scanMediaOperationCue(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Items = append(result.Items, cue)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Store) GetMediaOperationReview(ctx context.Context, actor identity.Principal, id string, options MediaOperationPageOptions) (MediaOperationCuePage, error) {
	return s.ListMediaOperationCues(ctx, actor, id, options)
}

func (s *Store) GetMediaOperationCueImage(ctx context.Context, actor identity.Principal, id string, ordinal int) ([]byte, error) {
	if !metadataIdentifier(id) || ordinal < 0 || ordinal >= MaxMediaOperationCues {
		return nil, ErrInvalidInput
	}
	tx, err := s.mediaOperationRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	var data []byte
	var digest string
	err = tx.QueryRow(ctx, `SELECT image_png,image_sha256 FROM media_operation_cues WHERE operation_id=$1 AND ordinal=$2`, id, ordinal).Scan(&data, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrNotFound
	}
	sum := sha256.Sum256(data)
	if len(data) > 1<<20 || hex.EncodeToString(sum[:]) != digest {
		return nil, ErrUnavailable
	}
	if err = checkMediaOperationActor(ctx, tx, actor, false); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return data, nil
}

func validMediaOperationCueEdit(edit MediaOperationCueEdit) bool {
	return edit.Ordinal >= 0 && edit.Ordinal < MaxMediaOperationCues && edit.StartTicks >= 0 && edit.EndTicks > edit.StartTicks && len(edit.Text) <= 256<<10 && utf8.ValidString(edit.Text) &&
		strings.IndexFunc(edit.Text, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) < 0 && (!edit.Included || strings.TrimSpace(edit.Text) != "")
}

// Review updates preserve original recognition evidence and images. A source
// replacement does not destroy a useful draft, but application still requires
// the original current source revision and separate administrator authority.
func (s *Store) UpdateMediaOperationReview(ctx context.Context, actor identity.Principal, id string, revision int64, edits []MediaOperationCueEdit) (MediaOperation, error) {
	var result MediaOperation
	if !metadataIdentifier(id) || revision < 1 || len(edits) < 1 || len(edits) > 200 {
		return result, ErrInvalidInput
	}
	seen := map[int]bool{}
	size := 0
	for _, edit := range edits {
		if !validMediaOperationCueEdit(edit) || seen[edit.Ordinal] {
			return result, ErrInvalidInput
		}
		seen[edit.Ordinal] = true
		size += len(edit.Text)
		if size > MaxMediaOperationTextBytes {
			return result, ErrInvalidInput
		}
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return result, err
	}
	defer rollback(tx)
	protected := tx.(*ownedTx).ctx
	if err = checkMediaOperationActor(protected, tx, actor, true); err != nil {
		return result, err
	}
	op, err := readMediaOperation(protected, tx, id, true)
	if err != nil {
		return result, err
	}
	if op.Revision != revision {
		return result, ErrMediaOperationConflict
	}
	if op.Kind != MediaOperationOCR || op.State != "ready" || op.CancelRequestedAt != nil {
		return result, ErrMediaOperationState
	}
	cues, err := readMediaOperationCues(protected, tx, id)
	if err != nil {
		return result, err
	}
	for _, edit := range edits {
		if edit.Ordinal >= len(cues) {
			return result, ErrInvalidInput
		}
		cue := &cues[edit.Ordinal]
		cue.StartTicks, cue.EndTicks, cue.Text, cue.Included = edit.StartTicks, edit.EndTicks, edit.Text, edit.Included
	}
	size = 0
	for _, cue := range cues {
		size += len(cue.Text)
		if size > MaxMediaOperationTextBytes {
			return result, ErrInvalidInput
		}
	}
	if err = validateMediaOperationReview(op, cues, true); err != nil {
		return result, err
	}
	digest, err := mediaOperationReviewHash(op, cues)
	if err != nil {
		return result, err
	}
	for _, edit := range edits {
		_, err = tx.Exec(protected, `UPDATE media_operation_cues SET start_ticks=$3,end_ticks=$4,text=$5,included=$6 WHERE operation_id=$1 AND ordinal=$2`, id, edit.Ordinal, edit.StartTicks, edit.EndTicks, edit.Text, edit.Included)
		if err != nil {
			return result, err
		}
	}
	_, err = tx.Exec(protected, `UPDATE media_operations SET revision=revision+1,result_hash=$2,updated_at=clock_timestamp() WHERE id=$1`, id, digest)
	if err != nil {
		return result, err
	}
	result, err = readMediaOperation(protected, tx, id, false)
	if err != nil {
		return result, err
	}
	if err = checkMediaOperationActor(protected, tx, actor, false); err != nil {
		return result, err
	}
	return result, tx.Commit(protected)
}

func validateMediaOperationReview(op MediaOperation, cues []MediaOperationCue, allowEmpty bool) error {
	var snapshot struct{ Media *media.Info }
	if json.Unmarshal(op.SourceSnapshot, &snapshot) != nil || snapshot.Media == nil || snapshot.Media.DurationTicks <= 0 {
		return ErrUnavailable
	}
	included := false
	for _, cue := range cues {
		included = included || cue.Included
	}
	if !included && allowEmpty {
		return nil
	}
	_, err := renderMediaOCRCues(cues, op.Parameters.OutputFormat, snapshot.Media.DurationTicks)
	return err
}

func (s *Store) ReadMediaOperationCuesForWork(ctx context.Context, work MediaOperationWork) ([]MediaOperationCue, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	op, err := readMediaOperation(ctx, tx, work.Operation.ID, false)
	if err != nil {
		return nil, err
	}
	if op.WorkerToken != work.Token || work.Token == "" || op.Kind != MediaOperationOCR || op.State != "applying" || op.CancelRequestedAt != nil {
		return nil, ErrMediaOperationState
	}
	if err = checkMediaOperationActor(ctx, tx, op.ApplyActor, false); err != nil {
		return nil, err
	}
	cues, err := readMediaOperationCues(ctx, tx, op.ID)
	if err != nil {
		return nil, err
	}
	return cues, tx.Commit(ctx)
}
