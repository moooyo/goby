package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/artwork"
	"github.com/moooyo/goby/internal/primaryio"
)

const maxLocalImageBytes = 20 * 1024 * 1024

type scannedImage struct {
	image    Image
	identity string
	filename string
	file     *os.File
	info     os.FileInfo
}

// scanImages runs on every successful media/folder visit, including a cached
// media probe. Validation happens before the short owned catalog transaction.
// One invalid candidate retains the entire previous image type; a complete,
// stable directory listing without candidates removes that type's old rows.
func (state *scanState) scanImages(itemID, itemType, relative string, isFolder bool) error {
	return state.scanImagesWithKnownAbsence(itemID, itemType, relative, isFolder, false)
}

func (state *scanState) scanImagesWithKnownAbsence(itemID, itemType, relative string, isFolder, knownNoLocalImages bool, completionCheck ...func() error) error {
	return state.retrySidecarScan(func() error {
		return state.scanImagesAttempt(itemID, itemType, relative, isFolder, knownNoLocalImages, completionCheck...)
	})
}

// imageRefreshAuthority is owned by a separately admitted metadata task. A
// scan uses its existing authority and unchanged-image fast path instead.
type imageRefreshAuthority struct {
	prepare func() (*PrimaryRootIO, rootBindingRow, error)
	check   func(context.Context, pgx.Tx) error
	finish  func(context.Context, pgx.Tx) error
	prove   func(context.Context) error
}

func (state *scanState) scanImagesAttempt(itemID, itemType, relative string, isFolder, knownNoLocalImages bool, completionCheck ...func() error) error {
	return state.refreshLocalImages(state.task.ctx, nil, itemID, itemType, relative, isFolder, knownNoLocalImages, completionCheck...)
}

func (state *scanState) refreshLocalImages(ctx context.Context, authority *imageRefreshAuthority, itemID, itemType, relative string,
	isFolder, knownNoLocalImages bool, completionCheck ...func() error) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !EffectiveLibraryOptions(state.library).EnableLocalImages {
		return nil
	}
	if !validImageScanPath(relative) || (strings.HasPrefix(relative, "//")) {
		state.warnings++
		return nil
	}
	directoryPath := relative
	if !isFolder {
		directoryPath = filepath.Dir(relative)
	}
	directoryPath = filepath.Clean(directoryPath)
	expected := state.directoryIdentities[directoryPath]
	if expected == nil {
		state.warnings++
		return nil
	}
	prepare := state.prepareSidecarScanIO
	if authority != nil {
		prepare = authority.prepare
	}
	operation, row, err := prepare()
	if err != nil {
		return scanReadFailure(err)
	}
	cleanupContext := operation.Context(ctx)
	var directoryRoot *os.Root
	var directory *os.File
	var directoryInfo os.FileInfo
	var sourceRoot *scanSourceRootWitness
	images := make(map[string][]*scannedImage)
	preserve := make(map[string]bool)
	inspected := make(map[string]*scannedImage)
	var proofWorkers sync.WaitGroup
	defer func() {
		proofWorkers.Wait()
		for _, image := range inspected {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanupContext, image.file))
		}
		if directory != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanupContext, directory))
		}
		if directoryRoot != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanupContext, directoryRoot))
		}
		if sourceRoot != nil {
			resultErr = errors.Join(resultErr, sourceRoot.Close())
		}
		resultErr = scanReadFailure(errors.Join(resultErr, operation.Close()))
	}()
	noCandidates := true
	var replaceTypes []string
	ready := false
	err = operation.Run(ctx, state.root.id, primaryio.Background, func(ctx context.Context) error {
		if authority != nil {
			if err := authority.prove(ctx); err != nil {
				return err
			}
		} else if err := state.checkSidecarScanAuthority(ctx, row); err != nil {
			return err
		}
		directoryRoot, err = openRegisteredRoot(state.opened, directoryPath)
		if err != nil {
			state.warnings++
			return nil
		}
		directory, err = openScanFile(directoryRoot, ".")
		if err != nil {
			state.warnings++
			return nil
		}
		directoryInfo, err = directory.Stat()
		if err != nil || !directoryInfo.IsDir() || !os.SameFile(expected, directoryInfo) {
			state.warnings++
			return nil
		}
		index := state.imageDirectories[directoryPath]
		if index != nil {
			if !sameSubtitleDirectoryInfo(index.info, directoryInfo) {
				state.warnings++
				return nil
			}
		} else {
			index, err = readImageDirectoryIndex(ctx, directory, directoryInfo)
			if err != nil {
				state.warnings++
				return nil
			}
			if state.imageDirectories == nil {
				state.imageDirectories = make(map[string]*imageDirectoryIndex)
			}
			state.imageDirectories[directoryPath] = index
		}
		candidates := index.selectedNames(itemType, relative, isFolder)
		for _, imageType := range scannedImageTypes {
			if len(candidates[imageType]) != 0 {
				noCandidates = false
			}
			for _, filename := range candidates[imageType] {
				image := inspected[filename]
				if image == nil {
					image, err = state.inspectLocalImageContext(ctx, directoryRoot, directoryPath, filename)
					if err != nil {
						if fatal := localNFOFatalReadError(ctx, err); fatal != nil {
							return fatal
						}
						preserve[imageType] = true
						state.warnings++
						break
					}
					inspected[filename] = image
				}
				images[imageType] = append(images[imageType], image)
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		currentDirectory, err := state.opened.Lstat(directoryPath)
		afterDirectory, afterErr := directory.Stat()
		if err != nil || afterErr != nil || !currentDirectory.IsDir() || currentDirectory.Mode()&os.ModeSymlink != 0 ||
			!os.SameFile(directoryInfo, currentDirectory) || !os.SameFile(directoryInfo, afterDirectory) ||
			!afterDirectory.ModTime().Equal(directoryInfo.ModTime()) || !currentDirectory.ModTime().Equal(directoryInfo.ModTime()) {
			state.warnings++
			return nil
		}
		for _, imageType := range scannedImageTypes {
			if preserve[imageType] {
				continue
			}
			for _, image := range images[imageType] {
				if err := verifyScannedImage(directoryRoot, image); err != nil {
					preserve[imageType] = true
					state.warnings++
					break
				}
			}
		}
		replaceTypes = make([]string, 0, len(scannedImageTypes))
		for _, imageType := range scannedImageTypes {
			if !preserve[imageType] {
				replaceTypes = append(replaceTypes, imageType)
			}
		}
		if len(replaceTypes) == 0 {
			return nil
		}
		if authority == nil && (!knownNoLocalImages || !noCandidates) {
			sourceRoot, err = state.captureScanSourceRoot(ctx, row)
			if err != nil {
				return err
			}
		}
		ready = true
		return nil
	})
	if err != nil {
		return scanReadFailure(err)
	}
	if !ready {
		return nil
	}
	if knownNoLocalImages && noCandidates {
		// The stored item lookup found no local image rows, and the complete,
		// stable directory supplied no replacement candidates.
		if len(completionCheck) != 0 && completionCheck[0] != nil {
			// A cached task-owned visit may combine its final fresh progress
			// fence with this already required owner-session query. Other image
			// paths do not complete that fence and retain their normal checkpoint.
			return completionCheck[0]()
		}
		return state.store.CheckOwnership(ctx)
	}
	replaceCounts := make([]int, len(replaceTypes))
	rowCount := 0
	for index, imageType := range replaceTypes {
		replaceCounts[index] = len(images[imageType])
		rowCount += replaceCounts[index]
	}
	// One item's selected population is bounded to 32 backdrops and one image
	// for each of the other five types. Prepare equal-length arrays before
	// acquiring catalog ownership; preserved types never enter this rowset.
	replacement := newImageScanReplacement(rowCount)
	position := 0
	for _, imageType := range replaceTypes {
		for index, source := range images[imageType] {
			replacement.set(position, imageType, index, directoryPath, source)
			position++
		}
	}
	// Preserve the same final source proof on both publication paths. It must
	// follow the catalog observation, including any wait for the owner session.
	finalSourceProof := func(proofContext context.Context) error {
		// The payload phase has retired. Its proof may only use immediately
		// available capacity, never queue behind a held database transaction.
		check := sourceRoot.Check
		if authority != nil {
			check = authority.prove
		}
		verify := func(proof context.Context) error {
			if err := check(proof); err != nil {
				return err
			}
			current, err := state.opened.Lstat(directoryPath)
			after, afterErr := directory.Stat()
			if err != nil || afterErr != nil || !sameSubtitleDirectoryInfo(directoryInfo, current) ||
				!sameSubtitleDirectoryInfo(directoryInfo, after) {
				return ErrSourceChanged
			}
			for _, imageType := range replaceTypes {
				for _, image := range images[imageType] {
					if err := verifyScannedImage(directoryRoot, image); err != nil {
						return errors.Join(ErrSourceChanged, err)
					}
				}
			}
			return check(proof)
		}
		return scanReadFailure(operation.RunImmediate(proofContext, state.root.id, primaryio.Background, func(proof context.Context) error {
			if authority == nil {
				return verify(proof)
			}
			// Roll back the writer on deadline, but join the actual proof before
			// the outer task closes its directory and image descriptors.
			return runIngestSourceProof(proof, &proofWorkers, verify)
		}))
	}
	// Known absence only selects the ordinary writer for newly populated sets;
	// it never establishes equality or adds a precheck to every cold item.
	if authority == nil && !knownNoLocalImages {
		unchanged, err := state.imageCatalogReplacementUnchanged(itemID, replaceTypes, replacement, row)
		if err != nil {
			return err
		}
		if unchanged {
			if err := finalSourceProof(ctx); err != nil {
				return err
			}
			if err := state.task.ctx.Err(); err != nil {
				return err
			}
			if !state.store.Available() {
				return ErrUnavailable
			}
			grant := state.task.authority.Load()
			if grant == nil {
				return ErrTaskScanInactive
			}
			return state.store.checkScanOperationActive(state.task.ctx, state.task, grant)
		}
	}
	tx, err := state.store.beginOwnedTx(ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if authority != nil {
		if err := authority.check(ctx, tx); err != nil {
			return err
		}
	} else if err := state.checkSidecarScanRootTx(tx, row); err != nil {
		return err
	}
	// Recheck under the writer's locks even after a read-only mismatch.
	if _, err := publishImageCatalogReplacement(ctx, tx, itemID, state.library.ID, state.root.id, replaceTypes, replaceCounts, replacement); err != nil {
		return err
	}
	proofContext := ctx
	if authority != nil {
		deadline, _ := tx.(*ownedTx).ctx.Deadline()
		var cancel context.CancelFunc
		proofContext, cancel = context.WithDeadline(ctx, deadline.Add(-scanReconciliationRollbackSpace))
		defer cancel()
	}
	if err := finalSourceProof(proofContext); err != nil {
		return err
	}
	if authority != nil {
		if err := authority.finish(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func validImageScanPath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && !hasTraversal(path) && !strings.ContainsAny(path, "\\\x00")
}

func (state *scanState) inspectLocalImage(root *os.Root, directoryPath, filename string) (*scannedImage, error) {
	return state.inspectLocalImageContext(state.task.ctx, root, directoryPath, filename)
}

func (state *scanState) inspectLocalImageContext(ctx context.Context, root *os.Root, directoryPath, filename string) (_ *scannedImage, resultErr error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jpg", ".jpeg", ".png", ".gif":
	default:
		return nil, fmt.Errorf("local artwork format is unsupported")
	}
	before, err := root.Lstat(filename)
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxLocalImageBytes {
		return nil, fmt.Errorf("local artwork is not a regular file within the size limit")
	}
	file, err := openScanFile(root, filename)
	if err != nil {
		return nil, fmt.Errorf("local artwork cannot be opened safely")
	}
	keep := false
	defer func() {
		if !keep {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, file))
		}
	}()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !sameMediaSourceFile(before, opened) {
		return nil, fmt.Errorf("local artwork changed while opening")
	}
	var info artwork.Info
	if state.imageInspection == nil {
		info, err = artwork.InspectJoined(ctx, file)
	} else {
		info, err = state.imageInspection.InspectJoined(ctx, file)
	}
	if err != nil {
		return nil, err
	}
	source := &scannedImage{
		image: Image{Path: filepath.Join(state.root.path, directoryPath, filename), Filename: filename,
			Tag: info.Tag, MIMEType: info.MIMEType, Width: info.Width, Height: info.Height,
			Size: opened.Size(), ModifiedAt: catalogModifiedTime(opened)},
		identity: fileIdentity(opened), filename: filename, file: file, info: opened,
	}
	if err := verifyScannedImage(root, source); err != nil {
		return nil, err
	}
	keep = true
	return source, nil
}

func verifyScannedImage(root *os.Root, image *scannedImage) error {
	after, err := image.file.Stat()
	current, currentErr := root.Lstat(image.filename)
	if err != nil || currentErr != nil || !after.Mode().IsRegular() || !current.Mode().IsRegular() ||
		!sameMediaSourceFile(image.info, after) || !sameMediaSourceFile(image.info, current) {
		return fmt.Errorf("local artwork changed during inspection")
	}
	return nil
}
