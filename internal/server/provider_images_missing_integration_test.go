package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/moooyo/goby/internal/providers"
)

func TestHTTPManualMissingImagesReturnScopedArtworkAndKeepExistingSources(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/admin/v1/items/" + fixture.itemID + "/providers/images/missing"
	body := map[string]any{"LibraryId": fixture.libraryID, "ImageType": "Primary"}
	headers := http.Header{"X-CSRF-Token": {fixture.csrf}}
	before := jsonObject(t, f.request(t, http.MethodGet, "/admin/v1/items/"+fixture.itemID+"/images", nil, nil, fixture.cookie))
	response := f.request(t, http.MethodPost, path, body, headers, fixture.cookie)
	expectStatus(t, response, http.StatusOK)
	result := jsonObject(t, response)
	if stringValue(t, result, "Status") != "already_present" || stringValue(t, result, "ItemId") != fixture.itemID ||
		stringValue(t, result, "LibraryId") != fixture.libraryID || stringValue(t, result, "ImageType") != "Primary" ||
		!reflect.DeepEqual(objectValue(t, result, "Artwork"), before) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("manual missing request lost its scoped, authorized artwork result: %+v", result)
	}
	var enabled bool
	if err := f.pool.QueryRow(f.ctx, `SELECT COALESCE(options->'EnableOnlineImages'='true'::jsonb,false) FROM libraries WHERE id=$1`, fixture.libraryID).Scan(&enabled); err != nil || enabled {
		t.Fatalf("manual request opted the library into automatic downloads: enabled=%t error=%v", enabled, err)
	}
	body["ImageType"] = "Logo"
	response = f.request(t, http.MethodPost, path, body, headers, fixture.cookie)
	expectStatus(t, response, http.StatusOK)
	if stringValue(t, jsonObject(t, response), "Status") != "unmatched" {
		t.Fatal("an unmatched item was searched by title or reported as downloaded")
	}
}

func TestHTTPManualMissingImagesRequireNativeCSRFAndExactScope(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/admin/v1/items/" + fixture.itemID + "/providers/images/missing"
	body := map[string]any{"LibraryId": fixture.libraryID, "ImageType": "Primary"}
	headers := http.Header{"X-CSRF-Token": {fixture.csrf}}
	expectStatus(t, f.request(t, http.MethodPost, path, body, nil), http.StatusUnauthorized)
	expectAPIError(t, f.request(t, http.MethodPost, path, body, nil, fixture.cookie), http.StatusForbidden, "csrf_invalid", false)
	expectStatus(t, f.request(t, http.MethodPost, path, body, fixture.headers), http.StatusUnauthorized)
	for _, invalid := range []map[string]any{
		{"LibraryId": fixture.libraryID, "ImageType": "primary"},
		{"LibraryId": fixture.libraryID, "ImageType": "Thumb"},
		{"LibraryId": fixture.libraryID, "ImageType": "Banner"},
		{"LibraryId": fixture.libraryID},
		{"LibraryId": "", "ImageType": "Primary"},
		{"LibraryId": " " + fixture.libraryID, "ImageType": "Primary"},
		{"LibraryId": fixture.libraryID, "ImageType": "Primary", "Force": true},
	} {
		expectStatus(t, f.request(t, http.MethodPost, path, invalid, headers, fixture.cookie), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodPost, path+"?ImageType=Primary", body, headers, fixture.cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodPost, path, map[string]any{"LibraryId": "other-library", "ImageType": "Primary"}, headers, fixture.cookie), http.StatusNotFound)
}

func TestHTTPManualMissingImagesKeepProviderDisableAndAdmissionFailuresVisible(t *testing.T) {
	fixture := newImageAPIFixture(t)
	f := fixture.f
	path := "/admin/v1/items/" + fixture.itemID + "/providers/images/missing"
	body := map[string]any{"LibraryId": fixture.libraryID, "ImageType": "Logo"}
	headers := http.Header{"X-CSRF-Token": {fixture.csrf}}
	actor, err := f.users.Resolve(f.ctx, fixture.cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := f.app.library.GetItemMetadata(f.ctx, actor, fixture.itemID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.library.ApplyOnlineMetadata(f.ctx, actor, fixture.itemID, detail.Revision, providers.Metadata{
		Selection: providers.Selection{Provider: "tmdb", ID: "42", Type: detail.Type},
		Fields:    map[string]json.RawMessage{"ProviderIds": json.RawMessage(`{"Tmdb":"42"}`)},
	}); err != nil {
		t.Fatal(err)
	}
	query, err := f.app.library.OnlineMetadataQuery(f.ctx, actor, fixture.itemID)
	if err != nil || providers.MetadataID("tmdb", query.Type, query.ProviderIDs) != "42" {
		t.Fatalf("disabled-provider fixture has no resolved TMDB match: query=%+v error=%v", query, err)
	}
	if f.app.onlineProviderConfig().Enabled {
		t.Fatal("disabled-provider fixture unexpectedly enabled online providers")
	}
	expectAPIError(t, f.request(t, http.MethodPost, path, body, headers, fixture.cookie), http.StatusConflict, "providers_disabled", false)
	releaseFirst, first := admitArtworkMutation()
	if !first {
		t.Fatal("first artwork admission was unexpectedly occupied")
	}
	defer releaseFirst()
	releaseSecond, second := admitArtworkMutation()
	if !second {
		t.Fatal("second artwork admission was unexpectedly occupied")
	}
	defer releaseSecond()
	response := f.request(t, http.MethodPost, path, body, headers, fixture.cookie)
	expectAPIError(t, response, http.StatusTooManyRequests, "artwork_limit", false)
	if response.Header().Get("Retry-After") != "2" {
		t.Fatal("busy artwork admission did not expose a bounded precommit retry")
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_provider_images WHERE item_id=$1`, fixture.itemID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("a denied manual download published image bytes: count=%d error=%v", count, err)
	}
}
