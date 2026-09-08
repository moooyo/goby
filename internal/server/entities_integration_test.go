package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type entityHTTPFixture struct {
	*serverFixture
	adminID, viewerID, visibleID, hiddenID string
	adminHeaders, viewerHeaders          http.Header
}

type entityHTTPKind struct {
	path, itemType, filter, hiddenName string
	visibleNames                       []string
}

func entityHTTPKinds() []entityHTTPKind {
	return []entityHTTPKind{
		{path: "Genres", itemType: "Genre", filter: "GenreIds", hiddenName: "SecretGenre", visibleNames: []string{"Comedy", "Drama"}},
		{path: "Tags", itemType: "Tag", filter: "TagIds", hiddenName: "secret", visibleNames: []string{"local-artwork", "reference"}},
		{path: "Studios", itemType: "Studio", filter: "StudioIds", hiddenName: "Hidden Studio", visibleNames: []string{"North Studio", "South Studio"}},
		{path: "Persons", itemType: "Person", filter: "PersonIds", hiddenName: "Hidden Person", visibleNames: []string{"Alex", "Different Director"}},
	}
}

func newEntityHTTPFixture(t *testing.T) *entityHTTPFixture {
	t.Helper()
	f, root := newLibraryServerFixture(t)
	for _, file := range []struct{ relative, nfo string }{
		{
			relative: "visible/Alpha.mp4",
			nfo: `<movie><title>Alpha</title><genre>Drama</genre><tag>local-artwork</tag><studio>North Studio</studio>` +
				`<actor><name>Alex</name><role>Lead</role><order>0</order></actor><director>Different Director</director></movie>`,
		},
		{
			relative: "visible/Beta.mp4",
			nfo:      `<movie><title>Beta</title><genre>Comedy</genre><tag>reference</tag><studio>South Studio</studio><director>Alex</director></movie>`,
		},
		{
			relative: "hidden/SecretOnly.mp4",
			nfo:      `<movie><title>SecretOnly</title><genre>SecretGenre</genre><tag>secret</tag><studio>Hidden Studio</studio><actor><name>Hidden Person</name></actor></movie>`,
		},
	} {
		mediaPath := writeAPIMediaFile(t, root, file.relative)
		nfoPath := strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".nfo"
		if err := os.WriteFile(nfoPath, []byte(file.nfo), 0o600); err != nil {
			t.Fatalf("write entity NFO fixture: %v", err)
		}
	}
	fixture := &entityHTTPFixture{serverFixture: f, adminID: f.bootstrap(t)}
	cookie, csrf := f.adminLogin(t)
	fixture.visibleID = createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "visible"), "movies")
	fixture.hiddenID = createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "hidden"), "movies")
	viewer, err := f.users.CreateUser(f.ctx, "Entity Viewer", "viewer-password", false)
	if err != nil {
		t.Fatalf("create entity viewer: %v", err)
	}
	fixture.viewerID = viewer.ID
	policy, err := json.Marshal(map[string]any{"EnableAllFolders": false, "EnabledFolders": []string{fixture.visibleID}})
	if err != nil {
		t.Fatalf("encode entity viewer policy: %v", err)
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET policy = $1::jsonb WHERE id = $2", policy, viewer.ID); err != nil {
		t.Fatalf("restrict entity viewer to visible media: %v", err)
	}
	adminLogin := f.embyLogin(t, "Administrator", "administrator-password")
	viewerLogin := f.embyLogin(t, viewer.Name, "viewer-password")
	fixture.adminHeaders = http.Header{"X-Emby-Token": {stringValue(t, adminLogin, "AccessToken")}}
	fixture.viewerHeaders = http.Header{"X-Emby-Token": {stringValue(t, viewerLogin, "AccessToken")}}
	return fixture
}

func entityHTTPIndex(t *testing.T, f *entityHTTPFixture, path string, headers http.Header) map[string]map[string]any {
	t.Helper()
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/"+path, nil, headers))
	if total != len(items) {
		t.Fatalf("unpaginated %s total = %d, item count = %d", path, total, len(items))
	}
	index := make(map[string]map[string]any, len(items))
	for _, item := range items {
		name := stringValue(t, item, "Name")
		id := stringValue(t, item, "Id")
		if number, err := strconv.ParseInt(id, 10, 64); err != nil || number <= 0 {
			t.Fatalf("%s entity ID must be a positive decimal string: %q", path, id)
		}
		if _, exists := index[name]; exists {
			t.Fatalf("%s repeats the same entity name %q", path, name)
		}
		index[name] = item
	}
	return index
}

func entityHTTPID(t *testing.T, index map[string]map[string]any, name string) string {
	t.Helper()
	item, exists := index[name]
	if !exists {
		t.Fatalf("entity %q is missing from the HTTP listing: %#v", name, index)
	}
	return stringValue(t, item, "Id")
}

func entityHTTPMovieResults(t *testing.T, f *entityHTTPFixture, filters url.Values, expected ...string) []map[string]any {
	t.Helper()
	values := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie"}}
	for key, entries := range filters {
		values[key] = append([]string(nil), entries...)
	}
	items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Items?"+values.Encode(), nil, f.viewerHeaders))
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item["Type"] != "Movie" {
			t.Errorf("movie query returned another item type: %#v", item)
		}
		names = append(names, stringValue(t, item, "Name"))
	}
	want := append([]string{}, expected...)
	sort.Strings(names)
	sort.Strings(want)
	if total != len(want) || !reflect.DeepEqual(names, want) {
		t.Fatalf("movie query %s returned %v with total %d, want %v", values.Encode(), names, total, want)
	}
	return items
}

func entityHTTPReferences(t *testing.T, item map[string]any, field string) []map[string]any {
	t.Helper()
	entries, ok := item[field].([]any)
	if !ok {
		t.Fatalf("projected %s must be a JSON array: %#v", field, item[field])
	}
	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		object, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("projected %s entry must be an object: %#v", field, entry)
		}
		result = append(result, object)
	}
	return result
}

func TestHTTPEntityBrowsingRoundTripAndMediaReferenceIDTypes(t *testing.T) {
	f := newEntityHTTPFixture(t)
	indexes := make(map[string]map[string]map[string]any)
	for _, kind := range entityHTTPKinds() {
		t.Run(kind.path, func(t *testing.T) {
			index := entityHTTPIndex(t, f, kind.path, f.viewerHeaders)
			indexes[kind.path] = index
			if len(index) != len(kind.visibleNames) {
				t.Fatalf("%s visible entity count = %d, want %d", kind.path, len(index), len(kind.visibleNames))
			}
			for _, name := range kind.visibleNames {
				id := entityHTTPID(t, index, name)
				entity := index[name]
				for _, field := range []string{"Count", "ChildCount", "ItemCount", "IsFolder", "MediaType"} {
					if _, exists := entity[field]; exists {
						t.Errorf("default entity list added an unobserved %s field: %#v", field, entity)
					}
				}
				if kind.path == "Tags" {
					if len(entity) != 2 {
						t.Errorf("tag list entries must contain only Name and string Id: %#v", entity)
					}
				} else if entity["Type"] != kind.itemType {
					t.Errorf("entity Type = %#v, want %s", entity["Type"], kind.itemType)
				}
				byID := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+id, nil, f.viewerHeaders)
				expectStatus(t, byID, http.StatusOK)
				if detail := jsonObject(t, byID); detail["Id"] != id || detail["Name"] != name {
					t.Errorf("entity item-detail lookup changed identity: %#v", detail)
				}
				if kind.path != "Tags" {
					byName := f.request(t, http.MethodGet, "/emby/"+kind.path+"/"+url.PathEscape(name), nil, f.viewerHeaders)
					expectStatus(t, byName, http.StatusOK)
					if detail := jsonObject(t, byName); detail["Id"] != id || detail["Name"] != name || detail["Type"] != kind.itemType {
						t.Errorf("name-based entity detail changed identity: %#v", detail)
					}
				}
				want := []string{"Alpha"}
				if name == "Comedy" || name == "reference" || name == "South Studio" {
					want = []string{"Beta"}
				} else if name == "Alex" {
					want = []string{"Alpha", "Beta"}
				}
				entityHTTPMovieResults(t, f, url.Values{kind.filter: {id}}, want...)
			}
		})
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Tags/reference", nil, f.viewerHeaders), http.StatusNotFound, "not_implemented", true)
	page, pageTotal := responseItems(t, f.request(t, http.MethodGet, "/emby/Genres?IncludeItemTypes=Movie&StartIndex=1&Limit=1", nil, f.viewerHeaders))
	if len(page) != 1 || pageTotal != 2 || page[0]["Name"] != "Drama" {
		t.Errorf("entity pagination changed the pre-page count or ordering: %#v, total = %d", page, pageTotal)
	}
	items := entityHTTPMovieResults(t, f, url.Values{"Fields": {"Genres,Tags,Studios,People"}}, "Alpha", "Beta")
	for _, item := range items {
		name := stringValue(t, item, "Name")
		genre, tag, studio := "Drama", "local-artwork", "North Studio"
		if name == "Beta" {
			genre, tag, studio = "Comedy", "reference", "South Studio"
		}
		if !reflect.DeepEqual(item["Genres"], []any{genre}) {
			t.Errorf("movie %s lost its genre name projection: %#v", name, item["Genres"])
		}
		if _, exists := item["Tags"]; exists {
			t.Error("media tag projection must use TagItems instead of Tags")
		}
		for _, field := range []struct{ output, endpoint, expectedName string }{
			{output: "GenreItems", endpoint: "Genres", expectedName: genre},
			{output: "TagItems", endpoint: "Tags", expectedName: tag},
			{output: "Studios", endpoint: "Studios", expectedName: studio},
		} {
			references := entityHTTPReferences(t, item, field.output)
			if len(references) != 1 || references[0]["Name"] != field.expectedName {
				t.Fatalf("movie %s has incorrect %s: %#v", name, field.output, references)
			}
			id := entityHTTPID(t, indexes[field.endpoint], field.expectedName)
			numericID, err := strconv.ParseInt(id, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if value, ok := references[0]["Id"].(float64); !ok || value != float64(numericID) {
				t.Errorf("%s media reference must use the corresponding numeric ID, got %#v", field.output, references[0]["Id"])
			}
		}
		people := entityHTTPReferences(t, item, "People")
		wantPeople := 1
		if name == "Alpha" {
			wantPeople = 2
		}
		if len(people) != wantPeople {
			t.Fatalf("movie %s has %d people, want %d", name, len(people), wantPeople)
		}
		for _, person := range people {
			personName := stringValue(t, person, "Name")
			if id := stringValue(t, person, "Id"); id != entityHTTPID(t, indexes["Persons"], personName) {
				t.Errorf("person reference ID does not match the entity listing: %#v", person)
			}
			wantedType := "Director"
			if name == "Alpha" && personName == "Alex" {
				wantedType = "Actor"
			}
			if person["Type"] != wantedType {
				t.Errorf("movie-specific credit type was lost: %#v", person)
			}
		}
	}
}

func TestHTTPEntityNameFiltersCombineDimensionsAndPersonCredits(t *testing.T) {
	f := newEntityHTTPFixture(t)
	for _, test := range []struct {
		name     string
		filters  url.Values
		expected []string
	}{
		{name: "genre_pipe_or", filters: url.Values{"Genres": {"Drama|Comedy"}}, expected: []string{"Alpha", "Beta"}},
		{name: "tag_pipe_or", filters: url.Values{"Tags": {"local-artwork|reference"}}, expected: []string{"Alpha", "Beta"}},
		{name: "studio_pipe_or", filters: url.Values{"Studios": {"North Studio|South Studio"}}, expected: []string{"Alpha", "Beta"}},
		{name: "cross_dimension_and_mismatch", filters: url.Values{"Genres": {"Drama"}, "Tags": {"reference"}}},
		{name: "or_within_tag_and_studio", filters: url.Values{"Tags": {"local-artwork|reference"}, "Studios": {"North Studio"}}, expected: []string{"Alpha"}},
		{name: "three_matching_dimensions", filters: url.Values{"Genres": {"Comedy"}, "Tags": {"reference"}, "Studios": {"South Studio"}}, expected: []string{"Beta"}},
		{name: "person_in_both_movies", filters: url.Values{"Person": {"Alex"}}, expected: []string{"Alpha", "Beta"}},
		{name: "person_actor_credit", filters: url.Values{"Person": {"Alex"}, "PersonTypes": {"Actor"}}, expected: []string{"Alpha"}},
		{name: "person_director_credit_same_association", filters: url.Values{"Person": {"Alex"}, "PersonTypes": {"Director"}}, expected: []string{"Beta"}},
		{name: "multiple_person_types", filters: url.Values{"Person": {"Alex"}, "PersonTypes": {"Actor,Director"}}, expected: []string{"Alpha", "Beta"}},
		{name: "other_person_cannot_borrow_actor_type", filters: url.Values{"Person": {"Different Director"}, "PersonTypes": {"Actor"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			entityHTTPMovieResults(t, f, test.filters, test.expected...)
		})
	}
}

func TestHTTPEntityIDFilterSyntaxValidationAndZeroLimit(t *testing.T) {
	f := newEntityHTTPFixture(t)
	for _, kind := range entityHTTPKinds() {
		t.Run(kind.path, func(t *testing.T) {
			index := entityHTTPIndex(t, f, kind.path, f.viewerHeaders)
			first := entityHTTPID(t, index, kind.visibleNames[0])
			second := entityHTTPID(t, index, kind.visibleNames[1])
			for _, separator := range []string{",", "|"} {
				entityHTTPMovieResults(t, f, url.Values{kind.filter: {first + separator + second}}, "Alpha", "Beta")
			}
			entityHTTPMovieResults(t, f, url.Values{kind.filter: {first + "|" + second + "," + first}}, "Alpha", "Beta")
			// A valid but nonexistent ID is an empty result, not malformed input.
			entityHTTPMovieResults(t, f, url.Values{kind.filter: {"9223372036854775807"}})
			for _, invalid := range []string{"0", "-1", "not-a-number", "9223372036854775808"} {
				values := url.Values{"Recursive": {"true"}, "IncludeItemTypes": {"Movie"}, kind.filter: {invalid}}
				response := f.request(t, http.MethodGet, "/emby/Items?"+values.Encode(), nil, f.viewerHeaders)
				expectAPIError(t, response, http.StatusBadRequest, "invalid_input", true)
			}
			items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/"+kind.path+"?Limit=0", nil, f.viewerHeaders))
			if len(items) != 0 || total != len(kind.visibleNames) {
				t.Errorf("Limit=0 must retain the visible total: %s items = %#v, total = %d", kind.path, items, total)
			}
		})
	}
	people := entityHTTPIndex(t, f, "Persons", f.viewerHeaders)
	entityHTTPMovieResults(t, f, url.Values{"PersonIds": {entityHTTPID(t, people, "Alex")}, "PersonTypes": {"Director"}}, "Beta")
}

func TestHTTPEntityACLHidesNamesIDsAndSourceItems(t *testing.T) {
	f := newEntityHTTPFixture(t)
	visibleIDs := make(map[string]string)
	for _, kind := range entityHTTPKinds() {
		t.Run(kind.path, func(t *testing.T) {
			adminIndex := entityHTTPIndex(t, f, kind.path, f.adminHeaders)
			hiddenID := entityHTTPID(t, adminIndex, kind.hiddenName)
			viewerIndex := entityHTTPIndex(t, f, kind.path, f.viewerHeaders)
			if _, exists := viewerIndex[kind.hiddenName]; exists {
				t.Errorf("%s listing exposed a hidden-library entity", kind.path)
			}
			if len(viewerIndex) != len(kind.visibleNames) {
				t.Errorf("%s listing leaked hidden entity totals: %#v", kind.path, viewerIndex)
			}
			visibleIDs[kind.path] = entityHTTPID(t, viewerIndex, kind.visibleNames[0])
			byID := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+hiddenID, nil, f.viewerHeaders)
			expectAPIError(t, byID, http.StatusNotFound, "not_found", true)
			if kind.path != "Tags" {
				byName := f.request(t, http.MethodGet, "/emby/"+kind.path+"/"+url.PathEscape(kind.hiddenName), nil, f.viewerHeaders)
				expectAPIError(t, byName, http.StatusNotFound, "not_found", true)
			}
			entityHTTPMovieResults(t, f, url.Values{kind.filter: {hiddenID}})
			entityHTTPMovieResults(t, f, url.Values{"Ids": {hiddenID}})
			spoofed := f.request(t, http.MethodGet, "/emby/"+kind.path+"?UserId="+f.adminID, nil, f.viewerHeaders)
			expectAPIError(t, spoofed, http.StatusForbidden, "access_denied", true)
		})
	}
	conflictingUser := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+visibleIDs["Genres"]+"?UserId="+f.adminID, nil, f.viewerHeaders)
	expectAPIError(t, conflictingUser, http.StatusBadRequest, "invalid_input", true)
	spoofedItems := f.request(t, http.MethodGet, "/emby/Items?Recursive=true&UserId="+f.adminID, nil, f.viewerHeaders)
	expectAPIError(t, spoofedItems, http.StatusForbidden, "access_denied", true)
	// Existing sessions must observe access revocation on the next entity read.
	if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy = '{"EnableAllFolders":false,"EnabledFolders":[]}'::jsonb WHERE id = $1`, f.viewerID); err != nil {
		t.Fatalf("revoke visible entity access: %v", err)
	}
	for _, kind := range entityHTTPKinds() {
		items, total := responseItems(t, f.request(t, http.MethodGet, "/emby/"+kind.path, nil, f.viewerHeaders))
		if len(items) != 0 || total != 0 {
			t.Errorf("revoked policy exposed %s entities or totals: %#v, total = %d", kind.path, items, total)
		}
		response := f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+visibleIDs[kind.path], nil, f.viewerHeaders)
		expectAPIError(t, response, http.StatusNotFound, "not_found", true)
	}
}
