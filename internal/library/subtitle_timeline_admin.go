package library

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

const subtitleTimelineStreamCountSQL = `((SELECT count(*) FROM jsonb_array_elements(
 CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN i.media->'Streams' ELSE '[]'::jsonb END) stream
 WHERE stream->>'CodecType'='subtitle' AND stream->>'Codec' IN ('hdmv_pgs_subtitle','dvd_subtitle')
 AND stream->'IsExternal' IS DISTINCT FROM 'true'::jsonb)
 + (SELECT count(*) FROM item_bitmap_subtitles bitmap WHERE bitmap.item_id=i.id AND bitmap.root_id=i.root_id AND bitmap.active
 AND bitmap.stream_index>COALESCE((SELECT max((stream->>'Index')::integer) FROM jsonb_array_elements(
 CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN i.media->'Streams' ELSE '[]'::jsonb END) stream),-1)))`

var subtitleTimelineEligibleSQL = analysisPhysicalSQL + `
 AND EXISTS(SELECT 1 FROM library_roots root WHERE root.id=i.root_id AND root.library_id=i.library_id)
 AND ` + subtitleTimelineStreamCountSQL + `>0`

const subtitleTimelineItemSQL = `NOT i.is_folder AND i.type IN ('Movie','Episode') AND i.root_id IS NOT NULL`
const subtitleTimelineDetailColumns = `i.id,i.library_id,i.name,` + introSourceRevisionSQL + `,
 COALESCE((i.media->>'DurationTicks')::bigint,0),` + subtitleTimelineStreamCountSQL + `,
 COALESCE(queue.state,'missing'),COALESCE(queue.requested_revision,0)::text,COALESCE(queue.completed_revision,0)::text,
 COALESCE(queue.run_id,''),COALESCE(queue.reused,false),COALESCE(queue.error_code,''),queue.requested_at,queue.started_at,queue.finished_at`
const subtitleTimelineDetailJoins = ` LEFT JOIN subtitle_timeline_queue queue ON queue.item_id=i.id `

func (s *Store) withSubtitleTimelineAdministrator(ctx context.Context, actor identity.Principal, callback func(OwnedTx) error) error {
	if err := analysisContext(ctx); err != nil {
		return err
	}
	return s.WithOwnedTx(ctx, func(tx OwnedTx) error {
		administrator := catalogAdministrator{actor: actor, audience: identity.AdministratorNative}
		authorization := catalogAuthorizationTx{tx: tx}
		if err := administrator.check(ctx, authorization, true); err != nil {
			return err
		}
		if err := callback(tx); err != nil {
			return err
		}
		if err := administrator.check(ctx, authorization, false); err != nil {
			return err
		}
		return analysisContext(ctx)
	})
}

func scanSubtitleTimelineDetail(row rowScanner) (SubtitleTimelineDetail, error) {
	var value SubtitleTimelineDetail
	err := row.Scan(&value.ItemID, &value.LibraryID, &value.Name, &value.SourceRevision, &value.DurationTicks, &value.SubtitleStreamCount,
		&value.State, &value.RequestedRevision, &value.CompletedRevision, &value.RunID, &value.Reused, &value.ErrorCode, &value.RequestedAt, &value.StartedAt, &value.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrNotFound
	}
	return value, err
}

func (s *Store) GetSubtitleTimelineItem(ctx context.Context, actor identity.Principal, itemID string) (SubtitleTimelineDetail, error) {
	if !metadataIdentifier(itemID) {
		return SubtitleTimelineDetail{}, ErrInvalidInput
	}
	var result SubtitleTimelineDetail
	err := s.withSubtitleTimelineAdministrator(ctx, actor, func(tx OwnedTx) error {
		var err error
		result, err = scanSubtitleTimelineDetail(tx.QueryRow(`SELECT `+subtitleTimelineDetailColumns+` FROM items i `+subtitleTimelineDetailJoins+` WHERE i.id=$1 AND `+subtitleTimelineItemSQL, itemID))
		return err
	})
	return result, err
}

// This inventory contains indexed facts only. Missing/changed local files never
// silently delete a previously generated bundle or trigger regeneration.
func (s *Store) GetSubtitleTimelineItems(ctx context.Context, actor identity.Principal, libraryID, search, state string, start, limit int) (SubtitleTimelinePage, error) {
	if start < 0 || start > 1000000 || limit < 1 || limit > 200 || len(search) > 256 || !utf8.ValidString(search) || strings.ContainsRune(search, 0) || libraryID != "" && !metadataIdentifier(libraryID) {
		return SubtitleTimelinePage{}, ErrInvalidInput
	}
	if state != "" && state != "missing" && state != "pending" && state != "running" && state != "ready" && state != "failed" && state != "cancelled" {
		return SubtitleTimelinePage{}, ErrInvalidInput
	}
	result := SubtitleTimelinePage{Items: []SubtitleTimelineDetail{}, StartIndex: start, Limit: limit}
	err := s.withSubtitleTimelineAdministrator(ctx, actor, func(tx OwnedTx) error {
		population := ` FROM items i ` + subtitleTimelineDetailJoins + ` WHERE ` + subtitleTimelineItemSQL + ` AND ($1='' OR i.library_id=$1)
   AND ($2='' OR strpos(lower(i.name),$2)>0) AND ($3='' OR COALESCE(queue.state,'missing')=$3)`
		if err := tx.QueryRow(`SELECT count(*)`+population, libraryID, strings.ToLower(search), state).Scan(&result.TotalRecordCount); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT `+subtitleTimelineDetailColumns+population+` ORDER BY i.sort_name COLLATE "C",i.id LIMIT $4 OFFSET $5`, libraryID, strings.ToLower(search), state, limit, start)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			value, err := scanSubtitleTimelineDetail(rows)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, value)
		}
		return rows.Err()
	})
	return result, err
}
