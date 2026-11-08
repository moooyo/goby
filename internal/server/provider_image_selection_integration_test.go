package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func providerSelectionTestHandler(fixture *imageAPIFixture, download func(context.Context, providers.RemoteImage) ([]byte, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/v1/items/{id}/providers/image", fixture.f.app.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		fixture.f.app.serveProviderImageSelection(w, r, download)
	}))
	return fixture.f.app.middleware(mux)
}

func providerSelectionTestRequest(t *testing.T, fixture *imageAPIFixture, handler http.Handler, input map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/items/"+fixture.itemID+"/providers/image", bytes.NewReader(body)).WithContext(fixture.f.ctx)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", fixture.csrf)
	request.AddCookie(fixture.cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func providerSelectionTestImage(t *testing.T, collection map[string]any, imageType string, index int) map[string]any {
	t.Helper()
	items, ok := collection["Items"].([]any)
	if !ok {
		t.Fatalf("artwork omitted its native image array: %+v", collection)
	}
	for _, value := range items {
		image, ok := value.(map[string]any)
		if ok && image["ImageType"] == imageType && image["ImageIndex"] == float64(index) {
			return image
		}
	}
	t.Fatalf("artwork omitted %s/%d: %+v", imageType, index, collection)
	return nil
}

func TestHTTPProviderImageSelectionReturnsEffectiveArtworkAndRetainsOtherImages(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	base := "/admin/v1/items/" + fixture.itemID
	metadata := jsonObject(t, f.request(t, http.MethodGet, base+"/metadata", nil, nil, fixture.cookie))
	initialRevision := artworkHTTPRevision(t, f.request(t, http.MethodGet, base+"/images", nil, nil, fixture.cookie))
	second := writeAPIJPEG(t, filepath.Join(t.TempDir(), "second.jpg"), 44, 26, 7)
	before := jsonObject(t, artworkRawRequest(t, f, http.MethodPut, base+"/images/Backdrop/1", second,
		http.Header{"X-CSRF-Token": {fixture.csrf}, "If-Match": {strconv.Quote(initialRevision)}, "Content-Type": {"image/jpeg"}}, fixture.cookie))
	replacement := writeAPIJPEG(t, filepath.Join(t.TempDir(), "replacement.jpg"), 32, 18, 8)
	downloads := 0
	handler := providerSelectionTestHandler(fixture, func(_ context.Context, selected providers.RemoteImage) ([]byte, error) {
		downloads++
		if selected.Provider != "tmdb" || selected.ID != "42" || selected.ImageID != "/Fixture.jpg" || selected.ImageType != "Backdrop" || selected.Type != "Movie" {
			t.Fatalf("image selection changed identity: %+v", selected)
		}
		return bytes.Clone(replacement), nil
	})
	input := map[string]any{"Provider": "tmdb", "Id": "42", "ImageId": "/Fixture.jpg", "ImageType": "Backdrop", "ImageIndex": 0,
		"Revision": metadata["Revision"], "ArtworkRevision": before["Revision"]}
	response := providerSelectionTestRequest(t, fixture, handler, input)
	expectStatus(t, response, http.StatusOK)
	result := jsonObject(t, response)
	after := objectValue(t, result, "Artwork")
	if result["Provider"] != "tmdb" || result["ProviderId"] != "42" || result["ImageId"] != "/Fixture.jpg" || result["ImageType"] != "Backdrop" || result["ImageIndex"] != float64(0) || result["Tag"] != imageSourceTag(replacement) || result["Width"] != float64(32) || result["Height"] != float64(18) || after["Revision"] == before["Revision"] {
		t.Fatalf("provider selection did not acknowledge the committed image: %+v", result)
	}
	if image := providerSelectionTestImage(t, after, "Backdrop", 0); image["Tag"] != result["Tag"] || image["Source"] != "managed" {
		t.Fatalf("candidate success did not identify effective managed artwork: %+v", image)
	}
	for _, slot := range []struct {
		imageType string
		index     int
	}{
		{"Primary", 0}, {"Backdrop", 1},
	} {
		if providerSelectionTestImage(t, before, slot.imageType, slot.index)["Tag"] != providerSelectionTestImage(t, after, slot.imageType, slot.index)["Tag"] {
			t.Fatalf("candidate selection changed %s/%d", slot.imageType, slot.index)
		}
	}
	actual := jsonObject(t, f.request(t, http.MethodGet, base+"/images", nil, nil, fixture.cookie))
	if !reflect.DeepEqual(after, actual) {
		t.Fatalf("acknowledged artwork differs from readback: acknowledged=%+v actual=%+v", after, actual)
	}
	preview := stringValue(t, providerSelectionTestImage(t, after, "Backdrop", 0), "PreviewUrl")
	assertAPIImage(t, f.request(t, http.MethodGet, preview, nil, nil, fixture.cookie), 32, 18, "jpeg")
	var providerRows int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_provider_images WHERE item_id=$1`, fixture.itemID).Scan(&providerRows); err != nil || providerRows != 0 {
		t.Fatalf("managed selection changed its underlying provider cache: rows=%d error=%v", providerRows, err)
	}
	expectAPIError(t, providerSelectionTestRequest(t, fixture, handler, input), http.StatusConflict, "artwork_conflict", false)
	if _, err := f.pool.Exec(f.ctx, `UPDATE item_metadata_state SET revision=revision+1 WHERE item_id=$1`, fixture.itemID); err != nil {
		t.Fatal(err)
	}
	input["ArtworkRevision"] = after["Revision"]
	downloadsBeforeConflict := downloads
	expectAPIError(t, providerSelectionTestRequest(t, fixture, handler, input), http.StatusConflict, "artwork_conflict", false)
	if downloads != downloadsBeforeConflict {
		t.Fatal("known metadata conflict downloaded a candidate")
	}
	metadata = jsonObject(t, f.request(t, http.MethodGet, base+"/metadata", nil, nil, fixture.cookie))
	input["Revision"] = metadata["Revision"]
	delete(input, "ArtworkRevision")
	legacy := providerSelectionTestRequest(t, fixture, handler, input)
	expectStatus(t, legacy, http.StatusOK)
	if _, exists := jsonObject(t, legacy)["Artwork"]; exists {
		t.Fatal("omitted ArtworkRevision changed the legacy response shape")
	}
	legacyReadback := jsonObject(t, f.request(t, http.MethodGet, base+"/images", nil, nil, fixture.cookie))
	if !reflect.DeepEqual(actual, legacyReadback) {
		t.Fatal("legacy provider cache selection overwrote managed artwork")
	}
}

func TestHTTPProviderImageSelectionRejectsInvalidArtworkRevisionBeforeDownload(t *testing.T) {
	fixture := newImageAPIFixture(t)
	downloads := 0
	handler := providerSelectionTestHandler(fixture, func(context.Context, providers.RemoteImage) ([]byte, error) {
		downloads++
		return bytes.Clone(fixture.poster), nil
	})
	for _, revision := range []any{nil, "", "01", 1, true, []string{"1"}} {
		response := providerSelectionTestRequest(t, fixture, handler, map[string]any{"Provider": "tmdb", "Id": "42", "ImageId": "/Fixture.jpg",
			"ImageType": "Primary", "ImageIndex": 0, "Revision": "1", "ArtworkRevision": revision})
		expectAPIError(t, response, http.StatusBadRequest, "invalid_artwork_request", false)
	}
	if downloads != 0 {
		t.Fatalf("invalid artwork preconditions reached the provider: downloads=%d", downloads)
	}
}

func TestHTTPProviderImageSelectionRejectsBusyManagedAdmissionBeforeDownload(t *testing.T) {
	fixture := newImageAPIFixture(t)
	metadata := jsonObject(t, fixture.f.request(t, http.MethodGet, "/admin/v1/items/"+fixture.itemID+"/metadata", nil, nil, fixture.cookie))
	revision := artworkHTTPRevision(t, fixture.f.request(t, http.MethodGet, "/admin/v1/items/"+fixture.itemID+"/images", nil, nil, fixture.cookie))
	for range cap(artworkRequestSlots) {
		release, admitted := admitArtworkMutation()
		if !admitted {
			t.Fatal("could not reserve managed request-admission pressure")
		}
		t.Cleanup(release)
	}
	downloads := 0
	handler := providerSelectionTestHandler(fixture, func(context.Context, providers.RemoteImage) ([]byte, error) {
		downloads++
		return bytes.Clone(fixture.poster), nil
	})
	response := providerSelectionTestRequest(t, fixture, handler, map[string]any{"Provider": "tmdb", "Id": "42", "ImageId": "/Fixture.jpg",
		"ImageType": "Primary", "ImageIndex": 0, "Revision": metadata["Revision"], "ArtworkRevision": revision})
	expectAPIError(t, response, http.StatusTooManyRequests, "artwork_limit", false)
	if downloads != 0 || response.Header().Get("Retry-After") != "2" {
		t.Fatalf("busy admission reached the provider or omitted retry guidance: downloads=%d headers=%+v", downloads, response.Header())
	}
}
