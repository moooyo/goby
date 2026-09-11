package server

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Literal wire defaults are from the retained fresh Emby 4.9.5.0 viewer
// capture, independently of the implementation's constructor.
const observedUserConfiguration = `{
	"DisplayMissingEpisodes":false,"EnableLocalPassword":false,"EnableNextEpisodeAutoPlay":true,
	"HidePlayedInLatest":true,"HidePlayedInMoreLikeThis":false,"HidePlayedInSuggestions":false,
	"IntroSkipMode":"ShowButton","LatestItemsExcludes":[],"MyMediaExcludes":[],"OrderedViews":[],
	"PlayDefaultAudioTrack":true,"RememberAudioSelections":true,"RememberSubtitleSelections":true,
	"ResumeRewindSeconds":0,"SubtitleMode":"Smart"
}`

func userConfigurationObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal("encode user configuration projection")
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal("decode user configuration projection")
	}
	return object
}

func observedUserConfigurationObject(t *testing.T) map[string]any {
	t.Helper()
	var expected map[string]any
	if err := json.Unmarshal([]byte(observedUserConfiguration), &expected); err != nil {
		t.Fatal("decode independently captured configuration defaults")
	}
	return expected
}

func TestUserConfigurationProjectionMatchesObservedDefaultShape(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`null`), json.RawMessage(`[]`), json.RawMessage(`false`),
		json.RawMessage(`{"OrderedViews":`), json.RawMessage(`{"OrderedViews":["\ud800"]}`), json.RawMessage("{\"OrderedViews\":[\"\xff\"]}")} {
		projected := projectUserConfiguration(raw)
		if !reflect.DeepEqual(userConfigurationObject(t, projected), observedUserConfigurationObject(t)) {
			t.Fatal("default or malformed configuration did not retain the observed fifteen-field shape")
		}
		if projected.OrderedViews == nil || projected.LatestItemsExcludes == nil || projected.MyMediaExcludes == nil {
			t.Fatal("client array preferences must never become absent or null")
		}
	}
}

func TestUserConfigurationProjectionPreservesValidStoredPreferences(t *testing.T) {
	raw := json.RawMessage(`{
		"DisplayMissingEpisodes":true,"EnableLocalPassword":true,"EnableNextEpisodeAutoPlay":false,
		"HidePlayedInLatest":false,"HidePlayedInMoreLikeThis":true,"HidePlayedInSuggestions":true,
		"IntroSkipMode":"AutoSkip","LatestItemsExcludes":["latest-b","latest-a"],"MyMediaExcludes":["hidden-c"],
		"OrderedViews":["view-b","view-a","view-b"],"PlayDefaultAudioTrack":false,
		"RememberAudioSelections":false,"RememberSubtitleSelections":false,"ResumeRewindSeconds":15,"SubtitleMode":"OnlyForced",
		"ProfilePin":"private-config-marker","AudioLanguagePreference":"unobserved-audio",
		"SubtitleLanguagePreference":"unobserved-subtitle","Unknown":"private-config-marker","EnableAllFolders":true
	}`)
	before := append([]byte(nil), raw...)
	projected := projectUserConfiguration(raw)
	if !projected.DisplayMissingEpisodes || !projected.EnableLocalPassword || projected.EnableNextEpisodeAutoPlay || projected.HidePlayedInLatest ||
		!projected.HidePlayedInMoreLikeThis || !projected.HidePlayedInSuggestions || projected.IntroSkipMode != "AutoSkip" ||
		projected.PlayDefaultAudioTrack || projected.RememberAudioSelections || projected.RememberSubtitleSelections ||
		projected.ResumeRewindSeconds != 15 || projected.SubtitleMode != "OnlyForced" ||
		!reflect.DeepEqual(projected.OrderedViews, []string{"view-b", "view-a", "view-b"}) ||
		!reflect.DeepEqual(projected.LatestItemsExcludes, []string{"latest-b", "latest-a"}) || !reflect.DeepEqual(projected.MyMediaExcludes, []string{"hidden-c"}) {
		t.Fatal("typed user configuration projection discarded valid persisted values or ordering")
	}
	encoded, _ := json.Marshal(projected)
	if len(userConfigurationObject(t, projected)) != 15 || bytes.Contains(encoded, []byte("private-config-marker")) ||
		bytes.Contains(encoded, []byte("unobserved-")) || bytes.Contains(encoded, []byte("EnableAllFolders")) || !bytes.Equal(raw, before) {
		t.Fatal("configuration projection leaked an unobserved field or changed its stored snapshot")
	}
	projected.OrderedViews[0] = "changed-local-output"
	if next := projectUserConfiguration(raw); next.OrderedViews[0] != "view-b" || !bytes.Equal(raw, before) {
		t.Fatal("a mutable DTO array was shared with another request or the persisted snapshot")
	}
}

func TestUserConfigurationProjectionFallsBackPerInvalidField(t *testing.T) {
	for _, invalid := range []string{`null`, `false`, `"view-a"`, `[1]`, `[null]`, `[""]`, `[" leading"]`, `["line\nbreak"]`,
		`["` + strings.Repeat("x", 257) + `"]`, `[` + strings.Repeat(`"view-a",`, 1024) + `"view-a"]`} {
		raw := json.RawMessage(`{"OrderedViews":` + invalid + `,"LatestItemsExcludes":["retained"],"RememberAudioSelections":false}`)
		result := projectUserConfiguration(raw)
		if result.OrderedViews == nil || len(result.OrderedViews) != 0 || !reflect.DeepEqual(result.LatestItemsExcludes, []string{"retained"}) || result.RememberAudioSelections {
			t.Fatal("invalid array preferences either escaped type checks or erased other valid fields")
		}
	}
	for _, invalid := range []string{`null`, `"false"`, `0`, `{}`, `[]`} {
		result := projectUserConfiguration(json.RawMessage(`{"HidePlayedInLatest":` + invalid + `,"DisplayMissingEpisodes":` + invalid + `}`))
		if !result.HidePlayedInLatest || result.DisplayMissingEpisodes {
			t.Fatal("wrong-typed booleans did not retain their distinct observed defaults")
		}
	}
	for _, invalid := range []string{`null`, `"5"`, `1.5`, `-1`, `2147483648`} {
		if result := projectUserConfiguration(json.RawMessage(`{"ResumeRewindSeconds":` + invalid + `}`)); result.ResumeRewindSeconds != 0 {
			t.Fatal("invalid rewind values were projected as valid client settings")
		}
	}
	result := projectUserConfiguration(json.RawMessage(`{"SubtitleMode":"Unknown","IntroSkipMode":true,"ResumeRewindSeconds":2147483647}`))
	if result.SubtitleMode != "Smart" || result.IntroSkipMode != "ShowButton" || result.ResumeRewindSeconds != 2147483647 {
		t.Fatal("enum defaults or the declared int32 boundary changed")
	}
}
