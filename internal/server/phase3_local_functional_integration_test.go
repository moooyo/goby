//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/media"
)

// This opt-in scenario uses real PostgreSQL, production Server.New, real
// FFmpeg/ffprobe, scan-evidence spooling, and HTTP over a loopback TLS listener.
// It covers one reconciliation journey; it is not a capacity or fault campaign.
// Set GOBY_PHASE3_FUNCTIONAL=1 and optionally GOBY_PHASE3_FUNCTIONAL_ITEMS=10000.
// The default shape includes 80 non-media entries per directory, matching the
// failed campaign. Set GOBY_PHASE3_FUNCTIONAL_NONMEDIA_PER_DIRECTORY=0 for the
// faster empty-directory control; its result does not cover the loaded shape.
func TestHTTPPhase3LocalFunctionalReconciliation(t *testing.T) {
	if os.Getenv("GOBY_PHASE3_FUNCTIONAL") != "1" {
		t.Skip("set GOBY_PHASE3_FUNCTIONAL=1 for the real-media reconciliation journey")
	}
	for _, name := range []string{"GOBY_TEST_DATABASE_URL", "GOBY_FFMPEG", "GOBY_FFPROBE"} {
		if os.Getenv(name) == "" {
			t.Fatalf("%s is required for the requested functional journey", name)
		}
	}
	count := 20
	if value := os.Getenv("GOBY_PHASE3_FUNCTIONAL_ITEMS"); value != "" {
		var err error
		count, err = strconv.Atoi(value)
		if err != nil || count < 4 || count > 10000 {
			t.Fatal("GOBY_PHASE3_FUNCTIONAL_ITEMS must be between 4 and 10000")
		}
	}
	nonMedia := 80
	if value := os.Getenv("GOBY_PHASE3_FUNCTIONAL_NONMEDIA_PER_DIRECTORY"); value != "" {
		var err error
		nonMedia, err = strconv.Atoi(value)
		if err != nil || nonMedia < 0 || nonMedia > 80 {
			t.Fatal("GOBY_PHASE3_FUNCTIONAL_NONMEDIA_PER_DIRECTORY must be between 0 and 80")
		}
	}
	// The deadline is a hang guard, not a performance acceptance target.
	f := newServerFixtureWithTimeout(t, 2*time.Hour)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatalf("close initial server: %v", err)
	}
	base, err := os.MkdirTemp(os.Getenv("GOTMPDIR"), "goby-phase3-functional-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove functional media fixture: %v", err)
		}
	})
	mediaRoot, evidenceRoot := filepath.Join(base, "media"), filepath.Join(base, "evidence")
	roots := []string{filepath.Join(mediaRoot, "first"), filepath.Join(mediaRoot, "second")}
	const directories = 4200
	nonMediaPadding := strings.Repeat("x", 221)
	for _, root := range append(append([]string{}, roots...), evidenceRoot) {
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < directories; index++ {
		directory := filepath.Join(roots[index%2], fmt.Sprintf("d%04d", index/2))
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		for entry := 0; entry < nonMedia; entry++ {
			// These 237-byte names reproduce the old directory-entry volume.
			name := fmt.Sprintf("ignored-%03d-%s.dat", entry, nonMediaPadding)
			if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("fixture created movies=%d roots=2 directories=%d non_media_entries=%d", count, directories, directories*nonMedia)
	seed := filepath.Join(base, "seed.mp4")
	hlsHTTPMediaCommand(t, os.Getenv("GOBY_FFMPEG"), "-hide_banner", "-nostdin", "-v", "error",
		"-filter_threads", "1", "-f", "lavfi", "-i", "color=c=blue:size=160x90:rate=24:duration=1",
		"-c:v", "libx264", "-threads:v", "1", "-g", "24", "-keyint_min", "24", "-sc_threshold", "0",
		"-bf", "0", "-pix_fmt", "yuv420p", "-t", "1", seed)
	seedData, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	writeMovie := func(path string, number int) {
		t.Helper()
		// A valid MP4 free box gives each copy distinct bytes without changing
		// the actual encoded video or replacing production media probing.
		box := make([]byte, 16)
		binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
		copy(box[4:8], "free")
		binary.BigEndian.PutUint64(box[8:], uint64(number))
		data := append(append([]byte(nil), seedData...), box...)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths := make([]string, count)
	expected := make(map[string]string, count)
	for index := range paths {
		paths[index] = filepath.Join(roots[index%2], fmt.Sprintf("d%04d", (index/2)%(directories/2)), fmt.Sprintf("Movie%05d.mp4", index))
		writeMovie(paths[index], index)
		expected[paths[index]] = ""
	}
	f.cfg.MediaRoots = []string{mediaRoot}
	f.cfg.FFmpegPath, f.cfg.FFprobePath = os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE")
	f.cfg.ScanEvidence = config.ScanEvidenceConfig{Enabled: true, Directory: evidenceRoot, MaxBytes: 1 << 30,
		MaxDirectories: 131072, MaxEntries: 1048576, MaxFallbackHandles: 4096}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "phase3-local-functional")
	if err != nil {
		t.Fatalf("create real-media server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close functional server: %v", err)
		}
	})
	if !app.library.ScanEvidenceStatus().Enabled {
		t.Fatal("production server did not enable scan-evidence spooling")
	}
	server := httptest.NewTLSServer(app.Handler())
	t.Cleanup(server.Close)
	app.cfg.PublicURL = server.URL
	request := func(method, target string, body any, headers http.Header, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		var input io.Reader
		if body != nil {
			data, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			input = bytes.NewReader(data)
		}
		ctx, cancel := context.WithTimeout(f.ctx, 2*time.Minute)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+target, input)
		if err != nil {
			t.Fatal(err)
		}
		req.Header = headers.Clone()
		if req.Header == nil {
			req.Header = make(http.Header)
		}
		req.Header.Set("Origin", server.URL)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		response, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("perform functional HTTP request: %v", err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil || len(data) >= 16<<20 {
			t.Fatalf("read functional HTTP response: %v", err)
		}
		result := httptest.NewRecorder()
		for name, values := range response.Header {
			result.Header()[name] = append([]string(nil), values...)
		}
		result.WriteHeader(response.StatusCode)
		_, _ = result.Write(data)
		return result
	}
	bootstrap := request(http.MethodPost, "/admin/v1/bootstrap", map[string]any{
		"SetupToken": f.cfg.SetupToken, "Name": "Administrator", "Password": "administrator-password",
	}, nil)
	expectStatus(t, bootstrap, http.StatusCreated)
	adminID := stringValue(t, objectValue(t, jsonObject(t, bootstrap), "User"), "Id")
	login := request(http.MethodPost, "/admin/v1/session", map[string]any{
		"Name": "Administrator", "Password": "administrator-password",
	}, nil)
	expectStatus(t, login, http.StatusOK)
	csrf := stringValue(t, jsonObject(t, login), "CSRFToken")
	var cookie *http.Cookie
	for _, candidate := range login.Result().Cookies() {
		if candidate.Name == sessionCookie {
			cookie = candidate
		}
	}
	if cookie == nil {
		t.Fatal("administrator login omitted its session cookie")
	}
	adminHeaders := http.Header{"X-CSRF-Token": {csrf}}
	emby := request(http.MethodPost, "/emby/Users/AuthenticateByName", map[string]any{
		"Username": "Administrator", "Pw": "administrator-password",
	}, http.Header{"Authorization": {`Emby Client="Phase3Functional", DeviceId="phase3-functional", Device="Linux", Version="1.0"`}})
	expectStatus(t, emby, http.StatusOK)
	embyHeaders := http.Header{"X-Emby-Token": {stringValue(t, jsonObject(t, emby), "AccessToken")}}
	created := request(http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Local functional reconciliation", "CollectionType": "movies", "Paths": roots, "Scan": false,
	}, adminHeaders, cookie)
	expectStatus(t, created, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")
	rootPath := "/admin/v1/libraries/" + libraryID + "/roots"
	registered, total := responseItems(t, request(http.MethodGet, rootPath, nil, nil, cookie))
	if total != 2 || len(registered) != 2 {
		t.Fatal("functional library did not register exactly two roots")
	}
	rootIDs := make(map[string]string, 2)
	for _, root := range registered {
		id := stringValue(t, root, "Id")
		binding := objectValue(t, jsonObject(t, request(http.MethodGet, rootPath+"/"+id+"/binding", nil, nil, cookie)), "Binding")
		if binding["Status"] != "verified" {
			t.Fatalf("functional root is not verified: %#v", binding)
		}
		rootIDs[stringValue(t, binding, "Path")] = id
	}
	stableID := ""
	playbackVerified := false
	runScan := func(phase string, added, updated int) {
		t.Helper()
		started := time.Now()
		response := request(http.MethodPost, "/admin/v1/libraries/"+libraryID+"/scan", map[string]any{"ForceProbe": phase == "cold"}, adminHeaders, cookie)
		expectStatus(t, response, http.StatusAccepted)
		jobID := stringValue(t, objectValue(t, jsonObject(t, response), "Job"), "Id")
		t.Logf("phase=%s job=%s items=%d directories=%d non_media_entries=%d started", phase, jobID, count, directories, directories*nonMedia)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		progress := time.NewTicker(30 * time.Second)
		defer progress.Stop()
		var latest map[string]any
		overlappingQueries := 0
		for {
			jobs, _ := responseItems(t, request(http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
			for _, job := range jobs {
				if job["Id"] != jobID {
					continue
				}
				latest = job
				switch job["Status"] {
				case "completed":
					if job["Error"] != "" || job["Scanned"] != float64(count) || job["Added"] != float64(added) || job["Updated"] != float64(updated) {
						t.Fatalf("phase=%s completed with unexpected counters: %#v", phase, job)
					}
					if phase == "incremental" && (overlappingQueries == 0 || !playbackVerified) {
						t.Fatalf("incremental scan lacked observed query overlap or direct media verification: queries=%d playback=%t", overlappingQueries, playbackVerified)
					}
					t.Logf("phase=%s completed elapsed=%s scanned=%v added=%v updated=%v overlapping_queries=%d", phase, time.Since(started), job["Scanned"], job["Added"], job["Updated"], overlappingQueries)
					return
				case "failed", "cancelled", "interrupted":
					t.Fatalf("phase=%s scan failed: %#v", phase, job)
				}
			}
			if latest["Status"] == "running" && stableID != "" {
				// The task is asynchronous. Bracket a real page request with HTTP
				// Running observations instead of slowing the scan to fake overlap.
				items, total := responseItems(t, request(http.MethodGet, "/emby/Items?Ids="+url.QueryEscape(stableID), nil, embyHeaders))
				if total != 1 || len(items) != 1 || items[0]["Id"] != stableID {
					t.Fatalf("phase=%s concurrent stable page changed", phase)
				}
				after, _ := responseItems(t, request(http.MethodGet, "/admin/v1/jobs", nil, nil, cookie))
				for _, job := range after {
					if job["Id"] == jobID && job["Status"] == "running" {
						overlappingQueries++
					}
				}
				if phase == "incremental" && !playbackVerified {
					response := request(http.MethodGet, "/emby/Videos/"+stableID+"/stream.mp4?Static=true", nil, embyHeaders)
					expectStatus(t, response, http.StatusOK)
					original, err := os.ReadFile(paths[1])
					if err != nil || !bytes.Equal(response.Body.Bytes(), original) {
						t.Fatalf("direct HTTP media bytes differ from the actual MP4: %v", err)
					}
					playbackVerified = true
					t.Logf("phase=%s direct original media verified bytes=%d", phase, len(original))
				}
			}
			select {
			case <-f.ctx.Done():
				t.Fatalf("phase=%s reached its hang guard: %v; last job=%#v", phase, f.ctx.Err(), latest)
			case <-progress.C:
				t.Logf("phase=%s elapsed=%s status=%v scanned=%v added=%v updated=%v", phase, time.Since(started), latest["Status"], latest["Scanned"], latest["Added"], latest["Updated"])
			case <-ticker.C:
			}
		}
	}
	assertCatalog := func(phase string) map[string]string {
		t.Helper()
		rows, err := f.pool.Query(f.ctx, `SELECT id,path,root_id,relative_path,media FROM items WHERE library_id=$1 AND type='Movie' ORDER BY path`, libraryID)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		actual := make(map[string]string, count)
		ids := make(map[string]bool, count)
		for rows.Next() {
			var id, path, rootID, relative string
			var raw []byte
			if err := rows.Scan(&id, &path, &rootID, &relative, &raw); err != nil {
				t.Fatal(err)
			}
			wantID, exists := expected[path]
			if !exists || wantID != "" && wantID != id || actual[path] != "" || ids[id] {
				t.Fatalf("phase=%s has an unexpected or duplicated movie identity: path=%s id=%s wanted=%s", phase, path, id, wantID)
			}
			bound := false
			for root, registeredID := range rootIDs {
				if rootID == registeredID && filepath.Join(root, filepath.FromSlash(relative)) == path {
					bound = true
				}
			}
			var info media.Info
			if !bound || json.Unmarshal(raw, &info) != nil || info.ProbeVersion != media.CurrentProbeVersion || info.DurationTicks <= 0 || len(info.Streams) == 0 {
				t.Fatalf("phase=%s movie lacks correct root ownership or actual probe facts: %s", phase, path)
			}
			actual[path], ids[id] = id, true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if len(actual) != count || len(actual) != len(expected) {
			t.Fatalf("phase=%s movie count=%d, expected=%d", phase, len(actual), len(expected))
		}
		seen := make(map[string]bool, count)
		for offset := 0; offset < count; offset += 200 {
			query := url.Values{"ParentId": {libraryID}, "Recursive": {"true"}, "IncludeItemTypes": {"Movie"},
				"SortBy": {"SortName"}, "SortOrder": {"Ascending"}, "StartIndex": {strconv.Itoa(offset)}, "Limit": {"200"}}
			items, total := responseItems(t, request(http.MethodGet, "/emby/Items?"+query.Encode(), nil, embyHeaders))
			wantPage := min(200, count-offset)
			if total != count || len(items) != wantPage {
				t.Fatalf("phase=%s HTTP page offset=%d returned=%d total=%d", phase, offset, len(items), total)
			}
			for _, item := range items {
				id := stringValue(t, item, "Id")
				if !ids[id] || seen[id] || item["Type"] != "Movie" {
					t.Fatalf("phase=%s HTTP query has an unexpected or duplicate item: %s", phase, id)
				}
				seen[id] = true
			}
		}
		if len(seen) != count {
			t.Fatalf("phase=%s HTTP result omitted movies", phase)
		}
		t.Logf("phase=%s catalog and paginated HTTP set verified movies=%d roots=%d", phase, count, len(rootIDs))
		return actual
	}
	runScan("cold", count, 0)
	expected = assertCatalog("cold")
	stableID = expected[paths[1]]
	runScan("cached", 0, 0)
	assertCatalog("cached")
	removedID := expected[paths[0]]
	movedDataURL := "/emby/Users/" + adminID + "/Items/" + expected[paths[2]] + "/UserData"
	seedState := request(http.MethodPost, movedDataURL, map[string]any{
		"PlaybackPositionTicks": media.TicksPerSecond / 2, "PlayCount": 3, "Played": false,
		"IsFavorite": true, "LastPlayedDate": "2026-09-01T03:04:05Z",
	}, embyHeaders)
	expectStatus(t, seedState, http.StatusOK)
	preservedState := jsonObject(t, request(http.MethodGet, movedDataURL, nil, embyHeaders))
	if preservedState["IsFavorite"] != true || preservedState["PlayCount"] != float64(3) || preservedState["PlaybackPositionTicks"] != float64(media.TicksPerSecond/2) {
		t.Fatal("cross-root fixture did not persist nondefault user state")
	}
	assertPreservedState := func() {
		t.Helper()
		current := jsonObject(t, request(http.MethodGet, movedDataURL, nil, embyHeaders))
		if !reflect.DeepEqual(current, preservedState) {
			t.Fatalf("cross-root move changed saved user state: before=%#v after=%#v", preservedState, current)
		}
	}
	added := filepath.Join(roots[0], "d0000", "Added.mp4")
	moved := filepath.Join(roots[1], "d0001", "Moved.mp4")
	writeMovie(added, count)
	if err := os.Rename(paths[2], moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	expected[moved] = expected[paths[2]]
	delete(expected, paths[2])
	delete(expected, paths[0])
	expected[added] = ""
	runScan("incremental", 1, 1)
	expected = assertCatalog("incremental")
	var removedExists bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM items WHERE id=$1)`, removedID).Scan(&removedExists); err != nil {
		t.Fatal(err)
	}
	if removedExists || expected[added] == removedID {
		t.Fatal("deleted movie identity survived or was reused for the newly added movie")
	}
	assertPreservedState()
	runScan("settled", 0, 0)
	if settled := assertCatalog("settled"); !reflect.DeepEqual(settled, expected) {
		t.Fatal("unchanged final rescan changed movie identities or ownership")
	}
	assertPreservedState()
	t.Logf("functional reconciliation accepted: real media=%d registered roots=2 directories=%d non_media_entries=%d cold/cached/incremental/settled completed", count, directories, directories*nonMedia)
}
