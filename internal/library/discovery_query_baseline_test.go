package library

import (
	"fmt"
	"strings"
)

// These frozen query builders retain the pre-L02/L03 shape for opt-in comparisons.
func discoveryProfileOriginalMixSQL(seed resolvedMusicMixSeed, query InstantMixQuery, access libraryAccess, parent string) (string, []any) {
	prefix, filter, args := itemQuerySQL(query.Query, access, parent)
	if prefix == "" {
		prefix = "WITH "
	} else {
		prefix = strings.TrimSpace(prefix) + ", "
	}
	args = append(args, seed.itemID, seed.entityID)
	itemParameter, entityParameter := len(args)-1, len(args)
	prefix += fmt.Sprintf(`mix_input AS (SELECT $%d::text AS item_id,$%d::bigint AS entity_id), `, itemParameter, entityParameter)
	prefix += `mix_scope AS MATERIALIZED (
		SELECT i.id,i.type,` + access.scopeSQL(physicalMusicAlbumIDSQL) + ` AS album_id,
		` + access.scopeSQL(effectiveMusicAlbumArtistItemSQL) + ` AS album_artist_item_id,
		(` + musicMixPlayableSQL("i") + `) AS playable FROM items i
		WHERE i.type IN ('Audio','MusicAlbum','MusicVideo') AND ` + access.ordinarySQL("i") + `
	), mix_credits AS MATERIALIZED (
		SELECT DISTINCT scope.id AS item_id,entity.id AS entity_id,entity.kind FROM mix_scope scope
		JOIN item_entities association ON association.item_id=scope.id JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE ` + similarOrdinaryCreditSQL + ` OR ` + similarArtistCreditSQL + `
		UNION SELECT scope.id,entity.id,entity.kind FROM mix_scope scope
		JOIN item_entities association ON association.item_id=scope.album_artist_item_id JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE ` + similarAlbumArtistCreditSQL + `
	), mix_seed_tracks AS MATERIALIZED (
		SELECT scope.id FROM mix_scope scope WHERE scope.playable AND `
	switch seed.kind {
	case "Song":
		prefix += fmt.Sprintf("scope.id=$%d::text", itemParameter)
	case "Album":
		prefix += fmt.Sprintf("scope.album_id=$%d::text", itemParameter)
	case "Playlist":
		prefix += fmt.Sprintf(`EXISTS(SELECT 1 FROM media_collection_entries membership WHERE membership.collection_id=$%d::text AND membership.item_id=scope.id)`, itemParameter)
	case "Artist", "Genre":
		prefix += fmt.Sprintf(`EXISTS(SELECT 1 FROM mix_credits credit WHERE credit.item_id=scope.id AND credit.entity_id=$%d::bigint)`, entityParameter)
	}
	prefix += `
	), mix_seed_items AS (
		SELECT id FROM mix_seed_tracks`
	if seed.kind == "Album" {
		prefix += fmt.Sprintf(" UNION SELECT $%d::text", itemParameter)
	}
	prefix += `
	), mix_seed_entities AS MATERIALIZED (
		SELECT DISTINCT credit.entity_id,credit.kind FROM mix_credits credit JOIN mix_seed_items seed ON seed.id=credit.item_id
	), mix_seed_albums AS MATERIALIZED (
		SELECT DISTINCT scope.album_id FROM mix_scope scope JOIN mix_seed_tracks seed ON seed.id=scope.id WHERE scope.album_id IS NOT NULL
	), mix_candidates AS MATERIALIZED (
		SELECT i.id FROM items i JOIN mix_scope scope ON scope.id=i.id WHERE scope.playable AND ` + filter + ` AND EXISTS(SELECT 1 FROM mix_seed_tracks)`
	if len(query.ExcludeArtistIds) != 0 {
		args = append(args, query.ExcludeArtistIds)
		prefix += fmt.Sprintf(` AND NOT EXISTS(SELECT 1 FROM mix_credits credit WHERE credit.item_id=i.id AND credit.kind='MusicArtist' AND credit.entity_id=ANY($%d::bigint[]))`, len(args))
	}
	prefix += `
	), mix_scores AS (
		SELECT candidate.id,
		CASE WHEN EXISTS(SELECT 1 FROM mix_seed_tracks seed WHERE seed.id=candidate.id) THEN 16 ELSE 0 END
		+ CASE WHEN EXISTS(SELECT 1 FROM mix_seed_albums album WHERE album.album_id=scope.album_id) THEN 8 ELSE 0 END
		+ COALESCE((SELECT sum(CASE credit.kind WHEN 'MusicArtist' THEN 4 WHEN 'Genre' THEN 2 ELSE 1 END)
			FROM mix_credits credit JOIN mix_seed_entities seed ON seed.entity_id=credit.entity_id AND seed.kind=credit.kind
			WHERE credit.item_id=candidate.id),0) AS score
		FROM mix_candidates candidate JOIN mix_scope scope ON scope.id=candidate.id
	) `
	return prefix, args
}

func discoveryProfileOriginalSearchSQL(query SearchHintsQuery, access libraryAccess) (string, []any) {
	_, sourceFilter, args := itemQuerySQL(Query{Recursive: true, MediaTypes: query.MediaTypes}, access, "")
	sourceFilter += " AND i.type<>'CollectionFolder'"
	args = append(args, query.SearchTerm, "%"+escapeLikeLiteral(query.SearchTerm)+"%", escapeLikeLiteral(query.SearchTerm)+"%")
	term, contains, starts := len(args)-2, len(args)-1, len(args)
	nameMatch := fmt.Sprintf("name ILIKE $%d ESCAPE E'\\\\'", contains)
	rank := fmt.Sprintf("CASE WHEN lower(name)=lower($%d::text) THEN 0 WHEN name ILIKE $%d ESCAPE E'\\\\' THEN 1 ELSE 2 END", term, starts)
	kinds := []string{}
	for _, entry := range []struct {
		kind string
		flag *bool
	}{
		{"Person", query.IncludePeople}, {"Genre", query.IncludeGenres},
		{"Studio", query.IncludeStudios}, {"MusicArtist", query.IncludeArtists},
	} {
		if searchHintEnabled(entry.flag) {
			kinds = append(kinds, entry.kind)
		}
	}
	args = append(args, kinds, searchHintEnabled(query.IncludeMedia))
	entityKinds, includeMedia := len(args)-1, len(args)
	prefix := "WITH eligible_sources AS (SELECT i.id,i.name,i.type FROM items i WHERE " + sourceFilter + "), " +
		"mixed_hints AS (SELECT 'Item'::text AS owner_kind,i.id,i.name,i.type FROM eligible_sources i " +
		fmt.Sprintf("WHERE $%d::boolean UNION ALL ", includeMedia) +
		"SELECT 'Entity'::text,entity.id::text,entity.name,entity.kind " +
		"FROM eligible_sources i JOIN item_entities association ON association.item_id=i.id " +
		"JOIN catalog_entities entity ON entity.id=association.entity_id WHERE " +
		fmt.Sprintf("entity.kind=ANY($%d::text[]) AND ", entityKinds) + validEntityAssociationSQL +
		" AND (entity.kind<>'MusicArtist' OR i.type IN ('Audio','MusicAlbum','MusicVideo')) " +
		"GROUP BY entity.id,entity.name,entity.kind), eligible_hints AS (SELECT owner_kind,id,name,type," + rank +
		" AS match_rank FROM mixed_hints WHERE " + nameMatch
	for _, field := range []struct {
		values []string
		negate bool
	}{
		{query.IncludeItemTypes, false}, {query.ExcludeItemTypes, true},
	} {
		if len(field.values) != 0 {
			args = append(args, field.values)
			condition := fmt.Sprintf("type=ANY($%d::text[])", len(args))
			if field.negate {
				condition = "NOT (" + condition + ")"
			}
			prefix += " AND " + condition
		}
	}
	for _, field := range []struct {
		kind  string
		value *bool
	}{{"Movie", query.IsMovie}, {"Series", query.IsSeries}} {
		if field.value != nil {
			args = append(args, *field.value)
			prefix += fmt.Sprintf(" AND (type='%s')=$%d::boolean", field.kind, len(args))
		}
	}
	return prefix + ") ", args
}
