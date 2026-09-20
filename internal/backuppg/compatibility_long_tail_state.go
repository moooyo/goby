package backuppg

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/settings"
)

// Historical schemas predate the selected fields. New snapshots validate each
// retained layer, including dormant overrides, before recovery normalization.
// Missing keys and explicit null retain the historical unknown-value contract.
func validateCompatibilityLongTailState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 49 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var words []string
	if err := tx.QueryRow(ctx, `SELECT sort_remove_words FROM managed_settings WHERE id=1`).Scan(&words); err != nil {
		return classifyResourceStateError(ctx, err)
	}
	if settings.ValidateStoredSorting(settings.Sorting{SortRemoveWords: words}) != nil {
		return ErrSchema
	}
	var validSortProvenance bool
	if err := tx.QueryRow(ctx, `SELECT NOT EXISTS(
		SELECT 1 FROM item_metadata_state state JOIN items item ON item.id=state.item_id
		CROSS JOIN LATERAL (SELECT CASE
			WHEN state.online_type=item.type AND state.online_source ? 'SortName' THEN state.online_source->'SortName'
			ELSE item.local_metadata->'SortName' END AS value) winning
		WHERE NOT state.automatic_sort_name_explicit AND jsonb_typeof(winning.value)='string'
			AND winning.value #>> '{}' <> '')`).Scan(&validSortProvenance); err != nil {
		return classifyResourceStateError(ctx, err)
	}
	if !validSortProvenance {
		return ErrSchema
	}
	rows, err := tx.Query(ctx, `WITH retained(value) AS (
		SELECT local_metadata FROM items
		UNION ALL
		SELECT value FROM item_metadata_state state
		CROSS JOIN LATERAL (VALUES(state.automatic),(state.overrides),(state.locked_values),(state.effective),
			(state.music_source),(state.online_source),(state.online_base)) layers(value)
	), selected AS (
		SELECT (SELECT COALESCE(jsonb_object_agg(entry.key,entry.value),'{}')
			FROM jsonb_each(retained.value) entry
			WHERE lower(entry.key) IN ('status','enddate','airsbeforeseasonnumber','airsafterseasonnumber','airsbeforeepisodenumber')) facts
		FROM retained WHERE jsonb_typeof(value)='object'
	)
	SELECT CASE WHEN octet_length(facts::text)<=8192 THEN facts END FROM selected WHERE facts<>'{}'::jsonb`)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !validCompatibilityMetadataFacts(raw) {
			return ErrSchema
		}
	}
	return classifyResourceStateError(ctx, rows.Err())
}

func validCompatibilityMetadataFacts(raw []byte) bool {
	var values map[string]json.RawMessage
	if len(raw) > 8192 || json.Unmarshal(raw, &values) != nil || values == nil {
		return false
	}
	for field, value := range values {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			continue
		}
		switch field {
		case "Status":
			var status string
			if json.Unmarshal(value, &status) != nil || (status != "Continuing" && status != "Ended") {
				return false
			}
		case "EndDate":
			var date time.Time
			if json.Unmarshal(value, &date) != nil || date.Year() < 1 || date.Year() > 9999 {
				return false
			}
			_, offset := date.Zone()
			if offset != 0 {
				return false
			}
		case "AirsBeforeSeasonNumber", "AirsAfterSeasonNumber", "AirsBeforeEpisodeNumber":
			var number int64
			if json.Unmarshal(value, &number) != nil || number < 0 || number > 2147483647 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
