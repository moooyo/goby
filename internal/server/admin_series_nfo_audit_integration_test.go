//go:build linux

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHTTPAdminSeriesNFOAuditRequiresNativeAuthorityAndEmptyRequest(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	headers := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	embyToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	embyHeaders := http.Header{"X-Emby-Token": {embyToken}}

	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Unscanned Series", "CollectionType": "tvshows", "Paths": []string{root}, "Scan": false,
	}, headers, cookie)
	expectStatus(t, created, http.StatusCreated)
	id := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")
	const prefix, suffix = "/admin/v1/libraries/", "/series-nfo-audit"
	target := prefix + id + suffix
	audit := func(t *testing.T, target string, body any) *httptest.ResponseRecorder {
		t.Helper()
		return f.request(t, http.MethodPost, target, body, headers, cookie)
	}

	t.Run("authorization", func(t *testing.T) {
		expectAPIError(t, f.request(t, http.MethodPost, target, nil, nil), http.StatusUnauthorized, "authentication_required", false)
		expectAPIError(t, f.request(t, http.MethodPost, target, nil, embyHeaders), http.StatusUnauthorized, "authentication_required", false)
		embyCookie := &http.Cookie{Name: sessionCookie, Value: embyToken}
		expectAPIError(t, f.request(t, http.MethodPost, target, nil, http.Header{"X-CSRF-Token": {csrfToken(embyToken)}}, embyCookie), http.StatusUnauthorized, "invalid_credentials", false)
		expectAPIError(t, f.request(t, http.MethodPost, target, nil, nil, cookie), http.StatusForbidden, "csrf_invalid", false)
		crossSite := http.Header{"Origin": {"https://other.example.test"}, "X-CSRF-Token": {csrf}}
		expectAPIError(t, f.request(t, http.MethodPost, target, nil, crossSite, cookie), http.StatusForbidden, "origin_denied", false)
		expectAPIError(t, f.request(t, http.MethodPost, "/emby/Library/VirtualFolders/"+id+suffix, nil, embyHeaders), http.StatusNotFound, "not_implemented", true)
		expectAPIError(t, f.request(t, http.MethodGet, target, nil, headers, cookie), http.StatusNotFound, "not_found", false)
	})

	t.Run("input", func(t *testing.T) {
		expectAPIError(t, audit(t, prefix+"missing-library"+suffix, nil), http.StatusNotFound, "not_found", false)
		expectAPIError(t, audit(t, prefix+"series-nfo-audit", nil), http.StatusNotFound, "not_found", false)
		for _, badID := range []string{"bad%00id", "%20leading-space", "%ff", strings.Repeat("x", 257)} {
			expectAPIError(t, audit(t, prefix+badID+suffix, nil), http.StatusBadRequest, "invalid_input", false)
		}
		for _, query := range []string{"?", "?Unexpected=value"} {
			expectAPIError(t, audit(t, target+query, nil), http.StatusBadRequest, "invalid_input", false)
		}
		expectAPIError(t, audit(t, target, map[string]any{}), http.StatusBadRequest, "invalid_input", false)

		// A streaming POST with no known Content-Length is not an empty request.
		request := httptest.NewRequest(http.MethodPost, target, io.LimitReader(strings.NewReader("{}"), 2)).WithContext(f.ctx)
		request.Header.Set("Origin", f.cfg.PublicURL)
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		expectAPIError(t, response, http.StatusBadRequest, "invalid_input", false)
	})

	// Make this session eligible for normal administrator activity sampling.
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET last_seen_at=clock_timestamp()-interval '1 minute'
		WHERE user_id=$1 AND kind='admin'`, adminID); err != nil {
		t.Fatal(err)
	}
	activity := func() (time.Time, time.Time) {
		var lastSeen, lastActivity time.Time
		if err := f.pool.QueryRow(f.ctx, `SELECT s.last_seen_at,a.last_activity_at FROM sessions s
			JOIN user_activity a ON a.user_id=s.user_id WHERE s.user_id=$1 AND s.kind='admin'`, adminID).Scan(&lastSeen, &lastActivity); err != nil {
			t.Fatal(err)
		}
		return lastSeen, lastActivity
	}
	lastSeen, lastActivity := activity()
	expectAdminSeriesNFOAuditReport(t, audit(t, target, nil), map[string]any{
		"LibraryId": id, "Status": "scan_unsettled", "SeriesCount": float64(0), "ObservedCount": float64(0), "Warnings": []any{},
	})
	if seenAfter, activityAfter := activity(); !seenAfter.Equal(lastSeen) || !activityAfter.Equal(lastActivity) {
		t.Fatal("audit sampled administrator session or user activity")
	}
	var jobs int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM scan_jobs WHERE library_id=$1", id).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("audit created scan work: jobs = %d, error = %v", jobs, err)
	}
}

func TestHTTPAdminSeriesNFOAuditReportsOnlyWarningFacts(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	root = filepath.Join(root, "series")
	const wrong, right = "Frost Lanterns (2004)", "Glass Harbor (2019)"
	const nfo = `<tvshow><title>Glass Harbor</title><year>2019</year><uniqueid type="imdb">tt1234567</uniqueid></tvshow>`
	for _, directory := range []string{wrong, right} {
		writeAPIMediaFile(t, root, directory+"/Season 1/Show.S01E01.mp4")
		writeNFOReferenceSidecar(t, root, directory+"/tvshow.nfo", []byte(nfo))
	}
	id := createAndScanAPILibrary(t, f, cookie, csrf, root, "tvshows")
	findSeriesID := func(directory string) string {
		var itemID string
		if err := f.pool.QueryRow(f.ctx, "SELECT id FROM items WHERE library_id=$1 AND type='Series' AND path=$2", id, filepath.Join(root, directory)).Scan(&itemID); err != nil {
			t.Fatalf("find scanned series for audit fixture: %v", err)
		}
		return itemID
	}
	wrongID, rightID := findSeriesID(wrong), findSeriesID(right)
	target := "/admin/v1/libraries/" + id + "/series-nfo-audit"
	response := f.request(t, http.MethodPost, target, nil, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectAdminSeriesNFOAuditReport(t, response, map[string]any{
		"LibraryId": id, "Status": "observed", "SeriesCount": float64(2), "ObservedCount": float64(2),
		"Warnings": []any{map[string]any{"ItemId": wrongID, "Code": "shared_declaration_disagreement", "CorroboratingItemId": rightID}},
	})
	for _, directory := range []string{wrong, right} {
		data, err := os.ReadFile(filepath.Join(root, directory, "tvshow.nfo"))
		if err != nil || string(data) != nfo {
			t.Fatalf("audit did not preserve the NFO fixture: %v", err)
		}
	}
}

func expectAdminSeriesNFOAuditReport(t *testing.T, response *httptest.ResponseRecorder, want map[string]any) {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := jsonObject(t, response); !reflect.DeepEqual(got, want) {
		t.Fatalf("audit report = %#v, want %#v", got, want)
	}
}
