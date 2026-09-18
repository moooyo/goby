package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func collectionTestRequest(method, target, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, target, nil)
	} else {
		request = httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	principal := identity.Principal{Kind: "emby", SessionID: "owner-session", User: identity.User{ID: "owner"}}
	return request.WithContext(context.WithValue(request.Context(), principalKey, principal))
}

func TestCollectionRoutesRequireAuthentication(t *testing.T) {
	app := &Server{}
	mux := http.NewServeMux()
	app.registerCollectionRoutes(mux)
	for _, prefix := range []string{"/emby/Playlists", "/emby/Collections", "/admin/v1/playlists", "/admin/v1/collections"} {
		itemsPath, deletePath := "Items", "Delete"
		if strings.HasPrefix(prefix, "/admin/") {
			itemsPath, deletePath = "items", "delete"
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, prefix, nil))
			if response.Code != http.StatusUnauthorized {
				t.Errorf("GET %s did not require administrator authentication: %d", prefix, response.Code)
			}
		}
		for _, route := range []struct{ method, suffix string }{
			{http.MethodPost, ""}, {http.MethodGet, "/collection"},
			{http.MethodPost, "/collection"}, {http.MethodPatch, "/collection"}, {http.MethodDelete, "/collection"},
			{http.MethodGet, "/collection/" + itemsPath}, {http.MethodPost, "/collection/" + itemsPath},
			{http.MethodDelete, "/collection/" + itemsPath}, {http.MethodPost, "/collection/" + itemsPath + "/" + deletePath},
		} {
			request := httptest.NewRequest(route.method, prefix+route.suffix, nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Errorf("%s %s did not require authentication: %d", route.method, request.URL.Path, response.Code)
			}
		}
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/emby/Playlists/collection/AddToPlaylistInfo"},
		{http.MethodPost, "/emby/Playlists/collection/Items/entry/Move/0"},
		{http.MethodGet, "/admin/v1/playlists/collection/items/preview"},
		{http.MethodPost, "/admin/v1/playlists/collection/items/entry/move/0"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(route.method, route.path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s did not require authentication: %d", route.method, route.path, response.Code)
		}
	}
}

func TestCollectionMutationResponseMatchesTransport(t *testing.T) {
	for _, path := range []string{"/admin/v1/playlists/list", "/emby/Playlists/list"} {
		response := httptest.NewRecorder()
		collectionMutationResponse(response, httptest.NewRequest(http.MethodDelete, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("mutation returned %d for %s", response.Code, path)
		}
		if strings.HasPrefix(path, "/admin/") {
			if strings.TrimSpace(response.Body.String()) != "{}" || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatal("native mutation did not return the administrator client's JSON object")
			}
		} else if response.Body.Len() != 0 {
			t.Fatal("Emby mutation did not retain its empty success response")
		}
	}
}

func TestCollectionCreateParsesQueryAndJSONWithoutChangingEntryMultiplicity(t *testing.T) {
	for _, test := range []struct{ name, query, body string }{
		{"query", "Name=Favorites&ParentId=parent&MediaType=Video&IsPublic=true&IsLocked=false&Ids=first,first,second&UserId=owner", ""},
		{"JSON", "", `{"Name":"Favorites","ParentId":"parent","MediaType":"Video","IsPublic":true,"IsLocked":false,"Ids":["first","first","second"],"UserId":"owner"}`},
		{"matching carriers", "Name=Favorites&IsPublic=true&Ids=first,first,second&UserId=owner", `{"Name":"Favorites","ParentId":"parent","MediaType":"Video","IsPublic":true,"IsLocked":false,"Ids":["first","first","second"],"UserId":"owner"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := collectionTestRequest(http.MethodPost, "/emby/Playlists?"+test.query, test.body)
			input, owner, ok := readCollectionCreate(response, request)
			if !ok || owner != "owner" || input.Name != "Favorites" || input.ParentID != "parent" ||
				input.MediaType != "Video" || !input.IsPublic || input.IsLocked ||
				!reflect.DeepEqual(input.ItemIDs, []string{"first", "first", "second"}) {
				t.Fatalf("creation request changed its fields or duplicate entries: %#v, owner=%q, response=%s", input, owner, response.Body.String())
			}
		})
	}
}

func TestCollectionCreateRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	cases := []struct{ name, query, body string }{
		{"missing name", "", ""},
		{"empty name", "Name=", ""},
		{"control name", "Name=Bad%0aName", ""},
		{"oversized name", "Name=" + strings.Repeat("a", 257), ""},
		{"oversized unicode name", "Name=" + url.QueryEscape(strings.Repeat("\U0001f3b5", 257)), ""},
		{"invalid UTF8", "Name=%ff", ""},
		{"query duplicate", "Name=First&Name=First", ""},
		{"query alias", "Name=First&name=First", ""},
		{"unknown query", "Name=First&OwnerId=other", ""},
		{"malformed query", "Name=First&Ids=%gg", ""},
		{"empty CSV member", "Name=First&Ids=one,,two", ""},
		{"CSV control member", "Name=First&Ids=one,%0atwo", ""},
		{"oversized ID", "Name=First&Ids=" + strings.Repeat("a", collectionIDBytes+1), ""},
		{"too many entries", "Name=First&Ids=" + strings.Repeat("one,", collectionInputCount) + "one", ""},
		{"invalid boolean", "Name=First&IsPublic=1", ""},
		{"empty boolean", "Name=First&IsLocked=", ""},
		{"unsupported media type", "Name=First&MediaType=Photo", ""},
		{"duplicate JSON key", "", `{"Name":"First","Name":"Second"}`},
		{"JSON key alias", "", `{"Name":"First","name":"First"}`},
		{"unknown JSON key", "", `{"Name":"First","OwnerId":"other"}`},
		{"null body", "Name=First", `null`},
		{"array body", "Name=First", `[]`},
		{"multiple values", "", `{"Name":"First"}{"Name":"Second"}`},
		{"null field", "Name=First", `{"Ids":null}`},
		{"wrong field type", "", `{"Name":true}`},
		{"wrong IDs type", "Name=First", `{"Ids":"one,two"}`},
		{"unpaired surrogate", "", `{"Name":"\ud800"}`},
		{"name conflict", "Name=First", `{"Name":"Second"}`},
		{"boolean conflict", "Name=First&IsPublic=true", `{"IsPublic":false}`},
		{"IDs conflict", "Name=First&Ids=one,two", `{"Ids":["two","one"]}`},
		{"owner conflict", "Name=First&UserId=owner", `{"UserId":"other"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			_, _, ok := readCollectionCreate(response, collectionTestRequest(http.MethodPost, "/emby/Playlists?"+test.query, test.body))
			if ok || response.Code != http.StatusBadRequest {
				t.Fatalf("invalid creation accepted or returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCollectionRequestBodyLimitsAndCompatibleMediaTypes(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/json; charset=utf-8", "text/plain;charset=UTF-8"} {
		request := collectionTestRequest(http.MethodPost, "/emby/Playlists", `{"Name":"Favorites"}`)
		request.Header.Set("Content-Type", contentType)
		if _, _, ok := readCollectionCreate(httptest.NewRecorder(), request); !ok {
			t.Errorf("compatible JSON media type rejected: %s", contentType)
		}
	}
	for _, contentType := range []string{"", "text/xml", "application/json; charset=latin1", "application/json; profile=test"} {
		request := collectionTestRequest(http.MethodPost, "/emby/Playlists", `{"Name":"Favorites"}`)
		request.Header.Set("Content-Type", contentType)
		response := httptest.NewRecorder()
		if _, _, ok := readCollectionCreate(response, request); ok || response.Code != http.StatusUnsupportedMediaType {
			t.Errorf("unsupported JSON media type accepted: %s, status=%d", contentType, response.Code)
		}
	}
	request := collectionTestRequest(http.MethodPost, "/emby/Playlists", `{"Name":"`+strings.Repeat("x", collectionInputBytes)+`"}`)
	response := httptest.NewRecorder()
	if _, _, ok := readCollectionCreate(response, request); ok || response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body was not bounded: status=%d", response.Code)
	}
}

func TestCollectionPatchPreservesExplicitFalseAndShareReplacement(t *testing.T) {
	request := collectionTestRequest(http.MethodPost, "/emby/Playlists/list?UserId=owner", `{"Name":"Renamed","IsPublic":false,"IsLocked":false,"Shares":[{"UserId":"reader"},{"UserId":"editor","CanEdit":true}]}`)
	patch, userID, ok := readCollectionPatch(httptest.NewRecorder(), request)
	if !ok || userID != "owner" || patch.Name == nil || *patch.Name != "Renamed" ||
		patch.IsPublic == nil || *patch.IsPublic || patch.IsLocked == nil || *patch.IsLocked || patch.Shares == nil ||
		!reflect.DeepEqual(*patch.Shares, []library.CollectionShare{{UserID: "reader"}, {UserID: "editor", CanEdit: true}}) {
		t.Fatalf("patch lost explicit values or shares: %#v", patch)
	}
	patch, _, ok = readCollectionPatch(httptest.NewRecorder(), collectionTestRequest(http.MethodPatch, "/emby/Playlists/list", `{"Shares":[]}`))
	if !ok || patch.Shares == nil || len(*patch.Shares) != 0 {
		t.Fatal("empty Shares did not represent explicit share revocation")
	}
}

func TestCollectionPatchRejectsInvalidShareAuthority(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"OwnerId":"other"}`, `{"UserId":"other"}`, `{"Shares":null}`, `{"Shares":{}}`,
		`{"Shares":[null]}`, `{"Shares":[{}]}`, `{"Shares":[{"UserId":""}]}`,
		`{"Shares":[{"UserId":"reader","CanEdit":null}]}`,
		`{"Shares":[{"UserId":"reader","CanEdit":1}]}`,
		`{"Shares":[{"UserId":"reader","IsAdministrator":true}]}`,
		`{"Shares":[{"UserId":"reader","UserId":"editor"}]}`,
		`{"Shares":[{"UserId":"reader"},{"UserId":"reader","CanEdit":true}]}`,
	} {
		response := httptest.NewRecorder()
		if _, _, ok := readCollectionPatch(response, collectionTestRequest(http.MethodPatch, "/emby/Playlists/list", body)); ok || response.Code != http.StatusBadRequest {
			t.Errorf("invalid share patch accepted or returned %d: %s", response.Code, body)
		}
	}
}

func TestCollectionAuthorityCannotBeBorrowedFromUserId(t *testing.T) {
	app := &Server{}
	viewer := identity.Principal{Kind: "emby", User: identity.User{ID: "viewer"}}
	admin := identity.Principal{Kind: "emby", User: identity.User{ID: "admin", IsAdministrator: true}}
	key := identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: "application-key", ClientSessionID: "application-client", ApplicationKeyID: 1}
	for _, test := range []struct {
		name      string
		principal identity.Principal
		userID    string
		create    bool
		status    int
		subject   library.Subject
	}{
		{"implicit owner", viewer, "", true, http.StatusOK, library.Subject{UserID: "viewer", Actor: &viewer}},
		{"explicit owner", viewer, "viewer", true, http.StatusOK, library.Subject{UserID: "viewer", Actor: &viewer}},
		{"viewer spoof", viewer, "victim", true, http.StatusForbidden, library.Subject{}},
		{"administrator spoof", admin, "victim", true, http.StatusForbidden, library.Subject{}},
		{"administrator update spoof", admin, "victim", false, http.StatusForbidden, library.Subject{}},
		{"key without owner", key, "", true, http.StatusBadRequest, library.Subject{}},
		{"key explicit owner", key, "owner", true, http.StatusOK, library.Subject{UserID: "owner", ApplicationCredentialID: "application-key"}},
		{"key without projection", key, "", false, http.StatusOK, library.Subject{ApplicationCredentialID: "application-key"}},
		{"invalid target", key, "bad\nuser", false, http.StatusBadRequest, library.Subject{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := collectionTestRequest(http.MethodPost, "/emby/Playlists", "")
			request = request.WithContext(context.WithValue(request.Context(), principalKey, test.principal))
			response := httptest.NewRecorder()
			subject, ok := app.collectionSubject(response, request, test.userID, test.create)
			if response.Code != test.status || ok != (test.status == http.StatusOK) || !reflect.DeepEqual(subject, test.subject) {
				t.Fatalf("wrong collection authority: %#v, ok=%v, status=%d", subject, ok, response.Code)
			}
		})
	}
}

func TestCollectionPaginationBoundsAndZeroLimit(t *testing.T) {
	for _, test := range []struct {
		query        string
		start, limit int
	}{
		{"", 0, 100}, {"StartIndex=12&Limit=0", 12, 0}, {"Limit=1001", 0, 1000}, {"StartIndex=2147483647&Limit=2147483647", 2147483647, 1000},
	} {
		request := collectionTestRequest(http.MethodGet, "/emby/Playlists/list/Items?"+test.query, "")
		start, limit, ok := readCollectionPagination(httptest.NewRecorder(), request, request.URL.Query())
		if !ok || start != test.start || limit != test.limit {
			t.Errorf("wrong pagination for %s: %d, %d, %v", test.query, start, limit, ok)
		}
	}
	for _, query := range []string{"Limit=", "Limit=-1", "Limit=+1", "Limit=01", "Limit=1.5", "Limit=2147483648", "StartIndex=-0"} {
		request := collectionTestRequest(http.MethodGet, "/emby/Playlists/list/Items?"+query, "")
		response := httptest.NewRecorder()
		if _, _, ok := readCollectionPagination(response, request, request.URL.Query()); ok || response.Code != http.StatusBadRequest {
			t.Errorf("invalid pagination accepted: %s", query)
		}
	}
}

func TestCollectionMalformedMutationsDoNotReachStore(t *testing.T) {
	app := &Server{}
	for _, test := range []struct {
		name, method, query, body string
		handler                   func(http.ResponseWriter, *http.Request)
	}{
		{"missing membership", http.MethodPost, "", "", func(w http.ResponseWriter, r *http.Request) { app.addCollectionItems(w, r, library.PlaylistKind) }},
		{"empty membership", http.MethodPost, "Ids=", "", func(w http.ResponseWriter, r *http.Request) { app.addCollectionItems(w, r, library.PlaylistKind) }},
		{"wrong removal identifier", http.MethodDelete, "Ids=item", "", func(w http.ResponseWriter, r *http.Request) { app.removeCollectionItems(w, r, library.PlaylistKind) }},
		{"wrong collection identifier", http.MethodDelete, "EntryIds=entry", "", func(w http.ResponseWriter, r *http.Request) { app.removeCollectionItems(w, r, library.BoxSetKind) }},
		{"unexpected membership body", http.MethodPost, "Ids=item", `{}`, func(w http.ResponseWriter, r *http.Request) { app.addCollectionItems(w, r, library.PlaylistKind) }},
		{"invalid move index", http.MethodPost, "", "", app.movePlaylistEntry},
		{"invalid image switch", http.MethodGet, "EnableImages=maybe", "", func(w http.ResponseWriter, r *http.Request) { app.collectionItems(w, r, library.PlaylistKind) }},
		{"duplicate pagination", http.MethodGet, "Limit=1&Limit=2", "", func(w http.ResponseWriter, r *http.Request) { app.collectionItems(w, r, library.PlaylistKind) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := collectionTestRequest(test.method, "/emby/Playlists/list/Items?"+test.query, test.body)
			request.SetPathValue("Id", "list")
			request.SetPathValue("ItemId", "entry")
			request.SetPathValue("NewIndex", "-1")
			response := httptest.NewRecorder()
			test.handler(response, request)
			if response.Code != http.StatusBadRequest {
				t.Errorf("malformed mutation returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestCollectionDTOUsesExplicitShareWireFields(t *testing.T) {
	info := library.CollectionInfo{ID: "list", Name: "Favorites", Kind: library.PlaylistKind, OwnerID: "owner", ItemCount: 2,
		Shares: []library.CollectionShare{{UserID: "reader"}, {UserID: "editor", CanEdit: true}}}
	encoded, err := json.Marshal(collectionDTO(info))
	if err != nil {
		t.Fatal(err)
	}
	var dto map[string]any
	if err := json.Unmarshal(encoded, &dto); err != nil {
		t.Fatal(err)
	}
	if dto["Type"] != "Playlist" || dto["OwnerId"] != "owner" || dto["ChildCount"] != float64(2) ||
		!reflect.DeepEqual(dto["Shares"], []any{map[string]any{"UserId": "reader", "CanEdit": false}, map[string]any{"UserId": "editor", "CanEdit": true}}) {
		t.Fatalf("unexpected collection wire contract: %s", encoded)
	}
}
