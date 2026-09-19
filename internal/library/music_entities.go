package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
)

// musicEntityQuerySQL produces the same authorized entity set for navigation
// counts, lists and detail reads. Favorite filters belong to entity user state;
// all other item filters constrain the associated music source catalog.
func musicEntityQuerySQL(query Query, access libraryAccess, parentLibraryID, family string) (string, []any, error) {
	kind, credit, owner := "MusicArtist", "association.credit_group=1 AND association.credit_type='Artist'", "i.id"
	switch family {
	case "artists":
	case "allartists":
		credit = "((association.credit_group=1 AND association.credit_type='Artist') OR (association.credit_group=2 AND association.credit_type='AlbumArtist'))"
	case "albumartists":
		credit, owner = "association.credit_group=2 AND association.credit_type='AlbumArtist'", effectiveMusicAlbumArtistItemSQL
	case "genres":
		kind, credit = "Genre", "association.credit_group=0"
	default:
		return "", nil, ErrInvalidInput
	}
	search, favorite, favoriteOrLikes := query.SearchTerm, query.IsFavorite, query.IsFavoriteOrLikes
	starts, from, before := query.NameStartsWith, query.NameStartsWithOrGreater, query.NameLessThan
	if len(search) > 1024 || !utf8.ValidString(search) || strings.ContainsRune(search, '\x00') || (favorite != nil || favoriteOrLikes != nil) && query.UserID == "" {
		return "", nil, ErrInvalidInput
	}
	query.SearchTerm, query.IsFavorite, query.Recursive = "", nil, true
	query.IsFavoriteOrLikes = nil
	query.NameStartsWith, query.NameStartsWithOrGreater, query.NameLessThan = "", "", ""
	prefix, filter, args := itemQuerySQL(query, access, parentLibraryID)
	filter += " AND i.type IN ('Audio','MusicAlbum','MusicVideo') AND " + credit
	args = append(args, kind)
	filter += fmt.Sprintf(" AND entity.kind=$%d", len(args))
	if search != "" {
		args = append(args, "%"+escapeLikeLiteral(search)+"%")
		filter += fmt.Sprintf(" AND entity.name ILIKE $%d ESCAPE E'\\\\'", len(args))
	}
	if starts != "" {
		args = append(args, escapeLikeLiteral(starts)+"%")
		filter += fmt.Sprintf(" AND entity.name ILIKE $%d ESCAPE E'\\\\'", len(args))
	}
	for _, bound := range []struct{ value, operator string }{{from, ">="}, {before, "<"}} {
		if bound.value != "" {
			args = append(args, bound.value)
			filter += fmt.Sprintf(" AND lower(entity.name) %s lower($%d::text)", bound.operator, len(args))
		}
	}
	if favorite != nil {
		args = append(args, query.UserID)
		condition := fmt.Sprintf(`EXISTS (SELECT 1 FROM entity_user_data music_state
			WHERE music_state.user_id=$%d AND music_state.entity_id=entity.id AND music_state.is_favorite)`, len(args))
		if !*favorite {
			condition = "NOT " + condition
		}
		filter += " AND " + condition
	}
	if favoriteOrLikes != nil {
		args = append(args, query.UserID)
		condition := fmt.Sprintf(`EXISTS(SELECT 1 FROM entity_user_data music_state
			WHERE music_state.user_id=$%d AND music_state.entity_id=entity.id AND (music_state.is_favorite OR music_state.likes IS TRUE))`, len(args))
		if !*favoriteOrLikes {
			condition = "NOT " + condition
		}
		filter += " AND " + condition
	}
	if prefix == "" {
		prefix = "WITH "
	} else {
		prefix = strings.TrimSpace(prefix) + ", "
	}
	prefix += `eligible_entities AS (
		SELECT entity.id,entity.name,entity.kind,count(DISTINCT i.id) AS item_count
		FROM items i JOIN item_entities association ON association.item_id=` + owner + `
		JOIN catalog_entities entity ON entity.id=association.entity_id
		WHERE ` + filter + ` GROUP BY entity.id,entity.name,entity.kind
	) `
	return prefix, args, nil
}

func normalizeMusicEntityQuery(query Query) (Query, error) {
	query.Recursive = true
	query, err := normalizeItemQuery(query)
	if err != nil {
		return Query{}, err
	}
	if query.SortBy != "Name" && query.SortBy != "SortName" {
		return Query{}, ErrInvalidInput
	}
	return query, nil
}

func (s *Store) ListMusicEntities(ctx context.Context, family string, query Query) (EntityResult, error) {
	return s.listMusicEntities(ctx, family, query, nil)
}

// QueryMusicMetadataEntities revalidates the native administrator in the same
// snapshot as source membership, counts, images and independent entity state.
func (s *Store) QueryMusicMetadataEntities(ctx context.Context, actor identity.Principal, family string, query Query) (EntityResult, error) {
	query.UserID, query.ApplicationCredentialID = actor.User.ID, ""
	return s.listMusicEntities(ctx, family, query, &actor)
}

func (s *Store) listMusicEntities(ctx context.Context, family string, query Query, actor *identity.Principal) (EntityResult, error) {
	query, err := normalizeMusicEntityQuery(query)
	if err != nil {
		return EntityResult{}, err
	}
	var tx pgx.Tx
	var access libraryAccess
	if actor == nil {
		tx, access, err = s.beginSubjectRead(ctx, Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID})
	} else {
		tx, err = s.beginMetadataRead(ctx, *actor)
		access = libraryAccess{all: true, administrator: true, userID: actor.User.ID}
	}
	if err != nil {
		return EntityResult{}, err
	}
	defer rollback(tx)
	parent, err := readOrdinaryQueryParent(ctx, tx, query.ParentID, access)
	if err != nil {
		return EntityResult{}, err
	}
	prefix, args, err := musicEntityQuerySQL(query, access, parent, family)
	if err != nil {
		return EntityResult{}, err
	}
	result := EntityResult{Items: []Entity{}}
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM eligible_entities", args...).Scan(&result.TotalRecordCount); err != nil {
		return EntityResult{}, fmt.Errorf("count music entities: %w", err)
	}
	args = append(args, query.Limit, query.StartIndex)
	rows, err := tx.Query(ctx, prefix+`SELECT id,name,kind,item_count FROM eligible_entities
		ORDER BY lower(name) `+query.SortOrder+`,id `+query.SortOrder+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return EntityResult{}, fmt.Errorf("list music entities: %w", err)
	}
	for rows.Next() {
		entity, err := scanEntity(rows)
		if err != nil {
			rows.Close()
			return EntityResult{}, fmt.Errorf("read music entity: %w", err)
		}
		result.Items = append(result.Items, entity)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return EntityResult{}, fmt.Errorf("finish music entities: %w", err)
	}
	subject := Subject{UserID: query.UserID, ApplicationCredentialID: query.ApplicationCredentialID, Actor: actor}
	if err := populateEntityProjections(ctx, tx, subject, result.Items); err != nil {
		return EntityResult{}, err
	}
	if actor != nil {
		if err := authorizeMetadataActor(ctx, tx, *actor); err != nil {
			return EntityResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return EntityResult{}, fmt.Errorf("complete music entity read: %w", err)
	}
	return result, nil
}

func (s *Store) GetMusicEntityFor(ctx context.Context, subject Subject, family, name string) (Entity, error) {
	if strings.TrimSpace(name) == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsRune(name, '\x00') {
		return Entity{}, ErrInvalidInput
	}
	query, err := normalizeMusicEntityQuery(Query{UserID: subject.UserID, ApplicationCredentialID: subject.ApplicationCredentialID})
	if err != nil {
		return Entity{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return Entity{}, err
	}
	defer rollback(tx)
	// The standard artist name-detail route also resolves an artist whose only
	// surviving credit is AlbumArtist. It still requires a visible music item.
	if family == "artists" {
		family = "allartists"
	}
	prefix, args, err := musicEntityQuerySQL(query, access, "", family)
	if err != nil {
		return Entity{}, err
	}
	args = append(args, name)
	entity, err := scanEntity(tx.QueryRow(ctx, prefix+`SELECT id,name,kind,item_count FROM eligible_entities
		WHERE lower(btrim(name))=lower(btrim($`+fmt.Sprint(len(args))+`::text))`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Entity{}, ErrNotFound
	}
	if err != nil {
		return Entity{}, fmt.Errorf("read music entity by name: %w", err)
	}
	entities := []Entity{entity}
	if err := populateEntityProjections(ctx, tx, subject, entities); err != nil {
		return Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Entity{}, fmt.Errorf("complete music entity detail: %w", err)
	}
	return entities[0], nil
}
