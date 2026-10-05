package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestVideoNavigationQueryValidatesPublicAndGobySelectors(t *testing.T) {
	for _, raw := range []string{"Is4K=true", "is4k=false", "ExtendedVideoTypes=Hdr10,Hdr10Plus,HyperLogGamma,DolbyVision", "ExtendedVideoTypes=None", "Is4K=true&GobyAggregateVideoFilters=true"} {
		query := library.Query{}
		if !readNavigationFilters(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/emby/Items?"+raw, nil), &query) || !hasNavigationFilters(query) {
			t.Fatalf("valid video selector was ignored: %q", raw)
		}
	}
	for _, raw := range []string{"Is4K=", "Is4K=yes", "Is4K=true&is4k=false", "ExtendedVideoTypes=", "ExtendedVideoTypes=HDR", "ExtendedVideoTypes=Hdr10,", "ExtendedVideoTypes=Hdr10,,None", "ExtendedVideoTypes=None&extendedvideotypes=Hdr10", "ExtendedVideoTypes=None,None,None,None,None,None", "GobyAggregateVideoFilters=", "GobyAggregateVideoFilters=perhaps", "GobyAggregateVideoFilters=true&gobyaggregatevideofilters=false"} {
		query := library.Query{}
		recorder := httptest.NewRecorder()
		if readNavigationFilters(recorder, httptest.NewRequest(http.MethodGet, "/emby/Items?"+raw, nil), &query) || recorder.Code != http.StatusBadRequest {
			t.Fatalf("malformed video selector was accepted: %q", raw)
		}
	}
}
