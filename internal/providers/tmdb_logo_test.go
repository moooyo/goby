package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTMDBLogoSelectionAndDownload(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	var imageRequests int
	client := New(Config{Enabled: true, TMDBToken: "fixture-credential", Language: "en"})
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "api.themoviedb.org":
			if request.URL.Path != "/3/movie/123/images" || request.URL.Query().Get("include_image_language") != "en,null" {
				t.Fatalf("unexpected image selection request: %s", request.URL.Redacted())
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
				`{"logos":[{"file_path":"/Logo.png","width":1200,"height":300},{"file_path":"/Logo.png","width":1200,"height":300},{"file_path":"/../../private.png","width":1,"height":1},{"file_path":"/Oversized.png","width":32769,"height":20}]}`))}, nil
		case "image.tmdb.org":
			if request.URL.Path != "/t/p/original/Logo.png" {
				t.Fatalf("unexpected image download: %s", request.URL.Redacted())
			}
			imageRequests++
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(encoded.Bytes()))}, nil
		default:
			t.Fatalf("untrusted image host: %s", request.URL.Host)
			return nil, ErrInvalidInput
		}
	})}
	selection := Selection{Provider: "tmdb", ID: "123", Type: "Movie", Language: "en"}
	candidates, err := client.Images(context.Background(), selection)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("expected one bounded, deduplicated logo: %#v, %v", candidates, err)
	}
	candidate := candidates[0]
	if candidate.ImageType != "Logo" || candidate.Width != 1200 || candidate.Height != 300 {
		t.Fatalf("incorrect logo candidate: %#v", candidate)
	}
	candidate.PreviewURL = "https://untrusted.example/private"
	data, err := client.DownloadImage(context.Background(), candidate)
	if err != nil || !bytes.Equal(data, encoded.Bytes()) || imageRequests != 1 {
		t.Fatalf("logo download failed: %v, requests=%d", err, imageRequests)
	}
	candidate.ImageID = "/Unlisted.png"
	if _, err := client.DownloadImage(context.Background(), candidate); !errors.Is(err, ErrNotFound) || imageRequests != 1 {
		t.Fatalf("unlisted logo was not rejected: %v, requests=%d", err, imageRequests)
	}
}

func TestTMDBImageBoundDoesNotStarveLogoCandidates(t *testing.T) {
	type imageFixture struct {
		Path   string `json:"file_path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	payload := struct {
		Posters   []imageFixture `json:"posters"`
		Backdrops []imageFixture `json:"backdrops"`
		Logos     []imageFixture `json:"logos"`
	}{Logos: []imageFixture{{"/FirstLogo.png", 1000, 200}, {"/SecondLogo.png", 1200, 240}}}
	for index := 0; index < tmdbMaximumImages*2; index++ {
		payload.Posters = append(payload.Posters, imageFixture{fmt.Sprintf("/Poster%d.png", index), 1000, 1500})
		payload.Backdrops = append(payload.Backdrops, imageFixture{fmt.Sprintf("/Backdrop%d.png", index), 1920, 1080})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	client := New(Config{Enabled: true, TMDBToken: "fixture-credential", Language: "en"})
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	candidates, err := client.Images(context.Background(), Selection{Provider: "tmdb", ID: "123", Type: "Movie", Language: "en"})
	if err != nil || len(candidates) != tmdbMaximumImages {
		t.Fatalf("image bound was not preserved: count=%d, error=%v", len(candidates), err)
	}
	counts := make(map[string]int)
	for _, candidate := range candidates {
		counts[candidate.ImageType]++
	}
	if counts["Logo"] != 2 || counts["Primary"] == 0 || counts["Backdrop"] == 0 {
		t.Fatalf("one image type was hidden by another type's volume: %#v", counts)
	}
}
