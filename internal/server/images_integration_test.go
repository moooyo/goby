package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type imageAPIFixture struct {
	f                                *serverFixture
	root, adminID, libraryID, itemID string
	posterPath, backdropPath         string
	poster, backdrop                 []byte
	cookie                           *http.Cookie
	csrf                             string
	headers                          http.Header
}

func newImageAPIFixture(t *testing.T) *imageAPIFixture {
	t.Helper()
	f, root := newLibraryServerFixture(t)
	writeAPIMediaFile(t, root, "movies/Picture.Movie.mp4")
	fixture := &imageAPIFixture{
		f: f, root: root,
		posterPath:   filepath.Join(root, "movies", "Picture.Movie-poster.jpg"),
		backdropPath: filepath.Join(root, "movies", "Picture.Movie-fanart.jpg"),
	}
	fixture.poster = writeAPIJPEG(t, fixture.posterPath, 160, 240, 1)
	fixture.backdrop = writeAPIJPEG(t, fixture.backdropPath, 240, 160, 2)
	fixture.adminID = f.bootstrap(t)
	fixture.cookie, fixture.csrf = f.adminLogin(t)
	fixture.libraryID = createAndScanAPILibrary(t, f, fixture.cookie, fixture.csrf, filepath.Join(root, "movies"), "movies")
	login := f.embyLogin(t, "Administrator", "administrator-password")
	fixture.headers = http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	items, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?ParentId="+fixture.libraryID+"&IncludeItemTypes=Movie", nil, fixture.headers))
	if total != 1 || len(items) != 1 {
		t.Fatalf("image scan did not expose exactly one movie: %#v, total = %d", items, total)
	}
	fixture.itemID = stringValue(t, items[0], "Id")
	if tag := objectValue(t, items[0], "ImageTags")["Primary"]; tag != imageSourceTag(fixture.poster) {
		t.Fatalf("movie Primary tag = %v, want the actual image SHA-256", tag)
	}
	backdrops, ok := items[0]["BackdropImageTags"].([]any)
	if !ok || len(backdrops) != 1 || backdrops[0] != imageSourceTag(fixture.backdrop) {
		t.Fatalf("movie Backdrop tags do not describe the indexed image: %#v", items[0])
	}
	return fixture
}

// Patterned pixels make changes in JPEG quality observable without depending
// on a particular encoder's exact byte output or compression ratio.
func writeAPIJPEG(t *testing.T, path string, width, height, seed int) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			picture.SetRGBA(x, y, color.RGBA{
				R: uint8((x*17 + y*11 + seed*31) % 256),
				G: uint8((x*3 + y*19 + seed*47) % 256),
				B: uint8((x*23 + y*7 + seed*61) % 256), A: 255,
			})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, picture, &jpeg.Options{Quality: 91}); err != nil {
		t.Fatalf("encode image fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create image fixture directory: %v", err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatalf("write image fixture: %v", err)
	}
	return encoded.Bytes()
}

func imageSourceTag(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func assertAPIImage(t *testing.T, response *httptest.ResponseRecorder, width, height int, format string) {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	mime := "image/" + format
	if got := response.Header().Get("Content-Type"); got != mime {
		t.Errorf("image Content-Type = %q, want %q", got, mime)
	}
	if got := response.Header().Get("Content-Length"); got != strconv.Itoa(response.Body.Len()) {
		t.Errorf("image Content-Length = %q, want %d", got, response.Body.Len())
	}
	picture, decodedFormat, err := image.Decode(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("image response cannot be decoded: %v", err)
	}
	if decodedFormat != format || picture.Bounds().Dx() != width || picture.Bounds().Dy() != height {
		t.Errorf("decoded image = %s %dx%d, want %s %dx%d", decodedFormat, picture.Bounds().Dx(), picture.Bounds().Dy(), format, width, height)
	}
	if tag := response.Header().Get("ETag"); len(tag) != 66 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		t.Errorf("image ETag must be a quoted SHA-256 validator: %q", tag)
	} else if _, err := hex.DecodeString(tag[1 : len(tag)-1]); err != nil {
		t.Errorf("image ETag contains a nonhexadecimal digest: %q", tag)
	}
}

func assertImageNotModified(t *testing.T, response *httptest.ResponseRecorder, etag string) {
	t.Helper()
	expectStatus(t, response, http.StatusNotModified)
	if response.Body.Len() != 0 || response.Header().Get("Content-Length") != "" {
		t.Errorf("304 must omit its body and Content-Length: headers = %v, body length = %d", response.Header(), response.Body.Len())
	}
	if response.Header().Get("ETag") != etag {
		t.Errorf("304 ETag = %q, want %q", response.Header().Get("ETag"), etag)
	}
	if response.Header().Get("Cache-Control") != "" {
		t.Errorf("reference-compatible 304 must omit Cache-Control: %v", response.Header())
	}
}

func TestHTTPImagesPublicBodiesProjectionTransformsAndCacheValidators(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/emby/Items/" + fixture.itemID + "/Images/Primary"
	tag := imageSourceTag(fixture.poster)
	etag := `"` + tag + `"`

	// Public binary artwork is an intentional reference contract. Its metadata
	// enumeration has a separate authenticated and user-scoped route below.
	for name, headers := range map[string]http.Header{
		"anonymous":   nil,
		"bad token":   {"X-Emby-Token": {strings.Repeat("A", 43)}},
		"valid token": fixture.headers,
	} {
		t.Run(name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, path, nil, headers)
			assertAPIImage(t, response, 160, 240, "jpeg")
			if !bytes.Equal(response.Body.Bytes(), fixture.poster) || response.Header().Get("ETag") != etag {
				t.Errorf("original image bytes or source ETag changed")
			}
			if response.Header().Get("Cache-Control") != "public" {
				t.Errorf("untagged public image cache policy = %q", response.Header().Get("Cache-Control"))
			}
		})
	}
	for _, suffix := range []string{"/0", "?Index=0", "?api_key=invalid-token", "?Format=original", "?Quality=0"} {
		response := f.request(t, http.MethodGet, path+suffix, nil, nil)
		assertAPIImage(t, response, 160, 240, "jpeg")
		if !bytes.Equal(response.Body.Bytes(), fixture.poster) {
			t.Errorf("original image changed for %s", suffix)
		}
	}
	head := f.request(t, http.MethodHead, path, nil, nil)
	expectStatus(t, head, http.StatusOK)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(fixture.poster)) ||
		head.Header().Get("Content-Type") != "image/jpeg" || head.Header().Get("ETag") != etag {
		t.Errorf("HEAD does not match original GET metadata without a body: headers = %v, body length = %d", head.Header(), head.Body.Len())
	}
	items, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?Ids="+fixture.itemID+"&EnableImages=false", nil, fixture.headers))
	if total != 1 || len(items) != 1 {
		t.Fatalf("disabling image projection changed item membership: %#v", items)
	}
	for _, field := range []string{"ImageTags", "BackdropImageTags"} {
		if _, exists := items[0][field]; exists {
			t.Errorf("EnableImages=false still returned %s", field)
		}
	}

	var widthETag, maxWidthETag string
	for _, variant := range []struct {
		name, query, format string
		width, height       int
	}{
		{"width", "Width=64", "jpeg", 64, 96},
		{"maximum width", "MaxWidth=64", "jpeg", 64, 96},
		{"height", "Height=96", "jpeg", 64, 96},
		{"maximum height", "MaxHeight=96", "jpeg", 64, 96},
		{"combined limits", "Width=120&MaxWidth=64&Height=240&MaxHeight=48", "jpeg", 32, 48},
		{"no enlargement", "Width=4096&Height=4096", "jpeg", 160, 240},
		{"PNG", "Width=64&Format=png", "png", 64, 96},
		{"GIF", "Width=64&Format=gif", "gif", 64, 96},
		{"JPEG alias", "Width=64&Format=jpg", "jpeg", 64, 96},
		{"JPEG name", "Width=64&Format=jpeg", "jpeg", 64, 96},
		{"case-insensitive query names", "wIdTh=64&fOrMaT=png&qUaLiTy=50", "png", 64, 96},
		{"matching duplicate values", "Width=64&width=64", "jpeg", 64, 96},
	} {
		t.Run(variant.name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, path+"?"+variant.query, nil, nil)
			assertAPIImage(t, response, variant.width, variant.height, variant.format)
			validator := response.Header().Get("ETag")
			if validator == etag {
				t.Error("explicit image variant reused the untransformed ETag")
			}
			if variant.name == "width" {
				widthETag = validator
			}
			if variant.name == "maximum width" {
				maxWidthETag = validator
			}
			repeated := f.request(t, http.MethodGet, path+"?"+variant.query, nil, nil)
			if repeated.Header().Get("ETag") != validator || !bytes.Equal(repeated.Body.Bytes(), response.Body.Bytes()) {
				t.Error("repeated image request changed its representation or ETag")
			}
		})
	}
	if widthETag == "" || widthETag == maxWidthETag {
		t.Error("different transformation parameters must have separate validators")
	}
	low := f.request(t, http.MethodGet, path+"?Width=64&Quality=20", nil, nil)
	high := f.request(t, http.MethodGet, path+"?Width=64&Quality=95", nil, nil)
	assertAPIImage(t, low, 64, 96, "jpeg")
	assertAPIImage(t, high, 64, 96, "jpeg")
	if bytes.Equal(low.Body.Bytes(), high.Body.Bytes()) || low.Header().Get("ETag") == high.Header().Get("ETag") {
		t.Error("JPEG quality change did not affect representation and validator")
	}
	pngGET := f.request(t, http.MethodGet, path+"?Width=64&Format=png", nil, nil)
	pngHEAD := f.request(t, http.MethodHead, path+"?Width=64&Format=png", nil, nil)
	expectStatus(t, pngHEAD, http.StatusOK)
	if pngHEAD.Body.Len() != 0 || pngHEAD.Header().Get("Content-Length") != strconv.Itoa(pngGET.Body.Len()) ||
		pngHEAD.Header().Get("Content-Type") != "image/png" || pngHEAD.Header().Get("ETag") != pngGET.Header().Get("ETag") {
		t.Errorf("transformed HEAD disagrees with GET: headers = %v, body length = %d", pngHEAD.Header(), pngHEAD.Body.Len())
	}

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, validator := range []string{etag, "W/" + etag, `"other", W/` + etag, "*"} {
			response := f.request(t, method, path, nil, http.Header{"If-None-Match": {validator}})
			assertImageNotModified(t, response, etag)
		}
		response := f.request(t, method, path+"?Width=64", nil, http.Header{"If-None-Match": {widthETag}})
		assertImageNotModified(t, response, widthETag)
	}
	wrongVariant := f.request(t, http.MethodGet, path+"?Width=64", nil, http.Header{"If-None-Match": {etag}})
	assertAPIImage(t, wrongVariant, 64, 96, "jpeg")
	for _, query := range []string{"Tag=" + tag, "tAg=" + tag + "&Format=png&Width=64"} {
		response := f.request(t, http.MethodGet, path+"?"+query, nil, nil)
		expectStatus(t, response, http.StatusOK)
		if response.Header().Get("Cache-Control") != "public, max-age=31536000" {
			t.Errorf("matching Tag did not enable long-lived caching: %v", response.Header())
		}
		if _, err := http.ParseTime(response.Header().Get("Expires")); err != nil {
			t.Errorf("tagged image Expires is missing or invalid: %v", err)
		}
	}
	wrongTag := f.request(t, http.MethodGet, path+"?Tag="+strings.Repeat("0", 64), nil, nil)
	assertAPIImage(t, wrongTag, 160, 240, "jpeg")
	if wrongTag.Header().Get("Cache-Control") != "public" {
		t.Errorf("nonmatching Tag incorrectly enabled immutable caching: %v", wrongTag.Header())
	}

	for _, query := range []string{
		"Width=-1", "Width=4097", "Width=abc", "Width=1.5", "Height=4097", "MaxWidth=4097", "MaxHeight=-1",
		"Quality=-1", "Quality=101", "Quality=NaN", "Format=webp", "Width=64&width=65", "Tag=one&tag=two",
		"CropWhitespace=true", "AutoOrient=true", "KeepAnimation=true", "AddPlayedIndicator=true",
		"BackgroundColor=red", "ForegroundLayer=one", "PercentPlayed=1", "UnplayedCount=1", "PercentPlayed=NaN",
		"Index=-1", "Index=32", "Index=invalid",
	} {
		t.Run("reject "+query, func(t *testing.T) {
			expectAPIError(t, f.request(t, http.MethodGet, path+"?"+query, nil, nil), http.StatusBadRequest, "invalid_image_request", true)
		})
	}
	for _, index := range []string{"-1", "32", "invalid"} {
		expectAPIError(t, f.request(t, http.MethodGet, path+"/"+index, nil, nil), http.StatusBadRequest, "invalid_image_request", true)
	}
	expectAPIError(t, f.request(t, http.MethodGet, path+"/0?Index=1", nil, nil), http.StatusBadRequest, "invalid_image_request", true)
	for _, missing := range []string{
		"/emby/Items/" + fixture.itemID + "/Images/Primary/1",
		"/emby/Items/" + fixture.itemID + "/Images/Backdrop/31",
		"/emby/Items/" + fixture.itemID + "/Images/Logo",
		"/emby/Items/missing-image-item/Images/Primary",
	} {
		expectAPIError(t, f.request(t, http.MethodGet, missing, nil, nil), http.StatusNotFound, "not_found", true)
	}
}

func TestHTTPImageListsEnforceUserLibraryACLWhileBinaryArtworkRemainsPublic(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	writeAPIMediaFile(t, fixture.root, "other/Other.Movie.mp4")
	otherLibraryID := createAndScanAPILibrary(t, f, fixture.cookie, fixture.csrf, filepath.Join(fixture.root, "other"), "movies")
	otherItems, total := responseItems(t, f.request(t, http.MethodGet,
		"/emby/Items?ParentId="+otherLibraryID+"&IncludeItemTypes=Movie", nil, fixture.headers))
	if total != 1 || len(otherItems) != 1 {
		t.Fatalf("secondary fixture library is incomplete: %#v", otherItems)
	}
	otherID := stringValue(t, otherItems[0], "Id")
	type viewer struct {
		id      string
		headers http.Header
	}
	viewers := make([]viewer, 2)
	for index, libraryID := range []string{fixture.libraryID, otherLibraryID} {
		user, err := f.users.CreateUser(f.ctx, "Image Viewer "+strconv.Itoa(index), "viewer-password", false)
		if err != nil {
			t.Fatalf("create image viewer: %v", err)
		}
		policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{libraryID}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", policy, user.ID); err != nil {
			t.Fatalf("limit image viewer to its library: %v", err)
		}
		login := f.embyLogin(t, user.Name, "viewer-password")
		viewers[index] = viewer{id: user.ID, headers: http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}}
	}
	path := "/emby/Items/" + fixture.itemID + "/Images"
	for _, headers := range []http.Header{nil, {"X-Emby-Token": {strings.Repeat("A", 43)}}} {
		expectEmbyTextError(t, f.request(t, http.MethodGet, path, nil, headers), http.StatusUnauthorized, "Access token is invalid or expired.")
	}
	images := responseArray(t, f.request(t, http.MethodGet, path, nil, viewers[0].headers))
	if len(images) != 2 {
		t.Fatalf("authorized image list = %#v, want Primary and Backdrop", images)
	}
	byType := make(map[string]map[string]any, len(images))
	for _, entry := range images {
		byType[stringValue(t, entry, "ImageType")] = entry
	}
	for _, expected := range []struct {
		kind, path    string
		width, height int
		data          []byte
	}{
		{"Primary", fixture.posterPath, 160, 240, fixture.poster},
		{"Backdrop", fixture.backdropPath, 240, 160, fixture.backdrop},
	} {
		entry, exists := byType[expected.kind]
		if !exists {
			t.Fatalf("image list omitted %s", expected.kind)
		}
		if entry["Path"] != expected.path || entry["Filename"] != filepath.Base(expected.path) ||
			entry["Width"] != float64(expected.width) || entry["Height"] != float64(expected.height) || entry["Size"] != float64(len(expected.data)) {
			t.Errorf("%s image metadata does not describe its actual file: %#v", expected.kind, entry)
		}
		index, exists := entry["ImageIndex"]
		if expected.kind == "Primary" && exists || expected.kind == "Backdrop" && (!exists || index != float64(0)) {
			t.Errorf("%s ImageIndex presence/value is incorrect: %#v", expected.kind, entry)
		}
	}
	otherPath := "/emby/Items/" + otherID + "/Images"
	for _, request := range []struct {
		path    string
		headers http.Header
	}{
		{path, viewers[1].headers},
		{otherPath, viewers[0].headers},
		{path + "?UserId=" + viewers[1].id, fixture.headers},
	} {
		response := f.request(t, http.MethodGet, request.path, nil, request.headers)
		expectAPIError(t, response, http.StatusNotFound, "not_found", true)
		if strings.Contains(response.Body.String(), fixture.root) {
			t.Error("unauthorized image enumeration exposed a filesystem path")
		}
	}
	empty := responseArray(t, f.request(t, http.MethodGet, otherPath, nil, viewers[1].headers))
	if len(empty) != 0 {
		t.Errorf("authorized item without artwork did not return an empty array: %#v", empty)
	}
	expectAPIError(t, f.request(t, http.MethodGet, path+"?UserId="+fixture.adminID, nil, viewers[1].headers), http.StatusForbidden, "access_denied", true)
	// The same restricted viewer may read a known public image URL, but cannot
	// enumerate its source metadata. Keep this compatibility decision explicit.
	assertAPIImage(t, f.request(t, http.MethodGet, path+"/Primary", nil, viewers[1].headers), 160, 240, "jpeg")
	assertAPIImage(t, f.request(t, http.MethodGet, path+"/Backdrop/0", nil, nil), 240, 160, "jpeg")
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		expectAPIError(t, f.request(t, method, path+"/Primary", nil, viewers[0].headers), http.StatusNotFound, "not_implemented", true)
	}
	if data, err := os.ReadFile(fixture.posterPath); err != nil || !bytes.Equal(data, fixture.poster) {
		t.Errorf("unsupported image mutation changed its source: %v", err)
	}
}

func TestHTTPImageCacheRejectsChangedSourcesAndDeletedLibraries(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/emby/Items/" + fixture.itemID + "/Images/Primary"
	oldTag := imageSourceTag(fixture.poster)
	oldOriginal := f.request(t, http.MethodGet, path+"?Tag="+oldTag, nil, nil)
	oldVariant := f.request(t, http.MethodGet, path+"?Width=64&Format=png&Tag="+oldTag, nil, nil)
	assertAPIImage(t, oldOriginal, 160, 240, "jpeg")
	assertAPIImage(t, oldVariant, 64, 96, "png")
	stat, err := os.Stat(fixture.posterPath)
	if err != nil {
		t.Fatal(err)
	}
	// Modify the same inode without changing length or mtime. A metadata-only
	// cache check would incorrectly accept it; the source hash must be rechecked.
	changed := bytes.Clone(fixture.poster)
	changed[len(changed)/2] ^= 1
	if err := os.WriteFile(fixture.posterPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fixture.posterPath, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "?Width=64&Format=png"} {
		response := f.request(t, http.MethodGet, path+suffix, nil, nil)
		expectStatus(t, response, http.StatusServiceUnavailable)
		if response.Header().Get("ETag") != "" {
			t.Error("unconditional cache request returned a stale image validator")
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, suffix := range []string{"", "?Tag=" + oldTag, "?Width=64&Format=png&Tag=" + oldTag} {
			response := f.request(t, method, path+suffix, nil, http.Header{"If-None-Match": {"*"}})
			expectStatus(t, response, http.StatusServiceUnavailable)
			if response.Header().Get("ETag") != "" || bytes.Equal(response.Body.Bytes(), oldOriginal.Body.Bytes()) || bytes.Equal(response.Body.Bytes(), oldVariant.Body.Bytes()) {
				t.Errorf("stale source reached an image cache response: status = %d, headers = %v", response.Code, response.Header())
			}
		}
	}
	// Replace the path with a fresh valid image. It remains unavailable until a
	// completed rescan updates both the inode identity and source hash.
	replacementPath := filepath.Join(filepath.Dir(fixture.posterPath), "replacement.jpg")
	replacement := writeAPIJPEG(t, replacementPath, 160, 240, 9)
	if err := os.Rename(replacementPath, fixture.posterPath); err != nil {
		t.Fatalf("replace image fixture inode: %v", err)
	}
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusServiceUnavailable)
	indexed, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?Ids="+fixture.itemID, nil, fixture.headers))
	if total != 1 || len(indexed) != 1 || objectValue(t, indexed[0], "ImageTags")["Primary"] != oldTag {
		t.Fatalf("image replacement changed the indexed tag without a completed scan: %#v", indexed)
	}
	rescanAPIImageLibrary(t, fixture)
	newTag := imageSourceTag(replacement)
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?Ids="+fixture.itemID, nil, fixture.headers))
	if total != 1 || len(items) != 1 || objectValue(t, items[0], "ImageTags")["Primary"] != newTag || newTag == oldTag {
		t.Fatalf("rescan did not publish the replacement image tag: %#v", items)
	}
	updated := f.request(t, http.MethodGet, path+"?Tag="+newTag, nil, http.Header{"If-None-Match": {oldOriginal.Header().Get("ETag")}})
	assertAPIImage(t, updated, 160, 240, "jpeg")
	if !bytes.Equal(updated.Body.Bytes(), replacement) || updated.Header().Get("ETag") != `"`+newTag+`"` {
		t.Error("replacement image bytes or source validator are stale")
	}
	newVariant := f.request(t, http.MethodGet, path+"?Width=64&Format=png", nil, http.Header{"If-None-Match": {oldVariant.Header().Get("ETag")}})
	assertAPIImage(t, newVariant, 64, 96, "png")
	if bytes.Equal(newVariant.Body.Bytes(), oldVariant.Body.Bytes()) || newVariant.Header().Get("ETag") == oldVariant.Header().Get("ETag") {
		t.Error("replacement image reused its predecessor's transformed cache entry")
	}
	staleTag := f.request(t, http.MethodGet, path+"?Tag="+oldTag, nil, nil)
	assertAPIImage(t, staleTag, 160, 240, "jpeg")
	if !bytes.Equal(staleTag.Body.Bytes(), replacement) || staleTag.Header().Get("Cache-Control") != "public" {
		t.Error("old tag URL returned old data or retained a long-lived cache policy")
	}
	backdropURL := "/emby/Items/" + fixture.itemID + "/Images/Backdrop/0"
	assertAPIImage(t, f.request(t, http.MethodGet, backdropURL, nil, nil), 240, 160, "jpeg")
	if err := os.Remove(fixture.backdropPath); err != nil {
		t.Fatalf("remove indexed backdrop fixture: %v", err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		expectStatus(t, f.request(t, method, backdropURL, nil, http.Header{"If-None-Match": {"*"}}), http.StatusServiceUnavailable)
	}
	rescanAPIImageLibrary(t, fixture)
	expectAPIError(t, f.request(t, http.MethodGet, backdropURL, nil, nil), http.StatusNotFound, "not_found", true)
	remainingImages := responseArray(t, f.request(t, http.MethodGet, "/emby/Items/"+fixture.itemID+"/Images", nil, fixture.headers))
	if len(remainingImages) != 1 || remainingImages[0]["ImageType"] != "Primary" {
		t.Errorf("rescan retained deleted backdrop metadata: %#v", remainingImages)
	}
	deleted := f.request(t, http.MethodDelete, "/admin/v1/libraries/"+fixture.libraryID, nil,
		http.Header{"X-CSRF-Token": {fixture.csrf}}, fixture.cookie)
	expectStatus(t, deleted, http.StatusNoContent)
	for _, suffix := range []string{"", "?Width=64&Format=png&Tag=" + newTag} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			expectStatus(t, f.request(t, method, path+suffix, nil, http.Header{"If-None-Match": {"*"}}), http.StatusNotFound)
		}
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Items/"+fixture.itemID+"/Images", nil, fixture.headers), http.StatusNotFound, "not_found", true)
	if data, err := os.ReadFile(fixture.posterPath); err != nil || !bytes.Equal(data, replacement) {
		t.Errorf("library deletion modified original artwork: %v", err)
	}
}

func rescanAPIImageLibrary(t *testing.T, fixture *imageAPIFixture) {
	t.Helper()
	f := fixture.f
	response := f.request(t, http.MethodPost, "/admin/v1/libraries/"+fixture.libraryID+"/scan", nil,
		http.Header{"X-CSRF-Token": {fixture.csrf}}, fixture.cookie)
	expectStatus(t, response, http.StatusAccepted)
	jobID := stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		jobs, _ := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, fixture.cookie))
		for _, job := range jobs {
			if job["Id"] != jobID {
				continue
			}
			switch job["Status"] {
			case "completed":
				if job["Error"] != "" {
					t.Fatalf("image rescan completed with an error: %#v", job)
				}
				return
			case "failed", "cancelled", "interrupted":
				t.Fatalf("image rescan did not complete: %#v", job)
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for image rescan")
		case <-f.ctx.Done():
			t.Fatalf("image fixture context ended: %v", f.ctx.Err())
		case <-ticker.C:
		}
	}
}
