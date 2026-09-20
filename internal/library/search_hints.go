package library

import (
	"context"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// SearchHintsQuery describes Goby's bounded legacy search adapter. It does not
// reuse item IDs as entity IDs or apply personal playback preferences to search.
type SearchHintsQuery struct {
	SearchTerm                                                                 string
	StartIndex, Limit                                                          int
	IncludeMedia, IncludePeople, IncludeGenres, IncludeStudios, IncludeArtists *bool
	IncludeItemTypes, ExcludeItemTypes, MediaTypes                             []string
	IsMovie, IsSeries                                                          *bool
}

// SearchHintReference keeps physical and entity identities disjoint even when
// their wire IDs have the same decimal spelling.
type SearchHintReference struct {
	Kind string
	ID   string
}

type SearchHint struct {
	Reference            SearchHintReference
	Item                 *Item
	Entity               *Entity
	Images               []Image
	LegacyImageReference bool
}

type SearchHintResult struct {
	SearchHints      []SearchHint
	TotalRecordCount int
}

var searchHintTypes = map[string]string{
	"folder": "Folder", "movie": "Movie", "series": "Series", "season": "Season",
	"episode": "Episode", "video": "Video", "audio": "Audio", "musicalbum": "MusicAlbum",
	"playlist": "Playlist", "boxset": "BoxSet", "musicvideo": "MusicVideo",
	"person": "Person", "genre": "Genre", "studio": "Studio", "musicartist": "MusicArtist",
}

func normalizeSearchHintsQuery(subject Subject, query SearchHintsQuery) (SearchHintsQuery, error) {
	if !validSubject(subject) || len(query.SearchTerm) > 1024 || !utf8.ValidString(query.SearchTerm) ||
		strings.ContainsRune(query.SearchTerm, '\x00') || query.StartIndex < 0 || query.StartIndex > math.MaxInt32 ||
		query.Limit < 0 || query.Limit > 1000 {
		return SearchHintsQuery{}, ErrInvalidInput
	}
	query.SearchTerm = strings.TrimSpace(query.SearchTerm)
	var err error
	for _, field := range []struct {
		values  *[]string
		allowed map[string]string
	}{
		{&query.IncludeItemTypes, searchHintTypes}, {&query.ExcludeItemTypes, searchHintTypes},
		{&query.MediaTypes, map[string]string{"audio": "Audio", "video": "Video"}},
	} {
		if len(*field.values) > 32 {
			return SearchHintsQuery{}, ErrInvalidInput
		}
		*field.values, err = normalizeQueryValues(*field.values, field.allowed)
		if err != nil {
			return SearchHintsQuery{}, err
		}
	}
	return query, nil
}

func searchHintEnabled(value *bool) bool { return value == nil || *value }

// SearchHints authorizes the complete mixed population before ranking, counting
// and paging it. All returned identities, metadata and artwork are projected in
// the same read transaction as the Subject's current policy.
func (s *Store) SearchHints(ctx context.Context, subject Subject, query SearchHintsQuery) (SearchHintResult, error) {
	query, err := normalizeSearchHintsQuery(subject, query)
	if err != nil {
		return SearchHintResult{}, err
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return SearchHintResult{}, err
	}
	defer rollback(tx)
	result := SearchHintResult{SearchHints: []SearchHint{}}
	if query.SearchTerm == "" {
		if err := tx.Commit(ctx); err != nil {
			return SearchHintResult{}, fmt.Errorf("complete empty hint search: %w", err)
		}
		return result, nil
	}
	prefix, args := searchHintsSQL(query, access)
	if err := tx.QueryRow(ctx, prefix+"SELECT count(*) FROM eligible_hints", args...).Scan(&result.TotalRecordCount); err != nil {
		return SearchHintResult{}, fmt.Errorf("count search hints: %w", err)
	}
	if query.Limit != 0 && query.StartIndex < result.TotalRecordCount {
		args = append(args, query.Limit, query.StartIndex)
		rows, err := tx.Query(ctx, prefix+"SELECT owner_kind,id FROM eligible_hints "+
			"ORDER BY match_rank,lower(name) COLLATE \"C\",name COLLATE \"C\",owner_kind COLLATE \"C\",type COLLATE \"C\",id COLLATE \"C\""+
			fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
		if err != nil {
			return SearchHintResult{}, fmt.Errorf("page search hints: %w", err)
		}
		for rows.Next() {
			var reference SearchHintReference
			if err := rows.Scan(&reference.Kind, &reference.ID); err != nil {
				rows.Close()
				return SearchHintResult{}, fmt.Errorf("read search hint reference: %w", err)
			}
			result.SearchHints = append(result.SearchHints, SearchHint{Reference: reference, Images: []Image{}})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return SearchHintResult{}, fmt.Errorf("finish search hint references: %w", err)
		}
		if err := populateSearchHints(ctx, tx, subject, access, result.SearchHints); err != nil {
			return SearchHintResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SearchHintResult{}, fmt.Errorf("complete hint search: %w", err)
	}
	return result, nil
}

// The source set is independent of the search text: an entity may match even
// when its associated media title does not. MediaTypes scopes both physical
// matches and the visible source membership that qualifies entity matches.
func searchHintsSQL(query SearchHintsQuery, access libraryAccess) (string, []any) {
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

func populateSearchHints(ctx context.Context, tx pgx.Tx, subject Subject, access libraryAccess, hints []SearchHint) error {
	itemIDs, entityIDs := []string{}, []string{}
	itemPositions, entityPositions := map[string]int{}, map[string]int{}
	for index, hint := range hints {
		switch hint.Reference.Kind {
		case "Item":
			itemIDs = append(itemIDs, hint.Reference.ID)
			itemPositions[hint.Reference.ID] = index
		case "Entity":
			entityIDs = append(entityIDs, hint.Reference.ID)
			entityPositions[hint.Reference.ID] = index
		default:
			return ErrUnavailable
		}
	}
	if len(itemIDs) != 0 {
		rows, err := tx.Query(ctx, "SELECT "+access.itemColumnsSQL()+" FROM items i WHERE i.id=ANY($1::text[]) AND "+access.ordinarySQL("i"), itemIDs)
		if err != nil {
			return fmt.Errorf("project item search hints: %w", err)
		}
		for rows.Next() {
			item, err := scanItem(rows)
			if err != nil {
				rows.Close()
				return fmt.Errorf("read item search hint: %w", err)
			}
			index, exists := itemPositions[item.ID]
			if !exists {
				rows.Close()
				return ErrUnavailable
			}
			item.CanPlay = access.canPlay
			hints[index].Item, hints[index].LegacyImageReference = &item, true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("finish item search hints: %w", err)
		}
		images, err := searchHintItemImages(ctx, tx, access, itemIDs)
		if err != nil {
			return err
		}
		for id, index := range itemPositions {
			hints[index].Images = images[id]
		}
	}
	if len(entityIDs) != 0 {
		// Physical collisions are checked only against visible direct items.
		// An inaccessible physical item does not shadow the existing authorized
		// entity fallback and must not disclose even a collision bit.
		rows, err := tx.Query(ctx, "SELECT entity.id,entity.name,entity.kind,0,"+
			"NOT EXISTS(SELECT 1 FROM items i WHERE i.id=entity.id::text AND "+access.directSQL("i")+") "+
			"FROM catalog_entities entity WHERE entity.id::text=ANY($1::text[])", entityIDs)
		if err != nil {
			return fmt.Errorf("project entity search hints: %w", err)
		}
		entities := []Entity{}
		for rows.Next() {
			var entity Entity
			var legacy bool
			if err := rows.Scan(&entity.ID, &entity.Name, &entity.Type, &entity.Count, &legacy); err != nil {
				rows.Close()
				return fmt.Errorf("read entity search hint: %w", err)
			}
			id := fmt.Sprint(entity.ID)
			index, exists := entityPositions[id]
			if !exists {
				rows.Close()
				return ErrUnavailable
			}
			hints[index].LegacyImageReference = legacy
			entities = append(entities, entity)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("finish entity search hints: %w", err)
		}
		if err := populateEntityProjections(ctx, tx, subject, access, entities); err != nil {
			return err
		}
		for index := range entities {
			entity := &entities[index]
			hint := &hints[entityPositions[fmt.Sprint(entity.ID)]]
			hint.Entity, hint.Images = entity, entity.Images
		}
	}
	for _, hint := range hints {
		if (hint.Item == nil) == (hint.Entity == nil) {
			return ErrUnavailable
		}
	}
	return nil
}

// Only physical item references reach this helper. It deliberately omits the
// generic batch's decimal-ID entity fallback, so colliding owners cannot merge.
func searchHintItemImages(ctx context.Context, tx pgx.Tx, access libraryAccess, ids []string) (map[string][]Image, error) {
	result := make(map[string][]Image, len(ids))
	rows, err := tx.Query(ctx, "SELECT i.id,"+storedImageColumns+storedImageSource+
		" WHERE i.id=ANY($1::text[]) AND "+access.ordinarySQL("i")+
		strings.Replace(storedImageOrder, " ORDER BY ", " ORDER BY i.id,", 1), ids)
	if err != nil {
		return nil, fmt.Errorf("query search hint images: %w", err)
	}
	for rows.Next() {
		var id string
		stored, err := scanStoredImage(rows, &id)
		if err != nil {
			rows.Close()
			return nil, err
		}
		result[id] = append(result[id], stored.Image)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, fmt.Errorf("read search hint images: %w", err)
	}
	for _, merge := range []func(context.Context, pgx.Tx, libraryAccess, []string, map[string][]Image) error{
		mergeEmbeddedImageListing, mergeProviderImageListing, mergeManagedImageListing, mergeCollageImageListing,
	} {
		if err := merge(ctx, tx, access, ids, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}
