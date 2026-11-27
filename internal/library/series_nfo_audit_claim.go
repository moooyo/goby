package library

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/moooyo/goby/internal/metadata"
	"golang.org/x/text/unicode/norm"
)

// SeriesNFOAuditWarning describes a disagreement, not a verdict on identity.
// The other item, when present, is a separately observed physical directory
// whose own name and year agree with the shared NFO declaration.
type SeriesNFOAuditWarning struct {
	ItemID              string `json:"ItemId"`
	Code                string `json:"Code"`
	CorroboratingItemID string `json:"CorroboratingItemId,omitempty"`
}

const (
	seriesNFOTitleYearDisagreement = "title_year_disagreement"
	seriesNFOSharedDeclaration     = "shared_declaration_disagreement"
)

var (
	seriesNFOAuditLeafPattern = regexp.MustCompile(`^(.+) \(([0-9]{4})\)$`)
	seriesNFOAuditIMDbPattern = regexp.MustCompile(`^tt[0-9]{7,16}$`)
)

type seriesNFOAuditClaim struct {
	itemID, directoryID, fileID, directoryTitle string
	directoryYear, year                         int
	titles                                      [2]string
	ids, declaredIDs                            [3]string // IMDb, TMDB, TVDB.
	conflictingIDs                              bool
}

func parseSeriesNFOAuditClaim(data []byte, itemID, directoryID, fileID, leaf string) (seriesNFOAuditClaim, bool) {
	var seen [3]string
	var conflicting bool
	value, err := metadata.ParseNFOWithProviderDeclarations(bytes.NewReader(data), func(key, value string) {
		provider, id := seriesNFOAuditProviderID(key, value)
		if id != "" {
			conflicting = conflicting || seen[provider] != "" && seen[provider] != id
			seen[provider] = id
		}
	})
	if err != nil || value.Kind != "tvshow" {
		return seriesNFOAuditClaim{}, false
	}
	claim := newSeriesNFOAuditClaim(itemID, directoryID, fileID, leaf, &value)
	claim.declaredIDs, claim.conflictingIDs = seen, conflicting
	return claim, true
}

func newSeriesNFOAuditClaim(itemID, directoryID, fileID, leaf string, value *metadata.Metadata) seriesNFOAuditClaim {
	claim := seriesNFOAuditClaim{itemID: itemID, directoryID: directoryID, fileID: fileID}
	if parts := seriesNFOAuditLeafPattern.FindStringSubmatch(seriesNFOAuditTitle(leaf)); len(parts) != 0 {
		claim.directoryTitle = parts[1]
		claim.directoryYear, _ = strconv.Atoi(parts[2])
		if seriesNFOAuditLeafPattern.MatchString(claim.directoryTitle) {
			claim.directoryTitle = ""
		}
		switch strings.ToLower(claim.directoryTitle) {
		case "tv", "tv shows", "tvshows", "television", "series", "shows", "unknown", "misc", "剧集", "电视剧", "未知":
			claim.directoryTitle = ""
		}
	}
	if value.ProductionYear != nil && (value.PremiereDate == nil || value.PremiereDate.Year() == *value.ProductionYear) {
		claim.year = *value.ProductionYear
	}
	claim.titles = [2]string{seriesNFOAuditTitle(value.Name), seriesNFOAuditTitle(value.OriginalTitle)}
	for _, provider := range [...]string{"Imdb", "Tmdb", "Tvdb"} {
		if index, id := seriesNFOAuditProviderID(provider, value.ProviderIDs[provider]); id != "" {
			claim.ids[index] = id
		}
	}
	return claim
}

func seriesNFOAuditProviderID(provider, id string) (int, string) {
	switch provider {
	case "Imdb":
		if seriesNFOAuditIMDbPattern.MatchString(id) && strings.Trim(id[2:], "0") != "" {
			return 0, id
		}
	case "Tmdb":
		return 1, seriesNFOAuditNumericID(id)
	case "Tvdb":
		return 2, seriesNFOAuditNumericID(id)
	}
	return 0, ""
}

func seriesNFOAuditTitle(value string) string {
	value = strings.Join(strings.Fields(norm.NFC.String(value)), " ")
	if len(value) > 1024 || strings.ContainsFunc(value, unicode.IsControl) {
		return ""
	}
	return value
}

func seriesNFOAuditNumericID(value string) string {
	if value == "" || len(value) > 19 || strings.ContainsFunc(value, func(r rune) bool { return r < '0' || r > '9' }) {
		return ""
	}
	if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
		return strconv.FormatInt(id, 10)
	}
	return ""
}

func (claim seriesNFOAuditClaim) comparable() bool {
	return claim.directoryID != "" && claim.directoryTitle != "" && claim.directoryYear >= 1900 && claim.directoryYear <= 2100 &&
		claim.year >= 1900 && claim.year <= 2100 && (claim.titles[0] != "" || claim.titles[1] != "")
}

func (claim seriesNFOAuditClaim) hasTitle(title string) bool {
	return title != "" && (strings.EqualFold(title, claim.titles[0]) || strings.EqualFold(title, claim.titles[1]))
}

func (claim seriesNFOAuditClaim) corroborates(other seriesNFOAuditClaim) bool {
	if claim.conflictingIDs || other.conflictingIDs || claim.directoryID == other.directoryID || claim.fileID == other.fileID ||
		claim.year != other.year || !other.hasTitle(other.directoryTitle) ||
		!(other.hasTitle(claim.titles[0]) || other.hasTitle(claim.titles[1])) {
		return false
	}
	shared := false
	for provider, id := range claim.ids {
		if a, b := claim.declaredIDs[provider], other.declaredIDs[provider]; a != "" && b != "" && a != b {
			return false
		}
		if id == "" || other.ids[provider] == "" {
			continue
		}
		if id != other.ids[provider] {
			return false
		}
		shared = true
	}
	return shared
}

// Both sides were proved fresh before calling this function. No number of
// agreeing ID fields makes a declaration independent of its owning document.
func seriesNFOAuditWarnings(claims []seriesNFOAuditClaim) []SeriesNFOAuditWarning {
	warnings := []SeriesNFOAuditWarning{}
	var corroborators []seriesNFOAuditClaim
	for _, claim := range claims {
		if claim.comparable() && claim.directoryYear == claim.year && claim.hasTitle(claim.directoryTitle) {
			corroborators = append(corroborators, claim)
		}
	}
	for _, claim := range claims {
		if !claim.comparable() || claim.year-claim.directoryYear >= -1 && claim.year-claim.directoryYear <= 1 || claim.hasTitle(claim.directoryTitle) {
			continue
		}
		warning := SeriesNFOAuditWarning{ItemID: claim.itemID, Code: seriesNFOTitleYearDisagreement}
		for _, other := range corroborators {
			if (warning.CorroboratingItemID == "" || other.itemID < warning.CorroboratingItemID) && claim.corroborates(other) {
				warning.Code = seriesNFOSharedDeclaration
				warning.CorroboratingItemID = other.itemID
			}
		}
		warnings = append(warnings, warning)
	}
	slices.SortFunc(warnings, func(a, b SeriesNFOAuditWarning) int { return strings.Compare(a.ItemID, b.ItemID) })
	return warnings
}
