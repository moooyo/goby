package library

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

type localNFORereadObservation struct {
	root      seriesNFOAuditRootWitness
	directory os.FileInfo
	nfo       localNFOObservation
	proof     nfoIngestProof
}

func (o localNFORereadObservation) same(other localNFORereadObservation) bool {
	return !other.nfo.invalid && o.nfo.path == other.nfo.path && bytes.Equal(o.nfo.data, other.nfo.data) &&
		o.root.same(other.root) && sameSubtitleDirectoryInfo(o.directory, other.directory) &&
		sameLocalNFOFile(selectedNFOFile(o.proof), selectedNFOFile(other.proof))
}

func observeLocalNFOReread(ctx context.Context, op *PrimaryRootIO, plan seriesNFOAuditRootPlan,
	target localNFORereadTarget) (localNFORereadObservation, error) {
	// This also bounds the independent final observation under catalog ownership.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	observed, err := runSeriesNFOAuditRoot(ctx, op, plan, func(work context.Context, root *os.Root,
		witness seriesNFOAuditRootWitness) (localNFORereadObservation, error) {
		return target.observeAt(work, root, plan.root, witness)
	})
	if errors.Is(err, errSeriesNFOAuditRoot) {
		err = errors.Join(ErrSourceChanged, err)
	}
	return observed, err
}

func (target localNFORereadTarget) observeAt(ctx context.Context, root *os.Root, registered libraryRoot,
	witness seriesNFOAuditRootWitness) (_ localNFORereadObservation, resultErr error) {
	directoryPath := target.directory(registered)
	mediaPath, err := filepath.Rel(directoryPath, target.witness.relative)
	if err != nil || !localNFORereadPath(mediaPath) {
		return localNFORereadObservation{}, ErrSourceChanged
	}
	directory, err := openRootTopologyRelative(ctx, root, directoryPath)
	if err != nil {
		return localNFORereadObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, directory)) }()
	before, err := directory.Stat(".")
	if err != nil || fileIdentity(before) == "" {
		return localNFORereadObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	if err := target.witness.verify(ctx, directory, mediaPath); err != nil {
		return localNFORereadObservation{}, err
	}
	candidates, _ := target.candidates()
	nfo, proof, err := observeMetadataIngestNFOWithReader(ctx, directory, directoryPath, candidates, readLiteralRereadNFO)
	if err != nil {
		return localNFORereadObservation{}, err
	}
	if err := ctx.Err(); err != nil {
		return localNFORereadObservation{}, err
	}
	// A held descriptor can still see a directory detached by an external
	// rename. Start at the registered root again before accepting the proof.
	current, err := openRootTopologyRelative(ctx, root, directoryPath)
	if err != nil {
		return localNFORereadObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, current)) }()
	after, err := current.Stat(".")
	if err != nil || !sameSubtitleDirectoryInfo(before, after) {
		return localNFORereadObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	if err := target.witness.verify(ctx, current, mediaPath); err != nil {
		return localNFORereadObservation{}, err
	}
	if err := proof.verifyAt(ctx, current); err != nil {
		return localNFORereadObservation{}, errors.Join(ErrSourceChanged, err)
	}
	return localNFORereadObservation{root: witness, directory: before, nfo: nfo, proof: proof}, ctx.Err()
}

func (w localNFORereadMedia) verify(ctx context.Context, directory *os.Root, relative string) (resultErr error) {
	parent, err := openRootTopologyRelative(ctx, directory, filepath.Dir(relative))
	if err != nil {
		return errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, parent)) }()
	info, err := parent.Lstat(filepath.Base(relative))
	if err != nil || !w.matches(info) {
		return errors.Join(ErrSourceChanged, ctx.Err())
	}
	return ctx.Err()
}

func readLiteralRereadNFO(ctx context.Context, dir *os.Root, name string, before os.FileInfo) (_ []byte, _ bool, resultErr error) {
	parent, err := dir.Open(".")
	if err != nil {
		return nil, false, err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, parent)) }()
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	// Pin and inspect the literal leaf before opening for data. A replacement
	// symlink, FIFO or device must not even be opened for reading.
	pin, err := openLiteralNFOLeaf(parent, name, unix.O_PATH)
	if err != nil {
		return nil, false, err
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, pin)) }()
	info, err := pin.Stat()
	if err != nil || !sameLocalNFOFile(before, info) {
		return nil, true, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	file, err := os.OpenFile("/proc/self/fd/"+strconv.FormatUint(uint64(pin.Fd()), 10), os.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, err
	}
	return readOpenedLocalNFO(ctx, file, before, func() (os.FileInfo, error) { return dir.Lstat(name) })
}
