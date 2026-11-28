package library

import (
	"context"
	"errors"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/media"
)

// scannedNFOReuse ties an accepted local layer to the catalog row observed
// before reading the current sidecar. Manual and provider layers are separate.
type scannedNFOReuse struct {
	id, relative, path, itemType, parentID string
	folder, pending                        bool
	local                                  localMetadata
}

func (state *scanState) readDeferredFolderNFO(relative string) (scannedNFOReuse, error) {
	var snapshot scannedNFOReuse
	var raw []byte
	snapshot.relative, snapshot.folder = relative, true
	err := state.store.pool.QueryRow(state.task.ctx, `SELECT i.id, i.path, i.type, COALESCE(i.parent_id,''),
		i.local_metadata,i.local_metadata_hash,i.local_metadata_path,`+scanNFOUnsettledSQL+`
		FROM items i WHERE i.root_id=$1 AND i.relative_path=$2 AND i.is_folder`, state.root.id, relative).
		Scan(&snapshot.id, &snapshot.path, &snapshot.itemType, &snapshot.parentID, &raw, &snapshot.local.hash, &snapshot.local.path, &snapshot.pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return scannedNFOReuse{}, nil
	}
	if err != nil {
		return scannedNFOReuse{}, err
	}
	if err := decodeLocalMetadata(raw, &snapshot.local); err != nil {
		return scannedNFOReuse{}, err
	}
	return snapshot, nil
}

const scanNFOUnsettledSQL = `(EXISTS (SELECT 1 FROM local_metadata_ingest q WHERE q.item_id=i.id AND q.import_nfo)
	OR EXISTS (SELECT 1 FROM local_nfo_recovery_work w WHERE w.item_id=i.id))`

func (state *scanState) deferredFolderNFO(relative, path, itemType, parentID string, source *scanFolderSource,
	request *scanMetadataImport, reject, skipNFORecovery bool) (localMetadata, error) {
	snapshot, err := state.readDeferredFolderNFO(relative)
	if err != nil {
		return localMetadata{}, err
	}
	candidates, kind := folderNFOCandidate(".", itemType)
	var observation localNFOObservation
	var proof nfoIngestProof
	check := !reject && snapshot.id != "" && snapshot.path == path && snapshot.itemType == itemType &&
		snapshot.parentID == parentID && !snapshot.pending
	saveRecovery := state.store.nfoRecoveryWritable(state.root) && !skipNFORecovery
	if err := source.readMetadata(state.task.ctx, state, func(ctx context.Context) error {
		if !check {
			return ctx.Err()
		}
		var err error
		if saveRecovery {
			observation, proof, err = observeNFORecoveryCandidate(ctx, source.directory, source.relative, candidates, source.relative, itemType, true)
		} else {
			observation, proof, err = observeMetadataIngestNFO(ctx, source.directory, source.relative, candidates)
		}
		return err
	}); err != nil {
		return localMetadata{}, err
	}
	if check && canReuseScannedNFO(snapshot.local, observation, proof, kind, itemType, nil, *request) {
		source.nfoProof = proof
		request.nfoReuse = &snapshot
		request.reuseAcceptedNFO()
		if saveRecovery {
			state.captureScannedNFORecovery(request, source.row, source.relative, itemType, true, observation, &source.nfoProof, snapshot.local, 0)
		}
	} else if snapshot.id != "" {
		request.nfoCheck = scanNFOPending
	}
	local := state.applyLocalNFO(localNFOObservation{disabled: true}, kind, snapshot.local)
	return state.seasonNumbering(local, itemType, request.index), nil
}

// The caller holds the scan's root fence. Check the same local layer, without
// invalidating an independent manual or provider edit to the effective layer.
func (snapshot scannedNFOReuse) check(ctx context.Context, tx pgx.Tx, root rootBindingRow) error {
	var same, pending bool
	err := tx.QueryRow(ctx, `SELECT i.local_metadata IS NOT DISTINCT FROM $9::jsonb
		AND i.local_metadata_hash=$10 AND i.local_metadata_path=$11, `+scanNFOUnsettledSQL+`
		FROM items i JOIN library_roots r ON r.id=i.root_id AND r.library_id=i.library_id
		WHERE i.id=$1 AND i.root_id=$2 AND i.library_id=$3 AND r.binding_revision=$4
		AND i.relative_path=$5 AND i.path=$6 AND i.type=$7 AND COALESCE(i.parent_id,'')=$8
		AND i.is_folder=$12 AND `+ordinaryItemSQL("i")+` FOR UPDATE OF i`,
		snapshot.id, root.root.id, root.root.libraryID, root.revision, snapshot.relative, snapshot.path, snapshot.itemType,
		snapshot.parentID, snapshot.local.raw, snapshot.local.hash, snapshot.local.path, snapshot.folder).Scan(&same, &pending)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
		return errScanProbeSourceChanged
	}
	if err != nil {
		return err
	}
	if pending {
		return errScannedNFOChanged
	}
	return nil
}

// A fresh observation must select the same document, or prove that every
// candidate is absent. Equal content alone does not prove a valid episode merge.
func canReuseScannedNFO(previous localMetadata, observation localNFOObservation, proof nfoIngestProof,
	kind, itemType string, binding *episodePairBinding, request scanMetadataImport) bool {
	if kind == "" || observation.disabled || observation.invalid || observation.path != previous.path || len(proof.entries) == 0 {
		return false
	}
	for _, entry := range proof.entries {
		if entry.info != nil && media.FileChangeTime(entry.info) <= 0 {
			return false
		}
	}
	current, err := parseObservedNFO(observation, kind, binding)
	if err != nil || current.hash != previous.hash || !current.pair.same(previous.pair) {
		return false
	}
	switch itemType {
	case "Episode":
		current, _, _, _ = normalizeEpisodeNumbering(current, request.index, request.parentIndex, request.parentDefined)
	case "Season":
		current, _ = normalizeSeasonNumbering(current, itemType, request.index)
	}
	return reflect.DeepEqual(current.value, previous.value)
}

func (request *scanMetadataImport) reuseAcceptedNFO() {
	// Not requesting a new verification does not retire any older work.
	request.nfo = false
	request.preserveNFO = true
}
