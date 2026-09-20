package library

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	MaxEpisodeRosterEntries    = 2000
	MaxEpisodeRosterBytes      = 512 << 10
	EpisodeRosterParserVersion = 1
)

var ErrEpisodeRosterSourceRevision = errors.New("episode roster source revision identifies different content")

// EpisodeRosterSource identifies a declared local source, never a provider or
// filesystem location. Revision belongs to that source; the outer roster
// revision is the independent compare-and-swap token.
type EpisodeRosterSource struct {
	Kind          string `json:"Kind"`
	Key           string `json:"Key"`
	Label         string `json:"Label"`
	Revision      string `json:"Revision"`
	ParserVersion int    `json:"ParserVersion"`
	SHA256        string `json:"SHA256"`
}

type EpisodeRosterSourceInput struct {
	Key      string `json:"Key"`
	Label    string `json:"Label"`
	Revision string `json:"Revision"`
}

// A missing PremiereDate is unknown. Numbered gaps and physical file absence
// never supply a date or evidence that an episode exists.
type EpisodeRosterEntryInput struct {
	Key           string `json:"Key"`
	SeasonNumber  int    `json:"SeasonNumber"`
	EpisodeNumber int    `json:"EpisodeNumber"`
	Name          string `json:"Name"`
	PremiereDate  string `json:"PremiereDate,omitempty"`
}

type EpisodeRosterEdit struct {
	Revision string                    `json:"Revision"`
	Source   EpisodeRosterSourceInput  `json:"Source"`
	Entries  []EpisodeRosterEntryInput `json:"Entries"`
}

type EpisodeRosterEntry struct {
	EpisodeRosterEntryInput
	ID                 string   `json:"Id"`
	AvailableItemIDs   []string `json:"AvailableItemIds"`
	AvailableItemCount int      `json:"AvailableItemCount"`
	Availability       string   `json:"Availability"`
	Airing             string   `json:"Airing"`
}

type EpisodeRosterDetail struct {
	SeriesID     string               `json:"SeriesId"`
	SeriesName   string               `json:"SeriesName"`
	Revision     string               `json:"Revision"`
	State        string               `json:"State"`
	Source       *EpisodeRosterSource `json:"Source"`
	Entries      []EpisodeRosterEntry `json:"Entries"`
	RetiredCount int                  `json:"RetiredCount"`
	LastEditedBy string               `json:"LastEditedBy"`
	LastEditedAt *time.Time           `json:"LastEditedAt"`
}

// ExpectedEpisodeInfo marks a virtual discovery result. It carries no source
// payload, path, playback capability, or writable user-state identity.
type ExpectedEpisodeInfo struct {
	PremiereDateKnown bool `json:"PremiereDateKnown"`
	IsUnaired         bool `json:"IsUnaired"`
}

func episodeRosterText(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func episodeRosterRevision(value string) (int64, error) {
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != value {
		return 0, ErrInvalidInput
	}
	return revision, nil
}

func expectedEpisodeID(seriesID, sourceKey, entryKey string) string {
	// JSON length boundaries avoid ambiguous concatenations of arbitrary UTF-8
	// keys. The versioned namespace makes this independent from physical IDs.
	encoded, _ := json.Marshal([]string{"goby-expected-episode-v1", seriesID, sourceKey, entryKey})
	digest := sha256.Sum256(encoded)
	return "missing-" + hex.EncodeToString(digest[:16])
}

// ExpectedEpisodeID reproduces the stable identity when checking an archive.
// It does not establish that the supplied source or episode fact is valid.
func ExpectedEpisodeID(seriesID, sourceKey, entryKey string) string {
	return expectedEpisodeID(seriesID, sourceKey, entryKey)
}

// ParseEpisodeRosterPayload accepts only the exact normalized representation
// retained by a successful import. It is shared with archive integrity checks;
// request JSON uses a separate strict field parser before normalization.
func ParseEpisodeRosterPayload(raw []byte) (EpisodeRosterEdit, string, error) {
	if len(raw) == 0 || len(raw) > MaxEpisodeRosterBytes || !utf8.Valid(raw) {
		return EpisodeRosterEdit{}, "", ErrInvalidInput
	}
	var payload struct {
		ParserVersion int                       `json:"ParserVersion"`
		Source        EpisodeRosterSourceInput  `json:"Source"`
		Entries       []EpisodeRosterEntryInput `json:"Entries"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.ParserVersion != EpisodeRosterParserVersion {
		return EpisodeRosterEdit{}, "", ErrInvalidInput
	}
	edit, canonical, digest, err := normalizeEpisodeRosterEdit(EpisodeRosterEdit{Revision: "0", Source: payload.Source, Entries: payload.Entries})
	if err != nil || !bytes.Equal(raw, canonical) {
		return EpisodeRosterEdit{}, "", ErrInvalidInput
	}
	return edit, digest, nil
}

func IsExpectedEpisodeID(id string) bool {
	if len(id) != 40 || !strings.HasPrefix(id, "missing-") {
		return false
	}
	decoded, err := hex.DecodeString(id[8:])
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == id[8:]
}

func normalizeEpisodeRosterEdit(edit EpisodeRosterEdit) (EpisodeRosterEdit, []byte, string, error) {
	if _, err := episodeRosterRevision(edit.Revision); err != nil ||
		!episodeRosterText(edit.Source.Key, 128, false) || !episodeRosterText(edit.Source.Label, 256, true) ||
		!episodeRosterText(edit.Source.Revision, 128, false) || edit.Entries == nil || len(edit.Entries) > MaxEpisodeRosterEntries {
		return EpisodeRosterEdit{}, nil, "", ErrInvalidInput
	}
	edit.Entries = append([]EpisodeRosterEntryInput{}, edit.Entries...)
	keys, numbers := make(map[string]bool, len(edit.Entries)), make(map[[2]int]bool, len(edit.Entries))
	for _, entry := range edit.Entries {
		pair := [2]int{entry.SeasonNumber, entry.EpisodeNumber}
		if !episodeRosterText(entry.Key, 128, false) || !episodeRosterText(entry.Name, 512, true) ||
			entry.SeasonNumber < 0 || entry.SeasonNumber > 9999 || entry.EpisodeNumber < 0 || entry.EpisodeNumber > 9999 || keys[entry.Key] || numbers[pair] {
			return EpisodeRosterEdit{}, nil, "", ErrInvalidInput
		}
		if entry.PremiereDate != "" {
			date, err := time.Parse(time.DateOnly, entry.PremiereDate)
			if err != nil || date.Year() < 1 || date.Format(time.DateOnly) != entry.PremiereDate {
				return EpisodeRosterEdit{}, nil, "", ErrInvalidInput
			}
		}
		keys[entry.Key], numbers[pair] = true, true
	}
	sort.Slice(edit.Entries, func(i, j int) bool {
		left, right := edit.Entries[i], edit.Entries[j]
		if left.SeasonNumber != right.SeasonNumber {
			return left.SeasonNumber < right.SeasonNumber
		}
		if left.EpisodeNumber != right.EpisodeNumber {
			return left.EpisodeNumber < right.EpisodeNumber
		}
		return left.Key < right.Key
	})
	payload, err := json.Marshal(struct {
		ParserVersion int                       `json:"ParserVersion"`
		Source        EpisodeRosterSourceInput  `json:"Source"`
		Entries       []EpisodeRosterEntryInput `json:"Entries"`
	}{EpisodeRosterParserVersion, edit.Source, edit.Entries})
	if err != nil || len(payload) > MaxEpisodeRosterBytes {
		return EpisodeRosterEdit{}, nil, "", ErrInvalidInput
	}
	digest := sha256.Sum256(payload)
	return edit, payload, hex.EncodeToString(digest[:]), nil
}
