package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	var sourceRoot *scanSourceRootWitness
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
		if sourceRoot != nil {
			resultErr = errors.Join(resultErr, sourceRoot.Close())
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
		if !knownNoLocalImages || !noCandidates {
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
		return state.store.CheckOwnership(state.task.ctx)
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
	replacement := imageScanReplacement{
		types: make([]string, rowCount), indexes: make([]int, rowCount), paths: make([]string, rowCount),
		identities: make([]string, rowCount), hashes: make([]string, rowCount), sizes: make([]int64, rowCount),
		modified: make([]time.Time, rowCount), widths: make([]int, rowCount), heights: make([]int, rowCount),
		mimes: make([]string, rowCount),
	}
	position := 0
	for _, imageType := range replaceTypes {
		for index, source := range images[imageType] {
			image := source.image
			replacement.types[position], replacement.indexes[position] = imageType, index
			replacement.paths[position] = filepath.ToSlash(filepath.Join(directoryPath, source.filename))
			replacement.identities[position], replacement.hashes[position] = source.identity, image.Tag
			replacement.sizes[position], replacement.modified[position] = image.Size, image.ModifiedAt
			replacement.widths[position], replacement.heights[position], replacement.mimes[position] = image.Width, image.Height, image.MIMEType
			position++
		}
	}
	// Preserve the same final source proof on both publication paths. It must
	// follow the catalog observation, including any wait for the owner session.
	finalSourceProof := func() error {
		// The payload phase has retired. Its proof may only use immediately
		// available capacity, never queue behind a held database transaction.
		return scanReadFailure(operation.RunImmediate(state.task.ctx, state.root.id, primaryio.Background, func(proof context.Context) error {
			if err := sourceRoot.Check(proof); err != nil {
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
			return sourceRoot.Check(proof)
		}))
	}
	// Known absence only selects the ordinary writer for newly populated sets;
	// it never establishes equality or adds a precheck to every cold item.
	if !knownNoLocalImages {
		unchanged, err := state.imageCatalogReplacementUnchanged(itemID, replaceTypes, replacement, row)
		if err != nil {
			return err
		}
		if unchanged {
			if err := finalSourceProof(); err != nil {
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
	tx, err := state.store.beginOwnedTx(state.task.ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, rollbackSidecarTransaction(tx, resultErr)) }()
	if err := state.checkSidecarScanRootTx(tx, row); err != nil {
		return err
	}
	beforeCatalog, changed, err := readImageCatalogReplacement(state.task.ctx, tx, itemID, state.library.ID, state.root.id,
		replaceTypes, replacement)
	if err != nil {
		return err
	}
	// Re-read under the writer's locks even after a read-only mismatch. Its
	// before-notification snapshot and changes belong to this transaction.
	if changed {
		// Candidate indexes are contiguous within each replaceable type. Keep
		// existing keys for conditional updates, including rows from an old root.
		deleted, err := tx.Exec(state.task.ctx, `DELETE FROM item_images im
		USING unnest($2::text[], $3::integer[]) AS replacement(image_type, image_count)
		WHERE im.item_id = $1 AND im.image_type = replacement.image_type
		AND im.image_index >= replacement.image_count`, itemID, replaceTypes, replaceCounts)
		if err != nil {
			return err
		}
		changedRows := deleted.RowsAffected()
		if rowCount != 0 {
			written, err := tx.Exec(state.task.ctx, `INSERT INTO item_images
			(item_id, root_id, image_type, image_index, relative_path, file_identity, source_hash,
			 file_size, modified_at, width, height, mime_type)
			SELECT $1,$2,replacement.image_type,replacement.image_index,replacement.relative_path,
				replacement.file_identity,replacement.source_hash,replacement.file_size,replacement.modified_at,
				replacement.width,replacement.height,replacement.mime_type
			FROM unnest($3::text[],$4::integer[],$5::text[],$6::text[],$7::text[],$8::bigint[],
				$9::timestamptz[],$10::integer[],$11::integer[],$12::text[])
			AS replacement(image_type,image_index,relative_path,file_identity,source_hash,file_size,
				modified_at,width,height,mime_type)
			WHERE true
			ON CONFLICT (item_id, image_type, image_index) DO UPDATE SET
			root_id = EXCLUDED.root_id, relative_path = EXCLUDED.relative_path,
			file_identity = EXCLUDED.file_identity, source_hash = EXCLUDED.source_hash,
			file_size = EXCLUDED.file_size, modified_at = EXCLUDED.modified_at,
			width = EXCLUDED.width, height = EXCLUDED.height, mime_type = EXCLUDED.mime_type
			WHERE (item_images.root_id, item_images.relative_path, item_images.file_identity,
			item_images.source_hash, item_images.file_size, item_images.modified_at,
			item_images.width, item_images.height, item_images.mime_type)
			IS DISTINCT FROM (EXCLUDED.root_id, EXCLUDED.relative_path, EXCLUDED.file_identity,
			EXCLUDED.source_hash, EXCLUDED.file_size, EXCLUDED.modified_at,
			EXCLUDED.width, EXCLUDED.height, EXCLUDED.mime_type)`,
				itemID, state.root.id, replacement.types, replacement.indexes, replacement.paths, replacement.identities, replacement.hashes,
				replacement.sizes, replacement.modified, replacement.widths, replacement.heights, replacement.mimes)
			if err != nil {
				return err
			}
			changedRows += written.RowsAffected()
		}
		if changedRows != 0 {
			afterCatalog, err := readImageCatalogSnapshot(state.task.ctx, tx, itemID, state.library.ID, state.root.id)
			if err != nil {
				return err
			}
			if beforeCatalog.properties != afterCatalog.properties {
				if err := recordCatalogChanges(tx, afterCatalog.owner); err != nil {
					return err
				}
			}
		}
	}
	if err := finalSourceProof(); err != nil {
		return err
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
		!os.SameFile(image.info, after) || !os.SameFile(image.info, current) || after.Size() != image.info.Size() ||
		current.Size() != image.info.Size() || !after.ModTime().Equal(image.info.ModTime()) || !current.ModTime().Equal(image.info.ModTime()) {
		return fmt.Errorf("local artwork changed during inspection")
	}
	return nil
}
