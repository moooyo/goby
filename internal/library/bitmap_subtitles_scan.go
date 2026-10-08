package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

type scannedBitmapSubtitle struct {
	tracks []storedBitmapSubtitle
	files  []*os.File
	infos  []os.FileInfo
}

func bitmapSubtitleKey(path string, sourceStreamIndex int) string {
	return path + "\x00" + strconv.Itoa(sourceStreamIndex)
}

func (entry *scannedBitmapSubtitle) close(ctx context.Context) error {
	var err error
	for _, file := range entry.files {
		err = errors.Join(err, closePrimarySidecarResource(ctx, file))
	}
	entry.files = nil
	return err
}

func inspectBitmapSubtitleComponent(ctx context.Context, root *os.Root, name string) (_ *os.File, _ os.FileInfo, _ BitmapSubtitleComponent, resultErr error) {
	var component BitmapSubtitleComponent
	before, err := root.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > media.MaxExternalBitmapSubtitleBytes {
		return nil, nil, component, fmt.Errorf("bitmap subtitle is not a bounded regular file")
	}
	file, err := openScanFile(root, name)
	if err != nil {
		return nil, nil, component, err
	}
	keep := false
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, file))
		}
	}()
	opened, err := file.Stat()
	if err != nil || !sameMediaSourceFile(before, opened) || !opened.Mode().IsRegular() {
		return nil, nil, component, ErrSourceChanged
	}
	hash := sha256.New()
	count, err := io.CopyBuffer(hash, subtitleContextReader{ctx: ctx, reader: io.NewSectionReader(file, 0, opened.Size())}, make([]byte, 64<<10))
	if err != nil || count != opened.Size() {
		return nil, nil, component, errors.Join(ErrSourceChanged, err)
	}
	after, err := file.Stat()
	current, currentErr := root.Lstat(name)
	if err != nil || currentErr != nil || !sameMediaSourceFile(opened, after) || !sameMediaSourceFile(opened, current) {
		return nil, nil, component, ErrSourceChanged
	}
	component = BitmapSubtitleComponent{Name: name, Identity: fileIdentity(opened), SHA256: hex.EncodeToString(hash.Sum(nil)),
		Size: opened.Size(), ModifiedNS: opened.ModTime().UnixNano(), ChangeTimeNS: media.FileChangeTime(opened)}
	keep = true
	return file, opened, component, nil
}

func inspectLocalBitmapSubtitle(ctx context.Context, root *os.Root, candidate bitmapSubtitleCandidate) (_ *scannedBitmapSubtitle, resultErr error) {
	entry := &scannedBitmapSubtitle{}
	keep := false
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, entry.close(ctx))
		}
	}()
	names := []string{candidate.filename}
	if candidate.companion != "" {
		names = append(names, candidate.companion)
	}
	components := make([]BitmapSubtitleComponent, 0, len(names))
	for _, name := range names {
		file, info, component, err := inspectBitmapSubtitleComponent(ctx, root, name)
		if err != nil {
			return nil, err
		}
		entry.files, entry.infos = append(entry.files, file), append(entry.infos, info)
		components = append(components, component)
	}
	var companion *os.File
	if len(entry.files) == 2 {
		companion = entry.files[1]
	}
	tracks, err := media.InspectExternalBitmapSubtitles(ctx, candidate.format, entry.files[0], companion)
	if err != nil || len(tracks) == 0 || len(tracks) > maxActiveSubtitles {
		return nil, errors.Join(fmt.Errorf("invalid bitmap subtitle inventory"), err)
	}
	seen := make(map[int]bool)
	for _, track := range tracks {
		language := track.Language
		if language == "" {
			language = candidate.metadata.Language
		}
		value := BitmapSubtitle{SourceStreamIndex: track.SourceStreamIndex, Codec: track.Codec, Format: candidate.format,
			Language: language, Title: language, Filename: candidate.filename, Components: append([]BitmapSubtitleComponent(nil), components...),
			IsDefault: candidate.metadata.IsDefault, IsForced: candidate.metadata.IsForced, IsHearingImpaired: candidate.metadata.IsHearingImpaired}
		value.Tag = BitmapSubtitleSourceHash(value.Format, value.SourceStreamIndex, value.Components)
		if seen[value.SourceStreamIndex] || ValidateBitmapSubtitle(value) != nil {
			return nil, fmt.Errorf("invalid bitmap subtitle track metadata")
		}
		seen[value.SourceStreamIndex] = true
		entry.tracks = append(entry.tracks, storedBitmapSubtitle{BitmapSubtitle: value})
	}
	if err := verifyScannedBitmapSubtitle(root, entry); err != nil {
		return nil, err
	}
	keep = true
	return entry, nil
}

func verifyScannedBitmapSubtitle(root *os.Root, entry *scannedBitmapSubtitle) error {
	if len(entry.tracks) == 0 || len(entry.files) != len(entry.tracks[0].Components) || len(entry.infos) != len(entry.files) {
		return ErrSourceChanged
	}
	for index, file := range entry.files {
		after, err := file.Stat()
		current, currentErr := root.Lstat(entry.tracks[0].Components[index].Name)
		if err != nil || currentErr != nil || !after.Mode().IsRegular() || !current.Mode().IsRegular() ||
			!sameMediaSourceFile(entry.infos[index], after) || !sameMediaSourceFile(entry.infos[index], current) {
			return ErrSourceChanged
		}
	}
	return nil
}

func (state *scanState) scanBitmapSubtitlesAttempt(itemID, relative string, probe *media.Info) (resultErr error) {
	ctx := state.task.ctx
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validMediaSourceRelativePath(relative) || probe == nil {
		state.warnings++
		return nil
	}
	directoryPath := filepath.Clean(filepath.Dir(relative))
	expected := state.directoryIdentities[directoryPath]
	if expected == nil {
		state.warnings++
		return nil
	}
	// The text scan has already checked the shared directory index. Avoid a
	// second storage phase for the common case with no bitmap files or rows.
	if index := state.subtitleDirectories[directoryPath]; index != nil {
		selected := index.bitmap.selection(relative)
		if len(selected.present) == 0 && !selected.overflow {
			var active bool
			args := []any{state.store.scanPooledReadMode, itemID}
			if state.store.scanPooledReadMode == 0 {
				args = args[1:]
			}
			if err := state.store.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM item_bitmap_subtitles WHERE item_id=$1 AND active)`, args...).Scan(&active); err != nil {
				return err
			}
			if !active {
				return nil
			}
		}
	}
	operation, row, err := state.prepareSidecarScanIO()
	if err != nil {
		return scanReadFailure(err)
	}
	cleanup := operation.Context(ctx)
	var root, currentRoot, currentDirectory *os.Root
	var directory *os.File
	var directoryInfo, primary os.FileInfo
	var sourceRoot *scanSourceRootWitness
	inspected := make(map[string]*scannedBitmapSubtitle)
	present := make(map[string]bool)
	defer func() {
		for _, entry := range inspected {
			resultErr = errors.Join(resultErr, entry.close(cleanup))
		}
		for _, value := range []io.Closer{currentDirectory, currentRoot, directory, root} {
			switch file := value.(type) {
			case *os.Root:
				if file != nil {
					resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanup, file))
				}
			case *os.File:
				if file != nil {
					resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanup, file))
				}
			}
		}
		if sourceRoot != nil {
			resultErr = errors.Join(resultErr, sourceRoot.Close())
		}
		resultErr = scanReadFailure(errors.Join(resultErr, operation.Close()))
	}()
	ready := false
	err = operation.Run(ctx, state.root.id, primaryio.Background, func(work context.Context) error {
		if err := state.checkSidecarScanAuthority(work, row); err != nil {
			return err
		}
		root, err = openRegisteredRoot(state.opened, directoryPath)
		if err != nil {
			state.warnings++
			return nil
		}
		directory, err = openScanFile(root, ".")
		if err != nil {
			state.warnings++
			return nil
		}
		directoryInfo, err = directory.Stat()
		if err != nil || !directoryInfo.IsDir() || !os.SameFile(expected, directoryInfo) {
			state.warnings++
			return nil
		}
		index := state.subtitleDirectories[directoryPath]
		if index == nil {
			entries, readErr := directory.ReadDir(-1)
			if readErr != nil {
				state.warnings++
				return nil
			}
			index = newSubtitleDirectoryIndex(entries, state.library.CollectionType, directoryInfo)
			if state.subtitleDirectories == nil {
				state.subtitleDirectories = make(map[string]*subtitleDirectoryIndex)
			}
			state.subtitleDirectories[directoryPath] = index
		} else if !sameSubtitleDirectoryInfo(index.info, directoryInfo) {
			state.warnings++
			return nil
		}
		selected := index.bitmap.selection(relative)
		if selected.overflow {
			state.warnings++
			return nil
		}
		for name := range selected.present {
			present[strings.ToLower(filepath.ToSlash(filepath.Join(directoryPath, name)))] = true
		}
		primary, err = root.Lstat(filepath.Base(relative))
		if err != nil || !primary.Mode().IsRegular() || probe.FileChangeTimeNs > 0 && media.FileChangeTime(primary) != probe.FileChangeTimeNs {
			state.warnings++
			return nil
		}
		for _, candidate := range selected.candidates {
			entry, inspectErr := inspectLocalBitmapSubtitle(work, root, candidate)
			if inspectErr != nil {
				if work.Err() != nil {
					return work.Err()
				}
				state.warnings++
				continue
			}
			path := filepath.ToSlash(filepath.Join(directoryPath, candidate.filename))
			for position := range entry.tracks {
				entry.tracks[position].relativePath, entry.tracks[position].rootID = path, state.root.id
			}
			inspected[path] = entry
		}
		currentRoot, err = state.store.openScanOperationRoot(work, state.task, state.root)
		if err != nil {
			state.warnings++
			return nil
		}
		currentDirectory, err = openRegisteredRoot(currentRoot, directoryPath)
		if err != nil || !state.subtitleSourceStable(relative, directoryInfo, primary, directory, currentRoot, currentDirectory) {
			state.warnings++
			return nil
		}
		for path, entry := range inspected {
			if err := verifyScannedBitmapSubtitle(currentDirectory, entry); err != nil {
				if err := entry.close(work); err != nil {
					return err
				}
				delete(inspected, path)
				state.warnings++
			}
		}
		sourceRoot, err = state.captureScanSourceRoot(work, row)
		if err != nil {
			return err
		}
		ready = true
		return work.Err()
	})
	if err != nil || !ready {
		return scanReadFailure(err)
	}
	proof := func() error {
		return operation.RunImmediate(ctx, state.root.id, primaryio.Background, func(proof context.Context) error {
			if err := sourceRoot.Check(proof); err != nil {
				return err
			}
			if !state.subtitleSourceStable(relative, directoryInfo, primary, directory, currentRoot, currentDirectory) {
				return ErrSourceChanged
			}
			for _, entry := range inspected {
				if err := verifyScannedBitmapSubtitle(currentDirectory, entry); err != nil {
					return err
				}
			}
			return sourceRoot.Check(proof)
		})
	}
	return state.persistBitmapSubtitles(itemID, relative, primary, present, inspected, row, proof)
}

func (state *scanState) persistBitmapSubtitles(itemID, relative string, primary os.FileInfo, present map[string]bool,
	inspected map[string]*scannedBitmapSubtitle, root rootBindingRow, finalProof func() error) (resultErr error) {
	ctx := state.task.ctx
	tx, err := state.store.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if err := state.checkSidecarScanRootTx(tx, root); err != nil {
		return err
	}
	var identity string
	var size int64
	var modified *time.Time
	var mediaJSON []byte
	change := CatalogChange{Kind: CatalogUpdated}
	err = tx.QueryRow(ctx, `SELECT i.file_identity,i.file_size,i.modified_at,i.media,
		i.id,i.library_id,COALESCE(i.parent_id,''),i.is_folder,i.type='CollectionFolder'
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND i.library_id=$2 AND i.root_id=$3 AND i.relative_path=$4
		AND NOT i.is_folder AND i.media IS NOT NULL FOR UPDATE OF i`, itemID, state.library.ID, state.root.id, filepath.ToSlash(relative)).
		Scan(&identity, &size, &modified, &mediaJSON, &change.ItemID, &change.LibraryID, &change.ParentID, &change.IsFolder, &change.IsCollectionFolder)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	probe, matches, err := subtitleScanPrimaryMatches(primary, identity, size, modified, mediaJSON)
	if err != nil {
		return err
	}
	if !matches {
		state.warnings++
		return nil
	}
	active, err := readBitmapSubtitleRows(ctx, tx, itemID, "")
	if err != nil {
		return err
	}
	total, highest, combinedActive, err := subtitleCatalogCapacity(ctx, tx, itemID)
	if err != nil {
		return err
	}
	embedded := highestEmbeddedStreamIndex(&probe)
	highest = max(highest, embedded)
	otherActive := combinedActive - len(active)
	entries := make(map[string]storedBitmapSubtitle)
	for path, source := range inspected {
		for _, track := range source.tracks {
			entries[bitmapSubtitleKey(path, track.SourceStreamIndex)] = track
		}
	}
	retire := make([]int, 0)
	retained := make(map[string]storedBitmapSubtitle)
	for _, track := range active {
		key := bitmapSubtitleKey(track.relativePath, track.SourceStreamIndex)
		_, found := entries[key]
		_, inspectedPath := inspected[track.relativePath]
		if track.rootID != state.root.id || track.Index <= embedded || !present[strings.ToLower(track.relativePath)] || inspectedPath && !found {
			retire = append(retire, track.Index)
			continue
		}
		retained[key] = track
	}
	beforeProjection, err := readSubtitleCatalogProjection(ctx, tx, itemID, embedded)
	if err != nil {
		return err
	}
	if len(retire) != 0 {
		if _, err := tx.Exec(ctx, `UPDATE item_bitmap_subtitles SET active=false WHERE item_id=$1 AND stream_index=ANY($2::integer[])`, itemID, retire); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(a, b int) bool {
		first, second := entries[keys[a]], entries[keys[b]]
		if first.relativePath != second.relativePath {
			return first.relativePath < second.relativePath
		}
		return first.SourceStreamIndex < second.SourceStreamIndex
	})
	for _, key := range keys {
		track := entries[key]
		previous, exists := retained[key]
		if exists {
			track.Index = previous.Index
			if reflect.DeepEqual(track, previous) {
				continue
			}
		} else {
			if total >= maxSubtitleIdentities || highest >= maxSubtitleStreamIndex || len(retained)+otherActive >= maxActiveSubtitles {
				state.warnings++
				continue
			}
			highest++
			total++
			track.Index = highest
			retained[key] = track
		}
		components, err := json.Marshal(track.Components)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO item_bitmap_subtitles(item_id,root_id,stream_index,relative_path,source_stream_index,
			format,codec,source_hash,language,title,is_default,is_forced,is_hearing_impaired,components)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb)
			ON CONFLICT(item_id,stream_index) DO UPDATE SET root_id=EXCLUDED.root_id,relative_path=EXCLUDED.relative_path,
			source_stream_index=EXCLUDED.source_stream_index,format=EXCLUDED.format,codec=EXCLUDED.codec,source_hash=EXCLUDED.source_hash,
			language=EXCLUDED.language,title=EXCLUDED.title,is_default=EXCLUDED.is_default,is_forced=EXCLUDED.is_forced,
			is_hearing_impaired=EXCLUDED.is_hearing_impaired,components=EXCLUDED.components`, itemID, track.rootID, track.Index,
			track.relativePath, track.SourceStreamIndex, track.Format, track.Codec, track.Tag, track.Language, track.Title,
			track.IsDefault, track.IsForced, track.IsHearingImpaired, components)
		if err != nil {
			return err
		}
	}
	afterProjection, err := readSubtitleCatalogProjection(ctx, tx, itemID, embedded)
	if err != nil {
		return err
	}
	if beforeProjection != afterProjection {
		if err := recordCatalogChanges(tx, change); err != nil {
			return err
		}
	}
	if err := finalProof(); err != nil {
		return scanReadFailure(err)
	}
	return tx.Commit(ctx)
}
