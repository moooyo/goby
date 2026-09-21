package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScanEvidenceDeploymentDefaultsAndDisabledInventory(t *testing.T) {
	for _, raw := range []string{`{}`, `{"enabled":false}`, `{"enabled":false,"directory":"","maxBytes":0}`} {
		value, err := parseScanEvidence([]byte(raw))
		if err != nil || value != (ScanEvidenceConfig{}) {
			t.Fatalf("zero deployment inventory = %+v, error = %v", value, err)
		}
	}
	value, err := parseScanEvidence([]byte(`{"enabled":true,"directory":"/srv/goby/scan-evidence"}`))
	want := ScanEvidenceConfig{Enabled: true, Directory: "/srv/goby/scan-evidence", MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	if err != nil || value != want {
		t.Fatalf("enabled bounded defaults = %+v, want %+v, error = %v", value, want, err)
	}
	value, err = parseScanEvidence([]byte(`{"enabled":true,"directory":"/srv/goby/scan-evidence","maxBytes":65536,"maxDirectories":2,"maxEntries":3,"maxFallbackHandles":1}`))
	if err != nil || value.MaxBytes != 65536 || value.MaxDirectories != 2 || value.MaxEntries != 3 || value.MaxFallbackHandles != 1 {
		t.Fatalf("explicit smaller budgets were replaced by defaults: %+v, %v", value, err)
	}
	for _, mutate := range []func(*ScanEvidenceConfig){
		func(c *ScanEvidenceConfig) { c.Directory = "/spool" },
		func(c *ScanEvidenceConfig) { c.MaxBytes = 1 },
		func(c *ScanEvidenceConfig) { c.MaxDirectories = 1 },
		func(c *ScanEvidenceConfig) { c.MaxEntries = 1 },
		func(c *ScanEvidenceConfig) { c.MaxFallbackHandles = 1 },
	} {
		var disabled ScanEvidenceConfig
		mutate(&disabled)
		if err := disabled.Validate(); err == nil {
			t.Fatal("disabled deployment retained spool inventory")
		}
	}
}

func TestScanEvidenceDeploymentRejectsAmbiguousOrUnboundedJSON(t *testing.T) {
	for _, raw := range []string{
		``, `null`, `[]`, `{} {}`, `{"Enabled":true}`, `{"enabled":true,"enabled":false}`,
		`{"enabled":null}`, `{"enabled":"true"}`, `{"enabled":true,"directory":null}`,
		`{"enabled":true,"directory":"/spool","directory":"/other"}`,
		`{"enabled":true,"directory":"/spool","maxRoots":256}`,
		`{"enabled":true,"directory":"/spool","maxPasses":4}`,
		`{"enabled":true,"directory":"/spool","unknown":true}`,
		`{"enabled":true}`, `{"enabled":true,"directory":"relative"}`,
		`{"enabled":true,"directory":"/"}`, `{"enabled":true,"directory":"/spool/../other"}`,
		`{"enabled":true,"directory":"/spool/"}`, `{"enabled":true,"directory":"/spool\\other"}`,
		`{"enabled":true,"directory":"/spool/\u0000"}`, `{"enabled":true,"directory":"/spool/\ud800"}`,
		`{"enabled":true,"directory":"/spool/\udc00"}`,
		`{"enabled":true,"directory":"/spool","maxBytes":null}`,
		`{"enabled":true,"directory":"/spool","maxBytes":"1024"}`,
		`{"enabled":true,"directory":"/spool","maxBytes":1.5}`,
		`{"enabled":true,"directory":"/spool","maxBytes":65535}`,
		`{"enabled":true,"directory":"/spool","maxEntries":1.0}`,
		`{"enabled":true,"directory":"/spool","maxDirectories":1e2}`,
		`{"enabled":true,"directory":"/spool","maxBytes":9223372036854775808}`,
		`{"enabled":true,"directory":"/spool","maxFallbackHandles":18446744073709551616}`,
	} {
		if value, err := parseScanEvidence([]byte(raw)); err == nil || value != (ScanEvidenceConfig{}) {
			t.Fatalf("ambiguous deployment returned usable inventory: %q, %+v, %v", raw, value, err)
		}
	}
	for _, raw := range [][]byte{
		append([]byte(`{"directory":"/`), 0xff),
		append([]byte(`{}`), bytes.Repeat([]byte(" "), maxScanEvidenceConfigBytes-1)...),
	} {
		if _, err := parseScanEvidence(raw); err == nil {
			t.Fatal("invalid UTF-8 or oversized deployment JSON was accepted")
		}
	}
	for _, budget := range []struct {
		name    string
		maximum int64
	}{
		{"maxBytes", 1 << 30}, {"maxDirectories", 131072}, {"maxEntries", 1048576}, {"maxFallbackHandles", 4096},
	} {
		for _, number := range []int64{-1, 0, budget.maximum + 1} {
			raw := fmt.Sprintf(`{"enabled":true,"directory":"/spool",%q:%d}`, budget.name, number)
			if _, err := parseScanEvidence([]byte(raw)); err == nil {
				t.Fatalf("accepted out-of-bounds %s=%d", budget.name, number)
			}
		}
	}
}

func TestScanEvidenceDeploymentSeparatesEveryOwnedRoot(t *testing.T) {
	value, err := parseScanEvidence([]byte(`{"enabled":true,"directory":"/srv/goby/scan-evidence"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name string
		set  func(*Config, string)
	}{
		{"media", func(c *Config, root string) { c.MediaRoots = []string{root} }},
		{"transcoding", func(c *Config, root string) { c.Transcoding.CacheDirectory = root }},
		{"timeshift", func(c *Config, root string) { c.Timeshift.CacheDirectory = root }},
		{"analysis", func(c *Config, root string) { c.MediaAnalysis.CacheDirectory = root }},
		{"operations", func(c *Config, root string) { c.MediaOperations.ScratchDirectory = root }},
		{"media_diagnostics", func(c *Config, root string) { c.MediaDiagnostics.ScratchDirectory = root }},
		{"logs", func(c *Config, root string) { c.Diagnostics.Directory = root }},
		{"web", func(c *Config, root string) { c.WebDirectory = root }},
		{"recovery", func(c *Config, root string) { c.Recovery.Directory = root }},
		{"recovery_operations", func(c *Config, root string) { c.Recovery.OperationsDirectory = root }},
		{"backups", func(c *Config, root string) { c.Recovery.Backups.Directory = root }},
	} {
		t.Run(field.name, func(t *testing.T) {
			for _, root := range []string{"/srv/goby", value.Directory, value.Directory + "/child"} {
				configuration := Config{ScanEvidence: value}
				field.set(&configuration, root)
				if configuration.validateScanEvidence() == nil {
					t.Fatalf("accepted overlapping %s root %q", field.name, root)
				}
			}
			configuration := Config{ScanEvidence: value}
			field.set(&configuration, value.Directory+"-other")
			if err := configuration.validateScanEvidence(); err != nil {
				t.Fatalf("a lexical prefix without a directory boundary overlaps: %v", err)
			}
		})
	}
}

func TestScanEvidenceExcludedRootsPreserveDefaultsAndCallerOwnership(t *testing.T) {
	configuration := Config{MediaRoots: []string{"/media", "/media", ""}, WebDirectory: "/web"}
	configuration.Transcoding.CacheDirectory = "/transcoding"
	configuration.Timeshift.CacheDirectory = "/timeshift"
	configuration.MediaAnalysis.CacheDirectory = "/analysis"
	configuration.MediaOperations.ScratchDirectory = "/operations"
	configuration.MediaDiagnostics.ScratchDirectory = "/media-diagnostics"
	configuration.Diagnostics.Directory = "/logs"
	configuration.Recovery.Directory = "/recovery"
	configuration.Recovery.Backups.Directory = "/backups"
	want := []string{"/transcoding", "/timeshift", "/analysis", "/operations", "/media-diagnostics", "/logs", "/web", "/media", "/recovery", "/recovery-operations", "/backups"}
	roots := configuration.ScanEvidenceExcludedRoots()
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("runtime exclusion inventory = %v, want %v", roots, want)
	}
	for index := range roots {
		roots[index] = "/caller-mutated"
	}
	if !reflect.DeepEqual(configuration.ScanEvidenceExcludedRoots(), want) || configuration.MediaRoots[0] != "/media" {
		t.Fatal("mutating the returned root inventory changed deployment authority")
	}
	defaults := (Config{}).ScanEvidenceExcludedRoots()
	if len(defaults) != 1 || defaults[0] != (Config{}).Diagnostics.WithDefaults().Directory {
		t.Fatalf("default diagnostic root was omitted: %v", defaults)
	}
}

func TestScanEvidenceLoadBindsOnlyItsBoundedDeploymentFile(t *testing.T) {
	t.Setenv("GOBY_SCAN_EVIDENCE_FILE", "")
	if value, err := loadScanEvidence(); err != nil || value != (ScanEvidenceConfig{}) {
		t.Fatalf("unconfigured spool = %+v, %v", value, err)
	}
	directory := t.TempDir()
	filename := filepath.Join(directory, "scan-evidence.json")
	t.Setenv("GOBY_SCAN_EVIDENCE_FILE", filepath.ToSlash(filename))
	if err := os.WriteFile(filename, []byte(`{"enabled":true,"directory":"/unopened-scan-evidence-fixture/spool"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := loadScanEvidence(); err != nil || !value.Enabled || value.Directory != "/unopened-scan-evidence-fixture/spool" {
		t.Fatalf("bounded deployment load = %+v, %v", value, err)
	}
	for _, data := range [][]byte{nil, []byte(`{"enabled":false,"enabled":false}`), bytes.Repeat([]byte(" "), maxScanEvidenceConfigBytes+1)} {
		if err := os.WriteFile(filename, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadScanEvidence(); err == nil {
			t.Fatal("configured invalid deployment was silently disabled")
		}
	}
	for _, name := range []string{filepath.ToSlash(directory), filepath.ToSlash(filepath.Join(directory, "missing.json"))} {
		t.Setenv("GOBY_SCAN_EVIDENCE_FILE", name)
		if _, err := loadScanEvidence(); err == nil {
			t.Fatal("nonregular or missing configuration was silently disabled")
		}
	}
}

func TestScanEvidenceLoadAndValidateUseTheDeploymentConfiguration(t *testing.T) {
	recoveryConfigEnvironment(t)
	for _, name := range []string{"GOBY_SCAN_EVIDENCE_FILE", "GOBY_MEDIA_ANALYSIS_FILE", "GOBY_MEDIA_OPERATIONS_FILE", "GOBY_MEDIA_DIAGNOSTICS_CGROUP", "GOBY_MEDIA_DIAGNOSTICS_SCRATCH"} {
		t.Setenv(name, "")
	}
	t.Setenv("GOBY_MEDIA_ROOTS", "/fixture/media")
	t.Setenv("GOBY_WEB_DIR", "/fixture/web")
	filename := filepath.Join(t.TempDir(), "scan-evidence.json")
	data, err := json.Marshal(ScanEvidenceConfig{Enabled: true, Directory: "/fixture/scan-evidence", MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096})
	if err != nil || os.WriteFile(filename, data, 0600) != nil {
		t.Fatal("write deployment fixture")
	}
	t.Setenv("GOBY_SCAN_EVIDENCE_FILE", filepath.ToSlash(filename))
	configuration, err := Load()
	if err != nil || !configuration.ScanEvidence.Enabled || configuration.ScanEvidence.Directory != "/fixture/scan-evidence" {
		t.Fatalf("Config.Load did not bind scan evidence: %+v, %v", configuration.ScanEvidence, err)
	}
	t.Setenv("GOBY_WEB_DIR", "/fixture/scan-evidence/web")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "scan evidence") {
		t.Fatalf("Config.Validate did not reject overlapping web assets: %v", err)
	}
}
