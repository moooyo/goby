package library

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Keep this pattern valid in both Go and PostgreSQL. Requiring a trailing
// slash matches only directory components, not prefixes or file names.
const scanSystemDirectoryPattern = `(^|/)(#recycle|@eaDir|[$]RECYCLE[.]BIN)/`

var scanSystemDirectory = regexp.MustCompile(scanSystemDirectoryPattern)

func excludedScanPath(relative string, directory bool) bool {
	if directory {
		relative += "/"
	}
	return scanSystemDirectory.MatchString(relative)
}

// Older scans may have indexed these directories. Keep their rows and user
// data, but do not expose them as media or treat exclusion as proof of absence.
func includedScanPathSQL(alias string) string {
	item := pgx.Identifier{alias}.Sanitize()
	return `((` + item + `.relative_path || CASE WHEN ` + item + `.is_folder THEN '/' ELSE '' END) COLLATE "C" !~ '` + scanSystemDirectoryPattern + `')`
}

func tvSeasonNumber(name string) (int, bool) {
	if strings.EqualFold(name, "Specials") {
		return 0, true
	}
	match := seasonPattern.FindStringSubmatch(name)
	if match == nil {
		return 0, false
	}
	number, _ := strconv.Atoi(match[1])
	return number, true
}

func tvVideoEntry(entry os.DirEntry) bool {
	return entry.Type().IsRegular() && !ignoredName(entry.Name()) && extensionKind(entry.Name()) == "video"
}

// Directory classification needs only the marker's name and type. Disabling
// NFO import must not change the hierarchy or clear already imported values.
func tvShowMetadataPresent(entries []os.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() == "tvshow.nfo" && entry.Type().IsRegular() {
			return true
		}
	}
	return false
}

// A grouping directory cannot inherit series evidence from arbitrary
// descendants. Inspect its own entries and its immediate season directories,
// including one disc level. Loose, unnumbered videos still work in leaf folders.
func (state *scanState) tvSeriesDirectory(relative string, info os.FileInfo, entries []os.DirEntry, remainingDepth int) (bool, error) {
	if tvShowMetadataPresent(entries) {
		return true, nil
	}
	var hasVideo, hasDirectories, hasSeason, numbered, mixedTitles bool
	var title string
	for _, entry := range entries {
		name := entry.Name()
		if ignoredName(name) {
			continue
		}
		if entry.IsDir() {
			hasDirectories = true
			if _, ok := tvSeasonNumber(name); ok {
				hasSeason = true
			}
			continue
		}
		if !tvVideoEntry(entry) {
			continue
		}
		hasVideo = true
		match := episodePattern.FindStringSubmatch(strings.TrimSuffix(name, filepath.Ext(name)))
		if match == nil {
			continue
		}
		numbered = true
		// A flat folder of episodes from several named shows is a grouping
		// folder unless a stronger local marker says otherwise.
		show := cleanName(match[1])
		if show == "" {
			continue
		}
		if title == "" {
			title = show
		} else if !strings.EqualFold(title, show) {
			mixedTitles = true
		}
	}
	if !mixedTitles && (numbered || hasVideo && !hasDirectories) {
		return true, nil
	}
	if !hasSeason || remainingDepth < 1 {
		return false, nil
	}
	var found bool
	budget := tvSeasonReadBudget{entries: maxScanDirectoryEntries, bytes: maxScanDirectoryBytes}
	err := state.runPrimaryScanMetadata(state.task.ctx, func(ctx context.Context) (resultErr error) {
		// Use the original walk's witness so an intervening replacement cannot
		// supply evidence for the directory being classified.
		parent, err := openRegisteredRoot(state.opened, relative)
		if err != nil {
			return scanReadFailure(err)
		}
		defer func() { resultErr = errors.Join(resultErr, scanReadFailure(closeDirectoryPrimaryRoot(ctx, parent))) }()
		before, err := parent.Stat(".")
		if err != nil || !themeSnapshotEqual(info, before) {
			return errors.Join(ErrRootBindingConflict, err)
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			name := entry.Name()
			if _, ok := tvSeasonNumber(name); !entry.IsDir() || ignoredName(name) || !ok {
				continue
			}
			found, err = state.tvSeasonHasVideo(ctx, parent, relative, name, remainingDepth-1, &budget)
			if err != nil {
				return err
			}
			if found {
				break
			}
		}
		current, err := state.opened.Lstat(relative)
		if err != nil || !themeSnapshotEqual(info, current) {
			return errors.Join(ErrRootBindingConflict, err)
		}
		return ctx.Err()
	})
	if auxiliaryInputWarning(err) {
		return false, err
	}
	return found, scanReadFailure(err)
}

var tvDiscDirectory = regexp.MustCompile(`(?i)^(disc|disk|cd)[ ._-]*[0-9]{1,3}$`)

type tvSeasonReadBudget struct {
	entries, bytes int
}

func (state *scanState) tvSeasonHasVideo(ctx context.Context, parent *os.Root, relative, name string, remainingDepth int, budget *tvSeasonReadBudget) (_ bool, resultErr error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	root, err := openRegisteredRoot(parent, name)
	if err != nil {
		return false, auxiliarySourceFailure(err)
	}
	defer func() { resultErr = errors.Join(resultErr, scanReadFailure(closeDirectoryPrimaryRoot(ctx, root))) }()
	directory, err := openScanFile(root, ".")
	if err != nil {
		return false, auxiliarySourceFailure(err)
	}
	defer func() {
		resultErr = errors.Join(resultErr, scanReadFailure(closeDirectoryPrimaryResource(ctx, directory.Close)))
	}()
	before, err := directory.Stat()
	if err != nil {
		return false, auxiliarySourceFailure(err)
	}
	if !before.IsDir() {
		return false, ErrRootBindingConflict
	}
	relative = filepath.Join(relative, name)
	var found bool
	var childErr error
	if err := visitScanDirectoryEntries(ctx, directory, maxScanDirectoryEntries, maxScanDirectoryBytes, func(entry os.DirEntry) bool {
		entryName := entry.Name()
		budget.entries--
		budget.bytes -= scanDirectoryEntryCharge(entryName)
		if budget.entries < 0 || budget.bytes < 0 {
			childErr = auxiliarySourceFailure(errScanReconciliationEvidenceBudget)
			return true
		}
		path := filepath.ToSlash(filepath.Join(relative, entryName))
		if state.themePathReserved(path) || state.extraPathReserved(path) {
			return false
		}
		switch {
		case tvVideoEntry(entry):
			found = true
		case remainingDepth > 0 && entry.IsDir() && tvDiscDirectory.MatchString(entryName):
			// Check one disc level, not an arbitrary subtree under the season.
			found, childErr = state.tvSeasonHasVideo(ctx, root, relative, entryName, 0, budget)
		}
		return found || childErr != nil
	}); err != nil {
		return false, auxiliarySourceFailure(err)
	}
	if childErr != nil {
		return false, childErr
	}
	after, err := parent.Lstat(name)
	if err != nil || !themeSnapshotEqual(before, after) {
		return false, errors.Join(ErrRootBindingConflict, err)
	}
	return found, ctx.Err()
}
