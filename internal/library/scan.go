package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

var episodePattern = regexp.MustCompile(`(?i)^(.*?)[ ._-]*s([0-9]{1,3})e([0-9]{1,4})(?:[^0-9].*)?$`)
var seasonPattern = regexp.MustCompile(`(?i)^season[ ._-]*([0-9]{1,3})$`)
var trackPattern = regexp.MustCompile(`^([0-9]{1,3})[ ._-]+(.+)$`)

type hierarchy struct {
	parentID, seriesID, seasonID, albumID string
	seasonNumber                          int
	folderType                            string
	folderIndex                           int
}

type scanState struct {
	store               *Store
	task                *scanTask
	library             Library
	root                libraryRoot
	opened              *os.Root
	warnings            int
	numberingConflicts  int
	directoryIdentities map[string]os.FileInfo
	imageDirectories    map[string]*imageDirectoryIndex
	subtitleDirectories map[string]*subtitleDirectoryIndex
	musicParents        map[string]bool
}

type storedFile struct {
	id, rootID, relativePath, identity, parentID, path, name, itemType, sortName, overview string
	indexNumber, parentIndexNumber                                                         int
	size                                                                                   int64
	modified                                                                               *time.Time
	media                                                                                  *media.Info
	local                                                                                  localMetadata
	automatic                                                                              *MetadataValues
}

func (s *Store) scanLibrary(task *scanTask) (string, error) {
	library, err := s.GetLibrary(task.ctx, task.job.LibraryID)
	if err != nil {
		return "Library is unavailable", err
	}
	rows, err := s.pool.Query(task.ctx, `SELECT id, library_id, path, allowed_path, relative_path
		FROM library_roots WHERE library_id = $1 ORDER BY path`, library.ID)
	if err != nil {
		return "Library directories could not be read", err
	}
	roots := make([]libraryRoot, 0)
	for rows.Next() {
		var root libraryRoot
		if err := rows.Scan(&root.id, &root.libraryID, &root.path, &root.allowedPath, &root.relativePath); err != nil {
			rows.Close()
			return "Library directories could not be read", err
		}
		roots = append(roots, root)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "Library directories could not be read", err
	}
	warnings, failedRoots, numberingConflicts := 0, 0, 0
	musicParents := make(map[string]bool)
	completeRoots := make(map[string]bool)
	for _, root := range roots {
		if err := task.ctx.Err(); err != nil {
			return "Scan cancelled", err
		}
		opened, err := s.openLibraryRoot(root)
		if err != nil {
			failedRoots++
			continue
		}
		state := &scanState{store: s, task: task, library: library, root: root, opened: opened}
		err = state.walk(".", hierarchy{parentID: library.ID}, 0)
		_ = opened.Close()
		for parentID := range state.musicParents {
			musicParents[parentID] = true
		}
		completeRoots[root.id] = err == nil && state.warnings == 0
		warnings += state.warnings
		numberingConflicts += state.numberingConflicts
		if err != nil {
			if task.ctx.Err() != nil {
				return "Scan cancelled", task.ctx.Err()
			}
			failedRoots++
		}
	}
	if _, musicEnabled := s.prober.(interface{ MusicMetadataVersion() int }); musicEnabled {
		musicWarnings, err := s.refreshScannedMusicAlbums(task.ctx, library.ID, musicParents, completeRoots)
		if err != nil {
			return "Accepted music album metadata could not be refreshed", err
		}
		warnings += musicWarnings
	}
	numberingMessage := ""
	if numberingConflicts > 0 {
		numberingMessage = fmt.Sprintf("; %d local metadata numbering conflicts were ignored to preserve the existing hierarchy", numberingConflicts)
	}
	if failedRoots > 0 {
		return fmt.Sprintf("%d media directories could not be scanned; existing catalog records were retained", failedRoots) + numberingMessage, ErrUnavailable
	}
	if warnings > 0 {
		return fmt.Sprintf("%d media or local metadata entries could not be inspected; previous valid metadata was retained", warnings) + numberingMessage, nil
	}
	return "", nil
}

func (state *scanState) walk(relative string, current hierarchy, depth int) error {
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	if depth > 128 {
		state.warnings++
		return nil
	}
	directory, err := openScanFile(state.opened, relative)
	if err != nil {
		return err
	}
	info, err := directory.Stat()
	if err != nil || !info.IsDir() {
		_ = directory.Close()
		return fmt.Errorf("media directory is unavailable")
	}
	// Hold the observed directory while its NFO and children are processed so
	// its identity cannot be recycled after a concurrent rename or removal.
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	if state.directoryIdentities == nil {
		state.directoryIdentities = make(map[string]os.FileInfo)
	}
	directoryKey := filepath.Clean(relative)
	state.directoryIdentities[directoryKey] = info
	defer delete(state.directoryIdentities, directoryKey)
	defer func() { delete(state.imageDirectories, directoryKey) }()
	defer func() { delete(state.subtitleDirectories, directoryKey) }()
	// A directory containing audio files is the album boundary. Local metadata
	// may describe this existing hierarchy but cannot choose a different kind.
	folderType := current.folderType
	albumDirectory := (state.library.CollectionType == "music" || state.library.CollectionType == "mixed") && containsAudio(entries)
	if relative == "." {
		if albumDirectory {
			id, err := state.folder("//album/root", "", cleanName(filepath.Base(state.root.path)), "MusicAlbum", current.parentID, 0, ".")
			if err != nil {
				return err
			}
			current.albumID = id
		}
	} else {
		if albumDirectory {
			folderType = "MusicAlbum"
		}
		id, err := state.folder(filepath.ToSlash(relative), filepath.Join(state.root.path, relative), cleanName(filepath.Base(relative)), folderType, current.parentID, current.folderIndex)
		if err != nil {
			return err
		}
		current.parentID = id
		if folderType == "Series" {
			current.seriesID, current.seasonID = id, ""
		} else if folderType == "Season" {
			current.seasonID, current.seasonNumber = id, current.folderIndex
		} else if folderType == "MusicAlbum" {
			current.albumID = id
		}
	}
	for _, entry := range entries {
		if err := state.task.ctx.Err(); err != nil {
			return err
		}
		if ignoredName(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(relative, entry.Name())
		if entry.IsDir() {
			next := current
			itemType, number := "Folder", 0
			if state.library.CollectionType == "tvshows" || (state.library.CollectionType == "mixed" && current.seriesID != "") {
				if matches := seasonPattern.FindStringSubmatch(entry.Name()); len(matches) != 0 {
					number, _ = strconv.Atoi(matches[1])
					itemType = "Season"
					if next.seriesID == "" {
						next.seriesID, err = state.folder("//series/root", "", cleanName(filepath.Base(state.root.path)), "Series", state.library.ID, 0, ".")
						if err != nil {
							return err
						}
					}
					next.parentID = next.seriesID
				} else if current.seriesID == "" && depth == 0 {
					itemType = "Series"
				}
			}
			next.folderType, next.folderIndex = itemType, number
			if err := state.walk(path, next, depth+1); err != nil {
				return err
			}
			continue
		}
		kind := extensionKind(entry.Name())
		if kind == "" || (state.library.CollectionType == "music" && kind != "audio") || ((state.library.CollectionType == "movies" || state.library.CollectionType == "tvshows") && kind != "video") {
			continue
		}
		if err := state.scanFile(path, kind, current); err != nil {
			return err
		}
	}
	return nil
}

func (state *scanState) scanFile(path, kind string, current hierarchy) error {
	file, err := openScanFile(state.opened, path)
	if err != nil {
		state.warnings++
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		state.warnings++
		return nil
	}
	state.task.job.Scanned++
	if err := state.store.persistProgress(state.task); err != nil {
		return err
	}
	relative := filepath.ToSlash(path)
	stored, err := state.findStoredFile(relative, info)
	if err != nil {
		return err
	}
	probe := stored.media
	// A forced refresh uses the same descriptor checks and persistence path as
	// any changed source, even when the accepted probe facts remain identical.
	unchanged := !state.task.job.ForceProbe && probe != nil && stored.size == info.Size() && stored.modified != nil && stored.modified.Equal(catalogModifiedTime(info)) && (stored.identity == "" || stored.identity == fileIdentity(info))
	versioned, checksVersion := state.store.prober.(interface{ CacheVersion() int })
	if checksVersion {
		unchanged = unchanged && probe.ProbeVersion == versioned.CacheVersion() &&
			probe.FileChangeTimeNs > 0 && probe.FileChangeTimeNs == media.FileChangeTime(info)
	}
	if musicVersioned, ok := state.store.prober.(interface{ MusicMetadataVersion() int }); kind == "audio" && ok {
		unchanged = unchanged && probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == musicVersioned.MusicMetadataVersion()
	}
	if !unchanged {
		probed, probeErr := state.store.prober.ProbeFile(state.task.ctx, file)
		if probeErr != nil {
			if state.task.ctx.Err() != nil {
				return state.task.ctx.Err()
			}
			state.warnings++
			return state.store.persistProgress(state.task)
		}
		probe = &probed
		// A writer may modify the opened inode during probing. Preserve the old
		// catalog entry until a later scan observes a consistent file snapshot.
		after, statErr := file.Stat()
		if statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) ||
			(checksVersion && media.FileChangeTime(after) != media.FileChangeTime(info)) {
			state.warnings++
			return state.store.persistProgress(state.task)
		}
	}
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	if kind == "audio" && probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == media.CurrentMusicMetadataVersion {
		if !validTrackMusic(*probe.EmbeddedMusic) {
			state.warnings++
			return state.store.persistProgress(state.task)
		}
		rawMusic, err := json.Marshal(probe.EmbeddedMusic)
		if _, accepted := acceptedTrackMusic(rawMusic); err != nil || !accepted {
			state.warnings++
			return state.store.persistProgress(state.task)
		}
	}
	name := cleanName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	itemType, parentID, indexNumber, parentIndex := "Movie", current.parentID, 0, 0
	parentNumberDefined := false
	if kind == "audio" {
		itemType = "Audio"
		if current.albumID != "" {
			parentID = current.albumID
		}
		if matches := trackPattern.FindStringSubmatch(name); len(matches) != 0 {
			indexNumber, _ = strconv.Atoi(matches[1])
			name = matches[2]
		}
		if probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == media.CurrentMusicMetadataVersion && probe.EmbeddedMusic.Title != "" {
			name = probe.EmbeddedMusic.Title
		}
	} else {
		matches := episodePattern.FindStringSubmatch(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		if state.library.CollectionType == "tvshows" || (state.library.CollectionType == "mixed" && len(matches) != 0) {
			itemType = "Episode"
			if len(matches) != 0 {
				parentNumberDefined = true
				parentIndex, _ = strconv.Atoi(matches[2])
				indexNumber, _ = strconv.Atoi(matches[3])
				seriesID := current.seriesID
				if seriesID == "" {
					seriesName := cleanName(matches[1])
					if seriesName == "" {
						seriesName = filepath.Base(state.root.path)
					}
					seriesID, err = state.folder("//series/"+strings.ToLower(seriesName), "", seriesName, "Series", state.library.ID, 0)
					if err != nil {
						return err
					}
				}
				parentID = current.seasonID
				if parentID == "" || current.seasonNumber != parentIndex {
					parentID, err = state.folder(fmt.Sprintf("//season/%s/%d", seriesID, parentIndex), "", fmt.Sprintf("Season %d", parentIndex), "Season", seriesID, parentIndex)
					if err != nil {
						return err
					}
				}
			} else if current.seasonID != "" {
				parentIndex = current.seasonNumber
				parentNumberDefined = true
			}
		}
	}
	candidates, nfoKind := fileNFOCandidates(path, itemType)
	local := state.localNFO(candidates, nfoKind, stored.local)
	if itemType == "Episode" {
		local, indexNumber, parentIndex = state.episodeNumbering(local, indexNumber, parentIndex, parentNumberDefined)
	}
	name, sortName, overview := describeFromLocal(name, local)
	// Sidecar absence is authoritative only while this media pathname still
	// names the opened file. A concurrent directory move is not NFO deletion.
	currentInfo, pathErr := state.opened.Lstat(path)
	if pathErr != nil || !currentInfo.Mode().IsRegular() || !os.SameFile(info, currentInfo) ||
		currentInfo.Size() != info.Size() || !currentInfo.ModTime().Equal(info.ModTime()) ||
		(checksVersion && media.FileChangeTime(currentInfo) != media.FileChangeTime(info)) {
		state.warnings++
		return state.store.persistProgress(state.task)
	}
	fullPath := filepath.Join(state.root.path, path)
	if itemType == "Audio" || stored.itemType == "Audio" {
		state.queueMusicParent(stored.parentID)
		state.queueMusicParent(parentID)
	}
	previousName, previousSort, previousOverview := stored.name, stored.sortName, stored.overview
	previousIndex, previousParentIndex := stored.indexNumber, stored.parentIndexNumber
	if stored.automatic != nil {
		previousName, previousSort, previousOverview = stored.automatic.Name, stored.automatic.SortName, stored.automatic.Overview
		if stored.itemType == "Episode" {
			previousIndex, previousParentIndex = 0, 0
			if stored.automatic.IndexNumber != nil {
				previousIndex = *stored.automatic.IndexNumber
			}
			if stored.automatic.ParentIndexNumber != nil {
				previousParentIndex = *stored.automatic.ParentIndexNumber
			}
		}
	}
	// Compare accepted automatic facts, not the overlaid display fields. A
	// manual title or episode number must not make every cached scan a write.
	changed := !unchanged || stored.path != fullPath || stored.parentID != parentID || previousName != name || stored.itemType != itemType ||
		previousSort != sortName || previousOverview != overview || previousIndex != indexNumber || previousParentIndex != parentIndex ||
		stored.local.hash != local.hash || stored.local.path != local.path || !reflect.DeepEqual(stored.local.value, local.value)
	if stored.id != "" && !changed {
		if err := state.scanSubtitles(stored.id, path, probe); err != nil {
			return err
		}
		if err := state.scanImages(stored.id, itemType, path, false); err != nil {
			return err
		}
		return state.store.persistProgress(state.task)
	}
	mediaJSON, err := json.Marshal(probe)
	if err != nil {
		return err
	}
	localJSON, err := encodeLocalMetadata(local)
	if err != nil {
		return err
	}
	id := stored.id
	if id == "" {
		id, err = randomID()
		if err != nil {
			return err
		}
	}
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	_, err = tx.Exec(state.task.ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path,
		 index_number, parent_index_number, media, file_identity, file_size, modified_at,
		 overview, local_metadata, local_metadata_hash, local_metadata_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (id) DO UPDATE SET root_id = EXCLUDED.root_id, parent_id = EXCLUDED.parent_id,
		 name = EXCLUDED.name, sort_name = EXCLUDED.sort_name, type = EXCLUDED.type, path = EXCLUDED.path,
		 is_folder = false,
		 relative_path = EXCLUDED.relative_path, index_number = EXCLUDED.index_number,
		 parent_index_number = EXCLUDED.parent_index_number, media = EXCLUDED.media,
		 overview = EXCLUDED.overview, local_metadata = EXCLUDED.local_metadata,
		 local_metadata_hash = EXCLUDED.local_metadata_hash, local_metadata_path = EXCLUDED.local_metadata_path,
		 file_identity = EXCLUDED.file_identity, file_size = EXCLUDED.file_size,
		 modified_at = EXCLUDED.modified_at, updated_at = now()`, id, state.library.ID, state.root.id,
		parentID, name, sortName, itemType, fullPath, relative, indexNumber, parentIndex,
		mediaJSON, fileIdentity(info), info.Size(), catalogModifiedTime(info), overview, localJSON, local.hash, local.path)
	if err != nil {
		return err
	}
	if err := writeScannedMusicSource(state.task.ctx, tx, id, itemType, probe); err != nil {
		return err
	}
	if err := syncScannedMetadata(state.task.ctx, tx, id, scannedMetadataOptions{ForceEntities: stored.id == ""}); err != nil {
		return err
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return err
	}
	if stored.id == "" {
		state.task.job.Added++
	} else {
		state.task.job.Updated++
	}
	if err := state.scanSubtitles(id, path, probe); err != nil {
		return err
	}
	if err := state.scanImages(id, itemType, path, false); err != nil {
		return err
	}
	return state.store.persistProgress(state.task)
}

func (state *scanState) folder(relative, path, name, itemType, parentID string, indexNumber int, nfoRelative ...string) (string, error) {
	metadataPath := relative
	if strings.HasPrefix(relative, "//") {
		metadataPath = ""
	}
	if len(nfoRelative) != 0 {
		metadataPath = nfoRelative[0]
	}
	local, err := state.folderLocalMetadata(relative, metadataPath, itemType, indexNumber)
	if err != nil {
		return "", err
	}
	if metadataPath != "" {
		expected := state.directoryIdentities[filepath.Clean(metadataPath)]
		current, statErr := state.opened.Lstat(metadataPath)
		if expected == nil || statErr != nil || !current.IsDir() || !os.SameFile(expected, current) {
			return "", fmt.Errorf("%w: media directory changed before metadata persistence", ErrUnavailable)
		}
	}
	name, sortName, overview := describeFromLocal(name, local)
	localJSON, err := encodeLocalMetadata(local)
	if err != nil {
		return "", err
	}
	id, err := randomID()
	if err != nil {
		return "", err
	}
	insertID := id
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return "", err
	}
	defer rollback(tx)
	err = tx.QueryRow(state.task.ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path, is_folder, index_number,
		 overview, local_metadata, local_metadata_hash, local_metadata_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10,$11,$12,$13,$14)
		ON CONFLICT (root_id, relative_path) DO UPDATE SET parent_id = EXCLUDED.parent_id,
		name = EXCLUDED.name, sort_name = EXCLUDED.sort_name, type = EXCLUDED.type,
		is_folder = true, media = NULL, file_identity = '', file_size = 0, modified_at = NULL,
		parent_index_number = 0,
		overview = EXCLUDED.overview, local_metadata = EXCLUDED.local_metadata,
		local_metadata_hash = EXCLUDED.local_metadata_hash, local_metadata_path = EXCLUDED.local_metadata_path,
		path = EXCLUDED.path, index_number = EXCLUDED.index_number, updated_at = now()
		RETURNING id`, id, state.library.ID, state.root.id, parentID, name, sortName, itemType, path, relative, indexNumber,
		overview, localJSON, local.hash, local.path).Scan(&id)
	if err != nil {
		return "", err
	}
	if itemType != "MusicAlbum" {
		if err := writeScannedMusicSource(state.task.ctx, tx, id, itemType, nil); err != nil {
			return "", err
		}
	}
	if err := syncScannedMetadata(state.task.ctx, tx, id, scannedMetadataOptions{ForceEntities: id == insertID}); err != nil {
		return "", err
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return "", err
	}
	if itemType == "MusicAlbum" {
		state.queueMusicParent(id)
	}
	if metadataPath != "" {
		if err := state.scanImages(id, itemType, metadataPath, true); err != nil {
			return "", err
		}
	}
	return id, nil
}

const storedFileColumns = `id, root_id, relative_path, file_identity, file_size, modified_at, media, COALESCE(parent_id, ''), path, name, type,
	sort_name, overview, index_number, parent_index_number, local_metadata, local_metadata_hash, local_metadata_path,
	(SELECT jsonb_build_object('Name', ms.automatic->'Name', 'SortName', ms.automatic->'SortName',
		'Overview', ms.automatic->'Overview', 'IndexNumber', ms.automatic->'IndexNumber',
		'ParentIndexNumber', ms.automatic->'ParentIndexNumber') FROM item_metadata_state ms WHERE ms.item_id = items.id)`

func readStoredFile(row rowScanner) (storedFile, error) {
	var item storedFile
	var raw, localRaw, automaticRaw []byte
	err := row.Scan(&item.id, &item.rootID, &item.relativePath, &item.identity, &item.size, &item.modified, &raw, &item.parentID, &item.path, &item.name, &item.itemType,
		&item.sortName, &item.overview, &item.indexNumber, &item.parentIndexNumber, &localRaw, &item.local.hash, &item.local.path, &automaticRaw)
	if err != nil {
		return storedFile{}, err
	}
	if len(raw) > 0 && string(raw) != "null" {
		item.media = &media.Info{}
		if err := json.Unmarshal(raw, item.media); err != nil {
			return storedFile{}, err
		}
	}
	if err := decodeLocalMetadata(localRaw, &item.local); err != nil {
		return storedFile{}, err
	}
	if len(automaticRaw) != 0 && string(automaticRaw) != "null" {
		automatic, err := decodeMetadataValues(automaticRaw)
		if err != nil {
			return storedFile{}, err
		}
		item.automatic = &automatic
	}
	return item, nil
}

func (state *scanState) findStoredFile(relative string, info os.FileInfo) (storedFile, error) {
	stored, err := readStoredFile(state.store.pool.QueryRow(state.task.ctx,
		"SELECT "+storedFileColumns+" FROM items WHERE root_id = $1 AND relative_path = $2", state.root.id, relative))
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return storedFile{}, err
	}
	identity := fileIdentity(info)
	if identity == "" {
		return storedFile{}, nil
	}
	// An inode match is only a rename candidate when the old path disappeared.
	// Size and modification time reduce accidental reuse after inode recycling.
	rows, err := state.store.pool.Query(state.task.ctx, "SELECT "+storedFileColumns+` FROM items
		WHERE library_id = $1 AND file_identity = $2 AND NOT is_folder AND file_size = $3
		AND modified_at = $4 ORDER BY created_at, id LIMIT 8`, state.library.ID, identity, info.Size(), catalogModifiedTime(info))
	if err != nil {
		return storedFile{}, err
	}
	candidates := make([]storedFile, 0)
	for rows.Next() {
		candidate, err := readStoredFile(rows)
		if err != nil {
			rows.Close()
			return storedFile{}, err
		}
		candidates = append(candidates, candidate)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return storedFile{}, err
	}
	for _, candidate := range candidates {
		oldRoot := state.opened
		if candidate.rootID != state.root.id {
			var record libraryRoot
			err := state.store.pool.QueryRow(state.task.ctx, `SELECT id, library_id, path, allowed_path, relative_path
				FROM library_roots WHERE id = $1 AND library_id = $2`, candidate.rootID, state.library.ID).
				Scan(&record.id, &record.libraryID, &record.path, &record.allowedPath, &record.relativePath)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return storedFile{}, err
			}
			oldRoot, err = state.store.openLibraryRoot(record)
			if err != nil {
				continue
			}
		}
		_, err := oldRoot.Lstat(filepath.FromSlash(candidate.relativePath))
		if oldRoot != state.opened {
			_ = oldRoot.Close()
		}
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
	}
	return storedFile{}, nil
}

func ignoredName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") || strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".partial") || strings.HasSuffix(lower, ".download") || strings.HasSuffix(name, "~")
}

func cleanName(name string) string {
	return strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(name)), " ")
}

func containsAudio(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if !entry.IsDir() && entry.Type()&os.ModeSymlink == 0 && !ignoredName(entry.Name()) && extensionKind(entry.Name()) == "audio" {
			return true
		}
	}
	return false
}

func extensionKind(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".m4v", ".webm", ".mpg", ".mpeg", ".ts", ".m2ts", ".mts", ".vob", ".ogv", ".3gp":
		return "video"
	case ".mp3", ".flac", ".m4a", ".aac", ".ogg", ".opus", ".wav", ".wma", ".aiff", ".aif", ".alac", ".ape", ".mka":
		return "audio"
	default:
		return ""
	}
}

// PostgreSQL timestamps have microsecond precision, while file timestamps may
// contain nanoseconds. Normalize before both persistence and cache comparisons.
func catalogModifiedTime(info os.FileInfo) time.Time {
	return info.ModTime().UTC().Truncate(time.Microsecond)
}
