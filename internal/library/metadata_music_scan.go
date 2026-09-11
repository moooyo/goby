package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

func (state *scanState) queueMusicParent(id string) {
	if id == "" {
		return
	}
	if state.musicParents == nil {
		state.musicParents = make(map[string]bool)
	}
	state.musicParents[id] = true
}

func musicSourceFromProbe(probe *media.Info) (musicMetadataSource, bool) {
	if probe == nil || probe.EmbeddedMusic == nil || !validTrackMusic(*probe.EmbeddedMusic) {
		return musicMetadataSource{}, false
	}
	facts := probe.EmbeddedMusic
	source := musicMetadataSource{Version: musicSourceVersion, Name: facts.Title, Album: facts.Album,
		Artists: []string{}, AlbumArtists: []string{}}
	if strings.TrimSpace(facts.Artist) != "" {
		source.Artists = append(source.Artists, facts.Artist)
	}
	if strings.TrimSpace(facts.AlbumArtist) != "" {
		source.AlbumArtists = append(source.AlbumArtists, facts.AlbumArtist)
	}
	return source, true
}

func validTrackMusic(facts media.MusicMetadata) bool {
	if facts.Version != media.CurrentMusicMetadataVersion {
		return false
	}
	total := 0
	for _, value := range []string{facts.Title, facts.Album, facts.Artist, facts.AlbumArtist} {
		if !utf8.ValidString(value) || len(value) > metadataValueMaxName || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return false
		}
		total += len(value)
	}
	return total <= 4*metadataValueMaxName
}

func encodeAcceptedMusicSource(source musicMetadataSource) ([]byte, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, fmt.Errorf("encode accepted music source: %w", err)
	}
	if _, extracted, err := decodeAcceptedMusicSource(encoded); err != nil || !extracted {
		return nil, fmt.Errorf("accepted music source is invalid")
	}
	return encoded, nil
}

func acceptedTrackMusic(raw []byte) (media.MusicMetadata, bool) {
	if len(raw) > musicSourceMaxBytes {
		return media.MusicMetadata{}, false
	}
	object, err := metadataSourceObject(raw)
	if err != nil || len(object) == 0 {
		return media.MusicMetadata{}, false
	}
	for field := range object {
		if field != "Version" && field != "Title" && field != "Album" && field != "Artist" && field != "AlbumArtist" {
			return media.MusicMetadata{}, false
		}
	}
	var facts media.MusicMetadata
	if json.Unmarshal(object["Version"], &facts.Version) != nil || facts.Version != media.CurrentMusicMetadataVersion {
		return media.MusicMetadata{}, false
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "Title", value: &facts.Title},
		{name: "Album", value: &facts.Album},
		{name: "Artist", value: &facts.Artist},
		{name: "AlbumArtist", value: &facts.AlbumArtist},
	} {
		if value, exists := object[field.name]; exists {
			text, err := acceptedMusicText(value)
			if err != nil {
				return media.MusicMetadata{}, false
			}
			*field.value = text
		}
	}
	if !validTrackMusic(facts) {
		return media.MusicMetadata{}, false
	}
	return facts, true
}

func writeScannedMusicSource(ctx context.Context, tx pgx.Tx, itemID, itemType string, probe *media.Info) error {
	var encoded []byte
	if itemType == "Audio" {
		source, read := musicSourceFromProbe(probe)
		if !read {
			return nil
		}
		var err error
		encoded, err = encodeAcceptedMusicSource(source)
		if err != nil {
			return err
		}
	} else {
		encoded = []byte(`{}`)
	}
	if _, err := tx.Exec(ctx, `UPDATE item_metadata_state SET music_source = $2::jsonb
		WHERE item_id = $1 AND music_source IS DISTINCT FROM $2::jsonb`, itemID, encoded); err != nil {
		return fmt.Errorf("retain accepted music source: %w", err)
	}
	return nil
}

// Album publication follows complete root enumeration. File transactions that
// succeeded before a later failure retain their own accepted probe facts, while
// incomplete roots retain their previous album source. Each album is published
// in its own owned transaction. Cancellation retains earlier committed albums
// as accepted facts and leaves albums not yet processed unchanged.
func (s *Store) refreshScannedMusicAlbums(ctx context.Context, libraryID string, parents, completeRoots map[string]bool) (int, error) {
	if len(parents) == 0 {
		return 0, nil
	}
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)
	parentIDs := make([]string, 0, len(parents))
	for id := range parents {
		parentIDs = append(parentIDs, id)
	}
	sort.Strings(parentIDs)
	albums := make(map[string]bool)
	for _, parentID := range parentIDs {
		var albumID, rootID string
		err := tx.QueryRow(ctx, `WITH RECURSIVE ancestors AS (
			SELECT id, parent_id, library_id, root_id, type, is_folder, ARRAY[id] AS visited
			FROM items WHERE id = $1 AND library_id = $2
			UNION ALL
			SELECT parent.id, parent.parent_id, parent.library_id, parent.root_id, parent.type, parent.is_folder,
				child.visited || parent.id FROM ancestors child JOIN items parent
				ON parent.id = child.parent_id AND parent.library_id = child.library_id
			WHERE NOT (child.type = 'MusicAlbum' AND child.is_folder) AND NOT parent.id = ANY(child.visited)
		) SELECT id, COALESCE(root_id, '') FROM ancestors WHERE type = 'MusicAlbum' AND is_folder`,
			parentID, libraryID).Scan(&albumID, &rootID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("resolve accepted music album: %w", err)
		}
		if completeRoots[rootID] {
			albums[albumID] = true
		}
	}
	albumIDs := make([]string, 0, len(albums))
	for id := range albums {
		albumIDs = append(albumIDs, id)
	}
	sort.Strings(albumIDs)
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("complete music album discovery: %w", err)
	}
	warnings := 0
	for _, albumID := range albumIDs {
		ready, err := s.refreshOwnedMusicAlbum(ctx, libraryID, albumID)
		if err != nil {
			return warnings, err
		}
		if !ready {
			warnings++
		}
	}
	return warnings, nil
}

func (s *Store) refreshOwnedMusicAlbum(ctx context.Context, libraryID, albumID string) (bool, error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	ready, err := refreshAcceptedMusicAlbum(ctx, tx, libraryID, albumID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit accepted music album source: %w", err)
	}
	return ready, nil
}

func refreshAcceptedMusicAlbum(ctx context.Context, tx pgx.Tx, libraryID, albumID string) (bool, error) {
	var path, rootPath, existingName string
	var localSource []byte
	err := tx.QueryRow(ctx, `SELECT i.path, COALESCE(root.path, ''), i.name, i.local_metadata
		FROM items i JOIN item_metadata_state ms ON ms.item_id = i.id
		LEFT JOIN library_roots root ON root.id = i.root_id AND root.library_id = i.library_id
		WHERE i.id = $1 AND i.library_id = $2 AND i.type = 'MusicAlbum' AND i.is_folder
		FOR UPDATE OF i, ms`, albumID, libraryID).Scan(&path, &rootPath, &existingName, &localSource)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("lock accepted music album: %w", err)
	}
	queryCtx := ctx
	if owned, ok := tx.(*ownedTx); ok {
		// Finish the current accepted album on the ownership transaction's bounded
		// context, matching Exec/QueryRow without cancelling its lock connection.
		queryCtx = owned.ctx
	}
	rows, err := tx.Query(queryCtx, `WITH RECURSIVE members AS (
		SELECT id, library_id FROM items WHERE id = $1 AND library_id = $2
		UNION
		SELECT child.id, child.library_id FROM members parent JOIN items child
			ON child.parent_id = parent.id AND child.library_id = parent.library_id
		WHERE NOT (child.type = 'MusicAlbum' AND child.is_folder)
	) SELECT i.media -> 'EmbeddedMusic' FROM members member JOIN items i ON i.id = member.id AND i.library_id = member.library_id
		WHERE i.type = 'Audio' AND NOT i.is_folder
		ORDER BY i.parent_index_number, i.index_number, i.id`, albumID, libraryID)
	if err != nil {
		return false, fmt.Errorf("read accepted album members: %w", err)
	}
	defer rows.Close()
	source := musicMetadataSource{Version: musicSourceVersion, Artists: []string{}, AlbumArtists: []string{}}
	seenArtists := make(map[string]bool)
	var uniformAlbum, uniformArtist, uniformAlbumArtist string
	allAlbums, allArtists, readAll, withinBounds := true, true, true, true
	allAlbumArtists, noAlbumArtists := true, true
	memberCount := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return false, fmt.Errorf("scan accepted album member: %w", err)
		}
		facts, accepted := acceptedTrackMusic(raw)
		if !accepted {
			readAll = false
			continue
		}
		if memberCount == 0 {
			uniformAlbum, uniformArtist, uniformAlbumArtist = facts.Album, facts.Artist, facts.AlbumArtist
		}
		memberCount++
		allAlbums = allAlbums && strings.TrimSpace(facts.Album) != "" && facts.Album == uniformAlbum
		allArtists = allArtists && strings.TrimSpace(facts.Artist) != "" && facts.Artist == uniformArtist
		albumArtistPresent := strings.TrimSpace(facts.AlbumArtist) != ""
		allAlbumArtists = allAlbumArtists && albumArtistPresent && facts.AlbumArtist == uniformAlbumArtist
		noAlbumArtists = noAlbumArtists && !albumArtistPresent
		if strings.TrimSpace(facts.Artist) != "" && !seenArtists[facts.Artist] {
			if len(source.Artists) == musicSourceMaxEntries {
				withinBounds = false
				continue
			}
			source.Artists = append(source.Artists, facts.Artist)
			seenArtists[facts.Artist] = true
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("finish accepted album member read: %w", err)
	}
	rows.Close()
	if !readAll || !withinBounds {
		return false, nil
	}
	if memberCount > 0 && allAlbums {
		source.Name, source.Album = uniformAlbum, uniformAlbum
	}
	// Explicit album artists win only with complete agreement. The historical
	// track-artist fallback applies only when no member supplies album_artist;
	// partial or conflicting explicit facts must not be hidden by that fallback.
	if memberCount > 0 {
		if allAlbumArtists {
			source.AlbumArtists = []string{uniformAlbumArtist}
		} else if noAlbumArtists && allArtists {
			source.AlbumArtists = []string{uniformArtist}
		}
	}
	encoded, err := encodeAcceptedMusicSource(source)
	if err != nil {
		return false, nil
	}
	var changed bool
	if err := tx.QueryRow(ctx, "SELECT music_source IS DISTINCT FROM $2::jsonb FROM item_metadata_state WHERE item_id = $1",
		albumID, encoded).Scan(&changed); err != nil {
		return false, fmt.Errorf("compare accepted album source: %w", err)
	}
	if !changed {
		return true, nil
	}
	if _, err := tx.Exec(ctx, "UPDATE item_metadata_state SET music_source = $2::jsonb WHERE item_id = $1", albumID, encoded); err != nil {
		return false, fmt.Errorf("store accepted album source: %w", err)
	}
	physicalPath := path
	if physicalPath == "" {
		physicalPath = rootPath
	}
	fallbackName := existingName
	if physicalPath != "" {
		fallbackName = cleanName(filepath.Base(physicalPath))
	}
	var local localMetadata
	if err := decodeLocalMetadata(localSource, &local); err != nil {
		return false, err
	}
	name, sortName, overview := describeFromLocal(fallbackName, local)
	if err := syncScannedMetadata(ctx, tx, albumID, scannedMetadataOptions{Base: &scannedMetadataBase{
		Name: name, SortName: sortName, Overview: overview,
	}}); err != nil {
		return false, err
	}
	return true, nil
}
