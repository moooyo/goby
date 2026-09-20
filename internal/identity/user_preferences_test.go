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
		"IntroSkipMode": json.RawMessage(`"AutoSkip"`), "EnableNextEpisodeAutoPlay": json.RawMessage(`false`),
		"DisplayMissingEpisodes": json.RawMessage(`true`), "HidePlayedInSuggestions": json.RawMessage(`true`),
	})
	if err != nil || updated.AudioLanguagePreference != "eng" || updated.SubtitleMode != "HearingImpaired" ||
		updated.PlayDefaultAudioTrack || updated.ResumeRewindSeconds != 10 || PreferenceLanguageKey("eng") != PreferenceLanguageKey("en") ||
		PreferenceLanguageKey("zh-Hans") != PreferenceLanguageKey("zho") || updated.IntroSkipMode != "AutoSkip" || updated.EnableNextEpisodeAutoPlay ||
		!updated.DisplayMissingEpisodes || !updated.HidePlayedInSuggestions {
		t.Fatalf("valid preferences lost their language or field semantics: %+v, %v", updated, err)
	}
	for name, patch := range map[string]UserConfigurationPatch{
		"unknown":                  {"IsAdministrator": json.RawMessage(`true`)},
		"duplicate aliases":        {"SubtitleMode": json.RawMessage(`"None"`), "subTITLEmode": json.RawMessage(`"Always"`)},
		"null boolean":             {"PlayDefaultAudioTrack": json.RawMessage(`null`)},
		"null list":                {"OrderedViews": json.RawMessage(`null`)},
		"invalid language":         {"AudioLanguagePreference": json.RawMessage(`"en\nprivate"`)},
		"rewind overflow":          {"ResumeRewindSeconds": json.RawMessage(`301`)},
		"invalid mode":             {"SubtitleMode": json.RawMessage(`"auto"`)},
		"credential operation":     {"EnableLocalPassword": json.RawMessage(`true`)},
		"PIN credential operation": {"ProfilePin": json.RawMessage(`"1234"`)},
		"invalid intro mode":       {"IntroSkipMode": json.RawMessage(`"auto"`)},
		"invalid autoplay":         {"EnableNextEpisodeAutoPlay": json.RawMessage(`"false"`)},
		"invalid missing":          {"DisplayMissingEpisodes": json.RawMessage(`"true"`)},
		"invalid suggestions":      {"HidePlayedInSuggestions": json.RawMessage(`null`)},
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

func TestPlaybackBehaviorPreferencesRoundTripWithoutCredentialMaterial(t *testing.T) {
	for _, mode := range []string{"None", "ShowButton", "AutoSkip"} {
		for _, enabled := range []bool{false, true} {
			modeJSON, _ := json.Marshal(mode)
			autoplayJSON, _ := json.Marshal(enabled)
			updated, err := ApplyUserConfigurationPatch(DefaultUserConfiguration(), UserConfigurationPatch{
				"IntroSkipMode": modeJSON, "EnableNextEpisodeAutoPlay": autoplayJSON,
				"ProfilePin": json.RawMessage(`""`),
			})
			if err != nil {
				t.Fatalf("write playback behavior preferences: %v", err)
			}
			encoded, err := json.Marshal(updated)
			if err != nil {
				t.Fatal(err)
			}
			projected := ProjectUserConfiguration(encoded)
			if projected.IntroSkipMode != mode || projected.EnableNextEpisodeAutoPlay != enabled {
				t.Fatal("playback behavior preference did not survive projection")
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(encoded, &fields) != nil || fields["ProfilePin"] != nil {
				t.Fatal("configuration exposed PIN credential material")
			}
		}
	}
}
