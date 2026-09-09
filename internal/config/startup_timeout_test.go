package config

import (
	"strings"
	"testing"
	"time"
)

func startupConfigEnvironment(t *testing.T) {
	t.Helper()
	for name, value := range map[string]string{
		"GOBY_DATABASE_URL": "postgres://goby:fixture@localhost:5432/goby?sslmode=disable",
		"GOBY_PUBLIC_URL": "http://localhost:8096",
		"GOBY_SERVER_NAME": "Goby",
		"GOBY_SETUP_TOKEN": "",
		"GOBY_COOKIE_SECURE": "true",
		"GOBY_TRUSTED_PROXIES": "",
		"GOBY_MEDIA_ROOTS": "",
	} {
		t.Setenv(name, value)
	}
}

func TestLoadStartupTimeoutDefaultsAndValidDurations(t *testing.T) {
	startupConfigEnvironment(t)
	for _, test := range []struct {
		value string
		want time.Duration
	}{
		{value: "", want: 5 * time.Minute},
		{value: "1s", want: time.Second},
		{value: "1500ms", want: 1500 * time.Millisecond},
		{value: "4m30s", want: 4*time.Minute + 30*time.Second},
		{value: "30m", want: 30 * time.Minute},
	} {
		t.Run(test.value, func(t *testing.T) {
			t.Setenv("GOBY_STARTUP_TIMEOUT", test.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("load valid startup duration: %v", err)
			}
			if cfg.StartupTimeout != test.want {
				t.Errorf("StartupTimeout = %s, want %s", cfg.StartupTimeout, test.want)
			}
		})
	}
}

func TestLoadStartupTimeoutRejectsInvalidOrOutOfRangeValues(t *testing.T) {
	startupConfigEnvironment(t)
	for _, value := range []string{
		"not-a-duration", "60", "5minutes", "1h", "0", "-1s", "999ms", "30m1ns", " 5m ", "9223372036854775808ns",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("GOBY_STARTUP_TIMEOUT", value)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_STARTUP_TIMEOUT") {
				t.Errorf("startup duration %q error = %v, want a named configuration error", value, err)
			}
		})
	}
}

func TestValidateStartupTimeoutEnforcesBothBounds(t *testing.T) {
	base := Config{DatabaseURL: "postgres://goby:fixture@localhost:5432/goby", PublicURL: "http://localhost:8096", ServerName: "Goby"}
	for _, timeout := range []time.Duration{0, time.Second - time.Nanosecond, 30*time.Minute + time.Nanosecond} {
		cfg := base
		cfg.StartupTimeout = timeout
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "GOBY_STARTUP_TIMEOUT") {
			t.Errorf("Validate with timeout %s returned %v, want a startup timeout error", timeout, err)
		}
	}
	for _, timeout := range []time.Duration{time.Second, 30 * time.Minute} {
		cfg := base
		cfg.StartupTimeout = timeout
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate rejected inclusive timeout bound %s: %v", timeout, err)
		}
	}
}
