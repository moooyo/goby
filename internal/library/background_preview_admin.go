package library

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

const backgroundDetailColumns = `i.id,i.library_id,i.name,COALESCE(settings.revision,0)::text,` + introSourceRevisionSQL + `,
 COALESCE((i.media->>'DurationTicks')::bigint,0),settings.manual_start_ticks,
 COALESCE(queue.state,'missing'),COALESCE(queue.requested_revision,0)::text,COALESCE(queue.completed_revision,0)::text,
 COALESCE(queue.run_id,''),COALESCE(queue.reused,false),COALESCE(queue.error_code,''),queue.requested_at,queue.started_at,queue.finished_at`
const backgroundDetailJoins = ` LEFT JOIN item_background_preview_settings settings ON settings.item_id=i.id
 LEFT JOIN background_preview_queue queue ON queue.item_id=i.id `
const backgroundItemSQL = `NOT i.is_folder AND i.type IN ('Movie','Episode') AND i.root_id IS NOT NULL`

func scanBackgroundPreviewDetail(row rowScanner) (BackgroundPreviewDetail, error) {
	var value BackgroundPreviewDetail
	err := row.Scan(&value.ItemID, &value.LibraryID, &value.Name, &value.Revision, &value.SourceRevision, &value.DurationTicks, &value.StartTicks,
		&value.State, &value.RequestedRevision, &value.CompletedRevision, &value.RunID, &value.Reused, &value.ErrorCode, &value.RequestedAt, &value.StartedAt, &value.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrNotFound
	}
	return value, err
}

func readBackgroundPreviewDetail(tx OwnedTx, itemID string) (BackgroundPreviewDetail, error) {
	return scanBackgroundPreviewDetail(tx.QueryRow(`SELECT `+backgroundDetailColumns+` FROM items i `+backgroundDetailJoins+` WHERE i.id=$1 AND `+backgroundItemSQL, itemID))
}

func (s *Store) GetBackgroundPreviewAsAdministrator(ctx context.Context, actor identity.Principal, itemID string) (BackgroundPreviewDetail, error) {
	if !metadataIdentifier(itemID) {
		return BackgroundPreviewDetail{}, ErrInvalidInput
	}
	var result BackgroundPreviewDetail
	err := s.withBackgroundAdministrator(ctx, actor, func(tx OwnedTx) error {
		var err error
		result, err = readBackgroundPreviewDetail(tx, itemID)
		return err
	})
	return result, err
}

func (s *Store) UpdateBackgroundPreviewAsAdministrator(ctx context.Context, actor identity.Principal, itemID string, input BackgroundPreviewEdit) (BackgroundPreviewDetail, error) {
	revision, err := strconv.ParseInt(input.Revision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != input.Revision || !metadataIdentifier(itemID) || !analysisOpaque(input.SourceRevision, 256) {
		return BackgroundPreviewDetail{}, ErrInvalidInput
	}
	var result BackgroundPreviewDetail
	err = s.withBackgroundAdministrator(ctx, actor, func(tx OwnedTx) error {
		var id string
		if err := tx.QueryRow(`SELECT i.id FROM items i WHERE i.id=$1 AND `+backgroundItemSQL+` FOR UPDATE`, itemID).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		current, err := readBackgroundPreviewDetail(tx, itemID)
		if err != nil {
			return err
		}
		if current.Revision != input.Revision || current.SourceRevision != input.SourceRevision {
			return ErrBackgroundPreviewConflict
		}
		if input.StartTicks != nil && (*input.StartTicks < 0 || *input.StartTicks >= current.DurationTicks) {
			return ErrInvalidInput
		}
		if revision == int64(^uint64(0)>>1) {
			return ErrBackgroundPreviewConflict
		}
		if _, err := tx.Exec(`INSERT INTO item_background_preview_settings(item_id,manual_start_ticks,updated_by) VALUES($1,$2,$3)
   ON CONFLICT(item_id) DO UPDATE SET revision=item_background_preview_settings.revision+1,
   manual_start_ticks=EXCLUDED.manual_start_ticks,updated_by=EXCLUDED.updated_by,updated_at=clock_timestamp()`, itemID, input.StartTicks, actor.User.ID); err != nil {
			return err
		}
		result, err = readBackgroundPreviewDetail(tx, itemID)
		return err
	})
	return result, err
}

// ListBackgroundPreviewItems is a database-only administrator inventory. Disk
// discovery and reads happen only in the dedicated sidecar API and worker.
func (s *Store) ListBackgroundPreviewItems(ctx context.Context, actor identity.Principal, libraryID, search, state string, start, limit int) (BackgroundPreviewPage, error) {
	if start < 0 || start > 1000000 || limit < 1 || limit > 200 || len(search) > 256 || !utf8.ValidString(search) || strings.ContainsRune(search, 0) || libraryID != "" && !metadataIdentifier(libraryID) {
		return BackgroundPreviewPage{}, ErrInvalidInput
	}
	if state != "" && state != "missing" && state != "pending" && state != "running" && state != "ready" && state != "failed" && state != "cancelled" {
		return BackgroundPreviewPage{}, ErrInvalidInput
	}
	result := BackgroundPreviewPage{Items: []BackgroundPreviewDetail{}, StartIndex: start, Limit: limit}
	err := s.withBackgroundAdministrator(ctx, actor, func(tx OwnedTx) error {
		population := ` FROM items i ` + backgroundDetailJoins + ` WHERE ` + backgroundItemSQL + ` AND ($1='' OR i.library_id=$1)
   AND ($2='' OR strpos(lower(i.name),$2)>0) AND ($3='' OR COALESCE(queue.state,'missing')=$3)`
		if err := tx.QueryRow(`SELECT count(*)`+population, libraryID, strings.ToLower(search), state).Scan(&result.TotalRecordCount); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT `+backgroundDetailColumns+population+` ORDER BY i.sort_name COLLATE "C",i.id LIMIT $4 OFFSET $5`, libraryID, strings.ToLower(search), state, limit, start)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scanBackgroundPreviewDetail(rows)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
		}
		return rows.Err()
	})
	return result, err
}
