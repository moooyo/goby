//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/diagnostics"
)

const embyObservabilityDateLayout = "2006-01-02T15:04:05.0000000Z"

type embyObservabilityHTTPFixture struct {
	*applicationKeyHTTPFixture
	adminHeaders, keyHeaders, viewerHeaders http.Header
	adminToken, keyToken, viewerToken       string
	viewerName, name, directory             string
	store                                   *diagnostics.Store
	content                                 []byte
}

func embyObservabilityClientHeaders(token string) http.Header {
	return http.Header{
		"X-Emby-Token": {token}, "X-Emby-Client": {"Observability fixture"}, "X-Emby-Device-Id": {"observability-client"},
		"X-Emby-Device-Name": {"Linux"}, "X-Emby-Client-Version": {"1.0"},
	}
}

func newEmbyObservabilityHTTPFixture(t *testing.T) *embyObservabilityHTTPFixture {
	t.Helper()
	f := &embyObservabilityHTTPFixture{applicationKeyHTTPFixture: newApplicationKeyHTTPFixture(t), viewerName: "Observability Viewer"}
	if _, err := f.users.CreateUser(f.ctx, f.viewerName, "observability-viewer-password", false); err != nil {
		t.Fatal("create the owned observability viewer")
	}
	f.adminToken = stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	f.viewerToken = stringValue(t, f.embyLogin(t, f.viewerName, "observability-viewer-password"), "AccessToken")
	f.keyToken = f.create(t, "Observability complete application").token
	f.adminHeaders = embyObservabilityClientHeaders(f.adminToken)
	f.viewerHeaders = embyObservabilityClientHeaders(f.viewerToken)
	f.keyHeaders = embyObservabilityClientHeaders(f.keyToken)
	f.store, f.directory = nativeDiagnosticStore(t, f.serverFixture)
	f.content = nativeDiagnosticRecords(t, f.serverFixture, f.store)
	page, err := f.store.List(f.ctx, diagnostics.ListOptions{Limit: 1})
	if err != nil || len(page.Items) != 1 {
		t.Fatal("read the owned diagnostic filename")
	}
	f.name = page.Items[0].Name
	return f
}

func embyObservabilityHTTPPage(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	return nativeObservabilityObject(t, response, "Items", "TotalRecordCount")
}

func embyObservabilityUTCDate(t *testing.T, item map[string]any, field string) time.Time {
	t.Helper()
	value := stringValue(t, item, field)
	stamp, err := time.Parse(embyObservabilityDateLayout, value)
	if err != nil || stamp.UTC().Format(embyObservabilityDateLayout) != value {
		t.Fatalf("Emby %s must use the captured UTC date shape with seven fractional digits", field)
	}
	return stamp
}

func embyObservabilityFailure(t *testing.T, response *httptest.ResponseRecorder, status int) []byte {
	t.Helper()
	message := ""
	switch status {
	case http.StatusBadRequest:
		message = "Supply valid activity or diagnostic query fields."
	case http.StatusNotFound:
		message = "The requested diagnostic file was not found."
	case http.StatusServiceUnavailable:
		message = "Diagnostic logs are currently unavailable."
	default:
		t.Fatal("unsupported observability error assertion")
	}
	expectEmbyTextError(t, response, status, message)
	nativeObservabilityNoCache(t, response)
	if response.Body.Len() == 0 {
		t.Fatal("an Emby observability GET error omitted its bounded error body")
	}
	if response.Body.Len() > 1024 {
		t.Fatal("an Emby observability error exposed an unbounded diagnostic")
	}
	return append([]byte(nil), response.Body.Bytes()...)
}

func TestHTTPEmbyObservabilityUsesCapturedAuthenticationAndRejectsNativeCookies(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	routes := []string{
		"/emby/System/ActivityLog/Entries",
		"/emby/System/Logs/Query",
		"/emby/System/Logs/" + f.name,
		"/emby/System/Logs/" + f.name + "/Lines",
	}
	for _, route := range routes {
		for _, request := range []struct {
			headers http.Header
			cookies []*http.Cookie
		}{
			{},
			{headers: embyObservabilityClientHeaders("invalid-observability-private-token")},
			{cookies: []*http.Cookie{f.cookie}},
			{headers: embyObservabilityClientHeaders(f.cookie.Value)},
		} {
			response := f.request(t, http.MethodGet, route, nil, request.headers, request.cookies...)
			expectEmbyTextError(t, response, http.StatusUnauthorized, embyInvalidTokenMessage)
			nativeObservabilityNoCache(t, response)
		}
		for _, suffix := range []string{"", "?Limit=private-query-marker"} {
			response := f.request(t, http.MethodGet, route+suffix, nil, f.viewerHeaders)
			expectEmbyTextError(t, response, http.StatusForbidden,
				"User "+f.viewerName+" does not have access to ManageServer feature.")
			nativeObservabilityNoCache(t, response)
		}
		for _, headers := range []http.Header{f.adminHeaders, f.keyHeaders} {
			response := f.request(t, http.MethodGet, route, nil, headers)
			expectStatus(t, response, http.StatusOK)
			nativeObservabilityNoCache(t, response)
		}
		queryHeaders := f.keyHeaders.Clone()
		queryHeaders.Del("X-Emby-Token")
		queryToken := f.request(t, http.MethodGet, route+"?api_key="+url.QueryEscape(f.keyToken), nil, queryHeaders)
		expectStatus(t, queryToken, http.StatusOK)
		nativeObservabilityNoCache(t, queryToken)
		unauthorized := f.request(t, http.MethodGet, route+"?Limit=private-query-marker", nil, nil)
		expectEmbyTextError(t, unauthorized, http.StatusUnauthorized, embyInvalidTokenMessage)
	}
	for _, route := range routes {
		for _, headers := range []http.Header{nil, f.adminHeaders, f.keyHeaders, f.viewerHeaders} {
			response := f.request(t, http.MethodHead, route, nil, headers)
			expectStatus(t, response, http.StatusNotFound)
			if response.Body.Len() != 0 || response.Header().Get("Content-Type") != "text/plain" ||
				response.Header().Get("Content-Length") != strconv.Itoa(len("The requested operation was not found.")) {
				t.Fatal("the unsupported Emby HEAD adapter returned an unexpected body or error representation")
			}
			nativeObservabilityNoCache(t, response)
		}
	}
}

func TestHTTPEmbyActivityProjectsRealChangesAndCapturedPaging(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	const privateSetting = "Private Emby activity server name"
	expectStatus(t, adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate("1", privateSetting, nil)), http.StatusOK)
	createdID := managedHTTPCreate(t, f.serverFixture, f.cookie, f.csrf, "Private Activity Subject", false)
	const route = "/emby/System/ActivityLog/Entries"
	read := func(query string, headers http.Header) map[string]any {
		t.Helper()
		return embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, route+query, nil, headers))
	}
	baseline := read("?Limit=200", f.adminHeaders)
	items := nativeObservabilityItems(t, baseline)
	if len(items) < 2 || baseline["TotalRecordCount"] != float64(len(items)) {
		t.Fatal("the activity fixture did not return its complete real history")
	}
	var previousDate time.Time
	var previousID float64
	settingsFound, authenticatedFound := false, false
	for index, raw := range items {
		item := nativeObservabilityItem(t, raw)
		nativeObservabilityFields(t, item, "Id", "Name", "Overview", "Type", "Date", "UserId", "Severity")
		id, ok := item["Id"].(float64)
		if !ok || id < 1 || id != float64(int64(id)) {
			t.Fatal("Emby activity IDs must remain exact positive JSON integers")
		}
		date := embyObservabilityUTCDate(t, item, "Date")
		if index > 0 && (date.After(previousDate) || date.Equal(previousDate) && id >= previousID) {
			t.Fatal("Emby activity is not ordered by descending date and ID")
		}
		previousDate, previousID = date, id
		if _, ok := item["UserId"].(string); !ok {
			t.Fatal("Emby activity UserId must remain a string")
		}
		if item["Type"] == "session.login" {
			t.Fatal("Emby activity did not apply the observed ordinary login type mapping")
		}
		if item["Type"] == "user.authenticated" {
			authenticatedFound = true
		}
		if item["Type"] == "settings.updated" {
			settingsFound = true
			if item["UserId"] != f.adminID || item["Name"] != "Server settings updated" || item["Overview"] != "Supported server settings were updated." || item["Severity"] != "Info" {
				t.Fatal("real settings activity did not preserve its safe description and associated actor")
			}
		}
	}
	latest := nativeObservabilityItem(t, items[0])
	if !settingsFound || !authenticatedFound || latest["Type"] != "user.created" || latest["UserId"] != createdID || latest["Name"] != "User created" || latest["Overview"] != "A user account was created." {
		t.Fatal("real user creation did not identify its created user through the captured seven-field DTO")
	}
	encoded, err := json.Marshal(baseline)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{privateSetting, "Private Activity Subject", f.cookie.Value, f.csrf, f.adminToken, f.keyToken, f.viewerToken, f.cfg.DatabaseURL, "CredentialID", "ChangedFields"} {
		if private != "" && bytes.Contains(encoded, []byte(private)) {
			t.Fatal("Emby activity exposed a credential, request value, or native-only field")
		}
	}
	for _, test := range []struct {
		query        string
		start, count int
		total        float64
	}{
		{"", 0, len(items), 0},
		{"?Limit=1", 0, 1, float64(len(items))},
		{"?StartIndex=1&Limit=1", 1, 1, float64(len(items))},
		{"?StartIndex=-1&Limit=1", 0, 1, float64(len(items))},
		{"?Limit=0", 0, 0, float64(len(items))},
		{"?Limit=-1", 0, 0, float64(len(items))},
		{"?StartIndex=2147483647&Limit=1", 0, 0, 0},
	} {
		page := read(test.query, f.adminHeaders)
		actual := nativeObservabilityItems(t, page)
		if len(actual) != test.count || page["TotalRecordCount"] != test.total || !reflect.DeepEqual(actual, items[test.start:test.start+test.count]) {
			t.Fatalf("Emby activity paging differs from the captured semantics for %s", test.query)
		}
	}
	if keyPage := read("?Limit=200", f.keyHeaders); !reflect.DeepEqual(keyPage, baseline) {
		t.Fatal("a complete application key did not receive the same administrative activity projection")
	}
	latestDate := embyObservabilityUTCDate(t, latest, "Date")
	for _, stamp := range []string{latestDate.Format(embyObservabilityDateLayout), latestDate.In(time.FixedZone("UTC+8", 8*60*60)).Format(time.RFC3339Nano)} {
		page := read("?Limit=1&MinDate="+url.QueryEscape(stamp), f.adminHeaders)
		if page["TotalRecordCount"] != float64(1) || !reflect.DeepEqual(nativeObservabilityItems(t, page), items[:1]) {
			t.Fatal("Emby MinDate did not accept the exact date and its equivalent timezone representation")
		}
	}
}

func TestHTTPEmbyActivityPreservesRawInt64IDsAndOmitsInventedApplicationUsers(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	const previousID int64 = 9007199254740992
	var assigned int64
	if err := f.pool.QueryRow(f.ctx, "SELECT setval(pg_get_serial_sequence('activity_entries', 'id'), $1, true)", previousID).Scan(&assigned); err != nil || assigned != previousID {
		t.Fatal("advance the owned activity sequence beyond the JavaScript integer boundary")
	}
	expectStatus(t, adminSettingsHTTPWrite(t, f.serverFixture, f.cookie, f.csrf, http.MethodPut, "/admin/v1/settings",
		adminSettingsHTTPUpdate("1", "Native precise activity", nil)), http.StatusOK)
	latest := func() map[string]json.RawMessage {
		t.Helper()
		response := f.request(t, http.MethodGet, "/emby/System/ActivityLog/Entries?Limit=1", nil, f.adminHeaders)
		expectStatus(t, response, http.StatusOK)
		nativeObservabilityNoCache(t, response)
		var page struct {
			Items []map[string]json.RawMessage `json:"Items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
			t.Fatal("decode the exact raw activity number token")
		}
		return page.Items[0]
	}
	item := latest()
	if string(item["Id"]) != "9007199254740993" || string(item["UserId"]) != strconv.Quote(f.adminID) || len(item) != 7 {
		t.Fatal("Emby activity rounded its large ID, encoded it as a string, or lost its real user association")
	}
	configuration := f.request(t, http.MethodPost, "/emby/System/Configuration", map[string]any{"ServerName": "Application precise activity"}, f.keyHeaders)
	expectStatus(t, configuration, http.StatusNoContent)
	item = latest()
	if string(item["Id"]) != "9007199254740994" || string(item["Type"]) != `"settings.updated"` || len(item) != 6 {
		t.Fatal("application activity did not retain its real action and exact numeric ID")
	}
	if _, present := item["UserId"]; present {
		t.Fatal("application activity invented a user identity instead of omitting UserId")
	}
}

func TestHTTPEmbyLogsUseCapturedDTOAndBoundedSanitizedContent(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	const queryRoute = "/emby/System/Logs/Query"
	listed := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, queryRoute, nil, f.adminHeaders))
	items := nativeObservabilityItems(t, listed)
	if len(items) != 1 || listed["TotalRecordCount"] != float64(1) {
		t.Fatal("Emby logs did not list the registered owned file")
	}
	file := nativeObservabilityItem(t, items[0])
	nativeObservabilityFields(t, file, "Name", "DateCreated", "DateModified", "Size")
	if file["Name"] != f.name || file["Size"] != float64(len(f.content)) {
		t.Fatal("Emby log metadata did not use its registered filename and numeric size")
	}
	embyObservabilityUTCDate(t, file, "DateCreated")
	embyObservabilityUTCDate(t, file, "DateModified")
	for _, test := range []struct {
		query string
		count int
	}{
		{"?Limit=1", 1}, {"?StartIndex=-1", 1}, {"?Limit=0", 0}, {"?Limit=-1", 0}, {"?StartIndex=2147483647&Limit=1", 0},
	} {
		page := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, queryRoute+test.query, nil, f.adminHeaders))
		actual := nativeObservabilityItems(t, page)
		if len(actual) != test.count || page["TotalRecordCount"] != float64(1) || !reflect.DeepEqual(actual, items[:test.count]) {
			t.Fatal("Emby log paging did not preserve its captured total and empty-page semantics")
		}
	}
	base := "/emby/System/Logs/" + url.PathEscape(f.name)
	lines := strings.Split(strings.TrimSuffix(string(f.content), "\n"), "\n")
	for _, test := range []struct {
		query        string
		start, count int
	}{
		{"", 0, 0}, {"?Limit=0", 0, 0}, {"?Limit=1", 0, 1}, {"?StartIndex=1&Limit=1", 1, 1}, {"?StartIndex=2147483647&Limit=1", 0, 0},
	} {
		page := embyObservabilityHTTPPage(t, f.request(t, http.MethodGet, base+"/Lines"+test.query, nil, f.adminHeaders))
		actual := nativeObservabilityItems(t, page)
		if len(actual) != test.count || page["TotalRecordCount"] != float64(len(lines)) {
			t.Fatal("Emby lines did not preserve its captured default and total semantics")
		}
		for index, line := range actual {
			if line != lines[test.start+index] {
				t.Fatal("Emby lines differ from the sanitized persisted diagnostic snapshot")
			}
		}
	}
	for _, headers := range []http.Header{f.adminHeaders, f.keyHeaders} {
		for _, query := range []string{"", "?Sanitize=false", "?Sanitize=true"} {
			response := f.request(t, http.MethodGet, base+query, nil, headers)
			expectStatus(t, response, http.StatusOK)
			nativeObservabilityNoCache(t, response)
			if response.Header().Get("Content-Type") != "text/plain; charset=UTF-8" || response.Header().Get("Content-Length") != strconv.Itoa(len(f.content)) ||
				response.Header().Get("Accept-Ranges") != "bytes" || !bytes.Equal(response.Body.Bytes(), f.content) {
				t.Fatal("Emby download changed its captured content type or exposed different bytes for a Sanitize variant")
			}
			for _, private := range []string{"diagnostic-private", "/private/library/file.mkv", f.directory, f.master, f.adminToken, f.keyToken} {
				if private != "" && bytes.Contains(response.Body.Bytes(), []byte(private)) {
					t.Fatal("an Emby Sanitize variant exposed a private diagnostic value")
				}
			}
		}
	}
	native := f.request(t, http.MethodGet, "/admin/v1/logs/"+f.name+"/download", nil, nil, f.cookie)
	expectStatus(t, native, http.StatusOK)
	if !bytes.Equal(native.Body.Bytes(), f.content) {
		t.Fatal("native and Emby diagnostics did not share the same registered sanitized snapshot")
	}
	rangeHeaders := f.adminHeaders.Clone()
	rangeHeaders.Set("Range", "bytes=0-15")
	ranged := f.request(t, http.MethodGet, base, nil, rangeHeaders)
	expectStatus(t, ranged, http.StatusPartialContent)
	nativeObservabilityNoCache(t, ranged)
	if !bytes.Equal(ranged.Body.Bytes(), f.content[:16]) || ranged.Header().Get("Content-Range") != fmt.Sprintf("bytes 0-15/%d", len(f.content)) {
		t.Fatal("Emby range delivery did not use the same sanitized snapshot bytes")
	}
}

func TestHTTPEmbyObservabilityRejectsUnsafeQueriesAndUnregisteredFilesWithoutReflection(t *testing.T) {
	f := newEmbyObservabilityHTTPFixture(t)
	for _, route := range []string{"/emby/System/ActivityLog/Entries", "/emby/System/Logs/Query", "/emby/System/Logs/" + f.name + "/Lines"} {
		var previous []byte
		for _, query := range []string{
			"?Limit=private-first", "?Limit=private-second", "?Limit=2147483648", "?Limit=1&Limit=2", "?StartIndex=%FF", "?Unknown=private-first",
			"?Limit=1&limit=2", "?StartIndex=0&startindex=1", "?Limit=1" + strings.Repeat("&", 4097),
		} {
			body := embyObservabilityFailure(t, f.request(t, http.MethodGet, route+query, nil, f.adminHeaders), http.StatusBadRequest)
			if previous != nil && !bytes.Equal(body, previous) {
				t.Fatal("query rejection reflected an input-specific value instead of its fixed safe error")
			}
			previous = body
			for _, private := range []string{"private-first", "private-second", f.adminToken, "SQLSTATE", "System.FormatException"} {
				if bytes.Contains(body, []byte(private)) {
					t.Fatal("Emby query rejection exposed a credential or exception detail")
				}
			}
		}
	}
	for _, route := range []string{"/emby/System/ActivityLog/Entries", "/emby/System/Logs/Query"} {
		embyObservabilityFailure(t, f.request(t, http.MethodGet, route+"?Limit=201", nil, f.adminHeaders), http.StatusBadRequest)
	}
	for _, query := range []string{"?Limit=501", "?Limit=-1", "?StartIndex=-1", "?StartPosition=1", "?SearchTerm=x"} {
		embyObservabilityFailure(t, f.request(t, http.MethodGet, "/emby/System/Logs/"+f.name+"/Lines"+query, nil, f.adminHeaders), http.StatusBadRequest)
	}
	for _, query := range []string{"?MinDate=private-first", "?MinDate=private-second"} {
		embyObservabilityFailure(t, f.request(t, http.MethodGet, "/emby/System/ActivityLog/Entries"+query, nil, f.adminHeaders), http.StatusBadRequest)
	}
	for _, query := range []string{"?Sanitize=private-first", "?Sanitize=private-second", "?Sanitize=true&Sanitize=false", "?Unknown=private-first"} {
		body := embyObservabilityFailure(t, f.request(t, http.MethodGet, "/emby/System/Logs/"+f.name+query, nil, f.adminHeaders), http.StatusBadRequest)
		if bytes.Contains(body, []byte("private-")) || bytes.Contains(body, []byte(f.directory)) {
			t.Fatal("Emby download query rejection reflected an unsafe value")
		}
	}
	for _, suffix := range []string{"", "/Lines"} {
		var previous []byte
		for _, name := range []string{"missing-first-private.jsonl", "missing-second-private.jsonl"} {
			body := embyObservabilityFailure(t, f.request(t, http.MethodGet, "/emby/System/Logs/"+name+suffix, nil, f.adminHeaders), http.StatusNotFound)
			if previous != nil && !bytes.Equal(body, previous) || bytes.Contains(body, []byte(name)) {
				t.Fatal("unknown Emby logs did not return a fixed error without reflecting their names")
			}
			previous = body
		}
		for _, name := range []string{"%2e%2e", "%2fetc%2fpasswd", "..%5cprivate-file", "%00", strings.Repeat("a", 161)} {
			body := embyObservabilityFailure(t, f.request(t, http.MethodGet, "/emby/System/Logs/"+name+suffix, nil, f.adminHeaders), http.StatusBadRequest)
			if bytes.Contains(body, []byte("private-file")) || bytes.Contains(body, []byte("/etc/passwd")) || bytes.Contains(body, []byte(f.directory)) {
				t.Fatal("an invalid Emby log name exposed a request or filesystem value")
			}
		}
	}
	f.app.diagnostics = nil
	for _, route := range []string{"/emby/System/Logs/Query", "/emby/System/Logs/" + f.name, "/emby/System/Logs/" + f.name + "/Lines"} {
		body := embyObservabilityFailure(t, f.request(t, http.MethodGet, route, nil, f.adminHeaders), http.StatusServiceUnavailable)
		if bytes.Contains(body, []byte(f.directory)) || bytes.Contains(body, []byte(f.master)) {
			t.Fatal("an unavailable Emby diagnostics backend exposed internal paths")
		}
	}
}
