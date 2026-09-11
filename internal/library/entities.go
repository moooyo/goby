package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// EntityRef carries the numeric identifier used by genre, tag, and studio item
// projections. Orphaned entity records retain their IDs but are never browsable.
type EntityRef struct {
	ID   int64
	Name string
}

// PersonRef describes an item credit. A person's stable wire ID is decimal text,
// while role, type, and ordering belong to this item association.
type PersonRef struct {
	ID, Name, Role, Type string
	SortOrder            *int
}

type ItemEntities struct {
	Genres, Tags, Studios []EntityRef
	Artists, AlbumArtists []EntityRef
	People                []PersonRef
}

type Entity struct {
	ID         int64
	Name, Type string
	Count      int
}

type EntityResult struct {
	Items            []Entity
	TotalRecordCount int
}

// Music roles use a bounded key component; legacy free-form credit text alone
// must never be interpreted as a newly indexed music relationship.
const validEntityAssociationSQL = `(entity.kind <> 'MusicArtist' OR
	(association.credit_group = 1 AND association.credit_type = 'Artist') OR
	(association.credit_group = 2 AND association.credit_type = 'AlbumArtist'))`

// ListEntities counts distinct authorized source items before entity pagination.
// SearchTerm searches entity names; other item filters scope the source catalog.
func (s *Store) ListEntities(ctx context.Context, kind string, query Query) (EntityResult, error) {
	kind, err := normalizeEntityKind(kind)
	if err != nil {
		return EntityResult{}, err
	}
	searchTerm := query.SearchTerm
	if !utf8.ValidString(searchTerm) || strings.ContainsRune(searchTerm, '\x00') {
		return EntityResult{}, ErrInvalidInput
	}
	query.SearchTerm = ""
	query.Recursive = true
	query, err = normalizeItemQuery(query)
	if err != nil {
		return EntityResult{}, err
	}
	if len(query.ListItemIds) != 0 {
		return EntityResult{}, ErrUnsupportedFilter
	}
	if query.SortBy != "Name" && query.SortBy != "SortName" {
		return EntityResult{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	if err != nil {
		return EntityResult{}, err
	}
	defer rollback(tx)
	parentLibraryID, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return EntityResult{}, err
	}
	prefix, filter, args := itemQuerySQL(query, access, parentLibraryID)
	args = append(args, kind)
	filter += fmt.Sprintf(" AND entity.kind = $%d", len(args)) + " AND " + validEntityAssociationSQL
	if searchTerm != "" {
		args = append(args, "%"+escapeLikeLiteral(searchTerm)+"%")
		filter += fmt.Sprintf(" AND entity.name ILIKE $%d ESCAPE E'\\\\'", len(args))
	}
	if prefix == "" {
		prefix = "WITH "
	} else {
		prefix = strings.TrimSpace(prefix) + ", "
	}
	prefix += `eligible_entities AS (
		SELECT entity.id, entity.name, entity.kind, count(DISTINCT i.id) AS item_count
		FROM items i JOIN item_entities association ON association.item_id = i.id
		JOIN catalog_entities entity ON entity.id = association.entity_id
		WHERE ` + filter + ` GROUP BY entity.id, entity.name, entity.kind
	) `
	result := EntityResult{Items: make([]Entity, 0)}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM eligible_entities", args...).Scan(&result.TotalRecordCount); err != nil {
		return EntityResult{}, fmt.Errorf("count visible catalog entities: %w", err)
	}
	args = append(args, query.Limit, query.StartIndex)
	statement := prefix + `SELECT id, name, kind, item_count FROM eligible_entities
		ORDER BY lower(name) ` + query.SortOrder + ", id " + query.SortOrder + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	rows, err := tx.Query(ctx, statement, args...)
	if err != nil {
		return EntityResult{}, fmt.Errorf("list visible catalog entities: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		entity, err := scanEntity(rows)
		if err != nil {
			return EntityResult{}, fmt.Errorf("read visible catalog entity: %w", err)
		}
		result.Items = append(result.Items, entity)
	}
	if err := rows.Err(); err != nil {
		return EntityResult{}, fmt.Errorf("read visible catalog entities: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return EntityResult{}, fmt.Errorf("complete entity listing: %w", err)
	}
	return result, nil
}

// GetEntity resolves a kind and normalized name without exposing orphaned or
// unauthorized records. Case normalization is performed by PostgreSQL.
func (s *Store) GetEntity(ctx context.Context, userID, kind, name string) (Entity, error) {
	return s.GetEntityFor(ctx, Subject{UserID: userID}, kind, name)
}

// GetEntityFor resolves an associated entity within the subject's catalog scope.
func (s *Store) GetEntityFor(ctx context.Context, subject Subject, kind, name string) (Entity, error) {
	kind, err := normalizeEntityKind(kind)
	if err != nil {
		return Entity{}, err
	}
	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
		return Entity{}, ErrInvalidInput
	}
	return s.getEntity(ctx, subject, `entity.kind = $3 AND entity.normalized_hash = sha256(convert_to(lower(btrim($4::text)), 'UTF8'))
		AND entity.normalized_name = lower(btrim($4::text))`, kind, name)
}

func (s *Store) GetEntityByID(ctx context.Context, userID string, id int64) (Entity, error) {
	return s.GetEntityByIDFor(ctx, Subject{UserID: userID}, id)
}

// GetEntityByIDFor does not expose orphaned catalog entities.
func (s *Store) GetEntityByIDFor(ctx context.Context, subject Subject, id int64) (Entity, error) {
	if id <= 0 {
		return Entity{}, ErrInvalidInput
	}
	return s.getEntity(ctx, subject, "entity.id = $3", id)
}

func (s *Store) getEntity(ctx context.Context, subject Subject, condition string, values ...any) (Entity, error) {
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return Entity{}, err
	}
	defer rollback(tx)
	args := append([]any{access.all, access.folders}, values...)
	entity, err := scanEntity(tx.QueryRow(ctx, `SELECT entity.id, entity.name, entity.kind, count(DISTINCT i.id)
		FROM catalog_entities entity JOIN item_entities association ON association.entity_id = entity.id
		JOIN items i ON i.id = association.item_id
		WHERE ($1::boolean OR i.library_id = ANY($2::text[])) AND i.type <> 'CollectionFolder'
		AND `+ordinaryItemSQL("i")+` AND `+validEntityAssociationSQL+` AND `+condition+` GROUP BY entity.id, entity.name, entity.kind`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entity{}, ErrNotFound
	}
	if err != nil {
		return Entity{}, fmt.Errorf("get visible catalog entity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Entity{}, fmt.Errorf("complete entity read: %w", err)
	}
	return entity, nil
}

func scanEntity(row rowScanner) (Entity, error) {
	var entity Entity
	err := row.Scan(&entity.ID, &entity.Name, &entity.Type, &entity.Count)
	return entity, err
}

func normalizeEntityKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "genre", "genres":
		return "Genre", nil
	case "tag", "tags":
		return "Tag", nil
	case "studio", "studios":
		return "Studio", nil
	case "person", "persons":
		return "Person", nil
	default:
		return "", ErrInvalidInput
	}
}

func syncItemEntities(ctx context.Context, tx pgx.Tx, itemID string, metadataJSON []byte) error {
	if _, err := tx.Exec(ctx, "SELECT sync_catalog_item_entities($1, $2::jsonb)", itemID, metadataJSON); err != nil {
		return fmt.Errorf("synchronize item entities: %w", err)
	}
	return nil
}
