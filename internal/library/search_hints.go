package library

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// SearchHintsQuery describes Goby's bounded legacy search adapter. It does not
// reuse item IDs as entity IDs or apply personal playback preferences to search.
type SearchHintsQuery struct {
	Projection                                                                 QueryProjection
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
		if err := populateSearchHints(ctx, tx, subject, access, result.SearchHints, query.Projection); err != nil {
			return SearchHintResult{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return SearchHintResult{}, fmt.Errorf("complete hint search: %w", err)
	}
	return result, nil
}

// Each branch filters its own names before proving source visibility. Entity
// membership is independent of media titles, while MediaTypes still scopes
// both physical matches and the authorized sources that qualify entities.
func searchHintsSQL(query SearchHintsQuery, access libraryAccess) (string, []any) {
	_, sourceFilter, args := itemQuerySQL(Query{Recursive: true, MediaTypes: query.MediaTypes}, access, "")
	sourceFilter += " AND i.type<>'CollectionFolder'"
	args = append(args, query.SearchTerm, "%"+escapeLikeLiteral(query.SearchTerm)+"%", escapeLikeLiteral(query.SearchTerm)+"%")
	term, contains, starts := len(args)-2, len(args)-1, len(args)
	nameMatch := fmt.Sprintf("ILIKE $%d ESCAPE E'\\\\'", contains)
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
	prefix := "WITH mixed_hints AS (SELECT 'Item'::text AS owner_kind,i.id,i.name,i.type FROM items i " +
		fmt.Sprintf("WHERE $%d::boolean AND i.name ", includeMedia) + nameMatch + " AND " + sourceFilter + " UNION ALL " +
		"SELECT 'Entity'::text,entity.id::text,entity.name,entity.kind " +
		"FROM catalog_entities entity WHERE entity.name " + nameMatch +
		fmt.Sprintf(" AND entity.kind=ANY($%d::text[]) AND EXISTS (", entityKinds) +
		"SELECT 1 FROM item_entities association JOIN items i ON i.id=association.item_id " +
		"WHERE association.entity_id=entity.id AND " + sourceFilter + " AND " + validEntityAssociationSQL +
		" AND (entity.kind<>'MusicArtist' OR i.type IN ('Audio','MusicAlbum','MusicVideo'))" +
		")), eligible_hints AS (SELECT owner_kind,id,name,type," + rank + " AS match_rank FROM mixed_hints"
	conditions := []string{}
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
			conditions = append(conditions, condition)
		}
	}
	for _, field := range []struct {
		kind  string
		value *bool
	}{{"Movie", query.IsMovie}, {"Series", query.IsSeries}} {
		if field.value != nil {
			args = append(args, *field.value)
			conditions = append(conditions, fmt.Sprintf("(type='%s')=$%d::boolean", field.kind, len(args)))
		}
	}
	if len(conditions) != 0 {
		prefix += " WHERE " + strings.Join(conditions, " AND ")
	}
	return prefix + ") ", args
}

func populateSearchHints(ctx context.Context, tx pgx.Tx, subject Subject, access libraryAccess, hints []SearchHint, projection QueryProjection) error {
	itemIDs := []string{}
	entityIDs := []int64{}
	itemPositions, entityPositions := map[string]int{}, map[string]int{}
	for index, hint := range hints {
		switch hint.Reference.Kind {
		case "Item":
			itemIDs = append(itemIDs, hint.Reference.ID)
			itemPositions[hint.Reference.ID] = index
		case "Entity":
			id, err := strconv.ParseInt(hint.Reference.ID, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != hint.Reference.ID {
				return ErrUnavailable
			}
			entityIDs = append(entityIDs, id)
			entityPositions[hint.Reference.ID] = index
		default:
			return ErrUnavailable
		}
	}
	if len(itemIDs) != 0 {
		rows, err := tx.Query(ctx, "SELECT "+access.scopeSQL(itemQueryColumns(Query{Projection: projection}))+" FROM items i WHERE i.id=ANY($1::text[]) AND "+access.ordinarySQL("i"), itemIDs)
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
		if !projection.ImagesDisabled {
			images, err := searchHintItemImages(ctx, tx, access, itemIDs)
			if err != nil {
				return err
			}
			for id, index := range itemPositions {
				hints[index].Images = images[id]
			}
		}
	}
	if len(entityIDs) != 0 {
		// Physical collisions are checked only against visible direct items.
		// An inaccessible physical item does not shadow the existing authorized
		// entity fallback and must not disclose even a collision bit.
		rows, err := tx.Query(ctx, "SELECT entity.id,entity.name,entity.kind,0,"+
			"NOT EXISTS(SELECT 1 FROM items i WHERE i.id=entity.id::text AND "+access.directSQL("i")+") "+
			"FROM catalog_entities entity WHERE entity.id=ANY($1::bigint[])", entityIDs)
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
		if err := populateEntityProjections(ctx, tx, subject, access, entities, projection); err != nil {
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
