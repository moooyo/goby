package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CurrentMusicMetadataVersion identifies the format-tag facts extracted from
// audio-only sources, independently of technical probe-cache compatibility.
const CurrentMusicMetadataVersion = 2

const (
	maxMusicMetadataNameBytes  = 1024
	maxMusicMetadataTotalBytes = 4 * maxMusicMetadataNameBytes
)

// ErrInvalidMusicMetadata never includes untrusted tag names or values.
var ErrInvalidMusicMetadata = errors.New("invalid embedded music metadata")

// MusicMetadata contains only explicitly observed format tags. Artist and
// AlbumArtist remain exact scalars; aliases, separators, composers, and numbering
// are not inferred. Version distinguishes refreshed facts from older caches.
type MusicMetadata struct {
	Version     int
	Title       string
	Album       string
	Artist      string
	AlbumArtist string
}

func isMusicMetadataSource(info Info) bool {
	hasAudio := false
	for _, stream := range info.Streams {
		switch {
		case strings.EqualFold(stream.CodecType, "audio"):
			hasAudio = true
		case strings.EqualFold(stream.CodecType, "video") && !stream.IsAttachedPicture:
			return false
		}
	}
	return hasAudio
}

func parseMusicMetadata(raw json.RawMessage) (MusicMetadata, error) {
	result := MusicMetadata{Version: CurrentMusicMetadataVersion}
	if len(raw) == 0 {
		return result, nil
	}
	if len(raw) > maxProbeOutput {
		return MusicMetadata{}, ErrInvalidMusicMetadata
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return MusicMetadata{}, ErrInvalidMusicMetadata
	}
	seen := make(map[string]string, 4)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return MusicMetadata{}, ErrInvalidMusicMetadata
		}
		name, ok := token.(string)
		if !ok {
			return MusicMetadata{}, ErrInvalidMusicMetadata
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return MusicMetadata{}, ErrInvalidMusicMetadata
		}
		name = musicMetadataTagName(name)
		if name == "" {
			continue
		}
		text, err := musicMetadataString(value)
		if err != nil {
			return MusicMetadata{}, ErrInvalidMusicMetadata
		}
		if previous, exists := seen[name]; exists && previous != text {
			return MusicMetadata{}, ErrInvalidMusicMetadata
		}
		seen[name] = text
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return MusicMetadata{}, ErrInvalidMusicMetadata
	}
	if _, err := decoder.Token(); err != io.EOF {
		return MusicMetadata{}, ErrInvalidMusicMetadata
	}
	result.Title, result.Album, result.Artist = seen["title"], seen["album"], seen["artist"]
	result.AlbumArtist = seen["album_artist"]
	if len(result.Title)+len(result.Album)+len(result.Artist)+len(result.AlbumArtist) > maxMusicMetadataTotalBytes {
		return MusicMetadata{}, ErrInvalidMusicMetadata
	}
	return result, nil
}

func musicMetadataTagName(name string) string {
	if len(name) < 5 || len(name) > len("album_artist") {
		return ""
	}
	var lowered [len("album_artist")]byte
	for index := range len(name) {
		character := name[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		if (character < 'a' || character > 'z') && character != '_' {
			return ""
		}
		lowered[index] = character
	}
	key := string(lowered[:len(name)])
	switch key {
	case "title", "album", "artist", "album_artist":
		return key
	default:
		return ""
	}
}

func musicMetadataString(raw json.RawMessage) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || !utf8.Valid(raw) || !musicMetadataUnicode(raw) {
		return "", ErrInvalidMusicMetadata
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || len(value) > maxMusicMetadataNameBytes ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", ErrInvalidMusicMetadata
	}
	return value, nil
}

// JSON decoding otherwise replaces unpaired UTF-16 escapes with U+FFFD. Keep
// actual replacement characters and valid surrogate pairs without lossy repair.
func musicMetadataUnicode(raw []byte) bool {
	hexUnit := func(value []byte) (uint16, bool) {
		if len(value) != 4 {
			return 0, false
		}
		var result uint16
		for _, digit := range value {
			result <<= 4
			switch {
			case digit >= '0' && digit <= '9':
				result += uint16(digit - '0')
			case digit >= 'a' && digit <= 'f':
				result += uint16(digit-'a') + 10
			case digit >= 'A' && digit <= 'F':
				result += uint16(digit-'A') + 10
			default:
				return 0, false
			}
		}
		return result, true
	}
	for index := 1; index < len(raw)-1; index++ {
		if raw[index] != '\\' {
			continue
		}
		index++
		if index >= len(raw)-1 {
			return false
		}
		if raw[index] != 'u' {
			continue
		}
		if index+4 >= len(raw)-1 {
			return false
		}
		unit, ok := hexUnit(raw[index+1 : index+5])
		if !ok || unit >= 0xdc00 && unit <= 0xdfff {
			return false
		}
		index += 4
		if unit < 0xd800 || unit > 0xdbff {
			continue
		}
		if index+6 >= len(raw)-1 || raw[index+1] != '\\' || raw[index+2] != 'u' {
			return false
		}
		low, ok := hexUnit(raw[index+3 : index+7])
		if !ok || low < 0xdc00 || low > 0xdfff {
			return false
		}
		index += 6
	}
	return true
}
