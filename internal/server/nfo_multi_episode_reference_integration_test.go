package server

import (
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"testing"
)

const multiEpisodeNFOReferenceCapture = "../../tests/compatibility/fixtures/reference/emby-4.10.2.0/nfo-multi-episode.json"

var multiEpisodeNFOFields = []string{"Name", "Overview", "ProviderIds", "IndexNumber", "IndexNumberEnd", "ParentIndexNumber"}

type multiEpisodeNFOCase struct {
	Name, Media         string
	InitialNfo, WarmNfo *string
	First               map[string]any
}

// TestHTTPMultiEpisodeNFOReferenceReplay drives both catalog surfaces using a
// private PostgreSQL schema and synthetic probe facts, with no vendor server.
func TestHTTPMultiEpisodeNFOReferenceReplay(t *testing.T) {
	var reference struct{ Cases []multiEpisodeNFOCase }
	var gaps nfoReferenceDifferences
	readNFOReferenceJSON(t, multiEpisodeNFOReferenceCapture, &reference)
	readNFOReferenceJSON(t, "testdata/nfo-multi-episode-4.10.2.0-differences.json", &gaps)
	if len(reference.Cases) == 0 {
		t.Fatal("the multi-episode reference has no cases")
	}

	f, root := newLibraryServerFixture(t)
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	adminHeaders := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	client := f.embyLogin(t, "Administrator", "administrator-password")
	clientHeaders := http.Header{"X-Emby-Token": {stringValue(t, client, "AccessToken")}}
	root = filepath.Join(root, "tv")
	writeNFO := func(item multiEpisodeNFOCase, text string) {
		sidecar := item.Media[:len(item.Media)-len(path.Ext(item.Media))] + ".nfo"
		writeNFOReferenceSidecar(t, root, sidecar, []byte("\xef\xbb\xbf"+text))
	}
	for _, item := range reference.Cases {
		if _, err := filepath.Localize(item.Media); err != nil || path.Ext(item.Media) != ".mp4" {
			t.Fatalf("invalid synthetic media path %q", item.Media)
		}
		writeAPIMediaFile(t, root, item.Media)
		if item.InitialNfo != nil {
			writeNFO(item, *item.InitialNfo)
		}
	}
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Synthetic NFO Pairs", "CollectionType": "tvshows", "Paths": []string{root}, "Scan": false,
		"LibraryOptions": map[string]bool{"EnableLocalMetadata": true, "EnableLocalImages": false, "EnableEmbeddedArtwork": false},
	}, adminHeaders, cookie)
	expectStatus(t, created, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")

	itemList := "/emby/Users/" + userID + "/Items"
	firstIDs := make(map[string]string)
	for _, phase := range []string{"first", "warm"} {
		if phase == "warm" {
			for _, item := range reference.Cases {
				if item.WarmNfo != nil {
					writeNFO(item, *item.WarmNfo)
				}
			}
		}
		scanNFOReferenceLibrary(t, f, libraryID, adminHeaders, cookie)
		t.Run(phase, func(t *testing.T) {
			query := url.Values{"ParentId": {libraryID}, "Recursive": {"true"}, "IncludeItemTypes": {"Episode,Season,Series"}, "Fields": {"Path"}}
			rows, total := responseItems(t, f.request(t, http.MethodGet, itemList+"?"+query.Encode(), nil, clientHeaders))
			if total != len(rows) {
				t.Fatalf("incomplete catalog listing: %d/%d items", len(rows), total)
			}
			byPath := make(map[[2]string]map[string]any)
			episodes := 0
			for _, row := range rows {
				kind := stringValue(t, row, "Type")
				if kind == "Episode" {
					episodes++ // Count even pathless entries, not just the observed sidecar targets.
				}
				if relative, ok := nfoReferenceRelativePath(root, row["Path"]); ok {
					key := [2]string{kind, relative}
					if _, duplicate := byPath[key]; duplicate {
						t.Fatalf("duplicate catalog item for %v", key)
					}
					byPath[key] = row
				}
			}
			if episodes != len(reference.Cases) {
				t.Fatalf("catalog has %d episodes for %d physical media files", episodes, len(reference.Cases))
			}
			for _, item := range reference.Cases {
				t.Run(item.Name, func(t *testing.T) {
					episode := byPath[[2]string{"Episode", item.Media}]
					seasonPath := path.Dir(item.Media)
					season := byPath[[2]string{"Season", seasonPath}]
					series := byPath[[2]string{"Series", path.Dir(seasonPath)}]
					if episode == nil || season == nil || series == nil {
						t.Fatalf("physical item or ancestors missing for %q", item.Media)
					}
					id, seasonID, seriesID := stringValue(t, episode, "Id"), stringValue(t, season, "Id"), stringValue(t, series, "Id")
					if phase == "first" {
						firstIDs[item.Name] = id
					} else if prior, ok := firstIDs[item.Name]; ok && prior != id {
						t.Errorf("rescan changed episode ID from %q to %q", prior, id)
					}
					listed, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Shows/"+seriesID+"/Episodes?Fields=Path", nil, clientHeaders))
					if total != 1 || len(listed) != 1 || listed[0]["Id"] != id || listed[0]["Path"] != episode["Path"] {
						t.Fatalf("series listing did not expose the same single episode: %#v", listed)
					}
					r := f.request(t, http.MethodGet, itemList+"/"+id, nil, clientHeaders)
					expectStatus(t, r, http.StatusOK)
					detail := jsonObject(t, r)
					if detail["Type"] != "Episode" || detail["Id"] != id || detail["Path"] != episode["Path"] ||
						detail["ParentId"] != seasonID || detail["SeasonId"] != seasonID || detail["SeriesId"] != seriesID || season["ParentId"] != seriesID {
						t.Fatal("item detail did not retain the physical episode, season and series identity")
					}
					sources, ok := detail["MediaSources"].([]any)
					if !ok || len(sources) != 1 {
						t.Fatalf("expected one original media source, got %#v", detail["MediaSources"])
					}
					source, ok := sources[0].(map[string]any)
					if !ok || source["Path"] != detail["Path"] || source["ItemId"] != id || source["Protocol"] != "File" || source["IsRemote"] != false {
						t.Fatalf("media source was not the original episode file: %#v", source)
					}
					for _, field := range []string{"IndexNumber", "IndexNumberEnd", "ParentIndexNumber"} {
						value, present := detail[field]
						for _, projection := range []map[string]any{episode, listed[0]} {
							other, included := projection[field]
							if included != present || !reflect.DeepEqual(other, value) {
								t.Errorf("list/detail %s differ: list=%v (present=%v), detail=%v (present=%v)", field, other, included, value, present)
							}
						}
					}
					// Both baseline scans produced the same field differences. Warm
					// Goby behavior may later diverge when it reimports a valid edit.
					delta := gaps.Cases[item.Name][phase]
					if delta == nil {
						delta = gaps.Cases[item.Name]["first"]
					}
					compareNFOReferenceFields(t, multiEpisodeNFOFields, detail, item.First, delta)
				})
			}
		})
	}
}
