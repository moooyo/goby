package library

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
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
	imageInspection     *artwork.InspectionCache
	subtitleDirectories map[string]*subtitleDirectoryIndex
	musicParents        map[string]bool
	virtualFolders      map[string]scannedVirtualFolder
	themes              *themeScan
	themeLibrary        *themeLibraryScan
	extras              *extraScan
	reconciliation      *scanReconciliationEvidence
	reconciliationPass  *scanReconciliationPass
	sourceRoot          *scanSourceRootWitness
	walkIO              *PrimaryRootIO
	walkRow             rootBindingRow
	primaryReadMu       sync.Mutex
	primaryReadErr      error
	primaryReadParent   *scanState
}

type storedFile struct {
	id, rootID, relativePath, identity, parentID, path, name, itemType, sortName, overview string
	indexNumber, parentIndexNumber                                                         int
	size                                                                                   int64
	modified                                                                               *time.Time
	media                                                                                  *media.Info
	local                                                                                  localMetadata
	automatic                                                                              *MetadataValues
	scanSortName                                                                           *string
	hasLocalImages                                                                         bool
}

func (s *Store) scanLibrary(task *scanTask) (message string, resultErr error) {
	library, err := s.GetLibrary(task.ctx, task.job.LibraryID)
	if err != nil {
		return "Library is unavailable", err
	}
	rows, err := s.pool.Query(task.ctx, `SELECT id, library_id, path, allowed_path, relative_path
		FROM library_roots WHERE library_id = $1 ORDER BY path LIMIT $2`, library.ID, maxRegisteredRootBindingList+1)
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
	if len(roots) > maxRegisteredRootBindingList {
		return "Library directory count exceeds the complete scan limit; existing catalog records were retained", ErrUnavailable
	}
	if err := s.prepareScanOperationAuthority(task.ctx, task, roots); err != nil {
		return "Library storage approval or scan ownership changed; existing catalog records were retained", err
	}
	reconciliation, err := s.prepareScanReconciliation(task, roots)
	if err != nil {
		return "Library storage approval or scan ownership changed; existing catalog records were retained", err
	}
	defer func() {
		if err := reconciliation.Close(); err != nil {
			resultErr = errors.Join(resultErr, err)
			if message == "" {
				message = "Scan evidence cleanup could not be completed"
			}
		}
	}()
	warnings, failedRoots, numberingConflicts := 0, 0, 0
	themeOwners := &themeLibraryScan{roots: make(map[string]*scanState), expected: roots,
		claimed: make(map[string]string), issues: make(map[string]int)}
	defer func() { resultErr = errors.Join(resultErr, themeOwners.Close()) }()
	musicParents := make(map[string]bool)
	completeRoots := make(map[string]bool)
	// Reuse validated content metadata across this Store's scans. Each inspection
	// still fully reads and hashes its current source before the shared lookup.
	imageInspection := &s.imageInspection
	for _, root := range roots {
		if err := task.ctx.Err(); err != nil {
			return "Scan cancelled", err
		}
		opened, sourceRoot, err := reconciliation.openSourceRoot(s, root)
		if err != nil {
			var preparationFailure *scanReconciliationPreparationFailure
			if errors.As(err, &preparationFailure) {
				return "Library storage approval or scan ownership changed; existing catalog records were retained", err
			}
			failedRoots++
			continue
		}
		state := &scanState{store: s, task: task, library: library, root: root, opened: opened, sourceRoot: sourceRoot,
			themeLibrary: themeOwners, reconciliation: reconciliation.collector(), reconciliationPass: reconciliation,
			imageInspection: imageInspection}
		err = state.startThemeScan()
		if err == nil {
			err = state.startExtraScan()
		}
		if err == nil {
			err = state.walk(".", hierarchy{parentID: library.ID}, 0)
		}
		if err == nil && state.themes != nil {
			state.themes.walkComplete = true
		}
		if err == nil && state.warnings == 0 {
			err = state.finishExtraScan()
		}
		if err == nil && state.warnings == 0 {
			err = state.finishThemeScan()
		}
		closeErr := errors.Join(opened.Close(), sourceRoot.Close())
		err = errors.Join(err, state.primaryScanReadError(), closeErr)
		state.opened = nil
		state.sourceRoot = nil
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
			var recordingFailure *scanSeenRecordingError
			if errors.As(err, &recordingFailure) {
				return "Accepted scan identities could not be retained safely", err
			}
			var readFailure *primaryScanReadFailure
			if errors.As(err, &readFailure) || closeErr != nil {
				return "Scan input ownership or cleanup could not be completed", err
			}
			failedRoots++
		}
	}
	themeWarnings, err := s.finishCollectionThemes(task, library, themeOwners, completeRoots)
	if err != nil {
		return "Theme owner resources could not be published", err
	}
	warnings += themeWarnings
	for _, state := range themeOwners.roots {
		if err := state.primaryScanReadError(); err != nil {
			return "Scan input cleanup could not be completed", err
		}
		for parentID := range state.musicParents {
			musicParents[parentID] = true
		}
	}
	reconciliationMessage, err := reconciliation.finish(s, task, library, failedRoots == 0 && warnings == 0, musicParents)
	if err != nil {
		return "Missing catalog records could not be reconciled safely", err
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
	numberingMessage += themeWarningMessage(themeOwners)
	numberingMessage += reconciliationMessage
	if failedRoots > 0 {
		return fmt.Sprintf("%d media directories could not be scanned; existing catalog records were retained", failedRoots) + numberingMessage, ErrUnavailable
	}
	if warnings > 0 {
		return fmt.Sprintf("%d media or local metadata entries could not be inspected; previous valid metadata was retained", warnings) + numberingMessage, nil
	}
	return strings.TrimPrefix(reconciliationMessage, "; "), nil
}

func (state *scanState) walk(relative string, current hierarchy, depth int) (resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, state.primaryScanReadError()) }()
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	if depth > MaxThemeAncestorDepth {
		state.warnings++
		return nil
	}
	if state.walkIO == nil {
		row, err := state.readPrimaryScanAuthority(state.task.ctx)
		if err != nil {
			return err
		}
		if state.reconciliationPass != nil {
			if capture := state.reconciliationPass.byRoot[state.root.id]; capture != nil && !capture.row.same(row) {
				return ErrRootBindingConflict
			}
		}
		operation, err := state.store.prepareScanOperationRootIO(state.task.ctx, state.task,
			[]mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
		if err != nil {
			return err
		}
		state.walkIO, state.walkRow = operation, row
		defer func() {
			resultErr = errors.Join(resultErr, operation.Close())
			state.walkIO = nil
		}()
	}
	var directory *os.File
	var info os.FileInfo
	var entries []os.DirEntry
	// Hold the observed directory while its NFO and children are processed so
	// its identity cannot be recycled after a concurrent rename or removal.
	defer func() {
		if directory != nil {
			resultErr = errors.Join(resultErr, directory.Close())
		}
	}()
	err := state.walkIO.Run(state.task.ctx, state.root.id, primaryio.Background, func(ctx context.Context) error {
		fresh, err := state.readPrimaryScanAuthority(ctx)
		if err != nil || !state.walkRow.same(fresh) {
			return errors.Join(err, ErrRootBindingConflict)
		}
		directory, err = openScanFile(state.opened, relative)
		if err != nil {
			return err
		}
		info, err = directory.Stat()
		if err != nil || !info.IsDir() {
			return fmt.Errorf("media directory is unavailable")
		}
		if state.reconciliation != nil {
			_ = state.reconciliation.BeginDirectoryObservationFor(ctx, state.root.id, relative, directory, info)
		}
		entries, err = readScanDirectoryEntries(ctx, directory)
		if err == nil && state.reconciliation != nil {
			_ = state.reconciliation.RecordDirectoryFor(ctx, state.root.id, relative, info, entries)
		}
		return err
	})
	if err != nil {
		return err
	}
	entries, err = state.classifyExtraDirectory(relative, entries, info)
	if err == nil {
		entries, err = state.classifyThemeDirectory(relative, entries, info)
	}
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
	folderWarnings := state.warnings
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
	if state.warnings != folderWarnings {
		state.failThemeDirectory(relative)
	}
	if err := state.recordThemeDirectoryOwner(relative, current, folderType); err != nil {
		return err
	}
	probes := state.newScanProbeWindow()
	if probes != nil {
		// A failed prepare or publication must cancel and join the other probe
		// before any directory or root descriptor can be retired.
		defer func() { resultErr = errors.Join(resultErr, probes.close()) }()
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
			if probes != nil {
				if err := probes.flush(); err != nil {
					return err
				}
			}
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
		kind := scannedMediaKind(entry.Name(), state.library.CollectionType)
		if kind == "" {
			continue
		}
		if probes != nil {
			if err := probes.submit(path, kind, current); err != nil {
				return err
			}
		} else {
			fileWarnings := state.warnings
			if err := state.scanFile(path, kind, current); err != nil {
				return err
			}
			if state.warnings != fileWarnings {
				state.failThemeDirectory(relative)
			}
		}
	}
	if probes != nil {
		if err := probes.flush(); err != nil {
			return err
		}
	}
	if err := state.publishThemeDirectory(relative); err != nil {
		return err
	}
	if state.reconciliation != nil && state.warnings == 0 {
		if err := state.walkIO.Run(state.task.ctx, state.root.id, primaryio.Background, func(ctx context.Context) error {
			fresh, err := state.readPrimaryScanAuthority(ctx)
			if err != nil || !state.walkRow.same(fresh) {
				return errors.Join(err, ErrRootBindingConflict)
			}
			_ = state.reconciliation.CompleteDirectoryFor(ctx, state.root.id, relative)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (state *scanState) scanFile(path, kind string, current hierarchy) (resultErr error) {
	input, err := state.inspectScannedMedia(path, kind, scannedRoleOrdinary)
	if err != nil || input == nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, input.close()) }()
	return state.publishScannedMedia(path, kind, current, input)
}

// publishScannedMedia runs only on the scan worker. Probe workers never mutate
// counters, claims, hierarchy, sidecars, evidence, or the catalog.
func (state *scanState) publishScannedMedia(path, kind string, current hierarchy, input *scannedMediaInput) (resultErr error) {
	if input == nil || input.primary == nil {
		return scanReadFailure(ErrUnavailable)
	}
	for {
		warnings := state.warnings
		committed := false
		err := func() (resultErr error) {
			operation, err := input.primary.preparePublicationIO()
			if err != nil {
				return scanReadFailure(err)
			}
			input.primary.publicationIO = operation
			defer func() {
				resultErr = errors.Join(resultErr, closeScanPublicationIO(operation))
				input.primary.publicationIO = nil
			}()
			return state.publishScannedMediaAttempt(path, kind, current, input, &committed)
		}()
		if !committed && scanProbeSourceChangedOnly(err) {
			state.warnings++
			return state.store.persistProgress(state.task)
		}
		if committed || !sidecarAdmissionRetryable(err) {
			return err
		}
		// The attempt has rolled back and closed its publication handle. The
		// original bounded input remains owned until actual scan retirement.
		state.warnings = warnings
		if err := state.waitSidecarScanAdmission(); err != nil {
			return scanReadFailure(err)
		}
	}
}

func (state *scanState) publishScannedMediaAttempt(path, kind string, current hierarchy, input *scannedMediaInput, committed *bool) (resultErr error) {
	completionRequired, completionChecked := false, false
	defer func() {
		if completionRequired && !completionChecked {
			if resultErr != nil {
				// Failed sidecar work still forces canonical cancellation/accepted
				// prefix repair without replacing the original failure.
				resultErr = errors.Join(resultErr, state.store.checkCachedTaskScanProgress(state.task))
			} else {
				resultErr = state.store.maybePersistProgress(state.task)
			}
		}
	}()
	info, stored, probe := input.info, input.stored, input.probe
	var err error
	unchanged := input.unchanged
	relative := filepath.ToSlash(path)
	name := cleanName(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	itemType, parentID, indexNumber, parentIndex := "Movie", current.parentID, 0, 0
	parentNumberDefined := false
	virtualSeriesID, virtualSeriesName := "", ""
	needVirtualSeries, needVirtualSeason := false, false
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
				virtualSeriesID = current.seriesID
				needVirtualSeries = virtualSeriesID == ""
				if needVirtualSeries {
					virtualSeriesName = cleanName(matches[1])
					if virtualSeriesName == "" {
						virtualSeriesName = filepath.Base(state.root.path)
					}
				}
				parentID = current.seasonID
				needVirtualSeason = parentID == "" || current.seasonNumber != parentIndex
			} else if current.seasonID != "" {
				parentIndex = current.seasonNumber
				parentNumberDefined = true
			}
		}
	}
	candidates, nfoKind := fileNFOCandidates(path, itemType)
	// Sidecar absence is authoritative only while this media pathname still
	// names the opened file. A concurrent directory move is not NFO deletion.
	var nfoObservation localNFOObservation
	var sidecarObservation cachedSidecarObservation
	var currentInfo os.FileInfo
	var pathErr error
	if err := input.primary.runPublicationMetadata(input.primary.publicationIO, func(ctx context.Context) error {
		var err error
		nfoObservation, err = state.observeLocalNFO(ctx, candidates, nfoKind)
		if err != nil {
			return err
		}
		currentInfo, pathErr = state.opened.Lstat(path)
		if unchanged && stored.id != "" && !stored.hasLocalImages && pathErr == nil && sameMediaSourceFile(info, currentInfo) {
			sidecarObservation, err = state.observeCachedSidecarAbsence(ctx, path, itemType, probe, currentInfo)
			if err != nil {
				return err
			}
		}
		// NFO and directory observation may have overlapped a media replacement.
		// Even an ordinary fast-path fallback must finish with a fresh pathname.
		currentInfo, pathErr = state.opened.Lstat(path)
		return ctx.Err()
	}); err != nil {
		if scanProbeSourceChangedOnly(err) {
			return err
		}
		return scanReadFailure(err)
	}
	if sidecarObservation.sourceChanged || pathErr != nil || !currentInfo.Mode().IsRegular() || !sameMediaSourceFile(info, currentInfo) {
		state.warnings++
		return state.store.persistProgress(state.task)
	}
	// Hierarchy selection is computed above, but source rejection must precede
	// every virtual-folder write for cached and newly probed media alike.
	if needVirtualSeries {
		virtualSeriesID, err = state.folder("//series/"+strings.ToLower(virtualSeriesName), "", virtualSeriesName, "Series", state.library.ID, 0)
		if err != nil {
			return err
		}
	}
	if needVirtualSeason {
		parentID, err = state.folder(fmt.Sprintf("//season/%s/%d", virtualSeriesID, parentIndex), "", fmt.Sprintf("Season %d", parentIndex), "Season", virtualSeriesID, parentIndex)
		if err != nil {
			return err
		}
	}
	local := state.applyLocalNFO(nfoObservation, nfoKind, stored.local)
	if itemType == "Episode" {
		local, indexNumber, parentIndex = state.episodeNumbering(local, indexNumber, parentIndex, parentNumberDefined)
	}
	name, sortName, overview := describeFromLocal(name, local)
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
	// The existing stored-file query captures the generated or retained explicit
	// key with the same snapshot as automatic metadata and settings. Compare it
	// only when the source Name is unchanged and has no explicit NFO sort title.
	// Publication still derives from the complete incoming name under the current
	// owner transaction.
	comparisonSortName := sortName
	if previousName == name && (local.value == nil || local.value.SortName == "") && stored.scanSortName != nil {
		comparisonSortName = *stored.scanSortName
	}
	changed := !unchanged || stored.path != fullPath || stored.parentID != parentID || previousName != name || stored.itemType != itemType ||
		previousSort != comparisonSortName || previousOverview != overview || previousIndex != indexNumber || previousParentIndex != parentIndex ||
		stored.local.hash != local.hash || stored.local.path != local.path || !reflect.DeepEqual(stored.local.value, local.value)
	if stored.id != "" && !changed {
		completionRequired = state.task.job.TaskChildID != ""
		sidecarsUnchanged := false
		if sidecarObservation.absent {
			state.retainCachedSidecarObservation(sidecarObservation)
			// The observation has released its phase and every new descriptor.
			// Any active subtitle row needs the ordinary deletion transaction.
			sidecarsUnchanged, err = state.cachedSubtitleScanEmpty(stored.id, path, sidecarObservation.primary)
			if err != nil {
				return err
			}
		}
		if !sidecarsUnchanged {
			if err := state.scanSubtitles(stored.id, path, probe); err != nil {
				return err
			}
		}
		combineCompletion := state.task.job.TaskChildID != "" && !stored.hasLocalImages &&
			EffectiveLibraryOptions(state.library).EnableLocalImages
		if !combineCompletion {
			if sidecarsUnchanged {
				if EffectiveLibraryOptions(state.library).EnableLocalImages {
					if err := state.store.CheckOwnership(state.task.ctx); err != nil {
						return err
					}
				}
			} else {
				if err := state.scanImagesWithKnownAbsence(stored.id, itemType, path, false, !stored.hasLocalImages); err != nil {
					return err
				}
			}
		}
		if probe != nil {
			if err := state.scanEmbeddedArtwork(stored.id, itemType, path, input.file, *probe); err != nil {
				return err
			}
		}
		state.recordThemePrimary(path, stored.id, itemType)
		if err := state.recordScanSeen(stored.id); err != nil {
			return err
		}
		checked := false
		if combineCompletion {
			// Only a task-owned visit with no stored local images moves images
			// last. Apply the checkpoint policy after complete stable absence;
			// no sidecar or staging write follows this completion boundary.
			completionCheck := func() error {
				completionChecked = true
				err := state.store.maybePersistProgress(state.task)
				checked = err == nil
				return err
			}
			if sidecarsUnchanged {
				if err := completionCheck(); err != nil {
					return err
				}
			} else {
				if err := state.scanImagesWithKnownAbsence(stored.id, itemType, path, false, true, completionCheck); err != nil {
					return err
				}
			}
		}
		// Long sidecar observations can make the checkpoint due after entry.
		// Cached visits otherwise retain their counters in memory.
		if checked {
			return state.task.ctx.Err()
		}
		completionChecked = true
		return state.store.maybePersistProgress(state.task)
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
	defer func() {
		if !*committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	// Every primary write fences the immutable operation identity before root
	// or item locks, including independent scans using the synchronous path.
	relation, err := lockScanPublicationProgress(scanProbeAuthorityTx{ctx: state.task.ctx, tx: tx}, state.task)
	if err != nil {
		return err
	}
	var publicationProgress *taskScanRelation
	if state.task.job.TaskChildID != "" {
		publicationProgress = &relation
	}
	if input.authority != nil {
		if err := state.checkScanProbeAuthorityWithRelation(tx, path, input, &relation); err != nil {
			return err
		}
	} else if err := state.store.checkScanOperationRootTx(state.task.ctx, tx, state.task, input.primary.row); err != nil {
		return err
	}
	beforeCatalog, err := readScanCatalogItem(state.task.ctx, tx, id)
	if err != nil {
		return err
	}
	if beforeCatalog.present && beforeCatalog.change.LibraryID != state.library.ID {
		return fmt.Errorf("%w: scanned item belongs to another library", ErrUnavailable)
	}
	beforeAuxiliary, err := readAuxiliaryChildCatalogSnapshot(state.task.ctx, tx, id)
	if err != nil {
		return err
	}
	// Repeating the same facts need not change the item version. A successful
	// forced probe still repairs associations and publishes accepted progress below.
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
		 modified_at = EXCLUDED.modified_at, updated_at = now()
		WHERE ROW(items.root_id, items.parent_id, items.name, items.sort_name, items.type, items.path,
			items.is_folder, items.relative_path, items.index_number, items.parent_index_number, items.media,
			items.overview, items.local_metadata, items.local_metadata_hash, items.local_metadata_path,
			items.file_identity, items.file_size, items.modified_at)
		IS DISTINCT FROM ROW(EXCLUDED.root_id, EXCLUDED.parent_id, EXCLUDED.name, EXCLUDED.sort_name,
			EXCLUDED.type, EXCLUDED.path, false, EXCLUDED.relative_path, EXCLUDED.index_number,
			EXCLUDED.parent_index_number, EXCLUDED.media, EXCLUDED.overview, EXCLUDED.local_metadata,
			EXCLUDED.local_metadata_hash, EXCLUDED.local_metadata_path, EXCLUDED.file_identity,
			EXCLUDED.file_size, EXCLUDED.modified_at)`, id, state.library.ID, state.root.id,
		parentID, name, sortName, itemType, fullPath, relative, indexNumber, parentIndex,
		mediaJSON, fileIdentity(info), info.Size(), catalogModifiedTime(info), overview, localJSON, local.hash, local.path)
	if err != nil {
		return err
	}
	if err := writeScannedMusicSource(state.task.ctx, tx, id, itemType, probe); err != nil {
		return err
	}
	// A successful explicit refresh also repairs derived entity associations
	// from the accepted projection, retaining the current overrides and locks.
	if err := syncScannedMetadata(state.task.ctx, tx, id, scannedMetadataOptions{ForceEntities: stored.id == "" || state.task.job.ForceProbe}); err != nil {
		return err
	}
	if err := deactivateInvalidThemeChildren(state.task.ctx, tx, []string{id}); err != nil {
		return err
	}
	afterCatalog, err := readScanCatalogItem(state.task.ctx, tx, id)
	if err != nil {
		return err
	}
	if err := recordScanCatalogChange(tx, state.library.ID, beforeCatalog, afterCatalog, state.task.job.ForceProbe); err != nil {
		return err
	}
	if err := beforeAuxiliary.record(state.task.ctx, tx, nil); err != nil {
		return err
	}
	candidateProgress := state.task.job
	if stored.id == "" {
		candidateProgress.Added++
	} else {
		candidateProgress.Updated++
	}
	progressInItemTx := false
	var committedProgress Job
	if publicationProgress != nil {
		// The task rows were locked before catalog rows. Publish their accepted
		// counters atomically with this primary item, without changing memory
		// until its transaction has actually committed.
		committedProgress, err = writeScanPublicationProgress(scanProbeAuthorityTx{ctx: state.task.ctx, tx: tx}, *publicationProgress, candidateProgress)
		if err != nil {
			return err
		}
		progressInItemTx = true
	} else {
		// Independent scans retain their existing primary-item checkpoint.
		err = tx.QueryRow(state.task.ctx, `UPDATE scan_jobs SET scanned = $2, added = $3, updated = $4
			WHERE id = $1 AND task_child_id IS NULL AND status = 'Running' AND NOT cancel_requested
			RETURNING true`, state.task.job.ID, candidateProgress.Scanned, candidateProgress.Added, candidateProgress.Updated).Scan(&progressInItemTx)
		if errors.Is(err, pgx.ErrNoRows) {
			// Do not commit a primary item after its scan job stopped accepting
			// progress; finalization will resolve the terminal job state.
			return ErrUnavailable
		} else if err != nil {
			return err
		}
	}
	if input.authority != nil {
		if err := state.task.ctx.Err(); err != nil {
			return err
		}
		if state.store.closing.Load() {
			return ErrUnavailable
		}
		if err := state.revalidateScanProbeStorage(tx, path, input); err != nil {
			return err
		}
		if err := state.task.ctx.Err(); err != nil {
			return err
		}
		if state.store.closing.Load() {
			return ErrUnavailable
		}
	} else if err := input.primary.revalidatePublicationSource(tx); err != nil {
		return err
	}
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	if state.store.closing.Load() {
		return ErrUnavailable
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return err
	}
	*committed = true
	savedProgress := candidateProgress
	if publicationProgress != nil {
		// Probe workers may still read the immutable operation identity. Refresh
		// accepted progress without rewriting those shared identity fields.
		state.task.job.Scanned, state.task.job.Added, state.task.job.Updated =
			committedProgress.Scanned, committedProgress.Added, committedProgress.Updated
		savedProgress = committedProgress
	} else {
		state.task.job.Added, state.task.job.Updated = candidateProgress.Added, candidateProgress.Updated
	}
	state.task.recordSavedProgress(savedProgress, time.Now())
	if progressInItemTx {
		state.store.notifyScanUpdate()
	}
	if publicationProgress != nil {
		completionRequired = true
	}
	if err := state.scanSubtitles(id, path, probe); err != nil {
		return err
	}
	// A newly allocated ID absent before the successful primary write had no
	// older image rows: item_images requires an existing item through its FK.
	// Renames reuse stored.id and must retain that item's captured image state.
	knownNoLocalImages := stored.id != "" && !stored.hasLocalImages || stored.id == "" && !beforeCatalog.present
	if err := state.scanImagesWithKnownAbsence(id, itemType, path, false, knownNoLocalImages); err != nil {
		return err
	}
	if probe != nil {
		if err := state.scanEmbeddedArtwork(id, itemType, path, input.file, *probe); err != nil {
			return err
		}
	}
	state.recordThemePrimary(path, id, itemType)
	if err := state.recordScanSeen(id); err != nil {
		return err
	}
	completionChecked = true
	if publicationProgress != nil {
		// A committed primary followed by sidecars has a distinct final task
		// fence. Keep accepted counters atomic and repair a later cancellation.
		return state.store.checkCachedTaskScanProgress(state.task)
	}
	return state.store.maybePersistProgress(state.task)
}

func (state *scanState) folder(relative, path, name, itemType, parentID string, indexNumber int, nfoRelative ...string) (string, error) {
	for {
		warnings, conflicts := state.warnings, state.numberingConflicts
		committed := false
		id, err := state.folderAttempt(relative, path, name, itemType, parentID, indexNumber, &committed, nfoRelative...)
		if committed || !sidecarAdmissionRetryable(err) {
			return id, err
		}
		// The failed attempt has rolled back and retired its source witnesses.
		// Capacity waiting never retains catalog ownership or a transaction.
		state.warnings, state.numberingConflicts = warnings, conflicts
		if err := state.waitSidecarScanAdmission(); err != nil {
			return "", scanReadFailure(err)
		}
	}
}

func (state *scanState) folderAttempt(relative, path, name, itemType, parentID string, indexNumber int, committed *bool,
	nfoRelative ...string) (_ string, resultErr error) {
	if !strings.HasPrefix(relative, "//") && (state.themePathReserved(filepath.ToSlash(relative)) || state.extraPathReserved(filepath.ToSlash(relative))) {
		return "", fmt.Errorf("%w: a permanently reserved auxiliary path cannot become an ordinary folder", ErrUnavailable)
	}
	metadataPath := relative
	if strings.HasPrefix(relative, "//") {
		metadataPath = ""
	}
	if len(nfoRelative) != 0 {
		metadataPath = nfoRelative[0]
	}
	virtual := scannedVirtualFolder{path: path, name: name, itemType: itemType, parentID: parentID, indexNumber: indexNumber}
	if metadataPath == "" && strings.HasPrefix(relative, "//") {
		if cached, exists := state.virtualFolders[relative]; exists && cached.sameInput(virtual) {
			if _, err := state.readPrimaryScanAuthority(state.task.ctx); err != nil {
				return "", err
			}
			if err := state.recordScanSeen(cached.id); err != nil {
				return "", err
			}
			return cached.id, nil
		}
	}
	// A source-backed visit or different input supersedes an earlier virtual
	// publication at this path, even if the old input appears again later.
	delete(state.virtualFolders, relative)
	var source *scanFolderSource
	if metadataPath != "" {
		var err error
		source, err = state.prepareFolderSource(metadataPath)
		if err != nil {
			return "", scanReadFailure(err)
		}
		defer func() { resultErr = errors.Join(resultErr, source.Close()) }()
	}
	local, err := state.folderLocalMetadataWithSource(relative, metadataPath, itemType, indexNumber, source)
	if err != nil {
		return "", err
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
	granted, err := state.readPrimaryScanAuthority(state.task.ctx)
	if err != nil {
		return "", err
	}
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		if !*committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	if _, err := lockScanPublicationProgress(scanProbeAuthorityTx{ctx: state.task.ctx, tx: tx}, state.task); err != nil {
		return "", err
	}
	if err := state.store.checkScanOperationRootTx(state.task.ctx, tx, state.task, granted); err != nil {
		return "", err
	}
	beforeCatalog, err := readScanCatalogFolder(state.task.ctx, tx, state.root.id, relative)
	if err != nil {
		return "", err
	}
	if beforeCatalog.present && beforeCatalog.change.LibraryID != state.library.ID {
		return "", fmt.Errorf("%w: scanned folder belongs to another library", ErrUnavailable)
	}
	auxiliaryID := id
	if beforeCatalog.present {
		auxiliaryID = beforeCatalog.change.ItemID
	}
	beforeAuxiliary, err := readAuxiliaryChildCatalogSnapshot(state.task.ctx, tx, auxiliaryID)
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(state.task.ctx, `INSERT INTO items
		(id, library_id, root_id, parent_id, name, sort_name, type, path, relative_path, is_folder, index_number,
		 overview, local_metadata, local_metadata_hash, local_metadata_path)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,true,$10,$11,$12,$13,$14)
		ON CONFLICT (root_id, relative_path) DO UPDATE SET parent_id = EXCLUDED.parent_id,
		type = EXCLUDED.type,
		is_folder = true, media = NULL, file_identity = '', file_size = 0, modified_at = NULL,
		parent_index_number = 0,
		local_metadata = EXCLUDED.local_metadata,
		local_metadata_hash = EXCLUDED.local_metadata_hash, local_metadata_path = EXCLUDED.local_metadata_path,
		path = EXCLUDED.path, index_number = EXCLUDED.index_number, updated_at = now()
		WHERE ROW(items.parent_id, items.type, items.is_folder, items.media, items.file_identity,
			items.file_size, items.modified_at, items.parent_index_number, items.local_metadata,
			items.local_metadata_hash, items.local_metadata_path, items.path, items.index_number)
		IS DISTINCT FROM ROW(EXCLUDED.parent_id, EXCLUDED.type, true, NULL::jsonb, ''::text,
			0::bigint, NULL::timestamptz, 0, EXCLUDED.local_metadata,
			EXCLUDED.local_metadata_hash, EXCLUDED.local_metadata_path, EXCLUDED.path, EXCLUDED.index_number)
		RETURNING id`, id, state.library.ID, state.root.id, parentID, name, sortName, itemType, path, relative, indexNumber,
		overview, localJSON, local.hash, local.path).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) && beforeCatalog.present {
		// The conflicting row was locked before the upsert. A distinctness guard
		// retains its identity even when PostgreSQL returns no updated row.
		id, err = beforeCatalog.change.ItemID, nil
	}
	if err != nil {
		return "", err
	}
	if itemType != "MusicAlbum" {
		if err := writeScannedMusicSource(state.task.ctx, tx, id, itemType, nil); err != nil {
			return "", err
		}
	}
	// Existing display fields may contain administrator or online overlays.
	// Supply the incoming automatic base directly instead of transiently
	// replacing those display fields on every physical-directory visit.
	if err := syncScannedMetadata(state.task.ctx, tx, id, scannedMetadataOptions{
		Base: &scannedMetadataBase{Name: name, SortName: sortName, Overview: overview}, ForceEntities: id == insertID,
	}); err != nil {
		return "", err
	}
	if err := deactivateInvalidThemeChildren(state.task.ctx, tx, []string{id}); err != nil {
		return "", err
	}
	afterCatalog, err := readScanCatalogItem(state.task.ctx, tx, id)
	if err != nil {
		return "", err
	}
	// ForceProbe refreshes media files. A directory has no successful probe to
	// invalidate, so its notifications still require changed effective facts.
	if err := recordScanCatalogChange(tx, state.library.ID, beforeCatalog, afterCatalog, false); err != nil {
		return "", err
	}
	if err := recordMusicAlbumReferenceChanges(state.task.ctx, tx, state.library.ID, id, beforeCatalog, afterCatalog); err != nil {
		return "", err
	}
	if err := beforeAuxiliary.record(state.task.ctx, tx, nil); err != nil {
		return "", err
	}
	if source != nil {
		if err := source.final(state.task.ctx, tx); err != nil {
			return "", scanReadFailure(err)
		}
	}
	if err := state.task.ctx.Err(); err != nil {
		return "", err
	}
	if state.store.closing.Load() {
		return "", ErrUnavailable
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return "", err
	}
	*committed = true
	if itemType == "MusicAlbum" {
		state.queueMusicParent(id)
	}
	if metadataPath != "" {
		// Use this committed folder's exact identity. Raw image rows include
		// obsolete roots that the public image projection may no longer expose.
		knownNoLocalImages := !beforeCatalog.present && id == insertID ||
			beforeCatalog.present && beforeCatalog.change.ItemID == id && !beforeCatalog.hasLocalImages
		if err := state.scanImagesWithKnownAbsence(id, itemType, metadataPath, true, knownNoLocalImages); err != nil {
			return "", err
		}
	}
	if err := state.recordScanSeen(id); err != nil {
		return "", err
	}
	if metadataPath == "" && strings.HasPrefix(relative, "//") {
		state.cacheVirtualFolder(relative, virtual, id)
	}
	return id, nil
}

const storedFileColumns = `id, root_id, relative_path, file_identity, file_size, modified_at, media, COALESCE(parent_id, ''), path, name, type,
	sort_name, overview, index_number, parent_index_number, local_metadata, local_metadata_hash, local_metadata_path,
	(SELECT jsonb_build_object('Name', ms.automatic->'Name', 'SortName', ms.automatic->'SortName',
		'Overview', ms.automatic->'Overview', 'IndexNumber', ms.automatic->'IndexNumber',
		'ParentIndexNumber', ms.automatic->'ParentIndexNumber',
		'ScanSortName',CASE WHEN ms.automatic_sort_name_explicit THEN COALESCE(ms.automatic->>'SortName','')
		ELSE (SELECT CASE WHEN cardinality(sort_remove_words)>0
			THEN goby_generated_sort_name(ms.automatic->>'Name',sort_remove_words) END FROM managed_settings WHERE id=1) END)
		FROM item_metadata_state ms WHERE ms.item_id = items.id),
	EXISTS (SELECT 1 FROM item_images im WHERE im.item_id = items.id)`

func readStoredFile(row rowScanner) (storedFile, error) {
	return readStoredFileAccepted(row, nil)
}

func readStoredFileAccepted(row rowScanner, acceptID func(string) bool) (storedFile, error) {
	var item storedFile
	var raw, localRaw, automaticRaw []byte
	err := row.Scan(&item.id, &item.rootID, &item.relativePath, &item.identity, &item.size, &item.modified, &raw, &item.parentID, &item.path, &item.name, &item.itemType,
		&item.sortName, &item.overview, &item.indexNumber, &item.parentIndexNumber, &localRaw, &item.local.hash, &item.local.path, &automaticRaw, &item.hasLocalImages)
	if err != nil {
		return storedFile{}, err
	}
	// A claim-excluded row was previously filtered by PostgreSQL. Reject it
	// before decoding metadata so it cannot introduce a new parse failure.
	if acceptID != nil && !acceptID(item.id) {
		return storedFile{}, pgx.ErrNoRows
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
		var sorting struct{ ScanSortName *string }
		if err := json.Unmarshal(automaticRaw, &sorting); err != nil {
			return storedFile{}, err
		}
		item.scanSortName = sorting.ScanSortName
	}
	return item, nil
}

func (state *scanState) findStoredFile(relative string, info os.FileInfo) (storedFile, error) {
	return state.findStoredFileForRole(relative, info, scannedRoleOrdinary)
}

func (state *scanState) findStoredFileForRole(relative string, info os.FileInfo, role scannedMediaRole) (storedFile, error) {
	visibility := ordinaryItemSQL("items")
	switch role {
	case scannedRoleTheme:
		visibility = "NOT EXISTS(SELECT 1 FROM item_extra_resources role WHERE role.resource_item_id=items.id)"
	case scannedRoleExtra:
		visibility = "NOT EXISTS(SELECT 1 FROM item_theme_resources role WHERE role.resource_item_id=items.id)"
	case scannedRoleOrdinary:
	default:
		return storedFile{}, fmt.Errorf("%w: unknown scanned media role", ErrInvalidInput)
	}
	// Read role compatibility and metadata in one snapshot of the unique path.
	// Reject a permanent opposite role before claims or JSON decoding, including
	// inactive resources whose pathname must never acquire a fresh identity.
	var mode pgx.QueryExecMode
	if role == scannedRoleOrdinary {
		mode = state.store.scanPooledReadMode
	}
	args := []any{mode, state.root.id, relative}
	if mode == 0 {
		args = args[1:]
	}
	row := scannedRoleRow{row: state.store.pool.QueryRow(state.task.ctx,
		"SELECT "+storedFileColumns+", ("+visibility+") FROM items WHERE root_id = $1 AND relative_path = $2",
		args...)}
	stored, err := readStoredFileAccepted(row, func(id string) bool { return state.scannedIDAvailable(id, relative, role) })
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
	// Keep claim exclusion in SQL before LIMIT so claimed identities cannot
	// consume the existing rename-candidate window.
	excluded := state.claimedScannedIDs(relative, role)
	rows, err := state.store.pool.Query(state.task.ctx, "SELECT "+storedFileColumns+` FROM items
		WHERE library_id = $1 AND file_identity = $2 AND NOT is_folder AND file_size = $3
		AND modified_at = $4 AND NOT (id=ANY($5::text[])) AND `+visibility+` ORDER BY created_at, id LIMIT 8`,
		state.library.ID, identity, info.Size(), catalogModifiedTime(info), excluded)
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
		absent, err := state.scannedRenameCandidateAbsent(candidate)
		if err != nil {
			return storedFile{}, err
		}
		if absent {
			return candidate, nil
		}
	}
	return storedFile{}, nil
}

// Catalog lookup has completed before this source observation acquires I/O.
// An inaccessible old path is not absence and cannot transfer its item ID.
func (state *scanState) scannedRenameCandidateAbsent(candidate storedFile) (_ bool, resultErr error) {
	granted, err := state.store.readScanOperationRoot(state.task.ctx, state.task, candidate.rootID)
	if errors.Is(err, ErrRootBindingConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	operation := state.walkIO
	if candidate.rootID != state.root.id || operation == nil {
		operation, err = state.store.prepareScanOperationRootIO(state.task.ctx, state.task,
			[]mediaSourceRootHint{{root: granted.root, bindingRevision: granted.revision}})
		if err != nil {
			return false, err
		}
		defer func() { resultErr = errors.Join(resultErr, scanReadFailure(operation.Close())) }()
	} else if !state.walkRow.same(granted) {
		return false, ErrRootBindingConflict
	}
	absent := false
	err = operation.Run(state.task.ctx, candidate.rootID, primaryio.Background, func(work context.Context) (resultErr error) {
		current, err := state.store.readScanOperationAuthority(work, state.task, granted.root)
		if err != nil {
			return err
		}
		if !granted.same(current) {
			return ErrRootBindingConflict
		}
		oldRoot := state.opened
		if candidate.rootID != state.root.id {
			oldRoot, err = state.store.openScanOperationRoot(work, state.task, granted.root)
			if err != nil {
				if !state.store.Available() || errors.Is(err, ErrTaskScanInactive) {
					return err
				}
				if work.Err() != nil || errors.Is(err, errSidecarRetirementUnknown) {
					return errors.Join(err, work.Err())
				}
				return nil
			}
			defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(work, oldRoot)) }()
		}
		_, err = oldRoot.Lstat(filepath.FromSlash(candidate.relativePath))
		absent = errors.Is(err, os.ErrNotExist)
		return work.Err()
	})
	return absent, err
}

func ignoredName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~") || strings.HasSuffix(lower, ".tmp") || strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".partial") || strings.HasSuffix(lower, ".download") || strings.HasSuffix(name, "~")
}

func cleanName(name string) string {
	return strings.Join(strings.Fields(strings.NewReplacer(".", " ", "_", " ").Replace(name)), " ")
}

func containsAudio(entries []os.DirEntry) bool {
	// The walker already filtered these entries using their root-relative
	// theme classification. Reclassifying a basename changes that context and
	// can reject ordinary Linux names such as C:Track.mp3.
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
