package server

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func TestExtraDTOObservedDefaultsAndFieldsPreserveResourceIdentity(t *testing.T) {
	server := &Server{serverID: "server"}
	for _, test := range []struct{ kind, name, itemType, extra string }{
		{library.ExtraKindClip, "Alpha Bonus", "Video", "Clip"},
		{library.ExtraKindDeletedScene, "Middle Deleted Scene", "Video", "DeletedScene"},
		{library.ExtraKindTrailer, "Feature Movie - Trailer", "Trailer", "Trailer"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			item := library.Item{ID: "resource", ParentID: "movie", Name: "Alpha Bonus", SortName: "Alpha Bonus", Type: "Video",
				Path: "/fixture/Film/featurettes/Alpha Bonus.mp4", ExtraKind: test.kind, ExtraOwnerName: "Feature Movie", CreatedAt: time.Now(),
				Media:    &media.Info{Container: "mp4", DurationTicks: 120000000, Bitrate: 651000, Size: 976750},
				UserData: &library.UserData{ItemID: "resource"}}
			if test.kind == library.ExtraKindDeletedScene {
				item.Name, item.SortName = test.name, test.name
			}
			defaults := server.itemDTO(item, nil, false)
			var actualKeys []string
			for key := range defaults {
				actualKeys = append(actualKeys, key)
			}
			sort.Strings(actualKeys)
			expectedKeys := []string{"BackdropImageTags", "ExtraType", "Id", "ImageTags", "IsFolder", "MediaType", "Name", "RunTimeTicks", "ServerId", "Type", "UserData"}
			if !reflect.DeepEqual(actualKeys, expectedKeys) || defaults["Name"] != test.name || defaults["Type"] != test.itemType ||
				defaults["ExtraType"] != test.extra || defaults["Id"] != "resource" {
				t.Fatalf("recorded extra default identity/shape differs: keys=%v", actualKeys)
			}
			fields := []string{"Path", "ParentId", "SortName", "MediaSources", "MediaStreams", "Overview", "Genres", "Tags", "People", "Studios", "ProviderIds", "DateCreated", "ProductionYear"}
			detailed := server.itemDTO(item, fields, false)
			if detailed["ParentId"] != "movie" || detailed["Path"] != item.Path || detailed["SortName"] != test.name ||
				detailed["Name"] != test.name || detailed["Type"] != test.itemType || detailed["ExtraType"] != test.extra ||
				detailed["Bitrate"] != item.Media.Bitrate || detailed["Size"] != item.Media.Size || detailed["Container"] != "mp4" {
				t.Fatal("extra Fields lost the actual parent, source, or attachment identity")
			}
			if _, ok := detailed["Overview"]; ok {
				t.Fatal("untagged extra fabricated an overview")
			}
			if _, ok := detailed["People"]; ok == (test.kind == library.ExtraKindTrailer) {
				t.Fatal("empty People projection differs from the recorded extra kind")
			}
			sources, ok := detailed["MediaSources"].([]map[string]any)
			if !ok || len(sources) != 1 || sources[0]["Id"] != media.SourceID(item.ID) || sources[0]["Name"] != "Alpha Bonus" {
				t.Fatal("extra item name replaced the source filename or identity")
			}
			direct := server.itemDTO(item, nil, true)
			if direct["Name"] != detailed["Name"] || direct["Type"] != detailed["Type"] || direct["ExtraType"] != detailed["ExtraType"] || direct["ParentId"] != "movie" {
				t.Fatal("direct attachment identity differs from its list projection")
			}
			if _, exists := direct["VideoType"]; exists {
				t.Fatal("direct extra projection added an unobserved VideoType")
			}
			if part, exists := direct["PartCount"]; test.kind == library.ExtraKindTrailer && exists ||
				test.kind != library.ExtraKindTrailer && part != 1 {
				t.Fatal("direct extra PartCount differs from the recorded kind")
			}
			sourceOnly := server.itemDTO(item, []string{"MediaSources"}, false)
			if sourceOnly["MediaSources"] == nil || sourceOnly["Container"] != "mp4" ||
				sourceOnly["Bitrate"] != item.Media.Bitrate || sourceOnly["Size"] != item.Media.Size {
				t.Fatal("MediaSources-only extra fields omitted observed source facts")
			}
			for _, absent := range []string{"MediaStreams", "ParentId", "SortName", "DateCreated"} {
				if _, exists := sourceOnly[absent]; exists {
					t.Fatal("MediaSources-only fields broadened the item projection")
				}
			}
		})
	}
}

func TestExtraParentCountsArePositiveDetailFieldsOnly(t *testing.T) {
	server := &Server{}
	positive, zero := 1, 0
	for _, test := range []struct {
		count   *int
		detail  bool
		present bool
	}{
		{&positive, true, true}, {&positive, false, false}, {&zero, true, false}, {nil, true, false},
	} {
		dto := server.itemDTO(library.Item{ID: "movie", Type: "Movie", LocalTrailerCount: test.count}, []string{"ItemCounts"}, test.detail)
		if _, present := dto["LocalTrailerCount"]; present != test.present {
			t.Fatal("LocalTrailerCount violated its observed positive detail boundary")
		}
		if _, present := dto["SpecialFeatureCount"]; present {
			t.Fatal("Movie details fabricated an unobserved SpecialFeatureCount")
		}
	}
}

func TestExtraNamespaceCanonicalizationPreservesOpaqueIdentifiers(t *testing.T) {
	for _, endpoint := range []struct{ raw, canonical string }{{"sPeCiAlFeAtUrEs", "SpecialFeatures"}, {"lOcAlTrAiLeRs", "LocalTrailers"}} {
		request := httptest.NewRequest(http.MethodGet, "/uSeRs/Viewer-ID/iTeMs/Movie-ID/"+endpoint.raw+"?Fields=Path", nil)
		canonical := compatibilityNamespace(request)
		if canonical.URL.Path != "/emby/Users/Viewer-ID/Items/Movie-ID/"+endpoint.canonical || canonical.URL.RawQuery != request.URL.RawQuery ||
			request.URL.Path != "/uSeRs/Viewer-ID/iTeMs/Movie-ID/"+endpoint.raw {
			t.Fatal("extra route canonicalization changed an opaque identity or input request")
		}
	}
}
