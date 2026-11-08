package providerconfig

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
)

func stringPointer(value string) *string { return &value }

func TestProviderOverrideModesAndProjectionNeverRevealCredentials(t *testing.T) {
	defaults := config.OnlineProvidersConfig{Enabled: true, TMDBToken: "deployment-private-token", MusicBrainzUserAgent: "private-contact@example.test"}
	overrides := map[string]string{"tmdb.Token": "replacement-private-token"}
	effective, err := apply(defaults, overrides)
	if err != nil || effective.TMDBToken != overrides["tmdb.Token"] || effective.MusicBrainzUserAgent != defaults.MusicBrainzUserAgent {
		t.Fatal("overrides did not preserve unrelated deployment defaults")
	}
	encoded, err := json.Marshal(view(&state{revision: "9007199254740993", overrides: overrides, effective: effective, ready: true}))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{defaults.TMDBToken, defaults.MusicBrainzUserAgent, overrides["tmdb.Token"]} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("safe provider projection exposed a credential or contact")
		}
	}
	if !strings.Contains(string(encoded), `"Revision":"9007199254740993"`) {
		t.Fatal("provider revision lost integer precision")
	}
	if _, err := apply(defaults, map[string]string{"unrecognized.Token": "unsafe"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown encrypted configuration field was accepted")
	}
	cleared, err := apply(defaults, map[string]string{"tmdb.Token": ""})
	if err != nil || cleared.TMDBToken != "" {
		t.Fatal("an explicit clear inherited the deployment credential")
	}
	restored, err := apply(defaults, map[string]string{})
	if err != nil || restored.TMDBToken != defaults.TMDBToken {
		t.Fatal("restoring the deployment default lost the startup credential")
	}
}

func TestProviderMutationRejectsAmbiguousModesAndFields(t *testing.T) {
	for _, changes := range []map[string]Change{
		nil, {}, {"Unknown": {Mode: "clear"}}, {"Token": {Mode: "keep"}},
		{"Token": {Mode: "replace"}}, {"Token": {Mode: "replace", Value: stringPointer("")}},
		{"Token": {Mode: "clear", Value: stringPointer("ignored-secret")}},
		{"Token": {Mode: "default", Value: stringPointer("ignored-secret")}},
	} {
		if validateChanges("tmdb", changes) == nil {
			t.Fatal("ambiguous provider mutation was accepted")
		}
	}
	for _, change := range []Change{{Mode: "clear"}, {Mode: "default"}, {Mode: "replace", Value: stringPointer("replacement")}} {
		if err := validateChanges("tmdb", map[string]Change{"Token": change}); err != nil {
			t.Fatal("an explicit supported provider mutation was rejected")
		}
	}
	for _, raw := range []string{"", "0", "01", "+1", "1.0", " 1", "9223372036854775808"} {
		if _, err := revision(raw); err == nil {
			t.Fatal("noncanonical provider revision was accepted")
		}
	}
	if value, err := revision("9007199254740993"); err != nil || value != 9007199254740993 {
		t.Fatal("a large exact revision was rejected or rounded")
	}
}
