package providers

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

const (
	openSubtitlesAPIURL           = "https://api.opensubtitles.com/api/v1"
	openSubtitlesMaxResults       = 100
	openSubtitlesMaxBytes   int64 = 8 << 20
)

type openSubtitlesSearchResponse struct {
	Data []struct {
		ID         string `json:"id"`
		Attributes struct {
			SubtitleID       string `json:"subtitle_id"`
			Language         string `json:"language"`
			Release          string `json:"release"`
			HearingImpaired  bool   `json:"hearing_impaired"`
			ForeignPartsOnly bool   `json:"foreign_parts_only"`
			DownloadCount    int64  `json:"download_count"`
			MovieHashMatch   bool   `json:"moviehash_match"`
			Files            []struct {
				FileID   int64  `json:"file_id"`
				FileName string `json:"file_name"`
			} `json:"files"`
		} `json:"attributes"`
	} `json:"data"`
}

// SearchSubtitles performs one bounded search page without consuming download quota.
func (client *Client) SearchSubtitles(ctx context.Context, query SubtitleQuery) ([]RemoteSubtitle, error) {
	if !client.config.Enabled {
		return nil, ErrDisabled
	}
	headers, err := client.openSubtitlesHeaders()
	if err != nil {
		return nil, err
	}
	params, err := client.openSubtitlesSearchParams(query)
	if err != nil {
		return nil, err
	}
	var response openSubtitlesSearchResponse
	if err := client.requestJSON(ctx, http.MethodGet, openSubtitlesAPIURL+"/subtitles?"+params.Encode(), headers, nil, &response); err != nil {
		return nil, err
	}
	if response.Data == nil {
		return nil, ErrUnavailable
	}
	results := make([]RemoteSubtitle, 0)
	seen := make(map[int64]bool)
	for _, entry := range response.Data {
		id, ok := openSubtitlesNumericID(entry.ID)
		if !ok {
			id, ok = openSubtitlesNumericID(entry.Attributes.SubtitleID)
		}
		language, validLanguage := openSubtitlesLanguage(entry.Attributes.Language)
		if !ok || !validLanguage {
			continue
		}
		for _, file := range entry.Attributes.Files {
			if file.FileID <= 0 || seen[file.FileID] {
				continue
			}
			name := openSubtitlesDisplayName(file.FileName)
			if name == "" {
				name = openSubtitlesDisplayName(entry.Attributes.Release)
			}
			if name == "" {
				name = "OpenSubtitles " + id
			}
			downloadCount := entry.Attributes.DownloadCount
			if downloadCount < 0 {
				downloadCount = 0
			}
			results = append(results, RemoteSubtitle{
				Provider: "opensubtitles", ID: id, FileID: file.FileID,
				Language: language, Name: name,
				HearingImpaired: entry.Attributes.HearingImpaired,
				IsForced:        entry.Attributes.ForeignPartsOnly,
				DownloadCount:   downloadCount, MovieHashMatch: entry.Attributes.MovieHashMatch,
			})
			seen[file.FileID] = true
			if len(results) == openSubtitlesMaxResults {
				return results, nil
			}
		}
	}
	return results, nil
}

// DownloadSubtitle requests UTF-8 SRT bytes and keeps credentials and temporary URLs private.
// Each explicit download obtains a fresh login token; quota-bearing requests are never retried here.
func (client *Client) DownloadSubtitle(ctx context.Context, selection RemoteSubtitle) (SubtitleDownload, error) {
	if !client.config.Enabled {
		return SubtitleDownload{}, ErrDisabled
	}
	id, validID := openSubtitlesNumericID(selection.ID)
	language, validLanguage := openSubtitlesLanguage(selection.Language)
	if selection.Provider != "opensubtitles" || !validID || selection.FileID <= 0 || !validLanguage {
		return SubtitleDownload{}, ErrInvalidInput
	}
	headers, err := client.openSubtitlesHeaders()
	if err != nil {
		return SubtitleDownload{}, err
	}
	if strings.TrimSpace(client.config.OpenSubtitlesUsername) == "" || strings.TrimSpace(client.config.OpenSubtitlesPassword) == "" {
		return SubtitleDownload{}, ErrNotConfigured
	}
	var login struct {
		Token   string `json:"token"`
		BaseURL string `json:"base_url"`
	}
	credentials := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{Username: client.config.OpenSubtitlesUsername, Password: client.config.OpenSubtitlesPassword}
	if err := client.requestJSON(ctx, http.MethodPost, openSubtitlesAPIURL+"/login", headers, credentials, &login); err != nil {
		return SubtitleDownload{}, err
	}
	if login.Token == "" || len(login.Token) > 8192 || strings.IndexFunc(login.Token, unicode.IsSpace) >= 0 || strings.IndexFunc(login.Token, unicode.IsControl) >= 0 {
		return SubtitleDownload{}, ErrUnavailable
	}
	apiURL, ok := openSubtitlesLoginURL(login.BaseURL)
	if !ok {
		return SubtitleDownload{}, ErrUnavailable
	}
	headers.Set("Authorization", "Bearer "+login.Token)
	var response struct {
		Link string `json:"link"`
	}
	body := struct {
		FileID int64  `json:"file_id"`
		Format string `json:"sub_format"`
	}{FileID: selection.FileID, Format: "srt"}
	if err := client.requestJSON(ctx, http.MethodPost, apiURL+"/download", headers, body, &response); err != nil {
		return SubtitleDownload{}, err
	}
	if !openSubtitlesDownloadURL(response.Link) {
		return SubtitleDownload{}, ErrUnavailable
	}
	// The returned URL is already authorized. Never forward API credentials to file hosts.
	fileHeaders := make(http.Header)
	fileHeaders.Set("User-Agent", headers.Get("User-Agent"))
	fileHeaders.Set("Accept", "application/x-subrip, text/plain, application/octet-stream")
	data, _, err := client.requestBytes(ctx, http.MethodGet, response.Link, fileHeaders, nil, openSubtitlesMaxBytes)
	if err != nil {
		return SubtitleDownload{}, err
	}
	if len(data) == 0 || int64(len(data)) > openSubtitlesMaxBytes || !utf8.Valid(data) {
		return SubtitleDownload{}, ErrUnavailable
	}
	return SubtitleDownload{
		Data: data, Format: "srt", Language: language,
		Provider: "opensubtitles", RemoteID: id + ":" + strconv.FormatInt(selection.FileID, 10),
		HearingImpaired: selection.HearingImpaired, IsForced: selection.IsForced,
	}, nil
}

func (client *Client) openSubtitlesHeaders() (http.Header, error) {
	key := strings.TrimSpace(client.config.OpenSubtitlesAPIKey)
	userAgent := strings.TrimSpace(client.config.OpenSubtitlesUserAgent)
	if key == "" || userAgent == "" || len(key) > 4096 || len(userAgent) > 512 || strings.IndexFunc(key, unicode.IsControl) >= 0 || strings.IndexFunc(userAgent, unicode.IsControl) >= 0 {
		return nil, ErrNotConfigured
	}
	headers := make(http.Header)
	headers.Set("Api-Key", key)
	headers.Set("User-Agent", userAgent)
	headers.Set("Accept", "application/json")
	return headers, nil
}

func (client *Client) openSubtitlesSearchParams(query SubtitleQuery) (url.Values, error) {
	name := strings.TrimSpace(query.Name)
	if len(name) > 1024 || !utf8.ValidString(name) || strings.IndexFunc(name, unicode.IsControl) >= 0 || query.Year < 0 || query.Year > 9999 || query.Season < 0 || query.Season > 100000 || query.Episode < 0 || query.Episode > 100000 || len(query.Languages) > 20 {
		return nil, ErrInvalidInput
	}
	params := make(url.Values)
	switch strings.ToLower(strings.TrimSpace(query.Type)) {
	case "", "video", "all":
		params.Set("type", "all")
	case "movie":
		params.Set("type", "movie")
	case "episode":
		params.Set("type", "episode")
	default:
		return nil, ErrInvalidInput
	}
	if name != "" {
		params.Set("query", name)
	}
	for key, value := range query.ProviderIDs {
		var parameter string
		switch strings.ToLower(key) {
		case "imdb":
			parameter = "imdb_id"
			value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "tt")
		case "tmdb":
			parameter = "tmdb_id"
			if strings.Contains(value, ":") {
				parts := strings.Split(value, ":")
				if params.Get("type") != "episode" || len(parts) != 3 {
					return nil, ErrInvalidInput
				}
				series, validSeries := openSubtitlesNumericID(parts[0])
				episode, validEpisode := openSubtitlesNumericID(parts[2])
				season, validSeason := openSubtitlesNumericID(parts[1])
				if parts[1] == "0" {
					season, validSeason = "0", true
				}
				if !validSeries || !validSeason || !validEpisode || (query.Season > 0 && strconv.Itoa(query.Season) != season) || (query.Episode > 0 && strconv.Itoa(query.Episode) != episode) {
					return nil, ErrInvalidInput
				}
				params.Set("parent_tmdb_id", series)
				params.Set("season_number", season)
				params.Set("episode_number", episode)
				continue
			}
		default:
			continue
		}
		id, ok := openSubtitlesNumericID(value)
		if !ok {
			return nil, ErrInvalidInput
		}
		params.Set(parameter, id)
	}
	if name == "" && params.Get("imdb_id") == "" && params.Get("tmdb_id") == "" && params.Get("parent_tmdb_id") == "" {
		return nil, ErrInvalidInput
	}
	if query.Year > 0 {
		params.Set("year", strconv.Itoa(query.Year))
	}
	if query.Season > 0 {
		params.Set("season_number", strconv.Itoa(query.Season))
	}
	if query.Episode > 0 {
		params.Set("episode_number", strconv.Itoa(query.Episode))
	}
	languages := query.Languages
	if len(languages) == 0 {
		language := strings.TrimSpace(query.Language)
		if language == "" {
			language = strings.TrimSpace(client.config.Language)
		}
		if language != "" {
			languages = []string{language}
		}
	}
	normalizedLanguages := make([]string, 0, len(languages))
	seenLanguages := make(map[string]bool)
	for _, value := range languages {
		language, ok := openSubtitlesLanguage(value)
		if !ok {
			return nil, ErrInvalidInput
		}
		if !seenLanguages[language] {
			normalizedLanguages = append(normalizedLanguages, language)
			seenLanguages[language] = true
		}
	}
	if len(normalizedLanguages) > 0 {
		params.Set("languages", strings.Join(normalizedLanguages, ","))
	}
	if query.HearingImpaired {
		params.Set("hearing_impaired", "only")
	}
	params.Set("page", "1")
	return params, nil
}

func openSubtitlesNumericID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 19 {
		return "", false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return "", false
		}
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return "", false
	}
	return strconv.FormatInt(id, 10), true
}

func openSubtitlesLanguage(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	if len(value) == 3 {
		tag, err := language.Parse(value)
		if err != nil {
			return "", false
		}
		base, _ := tag.Base()
		value = base.String()
	}
	if len(value) != 2 && len(value) != 5 {
		return "", false
	}
	for index, character := range value {
		if len(value) == 5 && index == 2 && character == '-' {
			continue
		}
		if character < 'a' || character > 'z' {
			return "", false
		}
	}
	if len(value) == 5 && value[2] != '-' {
		return "", false
	}
	// The API distinguishes Chinese and Portuguese variants; other locale regions use their language.
	if len(value) == 5 && !strings.HasPrefix(value, "zh-") && !strings.HasPrefix(value, "pt-") {
		value = value[:2]
	}
	return value, true
}

func openSubtitlesDisplayName(value string) string {
	value = strings.TrimSpace(strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, strings.ToValidUTF8(value, "")))
	if len(value) > 512 {
		value = value[:512]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func openSubtitlesLoginURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return openSubtitlesAPIURL, true
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", false
	}
	if parsed.Host != "api.opensubtitles.com" && parsed.Host != "vip-api.opensubtitles.com" {
		return "", false
	}
	if parsed.Path != "" && parsed.Path != "/" && parsed.Path != "/api/v1" && parsed.Path != "/api/v1/" {
		return "", false
	}
	return "https://" + parsed.Host + "/api/v1", true
}

func openSubtitlesDownloadURL(value string) bool {
	if len(value) > 8192 {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "opensubtitles.com" || strings.HasSuffix(host, ".opensubtitles.com")
}
