package providers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
)

// ConnectionTest describes an explicit request using the saved configuration.
// It contains no credentials, response bodies, redirect URLs, or bearer tokens.
type ConnectionTest struct {
	Provider  string    `json:"Provider"`
	Status    string    `json:"Status"`
	CheckedAt time.Time `json:"CheckedAt"`
}

// TestConnection authenticates against a fixed provider endpoint without
// searching a user's media or consuming subtitle download quota. A successful
// test establishes reachability and the tested credentials, not future quota,
// the availability of any particular title, or every provider operation.
func (client *Client) TestConnection(ctx context.Context, providerID string) (ConnectionTest, error) {
	result := ConnectionTest{Provider: providerID, CheckedAt: time.Now().UTC()}
	var configured bool
	for _, status := range client.config.Status() {
		if status.ID == providerID {
			configured = status.Configured
		}
	}
	if providerID != "tmdb" && providerID != "musicbrainz" && providerID != "opensubtitles" {
		return ConnectionTest{}, ErrInvalidInput
	}
	if !client.config.Enabled {
		result.Status = "disabled"
		return result, nil
	}
	if !configured {
		result.Status = "not_configured"
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// A redirect would be another request and would no longer test the fixed
	// endpoint selected here. The ordinary adapter redirect policy is unchanged.
	requestClient := *client
	httpClient := *client.http
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	requestClient.http = &httpClient
	client = &requestClient
	var err error
	switch providerID {
	case "tmdb":
		headers, headerErr := client.tmdbHeaders()
		if headerErr != nil {
			err = headerErr
			break
		}
		var response struct {
			Images *struct {
				BaseURL string `json:"base_url"`
			} `json:"images"`
		}
		err = client.requestJSON(ctx, http.MethodGet, tmdbAPIBase+"/configuration", headers, nil, &response)
		if err == nil && response.Images == nil {
			err = ErrUnavailable
		}
	case "musicbrainz":
		var response struct {
			Created string           `json:"created"`
			Count   *int             `json:"count"`
			Artists []jsonArtistTest `json:"artists"`
		}
		err = client.requestMusicBrainz(ctx, "artist/?query=artist%3AGoby&fmt=json&limit=1", &response)
		if err == nil && (response.Count == nil || *response.Count < 0) {
			err = ErrUnavailable
		}
	case "opensubtitles":
		headers, headerErr := client.openSubtitlesHeaders()
		if headerErr != nil {
			err = headerErr
			break
		}
		var response struct {
			Token string `json:"token"`
		}
		credentials := struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}{client.config.OpenSubtitlesUsername, client.config.OpenSubtitlesPassword}
		err = client.requestJSON(ctx, http.MethodPost, openSubtitlesAPIURL+"/login", headers, credentials, &response)
		if err == nil && (response.Token == "" || len(response.Token) > 8192 || strings.IndexFunc(response.Token, unicode.IsSpace) >= 0 || strings.IndexFunc(response.Token, unicode.IsControl) >= 0) {
			err = ErrUnavailable
		}
		// No temporary login token is returned or cached after the test.
		response.Token = ""
	}
	result.CheckedAt = time.Now().UTC()
	switch {
	case err == nil:
		result.Status = "passed"
	case errors.Is(err, context.Canceled):
		return ConnectionTest{}, err
	case errors.Is(err, ErrNotConfigured):
		result.Status = "authentication_failed"
	case errors.Is(err, ErrQuota):
		result.Status = "quota"
	default:
		result.Status = "unavailable"
	}
	return result, nil
}

type jsonArtistTest struct {
	ID string `json:"id"`
}
