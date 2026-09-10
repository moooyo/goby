package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func adminScanRawRequest(t *testing.T, f *serverFixture, target, body string, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body)).WithContext(f.ctx)
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

func assertAdminScanJobCount(t *testing.T, f *serverFixture, want int) {
	t.Helper()
	var count int
	if err := f.pool.QueryRow(f.ctx, "SELECT count(*) FROM scan_jobs").Scan(&count); err != nil {
		t.Fatalf("count durable scan jobs: %v", err)
	}
	if count != want {
		t.Fatalf("scan request changed the durable job count to %d, want %d", count, want)
	}
}

func assertAdminScanJobMode(t *testing.T, f *serverFixture, job map[string]any, libraryID string, forceProbe bool) string {
	t.Helper()
	id := stringValue(t, job, "Id")
	if got, ok := job["ForceProbe"].(bool); !ok || got != forceProbe || job["LibraryId"] != libraryID {
		t.Fatalf("HTTP scan job lost its boolean mode or library identity: %#v", job)
	}
	var durableMode bool
	if err := f.pool.QueryRow(f.ctx, "SELECT force_probe FROM scan_jobs WHERE id = $1", id).Scan(&durableMode); err != nil {
		t.Fatalf("read durable scan mode: %v", err)
	}
	if durableMode != forceProbe {
		t.Fatalf("durable scan mode = %v, want %v", durableMode, forceProbe)
	}
	return id
}

func waitAdminScanHTTPJob(t *testing.T, f *serverFixture, cookie *http.Cookie, libraryID, jobID string, forceProbe bool) map[string]any {
	t.Helper()
	timer, ticker := time.NewTimer(15*time.Second), time.NewTicker(25*time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		jobs, _ := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
		for _, job := range jobs {
			if job["Id"] != jobID {
				continue
			}
			assertAdminScanJobMode(t, f, job, libraryID, forceProbe)
			switch job["Status"] {
			case "completed":
				if job["Error"] != "" || job["Scanned"] != float64(1) {
					t.Fatalf("scan did not inspect exactly one fixture file without warnings: %#v", job)
				}
				return job
			case "failed", "cancelled", "interrupted":
				t.Fatalf("scan job did not complete: %#v", job)
			}
		}
		select {
		case <-timer.C:
			t.Fatal("timed out waiting for the administrator scan job")
		case <-f.ctx.Done():
			t.Fatalf("scan fixture context ended: %v", f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestHTTPAdminScanRejectsUnauthorizedAndInvalidRequestsWithoutQueuing(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	writeAPIMediaFile(t, root, "movies/Protected.mp4")
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Protected scan", "CollectionType": "movies", "Paths": []string{filepath.Join(root, "movies")}, "Scan": false,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")
	target := "/admin/v1/libraries/" + libraryID + "/scan"
	embyToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	for _, test := range []struct {
		name, query, code string
		status            int
		headers           http.Header
		cookie            *http.Cookie
	}{
		{name: "missing authentication", status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "Emby header is not an admin session", status: http.StatusUnauthorized, code: "authentication_required", headers: http.Header{"X-Emby-Token": {embyToken}}},
		{name: "Emby query is not an admin session", query: "?api_key=" + embyToken, status: http.StatusUnauthorized, code: "authentication_required"},
		{name: "Emby token in cookie", status: http.StatusUnauthorized, code: "invalid_credentials", cookie: &http.Cookie{Name: sessionCookie, Value: embyToken}, headers: http.Header{"X-CSRF-Token": {csrfToken(embyToken)}}},
		{name: "missing CSRF", status: http.StatusForbidden, code: "csrf_invalid", cookie: cookie},
		{name: "wrong CSRF", status: http.StatusForbidden, code: "csrf_invalid", cookie: cookie, headers: http.Header{"X-CSRF-Token": {"wrong-token"}}},
		{name: "cross origin", status: http.StatusForbidden, code: "origin_denied", cookie: cookie, headers: http.Header{"X-CSRF-Token": {csrf}, "Origin": {"https://other.example.test"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if test.cookie != nil {
				cookies = append(cookies, test.cookie)
			}
			// Authentication and CSRF must reject the request before body validation.
			response := adminScanRawRequest(t, f, target+test.query, "invalid-json", test.headers, cookies...)
			expectAPIError(t, response, test.status, test.code, false)
			assertAdminScanJobCount(t, f, 0)
		})
	}
	for _, test := range []struct {
		name, query, body, contentType string
		status                         int
	}{
		{name: "query force mode", query: "?ForceProbe=true", status: http.StatusBadRequest},
		{name: "undeclared query", query: "?Unused=", body: `{}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "unknown key", body: `{"Mode":"forced"}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "wrong key case", body: `{"forceProbe":true}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "duplicate key", body: `{"ForceProbe":true,"ForceProbe":false}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "null boolean", body: `{"ForceProbe":null}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "string boolean", body: `{"ForceProbe":"true"}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "null root", body: `null`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "trailing object", body: `{"ForceProbe":true}{}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "invalid UTF-8", body: "{\"ForceProbe\":true,\"\xff\":false}", contentType: "application/json", status: http.StatusBadRequest},
		{name: "oversized body", body: `{}` + strings.Repeat(" ", 4095), contentType: "application/json", status: http.StatusBadRequest},
		{name: "missing MIME", body: `{"ForceProbe":true}`, status: http.StatusUnsupportedMediaType},
		{name: "wrong MIME", body: `{"ForceProbe":true}`, contentType: "text/plain", status: http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{"X-CSRF-Token": {csrf}}
			if test.contentType != "" {
				headers.Set("Content-Type", test.contentType)
			}
			response := adminScanRawRequest(t, f, target+test.query, test.body, headers, cookie)
			code := "invalid_input"
			if test.status == http.StatusUnsupportedMediaType {
				code = "unsupported_media_type"
			}
			expectAPIError(t, response, test.status, code, false)
			assertAdminScanJobCount(t, f, 0)
		})
	}
	expectAPIError(t, f.request(t, http.MethodPost, "/admin/v1/libraries/missing-library/scan", map[string]any{"ForceProbe": true}, http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusNotFound, "not_found", false)
	assertAdminScanJobCount(t, f, 0)
	jobs, total := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
	if total != 0 || len(jobs) != 0 {
		t.Fatal("rejected scan requests became visible in the job list")
	}
}

type adminScanCountingProber struct {
	calls atomic.Int64
}

func (prober *adminScanCountingProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (apiMediaProber{}).ProbeFile(ctx, file)
	if err != nil {
		return media.Info{}, err
	}
	info.DurationTicks += prober.calls.Add(1) * media.TicksPerSecond
	info.Chapters[0].EndTicks = info.DurationTicks
	return info, nil
}

func installAdminScanCountingProber(t *testing.T, f *serverFixture, root string, prober *adminScanCountingProber) {
	t.Helper()
	closeFixtureCatalogForReplacement(t, f)
	catalog, err := library.New(f.pool, prober, []string{root})
	if err != nil {
		t.Fatalf("create counting scan catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.handler = f.app.Handler()
}

func TestHTTPAdminScanForceProbePersistsAndBypassesOnlyRequestedScans(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	prober := &adminScanCountingProber{}
	installAdminScanCountingProber(t, f, root, prober)
	path := writeAPIMediaFile(t, root, "movies/Refresh.mp4")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	contentBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	adminID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Refresh modes", "CollectionType": "movies", "Paths": []string{filepath.Dir(path)}, "Scan": true,
	}, http.Header{"X-CSRF-Token": {csrf}}, cookie)
	expectStatus(t, created, http.StatusCreated)
	object := jsonObject(t, created)
	libraryID := stringValue(t, objectValue(t, object, "Library"), "Id")
	initialJobID := assertAdminScanJobMode(t, f, objectValue(t, object, "Job"), libraryID, false)
	waitAdminScanHTTPJob(t, f, cookie, libraryID, initialJobID, false)
	if calls := prober.calls.Load(); calls != 1 {
		t.Fatalf("initial creation scan made %d probe calls, want 1", calls)
	}
	embyToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	embyHeaders := http.Header{"X-Emby-Token": {embyToken}}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?ParentId="+libraryID+"&Recursive=true&IncludeItemTypes=Movie", nil, embyHeaders))
	if total != 1 || len(items) != 1 {
		t.Fatalf("initial scan did not create one movie: %#v", items)
	}
	itemID := stringValue(t, items[0], "Id")
	jobModes := map[string]bool{initialJobID: false}
	for _, test := range []struct {
		name, body, contentType string
		forceProbe              bool
		wantCalls               int64
	}{
		{name: "legacy empty", wantCalls: 1},
		{name: "empty with legacy MIME", contentType: "text/plain", wantCalls: 1},
		{name: "empty object", body: `{}`, contentType: "application/json", wantCalls: 1},
		{name: "explicit cached", body: `{"ForceProbe":false}`, contentType: "application/json", wantCalls: 1},
		{name: "forced refresh", body: `{"ForceProbe":true}`, contentType: "application/json; charset=utf-8", forceProbe: true, wantCalls: 2},
		{name: "default after forced refresh", wantCalls: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{"X-CSRF-Token": {csrf}}
			if test.contentType != "" {
				headers.Set("Content-Type", test.contentType)
			}
			response := adminScanRawRequest(t, f, "/admin/v1/libraries/"+libraryID+"/scan", test.body, headers, cookie)
			expectStatus(t, response, http.StatusAccepted)
			jobID := assertAdminScanJobMode(t, f, objectValue(t, jsonObject(t, response), "Job"), libraryID, test.forceProbe)
			jobModes[jobID] = test.forceProbe
			job := waitAdminScanHTTPJob(t, f, cookie, libraryID, jobID, test.forceProbe)
			if calls := prober.calls.Load(); calls != test.wantCalls {
				t.Fatalf("probe calls = %d, want %d for ForceProbe=%v", calls, test.wantCalls, test.forceProbe)
			}
			wantUpdated := float64(0)
			if test.forceProbe {
				wantUpdated = 1
			}
			if job["Added"] != float64(0) || job["Updated"] != wantUpdated {
				t.Fatalf("refresh reported incorrect item changes: %#v", job)
			}
			detail := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+adminID+"/Items/"+itemID, nil, embyHeaders))
			wantDuration := float64((125 + test.wantCalls) * media.TicksPerSecond)
			if detail["Id"] != itemID || detail["RunTimeTicks"] != wantDuration {
				t.Fatalf("refresh did not preserve item identity and expose the expected probe result: %#v", detail)
			}
		})
	}
	// Compatibility refresh first commits the all-library task snapshot. Its
	// coordinator then admits the same ordinary cache-aware scan asynchronously.
	var priorRunIDs []string
	if err := f.pool.QueryRow(f.ctx, "SELECT ARRAY(SELECT id FROM task_runs)").Scan(&priorRunIDs); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Library/Refresh", nil, embyHeaders), http.StatusNoContent)
	refreshJobID := waitScheduledRefreshOwnedScanJob(t, f, libraryID, priorRunIDs)
	jobs, total := responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
	if total != len(jobModes)+1 || len(jobs) != total {
		t.Fatalf("compatibility refresh did not queue exactly one job: count = %d, total = %d", len(jobs), total)
	}
	for _, job := range jobs {
		jobID := stringValue(t, job, "Id")
		if _, present := jobModes[jobID]; !present {
			if jobID != refreshJobID {
				t.Fatal("refresh observed a new scan outside its acknowledged task child")
			}
			waitAdminScanHTTPJob(t, f, cookie, libraryID, jobID, false)
			jobModes[jobID] = false
		}
	}
	if calls := prober.calls.Load(); calls != 2 {
		t.Fatalf("compatibility refresh bypassed the probe cache: calls = %d", calls)
	}
	// Reopening the catalog makes the job list read every mode from durable state.
	installAdminScanCountingProber(t, f, root, prober)
	jobs, total = responseItems(t, f.request(t, http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
	if total != len(jobModes) || len(jobs) != total {
		t.Fatal("catalog restart changed the persisted scan job history")
	}
	for _, job := range jobs {
		jobID := stringValue(t, job, "Id")
		mode, present := jobModes[jobID]
		if !present {
			t.Fatalf("catalog restart exposed an unexpected scan job: %s", jobID)
		}
		assertAdminScanJobMode(t, f, job, libraryID, mode)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	contentAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || !bytes.Equal(contentBefore, contentAfter) {
		t.Fatal("scan refresh modified its source media file")
	}
}
