//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPEmbyUserQueryFiltersBeforeCountSortAndPagination(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	login := f.embyLogin(t, "Administrator", "administrator-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	for _, user := range []struct {
		name, policy string
		disabled     bool
	}{
		{"alpha", `{"IsHidden":false,"IsDisabled":true,"OpaqueSecret":"query-private-marker"}`, false},
		{"Bravo", `{"IsHidden":true}`, false},
		{"bravo later", `{"IsHidden":true,"IsDisabled":false}`, true},
		{"Broken", `{"IsHidden":"invalid"}`, false},
		{"Charlie", `{"IsHidden":false}`, true},
		{"Zulu", `{}`, false},
		{"\u03c2igma", `{}`, false},
	} {
		created, err := f.users.CreateUser(f.ctx, user.name, "query-user-password", false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE users SET policy=$2::jsonb, is_disabled=$3,
			configuration='{"ProfilePin":"query-private-marker","Pin":"query-private-marker","CustomSecret":"query-private-marker"}'::jsonb
			WHERE id=$1`, created.ID, user.policy, user.disabled); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name, query string
		want        []string
		total       int
	}{
		{"default", "", []string{"Administrator", "alpha", "Bravo", "bravo later", "Broken", "Charlie", "Zulu", "\u03c2igma"}, 8},
		{"hidden", "IsHidden=true", []string{"Bravo", "bravo later", "Broken"}, 3},
		{"visible", "IsHidden=false", []string{"Administrator", "alpha", "Charlie", "Zulu", "\u03c2igma"}, 5},
		{"disabled_column", "IsDisabled=true", []string{"bravo later", "Charlie"}, 2},
		{"enabled_hidden", "IsHidden=true&IsDisabled=false", []string{"Bravo", "Broken"}, 2},
		{"lexical_lower_bound", "NameStartsWithOrGreater=BRAVO", []string{"Bravo", "bravo later", "Broken", "Charlie", "Zulu", "\u03c2igma"}, 6},
		{"combined_page", "NameStartsWithOrGreater=bravo&IsDisabled=false&IsHidden=true&SortOrder=Descending&StartIndex=1&Limit=1", []string{"Bravo"}, 2},
		{"ascending_page", "NameStartsWithOrGreater=bravo&StartIndex=2&Limit=2", []string{"Broken", "Charlie"}, 6},
		{"descending_page", "NameStartsWithOrGreater=bravo&SortOrder=Descending&StartIndex=1&Limit=2", []string{"Zulu", "Charlie"}, 6},
		{"casefold_boundary", "NameStartsWithOrGreater=" + url.QueryEscape("\u03a3igma"), []string{"\u03c2igma"}, 1},
		{"literal_wildcards", "NameStartsWithOrGreater=bravo%25_", []string{"Broken", "Charlie", "Zulu", "\u03c2igma"}, 4},
		{"count_only", "IsHidden=true&Limit=0", []string{}, 3},
		{"past_end", "IsHidden=true&StartIndex=2147483647", []string{}, 3},
		{"no_matches", "NameStartsWithOrGreater=" + url.QueryEscape("\U0001f600"), []string{}, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := f.request(t, http.MethodGet, "/emby/Users/Query?"+test.query, nil, headers)
			items, total := responseItems(t, response)
			names := make([]string, 0, len(items))
			for _, item := range items {
				names = append(names, stringValue(t, item, "Name"))
			}
			if total != test.total || !reflect.DeepEqual(names, test.want) {
				t.Fatalf("names = %v, total = %d; want %v, %d", names, total, test.want, test.total)
			}
			if response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), "query-private-marker") || strings.Contains(response.Body.String(), `"ProfilePin"`) {
				t.Fatal("directory query exposed a private field or allowed response caching")
			}
			if len(jsonObject(t, response)) != 2 {
				t.Fatal("directory query changed its compatibility envelope")
			}
		})
	}
}

func TestHTTPEmbyUserQueryRequiresCurrentAdministratorOrApplicationAuthority(t *testing.T) {
	a := newApplicationKeyHTTPFixture(t)
	key := a.create(t, "User Query Application")
	keyHeaders := http.Header{"X-Emby-Token": {key.token}}
	adminLogin := a.embyLogin(t, "Administrator", "administrator-password")
	adminToken := stringValue(t, adminLogin, "AccessToken")
	adminHeaders := http.Header{"X-Emby-Token": {adminToken}}
	viewer, err := a.users.CreateUser(a.ctx, "Directory Viewer", "viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	viewerLogin := a.embyLogin(t, viewer.Name, "viewer-password")
	viewerToken := stringValue(t, viewerLogin, "AccessToken")
	for _, headers := range []http.Header{adminHeaders, keyHeaders} {
		items, total := responseItems(t, a.request(t, http.MethodGet, "/emby/Users/Query?IsHidden=false&IsDisabled=false", nil, headers))
		if total != 2 || len(items) != 2 {
			t.Fatal("administrator or application query lost account visibility")
		}
	}
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Users/Query?IsHidden=true", nil, nil), http.StatusUnauthorized)
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Users/Query?IsHidden=true", nil, http.Header{"X-Emby-Token": {viewerToken}}), http.StatusForbidden)
	for _, query := range []string{"IsHidden=invalid", "SortOrder=invalid", "Limit=2147483648", "IsDisabled=true&isdisabled=false", "Unknown=query-private-marker"} {
		response := a.request(t, http.MethodGet, "/emby/Users/Query?"+query, nil, adminHeaders)
		expectStatus(t, response, http.StatusBadRequest)
		if strings.Contains(response.Body.String(), "query-private-marker") || strings.Contains(response.Body.String(), "TotalRecordCount") {
			t.Fatal("invalid query reflected input or directory facts")
		}
	}
	actor, err := a.users.ResolveEmbyForClientWithPeer(a.ctx, adminToken, identity.Client{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(a.ctx, "UPDATE users SET is_administrator=false WHERE id=$1", a.adminID); err != nil {
		t.Fatal(err)
	}
	// Bypass middleware with its old principal to exercise the directory's own
	// transaction check after a concurrent demotion.
	r := httptest.NewRequest(http.MethodGet, "/emby/Users/Query", nil)
	r = r.WithContext(context.WithValue(r.Context(), principalKey, actor))
	w := httptest.NewRecorder()
	a.app.embyUsers(w, r)
	expectStatus(t, w, http.StatusForbidden)
	if _, err := a.users.QueryUsers(a.ctx, actor, identity.UserQuery{Limit: 100}); !errors.Is(err, identity.ErrClientSessionForbidden) {
		t.Fatalf("stale administrator principal authorized directory read: %v", err)
	}
	keyActor, err := a.users.ResolveEmbyForClientWithPeer(a.ctx, key.token, identity.Client{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.pool.Exec(a.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", keyActor.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.users.QueryUsers(a.ctx, keyActor, identity.UserQuery{Limit: 100}); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatalf("stale application principal authorized directory read: %v", err)
	}
	expectStatus(t, a.request(t, http.MethodGet, "/emby/Users/Query", nil, keyHeaders), http.StatusUnauthorized)
}
