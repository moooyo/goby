package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/moooyo/goby/internal/bif"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

type analysisPreviewTestLease struct {
	metadata   analysisPreviewMetadata
	reader     analysisPreviewReader
	ctx        context.Context
	checkError error
	checks     int
	closeCount atomic.Int32
}

func (lease *analysisPreviewTestLease) Context() context.Context          { return lease.ctx }
func (lease *analysisPreviewTestLease) Metadata() analysisPreviewMetadata { return lease.metadata }
func (lease *analysisPreviewTestLease) Reader() analysisPreviewReader     { return lease.reader }
func (lease *analysisPreviewTestLease) Close() error                      { lease.closeCount.Add(1); return nil }
func (lease *analysisPreviewTestLease) Revalidate(ctx context.Context) error {
	lease.checks++
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return lease.checkError
}

type analysisPreviewTestProvider struct {
	lease     *analysisPreviewTestLease
	err       error
	width     int
	principal identity.Principal
}

func (provider *analysisPreviewTestProvider) OpenPreview(_ context.Context, principal identity.Principal, item, source string, width int) (analysisPreviewLease, error) {
	provider.width, provider.principal = width, principal
	return provider.lease, provider.err
}

func analysisPreviewFixture(t *testing.T) (*analysisPreviewTestLease, []byte) {
	t.Helper()
	pixels := image.NewRGBA(image.Rect(0, 0, 400, 224))
	for y := 0; y < 224; y++ {
		for x := 0; x < 400; x++ {
			pixels.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8(x + y), A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, pixels, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal("encode bounded preview fixture")
	}
	archive, err := bif.Encode(context.Background(), []bif.Frame{{Timestamp: 0, JPEG: encoded.Bytes()}, {Timestamp: 10, JPEG: encoded.Bytes()}}, 1000, bif.DefaultLimits())
	if err != nil {
		t.Fatal("encode valid preview BIF fixture")
	}
	digest := sha256.Sum256(archive)
	return &analysisPreviewTestLease{metadata: analysisPreviewMetadata{ItemID: "item", MediaSourceID: "media-source", SourceRevision: "source-revision", BIFSHA256: hex.EncodeToString(digest[:]), Width: 400, Height: 224, Size: int64(len(archive)), Ready: true}, reader: bytes.NewReader(archive), ctx: context.Background()}, archive
}

func analysisPreviewTestRequest(method, query string) *http.Request {
	r := httptest.NewRequest(method, "/preview"+query, nil)
	r.SetPathValue("Id", "item")
	return r.WithContext(context.WithValue(r.Context(), principalKey, identity.Principal{Kind: "emby", SessionID: "trusted-session", User: identity.User{ID: "owner"}}))
}

func TestAnalysisPreviewThumbnailSetUsesExactSDKShapeAndSourceBoundTags(t *testing.T) {
	lease, _ := analysisPreviewFixture(t)
	provider := &analysisPreviewTestProvider{lease: lease}
	w := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?Width=400&MediaSourceId=media-source"), provider, "set")
	var object map[string]json.RawMessage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &object) != nil || len(object) != 2 || object["AspectRatio"] == nil || object["Thumbnails"] == nil {
		t.Fatal("thumbnail set differs from exact pinned SDK object shape")
	}
	var result analysisThumbnailSetDTO
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.AspectRatio != float64(400)/224 || len(result.Thumbnails) != 2 || result.Thumbnails[0].PositionTicks != 0 || result.Thumbnails[1].PositionTicks != 100000000 {
		t.Fatal("thumbnail set lost actual BIF timeline or aspect ratio")
	}
	var nested []map[string]json.RawMessage
	if json.Unmarshal(object["Thumbnails"], &nested) != nil || len(nested[0]) != 2 || nested[0]["ImageTag"] == nil || nested[0]["PositionTicks"] == nil {
		t.Fatal("thumbnail entry differs from exact pinned SDK fields")
	}
	if lease.checks != 1 || lease.closeCount.Load() != 1 || provider.width != 400 || provider.principal.User.ID != "owner" {
		t.Fatal("preview discovery lost authority, source revalidation or its single lease close")
	}
	changed := lease.metadata
	changed.SourceRevision = "replacement"
	if analysisPreviewImageTag(changed, 0) == result.Thumbnails[0].ImageTag {
		t.Fatal("thumbnail tags survived source replacement")
	}
}

func TestAnalysisPreviewImageUsesTagVariantAndBoundsDisplaySize(t *testing.T) {
	for _, maximum := range []int{200, 800} {
		lease, _ := analysisPreviewFixture(t)
		provider := &analysisPreviewTestProvider{lease: lease}
		tag := analysisPreviewImageTag(lease.metadata, 100000000)
		r := analysisPreviewTestRequest("GET", "?PositionTicks=150000000&maxWidth="+strconv.Itoa(maximum)+"&quality=90&tag="+tag)
		w := httptest.NewRecorder()
		(&Server{}).serveAnalysisPreview(w, r, provider, "image")
		decoded, err := jpeg.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
		if w.Code != 200 || err != nil || decoded.Width != min(maximum, 400) || provider.width != 400 || lease.checks != 1 || lease.closeCount.Load() != 1 {
			t.Fatal("display width changed the stored variant, enlarged the JPEG or bypassed revalidation")
		}
	}
	lease, _ := analysisPreviewFixture(t)
	tag := analysisPreviewImageTag(lease.metadata, 0)
	lease.metadata.SourceRevision = "replacement"
	w := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?PositionTicks=0&tag="+tag), &analysisPreviewTestProvider{lease: lease}, "image")
	if w.Code != 404 || w.Header().Get("ETag") != "" || lease.closeCount.Load() != 1 {
		t.Fatal("a stale ImageTag selected a replacement source image")
	}
}

func TestAnalysisPreviewAuthorizationPrecedesConditionalHeadAndRangeResponses(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		for _, headers := range []http.Header{
			{"If-None-Match": {"*"}}, {"Range": {"bytes=0-7"}}, {"Range": {"bytes=999999999-"}},
		} {
			lease, _ := analysisPreviewFixture(t)
			lease.checkError = library.ErrForbidden
			r := analysisPreviewTestRequest(method, "?Width=400")
			r.Header = headers
			w := httptest.NewRecorder()
			(&Server{}).serveAnalysisPreview(w, r, &analysisPreviewTestProvider{lease: lease}, "bif")
			if w.Code != 403 || w.Header().Get("ETag") != "" || w.Header().Get("Content-Range") != "" || lease.checks != 1 || lease.closeCount.Load() != 1 {
				t.Fatal("conditional, HEAD or range processing bypassed current media authority")
			}
		}
	}
	lease, _ := analysisPreviewFixture(t)
	w := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?Width=400"), &analysisPreviewTestProvider{lease: lease, err: library.ErrForbidden}, "bif")
	if w.Code != 403 || lease.checks != 0 || lease.closeCount.Load() != 1 {
		t.Fatal("failed authorized open retained or used its returned lease")
	}
}

func TestAnalysisPreviewMissingResultsFollowExplicitEndpointContracts(t *testing.T) {
	for _, kind := range []string{"set", "bif", "image"} {
		lease := &analysisPreviewTestLease{metadata: analysisPreviewMetadata{ItemID: "item", MediaSourceID: "media-source", SourceRevision: "current-source"}, ctx: context.Background()}
		query := "?Width=400"
		if kind == "image" {
			query = "?PositionTicks=0"
		}
		w := httptest.NewRecorder()
		(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", query), &analysisPreviewTestProvider{lease: lease}, kind)
		if kind == "bif" {
			decoded, err := bif.Open(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()), bif.DefaultLimits())
			if w.Code != 200 || err != nil || decoded.Len() != 0 || w.Body.Len() != 72 {
				t.Fatal("missing derivative did not return a valid empty BIF")
			}
		} else if w.Code != 404 {
			t.Fatal("missing metadata or JPEG did not return the documented not-found result")
		}
		if lease.checks != 1 || lease.closeCount.Load() != 1 {
			t.Fatal("missing derivative skipped current source authorization or lease cleanup")
		}
	}
}

func TestAnalysisPreviewBIFDeliveryPreservesBytesAndCloses304Lease(t *testing.T) {
	lease, archive := analysisPreviewFixture(t)
	w := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?Width=400"), &analysisPreviewTestProvider{lease: lease}, "bif")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), archive) || lease.closeCount.Load() != 1 {
		t.Fatal("full BIF response changed bytes or leaked its lease")
	}
	fresh, _ := analysisPreviewFixture(t)
	r := analysisPreviewTestRequest("GET", "?Width=400")
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	conditional := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(conditional, r, &analysisPreviewTestProvider{lease: fresh}, "bif")
	if conditional.Code != 304 || conditional.Body.Len() != 0 || fresh.checks != 1 || fresh.closeCount.Load() != 1 {
		t.Fatal("conditional BIF did not reauthorize and close exactly once")
	}
}

func TestAnalysisPreviewMalformedReadyMetadataDoesNotExposeValidators(t *testing.T) {
	for _, mutate := range []func(*analysisPreviewMetadata){
		func(value *analysisPreviewMetadata) { value.MediaSourceID = "another" },
		func(value *analysisPreviewMetadata) { value.BIFSHA256 = strings.Repeat("g", 64) },
		func(value *analysisPreviewMetadata) { value.Size = analysisPreviewMaxBIFBytes + 1 },
		func(value *analysisPreviewMetadata) { value.Width = 800 },
	} {
		lease, _ := analysisPreviewFixture(t)
		mutate(&lease.metadata)
		w := httptest.NewRecorder()
		(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?Width=400&MediaSourceId=media-source"), &analysisPreviewTestProvider{lease: lease}, "bif")
		if w.Code != 503 || w.Header().Get("ETag") != "" || lease.closeCount.Load() != 1 {
			t.Fatal("invalid ready metadata was served or leaked its lease")
		}
	}
}

func TestAnalysisPreviewCancellationClosesOnceWithoutResponse(t *testing.T) {
	lease, _ := analysisPreviewFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease.ctx = ctx
	w := httptest.NewRecorder()
	(&Server{}).serveAnalysisPreview(w, analysisPreviewTestRequest("GET", "?Width=400"), &analysisPreviewTestProvider{lease: lease}, "bif")
	if lease.closeCount.Load() != 1 || w.Body.Len() != 0 || w.Header().Get("ETag") != "" {
		t.Fatal("canceled preview lifetime leaked a lease or delivered content")
	}
}

func TestAnalysisPreviewLiteralRoutesCoexistWithArtworkAndStreamWildcards(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /emby/Items/{Id}/Images/{Type}", func(http.ResponseWriter, *http.Request) {})
	mux.HandleFunc("GET /emby/Videos/{Id}/{StreamFileName}", func(http.ResponseWriter, *http.Request) {})
	(&Server{}).registerAnalysisPreviewRoutes(mux, nil)
	for _, path := range []string{"/emby/Items/item/ThumbnailSet", "/emby/Videos/item/index.bif", "/emby/Items/item/Images/Thumbnail"} {
		_, pattern := mux.Handler(httptest.NewRequest("GET", path, nil))
		if strings.Contains(pattern, "{Type}") || strings.Contains(pattern, "{StreamFileName}") || pattern == "" {
			t.Fatal("specific preview route lost precedence to an existing wildcard")
		}
	}
	if reflect.TypeOf(analysisThumbnailSetDTO{}).NumField() != 2 || reflect.TypeOf(analysisThumbnailDTO{}).NumField() != 2 {
		t.Fatal("pinned preview DTOs gained undocumented fields")
	}
}

type analysisPreviewShortReader struct{ *bytes.Reader }

func (reader analysisPreviewShortReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAnalysisPreviewTransmissionFailureStillClosesLease(t *testing.T) {
	lease, archive := analysisPreviewFixture(t)
	lease.reader = analysisPreviewShortReader{Reader: bytes.NewReader(archive)}
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler || lease.closeCount.Load() != 1 {
			t.Fatal("a truncated BIF response was not aborted with its lease released")
		}
	}()
	(&Server{}).serveAnalysisPreview(httptest.NewRecorder(), analysisPreviewTestRequest("GET", "?Width=400"), &analysisPreviewTestProvider{lease: lease}, "bif")
}

func TestAnalysisPreviewRoutesRequireCredentialsEvenForImageTagsAndValidators(t *testing.T) {
	lease, _ := analysisPreviewFixture(t)
	mux := http.NewServeMux()
	(&Server{}).registerAnalysisPreviewRoutes(mux, &analysisPreviewTestProvider{lease: lease})
	for _, path := range []string{
		"/emby/Items/item/ThumbnailSet?Width=400",
		"/emby/Videos/item/index.bif?Width=400",
		"/emby/Items/item/Images/Thumbnail?PositionTicks=0&tag=" + analysisPreviewImageTag(lease.metadata, 0),
	} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("If-None-Match", "*")
		r.Header.Set("Range", "bytes=0-7")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized || w.Header().Get("ETag") != "" || lease.closeCount.Load() != 0 {
			t.Fatal("a credential-free preview reached its provider or a cached response")
		}
	}
}
