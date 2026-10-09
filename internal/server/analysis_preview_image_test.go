package server

import (
	"bytes"
	"context"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/library"
)

func analysisPreviewFixtureFrame(t *testing.T, archive []byte, number int) (bif.Entry, []byte) {
	t.Helper()
	index, err := bif.Open(bytes.NewReader(archive), int64(len(archive)), bif.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := index.Entry(number)
	if err != nil {
		t.Fatal(err)
	}
	return entry, archive[entry.Offset : entry.Offset+entry.Size]
}

func TestAnalysisPreviewUntransformedImagePreservesBytesAndHeaders(t *testing.T) {
	lease, archive := analysisPreviewFixture(t)
	_, frame := analysisPreviewFixtureFrame(t, archive, 1)
	initial := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(initial, analysisPreviewTestRequest("GET", "?PositionTicks=150000000"), &analysisPreviewTestProvider{lease: lease}, "image")
	etag := initial.Header().Get("ETag")
	if initial.Code != http.StatusOK || !bytes.Equal(initial.Body.Bytes(), frame) || len(etag) != 66 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Fatal("untransformed image did not preserve the selected JPEG and its strong validator")
	}
	if lease.checks != 1 || lease.closeCount.Load() != 1 {
		t.Fatal("untransformed image bypassed final revalidation or retained its lease")
	}
	for _, test := range []struct {
		name, method, query, header, value string
		status                             int
		body                               bool
		length                             string
	}{
		{name: "explicit-zero-options", method: "GET", query: "?PositionTicks=150000000&maxWidth=0&quality=0", status: http.StatusOK, body: true, length: strconv.Itoa(len(frame))},
		{name: "selected-position-head", method: "HEAD", query: "?PositionTicks=100000000", status: http.StatusOK, length: strconv.Itoa(len(frame))},
		{name: "conditional", method: "GET", query: "?PositionTicks=150000000", header: "If-None-Match", value: etag, status: http.StatusNotModified},
		{name: "failed-precondition", method: "GET", query: "?PositionTicks=150000000", header: "If-Match", value: `"another-image"`, status: http.StatusPreconditionFailed, length: "0"},
		{name: "image-ignores-range", method: "GET", query: "?PositionTicks=150000000", header: "Range", value: "bytes=0-7", status: http.StatusOK, body: true, length: strconv.Itoa(len(frame))},
	} {
		t.Run(test.name, func(t *testing.T) {
			fresh, _ := analysisPreviewFixture(t)
			r := analysisPreviewTestRequest(test.method, test.query)
			if test.header != "" {
				r.Header.Set(test.header, test.value)
			}
			w := httptest.NewRecorder()
			(&Server{}).serveAnalysisPreview(w, r, &analysisPreviewTestProvider{lease: fresh}, "image")
			if w.Code != test.status || w.Header().Get("Content-Length") != test.length || w.Header().Get("ETag") != etag {
				t.Fatalf("image response = %d, length %q, ETag %q", w.Code, w.Header().Get("Content-Length"), w.Header().Get("ETag"))
			}
			if test.body && !bytes.Equal(w.Body.Bytes(), frame) || !test.body && w.Body.Len() != 0 {
				t.Fatal("image response changed the selected JPEG bytes or sent an unexpected body")
			}
			for name, want := range map[string]string{
				"Content-Type":                  "image/jpeg",
				"X-Content-Type-Options":        "nosniff",
				"Cache-Control":                 "private, no-cache, no-transform",
				"Access-Control-Expose-Headers": "Accept-Ranges, Content-Length, Content-Range, ETag",
				"Accept-Ranges":                 "",
				"Content-Range":                 "",
			} {
				if got := w.Header().Get(name); got != want {
					t.Errorf("%s = %q; want %q", name, got, want)
				}
			}
			if fresh.checks != 1 || fresh.closeCount.Load() != 1 {
				t.Fatal("image response skipped final revalidation or did not close its lease once")
			}
		})
	}
}

func TestAnalysisPreviewUntransformedImageValidatesCurrentFrameBeforeConditionals(t *testing.T) {
	for _, damage := range []string{"invalid-scan", "missing-eoi", "truncated-reader"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, conditional := range []string{"", "If-None-Match", "If-Match"} {
				t.Run(damage+"/"+method+"/"+conditional, func(t *testing.T) {
					lease, archive := analysisPreviewFixture(t)
					_, frame := analysisPreviewFixtureFrame(t, archive, 1)
					switch damage {
					case "invalid-scan":
						scan := bytes.Index(frame, []byte{0xff, 0xda})
						if scan < 0 || scan+6 >= len(frame) {
							t.Fatal("JPEG fixture has no scan header")
						}
						frame[scan+6] = 0xff
						if _, err := jpeg.DecodeConfig(bytes.NewReader(frame)); err != nil {
							t.Fatal("corrupt scan fixture must retain a valid JPEG configuration")
						}
					case "missing-eoi":
						frame[len(frame)-1] = 0
					case "truncated-reader":
						archive = archive[:len(archive)-1]
					}
					lease.reader = bytes.NewReader(archive)
					r := analysisPreviewTestRequest(method, "?PositionTicks=150000000")
					if conditional != "" {
						r.Header.Set(conditional, "*")
					}
					w := httptest.NewRecorder()
					(&Server{}).serveAnalysisPreview(w, r, &analysisPreviewTestProvider{lease: lease}, "image")
					if w.Code != http.StatusServiceUnavailable || w.Header().Get("ETag") != "" || w.Header().Get("Content-Type") == "image/jpeg" || w.Header().Get("Cache-Control") != "private, no-store" {
						t.Fatal("a corrupt current JPEG reached a successful, conditional or image response")
					}
					if lease.checks != 0 || lease.closeCount.Load() != 1 {
						t.Fatal("invalid frame advanced past validation or leaked its lease")
					}
				})
			}
		}
	}
}

func TestAnalysisPreviewImageTransformsSingleRequestedOption(t *testing.T) {
	for _, test := range []struct {
		name, options string
		width, height int
	}{
		{name: "resize-only", options: "&maxWidth=200", width: 200, height: 112},
		{name: "quality-only", options: "&quality=15", width: 400, height: 224},
	} {
		t.Run(test.name, func(t *testing.T) {
			lease, archive := analysisPreviewFixture(t)
			_, original := analysisPreviewFixtureFrame(t, archive, 0)
			w := httptest.NewRecorder()
			(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?PositionTicks=0"+test.options), &analysisPreviewTestProvider{lease: lease}, "image")
			decoded, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
			if w.Code != http.StatusOK || err != nil {
				t.Fatalf("transformed image = %d, decode error %v", w.Code, err)
			}
			if decoded.Bounds().Dx() != test.width || decoded.Bounds().Dy() != test.height || bytes.Equal(w.Body.Bytes(), original) {
				t.Fatal("a requested image transform returned the original JPEG or wrong dimensions")
			}
			if w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) || lease.checks != 1 || lease.closeCount.Load() != 1 {
				t.Fatal("transformed image lost its delivery headers, final revalidation or lease cleanup")
			}
		})
	}
}

func TestAnalysisPreviewUntransformedImageRevalidatesBeforeConditionals(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			lease, _ := analysisPreviewFixture(t)
			lease.checkError = library.ErrForbidden
			r := analysisPreviewTestRequest(method, "?PositionTicks=0")
			r.Header.Set("If-None-Match", "*")
			w := httptest.NewRecorder()
			(&Server{}).serveAnalysisPreview(w, r, &analysisPreviewTestProvider{lease: lease}, "image")
			if w.Code != http.StatusForbidden || w.Header().Get("ETag") != "" || lease.checks != 1 || lease.closeCount.Load() != 1 {
				t.Fatal("a conditional image response bypassed final authority or lease cleanup")
			}
		})
	}
}

type analysisPreviewCancelFrameReader struct {
	*bytes.Reader
	frameOffset int64
	cancel      context.CancelFunc
}

func (reader analysisPreviewCancelFrameReader) ReadAt(buffer []byte, offset int64) (int, error) {
	n, err := reader.Reader.ReadAt(buffer, offset)
	if offset >= reader.frameOffset {
		reader.cancel()
	}
	return n, err
}

func TestAnalysisPreviewUntransformedImageCancellationDuringFrameRead(t *testing.T) {
	lease, archive := analysisPreviewFixture(t)
	entry, _ := analysisPreviewFixtureFrame(t, archive, 0)
	r := analysisPreviewTestRequest("GET", "?PositionTicks=0")
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	lease.reader = analysisPreviewCancelFrameReader{Reader: bytes.NewReader(archive), frameOffset: int64(entry.Offset), cancel: cancel}
	r = r.WithContext(ctx)
	r.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(w, r, &analysisPreviewTestProvider{lease: lease}, "image")
	if ctx.Err() != context.Canceled || w.Body.Len() != 0 || w.Header().Get("ETag") != "" || lease.checks != 0 || lease.closeCount.Load() != 1 {
		t.Fatal("a canceled frame read delivered a response, advanced to final authority or retained its lease")
	}
}
