package config

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/analysiscache"
)

const maxMediaAnalysisConfigBytes = 64 << 10

// MediaAnalysisConfig is trusted deployment inventory. Its zero value disables
// execution and opens no cache. Editable analysis policy lives in PostgreSQL;
// filesystem locations and tool inventory cannot be changed by an HTTP body.
type MediaAnalysisConfig struct {
	Enabled           bool   `json:"enabled"`
	CacheDirectory    string `json:"cacheDirectory"`
	CacheMaxBytes     int64  `json:"cacheMaxBytes"`
	CacheMaxEntries   int    `json:"cacheMaxEntries"`
	MaxEntryBytes     int64  `json:"maxEntryBytes"`
	MaxFileBytes      int64  `json:"maxFileBytes"`
	FingerprintPath   string `json:"fingerprintPath,omitempty"`
	FingerprintSHA256 string `json:"fingerprintSHA256,omitempty"`
}

func loadMediaAnalysis() (MediaAnalysisConfig, error) {
	name := os.Getenv("GOBY_MEDIA_ANALYSIS_FILE")
	if name == "" {
		return MediaAnalysisConfig{}, nil
	}
	if !recoveryPathText(name) {
		return MediaAnalysisConfig{}, errors.New("GOBY_MEDIA_ANALYSIS_FILE must name a bounded configuration file")
	}
	before, err := os.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxMediaAnalysisConfigBytes {
		return MediaAnalysisConfig{}, errors.New("media analysis configuration must be a bounded regular file")
	}
	file, err := os.Open(name)
	if err != nil {
		return MediaAnalysisConfig{}, errors.New("cannot open media analysis configuration")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return MediaAnalysisConfig{}, errors.New("media analysis configuration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxMediaAnalysisConfigBytes+1))
	if err != nil || len(data) > maxMediaAnalysisConfigBytes {
		return MediaAnalysisConfig{}, errors.New("cannot read bounded media analysis configuration")
	}
	return parseMediaAnalysis(data)
}

func parseMediaAnalysis(data []byte) (MediaAnalysisConfig, error) {
	var value MediaAnalysisConfig
	invalid := errors.New("media analysis configuration must contain one exact bounded JSON object")
	if len(data) > maxMediaAnalysisConfigBytes || !utf8.Valid(data) || !backupDefaultsUnicode(data) {
		return value, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return value, invalid
	}
	fields := map[string]any{
		"enabled": &value.Enabled, "cacheDirectory": &value.CacheDirectory,
		"cacheMaxBytes": &value.CacheMaxBytes, "cacheMaxEntries": &value.CacheMaxEntries,
		"maxEntryBytes": &value.MaxEntryBytes, "maxFileBytes": &value.MaxFileBytes,
		"fingerprintPath": &value.FingerprintPath, "fingerprintSHA256": &value.FingerprintSHA256,
	}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		name, err := decoder.Token()
		key, ok := name.(string)
		if err != nil || !ok || fields[key] == nil || seen[key] {
			return MediaAnalysisConfig{}, invalid
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, fields[key]) != nil {
			return MediaAnalysisConfig{}, invalid
		}
		seen[key] = true
	}
	if ending, err := decoder.Token(); err != nil || ending != json.Delim('}') {
		return MediaAnalysisConfig{}, invalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return MediaAnalysisConfig{}, invalid
	}
	if value.Enabled {
		if !seen["cacheMaxBytes"] {
			value.CacheMaxBytes = 2 << 30
		}
		if !seen["cacheMaxEntries"] {
			value.CacheMaxEntries = 512
		}
		if !seen["maxEntryBytes"] {
			value.MaxEntryBytes = 512 << 20
		}
		if !seen["maxFileBytes"] {
			value.MaxFileBytes = 128 << 20
		}
	}
	if err := value.Validate(); err != nil {
		return MediaAnalysisConfig{}, err
	}
	return value, nil
}

func (value MediaAnalysisConfig) Validate() error {
	if !value.Enabled {
		if value != (MediaAnalysisConfig{}) {
			return errors.New("disabled media analysis must not retain execution inventory")
		}
		return nil
	}
	if !recoveryDirectory(value.CacheDirectory) {
		return errors.New("media analysis cacheDirectory must be a canonical absolute private directory")
	}
	if value.CacheMaxBytes < 4096 || value.CacheMaxBytes > 16<<30 || value.CacheMaxEntries < 1 || value.CacheMaxEntries > 65536 ||
		value.MaxEntryBytes < 4096 || value.MaxEntryBytes > 1<<30 || value.MaxEntryBytes > value.CacheMaxBytes ||
		value.MaxFileBytes < 72 || value.MaxFileBytes > 128<<20 || value.MaxFileBytes > value.MaxEntryBytes {
		return errors.New("media analysis cache and artifact budgets are inconsistent or out of bounds")
	}
	if value.FingerprintPath != "" && (!recoveryExecutable(value.FingerprintPath) || !path.IsAbs(value.FingerprintPath)) {
		return errors.New("fingerprintPath must be a canonical absolute executable path")
	}
	if value.FingerprintSHA256 != "" {
		digest, err := hex.DecodeString(value.FingerprintSHA256)
		if value.FingerprintPath == "" || err != nil || len(digest) != 32 || strings.ToLower(value.FingerprintSHA256) != value.FingerprintSHA256 {
			return errors.New("fingerprintSHA256 must bind a configured tool with a lowercase SHA-256 digest")
		}
	}
	return nil
}

func (value MediaAnalysisConfig) CacheOptions() analysiscache.Config {
	return analysiscache.Config{Root: value.CacheDirectory, MaxBytes: value.CacheMaxBytes,
		MaxEntries: value.CacheMaxEntries, MaxEntryBytes: value.MaxEntryBytes,
		MaxFileBytes: value.MaxFileBytes, MaxTemporaryFiles: 8192}
}

func (c Config) validateMediaAnalysis() error {
	if err := c.MediaAnalysis.Validate(); err != nil {
		return err
	}
	if !c.MediaAnalysis.Enabled {
		return nil
	}
	others := append([]string{c.Transcoding.CacheDirectory, c.Timeshift.CacheDirectory,
		c.MediaOperations.ScratchDirectory, c.Diagnostics.WithDefaults().Directory}, c.MediaRoots...)
	if c.Recovery != (RecoveryConfig{}) {
		recovery := c.Recovery.WithDefaults()
		others = append(others, recovery.Directory, recovery.OperationsDirectory, recovery.Backups.Directory)
	}
	for _, other := range others {
		if other != "" && recoveryDirectoriesOverlap(c.MediaAnalysis.CacheDirectory, path.Clean(other)) {
			return errors.New("media analysis cache must not overlap media, other caches, diagnostics, operations, or recovery stores")
		}
	}
	return nil
}
