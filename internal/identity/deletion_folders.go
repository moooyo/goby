package identity

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

// DeletionFolder is a current catalog scope, not a filesystem deletion target.
// Selecting it never bypasses media deletion's independent ACL and source checks.
type DeletionFolder struct {
	ID          string `json:"Id"`
	Name        string `json:"Name"`
	Type        string `json:"Type"`
	LibraryID   string `json:"LibraryId"`
	LibraryName string `json:"LibraryName"`
	ParentID    string `json:"ParentId"`
	Path        string `json:"Path"`
}

type DeletionFolderQuery struct {
	SearchTerm, LibraryID string
	StartIndex, Limit     int
}

type DeletionFolderPage struct {
	Items            []DeletionFolder `json:"Items"`
	TotalRecordCount int64            `json:"TotalRecordCount"`
	StartIndex       int              `json:"StartIndex"`
	Limit            int              `json:"Limit"`
}

// A physical library has registered roots. This excludes the internal virtual
// collection library without copying its package-private reserved identifier.
// Real-root synthetic Series/Season folders are valid even with an empty path.
func deletionFolderCandidatesSQL() string {
	return `SELECT l.id,l.name,'CollectionFolder'::text AS type,l.id AS library_id,l.name AS library_name,
		''::text AS parent_id,''::text AS path,''::text AS root_id FROM libraries l
		WHERE EXISTS (SELECT 1 FROM library_roots root WHERE root.library_id=l.id)
		UNION ALL SELECT folder.id,folder.name,folder.type,folder.library_id,l.name,
		COALESCE(folder.parent_id,''),folder.path,root.id FROM items folder
		JOIN libraries l ON l.id=folder.library_id
		JOIN library_roots root ON root.id=folder.root_id AND root.library_id=folder.library_id
		WHERE folder.id<>folder.library_id AND folder.is_folder
		AND folder.type IN ('Folder','Series','Season','MusicAlbum','MusicArtist') AND ` + database.CatalogOrdinaryItemSQL("folder")
}

func normalizeDeletionFolderQuery(query DeletionFolderQuery) (DeletionFolderQuery, error) {
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.StartIndex < 0 || int64(query.StartIndex) > 2147483647 || query.Limit < 1 || query.Limit > 200 ||
		len(query.SearchTerm) > 256 || !utf8.ValidString(query.SearchTerm) || strings.IndexFunc(query.SearchTerm, unicode.IsControl) >= 0 ||
		query.LibraryID != "" && !validRevalidationID(query.LibraryID) {
		return DeletionFolderQuery{}, managedUserFieldError("Query", "use a bounded search, current library identifier, and a page limit from 1 through 200")
	}
	query.SearchTerm = strings.TrimSpace(query.SearchTerm)
	return query, nil
}

func (s *Store) ListDeletionFolders(ctx context.Context, actor Principal, query DeletionFolderQuery) (DeletionFolderPage, error) {
	query, err := normalizeDeletionFolderQuery(query)
	if err != nil {
		return DeletionFolderPage{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return DeletionFolderPage{}, err
	}
	defer rollback(tx)
	if err := CheckAdministrator(ctx, tx, actor, AdministratorNative, false); err != nil {
		return DeletionFolderPage{}, err
	}
	result := DeletionFolderPage{Items: []DeletionFolder{}, StartIndex: query.StartIndex, Limit: query.Limit}
	prefix := `WITH choices AS (` + deletionFolderCandidatesSQL() + `), filtered AS (
		SELECT * FROM choices WHERE ($1='' OR library_id=$1) AND ($2='' OR strpos(lower(name),lower($2))>0
		OR strpos(lower(library_name),lower($2))>0 OR strpos(lower(id),lower($2))>0)) `
	if err := tx.QueryRow(ctx, prefix+`SELECT count(*) FROM filtered`, query.LibraryID, query.SearchTerm).Scan(&result.TotalRecordCount); err != nil {
		return DeletionFolderPage{}, err
	}
	rows, err := tx.Query(ctx, prefix+`SELECT id,name,type,library_id,library_name,parent_id,path FROM filtered
		ORDER BY lower(library_name),library_id,(type='CollectionFolder') DESC,lower(name),id LIMIT $3 OFFSET $4`, query.LibraryID, query.SearchTerm, query.Limit, query.StartIndex)
	if err != nil {
		return DeletionFolderPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var value DeletionFolder
		if err := rows.Scan(&value.ID, &value.Name, &value.Type, &value.LibraryID, &value.LibraryName, &value.ParentID, &value.Path); err != nil {
			return DeletionFolderPage{}, err
		}
		result.Items = append(result.Items, value)
	}
	if err := rows.Err(); err != nil {
		return DeletionFolderPage{}, err
	}
	rows.Close()
	if err := CheckAdministrator(ctx, tx, actor, AdministratorNative, false); err != nil {
		return DeletionFolderPage{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeletionFolderPage{}, err
	}
	return result, nil
}

type deletionFolderReference struct{ libraryID, rootID string }

func readDeletionFolderReferences(ctx context.Context, tx pgx.Tx, ids []string) (map[string]deletionFolderReference, error) {
	rows, err := tx.Query(ctx, `SELECT id,library_id,root_id FROM (`+deletionFolderCandidatesSQL()+`) choices WHERE id=ANY($1::text[]) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]deletionFolderReference, len(ids))
	for rows.Next() {
		var id string
		var reference deletionFolderReference
		if err := rows.Scan(&id, &reference.libraryID, &reference.rootID); err != nil {
			return nil, err
		}
		if _, exists := result[id]; exists {
			return nil, managedUserFieldError("Policy.EnableContentDeletionFromFolders", "a deletion scope identifier is ambiguous")
		}
		result[id] = reference
	}
	return result, rows.Err()
}

// Only an unchanged saved value receives legacy preservation. A copied policy
// has no previous target grant and must validate every library/folder identity.
func validateManagedDeletionFolders(ctx context.Context, tx pgx.Tx, incoming, previous []string) error {
	retained := make(map[string]bool, len(previous))
	for _, id := range previous {
		retained[id] = true
	}
	ids := make([]string, 0, len(incoming))
	for _, id := range incoming {
		if !retained[id] {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	before, err := readDeletionFolderReferences(ctx, tx, ids)
	if err != nil {
		return err
	}
	invalid := func() error {
		return managedUserFieldError("Policy.EnableContentDeletionFromFolders", "new deletion grants must identify existing physical libraries or ordinary catalog folders")
	}
	if len(before) != len(ids) {
		return invalid()
	}
	libraries := make([]string, 0, len(before))
	for _, ref := range before {
		libraries = append(libraries, ref.libraryID)
	}
	slices.Sort(libraries)
	libraries = slices.Compact(libraries)
	// Account and credential locks precede catalog locks. Keep selected libraries
	// and items alive through publication, using the same library-before-item
	// order as the actual media deletion workflow. Root mutations advance the
	// library revision; item SHARE locks prevent folder reclassification/moves.
	for _, lock := range []struct {
		statement string
		values    []string
	}{
		{`SELECT id FROM libraries WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, libraries},
		{`SELECT id FROM items WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, ids},
	} {
		rows, err := tx.Query(ctx, lock.statement, lock.values)
		if err != nil {
			return fmt.Errorf("lock deletion grant scope: %w", err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	// The discovery statement may have preceded a row-lock wait. Re-read the
	// actual current type/root relationships before granting any new identity.
	after, err := readDeletionFolderReferences(ctx, tx, ids)
	if err != nil {
		return err
	}
	if len(after) != len(before) {
		return invalid()
	}
	for id, reference := range before {
		if current, ok := after[id]; !ok || current != reference {
			return invalid()
		}
	}
	return nil
}

func validateSelectedUnratedCategories(incoming, previous []string) error {
	for _, category := range incoming {
		if !slices.Contains([]string{"Movie", "Trailer", "Series", "Music", "Other"}, category) && !slices.Contains(previous, category) {
			return managedUserFieldError("Policy.BlockUnratedItems", "new categories must be Movie, Trailer, Series, Music, or Other; existing inactive categories may be retained or removed")
		}
	}
	return nil
}
