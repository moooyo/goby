package server

import (
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
)

// This is a Goby regression, not an additional Emby reference observation.
func TestHTTPEpisodePairRangeLabel(t *testing.T) {
	f, root := newLibraryServerFixture(t)
	root = filepath.Join(root, "tv")
	const nfo = `<episodedetails><season>1</season><episode>3</episode><title>First</title><plot>First plot</plot></episodedetails>
<episodedetails><season>1</season><episode>4</episode><title>Second</title><plot>Second plot</plot></episodedetails>`
	cases := []struct {
		series, stem, name, overview string
		paired                       bool
	}{
		{"Matching", "Matching - S01E03E04 - 第 3-4 集", "First,  Second", "First plot\n\nSecond plot", true},
		{"Mismatched", "Mismatched - S01E03E04 - 第 3-5 集", "Mismatched - S01E03E04 - 第 3-5 集", "", false},
	}
	for _, example := range cases {
		relative := example.series + "/Season 01/" + example.stem
		writeAPIMediaFile(t, root, relative+".mp4")
		writeNFOReferenceSidecar(t, root, relative+".nfo", []byte(nfo))
	}
	userID := f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	adminHeaders := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	created := f.request(t, http.MethodPost, "/admin/v1/libraries", map[string]any{
		"Name": "Episode Range Labels", "CollectionType": "tvshows", "Paths": []string{root}, "Scan": false,
	}, adminHeaders, cookie)
	expectStatus(t, created, http.StatusCreated)
	libraryID := stringValue(t, objectValue(t, jsonObject(t, created), "Library"), "Id")
	// The mismatched label deliberately produces a sidecar warning.
	scanNFOReferenceLibrary(t, f, libraryID, adminHeaders, cookie)
	client := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, client, "AccessToken")}}
	listPath := "/emby/Users/" + userID + "/Items"
	query := url.Values{"ParentId": {libraryID}, "Recursive": {"true"}, "IncludeItemTypes": {"Episode"}}
	rows, total := responseItems(t, f.request(t, http.MethodGet, listPath+"?"+query.Encode(), nil, headers))
	if total != len(cases) || len(rows) != len(cases) {
		t.Fatalf("episode count = %d/%d, want %d physical files", len(rows), total, len(cases))
	}
	byPath := make(map[string]map[string]any, len(cases))
	for _, row := range rows {
		id := stringValue(t, row, "Id")
		response := f.request(t, http.MethodGet, listPath+"/"+id, nil, headers)
		expectStatus(t, response, http.StatusOK)
		detail := jsonObject(t, response)
		path := stringValue(t, detail, "Path")
		for _, key := range []string{"IndexNumber", "ParentIndexNumber", "IndexNumberEnd"} {
			listed, listedOK := row[key]
			value, detailOK := detail[key]
			if listedOK != detailOK || !reflect.DeepEqual(listed, value) {
				t.Errorf("list/detail %s differ: %v (present=%t) versus %v (present=%t)", key, listed, listedOK, value, detailOK)
			}
		}
		byPath[path] = detail
	}
	for _, example := range cases {
		t.Run(example.series, func(t *testing.T) {
			detail := byPath[filepath.Join(root, example.series, "Season 01", example.stem+".mp4")]
			end, hasEnd := detail["IndexNumberEnd"]
			if hasEnd != example.paired || example.paired && end != float64(4) {
				t.Errorf("IndexNumberEnd = %v (present=%t), expected a pair: %t", end, hasEnd, example.paired)
			}
			if detail["IndexNumber"] != float64(3) || detail["ParentIndexNumber"] != float64(1) ||
				detail["Name"] != example.name || detail["Overview"] != example.overview {
				t.Errorf("incorrect numbering or NFO projection: %#v", detail)
			}
		})
	}
}
