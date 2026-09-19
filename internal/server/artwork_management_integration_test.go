package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
)

func TestArtworkRevisionRequiresCanonicalStrongValidator(t *testing.T) {
	for _, value := range []string{"0", "42", strings.Repeat("9", 78)} {
		r := httptest.NewRequest(http.MethodPut, "/test", nil)
		r.Header.Set("If-Match", strconv.Quote(value))
		if actual, err := artworkRevisionHeader(r, true); err != nil || actual != value {
			t.Fatalf("valid revision %q: %q, %v", value, actual, err)
		}
	}
	for _, value := range []string{"1", "'1'", "`1`", "W/\"1\"", "\"1\", \"2\"", "\"01\"", "\"\"", "*", "\"\\u0031\"", strconv.Quote(strings.Repeat("9", 79))} {
		r := httptest.NewRequest(http.MethodPut, "/test", nil)
		r.Header.Set("If-Match", value)
		if _, err := artworkRevisionHeader(r, true); err == nil {
			t.Errorf("accepted noncanonical image validator %q", value)
		}
	}
	r := httptest.NewRequest(http.MethodPut, "/test", nil)
	r.Header.Add("If-Match", "\"1\"")
	r.Header.Add("If-Match", "\"1\"")
	if _, err := artworkRevisionHeader(r, true); err == nil {
		t.Fatal("accepted multiple If-Match field lines")
	}
}

func artworkRawRequest(t *testing.T, f *serverFixture, method, path string, data []byte, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(data)).WithContext(f.ctx)
	for name, values := range headers {
		for _, value := range values {
			r.Header.Add(name, value)
		}
	}
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func artworkHTTPRevision(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	return stringValue(t, jsonObject(t, response), "Revision")
}

func TestHTTPArtworkManagementRevisionMaskResetAndCompatibility(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	base := "/admin/v1/items/" + fixture.itemID + "/images"
	compat := "/emby/Items/" + fixture.itemID + "/Images/Primary"
	before := f.request(t, http.MethodGet, base, nil, nil, fixture.cookie)
	revision := artworkHTTPRevision(t, before)
	if before.Header().Get("ETag") != strconv.Quote(revision) {
		t.Fatal("native artwork ETag did not describe its editable snapshot")
	}
	data := writeAPIJPEG(t, filepath.Join(t.TempDir(), "upload.jpg"), 32, 24, 4)
	headers := http.Header{"Content-Type": {"image/jpeg"}, "If-Match": {strconv.Quote(revision)}}
	headers.Set("X-CSRF-Token", fixture.csrf)
	missingCSRF := headers.Clone()
	missingCSRF.Del("X-CSRF-Token")
	expectAPIError(t, artworkRawRequest(t, f, http.MethodPut, base+"/Primary/0", data, missingCSRF, fixture.cookie), http.StatusForbidden, "csrf_invalid", false)
	missingRevision := headers.Clone()
	missingRevision.Del("If-Match")
	expectStatus(t, artworkRawRequest(t, f, http.MethodPut, base+"/Primary/0", data, missingRevision, fixture.cookie), http.StatusPreconditionRequired)
	updated := artworkRawRequest(t, f, http.MethodPut, base+"/Primary/0", data, headers, fixture.cookie)
	next := artworkHTTPRevision(t, updated)
	if next == revision {
		t.Fatal("successful replacement did not invalidate the old revision")
	}
	expectStatus(t, artworkRawRequest(t, f, http.MethodPut, base+"/Primary/0", fixture.poster, headers, fixture.cookie), http.StatusConflict)
	preview := base + "/Primary/0?Tag=" + imageSourceTag(data)
	content := f.request(t, http.MethodGet, preview, nil, nil, fixture.cookie)
	assertAPIImage(t, content, 32, 24, "jpeg")
	if !bytes.Equal(content.Body.Bytes(), data) || content.Header().Get("Cache-Control") != "private, no-cache, no-transform" {
		t.Fatal("native preview did not preserve original bytes and private cache rules")
	}
	expectStatus(t, f.request(t, http.MethodGet, preview, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, compat, nil, nil), http.StatusUnauthorized)
	assertAPIImage(t, f.request(t, http.MethodGet, compat, nil, fixture.headers), 32, 24, "jpeg")
	deleted := f.request(t, http.MethodDelete, base+"/Primary/0", nil,
		http.Header{"X-CSRF-Token": {fixture.csrf}, "If-Match": {strconv.Quote(next)}}, fixture.cookie)
	deletedRevision := artworkHTTPRevision(t, deleted)
	expectStatus(t, f.request(t, http.MethodGet, compat, nil, fixture.headers), http.StatusNotFound)
	if original, err := os.ReadFile(fixture.posterPath); err != nil || !bytes.Equal(original, fixture.poster) {
		t.Fatalf("managed deletion changed automatic image source: %v", err)
	}
	reset := f.request(t, http.MethodPost, base+"/Primary/reset", map[string]any{"Revision": deletedRevision}, http.Header{"X-CSRF-Token": {fixture.csrf}}, fixture.cookie)
	artworkHTTPRevision(t, reset)
	assertAPIImage(t, f.request(t, http.MethodGet, compat, nil, fixture.headers), 160, 240, "jpeg")
	compatHeaders := fixture.headers.Clone()
	compatHeaders.Set("Content-Type", "application/octet-stream")
	expectStatus(t, artworkRawRequest(t, f, http.MethodPost, compat, data, compatHeaders), http.StatusOK)
	assertAPIImage(t, f.request(t, http.MethodGet, compat, nil, fixture.headers), 32, 24, "jpeg")
	expectStatus(t, f.request(t, http.MethodPost, compat+"/Delete", nil, fixture.headers), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodGet, compat, nil, fixture.headers), http.StatusNotFound)
}

func TestHTTPManagedEntityImagesUseVisibleAssociationsAndNativeCAS(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	path := writeAPIMediaFile(t, root, "movies/Entity.Movie.mp4")
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "Entity.Movie.nfo"), []byte(`<movie><genre>Artwork Genre</genre><studio>Artwork Studio</studio><actor><name>Artwork Person</name></actor></movie>`), 0600); err != nil {
		t.Fatal(err)
	}
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	createAndScanAPILibrary(t, f, cookie, csrf, filepath.Dir(path), "movies")
	login := f.embyLogin(t, "Administrator", "administrator-password")
	emby := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	data := writeAPIJPEG(t, filepath.Join(t.TempDir(), "entity.jpg"), 30, 20, 8)
	for _, fixture := range []struct{ kind, route, name string }{
		{"Genre", "Genres", "Artwork%20Genre"}, {"Studio", "Studios", "Artwork%20Studio"}, {"Person", "Persons", "Artwork%20Person"},
	} {
		items, total := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/entities?Kind="+fixture.kind+"&StartIndex=0&Limit=25", nil, nil, cookie))
		if total != 1 || len(items) != 1 {
			t.Fatalf("native entity list omitted %s: %+v", fixture.kind, items)
		}
		id := stringValue(t, items[0], "Id")
		base := "/admin/v1/entities/" + id + "/images"
		revision := artworkHTTPRevision(t, f.request(t, http.MethodGet, base, nil, nil, cookie))
		updated := artworkRawRequest(t, f, http.MethodPut, base+"/Primary/0", data,
			http.Header{"X-CSRF-Token": {csrf}, "If-Match": {strconv.Quote(revision)}, "Content-Type": {"image/jpeg"}}, cookie)
		artworkHTTPRevision(t, updated)
		entity := jsonObject(t, f.request(t, http.MethodGet, "/emby/"+fixture.route+"/"+fixture.name, nil, emby))
		if objectValue(t, entity, "ImageTags")["Primary"] != imageSourceTag(data) {
			t.Fatalf("entity %s omitted its managed image tag", fixture.kind)
		}
		for _, imagePath := range []string{"/emby/Items/" + id + "/Images/Primary", "/emby/" + fixture.route + "/" + fixture.name + "/Images/Primary"} {
			assertAPIImage(t, f.request(t, http.MethodGet, imagePath, nil, emby), 30, 20, "jpeg")
			expectStatus(t, f.request(t, http.MethodGet, imagePath, nil, nil), http.StatusUnauthorized)
		}
		viewer, err := f.users.CreateUser(f.ctx, "Entity image "+fixture.kind, "viewer-password", false)
		if err != nil {
			t.Fatal(err)
		}
		viewerLogin := f.embyLogin(t, viewer.Name, "viewer-password")
		viewerHeaders := http.Header{"X-Emby-Token": {stringValue(t, viewerLogin, "AccessToken")}}
		imagePath := "/emby/" + fixture.route + "/" + fixture.name + "/Images/Primary"
		visible := f.request(t, http.MethodGet, imagePath, nil, viewerHeaders)
		assertAPIImage(t, visible, 30, 20, "jpeg")
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id=$1`, viewer.ID); err != nil {
			t.Fatal(err)
		}
		viewerHeaders.Set("If-None-Match", visible.Header().Get("ETag"))
		expectStatus(t, f.request(t, http.MethodGet, imagePath, nil, viewerHeaders), http.StatusNotFound)
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/"+id+"/Images/Primary", nil, viewerHeaders), http.StatusNotFound)
	}
}

func TestArtworkDeliveryRechecksAuthorityAndSourceBeforeConditionalResponse(t *testing.T) {
	data := writeAPIJPEG(t, filepath.Join(t.TempDir(), "source.jpg"), 12, 8, 3)
	for _, scenario := range []struct {
		name    string
		revoked bool
		want    int
	}{
		{"revoked after render", true, http.StatusNotFound}, {"source replaced after render", false, http.StatusConflict},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s := &Server{images: newImageCache()}
			r := httptest.NewRequest(http.MethodGet, "/test", nil)
			r.SetPathValue("Type", "Primary")
			r.SetPathValue("Index", "0")
			r.Header.Set("If-None-Match", strconv.Quote(imageSourceTag(data)))
			calls := 0
			w := httptest.NewRecorder()
			s.serveArtworkImage(w, r, func(context.Context) (io.ReadCloser, library.Image, error) {
				calls++
				if calls == 2 && scenario.revoked {
					return nil, library.Image{}, library.ErrNotFound
				}
				tag := imageSourceTag(data)
				if calls == 2 {
					tag = "changed"
				}
				return io.NopCloser(bytes.NewReader(data)), library.Image{ImageType: "Primary", Tag: tag, MIMEType: "image/jpeg", Width: 12, Height: 8, Size: int64(len(data))}, nil
			})
			expectStatus(t, w, scenario.want)
			if calls != 2 || w.Header().Get("ETag") != "" {
				t.Fatalf("cache response escaped the final source check: calls=%d headers=%v", calls, w.Header())
			}
		})
	}
}
