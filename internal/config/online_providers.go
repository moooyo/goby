package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const maxProviderCredentialBytes = 16384

// OnlineProvidersConfig contains startup-only operator credentials. Managed
// settings independently control whether internet providers may be used.
type OnlineProvidersConfig struct {
	Enabled                bool   `json:"-"`
	TMDBToken              string `json:"-"`
	MusicBrainzUserAgent   string `json:"-"`
	OpenSubtitlesAPIKey    string `json:"-"`
	OpenSubtitlesUsername  string `json:"-"`
	OpenSubtitlesPassword  string `json:"-"`
	OpenSubtitlesUserAgent string `json:"-"`
}

// String and GoString prevent accidental structured configuration logging from
// disclosing credentials. Public provider status is constructed separately.
func (OnlineProvidersConfig) String() string   { return "<online-provider configuration>" }
func (OnlineProvidersConfig) GoString() string { return "<online-provider configuration>" }

func loadOnlineProviders() (OnlineProvidersConfig, error) {
	var result OnlineProvidersConfig
	var err error
	result.Enabled, err = strconv.ParseBool(env("GOBY_ONLINE_PROVIDERS_ENABLED", "false"))
	if err != nil {
		return OnlineProvidersConfig{}, errors.New("GOBY_ONLINE_PROVIDERS_ENABLED must be a boolean")
	}
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"GOBY_TMDB_TOKEN", &result.TMDBToken},
		{"GOBY_OPENSUBTITLES_API_KEY", &result.OpenSubtitlesAPIKey},
		{"GOBY_OPENSUBTITLES_USERNAME", &result.OpenSubtitlesUsername},
		{"GOBY_OPENSUBTITLES_PASSWORD", &result.OpenSubtitlesPassword},
	} {
		*field.value, err = loadPrivateSetting(field.name)
		if err != nil {
			return OnlineProvidersConfig{}, err
		}
	}
	result.MusicBrainzUserAgent = env("GOBY_MUSICBRAINZ_USER_AGENT", "Goby/0.1 (https://github.com/moooyo/goby)")
	result.OpenSubtitlesUserAgent = env("GOBY_OPENSUBTITLES_USER_AGENT", "Goby v0.1")
	return result, result.Validate()
}

func (c OnlineProvidersConfig) Validate() error {
	for _, value := range []string{c.TMDBToken, c.OpenSubtitlesAPIKey, c.OpenSubtitlesUsername, c.OpenSubtitlesPassword} {
		if !validPrivateSetting(value) {
			return errors.New("provider credentials must be bounded single-line values")
		}
	}
	for _, value := range []string{c.MusicBrainzUserAgent, c.OpenSubtitlesUserAgent} {
		if len(value) > 512 || !validPrivateSetting(value) {
			return errors.New("provider user agents must be bounded single-line values")
		}
	}
	if (c.OpenSubtitlesUsername == "") != (c.OpenSubtitlesPassword == "") {
		return errors.New("OpenSubtitles username and password must be configured together")
	}
	return nil
}

func validPrivateSetting(value string) bool {
	if len(value) > maxProviderCredentialBytes {
		return false
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return false
		}
	}
	return true
}

func loadPrivateSetting(name string) (string, error) {
	value, file := os.Getenv(name), os.Getenv(name+"_FILE")
	if value != "" && file != "" {
		return "", fmt.Errorf("configure either %s or %s_FILE", name, name)
	}
	if file != "" {
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s_FILE must name a readable regular file", name)
		}
		input, err := os.Open(file)
		if err != nil {
			return "", fmt.Errorf("cannot read %s_FILE", name)
		}
		defer input.Close()
		opened, err := input.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			return "", fmt.Errorf("%s_FILE changed while opening", name)
		}
		data, err := io.ReadAll(io.LimitReader(input, maxProviderCredentialBytes+3))
		if err != nil {
			return "", fmt.Errorf("cannot read %s_FILE", name)
		}
		value = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
		if value == "" {
			return "", fmt.Errorf("%s_FILE must not be empty", name)
		}
	}
	if !validPrivateSetting(value) {
		return "", fmt.Errorf("%s must be a bounded single-line value", name)
	}
	return value, nil
}
