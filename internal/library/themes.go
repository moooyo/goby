package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/metadata"
)

const (
	// VirtualRootItemID is the existing navigation root, not a numeric item alias.
	VirtualRootItemID = "goby-library-root"
	// MaxThemeResourcesPerOwner bounds the complete active song/video population.
	// Scanning and querying must reject overflow rather than publish a partial set.
	MaxThemeResourcesPerOwner = 256
	// MaxThemeAncestorDepth counts parent edges; the seed itself has depth zero.
	MaxThemeAncestorDepth = 128
)

// ThemeQuery receives explicit defaults from the protocol adapter. The zero
// value disables both groups; it does not silently enable inheritance or media.
type ThemeQuery struct {
	Subject           Subject
	InheritFromParent bool
	EnableThemeSongs  bool
	EnableThemeVideos bool
}

// ThemeResult.OwnerID belongs to the durable theme-owner namespace. It is not
// an item/entity ID and must never be accepted as an alias on item routes.
type ThemeResult struct {
	OwnerID          int64
	Items            []Item
	TotalRecordCount int
}

type ThemeMediaResult struct {
	ThemeSongsResult      ThemeResult
	ThemeVideosResult     ThemeResult
	SoundtrackSongsResult ThemeResult
}

type themeOwner struct {
	id, libraryID string
	number        int64
}

type themePopulation struct {
	active        int64
	songs, videos bool
}

func emptyThemeMediaResult() ThemeMediaResult {
	return ThemeMediaResult{
		ThemeSongsResult:      ThemeResult{Items: make([]Item, 0)},
		ThemeVideosResult:     ThemeResult{Items: make([]Item, 0)},
		SoundtrackSongsResult: ThemeResult{Items: make([]Item, 0)},
	}
}

// QueryThemeMedia authorizes the seed, ancestors, resources, and user projection
// in one read-only repeatable-read snapshot. GET never allocates owner numbers
// or repairs relationships. Songs and videos inherit independently; soundtrack
// relationships are not indexed and remain the explicitly observed empty group.
func (s *Store) QueryThemeMedia(ctx context.Context, seedID string, query ThemeQuery) (ThemeMediaResult, error) {
	if seedID == "" || len(seedID) > 256 || !utf8.ValidString(seedID) || strings.TrimSpace(seedID) != seedID ||
		strings.IndexFunc(seedID, unicode.IsControl) >= 0 {
		return ThemeMediaResult{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, query.Subject)
	if err != nil {
		return ThemeMediaResult{}, err
	}
	defer rollback(tx)
	result := emptyThemeMediaResult()
	var seed themeOwner
	if seedID == VirtualRootItemID {
		seed.number, err = readThemeVirtualOwner(ctx, tx)
	} else {
		seed, err = readThemeSeed(ctx, tx, seedID, access)
	}
	if err != nil {
		return ThemeMediaResult{}, err
	}
	if query.EnableThemeSongs {
		result.ThemeSongsResult.OwnerID = seed.number
	}
	if query.EnableThemeVideos {
		result.ThemeVideosResult.OwnerID = seed.number
	}
	// Even disabled and virtual-root requests have now checked current subject
	// authority and durable seed identity. They cannot hide an unauthorized seed.
	if seedID != VirtualRootItemID && (query.EnableThemeSongs || query.EnableThemeVideos) {
		owners := []themeOwner{seed}
		if query.InheritFromParent {
			owners, err = readThemeAncestors(ctx, tx, seed, access)
			if err != nil {
				return ThemeMediaResult{}, err
			}
		}
		populations, err := readThemePopulations(ctx, tx, owners, access)
		if err != nil {
			return ThemeMediaResult{}, err
		}
		var songOwner, videoOwner string
		for _, owner := range owners {
			population := populations[owner.id]
			if population.active > MaxThemeResourcesPerOwner {
				return ThemeMediaResult{}, fmt.Errorf("%w: theme owner resource limit exceeded", ErrUnavailable)
			}
			if query.EnableThemeSongs && songOwner == "" && population.songs {
				songOwner, result.ThemeSongsResult.OwnerID = owner.id, owner.number
			}
			if query.EnableThemeVideos && videoOwner == "" && population.videos {
				videoOwner, result.ThemeVideosResult.OwnerID = owner.id, owner.number
			}
		}
		if query.InheritFromParent && (query.EnableThemeSongs && songOwner == "" || query.EnableThemeVideos && videoOwner == "") {
			root, err := readThemeVirtualOwner(ctx, tx)
			if err != nil {
				return ThemeMediaResult{}, err
			}
			if query.EnableThemeSongs && songOwner == "" {
				result.ThemeSongsResult.OwnerID = root
			}
			if query.EnableThemeVideos && videoOwner == "" {
				result.ThemeVideosResult.OwnerID = root
			}
		}
		if songOwner != "" || videoOwner != "" {
			if err := readThemeItems(ctx, tx, query.Subject, access, seed.libraryID, songOwner, videoOwner, &result); err != nil {
				return ThemeMediaResult{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ThemeMediaResult{}, fmt.Errorf("complete theme media query: %w", err)
	}
	return result, nil
}

func readThemeVirtualOwner(ctx context.Context, tx pgx.Tx) (int64, error) {
	var number int64
	err := tx.QueryRow(ctx, "SELECT id FROM theme_owner_ids WHERE virtual_root AND item_id IS NULL").Scan(&number)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("%w: theme virtual-root mapping is unavailable", ErrUnavailable)
	}
	return number, nil
}

func readThemeSeed(ctx context.Context, tx pgx.Tx, id string, access libraryAccess) (themeOwner, error) {
	var owner themeOwner
	var number *int64
	err := tx.QueryRow(ctx, `SELECT i.id, i.library_id, mapping.id FROM items i
		LEFT JOIN theme_owner_ids mapping ON mapping.item_id = i.id AND NOT mapping.virtual_root
		WHERE i.id = $1 AND `+directItemSQL("i")+` AND ($2::boolean OR i.library_id = ANY($3::text[]))`,
		id, access.all, access.folders).Scan(&owner.id, &owner.libraryID, &number)
	if errors.Is(err, pgx.ErrNoRows) {
		return themeOwner{}, themeMissingSeed(ctx, tx, id, access)
	}
	if err != nil {
		return themeOwner{}, fmt.Errorf("authorize theme seed: %w", err)
	}
	if number == nil || *number <= 0 {
		return themeOwner{}, fmt.Errorf("%w: theme seed mapping is unavailable", ErrUnavailable)
	}
	owner.number = *number
	return owner, nil
}

func themeMissingSeed(ctx context.Context, tx pgx.Tx, seedID string, access libraryAccess) error {
	id, err := strconv.ParseInt(seedID, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != seedID {
		return ErrNotFound
	}
	var visible bool
	err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM catalog_entities entity
		JOIN item_entities association ON association.entity_id = entity.id JOIN items i ON i.id = association.item_id
		WHERE entity.id = $1 AND i.type <> 'CollectionFolder' AND `+validEntityAssociationSQL+`
		AND `+ordinaryItemSQL("i")+` AND ($2::boolean OR i.library_id = ANY($3::text[])))`,
		id, access.all, access.folders).Scan(&visible)
	if err != nil {
		return fmt.Errorf("authorize theme entity seed: %w", err)
	}
	if visible {
		return ErrUnsupportedFilter
	}
	return ErrNotFound
}

func readThemeAncestors(ctx context.Context, tx pgx.Tx, seed themeOwner, access libraryAccess) ([]themeOwner, error) {
	rows, err := tx.Query(ctx, `WITH RECURSIVE theme_ancestors AS (
		SELECT i.id, i.library_id, i.parent_id, ARRAY[i.id] AS visited, 0 AS depth, false AS cycle
		FROM items i WHERE i.id = $1 AND i.library_id = $2
		UNION ALL
		SELECT parent.id, parent.library_id, parent.parent_id, child.visited || parent.id,
			child.depth + 1, parent.id = ANY(child.visited)
		FROM theme_ancestors child JOIN items parent ON parent.id = child.parent_id
		WHERE parent.library_id = $2 AND `+ordinaryItemSQL("parent")+`
			AND ($3::boolean OR parent.library_id = ANY($4::text[]))
			AND NOT child.cycle AND child.depth <= $5
	) SELECT ancestor.id, ancestor.library_id, mapping.id, ancestor.depth, ancestor.cycle
		FROM theme_ancestors ancestor LEFT JOIN theme_owner_ids mapping
		ON mapping.item_id = ancestor.id AND NOT mapping.virtual_root ORDER BY ancestor.depth`,
		seed.id, seed.libraryID, access.all, access.folders, MaxThemeAncestorDepth)
	if err != nil {
		return nil, fmt.Errorf("query theme ancestors: %w", err)
	}
	defer rows.Close()
	owners := make([]themeOwner, 0)
	for rows.Next() {
		var owner themeOwner
		var number *int64
		var depth int
		var cycle bool
		if err := rows.Scan(&owner.id, &owner.libraryID, &number, &depth, &cycle); err != nil {
			return nil, fmt.Errorf("read theme ancestor: %w", err)
		}
		if cycle || depth > MaxThemeAncestorDepth || number == nil || *number <= 0 {
			return nil, fmt.Errorf("%w: theme ancestry is cyclic, too deep, or lacks a durable owner", ErrUnavailable)
		}
		owner.number = *number
		owners = append(owners, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read theme ancestors: %w", err)
	}
	if len(owners) == 0 || owners[0] != seed {
		return nil, fmt.Errorf("%w: theme seed left its authorized ancestry", ErrUnavailable)
	}
	return owners, nil
}

func readThemePopulations(ctx context.Context, tx pgx.Tx, owners []themeOwner, access libraryAccess) (map[string]themePopulation, error) {
	ids := make([]string, 0, len(owners))
	for _, owner := range owners {
		ids = append(ids, owner.id)
	}
	rows, err := tx.Query(ctx, `SELECT association.owner_item_id, count(*),
		bool_or(association.kind = 'song' AND `+directItemSQL("i")+`
			AND ($2::boolean OR i.library_id = ANY($3::text[])) AND i.library_id = $4),
		bool_or(association.kind = 'video' AND `+directItemSQL("i")+`
			AND ($2::boolean OR i.library_id = ANY($3::text[])) AND i.library_id = $4)
		FROM item_theme_resources association JOIN items i ON i.id = association.resource_item_id
		WHERE association.active AND association.owner_item_id = ANY($1::text[])
		GROUP BY association.owner_item_id`, ids, access.all, access.folders, owners[0].libraryID)
	if err != nil {
		return nil, fmt.Errorf("query theme populations: %w", err)
	}
	defer rows.Close()
	result := make(map[string]themePopulation)
	for rows.Next() {
		var id string
		var population themePopulation
		if err := rows.Scan(&id, &population.active, &population.songs, &population.videos); err != nil {
			return nil, fmt.Errorf("read theme population: %w", err)
		}
		result[id] = population
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read theme populations: %w", err)
	}
	return result, nil
}

func readThemeItems(ctx context.Context, tx pgx.Tx, subject Subject, access libraryAccess, libraryID, songOwner, videoOwner string, result *ThemeMediaResult) error {
	rows, err := tx.Query(ctx, "SELECT "+itemColumns+`, association.kind FROM item_theme_resources association
		JOIN items i ON i.id = association.resource_item_id
		WHERE association.active AND ((association.kind = 'song' AND association.owner_item_id = $1)
			OR (association.kind = 'video' AND association.owner_item_id = $2))
			AND `+directItemSQL("i")+` AND i.library_id = $3
			AND ($4::boolean OR i.library_id = ANY($5::text[]))
		ORDER BY association.kind, lower(i.sort_name) COLLATE "C", i.id`,
		songOwner, videoOwner, libraryID, access.all, access.folders)
	if err != nil {
		return fmt.Errorf("query theme resources: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	kinds := make([]string, 0)
	for rows.Next() {
		var kind string
		item, err := scanItem(rows, &kind)
		if err != nil {
			return fmt.Errorf("scan theme resource: %w", err)
		}
		item.CanPlay = access.canPlay
		items, kinds = append(items, item), append(kinds, kind)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read theme resources: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, subject.UserID, items); err != nil {
		return err
	}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		return err
	}
	if err := attachThemeItemAttributes(ctx, tx, items); err != nil {
		return err
	}
	for index, item := range items {
		if kinds[index] == "song" {
			result.ThemeSongsResult.Items = append(result.ThemeSongsResult.Items, item)
		} else {
			result.ThemeVideosResult.Items = append(result.ThemeVideosResult.Items, item)
		}
	}
	result.ThemeSongsResult.TotalRecordCount = len(result.ThemeSongsResult.Items)
	result.ThemeVideosResult.TotalRecordCount = len(result.ThemeVideosResult.Items)
	return nil
}

// attachThemeItemAttributes is shared by theme groups and already authorized
// direct item projections in the same transaction. The shared direct predicate
// requires the resource and ordinary owner to share that authorized library.
// It borrows only the active owner's genres in the same snapshot;
// neither source metadata nor catalog relationships are persisted by a read.
// Explicit native controls, including empty overrides and empty saved locks,
// prevent fallback. The existing NFO parser does not implement NFO lock fields.
func attachThemeItemAttributes(ctx context.Context, tx pgx.Tx, items []Item) error {
	positions := make(map[string][]int)
	ids := make([]string, 0, len(items))
	for index, item := range items {
		if len(positions[item.ID]) == 0 {
			ids = append(ids, item.ID)
		}
		positions[item.ID] = append(positions[item.ID], index)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT i.id, relationship.kind, state.item_id IS NOT NULL,
		COALESCE(state.overrides ? 'Genres' OR state.locked_values ? 'Genres',false),
		COALESCE(owner_state.effective,owner.local_metadata),
		(SELECT COALESCE(jsonb_agg(jsonb_build_object('ID',entity.id,'Name',credit.display_name)
			ORDER BY credit.position,entity.id),'[]'::jsonb)
			FROM item_entities credit JOIN catalog_entities entity ON entity.id=credit.entity_id
			WHERE credit.item_id=owner.id AND entity.kind='Genre' AND credit.credit_group=0 AND credit.credit_type='')
		FROM item_theme_resources relationship JOIN items i ON i.id=relationship.resource_item_id
		JOIN items owner ON owner.id=relationship.owner_item_id
		LEFT JOIN item_metadata_state state ON state.item_id=i.id
		LEFT JOIN item_metadata_state owner_state ON owner_state.item_id=owner.id
		WHERE relationship.active AND i.id=ANY($1::text[]) AND `+directItemSQL("i")+`
			AND `+ordinaryItemSQL("owner")+` AND owner.library_id=i.library_id`, ids)
	if err != nil {
		return fmt.Errorf("query inherited theme genres: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind string
		var hasState, controlled bool
		var source, entities []byte
		if err := rows.Scan(&id, &kind, &hasState, &controlled, &source, &entities); err != nil {
			return fmt.Errorf("read inherited theme genres: %w", err)
		}
		if !hasState {
			return fmt.Errorf("%w: theme resource metadata controls are unavailable", ErrUnavailable)
		}
		for _, index := range positions[id] {
			items[index].ThemeKind = kind
		}
		needsGenres := false
		for _, index := range positions[id] {
			if !controlled && len(items[index].Entities.Genres) == 0 &&
				(items[index].Metadata == nil || len(items[index].Metadata.Genres) == 0) {
				needsGenres = true
			}
		}
		if !needsGenres {
			continue
		}
		var ownerMetadata struct{ Genres []string }
		var genres []EntityRef
		if len(source) != 0 && json.Unmarshal(source, &ownerMetadata) != nil || json.Unmarshal(entities, &genres) != nil {
			return fmt.Errorf("%w: inherited theme genre projection is invalid", ErrUnavailable)
		}
		if len(ownerMetadata.Genres) == 0 && len(genres) == 0 {
			continue
		}
		for _, index := range positions[id] {
			if controlled || len(items[index].Entities.Genres) != 0 ||
				items[index].Metadata != nil && len(items[index].Metadata.Genres) != 0 {
				continue
			}
			projection := metadata.Metadata{}
			if items[index].Metadata != nil {
				projection = *items[index].Metadata
			}
			projection.Genres = append([]string(nil), ownerMetadata.Genres...)
			items[index].Metadata = &projection
			items[index].Entities.Genres = append([]EntityRef(nil), genres...)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read inherited theme genres: %w", err)
	}
	return nil
}
