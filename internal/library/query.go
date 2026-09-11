package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

const itemMetadataColumn = `COALESCE((SELECT ms.effective FROM item_metadata_state ms WHERE ms.item_id = i.id), i.local_metadata)`

var itemColumns = `i.id, i.library_id, COALESCE(i.parent_id, ''), i.name,
	i.sort_name, i.type, i.path, i.overview, i.is_folder, i.index_number,
	i.parent_index_number, i.created_at, i.media,
	` + itemMetadataColumn + `, ` + itemEntitiesColumn + `,
	CASE WHEN i.type = 'MusicAlbum' AND i.is_folder THEN
		(SELECT count(*) FROM items child WHERE child.parent_id = i.id AND child.library_id = i.library_id AND ` + ordinaryItemSQL("child") + `)
	END, ` + itemAlbumColumn

var itemAlbumAncestorsSQL = `WITH RECURSIVE album_ancestors AS (
		SELECT parent.id, parent.parent_id, parent.name, parent.type, parent.is_folder,
			ARRAY[i.id, parent.id] AS visited, 1 AS depth
		FROM items parent WHERE parent.id = i.parent_id AND parent.library_id = i.library_id AND parent.id <> i.id AND ` + ordinaryItemSQL("parent") + `
		UNION ALL
		SELECT parent.id, parent.parent_id, parent.name, parent.type, parent.is_folder,
			ancestor.visited || parent.id, ancestor.depth + 1
		FROM album_ancestors ancestor JOIN items parent ON parent.id = ancestor.parent_id
		WHERE parent.library_id = i.library_id AND NOT (ancestor.type = 'MusicAlbum' AND ancestor.is_folder)
			AND NOT parent.id = ANY(ancestor.visited) AND ` + ordinaryItemSQL("parent") + `
	) `

var itemAlbumColumn = `CASE WHEN i.type IN ('Audio', 'MusicVideo') THEN (` + itemAlbumAncestorsSQL + `
	SELECT jsonb_build_object('ID', id, 'Name', name, 'AlbumArtists', (
		SELECT COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id, 'Name', association.display_name)
			ORDER BY association.position, entity.id), '[]'::jsonb)
		FROM item_entities association JOIN catalog_entities entity ON entity.id = association.entity_id
		WHERE association.item_id = album_ancestors.id AND entity.kind = 'MusicArtist'
			AND association.credit_group = 2 AND association.credit_type = 'AlbumArtist'
	)) FROM album_ancestors
	WHERE type = 'MusicAlbum' AND is_folder ORDER BY depth LIMIT 1
) END`

const itemEntitiesColumn = `(SELECT jsonb_build_object(
	'Genres', COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id, 'Name', association.display_name)
		ORDER BY association.position, entity.id) FILTER (WHERE entity.kind = 'Genre'), '[]'::jsonb),
	'Tags', COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id, 'Name', association.display_name)
		ORDER BY lower(association.display_name), entity.id, association.position) FILTER (WHERE entity.kind = 'Tag'), '[]'::jsonb),
	'Studios', COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id, 'Name', association.display_name)
		ORDER BY association.position, entity.id) FILTER (WHERE entity.kind = 'Studio'), '[]'::jsonb),
	'Artists', COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id, 'Name', association.display_name)
		ORDER BY association.position, entity.id) FILTER (WHERE entity.kind = 'MusicArtist' AND association.credit_group = 1 AND association.credit_type = 'Artist'), '[]'::jsonb),
	'AlbumArtists', COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id, 'Name', association.display_name)
		ORDER BY association.position, entity.id) FILTER (WHERE entity.kind = 'MusicArtist' AND association.credit_group = 2 AND association.credit_type = 'AlbumArtist'), '[]'::jsonb),
	'People', COALESCE(jsonb_agg(jsonb_build_object('ID', entity.id::text, 'Name', association.display_name,
		'Role', association.role, 'Type', association.credit_type, 'SortOrder', association.sort_order)
		ORDER BY association.sort_order ASC NULLS LAST, association.position, entity.id) FILTER (WHERE entity.kind = 'Person'), '[]'::jsonb)
	) FROM item_entities association JOIN catalog_entities entity ON entity.id = association.entity_id
	WHERE association.item_id = i.id)`

const libraryColumns = `l.id, l.name, l.collection_type,
	COALESCE((SELECT array_agg(r.path ORDER BY r.path) FROM library_roots r
		WHERE r.library_id = l.id), '{}'::text[]), l.created_at, l.last_scan_at`

type libraryAccess struct {
	all     bool
	folders []string
	canPlay bool
}

// QueryItems applies the current user policy before counting or paging items.
func (s *Store) QueryItems(ctx context.Context, query Query) (ItemResult, error) {
	return s.queryItems(ctx, query, query.Resumable && strings.TrimSpace(query.SortBy) == "")
}

func (s *Store) queryItems(ctx context.Context, query Query, resumeOrder bool) (ItemResult, error) {
	query, err := normalizeItemQuery(query)
	if err != nil {
		return ItemResult{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return ItemResult{}, err
	}
	defer tx.Rollback(ctx)

	parentLibraryID, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return ItemResult{}, err
	}

	prefix, filter, args := itemQuerySQL(query, access, parentLibraryID)
	result := ItemResult{Items: make([]Item, 0)}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM items i WHERE "+filter,
		args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, fmt.Errorf("count library items: %w", err)
	}
	// Membership is not indexed yet. An empty authorized candidate set proves
	// that its intersection is empty; any nonempty set remains undetermined.
	// Count before pagination so Limit=0 or a distant page cannot bypass this.
	if len(query.ListItemIds) != 0 && result.TotalRecordCount != 0 {
		return ItemResult{}, ErrUnsupportedFilter
	}
	userOrderParameter := 0
	if itemSortUsesUserData(query.SortBy) {
		args = append(args, query.UserID)
		userOrderParameter = len(args)
	}
	order := itemOrderSQL(query, userOrderParameter)
	if resumeOrder {
		args = append(args, query.UserID)
		order = fmt.Sprintf(`(SELECT user_data.last_played_at FROM user_item_data user_data
			WHERE user_data.user_id = $%d::text AND user_data.item_id = i.id) DESC NULLS LAST, i.id ASC`, len(args))
	}
	args = append(args, query.Limit, query.StartIndex)
	statement := prefix + "SELECT " + itemColumns + " FROM items i WHERE " + filter +
		" ORDER BY " + order + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return ItemResult{}, fmt.Errorf("query library items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return ItemResult{}, fmt.Errorf("scan library item: %w", err)
		}
		item.CanPlay = access.canPlay
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ItemResult{}, fmt.Errorf("read library items: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, query.UserID, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := attachSubtitles(ctx, tx, result.Items); err != nil {
		return ItemResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete item query: %w", err)
	}
	return result, nil
}

// GetItem returns the same error for missing and unauthorized item identifiers.
func (s *Store) GetItem(ctx context.Context, userID, id string) (Item, error) {
	return s.GetItemFor(ctx, Subject{UserID: userID}, id)
}

// GetItemFor reads a catalog item using the credential's independent authority.
func (s *Store) GetItemFor(ctx context.Context, subject Subject, id string) (Item, error) {
	if strings.TrimSpace(id) == "" || strings.ContainsRune(id, '\x00') {
		return Item{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	item, err := scanItem(tx.QueryRow(ctx, "SELECT "+itemColumns+` FROM items i
		WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+directItemSQL("i"),
		id, access.all, access.folders))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("read library item: %w", err)
	}
	item.CanPlay = access.canPlay
	items := []Item{item}
	if err := attachThemeItemAttributes(ctx, tx, items); err != nil {
		return Item{}, err
	}
	if err := attachUserData(ctx, tx, subject.UserID, items); err != nil {
		return Item{}, err
	}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("complete item read: %w", err)
	}
	return items[0], nil
}

// GetItemsByIDFor projects only these explicitly supplied IDs in one current
// authorization snapshot. Missing or inaccessible IDs are omitted. This is an
// internal direct-read path for playback projections, not a browse switch.
func (s *Store) GetItemsByIDFor(ctx context.Context, subject Subject, ids []string) ([]Item, error) {
	if len(ids) > 1000 {
		return nil, ErrInvalidInput
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || len(id) > 256 || !utf8.ValidString(id) || strings.ContainsRune(id, '\x00') || seen[id] {
			return nil, ErrInvalidInput
		}
		seen[id] = true
	}
	ids = append([]string{}, ids...)
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.Query(ctx, "SELECT "+itemColumns+` FROM items i
		WHERE i.id = ANY($1::text[]) AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+directItemSQL("i")+`
		ORDER BY array_position($1::text[], i.id)`, ids, access.all, access.folders)
	if err != nil {
		return nil, fmt.Errorf("query direct item identities: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0, len(ids))
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan direct item: %w", err)
		}
		item.CanPlay = access.canPlay
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read direct item identities: %w", err)
	}
	rows.Close()
	if err := attachThemeItemAttributes(ctx, tx, items); err != nil {
		return nil, err
	}
	if err := attachUserData(ctx, tx, subject.UserID, items); err != nil {
		return nil, err
	}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete direct item identities: %w", err)
	}
	return items, nil
}

// ListUserLibraries returns only the libraries granted by the user's policy.
func (s *Store) ListUserLibraries(ctx context.Context, userID string) ([]Library, error) {
	return s.ListUserLibrariesFor(ctx, Subject{UserID: userID})
}

// ListUserLibrariesFor applies target library policy when a user is selected.
func (s *Store) ListUserLibrariesFor(ctx context.Context, subject Subject) ([]Library, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, "SELECT "+libraryColumns+` FROM libraries l
		WHERE ($1::boolean OR l.id = ANY($2::text[])) ORDER BY lower(l.name), l.id`,
		access.all, access.folders)
	if err != nil {
		return nil, fmt.Errorf("query user libraries: %w", err)
	}
	defer rows.Close()
	libraries := make([]Library, 0)
	for rows.Next() {
		library, err := scanLibrary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user library: %w", err)
		}
		libraries = append(libraries, library)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read user libraries: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete user library query: %w", err)
	}
	return libraries, nil
}

// A stable snapshot keeps policy checks, total counts, and page contents aligned.
func (s *Store) beginUserRead(ctx context.Context, userID string) (pgx.Tx, libraryAccess, error) {
	if strings.TrimSpace(userID) == "" || strings.ContainsRune(userID, '\x00') {
		return nil, libraryAccess{}, ErrInvalidInput
	}
	if s == nil || s.pool == nil {
		return nil, libraryAccess{}, ErrUnavailable
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, libraryAccess{}, fmt.Errorf("begin authorized library read: %w", err)
	}
	var administrator, disabled bool
	var policy []byte
	err = tx.QueryRow(ctx, `SELECT is_administrator, is_disabled, policy
		FROM users WHERE id = $1`, userID).Scan(&administrator, &disabled, &policy)
	if err != nil {
		tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, libraryAccess{}, ErrForbidden
		}
		return nil, libraryAccess{}, fmt.Errorf("read library user policy: %w", err)
	}
	if disabled {
		tx.Rollback(ctx)
		return nil, libraryAccess{}, ErrForbidden
	}
	canPlay := playbackAllowed(policy)
	if administrator {
		return tx, libraryAccess{all: true, folders: []string{}, canPlay: canPlay}, nil
	}
	access, err := parseLibraryPolicy(policy)
	if err != nil {
		tx.Rollback(ctx)
		return nil, libraryAccess{}, err
	}
	access.canPlay = canPlay
	return tx, access, nil
}

func readQueryParent(ctx context.Context, tx pgx.Tx, parentID string, access libraryAccess) (string, error) {
	return readVisibleQueryParent(ctx, tx, parentID, access, directItemSQL("i"))
}

func readOrdinaryQueryParent(ctx context.Context, tx pgx.Tx, parentID string, access libraryAccess) (string, error) {
	return readVisibleQueryParent(ctx, tx, parentID, access, ordinaryItemSQL("i"))
}

func readVisibleQueryParent(ctx context.Context, tx pgx.Tx, parentID string, access libraryAccess, visibility string) (string, error) {
	if parentID == "" {
		return "", nil
	}
	var libraryID string
	err := tx.QueryRow(ctx, `SELECT i.library_id FROM items i
		WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[])) AND `+visibility,
		parentID, access.all, access.folders).Scan(&libraryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read query parent: %w", err)
	}
	return libraryID, nil
}

func parseLibraryPolicy(data []byte) (libraryAccess, error) {
	var policy map[string]json.RawMessage
	if err := json.Unmarshal(data, &policy); err != nil || policy == nil {
		return libraryAccess{}, ErrForbidden
	}
	access := libraryAccess{all: true, folders: []string{}}
	if raw, ok := policy["EnableAllFolders"]; ok {
		var all *bool
		if err := json.Unmarshal(raw, &all); err != nil || all == nil {
			return libraryAccess{}, ErrForbidden
		}
		access.all = *all
	}
	if access.all {
		return access, nil
	}
	if raw, ok := policy["EnabledFolders"]; ok {
		if err := json.Unmarshal(raw, &access.folders); err != nil {
			return libraryAccess{}, ErrForbidden
		}
	}
	if access.folders == nil {
		access.folders = []string{}
	}
	return access, nil
}

func normalizeItemQuery(query Query) (Query, error) {
	if !validSubject(Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID}) || query.StartIndex < 0 || query.Limit < 0 || query.Limit > 1000 {
		return Query{}, ErrInvalidInput
	}
	if query.UserID == "" && (query.IsPlayed != nil || query.IsFavorite != nil || query.Resumable) {
		return Query{}, ErrInvalidInput
	}
	if query.ParentIndexNumber != nil && (*query.ParentIndexNumber < 0 || *query.ParentIndexNumber > 1<<31-1) {
		return Query{}, ErrInvalidInput
	}
	for _, value := range []string{query.UserID, query.ParentID, query.SearchTerm} {
		if strings.ContainsRune(value, '\x00') {
			return Query{}, ErrInvalidInput
		}
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	var err error
	query, err = normalizeItemSort(query)
	if err != nil {
		return Query{}, err
	}
	// Recognizing a catalog kind for filtering does not add its creation or
	// management workflow. Absent kinds still run the same authorized SQL query.
	types, err := normalizeQueryValues(query.IncludeItemTypes, map[string]string{
		"collectionfolder": "CollectionFolder", "folder": "Folder", "movie": "Movie",
		"series": "Series", "season": "Season", "episode": "Episode", "video": "Video",
		"audio": "Audio", "musicalbum": "MusicAlbum", "musicartist": "MusicArtist",
		"playlist": "Playlist", "boxset": "BoxSet", "musicvideo": "MusicVideo",
	})
	if err != nil {
		return Query{}, err
	}
	query.IncludeItemTypes = types
	query.MediaTypes, err = normalizeQueryValues(query.MediaTypes, map[string]string{
		"audio": "Audio", "video": "Video",
	})
	if err != nil {
		return Query{}, err
	}
	query.Ids = append([]string(nil), query.Ids...)
	for _, id := range query.Ids {
		if strings.TrimSpace(id) == "" || strings.ContainsRune(id, '\x00') {
			return Query{}, ErrInvalidInput
		}
	}
	query, err = normalizeEntityFilters(query)
	if err != nil {
		return Query{}, err
	}
	return normalizeMusicFilters(query)
}

func normalizeEntityFilters(query Query) (Query, error) {
	for _, values := range [][]int64{query.GenreIds, query.TagIds, query.StudioIds} {
		if len(values) > 1024 {
			return Query{}, ErrInvalidInput
		}
		for _, id := range values {
			if id <= 0 {
				return Query{}, ErrInvalidInput
			}
		}
	}
	for _, values := range [][]string{query.Genres, query.Tags, query.Studios, query.PersonTypes} {
		if len(values) > 1024 {
			return Query{}, ErrInvalidInput
		}
		for _, name := range values {
			if !validEntityFilterName(name) {
				return Query{}, ErrInvalidInput
			}
		}
	}
	if query.Person != "" && !validEntityFilterName(query.Person) {
		return Query{}, ErrInvalidInput
	}
	if len(query.PersonIds) > 1024 {
		return Query{}, ErrInvalidInput
	}
	for _, value := range query.PersonIds {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
			return Query{}, ErrInvalidInput
		}
	}
	// Keep names intact so filtering uses the same PostgreSQL normalization as
	// the entity registry, including its treatment of non-ASCII whitespace.
	query.GenreIds = append([]int64(nil), query.GenreIds...)
	query.TagIds = append([]int64(nil), query.TagIds...)
	query.StudioIds = append([]int64(nil), query.StudioIds...)
	query.PersonIds = append([]string(nil), query.PersonIds...)
	query.Genres = append([]string(nil), query.Genres...)
	query.Tags = append([]string(nil), query.Tags...)
	query.Studios = append([]string(nil), query.Studios...)
	query.PersonTypes = append([]string(nil), query.PersonTypes...)
	return query, nil
}

func validEntityFilterName(name string) bool {
	return len(name) <= 65536 && utf8.ValidString(name) && strings.TrimSpace(name) != "" && !strings.ContainsRune(name, '\x00')
}

func normalizeQueryValues(values []string, allowed map[string]string) ([]string, error) {
	normalized := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		canonical, ok := allowed[strings.ToLower(strings.TrimSpace(value))]
		if !ok {
			return nil, ErrInvalidInput
		}
		if !seen[canonical] {
			normalized = append(normalized, canonical)
			seen[canonical] = true
		}
	}
	return normalized, nil
}

func itemQuerySQL(query Query, access libraryAccess, parentLibraryID string) (string, string, []any) {
	args := []any{access.all, access.folders}
	conditions := []string{"($1::boolean OR i.library_id = ANY($2::text[]))", ordinaryItemSQL("i")}
	prefix := ""
	if query.ParentID != "" {
		args = append(args, query.ParentID, parentLibraryID)
		if query.Recursive {
			// UNION bounds traversal even if corrupt rows contain a parent cycle.
			prefix = `WITH RECURSIVE descendants AS (
				SELECT child.id, child.library_id FROM items child
				WHERE child.parent_id = $3 AND child.library_id = $4 AND ` + ordinaryItemSQL("child") + `
				UNION
				SELECT child.id, child.library_id FROM items child
				JOIN descendants parent ON child.parent_id = parent.id
					AND child.library_id = parent.library_id
				WHERE ` + ordinaryItemSQL("child") + `
			) `
			conditions = append(conditions, "i.id IN (SELECT id FROM descendants)", "i.id <> $3")
		} else {
			conditions = append(conditions, "i.parent_id = $3", "i.library_id = $4")
		}
	} else if len(query.Ids) == 0 {
		if query.Recursive {
			conditions = append(conditions, "i.type <> 'CollectionFolder'")
		} else if !hasEntityFilters(query) {
			conditions = append(conditions, "i.parent_id IS NULL", "i.type = 'CollectionFolder'", "i.id = i.library_id")
		}
	}
	if len(query.IncludeItemTypes) != 0 {
		args = append(args, query.IncludeItemTypes)
		conditions = append(conditions, fmt.Sprintf("i.type = ANY($%d::text[])", len(args)))
	}
	// Classify numbered seasons and episodes, not every item with a default
	// zero index. Comparing the entire predicate preserves other item types
	// when a caller explicitly excludes specials from a mixed item query.
	for _, flag := range []struct {
		value     *bool
		predicate string
	}{
		{query.IsFolder, "i.is_folder"},
		{query.IsSpecialSeason, "(i.type = 'Season' AND i.index_number = 0)"},
		{query.IsSpecialEpisode, "(i.type = 'Episode' AND i.parent_index_number = 0)"},
	} {
		if flag.value != nil {
			args = append(args, *flag.value)
			conditions = append(conditions, fmt.Sprintf("%s = $%d::boolean", flag.predicate, len(args)))
		}
	}
	if query.ParentIndexNumber != nil {
		args = append(args, *query.ParentIndexNumber)
		conditions = append(conditions, fmt.Sprintf("i.parent_index_number = $%d::integer", len(args)))
	}
	if len(query.Ids) != 0 {
		args = append(args, query.Ids)
		conditions = append(conditions, fmt.Sprintf("i.id = ANY($%d::text[])", len(args)))
	}
	if len(query.MediaTypes) != 0 {
		args = append(args, query.MediaTypes)
		conditions = append(conditions, fmt.Sprintf(`(CASE WHEN i.type = 'Audio' THEN 'Audio'
			WHEN i.type IN ('Movie', 'Episode', 'Video') THEN 'Video' ELSE '' END) = ANY($%d::text[])`, len(args)))
	}
	if query.SearchTerm != "" {
		args = append(args, "%"+escapeLikeLiteral(query.SearchTerm)+"%")
		conditions = append(conditions, fmt.Sprintf("i.name ILIKE $%d ESCAPE E'\\\\'", len(args)))
	}
	conditions, args = addEntityConditions(query, conditions, args)
	conditions, args = addMusicConditions(query, conditions, args)
	conditions, args = addUserDataConditions(query, conditions, args)
	return prefix, strings.Join(conditions, " AND "), args
}

func hasEntityFilters(query Query) bool {
	return len(query.GenreIds)+len(query.TagIds)+len(query.StudioIds)+len(query.PersonIds)+
		len(query.Genres)+len(query.Tags)+len(query.Studios)+len(query.PersonTypes)+
		len(query.ArtistIds)+len(query.AlbumArtistIds)+len(query.AlbumIds) != 0 || query.Person != ""
}

func addEntityConditions(query Query, conditions []string, args []any) ([]string, []any) {
	for _, facet := range []struct {
		kind  string
		ids   []int64
		names []string
	}{
		{kind: "Genre", ids: query.GenreIds, names: query.Genres},
		{kind: "Tag", ids: query.TagIds, names: query.Tags},
		{kind: "Studio", ids: query.StudioIds, names: query.Studios},
	} {
		matches := make([]string, 0, 2)
		if len(facet.ids) != 0 {
			args = append(args, facet.ids)
			matches = append(matches, fmt.Sprintf("entity.id = ANY($%d::bigint[])", len(args)))
		}
		if len(facet.names) != 0 {
			args = append(args, facet.names)
			matches = append(matches, entityNameMatch("entity.normalized_name", len(args)))
		}
		for _, match := range matches {
			conditions = append(conditions, `EXISTS (SELECT 1 FROM item_entities association
				JOIN catalog_entities entity ON entity.id = association.entity_id
				WHERE association.item_id = i.id AND entity.kind = '`+facet.kind+`'
				AND (`+match+"))")
		}
	}
	if query.Person == "" && len(query.PersonIds) == 0 && len(query.PersonTypes) == 0 {
		return conditions, args
	}
	personConditions := []string{"association.item_id = i.id", "entity.kind = 'Person'"}
	if query.Person != "" {
		args = append(args, query.Person)
		personConditions = append(personConditions, fmt.Sprintf("entity.normalized_name = lower(btrim($%d::text))", len(args)))
	}
	if len(query.PersonIds) != 0 {
		args = append(args, query.PersonIds)
		personConditions = append(personConditions, fmt.Sprintf("entity.id::text = ANY($%d::text[])", len(args)))
	}
	if len(query.PersonTypes) != 0 {
		args = append(args, query.PersonTypes)
		personConditions = append(personConditions, entityNameMatch("lower(btrim(association.credit_type))", len(args)))
	}
	conditions = append(conditions, `EXISTS (SELECT 1 FROM item_entities association
		JOIN catalog_entities entity ON entity.id = association.entity_id
		WHERE `+strings.Join(personConditions, " AND ")+")")
	return conditions, args
}

func entityNameMatch(column string, parameter int) string {
	return fmt.Sprintf("%s IN (SELECT lower(btrim(value)) FROM unnest($%d::text[]) AS names(value))", column, parameter)
}

func escapeLikeLiteral(value string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value)
}

func scanItem(row rowScanner, additional ...any) (Item, error) {
	var item Item
	var encoded, encodedMetadata, encodedEntities, encodedAlbum []byte
	destinations := []any{&item.ID, &item.LibraryID, &item.ParentID, &item.Name, &item.SortName,
		&item.Type, &item.Path, &item.Overview, &item.IsFolder, &item.IndexNumber,
		&item.ParentIndexNumber, &item.CreatedAt, &encoded, &encodedMetadata, &encodedEntities, &item.ChildCount, &encodedAlbum}
	err := row.Scan(append(destinations, additional...)...)
	if err != nil {
		return Item{}, err
	}
	if len(encoded) != 0 && string(encoded) != "null" {
		item.Media = &media.Info{}
		if err := json.Unmarshal(encoded, item.Media); err != nil {
			return Item{}, fmt.Errorf("decode item media: %w", err)
		}
	}
	if len(encodedMetadata) != 0 {
		if err := json.Unmarshal(encodedMetadata, &item.Metadata); err != nil {
			return Item{}, fmt.Errorf("decode item local metadata: %w", err)
		}
	}
	if err := json.Unmarshal(encodedEntities, &item.Entities); err != nil {
		return Item{}, fmt.Errorf("decode item entities: %w", err)
	}
	if len(encodedAlbum) != 0 {
		if err := json.Unmarshal(encodedAlbum, &item.Album); err != nil {
			return Item{}, fmt.Errorf("decode item album: %w", err)
		}
	}
	return item, nil
}

func scanLibrary(row rowScanner) (Library, error) {
	var library Library
	err := row.Scan(&library.ID, &library.Name, &library.CollectionType, &library.Paths,
		&library.CreatedAt, &library.LastScanAt)
	return library, err
}
