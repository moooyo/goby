package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
)

type scannedMediaRole string

const (
	scannedRoleOrdinary scannedMediaRole = "ordinary"
	scannedRoleTheme    scannedMediaRole = "theme"
	scannedRoleExtra    scannedMediaRole = "extra"
)

var errScannedMediaRoleConflict = errors.New("the stored media identity has a permanent incompatible role")

type scannedMediaInput struct {
	file          *os.File
	info          os.FileInfo
	stored        storedFile
	probe         *media.Info
	unchanged     bool
	checksVersion bool
}

// inspectScannedMedia is shared by ordinary, theme, and extra files. It retains the
// opened descriptor, probe version/ctime cache checks, and accepted music-source
// validation; callers must close a successful input after their final identity
// check and owned transaction. No owner NFO is read by this helper.
func (state *scanState) inspectScannedMedia(path, kind string, role scannedMediaRole) (*scannedMediaInput, error) {
	file, err := openScanFile(state.opened, path)
	if err != nil {
		state.warnings++
		return nil, nil
	}
	accepted := false
	defer func() {
		if !accepted {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		state.warnings++
		return nil, nil
	}
	state.task.job.Scanned++
	if err := state.store.persistProgress(state.task); err != nil {
		return nil, err
	}
	stored, err := state.findStoredFileForRole(filepath.ToSlash(path), info, role)
	if errors.Is(err, errScannedMediaRoleConflict) {
		state.warnings++
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if state.themes != nil && stored.id != "" {
		claims := state.themes.claimed
		if state.themeLibrary != nil {
			claims = state.themeLibrary.claimed
		}
		key := string(role) + ":" + state.root.id + ":" + filepath.ToSlash(path)
		if previous, exists := claims[stored.id]; exists && previous != key {
			stored = storedFile{}
		} else {
			claims[stored.id] = key
		}
	}
	probe := stored.media
	unchanged := !state.task.job.ForceProbe && probe != nil && stored.size == info.Size() && stored.modified != nil &&
		stored.modified.Equal(catalogModifiedTime(info)) && (stored.identity == "" || stored.identity == fileIdentity(info))
	versioned, checksVersion := state.store.prober.(interface{ CacheVersion() int })
	if checksVersion {
		unchanged = unchanged && probe.ProbeVersion == versioned.CacheVersion() &&
			probe.FileChangeTimeNs > 0 && probe.FileChangeTimeNs == media.FileChangeTime(info)
	}
	if versioned, ok := state.store.prober.(interface{ MusicMetadataVersion() int }); kind == "audio" && ok {
		unchanged = unchanged && probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == versioned.MusicMetadataVersion()
	}
	if !unchanged {
		probed, probeErr := state.store.prober.ProbeFile(state.task.ctx, file)
		if probeErr != nil {
			if state.task.ctx.Err() != nil {
				return nil, state.task.ctx.Err()
			}
			state.warnings++
			return nil, state.store.persistProgress(state.task)
		}
		probe = &probed
		after, statErr := file.Stat()
		if statErr != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) ||
			(checksVersion && media.FileChangeTime(after) != media.FileChangeTime(info)) {
			state.warnings++
			return nil, state.store.persistProgress(state.task)
		}
	}
	if err := state.task.ctx.Err(); err != nil {
		return nil, err
	}
	if kind == "audio" && probe.EmbeddedMusic != nil && probe.EmbeddedMusic.Version == media.CurrentMusicMetadataVersion {
		if !validTrackMusic(*probe.EmbeddedMusic) {
			state.warnings++
			return nil, state.store.persistProgress(state.task)
		}
		raw, err := json.Marshal(probe.EmbeddedMusic)
		if _, valid := acceptedTrackMusic(raw); err != nil || !valid {
			state.warnings++
			return nil, state.store.persistProgress(state.task)
		}
	}
	accepted = true
	return &scannedMediaInput{file: file, info: info, stored: stored, probe: probe,
		unchanged: unchanged, checksVersion: checksVersion}, nil
}

type themeCandidate struct {
	relative string
	kind     themePathKind
	layout   themePathLayout
}

type themeDirectoryScan struct {
	relative   string
	candidates []themeCandidate
	failed     bool
	overflow   bool
	owner      themeDirectoryOwner
	complete   bool
}

type themeDirectoryOwner struct {
	id, itemType string
}

type themeScan struct {
	markers      map[string]bool
	directories  map[string]os.FileInfo
	groups       map[string]*themeDirectoryScan
	owners       map[string]themeDirectoryOwner
	movies       map[string]map[string]bool
	seen         map[string]bool
	claimed      map[string]string
	pending      map[string]*themeDirectoryScan
	walkComplete bool
	issues       map[string]int
}

// A CollectionFolder is the only supported semantic owner shared across
// registered roots. Its bounded candidate set is published only after every
// root has supplied its complete owner inventory; never one root at a time.
type themeLibraryScan struct {
	roots      map[string]*scanState
	expected   []libraryRoot
	collection []*preparedThemeFile
	count      int
	failed     bool
	claimed    map[string]string
	issues     map[string]int
}

type preparedThemeFile struct {
	state     *scanState
	role      scannedMediaRole
	candidate themeCandidate
	input     *scannedMediaInput
	id        string
	owner     themeDirectoryOwner
	name      string
	itemType  string
	changed   bool
}

func (state *scanState) startThemeScan() error {
	state.themes = &themeScan{markers: make(map[string]bool), directories: make(map[string]os.FileInfo),
		groups: make(map[string]*themeDirectoryScan), owners: make(map[string]themeDirectoryOwner),
		movies: make(map[string]map[string]bool), seen: make(map[string]bool), claimed: make(map[string]string),
		pending: make(map[string]*themeDirectoryScan)}
	state.themes.issues = make(map[string]int)
	if state.themeLibrary != nil {
		state.themeLibrary.roots[state.root.id] = state
	}
	rows, err := state.store.pool.Query(state.task.ctx, `SELECT relative_path,is_directory FROM theme_reserved_paths WHERE root_id=$1
		UNION ALL SELECT i.relative_path,i.is_folder FROM items i JOIN item_theme_resources r ON r.resource_item_id=i.id WHERE i.root_id=$1`, state.root.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var relative string
		var directory bool
		if err := rows.Scan(&relative, &directory); err != nil {
			return err
		}
		if _, err := classifyThemePath(relative, 0); err != nil {
			return fmt.Errorf("%w: a retained theme path is invalid", ErrUnavailable)
		}
		state.themes.markers[relative] = state.themes.markers[relative] || directory
	}
	return rows.Err()
}

func (state *scanState) themePathReserved(relative string) bool {
	if state.themes == nil {
		return false
	}
	if _, exists := state.themes.markers[relative]; exists {
		return true
	}
	for index := strings.LastIndexByte(relative, '/'); index >= 0; index = strings.LastIndexByte(relative, '/') {
		relative = relative[:index]
		if state.themes.markers[relative] {
			return true
		}
	}
	return false
}

func (state *scanState) themeGroup(relative string) *themeDirectoryScan {
	relative = filepath.ToSlash(filepath.Clean(relative))
	group := state.themes.groups[relative]
	if group == nil {
		group = &themeDirectoryScan{relative: relative}
		state.themes.groups[relative] = group
	}
	return group
}

func (state *scanState) failThemeDirectory(relative string) {
	if state.themes != nil {
		state.themeGroup(relative).failed = true
	}
}

func (state *scanState) noteThemeIssue(reason string) {
	if state.themes != nil {
		state.themes.issues[reason]++
	}
}

func themeWarningMessage(shared *themeLibraryScan) string {
	issues := make(map[string]int)
	for _, state := range shared.roots {
		for reason, count := range state.themes.issues {
			issues[reason] += count
		}
	}
	for reason, count := range shared.issues {
		issues[reason] += count
	}
	keys := make([]string, 0, len(issues))
	for reason := range issues {
		keys = append(keys, reason)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, reason := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", issues[reason], reason))
	}
	if len(parts) == 0 {
		return ""
	}
	return "; theme owner groups retained: " + strings.Join(parts, "; ")
}

func (state *scanState) recordThemeDirectoryOwner(relative string, current hierarchy, folderType string) error {
	if state.themes == nil {
		return nil
	}
	owner := themeDirectoryOwner{id: current.parentID, itemType: folderType}
	if relative == "." {
		owner.itemType = "CollectionFolder"
		if current.albumID != "" {
			owner.id, owner.itemType = current.albumID, "MusicAlbum"
		} else if state.library.CollectionType == "music" || state.library.CollectionType == "mixed" {
			var id string
			err := state.store.pool.QueryRow(state.task.ctx, `SELECT id FROM items
				WHERE root_id=$1 AND relative_path='//album/root' AND type='MusicAlbum' AND is_folder AND `+
				ordinaryItemSQL("items"), state.root.id).Scan(&id)
			if err == nil {
				owner.id, owner.itemType = id, "MusicAlbum"
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
	}
	state.themes.owners[filepath.ToSlash(relative)] = owner
	return nil
}

func (state *scanState) claimedScannedIDs(relative string, role scannedMediaRole) []string {
	result := []string{}
	if state.themes == nil {
		return result
	}
	claims := state.themes.claimed
	if state.themeLibrary != nil {
		claims = state.themeLibrary.claimed
	}
	key := string(role) + ":" + state.root.id + ":" + relative
	for id, claimed := range claims {
		if claimed != key {
			result = append(result, id)
		}
	}
	return result
}

func (state *scanState) recordThemePrimary(relative, id, itemType string) {
	if state.themes == nil || itemType != "Movie" {
		return
	}
	directory := filepath.ToSlash(filepath.Dir(relative))
	if state.themes.movies[directory] == nil {
		state.themes.movies[directory] = make(map[string]bool)
	}
	state.themes.movies[directory][id] = true
}

func themeSnapshotEqual(before, after os.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) && before.Mode().Type() == after.Mode().Type() &&
		before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && media.FileChangeTime(before) == media.FileChangeTime(after)
}

func (state *scanState) classifyThemeDirectory(relative string, entries []os.DirEntry, info os.FileInfo) ([]os.DirEntry, error) {
	if state.themes == nil {
		return entries, nil
	}
	key := filepath.ToSlash(relative)
	state.themes.directories[key] = info
	ordinary := make([]os.DirEntry, 0, len(entries))
	markers := make(map[string]bool)
	var resources []themeCandidate
	var directories []string
	for _, entry := range entries {
		name := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		classification, err := state.classifyScannedThemePath(name, entry.Type())
		retained := state.themePathReserved(name)
		if err != nil {
			fileShape, _ := state.classifyScannedThemePath(name, 0)
			directoryShape, _ := state.classifyScannedThemePath(name, os.ModeDir)
			if retained || fileShape.Reserved || directoryShape.Reserved {
				state.warnings++
				state.failThemeDirectory(relative)
				continue
			}
			ordinary = append(ordinary, entry)
			continue
		}
		if !retained && !classification.Reserved {
			ordinary = append(ordinary, entry)
			continue
		}
		observed, observeErr := state.opened.Lstat(filepath.FromSlash(name))
		if observeErr != nil || (!observed.Mode().IsRegular() && !observed.IsDir()) || observed.IsDir() != entry.IsDir() {
			state.warnings++
			state.failThemeDirectory(relative)
			continue
		}
		markers[name] = entry.IsDir()
		if classification.Reserved && classification.Kind != themePathKindNone {
			resources = append(resources, themeCandidate{relative: name, kind: classification.Kind, layout: classification.Layout})
		} else if entry.IsDir() && classification.Reserved && classification.OwnerDirectory == key {
			directories = append(directories, name)
		}
	}
	after, err := state.opened.Lstat(relative)
	if err != nil || !themeSnapshotEqual(info, after) {
		return nil, fmt.Errorf("%w: theme classification directory changed during enumeration", ErrUnavailable)
	}
	if err := state.persistThemeMarkers(markers); err != nil {
		return nil, err
	}
	group := state.themeGroup(relative)
	for _, candidate := range resources {
		state.addThemeCandidate(group, candidate)
	}
	for _, directory := range directories {
		if err := state.enumerateThemeDirectory(group, directory); err != nil {
			if state.task.ctx.Err() != nil {
				return nil, state.task.ctx.Err()
			}
			state.warnings++
			group.failed = true
		}
	}
	return ordinary, nil
}

func (state *scanState) addThemeCandidate(group *themeDirectoryScan, candidate themeCandidate) {
	if len(group.candidates) >= MaxThemeResourcesPerOwner {
		group.overflow = true
		return
	}
	group.candidates = append(group.candidates, candidate)
}

func (state *scanState) enumerateThemeDirectory(group *themeDirectoryScan, relative string) error {
	parent, err := openRegisteredRoot(state.opened, filepath.Dir(relative))
	if err != nil {
		return err
	}
	defer parent.Close()
	directory, err := openScanFile(parent, filepath.Base(relative))
	if err != nil {
		return err
	}
	defer directory.Close()
	before, err := directory.Stat()
	if err != nil || !before.IsDir() {
		return fmt.Errorf("theme directory is unavailable")
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := filepath.ToSlash(filepath.Join(relative, entry.Name()))
		classification, err := state.classifyScannedThemePath(name, entry.Type())
		if err != nil {
			return err
		}
		if classification.Kind != themePathKindNone {
			state.addThemeCandidate(group, themeCandidate{relative: name, kind: classification.Kind, layout: classification.Layout})
		}
	}
	after, err := directory.Stat()
	current, currentErr := parent.Lstat(filepath.Base(relative))
	if err != nil || currentErr != nil || !themeSnapshotEqual(before, after) || !themeSnapshotEqual(before, current) {
		return fmt.Errorf("theme directory changed during enumeration")
	}
	state.themes.directories[relative] = before
	return nil
}

func (state *scanState) persistThemeMarkers(markers map[string]bool) error {
	return state.persistAuxiliaryMarkers(markers, false)
}

func (state *scanState) persistAuxiliaryMarkers(markers map[string]bool, extra bool) error {
	if len(markers) == 0 {
		return nil
	}
	table, retained := "theme_reserved_paths", state.themes.markers
	if extra {
		table, retained = "extra_reserved_paths", state.extras.markers
	}
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return err
	}
	defer rollback(tx)
	var changedOwners []string
	var albums []string
	keys := make([]string, 0, len(markers))
	for key := range markers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	directoryFlags := make([]bool, 0, len(keys))
	for _, relative := range keys {
		directoryFlags = append(directoryFlags, markers[relative] || retained[relative])
	}
	// Acquire the rows that will lose ordinary visibility before publishing
	// any marker. Bulk/direct UserData writers retain SHARE locks and recheck
	// their visibility after acquiring them, so classification has one order.
	if err := tx.QueryRow(state.task.ctx, `SELECT COALESCE(array_agg(id),'{}'::text[]),
		COALESCE(array_agg(DISTINCT parent_id) FILTER(WHERE type='Audio' AND parent_id IS NOT NULL),'{}'::text[])
		FROM (SELECT i.id,i.parent_id,i.type FROM items i WHERE i.root_id=$1 AND EXISTS(
			SELECT 1 FROM unnest($2::text[],$3::boolean[]) marker(relative_path,is_directory)
			WHERE i.relative_path=marker.relative_path OR ((marker.is_directory OR EXISTS(
				SELECT 1 FROM items old WHERE old.root_id=$1 AND old.relative_path=marker.relative_path AND old.is_folder))
				AND left(i.relative_path,length(marker.relative_path)+1)=marker.relative_path||'/'))
		ORDER BY i.id FOR UPDATE OF i) affected`, state.root.id, keys, directoryFlags).Scan(&changedOwners, &albums); err != nil {
		return err
	}
	for _, relative := range keys {
		var directory bool
		err := tx.QueryRow(state.task.ctx, `INSERT INTO `+table+`(root_id,relative_path,is_directory)
			VALUES($1,$2,$3 OR EXISTS(SELECT 1 FROM items WHERE root_id=$1 AND relative_path=$2 AND is_folder))
			ON CONFLICT(root_id,relative_path) DO UPDATE SET is_directory=`+table+`.is_directory OR EXCLUDED.is_directory
			RETURNING is_directory`, state.root.id, relative, markers[relative]).Scan(&directory)
		if err != nil {
			return err
		}
		markers[relative] = directory
	}
	if err := deactivateInvalidThemeChildren(state.task.ctx, tx, changedOwners); err != nil {
		return err
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return err
	}
	for relative, directory := range markers {
		retained[relative] = directory
	}
	for _, album := range albums {
		state.queueMusicParent(album)
	}
	return nil
}

// An owner move or promotion must not commit invalid active child shapes.
// Permanent inactive relationships and all user data remain untouched.
func deactivateInvalidThemeChildren(ctx context.Context, tx pgx.Tx, ownerIDs []string) error {
	if len(ownerIDs) == 0 {
		return nil
	}
	var locked []string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id),'{}'::text[]) FROM (
		SELECT resource.id FROM items resource JOIN item_theme_resources relationship ON relationship.resource_item_id=resource.id
		WHERE relationship.active AND relationship.owner_item_id=ANY($1::text[])
		ORDER BY resource.id FOR UPDATE OF resource) locked`, ownerIDs).Scan(&locked); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE item_theme_resources relationship SET active=false
		WHERE relationship.active AND relationship.owner_item_id=ANY($1::text[])
		AND NOT EXISTS(SELECT 1 FROM items resource WHERE resource.id=relationship.resource_item_id AND `+
		database.ThemeResourceItemSQL("resource", true)+`)`, ownerIDs)
	if err != nil {
		return err
	}
	return deactivateInvalidExtraChildren(ctx, tx, ownerIDs)
}

func themeProbeMatches(kind themePathKind, probe *media.Info) bool {
	if probe == nil {
		return false
	}
	audio, video := false, false
	for _, stream := range probe.Streams {
		if strings.EqualFold(stream.CodecType, "audio") {
			audio = true
		}
		if strings.EqualFold(stream.CodecType, "video") && !stream.IsAttachedPicture {
			video = true
		}
	}
	return kind == themePathKindSong && audio && !video || kind == themePathKindVideo && video
}

func closeThemeFiles(files []*preparedThemeFile) {
	for _, file := range files {
		if file.input.file != nil {
			_ = file.input.file.Close()
			file.input.file = nil
		}
	}
}

func (state *scanState) resolveThemeOwner(group *themeDirectoryScan) error {
	group.owner = state.themes.owners[group.relative]
	if group.relative == "." && group.owner.itemType == "CollectionFolder" {
		// Season-directory discovery may establish this synthetic root Series
		// after the initial directory owner was recorded. Its real directory is
		// still '.', just as the root MusicAlbum has an empty physical path.
		var id string
		err := state.store.pool.QueryRow(state.task.ctx, `SELECT id FROM items
			WHERE root_id=$1 AND relative_path='//series/root' AND type='Series' AND is_folder AND `+
			ordinaryItemSQL("items"), state.root.id).Scan(&id)
		if err == nil {
			group.owner = themeDirectoryOwner{id: id, itemType: "Series"}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if movies := state.themes.movies[group.relative]; len(movies) > 1 {
		return fmt.Errorf("theme directory contains multiple accepted movie owners")
	} else if len(movies) == 1 {
		for id := range movies {
			group.owner = themeDirectoryOwner{id: id, itemType: "Movie"}
		}
	}
	switch group.owner.itemType {
	case "Movie", "Series", "Season", "MusicAlbum", "Folder", "CollectionFolder":
	default:
		return fmt.Errorf("theme directory has no proven semantic owner")
	}
	if group.owner.id == "" || group.owner.itemType == "CollectionFolder" && group.owner.id != state.library.ID {
		return fmt.Errorf("theme directory owner identity is unavailable")
	}
	// Coexistence between song and video groups needs no precedence rule. Two
	// different song layouts, or competing theme.<extension> files, do. Until
	// that policy is proved, retain the owner's complete prior population.
	return validateThemeLayouts(group.candidates)
}

func (state *scanState) prepareThemeFiles(group *themeDirectoryScan) ([]*preparedThemeFile, error) {
	return state.prepareAuxiliaryFiles(group, scannedRoleTheme)
}

func (state *scanState) prepareAuxiliaryFiles(group *themeDirectoryScan, role scannedMediaRole) ([]*preparedThemeFile, error) {
	files := make([]*preparedThemeFile, 0, len(group.candidates))
	complete := false
	defer func() {
		if !complete {
			closeThemeFiles(files)
		}
	}()
	for _, candidate := range group.candidates {
		kind, itemType := "audio", "Audio"
		if candidate.kind == themePathKindVideo {
			kind, itemType = "video", "Video"
		}
		input, err := state.inspectScannedMedia(filepath.FromSlash(candidate.relative), kind, role)
		if err != nil {
			return nil, err
		}
		if input == nil {
			return nil, nil
		}
		file := &preparedThemeFile{state: state, role: role, candidate: candidate, input: input, owner: group.owner, itemType: itemType}
		files = append(files, file)
		if !themeProbeMatches(candidate.kind, input.probe) {
			state.warnings++
			return nil, nil
		}
		file.name = cleanName(strings.TrimSuffix(filepath.Base(candidate.relative), filepath.Ext(candidate.relative)))
		if candidate.kind == themePathKindSong && input.probe.EmbeddedMusic != nil &&
			input.probe.EmbeddedMusic.Version == media.CurrentMusicMetadataVersion && input.probe.EmbeddedMusic.Title != "" {
			file.name = input.probe.EmbeddedMusic.Title
		}
		stored := input.stored
		previousName, previousSort, previousOverview := stored.name, stored.sortName, stored.overview
		if stored.automatic != nil {
			previousName, previousSort, previousOverview = stored.automatic.Name, stored.automatic.SortName, stored.automatic.Overview
		}
		file.id = stored.id
		if file.id == "" {
			file.id, err = randomID()
			if err != nil {
				return nil, err
			}
		}
		file.changed = !input.unchanged || stored.id == "" || stored.rootID != state.root.id ||
			stored.path != filepath.Join(state.root.path, filepath.FromSlash(candidate.relative)) || stored.parentID != group.owner.id ||
			stored.itemType != itemType || previousName != file.name || previousSort != file.name || previousOverview != "" ||
			stored.indexNumber != 0 || stored.parentIndexNumber != 0 || !reflect.DeepEqual(stored.local, localMetadata{})
	}
	if err := state.verifyThemeDirectories(group.relative, false); err != nil {
		state.warnings++
		return nil, nil
	}
	if err := verifyPreparedThemeFiles(files); err != nil {
		state.warnings++
		return nil, nil
	}
	complete = true
	return files, nil
}

func verifyPreparedThemeFiles(files []*preparedThemeFile) error {
	for _, file := range files {
		if err := file.state.task.ctx.Err(); err != nil {
			return err
		}
		root, err := file.state.store.openLibraryRoot(file.state.root)
		if err != nil {
			return err
		}
		parent, err := openRegisteredRoot(root, filepath.Dir(filepath.FromSlash(file.candidate.relative)))
		_ = root.Close()
		if err != nil {
			return err
		}
		current, err := openScanFile(parent, filepath.Base(file.candidate.relative))
		_ = parent.Close()
		if err != nil {
			return err
		}
		info, err := current.Stat()
		if err != nil || !info.Mode().IsRegular() || !themeSnapshotEqual(file.input.info, info) {
			_ = current.Close()
			return fmt.Errorf("theme media descriptor or pathname changed before publication")
		}
		if file.input.file == nil {
			file.input.file = current
		} else {
			_ = current.Close()
			held, err := file.input.file.Stat()
			if err != nil || !themeSnapshotEqual(file.input.info, held) {
				return fmt.Errorf("opened theme media changed before publication")
			}
		}
	}
	return nil
}

func (state *scanState) verifyThemeDirectories(ownerDirectory string, all bool) error {
	root, err := state.store.openLibraryRoot(state.root)
	if err != nil {
		return err
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	beforeRoot := state.themes.directories["."]
	if err != nil || beforeRoot == nil || !rootInfo.IsDir() || !os.SameFile(beforeRoot, rootInfo) ||
		all && !themeSnapshotEqual(beforeRoot, rootInfo) {
		return fmt.Errorf("registered theme root changed during scanning")
	}
	for relative, before := range state.themes.directories {
		if !all && relative != ownerDirectory {
			if filepath.ToSlash(filepath.Dir(relative)) != ownerDirectory ||
				themePathDirectoryLayout(filepath.Base(relative)) == themePathLayoutNone {
				continue
			}
		}
		directory, err := openRegisteredRoot(root, filepath.FromSlash(relative))
		if err != nil {
			return err
		}
		after, err := directory.Stat(".")
		_ = directory.Close()
		if err != nil || !themeSnapshotEqual(before, after) {
			return fmt.Errorf("an enumerated theme source directory changed during scanning")
		}
	}
	return nil
}

func (state *scanState) publishThemeDirectory(relative string) error {
	if state.themes == nil {
		return nil
	}
	group := state.themeGroup(relative)
	group.complete = true
	if len(group.candidates) == 0 {
		return nil
	}
	if group.failed || group.overflow {
		if group.overflow {
			state.warnings++
			state.noteThemeIssue("resource count exceeded the complete owner bound")
		}
		if relative == "." && state.themeLibrary != nil {
			state.themeLibrary.failed = true
		}
		return nil
	}
	if err := state.resolveThemeOwner(group); err != nil {
		state.warnings++
		state.noteThemeIssue(err.Error())
		group.failed = true
		if relative == "." && state.themeLibrary != nil {
			state.themeLibrary.failed = true
		}
		return nil
	}
	collection := group.owner.itemType == "CollectionFolder"
	if collection && state.themeLibrary != nil {
		state.themeLibrary.count += len(group.candidates)
		if state.themeLibrary.count > MaxThemeResourcesPerOwner {
			state.themeLibrary.failed = true
			state.warnings++
			state.noteThemeIssue("cross-root collection count exceeded the complete owner bound")
			closeThemeFiles(state.themeLibrary.collection)
			state.themeLibrary.collection = nil
			return nil
		}
	}
	files, err := state.prepareThemeFiles(group)
	if err != nil {
		return err
	}
	if files == nil {
		group.failed = true
		state.noteThemeIssue("resource inspection or directory stability was incomplete")
		if collection && state.themeLibrary != nil {
			state.themeLibrary.failed = true
		}
		return nil
	}
	defer closeThemeFiles(files)
	if collection {
		if state.themeLibrary == nil {
			return fmt.Errorf("theme collection owner lacks its cross-root inventory")
		}
		// Keep only the bounded probe snapshots between roots. Reopen and
		// revalidate every descriptor together before the owner transaction.
		state.themeLibrary.collection = append(state.themeLibrary.collection, files...)
		return nil
	}
	deferred, err := state.store.publishThemeOwner(state.task, state.library, group.owner, files, nil)
	if errors.Is(err, ErrInvalidInput) {
		state.warnings++
		state.noteThemeIssue(err.Error())
		group.failed = true
		return nil
	}
	if err != nil {
		return err
	}
	if deferred {
		state.themes.pending[group.relative] = group
		state.noteThemeIssue("replacement requires a complete root before missing resources can become inactive")
	}
	return nil
}

func persistThemeFile(ctx context.Context, tx pgx.Tx, file *preparedThemeFile) error {
	opposite := "item_extra_resources"
	if file.role == scannedRoleExtra {
		opposite = "item_theme_resources"
	}
	var conflict bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+opposite+` WHERE resource_item_id=$1)`, file.id).Scan(&conflict); err != nil {
		return err
	}
	if conflict {
		return fmt.Errorf("%w: a permanently assigned auxiliary identity cannot change roles", ErrUnavailable)
	}
	if !file.changed {
		return nil
	}
	encoded, err := json.Marshal(file.input.probe)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO items(id,library_id,root_id,parent_id,name,sort_name,type,path,relative_path,
		index_number,parent_index_number,media,file_identity,file_size,modified_at,overview,local_metadata,local_metadata_hash,local_metadata_path)
		VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8,0,0,$9,$10,$11,$12,'',NULL,'','')
		ON CONFLICT(id) DO UPDATE SET root_id=EXCLUDED.root_id,parent_id=EXCLUDED.parent_id,name=EXCLUDED.name,
		sort_name=EXCLUDED.sort_name,type=EXCLUDED.type,path=EXCLUDED.path,relative_path=EXCLUDED.relative_path,is_folder=false,
		index_number=0,parent_index_number=0,media=EXCLUDED.media,file_identity=EXCLUDED.file_identity,file_size=EXCLUDED.file_size,
		modified_at=EXCLUDED.modified_at,overview='',local_metadata=NULL,local_metadata_hash='',local_metadata_path='',updated_at=now()`,
		file.id, file.state.library.ID, file.state.root.id, file.owner.id, file.name, file.itemType,
		filepath.Join(file.state.root.path, filepath.FromSlash(file.candidate.relative)), file.candidate.relative,
		encoded, fileIdentity(file.input.info), file.input.info.Size(), catalogModifiedTime(file.input.info))
	if err != nil {
		return err
	}
	// Preserve accepted tags of this resource and its current native controls.
	// No directory/movie NFO or parent genre/people data enters this write.
	if err := writeScannedMusicSource(ctx, tx, file.id, file.itemType, file.input.probe); err != nil {
		return err
	}
	return syncScannedMetadata(ctx, tx, file.id, scannedMetadataOptions{ForceEntities: file.input.stored.id == ""})
}

func uniqueThemeIDs(values []string) []string {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type themeActiveResource struct {
	ID       string `json:"id"`
	RootID   string `json:"root"`
	Relative string `json:"path"`
	Kind     string `json:"kind"`
}

type themeRowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readThemeActiveResources(ctx context.Context, source themeRowQuerier, ownerID string) ([]themeActiveResource, error) {
	var raw []byte
	if err := source.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'root',root_id,
		'path',relative_path,'kind',kind) ORDER BY id),'[]'::jsonb) FROM (
		SELECT i.id,i.root_id,i.relative_path,r.kind FROM item_theme_resources r JOIN items i ON i.id=r.resource_item_id
		WHERE r.owner_item_id=$1 AND r.active ORDER BY i.id LIMIT $2) active`, ownerID, MaxThemeResourcesPerOwner+1).Scan(&raw); err != nil {
		return nil, err
	}
	var result []themeActiveResource
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if len(result) > MaxThemeResourcesPerOwner {
		return nil, fmt.Errorf("%w: existing theme owner population exceeds its bound", ErrInvalidInput)
	}
	return result, nil
}

func validateThemeLayouts(candidates []themeCandidate) error {
	var songLayout themePathLayout
	directSongs := 0
	for _, candidate := range candidates {
		if candidate.kind != themePathKindSong {
			continue
		}
		if songLayout != "" && songLayout != candidate.layout {
			return fmt.Errorf("theme song layouts have unresolved precedence")
		}
		songLayout = candidate.layout
		if candidate.layout == themePathLayoutFile {
			directSongs++
			if directSongs > 1 {
				return fmt.Errorf("multiple direct theme files have unresolved precedence")
			}
		}
	}
	return nil
}

// themePathAbsent proves ENOENT at the first missing component while holding
// its existing parent. A symlink, non-directory parent, access error, or a
// changed parent is uncertainty, never evidence authorizing retirement.
func themePathAbsent(root *os.Root, relative string) (bool, error) {
	if _, err := classifyThemePath(relative, 0); err != nil {
		return false, err
	}
	current, err := root.OpenRoot(".")
	if err != nil {
		return false, err
	}
	defer func() { _ = current.Close() }()
	components := strings.Split(relative, "/")
	for index, component := range components {
		before, err := current.Stat(".")
		if err != nil {
			return false, err
		}
		entry, entryErr := current.Lstat(component)
		after, afterErr := current.Stat(".")
		if afterErr != nil || !themeSnapshotEqual(before, after) {
			return false, fmt.Errorf("theme absence parent changed while reading")
		}
		if errors.Is(entryErr, os.ErrNotExist) {
			return true, nil
		}
		if entryErr != nil {
			return false, entryErr
		}
		if index == len(components)-1 {
			return false, nil
		}
		if !entry.IsDir() || entry.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("theme absence path contains an unproved directory")
		}
		next, err := openRegisteredRoot(current, component)
		if err != nil {
			return false, err
		}
		_ = current.Close()
		current = next
	}
	return false, nil
}

type themePublicationPlan struct {
	active []themeActiveResource
	retire []string
}

func (s *Store) planThemePublication(task *scanTask, library Library, owner themeDirectoryOwner,
	files []*preparedThemeFile, expected []string, completeRoots map[string]bool, possibleRoots []string) (themePublicationPlan, bool, error) {
	active, err := readThemeActiveResources(task.ctx, s.pool, owner.id)
	plan := themePublicationPlan{active: active, retire: []string{}}
	if err != nil {
		return plan, false, err
	}
	expectedSet, incoming, possible := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for _, id := range expected {
		expectedSet[id] = true
	}
	for _, id := range possibleRoots {
		possible[id] = true
	}
	current, future := []themeCandidate{}, []themeCandidate{}
	for _, file := range files {
		incoming[file.id] = true
		current = append(current, file.candidate)
		future = append(future, file.candidate)
	}
	for _, resource := range active {
		if incoming[resource.ID] {
			continue
		}
		classification, err := classifyThemePath(resource.Relative, 0)
		if err != nil || classification.Kind == themePathKindNone || string(classification.Kind) != resource.Kind {
			return plan, false, fmt.Errorf("%w: an existing theme layout is not a proved direct candidate", ErrInvalidInput)
		}
		candidate := themeCandidate{relative: resource.Relative, kind: classification.Kind, layout: classification.Layout}
		if !expectedSet[resource.ID] && completeRoots[resource.RootID] {
			var record libraryRoot
			if err := s.pool.QueryRow(task.ctx, `SELECT id,library_id,path,allowed_path,relative_path FROM library_roots
				WHERE id=$1 AND library_id=$2`, resource.RootID, library.ID).Scan(&record.id, &record.libraryID, &record.path,
				&record.allowedPath, &record.relativePath); err != nil {
				return plan, false, err
			}
			root, err := s.openLibraryRoot(record)
			if err != nil {
				return plan, false, err
			}
			absent, absenceErr := themePathAbsent(root, resource.Relative)
			_ = root.Close()
			if absenceErr != nil || !absent {
				return plan, false, fmt.Errorf("%w: a present or uncertain theme source cannot be retired", ErrInvalidInput)
			}
			plan.retire = append(plan.retire, resource.ID)
			continue
		}
		current = append(current, candidate)
		if expectedSet[resource.ID] || !possible[resource.RootID] {
			future = append(future, candidate)
		}
	}
	if len(current) <= MaxThemeResourcesPerOwner && validateThemeLayouts(current) == nil {
		return plan, false, nil
	}
	// Old active resources without absence authority remain part of the
	// projected layout and count. A later complete-root replacement may resolve
	// the conflict; the current transaction must not publish only its new half.
	if len(future) <= MaxThemeResourcesPerOwner && validateThemeLayouts(future) == nil {
		return plan, true, nil
	}
	return plan, false, fmt.Errorf("%w: complete theme owner layouts or population are unsupported", ErrInvalidInput)
}

// publishThemeOwner is one atomic owner replacement, including both media
// kinds and every registered root contributing to this batch. Missing records
// can become inactive only for explicitly completed roots. With no retirement
// authority, a full 256-to-256 replacement is deferred rather than activating
// a temporary 512-resource population or publishing a truncated subset.
func (s *Store) publishThemeOwner(task *scanTask, library Library, owner themeDirectoryOwner,
	files []*preparedThemeFile, completedRoots map[string]bool, retainedIDs ...string) (bool, error) {
	if err := task.ctx.Err(); err != nil {
		return false, err
	}
	if len(files) > MaxThemeResourcesPerOwner {
		return false, fmt.Errorf("%w: theme owner candidate count exceeds the complete population bound", ErrInvalidInput)
	}
	ids := append([]string{}, retainedIDs...)
	publicationRoots := make(map[string]bool)
	for rootID := range completedRoots {
		publicationRoots[rootID] = true
	}
	for _, file := range files {
		if file.owner != owner || file.state.library.ID != library.ID {
			return false, fmt.Errorf("%w: a theme batch mixes semantic owners or libraries", ErrInvalidInput)
		}
		ids = append(ids, file.id)
		publicationRoots[file.state.root.id] = true
	}
	expected := uniqueThemeIDs(ids)
	if len(expected) != len(ids) || len(expected) > MaxThemeResourcesPerOwner {
		return false, fmt.Errorf("%w: theme owner identities are duplicated or excessive", ErrInvalidInput)
	}
	retiredRoots, possibleRoots := []string{}, []string{}
	for rootID := range publicationRoots {
		possibleRoots = append(possibleRoots, rootID)
		if completedRoots[rootID] {
			retiredRoots = append(retiredRoots, rootID)
		}
	}
	sort.Strings(retiredRoots)
	sort.Strings(possibleRoots)
	plan, deferred, err := s.planThemePublication(task, library, owner, files, expected, completedRoots, possibleRoots)
	if err != nil || deferred {
		return deferred, err
	}
	tx, err := s.beginOwnedTx(task.ctx)
	if err != nil {
		return false, err
	}
	defer rollback(tx)
	var status string
	var cancelled bool
	if err := tx.QueryRow(task.ctx, `SELECT status,cancel_requested FROM scan_jobs WHERE id=$1 AND library_id=$2 FOR UPDATE`,
		task.job.ID, library.ID).Scan(&status, &cancelled); err != nil {
		return false, err
	}
	if cancelled || status != "Running" || task.ctx.Err() != nil {
		return false, context.Canceled
	}
	// Lock both the owner and affected resource rows in a stable order. This
	// also serializes role activation/retirement with direct UserData writers.
	var locked []string
	if err := tx.QueryRow(task.ctx, `SELECT COALESCE(array_agg(id),'{}'::text[]) FROM (
		SELECT i.id FROM items i WHERE i.id=$1 OR i.id=ANY($2::text[]) OR i.id IN
		(SELECT resource_item_id FROM item_theme_resources WHERE owner_item_id=$1 AND active)
		ORDER BY i.id FOR UPDATE OF i) locked`, owner.id, expected).Scan(&locked); err != nil {
		return false, err
	}
	var ownerLibrary, ownerRoot, itemType string
	var folder, ordinary bool
	if err := tx.QueryRow(task.ctx, `SELECT library_id,COALESCE(root_id,''),type,is_folder,`+ordinaryItemSQL("items")+
		` FROM items WHERE id=$1`, owner.id).Scan(&ownerLibrary, &ownerRoot, &itemType, &folder, &ordinary); err != nil {
		return false, err
	}
	collection := owner.id == library.ID && itemType == "CollectionFolder" && folder
	if ownerLibrary != library.ID || !ordinary || itemType != owner.itemType ||
		(owner.itemType == "CollectionFolder" && !collection) {
		return false, fmt.Errorf("%w: theme owner changed before publication", ErrUnavailable)
	}
	for _, file := range files {
		if !collection && ownerRoot != file.state.root.id {
			return false, fmt.Errorf("%w: theme owner is outside the resource root", ErrUnavailable)
		}
	}
	activeNow, err := readThemeActiveResources(task.ctx, tx, owner.id)
	if err != nil {
		return false, err
	}
	if !reflect.DeepEqual(plan.active, activeNow) {
		return false, fmt.Errorf("%w: theme active identities changed after source verification", ErrUnavailable)
	}
	var invalid, remaining, possibleRemaining int
	if err := tx.QueryRow(task.ctx, `SELECT
		count(*) FILTER(WHERE NOT `+directItemSQL("i")+`),
		count(*) FILTER(WHERE NOT (i.id=ANY($2::text[])) AND NOT (COALESCE(i.root_id,'')=ANY($3::text[]))),
		count(*) FILTER(WHERE NOT (i.id=ANY($2::text[])) AND NOT (COALESCE(i.root_id,'')=ANY($4::text[])))
		FROM item_theme_resources r JOIN items i ON i.id=r.resource_item_id WHERE r.owner_item_id=$1 AND r.active`,
		owner.id, expected, retiredRoots, possibleRoots).Scan(&invalid, &remaining, &possibleRemaining); err != nil {
		return false, err
	}
	if invalid != 0 {
		return false, fmt.Errorf("%w: the existing active theme population has an invalid source shape", ErrUnavailable)
	}
	if possibleRemaining+len(expected) > MaxThemeResourcesPerOwner {
		return false, fmt.Errorf("%w: complete theme owner population exceeds its bound", ErrInvalidInput)
	}
	if remaining+len(expected) > MaxThemeResourcesPerOwner {
		return true, nil
	}
	if len(retainedIDs) != 0 {
		var count int
		if err := tx.QueryRow(task.ctx, `SELECT count(*) FROM item_theme_resources
			WHERE owner_item_id=$1 AND active AND resource_item_id=ANY($2::text[])`, owner.id, retainedIDs).Scan(&count); err != nil {
			return false, err
		}
		if count != len(retainedIDs) {
			return false, fmt.Errorf("%w: a retained theme identity changed owner or activity", ErrUnavailable)
		}
	}
	if len(plan.retire) != 0 {
		tag, err := tx.Exec(task.ctx, `UPDATE item_theme_resources SET active=false
			WHERE owner_item_id=$1 AND active AND resource_item_id=ANY($2::text[])`, owner.id, plan.retire)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() != int64(len(plan.retire)) {
			return false, fmt.Errorf("%w: the proved missing theme population changed", ErrUnavailable)
		}
	}
	roleChanged := make(map[string]bool)
	for _, file := range files {
		if err := persistThemeFile(task.ctx, tx, file); err != nil {
			return false, err
		}
		tag, err := tx.Exec(task.ctx, `INSERT INTO item_theme_resources(resource_item_id,owner_item_id,kind,active)
			VALUES($1,$2,$3,true) ON CONFLICT(resource_item_id) DO UPDATE
			SET owner_item_id=EXCLUDED.owner_item_id,kind=EXCLUDED.kind,active=true
			WHERE item_theme_resources.owner_item_id<>EXCLUDED.owner_item_id OR item_theme_resources.kind<>EXCLUDED.kind OR NOT item_theme_resources.active`,
			file.id, owner.id, string(file.candidate.kind))
		if err != nil {
			return false, err
		}
		roleChanged[file.id] = tag.RowsAffected() != 0
	}
	if err := deactivateInvalidThemeChildren(task.ctx, tx, expected); err != nil {
		return false, err
	}
	var activeCount, invalidCount, expectedCount int
	if err := tx.QueryRow(task.ctx, `SELECT count(*),count(*) FILTER(WHERE NOT `+directItemSQL("i")+`),
		count(*) FILTER(WHERE i.id=ANY($2::text[])) FROM item_theme_resources r JOIN items i ON i.id=r.resource_item_id
		WHERE r.owner_item_id=$1 AND r.active`, owner.id, expected).Scan(&activeCount, &invalidCount, &expectedCount); err != nil {
		return false, err
	}
	if activeCount > MaxThemeResourcesPerOwner || invalidCount != 0 || expectedCount != len(expected) {
		return false, fmt.Errorf("%w: the atomic theme publication does not have its complete valid active shape", ErrUnavailable)
	}
	if err := task.ctx.Err(); err != nil {
		return false, err
	}
	if err := tx.Commit(task.ctx); err != nil {
		return false, err
	}
	for _, file := range files {
		file.state.themes.seen[file.id] = true
		if file.input.stored.itemType == "Audio" {
			file.state.queueMusicParent(file.input.stored.parentID)
		}
		if file.input.stored.id == "" {
			task.job.Added++
		} else if file.changed || roleChanged[file.id] {
			task.job.Updated++
		}
	}
	return false, s.persistProgress(task)
}

func (state *scanState) finishThemeScan() error {
	if state.themes == nil || len(state.themes.markers) == 0 {
		return nil
	}
	if err := state.verifyThemeDirectories(".", true); err != nil {
		state.warnings++
		return nil
	}
	directories := make([]string, 0, len(state.themes.pending))
	for directory := range state.themes.pending {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	for _, directory := range directories {
		group := state.themes.pending[directory]
		if err := state.verifyThemeDirectories(".", true); err != nil {
			state.warnings++
			return nil
		}
		files, err := state.prepareThemeFiles(group)
		if err != nil {
			return err
		}
		if files == nil {
			return nil
		}
		deferred, err := state.store.publishThemeOwner(state.task, state.library, group.owner, files, map[string]bool{state.root.id: true})
		closeThemeFiles(files)
		if errors.Is(err, ErrInvalidInput) || deferred {
			state.warnings++
			state.noteThemeIssue("deferred replacement could not prove a complete supported owner set")
			return nil
		}
		if err != nil {
			return err
		}
	}
	if state.warnings != 0 || state.task.ctx.Err() != nil {
		return state.task.ctx.Err()
	}
	// Unknown, present, or uninspectable active resources are not absence.
	// In particular, historical nested layouts cannot be retired merely because
	// the new classifier declines to promote them as direct candidates.
	rows, err := state.store.pool.Query(state.task.ctx, `SELECT i.id,i.relative_path,r.owner_item_id,owner.type
		FROM item_theme_resources r JOIN items i ON i.id=r.resource_item_id JOIN items owner ON owner.id=r.owner_item_id
		WHERE i.root_id=$1 AND r.active AND r.owner_item_id<>$2 ORDER BY r.owner_item_id,i.id`, state.root.id, state.library.ID)
	if err != nil {
		return err
	}
	type retainedOwner struct {
		owner themeDirectoryOwner
		ids   []string
	}
	owners := make(map[string]*retainedOwner)
	for rows.Next() {
		var id, relative, ownerID, itemType string
		if err := rows.Scan(&id, &relative, &ownerID, &itemType); err != nil {
			rows.Close()
			return err
		}
		if owners[ownerID] == nil {
			owners[ownerID] = &retainedOwner{owner: themeDirectoryOwner{id: ownerID, itemType: itemType}}
		}
		if state.themes.seen[id] {
			owners[ownerID].ids = append(owners[ownerID].ids, id)
			continue
		}
		classification, classificationErr := classifyThemePath(relative, 0)
		_, pathErr := state.opened.Lstat(filepath.FromSlash(relative))
		if classificationErr != nil || classification.Kind == themePathKindNone || !errors.Is(pathErr, os.ErrNotExist) {
			state.warnings++
			state.noteThemeIssue("an unobserved active resource was present, unknown, or unavailable")
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if state.warnings != 0 {
		return nil
	}
	keys := make([]string, 0, len(owners))
	for ownerID := range owners {
		keys = append(keys, ownerID)
	}
	sort.Strings(keys)
	for _, ownerID := range keys {
		if err := state.verifyThemeDirectories(".", true); err != nil {
			state.warnings++
			return nil
		}
		owner := owners[ownerID]
		deferred, err := state.store.publishThemeOwner(state.task, state.library, owner.owner, nil,
			map[string]bool{state.root.id: true}, owner.ids...)
		if errors.Is(err, ErrInvalidInput) || deferred {
			state.warnings++
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) finishCollectionThemes(task *scanTask, library Library, shared *themeLibraryScan, completeRoots map[string]bool) (int, error) {
	if shared == nil {
		return 0, nil
	}
	defer closeThemeFiles(shared.collection)
	if shared.failed {
		return 0, nil
	}
	for _, root := range shared.expected {
		state := shared.roots[root.id]
		if state == nil || state.themes == nil || !state.themes.walkComplete {
			return 0, nil
		}
	}
	if len(shared.collection) > MaxThemeResourcesPerOwner || shared.count > MaxThemeResourcesPerOwner {
		return 1, nil
	}
	// Validate the combined layout policy before one CollectionFolder can
	// expose a partial cross-root population. The ordinary-owner candidates
	// were already published independently and are not combined here.
	layouts := make(map[themePathLayout]bool)
	directSongs := 0
	for _, file := range shared.collection {
		if file.candidate.kind == themePathKindSong {
			layouts[file.candidate.layout] = true
			if file.candidate.layout == themePathLayoutFile {
				directSongs++
			}
		}
	}
	if len(layouts) > 1 || directSongs > 1 {
		shared.issues["cross-root song layouts have unresolved precedence"]++
		return 1, nil
	}
	for _, file := range shared.collection {
		if err := file.state.verifyThemeDirectories(".", false); err != nil {
			completeRoots[file.state.root.id] = false
			return 1, nil
		}
	}
	if err := verifyPreparedThemeFiles(shared.collection); err != nil {
		if task.ctx.Err() != nil {
			return 0, task.ctx.Err()
		}
		return 1, nil
	}
	for rootID, complete := range completeRoots {
		if complete {
			if err := shared.roots[rootID].verifyThemeDirectories(".", true); err != nil {
				completeRoots[rootID] = false
				return 1, nil
			}
		}
	}
	// A missing CollectionFolder resource is retireable only if its stored
	// path is a recognized direct layout and that complete root proves absence.
	seen := make(map[string]bool, len(shared.collection))
	for _, file := range shared.collection {
		seen[file.id] = true
	}
	rows, err := s.pool.Query(task.ctx, `SELECT i.id,i.root_id,i.relative_path FROM items i
		JOIN item_theme_resources r ON r.resource_item_id=i.id WHERE r.owner_item_id=$1 AND r.active`, library.ID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id, rootID, relative string
		if err := rows.Scan(&id, &rootID, &relative); err != nil {
			rows.Close()
			return 0, err
		}
		if seen[id] || !completeRoots[rootID] {
			continue
		}
		classification, err := classifyThemePath(relative, 0)
		state := shared.roots[rootID]
		if err != nil || classification.Kind == themePathKindNone || state == nil {
			rows.Close()
			return 1, nil
		}
		root, err := s.openLibraryRoot(state.root)
		if err != nil {
			rows.Close()
			return 1, nil
		}
		_, pathErr := root.Lstat(filepath.FromSlash(relative))
		_ = root.Close()
		if !errors.Is(pathErr, os.ErrNotExist) {
			rows.Close()
			return 1, nil
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	deferred, err := s.publishThemeOwner(task, library, themeDirectoryOwner{id: library.ID, itemType: "CollectionFolder"},
		shared.collection, completeRoots)
	if errors.Is(err, ErrInvalidInput) || deferred {
		shared.issues["cross-root replacement requires a complete supported owner set"]++
		return 1, nil
	}
	return 0, err
}
