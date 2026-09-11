package server

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// These are client preferences, never permissions or advertised server
// features. The default values and field presence were observed on the fresh
// Emby 4.9.5.0 viewer in m3e-reference-client-initialization.json. Unobserved
// language and PIN fields remain omitted; this projection adds no write API.
type embyUserConfiguration struct {
	DisplayMissingEpisodes     bool
	EnableLocalPassword        bool
	EnableNextEpisodeAutoPlay  bool
	HidePlayedInLatest         bool
	HidePlayedInMoreLikeThis   bool
	HidePlayedInSuggestions    bool
	IntroSkipMode              string
	LatestItemsExcludes        []string
	MyMediaExcludes            []string
	OrderedViews               []string
	PlayDefaultAudioTrack      bool
	RememberAudioSelections    bool
	RememberSubtitleSelections bool
	ResumeRewindSeconds        int32
	SubtitleMode               string
}

func defaultUserConfiguration() embyUserConfiguration {
	return embyUserConfiguration{
		EnableNextEpisodeAutoPlay: true, HidePlayedInLatest: true,
		IntroSkipMode: "ShowButton", LatestItemsExcludes: []string{}, MyMediaExcludes: []string{}, OrderedViews: []string{},
		PlayDefaultAudioTrack: true, RememberAudioSelections: true, RememberSubtitleSelections: true,
		SubtitleMode: "Smart",
	}
}

func projectUserConfiguration(raw json.RawMessage) embyUserConfiguration {
	result := defaultUserConfiguration()
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) || !adminSettingsUnicode(raw) {
		return result
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return result
	}
	for _, field := range []struct {
		name   string
		target *bool
	}{
		{"DisplayMissingEpisodes", &result.DisplayMissingEpisodes}, {"EnableLocalPassword", &result.EnableLocalPassword},
		{"EnableNextEpisodeAutoPlay", &result.EnableNextEpisodeAutoPlay}, {"HidePlayedInLatest", &result.HidePlayedInLatest},
		{"HidePlayedInMoreLikeThis", &result.HidePlayedInMoreLikeThis}, {"HidePlayedInSuggestions", &result.HidePlayedInSuggestions},
		{"PlayDefaultAudioTrack", &result.PlayDefaultAudioTrack}, {"RememberAudioSelections", &result.RememberAudioSelections},
		{"RememberSubtitleSelections", &result.RememberSubtitleSelections},
	} {
		var value *bool
		if json.Unmarshal(values[field.name], &value) == nil && value != nil {
			*field.target = *value
		}
	}
	for _, field := range []struct {
		name   string
		target *[]string
	}{
		{"OrderedViews", &result.OrderedViews}, {"LatestItemsExcludes", &result.LatestItemsExcludes}, {"MyMediaExcludes", &result.MyMediaExcludes},
	} {
		var value []string
		if json.Unmarshal(values[field.name], &value) != nil || value == nil || len(value) > 1024 {
			continue
		}
		valid := true
		for _, id := range value {
			if id == "" || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) != id || strings.IndexFunc(id, unicode.IsControl) >= 0 {
				valid = false
				break
			}
		}
		if valid {
			// Order, duplicates, and opaque IDs remain exactly as persisted.
			// These lists can only describe presentation; they never grant ACLs.
			*field.target = append([]string{}, value...)
		}
	}
	// Enum values are declared by definitions/SubtitlePlaybackMode and
	// definitions/SegmentSkipMode in docs/sources/emby-sdk-openapi.snapshot.json.
	for _, field := range []struct {
		name    string
		target  *string
		allowed []string
	}{
		{"SubtitleMode", &result.SubtitleMode, []string{"Default", "Always", "OnlyForced", "None", "Smart", "HearingImpaired"}},
		{"IntroSkipMode", &result.IntroSkipMode, []string{"ShowButton", "AutoSkip", "None"}},
	} {
		var value string
		if json.Unmarshal(values[field.name], &value) != nil {
			continue
		}
		for _, allowed := range field.allowed {
			if value == allowed {
				*field.target = value
				break
			}
		}
	}
	var rewind *int32
	if json.Unmarshal(values["ResumeRewindSeconds"], &rewind) == nil && rewind != nil && *rewind >= 0 {
		result.ResumeRewindSeconds = *rewind
	}
	return result
}
