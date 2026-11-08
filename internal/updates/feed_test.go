package updates

import (
	"encoding/json"
	"strings"
	"testing"
)

func fixtureFeed(t *testing.T, version string) []byte {
	t.Helper()
	data, err := json.Marshal(feedDocument{SchemaVersion: 1, Product: "goby", Release: feedRelease{
		Version: version, Classification: "Release", PublishedAt: "2026-11-06T08:00:00Z", Description: "A verified Docker release.\nReview its deployment instructions.",
		InfoURL: "https://releases.example.com/goby/0.2.0", SourceURL: "https://releases.example.com/goby-image.tar",
		SHA256: strings.Repeat("a", 64), TargetFilename: "goby-0.2.0-linux-amd64-image.tar",
	}})
	if err != nil {
		t.Fatal("encode release fixture")
	}
	return data
}

func TestFeedRequiresExactLosslessGobyArtifactContract(t *testing.T) {
	valid := string(fixtureFeed(t, "0.2.0"))
	release, err := parseFeed([]byte(valid))
	if err != nil || release.Version != "0.2.0" || release.Classification != "Release" || release.PublishedAt.IsZero() {
		t.Fatal("valid Goby release feed was rejected")
	}
	for _, test := range []struct{ name, payload string }{
		{"wrong-product", strings.Replace(valid, `"product":"goby"`, `"product":"emby"`, 1)},
		{"unknown-schema", strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":2`, 1)},
		{"duplicate-product", strings.Replace(valid, `"product":"goby"`, `"product":"emby","product":"goby"`, 1)},
		{"case-alias", strings.Replace(valid, `"version":"0.2.0"`, `"Version":"0.2.0"`, 1)},
		{"duplicate-release-version", strings.Replace(valid, `"version":"0.2.0"`, `"version":"0.1.0","version":"0.2.0"`, 1)},
		{"missing-description", strings.Replace(valid, `"description":"A verified Docker release.\nReview its deployment instructions.",`, "", 1)},
		{"null-description", strings.Replace(valid, `"description":"A verified Docker release.\nReview its deployment instructions."`, `"description":null`, 1)},
		{"unpaired-surrogate", strings.Replace(valid, `"description":"A verified Docker release.\nReview its deployment instructions."`, `"description":"\ud800"`, 1)},
		{"unknown-field", strings.Replace(valid, `"schemaVersion":1`, `"schemaVersion":1,"autoInstall":true`, 1)},
		{"unversioned-artifact", strings.Replace(valid, `"version":"0.2.0"`, `"version":"main"`, 1)},
		{"invalid-classification", strings.Replace(valid, `"classification":"Release"`, `"classification":"Nightly"`, 1)},
		{"invalid-timestamp", strings.Replace(valid, `2026-11-06T08:00:00Z`, `2026-11-06`, 1)},
		{"utc-year-overflow", strings.Replace(valid, `2026-11-06T08:00:00Z`, `9999-12-31T23:59:59-01:00`, 1)},
		{"utc-year-underflow", strings.Replace(valid, `2026-11-06T08:00:00Z`, `0000-01-01T00:00:00+01:00`, 1)},
		{"local-artifact-url", strings.Replace(valid, `https://releases.example.com/goby-image.tar`, `https://127.0.0.1/goby-image.tar`, 1)},
		{"artifact-credentials", strings.Replace(valid, `https://releases.example.com/goby-image.tar`, `https://secret:token@releases.example.com/goby-image.tar`, 1)},
		{"invalid-hash", strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("G", 64), 1)},
		{"filename-traversal", strings.Replace(valid, `goby-0.2.0-linux-amd64-image.tar`, `../goby.tar`, 1)},
		{"extra-document", valid + `{}`},
		{"oversized", strings.Repeat(" ", MaxFeedBytes) + valid},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseFeed([]byte(test.payload)); err == nil {
				t.Fatal("unsafe or ambiguous feed was accepted")
			}
		})
	}
	paired := strings.Replace(valid, `"description":"A verified Docker release.\nReview its deployment instructions."`, `"description":"Release \ud83d\udce6"`, 1)
	if _, err := parseFeed([]byte(paired)); err != nil {
		t.Fatal("valid escaped Unicode pair was rejected")
	}
}

func TestFeedURLSyntaxNeverAcceptsUserCredentialsOrLiteralDestinations(t *testing.T) {
	for _, value := range []string{"https://updates.example.com/latest.json", "https://updates.example.com:443/releases/stable.json"} {
		if err := ValidateFeedURL(value); err != nil {
			t.Errorf("valid deployment URL was rejected: %v", err)
		}
	}
	for _, value := range []string{"", "http://updates.example.com/latest.json", "https://localhost/latest.json", "https://127.0.0.1/latest.json", "https://[::1]/latest.json", "https://93.184.216.34/latest.json", "https://0177.0.0.1/latest.json", "https://0x7f.0.0.1/latest.json", "https://updates.1/latest.json", "https://updates.0x/latest.json", "https://user:token@updates.example.com/latest.json", "https://updates.example.com:8443/latest.json", "https://updates.example.com/latest.json?token=secret", "https://updates.example.com/latest.json?", "https://updates.example.com/latest.json#fragment", "https://updates.example.com/latest.json#", "https://updates.example.com./latest.json", "https://updates..example.com/latest.json", "https://updates_example.com/latest.json", " https://updates.example.com/latest.json"} {
		if err := ValidateFeedURL(value); err == nil {
			t.Errorf("invalid deployment URL %q was accepted", value)
		}
	}
}
