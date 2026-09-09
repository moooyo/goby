package library

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var subtitleLanguagePattern = regexp.MustCompile(`^[a-zA-Z]{2,3}(?:-[a-zA-Z0-9]{2,8})*$`)

type subtitleCandidate struct {
	filename string
	info     Subtitle
}

// subtitleDirectoryIndex groups sidecars once for each held scan directory.
// A more specific media basename always owns its matching subtitle, preventing
// Feature.en.srt from also attaching to Feature.mkv when Feature.en.mkv exists.
type subtitleDirectoryIndex struct {
	info     os.FileInfo
	byStem   map[string][]subtitleCandidate
	overflow map[string]bool
}

func newSubtitleDirectoryIndex(names []string, info os.FileInfo) *subtitleDirectoryIndex {
	index := &subtitleDirectoryIndex{info: info, byStem: make(map[string][]subtitleCandidate), overflow: make(map[string]bool)}
	mediaStems := make(map[string]bool)
	for _, name := range names {
		if safeSubtitleFilename(name) && !ignoredName(name) && extensionKind(name) != "" {
			mediaStems[strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))] = true
		}
	}
	// Sorting makes the finite candidate subset deterministic across directory
	// enumeration order; overflow never authorizes deletion of previous rows.
	names = append([]string(nil), names...)
	sort.Strings(names)
	for _, name := range names {
		if !safeSubtitleFilename(name) || ignoredName(name) {
			continue
		}
		codec := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if subtitleMIME(codec) == "" {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		owner := strings.ToLower(stem)
		for !mediaStems[owner] {
			position := strings.LastIndexByte(owner, '.')
			if position < 0 {
				owner = ""
				break
			}
			owner = owner[:position]
		}
		if owner == "" {
			continue
		}
		track, ok := subtitleNameMetadata(name, strings.ToLower(stem)[len(owner):], codec)
		if !ok {
			continue
		}
		if len(index.byStem[owner]) >= maxActiveSubtitles {
			index.overflow[owner] = true
			continue
		}
		index.byStem[owner] = append(index.byStem[owner], subtitleCandidate{filename: name, info: track})
	}
	return index
}

func safeSubtitleFilename(name string) bool {
	return name != "" && name == filepath.Base(name) && !strings.ContainsAny(name, "/\\\x00")
}

func subtitleNameMetadata(filename, suffix, codec string) (Subtitle, bool) {
	track := Subtitle{Filename: filename, Codec: codec, MIMEType: subtitleMIME(codec)}
	if suffix == "" {
		return track, true
	}
	if !strings.HasPrefix(suffix, ".") {
		return Subtitle{}, false
	}
	parts := strings.Split(strings.TrimPrefix(suffix, "."), ".")
	if len(parts) > 4 {
		return Subtitle{}, false
	}
	seen := make(map[string]bool)
	for _, value := range parts {
		flag := strings.ToLower(value)
		if seen[flag] {
			return Subtitle{}, false
		}
		seen[flag] = true
		switch flag {
		case "default":
			track.IsDefault = true
		case "forced":
			track.IsForced = true
		case "sdh":
			track.IsHearingImpaired = true
		default:
			if track.Language != "" || !subtitleLanguagePattern.MatchString(value) {
				return Subtitle{}, false
			}
			track.Language = flag
		}
	}
	track.Title = track.Language
	return track, true
}

func (index *subtitleDirectoryIndex) candidates(relative string) ([]subtitleCandidate, bool) {
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative)))
	return index.byStem[stem], index.overflow[stem]
}
