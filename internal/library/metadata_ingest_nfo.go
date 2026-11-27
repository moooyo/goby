package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/primaryio"
)

type nfoIngestEntry struct {
	name string
	info os.FileInfo
}

type nfoIngestProof []nfoIngestEntry

var errMetadataIngestInvalidNFO = errors.New("preferred local NFO could not be accepted")

// Observe the ordered candidates using the contained parent. A returned proof
// covers every absent higher-priority candidate and the chosen file's stamp.
func observeMetadataIngestNFO(ctx context.Context, dir *os.Root, directoryPath string, candidates []string) (localNFOObservation, nfoIngestProof, error) {
	proof := make(nfoIngestProof, 0, len(candidates))
	for _, name := range candidates {
		before, err := dir.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			proof = append(proof, nfoIngestEntry{name: name})
			continue
		}
		if err != nil {
			if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
				return localNFOObservation{}, nil, fatal
			}
			return localNFOObservation{invalid: true}, nil, nil
		}
		if !before.Mode().IsRegular() || before.Size() > maxLocalNFOBytes {
			return localNFOObservation{invalid: true}, nil, nil
		}
		data, found, invalid, err := readLocalNFO(ctx, dir, name)
		if err != nil {
			if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
				return localNFOObservation{}, nil, fatal
			}
			return localNFOObservation{invalid: true}, nil, nil
		}
		after, err := dir.Lstat(name)
		if !found || invalid || err != nil || !sameLocalNFOFile(before, after) {
			return localNFOObservation{invalid: true}, nil, nil
		}
		proof = append(proof, nfoIngestEntry{name: name, info: before})
		return localNFOObservation{path: filepath.ToSlash(filepath.Join(directoryPath, name)), data: data}, proof, ctx.Err()
	}
	return localNFOObservation{}, proof, ctx.Err()
}

func (proof nfoIngestProof) verify(ctx context.Context, lease *libraryRootLease, directory string) (resultErr error) {
	root, err := lease.Open()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root)) }()
	dir, err := openRegisteredRoot(root, directory)
	if err != nil {
		return errors.Join(errMetadataIngestChanged, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	return proof.verifyAt(ctx, dir)
}

func (proof nfoIngestProof) verifyAt(ctx context.Context, dir *os.Root) error {
	for _, entry := range proof {
		current, err := dir.Lstat(entry.name)
		if entry.info == nil && errors.Is(err, os.ErrNotExist) || entry.info != nil && err == nil && sameLocalNFOFile(entry.info, current) {
			continue
		}
		return errors.Join(errMetadataIngestChanged, err)
	}
	return ctx.Err()
}

func (s *Store) publishMetadataIngestNFO(ctx context.Context, operation *PrimaryRootIO,
	check func(context.Context, pgx.Tx) error, grant *internalTaskSourceGrant, verify func(context.Context) error, lease *libraryRootLease,
	target metadataIngestTarget, directory, kind string, observation localNFOObservation, proof nfoIngestProof, state *scanState,
	pairSource episodePairSource, binding *episodePairBinding) (_ bool, resultErr error) {
	// Parsing is independent of the owner transaction. On rejection, an episode
	// may still need to retire a merge belonging to a different binding.
	local, parseErr := parseObservedNFO(observation, kind, binding)
	if parseErr != nil && target.kind != "Episode" {
		return false, parseErr
	}
	index, parentIndex := target.request.index, target.request.parentIndex
	if target.kind == "Episode" {
		local, index, parentIndex = state.episodeNumbering(local, index, parentIndex, target.request.parentDefined)
	} else {
		local = state.seasonNumbering(local, target.kind, index)
	}
	base := filepath.Base(filepath.FromSlash(target.relative))
	if target.relative == tvRootSeriesPath {
		base = filepath.Base(state.root.path)
	} else if !target.folder {
		base = strings.TrimSuffix(base, filepath.Ext(base))
	}
	name, sortName, overview := describeFromLocal(cleanName(base), local)
	var proofWorkers sync.WaitGroup
	defer proofWorkers.Wait()
	tx, err := s.beginOwnedTx(ctx)
	if err != nil {
		return false, err
	}
	committed := false
	defer func() {
		if !committed {
			resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr))
		}
	}()
	if err := check(ctx, tx); err != nil {
		return false, err
	}
	if err := pairSource.checkBinding(ctx, tx, binding, errMetadataIngestChanged); err != nil {
		return false, err
	}
	var previous localMetadata
	var raw []byte
	var currentParentIndex int
	if err := tx.QueryRow(ctx, `SELECT local_metadata,local_metadata_hash,local_metadata_path,parent_index_number FROM items WHERE id=$1`, target.id).
		Scan(&raw, &previous.hash, &previous.path, &currentParentIndex); err != nil {
		return false, err
	}
	if err := decodeLocalMetadata(raw, &previous); err != nil {
		return false, err
	}
	if parseErr != nil && (previous.pair == nil || previous.pair.same(binding)) {
		return false, parseErr
	}
	if previous.pair == nil || previous.pair.same(binding) {
		local.raw = previous.raw
	}
	changed := local.hash != previous.hash || local.path != previous.path || !local.pair.same(previous.pair) ||
		!reflect.DeepEqual(local.value, previous.value) || target.kind == "Episode" && parentIndex != currentParentIndex
	if changed {
		localJSON, err := encodeLocalMetadata(local)
		if err != nil {
			return false, err
		}
		before, err := readScanCatalogItem(ctx, tx, target.id)
		if err != nil {
			return false, err
		}
		auxiliary, err := readAuxiliaryChildCatalogSnapshot(ctx, tx, target.id)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `UPDATE items SET local_metadata=$2,local_metadata_hash=$3,local_metadata_path=$4,
			parent_index_number=CASE WHEN type='Episode' THEN $5 ELSE parent_index_number END WHERE id=$1`,
			target.id, localJSON, local.hash, local.path, parentIndex); err != nil {
			return false, err
		}
		base := &scannedMetadataBase{Name: name, SortName: sortName, Overview: overview}
		if target.kind == "Episode" {
			base.IndexNumber = &index
		}
		if err := syncScannedMetadata(ctx, tx, target.id, scannedMetadataOptions{Base: base}); err != nil {
			return false, err
		}
		after, err := readScanCatalogItem(ctx, tx, target.id)
		if err != nil {
			return false, err
		}
		if err := recordScanCatalogChange(tx, state.library.ID, before, after, false); err != nil {
			return false, err
		}
		if err := auxiliary.record(ctx, tx, nil); err != nil {
			return false, err
		}
	}
	if parseErr == nil {
		if err := s.completeLocalMetadataImport(ctx, tx, grant, state.library, target, true, false); err != nil {
			return false, err
		}
	} else if err := checkTaskIngestFence(ctx, tx, grant); err != nil {
		return false, err
	}
	// Prove the observed sidecar after the queue and task fences so both the
	// description and the pending gate roll back if storage changed.
	deadline, _ := tx.(*ownedTx).ctx.Deadline()
	bounded, cancel := context.WithDeadline(ctx, deadline.Add(-scanReconciliationRollbackSpace))
	defer cancel()
	if err := operation.RunImmediate(bounded, target.rootID, primaryio.Background, func(work context.Context) error {
		return runIngestSourceProof(work, &proofWorkers, func(work context.Context) error {
			if err := verify(work); err != nil {
				return err
			}
			if err := proof.verify(work, lease, directory); err != nil {
				return err
			}
			return verify(work)
		})
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	committed = true
	return changed, parseErr
}
