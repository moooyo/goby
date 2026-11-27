package providers

import (
	"strings"

	"github.com/moooyo/goby/internal/metadata"
)

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

// ValidMetadataLookupID distinguishes a requestable identity from an opaque
// imported value, without consulting provider configuration or using HTTP.
func ValidMetadataLookupID(provider, itemType, id string) bool {
	switch provider {
	case "tmdb":
		return CanonicalTMDBMetadataID(itemType, id) != ""
	case "musicbrainz":
		_, _, supported := musicBrainzEntityType(itemType)
		return supported && musicBrainzID(id)
	default:
		return false
	}
}

func metadataIDValue(ids map[string]string, key string) string {
	if value := strings.TrimSpace(ids[key]); value != "" {
		return value
	}
	// Imported provider names may use different casing. Pick a stable spelling
	// if a legacy record contains more than one case variant.
	var matched, value string
	for name, candidate := range ids {
		if metadata.ProviderKeyEqual(name, key) && strings.TrimSpace(candidate) != "" && (matched == "" || name < matched) {
			matched, value = name, strings.TrimSpace(candidate)
		}
	}
	return value
}
