package library

import (
	"bytes"
	"context"
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
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

const maxLocalNFOBytes = 2 * 1024 * 1024

type localMetadata struct {
	value      *metadata.Metadata
	hash, path string
	raw        []byte
}

// localNFOObservation contains only owned bytes and immutable source facts.
// Reading finishes and closes its descriptor before parsing can mutate scan
// warnings or any publication transaction begins.
type localNFOObservation struct {
	disabled bool
	invalid  bool
	path     string
	data     []byte
}

// observeLocalNFO must run inside an admitted metadata phase. The first present
// candidate wins even when invalid, so a less specific file cannot replace it.
func (state *scanState) observeLocalNFO(ctx context.Context, candidates []string, kind string) (localNFOObservation, error) {
	if ctx == nil || state == nil {
		return localNFOObservation{}, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return localNFOObservation{}, err
	}
	if kind == "" {
		return localNFOObservation{}, nil
	}
	if !EffectiveLibraryOptions(state.library).EnableLocalMetadata {
		return localNFOObservation{disabled: true}, nil
	}
	if state.opened == nil {
		return localNFOObservation{}, ErrUnavailable
	}
	for _, candidate := range candidates {
		data, found, invalid, err := readLocalNFO(ctx, state.opened, candidate)
		err = errors.Join(err, ctx.Err())
		if err != nil {
			if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
				return localNFOObservation{}, fatal
			}
			return localNFOObservation{invalid: true}, nil
		}
		if invalid {
			return localNFOObservation{invalid: true}, nil
		}
		if found {
			return localNFOObservation{path: filepath.ToSlash(candidate), data: data}, nil
		}
	}
	return localNFOObservation{}, nil
}

// Ordinary unreadable or replaced NFOs retain their prior metadata. Lifecycle
// failures are checked first so a joined read error cannot conceal uncertain
// descriptor retirement or cancellation of the admitted operation.
func localNFOFatalReadError(ctx context.Context, err error) error {
	if ctx == nil {
		return errors.Join(err, ErrUnavailable)
	}
	err = errors.Join(err, ctx.Err())
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errSidecarRetirementUnknown) || errors.Is(err, media.ErrProcessRetirementUnknown) {
		return err
	}
	return nil
}

// applyLocalNFO runs on the serial scanner after its read phase has retired.
// Invalid preferred files retain the last valid metadata. Complete absence
// removes the local override, while a disabled importer retains its old facts.
func (state *scanState) applyLocalNFO(observation localNFOObservation, kind string, previous localMetadata) localMetadata {
	if kind == "" {
		return localMetadata{}
	}
	if previous.value != nil && previous.value.Kind != kind {
		previous = localMetadata{}
	}
	if observation.disabled {
		return previous
	}
	if observation.invalid {
		state.warnings++
		return previous
	}
	if observation.path == "" {
		return localMetadata{}
	}
	value, err := metadata.ParseNFO(bytes.NewReader(observation.data))
	if err != nil || value.Kind != kind {
		state.warnings++
		return previous
	}
	digest := sha256.Sum256(observation.data)
	return localMetadata{value: &value, hash: hex.EncodeToString(digest[:]), path: observation.path, raw: previous.raw}
}

func readLocalNFO(ctx context.Context, root *os.Root, path string) ([]byte, bool, bool, error) {
	if ctx == nil || root == nil {
		return nil, false, false, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, false, false, err
	}
	if path == "" || filepath.IsAbs(path) || hasTraversal(path) {
		return nil, false, true, nil
	}
	before, err := root.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, fmt.Errorf("read local metadata pathname: %w", err)
	}
	if !before.Mode().IsRegular() || before.Size() > maxLocalNFOBytes {
		return nil, false, true, nil
	}
	file, err := openScanFile(root, path)
	if err != nil {
		return nil, false, false, fmt.Errorf("open local metadata safely: %w", err)
	}
	data, invalid, err := readOpenedLocalNFO(ctx, file, before, func() (os.FileInfo, error) { return root.Lstat(path) })
	return data, true, invalid, err
}

type localNFOFile interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}

func readOpenedLocalNFO(ctx context.Context, file localNFOFile, before os.FileInfo, currentInfo func() (os.FileInfo, error)) (_ []byte, invalid bool, resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, file)) }()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	opened, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat opened local metadata: %w", err)
	}
	if !sameLocalNFOFile(before, opened) || opened.Size() > maxLocalNFOBytes {
		return nil, true, nil
	}
	data, err := io.ReadAll(io.LimitReader(subtitleContextReader{ctx: ctx, reader: file}, maxLocalNFOBytes+1))
	if err != nil {
		return nil, false, fmt.Errorf("read bounded local metadata: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if len(data) > maxLocalNFOBytes {
		return nil, true, nil
	}
	after, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat read local metadata: %w", err)
	}
	if !sameLocalNFOFile(opened, after) || int64(len(data)) != after.Size() {
		return nil, true, nil
	}
	current, err := currentInfo()
	if err != nil {
		return nil, false, fmt.Errorf("recheck local metadata pathname: %w", err)
	}
	if !sameLocalNFOFile(after, current) {
		return nil, true, nil
	}
	return data, false, ctx.Err()
}

func sameLocalNFOFile(first, second os.FileInfo) bool {
	return first != nil && second != nil && first.Mode().IsRegular() && second.Mode().IsRegular() &&
		os.SameFile(first, second) && first.Size() == second.Size() && first.ModTime().Equal(second.ModTime()) &&
		media.FileChangeTime(first) == media.FileChangeTime(second)
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
	var observation localNFOObservation
	if err := state.runPrimaryScanMetadata(state.task.ctx, func(ctx context.Context) error {
		var err error
		observation, err = state.observeLocalNFO(ctx, candidates, kind)
		if err != nil {
			return err
		}
		expected := state.directoryIdentities[filepath.Clean(metadataPath)]
		current, statErr := state.opened.Lstat(metadataPath)
		if expected == nil || statErr != nil || !current.IsDir() || !os.SameFile(expected, current) {
			return fmt.Errorf("%w: media directory changed before metadata persistence", ErrUnavailable)
		}
		return ctx.Err()
	}); err != nil {
		return localMetadata{}, scanReadFailure(err)
	}
	local := state.applyLocalNFO(observation, kind, previous)
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
	// Preserve future source fields while refreshing every field understood by
	// this scanner. RawMessage also preserves unknown integers without float64.
	object, err := metadataSourceObject(local.raw)
	if err != nil {
		return nil, err
	}
	known, err := json.Marshal(local.value)
	if err != nil {
		return nil, err
	}
	values, err := metadataSourceObject(known)
	if err != nil {
		return nil, err
	}
	// If a known field did not change, retain its original representation too.
	// This includes extensions inside credits or other future nested objects.
	// Changed fields are replaced as a unit so an extension cannot accidentally
	// move from an old credit to a different person after an NFO update.
	var previousKnown map[string]json.RawMessage
	if len(local.raw) != 0 {
		var previous metadata.Metadata
		if err := json.Unmarshal(local.raw, &previous); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(previous)
		if err != nil {
			return nil, err
		}
		previousKnown, err = metadataSourceObject(encoded)
		if err != nil {
			return nil, err
		}
	}
	for field, value := range values {
		if _, exists := object[field]; exists && bytes.Equal(previousKnown[field], value) {
			continue
		}
		object[field] = value
	}
	return json.Marshal(object)
}

func decodeLocalMetadata(raw []byte, local *localMetadata) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	local.value = &metadata.Metadata{}
	local.raw = append([]byte(nil), raw...)
	return json.Unmarshal(raw, local.value)
}
