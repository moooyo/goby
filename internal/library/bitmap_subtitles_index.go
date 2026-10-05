package library

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type bitmapSubtitleCandidate struct {
	filename, companion, format string
	metadata                    Subtitle
}

type bitmapSubtitleSelection struct {
	candidates []bitmapSubtitleCandidate
	// Lowercase primary names preserve prior snapshots for missing pairs,
	// ambiguous names, and invalid existing entries until a complete deletion.
	present  map[string]bool
	overflow bool
}

type bitmapSubtitleDirectoryIndex struct {
	byStem map[string]*bitmapSubtitleSelection
}

func newBitmapSubtitleDirectoryIndex(entries []os.DirEntry, collectionType string) *bitmapSubtitleDirectoryIndex {
	index := &bitmapSubtitleDirectoryIndex{byStem: make(map[string]*bitmapSubtitleSelection)}
	owners := make(map[string]int)
	type group struct{ sup, idx, sub []string }
	groups := make(map[string]*group)
	for _, entry := range entries {
		name := entry.Name()
		if !safeSubtitleFilename(name) || ignoredName(name) {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if entry.Type().IsRegular() && scannedMediaKind(name, collectionType) == "video" {
			owners[stem]++
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".sup" && ext != ".idx" && ext != ".sub" {
			continue
		}
		value := groups[stem]
		if value == nil {
			value = &group{}
			groups[stem] = value
		}
		switch ext {
		case ".sup":
			value.sup = append(value.sup, name)
		case ".idx":
			value.idx = append(value.idx, name)
		case ".sub":
			value.sub = append(value.sub, name)
		}
	}
	stems := make([]string, 0, len(groups))
	for stem := range groups {
		stems = append(stems, stem)
	}
	sort.Strings(stems)
	for _, stem := range stems {
		owner := stem
		for owners[owner] == 0 {
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
		selected := index.byStem[owner]
		if selected == nil {
			selected = &bitmapSubtitleSelection{present: make(map[string]bool)}
			index.byStem[owner] = selected
		}
		value := groups[stem]
		if len(value.sup) != 0 {
			selected.present[stem+".sup"] = true
		}
		if len(value.idx)+len(value.sub) != 0 {
			selected.present[stem+".idx"] = true
		}
		// Ambiguous primary media ownership cannot authorize a new association
		// or retire an existing one from either candidate's unchanged listing.
		if owners[owner] != 1 {
			continue
		}
		appendCandidate := func(filename, companion, format string) {
			metadata, ok := subtitleNameMetadata(filename, stem[len(owner):], format)
			if !ok {
				return
			}
			if len(selected.candidates) >= maxActiveSubtitles {
				selected.overflow = true
				return
			}
			selected.candidates = append(selected.candidates, bitmapSubtitleCandidate{filename, companion, format, metadata})
		}
		if len(value.sup) == 1 {
			appendCandidate(value.sup[0], "", "sup")
		}
		if len(value.idx) == 1 && len(value.sub) == 1 {
			appendCandidate(value.idx[0], value.sub[0], "vobsub")
		}
	}
	return index
}

func (index *bitmapSubtitleDirectoryIndex) selection(relative string) bitmapSubtitleSelection {
	if index == nil {
		return bitmapSubtitleSelection{}
	}
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative)))
	if selected := index.byStem[stem]; selected != nil {
		return *selected
	}
	return bitmapSubtitleSelection{}
}
