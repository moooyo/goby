package library

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// LibraryOptions contains scanner and automatic-analysis policy. Importer
// changes retain source facts; intro detection and preview generation are opt-in.
type LibraryOptions struct {
	EnableLocalMetadata               bool
	EnableLocalImages                 bool
	EnableEmbeddedArtwork             bool
	EnableIntroDetection              bool
	EnablePreviewGeneration           bool
	EnableBackgroundPreviewGeneration bool
	EnableAudioWaveformGeneration     bool
	EnableCreditsDetection            bool
	EnableSubtitleTimelineGeneration  bool
}

type LibraryOptionsUpdate struct {
	EnableLocalMetadata               *bool
	EnableLocalImages                 *bool
	EnableEmbeddedArtwork             *bool
	EnableIntroDetection              *bool
	EnablePreviewGeneration           *bool
	EnableBackgroundPreviewGeneration *bool
	EnableAudioWaveformGeneration     *bool
	EnableCreditsDetection            *bool
	EnableSubtitleTimelineGeneration  *bool
}

// UnmarshalJSON preserves omission while rejecting null, duplicate aliases and
// unsupported switches rather than acknowledging a setting without a consumer.
func (value *LibraryOptionsUpdate) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return ErrInvalidInput
	}
	*value = LibraryOptionsUpdate{}
	fields := map[string]**bool{"enablelocalmetadata": &value.EnableLocalMetadata,
		"enablelocalimages": &value.EnableLocalImages, "enableembeddedartwork": &value.EnableEmbeddedArtwork,
		"enableintrodetection": &value.EnableIntroDetection, "enablepreviewgeneration": &value.EnablePreviewGeneration,
		"enablebackgroundpreviewgeneration": &value.EnableBackgroundPreviewGeneration,
		"enableaudiowaveformgeneration":     &value.EnableAudioWaveformGeneration,
		"enablesubtitletimelinegeneration":  &value.EnableSubtitleTimelineGeneration,
		"enablecreditsdetection":            &value.EnableCreditsDetection}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		name, err := decoder.Token()
		key, ok := name.(string)
		key = strings.ToLower(key)
		if err != nil || !ok || fields[key] == nil || seen[key] {
			return ErrInvalidInput
		}
		var raw json.RawMessage
		var enabled bool
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &enabled) != nil {
			return ErrInvalidInput
		}
		*fields[key] = &enabled
		seen[key] = true
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return ErrInvalidInput
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidInput
	}
	return nil
}

func DefaultLibraryOptions() LibraryOptions {
	return LibraryOptions{EnableLocalMetadata: true, EnableLocalImages: true, EnableEmbeddedArtwork: true}
}

func validateLibraryOptions(collectionType string, options LibraryOptions) error {
	if options.EnableIntroDetection && collectionType != "tvshows" {
		return fmt.Errorf("%w: intro detection is supported only for TV libraries", ErrInvalidInput)
	}
	if options.EnablePreviewGeneration && collectionType != "movies" && collectionType != "tvshows" && collectionType != "mixed" {
		return fmt.Errorf("%w: preview generation is supported only for movie, TV and mixed video libraries", ErrInvalidInput)
	}
	if options.EnableBackgroundPreviewGeneration && collectionType != "movies" && collectionType != "tvshows" && collectionType != "mixed" {
		return fmt.Errorf("%w: background preview generation is supported only for movie, TV and mixed video libraries", ErrInvalidInput)
	}
	if options.EnableAudioWaveformGeneration && collectionType != "movies" && collectionType != "tvshows" && collectionType != "mixed" {
		return fmt.Errorf("%w: audio waveform generation is supported only for movie, TV and mixed video libraries", ErrInvalidInput)
	}
	if options.EnableCreditsDetection && collectionType != "movies" && collectionType != "tvshows" && collectionType != "mixed" {
		return fmt.Errorf("%w: credits detection is supported only for movie, TV and mixed video libraries", ErrInvalidInput)
	}
	if options.EnableSubtitleTimelineGeneration && collectionType != "movies" && collectionType != "tvshows" && collectionType != "mixed" {
		return fmt.Errorf("%w: subtitle timeline generation is supported only for movie, TV and mixed video libraries", ErrInvalidInput)
	}
	return nil
}

// A nil options pointer represents the historical scanner defaults. This also
// keeps in-memory callers from silently disabling the importers.
func EffectiveLibraryOptions(value Library) LibraryOptions {
	if value.Options == nil {
		return DefaultLibraryOptions()
	}
	return *value.Options
}

func applyLibraryOptions(previous LibraryOptions, update *LibraryOptionsUpdate) LibraryOptions {
	if update != nil {
		if update.EnableLocalMetadata != nil {
			previous.EnableLocalMetadata = *update.EnableLocalMetadata
		}
		if update.EnableLocalImages != nil {
			previous.EnableLocalImages = *update.EnableLocalImages
		}
		if update.EnableEmbeddedArtwork != nil {
			previous.EnableEmbeddedArtwork = *update.EnableEmbeddedArtwork
		}
		if update.EnableIntroDetection != nil {
			previous.EnableIntroDetection = *update.EnableIntroDetection
		}
		if update.EnablePreviewGeneration != nil {
			previous.EnablePreviewGeneration = *update.EnablePreviewGeneration
		}
		if update.EnableBackgroundPreviewGeneration != nil {
			previous.EnableBackgroundPreviewGeneration = *update.EnableBackgroundPreviewGeneration
		}
		if update.EnableAudioWaveformGeneration != nil {
			previous.EnableAudioWaveformGeneration = *update.EnableAudioWaveformGeneration
		}
		if update.EnableCreditsDetection != nil {
			previous.EnableCreditsDetection = *update.EnableCreditsDetection
		}
		if update.EnableSubtitleTimelineGeneration != nil {
			previous.EnableSubtitleTimelineGeneration = *update.EnableSubtitleTimelineGeneration
		}
	}
	return previous
}
