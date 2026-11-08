package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestSavedProviderConnectionUsesFixedEndpointsAndRedactsResponses(t *testing.T) {
	for _, test := range []struct{ provider, host, path, body string }{
		{"tmdb", "api.themoviedb.org", "/3/configuration", `{"images":{"base_url":"https://image.tmdb.org/t/p/"}}`},
		{"musicbrainz", "musicbrainz.org", "/ws/2/artist/", `{"created":"2026-11-06","count":0,"artists":[]}`},
		{"opensubtitles", "api.opensubtitles.com", "/api/v1/login", `{"token":"temporary-login-secret"}`},
	} {
		t.Run(test.provider, func(t *testing.T) {
			configuration := Config{Enabled: true, TMDBToken: "private-tmdb-token", MusicBrainzUserAgent: "Goby test (contact@example.test)", OpenSubtitlesAPIKey: "private-api-key", OpenSubtitlesUsername: "private-username", OpenSubtitlesPassword: "private-password", OpenSubtitlesUserAgent: "Goby test"}
			client := New(configuration)
			calls := 0
			client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.Scheme != "https" || request.URL.Host != test.host || request.URL.Path != test.path || request.URL.User != nil {
					t.Fatal("test contacted an unexpected endpoint")
				}
				if request.URL.Query().Get("api_key") != "" {
					t.Fatal("test exposed an API credential in its URL")
				}
				switch test.provider {
				case "tmdb":
					if request.Header.Get("Authorization") != "Bearer "+configuration.TMDBToken {
						t.Fatal("test omitted the saved TMDB credential")
					}
				case "opensubtitles":
					var login map[string]string
					if request.Method != http.MethodPost || json.NewDecoder(request.Body).Decode(&login) != nil || login["username"] != configuration.OpenSubtitlesUsername || login["password"] != configuration.OpenSubtitlesPassword {
						t.Fatal("test omitted the saved subtitle login")
					}
					if request.Header.Get("Api-Key") != configuration.OpenSubtitlesAPIKey {
						t.Fatal("test omitted the saved subtitle API key")
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(test.body)), Header: make(http.Header)}, nil
			})}
			result, err := client.TestConnection(context.Background(), test.provider)
			if err != nil || result.Status != "passed" || result.Provider != test.provider || result.CheckedAt.IsZero() || calls != 1 {
				t.Fatal("saved provider connection did not complete exactly one check")
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{configuration.TMDBToken, configuration.OpenSubtitlesAPIKey, configuration.OpenSubtitlesUsername, configuration.OpenSubtitlesPassword, "temporary-login-secret"} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("connection result revealed private provider data")
				}
			}
		})
	}
}

func TestProviderConnectionOutcomesDoNotExposeUpstreamBodies(t *testing.T) {
	for _, test := range []struct {
		status int
		want   string
	}{{401, "authentication_failed"}, {403, "authentication_failed"}, {429, "quota"}, {503, "unavailable"}} {
		t.Run(test.want+http.StatusText(test.status), func(t *testing.T) {
			client := New(Config{Enabled: true, TMDBToken: "saved-private-token"})
			client.http = &http.Client{Transport: providerRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Body: io.NopCloser(strings.NewReader("private-upstream-detail")), Header: make(http.Header)}, nil
			})}
			result, err := client.TestConnection(context.Background(), "tmdb")
			if err != nil || result.Status != test.want {
				t.Fatal("provider status was not classified safely")
			}
		})
	}
}

func TestProviderConnectionDoesNotSendDisabledOrIncompleteCredentials(t *testing.T) {
	for _, test := range []struct {
		configuration Config
		want          string
	}{{Config{TMDBToken: "saved-token"}, "disabled"}, {Config{Enabled: true}, "not_configured"}} {
		client := New(test.configuration)
		client.http = &http.Client{Transport: providerRoundTrip(func(*http.Request) (*http.Response, error) {
			t.Fatal("an unavailable configuration sent a provider request")
			return nil, errors.New("unexpected request")
		})}
		result, err := client.TestConnection(context.Background(), "tmdb")
		if err != nil || result.Status != test.want {
			t.Fatal("unavailable provider configuration was not reported")
		}
	}
	if _, err := New(Config{Enabled: true}).TestConnection(context.Background(), "https://untrusted.invalid"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("an administrator supplied URL was accepted as a provider")
	}
}

func TestProviderConnectionHasOneTotalDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := New(Config{Enabled: true, TMDBToken: "saved-token"})
		calls := 0
		client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
			calls++
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}
		started := time.Now()
		result, err := client.TestConnection(context.Background(), "tmdb")
		if err != nil || result.Status != "unavailable" || calls != 1 || time.Since(started) != 15*time.Second {
			t.Fatal("the provider connection test escaped its total deadline or retried")
		}
	})
}

func TestProviderConnectionDoesNotFollowRedirects(t *testing.T) {
	client := New(Config{Enabled: true, TMDBToken: "saved-private-token"})
	calls := 0
	client.http = &http.Client{Transport: providerRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": {tmdbAPIBase + "/another-endpoint"}}}, nil
	})}
	result, err := client.TestConnection(context.Background(), "tmdb")
	if err != nil || result.Status != "unavailable" || calls != 1 {
		t.Fatal("the provider connection test followed a redirect or claimed success")
	}
}
