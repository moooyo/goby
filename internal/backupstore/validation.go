package backupstore

import (
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Validate performs policy validation without filesystem access. Zero policy
// values select defaults; explicit environment parsing belongs to the caller.
func (c Config) Validate() error {
	c = c.WithDefaults()
	if !filepath.IsAbs(c.Directory) || filepath.Clean(c.Directory) != c.Directory || c.Directory == string(filepath.Separator) || len(c.Directory) > 4096 || strings.ContainsRune(c.Directory, 0) ||
		c.MaxObjectBytes < 1 || c.MaxObjectBytes > 1<<40 || c.MaxTotalBytes < c.MaxObjectBytes || c.MaxTotalBytes > 1<<44 || c.MaxObjects < 1 || c.MaxObjects > 4096 || c.MinFreeBytes < 1 || c.MinFreeBytes > 1<<40 {
		return ErrInvalid
	}
	return nil
}

func hexValue(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func identifier(value string, maximum int, empty bool) bool {
	if value == "" {
		return empty
	}
	if len(value) > maximum {
		return false
	}
	for i, c := range value {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == ':' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func validCode(code ErrorCode) bool {
	switch code {
	case CodeNone, CodeCancelled, CodeInterrupted, CodeStorage, CodeQuota, CodeIntegrity, CodeGeneration, CodeImport, CodeVerification:
		return true
	default:
		return false
	}
}

func validSummary(summary *SourceSummary) bool {
	if summary == nil {
		return true
	}
	if !hexValue(summary.ArchiveID, 32) || !boundedText(summary.ApplicationVersion, 128) || !boundedText(summary.ServerID, 256) || summary.FormatVersion != 1 || summary.SchemaVersion < 1 || summary.SchemaVersion > 1000000 || !validTime(summary.CreatedAt) || len(summary.Tables) > 512 {
		return false
	}
	seen := make(map[string]bool)
	for _, table := range summary.Tables {
		if table.Name == "" || len(table.Name) > 63 || table.Rows < 0 || seen[table.Name] {
			return false
		}
		for i, c := range table.Name {
			if !(c >= 'a' && c <= 'z' || c == '_' || i > 0 && c >= '0' && c <= '9') {
				return false
			}
		}
		seen[table.Name] = true
	}
	return true
}

func validTime(value time.Time) bool {
	_, offset := value.Zone()
	return !value.IsZero() && offset == 0 && value.Year() >= 2000 && value.Year() <= 9999
}

func boundedText(value string, limit int) bool {
	if value == "" || len(value) > limit || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func copyMetadata(value Metadata) Metadata {
	if value.Summary != nil {
		summary := *value.Summary
		summary.Tables = append([]TableCount(nil), summary.Tables...)
		value.Summary = &summary
	}
	return value
}
