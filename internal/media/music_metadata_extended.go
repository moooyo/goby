package media

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func musicMetadataTagName(name string) string {
	if len(name) == 0 || len(name) > 64 {
		return ""
	}
	for _, value := range []byte(name) {
		if value >= 128 {
			return ""
		}
	}
	key := strings.ToLower(name)
	switch key {
	case "title", "album", "artist", "artists", "composer", "composers", "genre", "genres", "date", "year":
		return key
	case "album_artist", "albumartist", "album artist":
		return "album_artist"
	case "album_artists", "albumartists":
		return "album_artists"
	case "track", "tracknumber":
		return "track"
	case "disc", "discnumber":
		return "disc"
	case "musicbrainz_trackid", "musicbrainz_recordingid":
		return "musicbrainz_recordingid"
	case "musicbrainz_albumid", "musicbrainz_releasegroupid", "musicbrainz_artistid":
		return key
	default:
		return ""
	}
}

func completeMusicMetadata(result *MusicMetadata, tags map[string]string) error {
	var err error
	for _, entry := range []struct {
		key, scalar string
		target      *[]string
	}{
		{"artists", "", &result.Artists}, {"album_artists", "", &result.AlbumArtists},
		{"composers", "composer", &result.Composers}, {"genres", "genre", &result.Genres},
	} {
		if value := tags[entry.key]; strings.TrimSpace(value) != "" {
			*entry.target, err = musicMetadataCredits(value)
			if err != nil {
				return err
			}
		} else if value := tags[entry.scalar]; strings.TrimSpace(value) != "" {
			*entry.target = []string{value}
		}
	}
	result.TrackNumber, result.TrackTotal, err = musicMetadataNumber(tags["track"])
	if err != nil {
		return err
	}
	result.DiscNumber, result.DiscTotal, err = musicMetadataNumber(tags["disc"])
	if err != nil {
		return err
	}
	if value := strings.TrimSpace(tags["year"]); value != "" {
		year, err := strconv.Atoi(value)
		if err != nil || year < 1 || year > 9999 || len(value) != 4 {
			return ErrInvalidMusicMetadata
		}
		result.Year = year
	}
	if value := strings.TrimSpace(tags["date"]); value != "" {
		layout := "2006-01-02"
		if len(value) == 4 {
			layout = "2006"
		}
		if len(value) == 7 {
			layout = "2006-01"
		}
		date, err := time.Parse(layout, value)
		if err != nil || date.Year() < 1 || result.Year != 0 && result.Year != date.Year() {
			return ErrInvalidMusicMetadata
		}
		result.Year, result.Date = date.Year(), value
	}
	for _, entry := range []struct{ tag, field string }{
		{"musicbrainz_recordingid", "MusicBrainzRecording"}, {"musicbrainz_albumid", "MusicBrainzRelease"},
		{"musicbrainz_releasegroupid", "MusicBrainzReleaseGroup"}, {"musicbrainz_artistid", "MusicBrainzArtist"},
	} {
		value := strings.TrimSpace(tags[entry.tag])
		if value == "" {
			continue
		}
		if !musicMetadataProviderID(value) {
			return ErrInvalidMusicMetadata
		}
		if result.ProviderIDs == nil {
			result.ProviderIDs = make(map[string]string)
		}
		result.ProviderIDs[entry.field] = strings.ToLower(value)
	}
	return ValidateMusicMetadata(*result)
}

// Explicit plural tags accept an actual JSON string array or semicolon-separated
// credits. Scalar ARTIST/ALBUM_ARTIST/COMPOSER values are never split. Empty plural
// tags add no credits; an administrator's explicit empty array can clear metadata.
func musicMetadataCredits(value string) ([]string, error) {
	var entries []string
	if strings.HasPrefix(strings.TrimSpace(value), "[") {
		var raw []json.RawMessage
		if json.Unmarshal([]byte(value), &raw) != nil || raw == nil || len(raw) > 64 {
			return nil, ErrInvalidMusicMetadata
		}
		for _, entry := range raw {
			text, err := musicMetadataString(entry)
			if err != nil {
				return nil, err
			}
			entries = append(entries, text)
		}
	} else {
		entries = strings.Split(value, ";")
	}
	if len(entries) > 64 {
		return nil, ErrInvalidMusicMetadata
	}
	result := make([]string, 0, len(entries))
	seen := make(map[string]bool)
	for _, value := range entries {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, ErrInvalidMusicMetadata
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, nil
}

func musicMetadataNumber(value string) (int, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, nil
	}
	parts := strings.Split(value, "/")
	if len(parts) > 2 {
		return 0, 0, ErrInvalidMusicMetadata
	}
	values := [2]int{}
	for index, part := range parts {
		if part == "" {
			return 0, 0, ErrInvalidMusicMetadata
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, 0, ErrInvalidMusicMetadata
			}
		}
		number, err := strconv.ParseInt(part, 10, 32)
		if err != nil || number <= 0 {
			return 0, 0, ErrInvalidMusicMetadata
		}
		values[index] = int(number)
	}
	if values[1] != 0 && values[1] < values[0] {
		return 0, 0, ErrInvalidMusicMetadata
	}
	return values[0], values[1], nil
}

func musicMetadataProviderID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

// ValidateMusicMetadata validates current facts without relabeling older cached
// versions. Technical media cache compatibility has an independent version.
func ValidateMusicMetadata(value MusicMetadata) error {
	if value.Version != CurrentMusicMetadataVersion {
		return ErrInvalidMusicMetadata
	}
	texts := []string{value.Title, value.Album, value.Artist, value.AlbumArtist, value.Date}
	for _, values := range [][]string{value.Artists, value.AlbumArtists, value.Composers, value.Genres} {
		if len(values) > 64 {
			return ErrInvalidMusicMetadata
		}
		for _, name := range values {
			if strings.TrimSpace(name) == "" {
				return ErrInvalidMusicMetadata
			}
		}
		texts = append(texts, values...)
	}
	total := 0
	for _, text := range texts {
		if !utf8.ValidString(text) || len(text) > maxMusicMetadataNameBytes || strings.IndexFunc(text, unicode.IsControl) >= 0 {
			return ErrInvalidMusicMetadata
		}
		total += len(text)
	}
	if total > maxMusicMetadataTotalBytes || value.Year < 0 || value.Year > 9999 {
		return ErrInvalidMusicMetadata
	}
	for _, number := range []int{value.TrackNumber, value.TrackTotal, value.DiscNumber, value.DiscTotal} {
		if number < 0 || int64(number) > math.MaxInt32 {
			return ErrInvalidMusicMetadata
		}
	}
	if value.TrackTotal != 0 && (value.TrackNumber == 0 || value.TrackTotal < value.TrackNumber) ||
		value.DiscTotal != 0 && (value.DiscNumber == 0 || value.DiscTotal < value.DiscNumber) {
		return ErrInvalidMusicMetadata
	}
	if value.Date != "" {
		layout := "2006-01-02"
		if len(value.Date) == 4 {
			layout = "2006"
		}
		if len(value.Date) == 7 {
			layout = "2006-01"
		}
		date, err := time.Parse(layout, value.Date)
		if err != nil || date.Year() < 1 || date.Year() != value.Year {
			return ErrInvalidMusicMetadata
		}
	}
	if len(value.ProviderIDs) > 4 {
		return ErrInvalidMusicMetadata
	}
	for key, id := range value.ProviderIDs {
		switch key {
		case "MusicBrainzRecording", "MusicBrainzRelease", "MusicBrainzReleaseGroup", "MusicBrainzArtist":
		default:
			return ErrInvalidMusicMetadata
		}
		if !musicMetadataProviderID(id) {
			return ErrInvalidMusicMetadata
		}
	}
	return nil
}
