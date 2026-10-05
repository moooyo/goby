package library

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

const audioWaveformStreamCountSQL = `(SELECT count(*) FROM jsonb_array_elements(
 CASE WHEN jsonb_typeof(i.media->'Streams')='array' THEN i.media->'Streams' ELSE '[]'::jsonb END) stream
 WHERE stream->>'CodecType'='audio' AND stream->'IsExternal' IS DISTINCT FROM 'true'::jsonb)`

var audioWaveformEligibleSQL = analysisPhysicalSQL + ` AND ` + audioWaveformStreamCountSQL + `>0`

const audioWaveformItemSQL = `NOT i.is_folder AND i.type IN ('Movie','Episode') AND i.root_id IS NOT NULL`
const audioWaveformDetailColumns = `i.id,i.library_id,i.name,` + introSourceRevisionSQL + `,
 COALESCE((i.media->>'DurationTicks')::bigint,0),` + audioWaveformStreamCountSQL + `,
 COALESCE(queue.state,'missing'),COALESCE(queue.requested_revision,0)::text,COALESCE(queue.completed_revision,0)::text,
 COALESCE(queue.run_id,''),COALESCE(queue.reused,false),COALESCE(queue.error_code,''),queue.requested_at,queue.started_at,queue.finished_at`
const audioWaveformDetailJoins = ` LEFT JOIN audio_waveform_queue queue ON queue.item_id=i.id `

func (s *Store) withAudioWaveformAdministrator(ctx context.Context, actor identity.Principal, callback func(OwnedTx) error) error {
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

func scanAudioWaveformDetail(row rowScanner) (AudioWaveformDetail, error) {
	var value AudioWaveformDetail
	err := row.Scan(&value.ItemID, &value.LibraryID, &value.Name, &value.SourceRevision, &value.DurationTicks, &value.AudioStreamCount,
		&value.State, &value.RequestedRevision, &value.CompletedRevision, &value.RunID, &value.Reused, &value.ErrorCode, &value.RequestedAt, &value.StartedAt, &value.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, ErrNotFound
	}
	return value, err
}

func (s *Store) GetAudioWaveformItem(ctx context.Context, actor identity.Principal, itemID string) (AudioWaveformDetail, error) {
	if !metadataIdentifier(itemID) {
		return AudioWaveformDetail{}, ErrInvalidInput
	}
	var result AudioWaveformDetail
	err := s.withAudioWaveformAdministrator(ctx, actor, func(tx OwnedTx) error {
		var err error
		result, err = scanAudioWaveformDetail(tx.QueryRow(`SELECT `+audioWaveformDetailColumns+` FROM items i `+audioWaveformDetailJoins+` WHERE i.id=$1 AND `+audioWaveformItemSQL, itemID))
		return err
	})
	return result, err
}

// This inventory contains indexed facts only. Missing/changed local files never
// silently delete a previously generated bundle or trigger regeneration.
func (s *Store) GetAudioWaveformItems(ctx context.Context, actor identity.Principal, libraryID, search, state string, start, limit int) (AudioWaveformPage, error) {
	if start < 0 || start > 1000000 || limit < 1 || limit > 200 || len(search) > 256 || !utf8.ValidString(search) || strings.ContainsRune(search, 0) || libraryID != "" && !metadataIdentifier(libraryID) {
		return AudioWaveformPage{}, ErrInvalidInput
	}
	if state != "" && state != "missing" && state != "pending" && state != "running" && state != "ready" && state != "failed" && state != "cancelled" {
		return AudioWaveformPage{}, ErrInvalidInput
	}
	result := AudioWaveformPage{Items: []AudioWaveformDetail{}, StartIndex: start, Limit: limit}
	err := s.withAudioWaveformAdministrator(ctx, actor, func(tx OwnedTx) error {
		population := ` FROM items i ` + audioWaveformDetailJoins + ` WHERE ` + audioWaveformItemSQL + ` AND ($1='' OR i.library_id=$1)
   AND ($2='' OR strpos(lower(i.name),$2)>0) AND ($3='' OR COALESCE(queue.state,'missing')=$3)`
		if err := tx.QueryRow(`SELECT count(*)`+population, libraryID, strings.ToLower(search), state).Scan(&result.TotalRecordCount); err != nil {
			return err
		}
		rows, err := tx.Query(`SELECT `+audioWaveformDetailColumns+population+` ORDER BY i.sort_name COLLATE "C",i.id LIMIT $4 OFFSET $5`, libraryID, strings.ToLower(search), state, limit, start)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			value, err := scanAudioWaveformDetail(rows)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, value)
		}
		return rows.Err()
	})
	return result, err
}
