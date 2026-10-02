package library

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// Batch only independent authority locks. Credential/policy validation still
// completes before any item or playback lock can wait.
func lockPlaybackMediaAuthorityBatch(ctx context.Context, tx pgx.Tx, principal identity.Principal, owner PlaybackOwner) error {
	batch := &pgx.Batch{}
	if !owner.ApplicationKey {
		batch.Queue("SELECT id FROM users WHERE id=$1 FOR SHARE", owner.UserID)
	}
	batch.Queue(`SELECT id FROM sessions WHERE id=$1
		AND user_id IS NOT DISTINCT FROM NULLIF($2,'') AND kind=$3 FOR SHARE`, owner.SessionID, owner.UserID, principal.Kind)
	if owner.ApplicationKey {
		batch.Queue(`SELECT credential_id FROM application_keys WHERE credential_id=$1 AND id=$2 FOR SHARE`, owner.SessionID, principal.ApplicationKeyID)
		batch.Queue(`SELECT id FROM application_key_clients WHERE credential_id=$1 AND id=$2 AND device_id=$3 FOR SHARE`, owner.SessionID, owner.ApplicationClientID, owner.DeviceID)
	}
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	for range batch.Len() {
		var id string
		if err := results.QueryRow().Scan(&id); err != nil {
			// Draining is mandatory, but cannot replace the original authority
			// error with a later result or a cancellation while draining.
			_ = results.Close()
			if errors.Is(err, pgx.ErrNoRows) {
				return identity.ErrUnauthorized
			}
			return fmt.Errorf("%w: lock playback media authority batch: %w", ErrUnavailable, err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("%w: complete playback authority batch: %w", ErrUnavailable, err)
	}
	return nil
}

// Source facts are completely consumed and validated before issuing this
// batch. Busy, malformed or unavailable sources cannot wait on a play lock.
func readPlaybackMediaPlayBatch(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, playID string) (PlaySession, time.Time, error) {
	play, observedAt, _, _, err := readPlaybackMediaPlayAndBindingBatch(ctx, tx, owner, playID, "", false)
	return play, observedAt, err
}

// Binding preparation adds only bounded catalog metadata between the play lock
// and the final play/database-clock observation. A binding error is separate
// from the primary play/batch error so callers can preserve fresh principal and
// playback error precedence before inspecting the prepared root. Neither root
// locks nor publication queries nor filesystem operations enter this batch.
func readPlaybackMediaPlayAndBindingBatch(ctx context.Context, tx pgx.Tx, owner PlaybackOwner, playID, itemID string, prepareBinding bool) (PlaySession, time.Time, mediaSourceRootHint, error, error) {
	batch := &pgx.Batch{}
	arguments := []any{playID, owner.UserID, owner.SessionID, owner.DeviceID, owner.ApplicationClientID}
	batch.Queue(`SELECT id FROM play_sessions
		WHERE id=$1 AND user_id IS NOT DISTINCT FROM NULLIF($2,'') AND auth_session_id=$3 AND device_id=$4
		AND application_client_id IS NOT DISTINCT FROM NULLIF($5,'') FOR SHARE`, arguments...)
	if prepareBinding {
		batch.Queue(`SELECT `+rootBindingMetadataColumns+`
			FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
			WHERE i.id=$1 AND NOT i.is_folder AND i.media IS NOT NULL
			AND i.type IN ('Movie','Episode','Video','Audio')`, itemID)
	}
	// Keep the clock query last: any play lock wait and optional binding
	// observation must finish before this database-clock value is evaluated.
	batch.Queue("SELECT "+playSessionColumns+`, clock_timestamp() FROM play_sessions
		WHERE id=$1 AND user_id IS NOT DISTINCT FROM NULLIF($2,'') AND auth_session_id=$3 AND device_id=$4
		AND application_client_id IS NOT DISTINCT FROM NULLIF($5,'')`, arguments...)
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	var id string
	if err := results.QueryRow().Scan(&id); err != nil {
		_ = results.Close()
		if errors.Is(err, pgx.ErrNoRows) {
			return PlaySession{}, time.Time{}, mediaSourceRootHint{}, nil, ErrNotFound
		}
		return PlaySession{}, time.Time{}, mediaSourceRootHint{}, nil, err
	}
	var hint mediaSourceRootHint
	var bindingErr error
	if prepareBinding {
		bindingErr = results.QueryRow().Scan(&hint.root.id, &hint.root.libraryID, &hint.root.path,
			&hint.root.allowedPath, &hint.root.relativePath, &hint.bindingRevision)
		if errors.Is(bindingErr, pgx.ErrNoRows) {
			bindingErr = ErrNotFound
		} else if bindingErr != nil {
			bindingErr = fmt.Errorf("%w: read prepared playback source binding: %w", ErrUnavailable, bindingErr)
		}
	}
	var observedAt time.Time
	var closeErr error
	// Consume and close the entire batch before any Go authority or mapping
	// validator runs. A SQL-fatal binding failure also fails the final play
	// result or the drain and must never be treated as a valid authority read.
	play, err := scanPlaySession(playbackMediaBatchClockRow{Row: results.QueryRow(), observedAt: &observedAt,
		results: results, closeErr: &closeErr})
	if errors.Is(err, pgx.ErrNoRows) {
		return PlaySession{}, time.Time{}, mediaSourceRootHint{}, bindingErr, ErrNotFound
	}
	if err != nil {
		return PlaySession{}, time.Time{}, mediaSourceRootHint{}, bindingErr, err
	}
	if closeErr != nil {
		return PlaySession{}, time.Time{}, mediaSourceRootHint{}, bindingErr, fmt.Errorf("%w: complete playback clock batch: %w", ErrUnavailable, closeErr)
	}
	if prepareBinding && bindingErr == nil {
		if err := (rootBindingRow{root: hint.root, revision: hint.bindingRevision}).validateMapping(); err != nil {
			bindingErr = fmt.Errorf("%w: invalid prepared playback source binding", ErrUnavailable)
		}
	}
	if bindingErr != nil {
		hint = mediaSourceRootHint{}
	}
	return play, observedAt, hint, bindingErr, nil
}

// scanPlaySession validates and decodes its values after Scan returns. Close
// during Scan so those validators never run with unread batch results alive.
type playbackMediaBatchClockRow struct {
	pgx.Row
	observedAt *time.Time
	results    pgx.BatchResults
	closeErr   *error
}

func (row playbackMediaBatchClockRow) Scan(destinations ...any) error {
	err := (playbackMediaClockRow{Row: row.Row, observedAt: row.observedAt}).Scan(destinations...)
	*row.closeErr = row.results.Close()
	return err
}
