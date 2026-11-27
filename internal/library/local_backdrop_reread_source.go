package library

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"
)

var localBackdropOtherTypes = []string{"Primary", "Thumb", "Banner", "Logo", "Art"}

type localBackdropObservation struct {
	root      seriesNFOAuditRootWitness
	directory os.FileInfo
	selected  map[string][]string
	files     map[string]os.FileInfo
	backdrops imageScanReplacement
	other     imageScanReplacement
}

type localBackdropObserver func(context.Context, *PrimaryRootIO, seriesNFOAuditRootPlan, localBackdropTarget) (localBackdropObservation, error)

func (s *Store) observeLocalBackdrop(ctx context.Context, op *PrimaryRootIO, plan seriesNFOAuditRootPlan, target localBackdropTarget) (localBackdropObservation, error) {
	return s.readLocalBackdropSource(ctx, op, plan, target, nil)
}

// Both phases start at fresh namespace descriptors. Only the first phase
// reads payload bytes; the writer's proof reselects names and compares their
// inode, size, mtime and ctime, independently of any scanner directory cache.
func (s *Store) readLocalBackdropSource(ctx context.Context, op *PrimaryRootIO, plan seriesNFOAuditRootPlan,
	target localBackdropTarget, previous *localBackdropObservation) (localBackdropObservation, error) {
	if previous != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	observed, err := runSeriesNFOAuditRoot(ctx, op, plan, func(work context.Context, root *os.Root, witness seriesNFOAuditRootWitness) (localBackdropObservation, error) {
		return s.observeLocalBackdropAt(work, root, witness, target, previous)
	})
	if errors.Is(err, errSeriesNFOAuditRoot) {
		err = errors.Join(ErrSourceChanged, err)
	}
	return observed, err
}

func (s *Store) observeLocalBackdropAt(ctx context.Context, root *os.Root, witness seriesNFOAuditRootWitness,
	target localBackdropTarget, previous *localBackdropObservation) (observed localBackdropObservation, resultErr error) {
	path := target.item.directory(target.root.root)
	directory, err := openRootTopologyRelative(ctx, root, path)
	if err != nil {
		return observed, errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, directory)) }()
	listing, err := openScanFile(directory, ".")
	if err != nil {
		return observed, errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, listing)) }()
	before, err := listing.Stat()
	if err != nil || !before.IsDir() || fileIdentity(before) != target.queue.directoryIdentity {
		return observed, errors.Join(ErrSourceChanged, ctx.Err())
	}
	if previous != nil && (!previous.root.same(witness) || !sameSubtitleDirectoryInfo(previous.directory, before)) {
		return observed, ErrSourceChanged
	}
	index, err := readImageDirectoryIndex(ctx, listing, before)
	if err != nil {
		return observed, errors.Join(ErrSourceChanged, ctx.Err())
	}
	selected := index.selectedNames("Series", path, true)
	if previous != nil {
		for _, imageType := range scannedImageTypes {
			if !slices.Equal(previous.selected[imageType], selected[imageType]) {
				return observed, ErrSourceChanged
			}
		}
		observed = *previous
	} else {
		observed = localBackdropObservation{root: witness, directory: before, selected: selected, files: make(map[string]os.FileInfo)}
		if err := s.inspectLocalBackdropSelection(ctx, directory, path, target.root.root, &observed); err != nil {
			return localBackdropObservation{}, err
		}
	}
	// Prove the directory and every selected file from the registered root
	// again. A held listing may still see a directory detached by a rename.
	current, err := openRootTopologyRelative(ctx, root, path)
	if err != nil {
		return localBackdropObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, current)) }()
	if info, err := current.Stat("."); err != nil || !sameSubtitleDirectoryInfo(before, info) {
		return localBackdropObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	for name, expected := range observed.files {
		if err := ctx.Err(); err != nil {
			return localBackdropObservation{}, err
		}
		info, err := current.Lstat(name)
		if err != nil || !info.Mode().IsRegular() || !sameMediaSourceFile(expected, info) {
			return localBackdropObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
		}
	}
	// Reopen the name after the leaf checks as well. If a parent was detached,
	// the held series directory itself need not have changed timestamps.
	linked, err := openRootTopologyRelative(ctx, root, path)
	if err != nil {
		return localBackdropObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	defer func() { resultErr = errors.Join(resultErr, closeDirectoryPrimaryRoot(ctx, linked)) }()
	if info, err := linked.Stat("."); err != nil || !sameSubtitleDirectoryInfo(before, info) {
		return localBackdropObservation{}, errors.Join(ErrSourceChanged, ctx.Err())
	}
	return observed, ctx.Err()
}

func (s *Store) inspectLocalBackdropSelection(ctx context.Context, directory *os.Root, path string, root libraryRoot, observed *localBackdropObservation) error {
	backdrops := observed.selected[localBackdropImageType]
	if len(backdrops) == 0 {
		return ErrLocalBackdropRejected
	}
	otherCount := 0
	for _, imageType := range localBackdropOtherTypes {
		otherCount += len(observed.selected[imageType])
	}
	observed.backdrops = newImageScanReplacement(len(backdrops))
	observed.other = newImageScanReplacement(otherCount)
	inspector := &scanState{root: root, imageInspection: &s.imageInspection}
	inspect := func(imageType, name string) (*scannedImage, error) {
		image, err := inspector.inspectLocalImageContext(ctx, directory, path, name)
		if err != nil {
			if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
				return nil, fatal
			}
			if imageType == localBackdropImageType {
				return nil, ErrLocalBackdropRejected
			}
			return nil, ErrLocalBackdropOtherImages
		}
		if err := closePrimarySidecarResource(ctx, image.file); err != nil {
			return nil, err
		}
		observed.files[name] = image.info
		return image, nil
	}
	for i, name := range backdrops {
		image, err := inspect(localBackdropImageType, name)
		if err != nil {
			return err
		}
		observed.backdrops.set(i, localBackdropImageType, i, path, image)
	}
	position := 0
	for _, imageType := range localBackdropOtherTypes {
		for i, name := range observed.selected[imageType] {
			image, err := inspect(imageType, name)
			if err != nil {
				return err
			}
			observed.other.set(position, imageType, i, path, image)
			position++
		}
	}
	return ctx.Err()
}
