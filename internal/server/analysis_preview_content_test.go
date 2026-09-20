package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
)

func TestAnalysisPreviewByteRangesAndConditionalPrecedence(t *testing.T) {
	for _, test := range []struct {
		name, method, rangeValue, ifRange, ifNone, ifMatch string
		status                                             int
		body, contentRange, length                         string
	}{
		{name: "full", method: "GET", status: 200, body: "0123456789", length: "10"},
		{name: "head", method: "HEAD", status: 200, length: "10"},
		{name: "closed", method: "GET", rangeValue: "bytes=2-5", status: 206, body: "2345", contentRange: "bytes 2-5/10", length: "4"},
		{name: "open", method: "GET", rangeValue: "bytes=8-", status: 206, body: "89", contentRange: "bytes 8-9/10", length: "2"},
		{name: "suffix", method: "GET", rangeValue: "bytes=-3", status: 206, body: "789", contentRange: "bytes 7-9/10", length: "3"},
		{name: "large-suffix", method: "GET", rangeValue: "bytes=-20", status: 206, body: "0123456789", contentRange: "bytes 0-9/10", length: "10"},
		{name: "head-range", method: "HEAD", rangeValue: "bytes=0-1", status: 206, contentRange: "bytes 0-1/10", length: "2"},
		{name: "unsatisfiable", method: "GET", rangeValue: "bytes=10-", status: 416, contentRange: "bytes */10", length: "0"},
		{name: "multi-range", method: "GET", rangeValue: "bytes=0-1,3-4", status: 416, contentRange: "bytes */10", length: "0"},
		{name: "wrong-unit", method: "GET", rangeValue: "frames=0-1", status: 416, contentRange: "bytes */10", length: "0"},
		{name: "zero-suffix", method: "GET", rangeValue: "bytes=-0", status: 416, contentRange: "bytes */10", length: "0"},
		{name: "weak-if-range", method: "GET", rangeValue: "bytes=0-1", ifRange: `W/"fixture"`, status: 200, body: "0123456789", length: "10"},
		{name: "matching-if-range", method: "GET", rangeValue: "bytes=0-1", ifRange: `"fixture"`, status: 206, body: "01", contentRange: "bytes 0-1/10", length: "2"},
		{name: "etag-before-range", method: "GET", rangeValue: "bytes=999-", ifNone: `W/"fixture"`, status: 304},
		{name: "failed-match", method: "GET", ifMatch: `W/"fixture"`, ifNone: `"fixture"`, status: 412, length: "0"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(test.method, "/preview", nil)
			for key, value := range map[string]string{"Range": test.rangeValue, "If-Range": test.ifRange, "If-None-Match": test.ifNone, "If-Match": test.ifMatch} {
				if value != "" {
					r.Header.Set(key, value)
				}
			}
			w := httptest.NewRecorder()
			(&Server{}).serveAnalysisPreviewContent(w, r, bytes.NewReader([]byte("0123456789")), 10, "application/octet-stream", `"fixture"`, true)
			if w.Code != test.status || w.Body.String() != test.body || w.Header().Get("Content-Range") != test.contentRange || w.Header().Get("Content-Length") != test.length {
				t.Fatalf("response = %d %q range=%q length=%q", w.Code, w.Body.String(), w.Header().Get("Content-Range"), w.Header().Get("Content-Length"))
			}
			if w.Header().Get("Cache-Control") != "private, no-cache, no-transform" || w.Header().Get("ETag") != `"fixture"` || w.Header().Get("Accept-Ranges") != "bytes" {
				t.Fatal("BIF delivery lost private revalidation or its strong validator")
			}
		})
	}
}

func TestAnalysisPreviewRangeParserRejectsOverflowAndAmbiguity(t *testing.T) {
	for _, value := range []string{"bytes=", "bytes=-", "bytes=1--2", "bytes=2-1", "bytes=+1-2", "bytes=1-2 ", "bytes=9223372036854775808-", "bytes=1-9223372036854775808"} {
		if _, _, ok := analysisPreviewSingleRange(value, 10); ok {
			t.Errorf("invalid range accepted: %s", value)
		}
	}
}

type analysisPreviewCountReader struct{ reads int }

func (reader *analysisPreviewCountReader) Read([]byte) (int, error) { reader.reads++; return 0, io.EOF }

func TestAnalysisPreviewCancelledReadDoesNotTouchContent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	content := &analysisPreviewCountReader{}
	if _, err := (analysisPreviewContextReader{context: ctx, reader: content}).Read(make([]byte, 1)); !errors.Is(err, context.Canceled) || content.reads != 0 {
		t.Fatal("a cancelled preview read touched content")
	}
}
