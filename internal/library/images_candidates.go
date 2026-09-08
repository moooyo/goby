package library

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var scannedImageTypes = []string{"Primary", "Backdrop", "Thumb", "Banner", "Logo", "Art"}

// The trailing formats are recognizable artwork candidates but are deliberately
// unsupported. Their presence retains the previous valid image with a warning,
// instead of silently treating an invalid replacement as an image deletion.
var localImageExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp", ".avif", ".bmp", ".tif", ".tiff"}

// Names follow the local-image tables in the official Emby Movie-Naming,
// TV-Naming, and Music-Naming articles. This subset does not follow extrafanart
// directories or series-directory seasonXX files. All returned names belong to
// the already enumerated immediate directory; no metadata URL is interpreted.
// Sources: https://emby.media/support/articles/Movie-Naming.html,
// https://emby.media/support/articles/TV-Naming.html,
// https://emby.media/support/articles/Music-Naming.html.
func imageCandidateNames(names []string, itemType, relative string, isFolder bool) map[string][]string {
	ordered := append([]string(nil), names...)
	sort.Strings(ordered)
	byName := make(map[string]string, len(ordered))
	for _, name := range ordered {
		if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, "/\\\x00") {
			continue
		}
		key := strings.ToLower(name)
		if _, exists := byName[key]; !exists {
			byName[key] = name
		}
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative)))
	if isFolder {
		base = ""
	}
	result := make(map[string][]string, len(scannedImageTypes))
	add := func(imageType string, stems ...string) {
		seen := make(map[string]bool)
		for _, stem := range stems {
			for _, extension := range localImageExtensions {
				if name, exists := byName[stem+extension]; exists && !seen[name] {
					result[imageType] = append(result[imageType], name)
					seen[name] = true
				}
			}
		}
	}
	if isFolder {
		primary := []string{"poster", "folder", "cover", "default"}
		if itemType == "Series" {
			primary = append(primary, "show")
		}
		if itemType == "MusicArtist" {
			primary = append(primary, "artist-poster", "artist-cover", "artist")
		}
		add("Primary", primary...)
	} else if itemType == "Episode" {
		add("Primary", base+"-thumb", base)
	} else {
		add("Primary", base+"-poster", base+"-cover", base, base+"-default", base+"-movie", "poster", "folder", "cover", "default", "movie")
	}
	allowGeneric := isFolder || (itemType != "Episode" && itemType != "Audio")
	for imageType, suffixes := range map[string][]string{
		"Thumb": {"thumb", "landscape"}, "Banner": {"banner"}, "Logo": {"clearlogo", "logo"}, "Art": {"clearart"},
	} {
		stems := make([]string, 0, len(suffixes)*2)
		if !isFolder {
			for _, suffix := range suffixes {
				stems = append(stems, base+"-"+suffix)
			}
		}
		if allowGeneric {
			stems = append(stems, suffixes...)
		}
		add(imageType, stems...)
	}
	type backdrop struct {
		name, stem                  string
		number, family, prefix, ext int
	}
	backdrops := make([]backdrop, 0)
	for lower, name := range byName {
		extension := filepath.Ext(lower)
		extensionRank := -1
		for index, allowed := range localImageExtensions {
			if extension == allowed {
				extensionRank = index
				break
			}
		}
		if extensionRank < 0 {
			continue
		}
		stem := strings.TrimSuffix(lower, extension)
		candidate, prefix := stem, 1
		if !isFolder && strings.HasPrefix(stem, base+"-") {
			candidate, prefix = strings.TrimPrefix(stem, base+"-"), 0
		} else if !allowGeneric {
			continue
		}
		for family, keyword := range []string{"backdrop", "fanart", "background", "art"} {
			if !strings.HasPrefix(candidate, keyword) {
				continue
			}
			suffix := strings.TrimPrefix(candidate, keyword)
			if suffix == "-" {
				break
			}
			suffix = strings.TrimPrefix(suffix, "-")
			number := 0
			if suffix != "" {
				if strings.IndexFunc(suffix, func(character rune) bool { return character < '0' || character > '9' }) >= 0 {
					break
				}
				value, err := strconv.ParseUint(suffix, 10, 31)
				if err != nil {
					break
				}
				number = int(value)
			}
			backdrops = append(backdrops, backdrop{name: name, stem: stem, number: number, family: family, prefix: prefix, ext: extensionRank})
			break
		}
	}
	sort.Slice(backdrops, func(i, j int) bool {
		a, b := backdrops[i], backdrops[j]
		if a.prefix != b.prefix {
			return a.prefix < b.prefix
		}
		if a.number != b.number {
			return a.number < b.number
		}
		if a.family != b.family {
			return a.family < b.family
		}
		if a.stem != b.stem {
			return a.stem < b.stem
		}
		if a.ext != b.ext {
			return a.ext < b.ext
		}
		return a.name < b.name
	})
	seenStems := make(map[string]bool)
	for _, entry := range backdrops {
		if seenStems[entry.stem] {
			continue
		}
		seenStems[entry.stem] = true
		result["Backdrop"] = append(result["Backdrop"], entry.name)
		if len(result["Backdrop"]) == 32 {
			break
		}
	}
	return result
}
