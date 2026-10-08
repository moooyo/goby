package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestRequestQueryProjectionReachesItemAndSearchQueries(t *testing.T) {
	for _, raw := range []string{"", "true", "false", "0", "1"} {
		t.Run(raw, func(t *testing.T) {
			values := url.Values{"SearchTerm": {"query"}}
			if raw != "" {
				values.Set("EnableImages", raw)
			}
			request := httptest.NewRequest(http.MethodGet, "/emby/Items?"+values.Encode(), nil)
			query, ok := readItemQuery(httptest.NewRecorder(), request, "viewer")
			hints, err := parseSearchHintsQuery(values)
			wantDisabled := raw == "false" || raw == "0"
			if !ok || err != nil || !query.Projection.Browse || !hints.Projection.Browse ||
				query.Projection.ImagesDisabled != wantDisabled || hints.Projection.ImagesDisabled != wantDisabled {
				t.Fatalf("request projections were lost: item=%+v hint=%+v err=%v", query.Projection, hints.Projection, err)
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, "/emby/Search/Hints?SearchTerm=query&enableimages=false", nil)
	if !normalizeItemProjectionQuery(httptest.NewRecorder(), request) || !requestQueryProjection(request).ImagesDisabled {
		t.Fatal("normalized image-control aliases lost their domain projection")
	}
}
