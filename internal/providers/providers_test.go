package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func TestProviderTransportRejectsNonPublicDestinations(t *testing.T) {
	for _, raw := range []string{"http://api.themoviedb.org/3/movie/1", "https://api.themoviedb.org.evil.example/", "https://127.0.0.1/", "https://api.themoviedb.org:444/", "https://secret@api.themoviedb.org/", "https://api.themoviedb.org/#fragment"} {
		if _, err := safeURL(raw); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "::1", "fc00::1", "::ffff:192.168.0.1", "64:ff9b::a00:1", "2001:db8::1"} {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("accepted nonpublic IP %s", raw)
		}
	}
	if !publicAddress(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("rejected public IPv4")
	}
}

func TestProviderConfigNeverSerializesCredentials(t *testing.T) {
	config := Config{Enabled: true, TMDBToken: "tmdb-secret", MusicBrainzUserAgent: "contact@example.test", OpenSubtitlesAPIKey: "api-secret", OpenSubtitlesPassword: "password-secret", OpenSubtitlesUsername: "private-user", OpenSubtitlesUserAgent: "private-agent", Language: "en", Country: "US"}
	data, err := json.Marshal(config)
	if err != nil || string(data) != "{}" {
		t.Fatalf("private configuration was serialized: %s, %v", data, err)
	}
	data, err = json.Marshal(config.Status())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{config.TMDBToken, config.OpenSubtitlesAPIKey, config.OpenSubtitlesPassword, config.OpenSubtitlesUsername, config.MusicBrainzUserAgent} {
		if strings.Contains(string(data), secret) {
			t.Fatal("status leaked private configuration")
		}
	}
}

type providerRoundTrip func(*http.Request) (*http.Response, error)

func (roundTrip providerRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestProviderResponseIsBoundedAndErrorsAreRedacted(t *testing.T) {
	client := New(Config{Enabled: true})
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("private-token upstream detail")), Header: make(http.Header)}, nil
	})}
	_, _, err := client.requestBytes(context.Background(), http.MethodGet, "https://api.themoviedb.org/3/movie/1", nil, nil, 32)
	if !errors.Is(err, ErrQuota) || strings.Contains(err.Error(), "private-token") {
		t.Fatalf("unexpected quota error: %v", err)
	}
	client.http.Transport = providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("a", 33))), Header: make(http.Header)}, nil
	})
	if _, _, err := client.requestBytes(context.Background(), http.MethodGet, "https://api.themoviedb.org/3/movie/1", nil, nil, 32); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("accepted oversized body: %v", err)
	}
}

func TestProviderIdentifiersCannotBecomePaths(t *testing.T) {
	for _, id := range []string{"../movie/1", "1?api_key=x", "1/2", "-1", "0", "1:../../etc"} {
		_, err := New(Config{Enabled: true, TMDBToken: "unused"}).Lookup(context.Background(), Selection{Provider: "tmdb", Type: "Movie", ID: id})
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unsafe identifier %q was accepted: %v", id, err)
		}
	}
	for _, id := range []string{"../release-group/id", "00000000-0000-0000-0000-00000000000?", "https://musicbrainz.org/"} {
		if musicBrainzID(id) {
			t.Fatalf("accepted invalid MBID %q", id)
		}
	}
}
