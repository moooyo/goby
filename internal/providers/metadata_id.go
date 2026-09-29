package providers

import "strings"

// MetadataID selects the identifier for the item's provider entity. A typed
// MusicBrainz ID takes precedence over the legacy generic MusicBrainz value;
// IDs for other entity types are never substituted for the requested type.
func MetadataID(provider, itemType string, ids map[string]string) string {
	if provider == "musicbrainz" {
		_, key, ok := musicBrainzEntityType(itemType)
		if !ok {
			return ""
		}
		if id := metadataIDValue(ids, key); id != "" {
			return id
		}
		return metadataIDValue(ids, "MusicBrainz")
	}
	if provider == "tmdb" {
		return metadataIDValue(ids, "Tmdb")
	}
	return ""
}

func metadataIDValue(ids map[string]string, key string) string {
	if value := strings.TrimSpace(ids[key]); value != "" {
		return value
	}
	// Imported provider names may use different casing. Pick a stable spelling
	// if a legacy record contains more than one case variant.
	var matched, value string
	for name, candidate := range ids {
		if strings.EqualFold(name, key) && strings.TrimSpace(candidate) != "" && (matched == "" || name < matched) {
			matched, value = name, strings.TrimSpace(candidate)
		}
	}
	return value
}
