//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"golang.org/x/sys/unix"
)

const libraryStorageDirectoriesHTTPPath = "/admin/v1/storage/library-directories"

type libraryStorageDirectoriesHTTPFixture struct {
	*serverFixture
	root, registered string
	cookie           *http.Cookie
	csrf             string
}

func newLibraryStorageDirectoriesHTTPFixture(t *testing.T, hook func(context.Context, *os.File) error) *libraryStorageDirectoriesHTTPFixture {
	t.Helper()
	f := newServerFixture(t)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	registered := filepath.Join(root, "registered")
	if err := os.Mkdir(registered, 0o700); err != nil {
		t.Fatal(err)
	}
	var options []library.Option
	if hook != nil {
		options = append(options, library.WithLibraryStorageDirectoryObservationHook(hook))
	}
	catalog, err := library.New(f.pool, apiMediaProber{}, []string{root}, options...)
	if err != nil {
		t.Fatalf("create directory capacity HTTP catalog: %v", err)
	}
	installFixtureCatalog(t, f, catalog)
	f.cfg.MediaRoots, f.app.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	return &libraryStorageDirectoriesHTTPFixture{serverFixture: f, root: root, registered: registered, cookie: cookie, csrf: csrf}
}

func (f *libraryStorageDirectoriesHTTPFixture) addLibrary(t *testing.T, name string, paths ...string) string {
	t.Helper()
	response := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": name, "CollectionType": "movies", "Paths": paths, "Scan": false,
	}, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusCreated)
	return stringValue(t, objectValue(t, jsonObject(t, response), "Library"), "Id")
}

func libraryStorageDirectoryHTTPItems(t *testing.T, response *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private directory capacity response may be cached")
	}
	body := jsonObject(t, response)
	items, ok := body["Items"].([]any)
	if !ok || len(body) != 2 || body["TotalRecordCount"] != float64(len(items)) {
		t.Fatalf("directory capacity response changed its bounded public envelope: %#v", body)
	}
	result := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || len(item) != 7 {
			t.Fatalf("directory capacity item exposed an unexpected projection: %#v", raw)
		}
		for _, field := range []string{"Path", "Available", "Readable", "Writable", "TotalBytes", "UsedBytes", "AvailableBytes"} {
			if _, present := item[field]; !present {
				t.Fatalf("directory capacity item omitted %s: %#v", field, item)
			}
		}
		result = append(result, item)
	}
	return result
}

func requireLibraryDirectoryUnknownCapacity(t *testing.T, item map[string]any) {
	t.Helper()
	for _, field := range []string{"TotalBytes", "UsedBytes", "AvailableBytes"} {
		if item[field] != nil {
			t.Fatalf("uncertain directory invented %s: %#v", field, item)
		}
	}
}

func requireLibraryDirectoryByteString(t *testing.T, item map[string]any, field string) uint64 {
	t.Helper()
	text, ok := item[field].(string)
	value, err := strconv.ParseUint(text, 10, 64)
	if !ok || err != nil || strconv.FormatUint(value, 10) != text {
		t.Fatalf("%s is not a canonical exact unsigned decimal string: %#v", field, item[field])
	}
	return value
}

func TestHTTPStorageLibraryDirectoriesUsesRegisteredPathsAndExactDeduplication(t *testing.T) {
	var calls atomic.Int32
	observed := make(chan os.FileInfo, 4)
	f := newLibraryStorageDirectoriesHTTPFixture(t, func(_ context.Context, directory *os.File) error {
		info, err := directory.Stat()
		if err == nil {
			calls.Add(1)
			observed <- info
		}
		return err
	})
	empty := libraryStorageDirectoryHTTPItems(t, f.request(t, http.MethodGet, libraryStorageDirectoriesHTTPPath, nil, nil, f.cookie))
	if len(empty) != 0 || calls.Load() != 0 {
		t.Fatal("unregistered allowed directories were observed as library directories")
	}
	f.addLibrary(t, "Shared directory first registration", f.registered)
	f.addLibrary(t, "Shared directory second registration", f.registered)
	items := libraryStorageDirectoryHTTPItems(t, f.request(t, http.MethodGet, libraryStorageDirectoriesHTTPPath, nil, nil, f.cookie))
	if len(items) != 1 || items[0]["Path"] != f.registered || items[0]["Available"] != true || items[0]["Readable"] != true || calls.Load() != 1 {
		t.Fatalf("registered directory was not observed exactly once across libraries: %#v calls=%d", items, calls.Load())
	}
	var actual os.FileInfo
	select {
	case actual = <-observed:
	default:
		t.Fatal("registered directory descriptor was not observed")
	}
	expected, err := os.Stat(f.registered)
	if err != nil || !os.SameFile(actual, expected) {
		t.Fatalf("capacity observer received the allowed parent instead of the registered directory: %v", err)
	}
	total := requireLibraryDirectoryByteString(t, items[0], "TotalBytes")
	used := requireLibraryDirectoryByteString(t, items[0], "UsedBytes")
	available := requireLibraryDirectoryByteString(t, items[0], "AvailableBytes")
	var native unix.Statfs_t
	if err := unix.Statfs(f.registered, &native); err != nil {
		t.Fatal(err)
	}
	if total == 0 || total != native.Blocks*uint64(native.Bsize) || used > total || available > total {
		t.Fatalf("directory capacity lost real filesystem bounds: %#v", items[0])
	}
	entries, err := os.ReadDir(f.registered)
	if err != nil || len(entries) != 0 {
		t.Fatalf("directory capacity observation wrote a media probe: entries=%d err=%v", len(entries), err)
	}
	rootsResponse := f.request(t, http.MethodGet, "/admin/v1/storage/roots", nil, nil, f.cookie)
	expectStatus(t, rootsResponse, http.StatusOK)
	roots := jsonObject(t, rootsResponse)["Items"].([]any)
	if len(roots) != 1 {
		t.Fatal("library capacity changed the allowed roots list")
	}
	root := roots[0].(map[string]any)
	if len(root) != 4 || root["Path"] != f.root || root["Available"] != true || root["Readable"] != true {
		t.Fatalf("library capacity changed the four-field allowed roots contract: %#v", root)
	}
}

func TestHTTPStorageLibraryDirectoriesRequiresNativeAdministratorAndClosedQuery(t *testing.T) {
	var calls atomic.Int32
	f := newLibraryStorageDirectoriesHTTPFixture(t, func(context.Context, *os.File) error {
		calls.Add(1)
		return nil
	})
	f.addLibrary(t, "Private library directory", f.registered)
	embyToken := stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")
	for _, credential := range []struct {
		name    string
		headers http.Header
		cookie  *http.Cookie
	}{
		{name: "missing"},
		{name: "compatibility header", headers: http.Header{"X-Emby-Token": {embyToken}}},
		{name: "compatibility cookie", cookie: &http.Cookie{Name: sessionCookie, Value: embyToken}},
		{name: "native token header", headers: http.Header{"X-Emby-Token": {f.cookie.Value}}},
	} {
		t.Run(credential.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if credential.cookie != nil {
				cookies = append(cookies, credential.cookie)
			}
			response := f.request(t, http.MethodGet, libraryStorageDirectoriesHTTPPath, nil, credential.headers, cookies...)
			expectStatus(t, response, http.StatusUnauthorized)
			if strings.Contains(response.Body.String(), f.root) || strings.Contains(response.Body.String(), "TotalBytes") {
				t.Fatal("denied directory capacity response exposed storage observations")
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("non-native credentials reached directory filesystem work")
	}
	for _, suffix := range []string{"?", "?Path=/private-marker", "?StartIndex=0", "?Limit=1", "?api_key=private-marker"} {
		response := f.request(t, http.MethodGet, libraryStorageDirectoriesHTTPPath+suffix, nil, nil, f.cookie)
		expectAPIError(t, response, http.StatusBadRequest, "invalid_query", false)
	}
	if calls.Load() != 0 {
		t.Fatal("caller-selected query reached directory filesystem work")
	}
	libraryStorageDirectoryHTTPItems(t, f.request(t, http.MethodGet, libraryStorageDirectoriesHTTPPath, nil, nil, f.cookie))
	if calls.Load() != 1 {
		t.Fatal("native administrator did not observe the registered directory")
	}
}

type libraryDirectoryHTTPObservationGate struct {
	entered chan *os.File
	release chan struct{}
	first   sync.Once
	opened  sync.Once
}

func newLibraryDirectoryHTTPObservationGate() *libraryDirectoryHTTPObservationGate {
	return &libraryDirectoryHTTPObservationGate{entered: make(chan *os.File, 1), release: make(chan struct{})}
}

func (gate *libraryDirectoryHTTPObservationGate) observe(_ context.Context, directory *os.File) error {
	gate.first.Do(func() {
		gate.entered <- directory
		<-gate.release
	})
	return nil
}

func (gate *libraryDirectoryHTTPObservationGate) open() {
	gate.opened.Do(func() { close(gate.release) })
}

func (gate *libraryDirectoryHTTPObservationGate) wait(t *testing.T) *os.File {
	t.Helper()
	select {
	case directory := <-gate.entered:
		return directory
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP capacity request did not reach its held directory observation")
		return nil
	}
}

func (f *libraryStorageDirectoriesHTTPFixture) startObservation() <-chan *httptest.ResponseRecorder {
	finished := make(chan *httptest.ResponseRecorder, 1)
	request := httptest.NewRequest(http.MethodGet, libraryStorageDirectoriesHTTPPath, nil).WithContext(f.ctx)
	request.AddCookie(f.cookie)
	response := httptest.NewRecorder()
	go func() {
		f.handler.ServeHTTP(response, request)
		finished <- response
	}()
	return finished
}

func awaitLibraryDirectoryHTTPObservation(t *testing.T, finished <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case response := <-finished:
		return response
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP capacity response did not complete after the held observation")
		return nil
	}
}

func TestHTTPStorageLibraryDirectoriesRejectsCapacityFromChangedRootRevision(t *testing.T) {
	gate := newLibraryDirectoryHTTPObservationGate()
	f := newLibraryStorageDirectoriesHTTPFixture(t, gate.observe)
	t.Cleanup(gate.open)
	libraryID := f.addLibrary(t, "Root revision capacity race", f.registered)
	finished := f.startObservation()
	gate.wait(t)
	result, err := f.pool.Exec(f.ctx, "UPDATE library_roots SET binding_revision=binding_revision+1 WHERE library_id=$1", libraryID)
	if err != nil || result.RowsAffected() != 1 {
		t.Fatalf("change registered root revision while the observation owns its descriptor: %v", err)
	}
	gate.open()
	items := libraryStorageDirectoryHTTPItems(t, awaitLibraryDirectoryHTTPObservation(t, finished))
	if len(items) != 1 || items[0]["Path"] != f.registered {
		t.Fatalf("root revision race lost its exact directory identity: %#v", items)
	}
	requireLibraryDirectoryUnknownCapacity(t, items[0])
}

func TestHTTPStorageLibraryDirectoriesReconcilesPathsAfterConcurrentHTTPCAS(t *testing.T) {
	gate := newLibraryDirectoryHTTPObservationGate()
	f := newLibraryStorageDirectoriesHTTPFixture(t, gate.observe)
	t.Cleanup(gate.open)
	libraryID := f.addLibrary(t, "Directory capacity and library edit race", f.registered)
	endpoint := "/admin/v1/libraries/" + libraryID
	detailResponse := f.request(t, http.MethodGet, endpoint, nil, nil, f.cookie)
	expectStatus(t, detailResponse, http.StatusOK)
	revision := stringValue(t, objectValue(t, jsonObject(t, detailResponse), "Library"), "Revision")
	second := filepath.Join(f.root, "second")
	if err := os.Mkdir(second, 0o700); err != nil {
		t.Fatal(err)
	}
	finished := f.startObservation()
	gate.wait(t)
	update := map[string]any{"Revision": revision, "Paths": []string{second}, "AcknowledgePathRemoval": true}
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, update, headers, f.cookie), http.StatusOK)
	expectStatus(t, f.request(t, http.MethodPatch, endpoint, update, headers, f.cookie), http.StatusConflict)
	gate.open()
	items := libraryStorageDirectoryHTTPItems(t, awaitLibraryDirectoryHTTPObservation(t, finished))
	if len(items) != 1 || items[0]["Path"] != second {
		t.Fatalf("concurrent library CAS published the removed path or dropped its replacement: %#v", items)
	}
	requireLibraryDirectoryUnknownCapacity(t, items[0])
}

func TestHTTPStorageLibraryDirectoriesRechecksAdministratorAfterObservation(t *testing.T) {
	for _, authority := range []string{"revoked", "demoted"} {
		t.Run(authority, func(t *testing.T) {
			gate := newLibraryDirectoryHTTPObservationGate()
			f := newLibraryStorageDirectoriesHTTPFixture(t, gate.observe)
			t.Cleanup(gate.open)
			f.addLibrary(t, "Capacity authority race", f.registered)
			actor, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
			if err != nil {
				t.Fatal("resolve native capacity fixture administrator")
			}
			finished := f.startObservation()
			gate.wait(t)
			if authority == "revoked" {
				_, err = f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID)
			} else {
				_, err = f.pool.Exec(f.ctx, "UPDATE users SET is_administrator=false WHERE id=$1", actor.User.ID)
			}
			if err != nil {
				t.Fatal("invalidate capacity fixture administrator during filesystem observation")
			}
			gate.open()
			response := awaitLibraryDirectoryHTTPObservation(t, finished)
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
				t.Fatalf("directory capacity returned after authority changed: %d %s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), f.root) || strings.Contains(response.Body.String(), "TotalBytes") || strings.Contains(response.Body.String(), "Items") {
				t.Fatal("revoked directory capacity request exposed paths or observations")
			}
		})
	}
}

func TestHTTPStorageLibraryDirectoriesRejectsNamedDirectoryReplacement(t *testing.T) {
	gate := newLibraryDirectoryHTTPObservationGate()
	f := newLibraryStorageDirectoriesHTTPFixture(t, gate.observe)
	t.Cleanup(gate.open)
	f.addLibrary(t, "Named directory capacity race", f.registered)
	finished := f.startObservation()
	directory := gate.wait(t)
	if err := os.Rename(f.registered, f.registered+"-previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(f.registered, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := directory.Stat(); err != nil {
		t.Fatalf("directory replacement closed the observation's held descriptor: %v", err)
	}
	gate.open()
	items := libraryStorageDirectoryHTTPItems(t, awaitLibraryDirectoryHTTPObservation(t, finished))
	if len(items) != 1 || items[0]["Path"] != f.registered {
		t.Fatalf("directory replacement lost the exact registered path: %#v", items)
	}
	requireLibraryDirectoryUnknownCapacity(t, items[0])
}

func TestHTTPStorageLibraryDirectoriesKeepsObservationFailuresUnknown(t *testing.T) {
	for _, cause := range []struct {
		name string
		err  error
	}{
		{name: "permission denied", err: os.ErrPermission},
		{name: "uncertain filesystem failure", err: errors.New("private-capacity-failure-marker")},
	} {
		t.Run(cause.name, func(t *testing.T) {
			f := newLibraryStorageDirectoriesHTTPFixture(t, func(context.Context, *os.File) error { return cause.err })
			f.addLibrary(t, "Unknown directory capacity", f.registered)
			response := f.request(t, http.MethodGet, libraryStorageDirectoriesHTTPPath, nil, nil, f.cookie)
			items := libraryStorageDirectoryHTTPItems(t, response)
			if len(items) != 1 || items[0]["Path"] != f.registered {
				t.Fatalf("failed directory observation disappeared from the response: %#v", items)
			}
			requireLibraryDirectoryUnknownCapacity(t, items[0])
			if strings.Contains(response.Body.String(), "private-capacity-failure-marker") {
				t.Fatal("directory capacity response exposed an internal filesystem error")
			}
		})
	}
}
