package library

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// MetadataItemSummary is the bounded administrator-facing catalog projection.
type MetadataItemSummary struct {
	ItemID            string `json:"ItemId"`
	LibraryID         string `json:"LibraryId"`
	ParentID          string `json:"ParentId"`
	ParentName        string `json:"ParentName"`
	Name              string `json:"Name"`
	Type              string `json:"Type"`
	Path              string `json:"Path"`
	IsFolder          bool   `json:"IsFolder"`
	IndexNumber       *int   `json:"IndexNumber"`
	ParentIndexNumber *int   `json:"ParentIndexNumber"`
	ProductionYear    *int   `json:"ProductionYear"`
	HasOverrides      bool   `json:"HasOverrides"`
	LockedFieldCount  int    `json:"LockedFieldCount"`
}

type MetadataItemQuery struct {
	SearchTerm string   `json:"SearchTerm"`
	Types      []string `json:"Types"`
	StartIndex int      `json:"StartIndex"`
	Limit      int      `json:"Limit"`
}

type MetadataItemResult struct {
	Library          Library               `json:"Library"`
	Items            []MetadataItemSummary `json:"Items"`
	TotalRecordCount int                   `json:"TotalRecordCount"`
}

const metadataItemFilterSQL = `i.library_id = $1 AND i.id <> $1
	AND i.type = ANY($2::text[])
	AND strpos(lower(i.name), lower($3::text)) > 0`

const metadataItemSummaryColumns = `i.id, i.library_id, COALESCE(i.parent_id, ''),
	COALESCE(parent.name, ''), i.name, i.type, i.path, i.is_folder,
	CASE WHEN i.type IN ('Season', 'Episode') THEN i.index_number END,
	CASE WHEN i.type = 'Episode' THEN i.parent_index_number END,
	CASE WHEN jsonb_typeof(metadata_year.value) = 'number' THEN
		CASE WHEN metadata_year.value::text::numeric BETWEEN 1 AND 9999
			AND trunc(metadata_year.value::text::numeric) = metadata_year.value::text::numeric
			THEN metadata_year.value::text::numeric::integer END
	END,
	COALESCE(ms.overrides, '{}'::jsonb) <> '{}'::jsonb,
	(SELECT count(*) FROM jsonb_object_keys(COALESCE(ms.locked_values, '{}'::jsonb)))`

// QueryMetadataItems reads one library and its summaries from the same snapshot.
func (s *Store) QueryMetadataItems(ctx context.Context, actor identity.Principal, libraryID string, query MetadataItemQuery) (MetadataItemResult, error) {
	query, err := normalizeMetadataItemQuery(libraryID, query)
	if err != nil {
		return MetadataItemResult{}, err
	}
	tx, err := s.beginMetadataRead(ctx, actor)
	if err != nil {
		return MetadataItemResult{}, err
	}
	defer rollback(tx)

	library, err := scanLibrary(tx.QueryRow(ctx, "SELECT "+libraryColumns+" FROM libraries l WHERE l.id = $1", libraryID))
	if errors.Is(err, pgx.ErrNoRows) {
		return MetadataItemResult{}, ErrNotFound
	}
	if err != nil {
		return MetadataItemResult{}, fmt.Errorf("read metadata library: %w", err)
	}
	result := MetadataItemResult{Library: library, Items: make([]MetadataItemSummary, 0)}
	args := []any{libraryID, query.Types, query.SearchTerm}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM items i WHERE "+metadataItemFilterSQL, args...).Scan(&result.TotalRecordCount); err != nil {
		return MetadataItemResult{}, fmt.Errorf("count metadata items: %w", err)
	}

	args = append(args, query.Limit, query.StartIndex)
	rows, err := tx.Query(ctx, "SELECT "+metadataItemSummaryColumns+` FROM items i
		LEFT JOIN items parent ON parent.id = i.parent_id AND parent.library_id = i.library_id
		LEFT JOIN item_metadata_state ms ON ms.item_id = i.id
		LEFT JOIN LATERAL (
			SELECT COALESCE(ms.effective, i.local_metadata)->'ProductionYear' AS value
		) metadata_year ON true
		WHERE `+metadataItemFilterSQL+`
		ORDER BY lower(i.sort_name), i.id LIMIT $4 OFFSET $5`, args...)
	if err != nil {
		return MetadataItemResult{}, fmt.Errorf("query metadata items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item MetadataItemSummary
		if err := rows.Scan(&item.ItemID, &item.LibraryID, &item.ParentID, &item.ParentName,
			&item.Name, &item.Type, &item.Path, &item.IsFolder, &item.IndexNumber,
			&item.ParentIndexNumber, &item.ProductionYear, &item.HasOverrides, &item.LockedFieldCount); err != nil {
			return MetadataItemResult{}, fmt.Errorf("scan metadata item: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return MetadataItemResult{}, fmt.Errorf("read metadata items: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return MetadataItemResult{}, fmt.Errorf("commit metadata item read: %w", err)
	}
	return result, nil
}

func normalizeMetadataItemQuery(libraryID string, query MetadataItemQuery) (MetadataItemQuery, error) {
	fields := make(map[string]string)
	if strings.TrimSpace(libraryID) == "" || len(libraryID) > 256 || !utf8.ValidString(libraryID) || strings.ContainsRune(libraryID, '\x00') {
		fields["LibraryId"] = "Must be a nonempty UTF-8 identifier of at most 256 bytes without NUL characters."
	}
	if len(query.SearchTerm) > 1024 || !utf8.ValidString(query.SearchTerm) || strings.ContainsRune(query.SearchTerm, '\x00') {
		fields["SearchTerm"] = "Must be UTF-8 text of at most 1024 bytes without NUL characters."
	}
	if query.StartIndex < 0 || int64(query.StartIndex) > math.MaxInt32 {
		fields["StartIndex"] = "Must be an integer from 0 to 2147483647."
	}
	if query.Limit < 1 || query.Limit > 200 {
		fields["Limit"] = "Must be an integer from 1 to 200."
	}
	types, err := normalizeQueryValues(query.Types, map[string]string{
		"movie": "Movie", "series": "Series", "season": "Season", "episode": "Episode",
		"audio": "Audio", "video": "Video", "musicalbum": "MusicAlbum",
		"musicartist": "MusicArtist", "folder": "Folder",
	})
	if err != nil {
		fields["Types"] = "Must contain only Movie, Series, Season, Episode, Audio, Video, MusicAlbum, MusicArtist, or Folder."
	}
	if len(fields) != 0 {
		return MetadataItemQuery{}, &MetadataValidationError{Fields: fields}
	}
	if len(types) == 0 {
		types = []string{"Movie", "Series", "Season", "Episode", "Audio", "Video", "MusicAlbum", "MusicArtist", "Folder"}
	}
	query.Types = types
	return query, nil
}
