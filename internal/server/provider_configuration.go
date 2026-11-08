package server

import (
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/settings"
)

// onlineProviderConfig combines the current private credential publication with
// managed policy. New operations capture one coherent credential snapshot;
// existing requests retain their original credentials and language policy.
func (s *Server) onlineProviderConfig() providers.Config {
	metadata := settings.MetadataOptions{PreferredMetadataLanguage: "en", MetadataCountryCode: "US"}
	if s.settings != nil {
		metadata = s.settings.MetadataValues()
	}
	return s.onlineProviderConfigForMetadata(metadata)
}

func (s *Server) onlineProviderConfigForMetadata(metadata settings.MetadataOptions) providers.Config {
	configured := s.cfg.OnlineProviders
	if s.providerConfiguration != nil {
		configured = s.providerConfiguration.Effective()
	}
	// An administrator override never bypasses the deployment's network gate.
	configured.Enabled = configured.Enabled && s.cfg.OnlineProviders.Enabled
	return providerConfigForMetadata(configured, metadata)
}

func providerConfigForMetadata(configured config.OnlineProvidersConfig, metadata settings.MetadataOptions) providers.Config {
	return providers.Config{
		Enabled:                configured.Enabled && metadata.EnableInternetProviders,
		TMDBToken:              configured.TMDBToken,
		MusicBrainzUserAgent:   configured.MusicBrainzUserAgent,
		OpenSubtitlesAPIKey:    configured.OpenSubtitlesAPIKey,
		OpenSubtitlesUsername:  configured.OpenSubtitlesUsername,
		OpenSubtitlesPassword:  configured.OpenSubtitlesPassword,
		OpenSubtitlesUserAgent: configured.OpenSubtitlesUserAgent,
		Language:               metadata.PreferredMetadataLanguage,
		Country:                metadata.MetadataCountryCode,
	}
}
