package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
	"github.com/moooyo/goby/internal/systemevents"
)

var errMetadataIngestChanged = errors.New("local metadata import no longer matches an accepted source")

func metadataIngestUncertainOnly(err error) bool {
	if err == nil || errors.Is(err, errSidecarRetirementUnknown) || errors.Is(err, errSidecarRollbackUnknown) ||
		errors.Is(err, media.ErrProcessRetirementUnknown) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var retirement *storageObservationRetirementFailure
	if errors.As(err, &retirement) {
		return false
	}
	if errors.Is(err, errMetadataIngestChanged) || ordinarySidecarSourceChange(err) {
		return true
	}
	return false
}

// SnapshotLocalMetadataIngestChildren includes only libraries with accepted
// work. An unsuccessful scan cannot authorize a second source operation.
func SnapshotLocalMetadataIngestChildren(tx OwnedTx, runID string) (int64, error) {
	if tx == nil || !validTaskChildID(runID) {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		SELECT md5($1||':'||l.id),$1,l.id,l.name,(row_number() OVER (ORDER BY l.id)-1)::integer
		FROM task_runs run CROSS JOIN libraries l WHERE run.id=$1 AND EXISTS(
		SELECT 1 FROM scan_jobs j JOIN local_metadata_ingest q ON q.scan_job_id=j.id JOIN items i ON i.id=q.item_id
		WHERE j.library_id=l.id AND i.library_id=l.id AND j.status='Completed' AND j.finished_at<=run.created_at
		AND (q.import_nfo OR q.import_images) AND `+database.CatalogPathVisibleSQL("i")+`
		AND `+scanIngestAutomaticGenerationSQL+`)`, runID)
	return tag.RowsAffected(), err
}

type metadataIngestTarget struct {
	id, jobID, rootID, relative, itemPath, kind, parentID string
	fileIdentity                                          string
	fileSize, changeNS                                    int64
	modified                                              *time.Time
	folder, ordinary, publishing, sourceCurrent           bool
	request                                               scanMetadataImport
}

// Automatic runs do not restart a generation explicitly stopped by an
// administrator. A later successful scan or a deliberate manual task start
// can retry it. Shutdown and a bounded runtime never suppress recovery.
const scanIngestAutomaticGenerationSQL = `(run.source IN ('manual','compatibility') OR NOT EXISTS(
	SELECT 1 FROM task_runs stopped JOIN task_run_children scope ON scope.run_id=stopped.id
	WHERE stopped.task_id=run.task_id AND stopped.id<>run.id AND scope.library_id=j.library_id
	AND stopped.stop_reason='administrator' AND scope.state='cancelled'
	AND stopped.created_at>=j.finished_at))`

const metadataIngestTargetSQL = `SELECT i.id,q.scan_job_id,COALESCE(i.root_id,''),i.relative_path,i.path,i.type,COALESCE(i.parent_id,''),
	i.file_identity,i.file_size,i.modified_at,COALESCE((i.media->>'FileChangeTimeNs')::bigint,0),i.is_folder,`

var metadataIngestTargetBase = metadataIngestTargetSQL + ordinaryItemSQL("i") + `,
		EXISTS(SELECT 1 FROM media_operations p WHERE p.source_item_id=i.id AND p.publication_phase IN ('prepared','catalog_committed')),
		(q.source_key=` + metadataIngestSourceKeySQL + `),
		q.directory_identity,q.index_number,q.parent_index_number,q.parent_number_defined,q.import_nfo,q.import_images,q.enrich_online
		FROM local_metadata_ingest q JOIN scan_jobs j ON j.id=q.scan_job_id JOIN items i ON i.id=q.item_id
		JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.library_id=$1 AND j.library_id=$1 AND j.status='Completed' AND (q.import_nfo OR q.import_images)
		AND ` + database.CatalogPathVisibleSQL("i")

var (
	metadataIngestTargetReadOne = metadataIngestTargetBase + ` AND i.id=$2 AND q.scan_job_id=$3`
	metadataIngestTargetLock    = metadataIngestTargetReadOne + ` FOR UPDATE OF i,q`
	metadataIngestTargetPage    = metadataIngestTargetBase + ` AND q.item_id>$2 AND EXISTS(
		SELECT 1 FROM task_runs run WHERE run.id=$4 AND j.finished_at<=run.created_at
		AND ` + scanIngestAutomaticGenerationSQL + `) ORDER BY q.item_id LIMIT $3`
)

func readMetadataIngestTarget(row rowScanner) (metadataIngestTarget, error) {
	var target metadataIngestTarget
	err := row.Scan(&target.id, &target.jobID, &target.rootID, &target.relative, &target.itemPath, &target.kind, &target.parentID,
		&target.fileIdentity, &target.fileSize, &target.modified, &target.changeNS, &target.folder, &target.ordinary, &target.publishing, &target.sourceCurrent,
		&target.request.directoryIdentity, &target.request.index, &target.request.parentIndex, &target.request.parentDefined,
		&target.request.nfo, &target.request.images, &target.request.online)
	return target, err
}

func (target metadataIngestTarget) matches(current metadataIngestTarget) bool {
	if target.modified == nil || current.modified == nil {
		if target.modified != current.modified {
			return false
		}
	} else if !target.modified.Equal(*current.modified) {
		return false
	}
	target.modified, current.modified = nil, nil
	return target == current
}

func (target metadataIngestTarget) sourcePath(root libraryRoot) (string, bool) {
	if target.publishing || !target.ordinary || !target.sourceCurrent || target.rootID != root.id {
		return "", false
	}
	if target.kind == "Series" && target.folder && target.relative == tvRootSeriesPath && target.itemPath == "" {
		return ".", true
	}
	if target.folder && target.kind != "Series" && target.kind != "Season" && target.kind != "Folder" ||
		!target.folder && target.kind != "Movie" && target.kind != "Episode" {
		return "", false
	}
	path := filepath.FromSlash(target.relative)
	return path, validImageScanPath(path) && path != "." && path == filepath.Clean(path) &&
		filepath.Join(root.path, path) == target.itemPath
}

func (target metadataIngestTarget) matchesFile(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && target.modified != nil && target.changeNS > 0 &&
		info.Size() == target.fileSize && fileIdentity(info) == target.fileIdentity &&
		catalogModifiedTime(info).Equal(*target.modified) && media.FileChangeTime(info) == target.changeNS
}

// ImportLocalScanMetadata uses a live generic task grant, never a completed
// scan capability. Bounded pages release their SQL snapshot before opening media.
func (s *Store) ImportLocalScanMetadata(ctx context.Context, childID, libraryID string, fence AnalysisFence, report func(ProviderProgress) error) (resultErr error) {
	if ctx == nil || fence == nil || report == nil || !metadataIdentifier(libraryID) {
		return ErrInvalidInput
	}
	var runID string
	checkedFence := func(tx OwnedTx) error {
		if err := fence(tx); err != nil {
			return err
		}
		if runID != "" {
			return nil
		}
		return tx.QueryRow(`SELECT run.id FROM task_run_children child JOIN task_runs run ON run.id=child.run_id
			WHERE child.id=$1 AND child.library_id=$2 AND run.task_key=$3`, childID, libraryID, TaskLocalMetadataIngestKey).Scan(&runID)
	}
	ctx, release, err := s.BeginTaskSourceOperation(systemevents.WithDerived(ctx), childID, checkedFence)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, release()) }()
	grant := taskSourceGrant(ctx)
	if grant.libraryID != libraryID {
		return ErrInvalidInput
	}
	library, err := s.GetLibrary(ctx, libraryID)
	if err != nil {
		return err
	}
	progress := ProviderProgress{}
	var warnings bool
	// Group each bounded page by physical directory. Keep only one immutable
	// listing so flat libraries do not reread a complete directory per item.
	var images metadataIngestImageListing
	const pageSize = 32
	for after := ""; ; {
		rows, err := s.pool.Query(ctx, metadataIngestTargetPage, libraryID, after, pageSize, runID)
		if err != nil {
			return err
		}
		page := make([]metadataIngestTarget, 0, pageSize)
		for rows.Next() {
			target, err := readMetadataIngestTarget(rows)
			if err != nil {
				rows.Close()
				return err
			}
			page = append(page, target)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		// Preserve the high-water ID even when processing this page out of order.
		after = page[len(page)-1].id
		sort.SliceStable(page, func(i, j int) bool {
			left, right := page[i], page[j]
			if left.rootID != right.rootID {
				return left.rootID < right.rootID
			}
			return left.directoryPath() < right.directoryPath()
		})
		for _, target := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			itemChanged := false
			for attempt := 0; ; attempt++ {
				changed, err := s.importLocalMetadataTarget(ctx, grant, library, target, &images)
				itemChanged = itemChanged || changed
				if sidecarAdmissionRetryable(err) {
					if attempt == 2 {
						warnings = true
						break
					}
					if err := waitSidecarRetryBackoff(ctx); err != nil {
						return err
					}
					current, err := readMetadataIngestTarget(s.pool.QueryRow(ctx, metadataIngestTargetReadOne, libraryID, target.id, target.jobID))
					if errors.Is(err, pgx.ErrNoRows) {
						break
					}
					if err != nil {
						return err
					}
					target = current
					continue
				}
				if err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if metadataIngestUncertainOnly(err) {
						warnings = true
					} else {
						return err
					}
				}
				break
			}
			progress.Processed++
			if itemChanged {
				progress.Updated++
			}
			if err := report(progress); err != nil {
				return err
			}
		}
		if len(page) < pageSize {
			break
		}
	}
	if warnings {
		return errors.New("some local metadata could not be verified and remains pending")
	}
	return nil
}

type metadataIngestImageListing struct {
	rootID, directory string
	index             *imageDirectoryIndex
}

func (target metadataIngestTarget) directoryPath() string {
	if target.relative == tvRootSeriesPath {
		return "."
	}
	path := filepath.FromSlash(target.relative)
	if target.folder {
		return path
	}
	return filepath.Dir(path)
}

func (s *Store) importLocalMetadataTarget(ctx context.Context, grant *internalTaskSourceGrant, library Library,
	target metadataIngestTarget, listing *metadataIngestImageListing) (_ bool, resultErr error) {
	row := grant.roots[target.rootID]
	path, valid := target.sourcePath(row.root)
	if !valid {
		return false, errMetadataIngestChanged
	}
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	if err != nil {
		return false, err
	}
	var lease *libraryRootLease
	var opened *os.Root
	defer func() {
		cleanup := operation.Context(ctx)
		if opened != nil {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(cleanup, opened))
		}
		if lease != nil {
			resultErr = errors.Join(resultErr, closeDirectoryPrimaryResource(cleanup, lease.Close))
		}
		resultErr = errors.Join(resultErr, operation.Close())
	}()
	directoryPath := target.directoryPath()
	var directoryInfo os.FileInfo
	verify := func(work context.Context) error {
		return verifyMetadataIngestSource(work, lease, opened, target, directoryPath, path, directoryInfo)
	}
	state := &scanState{store: s, library: library, root: row.root, imageInspection: &s.imageInspection}
	options := EffectiveLibraryOptions(library)
	var observation localNFOObservation
	var proof nfoIngestProof
	var kind, pairRootIdentity string
	importNFO := target.request.nfo && options.EnableLocalMetadata
	if err := operation.Run(ctx, row.root.id, primaryio.Background, func(work context.Context) (readErr error) {
		var err error
		lease, err = s.leaseLibraryRootContext(work, row.root)
		if err != nil {
			return err
		}
		opened, err = lease.Open()
		if err != nil {
			return err
		}
		state.opened = opened
		dir, err := openRegisteredRoot(opened, directoryPath)
		if err != nil {
			return errors.Join(errMetadataIngestChanged, err)
		}
		defer func() { readErr = errors.Join(readErr, closeDirectoryPrimaryRoot(work, dir)) }()
		directoryInfo, err = dir.Stat(".")
		if err != nil || directoryInfo == nil || fileIdentity(directoryInfo) != target.request.directoryIdentity {
			return errors.Join(errMetadataIngestChanged, err)
		}
		if err := verify(work); err != nil {
			return err
		}
		if importNFO {
			if target.kind == "Episode" {
				if _, paired := episodeFilePair(path); paired {
					info, err := opened.Stat(".")
					if err != nil {
						return errors.Join(errMetadataIngestChanged, err)
					}
					pairRootIdentity = fileIdentity(info)
				}
			}
			var candidates []string
			if target.folder {
				candidates, kind = folderNFOCandidate(".", target.kind)
			} else {
				candidates, kind = fileNFOCandidates(filepath.Base(path), target.kind)
			}
			observation, proof, err = observeMetadataIngestNFO(work, dir, directoryPath, candidates)
			if err != nil {
				return err
			}
		}
		return verify(work)
	}); err != nil {
		return false, err
	}
	state.directoryIdentities = map[string]os.FileInfo{directoryPath: directoryInfo}
	var pairSource episodePairSource
	var binding *episodePairBinding
	if importNFO && target.kind == "Episode" {
		file := episodePairFile{Identity: target.fileIdentity, Size: target.fileSize,
			ModifiedNS: target.modified.UnixNano(), ChangeNS: target.changeNS}
		pairSource = newEpisodePairSource(row, path, target.parentID, pairRootIdentity, target.request, file)
		binding, err = pairSource.readBinding(ctx, s.pool, false)
		if err != nil {
			return false, err
		}
	}
	check := func(work context.Context, tx pgx.Tx) error {
		return s.checkMetadataIngestTarget(work, tx, grant, library, row, target)
	}
	var changed bool
	// Disabling a reader does not verify a pending NFO or allow the previous
	// rating and tags to pass as current evidence for the source.
	uncertainNFO := target.request.nfo && !importNFO
	finish := func(work context.Context, tx pgx.Tx) error {
		return s.completeLocalMetadataImport(work, tx, grant, library, target, !uncertainNFO, true)
	}
	if importNFO {
		changed, err = s.publishMetadataIngestNFO(ctx, operation, check, grant, verify, lease, target, directoryPath, kind, observation, proof, state, pairSource, binding)
		if err != nil {
			if !scanPublicationErrorOnly(err, errMetadataIngestInvalidNFO) {
				return false, err
			}
			uncertainNFO = true
		} else {
			if !target.request.images {
				return changed, nil
			}
			target.request.nfo = false
		}
	}
	if target.request.images && options.EnableLocalImages {
		if listing.rootID == target.rootID && listing.directory == directoryPath && listing.index != nil &&
			sameSubtitleDirectoryInfo(directoryInfo, listing.index.info) {
			state.imageDirectories = map[string]*imageDirectoryIndex{directoryPath: listing.index}
		}
		before := state.warnings
		authority := &imageRefreshAuthority{
			prepare: func() (*PrimaryRootIO, rootBindingRow, error) {
				child, err := operation.Fork(ctx)
				return child, row, err
			},
			check: check,
			finish: func(work context.Context, tx pgx.Tx) error {
				if state.warnings != before {
					return checkTaskIngestFence(work, tx, grant)
				}
				return finish(work, tx)
			},
			prove: verify,
		}
		if err := state.refreshLocalImages(ctx, authority, target.id, target.kind, path, target.folder, false); err != nil {
			*listing = metadataIngestImageListing{}
			return changed, err
		}
		*listing = metadataIngestImageListing{rootID: target.rootID, directory: directoryPath, index: state.imageDirectories[directoryPath]}
		if state.warnings != before || uncertainNFO {
			return changed, errMetadataIngestChanged
		}
		return true, nil
	}
	if uncertainNFO {
		return changed, errMetadataIngestChanged
	}
	return changed, s.finishMetadataIngest(ctx, check, finish)
}

// In the same transaction that accepted the sidecar, retire only its phase
// and signal the next task if the remaining local work is now complete.
func (s *Store) completeLocalMetadataImport(ctx context.Context, tx pgx.Tx, grant *internalTaskSourceGrant,
	library Library, target metadataIngestTarget, nfoDone, imagesDone bool) error {
	if nfoDone && target.request.nfo {
		if err := mergeAuxiliaryCatalogChanges(tx, []CatalogChange{metadataIngestVisibilityChange(target, library.ID)}, false); err != nil {
			return err
		}
	}
	pending := target.request
	pending.nfo = pending.nfo && !nfoDone
	pending.images = pending.images && !imagesDone
	if pending.pending() {
		if _, err := tx.Exec(ctx, `UPDATE local_metadata_ingest SET import_nfo=$3,import_images=$4
			WHERE item_id=$1 AND scan_job_id=$2`, target.id, target.jobID, pending.nfo, pending.images); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `DELETE FROM local_metadata_ingest WHERE item_id=$1 AND scan_job_id=$2`, target.id, target.jobID); err != nil {
		return err
	}
	if !pending.nfo && !pending.images {
		var event systemevents.Event
		switch {
		case pending.online && s.enrichOnlineMetadata:
			event = systemevents.OnlineMetadataIngestRequested
		case EffectiveLibraryOptions(library).EnableOnlineImages:
			event = systemevents.OnlineImagesRequested
		}
		if event != "" {
			if err := systemevents.Record(taskSourcePublicationTx{ctx: ctx, tx: tx}.Exec, event); err != nil {
				return err
			}
		}
	}
	return checkTaskIngestFence(ctx, tx, grant)
}

func (s *Store) finishMetadataIngest(ctx context.Context, check, finish func(context.Context, pgx.Tx) error) (resultErr error) {
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if err := check(ctx, tx); err != nil {
		return err
	}
	if err := finish(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func metadataIngestVisibilityChange(target metadataIngestTarget, libraryID string) CatalogChange {
	return CatalogChange{Kind: CatalogUpdated, ItemID: target.id, LibraryID: libraryID, ParentID: target.parentID,
		IsFolder: target.folder, ChildrenAdded: target.folder}
}

func verifyMetadataIngestSource(ctx context.Context, lease *libraryRootLease, opened *os.Root, target metadataIngestTarget,
	directoryPath, path string, expected os.FileInfo) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := lease.Open()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root)) }()
	if !sameMediaSourceDirectory(opened, root) {
		return errMetadataIngestChanged
	}
	dir, err := openRegisteredRoot(root, directoryPath)
	if err != nil {
		return errors.Join(errMetadataIngestChanged, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	current, err := dir.Stat(".")
	if err != nil || !sameSubtitleDirectoryInfo(expected, current) {
		return errors.Join(errMetadataIngestChanged, err)
	}
	if !target.folder {
		file, err := dir.Lstat(filepath.Base(path))
		if err != nil || !target.matchesFile(file) {
			return errors.Join(errMetadataIngestChanged, err)
		}
	}
	return ctx.Err()
}

func checkTaskIngestFence(ctx context.Context, tx pgx.Tx, grant *internalTaskSourceGrant) error {
	if err := grant.check(ctx, grant.store, grant.childID); err != nil {
		return err
	}
	return grant.fence(taskSourcePublicationTx{ctx: ctx, tx: tx})
}

func (s *Store) checkMetadataIngestTarget(ctx context.Context, tx pgx.Tx, grant *internalTaskSourceGrant,
	library Library, row rootBindingRow, target metadataIngestTarget) error {
	if err := checkTaskIngestFence(ctx, tx, grant); err != nil {
		return err
	}
	var revision string
	if err := tx.QueryRow(ctx, `SELECT revision::text FROM libraries WHERE id=$1 FOR SHARE`, library.ID).Scan(&revision); err != nil {
		return err
	}
	if revision != library.Revision {
		return errMetadataIngestChanged
	}
	if err := checkScanOperationRootMappingTx(ctx, tx, row.root); err != nil {
		return err
	}
	var scanning bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_jobs WHERE library_id=$1 AND status IN ('Queued','Running'))`, library.ID).Scan(&scanning); err != nil {
		return err
	}
	if scanning {
		return errMetadataIngestChanged
	}
	current, err := readMetadataIngestTarget(tx.QueryRow(ctx, metadataIngestTargetLock, library.ID, target.id, target.jobID))
	if errors.Is(err, pgx.ErrNoRows) {
		return errMetadataIngestChanged
	}
	if err != nil {
		return err
	}
	if !target.matches(current) {
		return errMetadataIngestChanged
	}
	return nil
}

func runIngestSourceProof(ctx context.Context, workers *sync.WaitGroup, verify func(context.Context) error) error {
	retained := withStorageObservationPhaseRetention(ctx, func() (func() error, error) {
		release, err := retainStorageObservationPhase(ctx)
		if err != nil {
			return nil, err
		}
		workers.Add(1)
		return func() error { defer workers.Done(); return release() }, nil
	})
	return runStorageObservation(retained, nil, verify)
}
