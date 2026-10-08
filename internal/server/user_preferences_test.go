package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestApplyDisplayPreferenceDefaultsExplicitSortSkipsRead(t *testing.T) {
	for _, test := range []struct {
		name, rawQuery, parentID, sortBy, sortOrder string
	}{
		{"scope", "Client=web&DisplayPreferencesId=folder&SortBy=SortName&SortOrder=Ascending", "", "SortName", "Ascending"},
		{"case aliases", "client=web&displaypreferencesid=folder&sOrTbY=Name&sortorder=DESC", "", "Name", "DESC"},
		{"empty explicit values", "Client=web&DisplayPreferencesId=folder&SortBy=&SortOrder=", "", "", ""},
		{"query parent", "Client=web&SortBy=SortName&SortOrder=Ascending", "folder", "SortName", "Ascending"},
		{"URL parent", "Client=web&ParentId=folder&SortBy=SortName&SortOrder=Ascending", "", "SortName", "Ascending"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Items?"+test.rawQuery, nil)
			r = r.WithContext(context.WithValue(r.Context(), principalKey, identity.Principal{Kind: "emby"}))
			query := library.Query{UserID: "viewer", ParentID: test.parentID, SortBy: "DateCreated", SortOrder: "Descending"}
			// A nil identity store makes any unused preference read fail the test.
			if !(&Server{}).applyDisplayPreferenceDefaults(httptest.NewRecorder(), r, &query) {
				t.Fatal("fully explicit sorting was rejected")
			}
			if query.SortBy != test.sortBy || query.SortOrder != test.sortOrder {
				t.Fatalf("explicit sort = %q/%q, want %q/%q", query.SortBy, query.SortOrder, test.sortBy, test.sortOrder)
			}
		})
	}
}

func TestApplyDisplayPreferenceDefaultsExplicitSortRetainsValidation(t *testing.T) {
	valid := "Client=web&DisplayPreferencesId=folder&SortBy=SortName&SortOrder=Ascending"
	cases := map[string]string{
		"duplicate key":         valid + "&SortBy=Name",
		"duplicate alias":       valid + "&sortorder=Descending",
		"duplicate scope":       valid + "&displaypreferencesid=other",
		"duplicate client":      valid + "&client=other",
		"malformed query":       valid + "&ignored=%",
		"unsupported sort":      "Client=web&DisplayPreferencesId=folder&SortBy=Unsupported&SortOrder=Ascending",
		"unsupported direction": "Client=web&DisplayPreferencesId=folder&SortBy=SortName&SortOrder=Unsupported",
		"mismatched directions": "Client=web&DisplayPreferencesId=folder&SortBy=Name&SortOrder=Ascending,Descending",
	}
	for name, value := range map[string]string{
		"oversized": strings.Repeat("a", 257), "leading whitespace": " scope", "control character": "scope\x00", "invalid UTF-8": "\xff",
	} {
		cases["invalid id "+name] = "Client=web&DisplayPreferencesId=" + url.QueryEscape(value) + "&SortBy=Name&SortOrder=Ascending"
		cases["invalid client "+name] = "DisplayPreferencesId=folder&Client=" + url.QueryEscape(value) + "&SortBy=Name&SortOrder=Ascending"
	}
	for name, rawQuery := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Items?"+rawQuery, nil)
			r = r.WithContext(context.WithValue(r.Context(), principalKey, identity.Principal{Kind: "emby"}))
			response := httptest.NewRecorder()
			query := library.Query{UserID: "viewer"}
			if (&Server{}).applyDisplayPreferenceDefaults(response, r, &query) || response.Code != http.StatusBadRequest {
				t.Fatalf("invalid preference query was accepted: status %d", response.Code)
			}
		})
	}
}

func TestApplyDisplayPreferenceDefaultsPreservesBypasses(t *testing.T) {
	application := identity.Principal{Kind: identity.ApplicationKeyKind, ApplicationKeyID: 1, SessionID: "credential", ClientSessionID: "client"}
	for _, test := range []struct {
		name, rawQuery, userID string
		actor                  identity.Principal
	}{
		{"missing client", "DisplayPreferencesId=bad%00", "viewer", identity.Principal{Kind: "emby"}},
		{"missing scope", "Client=bad%00", "viewer", identity.Principal{Kind: "emby"}},
		{"application key", "DisplayPreferencesId=bad%00&Client=bad%00", "viewer", application},
		{"missing user", "DisplayPreferencesId=bad%00&Client=bad%00", "", identity.Principal{Kind: "emby"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/emby/Items?"+test.rawQuery+"&SortBy=Unsupported&SortOrder=Unsupported", nil)
			r = r.WithContext(context.WithValue(r.Context(), principalKey, test.actor))
			query := library.Query{UserID: test.userID}
			if !(&Server{}).applyDisplayPreferenceDefaults(httptest.NewRecorder(), r, &query) {
				t.Fatal("an existing preference bypass was rejected")
			}
			r.URL.RawQuery += "&sortby=Name"
			response := httptest.NewRecorder()
			if (&Server{}).applyDisplayPreferenceDefaults(response, r, &query) || response.Code != http.StatusBadRequest {
				t.Fatal("a preference bypass accepted ambiguous sort parameters")
			}
		})
	}
}
