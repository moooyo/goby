// Package providers implements bounded clients for supported online catalogs.
package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

var (
	ErrDisabled      = errors.New("online providers are disabled")
	ErrNotConfigured = errors.New("provider credentials are not configured")
	ErrInvalidInput  = errors.New("invalid provider input")
	ErrUnavailable   = errors.New("provider is unavailable")
	ErrNotFound      = errors.New("provider result was not found")
	ErrQuota         = errors.New("provider rate or download quota exceeded")
)

// Config contains private runtime credentials. It must never be serialized.
type Config struct {
	Enabled                bool   `json:"-"`
	TMDBToken              string `json:"-"`
	MusicBrainzUserAgent   string `json:"-"`
	OpenSubtitlesAPIKey    string `json:"-"`
	OpenSubtitlesUsername  string `json:"-"`
	OpenSubtitlesPassword  string `json:"-"`
	OpenSubtitlesUserAgent string `json:"-"`
	Language               string `json:"-"`
	Country                string `json:"-"`
}

type Status struct {
	ID           string   `json:"Id"`
	Name         string   `json:"Name"`
	Configured   bool     `json:"Configured"`
	Capabilities []string `json:"Capabilities"`
	Attribution  string   `json:"Attribution"`
	Website      string   `json:"Website"`
}

func (config Config) Status() []Status {
	return []Status{
		{ID: "tmdb", Name: "The Movie Database", Configured: config.Enabled && strings.TrimSpace(config.TMDBToken) != "", Capabilities: []string{"metadata", "images"}, Attribution: "This product uses the TMDB API but is not endorsed or certified by TMDB.", Website: "https://www.themoviedb.org"},
		{ID: "musicbrainz", Name: "MusicBrainz", Configured: config.Enabled && strings.TrimSpace(config.MusicBrainzUserAgent) != "", Capabilities: []string{"metadata"}, Attribution: "Music metadata provided by MusicBrainz.", Website: "https://musicbrainz.org"},
		{ID: "opensubtitles", Name: "OpenSubtitles", Configured: config.Enabled && config.OpenSubtitlesAPIKey != "" && config.OpenSubtitlesUserAgent != "" && config.OpenSubtitlesUsername != "" && config.OpenSubtitlesPassword != "", Capabilities: []string{"subtitles"}, Attribution: "Subtitles provided by OpenSubtitles.com.", Website: "https://www.opensubtitles.com"},
	}
}

type Query struct {
	Type        string            `json:"Type"`
	Name        string            `json:"Name"`
	Year        int               `json:"Year"`
	Language    string            `json:"Language"`
	Season      int               `json:"Season"`
	Episode     int               `json:"Episode"`
	ProviderIDs map[string]string `json:"ProviderIds"`
}

type Selection struct {
	Provider string `json:"Provider"`
	ID       string `json:"Id"`
	Type     string `json:"Type"`
	Language string `json:"Language"`
}

type Match struct {
	Selection
	Name          string  `json:"Name"`
	OriginalTitle string  `json:"OriginalTitle,omitempty"`
	Year          int     `json:"Year,omitempty"`
	Overview      string  `json:"Overview,omitempty"`
	Score         float64 `json:"Score,omitempty"`
}

// Fields use the administrator metadata wire names and contain sparse facts.
type Metadata struct {
	Selection
	Fields    map[string]json.RawMessage `json:"Fields"`
	SourceURL string                     `json:"SourceUrl"`
}

type RemoteImage struct {
	Selection
	ImageID    string `json:"ImageId"`
	ImageType  string `json:"ImageType"`
	Width      int    `json:"Width"`
	Height     int    `json:"Height"`
	PreviewURL string `json:"PreviewUrl"`
}

type SubtitleQuery struct {
	Query
	Languages       []string `json:"Languages"`
	HearingImpaired bool     `json:"HearingImpaired"`
}

type RemoteSubtitle struct {
	Provider        string `json:"Provider"`
	ID              string `json:"Id"`
	FileID          int64  `json:"FileId"`
	Language        string `json:"Language"`
	Name            string `json:"Name"`
	HearingImpaired bool   `json:"HearingImpaired"`
	IsForced        bool   `json:"IsForced"`
	DownloadCount   int64  `json:"DownloadCount"`
	MovieHashMatch  bool   `json:"MovieHashMatch"`
}

type SubtitleDownload struct {
	Data            []byte `json:"-"`
	Format          string `json:"Format"`
	Language        string `json:"Language"`
	Provider        string `json:"Provider"`
	RemoteID        string `json:"RemoteId"`
	HearingImpaired bool   `json:"HearingImpaired"`
	IsForced        bool   `json:"IsForced"`
}

type Client struct {
	config Config
	http   *http.Client
}

func New(config Config) *Client { return &Client{config: config, http: newHTTPClient()} }

func (client *Client) Status() []Status { return client.config.Status() }

func (client *Client) Search(ctx context.Context, provider string, query Query) ([]Match, error) {
	if !client.config.Enabled {
		return nil, ErrDisabled
	}
	if len(query.Name) > 1024 || strings.TrimSpace(query.Name) == "" || query.Year < 0 || query.Year > 9999 {
		return nil, ErrInvalidInput
	}
	switch provider {
	case "tmdb":
		return client.searchTMDB(ctx, query)
	case "musicbrainz":
		return client.searchMusicBrainz(ctx, query)
	default:
		return nil, ErrInvalidInput
	}
}

func (client *Client) Lookup(ctx context.Context, selection Selection) (Metadata, error) {
	if !client.config.Enabled {
		return Metadata{}, ErrDisabled
	}
	switch selection.Provider {
	case "tmdb":
		return client.lookupTMDB(ctx, selection)
	case "musicbrainz":
		return client.lookupMusicBrainz(ctx, selection)
	default:
		return Metadata{}, ErrInvalidInput
	}
}

func (client *Client) Images(ctx context.Context, selection Selection) ([]RemoteImage, error) {
	if !client.config.Enabled {
		return nil, ErrDisabled
	}
	if selection.Provider != "tmdb" {
		return nil, ErrInvalidInput
	}
	return client.imagesTMDB(ctx, selection)
}

func field(value any) json.RawMessage { encoded, _ := json.Marshal(value); return encoded }
