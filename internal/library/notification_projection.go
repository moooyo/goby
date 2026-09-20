package library

import (
	"context"
	"github.com/moooyo/goby/internal/notificationjournal"
	"strconv"
)

// FilterNotificationReferences applies one current Subject snapshot to explicit
// typed references. Decimal opaque item IDs never dispatch into entity reads.
func (s *Store) FilterNotificationReferences(ctx context.Context, subject Subject, refs []notificationjournal.Reference) ([]notificationjournal.Reference, error) {
	if len(refs) > 4096 {
		return nil, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	items := []string{}
	entities := []int64{}
	for _, ref := range refs {
		switch ref.Kind {
		case "Item", "Library":
			items = append(items, ref.ID)
			if ref.SourceID != "" {
				items = append(items, ref.SourceID)
			}
		case "Entity":
			id, e := strconv.ParseInt(ref.ID, 10, 64)
			if e != nil || id < 1 || strconv.FormatInt(id, 10) != ref.ID {
				return nil, ErrInvalidInput
			}
			entities = append(entities, id)
		default:
			return nil, ErrInvalidInput
		}
	}
	visibleItems := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT i.id,i.library_id FROM items i WHERE i.id=ANY($1::text[]) AND `+access.ordinarySQL("i"), items)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, lib string
		if err = rows.Scan(&id, &lib); err != nil {
			rows.Close()
			return nil, err
		}
		visibleItems[id] = lib
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	visibleEntities := map[string]bool{}
	rows, err = tx.Query(ctx, `SELECT DISTINCT entity.id::text FROM catalog_entities entity JOIN item_entities association ON association.entity_id=entity.id JOIN items i ON i.id=association.item_id WHERE entity.id=ANY($1::bigint[]) AND `+validEntityAssociationSQL+` AND `+access.ordinarySQL("i"), entities)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		visibleEntities[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []notificationjournal.Reference{}
	seen := map[notificationjournal.Reference]bool{}
	for _, ref := range refs {
		visible := visibleEntities[ref.ID] && ref.Kind == "Entity"
		if ref.Kind != "Entity" {
			lib, found := visibleItems[ref.ID]
			visible = found && (ref.LibraryID == "" || ref.LibraryID == lib)
			if ref.SourceID != "" {
				sourceLib, found := visibleItems[ref.SourceID]
				visible = visible && found && sourceLib == ref.LibraryID
			}
		}
		if visible && !seen[ref] {
			seen[ref] = true
			result = append(result, ref)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
