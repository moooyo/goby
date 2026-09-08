package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

const itemColumns = `i.id, i.library_id, COALESCE(i.parent_id, ''), i.name,
	i.sort_name, i.type, i.path, i.overview, i.is_folder, i.index_number,
	i.parent_index_number, i.created_at, i.media, i.local_metadata`

const libraryColumns = `l.id, l.name, l.collection_type,
	COALESCE((SELECT array_agg(r.path ORDER BY r.path) FROM library_roots r
		WHERE r.library_id = l.id), '{}'::text[]), l.created_at, l.last_scan_at`

type libraryAccess struct {
	all     bool
	folders []string
}

// QueryItems applies the current user policy before counting or paging items.
func (s *Store) QueryItems(ctx context.Context, query Query) (ItemResult, error) {
	query, err := normalizeItemQuery(query)
	if err != nil {
		return ItemResult{}, err
	}
	tx, access, err := s.beginUserRead(ctx, query.UserID)
	if err != nil {
		return ItemResult{}, err
	}
	defer tx.Rollback(ctx)

	parentLibraryID, err := readQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return ItemResult{}, err
	}

	prefix, filter, args := itemQuerySQL(query, access, parentLibraryID)
	result := ItemResult{Items: make([]Item, 0)}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM items i WHERE "+filter,
		args...).Scan(&result.TotalRecordCount); err != nil {
		return ItemResult{}, fmt.Errorf("count library items: %w", err)
	}
	args = append(args, query.Limit, query.StartIndex)
	statement := prefix + "SELECT " + itemColumns + " FROM items i WHERE " + filter +
		" ORDER BY " + itemOrderSQL(query) + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
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
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return ItemResult{}, fmt.Errorf("read library items: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ItemResult{}, fmt.Errorf("complete item query: %w", err)
	}
	return result, nil
}

// GetItem returns the same error for missing and unauthorized item identifiers.
func (s *Store) GetItem(ctx context.Context, userID, id string) (Item, error) {
	if strings.TrimSpace(id) == "" || strings.ContainsRune(id, '\x00') {
		return Item{}, ErrInvalidInput
	}
	tx, access, err := s.beginUserRead(ctx, userID)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	item, err := scanItem(tx.QueryRow(ctx, "SELECT "+itemColumns+` FROM items i
		WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[]))`,
		id, access.all, access.folders))
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("read library item: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, fmt.Errorf("complete item read: %w", err)
	}
	return item, nil
}

// ListUserLibraries returns only the libraries granted by the user's policy.
func (s *Store) ListUserLibraries(ctx context.Context, userID string) ([]Library, error) {
	tx, access, err := s.beginUserRead(ctx, userID)
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
	if administrator {
		return tx, libraryAccess{all: true, folders: []string{}}, nil
	}
	access, err := parseLibraryPolicy(policy)
	if err != nil {
		tx.Rollback(ctx)
		return nil, libraryAccess{}, err
	}
	return tx, access, nil
}

func readQueryParent(ctx context.Context, tx pgx.Tx, parentID string, access libraryAccess) (string, error) {
	if parentID == "" {
		return "", nil
	}
	var libraryID string
	err := tx.QueryRow(ctx, `SELECT i.library_id FROM items i
		WHERE i.id = $1 AND ($2::boolean OR i.library_id = ANY($3::text[]))`,
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
	if strings.TrimSpace(query.UserID) == "" || query.StartIndex < 0 || query.Limit < 0 || query.Limit > 1000 {
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
	switch strings.ToLower(strings.TrimSpace(query.SortBy)) {
	case "", "sortname":
		query.SortBy = "SortName"
	case "name":
		query.SortBy = "Name"
	case "datecreated":
		query.SortBy = "DateCreated"
	case "indexnumber":
		query.SortBy = "IndexNumber"
	default:
		return Query{}, ErrInvalidInput
	}
	switch strings.ToLower(strings.TrimSpace(query.SortOrder)) {
	case "", "ascending", "asc":
		query.SortOrder = "ASC"
	case "descending", "desc":
		query.SortOrder = "DESC"
	default:
		return Query{}, ErrInvalidInput
	}
	types, err := normalizeQueryValues(query.IncludeItemTypes, map[string]string{
		"collectionfolder": "CollectionFolder", "folder": "Folder", "movie": "Movie",
		"series": "Series", "season": "Season", "episode": "Episode", "video": "Video",
		"audio": "Audio", "musicalbum": "MusicAlbum", "musicartist": "MusicArtist",
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
	return query, nil
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
	conditions := []string{"($1::boolean OR i.library_id = ANY($2::text[]))"}
	prefix := ""
	if query.ParentID != "" {
		args = append(args, query.ParentID, parentLibraryID)
		if query.Recursive {
			// UNION bounds traversal even if corrupt rows contain a parent cycle.
			prefix = `WITH RECURSIVE descendants AS (
				SELECT child.id, child.library_id FROM items child
				WHERE child.parent_id = $3 AND child.library_id = $4
				UNION
				SELECT child.id, child.library_id FROM items child
				JOIN descendants parent ON child.parent_id = parent.id
					AND child.library_id = parent.library_id
			) `
			conditions = append(conditions, "i.id IN (SELECT id FROM descendants)", "i.id <> $3")
		} else {
			conditions = append(conditions, "i.parent_id = $3", "i.library_id = $4")
		}
	} else if len(query.Ids) == 0 {
		if query.Recursive {
			conditions = append(conditions, "i.type <> 'CollectionFolder'")
		} else {
			conditions = append(conditions, "i.parent_id IS NULL", "i.type = 'CollectionFolder'", "i.id = i.library_id")
		}
	}
	if len(query.IncludeItemTypes) != 0 {
		args = append(args, query.IncludeItemTypes)
		conditions = append(conditions, fmt.Sprintf("i.type = ANY($%d::text[])", len(args)))
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
	return prefix, strings.Join(conditions, " AND "), args
}

func escapeLikeLiteral(value string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(value)
}

func itemOrderSQL(query Query) string {
	column := "lower(i.sort_name)"
	switch query.SortBy {
	case "Name":
		column = "lower(i.name)"
	case "DateCreated":
		column = "i.created_at"
	case "IndexNumber":
		return "i.parent_index_number " + query.SortOrder + ", i.index_number " + query.SortOrder + ", i.id " + query.SortOrder
	}
	return column + " " + query.SortOrder + ", i.id " + query.SortOrder
}

func scanItem(row rowScanner, additional ...any) (Item, error) {
	var item Item
	var encoded, encodedMetadata []byte
	destinations := []any{&item.ID, &item.LibraryID, &item.ParentID, &item.Name, &item.SortName,
		&item.Type, &item.Path, &item.Overview, &item.IsFolder, &item.IndexNumber,
		&item.ParentIndexNumber, &item.CreatedAt, &encoded, &encodedMetadata}
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
	return item, nil
}

func scanLibrary(row rowScanner) (Library, error) {
	var library Library
	err := row.Scan(&library.ID, &library.Name, &library.CollectionType, &library.Paths,
		&library.CreatedAt, &library.LastScanAt)
	return library, err
}
