package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
)

func observabilityConfigEnvironment(t *testing.T) {
	t.Helper()
	transcodingConfigEnvironment(t)
	for _, name := range []string{
		"GOBY_LOG_DIR", "GOBY_LOG_MAX_FILE_BYTES", "GOBY_LOG_MAX_FILES",
		"GOBY_LOG_RETENTION_DAYS", "GOBY_LOG_MIN_FREE_BYTES", "GOBY_ACTIVITY_RETENTION_DAYS",
		"GOBY_API_KEY_MASTER_KEY_FILE",
	} {
		t.Setenv(name, "")
	}
}

type observabilityNumericSetting struct {
	name             string
	minimum, maximum int64
	read             func(Config) int64
}

func observabilityNumericSettings() []observabilityNumericSetting {
	return []observabilityNumericSetting{
		{"GOBY_LOG_MAX_FILE_BYTES", 8192, 64 << 20, func(c Config) int64 { return c.Diagnostics.MaxFileBytes }},
		{"GOBY_LOG_MAX_FILES", 1, 256, func(c Config) int64 { return int64(c.Diagnostics.MaxFiles) }},
		{"GOBY_LOG_RETENTION_DAYS", 1, 365, func(c Config) int64 { return int64(c.Diagnostics.RetentionDays) }},
		{"GOBY_LOG_MIN_FREE_BYTES", 1, 1 << 40, func(c Config) int64 { return c.Diagnostics.MinFreeBytes }},
		{"GOBY_ACTIVITY_RETENTION_DAYS", 1, 365, func(c Config) int64 { return int64(c.ActivityRetentionDays) }},
	}
}

func TestObservabilityEnvironmentDefaultsAndNumericBoundaries(t *testing.T) {
	observabilityConfigEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := diagnostics.Config{Directory: "/var/log/goby", MaxFileBytes: 4 << 20,
		MaxFiles: 16, RetentionDays: 7, MinFreeBytes: 32 << 20}
	if cfg.Diagnostics != want || cfg.ActivityRetentionDays != 30 {
		t.Fatalf("unexpected observability defaults: diagnostics=%+v activity=%d", cfg.Diagnostics, cfg.ActivityRetentionDays)
	}
	for _, setting := range observabilityNumericSettings() {
		for _, bound := range []struct {
			name  string
			value int64
		}{{"minimum", setting.minimum}, {"maximum", setting.maximum}} {
			t.Run(setting.name+"/"+bound.name, func(t *testing.T) {
				t.Setenv(setting.name, strconv.FormatInt(bound.value, 10))
				cfg, err := Load()
				if err != nil || setting.read(cfg) != bound.value {
					t.Fatalf("inclusive observability boundary was rejected or replaced: value=%d error=%v", setting.read(cfg), err)
				}
			})
		}
	}
}

func TestObservabilityEnvironmentRejectsExplicitZeroAndPrivateInvalidValues(t *testing.T) {
	observabilityConfigEnvironment(t)
	for _, setting := range observabilityNumericSettings() {
		for _, input := range []struct{ name, value string }{
			{"explicit_zero", "0"},
			{"negative", "-1"},
			{"below_minimum", strconv.FormatInt(setting.minimum-1, 10)},
			{"above_maximum", strconv.FormatInt(setting.maximum+1, 10)},
			{"fraction", "1.5"},
			{"leading_space", " 1"},
			{"trailing_space", "1 "},
			{"overflow", "9223372036854775808"},
			{"private_value", "private-configuration-value?api_key=never-echo"},
		} {
			t.Run(setting.name+"/"+input.name, func(t *testing.T) {
				t.Setenv(setting.name, input.value)
				cfg, err := Load()
				want := fmt.Sprintf("%s must be a decimal integer between %d and %d", setting.name, setting.minimum, setting.maximum)
				if err == nil || err.Error() != want {
					t.Fatalf("invalid observability input did not return the fixed setting error: %v", err)
				}
				if cfg.Diagnostics != (diagnostics.Config{}) || cfg.ActivityRetentionDays != 0 {
					t.Fatal("invalid observability environment returned a partial usable policy")
				}
			})
		}
	}
}

func TestObservabilityLinuxDirectoryPolicyDoesNotCreatePaths(t *testing.T) {
	observabilityConfigEnvironment(t)
	for _, directory := range []string{
		"/var/log/goby", "/srv/goby dedicated/logs", "/srv/goby-logs.1", "/srv/application-logs",
	} {
		t.Run(filepath.Base(directory), func(t *testing.T) {
			t.Setenv("GOBY_LOG_DIR", directory)
			cfg, err := Load()
			if err != nil || cfg.Diagnostics.Directory != directory {
				t.Fatalf("canonical absolute Linux diagnostic directory was rejected or rewritten: %v", err)
			}
		})
	}
	t.Run("does_not_create_directory", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("diagnostic filesystem ownership is supported only on Linux")
		}
		missing := filepath.Join(t.TempDir(), "not-created", "diagnostics")
		t.Setenv("GOBY_LOG_DIR", missing)
		if _, err := Load(); err != nil {
			t.Fatalf("loading required an existing diagnostic directory: %v", err)
		}
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("configuration loading created or required its diagnostic directory: %v", err)
		}
	})
	for _, input := range []struct{ name, value string }{
		{"relative", "private-diagnostic-directory/logs"},
		{"working_directory", "."},
		{"root", "/"},
		{"parent_segment", "/var/log/private-directory/../logs"},
		{"dot_segment", "/var/log/./private-directory"},
		{"trailing_separator", "/var/log/private-directory/"},
		{"double_separator", "/var//log/private-directory"},
		{"oversized", "/" + strings.Repeat("p", 4096)},
		{"windows_path", `C:\private-directory\logs`},
	} {
		t.Run(input.name, func(t *testing.T) {
			t.Setenv("GOBY_LOG_DIR", input.value)
			_, err := Load()
			if err == nil || err.Error() != "GOBY_LOG_DIR must be a clean absolute dedicated directory" {
				t.Fatalf("invalid path did not return the fixed directory error: %v", err)
			}
		})
	}
}

func TestObservabilityDirectConfigurationDefaultsAndInvalidPolicies(t *testing.T) {
	base := Config{DatabaseURL: "postgres://goby:fixture@localhost:5432/goby", PublicURL: "http://localhost:8096",
		ServerName: "Goby", StartupTimeout: time.Minute}
	if err := base.Validate(); err != nil {
		t.Fatalf("direct zero-value observability policy did not adopt defaults: %v", err)
	}
	if base.Diagnostics != (diagnostics.Config{}) || base.ActivityRetentionDays != 0 {
		t.Fatal("validation mutated the caller's directly constructed configuration")
	}
	partial := base
	partial.Diagnostics = diagnostics.Config{Directory: "/srv/goby/direct-logs", MaxFiles: 2}
	if err := partial.Validate(); err != nil {
		t.Fatalf("partially specified direct diagnostic policy did not adopt remaining defaults: %v", err)
	}
	for _, days := range []int{1, 365} {
		cfg := base
		cfg.ActivityRetentionDays = days
		if err := cfg.Validate(); err != nil {
			t.Fatalf("direct activity retention boundary %d was rejected: %v", days, err)
		}
	}
	for _, test := range []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"null_byte_directory", func(c *Config) { c.Diagnostics.Directory = "/var/log/private-directory\x00secret" }, "GOBY_LOG_DIR must be a clean absolute dedicated directory"},
		{"file_too_small", func(c *Config) { c.Diagnostics.MaxFileBytes = 8191 }, "diagnostic log policy exceeds the supported bounds"},
		{"file_too_large", func(c *Config) { c.Diagnostics.MaxFileBytes = 64<<20 + 1 }, "diagnostic log policy exceeds the supported bounds"},
		{"negative_file_count", func(c *Config) { c.Diagnostics.MaxFiles = -1 }, "diagnostic log policy exceeds the supported bounds"},
		{"excessive_file_count", func(c *Config) { c.Diagnostics.MaxFiles = 257 }, "diagnostic log policy exceeds the supported bounds"},
		{"negative_log_retention", func(c *Config) { c.Diagnostics.RetentionDays = -1 }, "diagnostic log policy exceeds the supported bounds"},
		{"excessive_log_retention", func(c *Config) { c.Diagnostics.RetentionDays = 366 }, "diagnostic log policy exceeds the supported bounds"},
		{"negative_disk_reserve", func(c *Config) { c.Diagnostics.MinFreeBytes = -1 }, "diagnostic log policy exceeds the supported bounds"},
		{"excessive_disk_reserve", func(c *Config) { c.Diagnostics.MinFreeBytes = 1<<40 + 1 }, "diagnostic log policy exceeds the supported bounds"},
		{"negative_activity_retention", func(c *Config) { c.ActivityRetentionDays = -1 }, "GOBY_ACTIVITY_RETENTION_DAYS must be between 1 and 365"},
		{"excessive_activity_retention", func(c *Config) { c.ActivityRetentionDays = 366 }, "GOBY_ACTIVITY_RETENTION_DAYS must be between 1 and 365"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			test.edit(&cfg)
			if err := cfg.Validate(); err == nil || err.Error() != test.want {
				t.Fatalf("invalid direct observability policy did not return its fixed error: %v", err)
			}
		})
	}
}
