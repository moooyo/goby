package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
}

type scanState struct {
	store    *Store
	task     *scanTask
	library  Library
	root     libraryRoot
	opened   *os.Root
	warnings int
}

type storedFile struct {
	id, rootID, relativePath, identity, parentID, path, name, itemType string
	size                                                               int64
	modified                                                           *time.Time
	media                                                              *media.Info
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
	warnings, failedRoots := 0, 0
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
		warnings += state.warnings
		if err != nil {
			if task.ctx.Err() != nil {
				return "Scan cancelled", task.ctx.Err()
			}
			failedRoots++
		}
	}
	if failedRoots > 0 {
		return fmt.Sprintf("%d media directories could not be scanned; existing catalog records were retained", failedRoots), ErrUnavailable
	}
	if warnings > 0 {
		return fmt.Sprintf("%d media entries could not be inspected; existing catalog records were retained", warnings), nil
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
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
	if err != nil {
		return err
	}
	// A directory containing audio files is the album boundary. Artist folders
	// are retained as folders because no provider metadata has been consulted.
	if (state.library.CollectionType == "music" || state.library.CollectionType == "mixed") && containsAudio(entries) {
		if relative == "." {
			id, err := state.folder("//album/root", "", filepath.Base(state.root.path), "MusicAlbum", current.parentID, 0)
			if err != nil {
				return err
			}
			current.albumID = id
		} else {
			if _, err := state.store.execOwned(state.task.ctx, "UPDATE items SET type = 'MusicAlbum' WHERE id = $1", current.parentID); err != nil {
				return err
			}
			current.albumID = current.parentID
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
						next.seriesID, err = state.folder("//series/root", "", filepath.Base(state.root.path), "Series", state.library.ID, 0)
						if err != nil {
							return err
						}
					}
					next.parentID = next.seriesID
				} else if current.seriesID == "" && depth == 0 {
					itemType = "Series"
				}
			}
			id, err := state.folder(filepath.ToSlash(path), filepath.Join(state.root.path, path), cleanName(entry.Name()), itemType, next.parentID, number)
			if err != nil {
				return err
			}
			next.parentID = id
			if itemType == "Series" {
				next.seriesID, next.seasonID = id, ""
			} else if itemType == "Season" {
				next.seasonID, next.seasonNumber = id, number
			}
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
	unchanged := probe != nil && stored.size == info.Size() && stored.modified != nil && stored.modified.Equal(catalogModifiedTime(info)) && (stored.identity == "" || stored.identity == fileIdentity(info))
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
		if statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
			state.warnings++
			return state.store.persistProgress(state.task)
		}
	}
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	name := cleanName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	itemType, parentID, indexNumber, parentIndex := "Movie", current.parentID, 0, 0
	if kind == "audio" {
		itemType = "Audio"
		if current.albumID != "" {
			parentID = current.albumID
		}
		if matches := trackPattern.FindStringSubmatch(name); len(matches) != 0 {
			indexNumber, _ = strconv.Atoi(matches[1])
			name = matches[2]
		}
	} else {
		matches := episodePattern.FindStringSubmatch(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		if state.library.CollectionType == "tvshows" || (state.library.CollectionType == "mixed" && len(matches) != 0) {
			itemType = "Episode"
			if len(matches) != 0 {
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
			}
		}
	}
	fullPath := filepath.Join(state.root.path, path)
	changed := !unchanged || stored.path != fullPath || stored.parentID != parentID || stored.name != name || stored.itemType != itemType
	if stored.id != "" && !changed {
		return state.store.persistProgress(state.task)
	}
	mediaJSON, err := json.Marshal(probe)
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
	_, err = state.store.execOwned(state.task.ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path,
		 index_number, parent_index_number, media, file_identity, file_size, modified_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		ON CONFLICT (id) DO UPDATE SET root_id = EXCLUDED.root_id, parent_id = EXCLUDED.parent_id,
		 name = EXCLUDED.name, sort_name = EXCLUDED.sort_name, type = EXCLUDED.type, path = EXCLUDED.path,
		 is_folder = false,
		 relative_path = EXCLUDED.relative_path, index_number = EXCLUDED.index_number,
		 parent_index_number = EXCLUDED.parent_index_number, media = EXCLUDED.media,
		 file_identity = EXCLUDED.file_identity, file_size = EXCLUDED.file_size,
		 modified_at = EXCLUDED.modified_at, updated_at = now()`, id, state.library.ID, state.root.id,
		parentID, name, strings.ToLower(name), itemType, fullPath, relative, indexNumber, parentIndex,
		mediaJSON, fileIdentity(info), info.Size(), catalogModifiedTime(info))
	if err != nil {
		return err
	}
	if stored.id == "" {
		state.task.job.Added++
	} else {
		state.task.job.Updated++
	}
	return state.store.persistProgress(state.task)
}

func (state *scanState) folder(relative, path, name, itemType, parentID string, indexNumber int) (string, error) {
	id, err := randomID()
	if err != nil {
		return "", err
	}
	err = state.store.queryOwnedRow(state.task.ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path, is_folder, index_number)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10)
		ON CONFLICT (root_id, relative_path) DO UPDATE SET parent_id = EXCLUDED.parent_id,
		name = EXCLUDED.name, sort_name = EXCLUDED.sort_name, type = EXCLUDED.type,
		is_folder = true, media = NULL, file_identity = '', file_size = 0, modified_at = NULL,
		parent_index_number = 0,
		path = EXCLUDED.path, index_number = EXCLUDED.index_number, updated_at = now()
		RETURNING id`, id, state.library.ID, state.root.id, parentID, name, strings.ToLower(name), itemType, path, relative, indexNumber).Scan(&id)
	return id, err
}

const storedFileColumns = `id, root_id, relative_path, file_identity, file_size, modified_at, media, COALESCE(parent_id, ''), path, name, type`

func readStoredFile(row rowScanner) (storedFile, error) {
	var item storedFile
	var raw []byte
	err := row.Scan(&item.id, &item.rootID, &item.relativePath, &item.identity, &item.size, &item.modified, &raw, &item.parentID, &item.path, &item.name, &item.itemType)
	if err != nil {
		return storedFile{}, err
	}
	if len(raw) > 0 && string(raw) != "null" {
		item.media = &media.Info{}
		if err := json.Unmarshal(raw, item.media); err != nil {
			return storedFile{}, err
		}
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
