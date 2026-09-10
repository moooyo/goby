package config

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/transcode"
)

// TranscodingConfig bounds the conversion manager and the server's output
// policy. Load enables conversion by default; an entirely zero value disables
// it so callers constructing Config directly opt in explicitly.
type TranscodingConfig struct {
	Enabled          bool
	CacheDirectory   string
	Threads          int
	MaxJobs          int
	MaxUserJobs      int
	MaxSessionJobs   int
	MaxQueueJobs     int
	MaxRetainedJobs  int
	MaxCacheBytes    int64
	MaxJobBytes      int64
	MinFreeBytes     int64
	MaxBitrate       int64
	MaxWidth         int
	MaxHeight        int
	MaxAudioChannels int
	Hardware         transcode.Hardware
}

const maxConfiguredCacheBytes = int64(1 << 50)

func loadTranscoding() (TranscodingConfig, error) {
	c := TranscodingConfig{
		CacheDirectory: env("GOBY_TRANSCODE_CACHE", "/var/cache/goby/transcodes"),
		Hardware: transcode.Hardware{
			Decode: env("GOBY_HW_DECODER", "software"),
			Encode: env("GOBY_HW_ENCODER", "software"),
			Device: env("GOBY_HW_DEVICE", ""),
		},
	}
	var err error
	c.Enabled, err = strconv.ParseBool(env("GOBY_TRANSCODING_ENABLED", "true"))
	if err != nil {
		return TranscodingConfig{}, fmt.Errorf("GOBY_TRANSCODING_ENABLED must be a boolean")
	}
	for _, field := range []struct {
		name     string
		value    *int
		fallback int
	}{
		{"GOBY_TRANSCODE_THREADS", &c.Threads, 2},
		{"GOBY_TRANSCODE_MAX_JOBS", &c.MaxJobs, 2},
		{"GOBY_TRANSCODE_MAX_USER_JOBS", &c.MaxUserJobs, 1},
		{"GOBY_TRANSCODE_MAX_SESSION_JOBS", &c.MaxSessionJobs, 1},
		{"GOBY_TRANSCODE_MAX_QUEUE_JOBS", &c.MaxQueueJobs, 16},
		{"GOBY_TRANSCODE_MAX_RETAINED_JOBS", &c.MaxRetainedJobs, 128},
		{"GOBY_TRANSCODE_MAX_WIDTH", &c.MaxWidth, DefaultMaxWidth},
		{"GOBY_TRANSCODE_MAX_HEIGHT", &c.MaxHeight, DefaultMaxHeight},
		{"GOBY_TRANSCODE_MAX_AUDIO_CHANNELS", &c.MaxAudioChannels, DefaultMaxAudioChannels},
	} {
		*field.value, err = strconv.Atoi(env(field.name, strconv.Itoa(field.fallback)))
		if err != nil {
			return TranscodingConfig{}, fmt.Errorf("%s must be a decimal integer", field.name)
		}
	}
	for _, field := range []struct {
		name     string
		value    *int64
		fallback int64
	}{
		{"GOBY_TRANSCODE_MAX_CACHE_BYTES", &c.MaxCacheBytes, 20 << 30},
		{"GOBY_TRANSCODE_MAX_JOB_BYTES", &c.MaxJobBytes, 8 << 30},
		{"GOBY_TRANSCODE_MIN_FREE_BYTES", &c.MinFreeBytes, 512 << 20},
		{"GOBY_TRANSCODE_MAX_BITRATE", &c.MaxBitrate, DefaultMaxBitrate},
	} {
		*field.value, err = strconv.ParseInt(env(field.name, strconv.FormatInt(field.fallback, 10)), 10, 64)
		if err != nil {
			return TranscodingConfig{}, fmt.Errorf("%s must be a decimal integer", field.name)
		}
	}
	return c, c.Validate()
}

// Validate checks configuration without creating a cache, probing a device, or
// starting FFmpeg. Cache paths use Linux semantics even in a compilation-only
// Windows environment; filesystem ownership is checked by the Linux manager.
func (c TranscodingConfig) Validate() error {
	if c == (TranscodingConfig{}) {
		return nil
	}
	if !path.IsAbs(c.CacheDirectory) || path.Clean(c.CacheDirectory) != c.CacheDirectory || c.CacheDirectory == "/" ||
		len(c.CacheDirectory) > 4096 || !utf8.ValidString(c.CacheDirectory) ||
		strings.Contains(c.CacheDirectory, "\\") || strings.IndexFunc(c.CacheDirectory, unicode.IsControl) >= 0 {
		return fmt.Errorf("GOBY_TRANSCODE_CACHE must be a canonical absolute Linux directory other than the filesystem root")
	}
	for _, field := range []struct {
		name       string
		value      int64
		lowerBound int64
		upperBound int64
	}{
		{"GOBY_TRANSCODE_THREADS", int64(c.Threads), 1, 64},
		{"GOBY_TRANSCODE_MAX_JOBS", int64(c.MaxJobs), 1, 64},
		{"GOBY_TRANSCODE_MAX_USER_JOBS", int64(c.MaxUserJobs), 1, int64(c.MaxJobs)},
		{"GOBY_TRANSCODE_MAX_SESSION_JOBS", int64(c.MaxSessionJobs), 1, int64(c.MaxJobs)},
		{"GOBY_TRANSCODE_MAX_QUEUE_JOBS", int64(c.MaxQueueJobs), 1, 1024},
		{"GOBY_TRANSCODE_MAX_RETAINED_JOBS", int64(c.MaxRetainedJobs), int64(c.MaxJobs), 4096},
		{"GOBY_TRANSCODE_MAX_CACHE_BYTES", c.MaxCacheBytes, 1, maxConfiguredCacheBytes},
		{"GOBY_TRANSCODE_MAX_JOB_BYTES", c.MaxJobBytes, 1, c.MaxCacheBytes},
		{"GOBY_TRANSCODE_MIN_FREE_BYTES", c.MinFreeBytes, 1, maxConfiguredCacheBytes},
	} {
		if field.value < field.lowerBound || field.value > field.upperBound {
			return fmt.Errorf("%s must be between %d and %d", field.name, field.lowerBound, field.upperBound)
		}
	}
	if err := ValidateOutputPlanningLimits(c.MaxBitrate, c.MaxWidth, c.MaxHeight, c.MaxAudioChannels); err != nil {
		return err
	}
	// A minimal encoded-video plan exercises the engine's single hardware
	// validator while leaving source-specific planning to the playback layer.
	if err := transcode.ValidatePlan(transcode.Plan{
		Container: "ts", VideoCodec: "h264", VideoStreamIndex: 0,
		AudioStreamIndex: -1, DurationTicks: 10_000_000, SegmentSeconds: 3,
		Hardware: c.Hardware,
	}); err != nil {
		return fmt.Errorf("GOBY_HW_DECODER, GOBY_HW_ENCODER, or GOBY_HW_DEVICE is invalid: %w", err)
	}
	return nil
}

// ManagerOptions passes explicit resource policy to the engine without opening
// files or starting a manager. Callers must first validate the configuration and
// check Enabled; repository ownership remains the server's responsibility.
func (c TranscodingConfig) ManagerOptions(ffmpegPath string, repository transcode.Repository) transcode.Options {
	return transcode.Options{
		Root: c.CacheDirectory, FFmpegPath: ffmpegPath, Repository: repository,
		Threads: c.Threads, MaxJobs: c.MaxJobs, MaxUserJobs: c.MaxUserJobs,
		MaxSessionJobs: c.MaxSessionJobs, MaxQueueJobs: c.MaxQueueJobs,
		MaxRetainedJobs: c.MaxRetainedJobs, MaxBytes: c.MaxCacheBytes,
		MaxJobBytes: c.MaxJobBytes, MinFreeBytes: c.MinFreeBytes,
	}
}
