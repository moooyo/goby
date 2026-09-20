package media

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"unicode/utf8"
)

const mediaEditMP4KindScheme = "urn:mpeg:dash:role:2011"

type mediaEditMP4KindProof struct {
	Scheme string `json:"scheme"`
	Value  string `json:"value"`
}

type mediaEditMP4UserDataProof struct {
	NamePresent bool                    `json:"name_present"`
	Name        string                  `json:"name"`
	Kinds       []mediaEditMP4KindProof `json:"kinds,omitempty"`
}

// These exact pairs are the complete track-kind mapping implemented by the
// pinned FFmpeg muxer/demuxer. FFmpeg's reader uses prefix matching, so the
// structural proof must reject unknown URI/value suffixes rather than trusting
// a matching disposition projection. Each combination retains both flags.
func mediaEditMP4KindDispositions(kind mediaEditMP4KindProof) ([]string, bool) {
	if kind.Scheme != mediaEditMP4KindScheme {
		return nil, false
	}
	values := map[string][]string{
		"caption":         {"hearing_impaired", "captions"},
		"commentary":      {"comment"},
		"description":     {"visual_impaired", "descriptions"},
		"dub":             {"dub"},
		"forced-subtitle": {"forced"},
	}
	dispositions, found := values[kind.Value]
	return dispositions, found
}

func mediaEditMP4ValidateUserDataProof(proof mediaEditMP4UserDataProof) error {
	if proof.NamePresent {
		if len(proof.Name) == 0 || len(proof.Name) > 4096 || !utf8.ValidString(proof.Name) || bytes.IndexByte([]byte(proof.Name), 0) >= 0 {
			return mediaEditContainerError("invalid MP4 trak/udta/name proof")
		}
	} else if proof.Name != "" {
		return mediaEditContainerError("MP4 trak/udta/name presence is ambiguous")
	}
	if len(proof.Kinds) > 5 {
		return mediaEditContainerError("excessive MP4 trak/udta/kind proof")
	}
	for index, kind := range proof.Kinds {
		if _, known := mediaEditMP4KindDispositions(kind); !known || index > 0 && proof.Kinds[index-1].Value >= kind.Value {
			return mediaEditContainerError("unknown, duplicate or unordered MP4 trak/udta/kind proof")
		}
	}
	return nil
}

// Track udta is a separate namespace from movie udta/meta. Only raw UTF-8 name
// and fully decoded standard kind boxes are admitted here. Unknown nested
// metadata cannot become silently accepted through the movie metadata rules.
func (s *mediaEditContainerScanner) mp4TrackUserData(box mediaEditMP4Box, depth int, track *mediaEditMP4Track) error {
	if track == nil || box.start < 0 || box.end <= box.start || box.end > s.size {
		return mediaEditContainerError("MP4 trak/udta has no valid extent or track")
	}
	seenKinds := map[string]bool{}
	for _, kind := range track.userData.Kinds {
		seenKinds[kind.Value] = true
	}
	for offset := box.start; offset < box.end; {
		child, err := s.mp4Box(offset, box.end, depth)
		if err != nil {
			return fmt.Errorf("MP4 trak/udta: %w", err)
		}
		offset = child.end
		if err := s.charge(child.end - child.start); err != nil {
			return err
		}
		switch child.kind {
		case "name":
			if track.userData.NamePresent {
				return mediaEditContainerError("duplicate MP4 trak/udta/name box")
			}
			data, err := s.mp4Bytes(child, 4096)
			if err != nil {
				return err
			}
			if len(data) == 0 || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
				return mediaEditContainerError("MP4 trak/udta/name must be one complete UTF-8 value")
			}
			track.userData.NamePresent, track.userData.Name = true, string(data)
		case "kind":
			data, err := s.mp4Bytes(child, 1024)
			if err != nil {
				return err
			}
			if len(data) < 6 || !mediaEditZero(data[:4]) {
				return mediaEditContainerError("invalid MP4 trak/udta/kind version or flags")
			}
			stringParts := bytes.Split(data[4:], []byte{0})
			if len(stringParts) != 3 || len(stringParts[0]) == 0 || len(stringParts[1]) == 0 || len(stringParts[2]) != 0 || !utf8.Valid(stringParts[0]) || !utf8.Valid(stringParts[1]) {
				return mediaEditContainerError("MP4 trak/udta/kind requires exactly two terminated strings")
			}
			kind := mediaEditMP4KindProof{Scheme: string(stringParts[0]), Value: string(stringParts[1])}
			if _, known := mediaEditMP4KindDispositions(kind); !known || seenKinds[kind.Value] || len(seenKinds) >= 5 {
				return mediaEditContainerError("unknown or duplicate MP4 trak/udta/kind value")
			}
			seenKinds[kind.Value] = true
			track.userData.Kinds = append(track.userData.Kinds, kind)
		default:
			return mediaEditContainerError(fmt.Sprintf("unproven MP4 trak/udta box %q", child.kind))
		}
	}
	sort.Slice(track.userData.Kinds, func(i, j int) bool { return track.userData.Kinds[i].Value < track.userData.Kinds[j].Value })
	return mediaEditMP4ValidateUserDataProof(track.userData)
}

// FFmpeg reads trak/udta/name as tags.name, but writes that atom from
// tags.title. Validate the entire projection before the remux command supplies
// that explicit alias. Raw userdata remains independently compared afterward.
func mediaEditValidateMP4UserDataProjection(proof mediaEditContainerProof, document mediaEditDocument) error {
	if len(proof.MP4Tracks) != len(document.Streams) {
		return mediaEditContainerError("MP4 user-data/probe track inventories differ")
	}
	byID := make(map[uint64]mediaEditMP4UserDataProof, len(proof.MP4Tracks))
	for _, track := range proof.MP4Tracks {
		if track.ID == 0 {
			return mediaEditContainerError("MP4 user data lacks its track identity")
		}
		if _, duplicate := byID[track.ID]; duplicate {
			return mediaEditContainerError("duplicate MP4 user-data track identity")
		}
		if err := mediaEditMP4ValidateUserDataProof(track.UserData); err != nil {
			return err
		}
		byID[track.ID] = track.UserData
	}
	for _, stream := range document.Streams {
		text, valid := stream["id"].(string)
		id, err := strconv.ParseUint(text, 0, 32)
		if !valid || err != nil || id == 0 {
			return mediaEditContainerError("MP4 user-data probe identity is invalid")
		}
		userData, found := byID[id]
		if !found {
			return mediaEditContainerError("MP4 user data does not match its probe track")
		}
		delete(byID, id)
		tags, err := mediaEditTags(stream["tags"])
		if err != nil {
			return err
		}
		name, namePresent := tags["name"]
		if namePresent != userData.NamePresent || name != userData.Name {
			return mediaEditContainerError("MP4 trak/udta/name differs from its complete probe projection")
		}
		if _, titlePresent := tags["title"]; titlePresent {
			return mediaEditContainerError("MP4 track title has an unproven metadata source")
		}
		expected := map[string]int64{"hearing_impaired": 0, "captions": 0, "comment": 0, "visual_impaired": 0, "descriptions": 0, "dub": 0, "forced": 0}
		for _, kind := range userData.Kinds {
			flags, _ := mediaEditMP4KindDispositions(kind)
			for _, flag := range flags {
				expected[flag] = 1
			}
		}
		disposition, valid := stream["disposition"].(map[string]any)
		if !valid {
			return mediaEditContainerError("MP4 trak/udta/kind has no disposition projection")
		}
		for flag, expectedValue := range expected {
			actual, err := mediaEditInteger(disposition[flag])
			if err != nil || actual != expectedValue {
				return mediaEditContainerError("MP4 trak/udta/kind differs from its complete disposition projection")
			}
		}
	}
	return nil
}
