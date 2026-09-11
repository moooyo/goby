package library

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The album filter follows the same physical relationship as the Audio DTO.
// A MusicAlbum itself is its album scope; an Audio item uses its nearest
// same-library album, without crossing a corrupt parent cycle.
const physicalMusicAlbumIDSQL = `(CASE WHEN i.type = 'MusicAlbum' AND i.is_folder THEN i.id
	WHEN i.type IN ('Audio', 'MusicVideo') THEN (` + itemAlbumAncestorsSQL + `
		SELECT id FROM album_ancestors WHERE type = 'MusicAlbum' AND is_folder ORDER BY depth LIMIT 1)
	END)`

func normalizeMusicFilters(query Query) (Query, error) {
	for _, values := range [][]int64{query.ArtistIds, query.AlbumArtistIds} {
		if len(values) > 1024 {
			return Query{}, ErrInvalidInput
		}
		for _, id := range values {
			if id <= 0 {
				return Query{}, ErrInvalidInput
			}
		}
	}
	for _, values := range [][]string{query.AlbumIds, query.ExcludeItemIds, query.ListItemIds} {
		if len(values) > 1024 {
			return Query{}, ErrInvalidInput
		}
		for _, id := range values {
			if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
				return Query{}, ErrInvalidInput
			}
		}
	}
	query.ArtistIds = append([]int64(nil), query.ArtistIds...)
	query.AlbumArtistIds = append([]int64(nil), query.AlbumArtistIds...)
	query.AlbumIds = append([]string(nil), query.AlbumIds...)
	query.ExcludeItemIds = append([]string(nil), query.ExcludeItemIds...)
	query.ListItemIds = append([]string(nil), query.ListItemIds...)
	return query, nil
}

func addMusicConditions(query Query, conditions []string, args []any) ([]string, []any) {
	if len(query.ArtistIds) != 0 {
		args = append(args, query.ArtistIds)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (SELECT 1 FROM item_entities association
			JOIN catalog_entities entity ON entity.id = association.entity_id
			WHERE association.item_id = i.id AND entity.kind = 'MusicArtist'
			AND association.credit_group = 1 AND association.credit_type = 'Artist' AND entity.id = ANY($%d::bigint[]))`, len(args)))
	}
	if len(query.AlbumArtistIds) != 0 {
		args = append(args, query.AlbumArtistIds)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (SELECT 1 FROM item_entities association
			JOIN catalog_entities entity ON entity.id = association.entity_id
			WHERE association.item_id = `+physicalMusicAlbumIDSQL+` AND entity.kind = 'MusicArtist'
			AND association.credit_group = 2 AND association.credit_type = 'AlbumArtist' AND entity.id = ANY($%d::bigint[]))`, len(args)))
	}
	if len(query.AlbumIds) != 0 {
		args = append(args, query.AlbumIds)
		conditions = append(conditions, fmt.Sprintf(physicalMusicAlbumIDSQL+" = ANY($%d::text[])", len(args)))
	}
	if len(query.ExcludeItemIds) != 0 {
		args = append(args, query.ExcludeItemIds)
		conditions = append(conditions, fmt.Sprintf("NOT (i.id = ANY($%d::text[]))", len(args)))
	}
	return conditions, args
}
