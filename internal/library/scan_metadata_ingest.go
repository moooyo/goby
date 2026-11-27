package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/systemevents"
)

const TaskLocalMetadataIngestKey = "metadata.import_local"

// WithDeferredLocalMetadata lets ordinary movie and TV scans leave descriptive
// sidecars to the local import task. Source verification, hierarchy, subtitles,
// and explicit full media refreshes retain their original scan contract.
func WithDeferredLocalMetadata() Option {
	return func(settings *storeOptions) error {
		settings.deferLocalMetadata = true
		return nil
	}
}

// WithIngestOnlineMetadata opts new fast scan generations into a separate
// provider task. It does not configure a provider or authorize network access.
func WithIngestOnlineMetadata() Option {
	return func(settings *storeOptions) error {
		settings.enrichOnlineMetadata = true
		return nil
	}
}

type scanNFOCheck uint8

const (
	scanNFOUnchanged scanNFOCheck = iota
	scanNFOPending
	scanNFOVerified
)

type scanMetadataImport struct {
	directoryIdentity   string
	index, parentIndex  int
	parentDefined       bool
	nfo, images, online bool
	recheckOnly         bool
	nfoCheck            scanNFOCheck
	preserveNFO         bool
	nfoRecovery         *scannedNFORecovery
}

type scannedNFORecovery struct {
	row    rootBindingRow
	source nfoRecoveryBinding
	data   []byte
	digest string
}

func (state *scanState) captureScannedNFORecovery(request *scanMetadataImport, row rootBindingRow, path, itemType string,
	folder bool, observation localNFOObservation, proof *nfoIngestProof, local localMetadata, changeNS int64) {
	capture := &scannedNFORecovery{row: row}
	request.nfoRecovery = capture
	rootInfo := state.directoryIdentities["."]
	if rootInfo != nil && rootInfo.IsDir() && proof.recoveryDirectory != nil && (folder || changeNS > 0) &&
		state.store.nfoRecoveryWritable(row.root) && trustedNFORecoveryDocument(path, folder, itemType, *request, local, observation.data) {
		capture.source = nfoRecoveryBinding{request.directoryIdentity, fileIdentity(rootInfo), observation.path, changeNS}
		if capture.source.valid() {
			capture.data, capture.digest = observation.data, local.hash
			return
		}
	}
	// An accepted observation with no safe backup replaces any older recovery
	// authority, but its normal import does not depend on the recovery proof.
	proof.recoveryDirectory = nil
}

func (s *Store) defersLocalMetadata(library Library, job Job) bool {
	if !s.deferLocalMetadata || job.ForceProbe || !scanIngestVideoLibrary(library) {
		return false
	}
	options := EffectiveLibraryOptions(library)
	return options.EnableLocalMetadata || options.EnableLocalImages
}

// Synchronous scans carry existing work forward when the fast path is off.
// A file visit with its own final NFO proof can complete that phase directly;
// other visits leave completion to the importer.
func (s *Store) recheckLocalMetadataOnScan(ctx context.Context, library Library, job Job) (bool, error) {
	if !scanIngestVideoLibrary(library) || s.defersLocalMetadata(library, job) {
		return false, nil
	}
	var pending bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM local_metadata_ingest q JOIN items i ON i.id=q.item_id
		WHERE i.library_id=$1 AND `+database.CatalogPathVisibleSQL("i")+`)`, library.ID).Scan(&pending)
	return pending, err
}

func (state *scanState) metadataImport(path, itemType string, folder bool) scanMetadataImport {
	deferred := state.store.defersLocalMetadata(state.library, state.task.job)
	online := state.store.enrichOnlineMetadata && scanIngestVideoLibrary(state.library) && !state.task.job.ForceProbe && itemType != "Folder"
	if !deferred && !online && !state.recheckLocalMetadata {
		return scanMetadataImport{}
	}
	switch itemType {
	case "Movie", "Episode", "Series", "Season", "Folder":
	default:
		return scanMetadataImport{}
	}
	if !validImageScanPath(path) {
		return scanMetadataImport{}
	}
	directory := path
	_, kind := folderNFOCandidate(path, itemType)
	if !folder {
		directory = filepath.Dir(path)
		_, kind = fileNFOCandidates(path, itemType)
	}
	info := state.directoryIdentities[filepath.Clean(directory)]
	if info == nil || !info.IsDir() {
		return scanMetadataImport{}
	}
	identity := fileIdentity(info)
	if identity == "" {
		return scanMetadataImport{}
	}
	options := EffectiveLibraryOptions(state.library)
	if !deferred && !online {
		return scanMetadataImport{directoryIdentity: identity, recheckOnly: true}
	}
	return scanMetadataImport{directoryIdentity: identity, online: online,
		nfo: deferred && kind != "" && options.EnableLocalMetadata, images: deferred && options.EnableLocalImages,
		preserveNFO: kind != "" && !options.EnableLocalMetadata}
}

// Virtual TV folders have no sidecar directory. Online matching can still use
// their accepted catalog hierarchy, without inventing a filesystem location.
func (state *scanState) virtualMetadataImport(relative, itemType string) scanMetadataImport {
	if !state.store.enrichOnlineMetadata || state.task.job.ForceProbe || !scanIngestVideoLibrary(state.library) ||
		!((itemType == "Series" && strings.HasPrefix(relative, "//series/")) || (itemType == "Season" && strings.HasPrefix(relative, "//season/"))) {
		return scanMetadataImport{}
	}
	if info := state.directoryIdentities["."]; info != nil && info.IsDir() {
		if identity := fileIdentity(info); identity != "" {
			return scanMetadataImport{directoryIdentity: identity, online: true}
		}
	}
	return scanMetadataImport{}
}

func (request scanMetadataImport) pending() bool {
	return request.nfo || request.images || request.online
}

func (request scanMetadataImport) needed() bool {
	return request.pending() || request.recheckOnly || request.nfoCheck != scanNFOUnchanged
}

// Exclude descriptive metadata and user state so a manual edit neither changes
// the accepted media source nor invalidates an already queued import.
const metadataIngestSourceKeySQL = `jsonb_build_array(i.root_id,i.relative_path,i.path,i.type,i.is_folder,i.parent_id,
	i.file_identity,i.file_size,extract(epoch FROM i.modified_at),i.media_operation_source_revision,
	r.binding_revision)::text`

const queueScanMetadataImportSQL = `INSERT INTO local_metadata_ingest
	(item_id,scan_job_id,directory_identity,source_key,index_number,parent_index_number,parent_number_defined,import_nfo,import_images,enrich_online)
	SELECT i.id,$2,$3,` + metadataIngestSourceKeySQL + `,$4,$5,$6,$7,$8,$9
	FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id WHERE i.id=$1
	ON CONFLICT(item_id) DO UPDATE SET scan_job_id=EXCLUDED.scan_job_id,directory_identity=EXCLUDED.directory_identity,
	source_key=EXCLUDED.source_key,index_number=EXCLUDED.index_number,parent_index_number=EXCLUDED.parent_index_number,
	parent_number_defined=EXCLUDED.parent_number_defined,
	import_nfo=EXCLUDED.import_nfo OR ($10::boolean AND local_metadata_ingest.import_nfo),
	import_images=EXCLUDED.import_images,enrich_online=EXCLUDED.enrich_online`

// A request is visible to background work only after the same scan's terminal
// checkpoint. Item writes and their request share the original scan transaction.
func (state *scanState) queueMetadataImport(tx pgx.Tx, itemID string, request scanMetadataImport) error {
	if request.recheckOnly {
		_, err := tx.Exec(state.task.ctx, `UPDATE local_metadata_ingest q SET scan_job_id=$2,directory_identity=$3,
			source_key=`+metadataIngestSourceKeySQL+`,index_number=$4,parent_index_number=$5,parent_number_defined=$6
			FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
			WHERE q.item_id=i.id AND i.id=$1`, itemID, state.task.job.ID, request.directoryIdentity,
			request.index, request.parentIndex, request.parentDefined)
		return err
	}
	if !request.pending() {
		return nil
	}
	_, err := tx.Exec(state.task.ctx, queueScanMetadataImportSQL, itemID, state.task.job.ID, request.directoryIdentity,
		request.index, request.parentIndex, request.parentDefined, request.nfo, request.images, request.online, request.preserveNFO)
	return err
}

// The synchronous observation changes only the NFO phase. A rejected sidecar
// can establish the same policy gate as a deferred scan on its first visit.
func (state *scanState) queueScannedNFOImport(tx pgx.Tx, itemID, parentID string, folder bool, request scanMetadataImport) error {
	ctx := state.task.ctx
	if saved := request.nfoRecovery; saved != nil {
		if err := finishNFORecoveryObservation(ctx, tx, saved.row, itemID, state.task.job.ID, saved.source, saved.data, saved.digest); err != nil {
			return err
		}
	}
	if request.nfoCheck == scanNFOUnchanged {
		return state.queueMetadataImport(tx, itemID, request)
	}
	var wasPending, images, online bool
	err := tx.QueryRow(ctx, `SELECT import_nfo,import_images,enrich_online FROM local_metadata_ingest WHERE item_id=$1 FOR UPDATE`, itemID).
		Scan(&wasPending, &images, &online)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	request.recheckOnly, request.preserveNFO = false, false
	request.nfo = request.nfoCheck == scanNFOPending
	request.images = request.images || images
	request.online = request.online || online
	if request.pending() {
		if err := state.queueMetadataImport(tx, itemID, request); err != nil {
			return err
		}
	} else if wasPending {
		if _, err := tx.Exec(ctx, `DELETE FROM local_metadata_ingest WHERE item_id=$1`, itemID); err != nil {
			return err
		}
	}
	if wasPending != request.nfo {
		changes := []CatalogChange{metadataIngestVisibilityChange(metadataIngestTarget{id: itemID, parentID: parentID, folder: folder}, state.library.ID)}
		if request.nfo {
			// A viewer may no longer be allowed to receive the item's ID. Its
			// visible parent still needs a membership refresh after a rejection.
			parent := CatalogChange{Kind: CatalogUpdated, ItemID: parentID, LibraryID: state.library.ID, IsFolder: true, ChildrenRemoved: true}
			err := tx.QueryRow(ctx, `SELECT COALESCE(parent_id,''),type='CollectionFolder' FROM items WHERE id=$1 AND library_id=$2 AND is_folder`,
				parentID, state.library.ID).Scan(&parent.ParentID, &parent.IsCollectionFolder)
			if err == nil {
				changes = append(changes, parent)
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		return mergeAuxiliaryCatalogChanges(tx, changes, false)
	}
	return nil
}

// A grouping folder has neither a folder NFO nor an online metadata match.
// A new request supersedes the old one by upsert. A synchronous recheck may
// carry forward only the image phase under the now-current item and scan key.
func (state *scanState) queueFolderMetadataImport(tx pgx.Tx, itemID, previousType, itemType string, request scanMetadataImport) error {
	if itemType == "Folder" && previousType != "" && previousType != itemType && !request.pending() {
		if _, err := tx.Exec(state.task.ctx, `DELETE FROM local_metadata_ingest
			WHERE item_id=$1 AND NOT ($2 AND import_images)`, itemID, request.recheckOnly); err != nil {
			return err
		}
		if request.recheckOnly {
			if _, err := tx.Exec(state.task.ctx, `UPDATE local_metadata_ingest SET import_nfo=false,enrich_online=false
				WHERE item_id=$1 AND (import_nfo OR enrich_online)`, itemID); err != nil {
				return err
			}
		}
	}
	return state.queueMetadataImport(tx, itemID, request)
}

func (state *scanState) queueCachedIngestWork(stored storedFile, path string, input *primaryScanRead, request scanMetadataImport,
	seek bool, dolbyVision []int, proof *scannedNFOProof, checkPair func(pgx.Tx) error) (resultErr error) {
	itemID := stored.id
	if !request.needed() && !seek && len(dolbyVision) == 0 && proof == nil {
		return nil
	}
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	if err := state.checkSidecarScanRootTx(tx, input.row); err != nil {
		return err
	}
	var sourceCurrent bool
	var changeNS int64
	if stored.media != nil {
		changeNS = stored.media.FileChangeTimeNs
	}
	if err := tx.QueryRow(state.task.ctx, `SELECT file_identity=$5 AND file_size=$6 AND modified_at IS NOT DISTINCT FROM $7
		AND COALESCE((media->>'FileChangeTimeNs')::bigint,0)=$8 AND COALESCE(parent_id,'')=$9 AND type=$10 AND path=$11
		FROM items WHERE id=$1 AND library_id=$2 AND root_id=$3 AND relative_path=$4 AND NOT is_folder AND `+ordinaryItemSQL("items")+` FOR UPDATE`,
		itemID, state.library.ID, state.root.id, filepath.ToSlash(path), stored.identity, stored.size, stored.modified, changeNS,
		stored.parentID, stored.itemType, stored.path).Scan(&sourceCurrent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errScanProbeSourceChanged
		}
		return fmt.Errorf("recheck cached ingest work: %w", err)
	}
	if !sourceCurrent {
		return errScanProbeSourceChanged
	}
	if err := checkPair(tx); err != nil {
		return err
	}
	if err := state.queueScannedNFOImport(tx, itemID, stored.parentID, false, request); err != nil {
		return err
	}
	if seek {
		if err := state.queueVideoSeekIngest(tx, itemID); err != nil {
			return err
		}
	}
	if err := state.queueDolbyVisionVerification(tx, itemID, dolbyVision); err != nil {
		return err
	}
	if err := input.revalidatePublicationNFO(tx, proof); err != nil {
		return err
	}
	if err := state.task.ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(state.task.ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) finishScanIngestWork(tx OwnedTx, job Job) error {
	if job.Status != "Completed" {
		// No derived work is authorized by an unsuccessful scan. Keep pending
		// NFO markers until a successful rescan, including for content policy.
		return nil
	}
	var local, online, seek, dolbyVision bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM local_metadata_ingest WHERE scan_job_id=$1 AND (import_nfo OR import_images)),
		EXISTS(SELECT 1 FROM local_metadata_ingest WHERE scan_job_id=$1 AND enrich_online AND NOT import_nfo AND NOT import_images),
		EXISTS(SELECT 1 FROM video_seek_ingest WHERE scan_job_id=$1 AND indexes IS NULL AND retry_at<=clock_timestamp()),
		EXISTS(SELECT 1 FROM dolby_vision_verifications WHERE scan_job_id=$1 AND evidence IS NULL AND retry_at<=clock_timestamp())`, job.ID).
		Scan(&local, &online, &seek, &dolbyVision); err != nil {
		return err
	}
	for _, signal := range []struct {
		pending bool
		event   systemevents.Event
	}{
		{local, systemevents.LocalMetadataIngestRequested},
		{online && s.enrichOnlineMetadata, systemevents.OnlineMetadataIngestRequested},
		{seek && s.deferVideoSeek, systemevents.VideoSeekIngestRequested},
		{dolbyVision && s.DolbyVisionVerificationConfigured(), systemevents.DolbyVisionVerificationRequested},
	} {
		if signal.pending {
			if err := systemevents.Record(tx.Exec, signal.event); err != nil {
				return err
			}
		}
	}
	return nil
}

func (state *scanState) readPreviousFolderMetadata(ctx context.Context, relative string) (localMetadata, error) {
	var previous localMetadata
	var raw []byte
	err := state.store.pool.QueryRow(ctx, `SELECT local_metadata,local_metadata_hash,local_metadata_path
		FROM items WHERE root_id=$1 AND relative_path=$2`, state.root.id, relative).Scan(&raw, &previous.hash, &previous.path)
	if errors.Is(err, pgx.ErrNoRows) {
		return localMetadata{}, nil
	}
	if err != nil {
		return localMetadata{}, err
	}
	if err := decodeLocalMetadata(raw, &previous); err != nil {
		return localMetadata{}, err
	}
	return previous, nil
}
