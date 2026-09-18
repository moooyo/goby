package server

import (
	"bytes"
	"encoding/json"

	"github.com/moooyo/goby/internal/settings"
)

func decodeManagementSettings(raw json.RawMessage, invalid map[string]string) *settings.Management {
	sections := []string{"Metadata", "Subtitles", "Tasks"}
	objects, errors := adminTaskObject(raw, sections, sections, "Management")
	for field, message := range errors {
		invalid[field] = message
	}
	if len(errors) != 0 {
		return nil
	}
	fields := map[string][]string{
		"Metadata":  {"EnableInternetProviders", "PreferredMetadataLanguage", "MetadataCountryCode"},
		"Subtitles": {"DownloadLanguages", "DownloadMovieSubtitles", "DownloadEpisodeSubtitles"},
		"Tasks":     {"MaxConcurrent", "CacheRetentionDays", "CacheMaxEntries"},
	}
	for _, section := range sections {
		values, errors := adminTaskObject(objects[section], fields[section], fields[section], "Management."+section)
		for field, message := range errors {
			invalid[field] = message
		}
		for field, value := range values {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				invalid["Management."+section+"."+field] = "Supply a non-null typed setting value."
			}
		}
	}
	if len(invalid) != 0 {
		return nil
	}
	var value settings.Management
	if json.Unmarshal(raw, &value) != nil {
		invalid["Management"] = "Supply supported typed management settings."
		return nil
	}
	if err := settings.ValidateManagement(value); err != nil {
		for field, message := range err.(*settings.ValidationError).Fields {
			invalid[field] = message
		}
		return nil
	}
	return &value
}
