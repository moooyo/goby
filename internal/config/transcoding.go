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
	Enabled               bool
	CacheDirectory        string
	Threads               int
	MaxJobs               int
	MaxUserJobs           int
	MaxSessionJobs        int
	MaxQueueJobs          int
	MaxRetainedJobs       int
	MaxCacheBytes         int64
	MaxJobBytes           int64
	MinFreeBytes          int64
	MaxBitrate            int64
	MaxWidth              int
	MaxHeight             int
	MaxAudioChannels      int
	Hardware              transcode.Hardware
	Execution             transcode.ExecutionOptions
	AllowedAMDDevices     [8]string
	AllowedAMDDeviceCount int
	// HardwareUnavailable is set only on a private runtime planning copy.
	HardwareUnavailable bool
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
	c.Execution = transcode.DefaultExecutionOptions(c.Threads)
	if value := env("GOBY_ALLOWED_AMD_DEVICES", ""); value != "" {
		devices := strings.Split(value, ",")
		if len(devices) > len(c.AllowedAMDDevices) {
			return TranscodingConfig{}, fmt.Errorf("GOBY_ALLOWED_AMD_DEVICES must contain at most %d devices", len(c.AllowedAMDDevices))
		}
		copy(c.AllowedAMDDevices[:], devices)
		c.AllowedAMDDeviceCount = len(devices)
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
	if c.Execution != (transcode.ExecutionOptions{}) {
		if err := transcode.ValidateExecutionOptions(c.Execution); err != nil {
			return fmt.Errorf("transcoding execution options are invalid: %w", err)
		}
	}
	if _, err := c.AuthorizedAMDDevices(); err != nil {
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

// ValidAMDDevicePath accepts only the closed set of Linux render node names.
// This is a syntax check; the server separately verifies node and vendor identity.
func ValidAMDDevicePath(device string) bool {
	const prefix = "/dev/dri/renderD"
	if !strings.HasPrefix(device, prefix) {
		return false
	}
	number := strings.TrimPrefix(device, prefix)
	value, err := strconv.Atoi(number)
	return err == nil && value >= 128 && value <= 255 && strconv.Itoa(value) == number
}

// AuthorizedAMDDevices returns a private union of explicit startup authorization,
// the existing VAAPI startup device, and an explicit software filter device from
// directly constructed configurations. It never discovers devices by scanning.
func (c TranscodingConfig) AuthorizedAMDDevices() ([]string, error) {
	invalid := func() ([]string, error) {
		return nil, fmt.Errorf("GOBY_ALLOWED_AMD_DEVICES must contain at most %d unique canonical /dev/dri/renderD128 through /dev/dri/renderD255 paths without whitespace or empty entries", len(c.AllowedAMDDevices))
	}
	if c.AllowedAMDDeviceCount < 0 || c.AllowedAMDDeviceCount > len(c.AllowedAMDDevices) {
		return invalid()
	}
	devices := make([]string, 0, len(c.AllowedAMDDevices))
	seen := make(map[string]bool, len(c.AllowedAMDDevices))
	for index, device := range c.AllowedAMDDevices {
		if index >= c.AllowedAMDDeviceCount {
			if device != "" {
				return invalid()
			}
			continue
		}
		if !ValidAMDDevicePath(device) || seen[device] {
			return invalid()
		}
		seen[device] = true
		devices = append(devices, device)
	}
	vaapi := c.Hardware.Decode == "vaapi" || c.Hardware.Encode == "vaapi"
	softwareDevice := (c.Hardware.Decode == "" || c.Hardware.Decode == "software") &&
		(c.Hardware.Encode == "" || c.Hardware.Encode == "software") && c.Hardware.Device != ""
	if vaapi || softwareDevice {
		device := c.Hardware.Device
		if device == "" {
			device = "/dev/dri/renderD128"
		}
		if !ValidAMDDevicePath(device) {
			return nil, fmt.Errorf("GOBY_HW_DEVICE must be a canonical /dev/dri/renderD128 through /dev/dri/renderD255 path for AMD processing")
		}
		if !seen[device] {
			if len(devices) == len(c.AllowedAMDDevices) {
				return invalid()
			}
			devices = append(devices, device)
		}
	}
	return devices, nil
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
