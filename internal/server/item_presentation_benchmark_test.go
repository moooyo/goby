package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

var benchmarkItemPresentationItems []map[string]any

// Both modes render the same DTOs without database or filesystem work.
// PerItemParsing is a conservative baseline for repeated request parsing;
// it does not reproduce every redundant parse in the previous implementation.
func BenchmarkItemPresentation(b *testing.B) {
	fields := []string{"MediaStreams", "Path", "PrimaryImageAspectRatio"}
	for index := range 64 {
		fields = append(fields, fmt.Sprintf("FutureField%02d", index))
	}
	large := url.Values{
		"Fields": {strings.Join(fields, ",")}, "ExcludeFields": {"Path"},
		"EnableImages": {"true"}, "EnableUserData": {"true"}, "EnableImageTypes": {"Primary,Backdrop"}, "ImageTypeLimit": {"8"},
		"SearchTerm": {strings.Repeat("example ", 128)}, "SortBy": {"SortName,DateCreated"}, "SortOrder": {"Ascending"},
		"X-Emby-Client": {"Benchmark client"}, "X-Emby-Device-Id": {"benchmark-device"}, "api_key": {"benchmark-token"},
	}
	for _, query := range []struct{ name, value string }{
		{"SmallQuery", "Fields=MediaStreams,PrimaryImageAspectRatio&ExcludeFields=Path&api_key=benchmark-token"},
		{"LargeQuery", large.Encode()},
	} {
		b.Run(query.name, func(b *testing.B) {
			for _, count := range []int{1, 100, 1000} {
				b.Run(fmt.Sprintf("Items%d", count), func(b *testing.B) {
					r := httptest.NewRequest(http.MethodGet, "/emby/Items?"+query.value, nil)
					if !normalizeItemProjectionQuery(httptest.NewRecorder(), r) {
						b.Fatal("invalid benchmark projection")
					}
					entries := make([]library.Item, count)
					for index := range entries {
						entries[index] = library.Item{ID: fmt.Sprintf("item-%d", index), Name: "Movie", SortName: "Movie",
							Type: "Movie", Path: "/media/movie.mkv", UserData: &library.UserData{Played: true},
							Media: &media.Info{Streams: []media.Stream{{Index: 2, CodecType: "subtitle", Codec: "subrip"}}}}
					}
					images := []library.Image{{ImageType: "Primary", Width: 320, Height: 180, Tag: "primary"}, {ImageType: "Backdrop", Tag: "backdrop"}}
					for _, mode := range []struct {
						name          string
						responseLocal bool
					}{{"PerItemParsing", false}, {"PerResponseParsing", true}} {
						b.Run(mode.name, func(b *testing.B) {
							s := &Server{}
							b.ReportAllocs()
							b.ResetTimer()
							for range b.N {
								var presentation itemPresentation
								if mode.responseLocal {
									presentation = readItemPresentation(r)
								}
								items := make([]map[string]any, 0, len(entries))
								for _, entry := range entries {
									if !mode.responseLocal {
										presentation = readItemPresentation(r)
									}
									dto := s.itemDTOWithToken(entry, presentation.fields, false, presentation.deliveryToken)
									presentation.applySwitches(dto)
									presentation.applyIndexedImageTags(dto, images, false)
									items = append(items, dto)
								}
								benchmarkItemPresentationItems = items
							}
							b.ReportMetric(float64(count), "items/op")
						})
					}
				})
			}
		})
	}
}
