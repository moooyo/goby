package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"unicode/utf8"
)

const (
	maxScanEvidenceConfigBytes     = 64 << 10
	defaultScanEvidenceMaxBytes    = 1 << 30
	defaultScanEvidenceDirectories = 131072
	defaultScanEvidenceEntries     = 1048576
	defaultScanEvidenceHandles     = 4096
)

// ScanEvidenceConfig is trusted deployment inventory for bounded scan evidence.
// Its zero value disables the spool. The store owns pass/root limits and runtime
// filesystem identity checks; neither paths nor limits come from HTTP input.
type ScanEvidenceConfig struct {
	Enabled            bool   `json:"enabled"`
	Directory          string `json:"directory"`
	MaxBytes           int64  `json:"maxBytes"`
	MaxDirectories     int    `json:"maxDirectories"`
	MaxEntries         int    `json:"maxEntries"`
	MaxFallbackHandles int    `json:"maxFallbackHandles"`
}

func loadScanEvidence() (ScanEvidenceConfig, error) {
	name := os.Getenv("GOBY_SCAN_EVIDENCE_FILE")
	if name == "" {
		return ScanEvidenceConfig{}, nil
	}
	if !recoveryPathText(name) {
		return ScanEvidenceConfig{}, errors.New("GOBY_SCAN_EVIDENCE_FILE must name a bounded configuration file")
	}
	before, err := os.Lstat(name)
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxScanEvidenceConfigBytes {
		return ScanEvidenceConfig{}, errors.New("scan evidence configuration must be a bounded regular file")
	}
	file, err := os.Open(name)
	if err != nil {
		return ScanEvidenceConfig{}, errors.New("cannot open scan evidence configuration")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return ScanEvidenceConfig{}, errors.New("scan evidence configuration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxScanEvidenceConfigBytes+1))
	if err != nil || len(data) > maxScanEvidenceConfigBytes {
		return ScanEvidenceConfig{}, errors.New("cannot read bounded scan evidence configuration")
	}
	return parseScanEvidence(data)
}

func parseScanEvidence(data []byte) (ScanEvidenceConfig, error) {
	var value ScanEvidenceConfig
	invalid := errors.New("scan evidence configuration must contain one exact bounded JSON object")
	if len(data) > maxScanEvidenceConfigBytes || !utf8.Valid(data) || !backupDefaultsUnicode(data) {
		return value, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if opening, err := decoder.Token(); err != nil || opening != json.Delim('{') {
		return value, invalid
	}
	fields := map[string]any{
		"enabled": &value.Enabled, "directory": &value.Directory, "maxBytes": &value.MaxBytes,
		"maxDirectories": &value.MaxDirectories, "maxEntries": &value.MaxEntries,
		"maxFallbackHandles": &value.MaxFallbackHandles,
	}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		name, err := decoder.Token()
		key, ok := name.(string)
		if err != nil || !ok || fields[key] == nil || seen[key] {
			return ScanEvidenceConfig{}, invalid
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, fields[key]) != nil {
			return ScanEvidenceConfig{}, invalid
		}
		seen[key] = true
	}
	if ending, err := decoder.Token(); err != nil || ending != json.Delim('}') {
		return ScanEvidenceConfig{}, invalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ScanEvidenceConfig{}, invalid
	}
	if value.Enabled {
		if !seen["maxBytes"] {
			value.MaxBytes = defaultScanEvidenceMaxBytes
		}
		if !seen["maxDirectories"] {
			value.MaxDirectories = defaultScanEvidenceDirectories
		}
		if !seen["maxEntries"] {
			value.MaxEntries = defaultScanEvidenceEntries
		}
		if !seen["maxFallbackHandles"] {
			value.MaxFallbackHandles = defaultScanEvidenceHandles
		}
	}
	if err := value.Validate(); err != nil {
		return ScanEvidenceConfig{}, err
	}
	return value, nil
}

func (value ScanEvidenceConfig) Validate() error {
	if !value.Enabled {
		if value != (ScanEvidenceConfig{}) {
			return errors.New("disabled scan evidence must not retain spool inventory")
		}
		return nil
	}
	if !recoveryDirectory(value.Directory) {
		return errors.New("scan evidence directory must be a canonical absolute private directory")
	}
	if value.MaxBytes < 64<<10 || value.MaxBytes > defaultScanEvidenceMaxBytes ||
		value.MaxDirectories < 1 || value.MaxDirectories > defaultScanEvidenceDirectories ||
		value.MaxEntries < 1 || value.MaxEntries > defaultScanEvidenceEntries ||
		value.MaxFallbackHandles < 1 || value.MaxFallbackHandles > defaultScanEvidenceHandles {
		return errors.New("scan evidence budgets must be positive and within the fixed store ceilings")
	}
	return nil
}

// ScanEvidenceExcludedRoots returns an independent inventory of other deployment
// roots. The runtime manager must resolve relative roots and filesystem aliases
// before admitting its private directory; configuration validation stays lexical.
func (c Config) ScanEvidenceExcludedRoots() []string {
	roots := append([]string{c.Transcoding.CacheDirectory, c.Timeshift.CacheDirectory,
		c.MediaAnalysis.CacheDirectory, c.MediaOperations.ScratchDirectory,
		c.MediaDiagnostics.ScratchDirectory, c.Diagnostics.WithDefaults().Directory,
		c.WebDirectory}, c.MediaRoots...)
	if c.Recovery != (RecoveryConfig{}) {
		recovery := c.Recovery.WithDefaults()
		roots = append(roots, recovery.Directory, recovery.OperationsDirectory, recovery.Backups.Directory)
	}
	result := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root != "" && !seen[root] {
			result = append(result, root)
			seen[root] = true
		}
	}
	return result
}

func (c Config) validateScanEvidence() error {
	if err := c.ScanEvidence.Validate(); err != nil {
		return err
	}
	if !c.ScanEvidence.Enabled {
		return nil
	}
	for _, other := range c.ScanEvidenceExcludedRoots() {
		if recoveryDirectoriesOverlap(c.ScanEvidence.Directory, path.Clean(other)) {
			return errors.New("scan evidence directory must not overlap media, caches, scratch, logs, web assets, or recovery stores")
		}
	}
	return nil
}
