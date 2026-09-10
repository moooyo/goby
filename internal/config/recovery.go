package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/backupstore"
)

const (
	DefaultRecoveryDirectory        = "/var/lib/goby/recovery"
	DefaultRecoveryOperationTimeout = 30 * time.Minute
	BackupDefaultsVersion           = 1
	MaxBackupDefaultsBytes          = 4096
)

var ErrInvalidBackupDefaults = errors.New("invalid backup deployment defaults")

// RecoveryConfig describes persistent recovery state and bounded backup work.
// Load supplies this policy for production startup. A directly constructed zero
// value remains valid without opening stores or opting fixtures into recovery.
type RecoveryConfig struct {
	Directory           string
	OperationsDirectory string
	Backups             backupstore.Config
	DatabaseURL         string
	PGDumpPath          string
	PGRestorePath       string
	OperationTimeout    time.Duration
}

func (c RecoveryConfig) WithDefaults() RecoveryConfig {
	if c.Directory == "" {
		c.Directory = DefaultRecoveryDirectory
	}
	// Keep the private operation journal beside the final state directory,
	// never inside a store whose ownership rules require a separate root.
	if c.OperationsDirectory == "" {
		c.OperationsDirectory = c.Directory + "-operations"
	}
	c.Backups = c.Backups.WithDefaults()
	if c.PGDumpPath == "" {
		c.PGDumpPath = "pg_dump"
	}
	if c.PGRestorePath == "" {
		c.PGRestorePath = "pg_restore"
	}
	if c.OperationTimeout == 0 {
		c.OperationTimeout = DefaultRecoveryOperationTimeout
	}
	return c
}

func loadRecovery() (RecoveryConfig, error) {
	c := RecoveryConfig{
		Directory:     env("GOBY_RECOVERY_STATE_DIR", DefaultRecoveryDirectory),
		DatabaseURL:   env("GOBY_RECOVERY_DATABASE_URL", ""),
		PGDumpPath:    env("GOBY_PG_DUMP", "pg_dump"),
		PGRestorePath: env("GOBY_PG_RESTORE", "pg_restore"),
		Backups: backupstore.Config{
			Directory: env("GOBY_BACKUP_DIR", backupstore.DefaultDirectory),
		},
	}
	c.OperationsDirectory = env("GOBY_RECOVERY_OPERATIONS_DIR", c.Directory+"-operations")
	for _, field := range []struct {
		name                       string
		value                      *int64
		fallback, minimum, maximum int64
	}{
		{"GOBY_BACKUP_MAX_OBJECT_BYTES", &c.Backups.MaxObjectBytes, backupstore.DefaultMaxObjectBytes, 1, 1 << 40},
		{"GOBY_BACKUP_MAX_TOTAL_BYTES", &c.Backups.MaxTotalBytes, backupstore.DefaultMaxTotalBytes, 1, 1 << 44},
		{"GOBY_BACKUP_MIN_FREE_BYTES", &c.Backups.MinFreeBytes, backupstore.DefaultMinFreeBytes, 1, 1 << 40},
	} {
		parsed, err := strconv.ParseInt(env(field.name, strconv.FormatInt(field.fallback, 10)), 10, 64)
		if err != nil || parsed < field.minimum || parsed > field.maximum {
			return RecoveryConfig{}, fmt.Errorf("%s must be a decimal integer between %d and %d", field.name, field.minimum, field.maximum)
		}
		*field.value = parsed
	}
	objects, err := strconv.ParseInt(env("GOBY_BACKUP_MAX_OBJECTS", strconv.Itoa(backupstore.DefaultMaxObjects)), 10, 64)
	if err != nil || objects < 1 || objects > 4096 {
		return RecoveryConfig{}, fmt.Errorf("GOBY_BACKUP_MAX_OBJECTS must be a decimal integer between 1 and 4096")
	}
	c.Backups.MaxObjects = int(objects)
	c.OperationTimeout, err = time.ParseDuration(env("GOBY_BACKUP_TIMEOUT", "30m"))
	if err != nil || c.OperationTimeout < time.Minute || c.OperationTimeout > 30*time.Minute {
		return RecoveryConfig{}, fmt.Errorf("GOBY_BACKUP_TIMEOUT must be a Go duration between 1m and 30m")
	}
	return c, nil
}

// Validate checks only syntax and policy, using Linux path semantics on every
// host. It does not inspect directories, resolve executables, or connect to a
// database. Recovery identity checks apply only when its URL is configured.
func (c RecoveryConfig) Validate(primaryDatabaseURL string) error {
	if c == (RecoveryConfig{}) {
		return nil
	}
	c = c.WithDefaults()
	directories := []struct{ name, value string }{
		{"GOBY_RECOVERY_STATE_DIR", c.Directory},
		{"GOBY_BACKUP_DIR", c.Backups.Directory},
		{"GOBY_RECOVERY_OPERATIONS_DIR", c.OperationsDirectory},
	}
	for _, directory := range directories {
		if !recoveryDirectory(directory.value) {
			return fmt.Errorf("%s must be a bounded canonical absolute Linux directory other than the filesystem root", directory.name)
		}
	}
	for index, directory := range directories {
		for _, previous := range directories[:index] {
			if recoveryDirectoriesOverlap(previous.value, directory.value) {
				return fmt.Errorf("%s and %s must be separate non-overlapping directories", previous.name, directory.name)
			}
		}
	}
	// Keep these bounds aligned with backupstore.Config.Validate without using
	// its host-dependent filepath validation on a compilation-only Windows host.
	if c.Backups.MaxObjectBytes < 1 || c.Backups.MaxObjectBytes > 1<<40 {
		return fmt.Errorf("GOBY_BACKUP_MAX_OBJECT_BYTES must be between 1 and 1099511627776")
	}
	if c.Backups.MaxTotalBytes < c.Backups.MaxObjectBytes || c.Backups.MaxTotalBytes > 1<<44 {
		return fmt.Errorf("GOBY_BACKUP_MAX_TOTAL_BYTES must be at least GOBY_BACKUP_MAX_OBJECT_BYTES and at most 17592186044416")
	}
	if c.Backups.MaxObjects < 1 || c.Backups.MaxObjects > 4096 {
		return fmt.Errorf("GOBY_BACKUP_MAX_OBJECTS must be between 1 and 4096")
	}
	if c.Backups.MinFreeBytes < 1 || c.Backups.MinFreeBytes > 1<<40 {
		return fmt.Errorf("GOBY_BACKUP_MIN_FREE_BYTES must be between 1 and 1099511627776")
	}
	for _, executable := range []struct{ name, value string }{
		{"GOBY_PG_DUMP", c.PGDumpPath},
		{"GOBY_PG_RESTORE", c.PGRestorePath},
	} {
		if !recoveryExecutable(executable.value) {
			return fmt.Errorf("%s must name a bounded executable basename or canonical absolute Linux path", executable.name)
		}
	}
	if c.OperationTimeout < time.Minute || c.OperationTimeout > 30*time.Minute {
		return fmt.Errorf("GOBY_BACKUP_TIMEOUT must be a Go duration between 1m and 30m")
	}
	if c.DatabaseURL != "" {
		recoveryUser, recoveryDatabase, ok := recoveryDatabaseIdentity(c.DatabaseURL, true)
		if !ok {
			return fmt.Errorf("GOBY_RECOVERY_DATABASE_URL must be a PostgreSQL connection URL with explicit host, database, user, and sslmode")
		}
		primaryUser, primaryDatabase, ok := recoveryDatabaseIdentity(primaryDatabaseURL, false)
		if !ok || recoveryUser == primaryUser || recoveryDatabase == primaryDatabase {
			return fmt.Errorf("GOBY_RECOVERY_DATABASE_URL must identify a different explicit database and user from GOBY_DATABASE_URL")
		}
	}
	return nil
}

func (c Config) validateRecovery() error {
	if err := c.Recovery.Validate(c.DatabaseURL); err != nil {
		return err
	}
	if c.Recovery == (RecoveryConfig{}) {
		return nil
	}
	recovery := c.Recovery.WithDefaults()
	otherDirectories := append([]string{c.Transcoding.CacheDirectory, c.Diagnostics.WithDefaults().Directory}, c.MediaRoots...)
	for _, directory := range otherDirectories {
		if directory == "" {
			continue
		}
		if recoveryDirectoriesOverlap(recovery.Directory, path.Clean(directory)) ||
			recoveryDirectoriesOverlap(recovery.Backups.Directory, path.Clean(directory)) ||
			recoveryDirectoriesOverlap(recovery.OperationsDirectory, path.Clean(directory)) {
			return fmt.Errorf("GOBY_RECOVERY_STATE_DIR, GOBY_BACKUP_DIR, and GOBY_RECOVERY_OPERATIONS_DIR must not overlap configured cache, log, or media directories")
		}
	}
	return nil
}

func recoveryDirectory(value string) bool {
	return recoveryPathText(value) && path.IsAbs(value) && path.Clean(value) == value && value != "/"
}

func recoveryExecutable(value string) bool {
	if !recoveryPathText(value) || path.Clean(value) != value || value == "." || value == ".." || value == "/" {
		return false
	}
	return path.IsAbs(value) || path.Base(value) == value
}

func recoveryPathText(value string) bool {
	return len(value) <= 4096 && utf8.ValidString(value) && strings.TrimSpace(value) != "" &&
		!strings.Contains(value, "\\") && strings.IndexFunc(value, unicode.IsControl) < 0
}

func recoveryDirectoriesOverlap(first, second string) bool {
	return first == second || strings.HasPrefix(first, strings.TrimRight(second, "/")+"/") ||
		strings.HasPrefix(second, strings.TrimRight(first, "/")+"/")
}

func recoveryDatabaseIdentity(value string, requireTLSMode bool) (string, string, bool) {
	if len(value) > 8192 || !utf8.ValidString(value) || strings.ContainsRune(value, ' ') || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", "", false
	}
	scheme, remainder, found := strings.Cut(value, "://")
	authority, _, _ := strings.Cut(remainder, "/")
	if !found || scheme != "postgres" && scheme != "postgresql" || strings.Count(authority, "@") != 1 {
		return "", "", false
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" ||
		u.Opaque != "" || strings.Contains(value, "#") || u.User == nil || !strings.HasPrefix(u.Path, "/") {
		return "", "", false
	}
	// pgx treats '+' in query values as a space; libpq preserves it. Percent
	// encoding is required so both consumers use the same connection policy.
	if requireTLSMode && strings.Contains(u.RawQuery, "+") {
		return "", "", false
	}
	username, database := u.User.Username(), strings.TrimPrefix(u.Path, "/")
	if !recoveryDatabaseName(username) || !recoveryDatabaseName(database) ||
		!recoveryDatabaseHost(u.Hostname()) {
		return "", "", false
	}
	if password, _ := u.User.Password(); strings.ContainsRune(password, 0) {
		return "", "", false
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
			return "", "", false
		}
	}
	query := make(map[string]string)
	if u.RawQuery != "" {
		for _, field := range strings.Split(u.RawQuery, "&") {
			if strings.Count(field, "=") != 1 {
				return "", "", false
			}
			name, content, _ := strings.Cut(field, "=")
			key, keyErr := url.PathUnescape(name)
			content, contentErr := url.PathUnescape(content)
			if _, duplicate := query[key]; keyErr != nil || contentErr != nil || duplicate || key == "" ||
				content == "" || !utf8.ValidString(content) || strings.IndexFunc(content, unicode.IsControl) >= 0 {
				return "", "", false
			}
			query[key] = content
		}
	}
	for key := range query {
		// Identity must be explicit in the URI rather than replaced through a
		// query parameter or a service file with deployment-specific contents.
		switch strings.ToLower(key) {
		case "user", "password", "dbname", "database", "host", "hostaddr", "port", "service", "servicefile":
			return "", "", false
		}
	}
	if requireTLSMode {
		switch query["sslmode"] {
		case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		default:
			return "", "", false
		}
		if !recoveryDatabaseParameters(query) {
			return "", "", false
		}
	}
	return username, database, true
}

func recoveryDatabaseHost(value string) bool {
	if strings.Contains(value, ":") {
		address, err := netip.ParseAddr(value)
		return err == nil && address.Zone() == ""
	}
	for _, r := range value {
		if r != '.' && r != '-' && r != '_' && !(r >= 'a' && r <= 'z') &&
			!(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return value != ""
}

// Connection options are the pure-syntax subset supported by the isolated
// PostgreSQL command adapter. Certificate paths remain target-owned settings.
func recoveryDatabaseParameters(query map[string]string) bool {
	mode := query["sslmode"]
	for key, value := range query {
		switch key {
		case "sslmode":
		case "sslrootcert", "sslcert", "sslkey", "sslcrl", "sslcrldir":
			if key == "sslrootcert" && value == "system" {
				if mode != "verify-full" {
					return false
				}
			} else if !recoveryDirectory(value) || key == "sslkey" && strings.Contains(value, ":") {
				return false
			}
		case "connect_timeout":
			seconds, err := strconv.ParseUint(value, 10, 32)
			if err != nil || seconds < 1 || seconds > 1800 || strconv.FormatUint(seconds, 10) != value {
				return false
			}
		case "channel_binding":
			if value != "disable" && value != "prefer" && value != "require" {
				return false
			}
		case "target_session_attrs":
			switch value {
			case "any", "read-write", "read-only", "primary", "standby", "prefer-standby":
			default:
				return false
			}
		default:
			return false
		}
	}
	return (!strings.HasPrefix(mode, "verify-") || query["sslrootcert"] != "") &&
		(query["sslcert"] == "") == (query["sslkey"] == "")
}

func recoveryDatabaseName(value string) bool {
	return value != "" && len(value) <= 63 && utf8.ValidString(value) && strings.TrimSpace(value) != "" &&
		!strings.ContainsAny(value, "/\\") && strings.IndexFunc(value, unicode.IsControl) < 0
}

// BackupDefaults is the complete versioned allowlist of deployment defaults
// carried by a logical backup. Database credentials, network bindings, public
// URLs, hardware, storage, caches, and logging policy are never archived here.
// Approved media roots belong to the target operator and are never restored.
type BackupDefaults struct {
	Version          int    `json:"version"`
	ServerName       string `json:"serverName"`
	MaxBitrate       int64  `json:"maxBitrate"`
	MaxWidth         int    `json:"maxWidth"`
	MaxHeight        int    `json:"maxHeight"`
	MaxAudioChannels int    `json:"maxAudioChannels"`
}

func BackupDefaultsFromConfig(c Config) (BackupDefaults, error) {
	d := BackupDefaults{
		Version: BackupDefaultsVersion, ServerName: c.ServerName,
		MaxBitrate: c.Transcoding.MaxBitrate, MaxWidth: c.Transcoding.MaxWidth,
		MaxHeight: c.Transcoding.MaxHeight, MaxAudioChannels: c.Transcoding.MaxAudioChannels,
	}
	if err := d.Validate(); err != nil {
		return BackupDefaults{}, err
	}
	return d, nil
}

func (d BackupDefaults) Validate() error {
	if d.Version != BackupDefaultsVersion || ValidateServerName(d.ServerName) != nil ||
		ValidateOutputPlanningLimits(d.MaxBitrate, d.MaxWidth, d.MaxHeight, d.MaxAudioChannels) != nil {
		return ErrInvalidBackupDefaults
	}
	return nil
}

func EncodeBackupDefaults(c Config) ([]byte, error) {
	d, err := BackupDefaultsFromConfig(c)
	if err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

// DecodeBackupDefaults rejects lossy Unicode, duplicate decoded names, unknown
// or missing fields, and anything after the single bounded JSON object.
func DecodeBackupDefaults(data []byte) (BackupDefaults, error) {
	invalid := func() (BackupDefaults, error) { return BackupDefaults{}, ErrInvalidBackupDefaults }
	if len(data) == 0 || len(data) > MaxBackupDefaultsBytes || !utf8.Valid(data) || !backupDefaultsUnicode(data) {
		return invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return invalid()
	}
	var d BackupDefaults
	seen := make(map[string]bool, 6)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return invalid()
		}
		seen[key] = true
		var target any
		switch key {
		case "version":
			target = &d.Version
		case "serverName":
			target = &d.ServerName
		case "maxBitrate":
			target = &d.MaxBitrate
		case "maxWidth":
			target = &d.MaxWidth
		case "maxHeight":
			target = &d.MaxHeight
		case "maxAudioChannels":
			target = &d.MaxAudioChannels
		default:
			return invalid()
		}
		if err := decoder.Decode(target); err != nil {
			return invalid()
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || len(seen) != 6 || d.Validate() != nil {
		return invalid()
	}
	if _, err := decoder.Token(); err != io.EOF {
		return invalid()
	}
	return d, nil
}

// Apply changes only logical defaults. All target secrets, approved media
// roots, filesystem locations, network settings, and hardware policy survive.
func (d BackupDefaults) Apply(target Config) (Config, error) {
	if err := d.Validate(); err != nil {
		return target, err
	}
	target.ServerName = d.ServerName
	target.Transcoding.MaxBitrate = d.MaxBitrate
	target.Transcoding.MaxWidth = d.MaxWidth
	target.Transcoding.MaxHeight = d.MaxHeight
	target.Transcoding.MaxAudioChannels = d.MaxAudioChannels
	return target, nil
}

// encoding/json replaces unpaired escaped UTF-16 surrogates. Reject them before
// decoding so the archive cannot silently change its server name or field names.
func backupDefaultsUnicode(data []byte) bool {
	for index := 0; index < len(data); index++ {
		if data[index] != '"' {
			continue
		}
		for index++; index < len(data) && data[index] != '"'; index++ {
			if data[index] != '\\' {
				continue
			}
			index++
			if index >= len(data) {
				return false
			}
			if data[index] != 'u' {
				continue
			}
			if index+4 >= len(data) {
				return false
			}
			unit, err := strconv.ParseUint(string(data[index+1:index+5]), 16, 16)
			if err != nil || unit >= 0xdc00 && unit <= 0xdfff {
				return false
			}
			index += 4
			if unit < 0xd800 || unit > 0xdbff {
				continue
			}
			if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(data[index+3:index+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}
