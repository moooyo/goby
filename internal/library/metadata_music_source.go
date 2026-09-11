package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode"
)

const (
	// The persisted source shape is independent from the refreshable probe
	// facts. Existing version 1 sources remain valid after probe upgrades.
	musicSourceVersion    = 1
	musicSourceMaxBytes   = 1 << 20
	musicSourceMaxEntries = 1024
)

// musicMetadataSource records accepted embedded facts separately from local NFOs.
type musicMetadataSource struct {
	Version      int      `json:"Version"`
	Name         string   `json:"Name,omitempty"`
	Album        string   `json:"Album,omitempty"`
	Artists      []string `json:"Artists"`
	AlbumArtists []string `json:"AlbumArtists"`
}

// mergeAcceptedMusicSource retains sparse local fields while accepted music
// facts own the music-specific values. Source versions never enter metadata.
func mergeAcceptedMusicSource(localSource, musicSource []byte) ([]byte, error) {
	music, extracted, err := decodeAcceptedMusicSource(musicSource)
	if err != nil {
		return nil, err
	}
	if !extracted {
		return bytes.Clone(localSource), nil
	}
	local, err := metadataSourceObject(localSource)
	if err != nil {
		return nil, err
	}
	name, err := acceptedMusicObjectName(local, music, "")
	if err != nil {
		return nil, err
	}
	if name != "" {
		local["Name"], err = json.Marshal(name)
		if err != nil {
			return nil, fmt.Errorf("encode accepted music name: %w", err)
		}
	}
	if music.Album == "" {
		delete(local, "Album")
	} else {
		local["Album"], err = json.Marshal(music.Album)
		if err != nil {
			return nil, fmt.Errorf("encode accepted music album: %w", err)
		}
	}
	local["Artists"], err = json.Marshal(music.Artists)
	if err != nil {
		return nil, fmt.Errorf("encode accepted music artists: %w", err)
	}
	local["AlbumArtists"], err = json.Marshal(music.AlbumArtists)
	if err != nil {
		return nil, fmt.Errorf("encode accepted album artists: %w", err)
	}
	merged, err := json.Marshal(local)
	if err != nil {
		return nil, fmt.Errorf("encode accepted music metadata: %w", err)
	}
	return merged, nil
}

// acceptedMusicSourceHash distinguishes extracted empty facts from an unread
// source, without depending on JSON formatting or object key ordering.
func acceptedMusicSourceHash(musicSource []byte) (string, error) {
	music, extracted, err := decodeAcceptedMusicSource(musicSource)
	if err != nil || !extracted {
		return "", err
	}
	encoded, err := json.Marshal(music)
	if err != nil {
		return "", fmt.Errorf("encode accepted music source: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// acceptedMusicName preserves accepted text exactly. NFO names retain priority
// over embedded names, and missing names retain the caller's scanner fallback.
func acceptedMusicName(localSource, musicSource []byte, fallback string) (string, error) {
	music, _, err := decodeAcceptedMusicSource(musicSource)
	if err != nil {
		return "", err
	}
	local, err := metadataSourceObject(localSource)
	if err != nil {
		return "", err
	}
	return acceptedMusicObjectName(local, music, fallback)
}

func acceptedMusicObjectName(local map[string]json.RawMessage, music musicMetadataSource, fallback string) (string, error) {
	if raw, exists := local["Name"]; exists {
		name, err := metadataStringValue(raw, false, metadataValueMaxName, false)
		if err != nil {
			return "", fmt.Errorf("read accepted local name: %w", err)
		}
		if name != "" {
			return name, nil
		}
	}
	if music.Name != "" {
		return music.Name, nil
	}
	return fallback, nil
}

func decodeAcceptedMusicSource(raw []byte) (musicMetadataSource, bool, error) {
	if len(raw) > musicSourceMaxBytes {
		return musicMetadataSource{}, false, fmt.Errorf("accepted music source exceeds the byte limit")
	}
	object, err := metadataSourceObject(raw)
	if err != nil {
		return musicMetadataSource{}, false, fmt.Errorf("decode accepted music source: %w", err)
	}
	if len(object) == 0 {
		return musicMetadataSource{}, false, nil
	}
	for field := range object {
		switch field {
		case "Version", "Name", "Album", "Artists", "AlbumArtists":
		default:
			return musicMetadataSource{}, false, fmt.Errorf("accepted music source contains an unknown field")
		}
	}
	music := musicMetadataSource{Artists: []string{}, AlbumArtists: []string{}}
	if err := json.Unmarshal(object["Version"], &music.Version); err != nil || music.Version != musicSourceVersion {
		return musicMetadataSource{}, false, fmt.Errorf("accepted music source must have Version 1")
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{name: "Name", value: &music.Name},
		{name: "Album", value: &music.Album},
	} {
		if value, exists := object[field.name]; exists {
			*field.value, err = acceptedMusicText(value)
			if err != nil {
				return musicMetadataSource{}, false, fmt.Errorf("read accepted music %s: %w", field.name, err)
			}
		}
	}
	for _, field := range []struct {
		name   string
		values *[]string
	}{
		{name: "Artists", values: &music.Artists},
		{name: "AlbumArtists", values: &music.AlbumArtists},
	} {
		if value, exists := object[field.name]; exists {
			*field.values, err = acceptedMusicStrings(value)
			if err != nil {
				return musicMetadataSource{}, false, fmt.Errorf("read accepted music %s: %w", field.name, err)
			}
		}
	}
	return music, true, nil
}

func acceptedMusicStrings(raw []byte) ([]string, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil || len(entries) > musicSourceMaxEntries {
		return nil, fmt.Errorf("must be an array with at most %d accepted names", musicSourceMaxEntries)
	}
	names := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name, err := acceptedMusicText(entry)
		if err != nil {
			return nil, err
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names, nil
}

func acceptedMusicText(raw []byte) (string, error) {
	text, err := metadataStringValue(raw, false, metadataValueMaxName, false)
	if err != nil {
		return "", err
	}
	for _, character := range text {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("accepted music text must not contain control characters")
		}
	}
	return text, nil
}
