package identity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

const MaxUserConfigurationBytes = 128 << 10

// UserConfiguration contains user preferences, never authorization policy.
// Fields without a current consumer are read-only compatibility projections.
type UserConfiguration struct {
	AudioLanguagePreference    string
	SubtitleLanguagePreference string
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

type UserConfigurationPatch map[string]json.RawMessage

var UserConfigurationFields = []string{
	"AudioLanguagePreference", "SubtitleLanguagePreference", "DisplayMissingEpisodes", "EnableLocalPassword",
	"EnableNextEpisodeAutoPlay", "HidePlayedInLatest", "HidePlayedInMoreLikeThis", "HidePlayedInSuggestions",
	"IntroSkipMode", "LatestItemsExcludes", "MyMediaExcludes", "OrderedViews", "PlayDefaultAudioTrack",
	"RememberAudioSelections", "RememberSubtitleSelections", "ResumeRewindSeconds", "SubtitleMode", "ProfilePin",
}

func DefaultUserConfiguration() UserConfiguration {
	return UserConfiguration{
		EnableNextEpisodeAutoPlay: true, HidePlayedInLatest: true, IntroSkipMode: "None",
		LatestItemsExcludes: []string{}, MyMediaExcludes: []string{}, OrderedViews: []string{},
		PlayDefaultAudioTrack: true, RememberAudioSelections: true, RememberSubtitleSelections: true,
		SubtitleMode: "Smart",
	}
}

func configurationField(name string) string {
	for _, allowed := range UserConfigurationFields {
		if strings.EqualFold(name, allowed) {
			return allowed
		}
	}
	return ""
}

func validPreferenceText(value string, maximum int) bool {
	return len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		strings.IndexFunc(value, unicode.IsControl) < 0
}

// PreferenceLanguageKey compares ISO-639 and BCP-47 preferences by their base
// language, while preserving the original validated value in stored settings.
func PreferenceLanguageKey(value string) string {
	if value == "" || !validPreferenceText(value, 64) {
		return ""
	}
	tag, err := language.Parse(value)
	if err != nil {
		return ""
	}
	base, _ := tag.Base()
	return base.ISO3()
}

// ProjectUserConfiguration tolerates old/unknown stored fields and reads only
// validated known values. This is also the projection used by playback and
// navigation consumers; it does not make an old unknown field writable.
func ProjectUserConfiguration(raw json.RawMessage) UserConfiguration {
	result := DefaultUserConfiguration()
	if len(raw) == 0 || len(raw) > 1<<20 || !utf8.Valid(raw) {
		return result
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return result
	}
	for name, value := range values {
		if configurationField(name) != name {
			continue
		}
		_ = applyConfigurationField(&result, name, value, false)
	}
	return result
}

// ApplyUserConfigurationPatch validates a field merge without side effects.
// Native and compatibility writers share this validation and the same store.
func ApplyUserConfigurationPatch(current UserConfiguration, patch UserConfigurationPatch) (UserConfiguration, error) {
	result := current
	seen := make(map[string]bool, len(patch))
	for name, raw := range patch {
		canonical := configurationField(name)
		if canonical == "" || seen[canonical] {
			return UserConfiguration{}, managedUserFieldError("Configuration", "Supply each supported preference exactly once.")
		}
		seen[canonical] = true
		if err := applyConfigurationField(&result, canonical, raw, true); err != nil {
			return UserConfiguration{}, managedUserFieldError("Configuration."+canonical, err.Error())
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > MaxUserConfigurationBytes {
		return UserConfiguration{}, managedUserFieldError("Configuration", "The preference document exceeds its 128 KiB limit.")
	}
	return result, nil
}

func applyConfigurationField(result *UserConfiguration, name string, raw json.RawMessage, writable bool) error {
	invalid := func() error { return fmt.Errorf("Supply a valid value for this preference.") }
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return invalid()
	}
	var flag *bool
	switch name {
	case "DisplayMissingEpisodes":
		flag = &result.DisplayMissingEpisodes
	case "EnableLocalPassword":
		flag = &result.EnableLocalPassword
	case "EnableNextEpisodeAutoPlay":
		flag = &result.EnableNextEpisodeAutoPlay
	case "HidePlayedInLatest":
		flag = &result.HidePlayedInLatest
	case "HidePlayedInMoreLikeThis":
		flag = &result.HidePlayedInMoreLikeThis
	case "HidePlayedInSuggestions":
		flag = &result.HidePlayedInSuggestions
	case "PlayDefaultAudioTrack":
		flag = &result.PlayDefaultAudioTrack
	case "RememberAudioSelections":
		flag = &result.RememberAudioSelections
	case "RememberSubtitleSelections":
		flag = &result.RememberSubtitleSelections
	}
	if flag != nil {
		var value bool
		if json.Unmarshal(raw, &value) != nil {
			return invalid()
		}
		if writable && value != *flag && (name == "DisplayMissingEpisodes" || name == "EnableLocalPassword" ||
			name == "EnableNextEpisodeAutoPlay" || name == "HidePlayedInSuggestions") {
			return fmt.Errorf("This compatibility preference has no implemented consumer and is read-only.")
		}
		*flag = value
		return nil
	}
	switch name {
	case "AudioLanguagePreference", "SubtitleLanguagePreference":
		var value string
		if json.Unmarshal(raw, &value) != nil || value != "" && PreferenceLanguageKey(value) == "" {
			return fmt.Errorf("Supply an empty string or a valid bounded language tag.")
		}
		if name == "AudioLanguagePreference" {
			result.AudioLanguagePreference = value
		} else {
			result.SubtitleLanguagePreference = value
		}
	case "OrderedViews", "LatestItemsExcludes", "MyMediaExcludes":
		var values []string
		if json.Unmarshal(raw, &values) != nil || values == nil || len(values) > 1024 {
			return invalid()
		}
		for _, value := range values {
			if value == "" || !validPreferenceText(value, 256) {
				return invalid()
			}
		}
		values = append([]string{}, values...)
		switch name {
		case "OrderedViews":
			result.OrderedViews = values
		case "LatestItemsExcludes":
			result.LatestItemsExcludes = values
		case "MyMediaExcludes":
			result.MyMediaExcludes = values
		}
	case "ResumeRewindSeconds":
		var value int32
		if json.Unmarshal(raw, &value) != nil || value < 0 || writable && value > 300 {
			return invalid()
		}
		result.ResumeRewindSeconds = value
	case "SubtitleMode":
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return invalid()
		}
		switch value {
		case "Default", "Always", "OnlyForced", "None", "Smart", "HearingImpaired":
			result.SubtitleMode = value
		default:
			return invalid()
		}
	case "IntroSkipMode":
		var value string
		if json.Unmarshal(raw, &value) != nil || value != "None" && value != "ShowButton" && value != "AutoSkip" {
			return invalid()
		}
		if writable && value != result.IntroSkipMode {
			return fmt.Errorf("Intro skipping is not implemented; this preference is read-only.")
		}
		result.IntroSkipMode = value
	case "ProfilePin":
		var value string
		if json.Unmarshal(raw, &value) != nil || value != "" {
			return fmt.Errorf("Profile PIN authentication is not implemented.")
		}
	default:
		return invalid()
	}
	return nil
}

func nextPreferenceRevision(revision int64) (int64, error) {
	if revision < 1 || revision == math.MaxInt64 {
		return 0, ErrRevisionConflict
	}
	return revision + 1, nil
}
