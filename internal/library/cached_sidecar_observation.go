package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/moooyo/goby/internal/media"
)

// A cached observation owns only immutable facts. All descriptors are closed
// inside the admitted publication phase before any catalog ownership check.
type cachedSidecarObservation struct {
	absent        bool
	sourceChanged bool
	directoryPath string
	primary       os.FileInfo
	subtitles     *subtitleDirectoryIndex
	images        *imageDirectoryIndex
}

const (
	maxCachedSidecarDirectoryEntries = 4096
	maxCachedSidecarDirectoryBytes   = 4 << 20
)

// observeCachedSidecarAbsence is a narrow no-payload observation. A candidate,
// incomplete listing, or changed source leaves the ordinary sidecar scan in
// charge of payload inspection and deletion. It never publishes catalog rows.
func (state *scanState) observeCachedSidecarAbsence(ctx context.Context, relative, itemType string, probe *media.Info, primary os.FileInfo) (_ cachedSidecarObservation, resultErr error) {
	return state.observeCachedSidecarAbsenceWithReader(ctx, relative, itemType, probe, primary, readScanDirectoryEntriesLimit)
}

func (state *scanState) observeCachedSidecarAbsenceWithReader(ctx context.Context, relative, itemType string, probe *media.Info, primary os.FileInfo,
	readEntries func(context.Context, *os.File, int, int) ([]os.DirEntry, error)) (_ cachedSidecarObservation, resultErr error) {
	if err := ctx.Err(); err != nil {
		return cachedSidecarObservation{}, err
	}
	if !validMediaSourceRelativePath(relative) || !validImageScanPath(relative) || probe == nil || primary == nil || !primary.Mode().IsRegular() {
		return cachedSidecarObservation{}, nil
	}
	directoryPath := filepath.Clean(filepath.Dir(relative))
	expected := state.directoryIdentities[directoryPath]
	if expected == nil {
		return cachedSidecarObservation{}, nil
	}
	var root, currentRoot, currentDirectory *os.Root
	var directory *os.File
	defer func() {
		if currentDirectory != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, currentDirectory))
		}
		if currentRoot != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, currentRoot))
		}
		if directory != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, directory))
		}
		if root != nil {
			resultErr = errors.Join(resultErr, closePrimarySidecarResource(ctx, root))
		}
		resultErr = errors.Join(resultErr, ctx.Err())
	}()
	var err error
	root, err = openRegisteredRoot(state.opened, directoryPath)
	if err != nil {
		return cachedSidecarObservation{}, nil
	}
	directory, err = openScanFile(root, ".")
	if err != nil {
		return cachedSidecarObservation{}, nil
	}
	directoryInfo, err := directory.Stat()
	if err != nil || !sameSubtitleDirectoryInfo(expected, directoryInfo) {
		return cachedSidecarObservation{}, nil
	}
	subtitles := state.subtitleDirectories[directoryPath]
	imagesEnabled := EffectiveLibraryOptions(state.library).EnableLocalImages
	images := state.imageDirectories[directoryPath]
	if subtitles != nil && !sameSubtitleDirectoryInfo(subtitles.info, directoryInfo) ||
		imagesEnabled && images != nil && !sameSubtitleDirectoryInfo(images.info, directoryInfo) {
		return cachedSidecarObservation{}, nil
	}
	if subtitles == nil || imagesEnabled && images == nil {
		entries, err := readEntries(ctx, directory, maxCachedSidecarDirectoryEntries, maxCachedSidecarDirectoryBytes)
		if err != nil {
			return cachedSidecarObservation{}, ctx.Err()
		}
		if subtitles == nil {
			subtitles = newSubtitleDirectoryIndex(entries, state.library.CollectionType, directoryInfo)
		}
		if imagesEnabled && images == nil {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			images = newImageDirectoryIndex(names, directoryInfo)
		}
	}
	candidates, overflow := subtitles.candidates(relative)
	if overflow || len(candidates) != 0 {
		return cachedSidecarObservation{}, nil
	}
	if imagesEnabled {
		candidates := images.candidateNames(itemType, relative, false)
		for _, imageType := range scannedImageTypes {
			if len(candidates[imageType]) != 0 {
				return cachedSidecarObservation{}, nil
			}
		}
	}
	if probe.FileChangeTimeNs > 0 && media.FileChangeTime(primary) != probe.FileChangeTimeNs {
		return cachedSidecarObservation{}, nil
	}
	currentRoot, err = state.store.openScanOperationRoot(ctx, state.task, state.root)
	if err != nil {
		return cachedSidecarObservation{sourceChanged: true}, nil
	}
	currentDirectory, err = openRegisteredRoot(currentRoot, directoryPath)
	if err != nil {
		return cachedSidecarObservation{sourceChanged: true}, nil
	}
	currentDirectoryInfo, currentErr := currentDirectory.Stat(".")
	afterDirectoryInfo, afterErr := directory.Stat()
	currentPrimary, primaryErr := currentDirectory.Lstat(filepath.Base(relative))
	if !sameMediaSourceDirectory(state.opened, currentRoot) || primaryErr != nil ||
		!sameMediaSourceFile(primary, currentPrimary) || !currentPrimary.Mode().IsRegular() {
		return cachedSidecarObservation{sourceChanged: true}, nil
	}
	if currentErr != nil || afterErr != nil || !sameSubtitleDirectoryInfo(directoryInfo, currentDirectoryInfo) ||
		!sameSubtitleDirectoryInfo(directoryInfo, afterDirectoryInfo) {
		return cachedSidecarObservation{}, nil
	}
	return cachedSidecarObservation{absent: true, directoryPath: directoryPath, primary: primary, subtitles: subtitles, images: images}, nil
}

func (state *scanState) retainCachedSidecarObservation(observation cachedSidecarObservation) {
	if !observation.absent {
		return
	}
	if state.subtitleDirectories == nil {
		state.subtitleDirectories = make(map[string]*subtitleDirectoryIndex)
	}
	state.subtitleDirectories[observation.directoryPath] = observation.subtitles
	if observation.images != nil {
		if state.imageDirectories == nil {
			state.imageDirectories = make(map[string]*imageDirectoryIndex)
		}
		state.imageDirectories[observation.directoryPath] = observation.images
	}
}
