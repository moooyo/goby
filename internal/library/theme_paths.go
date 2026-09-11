package library

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

type themePathKind string

const (
	themePathKindNone  themePathKind = "none"
	themePathKindSong  themePathKind = "song"
	themePathKindVideo themePathKind = "video"
)

type themePathLayout string

const (
	themePathLayoutNone      themePathLayout = "none"
	themePathLayoutFile      themePathLayout = "theme-file"
	themePathLayoutMusic     themePathLayout = "theme-music"
	themePathLayoutBackdrops themePathLayout = "backdrops"
)

// themePathClassification separates exclusion from resource candidacy. A
// reserved path with Kind=none must still stay out of the ordinary media walk
// and album audio detection. OwnerDirectory is a library-relative directory,
// not a catalog item ID or a decision between multiple movies in that directory.
// For a reserved subtree, Layout and OwnerDirectory identify its outer boundary;
// they do not promote nested layouts or specify precedence between resources.
type themePathClassification struct {
	Reserved       bool
	Kind           themePathKind
	OwnerDirectory string
	Layout         themePathLayout
}

// classifyThemePath recognizes the preserved theme layouts without opening
// files, probing media, selecting owners, or changing catalog state. Relative
// paths use canonical slash separators and name an entry below a registered
// root; "." is only used in the returned OwnerDirectory for root-level assets.
// Layout names use ASCII case-insensitive matching. Candidate extensions and
// ignored leaf names follow the existing scanner's local policy; that policy
// does not claim additional reference-server case or codec compatibility.
//
// Mode must come from a non-following entry observation in the anchored walk.
// Known symlinks and other nonregular types are rejected. This pure function
// cannot detect an ancestor symlink or a later filesystem replacement: callers
// must retain openRegisteredRoot/openScanFile and descriptor identity checks,
// and must not treat an error as permission to scan the path normally.
func classifyThemePath(relative string, mode fs.FileMode) (themePathClassification, error) {
	ordinary := themePathClassification{Kind: themePathKindNone, Layout: themePathLayoutNone}
	if relative == "." || !validMediaSourceRelativePath(relative) || path.Clean(relative) != relative ||
		strings.TrimSpace(relative) == "" || themePathHasDrivePrefix(relative) {
		return ordinary, fmt.Errorf("%w: theme paths must be canonical local relative entry paths", ErrInvalidInput)
	}
	if mode.Type() != 0 && mode.Type() != fs.ModeDir {
		return ordinary, fmt.Errorf("%w: theme classification requires a regular file or directory without symlinks", ErrInvalidInput)
	}

	components := strings.Split(relative, "/")
	last := len(components) - 1
	isDirectory := mode.Type() == fs.ModeDir
	for index, component := range components {
		if index == last && !isDirectory {
			break
		}
		layout := themePathDirectoryLayout(component)
		if layout == themePathLayoutNone {
			continue
		}
		owner := "."
		if index != 0 {
			owner = strings.Join(components[:index], "/")
		}
		reserved := themePathClassification{Reserved: true, Kind: themePathKindNone, OwnerDirectory: owner, Layout: layout}
		// The first reserved directory protects all descendants. Only a regular
		// immediate child with the corresponding media extension is a candidate.
		// In particular, an inner theme filename or reserved directory cannot
		// reinterpret an unproved nested resource as another layout's candidate.
		if isDirectory || index != last-1 || ignoredName(components[last]) {
			return reserved, nil
		}
		switch extensionKind(components[last]) {
		case "audio":
			if layout == themePathLayoutMusic {
				reserved.Kind = themePathKindSong
			}
		case "video":
			if layout == themePathLayoutBackdrops {
				reserved.Kind = themePathKindVideo
			}
		}
		return reserved, nil
	}

	name := components[last]
	if !isDirectory && extensionKind(name) == "audio" && themePathNameEqual(strings.TrimSuffix(name, path.Ext(name)), "theme") {
		return themePathClassification{Reserved: true, Kind: themePathKindSong,
			OwnerDirectory: path.Dir(relative), Layout: themePathLayoutFile}, nil
	}
	return ordinary, nil
}

func themePathDirectoryLayout(name string) themePathLayout {
	if themePathNameEqual(name, "theme-music") {
		return themePathLayoutMusic
	}
	if themePathNameEqual(name, "backdrops") {
		return themePathLayoutBackdrops
	}
	return themePathLayoutNone
}

func themePathNameEqual(name, expected string) bool {
	// Matching byte length excludes non-ASCII case-fold aliases of these
	// fixed ASCII layout names without rejecting Unicode in owner directories.
	return len(name) == len(expected) && strings.EqualFold(name, expected)
}

func themePathHasDrivePrefix(relative string) bool {
	if len(relative) < 2 || relative[1] != ':' {
		return false
	}
	letter := relative[0]
	return letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z'
}
