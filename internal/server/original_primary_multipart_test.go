package server

import (
	"bytes"
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

type originalPrimaryMultipartTestSource struct{ reader *bytes.Reader }

func (s *originalPrimaryMultipartTestSource) Read(p []byte) (int, error) { return s.reader.Read(p) }
func (s *originalPrimaryMultipartTestSource) Seek(offset int64, whence int) (int64, error) {
	return s.reader.Seek(offset, whence)
}
func (s *originalPrimaryMultipartTestSource) Close() error { return nil }

type originalPrimaryMultipartPart struct {
	contentRange string
	body         string
}

func originalPrimaryMultipartParts(t *testing.T, response *httptest.ResponseRecorder) []originalPrimaryMultipartPart {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Type"))
	if err != nil || mediaType != "multipart/byteranges" {
		t.Fatalf("multipart content type = %q: %v", response.Header().Get("Content-Type"), err)
	}
	reader := multipart.NewReader(bytes.NewReader(response.Body.Bytes()), parameters["boundary"])
	var parts []originalPrimaryMultipartPart
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, originalPrimaryMultipartPart{part.Header.Get("Content-Range"), string(body)})
	}
	return parts
}

func TestOriginalPrimaryMultipartMatchesPinnedStandardProtocol(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789abcdef"), 6000)
	modified := time.Unix(1700000000, 0).UTC()
	cases := []struct{ name, method, ranges, ifRange, ifNoneMatch string }{
		{"cross-chunk", http.MethodGet, "bytes=32760-65545,80000-80031", "", ""},
		{"suffix-zero", http.MethodGet, "bytes=-0,0-7", "", ""},
		{"suffix-open", http.MethodGet, "bytes=-9,65536-", "", ""},
		{"skipped-invalid-end", http.MethodGet, "bytes=999999-invalid,0-7,10-15", "", ""},
		{"empty-parts", http.MethodGet, "bytes=,0-7,,10-15,", "", ""},
		{"single-survivor", http.MethodGet, "bytes=999999-,0-7", "", ""},
		{"invalid-range", http.MethodGet, "bytes=broken,0-7", "", ""},
		{"no-overlap", http.MethodGet, "bytes=999999-,888888-", "", ""},
		{"excess-aggregate", http.MethodGet, "bytes=0-,0-", "", ""},
		{"if-range-match", http.MethodGet, "bytes=0-7,10-15", `"current"`, ""},
		{"if-range-miss", http.MethodGet, "bytes=0-7,10-15", `"different"`, ""},
		{"not-modified", http.MethodGet, "bytes=0-7,10-15", "", `"current"`},
		{"head", http.MethodHead, "bytes=0-7,10-15", "", ""},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			governor, err := primaryio.NewGovernor(primaryio.Limits{Owners: 4, BackgroundOwners: 2, RootOwners: 3, RootBackgroundOwners: 2, DomainOwners: 3, DomainBackgroundOwners: 2, Queued: 128, RootQueued: 32, DomainQueued: 32})
			if err != nil {
				t.Fatal(err)
			}
			owners, err := primaryio.NewOwnerRuntime(governor, 64)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := owners.Register(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			content, err := primaryio.NewReadSeeker(owner, primaryio.Route{Roots: []primaryio.RootKey{{Catalog: "protocol", RootID: "source"}}, Domains: []string{"domain"}}, primaryio.Foreground, &originalPrimaryMultipartTestSource{bytes.NewReader(data)}, primaryio.MaxReadChunkBytes, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = content.Close(); governor.Close() })
			request := httptest.NewRequest(test.method, "/original", nil)
			request.Header.Set("Range", test.ranges)
			request.Header.Set("If-Range", test.ifRange)
			request.Header.Set("If-None-Match", test.ifNoneMatch)
			actual, control := httptest.NewRecorder(), httptest.NewRecorder()
			for _, writer := range []*httptest.ResponseRecorder{actual, control} {
				writer.Header().Set("ETag", `"current"`)
				writer.Header().Set("Content-Type", "application/octet-stream")
			}
			if err := serveOriginalPrimaryContent(actual, request, "original.bin", modified, "application/octet-stream", int64(len(data)), content); err != nil {
				t.Fatal(err)
			}
			http.ServeContent(control, request, "original.bin", modified, bytes.NewReader(data))
			if actual.Code != control.Code {
				t.Fatalf("status = %d; standard=%d", actual.Code, control.Code)
			}
			for _, header := range []string{"Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
				if actual.Header().Get(header) != control.Header().Get(header) {
					t.Fatalf("%s = %q; standard=%q", header, actual.Header().Get(header), control.Header().Get(header))
				}
			}
			mediaType, _, _ := mime.ParseMediaType(actual.Header().Get("Content-Type"))
			if test.method == http.MethodHead {
				if actual.Body.Len() != 0 || control.Body.Len() != 0 {
					t.Fatal("HEAD delivered body bytes")
				}
			} else if mediaType == "multipart/byteranges" {
				if got, want := originalPrimaryMultipartParts(t, actual), originalPrimaryMultipartParts(t, control); !reflect.DeepEqual(got, want) {
					t.Fatalf("multipart parts = %+v; standard=%+v", got, want)
				}
				length, err := strconv.ParseInt(actual.Header().Get("Content-Length"), 10, 64)
				if err != nil || length != int64(actual.Body.Len()) {
					t.Fatalf("declared multipart length = %q; actual=%d", actual.Header().Get("Content-Length"), actual.Body.Len())
				}
			} else if !bytes.Equal(actual.Body.Bytes(), control.Body.Bytes()) {
				t.Fatal("non-multipart body differs from standard response")
			}
			if err := content.Close(); err != nil {
				t.Fatal(err)
			}
			if governor.Stats().Active != 0 || owners.Stats().RegisteredOwners != 0 {
				t.Fatal("protocol response retained a read phase or descriptor owner")
			}
		})
	}
}
