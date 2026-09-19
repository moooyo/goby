package server

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func avatarAPIPNG(t *testing.T, red uint8) []byte {
	t.Helper()
	frame := image.NewNRGBA(image.Rect(0, 0, 6, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 6; x++ {
			frame.SetNRGBA(x, y, color.NRGBA{R: red, B: uint8(x * 30), A: 255})
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, frame); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func avatarRawRequest(t *testing.T, f *serverFixture, method, target string, data []byte, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader(data)).WithContext(f.ctx)
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func TestHTTPAvatarsNativeCASPreviewDTOAndRemoval(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	endpoint := "/admin/v1/users/" + adminID + "/image"
	initial := f.request(t, http.MethodGet, endpoint, nil, nil, cookie)
	expectStatus(t, initial, http.StatusOK)
	initialObject := jsonObject(t, initial)
	revision := stringValue(t, initialObject, "Revision")
	if len(initialObject["Items"].([]any)) != 0 {
		t.Fatal("missing avatar did not return an empty collection")
	}
	data := avatarAPIPNG(t, 170)
	headers := http.Header{"Content-Type": {"image/png"}, "If-Match": {strconv.Quote(revision)}}
	headers.Set("X-CSRF-Token", csrf)
	missingCSRF := headers.Clone()
	missingCSRF.Del("X-CSRF-Token")
	expectAPIError(t, avatarRawRequest(t, f, http.MethodPut, endpoint, data, missingCSRF, cookie), http.StatusForbidden, "csrf_invalid", false)
	missingRevision := headers.Clone()
	missingRevision.Del("If-Match")
	expectStatus(t, avatarRawRequest(t, f, http.MethodPut, endpoint, data, missingRevision, cookie), http.StatusPreconditionRequired)
	uploaded := avatarRawRequest(t, f, http.MethodPut, endpoint, data, headers, cookie)
	expectStatus(t, uploaded, http.StatusOK)
	collection := jsonObject(t, uploaded)
	images := collection["Items"].([]any)
	if len(images) != 1 {
		t.Fatalf("upload returned no persisted image: %#v", collection)
	}
	image := images[0].(map[string]any)
	tag := stringValue(t, image, "Tag")
	if tag != imageSourceTag(data) || image["Width"] != float64(6) || image["Height"] != float64(3) || image["Size"] != strconv.Itoa(len(data)) || image["Source"] != "managed" {
		t.Fatalf("avatar metadata does not describe its bytes: %#v", image)
	}
	previewURL := stringValue(t, image, "PreviewUrl")
	preview := f.request(t, http.MethodGet, previewURL, nil, nil, cookie)
	expectStatus(t, preview, http.StatusOK)
	if !bytes.Equal(preview.Body.Bytes(), data) || preview.Header().Get("ETag") != strconv.Quote(tag) {
		t.Fatal("avatar preview changed the original bytes/hash")
	}
	conditional := http.Header{"If-None-Match": {preview.Header().Get("ETag")}}
	expectStatus(t, f.request(t, http.MethodGet, previewURL, nil, conditional, cookie), http.StatusNotModified)
	expectStatus(t, f.request(t, http.MethodGet, previewURL, nil, conditional), http.StatusUnauthorized)
	for _, route := range []string{"/admin/v1/session", "/admin/v1/users/" + adminID} {
		response := f.request(t, http.MethodGet, route, nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		user := objectValue(t, jsonObject(t, response), "User")
		if user["PrimaryImageTag"] != tag || user["PrimaryImageAspectRatio"] != float64(2) {
			t.Fatalf("native user projection omitted stored avatar: %#v", user)
		}
	}
	expectStatus(t, avatarRawRequest(t, f, http.MethodPut, endpoint, avatarAPIPNG(t, 30), headers, cookie), http.StatusConflict)
	headers.Set("If-Match", strconv.Quote(stringValue(t, collection, "Revision")))
	wrongMIME := headers.Clone()
	wrongMIME.Set("Content-Type", "image/jpeg")
	expectStatus(t, avatarRawRequest(t, f, http.MethodPut, endpoint, data, wrongMIME, cookie), http.StatusUnsupportedMediaType)
	deleted := f.request(t, http.MethodDelete, endpoint, nil, headers, cookie)
	expectStatus(t, deleted, http.StatusOK)
	if len(jsonObject(t, deleted)["Items"].([]any)) != 0 {
		t.Fatal("deleted avatar remained in native collection")
	}
	expectStatus(t, f.request(t, http.MethodGet, previewURL, nil, conditional, cookie), http.StatusNotFound)
	after := f.request(t, http.MethodGet, "/admin/v1/users/"+adminID, nil, nil, cookie)
	expectStatus(t, after, http.StatusOK)
	if _, exists := objectValue(t, jsonObject(t, after), "User")["PrimaryImageTag"]; exists {
		t.Fatal("deleted avatar tag remained in user DTO")
	}
}

func TestHTTPAvatarsPublicVisibilityAndPrivateAuthorityPrecedeConditionalGET(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/users", map[string]any{"Name": "Avatar viewer", "Password": "viewer-password"}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	id := stringValue(t, objectValue(t, jsonObject(t, created), "User"), "Id")
	data := avatarAPIPNG(t, 220)
	for _, targetID := range []string{id, adminID} {
		endpoint := "/admin/v1/users/" + targetID + "/image"
		initial := f.request(t, http.MethodGet, endpoint, nil, nil, cookie)
		expectStatus(t, initial, http.StatusOK)
		headers := http.Header{"Content-Type": {"image/png"}, "If-Match": {strconv.Quote(stringValue(t, jsonObject(t, initial), "Revision"))}, "X-CSRF-Token": {csrf}}
		expectStatus(t, avatarRawRequest(t, f, http.MethodPut, endpoint, data, headers, cookie), http.StatusOK)
	}
	publicURL := "/emby/Users/" + id + "/Images/Primary/0"
	public := f.request(t, http.MethodGet, publicURL, nil, nil)
	expectStatus(t, public, http.StatusOK)
	if !bytes.Equal(public.Body.Bytes(), data) || public.Header().Get("Cache-Control") != "private, no-cache, no-transform" {
		t.Fatal("public avatar omitted privacy-aware revalidation or actual image bytes")
	}
	conditional := http.Header{"If-None-Match": {public.Header().Get("ETag")}}
	expectStatus(t, f.request(t, http.MethodGet, publicURL, nil, conditional), http.StatusNotModified)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+adminID+"/Images/Primary", nil, nil), http.StatusNotFound)
	users := responseArray(t, f.request(t, http.MethodGet, "/emby/Users/Public", nil, nil))
	if len(users) != 1 || users[0]["Id"] != id || users[0]["PrimaryImageTag"] != imageSourceTag(data) {
		t.Fatalf("public picker avatar projection: %#v", users)
	}
	login := f.embyLogin(t, "Avatar viewer", "viewer-password")
	if objectValue(t, login, "User")["PrimaryImageTag"] != imageSourceTag(data) {
		t.Fatal("login omitted existing avatar")
	}
	selfHeaders := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}, "If-None-Match": {public.Header().Get("ETag")}}
	setHTTPUserPolicy(t, f, id, `{"IsHidden":true}`)
	expectStatus(t, f.request(t, http.MethodGet, publicURL, nil, conditional), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, publicURL, nil, selfHeaders), http.StatusNotModified)
	users = responseArray(t, f.request(t, http.MethodGet, "/emby/Users/Public", nil, nil))
	if len(users) != 0 {
		t.Fatal("hidden user or avatar leaked through login picker")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_disabled=true WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, publicURL, nil, conditional), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, publicURL, nil, selfHeaders), http.StatusUnauthorized)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET is_disabled=false,policy='{}'::jsonb WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	invalid := http.Header{"X-Emby-Token": {"invalid-token"}, "If-None-Match": {public.Header().Get("ETag")}}
	expectStatus(t, f.request(t, http.MethodGet, publicURL, nil, invalid), http.StatusUnauthorized)
}

func TestHTTPAvatarCompatibilitySelfUploadDeleteAndCrossUserDenial(t *testing.T) {
	f := newServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/users", map[string]any{"Name": "Avatar editor", "Password": "viewer-password"}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	id := stringValue(t, objectValue(t, jsonObject(t, created), "User"), "Id")
	login := f.embyLogin(t, "Avatar editor", "viewer-password")
	headers := http.Header{"Content-Type": {"image/png"}, "X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	endpoint := "/users/" + id + "/images/primary/0"
	expectStatus(t, avatarRawRequest(t, f, http.MethodPost, endpoint, avatarAPIPNG(t, 90), headers), http.StatusNoContent)
	expectStatus(t, avatarRawRequest(t, f, http.MethodPost, "/emby/Users/"+adminID+"/Images/Primary", avatarAPIPNG(t, 10), headers), http.StatusForbidden)
	expectStatus(t, avatarRawRequest(t, f, http.MethodPost, "/emby/Users/"+id+"/Images/Backdrop", avatarAPIPNG(t, 10), headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, endpoint+"/delete", nil, headers), http.StatusNoContent)
	expectStatus(t, f.request(t, http.MethodGet, endpoint, nil, headers), http.StatusNotFound)
}
