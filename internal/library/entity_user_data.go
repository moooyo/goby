package library

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
)

// One visible association is sufficient, but a mutation retains both that
// association and its item. Scanner/item locks precede entity foreign-key locks.
func visibleEntityForState(ctx context.Context, tx pgx.Tx, access libraryAccess, id int64, lock bool) error {
	if id <= 0 {
		return ErrInvalidInput
	}
	statement := `SELECT i.id FROM catalog_entities entity JOIN item_entities association ON association.entity_id=entity.id
		JOIN items i ON i.id=association.item_id WHERE entity.id=$1 AND i.type<>'CollectionFolder' AND ` +
		access.ordinarySQL("i") + " AND " + validEntityAssociationSQL + " ORDER BY i.id LIMIT 1"
	if lock {
		statement += " FOR SHARE OF i,association"
	}
	var itemID string
	err := tx.QueryRow(ctx, statement, id).Scan(&itemID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize catalog entity: %w", err)
	}
	return nil
}

const entityUserDataColumns = `entity_id::text,playback_position_ticks,play_count,is_favorite,played,last_played_at,rating,likes`

func scanEntityUserData(row rowScanner) (UserData, error) {
	var data UserData
	err := row.Scan(&data.ItemID, &data.PlaybackPositionTicks, &data.PlayCount, &data.IsFavorite, &data.Played, &data.LastPlayedDate, &data.Rating, &data.Likes)
	if data.LastPlayedDate != nil {
		value := data.LastPlayedDate.UTC()
		data.LastPlayedDate = &value
	}
	return data, err
}

func (s *Store) GetEntityUserDataFor(ctx context.Context, subject Subject, id int64) (UserData, error) {
	if subject.UserID == "" || id <= 0 {
		return UserData{}, ErrInvalidInput
	}
	tx, access, err := s.beginSubjectRead(ctx, subject)
	if err != nil {
		return UserData{}, err
	}
	defer rollback(tx)
	if err := visibleEntityForState(ctx, tx, access, id, false); err != nil {
		return UserData{}, err
	}
	data, err := scanEntityUserData(tx.QueryRow(ctx, "SELECT "+entityUserDataColumns+" FROM entity_user_data WHERE user_id=$1 AND entity_id=$2", subject.UserID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		data, err = UserData{ItemID: strconv.FormatInt(id, 10)}, nil
	}
	if err != nil {
		return UserData{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserData{}, err
	}
	return data, nil
}

func (s *Store) UpdateEntityUserDataFor(ctx context.Context, subject Subject, id int64, patch UserDataPatch) (UserData, error) {
	if subject.UserID == "" || id <= 0 || patch.PlaybackPositionTicks != nil && *patch.PlaybackPositionTicks != 0 || patch.HideFromResume != nil && *patch.HideFromResume {
		return UserData{}, ErrInvalidInput
	}
	if err := patch.Validate(); err != nil {
		return UserData{}, err
	}
	tx, access, err := s.beginSubjectStateWrite(ctx, subject, false)
	if err != nil {
		return UserData{}, err
	}
	defer rollback(tx)
	if err := visibleEntityForState(ctx, tx, access, id, true); err != nil {
		return UserData{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entity_user_data(user_id,entity_id) VALUES($1,$2) ON CONFLICT(user_id,entity_id) DO NOTHING`, subject.UserID, id); err != nil {
		return UserData{}, err
	}
	current, err := scanEntityUserData(tx.QueryRow(ctx, "SELECT "+entityUserDataColumns+" FROM entity_user_data WHERE user_id=$1 AND entity_id=$2 FOR UPDATE", subject.UserID, id))
	if err != nil {
		return UserData{}, err
	}
	if patch.PlayCount != nil {
		current.PlayCount = *patch.PlayCount
	}
	if patch.IsFavorite != nil {
		current.IsFavorite = *patch.IsFavorite
	}
	if patch.Played != nil {
		current.Played = *patch.Played
	}
	if patch.LastPlayedDate != nil {
		value := patch.LastPlayedDate.UTC()
		current.LastPlayedDate = &value
	}
	if patch.ClearLastPlayedDate {
		current.LastPlayedDate = nil
	}
	if patch.Rating != nil {
		value := *patch.Rating
		current.Rating = &value
		current.Likes = nil
	}
	if patch.ClearRating {
		current.Rating = nil
	}
	if patch.Likes != nil {
		value := *patch.Likes
		current.Likes = &value
	}
	if patch.ClearLikes {
		current.Likes = nil
	}
	data, err := scanEntityUserData(tx.QueryRow(ctx, `UPDATE entity_user_data SET play_count=$3,is_favorite=$4,played=$5,
		last_played_at=$6,rating=$7,likes=$8,updated_at=clock_timestamp() WHERE user_id=$1 AND entity_id=$2 RETURNING `+entityUserDataColumns,
		subject.UserID, id, current.PlayCount, current.IsFavorite, current.Played, current.LastPlayedDate, current.Rating, current.Likes))
	if err != nil {
		return UserData{}, err
	}
	if _, err := checkSubjectStateWrite(ctx, tx, subject, false, false); err != nil {
		return UserData{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return UserData{}, err
	}
	return data, nil
}

// The entity IDs have already passed the caller's same-transaction visibility
// query. Userless application reads have no synthetic shared user preferences.
func populateEntityProjections(ctx context.Context, tx pgx.Tx, subject Subject, access libraryAccess, entities []Entity) error {
	if len(entities) == 0 {
		return nil
	}
	ids := make([]int64, len(entities))
	byID := make(map[int64]*Entity, len(entities))
	for index := range entities {
		ids[index], byID[entities[index].ID] = entities[index].ID, &entities[index]
		entities[index].Images = []Image{}
		if subject.UserID != "" {
			entities[index].UserData = &UserData{ItemID: strconv.FormatInt(entities[index].ID, 10)}
		}
	}
	rows, err := tx.Query(ctx, `SELECT state.entity_id,im.image_type,im.image_index,im.source_hash,im.mime_type,im.width,im.height,octet_length(im.content),im.modified_at
		FROM artwork_state state JOIN artwork_images im ON im.state_id=state.id WHERE state.entity_id=ANY($1::bigint[]) ORDER BY state.entity_id,im.image_type,im.image_index`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var image artwork.StoredImage
		if err := rows.Scan(&id, &image.ImageType, &image.ImageIndex, &image.Tag, &image.MIMEType, &image.Width, &image.Height, &image.Size, &image.ModifiedAt); err != nil {
			rows.Close()
			return err
		}
		if entity := byID[id]; entity != nil {
			entity.Images = append(entity.Images, imageFromManaged(image))
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for index := range entities {
		entity := &entities[index]
		if entity.Type != "Genre" || hasPrimaryImage(entity.Images) {
			continue
		}
		manifest, err := readCollageManifest(ctx, tx, access, ArtworkTarget{EntityID: entity.ID})
		if err != nil {
			return err
		}
		if manifest != nil {
			entity.Images = append([]Image{manifest.image()}, entity.Images...)
		}
	}
	if subject.UserID == "" {
		return nil
	}
	rows, err = tx.Query(ctx, "SELECT "+entityUserDataColumns+" FROM entity_user_data WHERE user_id=$1 AND entity_id=ANY($2::bigint[])", subject.UserID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		data, err := scanEntityUserData(rows)
		if err != nil {
			return err
		}
		id, err := strconv.ParseInt(data.ItemID, 10, 64)
		if err != nil {
			return ErrUnavailable
		}
		if entity := byID[id]; entity != nil {
			entity.UserData = &data
		}
	}
	return rows.Err()
}
