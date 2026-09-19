package server

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/moooyo/goby/internal/settings"
)

// Named objects replace only their own section. Omitted fields use the same
// defaults as native Reset; explicit null is never a reset instruction.
func decodeManagementConfiguration(w http.ResponseWriter, r *http.Request, mutation settings.ConfigurationMutation, values map[string]json.RawMessage) (settings.ConfigurationMutation, bool) {
	defaults := settings.DefaultManagement()
	for name, raw := range values {
		var target any
		if mutation.Section == settings.ConfigurationSubtitles {
			switch name {
			case "downloadlanguages":
				target = &defaults.Subtitles.DownloadLanguages
			case "downloadmoviesubtitles":
				target = &defaults.Subtitles.DownloadMovieSubtitles
			case "downloadepisodesubtitles":
				target = &defaults.Subtitles.DownloadEpisodeSubtitles
			}
		} else {
			switch name {
			case "maxconcurrent":
				target = &defaults.Tasks.MaxConcurrent
			case "cacheretentiondays":
				target = &defaults.Tasks.CacheRetentionDays
			case "cachemaxentries":
				target = &defaults.Tasks.CacheMaxEntries
			}
		}
		if target == nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, target) != nil {
			configurationInputError(w, r)
			return settings.ConfigurationMutation{}, false
		}
	}
	if err := settings.ValidateManagement(defaults); err != nil {
		configurationInputError(w, r)
		return settings.ConfigurationMutation{}, false
	}
	if mutation.Section == settings.ConfigurationSubtitles {
		mutation.Subtitles = &defaults.Subtitles
	} else {
		mutation.Tasks = &defaults.Tasks
	}
	return mutation, true
}
