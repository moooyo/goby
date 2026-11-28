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

type localIngestPhase string

const (
	localIngestNFO     localIngestPhase = TaskLocalMetadataIngestKey
	localIngestArtwork localIngestPhase = TaskLocalArtworkIngestKey
)

func (phase localIngestPhase) predicate() string {
	if phase == localIngestNFO {
		return `q.import_nfo`
	}
	return `q.import_images`
}

func (phase localIngestPhase) pending(request scanMetadataImport) bool {
	if phase == localIngestNFO {
		return request.nfo
	}
	return request.images
}

// SnapshotLocalMetadataIngestChildren admits NFO and artwork independently,
// including when both phases are pending on the same accepted item.
// An unsuccessful scan cannot authorize either source operation.
func SnapshotLocalMetadataIngestChildren(tx OwnedTx, runID, key string) (int64, error) {
	phase := localIngestPhase(key)
	if tx == nil || !validTaskChildID(runID) || phase != localIngestNFO && phase != localIngestArtwork {
		return 0, ErrInvalidInput
	}
	tag, err := tx.Exec(`INSERT INTO task_run_children(id,run_id,library_id,library_name,ordinal)
		SELECT md5($1||':'||l.id),$1,l.id,l.name,(row_number() OVER (ORDER BY l.id)-1)::integer
		FROM task_runs run CROSS JOIN libraries l WHERE run.id=$1 AND run.task_key=$2 AND EXISTS(
		SELECT 1 FROM scan_jobs j JOIN local_metadata_ingest q ON q.scan_job_id=j.id JOIN items i ON i.id=q.item_id
		WHERE j.library_id=l.id AND i.library_id=l.id AND j.status='Completed' AND j.finished_at<=run.created_at
		AND `+phase.predicate()+` AND `+database.CatalogPathVisibleSQL("i")+`
		AND `+scanIngestAutomaticGenerationSQL+`)`, runID, key)
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
)

func (phase localIngestPhase) pageSQL() string {
	return metadataIngestTargetBase + ` AND ` + phase.predicate() + ` AND q.item_id>$2 AND EXISTS(
		SELECT 1 FROM task_runs run WHERE run.id=$4 AND j.finished_at<=run.created_at
		AND ` + scanIngestAutomaticGenerationSQL + `) ORDER BY q.item_id LIMIT $3`
}

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

// ImportLocalScanMetadata verifies pending NFOs without reading artwork.
func (s *Store) ImportLocalScanMetadata(ctx context.Context, childID, libraryID string, fence AnalysisFence, report func(ProviderProgress) error) error {
	return s.importLocalScanPhase(ctx, childID, libraryID, fence, report, localIngestNFO)
}

// ImportLocalScanArtwork processes artwork, including on items whose NFO is still pending.
func (s *Store) ImportLocalScanArtwork(ctx context.Context, childID, libraryID string, fence AnalysisFence, report func(ProviderProgress) error) error {
	return s.importLocalScanPhase(ctx, childID, libraryID, fence, report, localIngestArtwork)
}

// A live generic task grant authorizes only its phase of an accepted scan.
// Bounded pages release their SQL snapshot before opening storage.
func (s *Store) importLocalScanPhase(ctx context.Context, childID, libraryID string, fence AnalysisFence,
	report func(ProviderProgress) error, phase localIngestPhase) (resultErr error) {
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
			WHERE child.id=$1 AND child.library_id=$2 AND run.task_key=$3`, childID, libraryID, string(phase)).Scan(&runID)
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
	// Keep only one immutable listing so flat libraries do not reread a complete
	// directory per artwork item. The NFO phase never touches this listing.
	var images metadataIngestImageListing
	const pageSize = 32
	query := phase.pageSQL()
	for after := ""; ; {
		rows, err := s.pool.Query(ctx, query, libraryID, after, pageSize, runID)
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
		// Preserve the high-water ID even when processing an artwork page out of order.
		after = page[len(page)-1].id
		if phase == localIngestArtwork {
			sort.SliceStable(page, func(i, j int) bool {
				left, right := page[i], page[j]
				if left.rootID != right.rootID {
					return left.rootID < right.rootID
				}
				return left.directoryPath() < right.directoryPath()
			})
		}
		for _, target := range page {
			if err := ctx.Err(); err != nil {
				return err
			}
			itemChanged := false
			for attempt := 0; ; attempt++ {
				changed, err := s.importLocalMetadataTarget(ctx, grant, library, target, phase, &images)
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
					if !phase.pending(current.request) {
						break
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
		if phase == localIngestArtwork {
			return errors.New("some local artwork could not be verified and remains pending")
		}
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

func (target metadataIngestTarget) nfoCandidates(path string) ([]string, string) {
	if target.folder {
		return folderNFOCandidate(".", target.kind)
	}
	return fileNFOCandidates(filepath.Base(path), target.kind)
}

func (s *Store) importLocalMetadataTarget(ctx context.Context, grant *internalTaskSourceGrant, library Library,
	target metadataIngestTarget, phase localIngestPhase, listing *metadataIngestImageListing) (_ bool, resultErr error) {
	options := EffectiveLibraryOptions(library)
	if phase == localIngestNFO && !options.EnableLocalMetadata {
		// Disabling the reader is not proof that the old rating or tags still apply.
		return false, errMetadataIngestChanged
	}
	row := grant.roots[target.rootID]
	path, valid := target.sourcePath(row.root)
	if !valid {
		return false, errMetadataIngestChanged
	}
	check := func(work context.Context, tx pgx.Tx) error {
		return s.checkMetadataIngestTarget(work, tx, grant, library, row, target, phase)
	}
	finish := func(work context.Context, tx pgx.Tx) error {
		return s.completeLocalMetadataImport(work, tx, grant, library, target, phase)
	}
	if phase == localIngestArtwork && !options.EnableLocalImages {
		// Retire optional work under the current options and generation fence,
		// without opening a disabled image source or altering pending NFOs.
		return false, s.finishMetadataIngest(ctx, check, finish)
	}
	directoryPath := target.directoryPath()
	release, admitted := s.backdropExecution.beginWork(localBackdropWorkScope{
		itemID: target.id, directory: filepath.Join(row.root.path, directoryPath), inode: target.request.directoryIdentity,
	})
	if !admitted {
		// A later scheduled run can reconsider this still-pending row. Do not
		// wait or prevent work on another directory in this run.
		return false, nil
	}
	var retired <-chan struct{}
	defer func() { releaseAfterSourceOperationRetires(retired, release) }()
	operation, err := s.preparePrimaryRootIO(ctx, []mediaSourceRootHint{{root: row.root, bindingRevision: row.revision}})
	if err != nil {
		return false, err
	}
	retired = operation.handle.state.done
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
	var directoryInfo os.FileInfo
	verify := func(work context.Context) error {
		return verifyMetadataIngestSource(work, lease, opened, target, directoryPath, path, directoryInfo)
	}
	state := &scanState{store: s, library: library, root: row.root, imageInspection: &s.imageInspection}
	var observation localNFOObservation
	var proof nfoIngestProof
	var rootIdentity string
	candidates, kind := target.nfoCandidates(path)
	if phase == localIngestNFO && kind == "" {
		return false, errMetadataIngestChanged
	}
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
		if phase == localIngestNFO {
			info, err := opened.Stat(".")
			if err != nil {
				return errors.Join(errMetadataIngestChanged, err)
			}
			rootIdentity = fileIdentity(info)
			if scanIngestVideoLibrary(library) && s.nfoRecoveryWritable(row.root) {
				observation, proof, err = observeNFORecoveryCandidate(work, dir, directoryPath, candidates, path, target.kind, target.folder)
			} else {
				observation, proof, err = observeMetadataIngestNFO(work, dir, directoryPath, candidates)
			}
			if err != nil {
				return err
			}
		}
		return verify(work)
	}); err != nil {
		return false, err
	}
	if phase == localIngestNFO {
		var pairSource episodePairSource
		var binding *episodePairBinding
		if target.kind == "Episode" {
			file := episodePairFile{Identity: target.fileIdentity, Size: target.fileSize,
				ModifiedNS: target.modified.UnixNano(), ChangeNS: target.changeNS}
			pairSource = newEpisodePairSource(row, path, target.parentID, rootIdentity, target.request, file)
			binding, err = pairSource.readBinding(ctx, s.pool, false)
			if err != nil {
				return false, err
			}
		}
		changed, err := s.publishMetadataIngestNFO(ctx, operation, check, grant, verify, lease, target, path, directoryPath,
			kind, rootIdentity, candidates, observation, proof, state, pairSource, binding)
		switch {
		case scanPublicationErrorOnly(err, errMetadataIngestNFOHandedOff):
			return changed, nil
		case scanPublicationErrorOnly(err, errMetadataIngestInvalidNFO):
			return changed, errMetadataIngestChanged
		default:
			return changed, err
		}
	}
	state.directoryIdentities = map[string]os.FileInfo{directoryPath: directoryInfo}
	if listing.rootID == target.rootID && listing.directory == directoryPath && listing.index != nil &&
		sameSubtitleDirectoryInfo(directoryInfo, listing.index.info) {
		state.imageDirectories = map[string]*imageDirectoryIndex{directoryPath: listing.index}
	}
	authority := &imageRefreshAuthority{
		prepare: func() (*PrimaryRootIO, rootBindingRow, error) {
			child, err := operation.Fork(ctx)
			return child, row, err
		},
		check: check,
		finish: func(work context.Context, tx pgx.Tx) error {
			if state.warnings != 0 {
				return checkTaskIngestFence(work, tx, grant)
			}
			return finish(work, tx)
		},
		prove: verify,
	}
	if err := state.refreshLocalImages(ctx, authority, target.id, target.kind, path, target.folder, false); err != nil {
		*listing = metadataIngestImageListing{}
		return false, err
	}
	*listing = metadataIngestImageListing{rootID: target.rootID, directory: directoryPath, index: state.imageDirectories[directoryPath]}
	if state.warnings != 0 {
		return false, errMetadataIngestChanged
	}
	return true, nil
}

// The publishing transaction locks the current queue row. Retire only the
// completed phase, never the other phase's possibly stale observed value.
func (s *Store) completeLocalMetadataImport(ctx context.Context, tx pgx.Tx, grant *internalTaskSourceGrant,
	library Library, target metadataIngestTarget, phase localIngestPhase) error {
	var pending scanMetadataImport
	err := tx.QueryRow(ctx, `SELECT import_nfo,import_images,enrich_online FROM local_metadata_ingest
		WHERE item_id=$1 AND scan_job_id=$2 FOR UPDATE`, target.id, target.jobID).
		Scan(&pending.nfo, &pending.images, &pending.online)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !phase.pending(pending) {
		return errMetadataIngestChanged
	}
	if err != nil {
		return err
	}
	if phase == localIngestNFO {
		pending.nfo = false
		if err := recordMetadataIngestVisibility(ctx, tx, target, library.ID, false); err != nil {
			return err
		}
	} else {
		pending.images = false
	}
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

func recordMetadataIngestVisibility(ctx context.Context, tx pgx.Tx, target metadataIngestTarget, libraryID string, pending bool) error {
	change := CatalogChange{Kind: CatalogUpdated, ItemID: target.id, LibraryID: libraryID, ParentID: target.parentID,
		IsFolder: target.folder, ChildrenAdded: target.folder && !pending}
	changes := []CatalogChange{change}
	if target.parentID != "" {
		// A viewer who loses access cannot receive the affected ID. Invalidate
		// the surviving container as well; the socket still filters every ID.
		parent := CatalogChange{Kind: CatalogUpdated, ItemID: target.parentID, LibraryID: libraryID, IsFolder: true, ChildrenRemoved: true}
		err := tx.QueryRow(ctx, `SELECT COALESCE(parent_id,''),type='CollectionFolder' FROM items WHERE id=$1 AND library_id=$2 AND is_folder`,
			target.parentID, libraryID).Scan(&parent.ParentID, &parent.IsCollectionFolder)
		if err == nil {
			changes = append(changes, parent)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return mergeAuxiliaryCatalogChanges(tx, changes, false)
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
	library Library, row rootBindingRow, target metadataIngestTarget, phase localIngestPhase) error {
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
	// The other phase may have committed against this same generation since
	// the page was read. Only its flag may differ; its result cannot grant
	// authority over this phase or a different source.
	if phase == localIngestNFO {
		target.request.images = current.request.images
	} else {
		target.request.nfo = current.request.nfo
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
