package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

func (state *scanState) scanImagesAttempt(itemID, itemType, relative string, isFolder, knownNoLocalImages bool, completionCheck ...func() error) (resultErr error) {
	if err := state.task.ctx.Err(); err != nil {
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
	operation, row, err := state.prepareSidecarScanIO()
	if err != nil {
		return scanReadFailure(err)
	}
	cleanupContext := operation.Context(state.task.ctx)
	var directoryRoot *os.Root
	var directory *os.File
	var directoryInfo os.FileInfo
	images := make(map[string][]*scannedImage)
	preserve := make(map[string]bool)
	inspected := make(map[string]*scannedImage)
	defer func() {
		for _, image := range inspected {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanupContext, image.file))
		}
		if directory != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanupContext, directory))
		}
		if directoryRoot != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(cleanupContext, directoryRoot))
		}
		resultErr = scanReadFailure(errors.Join(resultErr, operation.Close()))
	}()
	noCandidates := true
	var replaceTypes []string
	ready := false
	err = operation.Run(state.task.ctx, state.root.id, primaryio.Background, func(ctx context.Context) error {
		if err := state.checkSidecarScanAuthority(ctx, row); err != nil {
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
			if !os.SameFile(index.info, directoryInfo) || !index.info.ModTime().Equal(directoryInfo.ModTime()) {
				state.warnings++
				return nil
			}
		} else {
			entries, err := directory.ReadDir(-1)
			if err != nil {
				state.warnings++
				return nil
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			index = newImageDirectoryIndex(names, directoryInfo)
			if state.imageDirectories == nil {
				state.imageDirectories = make(map[string]*imageDirectoryIndex)
			}
			state.imageDirectories[directoryPath] = index
		}
		candidates := index.candidateNames(itemType, relative, isFolder)
		for _, imageType := range scannedImageTypes {
			if len(candidates[imageType]) != 0 {
				noCandidates = false
				break
			}
		}
		for _, imageType := range scannedImageTypes {
			selected := candidates[imageType]
			if imageType != "Backdrop" && len(selected) > 1 {
				selected = selected[:1]
			}
			for _, filename := range selected {
				image := inspected[filename]
				if image == nil {
					image, err = state.inspectLocalImageContext(ctx, directoryRoot, directoryPath, filename)
					if err != nil {
						if state.task.ctx.Err() != nil {
							return state.task.ctx.Err()
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
		if err := state.task.ctx.Err(); err != nil {
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
		return state.store.CheckOwnership(state.task.ctx)
	}
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if err := state.checkSidecarScanRootTx(tx, row); err != nil {
		return err
	}
	beforeCatalog, err := readImageCatalogSnapshot(state.task.ctx, tx, itemID, state.library.ID, state.root.id)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(state.task.ctx, `DELETE FROM item_images
		WHERE item_id = $1 AND image_type = ANY($2::text[])`, itemID, replaceTypes); err != nil {
		return err
	}
	for _, imageType := range scannedImageTypes {
		if preserve[imageType] {
			continue
		}
		for index, source := range images[imageType] {
			image := source.image
			_, err := tx.Exec(state.task.ctx, `INSERT INTO item_images
				(item_id, root_id, image_type, image_index, relative_path, file_identity, source_hash,
				 file_size, modified_at, width, height, mime_type)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				itemID, state.root.id, imageType, index, filepath.ToSlash(filepath.Join(directoryPath, source.filename)),
				source.identity, image.Tag, image.Size, image.ModifiedAt, image.Width, image.Height, image.MIMEType)
			if err != nil {
				return err
			}
		}
	}
	afterCatalog, err := readImageCatalogSnapshot(state.task.ctx, tx, itemID, state.library.ID, state.root.id)
	if err != nil {
		return err
	}
	if beforeCatalog.properties != afterCatalog.properties {
		if err := recordCatalogChanges(tx, afterCatalog.owner); err != nil {
			return err
		}
	}
	// The payload phase retired before this transaction. Its final source proof
	// may only use immediately available capacity, never a database-held queue.
	if err := operation.RunImmediate(state.task.ctx, state.root.id, primaryio.Background, func(context.Context) error {
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
		return nil
	}); err != nil {
		return scanReadFailure(err)
	}
	return tx.Commit(state.task.ctx)
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
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() > maxLocalImageBytes {
		return nil, fmt.Errorf("local artwork changed while opening")
	}
	info, err := artwork.InspectJoined(ctx, file)
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
		!os.SameFile(image.info, after) || !os.SameFile(image.info, current) || after.Size() != image.info.Size() ||
		current.Size() != image.info.Size() || !after.ModTime().Equal(image.info.ModTime()) || !current.ModTime().Equal(image.info.ModTime()) {
		return fmt.Errorf("local artwork changed during inspection")
	}
	return nil
}
