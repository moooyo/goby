package library

import (
	"fmt"
	"strings"
)

func normalizeItemSort(query Query) (Query, error) {
	if strings.TrimSpace(query.SortBy) == "" {
		query.SortBy = "SortName"
	}
	fields := strings.Split(query.SortBy, ",")
	if len(fields) > 8 {
		return Query{}, ErrInvalidInput
	}
	seen := make(map[string]bool, len(fields))
	for index, field := range fields {
		canonical := map[string]string{
			"sortname": "SortName", "name": "Name", "datecreated": "DateCreated", "indexnumber": "IndexNumber",
			"productionyear": "ProductionYear", "premieredate": "PremiereDate", "dateplayed": "DatePlayed", "playcount": "PlayCount",
			"communityrating": "CommunityRating", "runtime": "Runtime", "parentindexnumber": "ParentIndexNumber",
			"album": "Album", "artist": "Artist", "albumartist": "AlbumArtist",
		}[strings.ToLower(strings.TrimSpace(field))]
		if canonical == "" || seen[canonical] {
			return Query{}, ErrInvalidInput
		}
		seen[canonical], fields[index] = true, canonical
	}
	if strings.TrimSpace(query.SortOrder) == "" {
		query.SortOrder = "ASC"
	}
	directions := strings.Split(query.SortOrder, ",")
	if len(directions) != 1 && len(directions) != len(fields) {
		return Query{}, ErrInvalidInput
	}
	for index, direction := range directions {
		switch strings.ToLower(strings.TrimSpace(direction)) {
		case "asc", "ascending":
			directions[index] = "ASC"
		case "desc", "descending":
			directions[index] = "DESC"
		default:
			return Query{}, ErrInvalidInput
		}
	}
	query.SortBy, query.SortOrder = strings.Join(fields, ","), strings.Join(directions, ",")
	if itemSortUsesUserData(query.SortBy) && query.UserID == "" {
		return Query{}, ErrInvalidInput
	}
	return query, nil
}

func itemSortUsesUserData(fields string) bool {
	for _, field := range strings.Split(fields, ",") {
		if field == "DatePlayed" || field == "PlayCount" {
			return true
		}
	}
	return false
}

func itemOrderSQL(query Query, userParameter int) string {
	fields, directions := strings.Split(query.SortBy, ","), strings.Split(query.SortOrder, ",")
	clauses := make([]string, 0, len(fields)+2)
	lastDirection := "ASC"
	for index, field := range fields {
		direction := directions[0]
		if len(directions) > 1 {
			direction = directions[index]
		}
		lastDirection = direction
		column := "lower(i.sort_name)"
		switch field {
		case "Name":
			column = "lower(i.name)"
		case "DateCreated":
			column = "i.created_at"
		case "IndexNumber":
			clauses = append(clauses, "i.parent_index_number "+direction)
			column = "i.index_number"
		case "ParentIndexNumber":
			column = "i.parent_index_number"
		case "Album":
			column = `(SELECT lower(music_album.name) FROM items music_album WHERE music_album.id=` + physicalMusicAlbumIDSQL + `)`
		case "Artist", "AlbumArtist":
			column = itemMusicArtistSortSQL(field)
		case "CommunityRating":
			column = navigationNumberSQL(itemMetadataColumn, "CommunityRating")
		case "Runtime":
			column = navigationNumberSQL("i.media", "DurationTicks")
		case "ProductionYear":
			column = "(CASE WHEN jsonb_typeof(" + itemMetadataColumn + "->'ProductionYear') = 'number' THEN (" + itemMetadataColumn + "->>'ProductionYear')::numeric END)"
		case "PremiereDate":
			column = "(CASE WHEN jsonb_typeof(" + itemMetadataColumn + "->'PremiereDate') = 'string' THEN NULLIF(" + itemMetadataColumn + "->>'PremiereDate', '')::timestamptz END)"
		case "DatePlayed":
			column = fmt.Sprintf("(SELECT data.last_played_at FROM user_item_data data WHERE data.item_id = i.id AND data.user_id = $%d::text)", userParameter)
		case "PlayCount":
			column = fmt.Sprintf("COALESCE((SELECT data.play_count FROM user_item_data data WHERE data.item_id = i.id AND data.user_id = $%d::text), 0)", userParameter)
		}
		clauses = append(clauses, column+" "+direction+" NULLS LAST")
	}
	return strings.Join(append(clauses, "i.id "+lastDirection), ", ")
}

// itemMusicArtistSortSQL chooses the first effective display credit, using the
// same ordered relationship and album inheritance as the music DTO. Prefix
// bounds reuse this scalar so jumping to a bucket cannot use a second credit
// that disagrees with the row's sort position. The role is package-owned.
func itemMusicArtistSortSQL(role string) string {
	owner, group := "i.id", "1"
	if role == "AlbumArtist" {
		owner, group = effectiveMusicAlbumArtistItemSQL, "2"
	}
	return `(SELECT lower(association.display_name) FROM item_entities association
		JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE association.item_id=` + owner + ` AND entity.kind='MusicArtist'
		AND association.credit_group=` + group + ` AND association.credit_type=` + policySQLString(role) + `
		ORDER BY association.position,entity.id LIMIT 1)`
}
