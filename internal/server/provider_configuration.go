package server

import (
	"github.com/moooyo/goby/internal/providers"
	"github.com/moooyo/goby/internal/settings"
)

// onlineProviderConfig combines private startup credentials with the current
// managed policy. It is constructed per operation, so disabling providers takes
// effect for new requests without revealing credentials in administrator DTOs.
func (s *Server) onlineProviderConfig() providers.Config {
	metadata := settings.MetadataOptions{PreferredMetadataLanguage: "en", MetadataCountryCode: "US"}
	if s.settings != nil {
		metadata = s.settings.MetadataValues()
	}
	return s.onlineProviderConfigForMetadata(metadata)
}

func (s *Server) onlineProviderConfigForMetadata(metadata settings.MetadataOptions) providers.Config {
	configured := s.cfg.OnlineProviders
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
