package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateProviderSettingSources(t *testing.T) {
	const key = "GOBY_TEST_PROVIDER_CREDENTIAL"
	t.Setenv(key, "")
	t.Setenv(key+"_FILE", "")
	if value, err := loadPrivateSetting(key); err != nil || value != "" {
		t.Fatalf("absent credential: %q, %v", value, err)
	}
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("private-value\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(key+"_FILE", path)
	if value, err := loadPrivateSetting(key); err != nil || value != "private-value" {
		t.Fatalf("file credential was not loaded correctly: %v", err)
	}
	t.Setenv(key, "different-value")
	if _, err := loadPrivateSetting(key); err == nil {
		t.Fatal("conflicting configuration must fail")
	}
	t.Setenv(key, "")
	for _, content := range []string{"", "first\nsecond", strings.Repeat("x", maxProviderCredentialBytes+1)} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadPrivateSetting(key); err == nil {
			t.Fatal("invalid file credential accepted")
		}
	}
	// Failure messages identify the configuration key, never a private path.
	t.Setenv(key+"_FILE", filepath.Join(t.TempDir(), "private-marker", "missing"))
	if _, err := loadPrivateSetting(key); err == nil || strings.Contains(err.Error(), "private-marker") {
		t.Fatal("missing credential file must produce a redacted error")
	}
}

func TestOnlineProviderCredentialsAreRedacted(t *testing.T) {
	c := OnlineProvidersConfig{Enabled: true, TMDBToken: "private-marker", OpenSubtitlesPassword: "private-marker"}
	encoded, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{string(encoded), fmt.Sprint(c), fmt.Sprintf("%+v", c), fmt.Sprintf("%#v", c)} {
		if strings.Contains(output, "private-marker") {
			t.Fatal("credentials escaped through configuration formatting")
		}
	}
	if err := c.Validate(); err == nil {
		t.Fatal("incomplete OpenSubtitles account accepted")
	}
	c.OpenSubtitlesUsername = "account"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
