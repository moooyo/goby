package library

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// QuerySimilarArtists uses Goby's local metadata model: an artist's profile is
// the distinct Genre, Tag, and Studio entities on authorized music credited to
// its own Artist or effective AlbumArtist role. Each shared feature scores once;
// at least one shared feature is required. This does not infer provider or
// popularity relationships, and an empty profile has no unrelated fallback.
func (s *Store) QuerySimilarArtists(ctx context.Context, seedID string, query SimilarQuery) (EntityResult, error) {
	seed, err := strconv.ParseInt(seedID, 10, 64)
	if err != nil || seed <= 0 || strconv.FormatInt(seed, 10) != seedID || len(query.ExcludeArtistIds) > 1024 {
		return EntityResult{}, ErrInvalidInput
	}
	for _, id := range query.ExcludeArtistIds {
		if id <= 0 {
			return EntityResult{}, ErrInvalidInput
		}
	}
	query.ExcludeArtistIds = append([]int64(nil), query.ExcludeArtistIds...)
	limit := query.Limit
	query.Query, err = normalizeMusicEntityQuery(query.Query)
	if err != nil {
		return EntityResult{}, err
	}
	// Similar preserves an explicit zero limit instead of the list default.
	query.Limit = limit
	subject := Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return EntityResult{}, err
	}
	defer rollback(tx)
	// Candidate filters must never narrow seed authorization. This baseline
	// membership also excludes orphan entities and stray nonmusic credits.
	seedPrefix, seedArgs, err := musicEntityQuerySQL(Query{
		UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID,
	}, access, "", "allartists")
	if err != nil {
		return EntityResult{}, err
	}
	seedArgs = append(seedArgs, seed)
	var visible bool
	if err := tx.QueryRow(ctx, seedPrefix+fmt.Sprintf(
		"SELECT EXISTS (SELECT 1 FROM eligible_entities WHERE id = $%d::bigint)", len(seedArgs)), seedArgs...).Scan(&visible); err != nil {
		return EntityResult{}, fmt.Errorf("authorize similar artist seed: %w", err)
	}
	if !visible {
		return EntityResult{}, ErrNotFound
	}
	parent, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return EntityResult{}, err
	}
	prefix, args, err := similarArtistRelationSQL(seed, query, access, parent)
	if err != nil {
		return EntityResult{}, err
	}
	order := "ranked.score DESC, lower(entity.name), entity.id"
	if query.ExplicitSort {
		order = "lower(entity.name) " + query.SortOrder + ", entity.id " + query.SortOrder
	}
	filter := "ranked.score >= 1"
	if len(query.ExcludeArtistIds) != 0 {
		args = append(args, query.ExcludeArtistIds)
		filter += fmt.Sprintf(" AND NOT (entity.id = ANY($%d::bigint[]))", len(args))
	}
	args = append(args, query.Limit, query.StartIndex)
	rows, err := tx.Query(ctx, prefix+`SELECT entity.id, entity.name, entity.kind, entity.item_count
		FROM eligible_entities entity JOIN similar_artist_scores ranked ON ranked.id = entity.id
		WHERE `+filter+` ORDER BY `+order+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return EntityResult{}, fmt.Errorf("query similar artists: %w", err)
	}
	result := EntityResult{Items: []Entity{}}
	for rows.Next() {
		entity, err := scanEntity(rows)
		if err != nil {
			rows.Close()
			return EntityResult{}, fmt.Errorf("scan similar artist: %w", err)
		}
		result.Items = append(result.Items, entity)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EntityResult{}, fmt.Errorf("read similar artists: %w", err)
	}
	if err := populateEntityProjections(ctx, tx, subject, access, result.Items); err != nil {
		return EntityResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return EntityResult{}, fmt.Errorf("complete similar artist query: %w", err)
	}
	result.TotalRecordCount = len(result.Items)
	return result, nil
}

func similarArtistRelationSQL(seedID int64, query SimilarQuery, access libraryAccess, parentLibraryID string) (string, []any, error) {
	prefix, args, err := musicEntityQuerySQL(query.Query, access, parentLibraryID, "allartists")
	if err != nil {
		return "", nil, err
	}
	args = append(args, seedID)
	seedParameter := len(args)
	// Profiles use all authorized music, independently of the filters that
	// select candidate entities. Scope nested album traversal to the same ACL.
	prefix = strings.TrimSpace(prefix) + `, similar_artist_scope AS MATERIALIZED (
		SELECT i.id, ` + access.scopeSQL(effectiveMusicAlbumArtistItemSQL) + ` AS album_artist_item_id
		FROM items i WHERE i.type IN ('Audio', 'MusicAlbum', 'MusicVideo')
		AND ($1::boolean OR i.library_id = ANY($2::text[])) AND ` + access.ordinarySQL("i") + `
	), similar_artist_credits AS MATERIALIZED (
		SELECT scope.id AS item_id, entity.id AS artist_id FROM similar_artist_scope scope
		JOIN item_entities association ON association.item_id = scope.id
		JOIN catalog_entities entity ON entity.id = association.entity_id WHERE ` + similarArtistCreditSQL + `
		UNION
		SELECT scope.id, entity.id FROM similar_artist_scope scope
		JOIN item_entities association ON association.item_id = scope.album_artist_item_id
		JOIN catalog_entities entity ON entity.id = association.entity_id WHERE ` + similarAlbumArtistCreditSQL + `
	), similar_artist_features AS MATERIALIZED (
		SELECT DISTINCT credit.artist_id, entity.id AS feature_id FROM similar_artist_credits credit
		JOIN item_entities association ON association.item_id = credit.item_id
		JOIN catalog_entities entity ON entity.id = association.entity_id WHERE ` + similarOrdinaryCreditSQL + `
	), similar_artist_scores AS (
		SELECT candidate.artist_id AS id, count(*) AS score FROM similar_artist_features candidate
		JOIN similar_artist_features seed ON seed.feature_id = candidate.feature_id
		WHERE seed.artist_id = $` + strconv.Itoa(seedParameter) + `::bigint
		AND candidate.artist_id <> seed.artist_id GROUP BY candidate.artist_id
	) `
	return prefix, args, nil
}
