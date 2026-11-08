package providers

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxTVEpisodeGroupEntries = 2000

// TVEpisodeGroup identifies one provider-declared ordering. Multiple groups of
// the same type remain separate choices; their names never select a group.
type TVEpisodeGroup struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	DisplayOrder string `json:"DisplayOrder"`
	EpisodeCount int    `json:"EpisodeCount"`
	GroupCount   int    `json:"GroupCount"`
}

// TVEpisodeGroupEntry keeps the original aired pair and stable provider ID.
// GroupOrder and EpisodeOrder are display ranks, not filesystem numbering.
type TVEpisodeGroupEntry struct {
	ProviderEpisodeID string `json:"ProviderEpisodeId"`
	SeasonNumber      int    `json:"SeasonNumber"`
	EpisodeNumber     int    `json:"EpisodeNumber"`
	GroupOrder        int    `json:"GroupOrder"`
	EpisodeOrder      int    `json:"EpisodeOrder"`
	Ordinal           int    `json:"Ordinal"`
	GroupName         string `json:"GroupName"`
}

type TVEpisodeGroupDetail struct {
	TVEpisodeGroup
	SeriesID string                `json:"SeriesId"`
	Entries  []TVEpisodeGroupEntry `json:"Entries"`
}

func tvEpisodeGroupText(value string, maximum int, empty bool) bool {
	return (empty || value != "") && len(value) <= maximum && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

func tvEpisodeGroupID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func tvEpisodeGroupOrder(groupType int) string {
	switch groupType {
	case 2:
		return "Absolute"
	case 3:
		return "Dvd"
	default:
		return ""
	}
}

// TVEpisodeGroups lists supported orderings for this exact series. It uses the
// same fixed-host transport and credential boundary as other TMDB requests.
func (client *Client) TVEpisodeGroups(ctx context.Context, seriesID string) ([]TVEpisodeGroup, error) {
	if !client.config.Enabled {
		return nil, ErrDisabled
	}
	number, err := tmdbNumber(seriesID, false, math.MaxInt64)
	if err != nil || strconv.FormatInt(number, 10) != seriesID {
		return nil, ErrInvalidInput
	}
	headers, err := client.tmdbHeaders()
	if err != nil {
		return nil, err
	}
	var payload struct {
		ID      int64 `json:"id"`
		Results []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Type         int    `json:"type"`
			EpisodeCount int    `json:"episode_count"`
			GroupCount   int    `json:"group_count"`
		} `json:"results"`
	}
	if err := client.requestJSON(ctx, http.MethodGet, tmdbAPIBase+"/tv/"+seriesID+"/episode_groups", headers, nil, &payload); err != nil {
		return nil, err
	}
	if payload.ID != number || payload.Results == nil || len(payload.Results) > 200 {
		return nil, ErrUnavailable
	}
	result := make([]TVEpisodeGroup, 0, len(payload.Results))
	seen := make(map[string]bool)
	for _, group := range payload.Results {
		order := tvEpisodeGroupOrder(group.Type)
		if order == "" {
			continue
		}
		if !tvEpisodeGroupID(group.ID) || seen[group.ID] || !tvEpisodeGroupText(group.Name, 512, false) ||
			group.EpisodeCount < 1 || group.EpisodeCount > MaxTVEpisodeGroupEntries || group.GroupCount < 1 || group.GroupCount > MaxTVEpisodeGroupEntries {
			return nil, ErrUnavailable
		}
		seen[group.ID] = true
		result = append(result, TVEpisodeGroup{ID: group.ID, Name: group.Name, DisplayOrder: order, EpisodeCount: group.EpisodeCount, GroupCount: group.GroupCount})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].DisplayOrder != result[right].DisplayOrder {
			return result[left].DisplayOrder < result[right].DisplayOrder
		}
		if result[left].Name != result[right].Name {
			return result[left].Name < result[right].Name
		}
		return result[left].ID < result[right].ID
	})
	return result, nil
}

// TVEpisodeGroup first verifies membership in the series' group list. A client
// cannot import a valid but unrelated show's group by supplying its identifier.
func (client *Client) TVEpisodeGroup(ctx context.Context, seriesID, groupID string) (TVEpisodeGroupDetail, error) {
	if !tvEpisodeGroupID(groupID) {
		return TVEpisodeGroupDetail{}, ErrInvalidInput
	}
	groups, err := client.TVEpisodeGroups(ctx, seriesID)
	if err != nil {
		return TVEpisodeGroupDetail{}, err
	}
	var selected *TVEpisodeGroup
	for index := range groups {
		if groups[index].ID == groupID {
			selected = &groups[index]
			break
		}
	}
	if selected == nil {
		return TVEpisodeGroupDetail{}, ErrNotFound
	}
	headers, err := client.tmdbHeaders()
	if err != nil {
		return TVEpisodeGroupDetail{}, err
	}
	var payload struct {
		ID           string `json:"id"`
		Type         int    `json:"type"`
		EpisodeCount int    `json:"episode_count"`
		GroupCount   int    `json:"group_count"`
		Groups       []struct {
			Name     string `json:"name"`
			Order    *int   `json:"order"`
			Episodes []struct {
				ID            int64 `json:"id"`
				ShowID        int64 `json:"show_id"`
				SeasonNumber  *int  `json:"season_number"`
				EpisodeNumber *int  `json:"episode_number"`
				Order         *int  `json:"order"`
			} `json:"episodes"`
		} `json:"groups"`
	}
	if err := client.requestJSON(ctx, http.MethodGet, tmdbAPIBase+"/tv/episode_group/"+url.PathEscape(groupID), headers, nil, &payload); err != nil {
		return TVEpisodeGroupDetail{}, err
	}
	if payload.ID != groupID || tvEpisodeGroupOrder(payload.Type) != selected.DisplayOrder || payload.EpisodeCount != selected.EpisodeCount ||
		payload.GroupCount != selected.GroupCount || len(payload.Groups) != selected.GroupCount {
		return TVEpisodeGroupDetail{}, ErrUnavailable
	}
	seriesNumber, _ := strconv.ParseInt(seriesID, 10, 64)
	detail := TVEpisodeGroupDetail{TVEpisodeGroup: *selected, SeriesID: seriesID, Entries: make([]TVEpisodeGroupEntry, 0, selected.EpisodeCount)}
	pairs, identities, ranks := map[[2]int]bool{}, map[int64]bool{}, map[int]bool{}
	for _, group := range payload.Groups {
		if group.Order == nil || *group.Order < 0 || *group.Order > MaxTVEpisodeGroupEntries || ranks[*group.Order] ||
			!tvEpisodeGroupText(group.Name, 512, true) || group.Episodes == nil || len(detail.Entries)+len(group.Episodes) > MaxTVEpisodeGroupEntries {
			return TVEpisodeGroupDetail{}, ErrUnavailable
		}
		ranks[*group.Order] = true
		episodeRanks := map[int]bool{}
		for _, episode := range group.Episodes {
			if episode.ID <= 0 || episode.ShowID != 0 && episode.ShowID != seriesNumber || episode.SeasonNumber == nil || episode.EpisodeNumber == nil ||
				*episode.SeasonNumber < 0 || *episode.SeasonNumber > 9999 || *episode.EpisodeNumber < 0 || *episode.EpisodeNumber > 9999 ||
				episode.Order == nil || *episode.Order < 0 || *episode.Order > MaxTVEpisodeGroupEntries {
				return TVEpisodeGroupDetail{}, ErrUnavailable
			}
			pair := [2]int{*episode.SeasonNumber, *episode.EpisodeNumber}
			if pairs[pair] || identities[episode.ID] || episodeRanks[*episode.Order] {
				return TVEpisodeGroupDetail{}, ErrUnavailable
			}
			pairs[pair], identities[episode.ID], episodeRanks[*episode.Order] = true, true, true
			detail.Entries = append(detail.Entries, TVEpisodeGroupEntry{ProviderEpisodeID: strconv.FormatInt(episode.ID, 10),
				SeasonNumber: pair[0], EpisodeNumber: pair[1], GroupOrder: *group.Order, EpisodeOrder: *episode.Order, GroupName: group.Name})
		}
	}
	if len(detail.Entries) != selected.EpisodeCount {
		return TVEpisodeGroupDetail{}, ErrUnavailable
	}
	sort.Slice(detail.Entries, func(left, right int) bool {
		if detail.Entries[left].GroupOrder != detail.Entries[right].GroupOrder {
			return detail.Entries[left].GroupOrder < detail.Entries[right].GroupOrder
		}
		return detail.Entries[left].EpisodeOrder < detail.Entries[right].EpisodeOrder
	})
	for index := range detail.Entries {
		detail.Entries[index].Ordinal = index
	}
	return detail, nil
}
