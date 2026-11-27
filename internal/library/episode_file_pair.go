package library

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/metadata"
)

var episodeFilePairPattern = regexp.MustCompile(`(?i)^(?:.*?[ ._-]+)?s([0-9]{1,3})e([0-9]{1,4})-?e([0-9]{1,4})$`)

// episodeFilePair accepts only two adjacent numbers at the end of a filename.
// The tag must be the same one the ordinary scanner uses for the first episode.
func episodeFilePair(path string) (metadata.EpisodePair, bool) {
	name := filepath.Base(path)
	if extensionKind(name) != "video" {
		return metadata.EpisodePair{}, false
	}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	matched := episodeFilePairPattern.FindStringSubmatchIndex(stem)
	if matched == nil {
		return metadata.EpisodePair{}, false
	}
	primary := episodePattern.FindStringSubmatchIndex(stem)
	if primary == nil || primary[4] != matched[2] || primary[6] != matched[4] {
		return metadata.EpisodePair{}, false
	}
	season, _ := strconv.Atoi(stem[matched[2]:matched[3]])
	first, _ := strconv.Atoi(stem[matched[4]:matched[5]])
	last, _ := strconv.Atoi(stem[matched[6]:matched[7]])
	if last != first+1 {
		return metadata.EpisodePair{}, false
	}
	return metadata.EpisodePair{Season: season, First: first, Last: last}, true
}

// EpisodeEndNumber projects a filename fact without opening media or creating
// another item. An effective numbering override can suppress but not shift it.
func (item Item) EpisodeEndNumber() (int, bool) {
	if item.Type != "Episode" || item.IsFolder || item.Path == "" {
		return 0, false
	}
	pair, ok := episodeFilePair(item.Path)
	if !ok || item.IndexNumber != pair.First || item.ParentIndexNumber != pair.Season {
		return 0, false
	}
	return pair.Last, true
}
