package library

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/metadata"
)

func normalizeMusicCreditValues(raw json.RawMessage) ([]string, error) {
	values, err := metadataStringValues(raw)
	if err != nil {
		return nil, err
	}
	for _, name := range values {
		if len(name) > metadataValueMaxName || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("Music credits must be bounded names without control characters.")
		}
	}
	return values, nil
}

func probeMusicCredits(plural []string, scalar string) []string {
	if len(plural) > 0 {
		return append([]string(nil), plural...)
	}
	if strings.TrimSpace(scalar) != "" {
		return []string{scalar}
	}
	return []string{}
}

func extendedMusicSource(facts media.MusicMetadata, source *musicMetadataSource) {
	source.Composers = append([]string(nil), facts.Composers...)
	source.Genres = append([]string(nil), facts.Genres...)
	if facts.TrackNumber > 0 {
		value := facts.TrackNumber
		source.IndexNumber = &value
	}
	if facts.DiscNumber > 0 {
		value := facts.DiscNumber
		source.ParentIndexNumber = &value
	}
	if facts.Year > 0 {
		value := facts.Year
		source.ProductionYear = &value
	}
	if len(facts.Date) == len("2006-01-02") {
		value, err := time.Parse("2006-01-02", facts.Date)
		if err == nil {
			source.PremiereDate = &value
		}
	}
	if len(facts.ProviderIDs) > 0 {
		source.ProviderIDs = make(map[string]string, len(facts.ProviderIDs))
		for key, value := range facts.ProviderIDs {
			source.ProviderIDs[key] = value
		}
	}
}

func decodeExtendedMusicFields(object map[string]json.RawMessage, music *musicMetadataSource) error {
	for _, entry := range []struct {
		name   string
		target any
	}{
		{"ProductionYear", &music.ProductionYear}, {"PremiereDate", &music.PremiereDate},
		{"IndexNumber", &music.IndexNumber}, {"ParentIndexNumber", &music.ParentIndexNumber},
		{"ProviderIDs", &music.ProviderIDs},
	} {
		raw, exists := object[entry.name]
		if !exists {
			continue
		}
		field := entry.name
		if field == "ProviderIDs" {
			field = "ProviderIds"
		}
		canonical, err := normalizeMetadataValue(field, raw)
		if err != nil || json.Unmarshal(canonical, entry.target) != nil {
			return fmt.Errorf("invalid accepted music %s", entry.name)
		}
	}
	for _, credits := range [][]string{music.Composers, music.Genres} {
		for _, name := range credits {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("accepted music credit must be nonempty")
			}
		}
	}
	return nil
}

// Newly admitted descriptive fields fill absent NFO fields. Existing artist
// precedence is unchanged; user overrides and retained locks are applied later.
func mergeExtendedMusicFields(local map[string]json.RawMessage, source musicMetadataSource) error {
	values := make(map[string]any)
	if len(source.Composers) > 0 {
		people := make([]metadata.Person, 0, len(source.Composers))
		for index, name := range source.Composers {
			order := index
			people = append(people, metadata.Person{Name: name, Type: "Composer", SortOrder: &order})
		}
		values["People"] = people
	}
	if len(source.Genres) > 0 {
		values["Genres"] = source.Genres
	}
	if source.ProductionYear != nil {
		values["ProductionYear"] = source.ProductionYear
	}
	if source.PremiereDate != nil {
		values["PremiereDate"] = source.PremiereDate
	}
	if source.IndexNumber != nil {
		values["IndexNumber"] = source.IndexNumber
	}
	if source.ParentIndexNumber != nil {
		values["ParentIndexNumber"] = source.ParentIndexNumber
	}
	for key, value := range values {
		if _, exists := local[key]; exists {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode accepted music field: %w", err)
		}
		local[key] = encoded
	}
	if len(source.ProviderIDs) > 0 {
		providers := make(map[string]string, len(source.ProviderIDs))
		for key, value := range source.ProviderIDs {
			providers[key] = value
		}
		if raw, exists := local["ProviderIDs"]; exists {
			var original map[string]string
			if json.Unmarshal(raw, &original) != nil {
				return fmt.Errorf("invalid local music provider IDs")
			}
			known := make(map[string]string)
			for key, value := range original {
				canonical := key
				switch strings.ToLower(key) {
				case "musicbrainzrecording":
					canonical = "MusicBrainzRecording"
				case "musicbrainzrelease":
					canonical = "MusicBrainzRelease"
				case "musicbrainzreleasegroup":
					canonical = "MusicBrainzReleaseGroup"
				case "musicbrainzartist":
					canonical = "MusicBrainzArtist"
				}
				if previous, exists := known[canonical]; exists && previous != value {
					return fmt.Errorf("conflicting local MusicBrainz provider IDs")
				}
				known[canonical], providers[canonical] = value, value
			}
		}
		encoded, err := json.Marshal(providers)
		if err != nil {
			return fmt.Errorf("encode accepted music provider IDs: %w", err)
		}
		local["ProviderIDs"] = encoded
	}
	return nil
}

type albumMusicMetadata struct {
	seen                      bool
	year                      int
	date                      string
	providers                 map[string]string
	genres, composers         []string
	genreNames, composerNames map[string]bool
}

func (album *albumMusicMetadata) add(facts media.MusicMetadata) bool {
	if !album.seen {
		album.seen, album.year, album.date = true, facts.Year, facts.Date
		album.providers = make(map[string]string)
		for _, key := range []string{"MusicBrainzRelease", "MusicBrainzReleaseGroup"} {
			if id := facts.ProviderIDs[key]; id != "" {
				album.providers[key] = id
			}
		}
		album.genreNames, album.composerNames = make(map[string]bool), make(map[string]bool)
	} else {
		if album.year != facts.Year {
			album.year = 0
		}
		if album.date != facts.Date {
			album.date = ""
		}
		for key, value := range album.providers {
			if facts.ProviderIDs[key] != value {
				delete(album.providers, key)
			}
		}
	}
	for _, entry := range []struct {
		values []string
		target *[]string
		seen   map[string]bool
	}{
		{facts.Genres, &album.genres, album.genreNames}, {facts.Composers, &album.composers, album.composerNames},
	} {
		for _, name := range entry.values {
			if entry.seen[name] {
				continue
			}
			if len(*entry.target) >= musicSourceMaxEntries {
				return false
			}
			*entry.target = append(*entry.target, name)
			entry.seen[name] = true
		}
	}
	return true
}

func (album *albumMusicMetadata) apply(source *musicMetadataSource) {
	source.Genres, source.Composers, source.ProviderIDs = album.genres, album.composers, album.providers
	if album.year > 0 {
		year := album.year
		source.ProductionYear = &year
	}
	if len(album.date) == len("2006-01-02") {
		value, err := time.Parse("2006-01-02", album.date)
		if err == nil {
			source.PremiereDate = &value
		}
	}
}
