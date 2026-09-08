package library

import (
	"os"
	"path/filepath"
	"strings"
)

// imageDirectoryIndex caches names only for one scan's held directory identity.
// Per-item lookups are bounded by the supported naming conventions and at most
// two pre-truncated backdrop groups, independent of the directory entry count.
type imageDirectoryIndex struct {
	info      os.FileInfo
	exact     map[string]string
	backdrops map[string][]string
}

func newImageDirectoryIndex(names []string, info os.FileInfo) *imageDirectoryIndex {
	index := &imageDirectoryIndex{info: info, exact: make(map[string]string, len(names)), backdrops: make(map[string][]string)}
	groups := make(map[string][]string)
	for _, name := range names {
		if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\\x00") {
			continue
		}
		key := strings.ToLower(name)
		if previous, found := index.exact[key]; !found || name < previous {
			index.exact[key] = name
		}
	}
	for lower, name := range index.exact {
		if prefix, ok := imageBackdropPrefix(lower); ok {
			groups[prefix] = append(groups[prefix], name)
		}
	}
	for prefix, names := range groups {
		if prefix == "" {
			index.backdrops[prefix] = imageCandidateNames(names, "Folder", ".", true)["Backdrop"]
		} else {
			index.backdrops[prefix] = imageCandidateNames(names, "Movie", prefix+".mkv", false)["Backdrop"]
		}
	}
	return index
}

func (index *imageDirectoryIndex) candidateNames(itemType, relative string, isFolder bool) map[string][]string {
	stems := []string{"poster", "folder", "cover", "default", "movie", "show", "artist-poster", "artist-cover", "artist",
		"thumb", "landscape", "banner", "clearlogo", "logo", "clearart"}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative)))
	if !isFolder {
		for _, suffix := range []string{"", "-poster", "-cover", "-default", "-movie", "-thumb", "-landscape", "-banner", "-clearlogo", "-logo", "-clearart"} {
			stems = append(stems, base+suffix)
		}
	}
	names := make([]string, 0, 32)
	for _, stem := range stems {
		for _, extension := range localImageExtensions {
			if name, exists := index.exact[stem+extension]; exists {
				names = append(names, name)
			}
		}
	}
	names = append(names, index.backdrops[""]...)
	if !isFolder && base != "" {
		names = append(names, index.backdrops[base]...)
	}
	return imageCandidateNames(names, itemType, relative, isFolder)
}

func imageBackdropPrefix(name string) (string, bool) {
	extension := filepath.Ext(name)
	accepted := false
	for _, candidate := range localImageExtensions {
		if candidate == extension {
			accepted = true
			break
		}
	}
	if !accepted {
		return "", false
	}
	stem := strings.TrimSuffix(name, extension)
	validSuffix := func(suffix string) bool {
		if suffix == "-" {
			return false
		}
		suffix = strings.TrimPrefix(suffix, "-")
		return strings.IndexFunc(suffix, func(character rune) bool { return character < '0' || character > '9' }) < 0
	}
	for _, family := range []string{"backdrop", "fanart", "background", "art"} {
		if strings.HasPrefix(stem, family) && validSuffix(strings.TrimPrefix(stem, family)) {
			return "", true
		}
		position := strings.LastIndex(stem, "-"+family)
		if position > 0 && validSuffix(stem[position+len(family)+1:]) {
			return stem[:position], true
		}
	}
	return "", false
}
