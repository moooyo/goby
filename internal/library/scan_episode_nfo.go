package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
)

var errScannedEpisodeNFOChanged = errors.New("episode NFO no longer matches its scan observation")

type scannedNFOProof struct {
	directory, identity string
	entries             nfoIngestProof
}

func (proof *scannedNFOProof) open(ctx context.Context, root *os.Root) (_ *os.Root, resultErr error) {
	dir, err := openRegisteredRoot(root, proof.directory)
	if err != nil {
		if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
			return nil, fatal
		}
		return nil, errScanProbeSourceChanged
	}
	info, err := dir.Stat(".")
	if err != nil || !info.IsDir() || fileIdentity(info) != proof.identity {
		return nil, errors.Join(errScanProbeSourceChanged, ctx.Err(), closeDirectoryPrimaryRoot(ctx, dir))
	}
	return dir, nil
}

func (proof *scannedNFOProof) observe(ctx context.Context, root *os.Root, candidates []string) (_ localNFOObservation, resultErr error) {
	dir, err := proof.open(ctx, root)
	if err != nil {
		return localNFOObservation{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	names := make([]string, len(candidates))
	for i, candidate := range candidates {
		names[i] = filepath.Base(candidate)
	}
	observation, entries, err := observeMetadataIngestNFO(ctx, dir, proof.directory, names)
	proof.entries = entries
	return observation, err
}

func (proof *scannedNFOProof) verify(ctx context.Context, root *os.Root) (resultErr error) {
	dir, err := proof.open(ctx, root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, dir)) }()
	if err := proof.entries.verifyAt(ctx, dir); err != nil {
		if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
			return fatal
		}
		return errScannedEpisodeNFOChanged
	}
	current, err := root.Lstat(proof.directory)
	if err != nil || !current.IsDir() || fileIdentity(current) != proof.identity {
		return errors.Join(errScanProbeSourceChanged, ctx.Err())
	}
	return ctx.Err()
}

func (input *primaryScanRead) revalidatePublicationNFO(tx pgx.Tx, proof *scannedNFOProof) error {
	if proof == nil {
		return input.revalidatePublicationSource(tx)
	}
	return input.revalidatePublicationStorage(tx, func(ctx context.Context) error {
		return input.checkPhysicalSourceWithNFO(ctx, proof)
	})
}

// Verify file pairs and pending video NFOs, including after an item is renamed
// out of a pair layout. Other local reads keep their existing scan behavior.
func (state *scanState) prepareScannedEpisodeNFO(input *scannedMediaInput, path, itemType, parentID string,
	request *scanMetadataImport) (episodePairSource, *scannedNFOProof, error) {
	stored := input.stored
	if stored.local.pair == nil && !stored.pendingNFO {
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
