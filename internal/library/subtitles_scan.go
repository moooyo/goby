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
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

type scannedSubtitle struct {
	source storedSubtitle
	file   *os.File
	info   os.FileInfo
}

// scanSubtitles executes on cached and fresh media visits. Bytes are inspected
// outside the owned catalog transaction, with bounded descriptors and input.
// Only a complete stable directory listing can retire absent file identities.
func (state *scanState) scanSubtitles(itemID, relative string, probe *media.Info) error {
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
	root, err := openRegisteredRoot(state.opened, directoryPath)
	if err != nil {
		state.warnings++
		return nil
	}
	defer root.Close()
	directory, err := openScanFile(root, ".")
	if err != nil {
		state.warnings++
		return nil
	}
	defer directory.Close()
	directoryInfo, err := directory.Stat()
	if err != nil || !directoryInfo.IsDir() || !os.SameFile(expected, directoryInfo) {
		state.warnings++
		return nil
	}
	index := state.subtitleDirectories[directoryPath]
	if index == nil {
		entries, err := directory.ReadDir(-1)
		if err != nil {
			state.warnings++
			return nil
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		index = newSubtitleDirectoryIndex(names, directoryInfo)
		if state.subtitleDirectories == nil {
			state.subtitleDirectories = make(map[string]*subtitleDirectoryIndex)
		}
		state.subtitleDirectories[directoryPath] = index
	} else if !sameSubtitleDirectoryInfo(index.info, directoryInfo) {
		state.warnings++
		return nil
	}
	candidates, overflow := index.candidates(relative)
	if overflow {
		state.warnings++
		return nil
	}
	primary, err := root.Lstat(filepath.Base(relative))
	if err != nil || !primary.Mode().IsRegular() ||
		(probe.FileChangeTimeNs > 0 && media.FileChangeTime(primary) != probe.FileChangeTimeNs) {
		state.warnings++
		return nil
	}
	inspected := make(map[string]*scannedSubtitle, len(candidates))
	present := make(map[string]bool, len(candidates))
	defer func() {
		for _, entry := range inspected {
			_ = entry.file.Close()
		}
	}()
	for _, candidate := range candidates {
		path := filepath.ToSlash(filepath.Join(directoryPath, candidate.filename))
		present[path] = true
		entry, err := inspectLocalSubtitle(ctx, root, candidate)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			state.warnings++
			continue
		}
		entry.source.relativePath = path
		entry.source.rootID = state.root.id
		inspected[path] = entry
	}
	// Root and directory identities must still refer to the held objects. A
	// renamed tree or a changed listing cannot authorize sidecar deletion.
	currentRoot, err := state.store.openLibraryRoot(state.root)
	if err != nil {
		state.warnings++
		return nil
	}
	defer currentRoot.Close()
	currentDirectory, err := openRegisteredRoot(currentRoot, directoryPath)
	if err != nil {
		state.warnings++
		return nil
	}
	defer currentDirectory.Close()
	current, currentErr := currentDirectory.Stat(".")
	after, afterErr := directory.Stat()
	currentPrimary, primaryErr := currentDirectory.Lstat(filepath.Base(relative))
	if !sameMediaSourceDirectory(state.opened, currentRoot) || currentErr != nil || afterErr != nil || primaryErr != nil ||
		!sameSubtitleDirectoryInfo(directoryInfo, current) || !sameSubtitleDirectoryInfo(directoryInfo, after) ||
		!sameMediaSourceFile(primary, currentPrimary) || !currentPrimary.Mode().IsRegular() {
		state.warnings++
		return nil
	}
	for path, entry := range inspected {
		if err := verifyScannedSubtitle(currentDirectory, entry); err != nil {
			_ = entry.file.Close()
			delete(inspected, path)
			state.warnings++
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return state.persistSubtitles(itemID, relative, primary, present, inspected)
}

func sameSubtitleDirectoryInfo(first, second os.FileInfo) bool {
	return first != nil && second != nil && first.IsDir() && second.IsDir() && os.SameFile(first, second) &&
		first.ModTime().Equal(second.ModTime()) && media.FileChangeTime(first) == media.FileChangeTime(second)
}

func inspectLocalSubtitle(ctx context.Context, root *os.Root, candidate subtitleCandidate) (*scannedSubtitle, error) {
	before, err := root.Lstat(candidate.filename)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > subtitle.MaxInputBytes {
		return nil, fmt.Errorf("local subtitle is not a regular file within the size limit")
	}
	file, err := openScanFile(root, candidate.filename)
	if err != nil {
		return nil, fmt.Errorf("local subtitle cannot be opened safely")
	}
	keep := false
	defer func() {
		if !keep {
			_ = file.Close()
		}
	}()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !sameMediaSourceFile(before, opened) {
		return nil, fmt.Errorf("local subtitle changed while opening")
	}
	data, err := readSubtitleBytes(ctx, file)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != opened.Size() {
		return nil, fmt.Errorf("local subtitle changed during reading")
	}
	if _, err := subtitle.Parse(data, subtitle.Format(candidate.info.Codec)); err != nil {
		return nil, fmt.Errorf("local subtitle is invalid: %w", err)
	}
	digest := sha256.Sum256(data)
	source := storedSubtitle{Subtitle: candidate.info, identity: fileIdentity(opened), changeTimeNs: media.FileChangeTime(opened)}
	source.Tag = hex.EncodeToString(digest[:])
	source.Size = opened.Size()
	source.ModifiedAt = catalogModifiedTime(opened)
	entry := &scannedSubtitle{source: source, file: file, info: opened}
	if err := verifyScannedSubtitle(root, entry); err != nil {
		return nil, err
	}
	keep = true
	return entry, nil
}

func verifyScannedSubtitle(root *os.Root, entry *scannedSubtitle) error {
	after, err := entry.file.Stat()
	current, currentErr := root.Lstat(entry.source.Filename)
	if err != nil || currentErr != nil || !after.Mode().IsRegular() || !current.Mode().IsRegular() ||
		!sameMediaSourceFile(entry.info, after) || !sameMediaSourceFile(entry.info, current) {
		return fmt.Errorf("local subtitle changed during inspection")
	}
	return nil
}

type subtitleContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader subtitleContextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func readSubtitleBytes(ctx context.Context, file *os.File) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(subtitleContextReader{ctx: ctx, reader: file}, subtitle.MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > subtitle.MaxInputBytes {
		return nil, fmt.Errorf("subtitle input exceeds the bounded size")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

func (state *scanState) persistSubtitles(itemID, relative string, primary os.FileInfo, present map[string]bool, inspected map[string]*scannedSubtitle) error {
	ctx := state.task.ctx
	tx, err := state.store.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var identity string
	var size int64
	var modified *time.Time
	var mediaJSON []byte
	err = tx.QueryRow(ctx, `SELECT i.file_identity, i.file_size, i.modified_at, i.media FROM items i
		JOIN library_roots r ON r.id = i.root_id AND r.library_id = i.library_id
		WHERE i.id = $1 AND i.library_id = $2 AND i.root_id = $3 AND i.relative_path = $4
		AND NOT i.is_folder AND i.media IS NOT NULL FOR UPDATE OF i`,
		itemID, state.library.ID, state.root.id, filepath.ToSlash(relative)).Scan(&identity, &size, &modified, &mediaJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var probe media.Info
	if err := json.Unmarshal(mediaJSON, &probe); err != nil {
		return err
	}
	if identity != fileIdentity(primary) || size != primary.Size() || modified == nil ||
		!modified.Equal(catalogModifiedTime(primary)) ||
		(probe.FileChangeTimeNs > 0 && probe.FileChangeTimeNs != media.FileChangeTime(primary)) {
		state.warnings++
		return nil
	}
	var activeJSON []byte
	var total, highest int
	// QueryRow keeps every write-side query on the cancellation-shielded owned
	// transaction, whose session also holds the scanner's advisory lock.
	err = tx.QueryRow(ctx, `SELECT count(*), COALESCE(max(stream_index), -1),
		COALESCE(jsonb_agg(jsonb_build_object('Path', relative_path, 'Index', stream_index))
		FILTER (WHERE active), '[]'::jsonb) FROM item_subtitles WHERE item_id = $1`, itemID).
		Scan(&total, &highest, &activeJSON)
	if err != nil {
		return err
	}
	var active []struct {
		Path  string
		Index int
	}
	if err := json.Unmarshal(activeJSON, &active); err != nil {
		return err
	}
	embedded := highestEmbeddedStreamIndex(&probe)
	if embedded > highest {
		highest = embedded
	}
	retire := make([]int, 0)
	retained := make(map[string]int, len(active))
	for _, previous := range active {
		if !present[previous.Path] || previous.Index <= embedded {
			retire = append(retire, previous.Index)
			continue
		}
		retained[previous.Path] = previous.Index
	}
	if len(retire) != 0 {
		if _, err := tx.Exec(ctx, `UPDATE item_subtitles SET active = false
			WHERE item_id = $1 AND stream_index = ANY($2::integer[])`, itemID, retire); err != nil {
			return err
		}
	}
	// Match the deterministic directory order rather than map iteration so new
	// tracks receive reproducible ascending indexes in their first scan.
	paths := make([]string, 0, len(inspected))
	for path := range inspected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		entry := inspected[path].source
		index, exists := retained[path]
		if !exists {
			if total >= maxSubtitleIdentities || highest >= maxSubtitleStreamIndex || len(retained) >= maxActiveSubtitles {
				state.warnings++
				continue
			}
			highest++
			total++
			index = highest
			retained[path] = index
		}
		_, err := tx.Exec(ctx, `INSERT INTO item_subtitles
			(item_id, root_id, stream_index, active, relative_path, file_identity, source_hash,
			 file_size, modified_at, change_time_ns, codec, language, title,
			 is_default, is_forced, is_hearing_impaired, mime_type)
			VALUES ($1,$2,$3,true,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (item_id, stream_index) DO UPDATE SET root_id = EXCLUDED.root_id,
			 relative_path = EXCLUDED.relative_path, file_identity = EXCLUDED.file_identity,
			 source_hash = EXCLUDED.source_hash, file_size = EXCLUDED.file_size,
			 modified_at = EXCLUDED.modified_at, change_time_ns = EXCLUDED.change_time_ns,
			 codec = EXCLUDED.codec, language = EXCLUDED.language, title = EXCLUDED.title,
			 is_default = EXCLUDED.is_default, is_forced = EXCLUDED.is_forced,
			 is_hearing_impaired = EXCLUDED.is_hearing_impaired, mime_type = EXCLUDED.mime_type`,
			itemID, entry.rootID, index, entry.relativePath, entry.identity, entry.Tag,
			entry.Size, entry.ModifiedAt, entry.changeTimeNs, entry.Codec, entry.Language, entry.Title,
			entry.IsDefault, entry.IsForced, entry.IsHearingImpaired, entry.MIMEType)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
