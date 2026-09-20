package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestHTTPImageSelectedTransformsProducePixelsAndStableValidators(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/emby/Items/" + fixture.itemID + "/Images/Primary"
	for _, test := range []struct {
		query         string
		width, height int
	}{
		{"Crop=0,0,100,80&Format=png", 100, 80},
		{"CropX=0&CropY=0&CropWidth=100&CropHeight=80&Format=png", 100, 80},
		{"Crop=0,0,100,80&Format=png&Width=50", 50, 40},
		{"Crop=0,0,100,80&Format=png&Height=20", 25, 20},
	} {
		response := f.request(t, http.MethodGet, path+"?"+test.query, nil, fixture.headers)
		assertAPIImage(t, response, test.width, test.height, "png")
		assertSelectedImageRepeatAndConditional(t, fixture, path+"?"+test.query, response)
	}
	expectAPIError(t, f.request(t, http.MethodGet, path+"?Crop=120,200,100,80&Format=png", nil, fixture.headers),
		http.StatusBadRequest, "invalid_image_request", true)

	// An opaque JPEG cannot expose background compositing. Replace its sidecar
	// with a transparent PNG through the real scan before comparing effects.
	selectedImageTransparentPoster(t, fixture)
	baseline := f.request(t, http.MethodGet, path+"?Format=png", nil, fixture.headers)
	assertAPIImage(t, baseline, 160, 240, "png")
	seen := map[string]string{baseline.Header().Get("ETag"): "baseline"}
	for _, effect := range []string{
		"BackgroundColor=black",
		"BackgroundColor=white",
		"ForegroundLayer=play",
		"ForegroundLayer=music",
		"ForegroundLayer=folder",
		"AddPlayedIndicator=true",
		"PercentPlayed=37.5",
		"UnplayedCount=12",
		"BackgroundColor=black&ForegroundLayer=play&AddPlayedIndicator=true&PercentPlayed=37.5&UnplayedCount=12",
	} {
		t.Run(effect, func(t *testing.T) {
			url := path + "?Format=png&" + effect
			response := f.request(t, http.MethodGet, url, nil, fixture.headers)
			assertAPIImage(t, response, 160, 240, "png")
			if bytes.Equal(response.Body.Bytes(), baseline.Body.Bytes()) {
				t.Error("requested effect did not change the PNG representation")
			}
			etag := response.Header().Get("ETag")
			if previous, exists := seen[etag]; exists {
				t.Errorf("effect %q reused the validator for %q", effect, previous)
			}
			seen[etag] = effect
			assertSelectedImageRepeatAndConditional(t, fixture, url, response)
		})
	}
}

func TestHTTPGeneratedLibraryImagesProjectManifestAndRecheckChangedSources(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/emby/Items/" + fixture.libraryID + "/Images/Primary"
	response := f.request(t, http.MethodGet, path, nil, fixture.headers)
	assertAPIImage(t, response, 512, 512, "png")
	manifestTag := strings.Trim(response.Header().Get("ETag"), `"`)
	listed, err := f.app.library.ListImagesFor(f.ctx, library.Subject{UserID: fixture.adminID}, fixture.libraryID)
	if err != nil {
		t.Fatalf("read generated image manifest: %v", err)
	}
	if len(listed) != 1 || listed[0].ImageType != "Primary" || listed[0].Source != "generated" ||
		listed[0].Tag != manifestTag || listed[0].SourceRevision != manifestTag {
		t.Fatalf("HTTP validator differs from the selected generated manifest: %+v", listed)
	}
	views, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+fixture.adminID+"/Views", nil, fixture.headers))
	if total != 1 || len(views) != 1 || views[0]["Id"] != fixture.libraryID {
		t.Fatalf("generated library view is missing: %#v, total=%d", views, total)
	}
	if tag := objectValue(t, views[0], "ImageTags")["Primary"]; tag != manifestTag {
		t.Errorf("Views Primary tag = %v, want generated manifest %q", tag, manifestTag)
	}
	detailResponse := f.request(t, http.MethodGet, "/emby/Users/"+fixture.adminID+"/Items/"+fixture.libraryID, nil, fixture.headers)
	expectStatus(t, detailResponse, http.StatusOK)
	if tag := objectValue(t, jsonObject(t, detailResponse), "ImageTags")["Primary"]; tag != manifestTag {
		t.Errorf("collection detail Primary tag = %v, want generated manifest %q", tag, manifestTag)
	}
	assertSelectedImageRepeatAndConditional(t, fixture, path, response)
	head := f.request(t, http.MethodHead, path, nil, fixture.headers)
	expectStatus(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Type") != "image/png" ||
		head.Header().Get("Content-Length") != strconv.Itoa(response.Body.Len()) || head.Header().Get("ETag") != response.Header().Get("ETag") {
		t.Fatalf("generated HEAD differs from GET metadata: headers=%v body=%d", head.Header(), head.Body.Len())
	}
	variantPath := path + "?Width=128&Format=png"
	variant := f.request(t, http.MethodGet, variantPath, nil, fixture.headers)
	assertAPIImage(t, variant, 128, 128, "png")
	assertSelectedImageRepeatAndConditional(t, fixture, variantPath, variant)

	// The manifest still points to the old catalog observation. Changing bytes
	// on the same inode with matching size and mtime must invalidate even a warm
	// generated-image cache before either a 200 or a conditional 304 is written.
	stat, err := os.Stat(fixture.posterPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := bytes.Clone(fixture.poster)
	changed[len(changed)/2] ^= 1
	if err := os.WriteFile(fixture.posterPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fixture.posterPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	current, err := os.Stat(fixture.posterPath)
	if err != nil || current.Size() != stat.Size() || !current.ModTime().Equal(stat.ModTime()) {
		t.Fatalf("source change did not preserve its metadata fixture: %v", err)
	}
	for _, request := range []struct {
		path string
		etag string
	}{
		{path, response.Header().Get("ETag")},
		{variantPath, variant.Header().Get("ETag")},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, validator := range []string{"", request.etag, "*"} {
				headers := fixture.headers.Clone()
				if validator != "" {
					headers.Set("If-None-Match", validator)
				}
				stale := f.request(t, method, request.path, nil, headers)
				expectStatus(t, stale, http.StatusServiceUnavailable)
				if stale.Header().Get("ETag") != "" || bytes.Equal(stale.Body.Bytes(), response.Body.Bytes()) || bytes.Equal(stale.Body.Bytes(), variant.Body.Bytes()) {
					t.Errorf("changed collage member exposed cached bytes or validator: method=%s headers=%v", method, stale.Header())
				}
			}
		}
	}
}

func TestHTTPGeneratedLibraryImagesRecheckViewerACLBeforeCachedValidators(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	viewer, err := f.users.CreateUser(f.ctx, "Generated Image Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{fixture.libraryID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy=$1::jsonb WHERE id=$2", policy, viewer.ID); err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, viewer.Name, "viewer-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	path := "/emby/Items/" + fixture.libraryID + "/Images/Primary"
	type warmedImage struct {
		path string
		etag string
		data []byte
	}
	var warmed []warmedImage
	for _, query := range []string{"", "?Width=128&Format=png"} {
		response := f.request(t, http.MethodGet, path+query, nil, headers)
		dimension := 512
		if query != "" {
			dimension = 128
		}
		assertAPIImage(t, response, dimension, dimension, "png")
		validatorHeaders := headers.Clone()
		validatorHeaders.Set("If-None-Match", response.Header().Get("ETag"))
		assertImageNotModified(t, f.request(t, http.MethodGet, path+query, nil, validatorHeaders), response.Header().Get("ETag"))
		warmed = append(warmed, warmedImage{path: path + query, etag: response.Header().Get("ETag"), data: bytes.Clone(response.Body.Bytes())})
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
		t.Fatal(err)
	}
	for _, request := range warmed {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, validator := range []string{request.etag, "*"} {
				validatorHeaders := headers.Clone()
				validatorHeaders.Set("If-None-Match", validator)
				denied := f.request(t, method, request.path, nil, validatorHeaders)
				expectStatus(t, denied, http.StatusNotFound)
				if denied.Header().Get("ETag") != "" || bytes.Equal(denied.Body.Bytes(), request.data) {
					t.Errorf("revoked viewer retained generated bytes or validator: method=%s headers=%v", method, denied.Header())
				}
			}
		}
	}
	views, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.ID+"/Views", nil, headers))
	if total != 0 || len(views) != 0 {
		t.Fatal("revoked viewer retained its generated library DTO")
	}
	// Revoking the viewer's library scope does not remove the aggregate image
	// for an independent administrator with current access to its sources.
	assertAPIImage(t, f.request(t, http.MethodGet, path, nil, fixture.headers), 512, 512, "png")
}

func assertSelectedImageRepeatAndConditional(t *testing.T, fixture *imageAPIFixture, path string, first *httptest.ResponseRecorder) {
	t.Helper()
	etag := first.Header().Get("ETag")
	repeated := fixture.f.request(t, http.MethodGet, path, nil, fixture.headers)
	expectStatus(t, repeated, http.StatusOK)
	if repeated.Header().Get("ETag") != etag || !bytes.Equal(repeated.Body.Bytes(), first.Body.Bytes()) {
		t.Errorf("cached image request changed its bytes or validator: %s", path)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		conditional := fixture.f.request(t, method, path, nil, imageValidatorHeaders(fixture, etag))
		assertImageNotModified(t, conditional, etag)
	}
}

func selectedImageTransparentPoster(t *testing.T, fixture *imageAPIFixture) {
	t.Helper()
	picture := image.NewNRGBA(image.Rect(0, 0, 160, 240))
	for y := range 240 {
		for x := range 160 {
			picture.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 100, A: uint8(40 + (x+y)%120)})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSuffix(fixture.posterPath, filepath.Ext(fixture.posterPath)) + ".png"
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.posterPath); err != nil {
		t.Fatal(err)
	}
	fixture.posterPath, fixture.poster = path, bytes.Clone(encoded.Bytes())
	rescanAPIImageLibrary(t, fixture)
}
