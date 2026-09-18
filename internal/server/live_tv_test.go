package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func liveTVTestRequest(rawQuery string, principal identity.Principal) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/emby/LiveTv/Programs", nil)
	r.URL.RawQuery = rawQuery
	return r.WithContext(context.WithValue(r.Context(), principalKey, principal))
}

func liveTVTestViewer() identity.Principal {
	return identity.Principal{Kind: "emby", SessionID: "viewer-session", User: identity.User{ID: "viewer"}}
}

func TestLiveTVProgramsQueryObservedRequest(t *testing.T) {
	values := url.Values{
		"UserId":           {"viewer"},
		"HasAired":         {"false"},
		"SortBy":           {"StartDate"},
		"ImageTypeLimit":   {"1"},
		"EnableImageTypes": {"Primary,Thumb,Backdrop"},
		"EnableUserData":   {"false"},
		"Fields":           {"PrimaryImageAspectRatio,ChannelInfo"},
		"Limit":            {"12"},
		"LibrarySeriesId":  {"series"},
		"X-Emby-Language":  {"en-us"},
		"X-Emby-Token":     {"test-token"},
		"X-Emby-Client":    {"Emby Web"},
	}
	r := liveTVTestRequest(values.Encode(), liveTVTestViewer())
	query, err := parseLiveTVProgramsQuery(r)
	if err != nil {
		t.Fatalf("observed query rejected: %v", err)
	}
	if query.UserID != "viewer" || query.LibrarySeriesID != "series" || query.SortBy != "StartDate" ||
		query.HasAired == nil || *query.HasAired || query.EnableUserData == nil || *query.EnableUserData ||
		query.Limit == nil || *query.Limit != 12 || query.ImageTypeLimit == nil || *query.ImageTypeLimit != 1 ||
		!reflect.DeepEqual(query.EnableImageTypes, []string{"Primary", "Thumb", "Backdrop"}) ||
		!reflect.DeepEqual(query.Fields, []string{"PrimaryImageAspectRatio", "ChannelInfo"}) {
		t.Fatalf("observed query changed during parsing: %#v", query)
	}
	if r.URL.RawQuery != values.Encode() {
		t.Fatal("Programs parsing modified the original query")
	}
	business, err := embyBusinessQuery(r)
	if err != nil || business.Get("X-Emby-Language") != "en-us" {
		t.Fatal("the endpoint's locale hint escaped into shared transport handling")
	}
	if configurationQuery(httptest.NewRecorder(), liveTVTestRequest("X-Emby-Language=en-us", liveTVTestViewer())) {
		t.Fatal("an unrelated endpoint inherited the Programs query allowlist")
	}
}

func TestLiveTVProgramsQueryValidBoundaries(t *testing.T) {
	for name, raw := range map[string]string{
		"omitted":            "",
		"empty user default": "UserId=&LibrarySeriesId=",
		"true filters":       "HasAired=true&EnableUserData=true",
		"zero limits":        "Limit=0&ImageTypeLimit=0",
		"int32 limits":       "Limit=2147483647&ImageTypeLimit=2147483647",
		"reordered CSV":      "Fields=ChannelInfo,PrimaryImageAspectRatio&EnableImageTypes=Backdrop,Thumb,Primary",
		"CSV spaces":         "Fields=ChannelInfo%2C%20PrimaryImageAspectRatio&EnableImageTypes=%20Thumb%20",
		"partial CSV":        "Fields=ChannelInfo&EnableImageTypes=Backdrop",
		"opaque IDs":         "UserId=" + url.QueryEscape("custom:user/\u754c") + "&LibrarySeriesId=" + url.QueryEscape("series:custom/\u754c"),
		"ID byte boundary":   "UserId=" + strings.Repeat("u", 256) + "&LibrarySeriesId=" + url.QueryEscape(strings.Repeat("\u754c", 85)+"s"),
		"locale case":        "x-emby-language=en-US",
		"empty locale":       "X-Emby-Language=",
		"locale boundary":    "X-Emby-Language=" + strings.Repeat("a", 256),
		"equal auth repeat":  "X-Emby-Token=test-token&api_key=test-token",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseLiveTVProgramsQuery(liveTVTestRequest(raw, liveTVTestViewer())); err != nil {
				t.Fatalf("valid query rejected: %v", err)
			}
		})
	}
}

func TestLiveTVProgramsQueryRejectsInvalidInput(t *testing.T) {
	cases := map[string]string{
		"unknown field":       "StartIndex=0",
		"unknown transport":   "X-Emby-Unknown=ignored",
		"claimed user hint":   "X-Emby-UserId=administrator",
		"business case":       "userid=viewer",
		"business alias":      "UserId=viewer&userid=viewer",
		"empty boolean":       "HasAired=",
		"boolean alias":       "HasAired=0",
		"boolean case":        "EnableUserData=TRUE",
		"unknown sort":        "SortBy=Name",
		"empty sort":          "SortBy=",
		"repeated sort value": "SortBy=StartDate,StartDate",
		"unknown field enum":  "Fields=ChannelInfo,Path",
		"unknown image enum":  "EnableImageTypes=Logo",
		"empty fields":        "Fields=",
		"empty images":        "EnableImageTypes=",
		"empty CSV member":    "Fields=ChannelInfo,",
		"duplicate fields":    "Fields=ChannelInfo,ChannelInfo",
		"duplicate images":    "EnableImageTypes=Primary,%20Primary",
		"CSV control":         "Fields=ChannelInfo,%09PrimaryImageAspectRatio",
		"locale duplicate":    "X-Emby-Language=en-us&x-emby-language=en-us",
		"locale newline":      "X-Emby-Language=en%0aus",
		"locale invalid UTF8": "X-Emby-Language=%ff",
		"locale too long":     "X-Emby-Language=" + strings.Repeat("a", 257),
		"bad percent":         "LibrarySeriesId=%gg",
		"bad transport":       "X-Emby-Device-Id=%ff",
		"token conflict":      "X-Emby-Token=first&api_key=second",
	}
	for _, name := range []string{"UserId", "LibrarySeriesId"} {
		for suffix, value := range map[string]string{
			"whitespace": "%20%20", "NUL": "%00", "control": "%7f", "Unicode control": "%C2%85", "invalid UTF8": "%ff",
			"too long": strings.Repeat("a", 257),
		} {
			cases[name+" "+suffix] = name + "=" + value
		}
	}
	for _, name := range []string{"Limit", "ImageTypeLimit"} {
		for suffix, value := range map[string]string{
			"empty": "", "negative": "-1", "negative zero": "-0", "leading zero": "01", "plus": "%2b1",
			"space": "%201", "fraction": "1.0", "overflow": "2147483648", "large overflow": "9999999999999999999999",
		} {
			cases[name+" "+suffix] = name + "=" + value
		}
	}
	for name, value := range map[string]string{
		"UserId": "viewer", "LibrarySeriesId": "series", "HasAired": "false", "SortBy": "StartDate", "ImageTypeLimit": "1",
		"EnableImageTypes": "Primary", "EnableUserData": "false", "Fields": "ChannelInfo", "Limit": "12", "X-Emby-Language": "en-us",
	} {
		cases[name+" repeated parameter"] = name + "=" + value + "&" + name + "=" + value
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseLiveTVProgramsQuery(liveTVTestRequest(raw, liveTVTestViewer())); !errors.Is(err, identity.ErrInvalidInput) {
				t.Fatalf("invalid query did not return invalid input: %v", err)
			}
		})
	}
	r := liveTVTestRequest("X-Emby-Token=query-token", liveTVTestViewer())
	r.Header.Set("X-Emby-Token", "header-token")
	if _, err := parseLiveTVProgramsQuery(r); !errors.Is(err, identity.ErrInvalidInput) {
		t.Fatal("the endpoint discarded a header/query credential conflict")
	}
}

type liveTVTestCatalog struct {
	item         library.Item
	libraries    []library.Library
	err          error
	itemIDs      []string
	subjects     []library.Subject
	libraryCalls int
}

func (catalog *liveTVTestCatalog) GetItemFor(_ context.Context, subject library.Subject, id string) (library.Item, error) {
	catalog.itemIDs = append(catalog.itemIDs, id)
	catalog.subjects = append(catalog.subjects, subject)
	return catalog.item, catalog.err
}

func (catalog *liveTVTestCatalog) ListUserLibrariesFor(_ context.Context, subject library.Subject) ([]library.Library, error) {
	catalog.libraryCalls++
	catalog.subjects = append(catalog.subjects, subject)
	return catalog.libraries, catalog.err
}

func TestLiveTVProgramsAuthorizesTheRequestedSubjectBeforeReturningEmptyGuide(t *testing.T) {
	viewer := liveTVTestViewer()
	admin := identity.Principal{Kind: "emby", SessionID: "admin-session", User: identity.User{ID: "admin", IsAdministrator: true}}
	key := identity.Principal{Kind: identity.ApplicationKeyKind, SessionID: "application-credential", ClientSessionID: "application-client", ApplicationKeyID: 1}
	for _, tc := range []struct {
		name      string
		principal identity.Principal
		query     string
		subject   library.Subject
		series    string
	}{
		{"implicit self", viewer, "", library.Subject{UserID: "viewer", Actor: &viewer}, ""},
		{"empty user defaults to self", viewer, "UserId=", library.Subject{UserID: "viewer", Actor: &viewer}, ""},
		{"explicit self", viewer, "UserId=viewer", library.Subject{UserID: "viewer", Actor: &viewer}, ""},
		{"visible series", viewer, "LibrarySeriesId=series", library.Subject{UserID: "viewer", Actor: &viewer}, "series"},
		{"zero limit catalog", viewer, "Limit=0&EnableUserData=false", library.Subject{UserID: "viewer", Actor: &viewer}, ""},
		{"zero limit series", viewer, "Limit=0&EnableUserData=false&LibrarySeriesId=series", library.Subject{UserID: "viewer", Actor: &viewer}, "series"},
		{"admin target catalog", admin, "UserId=target", library.Subject{UserID: "target", Actor: &admin}, ""},
		{"admin target series", admin, "UserId=target&LibrarySeriesId=series", library.Subject{UserID: "target", Actor: &admin}, "series"},
		{"userless key catalog", key, "", library.Subject{ApplicationCredentialID: "application-credential"}, ""},
		{"userless key series", key, "LibrarySeriesId=series", library.Subject{ApplicationCredentialID: "application-credential"}, "series"},
		{"key target catalog", key, "UserId=target", library.Subject{UserID: "target", ApplicationCredentialID: "application-credential"}, ""},
		{"key target series", key, "UserId=target&LibrarySeriesId=series", library.Subject{UserID: "target", ApplicationCredentialID: "application-credential"}, "series"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			catalog := &liveTVTestCatalog{
				item:      library.Item{ID: "series", Type: "Series"},
				libraries: []library.Library{{ID: "populated-tv-library", CollectionType: "tvshows"}},
			}
			w := httptest.NewRecorder()
			server := &Server{}
			server.serveLiveTVPrograms(w, liveTVTestRequest(tc.query, tc.principal), catalog)
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("authorized guide returned %d: %s", w.Code, w.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body) != 2 || string(body["Items"]) != "[]" || string(body["TotalRecordCount"]) != "0" {
				t.Fatalf("empty EPG wire shape changed: %s", w.Body.String())
			}
			if !reflect.DeepEqual(catalog.subjects, []library.Subject{tc.subject}) {
				t.Fatalf("wrong or missing subject authorization: %#v", catalog.subjects)
			}
			if tc.series != "" {
				if catalog.libraryCalls != 0 || !reflect.DeepEqual(catalog.itemIDs, []string{tc.series}) {
					t.Fatal("Series query did not authorize exactly the selected item")
				}
			} else if catalog.libraryCalls != 1 || len(catalog.itemIDs) != 0 {
				t.Fatal("unfiltered guide skipped the current catalog scope check")
			}
		})
	}
}

func TestLiveTVProgramsRejectsCrossUserAndMalformedQueriesBeforeCatalog(t *testing.T) {
	for _, tc := range []struct {
		query  string
		status int
	}{
		{"UserId=other", http.StatusForbidden},
		{"UserId=other&LibrarySeriesId=series&Limit=0", http.StatusForbidden},
		{"UserId=viewer&UserId=viewer", http.StatusBadRequest},
		{"LibrarySeriesId=series&Fields=Path", http.StatusBadRequest},
		{"LibrarySeriesId=series&X-Emby-Language=en%0aus", http.StatusBadRequest},
	} {
		catalog := &liveTVTestCatalog{item: library.Item{Type: "Series"}}
		w := httptest.NewRecorder()
		server := &Server{}
		server.serveLiveTVPrograms(w, liveTVTestRequest(tc.query, liveTVTestViewer()), catalog)
		if w.Code != tc.status || len(catalog.subjects) != 0 {
			t.Fatalf("rejected query reached the catalog or returned %d, expected %d", w.Code, tc.status)
		}
	}
}

func TestLiveTVProgramsPreservesCatalogFailures(t *testing.T) {
	for _, query := range []string{"Limit=0&EnableUserData=false", "LibrarySeriesId=series&Limit=0&EnableUserData=false"} {
		for _, tc := range []struct {
			name   string
			err    error
			status int
		}{
			{"unauthorized credential", identity.ErrUnauthorized, http.StatusUnauthorized},
			{"forbidden subject", library.ErrForbidden, http.StatusForbidden},
			{"absent or invisible resource", library.ErrNotFound, http.StatusNotFound},
			{"invalid subject", library.ErrInvalidInput, http.StatusBadRequest},
			{"unavailable catalog", library.ErrUnavailable, http.StatusServiceUnavailable},
			{"unexpected failure", errors.New("catalog read failed"), http.StatusInternalServerError},
		} {
			t.Run(query+"/"+tc.name, func(t *testing.T) {
				catalog := &liveTVTestCatalog{item: library.Item{Type: "Series"}, err: tc.err}
				server := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
				w := httptest.NewRecorder()
				server.serveLiveTVPrograms(w, liveTVTestRequest(query, liveTVTestViewer()), catalog)
				if w.Code != tc.status || len(catalog.subjects) != 1 || strings.Contains(w.Body.String(), "\"Items\"") {
					t.Fatalf("catalog failure was hidden: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestLiveTVProgramsRequiresAnAuthorizedSeries(t *testing.T) {
	for _, itemType := range []string{"Episode", "Season", "Movie", "CollectionFolder", ""} {
		catalog := &liveTVTestCatalog{item: library.Item{ID: "item", Type: itemType}}
		server := &Server{}
		w := httptest.NewRecorder()
		server.serveLiveTVPrograms(w, liveTVTestRequest("LibrarySeriesId=item", liveTVTestViewer()), catalog)
		if w.Code != http.StatusBadRequest || len(catalog.subjects) != 1 {
			t.Fatalf("non-Series %q produced an EPG result: %d", itemType, w.Code)
		}
	}
}

func TestLiveTVProgramsProductionHandlerRequiresTheCatalog(t *testing.T) {
	for _, query := range []string{"", "LibrarySeriesId=series"} {
		server := &Server{}
		w := httptest.NewRecorder()
		server.embyLiveTVPrograms(w, liveTVTestRequest(query, liveTVTestViewer()))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("production handler manufactured a result without a catalog: %d", w.Code)
		}
	}
}

func TestLiveTVProgramsRequiresEmbyAuthentication(t *testing.T) {
	server := &Server{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/emby/LiveTv/Programs?UserId=viewer&LibrarySeriesId=series", nil)
	server.requireEmby(server.embyLiveTVPrograms)(w, r)
	if w.Code != http.StatusUnauthorized || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("claimed user bypassed Emby authentication: %d", w.Code)
	}
}

type liveTVTestBody struct {
	reads  int
	closes int
}

func (body *liveTVTestBody) Read([]byte) (int, error) {
	body.reads++
	return 0, errors.New("the GET body must not be read")
}

func (body *liveTVTestBody) Close() error {
	body.closes++
	return nil
}

func TestLiveTVProgramsRejectsAndRetiresRequestBodies(t *testing.T) {
	for _, length := range []int64{1, -1, 0} {
		body := &liveTVTestBody{}
		r := liveTVTestRequest("LibrarySeriesId=series", liveTVTestViewer())
		r.Body, r.ContentLength = body, length
		if length == -1 {
			r.TransferEncoding = []string{"chunked"}
		}
		catalog := &liveTVTestCatalog{item: library.Item{Type: "Series"}}
		server := &Server{}
		w := httptest.NewRecorder()
		withBoundedRequestBodies(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			server.serveLiveTVPrograms(w, r, catalog)
		})).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || len(catalog.subjects) != 0 || body.reads != 0 || body.closes != 1 || w.Header().Get("Connection") != "close" {
			t.Fatalf("GET body was not rejected and retired: status=%d reads=%d closes=%d", w.Code, body.reads, body.closes)
		}
	}
}
