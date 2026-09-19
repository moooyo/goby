package library

import (
	"context"
	"errors"
	"strconv"
)

// VisibleUserDataFor authorizes notification identities without replacing the
// immutable published values. Physical items and independent numeric entities
// share one current policy snapshot; no associated media state is projected.
func (s *Store) VisibleUserDataFor(ctx context.Context, subject Subject, ids []string) (map[string]bool, error) {
	if subject.UserID == "" || len(ids) > 256 {
		return nil, ErrInvalidInput
	}
	physical := make([]string, 0, len(ids))
	entities := make([]int64, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !metadataIdentifier(id) {
			return nil, ErrInvalidInput
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		value, err := strconv.ParseInt(id, 10, 64)
		if err == nil && value > 0 && strconv.FormatInt(value, 10) == id {
			entities = append(entities, value)
		} else {
			physical = append(physical, id)
		}
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	result := make(map[string]bool, len(ids))
	if len(physical) > 0 {
		rows, err := tx.Query(ctx, "SELECT i.id FROM items i WHERE i.id=ANY($1::text[]) AND i.type IN ("+userDataFolderTypesSQL+") AND "+access.directSQL("i"), physical)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			result[id] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, id := range entities {
		err := visibleEntityForState(ctx, tx, access, id, false)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result[strconv.FormatInt(id, 10)] = true
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
