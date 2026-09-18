package server

import (
	"encoding/json"
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestManagementSettingsDecoderRejectsOpaqueAndAmbiguousObjects(t *testing.T) {
	valid, err := json.Marshal(settings.DefaultManagement())
	if err != nil {
		t.Fatal(err)
	}
	invalid := map[string]string{}
	if result := decodeManagementSettings(valid, invalid); result == nil || len(invalid) != 0 {
		t.Fatal("valid management settings rejected")
	}
	for name, raw := range map[string]string{
		"null":                  "null",
		"missing section":       `{"Metadata":{},"Subtitles":{}}`,
		"unknown section":       `{"Metadata":{},"Subtitles":{},"Tasks":{},"Secret":"private"}`,
		"duplicate section":     `{"Metadata":{},"Metadata":{},"Subtitles":{},"Tasks":{}}`,
		"null boolean":          `{"Metadata":{"EnableInternetProviders":null,"PreferredMetadataLanguage":"en","MetadataCountryCode":"US"},"Subtitles":{"DownloadLanguages":[],"DownloadMovieSubtitles":true,"DownloadEpisodeSubtitles":true},"Tasks":{"MaxConcurrent":2,"CacheRetentionDays":30,"CacheMaxEntries":10000}}`,
		"unknown setting":       `{"Metadata":{"EnableInternetProviders":false,"PreferredMetadataLanguage":"en","MetadataCountryCode":"US","TMDBToken":"private"},"Subtitles":{"DownloadLanguages":[],"DownloadMovieSubtitles":true,"DownloadEpisodeSubtitles":true},"Tasks":{"MaxConcurrent":2,"CacheRetentionDays":30,"CacheMaxEntries":10000}}`,
		"unbounded concurrency": `{"Metadata":{"EnableInternetProviders":false,"PreferredMetadataLanguage":"en","MetadataCountryCode":"US"},"Subtitles":{"DownloadLanguages":[],"DownloadMovieSubtitles":true,"DownloadEpisodeSubtitles":true},"Tasks":{"MaxConcurrent":200,"CacheRetentionDays":30,"CacheMaxEntries":10000}}`,
	} {
		t.Run(name, func(t *testing.T) {
			invalid := map[string]string{}
			if decodeManagementSettings(json.RawMessage(raw), invalid) != nil || len(invalid) == 0 {
				t.Fatal("untyped or unsupported management settings accepted")
			}
		})
	}
}
