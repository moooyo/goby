package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/subtitle"
)

const openSubtitlesContractSRT = "1\n00:00:01,000 --> 00:00:02,000\nOffline contract caption\n"

func openSubtitlesContractResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func openSubtitlesContractClient() *Client {
	return New(Config{Enabled: true, OpenSubtitlesAPIKey: "offline-api-key",
		OpenSubtitlesUserAgent: "Goby offline contract", OpenSubtitlesUsername: "offline-user",
		OpenSubtitlesPassword: "offline-password", Language: "en"})
}

func TestOpenSubtitlesSearchPreservesSelectionWithoutDownloadQuota(t *testing.T) {
	client := openSubtitlesContractClient()
	client.config.OpenSubtitlesUsername, client.config.OpenSubtitlesPassword = "", ""
	calls := 0
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.Host != "api.opensubtitles.com" ||
			request.URL.Path != "/api/v1/subtitles" || request.Header.Get("Api-Key") != "offline-api-key" ||
			request.Header.Get("User-Agent") != "Goby offline contract" || request.Header.Get("Authorization") != "" {
			t.Fatal("search changed its bounded API request or used an account credential")
		}
		want := map[string][]string{"type": {"movie"}, "query": {"Fixture film"}, "year": {"2020"},
			"imdb_id": {"1234567"}, "languages": {"en,zh-cn"}, "hearing_impaired": {"only"}, "page": {"1"}}
		if !reflect.DeepEqual(map[string][]string(request.URL.Query()), want) {
			t.Fatalf("unexpected subtitle search parameters: %v", request.URL.Query())
		}
		return openSubtitlesContractResponse(http.StatusOK, `{"data":[{"id":"12","attributes":{"language":"eng","release":"Fixture release","hearing_impaired":true,"foreign_parts_only":true,"download_count":9,"moviehash_match":true,"files":[{"file_id":34,"file_name":"Fixture.srt"},{"file_id":34,"file_name":"Duplicate.srt"}]}},{"id":"not-an-id","attributes":{"language":"en","files":[{"file_id":99,"file_name":"Rejected.srt"}]}}]}`), nil
	})}
	items, err := client.SearchSubtitles(context.Background(), SubtitleQuery{Query: Query{Type: "Movie", Name: "Fixture film", Year: 2020,
		ProviderIDs: map[string]string{"Imdb": "tt1234567"}}, Languages: []string{"eng", "en", "zh-CN"}, HearingImpaired: true})
	if err != nil || calls != 1 || len(items) != 1 {
		t.Fatalf("one search did not produce one unique valid selection: calls=%d items=%+v error=%v", calls, items, err)
	}
	want := RemoteSubtitle{Provider: "opensubtitles", ID: "12", FileID: 34, Language: "en", Name: "Fixture.srt",
		HearingImpaired: true, IsForced: true, DownloadCount: 9, MovieHashMatch: true}
	if items[0] != want {
		t.Fatalf("subtitle search lost its download identity or flags: %+v", items[0])
	}
}

func TestOpenSubtitlesDownloadKeepsCredentialsOffFileHost(t *testing.T) {
	client := openSubtitlesContractClient()
	var stages []string
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		stages = append(stages, request.Method+" "+request.URL.Host+request.URL.Path)
		switch request.URL.Host + request.URL.Path {
		case "api.opensubtitles.com/api/v1/login":
			if request.Method != http.MethodPost || request.Header.Get("Api-Key") != "offline-api-key" || request.Header.Get("Authorization") != "" {
				t.Fatal("login omitted its API key or reused a bearer token")
			}
			var body map[string]string
			if json.NewDecoder(request.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]string{"username": "offline-user", "password": "offline-password"}) {
				t.Fatal("login changed the explicit account credential body")
			}
			return openSubtitlesContractResponse(http.StatusOK, `{"token":"offline-bearer","base_url":"vip-api.opensubtitles.com"}`), nil
		case "vip-api.opensubtitles.com/api/v1/download":
			if request.Method != http.MethodPost || request.Header.Get("Api-Key") != "offline-api-key" ||
				request.Header.Get("Authorization") != "Bearer offline-bearer" || request.Header.Get("User-Agent") != "Goby offline contract" {
				t.Fatal("download authorization did not follow the admitted login API host")
			}
			var body struct {
				FileID int64  `json:"file_id"`
				Format string `json:"sub_format"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.FileID != 34 || body.Format != "srt" {
				t.Fatal("download did not request the selected file as SRT")
			}
			return openSubtitlesContractResponse(http.StatusOK, `{"link":"https://dl.opensubtitles.com/download/fixture.srt?ticket=offline"}`), nil
		case "dl.opensubtitles.com/download/fixture.srt":
			if request.Method != http.MethodGet || request.Header.Get("Api-Key") != "" || request.Header.Get("Authorization") != "" ||
				request.Header.Get("Cookie") != "" || request.Header.Get("User-Agent") != "Goby offline contract" {
				t.Fatal("file retrieval received API or account credentials")
			}
			return openSubtitlesContractResponse(http.StatusOK, openSubtitlesContractSRT), nil
		default:
			t.Fatal("download issued an unplanned request")
			return nil, errors.New("unplanned offline request")
		}
	})}
	download, err := client.DownloadSubtitle(context.Background(), RemoteSubtitle{Provider: "opensubtitles", ID: "12", FileID: 34,
		Language: "eng", HearingImpaired: true, IsForced: true})
	if err != nil || download.Provider != "opensubtitles" || download.RemoteID != "12:34" || download.Format != "srt" ||
		download.Language != "en" || !download.HearingImpaired || !download.IsForced || string(download.Data) != openSubtitlesContractSRT {
		t.Fatalf("downloaded SRT or its provenance changed: %+v error=%v", download, err)
	}
	if _, err := subtitle.Parse(download.Data, subtitle.Format(download.Format)); err != nil {
		t.Fatalf("returned bytes are not ready for SRT publication: %v", err)
	}
	want := []string{"POST api.opensubtitles.com/api/v1/login", "POST vip-api.opensubtitles.com/api/v1/download", "GET dl.opensubtitles.com/download/fixture.srt"}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("download repeated or skipped a provider stage: %v", stages)
	}
}

func TestOpenSubtitlesQuotaResponseDoesNotRetryDownload(t *testing.T) {
	for _, status := range []int{http.StatusPaymentRequired, http.StatusNotAcceptable, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client := openSubtitlesContractClient()
			loginCalls, downloadCalls := 0, 0
			client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/api/v1/login":
					loginCalls++
					return openSubtitlesContractResponse(http.StatusOK, `{"token":"offline-bearer"}`), nil
				case "/api/v1/download":
					downloadCalls++
					response := openSubtitlesContractResponse(status, `{"message":"private upstream quota detail"}`)
					response.Header.Set("Retry-After", "1")
					return response, nil
				default:
					t.Fatal("quota rejection was followed by a file request")
					return nil, errors.New("unplanned offline request")
				}
			})}
			download, err := client.DownloadSubtitle(context.Background(), RemoteSubtitle{Provider: "opensubtitles", ID: "12", FileID: 34, Language: "en"})
			if !errors.Is(err, ErrQuota) || len(download.Data) != 0 || loginCalls != 1 || downloadCalls != 1 ||
				strings.Contains(err.Error(), "private upstream") {
				t.Fatalf("quota rejection retried, disclosed upstream text, or returned media: login=%d download=%d error=%v", loginCalls, downloadCalls, err)
			}
		})
	}
}
