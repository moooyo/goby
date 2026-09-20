//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

type adminMediaOperationHTTPProber struct{}

func (adminMediaOperationHTTPProber) CacheVersion() int { return media.CurrentProbeVersion }

func (adminMediaOperationHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (introHTTPProber{}).ProbeFile(ctx, file)
	info.Streams = append(info.Streams, media.Stream{Index: 7, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle", Language: "eng", Filename: "private-source-filename"})
	return info, err
}

// This fixture starts at the OCR executor result boundary. It proves native
// HTTP authorization, review persistence and image delivery, not OCR accuracy.
func adminMediaOperationReadyHTTPFixture(t *testing.T) (*adminMetadataHTTPFixture, identity.Principal, library.MediaOperation) {
	t.Helper()
	f := newAdminMetadataHTTPFixture(t)
	closeFixtureCatalogForReplacement(t, f.serverFixture)
	catalog, err := library.New(f.pool, adminMediaOperationHTTPProber{}, []string{f.root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f.serverFixture, catalog)
	f.handler = f.app.Handler()
	f.rescan(t, adminMetadataAutomaticNFO)
	actor, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	target, err := catalog.GetMediaOperationTarget(f.ctx, actor, f.itemID)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := catalog.StartMediaOperation(f.ctx, actor, library.MediaOperationRequest{
		RequestID: "http-review-fixture", Kind: library.MediaOperationOCR, ItemID: f.itemID,
		MediaSourceID: target.MediaSourceID, SourceRevision: target.SourceRevision, StreamIndex: 7,
		MaxQueued: 4, ExecutionSnapshot: json.RawMessage(`{"Engine":"fixture"}`),
		Parameters: library.MediaOperationParameters{ModelIDs: []string{"eng"}, OutputFormat: "vtt", Language: "eng", Title: "Reviewed captions"},
	})
	if err != nil || !admission.Admitted {
		t.Fatalf("admit HTTP review fixture: %v", err)
	}
	work, err := catalog.ClaimMediaOperation(f.ctx, admission.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded.Bytes())
	confidence := 42.5
	err = catalog.ReadyMediaOperation(f.ctx, work, library.MediaOperationResult{
		Summary: json.RawMessage(`{"CueCount":1,"Warnings":["Review low confidence text."],"Path":"private-summary-path"}`),
		Cues: []library.MediaOperationCue{{StartTicks: 0, EndTicks: 2 * media.TicksPerSecond,
			Text: "Helo", Included: true, Confidence: &confidence, ImagePNG: encoded.Bytes(), ImageSHA256: hex.EncodeToString(sum[:]), Warnings: []string{"Low confidence recognition."}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := catalog.GetMediaOperation(f.ctx, actor, admission.Operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, actor, operation
}

func TestHTTPAdminMediaOperationReviewUsesNativeAuthorityAndExactCAS(t *testing.T) {
	f, actor, operation := adminMediaOperationReadyHTTPFixture(t)
	base := "/admin/v1/media-operations/" + operation.ID
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	for _, path := range []string{"/admin/v1/media-operations", "/admin/v1/media-operations/capabilities", base, base + "/review", base + "/cues/0/image", "/admin/v1/items/" + f.itemID + "/media-processing"} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {f.adminToken}}), http.StatusUnauthorized)
	}
	response := f.request(t, http.MethodGet, base+"/review?StartIndex=0&Limit=1", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	if response.Header().Get("Cache-Control") != "no-store" || page["TotalRecordCount"] != float64(1) || page["Limit"] != float64(1) || strings.Contains(response.Body.String(), "private-summary-path") {
		t.Fatal("review HTTP page lost its bounds or exposed private evidence")
	}
	items, ok := page["Items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatal("review page did not contain its persisted cue")
	}
	cue := items[0].(map[string]any)
	if cue["StartTicks"] != "0" || cue["EndTicks"] != "20000000" || cue["OriginalText"] != "Helo" || cue["Text"] != "Helo" || cue["Confidence"] != float64(42.5) {
		t.Fatal("review page changed original recognition evidence")
	}
	body := map[string]any{"Revision": strconv.FormatInt(operation.Revision, 10), "Edits": []map[string]any{{"Ordinal": 0, "StartTicks": "10000000", "EndTicks": "30000000", "Text": "Hello", "Included": true}}}
	expectStatus(t, f.request(t, http.MethodPut, base+"/review", body, nil, f.cookie), http.StatusForbidden)
	response = f.request(t, http.MethodPut, base+"/review", body, headers, f.cookie)
	expectStatus(t, response, http.StatusOK)
	reviewed := objectValue(t, jsonObject(t, response), "Operation")
	if reviewed["Revision"] == strconv.FormatInt(operation.Revision, 10) || reviewed["ResultHash"] == operation.ResultHash {
		t.Fatal("review correction did not produce a new durable confirmation")
	}
	expectStatus(t, f.request(t, http.MethodPut, base+"/review", body, headers, f.cookie), http.StatusConflict)
	response = f.request(t, http.MethodGet, base+"/review", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	updated := jsonObject(t, response)["Items"].([]any)[0].(map[string]any)
	if updated["OriginalText"] != "Helo" || updated["Text"] != "Hello" || updated["OriginalStartTicks"] != "0" || updated["StartTicks"] != "10000000" {
		t.Fatal("review overwrote original OCR evidence or dropped the corrected timing")
	}
	response = f.request(t, http.MethodGet, base+"/cues/0/image", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Content-Type") != "image/png" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("ETag") == "" {
		t.Fatal("review image lost its protected PNG contract")
	}
	conditional := http.Header{"If-None-Match": {response.Header().Get("ETag")}}
	expectStatus(t, f.request(t, http.MethodGet, base+"/cues/0/image", nil, conditional, f.cookie), http.StatusNotModified)
	expectStatus(t, f.request(t, http.MethodGet, base+"/cues/00/image", nil, nil, f.cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, base+"/cues/0/image?Path=/private", nil, nil, f.cookie), http.StatusBadRequest)
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM sessions WHERE id=$1`, actor.SessionID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"/cues/0/image", nil, conditional, f.cookie), http.StatusUnauthorized)
	// Replaying a principal captured before revocation must also fail inside the
	// store transaction, independently of the outer cookie middleware.
	request := httptest.NewRequest(http.MethodGet, base+"/review", nil)
	request.SetPathValue("id", operation.ID)
	request = request.WithContext(context.WithValue(f.ctx, principalKey, actor))
	recorder := httptest.NewRecorder()
	f.app.adminMediaOperationReview(recorder, request)
	expectStatus(t, recorder, http.StatusUnauthorized)
}

func TestHTTPAdminMediaOperationDisabledInventoryDoesNotInventSupport(t *testing.T) {
	f, _, operation := adminMediaOperationReadyHTTPFixture(t)
	base := "/admin/v1/media-operations/" + operation.ID
	response := f.request(t, http.MethodGet, "/admin/v1/media-operations/capabilities", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	capabilities := jsonObject(t, response)
	ocr := objectValue(t, capabilities, "OCR")
	if capabilities["Enabled"] != false || capabilities["Available"] != false || ocr["Available"] != false || capabilities["UnavailableReason"] != "disabled" {
		t.Fatal("disabled execution inventory advertised an available operation")
	}
	for _, value := range []any{capabilities["WritableProfiles"], ocr["Models"], ocr["OutputFormats"]} {
		items, ok := value.([]any)
		if !ok || len(items) != 0 {
			t.Fatal("disabled execution inventory omitted its explicit empty arrays")
		}
	}
	response = f.request(t, http.MethodGet, "/admin/v1/items/"+f.itemID+"/media-processing", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	target := jsonObject(t, response)
	if target["MediaSourceId"] != operation.MediaSourceID || target["SourceRevision"] != operation.SourceRevision || strings.Contains(response.Body.String(), "private-source-filename") {
		t.Fatal("source selection lost its revision or exposed private stream fields")
	}
	response = f.request(t, http.MethodGet, "/admin/v1/media-operations?ItemId="+f.itemID+"&Kind=subtitle_ocr&State=ready&Limit=1", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	if page["TotalRecordCount"] != float64(1) || len(page["Items"].([]any)) != 1 {
		t.Fatal("operation list did not preserve its explicit source and state filters")
	}
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	start := map[string]any{"RequestId": "disabled-start", "Kind": library.MediaOperationOCR,
		"MediaSourceId": operation.MediaSourceID, "SourceRevision": operation.SourceRevision, "StreamIndex": 7,
		"Parameters": map[string]any{"ModelIds": []string{"eng"}, "OutputFormat": "vtt", "Language": "eng", "Title": "Captions", "IsDefault": false, "IsForced": false, "IsHearingImpaired": false}}
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/items/"+f.itemID+"/media-operations", start, headers, f.cookie), http.StatusServiceUnavailable)
	confirmation := map[string]any{"Revision": strconv.FormatInt(operation.Revision, 10), "SourceRevision": operation.SourceRevision, "ResultHash": operation.ResultHash, "RequestId": "disabled-apply"}
	expectStatus(t, f.request(t, http.MethodPost, base+"/apply", confirmation, headers, f.cookie), http.StatusServiceUnavailable)
	expectStatus(t, f.request(t, http.MethodPost, base+"/recover", confirmation, headers, f.cookie), http.StatusServiceUnavailable)
	// Disabling execution must still let an administrator request cancellation
	// of retained work; a 202 does not assert that cleanup has already finished.
	cancel := map[string]any{"Revision": strconv.FormatInt(operation.Revision, 10)}
	expectStatus(t, f.request(t, http.MethodPost, base+"/cancel", cancel, nil, f.cookie), http.StatusForbidden)
	response = f.request(t, http.MethodPost, base+"/cancel", cancel, headers, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	cancelled := objectValue(t, jsonObject(t, response), "Operation")
	if cancelled["CancelRequestedAt"] == nil || cancelled["CanApply"] != false || cancelled["CanCancel"] != false {
		t.Fatal("cancellation receipt did not prevent a new apply admission")
	}
}
