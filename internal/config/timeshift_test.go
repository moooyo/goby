package config

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/timeshift"
)

func timeshiftConfigEnvironment(t *testing.T) {
	t.Helper()
	recoveryConfigEnvironment(t)
	for _, name := range []string{
		"GOBY_TIMESHIFT_ENABLED", "GOBY_TIMESHIFT_CACHE", "GOBY_TIMESHIFT_WINDOW_SECONDS",
		"GOBY_TIMESHIFT_MAX_WINDOW_BYTES", "GOBY_TIMESHIFT_MAX_CACHE_BYTES",
		"GOBY_TIMESHIFT_MAX_WINDOWS", "GOBY_TIMESHIFT_MAX_USER_WINDOWS", "GOBY_DYNAMIC_SOURCES_FILE",
	} {
		t.Setenv(name, "")
	}
}

func TestTimeshiftEnvironmentDefaultsAndExplicitDisable(t *testing.T) {
	timeshiftConfigEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := TimeshiftConfig{Enabled: true, CacheDirectory: "/var/cache/goby/timeshift",
		WindowSeconds: 600, MaxWindowBytes: 512 << 20, MaxCacheBytes: 2 << 30,
		MaxWindows: 32, MaxUserWindows: 4}
	if cfg.Timeshift != want || DefaultTimeshiftConfig() != want {
		t.Fatalf("unexpected timeshift defaults: %+v", cfg.Timeshift)
	}
	t.Setenv("GOBY_TIMESHIFT_ENABLED", "false")
	disabled, err := Load()
	if err != nil || disabled.Timeshift.Enabled {
		t.Fatalf("explicitly disabled retention was not preserved: %v", err)
	}
	disabled.Timeshift.Enabled = true
	if disabled.Timeshift != want {
		t.Fatal("disabling retention discarded its bounded policy")
	}
	t.Setenv("GOBY_TIMESHIFT_ENABLED", "")
	t.Setenv("GOBY_TRANSCODING_ENABLED", "false")
	cfg, err = Load()
	if err != nil || cfg.Transcoding.Enabled || cfg.Timeshift != want {
		t.Fatalf("conversion disable must not silently rewrite the retention policy: %v", err)
	}
}

func TestTimeshiftConstructedZeroDoesNotEnableRetention(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://goby:fixture@localhost:5432/goby", PublicURL: "http://localhost:8096",
		ServerName: "Goby", StartupTimeout: time.Minute}
	if err := cfg.Validate(); err != nil || cfg.Timeshift != (TimeshiftConfig{}) {
		t.Fatalf("direct zero configuration must remain disabled: %v", err)
	}
	for _, policy := range []TimeshiftConfig{{Enabled: true}, {CacheDirectory: "/owned/timeshift"}} {
		cfg.Timeshift = policy
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "GOBY_TIMESHIFT_") {
			t.Fatalf("incomplete nonzero retention configuration was accepted: %v", err)
		}
	}
}

func TestTimeshiftEnvironmentRejectsMalformedAndUnboundedSettings(t *testing.T) {
	timeshiftConfigEnvironment(t)
	for _, test := range []struct {
		name   string
		values []string
	}{
		{"GOBY_TIMESHIFT_ENABLED", []string{"enabled", "yes", " true "}},
		{"GOBY_TIMESHIFT_CACHE", []string{".", "relative/cache", "/", "/cache/", "//cache", "/cache/../window", `C:\cache`, "/cache\\child", "/cache\nchild", strings.Repeat("/a", 2049)}},
		{"GOBY_TIMESHIFT_WINDOW_SECONDS", []string{"0", "-1", "86401", "1.5", " 600 ", "9223372036854775808"}},
		{"GOBY_TIMESHIFT_MAX_CACHE_BYTES", []string{"0", "-1", "2GiB", "1099511627777", "9223372036854775808"}},
		{"GOBY_TIMESHIFT_MAX_WINDOW_BYTES", []string{"0", "-1", "2147483649", "512MiB", "9223372036854775808"}},
		{"GOBY_TIMESHIFT_MAX_WINDOWS", []string{"0", "-1", "129", "4.5", " 32 "}},
		{"GOBY_TIMESHIFT_MAX_USER_WINDOWS", []string{"0", "-1", "33", "four"}},
	} {
		for index, value := range test.values {
			t.Run(fmt.Sprintf("%s/%d", test.name, index), func(t *testing.T) {
				t.Setenv(test.name, value)
				if _, err := Load(); err == nil || !strings.Contains(err.Error(), test.name) {
					t.Fatalf("configuration error = %v, want named setting %s", err, test.name)
				}
			})
		}
	}
	t.Setenv("GOBY_TIMESHIFT_ENABLED", "false")
	t.Setenv("GOBY_TIMESHIFT_MAX_WINDOW_BYTES", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_TIMESHIFT_MAX_WINDOW_BYTES") {
		t.Fatalf("disabled retention hid an invalid explicit budget: %v", err)
	}
}

func TestTimeshiftLimitsPreserveBothInclusiveBoundaries(t *testing.T) {
	for _, policy := range []TimeshiftConfig{
		{Enabled: true, CacheDirectory: "/owned/timeshift", WindowSeconds: 1,
			MaxWindowBytes: 1, MaxCacheBytes: 1, MaxWindows: 1, MaxUserWindows: 1},
		{Enabled: true, CacheDirectory: "/owned/timeshift", WindowSeconds: 86400,
			MaxWindowBytes: 1 << 40, MaxCacheBytes: 1 << 40, MaxWindows: 128, MaxUserWindows: 128},
	} {
		if err := policy.Validate(); err != nil {
			t.Fatalf("inclusive retention boundary was rejected: %v", err)
		}
	}
	for _, directory := range []string{"/owned\x00window", "/owned\rwindow", "/owned/" + string([]byte{0xff})} {
		policy := DefaultTimeshiftConfig()
		policy.CacheDirectory = directory
		if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "GOBY_TIMESHIFT_CACHE") {
			t.Fatal("direct retention configuration accepted an invalid path byte sequence")
		}
	}
}

func TestTimeshiftCacheCannotOverlapConversionCache(t *testing.T) {
	timeshiftConfigEnvironment(t)
	base, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		for _, roots := range []struct{ retained, converted string }{
			{"/owned/cache", "/owned/cache"},
			{"/owned/cache", "/owned/cache/transcodes"},
			{"/owned/cache/timeshift", "/owned/cache"},
		} {
			cfg := base
			cfg.Timeshift.Enabled, cfg.Transcoding.Enabled = enabled, enabled
			cfg.Timeshift.CacheDirectory, cfg.Transcoding.CacheDirectory = roots.retained, roots.converted
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "GOBY_TIMESHIFT_CACHE") || !strings.Contains(err.Error(), "GOBY_TRANSCODE_CACHE") {
				t.Fatalf("overlapping retention/conversion caches were accepted: %v", err)
			}
		}
	}
	for _, roots := range []struct{ retained, converted string }{
		{"/owned/cache", "/owned/cache-transcodes"},
		{"/owned/cache/timeshift", "/owned/cache/transcodes"},
	} {
		cfg := base
		cfg.Timeshift.CacheDirectory, cfg.Transcoding.CacheDirectory = roots.retained, roots.converted
		if err := cfg.Validate(); err != nil {
			t.Fatalf("disjoint retention/conversion caches were rejected: %v", err)
		}
	}
	t.Setenv("GOBY_TIMESHIFT_CACHE", "/var/cache/goby/transcodes/timeshift")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_TIMESHIFT_CACHE") {
		t.Fatalf("Load did not apply the cache ownership boundary: %v", err)
	}
}

func TestTimeshiftEnvironmentOptionsPreservePolicyAndCopyVariants(t *testing.T) {
	timeshiftConfigEnvironment(t)
	for name, value := range map[string]string{
		"GOBY_TIMESHIFT_CACHE": "/owned/retained", "GOBY_TIMESHIFT_WINDOW_SECONDS": "1800",
		"GOBY_TIMESHIFT_MAX_WINDOW_BYTES": "1048576", "GOBY_TIMESHIFT_MAX_CACHE_BYTES": "8388608",
		"GOBY_TIMESHIFT_MAX_WINDOWS": "8", "GOBY_TIMESHIFT_MAX_USER_WINDOWS": "2",
	} {
		t.Setenv(name, value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	options := cfg.Timeshift.Options()
	if options.Root != "/owned/retained" || options.MaxBytes != 8<<20 || options.MaxWindows != 8 || options.MaxOwnerWindows != 2 {
		t.Fatalf("store options changed explicit resource limits: %+v", options)
	}
	variants := []timeshift.Variant{{ID: "video", Kind: "video", Format: "fmp4"}, {ID: "caption", Kind: "subtitle", Format: "vtt"}}
	window := cfg.Timeshift.WindowOptions(variants)
	if window.Window != 30*time.Minute || window.MaxBytes != 1<<20 || !reflect.DeepEqual(window.Variants, variants) {
		t.Fatalf("window options changed retention or renditions: %+v", window)
	}
	variants[0].ID = "caller-mutated"
	if window.Variants[0].ID != "video" {
		t.Fatal("window options retained the caller's mutable rendition slice")
	}
	window.Variants[1].ID = "window-mutated"
	if variants[1].ID != "caption" {
		t.Fatal("window options mutated the caller's selected renditions")
	}
}
