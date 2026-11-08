package config

import "testing"

func TestTranscodingTempPathRequiresDedicatedCanonicalLocation(t *testing.T) {
	excluded := []string{"/media", "/var/cache/goby/timeshift", "/var/lib/goby/analysis",
		"/var/lib/goby/operations", "/var/log/goby", "/var/lib/goby/recovery", "/var/lib/goby/scan-evidence"}
	for _, value := range []string{"relative", "/", "/var/cache/../cache/goby", "/media", "/media/transcodes",
		"/var", "/var/cache/goby/timeshift/sub", "/var/lib/goby", "/private\ncache", "/private\\cache"} {
		if err := ValidateTranscodingTempPath(value, excluded); err == nil {
			t.Fatalf("accepted an invalid or overlapping transcoding directory: %q", value)
		}
	}
	for _, value := range []string{"", "/var/cache/goby/transcodes", "/media-transcodes", "/private/transcodes"} {
		if err := ValidateTranscodingTempPath(value, excluded); err != nil {
			t.Fatalf("rejected a dedicated transcoding directory: %q: %v", value, err)
		}
	}
}
