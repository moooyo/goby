package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

func TestParseEmbyUserQueryPreservesOptionalFiltersAndCompatibilityMetadata(t *testing.T) {
	for _, test := range []struct {
		name, raw        string
		start, limit     int
		hidden, disabled *bool
		bound            string
		descending       bool
	}{
		{name: "defaults", limit: 100},
		{name: "both_false", raw: "IsHidden=false&IsDisabled=False", hidden: userQueryBool(false), disabled: userQueryBool(false), limit: 100},
		{name: "mixed_casing", raw: "ISHIDDEN=TRUE&isdisabled=true&STARTINDEX=2&limit=1&sortorder=dEsCeNdInG&NameStartsWithOrGreater=BrAvO", hidden: userQueryBool(true), disabled: userQueryBool(true), start: 2, limit: 1, descending: true, bound: "BrAvO"},
		{name: "count_only", raw: "StartIndex=2147483647&Limit=0", start: 2147483647},
		{name: "capped_limit", raw: "Limit=2147483647", limit: 1000},
		{name: "integer_compatibility", raw: "StartIndex=01&Limit=%2B2", start: 1, limit: 2},
		{name: "literal_bound", raw: "NameStartsWithOrGreater=%25_", bound: "%_", limit: 100},
		{name: "empty_bound", raw: "NameStartsWithOrGreater=&SortOrder=Ascending", limit: 100},
		{name: "transport", raw: "api_key=query-secret&X-Emby-Client=Client&X-Emby-Device-Id=device&IsHidden=true", hidden: userQueryBool(true), limit: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Users/Query?"+test.raw, nil)
			query, err := parseEmbyUserQuery(r)
			if err != nil || query.StartIndex != test.start || query.Limit != test.limit || query.Descending != test.descending || query.NameStartsWithOrGreater != test.bound ||
				!sameUserQueryBool(query.IsHidden, test.hidden) || !sameUserQueryBool(query.IsDisabled, test.disabled) {
				t.Fatalf("query = %#v, error = %v", query, err)
			}
		})
	}
}

func TestParseEmbyUserQueryRejectsAmbiguousOrUnboundedInput(t *testing.T) {
	for _, raw := range []string{
		"IsHidden=", "IsDisabled=null", "IsHidden=1", "IsDisabled=yes", "IsHidden=true&ishidden=true",
		"IsDisabled=false&IsDisabled=true", "SortOrder=", "SortOrder=Random", "SortOrder=Ascending,Descending",
		"Limit=", "Limit=-1", "StartIndex=-1", "Limit=2147483648", "StartIndex=2147483648",
		"Limit=1.5", "Limit=1e3", "StartIndex=1&startindex=2", "Limit=1&Limit=1",
		"NameStartsWithOrGreater=a&namestartswithorgreater=b", "NameStartsWithOrGreater=%ff",
		"NameStartsWithOrGreater=%00", "NameStartsWithOrGreater=%0a", "NameStartsWithOrGreater=%zz",
		"NameStartsWithOrGreater=" + strings.Repeat("a", 129),
		"NameStartsWithOrGreater=" + url.QueryEscape(strings.Repeat("\U0001f600", 129)),
		"NameStartsWithOrGreater=" + url.QueryEscape(strings.Repeat("\u4e2d", 129)),
		"Unknown=true", "SearchTerm=alpha", "UserId=another-user", "X-Emby-Unknown=ignored",
		"api_key=one&X-Emby-Token=two", "IsHidden=true;IsDisabled=false",
	} {
		r := httptest.NewRequest(http.MethodGet, "/emby/Users/Query?"+raw, nil)
		if _, err := parseEmbyUserQuery(r); !errors.Is(err, identity.ErrInvalidInput) {
			t.Errorf("accepted malformed user query %q: %v", raw, err)
		}
	}
	for _, bound := range []string{strings.Repeat("a", 128), strings.Repeat("\U0001f600", 128), " ", "\u03c2igma"} {
		r := httptest.NewRequest(http.MethodGet, "/emby/Users/Query?NameStartsWithOrGreater="+url.QueryEscape(bound), nil)
		if query, err := parseEmbyUserQuery(r); err != nil || query.NameStartsWithOrGreater != bound {
			t.Errorf("valid literal boundary changed: %#v, %v", query, err)
		}
	}
}

func TestEmbyUserQueryRejectsUnprivilegedCallerBeforeParsingOrReading(t *testing.T) {
	for _, actor := range []identity.Principal{
		{}, {Kind: "emby", SessionID: "viewer-session", User: identity.User{ID: "viewer"}},
		{Kind: "emby", SessionID: "disabled-session", User: identity.User{ID: "disabled", IsAdministrator: true, IsDisabled: true}},
		{Kind: identity.ApplicationKeyKind, SessionID: "incomplete-key", ApplicationKeyID: 1},
	} {
		r := httptest.NewRequest(http.MethodGet, "/emby/Users/Query?IsHidden=malformed", nil)
		r = r.WithContext(context.WithValue(r.Context(), principalKey, actor))
		w := httptest.NewRecorder()
		(&Server{}).embyUsers(w, r)
		if w.Code != http.StatusForbidden || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "TotalRecordCount") {
			t.Fatalf("unprivileged query exposed directory state: %d %s", w.Code, w.Body.String())
		}
	}
}

func userQueryBool(value bool) *bool { return &value }

func sameUserQueryBool(left, right *bool) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
