package identity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSelectUserCopyOptionsUsesExactBoundedSDKEnumeration(t *testing.T) {
	for _, test := range []struct {
		source  string
		options []string
		want    userCopySelection
	}{
		{},
		{source: "source-user"},
		{source: "source-user", options: []string{"UserPolicy"}, want: userCopySelection{policy: true}},
		{source: "source-user", options: []string{"UserConfiguration"}, want: userCopySelection{configuration: true}},
		{source: "source-user", options: []string{"UserData"}, want: userCopySelection{data: true}},
		{source: "source-user", options: []string{"UserData", "UserPolicy", "UserConfiguration"}, want: userCopySelection{policy: true, configuration: true, data: true}},
	} {
		got, err := selectUserCopyOptions(test.source, test.options)
		if err != nil || got != test.want {
			t.Errorf("selection %#v = %#v, %v; want %#v", test.options, got, err, test.want)
		}
	}
	for _, test := range []struct {
		source  string
		options []string
	}{
		{options: []string{"UserPolicy"}},
		{source: " source", options: []string{"UserPolicy"}},
		{source: "source", options: []string{"userpolicy"}},
		{source: "source", options: []string{"Password"}},
		{source: "source", options: []string{"UserPolicy", "UserPolicy"}},
		{source: "source", options: []string{"UserPolicy", "UserData", "UserConfiguration", "UserData"}},
	} {
		if _, err := selectUserCopyOptions(test.source, test.options); err == nil {
			t.Errorf("invalid copy selection accepted: %#v", test.options)
		}
	}
}

func TestCopyUserConfigurationTransfersSupportedPreferencesWithoutSecrets(t *testing.T) {
	raw := json.RawMessage(`{"EnableNextEpisodeAutoPlay":false,"SubtitleMode":"Always","IntroSkipMode":"None",` +
		`"ResumeRewindSeconds":7,"OrderedViews":["library-b","library-a","library-b"],"Pin":"private-pin",` +
		`"Password":"private-password","AccessToken":"private-token","UnknownPreference":"private-opaque"}`)
	before := append([]byte(nil), raw...)
	encoded, err := copyUserConfiguration(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private-") || !reflect.DeepEqual([]byte(raw), before) {
		t.Fatal("configuration copy leaked unsupported values or modified its source")
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"EnableNextEpisodeAutoPlay": false, "SubtitleMode": "Always", "IntroSkipMode": "None",
		"ResumeRewindSeconds": float64(7), "OrderedViews": []any{"library-b", "library-a", "library-b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("supported configuration values changed: %#v", got)
	}
}

func TestCopyUserConfigurationRejectsMalformedSupportedState(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `{`, `{"EnableNextEpisodeAutoPlay":null}`, `{"EnableNextEpisodeAutoPlay":"true"}`,
		`{"enableNextEpisodeAutoPlay":true}`, `{"OrderedViews":[null]}`, `{"OrderedViews":[" library"]}`,
		`{"SubtitleMode":"Unsupported"}`, `{"IntroSkipMode":""}`, `{"ResumeRewindSeconds":-1}`,
		`{"ResumeRewindSeconds":2147483648}`,
	} {
		if _, err := copyUserConfiguration(json.RawMessage(raw)); err == nil {
			t.Errorf("invalid source configuration accepted: %s", raw)
		}
	}
}
