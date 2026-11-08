package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestUpdateSourceIsAnOptionalImmutableDeploymentSetting(t *testing.T) {
	for _, value := range []string{"", "https://updates.example.com/goby/stable.json"} {
		t.Setenv("GOBY_UPDATE_FEED_URL", value)
		configuration, err := loadUpdates()
		if err != nil || configuration.FeedURL != value {
			t.Fatal("update source deployment value was not preserved")
		}
		if strings.Contains(fmt.Sprintf("%v %#v", configuration, configuration), "updates.example.com") {
			t.Fatal("structured configuration logging exposed a release-source URL")
		}
	}
	for _, value := range []string{"https://127.0.0.1/source.json", "http://updates.example.com/source.json", "https://user:secret@updates.example.com/source.json", "https://updates.example.com/source.json?token=secret", "https://updates.example.com:8443/source.json"} {
		t.Setenv("GOBY_UPDATE_FEED_URL", value)
		_, err := loadUpdates()
		if err == nil || !strings.Contains(err.Error(), "GOBY_UPDATE_FEED_URL") || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid deployment source was accepted or disclosed")
		}
	}
	startupConfigEnvironment(t)
	t.Setenv("GOBY_UPDATE_FEED_URL", "https://updates.example.com/goby/stable.json")
	configuration, err := Load()
	if err != nil || configuration.Updates.FeedURL != "https://updates.example.com/goby/stable.json" {
		t.Fatal("main configuration did not load the update source")
	}
}
