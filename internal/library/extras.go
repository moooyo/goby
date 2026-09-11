package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
)

const (
	ExtraKindClip         = "clip"
	ExtraKindDeletedScene = "deleted_scene"
	ExtraKindTrailer      = "trailer"
	// MaxExtraResourcesPerOwner bounds the complete active population across
	// SpecialFeatures and LocalTrailers. A read must never truncate this set.
	MaxExtraResourcesPerOwner = 256
)

// QuerySpecialFeatures returns the owner's active non-trailer attachments.
// Resource identifiers remain ordinary item IDs, never theme-owner numbers.
func (s *Store) QuerySpecialFeatures(ctx context.Context, ownerID string, subject Subject) ([]Item, error) {
	return s.queryExtraItems(ctx, ownerID, subject, false)
}

// QueryLocalTrailers returns the separately indexed local trailer population.
func (s *Store) QueryLocalTrailers(ctx context.Context, ownerID string, subject Subject) ([]Item, error) {
	return s.queryExtraItems(ctx, ownerID, subject, true)
}

func (s *Store) queryExtraItems(ctx context.Context, ownerID string, subject Subject, trailers bool) ([]Item, error) {
	if ownerID == "" || len(ownerID) > 256 || !utf8.ValidString(ownerID) || strings.TrimSpace(ownerID) != ownerID ||
		strings.IndexFunc(ownerID, unicode.IsControl) >= 0 {
		return nil, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	var libraryID, ownerName string
	err = tx.QueryRow(ctx, `SELECT i.library_id, i.name FROM items i WHERE i.id=$1
		AND `+ordinaryItemSQL("i")+` AND ($2::boolean OR i.library_id=ANY($3::text[]))`,
		ownerID, access.all, access.folders).Scan(&libraryID, &ownerName)
	if errors.Is(err, pgx.ErrNoRows) {
		if !trailers {
			// The reference SpecialFeatures endpoint distinguishes an absent
			// item from an existing inaccessible owner. Subject authority has
			// already been checked; never turn a denied existing item into [].
			var exists bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM items WHERE id=$1)", ownerID).Scan(&exists); err != nil {
				return nil, fmt.Errorf("check missing extra owner: %w", err)
			}
			if !exists {
				if err := tx.Commit(ctx); err != nil {
					return nil, fmt.Errorf("complete missing special-features query: %w", err)
				}
				return make([]Item, 0), nil
			}
		}
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("authorize extra owner: %w", err)
	}
	// Count every active kind before selecting an endpoint. Invalid siblings
	// cannot turn a partial result into a successful complete-array response.
	var active, valid int64
	err = tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE
		i.library_id=$2 AND ($3::boolean OR i.library_id=ANY($4::text[]))
		AND `+database.ExtraResourceItemSQL("i", true)+`) FROM item_extra_resources association
		LEFT JOIN items i ON i.id=association.resource_item_id
		WHERE association.owner_item_id=$1 AND association.active`,
		ownerID, libraryID, access.all, access.folders).Scan(&active, &valid)
	if err != nil {
		return nil, fmt.Errorf("read extra population: %w", err)
	}
	if active > MaxExtraResourcesPerOwner || active != valid {
		return nil, fmt.Errorf("%w: extra population is invalid or exceeds its owner limit", ErrUnavailable)
	}
	rows, err := tx.Query(ctx, "SELECT "+itemColumns+`, association.kind FROM item_extra_resources association
		JOIN items i ON i.id=association.resource_item_id
		WHERE association.owner_item_id=$1 AND association.active
		AND (association.kind='trailer')=$2 AND i.library_id=$3
		AND ($4::boolean OR i.library_id=ANY($5::text[]))
		AND `+database.ExtraResourceItemSQL("i", true)+`
		ORDER BY lower(i.sort_name) COLLATE "C", i.id`, ownerID, trailers, libraryID, access.all, access.folders)
	if err != nil {
		return nil, fmt.Errorf("query extra resources: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var kind string
		item, err := scanItem(rows, &kind)
		if err != nil {
			return nil, fmt.Errorf("scan extra resource: %w", err)
		}
		item.ExtraKind, item.ExtraOwnerName, item.CanPlay = kind, ownerName, access.canPlay
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read extra resources: %w", err)
	}
	rows.Close()
	if err := attachUserData(ctx, tx, subject.UserID, items); err != nil {
		return nil, err
	}
	if err := attachSubtitles(ctx, tx, items); err != nil {
		return nil, err
	}
	if err := attachExtraItemAttributes(ctx, tx, items); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("complete extra resource query: %w", err)
	}
	return items, nil
}

// attachExtraItemAttributes enriches already authorized direct items within
// their existing snapshot. Only the owner's display name is borrowed; source
// metadata and per-resource state remain independent of the main movie.
func attachExtraItemAttributes(ctx context.Context, tx pgx.Tx, items []Item) error {
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
	rows, err := tx.Query(ctx, `SELECT i.id, association.kind, owner.name, state.item_id IS NOT NULL,
		COALESCE(state.overrides ? 'Name' OR state.locked_values ? 'Name',false),
		COALESCE(state.overrides ? 'SortName' OR state.locked_values ? 'SortName',false)
		FROM item_extra_resources association JOIN items i ON i.id=association.resource_item_id
		JOIN items owner ON owner.id=association.owner_item_id
		LEFT JOIN item_metadata_state state ON state.item_id=i.id
		WHERE i.id=ANY($1::text[]) AND `+database.ExtraResourceItemSQL("i", true), ids)
	if err != nil {
		return fmt.Errorf("query extra item attributes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, kind, ownerName string
		var hasState, nameControlled, sortNameControlled bool
		if err := rows.Scan(&id, &kind, &ownerName, &hasState, &nameControlled, &sortNameControlled); err != nil {
			return fmt.Errorf("read extra item attributes: %w", err)
		}
		if !hasState {
			return fmt.Errorf("%w: extra resource metadata controls are unavailable", ErrUnavailable)
		}
		for _, index := range positions[id] {
			items[index].ExtraKind, items[index].ExtraOwnerName = kind, ownerName
			items[index].ExtraNameControlled, items[index].ExtraSortNameControlled = nameControlled, sortNameControlled
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read extra item attributes: %w", err)
	}
	rows.Close()
	return attachExtraParentCounts(ctx, tx, items, positions, ids)
}

func attachExtraParentCounts(ctx context.Context, tx pgx.Tx, items []Item, positions map[string][]int, ids []string) error {
	rows, err := tx.Query(ctx, `SELECT association.owner_item_id, count(*) FROM item_extra_resources association
		JOIN items i ON i.id=association.resource_item_id
		WHERE association.owner_item_id=ANY($1::text[]) AND association.kind='trailer'
		AND `+database.ExtraResourceItemSQL("i", true)+` GROUP BY association.owner_item_id`, ids)
	if err != nil {
		return fmt.Errorf("query local trailer counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return fmt.Errorf("read local trailer count: %w", err)
		}
		for _, index := range positions[id] {
			items[index].LocalTrailerCount = new(int)
			*items[index].LocalTrailerCount = count
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read local trailer counts: %w", err)
	}
	return nil
}
