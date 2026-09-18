package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// resolveCollectionMembers checks every requested identity before changing any
// entry. Playlist folders expand in stable catalog order; explicit duplicates
// remain distinct entries. Collection references are never playback resources.
func resolveCollectionMembers(ctx context.Context, tx pgx.Tx, access libraryAccess, collection CollectionInfo, requested []string) ([]string, error) {
	result := make([]string, 0, len(requested))
	for _, id := range requested {
		if id == collection.ID {
			return nil, ErrInvalidInput
		}
		var kind string
		var folder bool
		if err := tx.QueryRow(ctx, `SELECT i.type,i.is_folder FROM items i WHERE i.id=$1 AND `+access.itemPolicySQL("i")+` AND `+ordinaryItemSQL("i"), id).Scan(&kind, &folder); errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		} else if err != nil {
			return nil, err
		}
		if collection.Kind == BoxSetKind {
			if kind == PlaylistKind || kind == BoxSetKind || kind == "CollectionFolder" {
				return nil, ErrInvalidInput
			}
			result = append(result, id)
			continue
		}
		if kind == BoxSetKind {
			return nil, ErrInvalidInput
		}
		if !folder {
			if !playlistMediaCompatible(kind, collection.MediaType) {
				return nil, ErrInvalidInput
			}
			result = append(result, id)
			continue
		}
		var statement string
		mediaFilter := "i.type IN ('Audio','Movie','Episode','Video','MusicVideo')"
		if collection.MediaType == "Audio" {
			mediaFilter = "i.type='Audio'"
		}
		if collection.MediaType == "Video" {
			mediaFilter = "i.type IN ('Movie','Episode','Video','MusicVideo')"
		}
		if kind == PlaylistKind {
			statement = `SELECT i.id,i.type FROM media_collection_entries e JOIN items i ON i.id=e.item_id WHERE e.collection_id=$1 AND ` + access.itemPolicySQL("i") + ` AND ` + ordinaryItemSQL("i") + ` AND ` + mediaFilter + ` ORDER BY e.position,e.id LIMIT $2`
		} else {
			statement = `WITH RECURSIVE descendants AS (
				SELECT i.id,i.library_id FROM items i WHERE i.id=$1
				UNION
				SELECT child.id,child.library_id FROM items child JOIN descendants parent ON child.parent_id=parent.id AND child.library_id=parent.library_id
				WHERE ` + ordinaryItemSQL("child") + `
			) SELECT i.id,i.type FROM descendants d JOIN items i ON i.id=d.id WHERE NOT i.is_folder AND ` + mediaFilter + ` AND ` + access.itemPolicySQL("i") + ` AND ` + ordinaryItemSQL("i") + ` ORDER BY i.sort_name,i.id LIMIT $2`
		}
		rows, err := tx.Query(ctx, statement, id, collectionEntryLimit+1)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var member, memberKind string
			if err := rows.Scan(&member, &memberKind); err != nil {
				rows.Close()
				return nil, err
			}
			if playlistMediaCompatible(memberKind, collection.MediaType) {
				result = append(result, member)
			}
			if len(result) > collectionEntryLimit {
				rows.Close()
				return nil, fmt.Errorf("%w: collection entry limit exceeded", ErrInvalidInput)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if len(result) > collectionEntryLimit {
		return nil, ErrInvalidInput
	}
	return result, nil
}

func playlistMediaCompatible(kind, mediaType string) bool {
	if kind == "Audio" {
		return mediaType == "" || mediaType == "Audio"
	}
	if kind == "Movie" || kind == "Episode" || kind == "Video" || kind == "MusicVideo" {
		return mediaType == "" || mediaType == "Video"
	}
	return false
}

func appendCollectionMembers(ctx context.Context, tx pgx.Tx, collection CollectionInfo, members []string) (int, error) {
	if err := normalizeCollectionPositions(ctx, tx, collection.ID); err != nil {
		return 0, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM media_collection_entries WHERE collection_id=$1`, collection.ID).Scan(&count); err != nil {
		return 0, err
	}
	if collection.Kind == BoxSetKind {
		seen, err := collectionMemberSet(ctx, tx, collection.ID)
		if err != nil {
			return 0, err
		}
		unique := make([]string, 0, len(members))
		for _, id := range members {
			if !seen[id] {
				unique = append(unique, id)
				seen[id] = true
			}
		}
		members = unique
	}
	if count+len(members) > collectionEntryLimit {
		return 0, fmt.Errorf("%w: collection entry limit exceeded", ErrInvalidInput)
	}
	if len(members) == 0 {
		return 0, nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO media_collection_entries (collection_id,item_id,position)
		SELECT $1,member.id,($3::bigint+member.ordinality-1)::integer FROM unnest($2::text[]) WITH ORDINALITY AS member(id,ordinality)`, collection.ID, members, count)
	if err != nil {
		return 0, err
	}
	return len(members), nil
}
