package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadApplicationKeyMasterFileResolvesPathsWithoutCreatingFiles(t *testing.T) {
	transcodingConfigEnvironment(t)
	t.Setenv("GOBY_WEB_DIR", "web/admin/dist")
	t.Chdir(t.TempDir())
	for _, test := range []struct {
		name, value, relative string
	}{
		{name: "default", relative: "application-key-master.key"},
		{name: "relative", value: filepath.Join("secrets", "master.key"), relative: filepath.Join("secrets", "master.key")},
		{name: "absolute", value: filepath.Join(t.TempDir(), "master.key")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOBY_API_KEY_MASTER_KEY_FILE", test.value)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("load application key master file: %v", err)
			}
			want := test.value
			if test.relative != "" {
				want, err = filepath.Abs(test.relative)
				if err != nil {
					t.Fatalf("resolve expected master path: %v", err)
				}
			}
			if cfg.APIKeyMasterKeyFile != want || !filepath.IsAbs(cfg.APIKeyMasterKeyFile) {
				t.Errorf("master file = %q, want absolute path %q", cfg.APIKeyMasterKeyFile, want)
			}
			if _, err := os.Stat(want); !os.IsNotExist(err) {
				t.Errorf("loading configuration created or accessed the master file: %v", err)
			}
		})
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Errorf("configuration loading modified the working directory: entries = %v, error = %v", entries, err)
	}
}

func TestApplicationKeyMasterFileValidationRejectsInvalidPathsWithoutIO(t *testing.T) {
	base := Config{
		DatabaseURL: "postgres://goby:fixture@localhost:5432/goby", PublicURL: "http://localhost:8096",
		ServerName: "Goby", StartupTimeout: time.Minute,
	}
	for _, test := range []struct{ name, value string }{
		{name: "null_byte", value: "master\x00.key"},
		{name: "oversized", value: strings.Repeat("a", 4097)},
		{name: "whitespace", value: " \t "},
		{name: "working_directory", value: "."},
		{name: "filesystem_root", value: string(filepath.Separator)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.APIKeyMasterKeyFile = test.value
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "GOBY_API_KEY_MASTER_KEY_FILE") {
				t.Errorf("invalid master file error = %v, want a named configuration error", err)
			}
		})
	}
	transcodingConfigEnvironment(t)
	t.Chdir(t.TempDir())
	for _, value := range []string{".", string(filepath.Separator), " ", strings.Repeat("a", 4097)} {
		t.Setenv("GOBY_API_KEY_MASTER_KEY_FILE", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "GOBY_API_KEY_MASTER_KEY_FILE") {
			t.Errorf("Load with invalid master path returned %v", err)
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil || len(entries) != 0 {
		t.Errorf("invalid configuration created files: entries = %v, error = %v", entries, err)
	}
}
