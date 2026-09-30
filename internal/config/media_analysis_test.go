package config

import (
	"encoding/json"
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

func TestMediaAnalysisDeploymentRetainsIndependentIntroFFmpegInventory(t *testing.T) {
	const executable = "/opt/goby/intro/bin/ffmpeg"
	for _, fields := range []string{
		`"introFFmpegPath":"` + executable + `"`,
		`"introFFmpegPath":"` + executable + `","introFFmpegSHA256":"` + strings.Repeat("b", 64) + `"`,
		`"introFFmpegPath":"` + executable + `","introFFmpegSHA256":"` + strings.Repeat("b", 64) + `","fingerprintPath":"/opt/goby/fingerprint","fingerprintSHA256":"` + strings.Repeat("a", 64) + `"`,
	} {
		value, err := parseMediaAnalysis([]byte(`{"enabled":true,"cacheDirectory":"/cache",` + fields + `}`))
		if err != nil || value.IntroFFmpegPath != executable {
			t.Fatalf("the independent intro decoder inventory was not retained: %+v, %v", value, err)
		}
		if strings.Contains(fields, "introFFmpegSHA256") && value.IntroFFmpegSHA256 != strings.Repeat("b", 64) {
			t.Fatal("the intro decoder digest was confused with the fingerprint helper digest")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := parseMediaAnalysis(encoded)
		if err != nil || roundTrip != value {
			t.Fatalf("trusted deployment JSON lost its independent decoder binding: %+v, %v", roundTrip, err)
		}
	}
	value, err := parseMediaAnalysis([]byte(`{"enabled":true,"cacheDirectory":"/cache"}`))
	if err != nil || value.IntroFFmpegPath != "" || value.IntroFFmpegSHA256 != "" {
		t.Fatal("preview-only deployment invented an intro decoder inventory")
	}
	encoded, err := json.Marshal(value)
	if err != nil || strings.Contains(string(encoded), "introFFmpeg") {
		t.Fatalf("empty optional decoder inventory was serialized: %s, %v", encoded, err)
	}
}

func TestMediaAnalysisDeploymentRejectsInvalidIntroFFmpegBindings(t *testing.T) {
	for _, fields := range []string{
		`"introFFmpegPath":"ffmpeg"`,
		`"introFFmpegPath":"/"`,
		`"introFFmpegPath":"/opt/../ffmpeg"`,
		`"introFFmpegPath":"/opt//ffmpeg"`,
		`"introFFmpegPath":"C:\\tools\\ffmpeg.exe"`,
		`"introFFmpegPath":"/opt/ffmpeg\n"`,
		`"introFFmpegPath":null`,
		`"introFFmpegPath":42`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegPath":"/other/ffmpeg"`,
		`"introFFmpegPath":"/opt/ffmpeg","\u0069ntroFFmpegPath":"/other/ffmpeg"`,
		`"IntroFFmpegPath":"/opt/ffmpeg"`,
		`"introFFmpegSHA256":"` + strings.Repeat("b", 64) + `"`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegSHA256":null`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegSHA256":"` + strings.Repeat("B", 64) + `"`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegSHA256":"` + strings.Repeat("g", 64) + `"`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegSHA256":"` + strings.Repeat("b", 63) + `"`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegSHA256":"` + strings.Repeat("b", 65) + `"`,
		`"introFFmpegPath":"/opt/ffmpeg","introFFmpegSHA256":"` + strings.Repeat("b", 64) + `","introFFmpegSHA256":"` + strings.Repeat("a", 64) + `"`,
	} {
		value, err := parseMediaAnalysis([]byte(`{"enabled":true,"cacheDirectory":"/cache",` + fields + `}`))
		if err == nil || value != (MediaAnalysisConfig{}) {
			t.Fatalf("an invalid intro decoder binding returned execution inventory: %s, %+v, %v", fields, value, err)
		}
	}
	for _, fields := range []string{`"introFFmpegPath":"/opt/ffmpeg"`, `"introFFmpegSHA256":"` + strings.Repeat("b", 64) + `"`} {
		if _, err := parseMediaAnalysis([]byte(`{"enabled":false,` + fields + `}`)); err == nil {
			t.Fatal("disabled analysis retained an executable intro decoder inventory")
		}
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
