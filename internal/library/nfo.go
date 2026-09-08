package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/metadata"
)

const maxLocalNFOBytes = 2 * 1024 * 1024

type localMetadata struct {
	value      *metadata.Metadata
	hash, path string
}

// localNFO reads the first present candidate. Invalid preferred files do not
// fall through to a less specific file or replace the last valid metadata.
// Absence of every candidate removes the local override on the next scan.
func (state *scanState) localNFO(candidates []string, kind string, previous localMetadata) localMetadata {
	if kind == "" {
		return localMetadata{}
	}
	if previous.value != nil && previous.value.Kind != kind {
		previous = localMetadata{}
	}
	for _, candidate := range candidates {
		data, found, err := state.readLocalNFO(candidate)
		if err != nil {
			state.warnings++
			return previous
		}
		if !found {
			continue
		}
		value, err := metadata.ParseNFO(bytes.NewReader(data))
		if err != nil || value.Kind != kind {
			state.warnings++
			return previous
		}
		digest := sha256.Sum256(data)
		return localMetadata{value: &value, hash: hex.EncodeToString(digest[:]), path: filepath.ToSlash(candidate)}
	}
	return localMetadata{}
}

func (state *scanState) readLocalNFO(path string) ([]byte, bool, error) {
	if err := state.task.ctx.Err(); err != nil {
		return nil, false, err
	}
	if path == "" || filepath.IsAbs(path) || hasTraversal(path) {
		return nil, false, fmt.Errorf("invalid local metadata path")
	}
	before, err := state.opened.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxLocalNFOBytes {
		return nil, false, fmt.Errorf("local metadata is not a readable regular file within the size limit")
	}
	file, err := openScanFile(state.opened, path)
	if err != nil {
		return nil, false, fmt.Errorf("local metadata cannot be opened safely")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() > maxLocalNFOBytes {
		return nil, false, fmt.Errorf("local metadata changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxLocalNFOBytes+1))
	if err != nil || len(data) > maxLocalNFOBytes {
		return nil, false, fmt.Errorf("local metadata cannot be read within the size limit")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || int64(len(data)) != after.Size() {
		return nil, false, fmt.Errorf("local metadata changed while reading")
	}
	current, err := state.opened.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(after, current) {
		return nil, false, fmt.Errorf("local metadata pathname changed while reading")
	}
	return data, true, nil
}

func fileNFOCandidates(path, itemType string) ([]string, string) {
	base := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	switch itemType {
	case "Movie":
		fallback := filepath.Join(filepath.Dir(path), "movie.nfo")
		if base == fallback {
			return []string{base}, "movie"
		}
		return []string{base, fallback}, "movie"
	case "Episode":
		return []string{base}, "episodedetails"
	default:
		return nil, ""
	}
}

func folderNFOCandidate(relative, itemType string) ([]string, string) {
	name, kind := "", ""
	switch itemType {
	case "Series":
		name, kind = "tvshow.nfo", "tvshow"
	case "Season":
		name, kind = "season.nfo", "season"
	case "MusicAlbum":
		name, kind = "album.nfo", "album"
	default:
		return nil, ""
	}
	return []string{filepath.Join(relative, name)}, kind
}

func describeFromLocal(name string, local localMetadata) (string, string, string) {
	if local.value == nil {
		return name, strings.ToLower(name), ""
	}
	if local.value.Name != "" {
		name = local.value.Name
	}
	sortName := name
	if local.value.SortName != "" {
		sortName = local.value.SortName
	}
	return name, strings.ToLower(sortName), local.value.Overview
}

// Numbering describes the hierarchy already established from filenames and
// folders. Conflicting NFO values are not applied or passed to item projections.
func (state *scanState) episodeNumbering(local localMetadata, index, parent int, parentDefined bool) (localMetadata, int, int) {
	if local.value == nil {
		return local, index, parent
	}
	copy := *local.value
	if copy.IndexNumber != nil {
		index = *copy.IndexNumber
	}
	if copy.ParentIndexNumber != nil {
		if parentDefined && *copy.ParentIndexNumber != parent {
			state.warnings++
			state.numberingConflicts++
			copy.ParentIndexNumber = nil
		} else {
			parent = *copy.ParentIndexNumber
		}
	}
	local.value = &copy
	return local, index, parent
}

func (state *scanState) folderLocalMetadata(relative, metadataPath, itemType string, index int) (localMetadata, error) {
	if metadataPath == "" {
		return localMetadata{}, nil
	}
	var previous localMetadata
	var raw []byte
	err := state.store.pool.QueryRow(state.task.ctx, `SELECT local_metadata, local_metadata_hash, local_metadata_path
		FROM items WHERE root_id = $1 AND relative_path = $2`, state.root.id, relative).Scan(&raw, &previous.hash, &previous.path)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return localMetadata{}, err
	}
	if err := decodeLocalMetadata(raw, &previous); err != nil {
		return localMetadata{}, err
	}
	candidates, kind := folderNFOCandidate(metadataPath, itemType)
	local := state.localNFO(candidates, kind, previous)
	if itemType == "Season" && local.value != nil && local.value.IndexNumber != nil && *local.value.IndexNumber != index {
		state.warnings++
		state.numberingConflicts++
		copy := *local.value
		copy.IndexNumber = nil
		local.value = &copy
	}
	return local, nil
}

func encodeLocalMetadata(local localMetadata) ([]byte, error) {
	if local.value == nil {
		return nil, nil
	}
	return json.Marshal(local.value)
}

func decodeLocalMetadata(raw []byte, local *localMetadata) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	local.value = &metadata.Metadata{}
	return json.Unmarshal(raw, local.value)
}
