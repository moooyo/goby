package providers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"reflect"
	"testing"
)

func TestTMDBMovieSearchAndLookupMapLocalizedMetadata(t *testing.T) {
	client := New(Config{Enabled: true, TMDBToken: "fixture-token", Language: "zh-CN", Country: "us"})
	calls := 0
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		query := request.URL.Query()
		if request.URL.Host != "api.themoviedb.org" || request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer fixture-token" || query.Get("language") != "zh-CN" {
			t.Errorf("unexpected TMDB authentication or locale: %s", request.URL)
		}
		switch request.URL.Path {
		case "/3/search/movie":
			if query.Get("query") != "A Film" || query.Get("year") != "2001" || query.Get("region") != "US" || query.Get("include_adult") != "false" || query.Get("page") != "1" {
				t.Errorf("movie search lost its selection constraints: %s", request.URL)
			}
			return metadataContractResponse(`{"results":[{"id":42,"title":"Localized film","original_title":"A Film","release_date":"2001-02-03","overview":"Search summary","popularity":12.5}]}`), nil
		case "/3/movie/42":
			if query.Get("append_to_response") != "external_ids,credits,release_dates" {
				t.Errorf("movie lookup omitted required detail sources: %s", request.URL)
			}
			return metadataContractResponse(`{
				"id":42,"title":"Localized film","original_title":"A Film","overview":"Full summary","release_date":"2001-02-03","imdb_id":"tt0000042","vote_average":7.5,
				"genres":[{"name":"Drama"},{"name":"Drama"}],"production_companies":[{"name":"Studio"}],
				"credits":{"cast":[{"name":"Lead actor","character":"Hero","order":0}],"crew":[{"name":"Director","job":"Director"}]},
				"release_dates":{"results":[{"iso_3166_1":"GB","release_dates":[{"certification":"15","type":3}]},{"iso_3166_1":"US","release_dates":[{"certification":"R","type":4},{"certification":"PG-13","type":3}]}]}
			}`), nil
		default:
			t.Errorf("unexpected TMDB route %s", request.URL)
			return nil, ErrUnavailable
		}
	})}
	matches, err := client.Search(context.Background(), "tmdb", Query{Type: "Movie", Name: "A Film", Year: 2001})
	if err != nil || len(matches) != 1 {
		t.Fatalf("movie search failed: %#v, %v", matches, err)
	}
	if matches[0].ID != "42" || matches[0].Language != "zh-CN" || matches[0].OriginalTitle != "A Film" || matches[0].Year != 2001 || matches[0].Score != 12.5 {
		t.Fatalf("movie match lost identity or useful preview facts: %#v", matches[0])
	}
	result, err := client.Lookup(context.Background(), matches[0].Selection)
	if err != nil || calls != 2 {
		t.Fatalf("movie lookup failed: calls=%d error=%v", calls, err)
	}
	assertMetadataContractField(t, result, "Name", "Localized film")
	assertMetadataContractField(t, result, "SortName", "Localized film")
	assertMetadataContractField(t, result, "OriginalTitle", "A Film")
	assertMetadataContractField(t, result, "Overview", "Full summary")
	assertMetadataContractField(t, result, "ProductionYear", 2001)
	assertMetadataContractField(t, result, "PremiereDate", "2001-02-03T00:00:00Z")
	assertMetadataContractField(t, result, "CommunityRating", 7.5)
	assertMetadataContractField(t, result, "OfficialRating", "PG-13")
	assertMetadataContractField(t, result, "ProviderIds", map[string]string{"Tmdb": "42", "Imdb": "tt0000042"})
	assertMetadataContractField(t, result, "Genres", []string{"Drama"})
	assertMetadataContractField(t, result, "Studios", []string{"Studio"})
	assertMetadataContractField(t, result, "People", []map[string]any{
		{"Name": "Lead actor", "Role": "Hero", "Type": "Actor", "SortOrder": 0},
		{"Name": "Director", "Role": "Director", "Type": "Director"},
	})
	if result.SourceURL != "https://www.themoviedb.org/movie/42" {
		t.Fatalf("incorrect movie provenance: %s", result.SourceURL)
	}
}

func TestTMDBEpisodeSearchAndLookupRetainSeriesAndEpisodeIdentity(t *testing.T) {
	client := New(Config{Enabled: true, TMDBToken: "fixture-token", Language: "en-US"})
	calls := 0
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		calls++
		query := request.URL.Query()
		switch request.URL.Path {
		case "/3/search/tv":
			if query.Get("query") != "A Series" || query.Has("first_air_date_year") || query.Has("year") {
				t.Errorf("episode year incorrectly filtered the series premiere: %s", request.URL)
			}
			return metadataContractResponse(`{"results":[{"id":50,"name":"A Series","original_name":"Original series","first_air_date":"1999-01-02"}]}`), nil
		case "/3/tv/50/season/2/episode/3":
			if query.Get("append_to_response") != "external_ids,credits" {
				t.Errorf("unexpected episode detail appendices: %s", request.URL)
			}
			return metadataContractResponse(`{
				"id":503,"name":"Episode title","season_number":2,"episode_number":3,"air_date":"2024-05-06","overview":"Episode summary",
				"external_ids":{"imdb_id":"tt0000503","tvdb_id":12345},
				"guest_stars":[{"name":"Guest","character":"Visitor","order":1}],"crew":[{"name":"Writer","job":"Teleplay"}]
			}`), nil
		default:
			t.Errorf("unexpected episode route %s", request.URL)
			return nil, ErrUnavailable
		}
	})}
	matches, err := client.Search(context.Background(), "tmdb", Query{Type: "Episode", Name: "A Series", Year: 2024, Season: 2, Episode: 3})
	if err != nil || len(matches) != 1 || matches[0].ID != "50:2:3" {
		t.Fatalf("episode match lost its parent identity: %#v, %v", matches, err)
	}
	result, err := client.Lookup(context.Background(), matches[0].Selection)
	if err != nil || calls != 2 {
		t.Fatalf("episode lookup failed: calls=%d error=%v", calls, err)
	}
	assertMetadataContractField(t, result, "Name", "Episode title")
	assertMetadataContractField(t, result, "ProductionYear", 2024)
	assertMetadataContractField(t, result, "IndexNumber", 3)
	assertMetadataContractField(t, result, "ParentIndexNumber", 2)
	assertMetadataContractField(t, result, "ProviderIds", map[string]string{"Tmdb": "50:2:3", "Imdb": "tt0000503", "Tvdb": "12345"})
	assertMetadataContractField(t, result, "People", []map[string]any{
		{"Name": "Guest", "Role": "Visitor", "Type": "GuestStar", "SortOrder": 1},
		{"Name": "Writer", "Role": "Teleplay", "Type": "Writer"},
	})
	if result.SourceURL != "https://www.themoviedb.org/tv/50/season/2/episode/3" {
		t.Fatalf("incorrect episode provenance: %s", result.SourceURL)
	}
}

func TestTMDBSeriesLookupSelectsCountryContentRating(t *testing.T) {
	client := New(Config{Enabled: true, TMDBToken: "fixture-token", Country: "US"})
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/3/tv/50" || request.URL.Query().Get("append_to_response") != "external_ids,credits,content_ratings" {
			t.Errorf("series lookup did not request content ratings: %s", request.URL)
		}
		return metadataContractResponse(`{"id":50,"name":"Series title","original_name":"Original series","first_air_date":"1999-01-02","content_ratings":{"results":[{"iso_3166_1":"GB","rating":"12"},{"iso_3166_1":"US","rating":"TV-14"}]}}`), nil
	})}
	result, err := client.Lookup(context.Background(), Selection{Provider: "tmdb", Type: "Series", ID: "50"})
	if err != nil {
		t.Fatal(err)
	}
	assertMetadataContractField(t, result, "Name", "Series title")
	assertMetadataContractField(t, result, "OriginalTitle", "Original series")
	assertMetadataContractField(t, result, "ProductionYear", 1999)
	assertMetadataContractField(t, result, "OfficialRating", "TV-14")
}

func TestTMDBImagesRevalidateSelectionAndUseCredentialFreeOriginalCDN(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	imageBytes := encoded.Bytes()
	client := New(Config{Enabled: true, TMDBToken: "fixture-token"})
	var paths []string
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		paths = append(paths, request.URL.Host+request.URL.Path)
		switch request.URL.Host {
		case "api.themoviedb.org":
			if request.URL.Path != "/3/tv/50/season/2/episode/3/images" || request.URL.Query().Get("include_image_language") != "zh,null" || request.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Errorf("image lookup lost episode, language or credentials: %s", request.URL)
			}
			return metadataContractResponse(`{"stills":[{"file_path":"/StillA.png","width":640,"height":360},{"file_path":"/StillA.png","width":640,"height":360},{"file_path":"/../outside.jpg","width":640,"height":360},{"file_path":"/Empty.jpg","width":0,"height":360}]}`), nil
		case "image.tmdb.org":
			if request.URL.Path != "/t/p/original/StillA.png" || request.Header.Get("Authorization") != "" {
				t.Errorf("CDN fetch used caller input or API credentials: %s", request.URL)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(imageBytes))}, nil
		default:
			t.Errorf("unexpected image host %s", request.URL.Host)
			return nil, ErrUnavailable
		}
	})}
	selection := Selection{Provider: "tmdb", Type: "Episode", ID: "50:2:3", Language: "zh-TW"}
	images, err := client.Images(context.Background(), selection)
	if err != nil || len(images) != 1 || images[0].ImageType != "Thumb" || images[0].Width != 640 || images[0].Height != 360 || images[0].PreviewURL != "https://image.tmdb.org/t/p/w300/StillA.png" {
		t.Fatalf("image listing did not preserve the valid still: %#v, %v", images, err)
	}
	selected := images[0]
	selected.PreviewURL, selected.Width, selected.Height = "https://ignored.example/other.png", 1, 1
	download, err := client.DownloadImage(context.Background(), selected)
	if err != nil || !bytes.Equal(download, imageBytes) {
		t.Fatalf("freshly resolved original was not returned: %v", err)
	}
	selected.ImageID = "/Missing.png"
	if _, err := client.DownloadImage(context.Background(), selected); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unlisted image selection was accepted: %v", err)
	}
	wantPaths := []string{
		"api.themoviedb.org/3/tv/50/season/2/episode/3/images",
		"api.themoviedb.org/3/tv/50/season/2/episode/3/images",
		"image.tmdb.org/t/p/original/StillA.png",
		"api.themoviedb.org/3/tv/50/season/2/episode/3/images",
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("image revalidation or fetch sequence changed: %#v", paths)
	}
}
