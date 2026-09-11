package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
)

func newExtraHTTPFixture(t *testing.T) *themeHTTPFixture {
	t.Helper()
	f := newThemeHTTPFixture(t)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO items
		(id,library_id,root_id,parent_id,name,sort_name,type,is_folder,relative_path,path,media)
		SELECT id,'theme-visible','theme-root-visible','th-movie',name,name,'Video',false,relative,
		'/theme-http/visible/'||relative,(SELECT media FROM items WHERE id='th-video')
		FROM (VALUES
		('ex-zeta','Zeta Bonus','Film/featurettes/Zeta Bonus.mp4'),
		('ex-alpha','Alpha Bonus','Film/featurettes/Alpha Bonus.mp4'),
		('ex-middle','Middle Deleted Scene','Film/deleted scenes/Middle Deleted Scene.mp4'),
		('ex-trailer','Delta Local Trailer','Film/trailers/Delta Local Trailer.mp4'),
		('ex-retired','Old Bonus','Film/featurettes/Old Bonus.mp4')) fixture(id,name,relative);
		INSERT INTO extra_reserved_paths(root_id,relative_path,is_directory)
		SELECT root_id,relative_path,false FROM items WHERE id LIKE 'ex-%';
		INSERT INTO item_extra_resources(resource_item_id,owner_item_id,kind,active) VALUES
		('ex-zeta','th-movie','clip',true),('ex-alpha','th-movie','clip',true),
		('ex-middle','th-movie','deleted_scene',true),('ex-trailer','th-movie','trailer',true),
		('ex-retired','th-movie','clip',false)`); err != nil {
		t.Fatalf("seed indexed extra HTTP fixture: %v", err)
	}
	return f
}

func extraHTTPItems(t *testing.T, response *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	expectStatus(t, response, http.StatusOK)
	if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatal("extra response is not JSON")
	}
	var items []map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil || items == nil {
		t.Fatal("extra response is not a non-null bare array")
	}
	return items
}

func TestHTTPExtraArraysDefaultsDetailedProjectionAndEmptyOwners(t *testing.T) {
	f := newExtraHTTPFixture(t)
	before := f.state(t)
	base := "/emby/Users/" + f.viewerID + "/Items/"
	features := extraHTTPItems(t, f.request(t, http.MethodGet, base+"th-movie/SpecialFeatures", nil, f.headers))
	if !reflect.DeepEqual(themeHTTPItemIDs(t, features), []string{"ex-alpha", "ex-middle", "ex-zeta"}) {
		t.Fatal("HTTP SpecialFeatures included trailers, history, or reordered the recorded sample")
	}
	for _, item := range features {
		for _, absent := range []string{"ParentId", "SortName", "DateCreated", "Path", "MediaSources", "MediaStreams", "Overview", "VideoType"} {
			if _, exists := item[absent]; exists {
				t.Fatalf("default SpecialFeatures unexpectedly projected %s", absent)
			}
		}
		if item["Type"] != "Video" || item["MediaType"] != "Video" || item["IsFolder"] != false {
			t.Fatal("HTTP extra type differs from the recorded projection")
		}
	}
	trailers := extraHTTPItems(t, f.request(t, http.MethodGet, base+"th-movie/LocalTrailers?Fields=Path,ParentId,MediaSources,People", nil, f.headers))
	if len(trailers) != 1 || trailers[0]["Id"] != "ex-trailer" || trailers[0]["Name"] != "Theme Movie - Trailer" ||
		trailers[0]["Type"] != "Trailer" || trailers[0]["ExtraType"] != "Trailer" || trailers[0]["ParentId"] != "th-movie" {
		t.Fatal("local trailer lost its independent array or owner-based identity")
	}
	sources, ok := trailers[0]["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("local trailer omitted its own media source")
	}
	source, ok := sources[0].(map[string]any)
	if !ok || source["Id"] != media.SourceID("ex-trailer") || source["Name"] != "Delta Local Trailer" {
		t.Fatal("trailer display name replaced its source filename or ID")
	}
	for _, owner := range []string{"th-empty", "th-series", "th-episode"} {
		for _, endpoint := range []string{"SpecialFeatures", "LocalTrailers"} {
			if items := extraHTTPItems(t, f.request(t, http.MethodGet, base+owner+"/"+endpoint, nil, f.headers)); len(items) != 0 {
				t.Fatal("empty existing owner acquired fabricated extra matches")
			}
		}
	}
	for _, item := range append(features, trailers...) {
		response := f.request(t, http.MethodGet, base+item["Id"].(string), nil, f.headers)
		expectStatus(t, response, http.StatusOK)
		direct := jsonObject(t, response)
		if direct["Id"] != item["Id"] || direct["Type"] != item["Type"] || direct["ExtraType"] != item["ExtraType"] ||
			direct["Name"] != item["Name"] || direct["ParentId"] != "th-movie" {
			t.Fatal("direct extra projection disagrees with the endpoint identity")
		}
	}
	if f.state(t) != before {
		t.Fatal("extra HTTP reads wrote user state, owner IDs, or metadata")
	}
}

func TestHTTPExtraSubjectIsolationCurrentPolicyAndProjectionControls(t *testing.T) {
	f := newExtraHTTPFixture(t)
	base := "/emby/Users/" + f.viewerID + "/Items/th-movie/"
	for _, endpoint := range []string{"SpecialFeatures", "LocalTrailers"} {
		expectStatus(t, f.request(t, http.MethodGet, base+endpoint, nil, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, base+endpoint, nil, f.otherHeaders), http.StatusForbidden)
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.otherID+"/Items/th-movie/"+endpoint, nil, f.otherHeaders), http.StatusNotFound)
		missing := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/missing/"+endpoint, nil, f.headers)
		if endpoint == "SpecialFeatures" {
			if items := extraHTTPItems(t, missing); len(items) != 0 {
				t.Fatal("missing SpecialFeatures owner acquired invented resources")
			}
		} else {
			expectStatus(t, missing, http.StatusNotFound)
		}
		expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/th-private/"+endpoint, nil, f.headers), http.StatusNotFound)
		expectStatus(t, f.request(t, http.MethodGet, base+endpoint+"?EnableUserData=invalid", nil, f.headers), http.StatusBadRequest)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["theme-visible"]}' WHERE id=$1`, f.otherID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO user_item_data(user_id,item_id,play_count,is_favorite)
		VALUES($1,'ex-alpha',3,true),($2,'ex-alpha',7,false)`, f.viewerID, f.otherID); err != nil {
		t.Fatal(err)
	}
	before := f.state(t)
	for _, viewer := range []struct {
		id       string
		headers  http.Header
		count    float64
		favorite bool
	}{
		{f.viewerID, f.headers, 3, true}, {f.otherID, f.otherHeaders, 7, false},
	} {
		items := extraHTTPItems(t, f.request(t, http.MethodGet, "/emby/Users/"+viewer.id+"/Items/th-movie/SpecialFeatures", nil, viewer.headers))
		data, ok := items[0]["UserData"].(map[string]any)
		if !ok || data["PlayCount"] != viewer.count || data["IsFavorite"] != viewer.favorite {
			t.Fatal("extra list leaked or replaced another user's resource state")
		}
	}
	controlled := extraHTTPItems(t, f.request(t, http.MethodGet, base+"SpecialFeatures?EnableUserData=false&EnableImages=false&Fields=ParentId,SortName", nil, f.headers))
	for _, item := range controlled {
		for _, key := range []string{"UserData", "ImageTags", "BackdropImageTags"} {
			if _, exists := item[key]; exists {
				t.Fatal("extra projection ignored an explicit response switch")
			}
		}
		if item["ParentId"] != "th-movie" || item["SortName"] == nil {
			t.Fatal("extra response switch discarded independently requested fields")
		}
	}
	if f.state(t) != before {
		t.Fatal("extra projection controls mutated stored state")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":[]}' WHERE id=$1`, f.viewerID); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"SpecialFeatures", "LocalTrailers"} {
		expectStatus(t, f.request(t, http.MethodGet, base+endpoint, nil, f.headers), http.StatusNotFound)
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/ex-alpha", nil, f.headers), http.StatusNotFound)
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy='{"EnableAllFolders":false,"EnabledFolders":["theme-visible"]}' WHERE id=$1`, f.viewerID); err != nil {
		t.Fatal(err)
	}
	if items := extraHTTPItems(t, f.request(t, http.MethodGet, base+"SpecialFeatures", nil, f.headers)); len(items) != 3 {
		t.Fatal("restored policy did not take effect with the same authenticated session")
	}
	listed, _ := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items?Recursive=true&Limit=100", nil, f.headers))
	for _, item := range listed {
		if strings.HasPrefix(item["Id"].(string), "ex-") {
			t.Fatal("ordinary HTTP browsing exposed active or inactive extras")
		}
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/ex-retired", nil, f.headers), http.StatusNotFound)
	items, err := f.app.library.GetItemsByIDFor(f.ctx, library.Subject{UserID: f.viewerID}, []string{"ex-alpha", "ex-trailer", "ex-retired"})
	if err != nil || len(items) != 2 || items[0].ExtraKind != library.ExtraKindClip || items[1].ExtraKind != library.ExtraKindTrailer {
		t.Fatalf("internal direct projection lost extra attributes or exposed history: %v", err)
	}
}

func TestHTTPExplicitExtraIDsMatchPublicTypesWithoutOpeningOrdinaryBrowse(t *testing.T) {
	f := newExtraHTTPFixture(t)
	base := "/emby/Users/" + f.viewerID + "/Items"
	query := "?Ids=ex-alpha,ex-middle,ex-trailer,ex-retired,th-song,missing&Recursive=true&Fields=ParentId,MediaSources&Limit=128"
	items, count := responseItems(t, f.request(t, http.MethodGet, base+query, nil, f.headers))
	if count != 3 || !reflect.DeepEqual(themeHTTPItemIDs(t, items), []string{"ex-alpha", "ex-trailer", "ex-middle"}) {
		t.Fatal("known IDs did not project exactly the currently valid visible extras")
	}
	trailers, count := responseItems(t, f.request(t, http.MethodGet, base+query+"&IncludeItemTypes=Trailer", nil, f.headers))
	if count != 1 || len(trailers) != 1 || trailers[0]["Type"] != "Trailer" || trailers[0]["ExtraType"] != "Trailer" ||
		trailers[0]["Id"] != "ex-trailer" || trailers[0]["Name"] != "Theme Movie - Trailer" || trailers[0]["ParentId"] != "th-movie" {
		t.Fatal("known-ID Trailer type filtering used the internal Video type or lost attachment attributes")
	}
	videos, count := responseItems(t, f.request(t, http.MethodGet, base+query+"&IncludeItemTypes=Video", nil, f.headers))
	if count != 2 || !reflect.DeepEqual(themeHTTPItemIDs(t, videos), []string{"ex-alpha", "ex-middle"}) {
		t.Fatal("known-ID Video type filter included a public Trailer")
	}
	outside, count := responseItems(t, f.request(t, http.MethodGet, base+query+"&ParentId=th-empty", nil, f.headers))
	if count != 0 || len(outside) != 0 {
		t.Fatal("known IDs escaped the caller's independent parent intersection")
	}
	ordinary, count := responseItems(t, f.request(t, http.MethodGet, base+"?ParentId=th-movie&Recursive=true", nil, f.headers))
	if count != 0 || len(ordinary) != 0 {
		t.Fatal("ordinary parent traversal enumerated attached resources without known IDs")
	}
	response := f.request(t, http.MethodGet, base+"/th-movie", nil, f.headers)
	expectStatus(t, response, http.StatusOK)
	owner := jsonObject(t, response)
	if owner["LocalTrailerCount"] != float64(1) {
		t.Fatal("positive Movie detail omitted its recorded local trailer count")
	}
	if _, present := owner["SpecialFeatureCount"]; present {
		t.Fatal("parent detail invented an unobserved SpecialFeatureCount")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE item_extra_resources SET active=false WHERE resource_item_id='ex-trailer'`); err != nil {
		t.Fatal(err)
	}
	response = f.request(t, http.MethodGet, base+"/th-movie", nil, f.headers)
	expectStatus(t, response, http.StatusOK)
	if _, present := jsonObject(t, response)["LocalTrailerCount"]; present {
		t.Fatal("Movie detail retained a positive count after its last trailer retired")
	}
}
