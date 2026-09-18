package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

func decodeStoredManagement(data []byte) (Management, error) {
	var sections map[string]json.RawMessage
	if json.Unmarshal(data, &sections) != nil || len(sections) != 3 {
		return Management{}, fmt.Errorf("%w: incomplete management settings", ErrStoredSettings)
	}
	for section, fields := range map[string][]string{
		"Metadata":  {"EnableInternetProviders", "PreferredMetadataLanguage", "MetadataCountryCode"},
		"Subtitles": {"DownloadLanguages", "DownloadMovieSubtitles", "DownloadEpisodeSubtitles"},
		"Tasks":     {"MaxConcurrent", "CacheRetentionDays", "CacheMaxEntries"},
	} {
		var values map[string]json.RawMessage
		if json.Unmarshal(sections[section], &values) != nil || len(values) != len(fields) {
			return Management{}, fmt.Errorf("%w: incomplete management settings", ErrStoredSettings)
		}
		for _, field := range fields {
			value, exists := values[field]
			if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return Management{}, fmt.Errorf("%w: incomplete management settings", ErrStoredSettings)
			}
		}
	}
	var value Management
	if json.Unmarshal(data, &value) != nil {
		return Management{}, fmt.Errorf("%w: invalid management settings", ErrStoredSettings)
	}
	return value, nil
}

// Management contains only options with runtime consumers. Deployment paths,
// provider credentials, and unimplemented SDK properties are not writable here.
// Workers capture these values when a child starts; concurrency affects the
// next admission. None of these fields require a process restart.
type Management struct {
	Metadata  MetadataOptions
	Subtitles SubtitleOptions
	Tasks     TaskOptions
}

type MetadataOptions struct {
	EnableInternetProviders   bool
	PreferredMetadataLanguage string
	MetadataCountryCode       string
}

type SubtitleOptions struct {
	DownloadLanguages        []string
	DownloadMovieSubtitles   bool
	DownloadEpisodeSubtitles bool
}

type TaskOptions struct {
	MaxConcurrent      int
	CacheRetentionDays int
	CacheMaxEntries    int
}

const (
	FieldManagement Field = "Management"
	FieldMetadata   Field = "Management.Metadata"
	FieldSubtitles  Field = "Management.Subtitles"
	FieldTasks      Field = "Management.Tasks"
)

func DefaultManagement() Management {
	return Management{
		Metadata:  MetadataOptions{EnableInternetProviders: false, PreferredMetadataLanguage: "en", MetadataCountryCode: "US"},
		Subtitles: SubtitleOptions{DownloadLanguages: []string{"en"}, DownloadMovieSubtitles: true, DownloadEpisodeSubtitles: true},
		Tasks:     TaskOptions{MaxConcurrent: 2, CacheRetentionDays: 30, CacheMaxEntries: 10000},
	}
}

func cloneManagement(value Management) Management {
	value.Subtitles.DownloadLanguages = slices.Clone(value.Subtitles.DownloadLanguages)
	return value
}

func equalManagement(first, second Management) bool {
	return first.Metadata == second.Metadata && first.Tasks == second.Tasks &&
		first.Subtitles.DownloadMovieSubtitles == second.Subtitles.DownloadMovieSubtitles &&
		first.Subtitles.DownloadEpisodeSubtitles == second.Subtitles.DownloadEpisodeSubtitles &&
		slices.Equal(first.Subtitles.DownloadLanguages, second.Subtitles.DownloadLanguages)
}

var managementLanguage = regexp.MustCompile(`^[a-z]{2,3}(-[A-Z]{2})?$`)
var subtitleDownloadLanguage = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)
var managementCountry = regexp.MustCompile(`^[A-Z]{2}$`)

func ValidateManagement(value Management) error {
	fields := make(map[string]string)
	if !managementLanguage.MatchString(value.Metadata.PreferredMetadataLanguage) {
		fields["Management.Metadata.PreferredMetadataLanguage"] = "Use a lowercase two- or three-letter language code with an optional uppercase country suffix."
	}
	if !managementCountry.MatchString(value.Metadata.MetadataCountryCode) {
		fields["Management.Metadata.MetadataCountryCode"] = "Use an uppercase two-letter country code."
	}
	if value.Subtitles.DownloadLanguages == nil || len(value.Subtitles.DownloadLanguages) > 8 {
		fields["Management.Subtitles.DownloadLanguages"] = "Supply an array of at most eight distinct language codes."
	}
	seen := make(map[string]bool)
	for _, language := range value.Subtitles.DownloadLanguages {
		if !subtitleDownloadLanguage.MatchString(language) || seen[language] {
			fields["Management.Subtitles.DownloadLanguages"] = "Use distinct two-letter language codes with an optional uppercase country suffix."
		}
		seen[language] = true
	}
	if value.Tasks.MaxConcurrent < 1 || value.Tasks.MaxConcurrent > 16 {
		fields["Management.Tasks.MaxConcurrent"] = "Supply an integer between 1 and 16."
	}
	if value.Tasks.CacheRetentionDays < 1 || value.Tasks.CacheRetentionDays > 3650 {
		fields["Management.Tasks.CacheRetentionDays"] = "Supply an integer between 1 and 3650."
	}
	if value.Tasks.CacheMaxEntries < 1 || value.Tasks.CacheMaxEntries > 1000000 {
		fields["Management.Tasks.CacheMaxEntries"] = "Supply an integer between 1 and 1000000."
	}
	if len(fields) != 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func resetManagement(value Management, fields []Field) Management {
	defaults := DefaultManagement()
	for _, field := range fields {
		switch field {
		case FieldManagement:
			value = cloneManagement(defaults)
		case FieldMetadata:
			value.Metadata = defaults.Metadata
		case FieldSubtitles:
			value.Subtitles = defaults.Subtitles
		case FieldTasks:
			value.Tasks = defaults.Tasks
		}
	}
	return value
}
