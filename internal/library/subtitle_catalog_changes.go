package library

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// readSubtitleCatalogProjection follows attachSubtitles' active, root-matched,
// noncolliding stream order and limit. Stream properties and the validated
// content hash describe a changed representation; private inode/stat refreshes
// alone must not turn an unchanged sidecar scan into a notification.
func readSubtitleCatalogProjection(ctx context.Context, tx pgx.Tx, itemID string, highestEmbedded int) (string, error) {
	var projection string
	err := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		'Index', track.stream_index, 'Codec', track.codec, 'Language', track.language, 'Title', track.title,
		'IsDefault', track.is_default, 'IsForced', track.is_forced, 'IsHearingImpaired', track.is_hearing_impaired,
		'MIMEType', track.mime_type, 'Filename', track.filename,
		'ContentHash', track.source_hash, 'Size', track.file_size) ORDER BY track.stream_index), '[]'::jsonb)::text
		FROM (SELECT combined.* FROM (
			SELECT s.stream_index,s.codec,s.language,s.title,s.is_default,s.is_forced,s.is_hearing_impaired,
				s.mime_type,reverse(split_part(reverse(s.relative_path),'/',1)) AS filename,s.source_hash,s.file_size
			FROM item_subtitles s
			JOIN items i ON i.id = s.item_id AND i.root_id = s.root_id
			JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
			WHERE i.id = $1 AND NOT i.is_folder AND i.media IS NOT NULL AND s.active
			AND s.stream_index > $2 AND `+directItemSQL("i")+`
			UNION ALL
			SELECT s.stream_index,s.codec,s.language,s.title,s.is_default,s.is_forced,s.is_hearing_impaired,
				CASE s.codec WHEN 'srt' THEN 'application/x-subrip' ELSE 'text/vtt' END,'',s.content_sha256,octet_length(s.content)
			FROM item_owned_subtitles s JOIN items i ON i.id=s.item_id AND i.root_id=s.root_id
			JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
			WHERE i.id=$1 AND NOT i.is_folder AND i.media IS NOT NULL AND s.active
			AND s.stream_index>$2 AND s.source_revision=`+ownedSubtitleSourceRevisionSQL+` AND `+directItemSQL("i")+`) combined
			ORDER BY combined.stream_index LIMIT $3) track`, itemID, highestEmbedded, maxActiveSubtitles).Scan(&projection)
	if err != nil {
		return "", fmt.Errorf("read subtitle catalog notification projection: %w", err)
	}
	return projection, nil
}
