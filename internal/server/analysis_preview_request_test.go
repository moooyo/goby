package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnalysisPreviewRequestsSeparateStoredWidthFromDisplayWidth(t *testing.T) {
	tag := "goby-preview-240-" + strings.Repeat("a", 64)
	for _, test := range []struct {
		query string
		image bool
		want  analysisPreviewRequest
	}{
		{"?Width=400&MediaSourceId=media-source", false, analysisPreviewRequest{width: 400, sourceID: "media-source"}},
		{"?width=320&Width=320&api_key=credential", false, analysisPreviewRequest{width: 320}},
		{"?Width=240", false, analysisPreviewRequest{width: 240}},
		{"?PositionTicks=0&maxWidth=800&quality=90", true, analysisPreviewRequest{maxWidth: 800, quality: 90}},
		{"?PositionTicks=100000000&maxWidth=320&tag=" + tag, true, analysisPreviewRequest{width: 240, positionTicks: 100000000, maxWidth: 320, tag: tag}},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := httptest.NewRequest(method, "/preview"+test.query, nil)
			got, err := parseAnalysisPreviewRequest(r, test.image)
			if err != nil || got != test.want {
				t.Fatalf("preview request = %+v, error %v; want %+v", got, err, test.want)
			}
		}
	}
}

func TestAnalysisPreviewRequestsRejectUnknownConflictingAndInvalidCarriers(t *testing.T) {
	for _, test := range []struct {
		query string
		image bool
	}{
		{"", false}, {"?Width=0", false}, {"?Width=800", false}, {"?Width=0400", false}, {"?Width=400&width=320", false},
		{"?Width=400&Path=/private", false}, {"?Width=400&UserId=other", false}, {"?Width=400&MediaSourceId=../other", false},
		{"?Width=400&MediaSourceId=one&mediasourceid=two", false}, {"?Width=400&api_key=one&API_KEY=two", false},
		{"?Width=400&bad=%zz", false}, {"?Width=400&PositionTicks=0", false}, {"?Width=400&quality=90", false},
		{"?maxWidth=320", true}, {"?PositionTicks=-1", true}, {"?PositionTicks=00", true},
		{"?PositionTicks=9223372036854775808", true}, {"?PositionTicks=0&Width=400", true},
		{"?PositionTicks=0&ImageIndex=0", true}, {"?PositionTicks=0&maxWidth=4097", true},
		{"?PositionTicks=0&quality=101", true}, {"?PositionTicks=0&tag=untrusted", true},
		{"?PositionTicks=0&tag=goby-preview-800-" + strings.Repeat("a", 64), true},
		{"?PositionTicks=0&tag=goby-preview-400-" + strings.Repeat("A", 64), true},
	} {
		r := httptest.NewRequest(http.MethodGet, "/preview"+test.query, nil)
		if _, err := parseAnalysisPreviewRequest(r, test.image); err == nil {
			t.Errorf("invalid preview request accepted: %s", test.query)
		}
	}
	if _, err := parseAnalysisPreviewRequest(httptest.NewRequest(http.MethodPost, "/preview?Width=400", nil), false); err == nil {
		t.Fatal("preview delivery admitted a mutating method")
	}
}
