package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestDiscoveryQueryParsesLikesAndFolderFilterConjunctions(t *testing.T) {
	for _, test := range []struct {
		raw    string
		likes  *bool
		folder *bool
	}{
		{"Filters=Likes,IsNotFolder", discoveryTestBool(true), discoveryTestBool(false)},
		{"Filters=Dislikes,IsFolder", discoveryTestBool(false), discoveryTestBool(true)},
		{"Filters=IsFavorite,Likes&IsFavoriteOrLikes=true", discoveryTestBool(true), nil},
		{"Filters=Likes&Filters=IsUnplayed", discoveryTestBool(true), nil},
	} {
		request := httptest.NewRequest(http.MethodGet, "/emby/Items?"+test.raw, nil)
		query, ok := readItemQuery(httptest.NewRecorder(), request, "viewer")
		if !ok || !reflect.DeepEqual(query.Likes, test.likes) || !reflect.DeepEqual(query.IsFolder, test.folder) {
			t.Fatalf("supported filter forms were not preserved: %q, %+v", test.raw, query)
		}
	}
	for _, raw := range []string{"Filters=Likes,Dislikes", "Filters=IsFolder,IsNotFolder", "Filters=IsFolder&IsFolder=false",
		"Filters=IsPlayed&IsPlayed=false", "Filters=Unknown", "Filters=", "Filters=Likes,,IsFavorite", "IsFolder=", "IsFolder=true&IsFolder=true"} {
		recorder := httptest.NewRecorder()
		if _, ok := readItemQuery(recorder, httptest.NewRequest(http.MethodGet, "/emby/Items?"+raw, nil), "viewer"); ok || recorder.Code != http.StatusBadRequest {
			t.Fatalf("conflicting or malformed filter accepted: %q", raw)
		}
	}
}

func discoveryTestBool(value bool) *bool { return &value }

func TestDiscoveryMissingSelectorsPreserveAbsenceAndExplicitFalse(t *testing.T) {
	query, ok := readItemQuery(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/emby/Items", nil), "viewer")
	if !ok || query.IsMissing != nil || query.IsPlaceHolder != nil || query.IsVirtualUnaired != nil || query.IsUnaired != nil {
		t.Fatal("absent virtual selectors acquired a false value")
	}
	for _, raw := range []string{"IsMissing=false&IsVirtualUnaired=true&IsPlaceHolder=false&IsUnaired=true",
		"ismissing=false&ISVIRTUALUNAIRED=true&isplaceholder=false&isunaired=true"} {
		query, ok = readItemQuery(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/emby/Items?"+raw, nil), "viewer")
		if !ok || query.IsMissing == nil || *query.IsMissing || query.IsVirtualUnaired == nil || !*query.IsVirtualUnaired ||
			query.IsPlaceHolder == nil || *query.IsPlaceHolder || query.IsUnaired == nil || !*query.IsUnaired {
			t.Fatal("virtual selector presence, case alias, or explicit value was lost")
		}
	}
	for _, raw := range []string{"IsMissing=", "IsMissing=false&ismissing=false", "IsUnaired=yes", "IsPlaceHolder=null", "IsVirtualUnaired=true&IsVirtualUnaired=false"} {
		if _, ok := readItemQuery(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/emby/Items?"+raw, nil), "viewer"); ok {
			t.Fatalf("malformed virtual selector accepted: %s", raw)
		}
	}
}

func TestSuggestionPlayedDefaultsPreserveExplicitQueryPrecedence(t *testing.T) {
	for _, test := range []struct {
		raw  string
		hide bool
		want *bool
	}{
		{"", false, nil}, {"", true, discoveryTestBool(false)},
		{"IsPlayed=true", true, discoveryTestBool(true)},
		{"IsPlayed=false", true, discoveryTestBool(false)},
		{"Filters=IsPlayed", true, discoveryTestBool(true)},
		{"Filters=IsUnplayed", false, discoveryTestBool(false)},
		{"Filters=Likes,IsFavorite", true, discoveryTestBool(false)},
	} {
		query, ok := readItemQuery(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/emby/Items?"+test.raw, nil), "viewer")
		if !ok {
			t.Fatal("test query was rejected")
		}
		applySuggestionPlayedDefault(&query, test.hide)
		if !reflect.DeepEqual(query.IsPlayed, test.want) {
			t.Fatalf("played default overwrote explicit state for %q", test.raw)
		}
	}
}

func TestDiscoveryDTOUsesActualProbeFactsAndNeverAttachedImageDimensions(t *testing.T) {
	item := library.Item{ID: "track", Path: "/media/video.mkv", Media: &media.Info{Size: 123456, Bitrate: 765432,
		Streams: []media.Stream{
			{CodecType: "video", Codec: "png", Width: 600, Height: 600, IsAttachedPicture: true},
			{CodecType: "video", Codec: "h264", Width: 1920, Height: 1080},
			{CodecType: "audio", Codec: "aac"},
		}}}
	dto := map[string]any{}
	addDiscoveryItemFields(dto, item, []string{"LocationType", "Size", "Bitrate", "Width", "Height", "VideoCodec", "AudioCodec"}, false)
	want := map[string]any{"LocationType": "FileSystem", "Size": int64(123456), "Bitrate": int64(765432), "Width": 1920, "Height": 1080, "VideoCodec": "h264", "AudioCodec": "aac"}
	if !reflect.DeepEqual(dto, want) {
		t.Fatalf("source-backed discovery fields = %#v; want %#v", dto, want)
	}
	omitted := map[string]any{}
	addDiscoveryItemFields(omitted, item, []string{"UnsupportedProviderField"}, false)
	if len(omitted) != 0 {
		t.Fatal("unrequested probe details were projected")
	}
	unknown := map[string]any{}
	addDiscoveryItemFields(unknown, library.Item{Media: &media.Info{}}, nil, true)
	if len(unknown) != 0 {
		t.Fatal("absent facts were replaced with invented zero metadata")
	}
}

func TestDiscoveryDTOMarksExpectedEpisodeWithoutPlaybackOrPrivateSourceData(t *testing.T) {
	server := &Server{serverID: "fixture"}
	item := library.Item{ID: "missing-test", Type: "Episode", Name: "Expected", ExpectedEpisode: &library.ExpectedEpisodeInfo{IsUnaired: true, PremiereDateKnown: true}}
	dto := server.itemDTO(item, []string{"Path", "MediaSources", "MediaStreams", "UserData"}, true)
	for _, field := range []string{"Path", "MediaSources", "MediaStreams", "UserData", "SourceKey", "EntryKey", "RunTimeTicks"} {
		if _, found := dto[field]; found {
			t.Fatalf("expected episode exposed %s", field)
		}
	}
	if dto["LocationType"] != "Virtual" || dto["IsMissing"] != true || dto["IsPlaceHolder"] != true || dto["IsVirtualUnaired"] != true || dto["SupportsResume"] != false {
		t.Fatal("expected episode lacks its explicit unplayable classification")
	}
}
