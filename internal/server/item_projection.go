package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxItemProjectionBytes  = 8192
	maxItemProjectionFields = 128
)

// normalizeItemProjectionQuery accepts the casing used by ordinary clients
// without changing identifiers, credentials or unrelated business parameters.
// It runs after authentication and before catalog access. Case aliases must
// not compete; repeated lists with the same spelling retain their CSV meaning.
func normalizeItemProjectionQuery(w http.ResponseWriter, r *http.Request) bool {
	values, err := url.ParseQuery(r.URL.RawQuery)
	invalid := func() bool {
		apiError(w, r, http.StatusBadRequest, "invalid_item_projection", "Supply bounded field lists and valid projection or client language options.")
		return false
	}
	if err != nil || !validEmbyLanguageQuery(values) {
		return invalid()
	}
	names := map[string]string{
		"fields": "Fields", "excludefields": "ExcludeFields",
		"enableimages": "EnableImages", "enableuserdata": "EnableUserData",
		"imagetypelimit": "ImageTypeLimit", "enableimagetypes": "EnableImageTypes",
	}
	seen := make(map[string]bool, len(names))
	canonical := make(url.Values, len(names))
	bytes := 0
	for name, entries := range values {
		key, selected := names[strings.ToLower(name)]
		if !selected {
			continue
		}
		if seen[key] || len(entries) == 0 || len(entries) > maxItemProjectionFields {
			return invalid()
		}
		seen[key] = true
		for _, value := range entries {
			bytes += len(value)
			if bytes > maxItemProjectionBytes || !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return invalid()
			}
		}
		switch key {
		case "Fields", "ExcludeFields", "EnableImageTypes":
			fields := queryValues(entries)
			if len(fields) > maxItemProjectionFields {
				return invalid()
			}
			for _, field := range fields {
				if !validItemProjectionField(field) {
					return invalid()
				}
			}
			canonical[key] = []string{strings.Join(fields, ",")}
		case "EnableImages", "EnableUserData":
			if len(entries) != 1 {
				return invalid()
			}
			value, err := strconv.ParseBool(entries[0])
			if err != nil {
				return invalid()
			}
			canonical[key] = []string{strconv.FormatBool(value)}
		case "ImageTypeLimit":
			if len(entries) != 1 {
				return invalid()
			}
			value, err := strconv.ParseInt(entries[0], 10, 32)
			if err != nil || value < 0 {
				return invalid()
			}
			canonical[key] = []string{strconv.FormatInt(value, 10)}
		}
	}
	if len(canonical) == 0 {
		return true
	}
	for name := range values {
		if _, selected := names[strings.ToLower(name)]; selected {
			delete(values, name)
		}
	}
	for name, entries := range canonical {
		values[name] = entries
	}
	copyURL := *r.URL
	copyURL.RawQuery = values.Encode()
	r.URL = &copyURL
	return true
}

func validItemProjectionField(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			if char < 'a' || char > 'z' {
				if char < '0' || char > '9' {
					if char != '_' {
						return false
					}
				}
			}
		}
	}
	return true
}

// Item identity remains present. Exclusions select optional DTO projections,
// never catalog membership, authorization, original media or playback planning.
// Unknown names remain forward-compatible hints, like unknown Fields values.
func applyItemFieldExclusions(item map[string]any, r *http.Request) {
	fields := queryValues(r.URL.Query()["ExcludeFields"])
	if len(fields) == 0 {
		return
	}
	for name := range item {
		switch name {
		case "Id", "Name", "Type", "IsFolder", "ServerId", "MediaType":
			continue
		}
		if hasField(fields, name) {
			delete(item, name)
		}
	}
	// Detail responses repeat stream/chapter/path facts inside MediaSources.
	// Removing only the top-level field would still deliver the excluded data.
	if sources, ok := item["MediaSources"].([]map[string]any); ok {
		for _, source := range sources {
			for _, name := range []string{"MediaStreams", "Chapters", "Path"} {
				if hasField(fields, name) {
					delete(source, name)
				}
			}
		}
	}
}
