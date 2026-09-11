package library

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// SimilarQuery keeps projection switches outside the catalog operation. A zero
// limit is an explicit request: music returns no rows, while Movie returns at
// most one. ExplicitSort distinguishes a requested ordering from Query's default.
type SimilarQuery struct {
	Query
	ExcludeArtistIds []int64
	ExplicitSort     bool
}

const similarOrdinaryCreditSQL = `(association.credit_group = 0 AND
	entity.kind IN ('Genre', 'Tag', 'Studio') AND association.credit_type = '')`

const similarArtistCreditSQL = `(entity.kind = 'MusicArtist' AND association.credit_group = 1 AND association.credit_type = 'Artist')`
const similarAlbumArtistCreditSQL = `(entity.kind = 'MusicArtist' AND association.credit_group = 2 AND association.credit_type = 'AlbumArtist')`

// QuerySimilar ranks the complete authorized candidate set inside one repeatable
// read transaction. The score is an explicit, adjustable Goby model fitted to
// the recorded auxiliary fixtures, not a claim about Emby's private algorithm:
// distinct shared Genre, Tag, and Studio entities score once; Person credits do
// not affect qualification or order. Artist and AlbumArtist roles each score
// their distinct matches against the seed's union; Audio siblings gain one
// point for the same nonempty physical album. Two points qualify. The recorded
// mixed-feature Movie cases support these boundaries without proving unique
// weights for unobserved combinations.
func (s *Store) QuerySimilar(ctx context.Context, seedID string, query SimilarQuery) (ItemResult, error) {
	if seedID == "" || len(seedID) > 256 || !utf8.ValidString(seedID) || strings.TrimSpace(seedID) != seedID ||
		strings.IndexFunc(seedID, unicode.IsControl) >= 0 || len(query.ExcludeArtistIds) > 1024 {
		return ItemResult{}, ErrInvalidInput
	}
	for _, id := range query.ExcludeArtistIds {
		if id <= 0 {
			return ItemResult{}, ErrInvalidInput
		}
	}
	limit := query.Limit
	if query.ParentID == "" {
		query.Recursive = true
	}
	var err error
	query.Query, err = normalizeItemQuery(query.Query)
	if err != nil {
		return ItemResult{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return ItemResult{}, err
	}
	defer rollback(tx)
	var seedType string
	err = tx.QueryRow(ctx, `SELECT i.type FROM items i WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+ordinaryItemSQL("i"),
		seedID, access.all, access.folders).Scan(&seedType)
	if errors.Is(err, pgx.ErrNoRows) {
		return ItemResult{}, similarMissingSeed(ctx, tx, seedID, access)
	}
	if err != nil {
		return ItemResult{}, fmt.Errorf("authorize similar seed: %w", err)
	}
	parentLibraryID, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return ItemResult{}, err
	}
	// An unsupported membership filter must not become a successful distant or
	// zero page. It remains explicit until that separate relationship is indexed.
	if len(query.ListItemIds) != 0 {
		return ItemResult{}, ErrUnsupportedFilter
	}
	if limit == 0 && seedType == "Movie" {
		limit = 1
	}
	query.Limit = limit
	prefix, args := similarRelationSQL(seedID, seedType, query, access, parentLibraryID)
	order := "ranked.score DESC, random(), i.id"
	if query.ExplicitSort {
		userParameter := 0
		if itemSortUsesUserData(query.SortBy) {
			args = append(args, query.UserID)
			userParameter = len(args)
		}
		order = itemOrderSQL(query.Query, userParameter)
	}
	args = append(args, query.Limit, query.StartIndex)
	// Rank and page IDs before expanding metadata/entity/album projections.
	// row_number evaluates the chosen ordering once, including random ties, so
	// the final projection retains that exact page order without rerandomizing.
	prefix += `, similar_page AS MATERIALIZED (
		SELECT i.id, ranked.score, row_number() OVER (ORDER BY ` + order + `) AS ordinal
		FROM items i JOIN similar_scores ranked ON ranked.id = i.id WHERE ranked.score >= 2
		ORDER BY ordinal` + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)) + ") "
	rows, err := tx.Query(ctx, prefix+"SELECT "+similarItemColumns(seedType)+` FROM items i
		JOIN similar_page ranked ON ranked.id = i.id ORDER BY ranked.ordinal`, args...)
	if err != nil {
		return ItemResult{}, fmt.Errorf("query similar items: %w", err)
	}
	defer rows.Close()
	result := ItemResult{Items: make([]Item, 0)}
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return ItemResult{}, fmt.Errorf("scan similar item: %w", err)
		}
		item.CanPlay = access.canPlay
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ItemResult{}, fmt.Errorf("read similar items: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, query.UserID, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete similar query: %w", err)
	}
	// The captured Similar endpoints report the returned page length, including
	// distant pages, independently of the HTTP total-count projection switch.
	result.TotalRecordCount = len(result.Items)
	return result, nil
}

func similarItemColumns(seedType string) string {
	if seedType != "Audio" && seedType != "MusicVideo" {
		// Same-type qualification proves this CASE branch is NULL. Removing its
		// unreachable recursive subplan also keeps ordinary Movie planning small.
		return strings.Replace(itemColumns, itemAlbumColumn, "NULL::jsonb", 1)
	}
	return itemColumns
}

func similarRelationSQL(seedID, seedType string, query SimilarQuery, access libraryAccess, parentLibraryID string) (string, []any) {
	music := seedType == "Audio" || seedType == "MusicVideo" || seedType == "MusicAlbum"
	// Defer the two album filters until the physical relationship and complete
	// effective album-artist source have each been calculated once per item.
	base := query.Query
	base.AlbumIds, base.AlbumArtistIds = nil, nil
	prefix, filter, args := itemQuerySQL(base, access, parentLibraryID)
	args = append(args, seedID, seedType)
	seedParameter, typeParameter := len(args)-1, len(args)
	filter += fmt.Sprintf(" AND i.id <> $%d::text AND i.type = $%d::text", seedParameter, typeParameter)
	if prefix == "" {
		prefix = "WITH "
	} else {
		prefix = strings.TrimSpace(prefix) + ", "
	}
	album := "NULL::text"
	if seedType == "MusicAlbum" {
		album = "CASE WHEN i.is_folder THEN i.id END"
	} else if music {
		album = physicalMusicAlbumIDSQL
	}
	prefix += fmt.Sprintf(`similar_scope_albums AS MATERIALIZED (
		SELECT i.id, i.type, `+album+` AS album_id FROM items i
		WHERE `+ordinaryItemSQL("i")+` AND (i.id = $%d::text OR (`+filter+`))
	), `, seedParameter)
	artistOwner := "NULL::text"
	if music {
		// Preserve the shared DTO/filter helper's exact own-group precedence.
		// Its recursive fallback now reads the materialized physical album ID.
		artistOwner = strings.ReplaceAll(effectiveMusicAlbumArtistItemSQL, physicalMusicAlbumIDSQL, "i.album_id")
	}
	prefix += `similar_scope AS MATERIALIZED (
		SELECT i.id, i.type, i.album_id, ` + artistOwner + ` AS album_artist_item_id FROM similar_scope_albums i
	), similar_credits AS MATERIALIZED (
		SELECT DISTINCT scope.id AS item_id, entity.id AS entity_id,
			CASE WHEN entity.kind = 'MusicArtist' THEN 'Artist' ELSE entity.kind END AS credit
		FROM similar_scope scope JOIN item_entities association ON association.item_id = scope.id
		JOIN catalog_entities entity ON entity.id = association.entity_id
		WHERE ` + similarOrdinaryCreditSQL + ` OR ` + similarArtistCreditSQL
	if music {
		prefix += ` UNION
		SELECT scope.id, entity.id, 'AlbumArtist' FROM similar_scope scope
		JOIN item_entities association ON association.item_id = scope.album_artist_item_id
		JOIN catalog_entities entity ON entity.id = association.entity_id WHERE ` + similarAlbumArtistCreditSQL
	}
	prefix += fmt.Sprintf(`
	), similar_seed AS MATERIALIZED (
		SELECT id, album_id FROM similar_scope WHERE id = $%d::text
	), similar_seed_entities AS MATERIALIZED (
		SELECT DISTINCT entity_id FROM similar_credits WHERE item_id = $%d::text
	), similar_candidates AS MATERIALIZED (
		SELECT i.id, i.type, i.album_id FROM similar_scope i WHERE i.id <> $%d::text`, seedParameter, seedParameter, seedParameter)
	if len(query.AlbumIds) != 0 {
		args = append(args, query.AlbumIds)
		prefix += fmt.Sprintf(" AND i.album_id = ANY($%d::text[])", len(args))
	}
	if len(query.AlbumArtistIds) != 0 {
		args = append(args, query.AlbumArtistIds)
		prefix += fmt.Sprintf(` AND EXISTS (SELECT 1 FROM similar_credits credit WHERE credit.item_id = i.id
			AND credit.credit = 'AlbumArtist' AND credit.entity_id = ANY($%d::bigint[]))`, len(args))
	}
	if len(query.ExcludeArtistIds) != 0 {
		args = append(args, append([]int64(nil), query.ExcludeArtistIds...))
		// These are own Artist and effective AlbumArtist roles only. No parent's
		// aggregate ArtistItems are imported into a track's credit population.
		prefix += fmt.Sprintf(` AND NOT EXISTS (SELECT 1 FROM similar_credits credit WHERE credit.item_id = i.id
			AND credit.credit IN ('Artist','AlbumArtist') AND credit.entity_id = ANY($%d::bigint[]))`, len(args))
	}
	prefix += `
	), similar_candidate_matches AS MATERIALIZED (
		SELECT credit.item_id, credit.entity_id, credit.credit FROM similar_credits credit
		JOIN similar_seed_entities seed_entity ON seed_entity.entity_id = credit.entity_id
	), similar_scores AS (
		SELECT candidate.id, count(DISTINCT (matched.entity_id, matched.credit)) FILTER (WHERE matched.entity_id IS NOT NULL)
			+ CASE WHEN candidate.type = 'Audio' AND NULLIF(seed.album_id, '') IS NOT NULL
				AND candidate.album_id = seed.album_id THEN 1 ELSE 0 END AS score
		FROM similar_candidates candidate CROSS JOIN similar_seed seed
		LEFT JOIN similar_candidate_matches matched ON matched.item_id = candidate.id
		GROUP BY candidate.id, candidate.type, candidate.album_id, seed.album_id
	)`
	return prefix, args
}

func similarMissingSeed(ctx context.Context, tx pgx.Tx, seedID string, access libraryAccess) error {
	id, err := strconv.ParseInt(seedID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != seedID {
		return ErrNotFound
	}
	var visible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog_entities entity
		JOIN item_entities association ON association.entity_id = entity.id JOIN items i ON i.id = association.item_id
		WHERE entity.id = $1 AND i.type <> 'CollectionFolder' AND `+validEntityAssociationSQL+`
		AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+ordinaryItemSQL("i")+`)`, id, access.all, access.folders).Scan(&visible)
	if err != nil {
		return fmt.Errorf("authorize similar entity seed: %w", err)
	}
	if visible {
		return ErrUnsupportedFilter
	}
	return ErrNotFound
}
