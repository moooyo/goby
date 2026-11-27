package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const nfoReferenceCapture = "../../tests/compatibility/fixtures/reference/emby-4.10.2.0/nfo-differential.json"

var nfoReferenceFields = []string{
	"Name", "Overview", "ProviderIds", "ProductionYear", "CommunityRating", "OfficialRating", "IndexNumber", "ParentIndexNumber",
	"ParentPath", "SeriesPath", "SeasonPath",
}

type nfoReferenceItem struct {
	Name, Type, Path string
	First, Warm      map[string]any
}

type nfoReferenceFixtures struct {
	Libraries []struct{ Name, Type, Path string }
	Sidecars  map[string]struct {
		Text   *string
		Base64 []byte
	}
	WarmSidecars map[string]string
	Cases        []nfoReferenceItem
}

type nfoReferenceDifferences struct {
	Cases map[string]map[string]map[string]any // Case, then phase, then differing field.
}

// TestHTTPNFOReferenceReplay replays the recorded Emby inputs through Goby's
// normal synchronous scan and HTTP projection. The test uses synthetic probe
// facts and a private schema; it does not need the vendor binary or network.
func TestHTTPNFOReferenceReplay(t *testing.T) {
	var reference nfoReferenceFixtures
	var gaps nfoReferenceDifferences
	readNFOReferenceJSON(t, nfoReferenceCapture, &reference)
	readNFOReferenceJSON(t, "testdata/nfo-emby-4.10.2.0-differences.json", &gaps)
	f, root := newLibraryServerFixture(t)
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	adminHeaders := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	client := f.embyLogin(t, "Administrator", "administrator-password")
	clientHeaders := http.Header{"X-Emby-Token": {stringValue(t, client, "AccessToken")}}

	for _, item := range reference.Cases {
		if item.Type == "Movie" || item.Type == "Episode" {
			writeAPIMediaFile(t, root, item.Path)
		}
	}
	for path, sidecar := range reference.Sidecars {
		contents := sidecar.Base64
		if (contents != nil) == (sidecar.Text != nil) {
			t.Fatalf("sidecar %q must specify exactly one byte representation", path)
		}
		if sidecar.Text != nil {
			contents = []byte(*sidecar.Text)
		}
		writeNFOReferenceSidecar(t, root, path, contents)
	}

	var libraryIDs []string
	for _, collection := range reference.Libraries {
		created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
			"Name": collection.Name, "CollectionType": collection.Type, "Paths": []string{filepath.Join(root, filepath.FromSlash(collection.Path))},
			"Scan": false, "LibraryOptions": map[string]bool{"EnableLocalMetadata": true, "EnableLocalImages": false, "EnableEmbeddedArtwork": false},
		}, adminHeaders, cookie)
		expectStatus(t, created, http.StatusCreated)
		libraryIDs = append(libraryIDs, stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id"))
	}

	for _, phase := range []string{"first", "warm"} {
		t.Run(phase, func(t *testing.T) {
			if phase == "warm" {
				for path, text := range reference.WarmSidecars {
					writeNFOReferenceSidecar(t, root, path, []byte(text))
				}
			}
			for _, id := range libraryIDs {
				scanNFOReferenceLibrary(t, f, id, adminHeaders, cookie)
			}
			base := "/emby/Users/" + userID + "/Items"
			query := url.Values{
				"Recursive": {"true"}, "IncludeItemTypes": {"Movie,Series,Season,Episode"},
				"Fields": {"Path,ParentId,SeriesId,SeasonId"}, "EnableImages": {"false"},
			}
			rows, _ := responseItems(t, f.request(t, http.MethodGet, base+"?"+query.Encode(), nil, clientHeaders))
			byTypeAndPath := make(map[[2]string]string)
			byID := make(map[string]map[string]any)
			for _, row := range rows {
				id := stringValue(t, row, "Id")
				byID[id] = row
				if path, ok := nfoReferenceRelativePath(root, row["Path"]); ok {
					key := [2]string{stringValue(t, row, "Type"), path}
					if _, exists := byTypeAndPath[key]; exists {
						t.Fatalf("multiple catalog items for %v", key)
					}
					byTypeAndPath[key] = id
				}
			}
			for _, item := range reference.Cases {
				t.Run(item.Name, func(t *testing.T) {
					id, ok := byTypeAndPath[[2]string{item.Type, item.Path}]
					if !ok {
						t.Fatalf("missing %s fixture %q", item.Type, item.Path)
					}
					r := f.request(t, http.MethodGet, base+"/"+id, nil, clientHeaders)
					expectStatus(t, r, http.StatusOK)
					detail := jsonObject(t, r)
					if detail["Type"] != item.Type {
						t.Fatalf("item type = %v, want %s", detail["Type"], item.Type)
					}
					addNFOReferenceRelationships(root, item.Type, detail, byID[id], byID)
					want, delta := item.First, gaps.Cases[item.Name]["first"]
					if phase == "warm" && item.Warm != nil {
						want, delta = item.Warm, gaps.Cases[item.Name]["warm"]
					}
					compareNFOReferenceFields(t, detail, want, delta)
				})
			}
		})
	}
}

func readNFOReferenceJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func writeNFOReferenceSidecar(t *testing.T, root, relative string, contents []byte) {
	t.Helper()
	local, err := filepath.Localize(relative)
	if err != nil || filepath.Ext(local) != ".nfo" {
		t.Fatalf("invalid test sidecar path %q", relative)
	}
	if err := os.WriteFile(filepath.Join(root, local), contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func scanNFOReferenceLibrary(t *testing.T, f *serverFixture, id string, headers http.Header, cookie *http.Cookie) {
	t.Helper()
	admitted := f.request(t, http.MethodPost, "/admin/v1/libraries/"+id+"/scan", map[string]bool{"ForceProbe": false}, headers, cookie)
	expectStatus(t, admitted, http.StatusAccepted)
	jobID := stringValue(t, objectValue(t, jsonObject(t, admitted), "Job"), "Id")
	timer, ticker := time.NewTimer(15*time.Second), time.NewTicker(25*time.Millisecond)
	defer timer.Stop()
	defer ticker.Stop()
	for {
		job, err := f.app.library.GetJob(f.ctx, jobID)
		if err != nil {
			t.Fatal(err)
		}
		switch job.Status {
		case "Completed":
			// The replay intentionally includes malformed sidecars. Warnings do
			// not exempt any catalog field from the assertions below.
			return
		case "Failed", "Cancelled", "Interrupted":
			t.Fatalf("fixture scan did not complete: %+v", job)
		}
		select {
		case <-timer.C:
			t.Fatalf("timed out waiting for fixture scan: %+v", job)
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func nfoReferenceRelativePath(root string, value any) (string, bool) {
	path, ok := value.(string)
	if !ok || !filepath.IsAbs(path) {
		return "", false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func addNFOReferenceRelationships(root, kind string, detail, row map[string]any, rows map[string]map[string]any) {
	if kind != "Season" && kind != "Episode" {
		return
	}
	for property, field := range map[string]string{"ParentId": "ParentPath", "SeriesId": "SeriesPath", "SeasonId": "SeasonPath"} {
		id, _ := detail[property].(string)
		if id == "" {
			id, _ = row[property].(string)
		}
		if relative, ok := nfoReferenceRelativePath(root, rows[id]["Path"]); ok {
			detail[field] = relative
		}
	}
}

func compareNFOReferenceFields(t *testing.T, got, official, delta map[string]any) {
	t.Helper()
	for _, field := range nfoReferenceFields {
		value, present := got[field]
		reference, observed := official[field]
		if present == observed && reflect.DeepEqual(value, reference) {
			continue
		}
		// A recorded difference excuses only this field's exact previous
		// value (or omission). Convergence is allowed, but arbitrary new
		// outcomes are not. A null delta denotes an absent property.
		if previous, known := delta[field]; known && present == (previous != nil) && reflect.DeepEqual(value, previous) {
			t.Logf("known difference: %s = %#v (present=%v); Emby = %#v (present=%v)", field, value, present, reference, observed)
			continue
		}
		t.Errorf("%s = %#v (present=%v), want Emby %#v (present=%v); recorded Goby difference: %#v", field, value, present, reference, observed, delta[field])
	}
}
