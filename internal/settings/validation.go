package settings

import (
	"math"
	"strings"

	"github.com/moooyo/goby/internal/config"
)

// Validation delegates the actual value policy to the same pure functions
// used by startup configuration. Only native field naming is adapted here.
func validateValues(value Values) error {
	fields := make(map[string]string)
	if err := config.ValidateServerName(value.ServerName); err != nil {
		fields[string(FieldServerName)] = strings.TrimPrefix(err.Error(), "GOBY_SERVER_NAME ")
	}
	if err := config.ValidateOutputPlanningLimits(value.MaxBitrate, value.MaxWidth, value.MaxHeight, value.MaxAudioChannels); err != nil {
		field, message := "OutputPlanning", err.Error()
		for _, mapping := range []struct {
			prefix string
			field  Field
		}{
			{"GOBY_TRANSCODE_MAX_BITRATE ", FieldMaxBitrate},
			{"GOBY_TRANSCODE_MAX_WIDTH ", FieldMaxWidth},
			{"GOBY_TRANSCODE_MAX_HEIGHT ", FieldMaxHeight},
			{"GOBY_TRANSCODE_MAX_AUDIO_CHANNELS ", FieldMaxAudioChannels},
		} {
			if strings.HasPrefix(message, mapping.prefix) {
				field, message = string(mapping.field), strings.TrimPrefix(message, mapping.prefix)
				break
			}
		}
		fields[field] = message
	}
	if len(fields) != 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

func validateRevision(revision int64) error {
	if revision < 1 || revision == math.MaxInt64 {
		return &ValidationError{Fields: map[string]string{"Revision": "revision must be positive and leave room for its successor"}}
	}
	return nil
}

func validateReset(fields []Field) error {
	if len(fields) < 1 || len(fields) > 5 {
		return &ValidationError{Fields: map[string]string{"Fields": "select between one and five settings to reset"}}
	}
	seen := make(map[Field]struct{}, len(fields))
	for _, field := range fields {
		switch field {
		case FieldServerName, FieldMaxBitrate, FieldMaxWidth, FieldMaxHeight, FieldMaxAudioChannels:
		default:
			return &ValidationError{Fields: map[string]string{"Fields": "reset contains an unsupported setting"}}
		}
		if _, exists := seen[field]; exists {
			return &ValidationError{Fields: map[string]string{"Fields": "each reset setting must appear only once"}}
		}
		seen[field] = struct{}{}
	}
	return nil
}

func clearFields(value Overrides, fields []Field) Overrides {
	for _, field := range fields {
		switch field {
		case FieldServerName:
			value.ServerName = nil
		case FieldMaxBitrate:
			value.MaxBitrate = nil
		case FieldMaxWidth:
			value.MaxWidth = nil
		case FieldMaxHeight:
			value.MaxHeight = nil
		case FieldMaxAudioChannels:
			value.MaxAudioChannels = nil
		}
	}
	return value
}

func equalPointer[T comparable](first, second *T) bool {
	return first == nil && second == nil || first != nil && second != nil && *first == *second
}

func equalOverrides(first, second Overrides) bool {
	return equalPointer(first.ServerName, second.ServerName) &&
		equalPointer(first.MaxBitrate, second.MaxBitrate) &&
		equalPointer(first.MaxWidth, second.MaxWidth) &&
		equalPointer(first.MaxHeight, second.MaxHeight) &&
		equalPointer(first.MaxAudioChannels, second.MaxAudioChannels)
}
