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
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
	"github.com/moooyo/goby/internal/primaryio"
)

type nfoIngestEntry struct {
	name string
	info os.FileInfo
}

type nfoIngestProof struct {
	entries           []nfoIngestEntry
	directory         os.FileInfo
	recoveryDirectory os.FileInfo
}

var errMetadataIngestInvalidNFO = errors.New("preferred local NFO could not be accepted")

func observeMetadataIngestNFO(ctx context.Context, dir *os.Root, directoryPath string, candidates []string) (localNFOObservation, nfoIngestProof, error) {
	return observeMetadataIngestNFOWithReader(ctx, dir, directoryPath, candidates,
		func(ctx context.Context, dir *os.Root, name string, before os.FileInfo) ([]byte, bool, error) {
			data, found, invalid, err := readLocalNFO(ctx, dir, name)
			return data, invalid || !found, err
		})
}

// The proof covers every absent higher-priority candidate and the chosen
// file's stamp. A present but unreadable candidate never permits fallback.
func observeMetadataIngestNFOWithReader(ctx context.Context, dir *os.Root, directoryPath string, candidates []string,
	read func(context.Context, *os.Root, string, os.FileInfo) ([]byte, bool, error)) (localNFOObservation, nfoIngestProof, error) {
	before, err := dir.Stat(".")
	if err != nil || before == nil || !before.IsDir() || media.FileChangeTime(before) <= 0 {
		return localNFOObservation{invalid: true}, nfoIngestProof{}, localNFOFatalReadError(ctx, err)
	}
	proof := nfoIngestProof{directory: before, entries: make([]nfoIngestEntry, 0, len(candidates))}
	finish := func(observation localNFOObservation) (localNFOObservation, nfoIngestProof, error) {
		current, err := dir.Stat(".")
		if err != nil || !sameSubtitleDirectoryInfo(proof.directory, current) {
			return localNFOObservation{invalid: true}, nfoIngestProof{}, localNFOFatalReadError(ctx, err)
		}
		return observation, proof, ctx.Err()
	}
	for _, name := range candidates {
		candidate, err := dir.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			proof.entries = append(proof.entries, nfoIngestEntry{name: name})
			continue
		}
		if err != nil {
			if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
				return localNFOObservation{}, nfoIngestProof{}, fatal
			}
			return localNFOObservation{invalid: true}, nfoIngestProof{}, nil
		}
		if !candidate.Mode().IsRegular() || candidate.Size() > maxLocalNFOBytes || media.FileChangeTime(candidate) <= 0 {
			return localNFOObservation{invalid: true}, nfoIngestProof{}, nil
		}
		data, invalid, err := read(ctx, dir, name, candidate)
		if err != nil {
			if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
				return localNFOObservation{}, nfoIngestProof{}, fatal
			}
			return localNFOObservation{invalid: true}, nfoIngestProof{}, nil
		}
		after, err := dir.Lstat(name)
		if invalid || err != nil || !sameLocalNFOFile(candidate, after) {
			return localNFOObservation{invalid: true}, nfoIngestProof{}, localNFOFatalReadError(ctx, err)
		}
		proof.entries = append(proof.entries, nfoIngestEntry{name: name, info: candidate})
		return finish(localNFOObservation{path: filepath.ToSlash(filepath.Join(directoryPath, name)), data: data})
	}
	return finish(localNFOObservation{})
}

func (proof nfoIngestProof) verify(ctx context.Context, openRoot func() (*os.Root, error), directory string) (resultErr error) {
	root, err := openRoot()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, root)) }()
	dir, err := openRegisteredRoot(root, directory)
	if err != nil {
		return errors.Join(errMetadataIngestChanged, err)
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	if err := proof.verifyAt(ctx, dir); err != nil {
		return err
	}
	if proof.recoveryDirectory != nil {
		current, err := root.Lstat(directory)
		if err != nil {
			return errors.Join(errMetadataIngestChanged, err, ctx.Err())
		}
		if err := proof.verifyRecoveryDirectory(current); err != nil {
			return errors.Join(errMetadataIngestChanged, err)
		}
	}
	return ctx.Err()
}

type nfoProofDirectory interface {
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
}

func (proof nfoIngestProof) verifyAt(ctx context.Context, dir nfoProofDirectory) error {
	if len(proof.entries) == 0 {
		return ctx.Err()
	}
	before, err := dir.Stat(".")
	if err != nil || before == nil || !before.IsDir() || proof.directory == nil || !os.SameFile(proof.directory, before) || media.FileChangeTime(before) <= 0 {
		return errors.Join(errMetadataIngestChanged, err, ctx.Err())
	}
	for _, entry := range proof.entries {
		current, err := dir.Lstat(entry.name)
		if entry.info == nil && errors.Is(err, os.ErrNotExist) || entry.info != nil && err == nil && sameLocalNFOFile(entry.info, current) {
			continue
		}
		return errors.Join(errMetadataIngestChanged, err, ctx.Err())
	}
	// Inode identity cannot prove absence: a preferred candidate may have
	// appeared after its Lstat without changing the existing fallback file.
	current, err := dir.Stat(".")
	if err != nil || !sameSubtitleDirectoryInfo(before, current) {
		return errors.Join(errMetadataIngestChanged, err, ctx.Err())
	}
	return ctx.Err()
}

func (s *Store) publishMetadataIngestNFO(ctx context.Context, operation *PrimaryRootIO,
	check func(context.Context, pgx.Tx) error, grant *internalTaskSourceGrant, verify func(context.Context) error, lease *libraryRootLease,
	target metadataIngestTarget, path, directory, kind, rootIdentity string, candidates []string, observation localNFOObservation,
	proof nfoIngestProof, state *scanState, pairSource episodePairSource, binding *episodePairBinding) (_ bool, resultErr error) {
	// Parsing and the second independent read happen outside catalog ownership.
	// Failed readings and ordinary compatibility rejections are not corruption.
	local, parseErr := parseObservedNFO(observation, kind, binding)
	source := nfoRecoveryBinding{target.request.directoryIdentity, rootIdentity, observation.path, target.changeNS}
	var evidence nfoRecoveryEvidence
	if parseErr != nil && source.valid() {
		reason := metadata.NFOIntegrityFailure(observation.data)
		if version := nfoRecoveryVersion(observation, proof); version != "" && reason != "" {
			err := operation.Run(ctx, target.rootID, primaryio.Background, func(work context.Context) (readErr error) {
				if err := verify(work); err != nil {
					return err
				}
				root, err := lease.Open()
				if err != nil {
					return err
				}
				defer func() { readErr = errors.Join(readErr, closeDirectoryPrimaryRoot(work, root)) }()
				dir, err := openRegisteredRoot(root, directory)
				if err != nil {
					return errors.Join(errMetadataIngestChanged, err)
				}
				defer func() { readErr = errors.Join(readErr, closeDirectoryPrimaryRoot(work, dir)) }()
				confirmed, err := confirmNFORecovery(work, dir, directory, candidates, observation, proof)
				if confirmed && err == nil {
					evidence = nfoRecoveryEvidence{source, version, reason}
				}
				return err
			})
			if err != nil {
				return false, err
			}
		}
	}
	if parseErr != nil && evidence.version == "" && target.kind != "Episode" {
		return false, parseErr
	}
	trusted := parseErr == nil && source.valid() && (target.folder || source.changeNS > 0) && proof.recoveryDirectory != nil &&
		trustedNFORecoveryDocument(path, target.folder, target.kind, target.request, local, observation.data)
	if !trusted {
		proof.recoveryDirectory = nil
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
	keepPrevious := parseErr != nil && (previous.pair == nil || previous.pair.same(binding))
	if keepPrevious && evidence.version == "" {
		return false, parseErr
	}
	if previous.pair == nil || previous.pair.same(binding) {
		local.raw = previous.raw
	}
	changed := !keepPrevious && (local.hash != previous.hash || local.path != previous.path || !local.pair.same(previous.pair) ||
		!reflect.DeepEqual(local.value, previous.value) || target.kind == "Episode" && parentIndex != currentParentIndex)
	if changed {
		base := &scannedMetadataBase{Name: name, SortName: sortName, Overview: overview}
		var parentNumber *int
		if target.kind == "Episode" {
			base.IndexNumber = &index
			parentNumber = &parentIndex
		}
		if err := publishLocalNFOProjection(ctx, tx, state.library.ID, target.id, local, base, parentNumber); err != nil {
			return false, err
		}
	}
	if parseErr == nil {
		var data []byte
		if trusted {
			data = observation.data
		}
		if err := finishNFORecoveryObservation(ctx, tx, grant.roots[target.rootID], target.id, target.jobID, source, data, local.hash); err != nil {
			return false, err
		}
		if err := s.completeLocalMetadataImport(ctx, tx, grant, state.library, target, localIngestNFO); err != nil {
			return false, err
		}
	} else {
		if evidence.version != "" {
			if err := recordNFORecovery(ctx, tx, target, evidence); err != nil {
				return false, err
			}
			parseErr = errMetadataIngestNFOHandedOff
		}
		if err := checkTaskIngestFence(ctx, tx, grant); err != nil {
			return false, err
		}
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
			if err := proof.verify(work, lease.Open, directory); err != nil {
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
