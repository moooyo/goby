package config

import (
	"strings"
	"testing"
)

func TestMediaAnalysisDeploymentSeparatesPreviewAndFingerprintAvailability(t *testing.T) {
	configuration, err := parseMediaAnalysis([]byte(`{"enabled":true,"cacheDirectory":"/var/cache/goby/analysis"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Enabled || configuration.FingerprintPath != "" || configuration.CacheMaxBytes != 2<<30 ||
		configuration.MaxEntryBytes != 512<<20 || configuration.MaxFileBytes != 128<<20 || configuration.CacheMaxEntries != 512 {
		t.Fatal("explicit preview inventory did not retain its independent bounded defaults")
	}
	configuration, err = parseMediaAnalysis([]byte(`{"enabled":true,"cacheDirectory":"/var/cache/goby/analysis","fingerprintPath":"/opt/goby/bin/goby-intro-fingerprint","fingerprintSHA256":"` + strings.Repeat("a", 64) + `"}`))
	if err != nil || configuration.FingerprintSHA256 != strings.Repeat("a", 64) {
		t.Fatal("a pinned independent fingerprint helper was not retained")
	}
	if _, err := parseMediaAnalysis([]byte(`{}`)); err != nil {
		t.Fatal("zero deployment inventory must remain disabled")
	}
}

func TestMediaAnalysisDeploymentRejectsAmbiguousOrUnboundedInventory(t *testing.T) {
	for _, input := range []string{
		`null`, `[]`, `{} {}`, `{"Enabled":true}`, `{"enabled":true,"enabled":false}`,
		`{"enabled":null}`, `{"enabled":"true"}`, `{"enabled":false,"fingerprintPath":"/opt/tool"}`,
		`{"enabled":true,"cacheDirectory":"/"}`, `{"enabled":true,"cacheDirectory":"/tmp/../cache"}`,
		`{"enabled":true,"cacheDirectory":"cache"}`, `{"enabled":true,"cacheDirectory":"/cache","cacheMaxBytes":0}`,
		`{"enabled":true,"cacheDirectory":"/cache/\ud800"}`, `{"enabled":true,"cacheDirectory":"/cache/\udc00"}`,
		`{"enabled":true,"cacheDirectory":"/cache","cacheMaxBytes":4096,"maxEntryBytes":8192}`,
		`{"enabled":true,"cacheDirectory":"/cache","maxFileBytes":134217729}`,
		`{"enabled":true,"cacheDirectory":"/cache","fingerprintPath":"helper"}`,
		`{"enabled":true,"cacheDirectory":"/cache","fingerprintSHA256":"` + strings.Repeat("a", 64) + `"}`,
		`{"enabled":true,"cacheDirectory":"/cache","fingerprintPath":"/opt/helper","fingerprintSHA256":"` + strings.Repeat("A", 64) + `"}`,
		`{"enabled":true,"cacheDirectory":"/cache","unknown":true}`,
	} {
		if _, err := parseMediaAnalysis([]byte(input)); err == nil {
			t.Fatalf("accepted invalid deployment inventory: %s", input)
		}
	}
}

func TestMediaAnalysisCacheCannotOverlapIndependentStoresOrSourceRoots(t *testing.T) {
	analysis, err := parseMediaAnalysis([]byte(`{"enabled":true,"cacheDirectory":"/srv/goby/analysis"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, other := range []string{"/srv/goby", "/srv/goby/analysis", "/srv/goby/analysis/media"} {
		configuration := Config{MediaAnalysis: analysis, MediaRoots: []string{other}}
		if err := configuration.validateMediaAnalysis(); err == nil {
			t.Fatal("analysis cache overlaps an independently owned media root")
		}
		configuration.MediaRoots = nil
		configuration.MediaOperations.ScratchDirectory = other
		if err := configuration.validateMediaAnalysis(); err == nil {
			t.Fatal("analysis cache overlaps an independent operation workspace")
		}
	}
	configuration := Config{MediaAnalysis: analysis, MediaRoots: []string{"/srv/goby/analysis-other"}}
	if err := configuration.validateMediaAnalysis(); err != nil {
		t.Fatal("a lexical prefix without a directory boundary is not overlap")
	}
	configuration.Recovery = RecoveryConfig{Directory: "/srv/goby", OperationsDirectory: "/recovery-ops"}
	if err := configuration.validateMediaAnalysis(); err == nil {
		t.Fatal("analysis cache overlaps recovery state")
	}
}
