package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/timeshift"
)

// TimeshiftConfig bounds retained dynamic media independently of conversion
// scratch space. Load enables it by default. A directly constructed zero value
// remains disabled; callers must never interpret disabled retention as an
// instruction to use an unbounded dynamic-output cache.
type TimeshiftConfig struct {
	Enabled        bool
	CacheDirectory string
	WindowSeconds  int
	MaxWindowBytes int64
	MaxCacheBytes  int64
	MaxWindows     int
	MaxUserWindows int
}

// DefaultTimeshiftConfig returns the production policy without reading the
// environment or creating a store. Direct callers explicitly opt in by using
// this value; validation never supplies defaults to an incomplete policy.
func DefaultTimeshiftConfig() TimeshiftConfig {
	return TimeshiftConfig{
		Enabled: true, CacheDirectory: "/var/cache/goby/timeshift",
		WindowSeconds: 600, MaxWindowBytes: 512 << 20, MaxCacheBytes: 2 << 30,
		MaxWindows: 32, MaxUserWindows: 4,
	}
}

func loadTimeshift() (TimeshiftConfig, error) {
	c := DefaultTimeshiftConfig()
	c.CacheDirectory = env("GOBY_TIMESHIFT_CACHE", c.CacheDirectory)
	var err error
	c.Enabled, err = strconv.ParseBool(env("GOBY_TIMESHIFT_ENABLED", "true"))
	if err != nil {
		return TimeshiftConfig{}, fmt.Errorf("GOBY_TIMESHIFT_ENABLED must be a boolean")
	}
	for _, field := range []struct {
		name  string
		value *int
	}{
		{"GOBY_TIMESHIFT_WINDOW_SECONDS", &c.WindowSeconds},
		{"GOBY_TIMESHIFT_MAX_WINDOWS", &c.MaxWindows},
		{"GOBY_TIMESHIFT_MAX_USER_WINDOWS", &c.MaxUserWindows},
	} {
		*field.value, err = strconv.Atoi(env(field.name, strconv.Itoa(*field.value)))
		if err != nil {
			return TimeshiftConfig{}, fmt.Errorf("%s must be a decimal integer", field.name)
		}
	}
	for _, field := range []struct {
		name  string
		value *int64
	}{
		{"GOBY_TIMESHIFT_MAX_WINDOW_BYTES", &c.MaxWindowBytes},
		{"GOBY_TIMESHIFT_MAX_CACHE_BYTES", &c.MaxCacheBytes},
	} {
		*field.value, err = strconv.ParseInt(env(field.name, strconv.FormatInt(*field.value, 10)), 10, 64)
		if err != nil {
			return TimeshiftConfig{}, fmt.Errorf("%s must be a decimal integer", field.name)
		}
	}
	return c, c.Validate()
}

// Validate uses Linux directory syntax on every host. Actual directory
// ownership, symlink handling and exclusive locking belong to the store.
// A nonzero disabled policy is still validated so disabling a feature cannot
// hide invalid limits that would become unsafe on a later explicit enable.
func (c TimeshiftConfig) Validate() error {
	if c == (TimeshiftConfig{}) {
		return nil
	}
	if !recoveryDirectory(c.CacheDirectory) {
		return fmt.Errorf("GOBY_TIMESHIFT_CACHE must be a bounded canonical absolute Linux directory other than the filesystem root")
	}
	// Keep these bounds within the store's resource and retention limits.
	for _, field := range []struct {
		name       string
		value      int64
		lowerBound int64
		upperBound int64
	}{
		{"GOBY_TIMESHIFT_WINDOW_SECONDS", int64(c.WindowSeconds), 1, 24 * 60 * 60},
		{"GOBY_TIMESHIFT_MAX_CACHE_BYTES", c.MaxCacheBytes, 1, 1 << 40},
		{"GOBY_TIMESHIFT_MAX_WINDOW_BYTES", c.MaxWindowBytes, 1, c.MaxCacheBytes},
		{"GOBY_TIMESHIFT_MAX_WINDOWS", int64(c.MaxWindows), 1, 128},
		{"GOBY_TIMESHIFT_MAX_USER_WINDOWS", int64(c.MaxUserWindows), 1, int64(c.MaxWindows)},
	} {
		if field.value < field.lowerBound || field.value > field.upperBound {
			return fmt.Errorf("%s must be between %d and %d", field.name, field.lowerBound, field.upperBound)
		}
	}
	return nil
}

func (c Config) validateTimeshift() error {
	if err := c.Timeshift.Validate(); err != nil {
		return err
	}
	if c.Timeshift == (TimeshiftConfig{}) || c.Transcoding.CacheDirectory == "" {
		return nil
	}
	if recoveryDirectoriesOverlap(c.Timeshift.CacheDirectory, c.Transcoding.CacheDirectory) {
		return fmt.Errorf("GOBY_TIMESHIFT_CACHE and GOBY_TRANSCODE_CACHE must be separate non-overlapping directories")
	}
	return nil
}

// Options passes only the configured resource policy. Callers must validate
// the configuration and require both Enabled and enabled conversion before
// creating a store for configured dynamic sources.
func (c TimeshiftConfig) Options() timeshift.Options {
	return timeshift.Options{
		Root: c.CacheDirectory, MaxBytes: c.MaxCacheBytes,
		MaxWindows: c.MaxWindows, MaxOwnerWindows: c.MaxUserWindows,
	}
}

// WindowOptions copies the selected rendition set and applies the explicit
// per-window bounds. It does not authorize a source or create a presentation.
func (c TimeshiftConfig) WindowOptions(variants []timeshift.Variant) timeshift.WindowOptions {
	return timeshift.WindowOptions{
		Window: time.Duration(c.WindowSeconds) * time.Second, MaxBytes: c.MaxWindowBytes,
		Variants: append([]timeshift.Variant(nil), variants...),
	}
}
