package config

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	DefaultMaxBitrate       int64 = 20_000_000
	DefaultMaxWidth               = 1920
	DefaultMaxHeight              = 1080
	DefaultMaxAudioChannels       = 8
)

// ValidateServerName is shared by deployment defaults and managed overrides.
func ValidateServerName(value string) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("GOBY_SERVER_NAME must contain valid UTF-8 without null characters")
	}
	if strings.TrimSpace(value) == "" || len(value) > 128 {
		return fmt.Errorf("GOBY_SERVER_NAME must contain 1 to 128 bytes")
	}
	return nil
}

// ValidateOutputPlanningLimits checks new-plan ceilings without changing an
// existing conversion, opening a cache, or inspecting hardware.
func ValidateOutputPlanningLimits(maxBitrate int64, maxWidth, maxHeight, maxAudioChannels int) error {
	for _, field := range []struct {
		name       string
		value      int64
		upperBound int64
	}{
		{"GOBY_TRANSCODE_MAX_BITRATE", maxBitrate, 1_000_000_000},
		{"GOBY_TRANSCODE_MAX_WIDTH", int64(maxWidth), 8192},
		{"GOBY_TRANSCODE_MAX_HEIGHT", int64(maxHeight), 8192},
		{"GOBY_TRANSCODE_MAX_AUDIO_CHANNELS", int64(maxAudioChannels), 8},
	} {
		if field.value < 1 || field.value > field.upperBound {
			return fmt.Errorf("%s must be between 1 and %d", field.name, field.upperBound)
		}
	}
	return nil
}
