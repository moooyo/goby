package identity

import (
	"encoding/json"
	"math"
	"testing"
)

func TestUserConfigurationPatchIsValidatedAndConsumerBound(t *testing.T) {
	base := DefaultUserConfiguration()
	updated, err := ApplyUserConfigurationPatch(base, UserConfigurationPatch{
		"AudioLanguagePreference": json.RawMessage(`"eng"`), "SubtitleLanguagePreference": json.RawMessage(`"zh-Hans"`),
		"PlayDefaultAudioTrack": json.RawMessage(`false`), "SubtitleMode": json.RawMessage(`"HearingImpaired"`),
		"ResumeRewindSeconds": json.RawMessage(`10`), "OrderedViews": json.RawMessage(`["second","first"]`),
	})
	if err != nil || updated.AudioLanguagePreference != "eng" || updated.SubtitleMode != "HearingImpaired" ||
		updated.PlayDefaultAudioTrack || updated.ResumeRewindSeconds != 10 || PreferenceLanguageKey("eng") != PreferenceLanguageKey("en") ||
		PreferenceLanguageKey("zh-Hans") != PreferenceLanguageKey("zho") {
		t.Fatalf("valid preferences lost their language or field semantics: %+v, %v", updated, err)
	}
	for name, patch := range map[string]UserConfigurationPatch{
		"unknown":                   {"IsAdministrator": json.RawMessage(`true`)},
		"duplicate aliases":         {"SubtitleMode": json.RawMessage(`"None"`), "subTITLEmode": json.RawMessage(`"Always"`)},
		"null boolean":              {"PlayDefaultAudioTrack": json.RawMessage(`null`)},
		"null list":                 {"OrderedViews": json.RawMessage(`null`)},
		"invalid language":          {"AudioLanguagePreference": json.RawMessage(`"en\nprivate"`)},
		"rewind overflow":           {"ResumeRewindSeconds": json.RawMessage(`301`)},
		"invalid mode":              {"SubtitleMode": json.RawMessage(`"auto"`)},
		"unimplemented password":    {"EnableLocalPassword": json.RawMessage(`true`)},
		"unimplemented PIN":         {"ProfilePin": json.RawMessage(`"1234"`)},
		"unimplemented intro":       {"IntroSkipMode": json.RawMessage(`"AutoSkip"`)},
		"unimplemented missing":     {"DisplayMissingEpisodes": json.RawMessage(`true`)},
		"unimplemented suggestions": {"HidePlayedInSuggestions": json.RawMessage(`true`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ApplyUserConfigurationPatch(base, patch); err == nil {
				t.Fatal("unsupported or malformed preference was accepted")
			}
		})
	}
	if _, err := nextPreferenceRevision(math.MaxInt64); err == nil {
		t.Fatal("preference revision overflow was accepted")
	}
}

func TestDisplayPreferencePatchRejectsNullableAndOversizedLayoutValues(t *testing.T) {
	base := defaultDisplayPreferences("folder", "web")
	for name, patch := range map[string]DisplayPreferencesPatch{
		"foreign id":             {"Id": json.RawMessage(`"other"`)},
		"foreign client":         {"Client": json.RawMessage(`"other"`)},
		"null dictionary":        {"CustomPrefs": json.RawMessage(`null`)},
		"null value":             {"CustomPrefs": json.RawMessage(`{"layout":null}`)},
		"nonstring value":        {"CustomPrefs": json.RawMessage(`{"layout":true}`)},
		"duplicate layout key":   {"CustomPrefs": json.RawMessage(`{"layout":"poster","layout":"list"}`)},
		"invalid sort direction": {"SortOrder": json.RawMessage(`"random"`)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := applyDisplayPreferencesPatch(base, patch); err == nil {
				t.Fatal("invalid scoped preference was accepted")
			}
		})
	}
}
