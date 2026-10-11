package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestItemPresentationSwitchDefaultsAndResponseLifetime(t *testing.T) {
	for _, test := range []struct {
		name, query      string
		images, userData bool
	}{
		{name: "absent", images: true, userData: true},
		{name: "enabled", query: "EnableImages=true&EnableUserData=1", images: true, userData: true},
		{name: "images disabled", query: "EnableImages=0", userData: true},
		{name: "user data disabled", query: "EnableUserData=false", images: true},
		{name: "both disabled", query: "EnableImages=false&EnableUserData=0"},
		{name: "unnormalized empty", query: "EnableImages=&EnableUserData=", images: true, userData: true},
		{name: "unnormalized invalid", query: "EnableImages=invalid&EnableUserData=invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/admin/v1/items?ExcludeFields=Path,Id,Name,Type,IsFolder,ServerId,MediaType,FutureField&"+test.query, nil)
			presentation := readItemPresentation(r)
			r.URL.RawQuery = "ExcludeFields=UserData&EnableImages=true&EnableUserData=false"
			for range 2 {
				source := map[string]any{"Path": "/media/movie.mkv", "MediaStreams": []map[string]any{{"Type": "Video"}}}
				item := map[string]any{
					"Id": "item", "Name": "Movie", "Type": "Movie", "IsFolder": false, "ServerId": "server", "MediaType": "Video",
					"Path": "/media/movie.mkv", "MediaSources": []map[string]any{source},
					"ImageTags": map[string]string{"Primary": "tag"}, "BackdropImageTags": []string{"backdrop"},
					"UserData": map[string]any{"Played": true},
				}
				presentation.applySwitches(item)
				for _, field := range []string{"ImageTags", "BackdropImageTags", "UserData"} {
					_, present := item[field]
					want := test.images
					if field == "UserData" {
						want = test.userData
					}
					if present != want {
						t.Fatalf("%s presence = %v, want %v", field, present, want)
					}
				}
				if item["Path"] != nil || source["Path"] != nil || source["MediaStreams"] == nil {
					t.Fatal("response-local exclusions lost nested or unrelated source semantics")
				}
				for _, field := range []string{"Id", "Name", "Type", "IsFolder", "ServerId", "MediaType"} {
					if _, present := item[field]; !present {
						t.Fatalf("protected identity field %s was removed", field)
					}
				}
			}
		})
	}
}

func TestItemPresentationPreservesUnnormalizedFieldLists(t *testing.T) {
	values := url.Values{
		"Fields": {"MediaStreams", "MediaSources,Future.Field"}, "fields": {"CanDownload"},
		"ExcludeFields": {"Media\u017ftream\u017f", "Path"}, "EnableImages": {"false"}, "ImageTypeLimit": {"invalid"},
	}
	r := httptest.NewRequest(http.MethodGet, "/admin/v1/items?"+values.Encode(), nil)
	originalURL, originalQuery := r.URL, r.URL.RawQuery
	presentation := readItemPresentation(r)
	if r.URL != originalURL || r.URL.RawQuery != originalQuery || !reflect.DeepEqual(presentation.fields, []string{"MediaStreams", "MediaSources", "Future.Field"}) {
		t.Fatal("presentation parsing changed native query-list or spelling semantics")
	}
	s := &Server{}
	item := library.Item{ID: "item", Type: "Movie", Path: "/media/movie.mkv", Media: &media.Info{}}
	dto := s.itemDTOWithToken(item, presentation.fields, false, presentation.deliveryToken)
	presentation.applySwitches(dto)
	if dto["MediaStreams"] != nil || dto["MediaSources"] == nil {
		t.Fatal("native Unicode field matching or supplied projection changed")
	}
	w := httptest.NewRecorder()
	if !s.applyIndexedImages(w, r, "", []map[string]any{dto}, false, presentation) || w.Code != http.StatusOK {
		t.Fatal("native parsing introduced normalization or a new image error before the disabled-images branch")
	}
}

func TestItemPresentationKeepsCallerFieldsAndDetailSeparate(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/emby/Items?Fields=MediaStreams,MediaSources,Path", nil)
	presentation := readItemPresentation(r)
	s := &Server{}
	item := library.Item{ID: "item", Type: "Movie", Path: "/media/movie.mkv", Media: &media.Info{}}
	plain := s.itemDTOWithToken(item, nil, false, presentation.deliveryToken)
	for _, field := range []string{"MediaStreams", "MediaSources", "Path"} {
		if _, present := plain[field]; present {
			t.Fatalf("common query fields replaced the caller's nil selection: %s", field)
		}
	}
	selected := s.itemDTOWithToken(item, []string{"MediaStreams"}, false, presentation.deliveryToken)
	if selected["MediaStreams"] == nil || selected["MediaSources"] != nil || selected["Path"] != nil {
		t.Fatal("caller-supplied fields were replaced by the common query selection")
	}
	detail := s.itemDTOWithToken(item, nil, true, presentation.deliveryToken)
	for _, field := range []string{"MediaStreams", "MediaSources", "Path"} {
		if _, present := detail[field]; !present {
			t.Fatalf("detail omitted %s without an explicit field request", field)
		}
	}
}

func TestItemPresentationRetainsCredentialCarrierAndFormatterSemantics(t *testing.T) {
	token := "case-sensitive /+?&value"
	for _, test := range []struct {
		name, query, wantToken string
		headers                http.Header
	}{
		{name: "absent"},
		{name: "query api key", query: "API_KEY=" + url.QueryEscape(token), wantToken: token},
		{name: "query token", query: "X-Emby-Token=" + url.QueryEscape(token), wantToken: token},
		{name: "token header", headers: http.Header{"X-Emby-Token": {token}}, wantToken: token},
		{name: "legacy token header", headers: http.Header{"X-Mediabrowser-Token": {token}}, wantToken: token},
		{name: "authorization", headers: http.Header{"Authorization": {"MediaBrowser Token=\"" + token + "\""}}, wantToken: token},
		{name: "matching carriers", query: "api_key=" + url.QueryEscape(token), headers: http.Header{"X-Emby-Token": {token}}, wantToken: token},
		{name: "conflicting carriers", query: "api_key=other", headers: http.Header{"X-Emby-Token": {token}}},
		{name: "unsupported authorization", headers: http.Header{"Authorization": {"Bearer ignored-by-formatter"}}},
		{name: "invalid query", query: "unrelated=%zz", headers: http.Header{"X-Emby-Token": {token}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Items", nil)
			r.URL.RawQuery = test.query
			if test.headers != nil {
				r.Header = test.headers.Clone()
			}
			presentation := readItemPresentation(r)
			r.Header.Set("X-Emby-Token", "changed-after-capture")
			r.URL.RawQuery = "api_key=changed-after-capture"
			s := &Server{}
			for _, id := range []string{"first-item", "second item/?"} {
				item := library.Item{ID: id, Type: "Movie", Path: "/media/movie.mkv",
					Media:     &media.Info{Streams: []media.Stream{{Index: 2, CodecType: "subtitle", Codec: "subrip"}}},
					Subtitles: []library.Subtitle{{Index: 7, Codec: "vtt", Owned: true, Tag: "owned-tag"}}}
				dto := s.itemDTOWithToken(item, []string{"MediaStreams", "MediaSources"}, false, presentation.deliveryToken)
				source := dto["MediaSources"].([]map[string]any)[0]
				for _, projected := range []map[string]any{dto, source} {
					for _, stream := range projected["MediaStreams"].([]map[string]any) {
						delivery, err := url.Parse(stream["DeliveryUrl"].(string))
						if err != nil || delivery.Query().Get("api_key") != test.wantToken || !strings.HasPrefix(delivery.EscapedPath(), "/Videos/"+url.PathEscape(id)+"/") {
							t.Fatalf("delivery URL lost carrier selection, item identity, or escaping: %#v", stream)
						}
						if stream["Index"] == 7 && delivery.Query().Get("GobySubtitleTag") != "owned-tag" {
							t.Fatal("credential reuse removed the owned subtitle tag")
						}
					}
				}
			}
		})
	}
}

func TestItemPresentationImageOptionsAndFinalExclusions(t *testing.T) {
	images := []library.Image{
		{ImageType: "Primary", Width: 320, Height: 180, Tag: "primary"},
		{ImageType: "Primary", Width: 100, Height: 100, Tag: "duplicate-primary"},
		{ImageType: "Thumb", Tag: "thumb"},
		{ImageType: "Backdrop", Tag: "first-backdrop"}, {ImageType: "Backdrop", Tag: "second-backdrop"},
	}
	for _, test := range []struct {
		name, query   string
		wantTags      map[string]string
		wantBackdrops []string
		wantAspect    bool
	}{
		{name: "defaults", wantTags: map[string]string{"Primary": "primary", "Thumb": "thumb"}, wantBackdrops: []string{"first-backdrop", "second-backdrop"}, wantAspect: true},
		{name: "zero limit", query: "ImageTypeLimit=0", wantTags: map[string]string{}, wantBackdrops: []string{}},
		{name: "selected types", query: "ImageTypeLimit=1&EnableImageTypes=primary&EnableImageTypes=BACKDROP", wantTags: map[string]string{"Primary": "primary"}, wantBackdrops: []string{"first-backdrop"}, wantAspect: true},
		{name: "empty types", query: "EnableImageTypes=", wantTags: map[string]string{"Primary": "primary", "Thumb": "thumb"}, wantBackdrops: []string{"first-backdrop", "second-backdrop"}, wantAspect: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Items?Fields=PrimaryImageAspectRatio&"+test.query, nil)
			presentation := readItemPresentation(r)
			if !presentation.enableImages {
				t.Fatal("an image limit or type selection disabled image processing")
			}
			item := map[string]any{"Id": "item"}
			presentation.applyIndexedImageTags(item, images, false)
			_, aspect := item["PrimaryImageAspectRatio"]
			if !reflect.DeepEqual(item["ImageTags"], test.wantTags) || !reflect.DeepEqual(item["BackdropImageTags"], test.wantBackdrops) || aspect != test.wantAspect {
				t.Fatalf("image projection changed: %#v", item)
			}
			if aspect && item["PrimaryImageAspectRatio"] != float64(320)/180 {
				t.Fatal("duplicate Primary images changed the first image's aspect ratio")
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "/emby/Items?Fields=PrimaryImageAspectRatio&ExcludeFields=ImageTags,BackdropImageTags,PrimaryImageAspectRatio,CanDownload", nil)
	presentation := readItemPresentation(r)
	item := map[string]any{"Id": "item", "CanDownload": true}
	presentation.applyIndexedImageTags(item, images, true)
	if !reflect.DeepEqual(item, map[string]any{"Id": "item"}) {
		t.Fatalf("image enrichment restored a field excluded after capabilities: %#v", item)
	}
}

func TestItemPresentationPreservesImageErrorOrder(t *testing.T) {
	for _, test := range []struct {
		name, query string
		status      int
	}{
		{name: "capability before invalid image limit", query: "Fields=CanDownload&ImageTypeLimit=invalid", status: http.StatusForbidden},
		{name: "omitted capability reaches image error", query: "Fields=CanDownload&ExcludeFields=CanDownload&ImageTypeLimit=invalid", status: http.StatusBadRequest},
		{name: "capability before disabled images", query: "Fields=CanDownload&EnableImages=false&ImageTypeLimit=invalid", status: http.StatusForbidden},
		{name: "disabled images skip later limit error", query: "Fields=CanDownload&ExcludeFields=CanDownload&EnableImages=false&ImageTypeLimit=invalid", status: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Sessions?"+test.query, nil)
			w := httptest.NewRecorder()
			s := &Server{}
			ok := s.applyIndexedImages(w, r, "", []map[string]any{{"Id": "item"}}, false, readItemPresentation(r))
			if w.Code != test.status || ok != (test.status == http.StatusOK) {
				t.Fatalf("image error order changed: status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}
