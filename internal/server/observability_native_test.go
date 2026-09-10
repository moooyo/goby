//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
)

func nativeObservabilityNoCache(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatal("observability response must not be cached")
	}
}

func nativeObservabilityFields(t *testing.T, value map[string]any, fields ...string) {
	t.Helper()
	if len(value) != len(fields) {
		t.Fatalf("observability object has %d fields, want %d", len(value), len(fields))
	}
	for _, field := range fields {
		if _, present := value[field]; !present {
			t.Fatalf("observability object omitted %s", field)
		}
	}
}

func nativeObservabilityObject(t *testing.T, response *httptest.ResponseRecorder, fields ...string) map[string]any {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	nativeObservabilityNoCache(t, response)
	value := jsonObject(t, response)
	nativeObservabilityFields(t, value, fields...)
	return value
}

func nativeObservabilityError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	expectAPIError(t, response, status, code, false)
	nativeObservabilityNoCache(t, response)
}

func nativeObservabilityItems(t *testing.T, value map[string]any) []any {
	t.Helper()
	items, ok := value["Items"].([]any)
	if !ok {
		t.Fatal("observability Items must be an array, including on empty pages")
	}
	return items
}

func nativeObservabilityItem(t *testing.T, value any) map[string]any {
	t.Helper()
	item, ok := value.(map[string]any)
	if !ok {
		t.Fatal("observability item must be an object")
	}
	return item
}

func nativeObservabilityDecimal(t *testing.T, value map[string]any, field string) string {
	t.Helper()
	text := stringValue(t, value, field)
	parsed, err := strconv.ParseUint(text, 10, 64)
	if err != nil || strconv.FormatUint(parsed, 10) != text {
		t.Fatalf("observability %s must be a canonical nonnegative decimal string", field)
	}
	return text
}

func nativeObservabilityDate(t *testing.T, value map[string]any, field string) time.Time {
	t.Helper()
	text := stringValue(t, value, field)
	stamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || !strings.HasSuffix(text, "Z") {
		t.Fatalf("observability %s must be an explicit UTC date", field)
	}
	return stamp
}

func nativeActivityHTTPPage(t *testing.T, f *serverFixture, cookie *http.Cookie, query string) map[string]any {
	t.Helper()
	return nativeObservabilityObject(t, f.request(t, http.MethodGet, "/admin/v1/activity"+query, nil, nil, cookie),
		"Items", "TotalRecordCount", "StartIndex", "Limit", "RetentionDays")
}

func nativeDiagnosticStore(t *testing.T, f *serverFixture) (*diagnostics.Store, string) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal("restrict the owned diagnostics directory")
	}
	store, err := diagnostics.Open(diagnostics.Config{
		Directory: directory, MaxFileBytes: diagnostics.MaxRecordBytes, MaxFiles: 3, RetentionDays: 2, MinFreeBytes: 1,
	})
	if err != nil {
		t.Fatalf("open the owned diagnostics store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f.app.diagnostics = store
	handler := f.handler
	f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if recorder, ok := w.(*httptest.ResponseRecorder); ok {
			w = diagnosticRecorderWithDeadline{recorder}
		}
		handler.ServeHTTP(w, r)
	})
	return store, directory
}

func nativeDiagnosticRecords(t *testing.T, f *serverFixture, store *diagnostics.Store) []byte {
	t.Helper()
	var expected bytes.Buffer
	handler := diagnostics.NewHandler(store, slog.NewJSONHandler(&expected, nil))
	for index, entry := range []struct {
		message string
		attrs   []slog.Attr
	}{
		{"server listening", []slog.Attr{slog.String("version", "1.2.3"), slog.String("database", "postgresql"), slog.String("password", "diagnostic-private-password")}},
		{"request completed", []slog.Attr{slog.String("method", "GET"), slog.Int("status", 200), slog.Int("bytes", 17), slog.String("Authorization", "Bearer diagnostic-private-token")}},
		{"diagnostic-private-message /private/library/file.mkv", []slog.Attr{slog.String("path", "/private/library/file.mkv")}},
	} {
		record := slog.NewRecord(time.Date(2026, time.September, 10, 8, 0, index, 0, time.UTC), slog.LevelInfo, entry.message, 0)
		record.AddAttrs(entry.attrs...)
		if err := handler.Handle(f.ctx, record); err != nil {
			t.Fatalf("persist sanitized diagnostic fixture: %v", err)
		}
	}
	for _, secret := range []string{"diagnostic-private", "/private/library/file.mkv"} {
		if bytes.Contains(expected.Bytes(), []byte(secret)) {
			t.Fatal("sanitized diagnostic fixture retained a private value")
		}
	}
	return append([]byte(nil), expected.Bytes()...)
}

func TestHTTPNativeActivityProjectsCommittedSettingsWithoutValuesOrCredentialIDs(t *testing.T) {
	f, cookie, csrf, adminID := adminSettingsHTTPFixture(t)
	f.app.cfg.ActivityRetentionDays = 17
	const privateName = "Private settings value with password=activity-private-marker"
	write := func(revision, name string) {
		t.Helper()
		expectStatus(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
			adminSettingsHTTPUpdate(revision, name, 7_654_321)), http.StatusOK)
	}
	write("1", privateName)
	write("2", privateName)
	write("2", "A second private activity value")
	page := nativeActivityHTTPPage(t, f, cookie, "?Action=settings.updated")
	if page["TotalRecordCount"] != float64(2) || page["StartIndex"] != float64(0) || page["Limit"] != float64(50) || page["RetentionDays"] != float64(17) {
		t.Fatal("activity defaults or committed-change count differs from the contract")
	}
	items := nativeObservabilityItems(t, page)
	if len(items) != 2 {
		t.Fatal("settings activity must contain only the two committed changes")
	}
	var priorDate time.Time
	var priorID uint64
	for index, raw := range items {
		item := nativeObservabilityItem(t, raw)
		nativeObservabilityFields(t, item, "Id", "Date", "Action", "Severity", "Source", "Actor", "Resource", "Revision", "Count", "State", "ChangedFields", "Name", "Overview")
		id, _ := strconv.ParseUint(nativeObservabilityDecimal(t, item, "Id"), 10, 64)
		date := nativeObservabilityDate(t, item, "Date")
		if id == 0 || index > 0 && (date.After(priorDate) || date.Equal(priorDate) && id >= priorID) {
			t.Fatal("activity entries are not ordered by descending date and ID")
		}
		priorDate, priorID = date, id
		actor := objectValue(t, item, "Actor")
		nativeObservabilityFields(t, actor, "Kind", "Id", "Name")
		resource := objectValue(t, item, "Resource")
		nativeObservabilityFields(t, resource, "Kind", "Id")
		if item["Action"] != "settings.updated" || item["Severity"] != "Info" || item["Source"] != "native" ||
			actor["Kind"] != "user" || actor["Id"] != adminID || actor["Name"] != "Administrator" ||
			resource["Kind"] != "settings" || resource["Id"] != "1" || item["Revision"] != strconv.Itoa(3-index) ||
			item["Count"] != "1" || item["State"] != nil || item["Name"] != "Server settings updated" || item["Overview"] != "Supported server settings were updated." {
			t.Fatal("activity entry did not project the committed settings facts")
		}
		wantedFields := []any{"ServerName"}
		if index == 1 {
			wantedFields = []any{"MaxBitrate", "ServerName", "ServerNameMode"}
		}
		if !reflect.DeepEqual(item["ChangedFields"], wantedFields) {
			t.Fatal("settings activity lost its sorted changed-field names")
		}
	}
	principal, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve the owned activity actor")
	}
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{privateName, "A second private activity value", cookie.Value, csrf, principal.SessionID, f.cfg.DatabaseURL, "CredentialID", "RequestID"} {
		if secret != "" && bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("activity response exposed a setting value or credential detail")
		}
	}
	user := managedHTTPDetail(t, f, cookie, adminID)
	update := managedHTTPUpdateBody(user)
	update["Name"] = "Current Administrator Name"
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/users/"+adminID, update,
		http.Header{"X-CSRF-Token": {csrf}}, cookie), http.StatusOK)
	renamed := nativeActivityHTTPPage(t, f, cookie, "?Action=settings.updated&ActorId="+adminID+"&Severity=Info&Limit=1")
	renamedItems := nativeObservabilityItems(t, renamed)
	if renamed["TotalRecordCount"] != float64(2) || len(renamedItems) != 1 ||
		objectValue(t, nativeObservabilityItem(t, renamedItems[0]), "Actor")["Name"] != "Current Administrator Name" {
		t.Fatal("activity actor name was not enriched from the current user")
	}
	for _, test := range []struct {
		query string
		total float64
	}{
		{"?Action=settings.updated&Severity=Error", 0},
		{"?Action=settings.updated&ActorId=missing-user", 0},
		{"?Action=settings.updated&StartIndex=2&Limit=1", 2},
	} {
		empty := nativeActivityHTTPPage(t, f, cookie, test.query)
		if len(nativeObservabilityItems(t, empty)) != 0 || empty["TotalRecordCount"] != test.total {
			t.Fatal("activity filtering or exhausted pagination returned an unexpected row or total")
		}
	}
	latest := nativeObservabilityItem(t, items[0])
	latestDate := nativeObservabilityDate(t, latest, "Date")
	for _, test := range []struct {
		date  time.Time
		count float64
	}{
		{latestDate, 1},
		{latestDate.Add(time.Nanosecond), 0},
	} {
		filtered := nativeActivityHTTPPage(t, f, cookie, "?Action=settings.updated&MinDate="+url.QueryEscape(test.date.Format(time.RFC3339Nano)))
		if filtered["TotalRecordCount"] != test.count || len(nativeObservabilityItems(t, filtered)) != int(test.count) {
			t.Fatal("activity MinDate did not preserve its inclusive microsecond boundary")
		}
	}
}

func TestHTTPNativeActivityRetainsExactInt64Facts(t *testing.T) {
	f, cookie, csrf, _ := adminSettingsHTTPFixture(t)
	expectStatus(t, adminSettingsHTTPWrite(t, f, cookie, csrf, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate("1", "Precision fixture", nil)), http.StatusOK)
	const exact = "9007199254740993"
	if _, err := f.pool.Exec(f.ctx, `UPDATE activity_entries SET revision = $1::bigint, affected_count = $1::bigint
		WHERE action = 'settings.updated'`, exact); err != nil {
		t.Fatal("set the owned large activity counters")
	}
	page := nativeActivityHTTPPage(t, f, cookie, "?Action=settings.updated")
	items := nativeObservabilityItems(t, page)
	if len(items) != 1 {
		t.Fatal("large-counter activity fixture is missing")
	}
	item := nativeObservabilityItem(t, items[0])
	if nativeObservabilityDecimal(t, item, "Revision") != exact || nativeObservabilityDecimal(t, item, "Count") != exact {
		t.Fatal("activity facts lost precision above the JavaScript safe integer limit")
	}
	if page["RetentionDays"] != float64(30) {
		t.Fatal("activity did not expose its default retention policy")
	}
}

func TestHTTPNativeActivityStorageFailureIsUnavailableAndDoesNotExposeDatabaseDetails(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	before := nativeActivityHTTPPage(t, f, cookie, "")
	if _, err := f.pool.Exec(f.ctx, "ALTER TABLE activity_entries RENAME TO activity_entries_temporarily_unavailable"); err != nil {
		t.Fatal("temporarily rename the owned activity table")
	}
	renamed := true
	restore := func() {
		if !renamed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(ctx, "ALTER TABLE activity_entries_temporarily_unavailable RENAME TO activity_entries"); err != nil {
			t.Error("restore the owned activity table")
			return
		}
		renamed = false
	}
	defer restore()
	nativeObservabilityError(t, f.request(t, http.MethodGet, "/admin/v1/activity?Limit=private-marker", nil, nil),
		http.StatusUnauthorized, "authentication_required")
	response := f.request(t, http.MethodGet, "/admin/v1/activity", nil, nil, cookie)
	nativeObservabilityError(t, response, http.StatusServiceUnavailable, "activity_unavailable")
	for _, private := range []string{"activity_entries", "SQLSTATE", "relation", f.cfg.DatabaseURL} {
		if private != "" && strings.Contains(response.Body.String(), private) {
			t.Fatal("activity storage failure exposed a database detail")
		}
	}
	restore()
	if renamed {
		t.Fatal("the owned activity table was not restored")
	}
	after := nativeActivityHTTPPage(t, f, cookie, "")
	if !reflect.DeepEqual(after, before) {
		t.Fatal("a failed activity read changed history or prevented recovery")
	}
}

func TestHTTPNativeObservabilityAuthenticatesBeforeParsingQuery(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	viewer, err := a.users.CreateUser(a.ctx, "Observability Viewer", "observability-viewer-password", false)
	if err != nil {
		t.Fatal("create the owned observability viewer")
	}
	tokens := []string{
		stringValue(t, a.embyLogin(t, "Administrator", "administrator-password"), "AccessToken"),
		stringValue(t, a.embyLogin(t, viewer.Name, "observability-viewer-password"), "AccessToken"),
		a.create(t, "Observability native boundary").token,
	}
	for _, target := range []string{
		"/admin/v1/activity?Limit=invalid-private-marker",
		"/admin/v1/logs?StartIndex=-1",
		"/admin/v1/logs/missing.jsonl/lines?Limit=501",
		"/admin/v1/logs/missing.jsonl/download?Sanitize=private-marker",
	} {
		nativeObservabilityError(t, a.request(t, http.MethodGet, target, nil, nil), http.StatusUnauthorized, "authentication_required")
		invalid := a.request(t, http.MethodGet, target, nil, nil, &http.Cookie{Name: sessionCookie, Value: "invalid-private-cookie"})
		nativeObservabilityError(t, invalid, http.StatusUnauthorized, "invalid_credentials")
		for _, token := range tokens {
			for _, asCookie := range []bool{false, true} {
				headers := http.Header{"X-Emby-Token": {token}}
				var cookies []*http.Cookie
				if asCookie {
					headers = nil
					cookies = []*http.Cookie{{Name: sessionCookie, Value: token}}
				}
				response := a.request(t, http.MethodGet, target, nil, headers, cookies...)
				expectedCode := "authentication_required"
				if asCookie {
					expectedCode = "invalid_credentials"
				}
				nativeObservabilityError(t, response, http.StatusUnauthorized, expectedCode)
				if strings.Contains(response.Body.String(), token) || strings.Contains(response.Body.String(), "private-marker") {
					t.Fatal("native observability authentication reflected rejected input")
				}
			}
		}
		nativeObservabilityError(t, a.request(t, http.MethodGet, target, nil, nil, a.cookie), http.StatusBadRequest, "invalid_input")
	}
	for _, query := range []string{
		"?Unknown=private-marker", "?limit=1", "?Limit=1&Limit=2", "?Limit=", "?Limit=0", "?Limit=201", "?Limit=01", "?Limit=%2B1", "?Limit=1.0",
		"?StartIndex=-1", "?StartIndex=01", "?StartIndex=2147483648", "?StartIndex=999999999999999999999999999999", "?Limit=1;StartIndex=0", "?%FF=value", "?Limit=%FF", "?Limit=%ZZ",
		"?Unknown=" + strings.Repeat("x", 4097),
	} {
		for _, route := range []string{"/admin/v1/activity", "/admin/v1/logs"} {
			response := a.request(t, http.MethodGet, route+query, nil, nil, a.cookie)
			nativeObservabilityError(t, response, http.StatusBadRequest, "invalid_input")
			if strings.Contains(response.Body.String(), "private-marker") {
				t.Fatal("query validation reflected an untrusted value")
			}
		}
	}
	for _, query := range []string{
		"?MinDate=2026-09-10", "?MinDate=not-a-date", "?MinDate=0000-01-01T00:00:00Z", "?MinDate=2026-09-10T00:00:00Z&MinDate=2026-09-10T00:00:00Z",
		"?Severity=info", "?Severity=Warning", "?Severity=Info&Severity=Info", "?Action=unknown.action", "?ActorId=../private-marker", "?ActorId=",
	} {
		nativeObservabilityError(t, a.request(t, http.MethodGet, "/admin/v1/activity"+query, nil, nil, a.cookie), http.StatusBadRequest, "invalid_input")
	}
	for _, query := range []string{"?Limit=501", "?Limit=0", "?StartIndex=-1", "?Limit=2&Limit=2", "?SearchTerm=private-marker"} {
		nativeObservabilityError(t, a.request(t, http.MethodGet, "/admin/v1/logs/missing.jsonl/lines"+query, nil, nil, a.cookie), http.StatusBadRequest, "invalid_input")
	}
}

func TestHTTPNativeLogsExposeSanitizedSnapshotsAndDownloadRanges(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	store, directory := nativeDiagnosticStore(t, f)
	want := nativeDiagnosticRecords(t, f, store)
	page := nativeObservabilityObject(t, f.request(t, http.MethodGet, "/admin/v1/logs", nil, nil, cookie),
		"Items", "TotalRecordCount", "StartIndex", "Limit", "Status")
	items := nativeObservabilityItems(t, page)
	if len(items) != 1 || page["TotalRecordCount"] != float64(1) || page["StartIndex"] != float64(0) || page["Limit"] != float64(50) {
		t.Fatal("diagnostic listing did not expose the single owned file with paging defaults")
	}
	file := nativeObservabilityItem(t, items[0])
	nativeObservabilityFields(t, file, "Name", "DateCreated", "DateModified", "Size")
	name := stringValue(t, file, "Name")
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\") || nativeObservabilityDecimal(t, file, "Size") != strconv.Itoa(len(want)) {
		t.Fatal("diagnostic file metadata exposed a path or an inaccurate size")
	}
	nativeObservabilityDate(t, file, "DateCreated")
	nativeObservabilityDate(t, file, "DateModified")
	status := objectValue(t, page, "Status")
	nativeObservabilityFields(t, status, "Healthy", "Degraded", "Closed", "MaxFileBytes", "MaxFiles", "RetentionDays", "MinFreeBytes", "Format")
	if !reflect.DeepEqual(status, map[string]any{"Healthy": true, "Degraded": false, "Closed": false, "MaxFileBytes": strconv.Itoa(diagnostics.MaxRecordBytes),
		"MaxFiles": float64(3), "RetentionDays": float64(2), "MinFreeBytes": "1", "Format": "jsonl"}) {
		t.Fatal("diagnostic listing did not project its bounded sanitized storage policy")
	}
	encoded, _ := json.Marshal(page)
	if bytes.Contains(encoded, []byte(directory)) || bytes.Contains(encoded, []byte(".goby-diagnostics")) {
		t.Fatal("diagnostic listing exposed internal ownership files or its directory")
	}
	empty := nativeObservabilityObject(t, f.request(t, http.MethodGet, "/admin/v1/logs?StartIndex=1&Limit=1", nil, nil, cookie),
		"Items", "TotalRecordCount", "StartIndex", "Limit", "Status")
	if len(nativeObservabilityItems(t, empty)) != 0 || empty["TotalRecordCount"] != float64(1) || empty["StartIndex"] != float64(1) || empty["Limit"] != float64(1) {
		t.Fatal("diagnostic exhausted page lost its matching total or offset")
	}
	base := "/admin/v1/logs/" + url.PathEscape(name)
	wantLines := strings.Split(strings.TrimSuffix(string(want), "\n"), "\n")
	for _, test := range []struct {
		query        string
		start, count int
	}{
		{"", 0, 3},
		{"?StartIndex=1&Limit=1", 1, 1},
		{"?StartIndex=3&Limit=500", 3, 0},
	} {
		lines := nativeObservabilityObject(t, f.request(t, http.MethodGet, base+"/lines"+test.query, nil, nil, cookie),
			"Items", "StartIndex", "NextIndex", "TotalRecordCount", "SnapshotSize")
		actual := nativeObservabilityItems(t, lines)
		if len(actual) != test.count || lines["StartIndex"] != float64(test.start) || lines["NextIndex"] != float64(test.start+test.count) ||
			lines["TotalRecordCount"] != float64(3) || nativeObservabilityDecimal(t, lines, "SnapshotSize") != strconv.Itoa(len(want)) {
			t.Fatal("diagnostic lines did not describe one bounded snapshot")
		}
		for index, line := range actual {
			if line != wantLines[test.start+index] {
				t.Fatal("diagnostic lines differ from the persisted sanitized JSONL bytes")
			}
		}
	}
	download := f.request(t, http.MethodGet, base+"/download", nil, nil, cookie)
	expectStatus(t, download, http.StatusOK)
	nativeObservabilityNoCache(t, download)
	if !bytes.Equal(download.Body.Bytes(), want) || download.Header().Get("Content-Type") != "application/x-ndjson" || download.Header().Get("Content-Length") != strconv.Itoa(len(want)) ||
		download.Header().Get("Accept-Ranges") != "bytes" || download.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("diagnostic download did not serve the complete immutable sanitized snapshot")
	}
	disposition, parameters, err := mime.ParseMediaType(download.Header().Get("Content-Disposition"))
	if err != nil || disposition != "attachment" || parameters["filename"] != name {
		t.Fatal("diagnostic download did not use its registered attachment filename")
	}
	for _, test := range []struct {
		method, byteRange string
		status            int
		body              []byte
		contentRange      string
	}{
		{http.MethodHead, "", http.StatusOK, nil, ""},
		{http.MethodGet, "bytes=0-15", http.StatusPartialContent, want[:16], fmt.Sprintf("bytes 0-15/%d", len(want))},
		{http.MethodGet, "bytes=-11", http.StatusPartialContent, want[len(want)-11:], fmt.Sprintf("bytes %d-%d/%d", len(want)-11, len(want)-1, len(want))},
		{http.MethodHead, "bytes=0-15", http.StatusPartialContent, nil, fmt.Sprintf("bytes 0-15/%d", len(want))},
	} {
		headers := make(http.Header)
		if test.byteRange != "" {
			headers.Set("Range", test.byteRange)
		}
		response := f.request(t, test.method, base+"/download", nil, headers, cookie)
		expectStatus(t, response, test.status)
		nativeObservabilityNoCache(t, response)
		if !bytes.Equal(response.Body.Bytes(), test.body) || response.Header().Get("Content-Range") != test.contentRange {
			t.Fatal("diagnostic HEAD or byte range response differs from its snapshot")
		}
		if test.byteRange == "" && response.Header().Get("Content-Length") != strconv.Itoa(len(want)) ||
			test.byteRange == "bytes=0-15" && response.Header().Get("Content-Length") != "16" {
			t.Fatal("diagnostic HEAD or range response advertised the wrong byte count")
		}
	}
	unsatisfied := f.request(t, http.MethodGet, base+"/download", nil, http.Header{"Range": {fmt.Sprintf("bytes=%d-", len(want))}}, cookie)
	expectStatus(t, unsatisfied, http.StatusRequestedRangeNotSatisfiable)
	nativeObservabilityNoCache(t, unsatisfied)
	if unsatisfied.Header().Get("Content-Range") != fmt.Sprintf("bytes */%d", len(want)) || bytes.Contains(unsatisfied.Body.Bytes(), want) {
		t.Fatal("unsatisfied diagnostic range leaked snapshot data or lost its complete length")
	}
	for _, headers := range []http.Header{{"Origin": {"https://foreign.example.test"}}, {"Sec-Fetch-Site": {"cross-site"}}} {
		nativeObservabilityError(t, f.request(t, http.MethodGet, base+"/download", nil, headers, cookie), http.StatusForbidden, "origin_denied")
	}
	for _, suffix := range []string{"?Sanitize=false", "?Sanitize=true", "?Unknown=value", "?"} {
		nativeObservabilityError(t, f.request(t, http.MethodGet, base+"/download"+suffix, nil, nil, cookie), http.StatusBadRequest, "invalid_input")
	}
}

func TestHTTPNativeLogsRejectUnknownTraversalAndSubstitutedFiles(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	store, directory := nativeDiagnosticStore(t, f)
	nativeDiagnosticRecords(t, f, store)
	for _, suffix := range []string{"/lines", "/download"} {
		nativeObservabilityError(t, f.request(t, http.MethodGet, "/admin/v1/logs/missing.jsonl"+suffix, nil, nil, cookie), http.StatusNotFound, "not_found")
		for _, segment := range []string{"%2e%2e", "%2fetc%2fpasswd", "..%5cprivate-marker", "%00", strings.Repeat("a", 161)} {
			target := "/admin/v1/logs/" + segment + suffix
			response := f.request(t, http.MethodGet, target, nil, nil, cookie)
			nativeObservabilityError(t, response, http.StatusBadRequest, "invalid_input")
			if strings.Contains(response.Body.String(), "private-marker") || strings.Contains(response.Body.String(), "/etc/passwd") {
				t.Fatal("invalid diagnostic filename was reflected in its error")
			}
		}
	}
	page, err := store.List(f.ctx, diagnostics.ListOptions{Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("resolve the owned diagnostic file before substitution")
	}
	name := page.Items[0].Name
	privatePath := filepath.Join(t.TempDir(), "private-target.txt")
	const secret = "substituted-diagnostic-private-marker"
	if err := os.WriteFile(privatePath, []byte(secret), 0o600); err != nil {
		t.Fatal("write the private substitution fixture")
	}
	registeredPath := filepath.Join(directory, name)
	if err := os.Remove(registeredPath); err != nil {
		t.Fatal("remove the owned registered directory entry")
	}
	if err := os.Symlink(privatePath, registeredPath); err != nil {
		t.Fatal("substitute the owned registered entry with a symlink")
	}
	for _, route := range []string{"/admin/v1/logs/" + name + "/download", "/admin/v1/logs/" + name + "/lines", "/admin/v1/logs"} {
		response := f.request(t, http.MethodGet, route, nil, nil, cookie)
		nativeObservabilityError(t, response, http.StatusServiceUnavailable, "diagnostics_unavailable")
		if strings.Contains(response.Body.String(), secret) || strings.Contains(response.Body.String(), privatePath) || strings.Contains(response.Body.String(), directory) {
			t.Fatal("substituted diagnostic entry exposed external content or a filesystem path")
		}
	}
}

func TestHTTPNativeLogsUnavailableStoreFailsClosed(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	f.app.diagnostics = nil
	routes := []string{"/admin/v1/logs", "/admin/v1/logs/missing.jsonl/lines", "/admin/v1/logs/missing.jsonl/download"}
	for _, route := range routes {
		nativeObservabilityError(t, f.request(t, http.MethodGet, route, nil, nil, cookie), http.StatusServiceUnavailable, "diagnostics_unavailable")
	}
	store, _ := nativeDiagnosticStore(t, f)
	if err := store.Close(); err != nil {
		t.Fatal("close the owned diagnostic store")
	}
	for _, route := range routes {
		nativeObservabilityError(t, f.request(t, http.MethodGet, route, nil, nil, cookie), http.StatusServiceUnavailable, "diagnostics_unavailable")
	}
}
