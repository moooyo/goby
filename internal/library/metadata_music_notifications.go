package library

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Raw merged sources retain fields such as Album that a MusicAlbum response
// does not project. Compare its decoded public metadata and entity fields;
// changing an automatic field hidden by administrator controls stays quiet.
func musicAlbumCatalogSnapshot(snapshot scanCatalogSnapshot) (scanCatalogSnapshot, error) {
	var properties map[string]json.RawMessage
	if err := json.Unmarshal([]byte(snapshot.properties), &properties); err != nil {
		return scanCatalogSnapshot{}, fmt.Errorf("decode album notification properties: %w", err)
	}
	var metadata MetadataValues
	if err := json.Unmarshal(properties["Metadata"], &metadata); err != nil {
		return scanCatalogSnapshot{}, fmt.Errorf("decode public album metadata: %w", err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return scanCatalogSnapshot{}, err
	}
	properties["Metadata"] = encoded
	encoded, err = json.Marshal(properties)
	if err != nil {
		return scanCatalogSnapshot{}, err
	}
	snapshot.properties = string(encoded)
	return snapshot, nil
}

// Audio and MusicVideo responses inherit their nearest album's name and album
// artists. Those item projections change even when their own rows stay cached.
func recordMusicAlbumReferenceChanges(ctx context.Context, tx pgx.Tx, libraryID, albumID string, before, after scanCatalogSnapshot) error {
	if before.properties == after.properties {
		return nil
	}
	type reference struct {
		Name     string
		Entities struct {
			AlbumArtists json.RawMessage
		}
	}
	var oldReference, newReference reference
	if err := json.Unmarshal([]byte(before.properties), &oldReference); err != nil {
		return fmt.Errorf("read previous album notification projection: %w", err)
	}
	if err := json.Unmarshal([]byte(after.properties), &newReference); err != nil {
		return fmt.Errorf("read current album notification projection: %w", err)
	}
	if oldReference.Name == newReference.Name && bytes.Equal(oldReference.Entities.AlbumArtists, newReference.Entities.AlbumArtists) {
		return nil
	}
	var encoded []byte
	if err := tx.QueryRow(ctx, `WITH RECURSIVE descendants AS (
		SELECT i.id, i.library_id, i.parent_id, i.type, i.is_folder FROM items i
		WHERE i.id = $1 AND i.library_id = $2 AND `+ordinaryItemSQL("i")+`
		UNION
		SELECT child.id, child.library_id, child.parent_id, child.type, child.is_folder FROM items child
		JOIN descendants parent ON child.parent_id = parent.id AND child.library_id = parent.library_id
		WHERE NOT (child.type = 'MusicAlbum' AND child.is_folder) AND `+ordinaryItemSQL("child")+`
	) SELECT COALESCE(jsonb_agg(jsonb_build_object('ItemID', member.id,
		'ParentID', COALESCE(member.parent_id, '')) ORDER BY member.id), '[]'::jsonb)
	FROM (SELECT consumer.id, consumer.parent_id FROM items consumer JOIN descendants parent
		ON consumer.parent_id = parent.id AND consumer.library_id = parent.library_id
		WHERE consumer.id <> $1 AND consumer.type IN ('Audio', 'MusicVideo') AND NOT consumer.is_folder
		AND `+directItemSQL("consumer")+` ORDER BY consumer.id LIMIT $3) member`, albumID, libraryID, maxCatalogChanges).Scan(&encoded); err != nil {
		return fmt.Errorf("read changed album reference owners: %w", err)
	}
	var members []struct{ ItemID, ParentID string }
	if err := json.Unmarshal(encoded, &members); err != nil {
		return fmt.Errorf("decode changed album reference owners: %w", err)
	}
	changes := make([]CatalogChange, len(members))
	for index, member := range members {
		changes[index] = CatalogChange{Kind: CatalogUpdated, ItemID: member.ItemID, LibraryID: libraryID, ParentID: member.ParentID}
	}
	// The album fact is already retained. Reading at most maxCatalogChanges
	// descendants therefore detects excess through the existing whole-batch
	// resynchronization path, without retaining an unbounded identifier list.
	return recordCatalogChanges(tx, changes...)
}
