package server

import (
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/library"
)

// Historical item-query keys and unimplemented Fields remain explicitly inert
// compatibility hints. Supported Filters/SortBy and these named selectors are
// validated; recognizing a key never silently accepts an invalid selector.
func readNavigationFilters(w http.ResponseWriter, r *http.Request, query *library.Query) bool {
	names := map[string]string{}
	for _, name := range []string{"ExcludeItemTypes", "Years", "MinPremiereDate", "MaxPremiereDate", "MinDateCreated", "MaxDateCreated", "MinCommunityRating",
		"NameStartsWith", "NameStartsWithOrGreater", "NameLessThan", "HasOverview", "HasSubtitles", "IsHD",
		"ArtistStartsWithOrGreater", "AlbumArtistStartsWithOrGreater", "IsMissing", "IsVirtualUnaired", "IsPlaceHolder", "IsUnaired"} {
		names[strings.ToLower(name)] = name
	}
	values := make(map[string]string)
	invalid := func() bool {
		apiError(w, r, http.StatusBadRequest, "invalid_navigation_filter", "Check the supported catalog filter values and ranges.")
		return false
	}
	rawValues, parseErr := url.ParseQuery(r.URL.RawQuery)
	if parseErr != nil {
		return invalid()
	}
	for name, entries := range rawValues {
		canonical := names[strings.ToLower(name)]
		if canonical == "" {
			continue
		}
		if _, duplicate := values[canonical]; duplicate || len(entries) != 1 || entries[0] == "" {
			return invalid()
		}
		values[canonical] = entries[0]
	}
	if raw := values["ExcludeItemTypes"]; raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) > 32 {
			return invalid()
		}
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				return invalid()
			}
		}
		query.ExcludeItemTypes = queryValues([]string{raw})
	}
	if raw := values["Years"]; raw != "" {
		parts := strings.Split(raw, ",")
		if len(parts) > 256 {
			return invalid()
		}
		for _, part := range parts {
			year, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || year < 1 || year > 9999 {
				return invalid()
			}
			query.Years = append(query.Years, year)
		}
	}
	for _, field := range []struct {
		name   string
		target **time.Time
	}{
		{"MinPremiereDate", &query.MinPremiereDate}, {"MaxPremiereDate", &query.MaxPremiereDate}, {"MinDateCreated", &query.MinDateCreated}, {"MaxDateCreated", &query.MaxDateCreated},
	} {
		if raw := values[field.name]; raw != "" {
			value, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil || value.Year() < 1 || value.Year() > 9999 {
				return invalid()
			}
			*field.target = &value
		}
	}
	for _, interval := range [][2]*time.Time{{query.MinPremiereDate, query.MaxPremiereDate}, {query.MinDateCreated, query.MaxDateCreated}} {
		if interval[0] != nil && interval[1] != nil && interval[0].After(*interval[1]) {
			return invalid()
		}
	}
	if raw := values["MinCommunityRating"]; raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 10 {
			return invalid()
		}
		query.MinCommunityRating = &value
	}
	for _, field := range []struct {
		name   string
		target *string
	}{{"NameStartsWith", &query.NameStartsWith}, {"NameStartsWithOrGreater", &query.NameStartsWithOrGreater}, {"NameLessThan", &query.NameLessThan},
		{"ArtistStartsWithOrGreater", &query.ArtistStartsWithOrGreater}, {"AlbumArtistStartsWithOrGreater", &query.AlbumArtistStartsWithOrGreater}} {
		*field.target = values[field.name]
	}
	for _, field := range []struct {
		name   string
		target **bool
	}{{"HasOverview", &query.HasOverview}, {"HasSubtitles", &query.HasSubtitles}, {"IsHD", &query.IsHD},
		{"IsMissing", &query.IsMissing}, {"IsVirtualUnaired", &query.IsVirtualUnaired}, {"IsPlaceHolder", &query.IsPlaceHolder}, {"IsUnaired", &query.IsUnaired}} {
		if raw := values[field.name]; raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return invalid()
			}
			*field.target = &value
		}
	}
	return true
}

func hasNavigationFilters(query library.Query) bool {
	return len(query.ExcludeItemTypes) > 0 || len(query.Years) > 0 || query.MinPremiereDate != nil || query.MaxPremiereDate != nil ||
		query.MinDateCreated != nil || query.MaxDateCreated != nil || query.MinCommunityRating != nil || query.NameStartsWith != "" ||
		query.NameStartsWithOrGreater != "" || query.NameLessThan != "" || query.HasOverview != nil || query.HasSubtitles != nil || query.IsHD != nil ||
		query.ArtistStartsWithOrGreater != "" || query.AlbumArtistStartsWithOrGreater != "" || query.IsMissing != nil || query.IsVirtualUnaired != nil ||
		query.IsPlaceHolder != nil || query.IsUnaired != nil
}
