//go:build linux

package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/storagebinding"
)

type rootBindingHTTPFixture struct {
	*serverFixture
	root, registered, libraryID, rootID, adminID string
	cookie                                       *http.Cookie
	csrf                                         string
}

func newRootBindingHTTPFixture(t *testing.T) *rootBindingHTTPFixture {
	t.Helper()
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	if base := os.Getenv("GOTMPDIR"); base != "" {
		if !filepath.IsAbs(base) {
			t.Fatal("GOTMPDIR must be an absolute owned verification directory")
		}
		var err error
		root, err = os.MkdirTemp(base, "goby-root-binding-http-")
		if err != nil {
			t.Fatal("create owned root binding HTTP fixture")
		}
		owned := root
		t.Cleanup(func() {
			if err := os.RemoveAll(owned); err != nil {
				t.Error("remove owned root binding HTTP fixture")
			}
		})
	}
	catalog, err := library.New(f.pool, apiMediaProber{}, []string{root})
	if err != nil {
		t.Fatalf("create root binding HTTP catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.cfg.MediaRoots, f.app.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	fixture := &rootBindingHTTPFixture{serverFixture: f, root: root, registered: filepath.Join(root, "registered")}
	fixture.adminID = f.bootstrap(t)
	fixture.cookie, fixture.csrf = f.adminLogin(t)
	writeAPIMediaFile(t, root, "registered/Preserved.mp4")
	response := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Root binding HTTP", "CollectionType": "movies", "Paths": []string{fixture.registered}, "Scan": false,
	}, http.Header{"X-CSRF-Token": {fixture.csrf}}, fixture.cookie)
	expectStatus(t, response, http.StatusCreated)
	fixture.libraryID = stringValue(t, objectValue(t, jsonObject(t, response), "Library"), "Id")
	roots, total := responseItems(t, f.request(t, http.MethodGet, fixture.rootListPath(), nil, nil, fixture.cookie))
	if total != 1 || len(roots) != 1 {
		t.Fatal("new library did not expose exactly one registered root")
	}
	fixture.rootID = stringValue(t, roots[0], "Id")
	return fixture
}

func (f *rootBindingHTTPFixture) rootListPath() string {
	return "/admin/v1/libraries/" + f.libraryID + "/roots"
}

func (f *rootBindingHTTPFixture) bindingPath() string {
	return f.rootListPath() + "/" + f.rootID + "/binding"
}

func (f *rootBindingHTTPFixture) binding(t *testing.T) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, f.bindingPath(), nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("storage binding observation must not be cached")
	}
	object := jsonObject(t, response)
	if len(object) != 1 {
		t.Fatal("binding response must contain only the Binding envelope")
	}
	return objectValue(t, object, "Binding")
}

func (f *rootBindingHTTPFixture) update(t *testing.T, revision, fingerprint string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPut, f.bindingPath(), map[string]any{
		"Revision": revision, "ObservedFingerprint": fingerprint, "AcknowledgeMissingRemoval": true,
	}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
}

func (f *rootBindingHTTPFixture) scanMedia(t *testing.T) {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/libraries/"+f.libraryID+"/scan", nil,
		http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	jobID := stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
	waitAdminScanHTTPJob(t, f.serverFixture, f.cookie, f.libraryID, jobID, false)
	result, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id, item_id, play_count, is_favorite)
		SELECT $1, id, 7, true FROM items WHERE library_id = $2 AND type = 'Movie'`, f.adminID, f.libraryID)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("seed personal state for exactly one cataloged movie: %v", err)
	}
}

// Authentication activity is deliberately excluded: a rejected binding request
// must preserve all binding, media, personal-data, and scan state.
func (f *rootBindingHTTPFixture) state(t *testing.T) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'roots', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY id), '[]'::jsonb) FROM library_roots r),
		'items', (SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY id), '[]'::jsonb) FROM items i),
		'data', (SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id), '[]'::jsonb) FROM user_item_data d),
		'jobs', (SELECT COALESCE(jsonb_agg(to_jsonb(j) ORDER BY id), '[]'::jsonb) FROM scan_jobs j),
		'audit', (SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id), '[]'::jsonb) FROM activity_entries a
			WHERE action = 'library.root_binding.updated'))::text`).Scan(&state); err != nil {
		t.Fatalf("read binding HTTP state: %v", err)
	}
	return state
}

func (f *rootBindingHTTPFixture) mediaState(t *testing.T) string {
	t.Helper()
	var state string
	if err := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
		'items', (SELECT COALESCE(jsonb_agg(to_jsonb(i) ORDER BY id), '[]'::jsonb) FROM items i),
		'data', (SELECT COALESCE(jsonb_agg(to_jsonb(d) ORDER BY user_id, item_id), '[]'::jsonb) FROM user_item_data d),
		'jobs', (SELECT COALESCE(jsonb_agg(to_jsonb(j) ORDER BY id), '[]'::jsonb) FROM scan_jobs j))::text`).Scan(&state); err != nil {
		t.Fatalf("read preserved media state: %v", err)
	}
	return state
}

func rootBindingHTTPRequireObservation(t *testing.T, binding map[string]any) string {
	t.Helper()
	fingerprint, _ := binding["ObservedFingerprint"].(string)
	if fingerprint == "" && os.Getenv("GOTMPDIR") == "" {
		t.Skip("the temporary filesystem lacks a supported observation; this is not root binding HTTP acceptance")
	}
	if len(fingerprint) != 64 {
		t.Fatal("owned verification storage did not produce a complete root observation")
	}
	return fingerprint
}

func TestHTTPRootBindingLifecyclePreservesLargeRevisionsAndMedia(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET storage_binding = NULL,
		bound_at = NULL, bound_by = NULL WHERE id = $1`, f.rootID); err != nil {
		t.Fatal(err)
	}
	f.scanMedia(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET binding_revision = 9007199254740993 WHERE id = $1`, f.rootID); err != nil {
		t.Fatal(err)
	}
	initial := f.binding(t)
	fingerprint := rootBindingHTTPRequireObservation(t, initial)
	if initial["Status"] != "unbound" || initial["Revision"] != "9007199254740993" || initial["Approved"] != nil ||
		initial["Id"] != f.rootID || initial["LibraryId"] != f.libraryID || initial["Path"] != f.registered ||
		initial["AllowedPath"] != f.root || initial["RelativePath"] != "registered" {
		t.Fatal("unbound detail lost the registered mapping or exact revision")
	}
	listed, total := responseItems(t, f.request(t, http.MethodGet, f.rootListPath(), nil, nil, f.cookie))
	if total != 1 || len(listed) != 1 || len(listed[0]) != 6 || listed[0]["Revision"] != "9007199254740993" {
		t.Fatal("registered root discovery rounded the revision or exposed binding internals")
	}
	before := f.mediaState(t)
	response := f.update(t, "9007199254740993", fingerprint)
	expectStatus(t, response, http.StatusOK)
	bound := objectValue(t, jsonObject(t, response), "Binding")
	if bound["Status"] != "verified" || bound["Revision"] != "9007199254740994" || bound["BoundBy"] != f.adminID ||
		bound["ApprovedFingerprint"] != fingerprint || bound["ObservedFingerprint"] != fingerprint ||
		!reflect.DeepEqual(bound["Approved"], bound["Observed"]) || !reflect.DeepEqual(bound, f.binding(t)) {
		t.Fatal("binding update did not return the committed independently observed approval")
	}
	expectApplicationKeyHTTPUTC(t, stringValue(t, bound, "BoundAt"))
	if f.mediaState(t) != before {
		t.Fatal("binding approval changed catalog media, user data, or scan jobs")
	}
	var previous, revision int64
	var actor, recordedFingerprint string
	if err := f.pool.QueryRow(f.ctx, `SELECT previous_revision, revision, actor_id, observation_fingerprint
		FROM activity_entries WHERE action = 'library.root_binding.updated' AND resource_id = $1`, f.rootID).
		Scan(&previous, &revision, &actor, &recordedFingerprint); err != nil {
		t.Fatal(err)
	}
	if previous != 9007199254740993 || revision != 9007199254740994 || actor != f.adminID || recordedFingerprint != fingerprint {
		t.Fatal("binding audit lost its exact revision, actor, or observed fingerprint")
	}
	var document []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT storage_binding FROM library_roots WHERE id = $1`, f.rootID).Scan(&document); err != nil {
		t.Fatal(err)
	}
	snapshot, err := storagebinding.DecodeSnapshot(document)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{`"Handle":`, `"handle":`, `"storage_binding":`, `"Namespace":`, `"Witness":`,
		base64.StdEncoding.EncodeToString(snapshot.Anchor.Handle), base64.StdEncoding.EncodeToString(snapshot.RegisteredRoot.Handle)} {
		if private != "" && strings.Contains(response.Body.String(), private) {
			t.Fatal("binding HTTP response exposed a storage document or opaque handle")
		}
	}
	state := f.state(t)
	expectAPIError(t, f.update(t, "9007199254740993", fingerprint), http.StatusConflict, "root_binding_conflict", false)
	expectAPIError(t, f.update(t, "9007199254740994", strings.Repeat("0", 64)), http.StatusConflict, "root_binding_conflict", false)
	expectAPIError(t, f.update(t, "9223372036854775808", fingerprint), http.StatusConflict, "root_binding_conflict", false)
	if f.state(t) != state {
		t.Fatal("rejected root binding preconditions changed persistent state")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE library_roots SET binding_revision = 9223372036854775807 WHERE id = $1`, f.rootID); err != nil {
		t.Fatal(err)
	}
	state = f.state(t)
	expectAPIError(t, f.update(t, "9223372036854775807", fingerprint), http.StatusConflict, "root_binding_conflict", false)
	if f.state(t) != state {
		t.Fatal("exhausted revision changed root binding state")
	}
}

func TestHTTPRootBindingReplacementRejectsStaleFingerprintAndRetainsMissingMedia(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	initial := f.binding(t)
	fingerprint := rootBindingHTTPRequireObservation(t, initial)
	if initial["Status"] != "verified" || initial["Revision"] != "1" || initial["ApprovedFingerprint"] != fingerprint ||
		!reflect.DeepEqual(initial["Approved"], initial["Observed"]) {
		t.Fatal("library registration did not approve the independently observed root")
	}
	f.scanMedia(t)
	mediaBefore := f.mediaState(t)
	retained := filepath.Join(f.root, "retained-original")
	if err := os.Rename(f.registered, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.registered, 0o700); err != nil {
		t.Fatal(err)
	}
	replacement := f.binding(t)
	current := rootBindingHTTPRequireObservation(t, replacement)
	if replacement["Status"] != "mismatch" || replacement["ApprovedFingerprint"] != fingerprint || current == fingerprint {
		t.Fatal("same-path replacement was not shown as an approval mismatch")
	}
	state := f.state(t)
	expectAPIError(t, f.update(t, "1", fingerprint), http.StatusConflict, "root_binding_conflict", false)
	if f.state(t) != state {
		t.Fatal("stale physical observation was persisted")
	}
	if err := os.Remove(f.registered); err != nil {
		t.Fatal(err)
	}
	unavailable := f.binding(t)
	if unavailable["Status"] != "unavailable" || unavailable["ApprovedFingerprint"] != fingerprint ||
		unavailable["ObservedFingerprint"] != nil || unavailable["Observed"] != nil {
		t.Fatal("missing named root leaked a partial observation or lost its approval")
	}
	response := f.update(t, "1", current)
	expectAPIError(t, response, http.StatusServiceUnavailable, "library_unavailable", false)
	if strings.Contains(response.Body.String(), f.root) || f.state(t) != state {
		t.Fatal("unavailable update leaked a filesystem path or changed persistent state")
	}
	if err := os.Rename(retained, f.registered); err != nil {
		t.Fatal(err)
	}
	if restored := f.binding(t); restored["Status"] != "verified" || restored["Revision"] != "1" {
		t.Fatal("returning the original storage required an unnecessary binding mutation")
	}
	if err := os.Rename(f.registered, retained); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.registered, 0o700); err != nil {
		t.Fatal(err)
	}
	fresh := f.binding(t)
	response = f.update(t, "1", rootBindingHTTPRequireObservation(t, fresh))
	expectStatus(t, response, http.StatusOK)
	accepted := objectValue(t, jsonObject(t, response), "Binding")
	if accepted["Status"] != "verified" || accepted["Revision"] != "2" ||
		accepted["ApprovedFingerprint"] != fresh["ObservedFingerprint"] || f.mediaState(t) != mediaBefore {
		t.Fatal("accepting replacement storage changed retained media, personal data, or scan jobs")
	}
}

func TestHTTPRootBindingRejectsMismatchedLibraryAndStrictBody(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	other := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Other root binding scope", "CollectionType": "movies", "Paths": []string{f.registered}, "Scan": false,
	}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, other, http.StatusCreated)
	otherID := stringValue(t, objectValue(t, jsonObject(t, other), "Library"), "Id")
	mismatched := "/admin/v1/libraries/" + otherID + "/roots/" + f.rootID + "/binding"
	body := map[string]any{"Revision": "1", "ObservedFingerprint": strings.Repeat("a", 64), "AcknowledgeMissingRemoval": true}
	state := f.state(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		response := f.request(t, method, mismatched, body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
		expectAPIError(t, response, http.StatusNotFound, "not_found", false)
		if strings.Contains(response.Body.String(), f.registered) {
			t.Fatal("foreign library scope exposed the registered path")
		}
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/admin/v1/libraries/missing-library/roots", nil, nil, f.cookie), http.StatusNotFound, "not_found", false)
	valid, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	validJSON := string(valid)
	for _, test := range []struct {
		name, body, query, contentType string
		status                         int
	}{
		{name: "missing fields", body: `{}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "null object", body: `null`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "numeric revision", body: strings.Replace(validJSON, `"Revision":"1"`, `"Revision":1`, 1), contentType: "application/json", status: http.StatusBadRequest},
		{name: "null revision", body: strings.Replace(validJSON, `"Revision":"1"`, `"Revision":null`, 1), contentType: "application/json", status: http.StatusBadRequest},
		{name: "false acknowledgement", body: strings.Replace(validJSON, ":true", ":false", 1), contentType: "application/json", status: http.StatusBadRequest},
		{name: "wrong case", body: strings.Replace(validJSON, "Revision", "revision", 1), contentType: "application/json", status: http.StatusBadRequest},
		{name: "duplicate field", body: strings.TrimSuffix(validJSON, "}") + `,"Revision":"1"}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "caller identity", body: strings.TrimSuffix(validJSON, "}") + `,"Identity":{"Handle":"private-forged-handle"}}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "trailing JSON", body: validJSON + `{}`, contentType: "application/json", status: http.StatusBadRequest},
		{name: "oversized", body: validJSON + strings.Repeat(" ", 4097), contentType: "application/json", status: http.StatusBadRequest},
		{name: "query", body: validJSON, query: "?Revision=1", contentType: "application/json", status: http.StatusBadRequest},
		{name: "wrong media type", body: validJSON, contentType: "text/plain", status: http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, f.bindingPath()+test.query, strings.NewReader(test.body)).WithContext(f.ctx)
			request.Header.Set("Content-Type", test.contentType)
			request.Header.Set("X-CSRF-Token", f.csrf)
			request.AddCookie(f.cookie)
			response := httptest.NewRecorder()
			f.handler.ServeHTTP(response, request)
			code := "invalid_input"
			if test.status == http.StatusUnsupportedMediaType {
				code = "unsupported_media_type"
			}
			expectAPIError(t, response, test.status, code, false)
			if strings.Contains(response.Body.String(), "private-forged-handle") || strings.Contains(response.Body.String(), f.root) {
				t.Fatal("strict body error reflected a rejected identity or filesystem path")
			}
		})
	}
	for _, path := range []string{f.rootListPath(), f.bindingPath()} {
		expectAPIError(t, f.request(t, http.MethodGet, path+"?UserId=other", nil, nil, f.cookie), http.StatusBadRequest, "invalid_input", false)
	}
	if f.state(t) != state {
		t.Fatal("invalid root binding HTTP requests changed persistent state")
	}
}

func TestHTTPRootBindingDiscoveryIsBoundedWithoutOpeningRegisteredPaths(t *testing.T) {
	f := newRootBindingHTTPFixture(t)
	if err := os.Rename(f.registered, filepath.Join(f.root, "retained-original")); err != nil {
		t.Fatal(err)
	}
	const insertRoots = `INSERT INTO library_roots(id, library_id, path, allowed_path, relative_path)
		SELECT 'bounded-root-' || n, $1, $2 || '/absent-' || n, $2, 'absent-' || n
		FROM generate_series($3::integer, $4::integer) n`
	if _, err := f.pool.Exec(f.ctx, insertRoots, f.libraryID, f.root, 1, 4095); err != nil {
		t.Fatal(err)
	}
	response := f.request(t, http.MethodGet, f.rootListPath(), nil, nil, f.cookie)
	items, total := responseItems(t, response)
	if total != 4096 || len(items) != 4096 || len(jsonObject(t, response)) != 2 {
		t.Fatal("bounded root discovery lost registered metadata for unavailable paths")
	}
	if _, err := f.pool.Exec(f.ctx, insertRoots, f.libraryID, f.root, 4096, 4096); err != nil {
		t.Fatal(err)
	}
	before := f.state(t)
	response = f.request(t, http.MethodGet, f.rootListPath(), nil, nil, f.cookie)
	expectAPIError(t, response, http.StatusServiceUnavailable, "library_unavailable", false)
	if strings.Contains(response.Body.String(), f.root) || f.state(t) != before {
		t.Fatal("oversized root discovery returned a partial list, leaked paths, or changed persisted state")
	}
}
