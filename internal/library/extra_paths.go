package library

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

type extraPathClassification struct {
	Reserved       bool
	Kind           string
	OwnerDirectory string
	Boundary       string
}

// classifyExtraPath recognizes only the witnessed Movie directory layouts.
// The first auxiliary directory owns its entire subtree: a nested theme or
// extra directory cannot promote resources through that outer reservation.
// Callers apply this policy only to Movies collections.
func classifyExtraPath(relative string, mode fs.FileMode) (extraPathClassification, error) {
	ordinary := extraPathClassification{}
	if relative == "." || !validMediaSourceRelativePath(relative) || path.Clean(relative) != relative ||
		strings.TrimSpace(relative) == "" || themePathHasDrivePrefix(relative) {
		return ordinary, fmt.Errorf("%w: extra paths must be canonical local relative entry paths", ErrInvalidInput)
	}
	if mode.Type() != 0 && mode.Type() != fs.ModeDir {
		return ordinary, fmt.Errorf("%w: extra classification requires a regular file or directory without symlinks", ErrInvalidInput)
	}
	parts := strings.Split(relative, "/")
	last := len(parts) - 1
	for index, component := range parts {
		if index == last && !mode.IsDir() {
			break
		}
		if themePathDirectoryLayout(component) != themePathLayoutNone {
			return ordinary, nil
		}
		kind := extraDirectoryKind(component)
		if kind == "" {
			continue
		}
		owner := "."
		if index != 0 {
			owner = strings.Join(parts[:index], "/")
		}
		result := extraPathClassification{Reserved: true, OwnerDirectory: owner, Boundary: strings.Join(parts[:index+1], "/")}
		if !mode.IsDir() && index == last-1 && !ignoredName(parts[last]) && extensionKind(parts[last]) == "video" {
			result.Kind = kind
		}
		return result, nil
	}
	return ordinary, nil
}

func extraDirectoryKind(name string) string {
	switch {
	case themePathNameEqual(name, "featurettes"):
		return ExtraKindClip
	case themePathNameEqual(name, "deleted scenes"):
		return ExtraKindDeletedScene
	case themePathNameEqual(name, "trailers"):
		return ExtraKindTrailer
	default:
		return ""
	}
}

// classifyScannedThemePath adds the Movie-only outer extra boundary without
// changing the historical theme classifier used by other collection types.
func (state *scanState) classifyScannedThemePath(relative string, mode fs.FileMode) (themePathClassification, error) {
	if state.library.CollectionType == "movies" {
		extra, err := classifyExtraPath(relative, mode)
		if err != nil {
			return themePathClassification{}, err
		}
		if extra.Reserved {
			return themePathClassification{Kind: themePathKindNone, Layout: themePathLayoutNone}, nil
		}
	}
	return classifyThemePath(relative, mode)
}
