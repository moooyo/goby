//go:build linux

package server

import (
	"net/http"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/providers"
)

func TestOnlineProviderConfigKeepsCapturedManagementAcrossUpdates(t *testing.T) {
	f := newConfigurationHTTPFixture(t)
	s := &Server{settings: f.app.settings, cfg: config.Config{OnlineProviders: config.OnlineProvidersConfig{
		Enabled: true, TMDBToken: "test-tmdb-token", MusicBrainzUserAgent: "test-musicbrainz-agent",
		OpenSubtitlesAPIKey: "test-subtitle-key", OpenSubtitlesUsername: "test-subtitle-user",
		OpenSubtitlesPassword: "test-subtitle-password", OpenSubtitlesUserAgent: "test-subtitle-agent",
	}}}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", map[string]any{
		"EnableInternetProviders": true, "PreferredMetadataLanguage": "fr", "MetadataCountryCode": "FR",
	}, f.admin.headers), http.StatusNoContent)
	captured := f.app.settings.ManagementSnapshot()
	want := providers.Config{
		Enabled: true, TMDBToken: "test-tmdb-token", MusicBrainzUserAgent: "test-musicbrainz-agent",
		OpenSubtitlesAPIKey: "test-subtitle-key", OpenSubtitlesUsername: "test-subtitle-user",
		OpenSubtitlesPassword: "test-subtitle-password", OpenSubtitlesUserAgent: "test-subtitle-agent",
		Language: "fr", Country: "FR",
	}
	if s.onlineProviderConfig() != want {
		t.Fatal("provider configuration did not combine current managed values with startup credentials")
	}
	configurationHTTPStatus(t, f.request(t, http.MethodPost, "/System/Configuration/Partial", map[string]any{
		"EnableInternetProviders": false, "PreferredMetadataLanguage": "de", "MetadataCountryCode": "DE",
	}, f.admin.headers), http.StatusNoContent)
	if s.onlineProviderConfigForMetadata(captured.Metadata) != want {
		t.Fatal("provider configuration replaced the captured worker policy with a later publication")
	}
	want.Enabled, want.Language, want.Country = false, "de", "DE"
	if s.onlineProviderConfig() != want {
		t.Fatal("a new operation missed the current provider switch or language selection")
	}
	s.cfg.OnlineProviders.Enabled = false
	want.Language, want.Country = "fr", "FR"
	if s.onlineProviderConfigForMetadata(captured.Metadata) != want {
		t.Fatal("captured managed permission enabled deployment-disabled providers")
	}
}

func TestOnlineProviderConfigWithoutSettingsKeepsDisabledDefaults(t *testing.T) {
	s := &Server{cfg: config.Config{OnlineProviders: config.OnlineProvidersConfig{Enabled: true, TMDBToken: "test-token"}}}
	value := s.onlineProviderConfig()
	if value.Enabled || value.Language != "en" || value.Country != "US" || value.TMDBToken != "test-token" {
		t.Fatal("missing settings changed the disabled provider defaults or startup credentials")
	}
}
