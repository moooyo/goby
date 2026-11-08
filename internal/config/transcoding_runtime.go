package config

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidateTranscodingTempPath checks a requested private Linux cache location
// without creating a directory, recovering jobs, or changing the active cache.
// Empty selects the deployment location. The manager owns filesystem checks
// when a subsequent process starts with the resolved path.
func ValidateTranscodingTempPath(value string, excludedRoots []string) error {
	if value == "" {
		return nil
	}
	if len(value) > 4096 || !utf8.ValidString(value) || !path.IsAbs(value) ||
		path.Clean(value) != value || value == "/" || strings.Contains(value, "\\") ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 || strings.TrimSpace(value) != value {
		return fmt.Errorf("use a canonical absolute Linux directory other than the filesystem root")
	}
	for _, root := range excludedRoots {
		if root != "" && recoveryDirectoriesOverlap(value, path.Clean(root)) {
			return fmt.Errorf("use a dedicated directory separate from media, other caches, scratch, logs, web assets, and recovery stores")
		}
	}
	return nil
}

// TranscodingTempPathExclusions returns the other deployment roots that a
// managed transcoding cache must not contain or be contained by. The currently
// configured transcode cache is intentionally eligible as the deployment
// default or as a later explicit selection.
func (c Config) TranscodingTempPathExclusions() []string {
	roots := append([]string{c.Timeshift.CacheDirectory, c.MediaAnalysis.CacheDirectory,
		c.MediaOperations.ScratchDirectory, c.MediaDiagnostics.ScratchDirectory,
		c.Diagnostics.WithDefaults().Directory, c.ScanEvidence.Directory, c.WebDirectory}, c.MediaRoots...)
	if c.Recovery != (RecoveryConfig{}) {
		recovery := c.Recovery.WithDefaults()
		roots = append(roots, recovery.Directory, recovery.OperationsDirectory, recovery.Backups.Directory)
	}
	result := make([]string, 0, len(roots))
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		absolute, err := filepath.Abs(root)
		if err != nil {
			// Invalid deployment roots are rejected by startup configuration or
			// their owning runtime. Retain the text to fail an obvious overlap.
			absolute = root
		}
		absolute = path.Clean(absolute)
		if !seen[absolute] {
			result = append(result, absolute)
			seen[absolute] = true
		}
	}
	return result
}
