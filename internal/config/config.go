package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
)

const GoVersion = "1.27.1"
const FFmpegVersion = "9.0.1"

type Config struct {
	ListenAddress         string
	DatabaseURL           string
	PublicURL             string
	ServerName            string
	SetupToken            string
	CookieSecure          bool
	WebDirectory          string
	FFmpegPath            string
	FFprobePath           string
	TrustedProxies        []netip.Prefix
	MediaRoots            []string
	StartupTimeout        time.Duration
	APIKeyMasterKeyFile   string
	Transcoding           TranscodingConfig
	Diagnostics           diagnostics.Config
	ActivityRetentionDays int
}

func Load() (Config, error) {
	c := Config{
		ListenAddress:       env("GOBY_LISTEN", ":8096"),
		DatabaseURL:         os.Getenv("GOBY_DATABASE_URL"),
		PublicURL:           strings.TrimRight(env("GOBY_PUBLIC_URL", "http://localhost:8096"), "/"),
		ServerName:          env("GOBY_SERVER_NAME", "Goby"),
		SetupToken:          os.Getenv("GOBY_SETUP_TOKEN"),
		WebDirectory:        env("GOBY_WEB_DIR", "web/admin/dist"),
		FFmpegPath:          env("GOBY_FFMPEG", "ffmpeg"),
		FFprobePath:         env("GOBY_FFPROBE", "ffprobe"),
		MediaRoots:          filepath.SplitList(os.Getenv("GOBY_MEDIA_ROOTS")),
		APIKeyMasterKeyFile: env("GOBY_API_KEY_MASTER_KEY_FILE", "application-key-master.key"),
	}
	var err error
	for _, entry := range strings.Split(os.Getenv("GOBY_TRUSTED_PROXIES"), ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		prefix, parseErr := netip.ParsePrefix(entry)
		if parseErr != nil {
			return Config{}, fmt.Errorf("GOBY_TRUSTED_PROXIES must contain comma-separated IP CIDRs")
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
	}
	c.CookieSecure, err = strconv.ParseBool(env("GOBY_COOKIE_SECURE", "true"))
	if err != nil {
		return Config{}, fmt.Errorf("GOBY_COOKIE_SECURE must be a boolean")
	}
	c.StartupTimeout, err = time.ParseDuration(env("GOBY_STARTUP_TIMEOUT", "5m"))
	if err != nil {
		return Config{}, fmt.Errorf("GOBY_STARTUP_TIMEOUT must be a Go duration between 1s and 30m")
	}
	c.Transcoding, err = loadTranscoding()
	if err != nil {
		return Config{}, err
	}
	c.Diagnostics, c.ActivityRetentionDays, err = loadObservability()
	if err != nil {
		return Config{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	c.WebDirectory, err = filepath.Abs(c.WebDirectory)
	if err != nil {
		return Config{}, err
	}
	c.APIKeyMasterKeyFile, err = filepath.Abs(c.APIKeyMasterKeyFile)
	return c, err
}

func (c Config) Validate() error {
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return fmt.Errorf("GOBY_DATABASE_URL must be a PostgreSQL connection URL")
	}
	u, err = url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("GOBY_PUBLIC_URL must be an HTTP(S) origin without credentials, query, or path")
	}
	if err := ValidateServerName(c.ServerName); err != nil {
		return err
	}
	if c.SetupToken != "" && len(c.SetupToken) < 24 {
		return fmt.Errorf("GOBY_SETUP_TOKEN must contain at least 24 bytes")
	}
	if c.StartupTimeout < time.Second || c.StartupTimeout > 30*time.Minute {
		return fmt.Errorf("GOBY_STARTUP_TIMEOUT must be a Go duration between 1s and 30m")
	}
	if strings.ContainsRune(c.APIKeyMasterKeyFile, '\x00') || len(c.APIKeyMasterKeyFile) > 4096 ||
		(c.APIKeyMasterKeyFile != "" && (strings.TrimSpace(c.APIKeyMasterKeyFile) == "" || filepath.Base(c.APIKeyMasterKeyFile) == "." || filepath.Base(c.APIKeyMasterKeyFile) == string(filepath.Separator))) {
		return fmt.Errorf("GOBY_API_KEY_MASTER_KEY_FILE must name a bounded persistent key file")
	}
	if err := validateObservability(c.Diagnostics.WithDefaults(), c.ActivityRetentionDays); err != nil {
		return err
	}
	return c.Transcoding.Validate()
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
