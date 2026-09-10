package backupstore

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPolicyBoundsRejectOverflowBeforeFilesystemAccess(t *testing.T) {
	base := Config{Directory: filepath.Join(os.TempDir(), "backup-policy")}.WithDefaults()
	if base.MaxObjectBytes != 8<<30 || base.MaxTotalBytes != 32<<30 || base.MaxObjects != 128 || base.MinFreeBytes != 512<<20 || base.Validate() != nil {
		t.Fatal("default policy is invalid")
	}
	cases := map[string]func(*Config){
		"object negative":    func(c *Config) { c.MaxObjectBytes = -1 },
		"object overflow":    func(c *Config) { c.MaxObjectBytes = math.MaxInt64 },
		"total overflow":     func(c *Config) { c.MaxTotalBytes = math.MaxInt64 },
		"total below object": func(c *Config) { c.MaxTotalBytes = c.MaxObjectBytes - 1 },
		"objects negative":   func(c *Config) { c.MaxObjects = -1 },
		"objects overflow":   func(c *Config) { c.MaxObjects = math.MaxInt },
		"reserve negative":   func(c *Config) { c.MinFreeBytes = math.MinInt64 },
		"reserve overflow":   func(c *Config) { c.MinFreeBytes = math.MaxInt64 },
		"relative path":      func(c *Config) { c.Directory = "relative" },
		"unclean path":       func(c *Config) { c.Directory += string(filepath.Separator) + ".." },
		"path nul":           func(c *Config) { c.Directory += "\x00" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			candidate := base
			change(&candidate)
			if candidate.Validate() != ErrInvalid {
				t.Fatal("invalid policy accepted")
			}
		})
	}
}

func TestSummaryWhitelistAndCopyIsolation(t *testing.T) {
	valid := SourceSummary{ArchiveID: strings.Repeat("a", 32), FormatVersion: 1, ApplicationVersion: "0.1.0+test", SchemaVersion: 22, ServerID: "opaque:server-\U0001f5c3", CreatedAt: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), Tables: []TableCount{{Name: "users", Rows: 3}}}
	if !validSummary(&valid) {
		t.Fatal("valid archive summary rejected")
	}
	mutations := map[string]func(*SourceSummary){
		"archive identity":  func(s *SourceSummary) { s.ArchiveID = "../outside" },
		"format version":    func(s *SourceSummary) { s.FormatVersion = 2 },
		"database version":  func(s *SourceSummary) { s.SchemaVersion = -1 },
		"server controls":   func(s *SourceSummary) { s.ServerID = "server\nsecret" },
		"server encoding":   func(s *SourceSummary) { s.ServerID = string([]byte{255}) },
		"server length":     func(s *SourceSummary) { s.ServerID = strings.Repeat("x", 257) },
		"version controls":  func(s *SourceSummary) { s.ApplicationVersion = "version\x00" },
		"non UTC":           func(s *SourceSummary) { s.CreatedAt = s.CreatedAt.In(time.FixedZone("offset", 3600)) },
		"SQL expression":    func(s *SourceSummary) { s.Tables = []TableCount{{Name: "users;drop", Rows: 0}} },
		"duplicate table":   func(s *SourceSummary) { s.Tables = []TableCount{{Name: "users"}, {Name: "users"}} },
		"negative rows":     func(s *SourceSummary) { s.Tables = []TableCount{{Name: "users", Rows: -1}} },
		"table count bound": func(s *SourceSummary) { s.Tables = make([]TableCount, 513) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if validSummary(&candidate) {
				t.Fatal("unbounded or unsafe summary accepted")
			}
		})
	}
	metadata := Metadata{ID: strings.Repeat("b", 32), Summary: &valid, Verified: true}
	copy := copyMetadata(metadata)
	copy.Summary.ServerID = "other"
	copy.Summary.Tables[0].Rows = 999
	if metadata.Summary.ServerID != valid.ServerID || metadata.Summary.Tables[0].Rows != 3 {
		t.Fatal("returned metadata aliases the persisted summary")
	}
}

func TestIdentifiersAndErrorCodesExcludePathsAndRawFailures(t *testing.T) {
	for _, value := range []string{"../object", "/etc/passwd", "object\\name", "token\nvalue", "x\x00", strings.Repeat("x", 257)} {
		if identifier(value, 256, true) || hexValue(value, 32) {
			t.Fatalf("unsafe identifier admitted: length %d", len(value))
		}
	}
	for _, code := range []ErrorCode{"read /secret/file: denied", "password", "unknown", "\n"} {
		if validCode(code) {
			t.Fatal("arbitrary error text admitted")
		}
	}
}
