package config

import (
	"errors"
	"os"

	"github.com/moooyo/goby/internal/updates"
)

// UpdateConfig is immutable deployment configuration. Administrators may check
// this source but cannot replace its URL through the HTTP management surface.
type UpdateConfig struct {
	FeedURL string `json:"-"`
}

func (UpdateConfig) String() string   { return "<update-source configuration>" }
func (UpdateConfig) GoString() string { return "<update-source configuration>" }

func loadUpdates() (UpdateConfig, error) {
	configuration := UpdateConfig{FeedURL: os.Getenv("GOBY_UPDATE_FEED_URL")}
	return configuration, configuration.Validate()
}

func (configuration UpdateConfig) Validate() error {
	if configuration.FeedURL != "" && updates.ValidateFeedURL(configuration.FeedURL) != nil {
		return errors.New("GOBY_UPDATE_FEED_URL must be an HTTPS URL on port 443 with a DNS hostname and without credentials, query, or fragment")
	}
	return nil
}
