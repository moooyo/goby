package backuppg

import (
	"bytes"
	"context"
	"encoding/json"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/library"
)

// The inventory reserves stream identities, including retired indexes, and
// retains exact source component evidence. Recovery never opens those paths.
// A changed probe or a move within the same library may leave a stale snapshot;
// the ordinary source admission path decides whether it is currently usable.
func validateBitmapSubtitleState(ctx context.Context, tx pgx.Tx, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if version < 61 {
		return nil
	}
	if tx == nil {
		return ErrDatabase
	}
	var valid bool
	err := tx.QueryRow(ctx, bitmapSubtitleStateRelationsSQL).Scan(&valid)
	if err := classifyResourceStateError(ctx, err); err != nil {
		return err
	}
	if !valid {
		return ErrSchema
	}
	return analysisStateRows(ctx, tx, `SELECT stream_index,source_stream_index,codec,format,language,title,
		source_hash,is_default,is_forced,is_hearing_impaired,components,relative_path FROM item_bitmap_subtitles`, func(rows pgx.Rows) error {
		var track library.BitmapSubtitle
		var components []byte
		var relative string
		if err := rows.Scan(&track.Index, &track.SourceStreamIndex, &track.Codec, &track.Format, &track.Language,
			&track.Title, &track.Tag, &track.IsDefault, &track.IsForced, &track.IsHearingImpaired, &components, &relative); err != nil {
			return classifyResourceStateError(ctx, err)
		}
		if !validBitmapSubtitleArchiveTrack(track, relative, components) {
			return ErrSchema
		}
		return nil
	})
}

const bitmapSubtitleStateRelationsSQL = `WITH subtitle_indexes AS (
	SELECT item_id,stream_index FROM item_subtitles
	UNION ALL SELECT item_id,stream_index FROM item_owned_subtitles
	UNION ALL SELECT item_id,stream_index FROM item_bitmap_subtitles
)
SELECT NOT EXISTS(SELECT 1 FROM item_bitmap_subtitles subtitle
	LEFT JOIN items item ON item.id=subtitle.item_id
	LEFT JOIN library_roots root ON root.id=subtitle.root_id
	WHERE item.id IS NULL OR root.id IS NULL OR root.library_id IS DISTINCT FROM item.library_id)
	AND NOT EXISTS(SELECT 1 FROM subtitle_indexes GROUP BY item_id,stream_index HAVING count(*)>1)`

func validBitmapSubtitleArchiveTrack(track library.BitmapSubtitle, relative string, raw []byte) bool {
	if relative == "" || relative == "." || len(relative) > 4096 || !utf8.ValidString(relative) ||
		path.IsAbs(relative) || path.Clean(relative) != relative || strings.ContainsAny(relative, "\\\x00") {
		return false
	}
	for _, component := range strings.Split(relative, "/") {
		if component == ".." {
			return false
		}
	}
	if len(track.Language) > 32 || len(track.Title) > 512 {
		return false
	}
	components, valid := decodeBitmapSubtitleArchiveComponents(raw)
	if !valid {
		return false
	}
	track.Filename = path.Base(relative)
	track.Components = components
	return library.ValidateBitmapSubtitle(track) == nil
}

// PostgreSQL JSONB has already canonicalized object keys on storage. An exact
// field inventory rejects unknown, mis-cased, missing, and null
// facts rather than letting Go's permissive struct decoder normalize them.
func decodeBitmapSubtitleArchiveComponents(raw []byte) ([]library.BitmapSubtitleComponent, bool) {
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, false
	}
	var objects []map[string]json.RawMessage
	if json.Unmarshal(raw, &objects) != nil || len(objects) < 1 || len(objects) > 2 {
		return nil, false
	}
	for _, object := range objects {
		if len(object) != 6 {
			return nil, false
		}
		for _, key := range []string{"Name", "Identity", "SHA256", "Size", "ModifiedNS", "ChangeTimeNS"} {
			value, exists := object[key]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return nil, false
			}
		}
	}
	var components []library.BitmapSubtitleComponent
	if json.Unmarshal(raw, &components) != nil {
		return nil, false
	}
	return components, true
}
