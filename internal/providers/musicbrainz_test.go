package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const musicBrainzContractID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
const musicBrainzContractAgent = "Goby/contract-test (https://example.test/contact)"

func metadataContractResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func assertMetadataContractField(t *testing.T, result Metadata, key string, want any) {
	t.Helper()
	raw, ok := result.Fields[key]
	if !ok {
		t.Fatalf("metadata omitted %s", key)
	}
	var actual, expected any
	if err := json.Unmarshal(raw, &actual); err != nil {
		t.Fatalf("decode %s: %v", key, err)
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s = %#v, want %#v", key, actual, expected)
	}
}

func TestMusicBrainzSearchUsesEntityQueriesAndDecodesScoreForms(t *testing.T) {
	for _, test := range []struct{ itemType, entity, field, collection, nameField string }{
		{"MusicAlbum", "release-group", "releasegroup", "release-groups", "title"},
		{"MusicArtist", "artist", "artist", "artists", "name"},
		{"Audio", "recording", "recording", "recordings", "title"},
	} {
		t.Run(test.itemType, func(t *testing.T) {
			client := New(Config{Enabled: true, MusicBrainzUserAgent: musicBrainzContractAgent})
			calls := 0
			client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
				calls++
				wantQuery := test.field + `:"Blue\: Sky"`
				if test.itemType != "MusicArtist" {
					wantQuery += " AND firstreleasedate:1985"
				}
				query := request.URL.Query()
				if request.Method != http.MethodGet || request.URL.Host != "musicbrainz.org" || request.URL.Path != "/ws/2/"+test.entity+"/" || query.Get("query") != wantQuery || query.Get("fmt") != "json" || query.Get("limit") != "25" {
					t.Errorf("unexpected MusicBrainz search request: %s", request.URL)
				}
				if request.Header.Get("User-Agent") != musicBrainzContractAgent || request.Header.Get("Authorization") != "" {
					t.Error("credential-free search lost application identification")
				}
				return metadataContractResponse(fmt.Sprintf(`{"%s":[
					{"id":%q,%q:"Blue: Sky","first-release-date":"1985","score":97,"disambiguation":" Studio version "},
					{"id":"bbbbbbbb-cccc-4ddd-8eee-ffffffffffff",%q:"Blue: Sky Live","first-release-date":"1985-02","score":"83"},
					{"id":%q,%q:"Duplicate","score":50},
					{"id":"invalid",%q:"Invalid identifier","score":100}
				]}`, test.collection, strings.ToUpper(musicBrainzContractID), test.nameField, test.nameField, musicBrainzContractID, test.nameField, test.nameField)), nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			matches, err := client.Search(ctx, "musicbrainz", Query{Type: test.itemType, Name: "Blue: Sky", Year: 1985, Language: "en"})
			if err != nil || calls != 1 || len(matches) != 2 {
				t.Fatalf("search result: matches=%#v calls=%d error=%v", matches, calls, err)
			}
			if matches[0].ID != musicBrainzContractID || matches[0].Type != test.itemType || matches[0].Provider != "musicbrainz" || matches[0].Name != "Blue: Sky" || matches[0].Year != 1985 || matches[0].Score != 97 || matches[0].Overview != "Studio version" || matches[1].Score != 83 || matches[1].Year != 1985 {
				t.Fatalf("search did not retain canonical identities, dates or scores: %#v", matches)
			}
		})
	}
}

func TestMusicBrainzAlbumLookupPreservesCreditsAndPartialDates(t *testing.T) {
	for _, date := range []string{"2001", "2001-02", "2001-02-03"} {
		t.Run(date, func(t *testing.T) {
			client := New(Config{Enabled: true, MusicBrainzUserAgent: musicBrainzContractAgent})
			client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != "/ws/2/release-group/"+musicBrainzContractID || request.URL.Query().Get("inc") != "tags+genres+annotation+artist-rels+artist-credits" || request.URL.Query().Get("fmt") != "json" {
					t.Errorf("album lookup used the wrong entity or includes: %s", request.URL)
				}
				return metadataContractResponse(fmt.Sprintf(`{
					"id":%q,"title":"Album title","first-release-date":%q,
					"annotation":" Album notes ","disambiguation":"Fallback summary",
					"artist-credit":[{"name":"Credited artist","artist":{"name":"Canonical artist"}},{"artist":{"name":"Second artist"}}],
					"genres":[{"name":"Rock","count":2},{"name":"Rock","count":1},{"name":"Rejected","count":-1}],
					"tags":[{"name":"Live","count":1}]
				}`, musicBrainzContractID, date)), nil
			})}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			result, err := client.Lookup(ctx, Selection{Provider: "musicbrainz", Type: "MusicAlbum", ID: strings.ToUpper(musicBrainzContractID)})
			if err != nil {
				t.Fatal(err)
			}
			assertMetadataContractField(t, result, "Name", "Album title")
			assertMetadataContractField(t, result, "Overview", "Album notes")
			assertMetadataContractField(t, result, "ProductionYear", 2001)
			assertMetadataContractField(t, result, "ProviderIds", map[string]string{"MusicBrainz": musicBrainzContractID, "MusicBrainzReleaseGroup": musicBrainzContractID})
			assertMetadataContractField(t, result, "Artists", []string{"Credited artist", "Second artist"})
			assertMetadataContractField(t, result, "AlbumArtists", []string{"Credited artist", "Second artist"})
			assertMetadataContractField(t, result, "Genres", []string{"Rock"})
			assertMetadataContractField(t, result, "Tags", []string{"Live"})
			if len(date) == 10 {
				assertMetadataContractField(t, result, "PremiereDate", "2001-02-03T00:00:00Z")
			} else if _, exists := result.Fields["PremiereDate"]; exists {
				t.Fatal("partial release date invented a month or day")
			}
			if result.ID != musicBrainzContractID || result.SourceURL != "https://musicbrainz.org/release-group/"+musicBrainzContractID {
				t.Fatalf("lookup provenance did not identify the release group: %#v", result.Selection)
			}
		})
	}
}

func TestMusicBrainzRecordingLookupMapsWorkRelations(t *testing.T) {
	client := New(Config{Enabled: true, MusicBrainzUserAgent: musicBrainzContractAgent})
	client.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/ws/2/recording/"+musicBrainzContractID || request.URL.Query().Get("inc") != "tags+genres+annotation+artist-rels+artist-credits+work-rels+work-level-rels" {
			t.Errorf("recording lookup omitted work-level relations: %s", request.URL)
		}
		return metadataContractResponse(fmt.Sprintf(`{
			"id":%q,"title":"A recording","disambiguation":"Original take",
			"artist-credit":[{"name":"Performer","artist":{"name":"Performer"}}],
			"relations":[
				{"type":"producer","artist":{"name":"Producer"}},
				{"type":"performance","work":{"relations":[
					{"type":"composer","artist":{"name":"Composer"}},
					{"type":"lyricist","attributes":["additional"],"artist":{"name":"Lyricist"}}
				]}}
			]
		}`, musicBrainzContractID)), nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := client.Lookup(ctx, Selection{Provider: "musicbrainz", Type: "Audio", ID: musicBrainzContractID})
	if err != nil {
		t.Fatal(err)
	}
	assertMetadataContractField(t, result, "Overview", "Original take")
	assertMetadataContractField(t, result, "ProviderIds", map[string]string{"MusicBrainz": musicBrainzContractID, "MusicBrainzRecording": musicBrainzContractID})
	assertMetadataContractField(t, result, "People", []map[string]any{
		{"Name": "Producer", "Type": "Producer", "Role": "", "SortOrder": 0},
		{"Name": "Composer", "Type": "Composer", "Role": "", "SortOrder": 1},
		{"Name": "Lyricist", "Type": "Lyricist", "Role": "additional", "SortOrder": 2},
	})
	if _, exists := result.Fields["AlbumArtists"]; exists {
		t.Fatal("recording credits were relabeled as album artists")
	}
}

func TestMusicBrainzRateGateSerializesSeparateClientsAfterCompletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstCompleted := make(chan time.Time, 1)
	secondEntered := make(chan time.Time, 1)
	results := make(chan error, 2)
	response := fmt.Sprintf(`{"id":%q,"title":"Rate fixture"}`, musicBrainzContractID)
	first := New(Config{Enabled: true, MusicBrainzUserAgent: musicBrainzContractAgent})
	first.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		close(firstEntered)
		select {
		case <-releaseFirst:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		firstCompleted <- time.Now()
		return metadataContractResponse(response), nil
	})}
	second := New(Config{Enabled: true, MusicBrainzUserAgent: musicBrainzContractAgent})
	second.http = &http.Client{Transport: providerRoundTrip(func(request *http.Request) (*http.Response, error) {
		secondEntered <- time.Now()
		return metadataContractResponse(response), nil
	})}
	lookup := func(client *Client) {
		_, err := client.Lookup(ctx, Selection{Provider: "musicbrainz", Type: "Audio", ID: musicBrainzContractID})
		results <- err
	}
	go lookup(first)
	select {
	case <-firstEntered:
	case <-ctx.Done():
		t.Fatal("first client never reached the transport")
	}
	go lookup(second)
	select {
	case <-secondEntered:
		t.Fatal("separate clients sent overlapping MusicBrainz requests")
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(releaseFirst)
	var next time.Time
	select {
	case next = <-secondEntered:
	case <-ctx.Done():
		t.Fatal("second client did not resume after the shared gate")
	}
	if elapsed := next.Sub(<-firstCompleted); elapsed < time.Second {
		t.Fatalf("requests reached the transport only %s apart after completion", elapsed)
	}
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal("rate-gated lookup did not return")
		}
	}
}

func TestMusicBrainzCancellationDoesNotSendOrRetainGate(t *testing.T) {
	var calls atomic.Int32
	client := New(Config{Enabled: true, MusicBrainzUserAgent: musicBrainzContractAgent})
	client.http = &http.Client{Transport: providerRoundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return metadataContractResponse(`{}`), nil
	})}
	lookup := func(t *testing.T) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := client.Lookup(ctx, Selection{Provider: "musicbrainz", Type: "Audio", ID: musicBrainzContractID})
		if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 0 {
			t.Fatalf("cancelled request reached the API: calls=%d error=%v", calls.Load(), err)
		}
	}
	t.Run("waiting_for_another_request", func(t *testing.T) {
		musicBrainzRequests.gate <- struct{}{}
		defer func() { <-musicBrainzRequests.gate }()
		lookup(t)
	})
	t.Run("waiting_for_cooldown", func(t *testing.T) {
		musicBrainzRequests.gate <- struct{}{}
		previous := musicBrainzRequests.next
		musicBrainzRequests.next = time.Now().Add(time.Minute)
		<-musicBrainzRequests.gate
		defer func() { musicBrainzRequests.next = previous }()
		lookup(t)
		select {
		case musicBrainzRequests.gate <- struct{}{}:
			<-musicBrainzRequests.gate
		default:
			t.Fatal("cancelled cooldown retained the shared request gate")
		}
	})
}
