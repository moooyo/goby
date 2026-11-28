package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
)

var errScannedNFOChanged = errors.New("local NFO no longer matches its scan observation")

type scannedNFOProof struct {
	directory, identity string
	nfo                 nfoIngestProof
}

func (proof *scannedNFOProof) open(ctx context.Context, root *os.Root) (*os.Root, error) {
	dir, err := openRegisteredRoot(root, proof.directory)
	if err != nil {
		if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
			return nil, fatal
		}
		return nil, errScannedNFOChanged
	}
	return dir, nil
}

func (proof *scannedNFOProof) observe(ctx context.Context, root *os.Root, candidates []string,
	path, itemType string, proveRecovery bool) (observation localNFOObservation, resultErr error) {
	dir, err := proof.open(ctx, root)
	if err != nil {
		return localNFOObservation{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	info, err := dir.Stat(".")
	if err != nil || !info.IsDir() || fileIdentity(info) != proof.identity {
		return localNFOObservation{}, errors.Join(errScannedNFOChanged, localNFOFatalReadError(ctx, err))
	}
	names := make([]string, len(candidates))
	for i, candidate := range candidates {
		names[i] = filepath.Base(candidate)
	}
	if proveRecovery {
		observation, proof.nfo, err = observeNFORecoveryCandidate(ctx, dir, proof.directory, names, path, itemType, false)
	} else {
		observation, proof.nfo, err = observeMetadataIngestNFO(ctx, dir, proof.directory, names)
	}
	return observation, err
}

func (proof *scannedNFOProof) verify(ctx context.Context, root *os.Root) (resultErr error) {
	dir, err := proof.open(ctx, root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	if err := proof.nfo.verifyAt(ctx, dir); err != nil {
		if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
			return fatal
		}
		return errScannedNFOChanged
	}
	current, err := root.Lstat(proof.directory)
	if err != nil || !current.IsDir() || fileIdentity(current) != proof.identity {
		return errors.Join(errScannedNFOChanged, localNFOFatalReadError(ctx, err))
	}
	return errors.Join(proof.nfo.verifyRecoveryDirectory(current), ctx.Err())
}

func (input *primaryScanRead) revalidatePublicationNFO(tx pgx.Tx, proof *scannedNFOProof) error {
	if proof == nil || len(proof.nfo.entries) == 0 {
		return input.revalidatePublicationSource(tx)
	}
	return input.revalidatePublicationStorage(tx, func(ctx context.Context) error {
		return input.checkPhysicalSourceWithNFO(ctx, proof)
	})
}

// Verify file pairs and pending video NFOs, including after an item is renamed
// out of a pair layout. Retaining accepted NFO facts on a fast rescan or saving
// original bytes for a recovery task also requires this proof.
func (state *scanState) prepareScannedNFO(input *scannedMediaInput, path, itemType, parentID string,
	request *scanMetadataImport, checkDeferredNFO bool) (episodePairSource, *scannedNFOProof, error) {
	stored := input.stored
	witnessRecovery := scanIngestVideoLibrary(state.library) && state.store.nfoRecoveryWritable(state.root) &&
		(itemType == "Movie" || itemType == "Episode") && EffectiveLibraryOptions(state.library).EnableLocalMetadata
	if !witnessRecovery && stored.local.pair == nil && !stored.pendingNFO && !checkDeferredNFO {
		if itemType != "Episode" {
			return episodePairSource{}, nil, nil
		}
		if _, namedPair := episodeFilePair(path); !namedPair {
			return episodePairSource{}, nil, nil
		}
	}
	if !validImageScanPath(path) {
		return episodePairSource{}, nil, nil
	}
	directory := filepath.Dir(path)
	info := state.directoryIdentities[directory]
	if info == nil || !info.IsDir() {
		return episodePairSource{}, nil, errScanProbeSourceChanged
	}
	request.directoryIdentity = fileIdentity(info)
	if request.directoryIdentity == "" {
		return episodePairSource{}, nil, ErrUnavailable
	}
	proof := &scannedNFOProof{directory: directory, identity: request.directoryIdentity}
	if itemType != "Episode" {
		return episodePairSource{}, proof, nil
	}
	rootInfo := state.directoryIdentities["."]
	if rootInfo == nil || !rootInfo.IsDir() {
		return episodePairSource{}, nil, errScanProbeSourceChanged
	}
	source := newEpisodePairSource(input.primary.row, path, parentID, fileIdentity(rootInfo), *request, episodePairFileFromInfo(input.info))
	return source, proof, nil
}
