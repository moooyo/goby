package server

import "github.com/moooyo/goby/internal/providers"

// onlineProviderConfig combines private startup credentials with the current
// managed policy. It is constructed per operation, so disabling providers takes
// effect for new requests without revealing credentials in administrator DTOs.
func (s *Server) onlineProviderConfig() providers.Config {
	configured := s.cfg.OnlineProviders
	result := providers.Config{
		TMDBToken:              configured.TMDBToken,
		MusicBrainzUserAgent:   configured.MusicBrainzUserAgent,
		OpenSubtitlesAPIKey:    configured.OpenSubtitlesAPIKey,
		OpenSubtitlesUsername:  configured.OpenSubtitlesUsername,
		OpenSubtitlesPassword:  configured.OpenSubtitlesPassword,
		OpenSubtitlesUserAgent: configured.OpenSubtitlesUserAgent,
		Language:               "en", Country: "US",
	}
	if s.settings != nil {
		settings := s.settings.Snapshot().Management.Metadata
		result.Enabled = configured.Enabled && settings.EnableInternetProviders
		result.Language = settings.PreferredMetadataLanguage
		result.Country = settings.MetadataCountryCode
	}
	return result
}
